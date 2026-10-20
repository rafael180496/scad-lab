package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/detect"
	"scadlab/backend/hermes"
	"scadlab/backend/store"
)

// Un Ollama chico (S17): un modelo de 8 192 tokens de contexto, simulado, que cuenta lo
// que recibe en cada /api/chat (caracteres/4) y contesta 400 si pasa de la ventana. Le
// pide a ScadLab dos cambios sobre una maceta de ~600 líneas: (1) «hazla 10 mm más
// alta», que es set_parameters, y (2) un cambio de código, que es outline_checkpoint →
// find_in_checkpoint → edit_checkpoint. El modelo es un guion; lo que se prueba es que
// ScadLab le deje sitio: perfil de herramientas esencial, prompt compacto, resultados
// que no se acumulan, num_ctx igual a la ventana.
//
// La misma prueba corre el escenario «antes» (catálogo completo y el prompt de hoy, con
// el modelo leyendo el archivo entero y reescribiéndolo con update_checkpoint) sin tope,
// solo para medir cuántos tokens habría enviado: los números van a los registros (t.Log)
// y a claude/35.

const (
	smallModel   = "small-8k"
	smallWindow  = 8192
	macetaFile   = "002-maceta.scad"
	macetaHeader = "// @checkpoint parent=001-base.scad desc=\"Maceta modular con drenaje y plato\"\n"
)

// macetaSource arma una maceta de ~600 líneas: un Customizer con medidas y colores, el
// cuerpo cónico con drenajes y muchos módulos de detalle que el diseño define y casi no
// usa (como una maceta real con iconos, molduras y variantes). Solo se instancia el
// cuerpo, así que el render es barato.
func macetaSource() string {
	// La maceta de 17.1 (testdata del servidor MCP: ~600 líneas, con relieve curvado) si está; si no, la propia.
	if raw, err := os.ReadFile(filepath.Join("backend", "mcp", "testdata", "maceta.scad")); err == nil {
		return strings.Replace(string(raw), "parent= ", "parent=001-base.scad ", 1)
	}
	var b strings.Builder
	b.WriteString(macetaHeader)
	b.WriteString(`// Maceta cónica parametrizable con drenaje, plato y texto en relieve.

/* [Medidas] */
// Alto de la maceta en mm
altura = 80; // [20:1:200]
// Diámetro superior en mm
diametro = 90; // [30:1:200]
// Grosor de la pared en mm
pared = 2.4; // [1:0.2:6]
// Grosor del fondo en mm
base = 3; // [1:0.5:10]
// Diámetro de cada agujero de drenaje en mm
drain_d = 3.5; // [1:0.5:10]
// Cantidad de agujeros de drenaje
drain_n = 5; // [0:1:12]

/* [Colores] */
color_maceta = "#3A7D44";
color_texto = "#E8A33D";

/* [Hidden] */
$fn = 24;

module cuerpo() {
    difference() {
        cylinder(h = altura, d1 = diametro * 0.7, d2 = diametro);
        translate([0, 0, base]) cylinder(h = altura, d1 = diametro * 0.7 - 2 * pared, d2 = diametro - 2 * pared);
        drenajes();
    }
}

module drenajes() {
    for (i = [0 : drain_n - 1])
        rotate([0, 0, i * 360 / max(drain_n, 1)])
            translate([diametro * 0.2, 0, -1])
                cylinder(h = base + 2, d = drain_d);
}

`)
	for i := 1; i <= 52; i++ {
		fmt.Fprintf(&b, `// Detalle %[1]d: moldura decorativa que no se usa en la vista por defecto.
module detalle_%[1]d(r = %[2]d, h = %[3]d) {
    // anillo exterior
    difference() {
        cylinder(h = h, r = r);
        translate([0, 0, -0.1]) cylinder(h = h + 0.2, r = r - 1.2);
    }
    // cuatro resaltes
    for (a = [0 : 90 : 270]) rotate([0, 0, a]) translate([r, 0, 0]) cube([1.5, 1.5, h]);
}

`, i, 8+i%9, 2+i%5)
	}
	b.WriteString(`color(color_maceta) cuerpo();
`)
	return b.String()
}

// macetaSpec dice qué parámetros toca el guion sobre la maceta de prueba: la altura (de h a h+10), y el diámetro
// de los drenajes (de la línea `oldLine` a `newLine`). Depende de qué maceta se usó.
type macetaSpec struct {
	height, newHeight            int
	drainParam, oldLine, newLine string
}

func specFor(src string) macetaSpec {
	if strings.Contains(src, "diametro_drenaje = 6;") { // la de 17.1
		return macetaSpec{85, 95, "diametro_drenaje", "diametro_drenaje = 6; // [2:0.5:14]", "diametro_drenaje = 8; // [2:0.5:14]"}
	}
	return macetaSpec{80, 90, "drain_d", "drain_d = 3.5; // [1:0.5:10]", "drain_d = 5; // [1:0.5:10]"}
}

func lineOf(src, needle string) int {
	for i, l := range strings.Split(src, "\n") {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	return 0
}

// smallApp deja la App con la maceta y el servidor «ollama» simulado con el modelo chico.
func smallApp(t *testing.T, limit int) (*App, *ollamaSim, int64, string, *hermesEvents) {
	t.Helper()
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	sim := newSim()
	sim.caps[smallModel] = []string{"completion", "tools"}
	sim.ctxLen, sim.limit = smallWindow, limit
	a, wsID, dir, ev := hermesApp(t, sim)
	if err := os.WriteFile(filepath.Join(dir, macetaFile), []byte(macetaSource()), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sin ventana fijada en el servidor: la decide el modelo (8 192).
	p, err := a.store.GetModelServer("ollama")
	if err != nil {
		t.Fatal(err)
	}
	p.ContextTokens = 0
	if _, err := a.SaveModelServer(p, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListServerModels("ollama", true); err != nil {
		t.Fatal(err)
	}
	return a, sim, wsID, dir, ev
}

func sendSmall(t *testing.T, a *App, ev *hermesEvents, chatID int64, text string) {
	t.Helper()
	if _, err := a.SendChatMessage(chatID, text, macetaFile, nil, SendOptions{Agent: boolp(true), MaxSteps: 8}); err != nil {
		t.Fatal(err)
	}
	if d := ev.wait(t); d.Cancelled || d.MessageID == 0 {
		t.Fatalf("done: %+v", d)
	}
}

// El Ollama chico con las herramientas compactas: ninguna petición pasa de 8 192 tokens.
func TestSmallOllamaEditsAMacetaWithin8kTokens(t *testing.T) {
	a, sim, wsID, dir, ev := smallApp(t, smallWindow)
	src := macetaSource()
	if n := strings.Count(src, "\n"); n < 560 || n > 680 {
		t.Fatalf("la maceta de prueba debía rondar las 600 líneas: %d", n)
	}
	spec := specFor(src)
	drainLine := lineOf(src, spec.oldLine)
	if drainLine == 0 {
		t.Fatalf("la maceta de prueba no tiene la línea %q", spec.oldLine)
	}

	// (1) «hazla 10 mm más alta» → set_parameters, en el lugar.
	sim.script = []simReply{
		{calls: []simCall{{"set_parameters", map[string]any{"file": macetaFile, "values": map[string]any{"altura": spec.newHeight}}}}},
		{text: "Listo: la maceta ahora es 10 mm más alta."},
		// (2) un cambio de código: estructura → búsqueda → edición.
		{calls: []simCall{{"outline_checkpoint", map[string]any{"file": macetaFile}}}},
		{calls: []simCall{{"find_in_checkpoint", map[string]any{"file": macetaFile, "query": spec.drainParam}}}},
		{calls: []simCall{{"edit_checkpoint", map[string]any{"file": macetaFile, "edits": []any{map[string]any{"from_line": drainLine, "to_line": drainLine, "new": spec.newLine}}}}}},
		{text: "Listo: los agujeros de drenaje ahora son más grandes."},
	}
	c1, err := a.NewChat(wsID, "ollama", smallModel)
	if err != nil {
		t.Fatal(err)
	}
	sendSmall(t, a, ev, c1.ID, "hazla 10 mm más alta")
	c2, err := a.NewChat(wsID, "ollama", smallModel)
	if err != nil {
		t.Fatal(err)
	}
	sendSmall(t, a, ev, c2.ID, "cambia el diámetro de los agujeros de drenaje a 5 mm")

	// El resultado está en el archivo, en el lugar: ningún checkpoint nuevo.
	got := readFile(t, filepath.Join(dir, macetaFile))
	if !strings.Contains(got, fmt.Sprintf("altura = %d;", spec.newHeight)) || !strings.Contains(got, spec.newLine) || strings.Contains(got, spec.oldLine) {
		t.Errorf("la maceta debía quedar con la altura y el drenaje nuevos:\n%.700s", got)
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*.scad")); len(files) != 2 {
		t.Errorf("las ediciones son en el lugar (001-base + maceta): %v", files)
	}

	// Lo que importa: cuánto pesó cada petición, y que ninguna pasó de la ventana.
	if sim.over != 0 {
		t.Errorf("%d petición(es) pasaron de %d tokens (tokens por vuelta: %v)", sim.over, smallWindow, sim.tokens)
	}
	if sim.requests() != 6 {
		t.Fatalf("peticiones: %d", sim.requests())
	}
	for i, n := range sim.numCtx {
		if n != smallWindow {
			t.Errorf("la petición %d pidió num_ctx=%d: debía ser la ventana del modelo (%d)", i, n, smallWindow)
		}
	}
	for i, n := range sim.tokens {
		if n > smallWindow {
			t.Errorf("la petición %d pesó %d tokens (> %d)", i, n, smallWindow)
		}
	}
	// El perfil automático eligió las esenciales: set_parameters y outline_checkpoint se declararon, extract_module no.
	declared := toolNames(sim, 0)
	if !hasName(declared, "edit_checkpoint") || !hasName(declared, "outline_checkpoint") || hasName(declared, "extract_module") || len(declared) > 12 {
		t.Errorf("perfil esencial esperado, se declararon %d: %v", len(declared), declared)
	}
	// Los eventos de contexto avisan de la ventana y el perfil.
	ev.mu.Lock()
	ctxEvents := append([]ChatContextEvent(nil), ev.ctx...)
	steps := append([]ChatStepEvent(nil), ev.steps...)
	usage := append([]ChatUsageEvent(nil), ev.usage...)
	ev.mu.Unlock()
	if len(ctxEvents) != 2 || ctxEvents[0].NumCtx != smallWindow || ctxEvents[0].Profile != "essential" || !ctxEvents[0].Auto {
		t.Errorf("chat:context: %+v", ctxEvents)
	}
	t.Logf("DESPUÉS (8k, auto → esenciales): tokens por vuelta %v; herramientas declaradas: %d; prompt %d + herramientas %d tokens (sobre presupuesto: %v)",
		sim.tokens, len(declared), ctxEvents[0].PromptTokens, ctxEvents[0].ToolsTokens, ctxEvents[0].OverBudget)

	// El uso trae el contexto máximo y las vueltas; los pasos, el contexto de su vuelta.
	last := usage[len(usage)-1]
	if last.Rounds != 4 || last.PeakInput <= 0 || last.PeakInput > smallWindow {
		t.Errorf("uso del segundo turno: %+v", last)
	}
	for _, e := range steps {
		if e.Step.Status != hermes.StepRunning && (e.Step.RoundInput == 0 || e.Step.ResultTokens == 0) {
			t.Errorf("el paso %s debía traer su contexto y su resultado: %+v", e.Step.Tool, e.Step)
		}
	}
}

// El escenario «antes» para medir: catálogo completo, el prompt de siempre y el modelo
// leyendo el archivo entero y reescribiéndolo. Sin tope (no podría ni empezar): solo se
// registran los tokens que habría enviado y se comprueba que la ventana no alcanzaba.
func TestSmallOllamaWithTheFullProfileDoesNotFit(t *testing.T) {
	// Como antes de S17: sin ventana conocida rigen los topes fijos (32 000 caracteres por resultado, 160 000 de
	// mensajes) y el num_ctx es el de siempre (16 384), no el del modelo.
	old := windowFor
	windowFor = func(store.ModelServer, modelCaps) ctxWindow { return ctxWindow{} }
	t.Cleanup(func() { windowFor = old })
	a, sim, wsID, dir, ev := smallApp(t, 0)
	src := macetaSource()
	spec := specFor(src)
	rewritten := strings.Replace(src, spec.oldLine, spec.newLine, 1)

	sim.script = []simReply{
		{calls: []simCall{{"get_parameters", map[string]any{"file": macetaFile}}}},
		{calls: []simCall{{"set_parameters", map[string]any{"file": macetaFile, "values": map[string]any{"altura": spec.newHeight}, "variant": true}}}},
		{text: "Listo: otra versión 10 mm más alta."},
		{calls: []simCall{{"read_checkpoint", map[string]any{"file": macetaFile, "full": true}}}},
		{calls: []simCall{{"update_checkpoint", map[string]any{"file": macetaFile, "source": rewritten}}}},
		{text: "Listo: los agujeros de drenaje ahora miden 5 mm."},
	}
	c1, _ := a.NewChat(wsID, "ollama", smallModel)
	if err := a.SetChatToolProfile(c1.ID, "full"); err != nil {
		t.Fatal(err)
	}
	sendSmall(t, a, ev, c1.ID, "hazla 10 mm más alta")
	c2, _ := a.NewChat(wsID, "ollama", smallModel)
	if err := a.SetChatToolProfile(c2.ID, "full"); err != nil {
		t.Fatal(err)
	}
	sendSmall(t, a, ev, c2.ID, "cambia el diámetro de los agujeros de drenaje a 5 mm")
	_ = dir

	declared := toolNames(sim, 0)
	over := 0
	for _, n := range sim.tokens {
		if n > smallWindow {
			over++
		}
	}
	ev.mu.Lock()
	ctxEvents := append([]ChatContextEvent(nil), ev.ctx...)
	ev.mu.Unlock()
	t.Logf("ANTES (catálogo completo, prompt de siempre, archivo entero): tokens por vuelta %v; herramientas declaradas: %d; prompt %d + herramientas %d tokens; %d de %d vueltas pasan de %d",
		sim.tokens, len(declared), ctxEvents[0].PromptTokens, ctxEvents[0].ToolsTokens, over, len(sim.tokens), smallWindow)
	if ctxEvents[0].Profile != "full" || ctxEvents[0].Auto {
		t.Errorf("el perfil fijado a mano es «full»: %+v", ctxEvents[0])
	}
	// La ventana de este escenario es «desconocida» a propósito (como antes de S17); lo fijo pesa más del 60 % de los 8 192 del modelo.
	if fixed := ctxEvents[0].PromptTokens + ctxEvents[0].ToolsTokens; float64(fixed) <= 0.6*smallWindow {
		t.Errorf("con el catálogo completo el prompt y las herramientas pasan del 60 %% de 8k: %d tokens", fixed)
	}
	if over == 0 {
		t.Errorf("con el catálogo completo alguna vuelta tenía que pasar de la ventana: %v", sim.tokens)
	}
}
