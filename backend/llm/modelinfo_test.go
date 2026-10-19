package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func byID(list []ModelInfo, id string) (ModelInfo, bool) {
	for _, m := range list {
		if m.ID == id {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// Ollama: las capacidades salen de /api/show por modelo; un modelo solo de
// embeddings no se ofrece para conversar y uno cuyo /api/show falla se lista
// con todo desconocido (no se esconde).
func TestOllamaListModelInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[{"name":"qwen3-vl:8b"},{"name":"llama3.2:3b"},{"name":"nomic-embed-text"},{"name":"roto:1b"}]}`)
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch req.Model {
		case "qwen3-vl:8b":
			fmt.Fprint(w, `{"capabilities":["completion","vision","tools"],"model_info":{"qwen3vl.context_length":262144}}`)
		case "llama3.2:3b":
			fmt.Fprint(w, `{"capabilities":["completion"],"model_info":{"llama.context_length":131072}}`)
		case "nomic-embed-text":
			fmt.Fprint(w, `{"capabilities":["embedding"],"model_info":{"nomic-bert.context_length":2048}}`)
		default:
			w.WriteHeader(500)
			fmt.Fprint(w, `{"error":"boom"}`)
		}
	})
	c := newTestClient(t, KindOllama, mux, "")
	list, err := ListModelInfo(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := byID(list, "nomic-embed-text"); ok {
		t.Error("un modelo de embeddings no se ofrece para conversar")
	}
	vl, _ := byID(list, "qwen3-vl:8b")
	if !vl.Vision || !vl.Tools || !vl.VisionKnown || !vl.ToolsKnown || vl.ContextTokens != 262144 {
		t.Errorf("qwen3-vl: %+v", vl)
	}
	ll, _ := byID(list, "llama3.2:3b")
	if ll.Vision || ll.Tools || !ll.VisionKnown || !ll.ToolsKnown || ll.ContextTokens != 131072 {
		t.Errorf("llama: %+v", ll)
	}
	broken, ok := byID(list, "roto:1b")
	if !ok || broken.VisionKnown || broken.ToolsKnown {
		t.Errorf("un /api/show que falla deja el modelo listado y desconocido: %+v %v", broken, ok)
	}
}

// OpenRouter publica modalidades, parámetros y precios; los precios llegan por
// token y se pasan a dólares por millón.
func TestOpenRouterListModelInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/models", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[
{"id":"anthropic/claude-sonnet","context_length":200000,"architecture":{"input_modalities":["text","image"]},"supported_parameters":["tools","temperature"],"pricing":{"prompt":"0.000003","completion":"0.000015"}},
{"id":"meta/llama-text","context_length":8192,"architecture":{"input_modalities":["text"]},"supported_parameters":["temperature"],"pricing":{"prompt":"0","completion":"-1"}},
{"id":"sin/datos"}
]}`)
	})
	// El host real de OpenRouter no se puede levantar en un test: el cliente
	// apunta a openrouter.ai (así pasa por isOpenRouter de verdad) y su
	// transporte marca siempre la dirección del servidor de prueba.
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := New(KindOpenAI, "https://openrouter.ai/api/v1", Options{})
	if err != nil {
		t.Fatal(err)
	}
	oc := c.(*openaiClient)
	oc.base = "http://openrouter.ai/api/v1"
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	oc.http = &http.Client{Transport: tr}
	list, err := ListModelInfo(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	s, _ := byID(list, "anthropic/claude-sonnet")
	if !s.Vision || !s.Tools || !s.VisionKnown || !s.ToolsKnown || s.ContextTokens != 200000 {
		t.Errorf("sonnet: %+v", s)
	}
	if s.PromptPricePerM < 2.99 || s.PromptPricePerM > 3.01 || s.CompletionPricePerM < 14.99 || s.CompletionPricePerM > 15.01 {
		t.Errorf("precios por millón: %v / %v", s.PromptPricePerM, s.CompletionPricePerM)
	}
	l, _ := byID(list, "meta/llama-text")
	if l.Vision || l.Tools || !l.VisionKnown || !l.ToolsKnown || l.PromptPricePerM != 0 || l.CompletionPricePerM != 0 {
		t.Errorf("llama (gratis o precio variable no son un precio): %+v", l)
	}
	n, _ := byID(list, "sin/datos")
	if n.VisionKnown || n.ToolsKnown {
		t.Errorf("sin datos es desconocido, no «no»: %+v", n)
	}
}

// Un servidor compatible con OpenAI genérico solo trae ids: todo desconocido.
// Y Gemini lista «models/…»: el id se deja como lo acepta el chat.
func TestOpenAICompatibleListModelInfoIsUnknown(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"qwen2.5-7b"},{"id":"models/gemini-2.5-pro"}]}`)
	})
	c := newTestClient(t, KindOpenAI, mux, "")
	list, err := ListModelInfo(context.Background(), c)
	if err != nil || len(list) != 2 {
		t.Fatalf("%v, %v", list, err)
	}
	for _, m := range list {
		if m.VisionKnown || m.ToolsKnown || m.Vision || m.Tools {
			t.Errorf("un servidor genérico no informa capacidades: %+v", m)
		}
	}
	if list[1].ID != "models/gemini-2.5-pro" {
		t.Errorf("fuera de Gemini el id no se toca: %q", list[1].ID)
	}
	if got := openaiModelID("https://generativelanguage.googleapis.com/v1beta/openai", "models/gemini-2.5-pro"); got != "gemini-2.5-pro" {
		t.Errorf("Gemini: %q", got)
	}
}

// Anthropic: la ficha trae imágenes y ventana; las herramientas son siempre sí.
func TestAnthropicListModelInfo(t *testing.T) {
	f := &fakeAnthropic{models: `{"data":[
{"id":"claude-opus-5-5","type":"model","display_name":"Opus","created_at":"2026-09-01T00:00:00Z","max_input_tokens":1000000,"capabilities":{"image_input":{"supported":true}}},
{"id":"claude-texto","type":"model","display_name":"Texto","created_at":"2026-08-01T00:00:00Z","max_input_tokens":200000,"capabilities":{"image_input":{"supported":false}}},
{"id":"claude-sin-ficha","type":"model","display_name":"X","created_at":"2026-07-01T00:00:00Z"}
],"has_more":false}`}
	c := newAnthropicTest(t, f, Options{Token: "k"})
	list, err := ListModelInfo(context.Background(), c)
	if err != nil || len(list) != 3 {
		t.Fatalf("%v, %v", list, err)
	}
	if list[0].ID != "claude-opus-5-5" || !list[0].Vision || !list[0].VisionKnown || !list[0].Tools || !list[0].ToolsKnown || list[0].ContextTokens != 1000000 {
		t.Errorf("opus: %+v", list[0])
	}
	if list[1].Vision || !list[1].VisionKnown {
		t.Errorf("sin imágenes según la ficha: %+v", list[1])
	}
	if list[2].VisionKnown || !list[2].Tools {
		t.Errorf("sin ficha: visión desconocida, herramientas sí: %+v", list[2])
	}
}

func TestPricePerMillion(t *testing.T) {
	for in, want := range map[string]float64{`"0.000001"`: 1, `0.0000025`: 2.5, `"0"`: 0, `"-1"`: 0, `""`: 0, `"abc"`: 0, ``: 0} {
		got := pricePerMillion([]byte(in))
		if d := got - want; d > 1e-9 || d < -1e-9 {
			t.Errorf("%q → %v, esperaba %v", in, got, want)
		}
	}
}
