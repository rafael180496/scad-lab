//go:build integration

package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Prueba contra un Ollama real: go test -tags integration -run Ollama ./backend/llm
//
// Se salta sola si no hay un Ollama escuchando (OLLAMA_URL, por defecto
// http://localhost:11434) o si no tiene ningún modelo. SCADLAB_OLLAMA_MODEL
// elige el modelo; si no, el primero de la lista (conviene uno chico).
func TestIntegrationOllamaShortTurn(t *testing.T) {
	url := os.Getenv("OLLAMA_URL")
	if url == "" {
		url = "http://localhost:11434"
	}
	c, err := New(KindOllama, url, Options{ContextTokens: 4096})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	models, err := c.Models(ctx)
	if err != nil {
		t.Skipf("no hay un Ollama en %s: %v", url, err)
	}
	if len(models) == 0 {
		t.Skip("el Ollama no tiene modelos descargados")
	}
	model := os.Getenv("SCADLAB_OLLAMA_MODEL")
	if model == "" {
		model = models[0]
	}
	var deltas int
	res, err := c.Chat(ctx, ChatRequest{
		Model: model,
		Messages: []Message{
			{Role: RoleSystem, Content: "Responde en una sola palabra."},
			{Role: RoleUser, Content: "Di OK."},
		},
	}, func(Delta) { deltas++ })
	if err != nil {
		t.Fatalf("turno con %s: %v", model, err)
	}
	if strings.TrimSpace(res.Message.Content) == "" || deltas == 0 {
		t.Fatalf("respuesta vacía (%d deltas): %+v", deltas, res)
	}
	t.Logf("%s respondió %q en %d trozos (%d tokens)", model, res.Message.Content, deltas, res.Usage.Output)
}
