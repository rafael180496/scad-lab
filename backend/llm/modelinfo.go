package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"scadlab/backend/i18n"
)

// ModelInfo es un modelo de la lista de un servidor con lo que el proveedor
// SABE de él. Cada capacidad trae su «Known»: un servidor compatible con OpenAI
// genérico no dice si el modelo ve imágenes ni si sabe llamar herramientas, y
// «no se sabe» no es «no»: con Known en false un «no» nunca bloquea, solo se
// avisa (ver attachcaps.go).
type ModelInfo struct {
	ID          string
	Vision      bool
	VisionKnown bool
	Tools       bool
	ToolsKnown  bool
	// ContextTokens es la ventana de contexto que informó el proveedor; 0 = no
	// la dijo.
	ContextTokens int
	// PromptPricePerM y CompletionPricePerM son dólares por millón de tokens.
	// Solo OpenRouter los publica; 0 = desconocido.
	PromptPricePerM     float64
	CompletionPricePerM float64
}

// ModelLister lo implementan los clientes que saben listar los modelos con sus
// capacidades.
type ModelLister interface {
	ListModelInfo(ctx context.Context) ([]ModelInfo, error)
}

// ListModelInfo lista los modelos del servidor con sus capacidades. Un cliente
// que no sabe (un doble de prueba) devuelve los ids con todo desconocido.
func ListModelInfo(ctx context.Context, c Client) ([]ModelInfo, error) {
	if ml, ok := c.(ModelLister); ok {
		return ml.ListModelInfo(ctx)
	}
	ids, err := c.Models(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(ids))
	for _, id := range ids {
		out = append(out, ModelInfo{ID: id})
	}
	return out, nil
}

// --- Ollama --------------------------------------------------------------------

// showConcurrency acota los /api/show en paralelo: un Ollama con 40 modelos no
// debe recibir 40 pedidos a la vez ni dejar la lista esperando 40 en serie.
const showConcurrency = 6

// ListModelInfo de Ollama: /api/tags da los nombres y /api/show, por modelo,
// sus capacidades. Un /api/show que falla deja ese modelo con todo desconocido
// (el modelo se lista igual). Los modelos que solo son de embeddings no sirven
// para conversar y no se ofrecen.
func (c *ollamaClient) ListModelInfo(ctx context.Context) ([]ModelInfo, error) {
	names, err := c.Models(ctx)
	if err != nil {
		return nil, err
	}
	infos := make([]ModelInfo, len(names))
	skip := make([]bool, len(names))
	sem := make(chan struct{}, showConcurrency)
	var wg sync.WaitGroup
	for i, name := range names {
		infos[i] = ModelInfo{ID: name}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			d, err := c.showModel(ctx, name)
			if err != nil {
				return
			}
			if len(d.Capabilities) > 0 {
				infos[i].VisionKnown, infos[i].ToolsKnown = true, true
				infos[i].Vision = hasCap(d.Capabilities, "vision")
				infos[i].Tools = hasCap(d.Capabilities, "tools")
				skip[i] = hasCap(d.Capabilities, "embedding") && !hasCap(d.Capabilities, "completion")
			}
			infos[i].ContextTokens = int(d.ContextLength)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(infos))
	for i, m := range infos {
		if !skip[i] {
			out = append(out, m)
		}
	}
	return out, nil
}

// --- Compatibles con OpenAI ------------------------------------------------------

// openaiModelID deja el id tal como lo acepta /chat/completions. Google Gemini
// lista sus modelos como «models/gemini-2.5-pro» y su endpoint de chat acepta
// el nombre sin el prefijo, que es como lo documenta.
func openaiModelID(base, id string) string {
	if u, err := url.Parse(base); err == nil && strings.EqualFold(u.Hostname(), "generativelanguage.googleapis.com") {
		return strings.TrimPrefix(id, "models/")
	}
	return id
}

// ListModelInfo de un servidor compatible con OpenAI. /models solo trae ids,
// salvo en OpenRouter, que además informa por modelo las modalidades de
// entrada, los parámetros que admite (entre ellos `tools`), la ventana de
// contexto y el precio por token.
func (c *openaiClient) ListModelInfo(ctx context.Context) ([]ModelInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/models", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int64  `json:"context_length"`
			Architecture  struct {
				InputModalities []string `json:"input_modalities"`
				Modality        string   `json:"modality"`
			} `json:"architecture"`
			SupportedParameters []string `json:"supported_parameters"`
			Pricing             struct {
				Prompt     json.RawMessage `json:"prompt"`
				Completion json.RawMessage `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&body); err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "la respuesta de %s no es la de un servidor compatible con OpenAI: %w", EN: "the response from %s is not from an OpenAI-compatible server: %w"}, c.base, err)
	}
	router := isOpenRouter(c.base)
	out := make([]ModelInfo, 0, len(body.Data))
	for _, m := range body.Data {
		if m.ID == "" {
			continue
		}
		info := ModelInfo{ID: openaiModelID(c.base, m.ID)}
		if router {
			in := m.Architecture.InputModalities
			if len(in) == 0 && m.Architecture.Modality != "" {
				left, _, _ := strings.Cut(m.Architecture.Modality, "->")
				in = strings.Split(left, "+")
			}
			if len(in) > 0 {
				info.VisionKnown = true
				info.Vision = hasCap(in, "image")
			}
			if len(m.SupportedParameters) > 0 {
				info.ToolsKnown = true
				info.Tools = hasCap(m.SupportedParameters, "tools")
			}
			info.ContextTokens = int(m.ContextLength)
			info.PromptPricePerM = pricePerMillion(m.Pricing.Prompt)
			info.CompletionPricePerM = pricePerMillion(m.Pricing.Completion)
		}
		out = append(out, info)
	}
	return out, nil
}

// pricePerMillion convierte el precio por token de OpenRouter (un string como
// «0.000003», a veces un número) en dólares por millón de tokens. Un precio
// ausente, negativo (OpenRouter usa -1 para «variable») o ilegible es 0.
func pricePerMillion(raw json.RawMessage) float64 {
	if len(raw) == 0 {
		return 0
	}
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return f * 1e6
}

// --- Anthropic -----------------------------------------------------------------

// ListModelInfo de Anthropic: la Models API trae la ficha de cada modelo
// (imágenes, ventana de entrada). Todos los modelos de la API aceptan
// herramientas.
func (c *anthropicClient) ListModelInfo(ctx context.Context) ([]ModelInfo, error) {
	list, err := c.listModels(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(list))
	for _, m := range list {
		if m.ID == "" {
			continue
		}
		info := ModelInfo{ID: m.ID, Tools: true, ToolsKnown: true, ContextTokens: int(m.MaxInputTokens)}
		if m.Capabilities.ImageInput.JSON.Supported.Valid() {
			info.VisionKnown = true
			info.Vision = m.Capabilities.ImageInput.Supported
		}
		out = append(out, info)
	}
	return out, nil
}
