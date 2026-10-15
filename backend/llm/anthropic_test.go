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
)

// Servidor de prueba de la Messages API: mismo esquema que el de mini-tools,
// reducido a lo que usa el chat de texto.

func sse(typ, data string) string { return "event: " + typ + "\ndata: " + data + "\n\n" }

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func msgStart() string {
	return sse("message_start", `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":25,"output_tokens":1}}}`)
}

func textStart() string {
	return sse("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
}

func textDelta(s string) string {
	return sse("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":`+jsonStr(s)+`}}`)
}

func msgEnd(stop string) string {
	return sse("content_block_stop", `{"type":"content_block_stop","index":0}`) +
		sse("message_delta", fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q,"stop_sequence":null,"stop_details":null},"usage":{"output_tokens":5}}`, stop)) +
		sse("message_stop", `{"type":"message_stop"}`)
}

type fakeAnthropic struct {
	mu      sync.Mutex
	stream  []string // trozos del stream; cada uno se vacía por separado
	hold    bool     // tras el último trozo, esperar a que el cliente corte
	status  int
	errBody string
	bodies  []map[string]any
	headers []http.Header
}

func (f *fakeAnthropic) server() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"claude-opus-5-5","type":"model","display_name":"Opus","created_at":"2026-09-01T00:00:00Z"}],"has_more":false}`)
	})
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.headers = append(f.headers, r.Header.Clone())
		status, errBody, stream, hold := f.status, f.errBody, f.stream, f.hold
		f.mu.Unlock()
		if status != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			fmt.Fprint(w, errBody)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range stream {
			flushWrite(w, chunk)
		}
		if hold {
			<-r.Context().Done()
		}
	})
	return httptest.NewServer(mux)
}

func anthropicChat(t *testing.T, srv *httptest.Server, req ChatRequest, onDelta func(string)) (Result, error) {
	t.Helper()
	// Si el SDK leyera la variable del entorno, esta clave aparecería en el header.
	t.Setenv("ANTHROPIC_API_KEY", "clave-del-entorno")
	t.Setenv("ANTHROPIC_BASE_URL", "http://127.0.0.1:1")
	c := newClient(t, KindAnthropic, srv.URL, Options{Token: "sk-ant-prueba"})
	return c.Chat(context.Background(), req, onDelta)
}

func TestAnthropicStreamsAndRequestShape(t *testing.T) {
	f := &fakeAnthropic{stream: []string{
		msgStart(), textStart(), textDelta("Un "), textDelta("cubo"),
		// el delta cortado a la mitad de una línea SSE
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,",
		"\"delta\":{\"type\":\"text_delta\",\"text\":\" de 10.\"}}\n\n",
		msgEnd("end_turn"),
	}}
	srv := f.server()
	defer srv.Close()

	var got collect
	res, err := anthropicChat(t, srv, ChatRequest{
		Model:  "claude-sonnet-5-5",
		System: "Sos experto en OpenSCAD",
		Messages: []Message{
			{Role: RoleUser, Content: "hola"},
			{Role: RoleUser, Content: "un cubo"}, // dos de usuario seguidos: la API los funde
			{Role: RoleAssistant, Content: "vale"},
			{Role: RoleUser, Content: "   "}, // vacío: se descarta
		},
	}, got.add)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Un cubo de 10." {
		t.Errorf("texto: %q", res.Text)
	}
	if strings.Join(got.all(), "|") != "Un |cubo| de 10." {
		t.Errorf("deltas: %v", got.all())
	}
	if key := f.headers[0].Get("X-Api-Key"); key != "sk-ant-prueba" {
		t.Errorf("x-api-key: %q (el SDK no debe leer el entorno)", key)
	}
	b := f.bodies[0]
	if b["model"] != "claude-sonnet-5-5" || b["stream"] != true {
		t.Errorf("cuerpo: %v", b)
	}
	sys := b["system"].([]any)[0].(map[string]any)
	if sys["text"] != "Sos experto en OpenSCAD" || sys["cache_control"] == nil {
		t.Errorf("system: %v", sys)
	}
	msgs := b["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "user" || len(msgs[0].(map[string]any)["content"].([]any)) != 2 {
		t.Errorf("mensajes: %v", msgs)
	}
	if _, bad := b["temperature"]; bad {
		t.Error("no se manda temperature")
	}
	if _, bad := b["thinking"]; bad {
		t.Error("no se pide thinking")
	}
}

func TestAnthropicHTTPErrors(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`, "API key"},
		{404, `{"type":"error","error":{"type":"not_found_error","message":"model: nope"}}`, "model: nope"},
		{429, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`, "rate limit"},
	}
	for _, c := range cases {
		f := &fakeAnthropic{status: c.status, errBody: c.body}
		srv := f.server()
		_, err := anthropicChat(t, srv, ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil)
		srv.Close()
		var se *StatusError
		if !errors.As(err, &se) || se.Status != c.status || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%d: %v", c.status, err)
		}
		if err != nil && strings.Contains(err.Error(), "sk-ant-prueba") {
			t.Error("la clave no puede aparecer en un error")
		}
	}
}

func TestAnthropicStopReasons(t *testing.T) {
	f := &fakeAnthropic{stream: []string{msgStart(), textStart(), textDelta("cube(10"), msgEnd("max_tokens")}}
	srv := f.server()
	res, err := anthropicChat(t, srv, ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil)
	srv.Close()
	if err != nil || !strings.HasPrefix(res.Text, "cube(10") || !strings.Contains(res.Text, "cortó") && !strings.Contains(res.Text, "cut off") {
		t.Fatalf("cortada por tokens: %q, %v", res.Text, err)
	}

	f = &fakeAnthropic{stream: []string{msgStart(), textStart(), textDelta("no puedo"), msgEnd("refusal")}}
	srv = f.server()
	_, err = anthropicChat(t, srv, ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, nil)
	srv.Close()
	if err == nil {
		t.Fatal("un refusal debería ser un error")
	}
}

func TestAnthropicCancelMidStream(t *testing.T) {
	f := &fakeAnthropic{stream: []string{msgStart(), textStart(), textDelta("a medias")}, hold: true}
	srv := f.server()
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newClient(t, KindAnthropic, srv.URL, Options{Token: "k"})
	res, err := c.Chat(ctx, ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Content: "x"}}}, func(string) { cancel() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("esperaba context.Canceled, vino %v", err)
	}
	if res.Text != "a medias" {
		t.Errorf("parcial: %q", res.Text)
	}
}

func TestAnthropicModelsList(t *testing.T) {
	f := &fakeAnthropic{}
	srv := f.server()
	defer srv.Close()
	got, err := newClient(t, KindAnthropic, srv.URL, Options{Token: "k"}).Models(context.Background())
	if err != nil || len(got) != 1 || got[0] != "claude-opus-5-5" {
		t.Fatalf("%v, %v", got, err)
	}
	if len(AnthropicModels) == 0 {
		t.Error("la lista fija no puede estar vacía")
	}
}

func TestBuildAnthropicMessagesStartsWithUser(t *testing.T) {
	got := buildAnthropicMessages([]Message{{Role: RoleAssistant, Content: "huérfano"}, {Role: RoleUser, Content: "hola"}})
	if len(got) != 1 || got[0].Role != "user" {
		t.Fatalf("%+v", got)
	}
}
