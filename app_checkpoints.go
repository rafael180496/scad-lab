package main

import (
	"errors"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"scadlab/backend/agentsessions"
	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// Checkpoints avanzados (S3, bloque 3.B): papelera, padre activo, rótulo,
// comparar con el padre y el vínculo checkpoint ↔ sesión del agente. Las
// operaciones sobre archivos viven en backend/checkpoint (que comparte con el
// servidor MCP); acá se resuelve el workspace, se mantiene el índice de la base
// y se avisa a la interfaz.

// maxLabelRunes es el largo máximo de un rótulo: se muestra en una fila del
// árbol, no es una descripción.
const maxLabelRunes = 80

// Tamaño de arranque de la terminal que abre «Retomar sesión», igual que el de
// useTerminals: el primer fit del frontend lo corrige.
const (
	resumeCols = 110
	resumeRows = 28
)

// maxPendingMarks acota los conjuntos de nombres «propios» y «restaurados»: se
// vacían cuando el watcher los consume, pero con el watcher apagado
// (auto-render de otro workspace, carpeta ausente) no tienen quien los limpie.
const maxPendingMarks = 200

// checkpointService es el estado de los checkpoints avanzados de una App. Como
// el de la terminal, vive en un registro por App y no en el struct (app.go no se
// toca para cada servicio nuevo).
type checkpointService struct {
	app *App

	mu       sync.Mutex
	own      map[string]bool // «ws|archivo» creados por la propia app (editor, chat): no son del agente
	restored map[string]bool // «ws|archivo» que acaba de traer Restore: no son un checkpoint nuevo

	// Inyectables para probar sin un PTY ni los archivos de sesión de un CLI.
	activeAgent   func(ws int64) string
	sessions      func(ws int64, agentID string) ([]agentsessions.Session, error)
	startTerminal func(ws int64, agentID, mode, sessionID string) (string, error)
	runAsync      func(func())
}

var (
	ckptSvcMu sync.Mutex
	ckptSvcs  = map[*App]*checkpointService{}
)

// checkpoints devuelve el servicio de checkpoints de la App, creándolo la
// primera vez.
func (a *App) checkpoints() *checkpointService {
	ckptSvcMu.Lock()
	defer ckptSvcMu.Unlock()
	if s := ckptSvcs[a]; s != nil {
		return s
	}
	s := &checkpointService{
		app:      a,
		own:      map[string]bool{},
		restored: map[string]bool{},
		activeAgent: func(ws int64) string {
			return a.terminals().agentOf(ws)
		},
		sessions: a.ListAgentSessions,
		startTerminal: func(ws int64, agentID, mode, sessionID string) (string, error) {
			return a.StartAgentTerminal(ws, agentID, mode, sessionID, resumeCols, resumeRows)
		},
		runAsync: func(f func()) { go f() },
	}
	ckptSvcs[a] = s
	return s
}

// agentOf devuelve el agente de la terminal viva del workspace que el usuario
// tocó más recientemente ("" si no hay ninguna): el mismo criterio con que la
// auto-iteración elige a quién avisar.
func (s *terminalService) agentOf(workspaceID int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id := s.activeAgent(workspaceID); id != "" {
		if m := s.meta[id]; m != nil {
			return m.agentID
		}
	}
	return ""
}

func markKey(ws int64, file string) string { return strconv.FormatInt(ws, 10) + "|" + file }

// markOwn anota que la propia app creó el archivo (guardar desde el editor o el
// chat). El watcher lo verá aparecer como cualquier otro, pero no es obra del
// agente que haya abierto: sin esta marca se le atribuiría.
func (s *checkpointService) markOwn(ws int64, file string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.own) >= maxPendingMarks {
		s.own = map[string]bool{}
	}
	s.own[markKey(ws, file)] = true
}

func (s *checkpointService) markRestored(ws int64, file string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.restored) >= maxPendingMarks {
		s.restored = map[string]bool{}
	}
	s.restored[markKey(ws, file)] = true
}

// takeMarked quita de files los que están en set y los olvida. Con s.mu tomado
// por quien llama.
func takeMarked(set map[string]bool, ws int64, files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		k := markKey(ws, f)
		if set[k] {
			delete(set, k)
			continue
		}
		out = append(out, f)
	}
	return out
}

// dropRestored descarta de los «nuevos» del watcher los que acaba de traer
// Restore: volver de la papelera no es trabajo nuevo, así que ni avanza el padre
// activo, ni se vincula a la terminal abierta, ni se renderiza solo.
func (s *checkpointService) dropRestored(ws int64, added []string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.restored) == 0 {
		return added
	}
	return takeMarked(s.restored, ws, added)
}

// advance mueve el padre activo siguiendo a los checkpoints nuevos: si el activo
// es el padre de uno de ellos, el nuevo pasa a ser el activo. Create ya lo hace
// cuando el archivo lo crea la app o el servidor MCP; esto cubre al agente que
// escribe el .scad directo. Es idempotente, así que repetirlo no cambia nada.
func (s *checkpointService) advance(w store.Workspace, cps []store.Checkpoint, added []string) {
	parents := make(map[string]string, len(cps))
	for _, c := range cps {
		parents[c.FileName] = c.ParentFile
	}
	for _, f := range added {
		if err := checkpoint.Advance(w.Path, parents[f], f); err != nil {
			log.Printf("scadlab: avanzar el padre activo a %s: %v", f, err)
		}
	}
}

// linkSession guarda qué agente y qué sesión crearon los checkpoints nuevos,
// cuando hay una terminal de agente abierta en el workspace. La sesión es la más
// reciente de ese agente para la carpeta (agentsessions): el CLI la va
// escribiendo mientras trabaja, así que la que está activa ahora es la primera.
// Lo creado por la propia app (editor, chat) no se atribuye al agente.
func (s *checkpointService) linkSession(w store.Workspace, files []string) {
	s.mu.Lock()
	files = takeMarked(s.own, w.ID, files)
	s.mu.Unlock()
	if len(files) == 0 {
		return
	}
	agent := s.activeAgent(w.ID)
	if agent == "" {
		return
	}
	// Listar las sesiones lee archivos (o corre el CLI): en otra goroutine, para
	// no demorar el aviso al árbol ni el auto-render.
	s.runAsync(func() {
		sessionID := ""
		if agentsessions.CanList(agent) {
			list, err := s.sessions(w.ID, agent)
			if err != nil {
				log.Printf("scadlab: sesiones de %s: %v", agent, err)
			} else if len(list) > 0 {
				sessionID = list[0].ID
			}
		}
		linked := false
		for _, f := range files {
			// Quien ya tiene dueño (un chat lo guardó mientras tanto) no se pisa.
			if c, err := s.app.store.GetCheckpoint(w.ID, f); err == nil && (c.AgentID != "" || c.ChatID != 0) {
				continue
			}
			if err := s.app.store.SetCheckpointLink(w.ID, f, agent, sessionID); err != nil {
				log.Printf("scadlab: vincular %s con %s: %v", f, agent, err)
				continue
			}
			linked = true
		}
		if linked {
			s.app.emitView(w)
		}
	})
}

// emitView reescanea el workspace y avisa a la interfaz. Lo usan los bindings
// que cambian algo que el watcher no ve (el padre activo no es un .scad) o que
// quieren que el árbol responda ya, sin esperar el debounce del watcher.
func (a *App) emitView(w store.Workspace) {
	v, err := a.rescan(w)
	if err != nil {
		log.Printf("scadlab: rescan de %s: %v", w.Path, err)
		return
	}
	a.emit(evCheckpointsChanged, v)
}

// evCheckpointUpdated avisa de que el contenido de un checkpoint que ya existía
// cambió en el disco (S16). Contrato con frontend/src/lib/events.ts.
const evCheckpointUpdated = "checkpoint:updated"

// CheckpointUpdatedEvent es el payload de checkpoint:updated.
type CheckpointUpdatedEvent struct {
	WorkspaceID int64  `json:"workspaceId"`
	File        string `json:"file"`
}

// emitCheckpointUpdated avisa a la interfaz de que `file` cambió de contenido: si
// es el que está abierto y no tiene cambios sin guardar, relee el código, lo
// renderiza y refresca el visor y la etiqueta «editado por la IA».
func (a *App) emitCheckpointUpdated(workspaceID int64, file string) {
	a.emit(evCheckpointUpdated, CheckpointUpdatedEvent{WorkspaceID: workspaceID, File: file})
}

// checkpointWorkspace resuelve el workspace y valida el nombre del checkpoint:
// la entrada de todos los bindings de este archivo.
func (a *App) checkpointWorkspace(id int64, file string) (store.Workspace, error) {
	if err := a.ready(); err != nil {
		return store.Workspace{}, err
	}
	w, err := a.store.GetWorkspace(id)
	if err != nil {
		return w, err
	}
	if file != "" {
		if err := validCheckpointName(file); err != nil {
			return w, err
		}
	}
	return w, nil
}

// TrashCheckpoint manda un checkpoint a la papelera (con branch, también todos
// sus descendientes) y devuelve los nombres que les quedaron allí. Nunca borra:
// ListTrash y RestoreCheckpoint lo traen de vuelta.
func (a *App) TrashCheckpoint(id int64, file string, branch bool) ([]string, error) {
	w, err := a.checkpointWorkspace(id, file)
	if err != nil {
		return nil, err
	}
	var names []string
	if branch {
		names, err = checkpoint.TrashBranch(w.Path, file)
	} else {
		var name string
		// Los hijos directos pasan a colgar del abuelo (o quedan como raíz): el
		// árbol no se queda con padres ausentes por algo que el usuario hizo a
		// propósito. El rescan de emitView ya los ve con su padre nuevo.
		name, _, err = checkpoint.TrashRelinked(w.Path, file)
		if name != "" {
			names = []string{name}
		}
	}
	// Antes de que el rescan borre sus filas del índice se guarda lo que solo
	// vive en la base (rótulo, agente, sesión…), también si la operación quedó a
	// medias: lo que sí se movió tiene que poder volver igual.
	a.stashTrashedMeta(w, names)
	a.emitView(w)
	return names, err
}

// stashTrashedMeta asocia cada nombre de la papelera con su archivo original y
// guarda su rótulo, plegado y vínculo. Un fallo se registra y no impide nada:
// perder el rótulo es mejor que no poder borrar.
func (a *App) stashTrashedMeta(w store.Workspace, names []string) {
	if len(names) == 0 {
		return
	}
	items, err := checkpoint.ListTrash(w.Path)
	if err != nil {
		log.Printf("scadlab: leer la papelera de %s: %v", w.Path, err)
		return
	}
	original := make(map[string]string, len(items))
	for _, it := range items {
		original[it.Name] = it.Original
	}
	for _, name := range names {
		if orig, ok := original[name]; ok {
			if err := a.store.StashCheckpointMeta(w.ID, orig, name); err != nil {
				log.Printf("scadlab: guardar los datos de %s: %v", orig, err)
			}
		}
	}
}

// ListTrash lista la papelera del workspace, lo más reciente primero.
func (a *App) ListTrash(id int64) ([]checkpoint.TrashedItem, error) {
	w, err := a.checkpointWorkspace(id, "")
	if err != nil {
		return nil, err
	}
	return checkpoint.ListTrash(w.Path)
}

// RestoreCheckpoint saca un checkpoint de la papelera y devuelve el nombre con
// que volvió (el suyo si estaba libre; si no, el siguiente número con el mismo
// slug). Recupera también su rótulo y su vínculo con la sesión.
func (a *App) RestoreCheckpoint(id int64, trashedName string) (string, error) {
	w, err := a.checkpointWorkspace(id, trashedName)
	if err != nil {
		return "", err
	}
	// RestoreRelinked además devuelve a su padre los hijos que el borrado había
	// pasado al abuelo. Si eso falla a medias el checkpoint ya volvió: se sigue y
	// se devuelve el error al final, para que la interfaz lo muestre.
	name, _, relinkErr := checkpoint.RestoreRelinked(w.Path, trashedName)
	if name == "" {
		return "", relinkErr
	}
	a.checkpoints().markRestored(w.ID, name)
	if err := a.store.RestoreCheckpointMeta(w.ID, trashedName, name); err != nil {
		log.Printf("scadlab: recuperar los datos de %s: %v", name, err)
	}
	a.emitView(w)
	return name, relinkErr
}

// DeleteTrashed elimina PARA SIEMPRE un checkpoint de la papelera (por el nombre
// que lista ListTrash) junto con su registro de re-colgado y lo que la base
// guardaba de él. No se puede deshacer: la interfaz pide confirmación antes.
// Es solo del usuario —no hay herramienta MCP equivalente—.
func (a *App) DeleteTrashed(id int64, trashedName string) error {
	w, err := a.checkpointWorkspace(id, trashedName)
	if err != nil {
		return err
	}
	if err := checkpoint.PurgeTrashed(w.Path, trashedName); err != nil {
		return err
	}
	if err := a.store.ForgetCheckpointMeta(w.ID, trashedName); err != nil {
		log.Printf("scadlab: olvidar los datos de %s: %v", trashedName, err)
	}
	a.emitView(w)
	return nil
}

// EmptyTrash elimina PARA SIEMPRE todo lo que hay en .scadlab/trash del workspace
// (checkpoints, registros de re-colgado y cualquier otro archivo que alguien haya
// dejado allí) y devuelve cuántos checkpoints eran. No se puede deshacer. Vaciar
// la papelera no hace reaparecer el modelo de inicio al reabrir el workspace
// (checkpoint.StarterDeclined). Es solo del usuario, como el resto de este archivo
// que borra: no hay herramienta MCP.
func (a *App) EmptyTrash(id int64) (int, error) {
	w, err := a.checkpointWorkspace(id, "")
	if err != nil {
		return 0, err
	}
	n, err := checkpoint.EmptyTrash(w.Path)
	if ferr := a.store.ForgetCheckpointMeta(w.ID, ""); ferr != nil {
		log.Printf("scadlab: olvidar los datos de la papelera de %s: %v", w.Path, ferr)
	}
	a.emitView(w)
	return n, err
}

// MakeCheckpointRoot deja un checkpoint como principal (raíz): su `parent=` pasa
// a vacío. Es el arreglo de un hijo cuyo padre ya no está en el workspace
// —borrado a mano o mandado a la papelera por una versión anterior—. Reescribe
// solo la línea de cabecera (checkpoint.SetParent).
func (a *App) MakeCheckpointRoot(id int64, file string) error {
	w, err := a.checkpointWorkspace(id, file)
	if err != nil {
		return err
	}
	if file == "" {
		return i18n.New(i18n.Msg{ES: "nombre de checkpoint inválido", EN: "invalid checkpoint name"})
	}
	if err := checkpoint.SetParent(w.Path, file, ""); err != nil {
		return err
	}
	a.emitView(w)
	return nil
}

// SetActiveParent fija el padre activo: el checkpoint desde el que el agente (y
// el chat y el MCP) crean el próximo. "" lo limpia y vuelve a regir «la rama del
// más nuevo».
func (a *App) SetActiveParent(id int64, file string) error {
	w, err := a.checkpointWorkspace(id, file)
	if err != nil {
		return err
	}
	if err := checkpoint.SetActiveParent(w.Path, file); err != nil {
		return err
	}
	a.emitView(w)
	return nil
}

// GetActiveParent devuelve el padre activo ("" si no hay uno explícito).
func (a *App) GetActiveParent(id int64) (string, error) {
	w, err := a.checkpointWorkspace(id, "")
	if err != nil {
		return "", err
	}
	return checkpoint.ActiveParent(w.Path), nil
}

// cleanLabel normaliza un rótulo: sin saltos ni caracteres de control, espacios
// recortados. Vacío es válido (vuelve a mostrarse el nombre del archivo).
func cleanLabel(label string) (string, error) {
	label = strings.Join(strings.FieldsFunc(label, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
	if utf8.RuneCountInString(label) > maxLabelRunes {
		return "", i18n.Errorf(i18n.Msg{ES: "el nombre es demasiado largo (máximo %d letras)", EN: "the name is too long (at most %d characters)"}, maxLabelRunes)
	}
	return label, nil
}

// RenameCheckpoint cambia el rótulo que se muestra en el árbol. El archivo no
// cambia de nombre: sus hijos lo referencian en `parent=` y un checkpoint es
// inmutable. Un rótulo vacío lo quita.
func (a *App) RenameCheckpoint(id int64, file, label string) error {
	w, err := a.checkpointWorkspace(id, file)
	if err != nil {
		return err
	}
	label, err = cleanLabel(label)
	if err != nil {
		return err
	}
	if st, err := os.Stat(filepath.Join(w.Path, file)); err != nil || st.IsDir() {
		return i18n.Errorf(i18n.Msg{ES: "no se encontró el checkpoint %s", EN: "checkpoint %s was not found"}, file)
	}
	cp, err := a.store.GetCheckpoint(w.ID, file)
	if errors.Is(err, store.ErrNotFound) { // recién creado: todavía sin escanear
		if _, err := a.rescan(w); err != nil {
			return err
		}
		cp, err = a.store.GetCheckpoint(w.ID, file)
	}
	if err != nil {
		return err
	}
	if err := a.store.SetCheckpointUI(w.ID, file, label, cp.Collapsed); err != nil {
		return err
	}
	a.emitView(w)
	return nil
}

// CheckpointPair es un checkpoint y su padre, para el diff.
type CheckpointPair struct {
	File         string `json:"file"`
	Parent       string `json:"parent"`
	ParentSource string `json:"parentSource"`
	Source       string `json:"source"`
}

// ReadCheckpointPair devuelve el código de un checkpoint y el de su padre (el
// del encabezado `parent=`). Falla con un motivo claro si es una raíz o si el
// padre ya no está en el workspace (por ejemplo, está en la papelera).
func (a *App) ReadCheckpointPair(id int64, file string) (CheckpointPair, error) {
	w, err := a.checkpointWorkspace(id, file)
	if err != nil {
		return CheckpointPair{}, err
	}
	files, err := workspace.Scan(w.Path)
	if err != nil {
		return CheckpointPair{}, err
	}
	parent, found := "", false
	for _, f := range files {
		if f.FileName == file {
			parent, found = f.ParentFile, true
			break
		}
	}
	if !found {
		return CheckpointPair{}, i18n.Errorf(i18n.Msg{ES: "no se encontró el checkpoint %s", EN: "checkpoint %s was not found"}, file)
	}
	if parent == "" {
		return CheckpointPair{}, i18n.Errorf(i18n.Msg{ES: "%s es una raíz: no tiene padre con el que compararlo", EN: "%s is a root: it has no parent to compare with"}, file)
	}
	if validCheckpointName(parent) != nil {
		return CheckpointPair{}, i18n.Errorf(i18n.Msg{ES: "el padre de %s no es un nombre de checkpoint válido", EN: "the parent of %s is not a valid checkpoint name"}, file)
	}
	parentRaw, err := os.ReadFile(filepath.Join(w.Path, parent))
	if err != nil {
		return CheckpointPair{}, i18n.Errorf(i18n.Msg{ES: "el padre %s ya no está en el workspace (¿está en la papelera?)", EN: "the parent %s is no longer in the workspace (is it in the trash?)"}, parent)
	}
	raw, err := os.ReadFile(filepath.Join(w.Path, file))
	if err != nil {
		return CheckpointPair{}, err
	}
	return CheckpointPair{File: file, Parent: parent, ParentSource: string(parentRaw), Source: string(raw)}, nil
}

// RevealCheckpoint muestra el archivo del checkpoint en el Finder / Explorador.
func (a *App) RevealCheckpoint(id int64, file string) error {
	w, err := a.checkpointWorkspace(id, file)
	if err != nil {
		return err
	}
	if file == "" {
		return i18n.New(i18n.Msg{ES: "nombre de checkpoint inválido", EN: "invalid checkpoint name"})
	}
	return a.RevealPath(filepath.Join(w.Path, file))
}

// CheckpointResume dice qué se abrió al retomar la sesión de un checkpoint: una
// terminal de agente (terminalId) o un chat de diseño (chatId) que la interfaz
// tiene que mostrar.
type CheckpointResume struct {
	Kind       string `json:"kind"` // "terminal" | "chat"
	TerminalID string `json:"terminalId"`
	AgentID    string `json:"agentId"`
	ChatID     int64  `json:"chatId"`
}

// ResumeCheckpointSession retoma la conversación en la que se creó el
// checkpoint: si lo creó un agente de la terminal, abre ese CLI en modo
// `resume` con la sesión guardada (StartAgentTerminal); si lo guardó un chat de
// diseño, devuelve el chat para que la interfaz lo abra.
func (a *App) ResumeCheckpointSession(id int64, file string) (CheckpointResume, error) {
	w, err := a.checkpointWorkspace(id, file)
	if err != nil {
		return CheckpointResume{}, err
	}
	cp, err := a.store.GetCheckpoint(w.ID, file)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return CheckpointResume{}, i18n.New(i18n.Msg{ES: "este checkpoint no tiene una sesión vinculada", EN: "this checkpoint has no linked session"})
		}
		return CheckpointResume{}, err
	}
	switch {
	case cp.AgentID != "" && cp.SessionID != "":
		term, err := a.checkpoints().startTerminal(w.ID, cp.AgentID, agentsessions.ModeResume, cp.SessionID)
		if err != nil {
			return CheckpointResume{}, err
		}
		return CheckpointResume{Kind: "terminal", TerminalID: term, AgentID: cp.AgentID}, nil
	case cp.ChatID != 0:
		chat, err := a.store.GetChat(cp.ChatID)
		if err != nil || chat.WorkspaceID != w.ID {
			return CheckpointResume{}, i18n.New(i18n.Msg{ES: "el chat donde se creó este checkpoint ya no existe", EN: "the chat where this checkpoint was created no longer exists"})
		}
		return CheckpointResume{Kind: "chat", ChatID: chat.ID}, nil
	}
	return CheckpointResume{}, i18n.New(i18n.Msg{ES: "este checkpoint no tiene una sesión vinculada", EN: "this checkpoint has no linked session"})
}

// sortedCopy devuelve una copia ordenada (el watcher ya entrega ordenado, pero
// el orden de avance importa y no se quiere depender de ello).
func sortedCopy(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}
