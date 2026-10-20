//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/detect"
	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
)

// Pruebas de la red de seguridad del chat (S12.c): un turno en modo «Agente» que
// termina sin checkpoint pero con el diseño en su texto lo deja guardado y
// renderizado. Los CLIs son los de mentira de app_chat_cli_test.go (el NDJSON de
// backend/clichat/testdata); el servidor de modelos es el Ollama simulado. El
// render es el REAL (OpenSCAD integrado en WebAssembly): lo que se prueba es que
// quede un .scad que renderiza, no que se haya llamado a una función.

const designHeader = `// @checkpoint parent= desc="Caja con tapa a presión para una Raspberry Pi 5"`

// scadFiles son los .scad de la raíz del workspace, por nombre.
func rootScads(t *testing.T, dir string) []string {
	t.Helper()
	got, err := filepath.Glob(filepath.Join(dir, "*.scad"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		got[i] = filepath.Base(got[i])
	}
	return got
}

// autoSteps devuelve los pasos del turno (el estado final de cada uno) en orden.
func autoSteps(ev *hermesEvents) []hermes.Step {
	final := map[string]hermes.Step{}
	var order []string
	for _, e := range stepsOf(ev) {
		if _, seen := final[e.Step.ID]; !seen {
			order = append(order, e.Step.ID)
		}
		final[e.Step.ID] = e.Step
	}
	out := make([]hermes.Step, 0, len(order))
	for _, id := range order {
		out = append(out, final[id])
	}
	return out
}

// Claude, Codex y Antigravity responden SOLO texto con un bloque .scad —el caso
// real de «una caja con tapa para un Raspberry Pi 5»—: la red lo guarda como hijo
// del checkpoint abierto, lo renderiza y lo muestra como «Guardado
// automáticamente», con el workspace en el evento para que el visor lo abra.
func TestCLISafetyNetSavesAndRendersATextOnlyAnswer(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	for _, tc := range []struct{ cli, bin, stream string }{
		{"claude", "claude", "claude-text-only.jsonl"},
		{"codex", "codex", "codex-text-only.jsonl"},
		{"antigravity", "agy", "antigravity-text-only.jsonl"},
	} {
		t.Run(tc.cli, func(t *testing.T) {
			a, wsID, dir, ev, f := cliApp(t)
			agy := withFakeAgy(t, "/x/agy")
			agy.servers["scadlab"] = "/app/ScadLab --mcp" // ya registrado: no es lo que se prueba
			f.install(tc.bin, tc.stream)
			c, err := a.NewChat(wsID, "cli:"+tc.cli, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.SendChatMessage(c.ID, "Una caja con tapa a presión para un Raspberry Pi 5", "001-base.scad", nil, SendOptions{Agent: boolp(true)}); err != nil {
				t.Fatal(err)
			}
			if d := ev.wait(t); d.Cancelled || d.MessageID == 0 {
				t.Fatalf("done: %+v", d)
			}

			files := rootScads(t, dir)
			if len(files) != 2 || files[0] != "001-base.scad" || !strings.HasPrefix(files[1], "002-caja-con-tapa-a-presion") {
				t.Fatalf("archivos: %v", files)
			}
			raw := readFile(t, filepath.Join(dir, files[1]))
			if !strings.HasPrefix(raw, `// @checkpoint parent=001-base.scad desc="Caja con tapa a presión para una Raspberry Pi 5"`) || !strings.Contains(raw, "board_x = 85;") {
				t.Errorf("el checkpoint guardado:\n%s", raw)
			}

			steps := autoSteps(ev)
			if len(steps) != 2 || steps[0].Tool != "autosave" || steps[1].Tool != "render" {
				t.Fatalf("pasos: %+v", steps)
			}
			if steps[0].Status != "ok" || !strings.Contains(steps[0].Text, files[1]) || steps[0].Args["file"] != files[1] {
				t.Errorf("paso de guardado: %+v", steps[0])
			}
			if steps[1].Status != "ok" || !strings.Contains(steps[1].Text, `"status": "ok"`) || !strings.Contains(steps[1].Text, `"triangles"`) {
				t.Errorf("el render debía andar de verdad: %+v", steps[1])
			}
			for _, e := range stepsOf(ev) {
				if e.WorkspaceID != wsID || e.ChatID != c.ID {
					t.Errorf("el evento de paso lleva el proyecto: %+v", e)
				}
			}
			// Lo guardado: la respuesta con sus dos pasos, y el checkpoint enlazado al chat.
			view, _ := a.GetChat(c.ID)
			parts := hermes.ParseParts(view.Messages[1].Parts)
			if len(parts.Steps) != 2 || !strings.Contains(view.Messages[1].Content, "board_x = 85;") {
				t.Errorf("mensaje guardado: %+v", view.Messages[1])
			}
			if _, err := a.RescanWorkspace(wsID); err != nil {
				t.Fatal(err)
			}
			cps, _ := a.store.ListCheckpoints(wsID)
			for _, cp := range cps {
				if cp.FileName == files[1] && cp.ChatID != c.ID {
					t.Errorf("el checkpoint se vincula con el chat: %d", cp.ChatID)
				}
			}
		})
	}
}

// En un workspace sin ningún checkpoint el bloque es la RAÍZ (`parent=` vacío).
func TestCLISafetyNetCreatesTheRootInAnEmptyWorkspace(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	f := isolateCLIs(t)
	a := newTestApp(t)
	ev := captureHermesEvents(a)
	w, err := a.CreateWorkspace("Vacío", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	f.install("claude", "claude-text-only.jsonl")
	c, _ := a.NewChat(w.ID, "cli:claude", "")
	if _, err := a.SendChatMessage(c.ID, "una caja", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	files := rootScads(t, w.Path)
	if len(files) != 1 || !strings.HasPrefix(files[0], "001-caja-con-tapa") {
		t.Fatalf("archivos: %v", files)
	}
	if raw := readFile(t, filepath.Join(w.Path, files[0])); !strings.HasPrefix(raw, `// @checkpoint parent= desc="Caja con tapa`) {
		t.Errorf("la raíz no lleva padre:\n%s", raw)
	}
	if steps := autoSteps(ev); len(steps) != 2 || steps[1].Status != "ok" {
		t.Errorf("pasos: %+v", steps)
	}
}

// «Solo consulta» NO guarda nada solo: ahí el bloque se ofrece con un botón.
func TestCLISafetyNetIsOffInQueryOnlyMode(t *testing.T) {
	a, wsID, dir, ev, f := cliApp(t)
	f.install("claude", "claude-text-only.jsonl")
	c, _ := a.NewChat(wsID, "cli:claude", "")
	if _, err := a.SendChatMessage(c.ID, "una caja", "", nil, SendOptions{Agent: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if files := rootScads(t, dir); len(files) != 1 {
		t.Errorf("no se guarda nada solo: %v", files)
	}
	if steps := autoSteps(ev); len(steps) != 0 {
		t.Errorf("pasos: %+v", steps)
	}
}

// Un CLI que SÍ usa create_checkpoint: la red no duplica, aunque el texto repita
// el código. Y uno que escribió un .scad por su cuenta (Claude con `Write`,
// Codex editando el workspace) tampoco se duplica.
func TestCLISafetyNetDoesNotDuplicate(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	t.Run("create_checkpoint", func(t *testing.T) {
		a, wsID, dir, ev, f := cliApp(t)
		f.install("claude", "claude-create-checkpoint.jsonl")
		c, _ := a.NewChat(wsID, "cli:claude", "")
		if _, err := a.SendChatMessage(c.ID, "una caja", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
			t.Fatal(err)
		}
		ev.wait(t)
		steps := autoSteps(ev)
		if len(steps) != 1 || steps[0].Tool != "create_checkpoint" {
			t.Errorf("solo el paso del CLI: %+v", steps)
		}
		if files := rootScads(t, dir); len(files) != 1 {
			t.Errorf("el CLI falso no escribe archivos y la red no debe hacerlo: %v", files)
		}
	})
	t.Run("el CLI escribió el archivo", func(t *testing.T) {
		a, wsID, dir, ev, f := cliApp(t)
		f.install("claude", "claude-text-only.jsonl")
		f.put("write.claude", "// @checkpoint parent=001-base.scad desc=\"a mano\"\ncolor(\"red\") cube(5);\n")
		c, _ := a.NewChat(wsID, "cli:claude", "")
		if _, err := a.SendChatMessage(c.ID, "una caja", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
			t.Fatal(err)
		}
		ev.wait(t)
		if files := rootScads(t, dir); len(files) != 2 || files[1] != "002-escrito-por-el-cli.scad" {
			t.Errorf("archivos: %v", files)
		}
		if steps := autoSteps(ev); len(steps) != 0 {
			t.Errorf("sin pasos de la red: %+v", steps)
		}
	})
}

// Antigravity con un run_command que falla y ningún texto —la prueba real del
// usuario—: la tarjeta muestra el detalle del error, no «Sin resultado», y el
// turno NO queda vacío: termina con un mensaje que lo explica.
func TestAntigravityFailedToolShowsDetailAndAnExplanation(t *testing.T) {
	a, wsID, dir, ev, f := cliApp(t)
	agy := withFakeAgy(t, "/x/agy")
	agy.servers["scadlab"] = "/app/ScadLab --mcp"
	f.install("agy", "antigravity-error.jsonl")
	c, _ := a.NewChat(wsID, "cli:antigravity", "")
	if _, err := a.SendChatMessage(c.ID, "una caja", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	d := ev.wait(t)
	if d.MessageID == 0 {
		t.Fatal("el turno vacío debía guardar su explicación")
	}
	steps := autoSteps(ev)
	if len(steps) != 1 || steps[0].Tool != "run_command" || steps[0].Status != "error" || !strings.Contains(steps[0].Text, "command denied: run_command") {
		t.Fatalf("el paso debe llevar el detalle real: %+v", steps)
	}
	view, _ := a.GetChat(c.ID)
	msg := view.Messages[1].Content
	for _, want := range []string{"finished the turn without saying anything", "run_command", "command denied", "No checkpoint was created"} {
		if !strings.Contains(msg, want) {
			t.Errorf("la explicación no dice %q:\n%s", want, msg)
		}
	}
	if files := rootScads(t, dir); len(files) != 1 {
		t.Errorf("archivos: %v", files)
	}
}

// Elegir Antigravity sin ScadLab registrado: se registra solo, con el argv exacto
// (sin --workspace) ANTES de lanzarlo, y el aviso llega a la interfaz.
func TestAntigravityTurnRegistersTheMCPFirst(t *testing.T) {
	a, wsID, _, _, f := cliApp(t)
	agy := withFakeAgy(t, "/x/agy")
	events := captureAgyEvents(a)
	ev := captureHermesEvents(a)
	// captureHermesEvents reemplaza el emisor: se vuelve a poner el de los avisos de agy.
	prev := a.emitter
	a.emitter = func(name string, payload any) {
		if e, ok := payload.(AntigravityMCPEvent); ok {
			*events = append(*events, e)
		}
		prev(name, payload)
	}
	f.install("agy", "antigravity-stream.jsonl")
	c, _ := a.NewChat(wsID, "cli:antigravity", "")
	if _, err := a.SendChatMessage(c.ID, "revisa la caja", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if got := agy.servers["scadlab"]; got != "/app/ScadLab --mcp" {
		t.Fatalf("la entrada global: %q (llamadas %q)", got, agy.calls)
	}
	var add []string
	for _, call := range agy.calls {
		if call[2] == "add" {
			add = call
		}
	}
	if strings.Join(add, " ") != "/x/agy mcp add -- scadlab /app/ScadLab --mcp" {
		t.Errorf("add argv: %q", add)
	}
	if len(*events) != 1 || !(*events)[0].Added {
		t.Errorf("aviso: %+v", *events)
	}
	if len(f.calls("agy")) != 1 {
		t.Errorf("el CLI se lanzó una vez: %v", f.calls("agy"))
	}
}

// Si agy no deja registrar el servidor: el prompt le dice al CLI que no tiene las
// herramientas y que entregue UN bloque, y un turno sin nada termina explicando
// que sin el MCP no se pueden crear checkpoints. El error de agy llega entero.
func TestAntigravityWithoutTheMCPExplainsItself(t *testing.T) {
	a, wsID, dir, ev, f := cliApp(t)
	agy := withFakeAgy(t, "/x/agy")
	agy.fail["add"], agy.out["add"] = os.ErrPermission, "Error: ~/.gemini/config is read-only"
	f.install("agy", "antigravity-error.jsonl")
	c, _ := a.NewChat(wsID, "cli:antigravity", "")
	if _, err := a.SendChatMessage(c.ID, "una caja", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	prompt := f.calls("agy")[0][1]
	if !strings.Contains(prompt, "NOT connected") || !strings.Contains(prompt, "ONE ```openscad block") {
		t.Errorf("el prompt avisa que no hay herramientas: %q", prompt)
	}
	view, _ := a.GetChat(c.ID)
	msg := view.Messages[1].Content
	if !strings.Contains(msg, "read-only") || !strings.Contains(msg, "cannot create checkpoints") {
		t.Errorf("la explicación: %q", msg)
	}
	if files := rootScads(t, dir); len(files) != 1 {
		t.Errorf("archivos: %v", files)
	}
}

// El prompt de turno: en Agente ordena el flujo create_checkpoint → render →
// preview → corregir en uno nuevo y, en un workspace vacío, crear el primero sin
// preguntar; en Solo consulta pide UN bloque completo y ya no dice «no crees»
// donde no corresponde.
func TestCLIPromptPerMode(t *testing.T) {
	a, wsID, _, ev, f := cliApp(t)
	f.install("claude", "claude-text-only.jsonl")
	i18n.SetLang("en")
	empty, _ := a.CreateWorkspace("Vacío", "bambu-a1-mini")
	c1, _ := a.NewChat(empty.ID, "cli:claude", "")
	if _, err := a.SendChatMessage(c1.ID, "hola", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	c2, _ := a.NewChat(wsID, "cli:claude", "")
	if _, err := a.SendChatMessage(c2.ID, "hola", "", nil, SendOptions{Agent: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	calls := f.calls("claude")
	agent, ask := calls[0][1], calls[1][1]
	for _, want := range []string{"“Agent” mode", "create_checkpoint", "render", "preview", "update_checkpoint", "right away, without asking anything"} {
		if !strings.Contains(agent, want) {
			t.Errorf("Agente no dice %q:\n%s", want, agent)
		}
	}
	if strings.Contains(agent, "do not create checkpoints") || strings.Contains(agent, "Query only") {
		t.Errorf("en Agente no se prohíbe crear:\n%s", agent)
	}
	for _, want := range []string{"“Query only” mode", "ONE ```openscad block", "COMPLETE code"} {
		if !strings.Contains(ask, want) {
			t.Errorf("Solo consulta no dice %q:\n%s", want, ask)
		}
	}
	if strings.Contains(ask, "right away, without asking") {
		t.Errorf("Solo consulta no manda crear:\n%s", ask)
	}
}
