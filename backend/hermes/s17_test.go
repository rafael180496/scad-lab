package hermes

import (
	"context"
	"strings"
	"testing"

	"scadlab/backend/llm"
	"scadlab/backend/mcp"
)

// contextBudget es proporcional a la ventana: más ventana, más presupuesto, con el
// tope fijo de siempre como techo; sin ventana (0) rigen los topes fijos.
func TestContextBudgetIsProportionalToTheWindow(t *testing.T) {
	small, smallResult := contextBudget(8192, 1500)
	mid, midResult := contextBudget(32768, 1500)
	huge, hugeResult := contextBudget(1_000_000, 1500)
	none, noneResult := contextBudget(0, 0)
	if !(small < mid && mid <= huge) {
		t.Errorf("el presupuesto debe crecer con la ventana: %d %d %d", small, mid, huge)
	}
	if huge != contextBudgetChars || none != contextBudgetChars || noneResult != maxToolResultRunes || hugeResult > maxToolResultRunes {
		t.Errorf("topes fijos: %d %d %d %d", huge, none, noneResult, hugeResult)
	}
	// 8k: lo que queda tras las herramientas y la reserva, en caracteres (~4 por token).
	if want := (8192 - 1500 - 1365) * 4; small < want-8 || small > want+8 {
		t.Errorf("presupuesto de 8k = %d, esperaba ~%d", small, want)
	}
	if smallResult < minToolResultRunes || midResult < smallResult {
		t.Errorf("tope por resultado: %d %d", smallResult, midResult)
	}
	// Un caso absurdo (herramientas que ocupan toda la ventana) no deja presupuesto cero.
	if tight, _ := contextBudget(4096, 6000); tight < 4096 {
		t.Errorf("piso del presupuesto: %d", tight)
	}
}

// Una lectura repetida que no cambió deja la vieja como nota con su versión, y la
// reciente completa; una lectura que sí cambió conserva las dos.
func TestDedupeRepeatedReads(t *testing.T) {
	r := &runner{}
	push := func(file, text string) {
		r.msgs = append(r.msgs, llm.Message{Role: llm.RoleTool, Content: text})
		r.dedupeRead(llm.ToolCall{Name: "read_checkpoint", Arguments: map[string]any{"file": file}}, text)
	}
	push("001-a.scad", "cube(10);")
	push("001-a.scad", "cube(10);") // igual: la primera pasa a nota
	push("001-a.scad", "cube(20);") // cambió: no se toca nada
	push("002-b.scad", "cube(10);") // otro archivo con el mismo texto: no es repetición
	if !strings.HasPrefix(r.msgs[0].Content, "[") || !strings.Contains(r.msgs[0].Content, "001-a.scad") || !strings.Contains(r.msgs[0].Content, "version") {
		t.Errorf("la lectura vieja debía quedar como nota con archivo y versión: %q", r.msgs[0].Content)
	}
	for i, want := range map[int]string{1: "cube(10);", 2: "cube(20);", 3: "cube(10);"} {
		if r.msgs[i].Content != want {
			t.Errorf("msgs[%d] = %q, esperaba %q", i, r.msgs[i].Content, want)
		}
	}
	// Una herramienta que escribe nunca se deduplica: repetir una edición es otra edición.
	r2 := &runner{}
	for i := 0; i < 2; i++ {
		r2.msgs = append(r2.msgs, llm.Message{Role: llm.RoleTool, Content: `{"updated":true}`})
		r2.dedupeRead(llm.ToolCall{Name: "update_checkpoint", Arguments: map[string]any{"file": "a.scad"}}, `{"updated":true}`)
	}
	if r2.msgs[0].Content != `{"updated":true}` {
		t.Errorf("una escritura no se reemplaza: %q", r2.msgs[0].Content)
	}
}

// El presupuesto se aplica por pasadas: primero los resultados viejos, luego los
// argumentos largos de las llamadas viejas, y lo último siempre queda.
func TestCompactTightWindowShrinksOldCallsToo(t *testing.T) {
	code := strings.Repeat("cube(1); ", 400) // ~3600 caracteres
	var msgs []llm.Message
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: "s"}, llm.Message{Role: llm.RoleUser, Content: "u"})
	for i := 0; i < 6; i++ {
		msgs = append(msgs,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "x", Name: "update_checkpoint", Arguments: map[string]any{"file": "a.scad", "source": code}}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: "x", Content: `{"updated":true}`})
	}
	before := messagesChars(msgs)
	compactToolResults(msgs, before/2)
	if got := messagesChars(msgs); got > before/2 {
		t.Errorf("quedan %d caracteres, el presupuesto era %d", got, before/2)
	}
	last := msgs[len(msgs)-2]
	if last.ToolCalls[0].Arguments["source"] != code {
		t.Error("la llamada más reciente conserva su código")
	}
	if first := msgs[2]; first.ToolCalls[0].Arguments["source"] == code || first.ToolCalls[0].Arguments["file"] != "a.scad" {
		t.Errorf("la llamada vieja debía perder el texto largo y conservar el resto: %v", first.ToolCalls[0].Arguments)
	}
	// Un bloque firmado (Raw) no se toca.
	raw := []llm.Message{
		{Role: llm.RoleAssistant, Raw: "firmado", ToolCalls: []llm.ToolCall{{ID: "1", Name: "t", Arguments: map[string]any{"s": code}}}},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "2", Name: "t", Arguments: map[string]any{"s": code}}}},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "3", Name: "t", Arguments: map[string]any{"s": code}}}},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "4", Name: "t", Arguments: map[string]any{"s": code}}}},
	}
	compactToolResults(raw, 10)
	if raw[0].ToolCalls[0].Arguments["s"] != code {
		t.Error("un mensaje con Raw no se edita")
	}
}

// El turno lleva los tokens de cada vuelta a su paso y deja el contexto máximo, la
// última entrada, las vueltas y los tiempos en el uso.
func TestRunReportsRoundTokensAndTimes(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		oaStream("", []oaCall{{"c1", "render", `{"file":"001-caja.scad"}`}}, 1200, 30),
		oaStream("", []oaCall{{"c2", "measure", `{"file":"001-caja.scad"}`}}, 1500, 25),
		oaStream("Listo.", nil, 1400, 10),
	}}
	tb := newFakeTools("render", "measure")
	tb.on("render", func(context.Context, map[string]any) (mcp.ToolOutput, error) {
		return mcp.ToolOutput{Text: `{"status":"ok"}`}, nil
	})
	tb.on("measure", func(context.Context, map[string]any) (mcp.ToolOutput, error) {
		return mcp.ToolOutput{Text: strings.Repeat("m", 400)}, nil
	})
	res, err := Run(context.Background(), baseConfig(f.client(t), tb), Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	u := res.Usage
	if u.Rounds != 3 || u.PeakInput != 1500 || u.LastInput != 1400 || u.Input != 4100 || u.Output != 65 || u.InputEstimated {
		t.Errorf("uso: %+v", u)
	}
	if u.ModelMs < 0 || u.ToolMs < 0 {
		t.Errorf("tiempos: %+v", u)
	}
	if len(res.Steps) != 2 {
		t.Fatalf("pasos: %d", len(res.Steps))
	}
	s1, s2 := res.Steps[0], res.Steps[1]
	if s1.RoundInput != 1200 || s1.RoundOutput != 30 || s2.RoundInput != 1500 || s2.RoundOutput != 25 {
		t.Errorf("tokens de vuelta: %+v %+v", s1, s2)
	}
	if s2.ResultChars != 400 || s2.ResultTokens != 100 || s1.ResultChars != len(`{"status":"ok"}`) {
		t.Errorf("resultado: %d chars, %d tokens (%d)", s2.ResultChars, s2.ResultTokens, s1.ResultChars)
	}
}

// Un servidor que no informa la entrada deja una estimación, marcada como tal.
func TestRunEstimatesInputWhenServerDoesNotSayIt(t *testing.T) {
	f := &fakeOpenAI{script: []string{oaStream("Hola.", nil, 0, 0)}}
	res, err := Run(context.Background(), baseConfig(f.client(t), nil), Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Usage.InputEstimated || res.Usage.PeakInput <= 0 || res.Usage.Input != 0 {
		t.Errorf("uso: %+v", res.Usage)
	}
}

// ToolSpecs declara menos herramientas, pero el modelo puede llamar a cualquiera
// del catálogo; NumCtx viaja en el pedido.
func TestToolSpecsDeclareLessButCatalogStillRuns(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		oaStream("", []oaCall{{"c1", "measure", `{}`}}, 100, 5),
		oaStream("Hecho.", nil, 100, 5),
	}}
	tb := newFakeTools("render", "measure", "export")
	tb.on("measure", func(context.Context, map[string]any) (mcp.ToolOutput, error) {
		return mcp.ToolOutput{Text: "ok"}, nil
	})
	cfg := baseConfig(f.client(t), tb)
	cfg.ToolSpecs = tb.specs[:1] // solo `render` declarada
	res, err := Run(context.Background(), cfg, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	declared, _ := f.body(0)["tools"].([]any)
	if len(declared) != 1 {
		t.Errorf("se debía declarar 1 herramienta, hay %d", len(declared))
	}
	if len(res.Steps) != 1 || res.Steps[0].Status != StepOK {
		t.Errorf("measure (no declarada) debía ejecutarse: %+v", res.Steps)
	}
	if p, tt := cfg.Footprint(); p <= 0 || tt <= 0 {
		t.Errorf("huella: %d %d", p, tt)
	}
	cfg.NoTools = true
	if _, tt := cfg.Footprint(); tt != 0 {
		t.Errorf("sin herramientas no hay huella de herramientas: %d", tt)
	}
}

// SeedHistory resume lo que ya no cabe (sin llamar al modelo) y conserva enteros los
// últimos mensajes; el resumen nombra lo pedido y los archivos tocados.
func TestSeedHistorySummarizesWhatNoLongerFits(t *testing.T) {
	var stored []StoredMessage
	for i := 0; i < 12; i++ {
		stored = append(stored,
			StoredMessage{Role: "user", Content: "Pedido número " + string(rune('A'+i)) + ": hazle una asa más gruesa a la maceta " + strings.Repeat("x", 300)},
			StoredMessage{Role: "assistant", Content: "Listo, engrosé el asa. " + strings.Repeat("y", 300),
				Parts: `{"steps":[{"id":"1","index":1,"tool":"update_checkpoint","args":{"file":"00` + string(rune('1'+i%9)) + `-maceta.scad"},"status":"ok","text":"{\"file\":\"00` + string(rune('1'+i%9)) + `-maceta.scad\",\"updated\":true}"}]}`})
	}
	full := SeedHistory(stored, 1_000_000)
	if len(full) != 24 || strings.Contains(full[0].Content, "Automatic summary") {
		t.Fatalf("con presupuesto de sobra no se resume nada: %d mensajes", len(full))
	}
	got := SeedHistory(stored, 3000)
	if len(got) < 2 || len(got) >= 24 {
		t.Fatalf("debía quedar una cola: %d mensajes", len(got))
	}
	if got[0].Role != llm.RoleUser {
		t.Errorf("tiene que empezar por el usuario: %s", got[0].Role)
	}
	if !strings.Contains(got[0].Content, "Automatic summary") || !strings.Contains(got[0].Content, "Asked") || !strings.Contains(got[0].Content, "maceta.scad") {
		t.Errorf("el primer mensaje debía llevar el resumen con lo pedido y los archivos:\n%.600s", got[0].Content)
	}
	// Los últimos viajan enteros.
	if last := got[len(got)-1]; last.Content != stored[23].Content {
		t.Errorf("el último mensaje debía viajar entero")
	}
	// El resumen es chico: no se come el presupuesto.
	summary := got[0].Content[:strings.Index(got[0].Content, "Pedido número")]
	if len(summary) > summaryRoom(3000)+200 {
		t.Errorf("el resumen mide %d caracteres, su lugar era %d", len(summary), summaryRoom(3000))
	}
	// El resumen de toda la conversación respeta su tope y el código no entra.
	x := ExtractiveSummary([]StoredMessage{
		{Role: "user", Content: "hazme una caja"},
		{Role: "assistant", Content: "Aquí va:\n```openscad\ncube(10);\n```\nY listo."},
	}, 500)
	if strings.Contains(x, "cube(10)") || !strings.Contains(x, "[código]") || len(x) > 500 {
		t.Errorf("resumen: %q", x)
	}
	if ExtractiveSummary(nil, 500) != "" {
		t.Error("sin mensajes no hay resumen")
	}
}

// Parts guarda el uso del turno; un mensaje sin él se lee igual.
func TestPartsKeepUsage(t *testing.T) {
	raw := EncodeParts(Parts{Usage: &Usage{PeakInput: 12000, Rounds: 5, ModelMs: 41000}})
	if raw == "" {
		t.Fatal("un Parts con solo uso debe guardarse")
	}
	p := ParseParts(raw)
	if p.Usage == nil || p.Usage.PeakInput != 12000 || p.Usage.Rounds != 5 {
		t.Errorf("uso: %+v", p.Usage)
	}
	if ParseParts(`{"steps":[]}`).Usage != nil {
		t.Error("un mensaje anterior no trae uso")
	}
}
