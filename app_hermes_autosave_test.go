//go:build !windows

package main

import (
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/detect"
)

// Hermes con un Ollama simulado (S12.c): la red de seguridad con un modelo SIN
// tool calling —el caso real de la prueba del usuario—, el bucle de respaldo que
// devuelve los errores del render con su línea, y que un modelo CON herramientas
// reciba las herramientas y el prompt de sistema.

const testDesignBlock = "```openscad\n// @checkpoint parent= desc=\"Caja con tapa\"\ncolor(\"red\") cube([40, 40, 20]);\n```"

// brokenBlock tiene un paréntesis sin cerrar en la línea 3 del código.
const brokenBlock = "```openscad\n// @checkpoint parent= desc=\"Caja con tapa\"\ncolor(\"red\") cube([40, 40, 20);\n```"

// Un modelo sin herramientas contesta con un bloque: ScadLab lo guarda, lo
// renderiza y lo muestra como un paso; un solo pedido al modelo y sin
// herramientas declaradas.
func TestHermesNoToolsAutoSavesTheBlock(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	sim := newSim()
	sim.script = []simReply{{text: "Aquí tienes la caja:\n" + testDesignBlock}}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsPlain)
	if _, err := a.SendChatMessage(c.ID, "una caja con tapa", "001-base.scad", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if sim.requests() != 1 || sim.hasTools(0) {
		t.Fatalf("pedidos %d, herramientas %v", sim.requests(), sim.hasTools(0))
	}
	files, _ := filepath.Glob(filepath.Join(dir, "00[2-9]-*.scad"))
	if len(files) != 1 || !strings.HasSuffix(files[0], "002-caja-con-tapa.scad") {
		t.Fatalf("archivos: %v", files)
	}
	if raw := readFile(t, files[0]); !strings.HasPrefix(raw, `// @checkpoint parent=001-base.scad desc="Caja con tapa"`) {
		t.Errorf("checkpoint:\n%s", raw)
	}
	steps := autoSteps(ev)
	if len(steps) != 2 || steps[0].Tool != "autosave" || steps[1].Tool != "render" || steps[1].Status != "ok" {
		t.Fatalf("pasos: %+v", steps)
	}
	for _, e := range stepsOf(ev) {
		if e.WorkspaceID != wsID {
			t.Errorf("paso sin proyecto: %+v", e)
		}
	}
	v, _ := a.GetChat(c.ID)
	if !strings.Contains(v.Messages[1].Content, "does not support tools") || !strings.Contains(v.Messages[1].Content, "ScadLab saves and renders it") {
		t.Errorf("el aviso dice qué hará ScadLab: %q", v.Messages[1].Content)
	}
}

// Bucle de respaldo: el primer bloque no renderiza → el modelo recibe el error
// CON su línea → manda el corregido → se guarda como hijo del primero y renderiza.
func TestHermesNoToolsFeedsTheRenderErrorBack(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	sim := newSim()
	sim.script = []simReply{
		{text: "Primer intento:\n" + brokenBlock},
		{text: "Corregido:\n" + testDesignBlock},
	}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsPlain)
	if _, err := a.SendChatMessage(c.ID, "una caja", "", nil, SendOptions{Agent: boolp(true), MaxSteps: 6}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if sim.requests() != 2 {
		t.Fatalf("pedidos: %d", sim.requests())
	}
	// El segundo pedido lleva el error con su línea y el nombre del archivo guardado.
	tx := sim.text(1)
	if !strings.Contains(tx, "002-caja-con-tapa.scad") || !strings.Contains(tx, "does not render") || !strings.Contains(tx, `"line"`) {
		t.Errorf("la corrección no lleva el error con su línea:\n%.1200s", tx)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "00[2-9]-*.scad"))
	if len(files) != 2 {
		t.Fatalf("archivos: %v", files)
	}
	second := readFile(t, files[1])
	if !strings.Contains(second, "parent=002-caja-con-tapa.scad") {
		t.Errorf("la corrección cuelga del que falló:\n%s", second)
	}
	steps := autoSteps(ev)
	if len(steps) != 4 || steps[1].Status != "error" || steps[3].Status != "ok" {
		t.Fatalf("pasos: %+v", steps)
	}
	if !strings.Contains(steps[1].Text, "line") {
		t.Errorf("la tarjeta del render fallido lleva la línea: %q", steps[1].Text)
	}
}

// Un modelo CON herramientas las recibe y recibe el prompt de sistema del agente;
// si crea el checkpoint él mismo, la red no guarda un duplicado aunque repita el
// código en su respuesta.
func TestHermesWithToolsGetsToolsAndDoesNotDuplicate(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	sim := newSim()
	sim.script = []simReply{
		{calls: []simCall{{"create_checkpoint", map[string]any{"parent": "001-base.scad", "label": "caja con tapa", "source": "color(\"red\") cube([40,40,20]);\n"}}}},
		{calls: []simCall{{"render", map[string]any{"file": "002-caja-con-tapa.scad"}}}},
		{text: "Listo. El código es:\n" + testDesignBlock},
	}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "una caja", "001-base.scad", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if !sim.hasTools(0) {
		t.Error("el modelo con tool calling recibe las herramientas")
	}
	if sys := sim.text(0); !strings.Contains(sys, "Hermes") || !strings.Contains(sys, "create_checkpoint") {
		t.Errorf("prompt de sistema del agente: %.300q", sys)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "00[2-9]-*.scad"))
	if len(files) != 1 {
		t.Errorf("la red no duplica lo que el modelo ya creó: %v", files)
	}
	for _, st := range autoSteps(ev) {
		if st.Tool == "autosave" {
			t.Errorf("no debía haber guardado automático: %+v", st)
		}
	}
}

// Con herramientas disponibles pero un modelo que contesta con el código en vez de
// llamarlas, la red también lo guarda.
func TestHermesWithToolsButAnswersWithCodeIsSaved(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	sim := newSim()
	sim.script = []simReply{{text: "Aquí está:\n" + testDesignBlock}}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "una caja", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if !sim.hasTools(0) {
		t.Error("herramientas declaradas")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "00[2-9]-*.scad"))
	if len(files) != 1 {
		t.Fatalf("archivos: %v", files)
	}
	if st := autoSteps(ev); len(st) != 2 || st[0].Tool != "autosave" {
		t.Errorf("pasos: %+v", st)
	}
}

// En «Solo consulta» (agente apagado) el bloque queda en el chat: lo guarda el
// usuario con el botón.
func TestHermesQueryOnlyDoesNotAutoSave(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Aquí está:\n" + testDesignBlock}}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "una caja", "", nil, SendOptions{Agent: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if files, _ := filepath.Glob(filepath.Join(dir, "*.scad")); len(files) != 1 {
		t.Errorf("no se guarda solo: %v", files)
	}
}

// Un workspace vacío: el bloque del modelo sin herramientas es la raíz.
func TestHermesAutoSaveRootInAnEmptyWorkspace(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	sim := newSim()
	sim.script = []simReply{{text: testDesignBlock}}
	a, _, _, ev := hermesApp(t, sim)
	listModels(t, a)
	w, err := a.CreateWorkspace("Vacío", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	c, _ := a.NewChat(w.ID, "ollama", capsPlain)
	if _, err := a.SendChatMessage(c.ID, "una caja", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	files, _ := filepath.Glob(filepath.Join(w.Path, "*.scad"))
	if len(files) != 1 || filepath.Base(files[0]) != "001-caja-con-tapa.scad" {
		t.Fatalf("archivos: %v", files)
	}
	if raw := readFile(t, files[0]); !strings.HasPrefix(raw, `// @checkpoint parent= desc="Caja con tapa"`) {
		t.Errorf("raíz:\n%s", raw)
	}
}
