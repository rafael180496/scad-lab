// Package scadwasm ejecuta el OpenSCAD oficial compilado a WebAssembly dentro
// del proceso de Go, con wazero (Go puro, sin CGO). Es el motor de respaldo de
// ScadLab: se usa cuando no se encuentra un OpenSCAD nativo, y sirve igual al
// visor, a la exportación STL/3MF, a la estimación, al linter y al servidor MCP.
//
// El .wasm es el de openscad.org, sin modificar. No trae el sistema de archivos
// de JavaScript: lo que hace este paquete es el HOST de emscripten —los
// syscalls (un sistema de archivos virtual con montajes), las excepciones de
// C++ y setjmp/longjmp, el reloj, el entorno y el crecimiento de memoria—.
//
// Spec: .claude/specs/s11-motor-wasm-descarga.md. Notas: claude/17-motor-wasm.md.
package scadwasm

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/sys"

	"scadlab/assets/engines"
	"scadlab/backend/i18n"
)

// Version es lo que imprime `openscad --version` en este wasm. Entra en la
// clave de la caché de mallas como cualquier otra versión de OpenSCAD.
const Version = "2025.12.30.wasm30277"

// WasmSHA256 es el SHA-256 de openscad.wasm (dentro de
// OpenSCAD-2025.12.30.wasm30277-WebAssembly-web.zip, publicado por openscad.org).
const WasmSHA256 = "1db0bee42c5cf7e4a72208f7d85fc53c12be0980fd98b9f1dbd5e9dcdc732f72"

// Mount expone un directorio del disco dentro del árbol virtual del motor.
type Mount struct {
	Host     string // ruta del disco (absoluta; en Windows, con letra de unidad)
	Virtual  string // ruta dentro del motor, absoluta y con "/": "/work"
	ReadOnly bool
}

// RunSpec describe una ejecución de OpenSCAD.
type RunSpec struct {
	Args   []string          // argv sin argv[0], ya con rutas virtuales
	Cwd    string            // directorio actual, virtual; vacío = "/"
	Mounts []Mount           // lo único del disco que el motor puede ver (además de /fonts)
	Env    map[string]string // p. ej. OPENSCADPATH con rutas virtuales
}

// FontsDir es dónde se montan siempre las fuentes embebidas.
const FontsDir = "/fonts"

// maxOutputBytes es lo que se conserva de la salida (la cola: los ERROR y el
// resumen están al final). El motor de render recorta además a 64 KB.
const maxOutputBytes = 4 << 20

// Runner ejecuta OpenSCAD en WebAssembly. Compila el módulo la primera vez
// (2-3 s; con la caché en disco, unas decenas de ms) y después cada Run es una
// instancia nueva con memoria propia, así que es seguro correr varias a la vez.
type Runner struct {
	cacheDir string

	mu      sync.Mutex
	started bool
	done    chan struct{} // se cierra cuando termina la compilación en curso
	c       *compiled
	cerr    error

	debugCtx context.Context // solo tests: contexto de compilación (listeners)

	availOnce sync.Once
	avail     bool
	availErr  error
}

// compiled es el módulo listo para instanciar.
type compiled struct {
	rt    wazero.Runtime
	mod   wazero.CompiledModule
	cache wazero.CompilationCache // nil si la caché es solo en memoria
}

// New crea el Runner. cacheDir es <datos>/cache/wasm: ahí wazero guarda el
// código nativo ya compilado del módulo. Vacío = caché solo en memoria.
func New(cacheDir string) *Runner { return &Runner{cacheDir: cacheDir} }

// Available dice si el wasm embebido está y su SHA-256 coincide con el de
// openscad.org. Se calcula una vez (descomprimir y hashear son decenas de ms).
func (r *Runner) Available() bool {
	if r == nil {
		return false
	}
	r.availOnce.Do(func() {
		_, r.availErr = loadWasm()
		r.avail = r.availErr == nil
	})
	return r.avail
}

// loadWasm descomprime el wasm embebido y verifica su SHA-256: un binario
// distinto del que describe THIRD_PARTY_NOTICES.md no se ejecuta.
func loadWasm() ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(engines.OpenSCADWasmGz))
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "el motor integrado está dañado: %w", EN: "the built-in engine is damaged: %w"}, err)
	}
	defer zr.Close()
	data, err := io.ReadAll(zr)
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "el motor integrado está dañado: %w", EN: "the built-in engine is damaged: %w"}, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != WasmSHA256 {
		return nil, i18n.New(i18n.Msg{
			ES: "el motor integrado no coincide con el SHA-256 esperado: no se ejecuta",
			EN: "the built-in engine does not match the expected SHA-256: it will not run",
		})
	}
	return data, nil
}

// Warmup compila el módulo en segundo plano para que el primer render no pague
// la compilación. No bloquea; un error se devuelve en el primer Run.
func (r *Runner) Warmup() {
	if r == nil {
		return
	}
	go func() { _, _ = r.get(context.Background()) }()
}

// Close libera el módulo compilado y la caché en memoria.
func (r *Runner) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	c := r.c
	r.c = nil
	r.started = false
	r.mu.Unlock()
	if c != nil {
		err := c.rt.Close(context.Background())
		if c.cache != nil {
			_ = c.cache.Close(context.Background())
		}
		return err
	}
	return nil
}

// get devuelve el módulo compilado, compilándolo si hace falta. La compilación
// corre en su propia goroutine, desligada de ctx: si el primer llamador se
// cancela, el siguiente no vuelve a empezar de cero.
func (r *Runner) get(ctx context.Context) (*compiled, error) {
	r.mu.Lock()
	if r.c != nil {
		c := r.c
		r.mu.Unlock()
		return c, nil
	}
	if !r.started {
		r.started = true
		done := make(chan struct{})
		r.done = done
		go func() {
			c, err := r.build()
			r.mu.Lock()
			r.c, r.cerr = c, err
			if err != nil {
				r.started = false // un fallo no se recuerda: el próximo Run reintenta
			}
			r.mu.Unlock()
			close(done)
		}()
	}
	done := r.done
	r.mu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.c == nil {
		return nil, r.cerr
	}
	return r.c, nil
}

func (r *Runner) build() (*compiled, error) {
	data, err := loadWasm()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	if r.debugCtx != nil {
		ctx = r.debugCtx
	}

	cfg := wazero.NewRuntimeConfigCompiler().
		// OpenSCAD usa instrucciones atómicas (sin pthreads): hace falta la
		// extensión de threads.
		WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesThreads).
		// Cancelar el contexto de un Run corta la instancia aunque esté en medio
		// de un cálculo (un minkowski de minutos no tiene ninguna llamada al
		// host donde mirar una bandera: se probó). El costo es que wazero lanza
		// una goroutine por cada llamada al módulo; ver proc.nctx.
		WithCloseOnContextDone(true)
	var cache wazero.CompilationCache
	if r.cacheDir != "" {
		if err := os.MkdirAll(r.cacheDir, 0o755); err == nil {
			if c, cerr := wazero.NewCompilationCacheWithDir(r.cacheDir); cerr == nil {
				cache = c
				cfg = cfg.WithCompilationCache(cache)
			}
		}
	}
	closeAll := func(rt wazero.Runtime) {
		_ = rt.Close(ctx)
		if cache != nil {
			_ = cache.Close(ctx)
		}
	}
	rt := wazero.NewRuntimeWithConfig(ctx, cfg)
	mod, err := rt.CompileModule(ctx, data)
	if err != nil {
		closeAll(rt)
		return nil, i18n.Errorf(i18n.Msg{
			ES: "no se pudo compilar el motor integrado: %w",
			EN: "could not compile the built-in engine: %w",
		}, err)
	}
	if err := registerHost(ctx, rt, mod, data); err != nil {
		closeAll(rt)
		return nil, err
	}
	return &compiled{rt: rt, mod: mod, cache: cache}, nil
}

// Run ejecuta OpenSCAD con spec y devuelve stdout+stderr intercalados (con las
// rutas virtuales reescritas a las del disco), el código de salida y un error
// solo si falló el motor (trap, sin memoria, cancelación): un `exit 1` de
// OpenSCAD no es un error.
func (r *Runner) Run(ctx context.Context, spec RunSpec) (output string, exitCode int, err error) {
	if r == nil {
		return "", -1, errUnavailable()
	}
	c, err := r.get(ctx)
	if err != nil {
		return "", -1, err
	}
	p, err := newProc(spec)
	if err != nil {
		return "", -1, err
	}

	rctx := context.WithValue(ctx, procKey{}, p)
	mod, err := c.rt.InstantiateModule(rctx, c.mod, wazero.NewModuleConfig().WithName("").WithStartFunctions())
	if err != nil {
		p.fs.closeAll()
		if ctx.Err() != nil {
			return "", -1, ctx.Err()
		}
		return "", -1, i18n.Errorf(i18n.Msg{
			ES: "no se pudo iniciar el motor integrado: %w",
			EN: "could not start the built-in engine: %w",
		}, err)
	}

	defer p.fs.closeAll()
	defer mod.Close(context.Background())

	code, runErr := p.runGuarded(rctx, mod)
	out := rewritePaths(p.out.String(), p.mounts)
	if runErr != nil {
		if ctx.Err() != nil {
			return out, -1, ctx.Err()
		}
		var ee *sys.ExitError
		if errors.As(runErr, &ee) && (ee.ExitCode() == sys.ExitCodeContextCanceled || ee.ExitCode() == sys.ExitCodeDeadlineExceeded) {
			return out, -1, context.Canceled
		}
		return out, -1, i18n.Errorf(i18n.Msg{
			ES: "el motor integrado falló: %w",
			EN: "the built-in engine failed: %w",
		}, runErr)
	}
	return out, code, nil
}

func errUnavailable() error {
	return i18n.New(i18n.Msg{ES: "el motor integrado no está disponible", EN: "the built-in engine is not available"})
}

// procKey es la clave con la que las funciones del host encuentran el estado
// de la ejecución que las llamó (el módulo del host es uno solo por Runner).
type procKey struct{}

func procOf(ctx context.Context) *proc { return ctx.Value(procKey{}).(*proc) }

// newProc prepara el estado de una ejecución: valida los montajes y arma el
// árbol virtual.
func newProc(spec RunSpec) (*proc, error) {
	var mounts []*mountPoint
	var norm []Mount // montajes con la ruta del disco absoluta y limpia (para reescribir la salida)
	seen := map[string]bool{}
	add := func(m Mount, be backend) error {
		v := cleanVirtual(m.Virtual)
		if v == "" || v == "/" {
			return i18n.Errorf(i18n.Msg{ES: "ruta virtual de montaje no válida: %q", EN: "invalid mount virtual path: %q"}, m.Virtual)
		}
		if seen[v] {
			return i18n.Errorf(i18n.Msg{ES: "la ruta virtual %q está montada dos veces", EN: "the virtual path %q is mounted twice"}, v)
		}
		seen[v] = true
		mounts = append(mounts, &mountPoint{virtual: v, be: be})
		return nil
	}
	for _, m := range spec.Mounts {
		host, err := filepath.Abs(m.Host)
		if err != nil {
			return nil, err
		}
		st, err := os.Stat(host)
		if err != nil || !st.IsDir() {
			return nil, i18n.Errorf(i18n.Msg{ES: "el directorio %s no existe", EN: "the directory %s does not exist"}, m.Host)
		}
		if err := add(m, &hostBackend{root: host, ro: m.ReadOnly}); err != nil {
			return nil, err
		}
		norm = append(norm, Mount{Host: host, Virtual: cleanVirtual(m.Virtual), ReadOnly: m.ReadOnly})
	}
	if !seen[FontsDir] {
		if err := add(Mount{Virtual: FontsDir}, &fsBackend{fsys: engines.Fonts(), tag: "fonts"}); err != nil {
			return nil, err
		}
	}
	// /cache es memoria: fontconfig escribe ahí su caché de fuentes y, sin un
	// directorio escribible, avisa por la salida en cada ejecución.
	if !seen["/cache"] {
		if err := add(Mount{Virtual: "/cache"}, newMemBackend()); err != nil {
			return nil, err
		}
	}
	p := &proc{
		fs:     newVFS(mounts),
		out:    &tailBuffer{max: maxOutputBytes},
		mounts: norm,
	}
	cwd := spec.Cwd
	if cwd == "" {
		cwd = "/"
	}
	if e := p.fs.chdir(cwd); e != errnoOK {
		return nil, i18n.Errorf(i18n.Msg{ES: "el directorio de trabajo %q no existe en el motor", EN: "the working directory %q does not exist in the engine"}, cwd)
	}
	p.args = spec.Args
	envExtra := map[string]string{"FONTCONFIG_FILE": FontsDir + "/fonts.conf"}
	for k, v := range spec.Env {
		envExtra[k] = v
	}
	p.env = buildEnv(envExtra, p.fs.cwd)
	return p, nil
}

func cleanVirtual(v string) string {
	if !strings.HasPrefix(v, "/") {
		return ""
	}
	v = strings.TrimRight(v, "/")
	if v == "" {
		return "/"
	}
	return v
}

// tailBuffer conserva como mucho max bytes: la cola de la salida. Tiene un
// cerrojo porque Run lee la salida parcial de una ejecución cancelada mientras
// la goroutine del módulo todavía puede estar escribiendo.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailBuffer) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if len(t.buf) > 2*t.max {
		// Se recorta al doble y no en cada escritura para no copiar el buffer
		// entero por cada línea.
		t.buf = append(t.buf[:0], t.buf[len(t.buf)-t.max:]...)
	}
	return len(b), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.buf) > t.max {
		return string(t.buf[len(t.buf)-t.max:])
	}
	return string(t.buf)
}
