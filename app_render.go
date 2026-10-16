package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"scadlab/backend/checkpoint"
	"scadlab/backend/detect"
	"scadlab/backend/i18n"
	"scadlab/backend/render"
	"scadlab/backend/scadwasm"
	"scadlab/backend/store"
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
	opts := a.renderOptions(s)
	req := render.Request{WorkspaceKey: renderKey(id), WorkspaceDir: w.Path, FileName: fileName}

	// El render en curso lo ven los indicadores de las pestañas de proyecto
	// (ProjectActivity); se da por terminado ANTES de emitir render:finished para
	// que quien reconsulta al recibirlo ya lo vea apagado.
	endRender := a.projects().trackRender(id)
	defer endRender()
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
		endRender()
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
	endRender()
	a.emit(evRenderFinished, RenderFinishedEvent{WorkspaceID: id, Result: res})
	a.notifyRenderFinished(id, res, isLatest)
	return res, nil
}

// renderListener lo registran otros servicios de la app que reaccionan a un
// render (la auto-iteración de la terminal, S5). latest dice si es el
// checkpoint más nuevo del workspace: el mismo criterio que RENDER_ERRORS.md.
type renderListener func(workspaceID int64, res render.Result, latest bool)

// onRenderFinished agrega un listener. Es un hook interno y no el evento de
// Wails a propósito: no depende de que el runtime reparta los eventos de Go a
// Go, y se prueba con go test.
func (a *App) onRenderFinished(l renderListener) {
	a.renderMu.Lock()
	a.renderListeners = append(a.renderListeners, l)
	a.renderMu.Unlock()
}

func (a *App) notifyRenderFinished(id int64, res render.Result, latest bool) {
	a.renderMu.Lock()
	ls := append([]renderListener(nil), a.renderListeners...)
	a.renderMu.Unlock()
	for _, l := range ls {
		l(id, res, latest)
	}
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
	// Es lo primero que pide la interfaz al abrir un proyecto, antes del
	// primer render: buen momento para precompilar el motor integrado.
	a.warmWASM()
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
// carpetas, terminado en .scad (ver checkpoint.ValidName).
func validCheckpointName(fileName string) error { return checkpoint.ValidName(fileName) }

// latestCheckpoint es el .scad de número más alto del workspace ("" si no hay).
func latestCheckpoint(dir string) string { return checkpoint.Latest(dir) }

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

// wasmRunners guarda un scadwasm.Runner por carpeta de datos. Vive a nivel de
// paquete y no en App (app.go no es de este bloque) y por la misma razón que
// partsStores: la caché de compilación es por contenido, compartirla entre
// instancias de App es correcto, y así hay un solo Runner —un solo módulo
// compilado de 10 MB— por proceso.
var wasmRunners = struct {
	sync.Mutex
	m map[string]*scadwasm.Runner
}{m: map[string]*scadwasm.Runner{}}

// wasmRunnerFor devuelve el motor integrado de una carpeta de datos, con la
// caché de compilación de wazero en <datos>/cache/wasm (sin carpeta de datos,
// solo en memoria).
func wasmRunnerFor(dataDir string) *scadwasm.Runner {
	wasmRunners.Lock()
	defer wasmRunners.Unlock()
	r := wasmRunners.m[dataDir]
	if r == nil {
		cache := ""
		if dataDir != "" {
			cache = filepath.Join(dataDir, "cache", "wasm")
		}
		r = scadwasm.New(cache)
		wasmRunners.m[dataDir] = r
	}
	return r
}

// wasm es el motor integrado de esta App.
func (a *App) wasm() *scadwasm.Runner { return wasmRunnerFor(a.dataDir) }

// renderOptions arma las opciones del motor de render con la configuración
// vigente: el OpenSCAD nativo si se encuentra y, si no, el motor integrado.
// Es el único lugar que las arma, así render, exportación, partes y estimación
// eligen siempre el mismo motor.
func (a *App) renderOptions(s store.Settings) render.Options {
	return render.Options{
		OpenSCADPath:   a.openscadPath(s.OpenSCADPath),
		WASM:           a.wasm(),
		Backend:        s.RenderBackend,
		TimeoutSeconds: s.RenderTimeoutSec,
	}
}

var warmOnce sync.Once

// warmWASM compila el motor integrado en segundo plano para que el primer
// render no pague los 2-3 s de compilación. Solo si no hay un OpenSCAD
// instalado (con uno nativo el integrado no se usa) y una sola vez por proceso.
func (a *App) warmWASM() {
	if a.dataDir == "" || a.store == nil {
		return
	}
	warmOnce.Do(func() {
		go func() {
			s, err := a.store.GetSettings()
			if err != nil || a.openscadPath(s.OpenSCADPath) != "" {
				return
			}
			a.wasm().Warmup()
		}()
	})
}

// resetOpenSCADCache olvida la ruta cacheada de OpenSCAD: tras descargar o
// quitar el OpenSCAD gestionado (<datos>/tools/openscad) el próximo render
// vuelve a detectar, y el descargado gana al del PATH.
func (a *App) resetOpenSCADCache() {
	a.renderMu.Lock()
	a.scad = openscadCache{}
	a.renderMu.Unlock()
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
