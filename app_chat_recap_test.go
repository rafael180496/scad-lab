package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/clichat"
	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
	"scadlab/backend/store"
)

// newPromptJob arma lo mínimo de un turno de CLI en «Agente» para probar su prompt.
func newPromptJob(cliID string) chatJob {
	return chatJob{
		agent:     true,
		workspace: store.Workspace{Name: "Maceta", Path: filepath.Join(os.TempDir(), "scadlab-prompt-test")},
		cli:       &cliJob{det: clichat.Detected{ID: cliID, Name: cliID}, mode: clichat.ModeEdit, userText: "hola"},
	}
}

// Pruebas del resumen de un turno sin texto y de la nota de la shell bloqueada
// (S17, G.3 y G.2). Ninguna lanza el `agy` real: el texto del rechazo es el que
// imprime su --print (medido en una sesión real, saneado).

const realShellDenial = "permission check failed for unsandboxed \"python3 -c \\\"print(1)\\\"\": user denied permission to run command:\npython3 -c \"print(1)\"\nDo not attempt to circumvent this denial by rephrasing the command, using alternative tools/scripts (e.g. python, sh, curl), or accessing the same target resource. Proceed without performing this action."

func TestSummarizeToolResultPicksTheUsefulFields(t *testing.T) {
	cases := []struct{ in, want string }{
		{`{"file":"001-a.scad","updated":true,"changes":2}`, "file: 001-a.scad, updated: true, changes: 2"},
		{`{"status":"ok","mesh":{"triangles":12},"fitsBuildVolume":true,"watertight":true,"triangles":12}`, "status: ok, fitsBuildVolume: true, watertight: true, triangles: 12"},
		{`{"zeta":1,"alfa":2}`, "{alfa, zeta}"},
		{"texto  plano\ncon saltos", "texto plano con saltos"},
	}
	for _, c := range cases {
		if got := summarizeToolResult(c.in); got != c.want {
			t.Errorf("summarize(%q) = %q, esperaba %q", c.in, got, c.want)
		}
	}
	if got := summarizeToolResult(strings.Repeat("x", 1000)); len([]rune(got)) > 241 {
		t.Errorf("sin tope: %d", len(got))
	}
}

func TestTurnRecapTellsWhatWasDone(t *testing.T) {
	steps := []hermes.Step{
		{Tool: "outline_checkpoint", Status: hermes.StepOK, Text: `{"file":"001-maceta.scad","lines":612}`},
		{Tool: "update_checkpoint", Status: hermes.StepOK, Args: map[string]any{"file": "001-maceta.scad"}, Text: `{"file":"001-maceta.scad","updated":true}`},
		{Tool: "render", Status: hermes.StepOK, Text: `{"status":"ok","fitsBuildVolume":true}`},
		{Tool: "render", Status: hermes.StepOK, Text: `{"status":"ok","fitsBuildVolume":true}`},
		{Tool: "preview", Status: hermes.StepError, Text: "no se pudo"},
	}
	got := turnRecap(steps, nil)
	for _, want := range []string{
		"This is what it did before finishing",
		"`outline_checkpoint` → `update_checkpoint` → `render` ×2 → `preview` ✗",
		"created or changed `001-maceta.scad`",
		"Last result (`preview`): no se pudo",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("el resumen no dice %q:\n%s", want, got)
		}
	}
	if turnRecap(nil, nil) != "" {
		t.Error("sin pasos no hay nada que contar")
	}
	if none := turnRecap([]hermes.Step{{Tool: "render", Status: hermes.StepOK, Text: "{}"}}, nil); !strings.Contains(none, "No checkpoint was created or changed") {
		t.Errorf("sin archivos tocados lo dice: %s", none)
	}
	// El resultado que otra nota ya explica no se repite.
	skip := func(st hermes.Step) bool { return st.Tool == "preview" }
	if got := turnRecap(steps, skip); strings.Contains(got, "no se pudo") || !strings.Contains(got, "Last result (`render`)") {
		t.Errorf("skipResult: %s", got)
	}
}

func TestAgyShellDenialReadsTheRealRejection(t *testing.T) {
	steps := []hermes.Step{
		{Tool: "read_checkpoint", Status: hermes.StepOK},
		{Tool: "run_command", Status: hermes.StepError, Text: realShellDenial},
	}
	cmd, ok := agyShellDenial(steps)
	if !ok || cmd != `python3 -c "print(1)"` {
		t.Errorf("comando: %q %v", cmd, ok)
	}
	if _, ok := agyShellDenial([]hermes.Step{{Tool: "run_command", Status: hermes.StepError, Text: "exit status 1"}}); ok {
		t.Error("un fallo de la shell que no es un rechazo de permisos no cuenta")
	}
	if _, ok := agyShellDenial([]hermes.Step{{Tool: "render", Status: hermes.StepError, Text: realShellDenial}}); ok {
		t.Error("solo las herramientas de shell")
	}
	if _, ok := agyShellDenial([]hermes.Step{{Tool: "run_command", Status: hermes.StepOK, Text: realShellDenial}}); ok {
		t.Error("un paso bueno no es un rechazo")
	}
}

// Un turno de Antigravity que cuenta lo que hizo, nombra el comando bloqueado y NO repite
// el rechazo crudo (que el modelo escribe con el aviso «Do not attempt to circumvent…»).
func TestCLITurnTextExplainsABlockedShellInsteadOfSayingNothing(t *testing.T) {
	a := &App{}
	j := chatJob{agent: true, cli: &cliJob{det: clichat.Detected{ID: "antigravity", Name: "Antigravity"}}}
	res := hermes.Result{Steps: []hermes.Step{
		{Tool: "outline_checkpoint", Status: hermes.StepOK, Text: `{"file":"001-maceta.scad","lines":612}`},
		{Tool: "run_command", Status: hermes.StepError, Text: realShellDenial},
	}}
	got := a.cliTurnText(j, res, false)
	for _, want := range []string{
		"finished the turn without saying anything",
		"`outline_checkpoint` → `run_command` ✗",
		"tried to run a shell command (python3 -c \"print(1)\") and blocked it",
		"Nothing was run",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no dice %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Do not attempt to circumvent") || strings.Contains(got, "No checkpoint was created. Ask again") {
		t.Errorf("no debe repetir el rechazo crudo ni la pista genérica:\n%s", got)
	}

	// Con texto, la nota va al final; en otro CLI el rechazo de «shell» no es de Antigravity.
	with := res
	with.Text = "Revisé la maceta."
	if got := a.cliTurnText(j, with, false); !strings.HasPrefix(got, "Revisé la maceta.") || !strings.Contains(got, "blocked it") {
		t.Errorf("con texto: %s", got)
	}
	jc := chatJob{agent: true, cli: &cliJob{det: clichat.Detected{ID: "claude", Name: "Claude Code"}}}
	if got := a.cliTurnText(jc, hermes.Result{Text: "Hecho.", Steps: res.Steps}, false); strings.Contains(got, "blocked it") {
		t.Errorf("solo Antigravity: %s", got)
	}
}

// El prompt de Antigravity pide no tocar la shell y el de los demás CLIs no lo dice.
func TestAntigravityPromptForbidsTheShell(t *testing.T) {
	i18n.SetLang("en")
	a := &App{}
	if p := a.cliPrompt(newPromptJob("antigravity"), false); !strings.Contains(p, "Do not use `run_command`") || !strings.Contains(p, "edit_checkpoint") {
		t.Errorf("prompt de Antigravity:\n%s", p)
	}
	if p := a.cliPrompt(newPromptJob("claude"), false); strings.Contains(p, "Do not use `run_command`") {
		t.Errorf("solo Antigravity:\n%s", p)
	}
}
