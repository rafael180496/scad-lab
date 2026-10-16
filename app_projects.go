package main

import (
	"errors"
	"os"
	"slices"
	"sync"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/render"
	"scadlab/backend/store"
)

// Varios proyectos abiertos a la vez (S8, bloque 8.A): hasta store.MaxOpenProjects
// workspaces como pestañas de una sola ventana. Los de fondo siguen vivos: su
// watcher (app_workspaces.go), el auto-render, sus terminales de agente y la
// auto-iteración no dependen de cuál está al frente. Qué está abierto, en qué
// orden y cuál es el activo se guarda en settings (openProjects, activeProject)
// y al arrancar NO se abre nada solo: el frontend llama ListOpenProjects y
// reabre lo que le toque con OpenProject.

// evProjectsChanged avisa a la interfaz que cambió la lista de proyectos
// abiertos, su orden o el activo. Payload: OpenProjectsView.
const evProjectsChanged = "projects:changed"

// OpenProjectEntry es una pestaña: el workspace y si su carpeta ya no existe.
type OpenProjectEntry struct {
	Workspace store.Workspace `json:"workspace"`
	Missing   bool            `json:"missing"` // la carpeta ya no existe
}

// OpenProjectsView es la barra de pestañas: en el orden en que se muestran, y el
// id del proyecto al frente (0 = ninguno).
type OpenProjectsView struct {
	Projects []OpenProjectEntry `json:"projects"`
	ActiveID int64              `json:"activeId"`
}

// ProjectActivityEntry es lo que muestran los indicadores de una pestaña.
type ProjectActivityEntry struct {
	ID int64 `json:"id"` // id del workspace
	// Rendering: hay un render en curso (de cualquier checkpoint del proyecto).
	Rendering bool `json:"rendering"`
	// LatestFile es el checkpoint más nuevo del proyecto ("" si no hay ninguno).
	LatestFile string `json:"latestFile"`
	// LastRenderStatus es el estado del último render de LatestFile en esta
	// sesión: "ok" | "error" | "missing" ("cancelled" no se guarda), o "" si
	// todavía no se renderizó.
	LastRenderStatus string `json:"lastRenderStatus"`
	// RenderFailed: el último render del checkpoint más nuevo falló
	// (LastRenderStatus == "error"). Es lo que pinta la pestaña en rojo.
	RenderFailed bool `json:"renderFailed"`
	// AgentsAlive son las terminales de agente vivas del proyecto (0–2).
	AgentsAlive int `json:"agentsAlive"`
}

// projectService guarda lo que no está en la base: el exclusión mutua de abrir,
// cerrar y reordenar, y qué renders hay en curso. Como las terminales y los
// checkpoints, vive en un registro por App (ver projects()) para no ensanchar
// app.go.
type projectService struct {
	app *App
	mu  sync.Mutex // serializa abrir, cerrar, reordenar y activar

	rmu       sync.Mutex
	rendering map[int64]int // renders en curso por workspace
}

var (
	projSvcMu sync.Mutex
	projSvcs  = map[*App]*projectService{}
)

func (a *App) projects() *projectService {
	projSvcMu.Lock()
	defer projSvcMu.Unlock()
	if s := projSvcs[a]; s != nil {
		return s
	}
	s := &projectService{app: a, rendering: map[int64]int{}}
	projSvcs[a] = s
	return s
}

// trackRender anota un render en curso del workspace y devuelve la función que
// lo da por terminado (idempotente). Dos renders del mismo workspace a la vez
// son posibles —el motor cancela el anterior al llegar el nuevo—, por eso es un
// contador y no un bool.
func (p *projectService) trackRender(ws int64) func() {
	p.rmu.Lock()
	p.rendering[ws]++
	p.rmu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			p.rmu.Lock()
			if p.rendering[ws]--; p.rendering[ws] <= 0 {
				delete(p.rendering, ws)
			}
			p.rmu.Unlock()
		})
	}
}

func (p *projectService) isRendering(ws int64) bool {
	p.rmu.Lock()
	defer p.rmu.Unlock()
	return p.rendering[ws] > 0
}

// load lee de settings la lista de proyectos abiertos y el activo, sin los
// workspaces que ya no están registrados (borrados por otro camino). El activo
// que no está en la lista se descarta. No escribe nada: la limpieza se persiste
// en la próxima escritura.
func (p *projectService) load() ([]int64, int64, error) {
	s, err := p.app.store.GetSettings()
	if err != nil {
		return nil, 0, err
	}
	ids := make([]int64, 0, len(s.OpenProjects))
	for _, id := range s.OpenProjects {
		if _, err := p.app.store.GetWorkspace(id); err == nil {
			ids = append(ids, id)
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, 0, err
		}
	}
	active := s.ActiveProject
	if !slices.Contains(ids, active) {
		active = 0
		if len(ids) > 0 {
			active = ids[0]
		}
	}
	return ids, active, nil
}

// view arma lo que ve la interfaz de una lista de ids.
func (p *projectService) view(ids []int64, active int64) OpenProjectsView {
	v := OpenProjectsView{Projects: make([]OpenProjectEntry, 0, len(ids)), ActiveID: active}
	for _, id := range ids {
		w, err := p.app.store.GetWorkspace(id)
		if err != nil {
			continue
		}
		st, err := os.Stat(w.Path)
		v.Projects = append(v.Projects, OpenProjectEntry{Workspace: w, Missing: err != nil || !st.IsDir()})
	}
	return v
}

// commit persiste la lista y el activo y avisa a la interfaz. Con p.mu tomado.
func (p *projectService) commit(ids []int64, active int64) error {
	if err := p.app.store.SetOpenProjects(ids, active); err != nil {
		return err
	}
	p.app.emit(evProjectsChanged, p.view(ids, active))
	return nil
}

// ListOpenProjects devuelve los proyectos abiertos en el orden de las pestañas y
// el activo. No abre ni vigila nada: el frontend lo llama al arrancar y reabre
// cada uno con OpenProject. Un proyecto cuya carpeta ya no existe viene con
// missing=true; uno que ya no está registrado se omite.
func (a *App) ListOpenProjects() (OpenProjectsView, error) {
	if err := a.ready(); err != nil {
		return OpenProjectsView{}, err
	}
	ids, active, err := a.projects().load()
	if err != nil {
		return OpenProjectsView{}, err
	}
	return a.projects().view(ids, active), nil
}

// OpenProject agrega el workspace a las pestañas (si no estaba), lo activa y
// devuelve su índice de checkpoints. Empieza a vigilar su carpeta sin soltar la
// de los demás. Con store.MaxOpenProjects ya abiertos devuelve un error claro.
// Abrir uno que ya está abierto no lo mueve: solo lo activa y lo vuelve a leer.
func (a *App) OpenProject(id int64) (WorkspaceView, error) {
	if err := a.ready(); err != nil {
		return WorkspaceView{}, err
	}
	return a.projects().open(id)
}

func (p *projectService) open(id int64) (WorkspaceView, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	w, err := p.app.store.GetWorkspace(id)
	if err != nil {
		return WorkspaceView{}, err
	}
	ids, _, err := p.load()
	if err != nil {
		return WorkspaceView{}, err
	}
	isOpen := slices.Contains(ids, id)
	if !isOpen && len(ids) >= store.MaxOpenProjects {
		// Antes de tocar nada: un rechazo no deja watchers ni archivos escritos.
		return WorkspaceView{}, i18n.Errorf(i18n.Msg{
			ES: "ya hay %d proyectos abiertos; cierra uno antes de abrir otro",
			EN: "there are already %d open projects; close one before opening another",
		}, store.MaxOpenProjects)
	}
	v, err := p.app.openWorkspaceView(w)
	if err != nil {
		return v, err
	}
	if !isOpen {
		ids = append(ids, id)
	}
	if err := p.commit(ids, id); err != nil {
		return v, err
	}
	return v, nil
}

// CloseProject quita el proyecto de las pestañas: detiene su watcher, cancela su
// render en curso y cierra sus terminales de agente (los CLIs mueren). Si era el
// activo, pasa al que ocupaba su lugar (o al anterior si era el último). Cerrar
// uno que no está abierto no es un error. Los .scad y el workspace quedan.
func (a *App) CloseProject(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.projects().close(id)
}

func (p *projectService) close(id int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids, active, err := p.load()
	if err != nil {
		return err
	}
	p.release(id)
	i := slices.Index(ids, id)
	if i < 0 {
		return nil
	}
	ids = slices.Delete(ids, i, i+1)
	if active == id {
		active = 0
		if len(ids) > 0 {
			active = ids[min(i, len(ids)-1)]
		}
	}
	return p.commit(ids, active)
}

// release suelta lo que un proyecto tiene vivo: watcher, render y agentes.
func (p *projectService) release(id int64) {
	p.app.stopWatcherFor(id)
	p.app.CancelRender(id)
	p.app.terminals().closeWorkspace(id)
}

// forget cierra un proyecto que se va a quitar del registro (RemoveWorkspace).
// No devuelve error: quitar el workspace no puede quedar a medias por no poder
// guardar la lista de pestañas, que se limpia sola en load (un id sin
// workspace se descarta).
func (p *projectService) forget(id int64) {
	_ = p.close(id)
}

// SetActiveProject marca cuál proyecto abierto está al frente. Solo persiste y
// avisa (projects:changed): no relee nada. 0 = ninguno.
func (a *App) SetActiveProject(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	p := a.projects()
	p.mu.Lock()
	defer p.mu.Unlock()
	ids, _, err := p.load()
	if err != nil {
		return err
	}
	if id != 0 && !slices.Contains(ids, id) {
		return i18n.New(i18n.Msg{ES: "ese proyecto no está abierto", EN: "that project is not open"})
	}
	return p.commit(ids, id)
}

// ReorderProjects fija el orden de las pestañas. Es tolerante: ignora ids que no
// están abiertos y repetidos, y deja al final, en su orden, los abiertos que la
// lista no nombra (la interfaz puede estar un paso atrás de un cierre o una
// apertura). El activo no cambia.
func (a *App) ReorderProjects(ids []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	p := a.projects()
	p.mu.Lock()
	defer p.mu.Unlock()
	cur, active, err := p.load()
	if err != nil {
		return err
	}
	out := make([]int64, 0, len(cur))
	for _, id := range ids {
		if slices.Contains(cur, id) && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for _, id := range cur {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return p.commit(out, active)
}

// ProjectActivity devuelve, por cada proyecto abierto y en el orden de las
// pestañas, lo que muestran sus indicadores: render en curso, estado del último
// render del checkpoint más nuevo y agentes vivos. Es barato (sin OpenSCAD ni
// red): la interfaz lo vuelve a pedir al llegar render:*, terminal:* o
// checkpoints:changed.
func (a *App) ProjectActivity() ([]ProjectActivityEntry, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	p := a.projects()
	ids, _, err := p.load()
	if err != nil {
		return nil, err
	}
	out := make([]ProjectActivityEntry, 0, len(ids))
	for _, id := range ids {
		e := ProjectActivityEntry{ID: id, Rendering: p.isRendering(id), AgentsAlive: a.terminals().aliveFor(id)}
		if w, err := a.store.GetWorkspace(id); err == nil {
			e.LatestFile = checkpoint.Latest(w.Path)
		}
		if e.LatestFile != "" {
			a.renderMu.Lock()
			res, ok := a.results[id][e.LatestFile]
			a.renderMu.Unlock()
			if ok {
				e.LastRenderStatus = res.Status
				e.RenderFailed = res.Status == render.StatusError
			}
		}
		out = append(out, e)
	}
	return out, nil
}
