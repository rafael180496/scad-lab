package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"scadlab/backend/agentsessions"
	"scadlab/backend/checkpoint"
	"scadlab/backend/mcpconf"
	"scadlab/backend/render"
	"scadlab/backend/store"
)

// projFixture es una App con n workspaces creados y un emisor que recuerda los
// eventos. Al terminar detiene los watchers (si no, siguen vivos después del
// test y el directorio temporal no se puede borrar).
type projFixture struct {
	a  *App
	ws []store.Workspace

	mu     sync.Mutex
	events []projEvent
}

type projEvent struct {
	name    string
	payload any
}

func newProjFixture(t *testing.T, n int) *projFixture {
	t.Helper()
	withMCPBinary(t, "/Apps/ScadLab")
	a := newTestApp(t)
	f := &projFixture{a: a}
	a.emitter = func(name string, payload any) {
		f.mu.Lock()
		f.events = append(f.events, projEvent{name, payload})
		f.mu.Unlock()
	}
	t.Cleanup(a.stopWatcher)
	for i := 0; i < n; i++ {
		w, err := a.CreateWorkspace(fmt.Sprintf("Proyecto %d", i+1), "bambu-a1-mini")
		if err != nil {
			t.Fatal(err)
		}
		f.ws = append(f.ws, w)
	}
	return f
}

func (f *projFixture) eventsNamed(name string) []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []any
	for _, e := range f.events {
		if e.name == name {
			out = append(out, e.payload)
		}
	}
	return out
}

func (f *projFixture) openAll(t *testing.T, ids ...int) {
	t.Helper()
	for _, i := range ids {
		if _, err := f.a.OpenProject(f.ws[i].ID); err != nil {
			t.Fatalf("abrir %d: %v", i, err)
		}
	}
}

func (f *projFixture) list(t *testing.T) ([]int64, int64) {
	t.Helper()
	v, err := f.a.ListOpenProjects()
	if err != nil {
		t.Fatal(err)
	}
	ids := []int64{}
	for _, p := range v.Projects {
		ids = append(ids, p.Workspace.ID)
	}
	return ids, v.ActiveID
}

func TestOpenFourAndRejectTheFifth(t *testing.T) {
	f := newProjFixture(t, 5)
	f.openAll(t, 0, 1, 2, 3)

	ids, active := f.list(t)
	want := []int64{f.ws[0].ID, f.ws[1].ID, f.ws[2].ID, f.ws[3].ID}
	if !reflect.DeepEqual(ids, want) || active != f.ws[3].ID {
		t.Fatalf("lista %v activo %d, se esperaba %v / %d", ids, active, want, f.ws[3].ID)
	}
	for i := 0; i < 4; i++ {
		if !f.a.isWatched(f.ws[i].ID) {
			t.Errorf("el proyecto %d debía tener su watcher", i+1)
		}
	}

	rulesBefore, _ := os.ReadFile(filepath.Join(f.ws[4].Path, "CLAUDE.md"))
	_, err := f.a.OpenProject(f.ws[4].ID)
	if err == nil || !strings.Contains(err.Error(), "4") {
		t.Fatalf("el 5.º debía rechazarse con un error claro: %v", err)
	}
	if ids, active := f.list(t); len(ids) != 4 || active != f.ws[3].ID {
		t.Errorf("un rechazo no cambia nada: %v activo %d", ids, active)
	}
	if f.a.isWatched(f.ws[4].ID) {
		t.Error("un proyecto rechazado no debe quedar vigilado")
	}
	if rulesAfter, _ := os.ReadFile(filepath.Join(f.ws[4].Path, "CLAUDE.md")); string(rulesAfter) != string(rulesBefore) {
		t.Error("un proyecto rechazado no debe reescribir sus archivos")
	}

	// Volver a abrir uno que ya está abierto no cuenta de más: lo activa sin moverlo.
	if _, err := f.a.OpenProject(f.ws[1].ID); err != nil {
		t.Fatalf("reabrir uno abierto con el tope lleno: %v", err)
	}
	ids, active = f.list(t)
	if !reflect.DeepEqual(ids, want) || active != f.ws[1].ID {
		t.Errorf("reabrir no mueve la pestaña: %v activo %d", ids, active)
	}

	// Cerrar uno deja lugar.
	if err := f.a.CloseProject(f.ws[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.OpenProject(f.ws[4].ID); err != nil {
		t.Fatalf("tras cerrar uno debía abrir: %v", err)
	}
	// OpenWorkspace es el alias de siempre.
	if _, err := f.a.OpenWorkspace(f.ws[2].ID); err != nil {
		t.Fatal(err)
	}
	if _, active := f.list(t); active != f.ws[2].ID {
		t.Errorf("OpenWorkspace debía activarlo: %d", active)
	}
	if n := len(f.eventsNamed(evProjectsChanged)); n == 0 {
		t.Error("faltó projects:changed")
	} else if v, ok := f.eventsNamed(evProjectsChanged)[n-1].(OpenProjectsView); !ok || v.ActiveID != f.ws[2].ID || len(v.Projects) != 4 {
		t.Errorf("payload de projects:changed: %+v", v)
	}
}

// Nada se abre solo al arrancar: ListOpenProjects solo lee.
func TestListOpenProjectsOpensNothing(t *testing.T) {
	f := newProjFixture(t, 2)
	f.openAll(t, 0, 1)
	f.a.stopWatcher() // «reinicio»: los watchers mueren con el proceso

	ids, active := f.list(t)
	if len(ids) != 2 || active != f.ws[1].ID {
		t.Fatalf("la lista persistida: %v activo %d", ids, active)
	}
	for _, w := range f.ws {
		if f.a.isWatched(w.ID) {
			t.Errorf("ListOpenProjects no debe vigilar nada (%s)", w.Name)
		}
	}
	// El frontend reabre con OpenProject: queda vigilado y no se duplica.
	if _, err := f.a.OpenProject(f.ws[0].ID); err != nil {
		t.Fatal(err)
	}
	if !f.a.isWatched(f.ws[0].ID) {
		t.Error("OpenProject debía volver a vigilar")
	}
	if ids, _ := f.list(t); len(ids) != 2 {
		t.Errorf("reabrir no duplica: %v", ids)
	}
}

func TestProjectsPersistOrderActiveAndMissing(t *testing.T) {
	f := newProjFixture(t, 4)
	f.openAll(t, 0, 1, 2)
	a, b, c := f.ws[0].ID, f.ws[1].ID, f.ws[2].ID

	// Reordenar es tolerante: un id ajeno o repetido se ignora y los que faltan
	// quedan al final en su orden; el activo no cambia.
	if err := f.a.ReorderProjects([]int64{c, 999, a, c}); err != nil {
		t.Fatal(err)
	}
	if ids, active := f.list(t); !reflect.DeepEqual(ids, []int64{c, a, b}) || active != c {
		t.Fatalf("orden %v activo %d", ids, active)
	}
	if err := f.a.SetActiveProject(a); err != nil {
		t.Fatal(err)
	}
	if err := f.a.SetActiveProject(f.ws[3].ID); err == nil {
		t.Error("activar un proyecto cerrado debía fallar")
	}

	// Persistido en settings, no en memoria: una App nueva sobre la misma base lo ve.
	s, _ := f.a.store.GetSettings()
	if !reflect.DeepEqual(s.OpenProjects, []int64{c, a, b}) || s.ActiveProject != a {
		t.Fatalf("settings: %v activo %d", s.OpenProjects, s.ActiveProject)
	}
	again := &App{ctx: f.a.ctx, store: f.a.store}
	v, err := again.ListOpenProjects()
	if err != nil || len(v.Projects) != 3 || v.Projects[0].Workspace.ID != c || v.ActiveID != a {
		t.Fatalf("tras reiniciar: %+v, %v", v, err)
	}

	// La carpeta de un proyecto desapareció: se marca missing y no rompe.
	if err := os.RemoveAll(f.ws[1].Path); err != nil {
		t.Fatal(err)
	}
	// Un workspace quitado del registro se omite.
	if err := again.store.RemoveWorkspace(c); err != nil {
		t.Fatal(err)
	}
	v, err = again.ListOpenProjects()
	if err != nil || len(v.Projects) != 2 {
		t.Fatalf("lista con un quitado: %+v, %v", v, err)
	}
	for _, p := range v.Projects {
		if p.Workspace.ID == b && !p.Missing || p.Workspace.ID == a && p.Missing {
			t.Errorf("missing mal calculado: %+v", p)
		}
	}
	// Abrir el que ya no tiene carpeta no revienta y no lo vigila.
	view, err := again.OpenProject(b)
	if err != nil || !view.Missing || again.isWatched(b) {
		t.Errorf("abrir con la carpeta ausente: %+v, %v", view.Missing, err)
	}
}

func TestCloseActiveProjectPicksTheNeighbour(t *testing.T) {
	f := newProjFixture(t, 3)
	f.openAll(t, 0, 1, 2)
	a, b, c := f.ws[0].ID, f.ws[1].ID, f.ws[2].ID

	if err := f.a.SetActiveProject(b); err != nil {
		t.Fatal(err)
	}
	if err := f.a.CloseProject(b); err != nil {
		t.Fatal(err)
	}
	if ids, active := f.list(t); !reflect.DeepEqual(ids, []int64{a, c}) || active != c {
		t.Fatalf("cerrar el del medio: %v activo %d", ids, active)
	}
	if err := f.a.CloseProject(c); err != nil { // el último de la fila
		t.Fatal(err)
	}
	if _, active := f.list(t); active != a {
		t.Errorf("cerrar el último pasa al anterior: %d", active)
	}
	if err := f.a.CloseProject(b); err != nil { // idempotente
		t.Errorf("cerrar uno cerrado no es un error: %v", err)
	}
	if err := f.a.CloseProject(a); err != nil {
		t.Fatal(err)
	}
	if ids, active := f.list(t); len(ids) != 0 || active != 0 {
		t.Errorf("sin proyectos: %v activo %d", ids, active)
	}
	for _, w := range f.ws {
		if f.a.isWatched(w.ID) {
			t.Errorf("%s sigue vigilado", w.Name)
		}
	}
}

// Quitar un workspace del registro lo cierra primero.
func TestRemoveWorkspaceClosesItsProject(t *testing.T) {
	f := newProjFixture(t, 2)
	f.openAll(t, 0, 1)
	if err := f.a.RemoveWorkspace(f.ws[0].ID); err != nil {
		t.Fatal(err)
	}
	if f.a.isWatched(f.ws[0].ID) {
		t.Error("el workspace quitado sigue vigilado")
	}
	if ids, active := f.list(t); !reflect.DeepEqual(ids, []int64{f.ws[1].ID}) || active != f.ws[1].ID {
		t.Errorf("tras quitar: %v activo %d", ids, active)
	}
}

// Un proyecto de fondo sigue vivo: su watcher avisa a la UI, vincula la sesión
// del agente y dispara el auto-render. Sin OpenSCAD el render termina en
// «missing», que también cierra el ciclo; con OpenSCAD corre de verdad.
func TestBackgroundProjectWatcherRendersAndLinks(t *testing.T) {
	f := newProjFixture(t, 2)
	bg, front := f.ws[0], f.ws[1]

	svc := f.a.checkpoints()
	svc.runAsync = func(fn func()) { fn() }
	svc.activeAgent = func(ws int64) string {
		if ws == bg.ID {
			return "claude"
		}
		return ""
	}
	svc.sessions = func(int64, string) ([]agentsessions.Session, error) {
		return []agentsessions.Session{{ID: sessA}}, nil
	}
	finished := make(chan render.Result, 4)
	var busyAtStart, busyAtFinish atomic.Bool
	prevEmit := f.a.emitter
	f.a.emitter = func(name string, payload any) {
		switch ev := payload.(type) {
		case RenderStartedEvent:
			busyAtStart.Store(f.a.projects().isRendering(ev.WorkspaceID))
		case RenderFinishedEvent:
			busyAtFinish.Store(f.a.projects().isRendering(ev.WorkspaceID))
		}
		prevEmit(name, payload)
	}
	f.a.onRenderFinished(func(ws int64, res render.Result, _ bool) {
		if ws == bg.ID {
			finished <- res
		}
	})

	f.openAll(t, 0, 1) // el de fondo es el primero; el activo, el segundo
	if _, active := f.list(t); active != front.ID {
		t.Fatalf("activo %d", active)
	}

	name := "001-base.scad"
	body := checkpoint.Header("", "base") + "\ncube(10);\n"
	if err := os.WriteFile(filepath.Join(bg.Path, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case res := <-finished:
		if res.FileName != name {
			t.Errorf("se renderizó %q", res.FileName)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("el proyecto de fondo no renderizó solo")
	}
	if !busyAtStart.Load() || busyAtFinish.Load() {
		t.Errorf("el indicador de render: al empezar=%v (debía ser true), al terminar=%v (debía ser false)", busyAtStart.Load(), busyAtFinish.Load())
	}
	waitTerm(t, "el vínculo con la sesión", func() bool {
		c, err := f.a.store.GetCheckpoint(bg.ID, name)
		return err == nil && c.AgentID == "claude" && c.SessionID == sessA
	})
	var saw bool
	for _, p := range f.eventsNamed(evCheckpointsChanged) {
		if v, ok := p.(WorkspaceView); ok && v.Workspace.ID == bg.ID && len(v.Checkpoints) == 1 {
			saw = true
		}
	}
	if !saw {
		t.Error("checkpoints:changed no llegó del proyecto de fondo")
	}

	act, err := f.a.ProjectActivity()
	if err != nil || len(act) != 2 || act[0].ID != bg.ID {
		t.Fatalf("actividad: %+v, %v", act, err)
	}
	if act[0].LatestFile != name || act[0].LastRenderStatus == "" || act[0].Rendering {
		t.Errorf("actividad del de fondo: %+v", act[0])
	}
}

func TestProjectActivity(t *testing.T) {
	f := newProjFixture(t, 2)
	f.openAll(t, 0, 1)
	a, b := f.ws[0], f.ws[1]
	for _, w := range []store.Workspace{a, b} {
		for _, n := range []string{"001-uno.scad", "002-dos.scad"} {
			if err := os.WriteFile(filepath.Join(w.Path, n), []byte(checkpoint.Header("", n)+"\ncube(1);\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Estado de render solo del más nuevo cuenta: el 001 falló, pero el 002 no se
	// renderizó todavía.
	f.a.renderMu.Lock()
	f.a.results = map[int64]map[string]render.Result{
		a.ID: {"001-uno.scad": {FileName: "001-uno.scad", Status: render.StatusError}},
		b.ID: {"002-dos.scad": {FileName: "002-dos.scad", Status: render.StatusError}},
	}
	f.a.renderMu.Unlock()
	end := f.a.projects().trackRender(a.ID)

	act, err := f.a.ProjectActivity()
	if err != nil || len(act) != 2 {
		t.Fatalf("%+v, %v", act, err)
	}
	if !act[0].Rendering || act[0].LatestFile != "002-dos.scad" || act[0].LastRenderStatus != "" || act[0].RenderFailed {
		t.Errorf("a: %+v", act[0])
	}
	if act[1].Rendering || act[1].LastRenderStatus != render.StatusError || !act[1].RenderFailed {
		t.Errorf("b: %+v", act[1])
	}
	end()
	end() // idempotente: no deja el contador en negativo
	if act, _ := f.a.ProjectActivity(); act[0].Rendering {
		t.Error("terminado el render, el indicador debía apagarse")
	}
}

// ---- Terminales por proyecto y --add-dir ----

// fakeClaudeFor instala un «claude» que imprime su argv y se queda leyendo.
func fakeClaudeFor(t *testing.T, a *App) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("usa /bin/sh")
	}
	bin := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"argv:$*\"\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, _ := a.store.GetProvider("claude")
	p.Command = bin
	if err := a.store.UpdateProvider(p); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.terminals().mgr.CloseAll)
}

func TestAgentLimitIsPerProject(t *testing.T) {
	f := newProjFixture(t, 2)
	fakeClaudeFor(t, f.a)
	if err := f.a.store.SetSetting("mcpEnabled", false); err != nil {
		t.Fatal(err)
	}
	a, b := f.ws[0].ID, f.ws[1].ID
	start := func(ws int64) (string, error) { return f.a.StartAgentTerminal(ws, "claude", "new", "", 100, 30) }

	for i := 0; i < 2; i++ {
		if _, err := start(a); err != nil {
			t.Fatalf("terminal %d de A: %v", i+1, err)
		}
	}
	if _, err := start(a); err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("la 3.ª de A debía rechazarse: %v", err)
	}
	// Otro proyecto tiene su propio cupo.
	idsB := []string{}
	for i := 0; i < 2; i++ {
		id, err := start(b)
		if err != nil {
			t.Fatalf("terminal %d de B: %v", i+1, err)
		}
		idsB = append(idsB, id)
	}
	if _, err := start(b); err == nil {
		t.Error("la 3.ª de B debía rechazarse")
	}
	if list, _ := f.a.ListTerminals(); len(list) != 4 {
		t.Errorf("4 terminales en total (2 por proyecto): %+v", list)
	}
	// Cerrar una de B libera cupo en B y no toca el de A.
	if err := f.a.CloseTerminal(idsB[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := start(a); err == nil {
		t.Error("cerrar en B no libera lugar en A")
	}
	if _, err := start(b); err != nil {
		t.Errorf("B tenía lugar: %v", err)
	}
}

func TestCloseProjectStopsWatcherAndAgentTerminals(t *testing.T) {
	f := newProjFixture(t, 2)
	fakeClaudeFor(t, f.a)
	if err := f.a.store.SetSetting("mcpEnabled", false); err != nil {
		t.Fatal(err)
	}
	f.openAll(t, 0, 1)
	a, b := f.ws[0].ID, f.ws[1].ID
	for _, ws := range []int64{a, a, b} {
		if _, err := f.a.StartAgentTerminal(ws, "claude", "new", "", 100, 30); err != nil {
			t.Fatal(err)
		}
	}
	act, _ := f.a.ProjectActivity()
	if act[0].AgentsAlive != 2 || act[1].AgentsAlive != 1 {
		t.Fatalf("agentes vivos: %+v", act)
	}

	if err := f.a.CloseProject(a); err != nil {
		t.Fatal(err)
	}
	if f.a.isWatched(a) || !f.a.isWatched(b) {
		t.Errorf("watchers tras cerrar A: A=%v B=%v", f.a.isWatched(a), f.a.isWatched(b))
	}
	waitTerm(t, "que mueran los agentes de A", func() bool {
		list, _ := f.a.ListTerminals()
		return len(list) == 1 && list[0].WorkspaceID == b
	})
	// Los dos lugares de A vuelven a estar libres.
	if got := f.a.terminals().aliveFor(a); got != 0 {
		t.Errorf("agentes de A: %d", got)
	}
}

func TestBuildAddDirArgs(t *testing.T) {
	dirs := []string{"/p/uno", "/p/dos", "/p/uno", ""}
	want := []string{"--add-dir", "/p/uno", "--add-dir", "/p/dos"}
	for _, id := range []string{"claude", "codex", "antigravity"} {
		if got := buildAddDirArgs(id, dirs); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %q", id, got)
		}
	}
	for _, id := range []string{"opencode", "ollama", "gemini"} {
		if got := buildAddDirArgs(id, dirs); got != nil {
			t.Errorf("%s no tiene la opción: %q", id, got)
		}
	}
	if got := buildAddDirArgs("claude", nil); got != nil {
		t.Errorf("sin otros proyectos no hay flags: %q", got)
	}
}

// El argv exacto de cada CLI: MCP, luego las carpetas de los OTROS proyectos
// abiertos (que existan), luego el modo de arranque.
func TestAgentLaunchArgsAddDirPerCLI(t *testing.T) {
	f := newProjFixture(t, 4)
	f.openAll(t, 0, 1, 2, 3)
	me := f.ws[0]
	// El 3.º ya no tiene carpeta: no se pasa. El 4.º, cerrado, tampoco.
	if err := os.RemoveAll(f.ws[2].Path); err != nil {
		t.Fatal(err)
	}
	if err := f.a.CloseProject(f.ws[3].ID); err != nil {
		t.Fatal(err)
	}
	d1 := absDir(f.ws[1].Path)
	dirFlags := []string{"--add-dir", d1}

	join := func(parts ...[]string) []string {
		out := []string{}
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	cases := []struct {
		agent   string
		session []string
		want    []string
	}{
		// --mcp-config es variádico: lo sigue un flag (--add-dir) y --add-dir
		// termina en otro flag (-c) o en nada.
		{"claude", []string{"-c"}, join(mcpconf.ClaudeArgs("/Apps/ScadLab", absDir(me.Path)), dirFlags, []string{"-c"})},
		{"claude", nil, join(mcpconf.ClaudeArgs("/Apps/ScadLab", absDir(me.Path)), dirFlags)},
		{"claude", []string{"--resume", sessA}, join(mcpconf.ClaudeArgs("/Apps/ScadLab", absDir(me.Path)), dirFlags, []string{"--resume", sessA})},
		// En Codex el subcomando va después de todos los flags globales.
		{"codex", []string{"resume", "--last"}, join(mcpconf.CodexArgs("/Apps/ScadLab", absDir(me.Path)), dirFlags, []string{"resume", "--last"})},
		{"antigravity", []string{"--continue"}, join(dirFlags, []string{"--continue"})},
		// opencode no tiene la opción.
		{"opencode", []string{"-c"}, []string{"-c"}},
	}
	for _, c := range cases {
		got := f.a.agentLaunchArgs(c.agent, me, c.session)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %v:\n got %q\nwant %q", c.agent, c.session, got, c.want)
		}
	}

	// Con el MCP apagado, Claude arranca directo con las carpetas.
	if err := f.a.store.SetSetting("mcpEnabled", false); err != nil {
		t.Fatal(err)
	}
	if got := f.a.agentLaunchArgs("claude", me, nil); !reflect.DeepEqual(got, dirFlags) {
		t.Errorf("claude sin MCP: %q", got)
	}
	argv := launchArgv("/bin/codex", f.a.agentLaunchArgs("codex", me, []string{"resume", "--last"}))
	if !reflect.DeepEqual(argv, []string{"/bin/codex", "--add-dir", d1, "resume", "--last"}) {
		t.Errorf("argv de codex: %q", argv)
	}

	// Un solo proyecto abierto: nada que agregar.
	for _, w := range f.ws {
		_ = f.a.CloseProject(w.ID)
	}
	f.openAll(t, 0)
	if got := f.a.agentLaunchArgs("claude", me, []string{"-c"}); !reflect.DeepEqual(got, []string{"-c"}) {
		t.Errorf("solo un proyecto: %q", got)
	}
}

// De punta a punta, con un PTY real: lo que el CLI recibe en su argv.
func TestStartAgentTerminalPassesOtherOpenProjects(t *testing.T) {
	f := newProjFixture(t, 3)
	fakeClaudeFor(t, f.a)
	if err := f.a.store.SetSetting("mcpEnabled", false); err != nil {
		t.Fatal(err)
	}
	f.openAll(t, 0, 1, 2)
	id, err := f.a.StartAgentTerminal(f.ws[0].ID, "claude", "continue", "", 400, 30)
	if err != nil {
		t.Fatal(err)
	}
	want := "argv:--add-dir " + absDir(f.ws[1].Path) + " --add-dir " + absDir(f.ws[2].Path) + " -c"
	waitTerm(t, "el argv con --add-dir", func() bool { return strings.Contains(termOutput(f.a, id), want) })
	if out := termOutput(f.a, id); strings.Contains(out, absDir(f.ws[0].Path)) {
		t.Errorf("el agente no recibe su propia carpeta como extra: %s", out)
	}
}
