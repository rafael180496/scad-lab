package render

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"scadlab/backend/i18n"
)

const (
	defaultTimeoutSeconds = 300
	// maxLogBytes es el recorte del log que viaja en el Result (contrato: 64 KB).
	maxLogBytes = 64 << 10
	// maxOutputBytes es lo que se retiene de la salida para parsearla.
	maxOutputBytes = 4 << 20
	// versionTimeout: `openscad --version` es instantáneo; si tarda más, algo
	// está mal con el binario y no vale la pena esperar.
	versionTimeout = 10 * time.Second
	// waitDelay: tras matar a OpenSCAD, cuánto esperar a que se cierren sus
	// tuberías. Sin esto un nieto que herede stdout dejaría a Wait colgado y el
	// render cancelado no volvería nunca.
	waitDelay = 3 * time.Second
)

type job struct {
	id     uint64
	cancel context.CancelFunc
}

type engine struct {
	cacheDir      string
	maxCacheBytes int64

	mu     sync.Mutex
	jobs   map[string]*job // WorkspaceKey → render en curso
	seq    uint64
	closed bool

	metaMu      sync.Mutex
	versions    map[string]string // ruta|mtime|tamaño del binario → versión
	unsupported map[string]bool   // ruta\x00versión\x00opción → OpenSCAD la rechazó

	pruneMu sync.Mutex

	// slots limita cuántos OpenSCAD corren a la vez en todo el proceso. Con
	// varios proyectos abiertos (S8) y auto-render, cada uno podía lanzar el
	// suyo; Manifold ya usa todos los núcleos en un solo render, así que dos o
	// más en paralelo se reparten la CPU y la máquina entera se arrastra con un
	// modelo pesado. Los que esperan turno siguen siendo cancelables.
	slots chan struct{}
}

// maxConcurrentRenders es el tope de OpenSCAD simultáneos (ver engine.slots).
const maxConcurrentRenders = 2

func newEngine(cacheDir string) (*engine, error) {
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, i18n.Errorf(i18n.Msg{
			ES: "no se pudo crear la caché de mallas %s: %w",
			EN: "could not create the mesh cache %s: %w",
		}, cacheDir, err)
	}
	e := &engine{
		cacheDir:      cacheDir,
		maxCacheBytes: defaultMaxCacheBytes,
		jobs:          map[string]*job{},
		versions:      map[string]string{},
		unsupported:   map[string]bool{},
		slots:         make(chan struct{}, maxConcurrentRenders),
	}
	// Una poda al arrancar recoge temporales de una sesión que murió. Es un
	// ReadDir de una carpeta: barato aunque la caché esté llena.
	e.prune()
	return e, nil
}

// enqueue registra un render para key, cancelando el anterior de la misma
// clave. El cancelado se entera por su ctx y devuelve StatusCancelled.
func (e *engine) enqueue(parent context.Context, key string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		cancel()
		return ctx, func() {}
	}
	if prev := e.jobs[key]; prev != nil {
		prev.cancel()
	}
	e.seq++
	j := &job{id: e.seq, cancel: cancel}
	e.jobs[key] = j
	return ctx, func() {
		cancel()
		e.mu.Lock()
		if e.jobs[key] == j {
			delete(e.jobs, key)
		}
		e.mu.Unlock()
	}
}

func (e *engine) cancel(key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if j := e.jobs[key]; j != nil {
		j.cancel()
	}
}

func (e *engine) close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.closed = true
	for _, j := range e.jobs {
		j.cancel()
	}
}

func (e *engine) render(parent context.Context, req Request, opts Options) (Result, error) {
	start := time.Now()
	res := Result{FileName: req.FileName, Diagnostics: []Diagnostic{}}
	finish := func(r Result) (Result, error) {
		r.DurationMs = time.Since(start).Milliseconds()
		if r.Diagnostics == nil {
			r.Diagnostics = []Diagnostic{}
		}
		return r, nil
	}

	// Sin OpenSCAD no se ejecuta nada: la interfaz muestra el aviso con el
	// enlace a Configuración, no un error de render.
	if opts.OpenSCADPath == "" {
		res.Status = StatusMissing
		return finish(res)
	}

	ctx, done := e.enqueue(parent, req.WorkspaceKey)
	defer done()

	src, err := os.ReadFile(filepath.Join(req.WorkspaceDir, req.FileName))
	if err != nil {
		return res, i18n.Errorf(i18n.Msg{
			ES: "no se pudo leer %s: %w",
			EN: "could not read %s: %w",
		}, req.FileName, err)
	}

	version := e.version(ctx, opts.OpenSCADPath)
	res.Version = version
	useBackend := opts.Backend != "cgal" && !e.isUnsupported(opts.OpenSCADPath, version, "--backend")
	useFormat := !e.isUnsupported(opts.OpenSCADPath, version, "--export-format")
	backendName := func() string {
		if useBackend {
			return "manifold"
		}
		return "cgal"
	}

	sum := contentHash(req.WorkspaceDir, req.FileName, src)
	key := cacheKey(sum, backendName(), version)
	if cached, ok := e.loadCached(key); ok {
		cached.FileName = req.FileName
		cached.CacheHit = true
		return finish(cached)
	}
	if ctx.Err() != nil {
		res.Status = StatusCancelled
		res.Backend = backendName()
		return finish(res)
	}

	timeout := opts.TimeoutSeconds
	if timeout <= 0 {
		timeout = defaultTimeoutSeconds
	}
	runCtx, cancelRun := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancelRun()

	// El STL se escribe en un temporal con extensión .stl (OpenSCAD deduce el
	// formato de la extensión cuando no acepta --export-format) y se renombra a
	// la caché solo si el render salió bien: el visor nunca lee media malla.
	tmp, err := os.CreateTemp(e.cacheDir, key[:16]+"-*.stl")
	if err != nil {
		return res, i18n.Errorf(i18n.Msg{
			ES: "no se pudo crear el archivo temporal del render: %w",
			EN: "could not create the render temp file: %w",
		}, err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(tmpPath) // OpenSCAD lo crea; si queda vacío y existe, parece un render bueno
	defer os.Remove(tmpPath)

	var output string
	var runErr error
	// Como mucho tres intentos: completo, sin --backend, sin --export-format.
	// Cada opción rechazada se recuerda por binario+versión para no pagar el
	// intento fallido en cada render.
	for range 3 {
		output, runErr = e.run(runCtx, opts.OpenSCADPath, req.WorkspaceDir, req.FileName, tmpPath, useBackend, useFormat)
		if runErr == nil || runCtx.Err() != nil {
			break
		}
		if useBackend && rejectsOption(output, "backend") {
			e.markUnsupported(opts.OpenSCADPath, version, "--backend")
			useBackend = false
			continue
		}
		if useFormat && rejectsOption(output, "export-format") {
			e.markUnsupported(opts.OpenSCADPath, version, "--export-format")
			useFormat = false
			continue
		}
		break
	}
	res.Backend = backendName()
	res.Log = truncateLog(output)

	// Cancelado (render nuevo de la misma clave, Cancel, Close o el ctx del
	// llamador) se distingue del plazo vencido: el primero no es un error del
	// modelo y no debe dejar RENDER_ERRORS.md; el segundo sí.
	if errors.Is(ctx.Err(), context.Canceled) {
		res.Status = StatusCancelled
		return finish(res)
	}
	if runCtx.Err() != nil {
		res.Status = StatusError
		res.Diagnostics = append(parseOutput(output, req.WorkspaceDir, req.FileName), Diagnostic{
			Severity: SeverityError,
			Message: i18n.T(i18n.Msg{
				ES: "OpenSCAD superó el tiempo máximo de render (%d s). Simplifica el modelo (menos $fn, menos minkowski/hull anidados) o sube el límite en Configuración → OpenSCAD",
				EN: "OpenSCAD exceeded the maximum render time (%d s). Simplify the model (lower $fn, fewer nested minkowski/hull) or raise the limit in Settings → OpenSCAD",
			}, timeout),
		})
		return finish(res)
	}

	res.Diagnostics = parseOutput(output, req.WorkspaceDir, req.FileName)
	hasError := false
	for _, d := range res.Diagnostics {
		if d.Severity == SeverityError {
			hasError = true
			break
		}
	}
	meshOK := false
	if st, err := os.Stat(tmpPath); err == nil && st.Size() > 0 {
		meshOK = true
	}

	// msgNot3D se usa tanto si OpenSCAD lo dice explícitamente como si terminó
	// sin escribir la malla: en los dos casos la causa habitual es un modelo 2D.
	msgNot3D := Diagnostic{
		Severity: SeverityError,
		Message: i18n.T(i18n.Msg{
			ES: "OpenSCAD terminó sin escribir la malla. Si el modelo es 2D, extrúyelo (linear_extrude) para exportarlo a STL",
			EN: "OpenSCAD finished without writing the mesh. If the model is 2D, extrude it (linear_extrude) to export it as STL",
		}),
	}

	switch {
	case isNot3D(output) && !hasError:
		res.Status = StatusError
		res.Diagnostics = append(res.Diagnostics, msgNot3D)
	// Un assert() que falla deja también "top level object is empty": el error
	// real ya está en los diagnósticos y sumarle "modelo vacío" mandaría a
	// buscar la causa en el lugar equivocado.
	case isEmptyModel(output) && !hasError:
		res.Status = StatusError
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Severity: SeverityError,
			Message: i18n.T(i18n.Msg{
				ES: "El modelo está vacío: el nivel superior no genera geometría. Revisa que haya al menos un objeto fuera de module/function, que se llame al módulo principal y que un difference() no reste la pieza entera",
				EN: "The model is empty: the top level produces no geometry. Check that at least one object is outside module/function, that the main module is called, and that a difference() does not subtract the whole part",
			}),
		})
	case runErr != nil:
		res.Status = StatusError
		if !hasError {
			// OpenSCAD falló sin una línea ERROR (crash, opción rechazada,
			// biblioteca faltante): igual hay que decir algo con sentido.
			res.Diagnostics = append(res.Diagnostics, Diagnostic{
				Severity: SeverityError,
				Message: i18n.T(i18n.Msg{
					ES: "OpenSCAD terminó con error (%s). Revisa el log del render",
					EN: "OpenSCAD exited with an error (%s). Check the render log",
				}, exitDescription(runErr)),
			})
		}
	case hasError:
		res.Status = StatusError
	case !meshOK:
		res.Status = StatusError
		res.Diagnostics = append(res.Diagnostics, msgNot3D)
	default:
		res.Status = StatusOK
	}

	if res.Status != StatusOK {
		return finish(res)
	}
	// La clave se recalcula: si Manifold cayó a CGAL en este render, la malla
	// es de CGAL y tiene que quedar bajo esa clave.
	key = cacheKey(sum, res.Backend, version)
	res.DurationMs = time.Since(start).Milliseconds()
	stored, err := e.storeCached(key, tmpPath, res)
	if err != nil {
		return res, i18n.Errorf(i18n.Msg{
			ES: "no se pudo guardar la malla en la caché: %w",
			EN: "could not store the mesh in the cache: %w",
		}, err)
	}
	e.prune()
	return finish(stored)
}

// run ejecuta OpenSCAD una vez, sin shell intermedia y con los argumentos como
// lista (un nombre de archivo con espacios o comillas no rompe nada).
func (e *engine) run(ctx context.Context, bin, dir, file, out string, backend, format bool) (string, error) {
	select {
	case e.slots <- struct{}{}:
		defer func() { <-e.slots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	args := []string{"-o", out}
	if format {
		args = append(args, "--export-format", "binstl")
	}
	if backend {
		args = append(args, "--backend=manifold")
	}
	args = append(args, file)
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	cmd.WaitDelay = waitDelay
	hideWindow(cmd)
	buf := &limitedBuffer{max: maxOutputBytes}
	cmd.Stdout = buf
	cmd.Stderr = buf
	err := cmd.Run()
	return buf.String(), err
}

var versionRe = regexp.MustCompile(`\d{4}\.\d{2}(\.\d{2})?([.-][0-9A-Za-z]+)*|\d+\.\d+(\.\d+)?`)

// version devuelve la versión de OpenSCAD (`--version`, que la imprime por
// stderr). Se cachea por ruta + fecha + tamaño del binario: correrla en cada
// render costaría un proceso más, y con solo la ruta una actualización en el
// mismo lugar dejaría la versión vieja (y la caché de mallas apuntando a ella)
// hasta reiniciar la app.
func (e *engine) version(ctx context.Context, bin string) string {
	id := bin
	if st, err := os.Stat(bin); err == nil {
		id = fmt.Sprintf("%s|%d|%d", bin, st.ModTime().UnixNano(), st.Size())
	}
	e.metaMu.Lock()
	v, ok := e.versions[id]
	e.metaMu.Unlock()
	if ok {
		return v
	}
	vctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	cmd := exec.CommandContext(vctx, bin, "--version")
	cmd.WaitDelay = waitDelay
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		// No se cachea el fallo: puede ser un cancel del render, no del binario.
		return ""
	}
	v = versionRe.FindString(strings.TrimSpace(string(out)))
	e.metaMu.Lock()
	e.versions[id] = v
	e.metaMu.Unlock()
	return v
}

func (e *engine) isUnsupported(bin, version, flag string) bool {
	e.metaMu.Lock()
	defer e.metaMu.Unlock()
	return e.unsupported[bin+"\x00"+version+"\x00"+flag]
}

func (e *engine) markUnsupported(bin, version, flag string) {
	e.metaMu.Lock()
	defer e.metaMu.Unlock()
	e.unsupported[bin+"\x00"+version+"\x00"+flag] = true
}

func exitDescription(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exit %d", ee.ExitCode())
	}
	return err.Error()
}

// truncateLog deja los últimos 64 KB: los errores y el resumen del render están
// al final, lo que se pierde es el ruido de los echo() del principio.
func truncateLog(s string) string {
	if len(s) <= maxLogBytes {
		return s
	}
	cut := s[len(s)-maxLogBytes:]
	if i := strings.IndexByte(cut, '\n'); i >= 0 && i < 1024 {
		cut = cut[i+1:]
	}
	return "…\n" + cut
}
