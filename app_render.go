package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"scadlab/backend/detect"
	"scadlab/backend/i18n"
	"scadlab/backend/render"
	"scadlab/backend/store"
	"scadlab/backend/watch"
	"scadlab/backend/workspace"
)

// Eventos Wails que emite el render (Go → React). Los nombres son contrato con
// frontend/src/lib/events.ts.
const (
	evRenderStarted      = "render:started"
	evRenderFinished     = "render:finished"
	evCheckpointsChanged = "checkpoints:changed"
)

// RenderStartedEvent es el payload de render:started.
type RenderStartedEvent struct {
	WorkspaceID int64  `json:"workspaceId"`
	FileName    string `json:"fileName"`
}

// RenderFinishedEvent es el payload de render:finished.
type RenderFinishedEvent struct {
	WorkspaceID int64         `json:"workspaceId"`
	Result      render.Result `json:"result"`
}

// openscadCache recuerda dónde está OpenSCAD. detect.OpenSCAD corre
// `openscad --version` (cientos de ms, más en Windows), y con el auto-render
// cada checkpoint nuevo lo pagaría. Se invalida si cambia la ruta configurada
// o si el ejecutable desapareció.
type openscadCache struct {
	override string
	path     string
}

// RenderCheckpoint renderiza un checkpoint con OpenSCAD. Un fallo de OpenSCAD
// no es un error del binding: viaja en Result.Status y Result.Diagnostics. El
// error queda para lo que no es culpa del modelo (base o caché no
// disponibles, nombre inválido).
//
// Con StatusError escribe RENDER_ERRORS.md en el workspace (lo leen los
// agentes para corregir); con StatusOK lo borra. Cancelado o sin OpenSCAD no
// lo toca: no dicen nada sobre si el modelo está bien.
func (a *App) RenderCheckpoint(id int64, fileName string) (render.Result, error) {
	if err := a.ready(); err != nil {
		return render.Result{}, err
	}
	if a.engine == nil {
		return render.Result{}, i18n.Errorf(i18n.Msg{ES: "el motor de render no está disponible: %v", EN: "the render engine is not available: %v"}, a.engineErr)
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return render.Result{}, err
	}
	if err := validCheckpointName(fileName); err != nil {
		return render.Result{}, err
	}
	s, err := a.store.GetSettings()
	if err != nil {
		return render.Result{}, err
	}
	opts := render.Options{
		OpenSCADPath:   a.openscadPath(s.OpenSCADPath),
		Backend:        s.RenderBackend,
		TimeoutSeconds: s.RenderTimeoutSec,
	}
	req := render.Request{WorkspaceKey: renderKey(id), WorkspaceDir: w.Path, FileName: fileName}

	a.emit(evRenderStarted, RenderStartedEvent{WorkspaceID: id, FileName: fileName})
	res, err := a.engine.Render(a.ctx, req, opts)
	if res.FileName == "" {
		res.FileName = fileName
	}
	if err != nil {
		// El frontend tiene un spinner esperando render:finished: se le manda
		// un resultado de error igual, o el checkpoint quedaría «renderizando»
		// para siempre.
		res.Status = render.StatusError
		res.Diagnostics = append(res.Diagnostics, render.Diagnostic{Severity: render.SeverityError, Message: err.Error()})
		a.emit(evRenderFinished, RenderFinishedEvent{WorkspaceID: id, Result: res})
		return res, err
	}

	// RENDER_ERRORS.md describe el estado del checkpoint MÁS NUEVO, que es
	// sobre el que itera el agente. Renderizar uno viejo (el usuario navega
	// el historial) no lo toca: si no, abrir un checkpoint roto ya superado le
	// mandaría al agente errores que ya corrigió, y abrir uno viejo que anda
	// borraría el aviso de que el último está roto.
	isLatest := latestCheckpoint(w.Path) == fileName
	switch {
	case !isLatest:
	case res.Status == render.StatusError:
		if werr := render.WriteRenderErrors(w.Path, res); werr != nil {
			log.Printf("scadlab: escribir RENDER_ERRORS.md: %v", werr)
		}
	case res.Status == render.StatusOK:
		if cerr := render.ClearRenderErrors(w.Path); cerr != nil {
			log.Printf("scadlab: borrar RENDER_ERRORS.md: %v", cerr)
		}
	}
	// Un cancelado no pisa el resultado anterior: lo reemplazó un render más
	// nuevo, y la última malla buena de ese checkpoint sigue siendo válida.
	if res.Status != render.StatusCancelled {
		a.renderMu.Lock()
		if a.results == nil {
			a.results = map[int64]map[string]render.Result{}
		}
		if a.results[id] == nil {
			a.results[id] = map[string]render.Result{}
		}
		a.results[id][fileName] = res
		a.renderMu.Unlock()
	}
	a.emit(evRenderFinished, RenderFinishedEvent{WorkspaceID: id, Result: res})
	return res, nil
}

// CancelRender cancela el render en curso del workspace, si lo hay.
func (a *App) CancelRender(id int64) {
	if a.engine != nil {
		a.engine.Cancel(renderKey(id))
	}
}

// GetRenderState devuelve los últimos resultados en memoria del workspace,
// por nombre de checkpoint: con eso la UI pinta el árbol al reabrirlo sin
// volver a renderizar.
func (a *App) GetRenderState(id int64) (map[string]render.Result, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	a.renderMu.Lock()
	defer a.renderMu.Unlock()
	out := make(map[string]render.Result, len(a.results[id]))
	for k, v := range a.results[id] {
		out[k] = v
	}
	return out, nil
}

// validateRenderSettings la usa SaveSettings: un tope de 0 s haría fallar
// todo render y uno de un día dejaría colgado un OpenSCAD sin forma de verlo.
func validateRenderSettings(s store.Settings) error {
	if s.RenderTimeoutSec < 10 || s.RenderTimeoutSec > 3600 {
		return i18n.New(i18n.Msg{ES: "el tiempo máximo de render tiene que estar entre 10 y 3600 segundos", EN: "the render time limit must be between 10 and 3600 seconds"})
	}
	return nil
}

// validCheckpointName valida igual que ReadCheckpoint: un nombre suelto, sin
// carpetas, terminado en .scad. Así la UI no puede hacer correr OpenSCAD —ni
// escribir RENDER_ERRORS.md— fuera del workspace.
func validCheckpointName(fileName string) error {
	if fileName == "" || fileName != filepath.Base(fileName) || !strings.EqualFold(filepath.Ext(fileName), ".scad") {
		return i18n.New(i18n.Msg{ES: "nombre de checkpoint inválido", EN: "invalid checkpoint name"})
	}
	return nil
}

// latestCheckpoint es el .scad de número más alto del workspace ("" si no hay).
func latestCheckpoint(dir string) string {
	files, err := workspace.Scan(dir)
	if err != nil {
		return ""
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.FileName
	}
	return watch.Newest(names)
}

func renderKey(id int64) string { return "ws:" + strconv.FormatInt(id, 10) }

// openscadPath resuelve el ejecutable de OpenSCAD con caché. "" si no está:
// el motor lo devuelve como StatusMissing y la UI ofrece ir a Configuración.
// Un «no encontrado» no se cachea, así instalarlo con la app abierta funciona
// en el próximo render sin reiniciar.
func (a *App) openscadPath(override string) string {
	a.renderMu.Lock()
	c := a.scad
	a.renderMu.Unlock()
	if c.path != "" && c.override == override {
		if _, err := os.Stat(c.path); err == nil {
			return c.path
		}
	}
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	tool := detect.OpenSCAD(ctx, override)
	if !tool.Found {
		return ""
	}
	a.renderMu.Lock()
	a.scad = openscadCache{override: override, path: tool.Path}
	a.renderMu.Unlock()
	return tool.Path
}

// emit manda un evento al frontend. El emisor lo instala startup; en los
// tests queda nil y emit no hace nada (EventsEmit sin el runtime de Wails
// termina el proceso, no devuelve error).
func (a *App) emit(name string, payload any) {
	if a.emitter != nil {
		a.emitter(name, payload)
	}
}

// wailsEmitter es el emisor real, sobre el contexto de Wails.
func wailsEmitter(ctx context.Context) func(string, any) {
	return func(name string, payload any) { wruntime.EventsEmit(ctx, name, payload) }
}
