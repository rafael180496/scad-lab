package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"

	"scadlab/backend/i18n"
)

// Lo mínimo que ScadLab necesita de POST /api/show: las capacidades que declara
// el modelo («completion», «vision», «tools», «embedding»…) y su ventana de
// contexto. Es lo que mini-tools tiene en ollama_admin.go (que administra
// Ollama: bajar y borrar modelos, fuera del alcance de ScadLab).

type ollamaShow struct {
	Capabilities  []string
	ContextLength int64
}

func (c *ollamaClient) showModel(ctx context.Context, model string) (ollamaShow, error) {
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	// `name` por los Ollama viejos, `model` por los nuevos.
	raw, err := json.Marshal(map[string]any{"model": model, "name": model})
	if err != nil {
		return ollamaShow{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/show", bytes.NewReader(raw))
	if err != nil {
		return ollamaShow{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return ollamaShow{}, err
	}
	defer resp.Body.Close()
	var out struct {
		ModelInfo    map[string]json.RawMessage `json:"model_info"`
		Capabilities []string                   `json:"capabilities"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return ollamaShow{}, i18n.Errorf(i18n.Msg{ES: "la respuesta de %s no es la de un servidor Ollama: %w", EN: "the response from %s is not from an Ollama server: %w"}, c.base, err)
	}
	return ollamaShow{Capabilities: out.Capabilities, ContextLength: contextLengthOf(out.ModelInfo)}, nil
}

// contextLengthOf busca `<arquitectura>.context_length` en model_info: la clave
// lleva el nombre de la arquitectura (`llama.context_length`,
// `qwen2.context_length`…), así que se busca por sufijo.
func contextLengthOf(info map[string]json.RawMessage) int64 {
	keys := make([]string, 0, len(info))
	for k := range info {
		if strings.HasSuffix(k, ".context_length") {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		var n float64
		if json.Unmarshal(info[k], &n) == nil && n > 0 {
			return int64(n)
		}
	}
	return 0
}

func hasCap(caps []string, want string) bool {
	for _, c := range caps {
		if strings.EqualFold(c, want) {
			return true
		}
	}
	return false
}
