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

// ollamaClient habla el protocolo nativo de Ollama (/api/*), que transmite
// NDJSON: una línea JSON por trozo, la última con `done: true`.
type ollamaClient struct {
	requester
	// numCtx es la ventana de contexto a pedir; 0 = la del servidor.
	numCtx int
}

func (c *ollamaClient) Models(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&out); err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "la respuesta de %s no es la de un servidor Ollama: %w", EN: "the response from %s is not from an Ollama server: %w"}, c.base, err)
	}
	names := make([]string, 0, len(out.Models))
	for _, m := range out.Models {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	return names, nil
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
}

// ollamaOptions: hoy solo la ventana de contexto. Ollama arranca con 2k-4k
// tokens y recorta el prompt SIN avisar, así que un prompt de sistema largo
// más el código del checkpoint se comería el mensaje del usuario.
type ollamaOptions struct {
	NumCtx int `json:"num_ctx,omitempty"`
}

type ollamaChunk struct {
	Model   string `json:"model"`
	Message struct {
		Content string `json:"content"`
	} `json:"message"`
	Done  bool   `json:"done"`
	Error string `json:"error"`
}

func (c *ollamaClient) Chat(ctx context.Context, req ChatRequest, onDelta func(string)) (Result, error) {
	if strings.TrimSpace(req.Model) == "" {
		return Result{}, i18n.New(i18n.Msg{ES: "falta elegir el modelo", EN: "the model is missing"})
	}
	body := ollamaChatRequest{Model: req.Model, Stream: true}
	if c.numCtx > 0 {
		body.Options = &ollamaOptions{NumCtx: c.numCtx}
	}
	if req.System != "" {
		body.Messages = append(body.Messages, ollamaMessage{Role: "system", Content: req.System})
	}
	for _, m := range req.Messages {
		body.Messages = append(body.Messages, ollamaMessage{Role: m.Role, Content: m.Content})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Result{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/chat", bytes.NewReader(raw))
	if err != nil {
		return Result{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	resp, err := c.do(hr)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()

	var text strings.Builder
	res := Result{Model: req.Model}
	finished := false
	fail := func(err error) (Result, error) {
		res.Text = text.String()
		return res, err
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), maxLine)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ch ollamaChunk
		if err := json.Unmarshal(line, &ch); err != nil {
			// Una línea que no es JSON no corta el turno: lo que importa es
			// lo que ya llegó.
			continue
		}
		if ch.Error != "" {
			return fail(i18n.Errorf(i18n.Msg{ES: "el servidor %s respondió con un error: %s", EN: "the server %s answered with an error: %s"}, c.base, trimSnippet(ch.Error, nil)))
		}
		if ch.Model != "" {
			res.Model = ch.Model
		}
		if t := ch.Message.Content; t != "" {
			text.WriteString(t)
			if onDelta != nil {
				onDelta(t)
			}
		}
		if ch.Done {
			finished = true
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
	if !finished {
		return fail(endedError(c.base))
	}
	res.Text = text.String()
	return res, nil
}

// errorFromBody lee el `error` de un cuerpo de Ollama (texto) o de OpenAI
// (objeto con `message`).
func errorFromBody(body []byte) string {
	var e struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil || len(e.Error) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(e.Error, &s) == nil {
		return s
	}
	var o struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(e.Error, &o) == nil {
		return o.Message
	}
	return ""
}
