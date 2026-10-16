package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"scadlab/backend/exporter"
	"scadlab/backend/i18n"
	"scadlab/backend/printers"
	"scadlab/backend/render"
	"scadlab/backend/workspace"
)

// Bindings de exportación (S9). La lógica vive en backend/export; acá solo se
// resuelve el workspace, la impresora y el diálogo de guardar.

// exportJob reúne lo que backend/export necesita para un checkpoint.
func (a *App) exportJob(id int64, fileName string) (exporter.Job, string, string, error) {
	if err := a.ready(); err != nil {
		return exporter.Job{}, "", "", err
	}
	if a.engine == nil {
		return exporter.Job{}, "", "", i18n.Errorf(i18n.Msg{ES: "el motor de render no está disponible: %v", EN: "the render engine is not available: %v"}, a.engineErr)
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return exporter.Job{}, "", "", err
	}
	if err := validCheckpointName(fileName); err != nil {
		return exporter.Job{}, "", "", err
	}
	if _, err := os.Stat(filepath.Join(w.Path, fileName)); err != nil {
		return exporter.Job{}, "", "", i18n.Errorf(i18n.Msg{ES: "no se encontró el checkpoint %s", EN: "checkpoint %s was not found"}, fileName)
	}
	s, err := a.store.GetSettings()
	if err != nil {
		return exporter.Job{}, "", "", err
	}
	pr, _ := printers.ByID(w.PrinterID)
	job := exporter.Job{
		Engine: a.engine,
		// Cola propia: exportar no cancela el render del visor ni al revés.
		Req:      render.Request{WorkspaceKey: renderKey(id) + ":export", WorkspaceDir: w.Path, FileName: fileName},
		Opts:     a.renderOptions(s),
		CacheDir: filepath.Join(a.dataDir, "cache", "exports"),
		Printer:  pr,
	}
	return job, w.Path, workspace.Slug(w.Name), nil
}

// PreviewExportParts devuelve las partes (una por color) y los avisos que
// saldrían al exportar el checkpoint, sin guardar nada: la interfaz lo muestra
// antes de pedir dónde guardar. Renderiza el checkpoint a 3MF con OpenSCAD la
// primera vez (queda en caché, y la exportación posterior lo reutiliza).
func (a *App) PreviewExportParts(id int64, fileName string) (exporter.Preview, error) {
	job, _, _, err := a.exportJob(id, fileName)
	if err != nil {
		return exporter.Preview{}, err
	}
	p, err := exporter.Prepare(a.ctx, job, exporter.Format3MF)
	if err != nil {
		return exporter.Preview{}, err
	}
	return p.Preview(), nil
}

// ExportCheckpoint exporta un checkpoint ("stl" | "3mf" | "stl-per-color") y
// pide dónde guardarlo con el diálogo nativo. Devuelve la ruta (vacía si el
// usuario cancela), las partes y los avisos. «stl-per-color» escribe un ZIP con
// un STL por color. Un OpenSCAD que falla vuelve como error con la línea.
func (a *App) ExportCheckpoint(id int64, fileName, format string) (exporter.Result, error) {
	return a.exportCheckpoint(id, fileName, format, a.pickExportPath)
}

// pickFunc pide la ruta de destino; "" = el usuario canceló.
type pickFunc func(defaultDir, suggested, format string) (string, error)

func (a *App) exportCheckpoint(id int64, fileName, format string, pick pickFunc) (exporter.Result, error) {
	switch format {
	case exporter.FormatSTL, exporter.Format3MF, exporter.FormatSTLPerColor:
	default:
		return exporter.Result{}, i18n.Errorf(i18n.Msg{ES: "formato de exportación desconocido: %q", EN: "unknown export format: %q"}, format)
	}
	job, wsPath, slug, err := a.exportJob(id, fileName)
	if err != nil {
		return exporter.Result{}, err
	}
	// Se prepara (render + reparto) antes del diálogo: si el modelo no
	// renderiza, el usuario lo sabe sin haber elegido una carpeta para nada. Con
	// la vista previa ya hecha esto es un acierto de caché.
	p, err := exporter.Prepare(a.ctx, job, format)
	if err != nil {
		return exporter.Result{}, err
	}
	res := exporter.Result{Format: format, Parts: p.Infos(), Warnings: p.Warnings()}
	dir := filepath.Join(wsPath, "exports")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = ""
	}
	dest, err := pick(dir, exporter.SuggestedName(slug, fileName, format), format)
	if err != nil || dest == "" {
		return exporter.Result{Format: format, Parts: res.Parts, Warnings: res.Warnings}, err
	}
	// En Windows el diálogo no siempre agrega la extensión del filtro.
	if ext := exporter.Ext(format); !strings.EqualFold(filepath.Ext(dest), ext) {
		dest += ext
	}
	if err := p.Write(dest); err != nil {
		return exporter.Result{}, i18n.Errorf(i18n.Msg{ES: "no se pudo guardar la exportación: %w", EN: "could not save the export: %w"}, err)
	}
	res.Path = dest
	return res, nil
}

func (a *App) pickExportPath(defaultDir, suggested, format string) (string, error) {
	filter := wruntime.FileFilter{DisplayName: "STL (*.stl)", Pattern: "*.stl"}
	title := i18n.T(i18n.Msg{ES: "Exportar STL", EN: "Export STL"})
	switch format {
	case exporter.Format3MF:
		filter = wruntime.FileFilter{DisplayName: "3MF (*.3mf)", Pattern: "*.3mf"}
		title = i18n.T(i18n.Msg{ES: "Exportar 3MF para Bambu Studio", EN: "Export 3MF for Bambu Studio"})
	case exporter.FormatSTLPerColor:
		filter = wruntime.FileFilter{DisplayName: "ZIP (*.zip)", Pattern: "*.zip"}
		title = i18n.T(i18n.Msg{ES: "Exportar un STL por color (ZIP)", EN: "Export one STL per color (ZIP)"})
	}
	return wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:            title,
		DefaultDirectory: existingDir(defaultDir),
		DefaultFilename:  suggested,
		Filters:          []wruntime.FileFilter{filter},
	})
}

// --- Abrir en un slicer (S9.B) ---------------------------------------------
//
// «Abrir en Bambu Studio» abre la **aplicación** con el archivo, desacoplada y
// solo por un clic del usuario. Nunca se invoca la CLI de Bambu (--slice,
// --export-3mf…): se cae y le abre al usuario el diálogo de «se cerró
// inesperadamente» (regla 9 de .claude/rules/technical.md).

// slicerBambuStudio es el id del único slicer soportado hoy.
const slicerBambuStudio = "bambu-studio"

// Slicer es una aplicación de laminado detectada (o no) en esta máquina.
type Slicer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Path  string `json:"path"` // vacía si no se encontró
	Found bool   `json:"found"`
}

// launchApp arranca un proceso sin esperarlo ni usar shell. Es una variable de
// paquete para que los tests la reemplacen por un registrador: ninguna prueba
// abre una aplicación gráfica del usuario.
var launchApp = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	// Se recoge el proceso hijo para que no quede zombi; no se espera en línea.
	go func() { _ = cmd.Wait() }()
	return nil
}

// bambuStudioCandidates son las rutas de instalación habituales por sistema.
func bambuStudioCandidates(goos, home, programFiles string) []string {
	switch goos {
	case "darwin":
		out := []string{"/Applications/BambuStudio.app"}
		if home != "" {
			out = append(out, filepath.Join(home, "Applications", "BambuStudio.app"))
		}
		return out
	case "windows":
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		return []string{filepath.Join(programFiles, "Bambu Studio", "bambu-studio.exe")}
	}
	return nil
}

// appBundlePath lleva una ruta dentro de un .app (p. ej. el ejecutable de
// Contents/MacOS) al propio .app, que es lo que `open -a` espera.
func appBundlePath(p string) string {
	if i := strings.Index(strings.ToLower(p), ".app"+string(filepath.Separator)); i >= 0 {
		return p[:i+len(".app")]
	}
	return p
}

// detectBambuStudio devuelve la ruta de Bambu Studio: la configurada si existe
// y, si no, la primera de las rutas habituales que exista. "" si no hay.
func detectBambuStudio(goos, configured string, candidates []string) string {
	if configured = strings.TrimSpace(configured); configured != "" {
		if goos == "darwin" {
			configured = appBundlePath(configured)
		}
		if _, err := os.Stat(configured); err == nil {
			return configured
		}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

// DetectSlicers lista los slicers conocidos y si están instalados. La ruta de
// Configuración → Impresión (bambuStudioPath) gana sobre la autodetección.
func (a *App) DetectSlicers() []Slicer {
	configured := ""
	if a.store != nil {
		if s, err := a.store.GetSettings(); err == nil {
			configured = s.BambuStudioPath
		}
	}
	home, _ := os.UserHomeDir()
	path := detectBambuStudio(runtime.GOOS, configured, bambuStudioCandidates(runtime.GOOS, home, os.Getenv("ProgramFiles")))
	return []Slicer{{ID: slicerBambuStudio, Name: "Bambu Studio", Path: path, Found: path != ""}}
}

// slicerLaunch arma el comando que abre `file` con la app del slicer, según el
// sistema. Sin shell y sin flags de la CLI del slicer.
func slicerLaunch(goos, appPath, file string) (string, []string, error) {
	switch goos {
	case "darwin":
		return "open", []string{"-a", appPath, file}, nil
	case "windows":
		return appPath, []string{file}, nil
	}
	return "", nil, i18n.New(i18n.Msg{ES: "abrir en un slicer solo está disponible en macOS y Windows", EN: "opening in a slicer is only available on macOS and Windows"})
}

// OpenInSlicer abre un .3mf o .stl con la aplicación del slicer, desacoplada
// (no espera a que termine). Valida que el archivo exista y la extensión.
func (a *App) OpenInSlicer(slicerID, filePath string) error {
	if slicerID != slicerBambuStudio {
		return i18n.Errorf(i18n.Msg{ES: "slicer desconocido: %q", EN: "unknown slicer: %q"}, slicerID)
	}
	if ext := strings.ToLower(filepath.Ext(filePath)); ext != ".3mf" && ext != ".stl" {
		return i18n.New(i18n.Msg{ES: "solo se pueden abrir archivos .3mf o .stl en el slicer", EN: "only .3mf or .stl files can be opened in the slicer"})
	}
	// Una ruta absoluta nunca empieza con «-»: no se puede colar como opción.
	if !filepath.IsAbs(filePath) {
		return i18n.New(i18n.Msg{ES: "la ruta del archivo tiene que ser absoluta", EN: "the file path must be absolute"})
	}
	if st, err := os.Stat(filePath); err != nil || st.IsDir() {
		return i18n.Errorf(i18n.Msg{ES: "no se encontró el archivo %s", EN: "file %s was not found"}, filePath)
	}
	var found *Slicer
	for _, s := range a.DetectSlicers() {
		if s.ID == slicerID && s.Found {
			found = &s
		}
	}
	if found == nil {
		return i18n.New(i18n.Msg{ES: "Bambu Studio no está instalado; indica su ruta en Configuración → Impresión", EN: "Bambu Studio is not installed; set its path in Settings → Printing"})
	}
	name, args, err := slicerLaunch(runtime.GOOS, found.Path, filePath)
	if err != nil {
		return err
	}
	if err := launchApp(name, args...); err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no se pudo abrir Bambu Studio: %v", EN: "could not open Bambu Studio: %v"}, err)
	}
	return nil
}

// revealLaunch arma el comando que muestra `path` seleccionado en el Finder o
// el Explorador.
func revealLaunch(goos, path string) (string, []string) {
	switch goos {
	case "darwin":
		return "open", []string{"-R", path}
	case "windows":
		return "explorer.exe", []string{"/select," + path}
	}
	return "xdg-open", []string{filepath.Dir(path)}
}

// RevealPath muestra un archivo en el Finder / Explorador (lo deja
// seleccionado). La ruta tiene que existir.
func (a *App) RevealPath(path string) error {
	if !filepath.IsAbs(path) {
		return i18n.New(i18n.Msg{ES: "la ruta tiene que ser absoluta", EN: "the path must be absolute"})
	}
	if _, err := os.Stat(path); err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no se encontró %s", EN: "%s was not found"}, path)
	}
	name, args := revealLaunch(runtime.GOOS, path)
	return launchApp(name, args...)
}
