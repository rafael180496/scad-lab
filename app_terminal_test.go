package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"scadlab/backend/agentsessions"
	"scadlab/backend/render"
	"scadlab/backend/store"
)

// fakeTerms es un servicio de terminal sin PTY: las terminales «vivas» son las
// de alive y lo que se escribe queda en writes.
type fakeTerms struct {
	svc    *terminalService
	mu     sync.Mutex
	writes []string // «id:texto»
	alive  map[string]bool
	auto   bool
}

func newFakeTerms(t *testing.T) *fakeTerms {
	t.Helper()
	f := &fakeTerms{alive: map[string]bool{}, auto: true}
	f.svc = &terminalService{
		meta:         map[string]*termMeta{},
		nudged:       map[string]bool{},
		streak:       map[int64]int{},
		maxTerminals: maxAgentTerminals,
		runAsync:     func(fn func()) { fn() },
		lang:         func() string { return "es" },
		autoIterate:  func() bool { return f.auto },
		alive:        func(id string) bool { return f.alive[id] },
		write: func(id, data string) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.writes = append(f.writes, id+":"+data)
			return nil
		},
	}
	return f
}

func (f *fakeTerms) add(id string, ws int64, input time.Time) {
	f.alive[id] = true
	f.svc.meta[id] = &termMeta{id: id, workspaceID: ws, agentID: "claude", started: input, lastInput: input}
}

func failed(file string, msgs ...string) render.Result {
	r := render.Result{FileName: file, Status: render.StatusError}
	for i, m := range msgs {
		r.Diagnostics = append(r.Diagnostics, render.Diagnostic{Severity: render.SeverityError, Line: 10 + i, Message: m})
	}
	r.Diagnostics = append(r.Diagnostics, render.Diagnostic{Severity: render.SeverityWarning, Line: 3, Message: "aviso"})
	return r
}

func TestAutoIterateNudgesOncePerFailure(t *testing.T) {
	f := newFakeTerms(t)
	f.add("term-1", 1, time.Now())

	res := failed("004-usb-c.scad", "Parser error: syntax error")
	f.svc.onRender(1, res, true)
	if len(f.writes) != 2 {
		t.Fatalf("se esperaban 2 escrituras (texto y Enter), hubo %v", f.writes)
	}
	text := strings.TrimPrefix(f.writes[0], "term-1:")
	for _, want := range []string{"004-usb-c.scad", "1 error", "RENDER_ERRORS.md", "NUEVO"} {
		if !strings.Contains(text, want) {
			t.Errorf("el aviso no menciona %q: %s", want, text)
		}
	}
	if strings.ContainsAny(text, "\r\n") {
		t.Errorf("el texto no puede traer saltos: %q", text)
	}
	if f.writes[1] != "term-1:\r" {
		t.Errorf("el Enter va aparte: %q", f.writes[1])
	}

	// El mismo fallo otra vez (el agente volvió a fallar con el mismo
	// archivo, o se re-renderizó): no se vuelve a avisar.
	f.svc.onRender(1, res, true)
	if len(f.writes) != 2 {
		t.Errorf("avisó dos veces por el mismo fallo: %v", f.writes)
	}
	// Otro error en el mismo archivo es otro fallo.
	f.svc.onRender(1, failed("004-usb-c.scad", "otro error", "y otro"), true)
	if len(f.writes) != 4 || !strings.Contains(f.writes[2], "2 errores") {
		t.Errorf("un fallo distinto debía avisar: %v", f.writes)
	}
	// Otro archivo, mismos errores: también.
	f.svc.onRender(1, failed("005-fix.scad", "Parser error: syntax error"), true)
	if len(f.writes) != 6 || !strings.Contains(f.writes[4], "005-fix.scad") {
		t.Errorf("otro archivo debía avisar: %v", f.writes)
	}
}

func TestAutoIterateIgnores(t *testing.T) {
	f := newFakeTerms(t)
	f.add("term-1", 1, time.Now())
	res := failed("004.scad", "e")

	f.svc.onRender(1, res, false) // no es el checkpoint más nuevo
	for _, st := range []string{render.StatusOK, render.StatusCancelled, render.StatusMissing} {
		r := res
		r.Status = st
		f.svc.onRender(1, r, true)
	}
	f.auto = false // apagada en la barra de la terminal
	f.svc.onRender(1, res, true)
	f.auto = true
	f.svc.onRender(2, res, true) // otro workspace, sin terminal
	if len(f.writes) != 0 {
		t.Fatalf("no debía escribir nada: %v", f.writes)
	}

	// Sin terminal no se «gasta» el aviso: al abrir una, el próximo fallo sí.
	f.alive["term-1"] = false
	f.svc.onRender(1, res, true)
	f.alive["term-1"] = true
	f.svc.onRender(1, res, true)
	if len(f.writes) != 2 {
		t.Errorf("tras abrir la terminal debía avisar: %v", f.writes)
	}
}

func TestAutoIterateChoosesTheActiveTerminalOfThatWorkspace(t *testing.T) {
	f := newFakeTerms(t)
	now := time.Now()
	f.add("a", 1, now.Add(-time.Minute))
	f.add("b", 1, now) // la que el usuario tocó último
	f.add("c", 2, now.Add(time.Hour))

	f.svc.onRender(1, failed("x.scad", "e"), true)
	if len(f.writes) == 0 || !strings.HasPrefix(f.writes[0], "b:") {
		t.Fatalf("debía ir a «b»: %v", f.writes)
	}
	f.writes = nil
	f.svc.onRender(2, failed("y.scad", "e"), true)
	if len(f.writes) == 0 || !strings.HasPrefix(f.writes[0], "c:") {
		t.Fatalf("debía ir a «c»: %v", f.writes)
	}
}

func TestAutoIterateStopsAfterAStreakAndResetsOnSuccess(t *testing.T) {
	f := newFakeTerms(t)
	f.add("term-1", 1, time.Now())
	nudges := func() int { return len(f.writes) / 2 }

	for i := 0; i < maxNudgeStreak+3; i++ {
		f.svc.onRender(1, failed("f.scad", "error "+string(rune('a'+i))), true)
	}
	if nudges() != maxNudgeStreak {
		t.Fatalf("avisos = %d, tope %d", nudges(), maxNudgeStreak)
	}
	ok := render.Result{FileName: "g.scad", Status: render.StatusOK}
	f.svc.onRender(1, ok, true)
	f.svc.onRender(1, failed("h.scad", "nuevo"), true)
	if nudges() != maxNudgeStreak+1 {
		t.Errorf("un render correcto debía reiniciar la cuenta: %d avisos", nudges())
	}
}

func TestLaunchArgv(t *testing.T) {
	got := launchArgv("/usr/local/bin/claude", []string{"--resume", "x"})
	if strings.Join(got, " ") != "/usr/local/bin/claude --resume x" {
		t.Errorf("%v", got)
	}
	ps := launchArgv(`C:\npm\claude.ps1`, []string{"-c"})
	if len(ps) < 7 || ps[len(ps)-3] != "-File" || ps[len(ps)-2] != `C:\npm\claude.ps1` || ps[len(ps)-1] != "-c" {
		t.Errorf("un .ps1 va por powershell -File: %v", ps)
	}
}

// ── Bindings con un PTY real y un «agente» de mentira ───────────────────────

func openTestApp(t *testing.T) (*App, store.Workspace) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("usa /bin/sh")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dir := t.TempDir()
	id, err := st.InsertWorkspace(store.Workspace{Name: "caja", Path: dir, PrinterID: "bambu-a1-mini"})
	if err != nil {
		t.Fatal(err)
	}
	// Un «claude» que solo hace eco de lo que recibe: sirve para ver lo que la
	// auto-iteración le escribe, tal como lo vería un CLI de verdad.
	bin := filepath.Join(t.TempDir(), "fake-claude")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"argv:$*\"\nexec cat\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	p, _ := st.GetProvider("claude")
	p.Command = bin
	if err := st.UpdateProvider(p); err != nil {
		t.Fatal(err)
	}
	a := &App{store: st}
	t.Cleanup(a.terminals().mgr.CloseAll)
	w, _ := st.GetWorkspace(id)
	return a, w
}

func waitTerm(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no se cumplió a tiempo: %s", what)
}

func termOutput(a *App, id string) string {
	snap, err := a.ReadTerminalBuffer(id)
	if err != nil {
		return ""
	}
	b, _ := base64.StdEncoding.DecodeString(snap.Data)
	return string(b)
}

func TestStartAgentTerminalLimitsAndLists(t *testing.T) {
	a, w := openTestApp(t)
	// Este test mira límites y argv de reanudar: sin los flags del MCP, que
	// tienen su propio test (TestMCPLaunchArgsForCodexAndClaude).
	if err := a.store.SetSetting("mcpEnabled", false); err != nil {
		t.Fatal(err)
	}

	id1, err := a.StartAgentTerminal(w.ID, "claude", "new", "", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	waitTerm(t, "el arranque del agente", func() bool { return strings.Contains(termOutput(a, id1), "argv:") })

	id2, err := a.StartAgentTerminal(w.ID, "claude", "continue", "", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	waitTerm(t, "-c", func() bool { return strings.Contains(termOutput(a, id2), "argv:-c") })

	if _, err := a.StartAgentTerminal(w.ID, "claude", "new", "", 100, 30); err == nil || !strings.Contains(err.Error(), "2") {
		t.Fatalf("la tercera terminal debía rechazarse con un error claro: %v", err)
	}
	list, _ := a.ListTerminals()
	if len(list) != 2 || list[0].ID != id1 || list[0].AgentID != "claude" || list[0].WorkspaceID != w.ID || list[0].Cols != 100 {
		t.Fatalf("ListTerminals: %+v", list)
	}

	// Cerrar libera el lugar enseguida.
	if err := a.CloseTerminal(id1); err != nil {
		t.Fatal(err)
	}
	id3, err := a.StartAgentTerminal(w.ID, "claude", "resume", "aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa", 100, 30)
	if err != nil {
		t.Fatalf("tras cerrar una debía abrir otra: %v", err)
	}
	waitTerm(t, "--resume", func() bool {
		return strings.Contains(termOutput(a, id3), "argv:--resume aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa")
	})
}

func TestStartAgentTerminalRejects(t *testing.T) {
	a, w := openTestApp(t)
	cases := []struct {
		name                 string
		ws                   int64
		agent, mode, session string
	}{
		{"id con opción", w.ID, "claude", "resume", "--dangerously-skip-permissions"},
		{"id con inyección", w.ID, "claude", "resume", "x; rm -rf ~"},
		{"resume sin id", w.ID, "claude", "resume", ""},
		{"modo inválido", w.ID, "claude", "otro", ""},
		{"agente desconocido", w.ID, "nope", "new", ""},
		{"no es un CLI", w.ID, "ollama", "new", ""},
		{"workspace inexistente", 999, "claude", "new", ""},
		{"Antigravity no retoma por id", w.ID, "antigravity", "resume", "abc"},
	}
	for _, c := range cases {
		if id, err := a.StartAgentTerminal(c.ws, c.agent, c.mode, c.session, 80, 24); err == nil {
			t.Errorf("%s: debía fallar (abrió %s)", c.name, id)
		}
	}
	if list, _ := a.ListTerminals(); len(list) != 0 {
		t.Errorf("un rechazo no debe dejar terminales: %+v", list)
	}

	// Deshabilitado en Configuración → IA.
	p, _ := a.store.GetProvider("claude")
	p.Enabled = false
	_ = a.store.UpdateProvider(p)
	if _, err := a.StartAgentTerminal(w.ID, "claude", "new", "", 80, 24); err == nil {
		t.Error("un agente deshabilitado no debe abrirse")
	}
	// Ejecutable inexistente.
	p.Enabled, p.Command = true, filepath.Join(t.TempDir(), "no-existe")
	_ = a.store.UpdateProvider(p)
	if _, err := a.StartAgentTerminal(w.ID, "claude", "new", "", 80, 24); err == nil {
		t.Error("sin ejecutable debía fallar")
	}
}

// De punta a punta: el render falla, la auto-iteración escribe el aviso en la
// terminal del agente (un PTY real) y el «agente» lo recibe.
func TestAutoIterateWritesIntoTheRealTerminal(t *testing.T) {
	a, w := openTestApp(t)
	id, err := a.StartAgentTerminal(w.ID, "claude", "new", "", 200, 30)
	if err != nil {
		t.Fatal(err)
	}
	waitTerm(t, "el arranque", func() bool { return strings.Contains(termOutput(a, id), "argv:") })

	// Se ejerce por la vía real: el listener que registró terminals().
	a.notifyRenderFinished(w.ID, failed("007-lid.scad", "Parser error: syntax error"), true)
	waitTerm(t, "el aviso en el PTY", func() bool {
		out := termOutput(a, id)
		return strings.Contains(out, "007-lid.scad") && strings.Contains(out, "RENDER_ERRORS.md")
	})
	// El Enter llegó: el eco de cat repite la línea al recibir el salto.
	waitTerm(t, "el Enter", func() bool {
		return strings.Count(termOutput(a, id), "RENDER_ERRORS.md") >= 2
	})

	// Apagada la auto-iteración, no escribe.
	if err := a.SetAutoIterate(false); err != nil {
		t.Fatal(err)
	}
	before := len(termOutput(a, id))
	a.notifyRenderFinished(w.ID, failed("008-otro.scad", "otro error"), true)
	time.Sleep(600 * time.Millisecond)
	if after := len(termOutput(a, id)); after != before {
		t.Errorf("con la auto-iteración apagada la terminal recibió %d bytes", after-before)
	}
}

func TestListAgentSessionsAndChoices(t *testing.T) {
	a, w := openTestApp(t)
	got, err := a.ListAgentSessions(w.ID, "antigravity")
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("Antigravity: %v, %v", got, err)
	}
	if _, err := a.ListAgentSessions(w.ID, "ollama"); err == nil {
		t.Error("ollama no es un CLI")
	}
	if _, err := a.ListAgentSessions(999, "claude"); err == nil {
		t.Error("workspace inexistente")
	}
	if !agentsessions.CanList("claude") {
		t.Fatal("claude lista sesiones")
	}
	choices, err := a.ListAgentChoices()
	if err != nil || len(choices) != 4 {
		t.Fatalf("elecciones: %+v, %v", choices, err)
	}
	for _, c := range choices {
		if c.ID == "claude" && (!c.Found || !c.Enabled || !c.CanList) {
			t.Errorf("claude: %+v", c)
		}
		if c.ID == "antigravity" && c.CanList {
			t.Errorf("antigravity no lista sesiones: %+v", c)
		}
	}
}
