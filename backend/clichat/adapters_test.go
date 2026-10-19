package clichat

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// feed pasa cada línea de un archivo de testdata por el adaptador.
func feed(t *testing.T, adapt adapter, file string) []Event {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", file))
	if err != nil {
		t.Fatal(err)
	}
	var out []Event
	sc := bufio.NewScanner(bytes.NewReader(raw))
	sc.Buffer(make([]byte, 0, 1<<20), 1<<24)
	for sc.Scan() {
		out = append(out, adapt(sc.Bytes())...)
	}
	return out
}

func kinds(evs []Event) []EventKind {
	out := make([]EventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func text(evs []Event) string {
	var b strings.Builder
	for _, e := range evs {
		if e.Kind == KindText {
			b.WriteString(e.Text)
		}
	}
	return b.String()
}

func find(evs []Event, kind EventKind) []Event {
	var out []Event
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestClaudeAdapterStreamToolsAndUsage(t *testing.T) {
	evs := feed(t, claudeAdapter, "claude-stream.jsonl")

	start := find(evs, KindStart)
	if len(start) != 1 || start[0].ConversationID != "9cb82bf7-284f-4617-a552-dfe908f87b7f" || start[0].Model != "claude-opus-5[1m]" {
		t.Fatalf("start: %+v", start)
	}
	if len(start[0].MCPServers) != 1 || start[0].MCPServers[0] != "scadlab (connected)" {
		t.Errorf("el init debe decir si el MCP de ScadLab quedó conectado: %v", start[0].MCPServers)
	}
	if got := text(evs); got != "Voy a renderizar y quedó bien: una caja de 40 mm." {
		t.Errorf("texto: %q", got)
	}

	tools := find(evs, KindTool)
	if len(tools) != 2 || tools[0].Tool.Name != "render" || tools[0].Tool.ID != "toolu_01" {
		t.Fatalf("herramientas (el prefijo mcp__scadlab__ se quita): %+v", tools)
	}
	if tools[0].Tool.Args["file"] != "001-caja.scad" || !strings.Contains(tools[0].Tool.Input, "001-caja.scad") {
		t.Errorf("argumentos: %+v", tools[0].Tool)
	}

	results := find(evs, KindToolResult)
	if len(results) != 2 || results[0].Tool.ID != "toolu_01" || !strings.Contains(results[0].Tool.Result, `"status":"ok"`) || results[0].Tool.IsError {
		t.Fatalf("resultados: %+v", results)
	}
	if len(results[1].Tool.Images) != 1 || !strings.HasPrefix(results[1].Tool.Images[0], "data:image/png;base64,iVBOR") || results[1].Tool.Result != "Vista iso" {
		t.Errorf("la imagen de preview llega como data URL: %+v", results[1].Tool)
	}

	done := find(evs, KindDone)
	if len(done) != 1 || done[0].Usage == nil {
		t.Fatalf("done: %+v", done)
	}
	u := done[0].Usage
	// La entrada cuenta lo escrito en caché (7775) y no lo leído de ella.
	if u.Input != 2+7775 || u.Output != 120 || u.CacheRead != 20400 || u.CostUSD != 0.088648 {
		t.Errorf("uso: %+v", u)
	}
}

func TestClaudeAdapterErrorResult(t *testing.T) {
	evs := feed(t, claudeAdapter, "claude-error.jsonl")
	errs := find(evs, KindError)
	if len(errs) != 1 || errs[0].Error != "Not logged in · Please run /login" || len(find(evs, KindDone)) != 0 {
		t.Fatalf("un result con is_error es un fallo con el mensaje del CLI: %+v", evs)
	}
}

func TestClaudeAdapterIgnoresUnknownAndGarbage(t *testing.T) {
	for _, line := range []string{``, `no es json`, `{"type":"algo_nuevo"}`, `{"type":"user","message":{"content":"eco del prompt"}}`, `{"type":"system","subtype":"hook"}`} {
		if got := claudeAdapter([]byte(line)); got != nil {
			t.Errorf("%q debía descartarse, dio %+v", line, got)
		}
	}
}

func TestCodexAdapterShellMCPAndUsage(t *testing.T) {
	evs := feed(t, codexAdapter, "codex-stream.jsonl")

	if st := find(evs, KindStart); len(st) != 1 || st[0].ConversationID != "01a00217-12d1-7492-b8df-b6a2c1f0bc86" {
		t.Fatalf("el thread_id es el id de la conversación: %+v", st)
	}
	tools := find(evs, KindTool)
	if len(tools) != 2 || tools[0].Tool.Name != "command_execution" || tools[1].Tool.Name != "measure" || tools[1].Tool.Args["file"] != "001-caja.scad" {
		t.Fatalf("herramientas: %+v", tools)
	}
	results := find(evs, KindToolResult)
	if len(results) != 2 || results[0].Tool.Result != "1 VERSION\n" || results[0].Tool.IsError || results[1].Tool.Result != `{"fitsBuildVolume":true}` {
		t.Fatalf("resultados: %+v", results)
	}
	// El mensaje de Codex llega entero al completarse, una sola vez.
	if got := text(evs); got != "Cabe en la placa." {
		t.Errorf("texto: %q", got)
	}
	u := find(evs, KindDone)[0].Usage
	// input_tokens ya incluye los cacheados: se restan para no contarlos dos veces.
	if u.Input != 26654-26112 || u.CacheRead != 26112 || u.Output != 107 || u.Thinking != 40 {
		t.Errorf("uso: %+v", u)
	}
}

func TestCodexAdapterFailedTurn(t *testing.T) {
	evs := feed(t, codexAdapter, "codex-error.jsonl")
	errs := find(evs, KindError)
	if len(errs) != 1 || errs[0].Error != "unexpected status 401 Unauthorized" {
		t.Fatalf("solo turn.failed es un fallo (un «Reconnecting…» no lo es): %+v", evs)
	}
}

func TestCodexFailedCommandIsAnErrorResult(t *testing.T) {
	got := codexAdapter([]byte(`{"type":"item.completed","item":{"id":"i","type":"command_execution","command":"x","aggregated_output":"boom","exit_code":2,"status":"failed"}}`))
	if len(got) != 1 || got[0].Kind != KindToolResult || !got[0].Tool.IsError || got[0].Tool.Result != "boom" {
		t.Fatalf("%+v", got)
	}
	got = codexAdapter([]byte(`{"type":"item.completed","item":{"id":"m","type":"mcp_tool_call","tool":"render","error":{"message":"no hay OpenSCAD"}}}`))
	if len(got) != 1 || !got[0].Tool.IsError || got[0].Tool.Result != "no hay OpenSCAD" {
		t.Fatalf("%+v", got)
	}
}

func TestAntigravityAdapterDeltasToolsAndResult(t *testing.T) {
	evs := feed(t, antigravityAdapter, "antigravity-stream.jsonl")
	if st := find(evs, KindStart); len(st) != 1 || st[0].Model != "gemini-4-argon-high" || st[0].ConversationID != "5f757d90-12f2-4cbf-bd3b-851e61070cc8" {
		t.Fatalf("start: %+v", st)
	}
	// Los trozos se reenvían tal cual: quien muestra concatena.
	if got := text(evs); got != "La caja está bien." {
		t.Errorf("texto: %q", got)
	}
	tools := find(evs, KindTool)
	// El ACTIVE se repite; el emparejamiento por ID lo resuelve quien lo muestra.
	if len(tools) != 2 || tools[0].Tool.ID != "step-2" || tools[0].Tool.Name != "view_file" || tools[0].Tool.Args["path"] != "001-caja.scad" {
		t.Fatalf("herramientas: %+v", tools)
	}
	if res := find(evs, KindToolResult); len(res) != 1 || res[0].Tool.ID != "step-2" || res[0].Tool.IsError {
		t.Fatalf("el DONE cierra el paso: %+v", res)
	}
	done := find(evs, KindDone)
	if len(done) != 1 || done[0].Usage.Input != 14915 || done[0].Usage.Total != 14995 {
		t.Fatalf("done: %+v", done)
	}
}

func TestAntigravityNonSuccessIsAnError(t *testing.T) {
	got := antigravityAdapter([]byte(`{"event":"result","result":{"conversation_id":"c","status":"CANCELLED"}}`))
	if len(got) != 1 || got[0].Kind != KindError || !strings.Contains(got[0].Error, "CANCELLED") {
		t.Fatalf("%+v", got)
	}
}

// OpenCode no cierra el turno: el cierre lo pone el finisher, con el uso de la sesión.
func TestOpencodeAdapterToolsAndNoNativeDone(t *testing.T) {
	adapt, finish, ok := adapterFor("opencode")
	if !ok || finish == nil {
		t.Fatal("OpenCode necesita adaptador con estado y cierre")
	}
	evs := feed(t, adapt, "opencode-stream.jsonl")

	if st := find(evs, KindStart); len(st) != 1 || st[0].ConversationID != "ses_ebf3d5642ffesbIf6ikLOXZQDR" {
		t.Fatalf("un solo start por turno aunque haya varios step_start: %+v", st)
	}
	tools := find(evs, KindTool)
	if len(tools) != 2 || tools[0].Tool.Name != "render" || tools[0].Tool.ID != "call-51925572" || tools[1].Tool.Name != "read" {
		t.Fatalf("herramientas (scadlab_render → render): %+v", tools)
	}
	results := find(evs, KindToolResult)
	if len(results) != 2 || results[0].Tool.Result != `{"status":"ok"}` || results[0].Tool.IsError {
		t.Fatalf("resultados: %+v", results)
	}
	if !results[1].Tool.IsError || results[1].Tool.Result != "file not found" {
		t.Errorf("una herramienta en error lleva su mensaje: %+v", results[1].Tool)
	}
	if got := text(evs); got != "Listo: renderiza bien." {
		t.Errorf("texto: %q", got)
	}
	if len(find(evs, KindDone)) != 0 {
		t.Error("el stream de OpenCode no trae el cierre")
	}
}

func TestOpencodeErrorSuppressesFinish(t *testing.T) {
	adapt, finish, _ := adapterFor("opencode")
	evs := feed(t, adapt, "opencode-error.jsonl")
	errs := find(evs, KindError)
	if len(errs) != 1 || errs[0].Error != "Model unavailable: nope/nada" {
		t.Fatalf("%+v", evs)
	}
	if got := finish(Turn{}); got != nil {
		t.Errorf("un turno fallido no se cierra como terminado: %+v", got)
	}
}

func TestOpencodeUsageSumsOnlyTheLastTurn(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "opencode-export.json"))
	if err != nil {
		t.Fatal(err)
	}
	u := opencodeUsageFromExport(raw)
	if u == nil {
		t.Fatal("sin uso")
	}
	// Dos mensajes del asistente después del último del usuario; el primero (de
	// un turno anterior) no cuenta. La entrada suma lo escrito en caché.
	if u.Input != 4160+4532+20 || u.Output != 198+110 || u.CacheRead != 4336*2 || u.Thinking != 89+17 || u.CostUSD != 0.002 || u.Model != "opencode/nemotron-3.5-super-vl-free" {
		t.Errorf("uso: %+v", u)
	}
	if opencodeUsageFromExport([]byte(`{"messages":[{"type":"user"}]}`)) != nil || opencodeUsageFromExport([]byte(`no`)) != nil {
		t.Error("sin mensajes del asistente el uso es «no se sabe», no cero")
	}
}

func TestCleanToolName(t *testing.T) {
	for in, want := range map[string]string{
		"mcp__scadlab__create_checkpoint": "create_checkpoint",
		"scadlab_render":                  "render",
		"scadlab.measure":                 "measure",
		"Read":                            "Read",
		"mcp__otro__render":               "mcp__otro__render",
		"scadlab_":                        "scadlab_",
	} {
		if got := CleanToolName(in); got != want {
			t.Errorf("CleanToolName(%q) = %q, quería %q", in, got, want)
		}
	}
}

// Un carácter con tilde partido entre dos trozos del stream no se rompe.
func TestSplitUTF8ChunksAreStitched(t *testing.T) {
	// «ó» son los bytes C3 B3; cada mitad, por separado, es texto inválido.
	line1 := []byte("{\"event\":\"step_update\",\"step_update\":{\"step_type\":\"agent_response\",\"text_delta\":\"l\xc3\"}}")
	line2 := []byte("{\"event\":\"step_update\",\"step_update\":{\"step_type\":\"agent_response\",\"text_delta\":\"\xb3gico\"}}")
	var fix utf8Stitcher
	var got strings.Builder
	for _, l := range [][]byte{line1, line2} {
		for _, ev := range antigravityAdapter(l) {
			got.WriteString(fix.Feed(ev.Text))
		}
	}
	got.WriteString(fix.Flush())
	if got.String() != "lógico" {
		t.Errorf("%q", got.String())
	}
}

func TestJSONRawStringEscapes(t *testing.T) {
	if got := jsonRawString([]byte(`"a\n\"b\" ó 😀"`)); got != "a\n\"b\" ó 😀" {
		t.Errorf("%q", got)
	}
	if jsonRawString([]byte(`123`)) != "" {
		t.Error("no es una cadena")
	}
}

// Un run_command que falla muestra el detalle real, no «Sin resultado» (S12.c): el
// error puede venir como cadena, como objeto con `message` o en un campo que no
// conocemos, y siempre hay algo que leer.
func TestAntigravityToolErrorCarriesTheRealDetail(t *testing.T) {
	adapt, _, _ := adapterFor("antigravity")
	evs := feed(t, adapt, "antigravity-error.jsonl")
	res := find(evs, KindToolResult)
	if len(res) != 1 || !res[0].Tool.IsError || res[0].Tool.ID != "step-2" || res[0].Tool.Result != "command denied: run_command is not allowed in this session" {
		t.Fatalf("resultado: %+v", res)
	}
	if tools := find(evs, KindTool); len(tools) != 1 || tools[0].Tool.Name != "run_command" || tools[0].Tool.Args["command"] != "openscad -o caja.stl caja.scad" {
		t.Errorf("la llamada: %+v", tools)
	}
	if len(find(evs, KindText)) != 0 || len(find(evs, KindDone)) != 1 {
		t.Errorf("sin texto y con cierre: %v", kinds(evs))
	}

	for name, line := range map[string]string{
		"cadena":      `{"event":"step_update","step_update":{"step_index":4,"state":"ERROR","step_type":"run_command","error":"boom"}}`,
		"output":      `{"event":"step_update","step_update":{"step_index":4,"state":"FAILED","step_type":"run_command","output":"boom"}}`,
		"campo nuevo": `{"event":"step_update","step_update":{"step_index":4,"state":"ERROR","step_type":"run_command","exit_status":"boom","duration_seconds":0.1}}`,
	} {
		got := antigravityAdapter([]byte(line))
		if len(got) != 1 || !got[0].Tool.IsError || !strings.Contains(got[0].Tool.Result, "boom") {
			t.Errorf("%s: %+v", name, got)
		}
	}
	// Sin ningún detalle: un texto que lo dice, nunca vacío.
	got := antigravityAdapter([]byte(`{"event":"step_update","step_update":{"step_index":4,"state":"ERROR","step_type":"run_command"}}`))
	if len(got) != 1 || !got[0].Tool.IsError || got[0].Tool.Result == "" {
		t.Errorf("sin detalle: %+v", got)
	}
}

// Si el CLI cierra con `result.response` y nunca mandó los trozos, esa respuesta
// sale como texto (un turno por turno: el estado se reinicia).
func TestAntigravityResponseOnlyTurnStillShowsTheAnswer(t *testing.T) {
	adapt, _, _ := adapterFor("antigravity")
	evs := feed(t, adapt, "antigravity-response-only.jsonl")
	if got := text(evs); !strings.Contains(got, "cube(10);") {
		t.Fatalf("texto: %q", got)
	}
	// Con los trozos ya enviados, la respuesta final NO se repite.
	adapt2, _, _ := adapterFor("antigravity")
	evs = feed(t, adapt2, "antigravity-stream.jsonl")
	if got := text(evs); got != "La caja está bien." {
		t.Errorf("no se duplica: %q", got)
	}
}
