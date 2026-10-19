package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func attachClient(t *testing.T, kind string, h http.Handler, suffix string) Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(kind, srv.URL+suffix, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Ollama dice si un modelo ve imágenes en `capabilities` de /api/show, y lo
// que dice se recuerda: el segundo adjunto no vuelve a preguntar.
func TestOllamaAttachCapsVision(t *testing.T) {
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] == "llava:7b" {
			fmt.Fprint(w, `{"capabilities":["completion","vision"]}`)
			return
		}
		fmt.Fprint(w, `{"capabilities":["completion","tools"]}`)
	})
	c := attachClient(t, KindOllama, mux, "")

	v := AttachCapsOf(context.Background(), c, "llava:7b")
	if !v.Images || !v.Known {
		t.Errorf("llava: %+v", v)
	}
	n := AttachCapsOf(context.Background(), c, "llama3.2")
	if n.Images || !n.Known || !strings.Contains(n.Note, "llama3.2") {
		t.Errorf("llama3.2 no ve imágenes y el proveedor lo informó: %+v", n)
	}
	before := calls.Load()
	AttachCapsOf(context.Background(), c, "llava:7b")
	if calls.Load() != before {
		t.Error("la segunda consulta del mismo modelo debería salir del caché")
	}
}

// Si Ollama no contesta no hay base para negar nada: se deja pasar y no cuenta
// como información del proveedor.
func TestOllamaAttachCapsUnreachable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model 'nope' not found"}`, http.StatusNotFound)
	})
	c := attachClient(t, KindOllama, mux, "")
	got := AttachCapsOf(context.Background(), c, "nope")
	if !got.Images || got.Known {
		t.Errorf("sin respuesta: imágenes supuestas, Known=false: %+v", got)
	}
}

const openRouterModels = `{"data":[
 {"id":"openai/gpt-4o","architecture":{"input_modalities":["text","image","file"],"modality":"text+image->text"}},
 {"id":"meta/llama-text","architecture":{"input_modalities":["text"]}},
 {"id":"old/vision","architecture":{"modality":"text+image->text"}}
]}`

func TestOpenRouterAttachCaps(t *testing.T) {
	var calls atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/models", func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, openRouterModels)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	oc := &openaiClient{requester: requester{base: srv.URL + "/api/v1", http: srv.Client()}}
	// La detección es por host: el servidor de prueba no es openrouter.ai, así
	// que se prueba la lectura directamente.
	mods, ok := oc.openRouterModalities(context.Background())
	if !ok || strings.Join(mods["openai/gpt-4o"], ",") != "text,image,file" || strings.Join(mods["meta/llama-text"], ",") != "text" {
		t.Fatalf("modalidades: %v ok=%v", mods, ok)
	}
	if strings.Join(mods["old/vision"], ",") != "text,image" {
		t.Errorf("la forma vieja «text+image->text» también se entiende: %v", mods["old/vision"])
	}
	oc.openRouterModalities(context.Background())
	if calls.Load() != 1 {
		t.Errorf("las modalidades se piden una vez por servidor, no por modelo: %d llamadas", calls.Load())
	}
}

func TestIsOpenRouter(t *testing.T) {
	for base, want := range map[string]bool{
		"https://openrouter.ai/api/v1": true,
		"https://eu.openrouter.ai/v1":  true,
		"https://api.openai.com/v1":    false,
		"http://localhost:1234/v1":     false,
		"https://notopenrouter.ai/v1":  false,
	} {
		if got := isOpenRouter(base); got != want {
			t.Errorf("%s: %v, quería %v", base, got, want)
		}
	}
}

// Un servidor compatible con OpenAI que no es OpenRouter no informa nada: las
// imágenes se suponen (Known=false).
func TestOpenAICompatibleAttachCapsAreAssumed(t *testing.T) {
	c := attachClient(t, KindOpenAI, http.NewServeMux(), "/v1")
	got := AttachCapsOf(context.Background(), c, "qwen2.5-vl")
	if !got.Images || got.Known || got.Note == "" {
		t.Errorf("caps = %+v", got)
	}
}

func TestAnthropicAttachCaps(t *testing.T) {
	live := `{"id":"claude-x","max_tokens":4096,"capabilities":{"image_input":{"supported":true},"pdf_input":{"supported":true},"thinking":{"supported":false,"types":{"adaptive":{"supported":false}}},"effort":{"supported":false}}}`
	f := &fakeAnthropic{caps: live}
	c := newAnthropicTest(t, f, Options{})
	got := AttachCapsOf(context.Background(), c, "claude-x")
	if !got.Images || !got.Known {
		t.Errorf("la ficha dice imágenes: %+v", got)
	}

	noImages := `{"id":"claude-y","max_tokens":4096,"capabilities":{"image_input":{"supported":false},"pdf_input":{"supported":false},"thinking":{"supported":false,"types":{"adaptive":{"supported":false}}},"effort":{"supported":false}}}`
	c2 := newAnthropicTest(t, &fakeAnthropic{caps: noImages}, Options{})
	got = AttachCapsOf(context.Background(), c2, "claude-y")
	if got.Images || !got.Known || !strings.Contains(got.Note, "claude-y") {
		t.Errorf("sin imágenes: %+v", got)
	}

	// Sin Models API (proxy): se supone, y no cuenta como informado.
	c3 := newAnthropicTest(t, &fakeAnthropic{}, Options{})
	got = AttachCapsOf(context.Background(), c3, "claude-sonnet-5-5")
	if !got.Images || got.Known {
		t.Errorf("plan B: %+v", got)
	}

	// Una ficha recortada (sin image_input/pdf_input) tampoco es un «no».
	c4 := newAnthropicTest(t, &fakeAnthropic{caps: modernCaps}, Options{})
	got = AttachCapsOf(context.Background(), c4, "claude-opus-5-5")
	if !got.Images || got.Known {
		t.Errorf("ficha sin los campos de adjuntos: %+v", got)
	}
}
