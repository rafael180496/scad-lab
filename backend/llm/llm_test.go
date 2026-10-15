package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// flushWrite escribe y vacía el buffer: sin Flush el cliente recibiría todo
// junto al cerrar y no se probaría el streaming.
func flushWrite(w http.ResponseWriter, s string) {
	_, _ = io.WriteString(w, s)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func newClient(t *testing.T, kind, url string, opt Options) Client {
	t.Helper()
	c, err := New(kind, url, opt)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// collect acumula los deltas que recibe onDelta.
type collect struct {
	mu    sync.Mutex
	parts []string
}

func (c *collect) add(s string) {
	c.mu.Lock()
	c.parts = append(c.parts, s)
	c.mu.Unlock()
}

func (c *collect) all() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.parts...)
}

func TestNewValidatesURL(t *testing.T) {
	for _, bad := range []string{"", "localhost:11434", "ftp://x", "http://"} {
		if _, err := New(KindOllama, bad, Options{}); err == nil {
			t.Errorf("%q debería rechazarse", bad)
		}
	}
	if _, err := New("otro", "http://x", Options{}); err == nil {
		t.Error("un tipo desconocido debería rechazarse")
	}
	// Anthropic: vacío = la oficial, y un /v1 final se saca.
	if got := normalizeFor(KindAnthropic, ""); got != DefaultAnthropicBaseURL {
		t.Errorf("default: %q", got)
	}
	if got := normalizeFor(KindAnthropic, "https://proxy.example/v1/"); got != "https://proxy.example" {
		t.Errorf("/v1: %q", got)
	}
}

// --- Ollama (NDJSON) --------------------------------------------------------

func TestOllamaStreamsNDJSON(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("ruta: %s", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/x-ndjson")
		flushWrite(w, `{"model":"qwen3","message":{"role":"assistant","content":"Ho"},"done":false}`+"\n")
		// Una línea JSON cortada entre dos escrituras: el cliente tiene que
		// esperar a tenerla entera.
		flushWrite(w, `{"model":"qwen3","message":{"role":"assistant","content":"la, "`)
		time.Sleep(20 * time.Millisecond)
		flushWrite(w, `},"done":false}`+"\n"+`{"message":{"content":"mundo"},"done":false}`+"\n")
		flushWrite(w, "esto no es json\n")
		flushWrite(w, `{"message":{"content":""},"done":true,"eval_count":9}`+"\n")
	}))
	defer srv.Close()

	var got collect
	c := newClient(t, KindOllama, srv.URL, Options{NumCtx: 16384})
	res, err := c.Chat(context.Background(), ChatRequest{
		Model:    "qwen3",
		System:   "Sos experto en OpenSCAD",
		Messages: []Message{{Role: RoleUser, Content: "hola"}},
	}, got.add)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Hola, mundo" || res.Model != "qwen3" {
		t.Errorf("resultado: %+v", res)
	}
	if strings.Join(got.all(), "|") != "Ho|la, |mundo" {
		t.Errorf("deltas: %v", got.all())
	}
	msgs := gotBody["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" || msgs[1].(map[string]any)["content"] != "hola" {
		t.Errorf("mensajes enviados: %v", msgs)
	}
	if gotBody["stream"] != true {
		t.Errorf("stream: %v", gotBody["stream"])
	}
	if opts, _ := gotBody["options"].(map[string]any); opts["num_ctx"] != float64(16384) {
		t.Errorf("num_ctx: %v", gotBody["options"])
	}
}

func TestOllamaHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"error":"model 'nope' not found"}`)
	}))
	defer srv.Close()
	_, err := newClient(t, KindOllama, srv.URL, Options{}).Chat(context.Background(), ChatRequest{Model: "nope", Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 404 || !strings.Contains(err.Error(), "model 'nope' not found") {
		t.Fatalf("error: %v", err)
	}
}

func TestOllamaErrorInStreamAndTruncated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "x") {
			return
		}
		flushWrite(w, `{"message":{"content":"par"},"done":false}`+"\n")
		flushWrite(w, `{"error":"out of memory"}`+"\n")
	}))
	defer srv.Close()
	res, err := newClient(t, KindOllama, srv.URL, Options{}).Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "out of memory") {
		t.Fatalf("error: %v", err)
	}
	if res.Text != "par" {
		t.Errorf("el parcial se conserva: %q", res.Text)
	}

	// Cierra sin `done`: es un corte, no una respuesta completa.
	cut := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flushWrite(w, `{"message":{"content":"par"},"done":false}`+"\n")
	}))
	defer cut.Close()
	_, err = newClient(t, KindOllama, cut.URL, Options{}).Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil)
	var ce *ConnError
	if !errors.As(err, &ce) || ce.Kind != ConnEnded {
		t.Fatalf("esperaba ConnEnded, vino %v", err)
	}
}

func TestOllamaCancelMidStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flushWrite(w, `{"message":{"content":"uno "},"done":false}`+"\n")
		<-r.Context().Done() // se queda "pensando" hasta que el cliente corte
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan struct{}, 1)
	res, err := newClient(t, KindOllama, srv.URL, Options{}).Chat(ctx, ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, func(string) {
		select {
		case first <- struct{}{}:
		default:
		}
		cancel()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("esperaba context.Canceled, vino %v", err)
	}
	if res.Text != "uno " {
		t.Errorf("parcial: %q", res.Text)
	}
	select {
	case <-first:
	default:
		t.Error("nunca llegó el primer delta")
	}
}

func TestOllamaConnectionRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nadie escucha
	_, err := newClient(t, KindOllama, url, Options{}).Models(context.Background())
	var ce *ConnError
	if !errors.As(err, &ce) || ce.Kind != ConnRefused {
		t.Fatalf("esperaba ConnRefused, vino %v", err)
	}
}

func TestOllamaModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("ruta: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"models":[{"name":"qwen3-coder:30b"},{"name":""},{"name":"llama3.2:3b"}]}`)
	}))
	defer srv.Close()
	got, err := newClient(t, KindOllama, srv.URL+"/", Options{}).Models(context.Background())
	if err != nil || strings.Join(got, ",") != "qwen3-coder:30b,llama3.2:3b" {
		t.Fatalf("modelos: %v, %v", got, err)
	}
	// Un servidor que no es Ollama no devuelve JSON de modelos.
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "<html>") }))
	defer html.Close()
	if _, err := newClient(t, KindOllama, html.URL, Options{}).Models(context.Background()); err == nil {
		t.Error("una respuesta que no es de Ollama debería dar error")
	}
}

// --- OpenAI-compatible (SSE) ------------------------------------------------

func sseData(s string) string { return "data: " + s + "\n\n" }

func TestOpenAIStreamsSSE(t *testing.T) {
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("ruta: %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		flushWrite(w, ": keep-alive\n\n")
		flushWrite(w, sseData(`{"model":"gpt-x","choices":[{"delta":{"role":"assistant","content":""}}]}`))
		flushWrite(w, sseData(`{"choices":[{"delta":{"content":"cu"}}]}`))
		// Un `data:` partido entre dos lecturas, a mitad del JSON.
		flushWrite(w, `data: {"choices":[{"delta":{"con`)
		time.Sleep(20 * time.Millisecond)
		flushWrite(w, `tent":"be()"}}]}`+"\n\n")
		flushWrite(w, sseData(`{"choices":[{"delta":{"reasoning_content":"pensando…"}}]}`))
		flushWrite(w, sseData(`{"choices":[{"delta":{"content":";"},"finish_reason":"stop"}]}`))
		flushWrite(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	var got collect
	c := newClient(t, KindOpenAI, srv.URL+"/v1/", Options{Token: "sk-secreto"})
	res, err := c.Chat(context.Background(), ChatRequest{
		Model:    "gpt-x",
		System:   "sistema",
		Messages: []Message{{Role: RoleUser, Content: "hola"}, {Role: RoleAssistant, Content: "qué tal"}, {Role: RoleUser, Content: "un cubo"}},
	}, got.add)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "cube();" || res.Model != "gpt-x" {
		t.Errorf("resultado: %+v", res)
	}
	if strings.Join(got.all(), "|") != "cu|be()|;" {
		t.Errorf("deltas: %v", got.all())
	}
	if gotAuth != "Bearer sk-secreto" {
		t.Errorf("Authorization: %q", gotAuth)
	}
	msgs := gotBody["messages"].([]any)
	if len(msgs) != 4 || msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("mensajes: %v", msgs)
	}
}

func TestOpenAINoTokenNoAuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Authorization"]; ok {
			t.Error("sin token no debe mandarse Authorization")
		}
		flushWrite(w, sseData(`{"choices":[{"delta":{"content":"ok"}}]}`))
	}))
	defer srv.Close()
	// Sin [DONE], pero con contenido: se da por completa (LM Studio viejo).
	res, err := newClient(t, KindOpenAI, srv.URL, Options{}).Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil)
	if err != nil || res.Text != "ok" {
		t.Fatalf("%+v, %v", res, err)
	}
}

func TestOpenAIHTTPErrorDoesNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = io.WriteString(w, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error"}}`)
	}))
	defer srv.Close()
	_, err := newClient(t, KindOpenAI, srv.URL, Options{Token: "sk-secreto"}).Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil)
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 401 || !strings.Contains(err.Error(), "Incorrect API key provided") {
		t.Fatalf("error: %v", err)
	}
	if strings.Contains(err.Error(), "sk-secreto") {
		t.Error("el token no puede aparecer en un error")
	}
}

func TestOpenAIErrorEventAndEmptyStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flushWrite(w, sseData(`{"choices":[{"delta":{"content":"a"}}]}`))
		flushWrite(w, sseData(`{"error":{"message":"context length exceeded"}}`))
	}))
	defer srv.Close()
	res, err := newClient(t, KindOpenAI, srv.URL, Options{}).Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil)
	if err == nil || !strings.Contains(err.Error(), "context length exceeded") || res.Text != "a" {
		t.Fatalf("%+v, %v", res, err)
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer empty.Close()
	_, err = newClient(t, KindOpenAI, empty.URL, Options{}).Chat(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil)
	var ce *ConnError
	if !errors.As(err, &ce) || ce.Kind != ConnEnded {
		t.Fatalf("esperaba ConnEnded, vino %v", err)
	}
}

func TestOpenAICancelMidStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flushWrite(w, sseData(`{"choices":[{"delta":{"content":"par"}}]}`))
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res, err := newClient(t, KindOpenAI, srv.URL, Options{}).Chat(ctx, ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, func(string) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("esperaba context.Canceled, vino %v", err)
	}
	if res.Text != "par" {
		t.Errorf("parcial: %q", res.Text)
	}
}

func TestOpenAIModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("ruta: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"data":[{"id":"a"},{"id":"b"}]}`)
	}))
	defer srv.Close()
	got, err := newClient(t, KindOpenAI, srv.URL+"/v1", Options{}).Models(context.Background())
	if err != nil || strings.Join(got, ",") != "a,b" {
		t.Fatalf("modelos: %v, %v", got, err)
	}
}

func TestMissingModel(t *testing.T) {
	for _, kind := range []string{KindOllama, KindOpenAI, KindAnthropic} {
		c := newClient(t, kind, "http://localhost:1", Options{})
		if _, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil); err == nil {
			t.Errorf("%s: sin modelo debería fallar antes de conectar", kind)
		}
	}
}
