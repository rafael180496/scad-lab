package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/osopen"
	"scadlab/backend/store"
	"scadlab/backend/watch"
	"scadlab/backend/workspace"
)

// WorkspaceView es un workspace abierto con su índice de checkpoints.
type WorkspaceView struct {
	Workspace   store.Workspace    `json:"workspace"`
	Checkpoints []store.Checkpoint `json:"checkpoints"`
	Missing     bool               `json:"missing"` // la carpeta ya no existe
	// ActiveParent es el checkpoint desde el que se crea el próximo (S3):
	// <ws>/.scadlab/active-parent. "" = no hay uno explícito y rige la rama del
	// más nuevo.
	ActiveParent string `json:"activeParent"`
}

// ListWorkspaces devuelve los workspaces registrados.
func (a *App) ListWorkspaces() ([]store.Workspace, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListWorkspaces()
}

// CreateWorkspace crea el workspace en la carpeta predeterminada. Es el alias de
// CreateWorkspaceIn con la ubicación vacía.
func (a *App) CreateWorkspace(name, printerID string) (store.Workspace, error) {
	return a.CreateWorkspaceIn(name, printerID, "")
}

// workspaceParent resuelve la carpeta padre pedida: vacía = la predeterminada de
// Configuración (o ~/Documents/ScadLab).
func (a *App) workspaceParent(parentDir string) string {
	if p := strings.TrimSpace(parentDir); p != "" {
		return p
	}
	return a.EffectiveWorkspacesRoot()
}

// PreviewWorkspacePath devuelve la ruta exacta que crearía CreateWorkspaceIn:
// <padre>/<slug del nombre>, con el mismo workspace.Slug. "" si el nombre no da
// ningún carácter válido o si el padre no es una ruta absoluta. Es liviano (sin
// tocar el disco ni la base más que para leer la carpeta predeterminada): el
// diálogo «Nuevo workspace» lo llama al escribir.
func (a *App) PreviewWorkspacePath(name, parentDir string) string {
	return workspace.TargetDir(a.workspaceParent(parentDir), name)
}

// CreateWorkspaceIn crea la carpeta del workspace dentro de parentDir (vacío = la
// predeterminada), escribe las reglas e instala las skills. parentDir debe ser
// absoluta y se crea si no existe; si el destino <padre>/<slug> ya existe y no
// está vacío devuelve un error que nombra la carpeta, sin tocar nada.
func (a *App) CreateWorkspaceIn(name, printerID, parentDir string) (store.Workspace, error) {
	if err := a.ready(); err != nil {
		return store.Workspace{}, err
	}
	name = strings.TrimSpace(name)
	dir, err := workspace.Create(a.workspaceParent(parentDir), name, printerID)
	if err != nil {
		return store.Workspace{}, err
	}
	w := store.Workspace{Name: name, Path: dir, PrinterID: printerID}
	if w.ID, err = a.store.InsertWorkspace(w); err != nil {
		return store.Workspace{}, err
	}
	a.syncWorkspaceMCPQuiet(w)
	if err := a.syncSkills(dir); err != nil {
		return w, i18n.Errorf(i18n.Msg{ES: "workspace creado, pero falló la instalación de skills: %v", EN: "workspace created, but installing skills failed: %v"}, err)
	}
	return a.store.GetWorkspace(w.ID)
}

// ImportWorkspace registra una carpeta existente (p. ej. un proyecto que el
// usuario ya tenía). Escribe las reglas solo si faltan.
func (a *App) ImportWorkspace(dir, printerID string) (store.Workspace, error) {
	if err := a.ready(); err != nil {
		return store.Workspace{}, err
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return store.Workspace{}, i18n.New(i18n.Msg{ES: "la carpeta no existe", EN: "the folder does not exist"})
	}
	name := filepath.Base(dir)
	// Las reglas van en su bloque marcado: un CLAUDE.md propio de la carpeta se
	// conserva debajo (ver workspace.mergeRules).
	if err := workspace.WriteRules(dir, name, printerID); err != nil {
		return store.Workspace{}, err
	}
	w := store.Workspace{Name: name, Path: dir, PrinterID: printerID}
	if w.ID, err = a.store.InsertWorkspace(w); err != nil {
		return store.Workspace{}, err
	}
	_ = a.syncSkills(dir)
	a.syncWorkspaceMCPQuiet(w)
	return a.store.GetWorkspace(w.ID)
}

// UpdateWorkspace cambia nombre, impresora o descripción. Si cambió la
// impresora reescribe CLAUDE.md/AGENTS.md/printer.json: el volumen de
// impresión que lee la IA tiene que ser el nuevo.
func (a *App) UpdateWorkspace(w store.Workspace) error {
	if err := a.ready(); err != nil {
		return err
	}
	prev, err := a.store.GetWorkspace(w.ID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(w.Name) == "" {
		return i18n.New(i18n.Msg{ES: "el nombre del workspace es obligatorio", EN: "the workspace name is required"})
	}
	if err := a.store.UpdateWorkspace(w); err != nil {
		return err
	}
	if prev.PrinterID != w.PrinterID || prev.Name != w.Name {
		return workspace.WriteRules(prev.Path, w.Name, w.PrinterID)
	}
	return nil
}

// RemoveWorkspace lo quita de la lista. La carpeta y sus .scad quedan en disco.
// Si estaba abierto como proyecto, se cierra antes: su watcher, su render y sus
// terminales de agente no pueden quedar vivos sobre un workspace que ya no está.
func (a *App) RemoveWorkspace(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	a.projects().forget(id)
	return a.store.RemoveWorkspace(id)
}

// OpenWorkspace es el nombre de antes de S8: equivale a OpenProject (lo abre
// como una pestaña más y lo activa). Se conserva por compatibilidad.
func (a *App) OpenWorkspace(id int64) (WorkspaceView, error) { return a.OpenProject(id) }

// openWorkspaceView hace el trabajo de abrir un workspace: lo marca como abierto,
// reescanea sus .scad, empieza a vigilar la carpeta (sin soltar la de los otros
// proyectos abiertos) y devuelve el índice. Lo llama projectService.open, que es
// quien decide si hay lugar.
func (a *App) openWorkspaceView(w store.Workspace) (WorkspaceView, error) {
	_ = a.store.TouchWorkspace(w.ID)
	v, err := a.rescan(w)
	if err != nil {
		return v, err
	}
	if v.Missing {
		a.stopWatcherFor(w.ID)
	} else {
		a.watchWorkspace(w)
		// La ruta del ejecutable puede haber cambiado (app movida o
		// actualizada): se reescribe solo si difiere.
		a.syncWorkspaceMCPQuiet(w)
		// Las reglas de la app pueden haber cambiado desde que se creó el
		// workspace (MCP, ramas, papelera): se actualiza solo su bloque.
		if err := workspace.WriteRules(w.Path, w.Name, w.PrinterID); err != nil {
			log.Printf("scadlab: actualizar reglas de %s: %v", w.Path, err)
		}
	}
	return v, nil
}

// watchWorkspace empieza a vigilar w si todavía no se hace. Hay un watcher por
// proyecto abierto: los de fondo siguen avisando (checkpoints:changed) y
// renderizando solos. Un fallo no impide abrir el workspace: queda el botón de
// volver a leer.
func (a *App) watchWorkspace(w store.Workspace) {
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	if a.watchers[w.ID] != nil {
		return
	}
	id := w.ID
	wt, err := watch.Start(w.Path, func(added []string) { a.onWorkspaceChanged(id, added) })
	if err != nil {
		log.Printf("scadlab: vigilar %s: %v", w.Path, err)
		return
	}
	if a.watchers == nil {
		a.watchers = map[int64]*watch.Watcher{}
	}
	a.watchers[id] = wt
}

// isWatched dice si el workspace tiene un watcher vivo.
func (a *App) isWatched(id int64) bool {
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	return a.watchers[id] != nil
}

// stopWatcherFor deja de vigilar un workspace (al cerrar su pestaña, al quitarlo
// o al abrir uno cuya carpeta ya no existe). Close espera a la goroutine del
// watcher: nada de esto corre con otros locks de la App tomados.
func (a *App) stopWatcherFor(id int64) {
	a.watchMu.Lock()
	wt := a.watchers[id]
	delete(a.watchers, id)
	a.watchMu.Unlock()
	if wt != nil {
		_ = wt.Close()
	}
}

// stopWatcher deja de vigilar TODOS los workspaces (al cerrar la app).
func (a *App) stopWatcher() {
	a.watchMu.Lock()
	all := a.watchers
	a.watchers = nil
	a.watchMu.Unlock()
	for _, wt := range all {
		_ = wt.Close()
	}
}

// onWorkspaceChanged corre (ya con debounce) cuando cambió algún .scad del
// workspace vigilado: reescanea, avanza el padre activo, avisa a la UI y, si
// apareció un checkpoint nuevo, lo vincula con el agente abierto y —con el
// auto-render activo— lo renderiza. Así el agente que creó el archivo desde la
// terminal recibe RENDER_ERRORS.md sin que nadie toque nada.
func (a *App) onWorkspaceChanged(id int64, added []string) {
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return
	}
	v, err := a.rescan(w)
	if err != nil {
		log.Printf("scadlab: rescan de %s: %v", w.Path, err)
		return
	}
	// Un checkpoint que acaba de volver de la papelera no es trabajo nuevo.
	svc := a.checkpoints()
	added = svc.dropRestored(id, added)
	svc.advance(w, v.Checkpoints, sortedCopy(added))
	v.ActiveParent = checkpoint.ActiveParent(w.Path)
	a.emit(evCheckpointsChanged, v)
	if len(added) == 0 {
		return
	}
	svc.linkSession(w, added)
	if s, err := a.store.GetSettings(); err != nil || !s.AutoRender {
		return
	}
	// Si llegaron varios de golpe solo vale el más nuevo: los anteriores ya
	// quedaron superados. En otra goroutine, porque un render puede tardar
	// minutos y el watcher (y su Close) no deben esperarlo.
	go func() {
		if _, err := a.RenderCheckpoint(id, watch.Newest(added)); err != nil {
			log.Printf("scadlab: auto-render: %v", err)
		}
	}()
}

// RescanWorkspace relee los .scad del disco. El watcher ya lo hace solo; esto
// queda para el botón de la UI (p. ej. si la carpeta está en un disco de red
// que no entrega eventos).
func (a *App) RescanWorkspace(id int64) (WorkspaceView, error) {
	if err := a.ready(); err != nil {
		return WorkspaceView{}, err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return WorkspaceView{}, err
	}
	return a.rescan(w)
}

func (a *App) rescan(w store.Workspace) (WorkspaceView, error) {
	v := WorkspaceView{Workspace: w}
	files, err := workspace.Scan(w.Path)
	if err != nil {
		if os.IsNotExist(err) {
			v.Missing = true
			v.Checkpoints = []store.Checkpoint{}
			return v, nil
		}
		return v, err
	}
	found := make([]store.Checkpoint, len(files))
	for i, f := range files {
		found[i] = store.Checkpoint{FileName: f.FileName, ParentFile: f.ParentFile, Description: f.Description, FileMtime: f.Mtime}
	}
	if err := a.store.SyncCheckpoints(w.ID, found); err != nil {
		return v, err
	}
	v.Checkpoints, err = a.store.ListCheckpoints(w.ID)
	v.ActiveParent = checkpoint.ActiveParent(w.Path)
	return v, err
}

// ReadCheckpoint devuelve el código de un checkpoint. El nombre se valida
// contra el workspace para que la UI no pueda leer fuera de él.
func (a *App) ReadCheckpoint(id int64, fileName string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return "", err
	}
	if err := validCheckpointName(fileName); err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(w.Path, fileName))
	return string(raw), err
}

// SetCheckpointUI guarda rótulo y plegado de un nodo del árbol.
func (a *App) SetCheckpointUI(id int64, fileName, label string, collapsed bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetCheckpointUI(id, fileName, label, collapsed)
}

// RevealWorkspace abre la carpeta en el Finder / Explorador.
func (a *App) RevealWorkspace(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return err
	}
	return osopen.Open(w.Path)
}
