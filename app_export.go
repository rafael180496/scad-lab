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
	"scadlab/backend/render"
	"scadlab/backend/slicers"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
	"scadlab/backend/wsconfig"
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
	// La impresora efectiva: los avisos de colores de más se miden contra los
	// colores que el workspace eligió, no contra el máximo de la máquina.
	pr := wsconfig.EffectivePrinter(w)
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

// ExportCheckpoint exporta un checkpoint ("stl" | "3mf" | "3mf-prusa" | "stl-per-color") y
// pide dónde guardarlo con el diálogo nativo. Devuelve la ruta (vacía si el
// usuario cancela), las partes y los avisos. «stl-per-color» escribe un ZIP con
// un STL por color. Un OpenSCAD que falla vuelve como error con la línea.
func (a *App) ExportCheckpoint(id int64, fileName, format string) (exporter.Result, error) {
	return a.exportCheckpoint(id, fileName, format, a.pickExportPath)
}

// pickFunc pide la ruta de destino; "" = el usuario canceló.
type pickFunc func(defaultDir, suggested, format string) (string, error)

func (a *App) exportCheckpoint(id int64, fileName, format string, pick pickFunc) (exporter.Result, error) {
	if !exporter.IsFileFormat(format) {
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
		title = i18n.T(i18n.Msg{ES: "Exportar 3MF multicolor", EN: "Export multicolor 3MF"})
	case exporter.Format3MFPrusa:
		filter = wruntime.FileFilter{DisplayName: "3MF (*.3mf)", Pattern: "*.3mf"}
		title = i18n.T(i18n.Msg{ES: "Exportar 3MF para PrusaSlicer", EN: "Export 3MF for PrusaSlicer"})
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

// --- Abrir en un slicer (S9.B, S14) ----------------------------------------
//
// «Abrir en <slicer>» abre la **aplicación** con el archivo, desacoplada y solo
// por un clic del usuario. Nunca se invoca la CLI del slicer (--slice,
// --export-3mf…): Bambu Studio se cae con ella y le abre al usuario el diálogo de
// «se cerró inesperadamente» (regla 9 de .claude/rules/technical.md). El registro
// de slicers y dónde se instala cada uno está en backend/slicers.

// Slicer es una aplicación de laminado del registro y si está en esta máquina.
type Slicer struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Path       string `json:"path"` // la que se usaría; vacía si no se encontró
	Found      bool   `json:"found"`
	Configured string `json:"configured"` // la ruta que el usuario fijó en Configuración ("" = autodetectar)
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

// slicerEnv es el entorno en que se buscan los slicers (carpetas de programas,
// carpeta personal). Variable de paquete para que los tests apunten a carpetas
// temporales: la máquina de quien corre las pruebas puede tener slicers de verdad.
var slicerEnv = slicers.CurrentEnv

// DetectSlicers lista TODOS los slicers del registro y si están instalados. La
// ruta de Configuración → Impresión (settings.slicerPaths, y bambuStudioPath para
// Bambu Studio) gana sobre la autodetección.
func (a *App) DetectSlicers() []Slicer {
	var s store.Settings
	if a.store != nil {
		if got, err := a.store.GetSettings(); err == nil {
			s = got
		}
	}
	env := slicerEnv()
	out := make([]Slicer, 0, len(slicers.Registry))
	for _, d := range slicers.Registry {
		path := slicers.Find(d, env, s.SlicerPath(d.ID))
		out = append(out, Slicer{ID: d.ID, Name: d.Name, Path: path, Found: path != "", Configured: s.SlicerPath(d.ID)})
	}
	return out
}

// OpenInSlicer abre un .3mf o .stl con la aplicación del slicer (cualquiera del
// registro), desacoplada (no espera a que termine). Valida que el archivo exista
// y la extensión, y dice el nombre del slicer en cada mensaje.
func (a *App) OpenInSlicer(slicerID, filePath string) error {
	def, ok := slicers.ByID(slicerID)
	if !ok {
		return i18n.Errorf(i18n.Msg{ES: "slicer desconocido: %q", EN: "unknown slicer: %q"}, slicerID)
	}
	if ext := strings.ToLower(filepath.Ext(filePath)); ext != ".3mf" && ext != ".stl" {
		return i18n.Errorf(i18n.Msg{ES: "solo se pueden abrir archivos .3mf o .stl en %s", EN: "only .3mf or .stl files can be opened in %s"}, def.Name)
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
		return i18n.Errorf(i18n.Msg{
			ES: "%s no está instalado; indica su ruta en Configuración → Impresión",
			EN: "%s is not installed; set its path in Settings → Printing",
		}, def.Name)
	}
	name, args, err := slicers.LaunchCommand(runtime.GOOS, found.Path, filePath)
	if err != nil {
		return err
	}
	if err := launchApp(name, args...); err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no se pudo abrir %s: %v", EN: "could not open %s: %v"}, def.Name, err)
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
