package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"scadlab/backend/agentsessions"
	"scadlab/backend/checkpoint"
	"scadlab/backend/store"
)

const sessA = "11111111-2222-3333-4444-555555555555"

// ckptFixture es un workspace con tres checkpoints en cadena (001 → 002 → 003),
// un emisor que recuerda los avisos a la UI y un servicio sin terminal ni CLIs
// reales.
type ckptFixture struct {
	a  *App
	w  store.Workspace
	mu sync.Mutex

	views    []WorkspaceView
	agent    string // agente «abierto» en el workspace
	sessions []agentsessions.Session
	started  [][]string // [agente, modo, sesión]
}

func newCkptFixture(t *testing.T) *ckptFixture {
	t.Helper()
	a := newTestApp(t)
	// El auto-render lanzaría OpenSCAD en segundo plano: otro asunto.
	s, _ := a.store.GetSettings()
	s.AutoRender = false
	if err := a.store.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	w, err := a.CreateWorkspace("Prueba checkpoints", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	f := &ckptFixture{a: a, w: w}
	a.emitter = func(name string, payload any) {
		if v, ok := payload.(WorkspaceView); ok && name == evCheckpointsChanged {
			f.mu.Lock()
			f.views = append(f.views, v)
			f.mu.Unlock()
		}
	}
	svc := a.checkpoints()
	svc.runAsync = func(fn func()) { fn() }
	svc.activeAgent = func(int64) string { return f.agent }
	svc.sessions = func(int64, string) ([]agentsessions.Session, error) { return f.sessions, nil }
	svc.startTerminal = func(_ int64, agentID, mode, sid string) (string, error) {
		f.started = append(f.started, []string{agentID, mode, sid})
		return "term-9", nil
	}
	f.write("001-base.scad", "base", "")
	f.write("002-lid.scad", "lid", "001-base.scad")
	f.write("003-vents.scad", "vents", "002-lid.scad")
	if _, err := a.RescanWorkspace(w.ID); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *ckptFixture) write(name, desc, parent string) {
	body := checkpoint.Header(parent, desc) + "\ncube(10);\n"
	if err := os.WriteFile(filepath.Join(f.w.Path, name), []byte(body), 0o644); err != nil {
		panic(err)
	}
}

func (f *ckptFixture) exists(name string) bool {
	_, err := os.Stat(filepath.Join(f.w.Path, name))
	return err == nil
}

func (f *ckptFixture) row(t *testing.T, name string) store.Checkpoint {
	t.Helper()
	c, err := f.a.store.GetCheckpoint(f.w.ID, name)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return c
}

func (f *ckptFixture) lastView() WorkspaceView {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.views) == 0 {
		return WorkspaceView{}
	}
	return f.views[len(f.views)-1]
}

func TestTrashOneAndBranchThenRestoreKeepsLabel(t *testing.T) {
	f := newCkptFixture(t)
	if err := f.a.RenameCheckpoint(f.w.ID, "002-lid.scad", "Tapa deslizante"); err != nil {
		t.Fatal(err)
	}
	if err := f.a.store.SetCheckpointLink(f.w.ID, "002-lid.scad", "claude", sessA); err != nil {
		t.Fatal(err)
	}

	// La rama entera: 002 y su hijo 003.
	names, err := f.a.TrashCheckpoint(f.w.ID, "002-lid.scad", true)
	if err != nil || len(names) != 2 {
		t.Fatalf("papelera: %v, %v", names, err)
	}
	if f.exists("002-lid.scad") || f.exists("003-vents.scad") || !f.exists("001-base.scad") {
		t.Fatal("la rama tenía que salir del workspace y la raíz quedarse")
	}
	if v := f.lastView(); len(v.Checkpoints) != 1 {
		t.Errorf("el árbol avisado tenía que quedar con 1 checkpoint: %+v", v.Checkpoints)
	}
	items, err := f.a.ListTrash(f.w.ID)
	if err != nil || len(items) != 2 {
		t.Fatalf("ListTrash: %+v, %v", items, err)
	}

	// Restaurar el padre: vuelve con su rótulo y su vínculo.
	var lid string
	for _, it := range items {
		if it.Original == "002-lid.scad" {
			lid = it.Name
		}
	}
	got, err := f.a.RestoreCheckpoint(f.w.ID, lid)
	if err != nil || got != "002-lid.scad" {
		t.Fatalf("restaurar: %q, %v", got, err)
	}
	c := f.row(t, "002-lid.scad")
	if c.Label != "Tapa deslizante" || c.AgentID != "claude" || c.SessionID != sessA || c.ParentFile != "001-base.scad" {
		t.Errorf("fila restaurada: %+v", c)
	}
	if rest, _ := f.a.ListTrash(f.w.ID); len(rest) != 1 {
		t.Errorf("en la papelera debía quedar 1: %+v", rest)
	}

	// Solo uno: los hijos no se mueven.
	if _, err := f.a.TrashCheckpoint(f.w.ID, "001-base.scad", false); err != nil {
		t.Fatal(err)
	}
	if f.exists("001-base.scad") || !f.exists("002-lid.scad") {
		t.Error("solo la raíz debía irse")
	}
}

func TestTrashRejectsHostileNames(t *testing.T) {
	f := newCkptFixture(t)
	outside := filepath.Join(filepath.Dir(f.w.Path), "fuera.scad")
	if err := os.WriteFile(outside, []byte("cube(1);"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"../fuera.scad", "sub/001-base.scad", "", "notes.txt", `..\fuera.scad`} {
		if _, err := f.a.TrashCheckpoint(f.w.ID, name, true); err == nil {
			t.Errorf("TrashCheckpoint(%q) tenía que fallar", name)
		}
		if err := f.a.RenameCheckpoint(f.w.ID, name, "x"); err == nil {
			t.Errorf("RenameCheckpoint(%q) tenía que fallar", name)
		}
		if _, err := f.a.ReadCheckpointPair(f.w.ID, name); err == nil {
			t.Errorf("ReadCheckpointPair(%q) tenía que fallar", name)
		}
		if _, err := f.a.RestoreCheckpoint(f.w.ID, name); err == nil {
			t.Errorf("RestoreCheckpoint(%q) tenía que fallar", name)
		}
		if err := f.a.SetActiveParent(f.w.ID, name); err == nil && name != "" {
			t.Errorf("SetActiveParent(%q) tenía que fallar", name)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("el archivo de afuera tiene que seguir: %v", err)
	}
}

func TestActiveParentBindingsEmitAndFollowTrash(t *testing.T) {
	f := newCkptFixture(t)
	if got, _ := f.a.GetActiveParent(f.w.ID); got != "" {
		t.Fatalf("sin padre activo explícito: %q", got)
	}
	if err := f.a.SetActiveParent(f.w.ID, "002-lid.scad"); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.a.GetActiveParent(f.w.ID); got != "002-lid.scad" {
		t.Errorf("activo: %q", got)
	}
	if v := f.lastView(); v.ActiveParent != "002-lid.scad" {
		t.Errorf("el cambio tenía que avisar a la UI con el activo nuevo: %+v", v.ActiveParent)
	}
	if err := f.a.SetActiveParent(f.w.ID, "099-nope.scad"); err == nil {
		t.Error("un archivo inexistente tenía que fallar")
	}
	// El activo a la papelera: la UI no queda apuntando a un fantasma.
	if _, err := f.a.TrashCheckpoint(f.w.ID, "002-lid.scad", false); err != nil {
		t.Fatal(err)
	}
	if v := f.lastView(); v.ActiveParent == "002-lid.scad" {
		t.Errorf("el activo no puede seguir siendo un checkpoint de la papelera: %q", v.ActiveParent)
	}
	if err := f.a.SetActiveParent(f.w.ID, ""); err != nil {
		t.Errorf("limpiar: %v", err)
	}
}

func TestRenameCheckpoint(t *testing.T) {
	f := newCkptFixture(t)
	if err := f.a.store.SetCheckpointUI(f.w.ID, "002-lid.scad", "", true); err != nil {
		t.Fatal(err)
	}
	if err := f.a.RenameCheckpoint(f.w.ID, "002-lid.scad", "  Tapa\n  deslizante \t "); err != nil {
		t.Fatal(err)
	}
	c := f.row(t, "002-lid.scad")
	if c.Label != "Tapa deslizante" || !c.Collapsed {
		t.Errorf("rótulo y plegado: %+v", c)
	}
	if v := f.lastView(); len(v.Checkpoints) != 3 {
		t.Errorf("tenía que avisar al árbol: %+v", v)
	}
	if err := f.a.RenameCheckpoint(f.w.ID, "002-lid.scad", strings.Repeat("ñ", maxLabelRunes+1)); err == nil {
		t.Error("un rótulo demasiado largo tenía que fallar")
	}
	if err := f.a.RenameCheckpoint(f.w.ID, "002-lid.scad", strings.Repeat("ñ", maxLabelRunes)); err != nil {
		t.Errorf("el largo máximo es válido: %v", err)
	}
	if err := f.a.RenameCheckpoint(f.w.ID, "002-lid.scad", ""); err != nil || f.row(t, "002-lid.scad").Label != "" {
		t.Errorf("vacío quita el rótulo: %v", err)
	}
	if err := f.a.RenameCheckpoint(f.w.ID, "050-missing.scad", "x"); err == nil {
		t.Error("renombrar un archivo que no existe tenía que fallar")
	}
	// El archivo no cambia de nombre: los hijos lo referencian.
	if !f.exists("002-lid.scad") {
		t.Error("el archivo no se renombra")
	}
}

func TestReadCheckpointPair(t *testing.T) {
	f := newCkptFixture(t)
	p, err := f.a.ReadCheckpointPair(f.w.ID, "003-vents.scad")
	if err != nil || p.Parent != "002-lid.scad" || !strings.Contains(p.ParentSource, `desc="lid"`) || !strings.Contains(p.Source, `desc="vents"`) {
		t.Fatalf("par: %+v, %v", p, err)
	}
	if _, err := f.a.ReadCheckpointPair(f.w.ID, "001-base.scad"); err == nil {
		t.Error("una raíz no tiene padre")
	}
	// El padre en la papelera: motivo claro, no un error de archivo.
	if _, err := f.a.TrashCheckpoint(f.w.ID, "002-lid.scad", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.ReadCheckpointPair(f.w.ID, "003-vents.scad"); err == nil || !strings.Contains(err.Error(), "002-lid.scad") {
		t.Errorf("padre en la papelera: %v", err)
	}
}

// Un agente que escribe el .scad directo: el watcher avanza el padre activo y
// vincula el checkpoint con la terminal abierta y su sesión más reciente.
func TestWatcherAdvancesActiveParentAndLinksSession(t *testing.T) {
	f := newCkptFixture(t)
	f.agent = "claude"
	f.sessions = []agentsessions.Session{{ID: sessA}, {ID: "99999999-2222-3333-4444-555555555555"}}
	if err := f.a.SetActiveParent(f.w.ID, "003-vents.scad"); err != nil {
		t.Fatal(err)
	}

	f.write("004-ports.scad", "ports", "003-vents.scad")
	f.a.onWorkspaceChanged(f.w.ID, []string{"004-ports.scad"})
	if got, _ := f.a.GetActiveParent(f.w.ID); got != "004-ports.scad" {
		t.Errorf("el activo tenía que avanzar al hijo nuevo: %q", got)
	}
	c := f.row(t, "004-ports.scad")
	if c.AgentID != "claude" || c.SessionID != sessA || c.ParentFile != "003-vents.scad" {
		t.Errorf("vínculo: %+v", c)
	}
	if v := f.lastView(); v.ActiveParent != "004-ports.scad" || len(v.Checkpoints) != 4 {
		t.Errorf("la UI tenía que enterarse: %+v", v.ActiveParent)
	}

	// Una variante de otro padre no mueve la rama activa.
	f.write("005-alt.scad", "alt", "002-lid.scad")
	f.a.onWorkspaceChanged(f.w.ID, []string{"005-alt.scad"})
	if got, _ := f.a.GetActiveParent(f.w.ID); got != "004-ports.scad" {
		t.Errorf("una variante lateral no mueve el activo: %q", got)
	}
}

func TestLinkSessionRules(t *testing.T) {
	f := newCkptFixture(t)

	// Sin terminal de agente abierta: nada que vincular.
	f.write("004-a.scad", "a", "003-vents.scad")
	f.a.onWorkspaceChanged(f.w.ID, []string{"004-a.scad"})
	if c := f.row(t, "004-a.scad"); c.AgentID != "" || c.SessionID != "" {
		t.Errorf("sin terminal no hay vínculo: %+v", c)
	}

	// Lo guardado desde el editor o el chat no es del agente aunque haya uno abierto.
	f.agent = "claude"
	f.sessions = []agentsessions.Session{{ID: sessA}}
	f.write("005-own.scad", "own", "004-a.scad")
	f.a.checkpoints().markOwn(f.w.ID, "005-own.scad")
	f.a.onWorkspaceChanged(f.w.ID, []string{"005-own.scad"})
	if c := f.row(t, "005-own.scad"); c.AgentID != "" {
		t.Errorf("lo propio no se atribuye al agente: %+v", c)
	}

	// Antigravity no lista sesiones: se guarda el agente, sin sesión.
	f.agent = "antigravity"
	f.write("006-agy.scad", "agy", "005-own.scad")
	f.a.onWorkspaceChanged(f.w.ID, []string{"006-agy.scad"})
	if c := f.row(t, "006-agy.scad"); c.AgentID != "antigravity" || c.SessionID != "" {
		t.Errorf("agente sin listado de sesiones: %+v", c)
	}

	// Un lote de varios a la vez: todos quedan con el mismo vínculo.
	f.agent, f.sessions = "codex", []agentsessions.Session{{ID: sessA}}
	f.write("007-x.scad", "x", "006-agy.scad")
	f.write("008-y.scad", "y", "007-x.scad")
	f.a.onWorkspaceChanged(f.w.ID, []string{"007-x.scad", "008-y.scad"})
	for _, n := range []string{"007-x.scad", "008-y.scad"} {
		if c := f.row(t, n); c.AgentID != "codex" || c.SessionID != sessA {
			t.Errorf("%s: %+v", n, c)
		}
	}
}

// Un checkpoint que vuelve de la papelera mientras hay un agente abierto no se
// le atribuye ni mueve el padre activo.
func TestRestoredCheckpointIsNotNewWork(t *testing.T) {
	f := newCkptFixture(t)
	names, err := f.a.TrashCheckpoint(f.w.ID, "003-vents.scad", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.a.SetActiveParent(f.w.ID, "002-lid.scad"); err != nil {
		t.Fatal(err)
	}
	f.agent, f.sessions = "claude", []agentsessions.Session{{ID: sessA}}
	name, err := f.a.RestoreCheckpoint(f.w.ID, names[0])
	if err != nil {
		t.Fatal(err)
	}
	f.a.onWorkspaceChanged(f.w.ID, []string{name}) // lo que vería el watcher
	if got, _ := f.a.GetActiveParent(f.w.ID); got != "002-lid.scad" {
		t.Errorf("restaurar no mueve el activo: %q", got)
	}
	if c := f.row(t, name); c.AgentID != "" {
		t.Errorf("restaurar no vincula con el agente abierto: %+v", c)
	}
	// Pero el siguiente que aparezca sí cuenta (la marca se consumió).
	f.write("004-new.scad", "new", "002-lid.scad")
	f.a.onWorkspaceChanged(f.w.ID, []string{"004-new.scad"})
	if got, _ := f.a.GetActiveParent(f.w.ID); got != "004-new.scad" {
		t.Errorf("un checkpoint nuevo sí avanza el activo: %q", got)
	}
}

func TestResumeCheckpointSession(t *testing.T) {
	f := newCkptFixture(t)
	if _, err := f.a.ResumeCheckpointSession(f.w.ID, "001-base.scad"); err == nil {
		t.Error("sin vínculo tenía que fallar")
	}

	// Agente: abre el CLI en modo resume con la sesión guardada.
	if err := f.a.store.SetCheckpointLink(f.w.ID, "002-lid.scad", "claude", sessA); err != nil {
		t.Fatal(err)
	}
	r, err := f.a.ResumeCheckpointSession(f.w.ID, "002-lid.scad")
	if err != nil || r.Kind != "terminal" || r.TerminalID != "term-9" || r.AgentID != "claude" {
		t.Fatalf("resume: %+v, %v", r, err)
	}
	if len(f.started) != 1 || f.started[0][0] != "claude" || f.started[0][1] != "resume" || f.started[0][2] != sessA {
		t.Errorf("lanzador: %v", f.started)
	}

	// Un agente sin id de sesión (Antigravity) no se puede retomar por id.
	if err := f.a.store.SetCheckpointLink(f.w.ID, "003-vents.scad", "antigravity", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.ResumeCheckpointSession(f.w.ID, "003-vents.scad"); err == nil {
		t.Error("sin sesión guardada tenía que fallar")
	}
	if len(f.started) != 1 {
		t.Errorf("no tenía que lanzar nada: %v", f.started)
	}

	// Chat: devuelve el chat, y uno borrado se informa.
	chat, err := f.a.store.CreateChat(f.w.ID, "ollama", "m")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.a.store.SetCheckpointChat(f.w.ID, "001-base.scad", chat.ID); err != nil {
		t.Fatal(err)
	}
	r, err = f.a.ResumeCheckpointSession(f.w.ID, "001-base.scad")
	if err != nil || r.Kind != "chat" || r.ChatID != chat.ID {
		t.Fatalf("chat: %+v, %v", r, err)
	}
	if err := f.a.store.DeleteChat(chat.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.ResumeCheckpointSession(f.w.ID, "001-base.scad"); err == nil {
		t.Error("un chat borrado tenía que fallar")
	}
	if len(f.started) != 1 {
		t.Errorf("el chat no abre terminales: %v", f.started)
	}
}

func TestSaveChatCheckpointStoresChat(t *testing.T) {
	f := newCkptFixture(t)
	chat, err := f.a.store.CreateChat(f.w.ID, "ollama", "m")
	if err != nil {
		t.Fatal(err)
	}
	f.agent, f.sessions = "claude", []agentsessions.Session{{ID: sessA}}
	name, err := f.a.SaveChatCheckpoint(f.w.ID, chat.ID, "003-vents.scad", "Del chat", "", "cube(5);")
	if err != nil {
		t.Fatal(err)
	}
	f.a.onWorkspaceChanged(f.w.ID, []string{name}) // el watcher lo ve aparecer
	c := f.row(t, name)
	if c.ChatID != chat.ID || c.AgentID != "" {
		t.Errorf("tenía que quedar del chat y no del agente abierto: %+v", c)
	}

	// Un chat de otro workspace o inexistente no se guarda, y el checkpoint sí se crea.
	name2, err := f.a.SaveChatCheckpoint(f.w.ID, 9999, name, "Otro", "", "cube(6);")
	if err != nil {
		t.Fatal(err)
	}
	f.a.onWorkspaceChanged(f.w.ID, []string{name2})
	if c := f.row(t, name2); c.ChatID != 0 {
		t.Errorf("chat inexistente: %+v", c)
	}
}

// Guardar desde el editor tampoco se atribuye al agente.
func TestSaveAsCheckpointIsOwn(t *testing.T) {
	f := newCkptFixture(t)
	f.agent, f.sessions = "claude", []agentsessions.Session{{ID: sessA}}
	name, err := f.a.SaveAsCheckpoint(f.w.ID, "003-vents.scad", "A mano", "", "cube(5);")
	if err != nil {
		t.Fatal(err)
	}
	f.a.onWorkspaceChanged(f.w.ID, []string{name})
	if c := f.row(t, name); c.AgentID != "" {
		t.Errorf("lo que guarda el usuario no es del agente: %+v", c)
	}
}
