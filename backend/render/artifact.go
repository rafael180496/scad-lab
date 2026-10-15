package render

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"scadlab/backend/i18n"
)

// Este archivo agrega una segunda salida al motor sin tocar el contrato de
// types.go: renderizar el mismo checkpoint a otro formato (hoy 3MF, para la
// exportación de S9). Va en una carpeta de caché propia —no en la de mallas—
// porque la poda de esa carpeta solo conoce <clave>.stl + <clave>.json y
// borraría un .3mf a las 2 h como si fuera un temporal huérfano.

// maxArtifactCacheBytes es el tope de la caché de exportaciones: 512 MB.
const maxArtifactCacheBytes int64 = 512 << 20

// ArtifactExt3MF es la extensión del 3MF con colores por triángulo.
const ArtifactExt3MF = ".3mf"

// RenderArtifact renderiza req a un archivo con extensión ext (".3mf") dentro
// de dir, con caché por contenido + backend + versión + formato. El resultado
// usa el mismo Result que Render; MeshPath es la ruta del archivo generado.
//
// Corre en su propia cola (WorkspaceKey + ext): exportar no cancela el render
// del visor, ni al revés. Un fallo de OpenSCAD viaja en Status/Diagnostics; el
// error es solo para fallos del motor (archivo ilegible, disco lleno).
func (e *Engine) RenderArtifact(ctx context.Context, req Request, opts Options, ext, dir string) (Result, error) {
	return e.impl.renderArtifact(ctx, req, opts, ext, dir)
}

func (e *engine) renderArtifact(parent context.Context, req Request, opts Options, ext, dir string) (Result, error) {
	start := time.Now()
	res := Result{FileName: req.FileName, Diagnostics: []Diagnostic{}}
	finish := func(r Result) (Result, error) {
		r.DurationMs = time.Since(start).Milliseconds()
		if r.Diagnostics == nil {
			r.Diagnostics = []Diagnostic{}
		}
		return r, nil
	}
	if opts.OpenSCADPath == "" {
		res.Status = StatusMissing
		return finish(res)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, i18n.Errorf(i18n.Msg{
			ES: "no se pudo crear la caché de exportación %s: %w",
			EN: "could not create the export cache %s: %w",
		}, dir, err)
	}

	ctx, done := e.enqueue(parent, req.WorkspaceKey+":"+ext)
	defer done()

	src, err := os.ReadFile(filepath.Join(req.WorkspaceDir, req.FileName))
	if err != nil {
		return res, i18n.Errorf(i18n.Msg{ES: "no se pudo leer %s: %w", EN: "could not read %s: %w"}, req.FileName, err)
	}
	version := e.version(ctx, opts.OpenSCADPath)
	res.Version = version
	useBackend := opts.Backend != "cgal" && !e.isUnsupported(opts.OpenSCADPath, version, "--backend")
	// -O export-3mf/color-mode=model es el valor por defecto de OpenSCAD 2025+,
	// pero se pide explícito: con otro default el 3MF saldría sin colores. Una
	// versión que no conoce -O (2021.01) lo rechaza y se reintenta sin él.
	useColorOpt := ext == ArtifactExt3MF && !e.isUnsupported(opts.OpenSCADPath, version, "-O")
	backendName := func() string {
		if useBackend {
			return "manifold"
		}
		return "cgal"
	}
	artifactKey := func() string {
		return cacheKey(contentHash(req.WorkspaceDir, req.FileName, src), backendName()+"|"+ext, version)
	}

	key := artifactKey()
	if cached, ok := loadArtifact(dir, key, ext); ok {
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

	tmp, err := os.CreateTemp(dir, key[:16]+"-*"+ext)
	if err != nil {
		return res, i18n.Errorf(i18n.Msg{
			ES: "no se pudo crear el archivo temporal de la exportación: %w",
			EN: "could not create the export temp file: %w",
		}, err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	_ = os.Remove(tmpPath)
	defer os.Remove(tmpPath)

	var output string
	var runErr error
	for range 3 {
		output, runErr = e.runArtifact(runCtx, opts.OpenSCADPath, req.WorkspaceDir, req.FileName, tmpPath, useBackend, useColorOpt)
		if runErr == nil || runCtx.Err() != nil {
			break
		}
		if useBackend && rejectsOption(output, "backend") {
			e.markUnsupported(opts.OpenSCADPath, version, "--backend")
			useBackend = false
			continue
		}
		if useColorOpt && rejectsOption(output, "'-o'") {
			e.markUnsupported(opts.OpenSCADPath, version, "-O")
			useColorOpt = false
			continue
		}
		break
	}
	res.Backend = backendName()
	res.Log = truncateLog(output)

	if errors.Is(ctx.Err(), context.Canceled) {
		res.Status = StatusCancelled
		return finish(res)
	}
	res.Diagnostics = parseOutput(output, req.WorkspaceDir, req.FileName)
	if runCtx.Err() != nil {
		res.Status = StatusError
		res.Diagnostics = append(res.Diagnostics, Diagnostic{
			Severity: SeverityError,
			Message: i18n.T(i18n.Msg{
				ES: "OpenSCAD superó el tiempo máximo de render (%d s) al preparar la exportación. Simplifica el modelo o sube el límite en Configuración → OpenSCAD",
				EN: "OpenSCAD exceeded the maximum render time (%d s) while preparing the export. Simplify the model or raise the limit in Settings → OpenSCAD",
			}, timeout),
		})
		return finish(res)
	}
	hasError := false
	for _, d := range res.Diagnostics {
		if d.Severity == SeverityError {
			hasError = true
		}
	}
	st, statErr := os.Stat(tmpPath)
	switch {
	case runErr != nil || hasError || statErr != nil || st.Size() == 0:
		res.Status = StatusError
		if !hasError {
			msg := i18n.T(i18n.Msg{
				ES: "OpenSCAD no pudo generar el archivo de exportación. Revisa el log del render",
				EN: "OpenSCAD could not generate the export file. Check the render log",
			})
			if runErr != nil {
				msg = i18n.T(i18n.Msg{
					ES: "OpenSCAD terminó con error (%s) al preparar la exportación. Revisa el log del render",
					EN: "OpenSCAD exited with an error (%s) while preparing the export. Check the render log",
				}, exitDescription(runErr))
			}
			res.Diagnostics = append(res.Diagnostics, Diagnostic{Severity: SeverityError, Message: msg})
		}
		return finish(res)
	}
	res.Status = StatusOK

	// La clave se recalcula: si Manifold cayó a CGAL el archivo es de CGAL.
	key = artifactKey()
	res.DurationMs = time.Since(start).Milliseconds()
	stored, err := storeArtifact(dir, key, ext, tmpPath, res)
	if err != nil {
		return res, i18n.Errorf(i18n.Msg{
			ES: "no se pudo guardar la exportación en la caché: %w",
			EN: "could not store the export in the cache: %w",
		}, err)
	}
	pruneArtifacts(dir, maxArtifactCacheBytes)
	return finish(stored)
}

// runArtifact es run() para formatos sin --export-format: la extensión de out
// decide el formato.
func (e *engine) runArtifact(ctx context.Context, bin, dir, file, out string, backend, colorOpt bool) (string, error) {
	args := []string{"-o", out}
	if colorOpt {
		args = append(args, "-O", "export-3mf/color-mode=model")
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

func loadArtifact(dir, key, ext string) (Result, bool) {
	file := filepath.Join(dir, key+ext)
	if st, err := os.Stat(file); err != nil || st.Size() == 0 {
		return Result{}, false
	}
	b, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		return Result{}, false
	}
	var r Result
	if json.Unmarshal(b, &r) != nil || r.Status != StatusOK {
		return Result{}, false
	}
	now := time.Now()
	_ = os.Chtimes(file, now, now)
	_ = os.Chtimes(filepath.Join(dir, key+".json"), now, now)
	r.MeshPath = file
	if r.Diagnostics == nil {
		r.Diagnostics = []Diagnostic{}
	}
	return r, true
}

// storeArtifact mueve el temporal a la caché y escribe el .json después, igual
// que storeCached: un archivo sin .json es un miss; un .json sin archivo nunca.
func storeArtifact(dir, key, ext, tmpFile string, r Result) (Result, error) {
	file := filepath.Join(dir, key+ext)
	if err := os.Rename(tmpFile, file); err != nil {
		return r, err
	}
	r.MeshPath = file
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return r, err
	}
	meta := filepath.Join(dir, key+".json")
	tmp := meta + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return r, err
	}
	if err := os.Rename(tmp, meta); err != nil {
		_ = os.Remove(tmp)
		return r, err
	}
	return r, nil
}

// pruneArtifacts deja la caché por debajo de max borrando lo de acceso más
// viejo, y limpia temporales abandonados. Nunca falla la exportación.
func pruneArtifacts(dir string, max int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type item struct {
		key   string
		size  int64
		mtime time.Time
		files []string
	}
	byKey := map[string]*item{}
	var total int64
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		name := de.Name()
		ext := filepath.Ext(name)
		key := strings.TrimSuffix(name, ext)
		if _, err := hex.DecodeString(key); err != nil || len(key) != 64 || (ext != ".json" && ext != ArtifactExt3MF) {
			if time.Since(info.ModTime()) > staleTempAge {
				_ = os.Remove(filepath.Join(dir, name))
			}
			continue
		}
		it := byKey[key]
		if it == nil {
			it = &item{key: key}
			byKey[key] = it
		}
		it.size += info.Size()
		it.files = append(it.files, filepath.Join(dir, name))
		if info.ModTime().After(it.mtime) {
			it.mtime = info.ModTime()
		}
		total += info.Size()
	}
	if total <= max {
		return
	}
	items := make([]*item, 0, len(byKey))
	for _, it := range byKey {
		items = append(items, it)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mtime.Before(items[j].mtime) })
	for _, it := range items {
		if total <= max {
			break
		}
		for _, f := range it.files {
			_ = os.Remove(f)
		}
		total -= it.size
	}
}
