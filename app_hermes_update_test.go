package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/checkpoint"
	"scadlab/backend/detect"
	"scadlab/backend/hermes"
)

// Hermes (Ollama/API) con un servidor simulado y la caja de herramientas REAL: el
// usuario tiene 001-base.scad y 002-x.scad (el seleccionado) y pide «modifica 002-x.scad
// para…» (S16). El modelo guionado llama update_checkpoint y el archivo cambia en el lugar,
// con su versión anterior en el historial; con «hazme una variante» llama create_checkpoint.

// hermesUpdateApp deja el workspace con 002-x.scad y el servidor simulado listo.
func hermesUpdateApp(t *testing.T, sim *ollamaSim) (*App, int64, string, *hermesEvents, *namedEvents) {
	t.Helper()
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	a, wsID, dir, ev := hermesApp(t, sim)
	if err := os.WriteFile(filepath.Join(dir, "002-x.scad"), []byte(updOrigNoTag), 0o644); err != nil {
		t.Fatal(err)
	}
	listModels(t, a)
	return a, wsID, dir, ev, captureNamedEvents(a)
}

const (
	updOrigNoTag = "// @checkpoint parent=001-base.scad desc=\"caja con tapa\"\ncolor(\"red\") cube([20, 20, 20]);\n"
	updTallerSrc = "color(\"red\") cube([20, 20, 30]);\n"
)

// toolNames son las herramientas que el modelo recibió en el pedido i.
func toolNames(sim *ollamaSim, i int) []string {
	var out []string
	tools, _ := sim.body(i)["tools"].([]any)
	for _, t := range tools {
		fn, _ := t.(map[string]any)["function"].(map[string]any)
		if name, _ := fn["name"].(string); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// hermesSteps son los eventos de paso del turno (stepsOf vive en un archivo que no compila en Windows).
func hermesSteps(ev *hermesEvents) []ChatStepEvent {
	ev.mu.Lock()
	defer ev.mu.Unlock()
	return append([]ChatStepEvent(nil), ev.steps...)
}

// squash quita los espacios y saltos: los resultados de las herramientas van con sangría.
func squash(s string) string { return strings.Join(strings.Fields(s), "") }

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestHermesEditsTheSelectedFileInPlace(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{
		{text: "Voy a editar 002-x.scad.", calls: []simCall{{"update_checkpoint", map[string]any{"file": "002-x.scad", "source": updTallerSrc, "note": "más alto"}}}},
		{text: "Listo: 002-x.scad ahora mide 30 mm de alto."},
	}
	a, wsID, dir, ev, named := hermesUpdateApp(t, sim)
	c, err := a.NewChat(wsID, "ollama", capsAgent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendChatMessage(c.ID, "modifica 002-x.scad para que sea más alto", "002-x.scad", nil, SendOptions{Agent: boolp(true), MaxSteps: 6}); err != nil {
		t.Fatal(err)
	}
	if d := ev.wait(t); d.Cancelled {
		t.Fatalf("done: %+v", d)
	}

	// El modelo recibió la herramienta y el prompt le dice cuál es el archivo seleccionado y la regla.
	if !hasName(toolNames(sim, 0), "update_checkpoint") || !hasName(toolNames(sim, 0), "read_checkpoint") {
		t.Errorf("herramientas declaradas: %v", toolNames(sim, 0))
	}
	sys := sim.text(0)
	for _, want := range []string{"002-x.scad", "update_checkpoint", "selected in the app"} {
		if !strings.Contains(sys, want) {
			t.Errorf("al prompt de sistema le falta %q:\n%.600s", want, sys)
		}
	}

	// El archivo cambió en el lugar y no apareció un hijo.
	got := readFile(t, filepath.Join(dir, "002-x.scad"))
	if !strings.Contains(got, "cube([20, 20, 30])") || !strings.HasPrefix(got, "// @checkpoint parent=001-base.scad") {
		t.Errorf("002-x.scad cambió en el lugar y conserva su cabecera:\n%s", got)
	}
	hist, err := checkpoint.History(dir, "002-x.scad")
	if err != nil || len(hist) != 1 || hist[0].Reason != checkpoint.ReasonAI || hist[0].Note != "más alto" {
		t.Fatalf("historial: %+v %v", hist, err)
	}
	if old, _ := checkpoint.ReadVersion(dir, "002-x.scad", hist[0].ID); old != updOrigNoTag {
		t.Errorf("la versión guardada es la anterior, entera:\n%s", old)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*.scad")); len(files) != 2 {
		t.Errorf("no aparece un .scad nuevo: %v", files)
	}

	// La interfaz se entera: el paso nombra el archivo y se emite checkpoint:updated.
	var step *hermes.Step
	for _, e := range hermesSteps(ev) {
		if e.Step.Tool == "update_checkpoint" && e.Step.Status == hermes.StepOK {
			s := e.Step
			step = &s
		}
	}
	if step == nil || step.Args["file"] != "002-x.scad" || !strings.Contains(squash(step.Text), `"file":"002-x.scad"`) {
		t.Errorf("el paso enlaza 002-x.scad: %+v", hermesSteps(ev))
	}
	if u := named.updatedFiles(); len(u) != 1 || u[0] != "002-x.scad" {
		t.Errorf("checkpoint:updated: %v", u)
	}
	// El resultado de la herramienta (lo que lee el modelo) trae el lint y el render del archivo editado.
	if tx := squash(sim.text(1)); !strings.Contains(tx, `"updated":true`) || !strings.Contains(tx, `"render":`) {
		t.Errorf("el modelo recibe el resultado con el render: %.500q", tx)
	}
}

func TestHermesCreatesAVariantWhenAskedForOne(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{
		{calls: []simCall{{"create_checkpoint", map[string]any{"label": "variante alta", "parent": "002-x.scad", "source": updTallerSrc}}}},
		{text: "Listo: 003-variante-alta.scad."},
	}
	a, wsID, dir, ev, named := hermesUpdateApp(t, sim)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "hazme una variante más alta de 002-x.scad", "002-x.scad", nil, SendOptions{Agent: boolp(true), MaxSteps: 6}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)

	if _, err := os.Stat(filepath.Join(dir, "003-variante-alta.scad")); err != nil {
		t.Errorf("la variante es un checkpoint nuevo: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "002-x.scad")); got != updOrigNoTag {
		t.Errorf("002-x.scad no se toca:\n%s", got)
	}
	if hist, _ := checkpoint.History(dir, "002-x.scad"); len(hist) != 0 {
		t.Errorf("sin historial: %+v", hist)
	}
	if u := named.updatedFiles(); len(u) != 0 {
		t.Errorf("crear no es actualizar en el lugar: %v", u)
	}
}

// En modo texto («Solo consulta» de Hermes) no hay herramientas: ni update_checkpoint ni ninguna.
func TestHermesTextModeHasNoUpdateTool(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "```openscad\ncube(10);\n```"}}
	a, wsID, dir, ev, _ := hermesUpdateApp(t, sim)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "modifica 002-x.scad", "002-x.scad", nil, SendOptions{Agent: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if sim.hasTools(0) || len(toolNames(sim, 0)) != 0 {
		t.Errorf("en modo texto no se declara ninguna herramienta: %v", toolNames(sim, 0))
	}
	if got := readFile(t, filepath.Join(dir, "002-x.scad")); got != updOrigNoTag {
		t.Errorf("002-x.scad no cambia:\n%s", got)
	}
	if hist, _ := checkpoint.History(dir, "002-x.scad"); len(hist) != 0 {
		t.Errorf("sin historial: %+v", hist)
	}
}
