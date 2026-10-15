package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"

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
}

// ListWorkspaces devuelve los workspaces registrados.
func (a *App) ListWorkspaces() ([]store.Workspace, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListWorkspaces()
}

// CreateWorkspace crea la carpeta, escribe reglas e instala las skills.
func (a *App) CreateWorkspace(name, printerID string) (store.Workspace, error) {
	if err := a.ready(); err != nil {
		return store.Workspace{}, err
	}
	name = strings.TrimSpace(name)
	root := a.EffectiveWorkspacesRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return store.Workspace{}, err
	}
	dir, err := workspace.Create(root, name, printerID)
	if err != nil {
		return store.Workspace{}, err
	}
	w := store.Workspace{Name: name, Path: dir, PrinterID: printerID}
	if w.ID, err = a.store.InsertWorkspace(w); err != nil {
		return store.Workspace{}, err
	}
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
	if _, err := os.Stat(filepath.Join(dir, "CLAUDE.md")); err != nil {
		if err := workspace.WriteRules(dir, name, printerID); err != nil {
			return store.Workspace{}, err
		}
	}
	w := store.Workspace{Name: name, Path: dir, PrinterID: printerID}
	if w.ID, err = a.store.InsertWorkspace(w); err != nil {
		return store.Workspace{}, err
	}
	_ = a.syncSkills(dir)
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
func (a *App) RemoveWorkspace(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	a.watchMu.Lock()
	if a.watchedID == id {
		a.closeWatcherLocked()
	}
	a.watchMu.Unlock()
	return a.store.RemoveWorkspace(id)
}

// OpenWorkspace marca el workspace como abierto, reescanea sus .scad,
// empieza a vigilar la carpeta (deja de vigilar la anterior) y devuelve el
// índice.
func (a *App) OpenWorkspace(id int64) (WorkspaceView, error) {
	if err := a.ready(); err != nil {
		return WorkspaceView{}, err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return WorkspaceView{}, err
	}
	_ = a.store.TouchWorkspace(id)
	v, err := a.rescan(w)
	if err != nil {
		return v, err
	}
	if v.Missing {
		a.stopWatcher()
	} else {
		a.watchWorkspace(w)
	}
	return v, nil
}

// watchWorkspace reemplaza el watcher vigente por uno sobre w. Un fallo no
// impide abrir el workspace: queda el botón de volver a leer.
func (a *App) watchWorkspace(w store.Workspace) {
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	if a.watcher != nil && a.watchedID == w.ID {
		return
	}
	a.closeWatcherLocked()
	id := w.ID
	wt, err := watch.Start(w.Path, func(added []string) { a.onWorkspaceChanged(id, added) })
	if err != nil {
		log.Printf("scadlab: vigilar %s: %v", w.Path, err)
		return
	}
	a.watcher, a.watchedID = wt, id
}

// stopWatcher deja de vigilar el workspace abierto (al cerrar la app o al
// abrir uno cuya carpeta ya no existe).
func (a *App) stopWatcher() {
	a.watchMu.Lock()
	defer a.watchMu.Unlock()
	a.closeWatcherLocked()
}

func (a *App) closeWatcherLocked() {
	if a.watcher != nil {
		_ = a.watcher.Close()
		a.watcher, a.watchedID = nil, 0
	}
}

// onWorkspaceChanged corre (ya con debounce) cuando cambió algún .scad del
// workspace vigilado: reescanea, avisa a la UI y, si apareció un checkpoint
// nuevo y el auto-render está activo, lo renderiza. Así el agente que creó el
// archivo desde la terminal recibe RENDER_ERRORS.md sin que nadie toque nada.
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
	a.emit(evCheckpointsChanged, v)
	if len(added) == 0 {
		return
	}
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
