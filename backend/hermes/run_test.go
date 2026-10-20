package hermes

import (
	"context"
	"encoding/base64"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scadlab/backend/llm"
	"scadlab/backend/mcp"
)

func baseConfig(client llm.Client, tools Toolset) Config {
	return Config{
		Client: client, Model: "fake-model",
		SystemTools: "SISTEMA CON HERRAMIENTAS", SystemText: "SISTEMA SOLO TEXTO",
		User:  llm.Message{Role: llm.RoleUser, Content: "haz una caja de 40 mm"},
		Tools: tools, Vision: true, MaxSteps: 12,
	}
}

// cycleTools devuelve la caja de mentira del ciclo de diseño: el primer render
// falla con una línea de error, el segundo pasa y `preview` devuelve un PNG.
func cycleTools(t *testing.T) *fakeTools {
	t.Helper()
	tb := newFakeTools("create_checkpoint", "render", "preview", "measure")
	n := 0
	tb.on("create_checkpoint", func(_ context.Context, args map[string]any) (mcp.ToolOutput, error) {
		n++
		return mcp.ToolOutput{Text: `{"file":"00` + string(rune('0'+n)) + `-caja.scad"}`}, nil
	})
	renders := 0
	tb.on("render", func(_ context.Context, args map[string]any) (mcp.ToolOutput, error) {
		renders++
		if renders == 1 {
			return mcp.ToolOutput{IsError: true, Text: `{"status":"error","diagnostics":[{"severity":"error","line":7,"message":"syntax error","code":"cube([40,40,20);"}]}`}, nil
		}
		return mcp.ToolOutput{Text: `{"status":"ok","mesh":{"size":[40,40,20]}}`}, nil
	})
	tb.on("preview", func(context.Context, map[string]any) (mcp.ToolOutput, error) {
		return mcp.ToolOutput{Text: `{"view":"iso"}`, Images: []mcp.ToolImage{{MIME: "image/png", Data: pngBytes(t)}}}, nil
	})
	return tb
}

var cycleCalls = [][]oaCall{
	{{"c1", "create_checkpoint", `{"label":"caja","source":"cube([40,40,20);"}`}},
	{{"c2", "render", `{"file":"001-caja.scad"}`}},
	{{"c3", "create_checkpoint", `{"parent":"001-caja.scad","label":"caja v2","source":"color(\"red\") cube([40,40,20]);"}`}},
	{{"c4", "render", `{"file":"002-caja-v2.scad"}`}},
	{{"c5", "preview", `{"file":"002-caja-v2.scad"}`}},
}

// El ciclo completo contra un servidor compatible con OpenAI: crear → render con
// error de línea → crear → render ok → preview (la imagen vuelve en el pedido
// siguiente) → texto final.
func TestOpenAICycleCreateRenderFixPreview(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		oaStream("Voy a crear la caja.", cycleCalls[0], 100, 20),
		oaStream("", cycleCalls[1], 150, 10),
		oaStream("", cycleCalls[2], 200, 30),
		oaStream("", cycleCalls[3], 250, 10),
		oaStream("", cycleCalls[4], 300, 10),
		oaStream("Listo: la caja mide 40 × 40 × 20 mm.", nil, 400, 25),
	}}
	tools := cycleTools(t)
	cfg := baseConfig(f.client(t), tools)
	cfg.ImageDir = filepath.Join(t.TempDir(), "chat-attachments", "7")
	cfg.Price = Price{PromptPerM: 3, CompletionPerM: 15}
	rec := &recorder{}

	res, err := Run(context.Background(), cfg, rec.hooks())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tools.names(), ","); got != "create_checkpoint,render,create_checkpoint,render,preview" {
		t.Fatalf("herramientas llamadas: %s", got)
	}
	if f.count() != 6 {
		t.Fatalf("pedidos al modelo: %d, esperaba 6", f.count())
	}

	// Los pasos: 5, numerados, el primer render en error con su línea.
	if len(res.Steps) != 5 {
		t.Fatalf("pasos: %+v", res.Steps)
	}
	wantStatus := []string{StepOK, StepError, StepOK, StepOK, StepOK}
	for i, s := range res.Steps {
		if s.Index != i+1 || s.Status != wantStatus[i] || s.ID == "" {
			t.Errorf("paso %d: %+v", i, s)
		}
	}
	if !strings.Contains(res.Steps[1].Text, `"line":7`) || !strings.HasPrefix(res.Steps[1].Text, "error:") {
		t.Errorf("el paso del render con error no trae la línea: %q", res.Steps[1].Text)
	}
	if len(res.Steps[4].Images) != 1 || !strings.HasPrefix(res.Steps[4].Images[0], "data:image/png;base64,") {
		t.Errorf("el paso de preview debe llevar su imagen: %+v", res.Steps[4].Images)
	}

	// Cada paso se anuncia dos veces con el mismo ID: running y luego el final.
	if len(rec.steps) != 10 {
		t.Fatalf("eventos de paso: %d", len(rec.steps))
	}
	for i := 0; i < 10; i += 2 {
		if rec.steps[i].Status != StepRunning || rec.steps[i+1].ID != rec.steps[i].ID || rec.steps[i+1].Status == StepRunning {
			t.Errorf("eventos %d/%d: %+v / %+v", i, i+1, rec.steps[i], rec.steps[i+1])
		}
	}
	if rec.flush < 5 {
		t.Errorf("antes de cada paso se debía vaciar el texto pendiente: %d", rec.flush)
	}

	// El error del render llegó al modelo como resultado de herramienta.
	var toolErr string
	for _, m := range f.messages(2) {
		if m["role"] == "tool" && m["tool_call_id"] == "c2" {
			toolErr, _ = m["content"].(string)
		}
	}
	if !strings.HasPrefix(toolErr, "error:") || !strings.Contains(toolErr, `"line":7`) {
		t.Errorf("el modelo no recibió el error con línea: %q", toolErr)
	}

	// La imagen del preview está en el pedido siguiente (el 6) y no antes.
	if hasImage(f.messages(4)) {
		t.Error("el pedido previo al preview no debía llevar imagen")
	}
	if !hasImage(f.messages(5)) {
		t.Error("el pedido posterior al preview debe llevar la imagen como image_url")
	}
	// Las herramientas viajan declaradas y el sistema es el del modo agente.
	if tl, _ := f.body(0)["tools"].([]any); len(tl) != 4 {
		t.Errorf("herramientas declaradas: %v", f.body(0)["tools"])
	}
	if sys := f.messages(0)[0]; sys["role"] != "system" || sys["content"] != "SISTEMA CON HERRAMIENTAS" {
		t.Errorf("sistema: %+v", sys)
	}

	// Texto: las vueltas se separan con un párrafo; el final cierra el turno.
	if want := "Voy a crear la caja.\n\nListo: la caja mide 40 × 40 × 20 mm."; res.Text != want {
		t.Errorf("texto %q, esperaba %q", res.Text, want)
	}
	if rec.text.String() != res.Text {
		t.Errorf("los trozos emitidos no suman el texto: %q", rec.text.String())
	}
	// El primer paso arrancó después de «Voy a crear la caja.».
	if res.Steps[0].TextAt != len("Voy a crear la caja.") {
		t.Errorf("textAt del primer paso: %d", res.Steps[0].TextAt)
	}
	if res.Steps[4].TextAt != res.Steps[0].TextAt {
		t.Errorf("sin texto entre pasos el textAt no cambia: %d", res.Steps[4].TextAt)
	}

	// Uso y costo: suma de las seis vueltas, con el precio de OpenRouter.
	if res.Usage.Input != 1400 || res.Usage.Output != 105 {
		t.Errorf("uso: %+v", res.Usage)
	}
	if want := 1400*3.0/1e6 + 105*15.0/1e6; math.Abs(res.Usage.CostUSD-want) > 1e-9 {
		t.Errorf("costo %v, esperaba %v", res.Usage.CostUSD, want)
	}
	if len(rec.usage) != 6 || rec.usage[5].Input != 1400 {
		t.Errorf("avisos de uso: %+v", rec.usage)
	}
	if res.Cancelled || res.StepLimit || res.ToolsUnsupported || res.Model != "fake-model" {
		t.Errorf("flags: %+v", res)
	}
	// Las imágenes que se escribieron para el modelo se borran al terminar.
	if left, _ := os.ReadDir(cfg.ImageDir); len(left) != 0 {
		t.Errorf("quedaron archivos temporales: %v", left)
	}
}

// El mismo ciclo contra la API de Anthropic: la imagen vuelve como bloque
// `image` y los resultados como `tool_result`.
func TestAnthropicCycleCreateRenderFixPreview(t *testing.T) {
	f := &fakeAnthropic{script: []string{
		anMsg("Voy a crear la caja.", cycleCalls[0], 20),
		anMsg("", cycleCalls[1], 10),
		anMsg("", cycleCalls[2], 30),
		anMsg("", cycleCalls[3], 10),
		anMsg("", cycleCalls[4], 10),
		anMsg("Listo: la caja mide 40 × 40 × 20 mm.", nil, 25),
	}}
	tools := cycleTools(t)
	cfg := baseConfig(f.client(t), tools)
	cfg.Model = "claude-fake"
	cfg.ImageDir = t.TempDir()
	rec := &recorder{}

	res, err := Run(context.Background(), cfg, rec.hooks())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(tools.names(), ","); got != "create_checkpoint,render,create_checkpoint,render,preview" {
		t.Fatalf("herramientas llamadas: %s", got)
	}
	if f.count() != 6 || len(res.Steps) != 5 {
		t.Fatalf("pedidos %d, pasos %d", f.count(), len(res.Steps))
	}
	if res.Steps[1].Status != StepError || res.Steps[4].Status != StepOK || len(res.Steps[4].Images) != 1 {
		t.Errorf("pasos: %+v", res.Steps)
	}
	// El render con error llegó como tool_result con is_error.
	results := blocksOfType(f.blocks(2, "user"), "tool_result")
	foundErr := false
	for _, b := range results {
		if b["tool_use_id"] == "c2" && b["is_error"] == true {
			foundErr = true
		}
	}
	if !foundErr {
		t.Errorf("el error del render debía volver como tool_result con is_error: %v", results)
	}
	// La imagen del preview solo aparece a partir del pedido siguiente.
	if len(blocksOfType(f.blocks(4, "user"), "image")) != 0 {
		t.Error("antes del preview no debía haber imagen")
	}
	imgs := blocksOfType(f.blocks(5, "user"), "image")
	if len(imgs) != 1 {
		t.Fatalf("tras el preview debía haber una imagen: %v", f.blocks(5, "user"))
	}
	src := imgs[0]["source"].(map[string]any)
	if src["type"] != "base64" || src["media_type"] != "image/png" || src["data"] == "" {
		t.Errorf("imagen: %v", src)
	}
	if res.Text != "Voy a crear la caja.\n\nListo: la caja mide 40 × 40 × 20 mm." {
		t.Errorf("texto: %q", res.Text)
	}
	if res.Usage.Input == 0 || res.Usage.Output != 105 {
		t.Errorf("uso: %+v", res.Usage)
	}
}

// Un modelo sin visión recibe el texto del preview y una nota, nunca la imagen.
func TestNoVisionKeepsImageOutOfTheRequest(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		oaStream("", cycleCalls[4], 10, 5),
		oaStream("Se ve bien según las medidas.", nil, 10, 5),
	}}
	tools := cycleTools(t)
	cfg := baseConfig(f.client(t), tools)
	cfg.Vision = false
	cfg.ImageDir = t.TempDir()
	res, err := Run(context.Background(), cfg, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if hasImage(f.messages(1)) {
		t.Error("un modelo sin visión no debe recibir la imagen")
	}
	var toolText string
	for _, m := range f.messages(1) {
		if m["role"] == "tool" {
			toolText, _ = m["content"].(string)
		}
	}
	if !strings.Contains(toolText, "cannot see images") {
		t.Errorf("falta la nota de que el modelo no ve: %q", toolText)
	}
	// La pantalla sí muestra la imagen: es del usuario.
	if len(res.Steps) != 1 || len(res.Steps[0].Images) != 1 {
		t.Errorf("el paso conserva su imagen para la pantalla: %+v", res.Steps)
	}
}

// El tope de pasos: las llamadas que ya no caben se contestan con un error sin
// ejecutarse, el modelo resume en una última vuelta y el turno termina con aviso.
func TestStepLimit(t *testing.T) {
	two := []oaCall{{"a", "measure", `{"file":"x"}`}, {"b", "measure", `{"file":"y"}`}}
	f := &fakeOpenAI{script: []string{
		oaStream("", two, 10, 5),
		oaStream("", []oaCall{{"c", "render", `{}`}}, 10, 5), // ya no cabe
		oaStream("Me quedé sin pasos; falta el preview.", nil, 10, 5),
	}}
	tools := newFakeTools("measure", "render")
	cfg := baseConfig(f.client(t), tools)
	cfg.MaxSteps = 2
	rec := &recorder{}
	res, err := Run(context.Background(), cfg, rec.hooks())
	if err != nil {
		t.Fatal(err)
	}
	if !res.StepLimit || len(res.Steps) != 2 {
		t.Fatalf("limit=%v pasos=%d", res.StepLimit, len(res.Steps))
	}
	if got := strings.Join(tools.names(), ","); got != "measure,measure" {
		t.Errorf("la tercera llamada no debía ejecutarse: %s", got)
	}
	// La llamada que no cupo se contestó con un error (el protocolo exige una
	// respuesta por cada llamada) y la última vuelta lleva la orden de resumir.
	var answered string
	for _, m := range f.messages(2) {
		if m["role"] == "tool" && m["tool_call_id"] == "c" {
			answered, _ = m["content"].(string)
		}
	}
	if !strings.HasPrefix(answered, "error:") || !strings.Contains(answered, "limit") {
		t.Errorf("la llamada pendiente debía contestarse con error: %q", answered)
	}
	msgs := f.messages(2)
	last := msgs[len(msgs)-1]
	if last["role"] != "user" || !strings.Contains(last["content"].(string), "limit of 2 steps") {
		t.Errorf("falta la orden de resumir: %+v", last)
	}
	if !strings.Contains(res.Text, "Me quedé sin pasos") || !strings.Contains(res.Text, "limit of 2 steps") {
		t.Errorf("el texto debe traer el resumen y el aviso: %q", res.Text)
	}
	if rec.text.String() != res.Text {
		t.Errorf("lo emitido no suma el texto: %q", rec.text.String())
	}
}

// Cancelar a mitad de una herramienta: el turno vuelve sin error, el paso queda
// marcado y lo escrito hasta ahí se conserva.
func TestCancelMidTool(t *testing.T) {
	f := &fakeOpenAI{script: []string{oaStream("Renderizando…", []oaCall{{"r", "render", `{"file":"001-a.scad"}`}}, 10, 5)}}
	tools := newFakeTools("render")
	started := make(chan struct{})
	tools.on("render", func(ctx context.Context, _ map[string]any) (mcp.ToolOutput, error) {
		close(started)
		<-ctx.Done()
		return mcp.ToolOutput{Text: "render a medias"}, ctx.Err()
	})
	cfg := baseConfig(f.client(t), tools)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()
	rec := &recorder{}
	res, err := Run(ctx, cfg, rec.hooks())
	if err != nil {
		t.Fatalf("una cancelación no es un error: %v", err)
	}
	if !res.Cancelled {
		t.Fatal("el resultado debe decir que se canceló")
	}
	if res.Text != "Renderizando…" {
		t.Errorf("el texto escrito se conserva: %q", res.Text)
	}
	if len(res.Steps) != 1 || res.Steps[0].Status != StepError || res.Steps[0].Text != "render a medias" {
		t.Errorf("paso cancelado: %+v", res.Steps)
	}
	if f.count() != 1 {
		t.Errorf("no se debe volver al modelo tras cancelar: %d pedidos", f.count())
	}
}

// Cancelar mientras el modelo escribe (sin herramientas en curso) tampoco es un
// error y conserva el parcial.
func TestCancelWhileStreaming(t *testing.T) {
	f := &fakeOpenAI{script: []string{""}, pending: make(chan struct{})}
	cfg := baseConfig(f.client(t), newFakeTools("render"))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-f.pending
		cancel()
	}()
	res, err := Run(ctx, cfg, Hooks{})
	if err != nil || !res.Cancelled {
		t.Fatalf("cancelado=%v err=%v", res.Cancelled, err)
	}
}

// Un modelo que rechaza las herramientas cae al modo texto con un aviso, con el
// prompt del modo texto, y el resultado lo recuerda.
func TestToolsUnsupportedFallsBackToText(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		`ERR 400 {"error":{"message":"registry.ollama.ai/library/gemma:2b does not support tools"}}`,
		oaStream("```openscad\ncube(10);\n```", nil, 10, 5),
	}}
	tools := newFakeTools("render")
	cfg := baseConfig(f.client(t), tools)
	rec := &recorder{}
	res, err := Run(context.Background(), cfg, rec.hooks())
	if err != nil {
		t.Fatal(err)
	}
	if !res.ToolsUnsupported || len(res.Steps) != 0 {
		t.Fatalf("flags: %+v", res)
	}
	if f.count() != 2 {
		t.Fatalf("pedidos: %d", f.count())
	}
	if _, has := f.body(0)["tools"]; !has {
		t.Error("el primer pedido debía declarar herramientas")
	}
	if _, has := f.body(1)["tools"]; has {
		t.Error("el reintento no debe declarar herramientas")
	}
	if sys := f.messages(1)[0]["content"]; sys != "SISTEMA SOLO TEXTO" {
		t.Errorf("el prompt debía cambiar al del modo texto: %v", sys)
	}
	if !strings.Contains(res.Text, "does not support tools") || !strings.Contains(res.Text, "cube(10)") {
		t.Errorf("texto: %q", res.Text)
	}
	if len(tools.calls) != 0 {
		t.Errorf("no se debía ejecutar nada: %v", tools.names())
	}
}

// El modo texto (sin caja) hace un solo pedido, sin herramientas y con el prompt
// de respaldo.
func TestTextModeNoTools(t *testing.T) {
	for name, cfg := range map[string]func(Config) Config{
		"sin caja":       func(c Config) Config { c.Tools = nil; return c },
		"agente apagado": func(c Config) Config { c.NoTools = true; return c },
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeOpenAI{script: []string{oaStream("Aquí tienes el código.", nil, 10, 5)}}
			c := cfg(baseConfig(f.client(t), newFakeTools("render")))
			res, err := Run(context.Background(), c, Hooks{})
			if err != nil || res.Text != "Aquí tienes el código." {
				t.Fatalf("%q, %v", res.Text, err)
			}
			if _, has := f.body(0)["tools"]; has || f.messages(0)[0]["content"] != "SISTEMA SOLO TEXTO" {
				t.Errorf("modo texto: tools=%v sys=%v", f.body(0)["tools"], f.messages(0)[0]["content"])
			}
		})
	}
}

// Los tropiezos del modelo vuelven como «error: …» en vez de cortar el turno:
// herramienta inexistente, argumentos que no son JSON, pánico dentro de la
// herramienta.
func TestToolStumblesReturnToTheModel(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		oaStream("", []oaCall{{"u", "borrar_todo", `{}`}, {"j", "render", `{"file": "001-a.scad`}, {"p", "boom", `{}`}}, 10, 5),
		oaStream("Entendido, lo corrijo.", nil, 10, 5),
	}}
	tools := newFakeTools("render", "boom")
	tools.on("boom", func(context.Context, map[string]any) (mcp.ToolOutput, error) { panic("kaboom") })
	res, err := Run(context.Background(), baseConfig(f.client(t), tools), Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, m := range f.messages(1) {
		if m["role"] == "tool" {
			got[m["tool_call_id"].(string)], _ = m["content"].(string)
		}
	}
	for id, want := range map[string]string{"u": "not available", "j": "not valid JSON", "p": "failed internally"} {
		if !strings.HasPrefix(got[id], "error:") || !strings.Contains(got[id], want) {
			t.Errorf("%s: %q (esperaba error con %q)", id, got[id], want)
		}
	}
	if len(res.Steps) != 3 {
		t.Fatalf("pasos: %d", len(res.Steps))
	}
	for _, s := range res.Steps {
		if s.Status != StepError {
			t.Errorf("paso %s debía quedar en error: %+v", s.Tool, s)
		}
	}
	if len(tools.calls) != 1 || tools.calls[0].Name != "boom" {
		t.Errorf("solo boom llegó a la caja: %v", tools.names())
	}
}

// Un resultado enorme se recorta para el modelo, pero la pantalla guarda lo
// suyo acotado.
func TestHugeToolResultIsTrimmed(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		oaStream("", []oaCall{{"r", "render", `{}`}}, 10, 5),
		oaStream("ok", nil, 10, 5),
	}}
	tools := newFakeTools("render")
	tools.on("render", func(context.Context, map[string]any) (mcp.ToolOutput, error) {
		return mcp.ToolOutput{Text: strings.Repeat("ñ", 50000)}, nil
	})
	res, err := Run(context.Background(), baseConfig(f.client(t), tools), Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	var content string
	for _, m := range f.messages(1) {
		if m["role"] == "tool" {
			content, _ = m["content"].(string)
		}
	}
	if n := len([]rune(content)); n > maxToolResultRunes+100 || !strings.Contains(content, "result trimmed") {
		t.Errorf("el modelo recibió %d caracteres: …%q", n, content[len(content)-60:])
	}
	if n := len([]rune(res.Steps[0].Text)); n > maxStepTextRunes+1 {
		t.Errorf("el texto del paso no está acotado: %d", n)
	}
}

// Un modelo que no devuelve nada ni llama nada es un error claro.
func TestEmptyAnswerIsAnError(t *testing.T) {
	f := &fakeOpenAI{script: []string{oaStream("", nil, 1, 1)}}
	_, err := Run(context.Background(), baseConfig(f.client(t), nil), Hooks{})
	if err == nil || !strings.Contains(err.Error(), "no text") {
		t.Errorf("error: %v", err)
	}
}

// Un fallo del proveedor devuelve igual lo que alcanzó a hacer.
func TestProviderErrorKeepsPartialWork(t *testing.T) {
	f := &fakeOpenAI{script: []string{
		oaStream("Empiezo.", []oaCall{{"m", "measure", `{}`}}, 10, 5),
		`ERR 500 {"error":{"message":"boom"}}`,
	}}
	res, err := Run(context.Background(), baseConfig(f.client(t), newFakeTools("measure")), Hooks{})
	var se *llm.StatusError
	if !errors.As(err, &se) || se.Status != 500 {
		t.Fatalf("error: %v", err)
	}
	if res.Text != "Empiezo." || len(res.Steps) != 1 {
		t.Errorf("lo hecho se conserva: %q, %d pasos", res.Text, len(res.Steps))
	}
}

// Un fallo de conexión transitorio se reintenta una vez si no llegó nada.
type flakyClient struct {
	llm.Client
	calls int
}

func (c *flakyClient) Chat(ctx context.Context, req llm.ChatRequest, on func(llm.Delta)) (llm.ChatResult, error) {
	c.calls++
	if c.calls == 1 {
		return llm.ChatResult{}, &llm.ConnError{Base: "http://x", Kind: llm.ConnReset, Err: errors.New("connection reset")}
	}
	on(llm.Delta{Text: "hola"})
	return llm.ChatResult{Message: llm.Message{Role: llm.RoleAssistant, Content: "hola"}}, nil
}

func TestChatRetryOnce(t *testing.T) {
	old := llmRetryDelay
	llmRetryDelay = time.Millisecond
	defer func() { llmRetryDelay = old }()
	c := &flakyClient{}
	res, err := Run(context.Background(), baseConfig(c, nil), Hooks{})
	if err != nil || res.Text != "hola" || c.calls != 2 {
		t.Fatalf("texto %q, err %v, llamadas %d", res.Text, err, c.calls)
	}
}

// Las imágenes de la conversación (adjuntos del usuario) viajan en el primer
// pedido.
func TestUserImagesTravelInTheFirstRequest(t *testing.T) {
	f := &fakeOpenAI{script: []string{oaStream("La veo.", nil, 10, 5)}}
	cfg := baseConfig(f.client(t), nil)
	img := filepath.Join(t.TempDir(), "foto.png")
	if err := os.WriteFile(img, pngBytes(t), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.User.Images = []string{img}
	if _, err := Run(context.Background(), cfg, Hooks{}); err != nil {
		t.Fatal(err)
	}
	if !hasImage(f.messages(0)) {
		t.Error("la imagen del usuario debía viajar")
	}
	var found bool
	for _, m := range f.messages(0) {
		if parts, ok := m["content"].([]any); ok {
			for _, p := range parts {
				if u, ok := p.(map[string]any)["image_url"].(map[string]any); ok {
					found = strings.HasPrefix(u["url"].(string), "data:image/png;base64,"+base64.StdEncoding.EncodeToString(pngBytes(t))[:20])
				}
			}
		}
	}
	if !found {
		t.Error("la imagen debía ir como data URI PNG")
	}
}

func TestCompactToolResults(t *testing.T) {
	big := strings.Repeat("x", 50000)
	var msgs []llm.Message
	msgs = append(msgs, llm.Message{Role: llm.RoleSystem, Content: "s"}, llm.Message{Role: llm.RoleUser, Content: "u"})
	for i := 0; i < 8; i++ {
		msgs = append(msgs,
			llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "x", Name: "render"}}},
			llm.Message{Role: llm.RoleTool, ToolCallID: "x", Content: big})
	}
	compactToolResults(msgs, contextBudgetChars)
	var trimmed, whole int
	for _, m := range msgs {
		if m.Role != llm.RoleTool {
			continue
		}
		if m.Content == big {
			whole++
		} else {
			trimmed++
		}
	}
	// Cuatro resultados de 50 000 caracteres no caben en 160 000: la segunda pasada
	// deja menos, pero siempre el más reciente.
	if whole < 1 || whole > keepRecentToolResults || trimmed == 0 {
		t.Errorf("completos %d, recortados %d", whole, trimmed)
	}
	if got := messagesChars(msgs); got > contextBudgetChars {
		t.Errorf("tras compactar quedan %d caracteres, sobre el presupuesto %d", got, contextBudgetChars)
	}
	// Los últimos quedan completos.
	last := msgs[len(msgs)-1]
	if last.Content != big {
		t.Error("el resultado más reciente no se recorta nunca")
	}
	// Bajo el presupuesto no se toca nada.
	small := []llm.Message{{Role: llm.RoleTool, Content: "ok"}}
	compactToolResults(small, contextBudgetChars)
	if small[0].Content != "ok" {
		t.Error("no debía tocar nada")
	}
}

func TestClampSteps(t *testing.T) {
	for in, want := range map[int]int{-3: 12, 0: 12, 1: 1, 12: 12, 30: 30, 31: 30, 500: 30} {
		if got := ClampSteps(in); got != want {
			t.Errorf("ClampSteps(%d)=%d, esperaba %d", in, got, want)
		}
	}
}

func TestPriceCost(t *testing.T) {
	if (Price{}).Cost(Usage{Input: 1e6, Output: 1e6}) != 0 {
		t.Error("sin precios conocidos el costo es 0 (no informado)")
	}
	if got := (Price{PromptPerM: 2, CompletionPerM: 10}).Cost(Usage{Input: 500_000, Output: 100_000}); math.Abs(got-2.0) > 1e-9 {
		t.Errorf("costo %v", got)
	}
}

func TestUTF16Len(t *testing.T) {
	for in, want := range map[string]int{"": 0, "abc": 3, "ñandú": 5, "😀": 2, "a😀b": 4, "caja de 40 mm — ok": 18} {
		if got := utf16Len(in); got != want {
			t.Errorf("utf16Len(%q)=%d, esperaba %d", in, got, want)
		}
	}
}

// El tope de pasos con la API de Anthropic: la última vuelta (resumir) sigue
// declarando las herramientas —la API lo exige si la conversación tiene
// tool_use— y la orden de resumir se funde con los resultados.
func TestAnthropicStepLimit(t *testing.T) {
	f := &fakeAnthropic{script: []string{
		anMsg("", []oaCall{{"toolu_1", "measure", `{"file":"a"}`}}, 5),
		anMsg("", []oaCall{{"toolu_2", "measure", `{"file":"b"}`}}, 5),
		anMsg("Resumen: medí dos veces.", nil, 5),
	}}
	tools := newFakeTools("measure")
	cfg := baseConfig(f.client(t), tools)
	cfg.Model = "claude-fake"
	cfg.MaxSteps = 1
	res, err := Run(context.Background(), cfg, Hooks{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.StepLimit || len(res.Steps) != 1 || f.count() != 3 {
		t.Fatalf("limit=%v pasos=%d pedidos=%d", res.StepLimit, len(res.Steps), f.count())
	}
	if tl, _ := f.body(2)["tools"].([]any); len(tl) != 1 {
		t.Errorf("la vuelta final debe declarar las herramientas: %v", f.body(2)["tools"])
	}
	users := f.blocks(2, "user")
	if len(blocksOfType(users, "tool_result")) != 2 {
		t.Errorf("las dos llamadas deben tener su tool_result: %v", users)
	}
	var nudge bool
	for _, b := range blocksOfType(users, "text") {
		if strings.Contains(b["text"].(string), "limit of 1 steps") {
			nudge = true
		}
	}
	if !nudge {
		t.Errorf("falta la orden de resumir: %v", users)
	}
	if !strings.Contains(res.Text, "Resumen: medí dos veces.") {
		t.Errorf("texto: %q", res.Text)
	}
}
