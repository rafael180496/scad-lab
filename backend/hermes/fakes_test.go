package hermes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/mcp"
)

// --- Servidor falso compatible con OpenAI --------------------------------------

type oaCall struct {
	ID, Name, Args string
}

// oaStream arma una respuesta SSE: texto en trozos, llamadas con los argumentos
// partidos en dos (como el stream real) y el uso al final.
func oaStream(text string, calls []oaCall, in, out int) string {
	var b strings.Builder
	chunk := func(delta string) {
		fmt.Fprintf(&b, "data: {\"model\":\"fake-model\",\"choices\":[{\"delta\":%s}]}\n\n", delta)
	}
	if text != "" {
		half := len(text) / 2
		for _, part := range []string{text[:half], text[half:]} {
			if part != "" {
				chunk(`{"content":` + jstr(part) + `}`)
			}
		}
	}
	for i, c := range calls {
		half := len(c.Args) / 2
		chunk(fmt.Sprintf(`{"tool_calls":[{"index":%d,"id":%q,"type":"function","function":{"name":%q,"arguments":%s}}]}`, i, c.ID, c.Name, jstr(c.Args[:half])))
		chunk(fmt.Sprintf(`{"tool_calls":[{"index":%d,"function":{"arguments":%s}}]}`, i, jstr(c.Args[half:])))
	}
	fmt.Fprintf(&b, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d}}\n\n", in, out)
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

func jstr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// fakeOpenAI contesta /chat/completions con el siguiente guion y guarda el
// cuerpo de cada pedido. Un guion "" mantiene la conexión abierta hasta que el
// cliente la corte; "ERR <status> <cuerpo>" contesta ese error.
type fakeOpenAI struct {
	mu      sync.Mutex
	script  []string
	bodies  []map[string]any
	pending chan struct{} // se cierra cuando llega un pedido con guion "" (retenido)
}

func (f *fakeOpenAI) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		n := len(f.bodies)
		var resp string
		if n <= len(f.script) {
			resp = f.script[n-1]
		} else {
			resp = oaStream("(sin guion)", nil, 1, 1)
		}
		f.mu.Unlock()
		if resp == "" {
			if f.pending != nil {
				close(f.pending)
			}
			<-r.Context().Done()
			return
		}
		if rest, ok := strings.CutPrefix(resp, "ERR "); ok {
			status, msg, _ := strings.Cut(rest, " ")
			var code int
			_, _ = fmt.Sscanf(status, "%d", &code)
			w.WriteHeader(code)
			_, _ = io.WriteString(w, msg)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeOpenAI) client(t *testing.T) llm.Client {
	t.Helper()
	c, err := llm.New(llm.KindOpenAI, f.server(t).URL, llm.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *fakeOpenAI) body(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[i]
}

func (f *fakeOpenAI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

// oaMessages devuelve los mensajes del pedido i.
func (f *fakeOpenAI) messages(i int) []map[string]any {
	var out []map[string]any
	for _, m := range f.body(i)["messages"].([]any) {
		out = append(out, m.(map[string]any))
	}
	return out
}

// hasImage dice si algún mensaje de usuario del pedido trae una parte image_url.
func hasImage(msgs []map[string]any) bool {
	for _, m := range msgs {
		if parts, ok := m["content"].([]any); ok {
			for _, p := range parts {
				if pm, ok := p.(map[string]any); ok && pm["type"] == "image_url" {
					return true
				}
			}
		}
	}
	return false
}

// --- Servidor falso de la Messages API de Anthropic ----------------------------

func sse(typ, data string) string { return "event: " + typ + "\ndata: " + data + "\n\n" }

func anStart() string {
	return sse("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-fake","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":30,"output_tokens":1}}}`)
}

func anText(idx int, text string) string {
	return sse("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, idx)) +
		sse("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"text_delta","text":%s}}`, idx, jstr(text))) +
		sse("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, idx))
}

func anTool(idx int, id, name, args string) string {
	half := len(args) / 2
	return sse("content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"tool_use","id":%q,"name":%q,"input":{}}}`, idx, id, name)) +
		sse("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`, idx, jstr(args[:half]))) +
		sse("content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":%d,"delta":{"type":"input_json_delta","partial_json":%s}}`, idx, jstr(args[half:]))) +
		sse("content_block_stop", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, idx))
}

func anEnd(stop string, out int) string {
	return sse("message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null},"usage":{"output_tokens":%d}}`, stop, out)) +
		sse("message_stop", `{"type":"message_stop"}`)
}

// anMsg arma un mensaje completo: texto opcional, llamadas y el cierre que
// corresponde (tool_use si hay llamadas, end_turn si no).
func anMsg(text string, calls []oaCall, out int) string {
	s := anStart()
	idx := 0
	if text != "" {
		s += anText(idx, text)
		idx++
	}
	for _, c := range calls {
		s += anTool(idx, c.ID, c.Name, c.Args)
		idx++
	}
	stop := "end_turn"
	if len(calls) > 0 {
		stop = "tool_use"
	}
	return s + anEnd(stop, out)
}

type fakeAnthropic struct {
	mu     sync.Mutex
	script []string
	bodies []map[string]any
}

func (f *fakeAnthropic) client(t *testing.T) llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			// /v1/models/{id}: sin ficha, el cliente usa sus valores por defecto.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(404)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"not_found_error","message":"nope"}}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		n := len(f.bodies)
		resp := anMsg("(sin guion)", nil, 1)
		if n <= len(f.script) {
			resp = f.script[n-1]
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	c, err := llm.New(llm.KindAnthropic, srv.URL, llm.Options{Token: "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *fakeAnthropic) body(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[i]
}

func (f *fakeAnthropic) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

// anBlocks devuelve los bloques de contenido de todos los mensajes del pedido i,
// con el rol de su mensaje.
func (f *fakeAnthropic) blocks(i int, role string) []map[string]any {
	var out []map[string]any
	for _, m := range f.body(i)["messages"].([]any) {
		mm := m.(map[string]any)
		if mm["role"] != role {
			continue
		}
		switch c := mm["content"].(type) {
		case []any:
			for _, b := range c {
				out = append(out, b.(map[string]any))
			}
		case string:
			out = append(out, map[string]any{"type": "text", "text": c})
		}
	}
	return out
}

func blocksOfType(bs []map[string]any, typ string) []map[string]any {
	var out []map[string]any
	for _, b := range bs {
		if b["type"] == typ {
			out = append(out, b)
		}
	}
	return out
}

// --- Caja de herramientas de mentira --------------------------------------------

type call struct {
	Name string
	Args map[string]any
}

// fakeTools responde con funciones por nombre y registra las llamadas.
type fakeTools struct {
	mu      sync.Mutex
	specs   []mcp.ToolSpec
	handler map[string]func(ctx context.Context, args map[string]any) (mcp.ToolOutput, error)
	calls   []call
}

func newFakeTools(names ...string) *fakeTools {
	f := &fakeTools{handler: map[string]func(context.Context, map[string]any) (mcp.ToolOutput, error){}}
	for _, n := range names {
		f.specs = append(f.specs, mcp.ToolSpec{Name: n, Description: "tool " + n, InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}})
	}
	return f
}

func (f *fakeTools) on(name string, h func(ctx context.Context, args map[string]any) (mcp.ToolOutput, error)) {
	f.handler[name] = h
}

func (f *fakeTools) Tools() []mcp.ToolSpec { return f.specs }

func (f *fakeTools) Call(ctx context.Context, name string, args map[string]any) (mcp.ToolOutput, error) {
	f.mu.Lock()
	f.calls = append(f.calls, call{Name: name, Args: args})
	h := f.handler[name]
	f.mu.Unlock()
	if h == nil {
		return mcp.ToolOutput{Text: "ok"}, nil
	}
	return h(ctx, args)
}

func (f *fakeTools) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.Name)
	}
	return out
}

// pngBytes es un PNG válido de 2×2 píxeles rojos: lo que devolvería `preview`.
func pngBytes(t testing.TB) []byte {
	t.Helper()
	return mustPNG(t, 8, 6)
}

// recorder junta lo que el bucle reporta.
type recorder struct {
	mu    sync.Mutex
	text  strings.Builder
	steps []Step
	usage []Usage
	flush int
}

func (r *recorder) hooks() Hooks {
	return Hooks{
		Text:  func(s string) { r.mu.Lock(); r.text.WriteString(s); r.mu.Unlock() },
		Flush: func() { r.mu.Lock(); r.flush++; r.mu.Unlock() },
		Step:  func(s Step) { r.mu.Lock(); r.steps = append(r.steps, s); r.mu.Unlock() },
		Usage: func(u Usage) { r.mu.Lock(); r.usage = append(r.usage, u); r.mu.Unlock() },
	}
}

func init() { i18n.SetLang("en") }
