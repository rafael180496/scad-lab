package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"scadlab/backend/i18n"
)

// openaiClient habla la API de OpenAI (/chat/completions) con el stream SSE:
// líneas `data: {json}` y un `data: [DONE]` final. La URL base se da
// incluyendo el /v1 (http://localhost:1234/v1), como la muestran LM Studio,
// vLLM y compañía.
type openaiClient struct {
	requester
}

func (c *openaiClient) Models(ctx context.Context) ([]string, error) {
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
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "la respuesta de %s no es la de un servidor compatible con OpenAI: %w", EN: "the response from %s is not from an OpenAI-compatible server: %w"}, c.base, err)
	}
	names := make([]string, 0, len(out.Data))
	for _, m := range out.Data {
		if m.ID != "" {
			names = append(names, m.ID)
		}
	}
	return names, nil
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiChatRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	Stream   bool            `json:"stream"`
}

type openaiChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Error json.RawMessage `json:"error"`
}

func (c *openaiClient) Chat(ctx context.Context, req ChatRequest, onDelta func(string)) (Result, error) {
	if strings.TrimSpace(req.Model) == "" {
		return Result{}, i18n.New(i18n.Msg{ES: "falta elegir el modelo", EN: "the model is missing"})
	}
	body := openaiChatRequest{Model: req.Model, Stream: true}
	if req.System != "" {
		body.Messages = append(body.Messages, openaiMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, openaiMessage{Role: m.Role, Content: m.Content})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Accept", "text/event-stream")
	resp, err := c.do(hr)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	var text strings.Builder
	res := Result{Model: req.Model}
	done := false
	fail := func(err error) (Result, error) {
		res.Text = text.String()
		return res, err
	}

	// El Scanner arma cada línea completa aunque un `data:` llegue cortado entre
	// dos lecturas del socket: lo que se parsea es siempre una línea entera.
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		data, ok := strings.CutPrefix(line, "data:")
		if !ok {
			// Comentarios (`: keep-alive`), `event:` y líneas vacías.
			continue
		}
		data = strings.TrimSpace(data)
		if data == "[DONE]" {
			done = true
			break
		}
		var ch openaiChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			continue
		}
		if len(ch.Error) > 0 && string(ch.Error) != "null" {
			msg := trimSnippet(errorFromBody([]byte(`{"error":`+string(ch.Error)+`}`)), ch.Error)
			return fail(i18n.Errorf(i18n.Msg{ES: "el servidor %s respondió con un error: %s", EN: "the server %s answered with an error: %s"}, c.base, msg))
		}
		if ch.Model != "" {
			res.Model = ch.Model
		}
		for _, choice := range ch.Choices {
			if t := choice.Delta.Content; t != "" {
				text.WriteString(t)
				if onDelta != nil {
					onDelta(t)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		if ctx.Err() != nil {
			return fail(ctx.Err())
		}
		return fail(interruptedError(c.base, err))
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	// Algunos servidores cierran sin mandar [DONE]; si ya llegó contenido se da
	// por completa. Sin nada de nada, es un corte.
	if !done && text.Len() == 0 {
		return fail(endedError(c.base))
	}
	res.Text = text.String()
	return res, nil
}
