package llm

import (
	"context"
	"errors"
	"strconv"
	"strings"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"scadlab/backend/i18n"
)

// Cliente de la API de Anthropic (Messages API) sobre el SDK oficial.
//
// Qué se manda y por qué:
//
//   - Streaming siempre: con un max_tokens alto el SDK lo exige para no pasarse
//     del timeout HTTP, y además es lo que hace fluido el chat.
//   - max_tokens 16000: un .scad completo con su explicación cabe de sobra, y
//     es un tope que aceptan todos los modelos actuales (uno más alto da 400 en
//     los de salida corta). Si aun así se corta, Chat lo avisa en el texto.
//   - Sin temperature/top_p/top_k, sin prefill y sin `thinking`: los modelos
//     actuales responden 400 a los parámetros viejos y el razonamiento no se
//     muestra en este chat, así que no se pide.
//   - El prompt de sistema lleva cache_control: es largo, idéntico en cada
//     turno de la conversación y es justo el prefijo que conviene cachear.
//   - Sin variables de entorno del SDK (WithoutEnvironmentDefaults): el
//     proveedor usa SOLO lo que ScadLab le configuró. La clave se la pasa
//     app_chat.go, leída de la variable que nombra apiKeyEnv.
const anthropicMaxTokens = 16000

type anthropicClient struct {
	base string
	api  anthropic.Client
}

func newAnthropicClient(r requester) *anthropicClient {
	opts := []option.RequestOption{
		option.WithoutEnvironmentDefaults(),
		option.WithBaseURL(r.base),
		option.WithHTTPClient(r.http),
	}
	if r.token != "" {
		opts = append(opts, option.WithAPIKey(r.token))
	}
	return &anthropicClient{base: r.base, api: anthropic.NewClient(opts...)}
}

// Models lista los ids de la API. ScadLab no la usa para el desplegable (ver
// AnthropicModels); queda para probar la conexión y el servidor de pruebas.
func (c *anthropicClient) Models(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, controlTimeout)
	defer cancel()
	pager := c.api.Models.ListAutoPaging(ctx, anthropic.ModelListParams{Limit: anthropic.Int(1000)})
	var ids []string
	for pager.Next() {
		if id := pager.Current().ID; id != "" {
			ids = append(ids, id)
		}
	}
	if err := pager.Err(); err != nil {
		return nil, c.mapError(ctx, err)
	}
	return ids, nil
}

// Chat envía la conversación y transmite la respuesta.
func (c *anthropicClient) Chat(ctx context.Context, req ChatRequest, onDelta func(string)) (Result, error) {
	model := strings.TrimSpace(req.Model)
	if model == "" {
		return Result{}, i18n.New(i18n.Msg{ES: "falta elegir el modelo", EN: "the model is missing"})
	}
	msgs := buildAnthropicMessages(req.Messages)
	if len(msgs) == 0 {
		return Result{}, i18n.New(i18n.Msg{ES: "no hay ningún mensaje para enviar", EN: "there is no message to send"})
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: anthropicMaxTokens,
		Messages:  msgs,
	}
	if strings.TrimSpace(req.System) != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System, CacheControl: anthropic.NewCacheControlEphemeralParam()}}
	}

	stream := c.api.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	var msg anthropic.Message
	var text strings.Builder
	res := Result{Model: model}
	fail := func(err error) (Result, error) {
		res.Text = text.String()
		return res, err
	}
	for stream.Next() {
		ev := stream.Current()
		if err := msg.Accumulate(ev); err != nil {
			return fail(i18n.Errorf(i18n.Msg{ES: "la respuesta de %s no tiene el formato esperado: %w", EN: "the response from %s does not have the expected format: %w"}, c.base, err))
		}
		if ev.Type == "content_block_delta" && ev.Delta.Type == "text_delta" && ev.Delta.Text != "" {
			text.WriteString(ev.Delta.Text)
			if onDelta != nil {
				onDelta(ev.Delta.Text)
			}
		}
	}
	if err := stream.Err(); err != nil {
		return fail(c.mapError(ctx, err))
	}
	if ctx.Err() != nil {
		return fail(ctx.Err())
	}
	if msg.StopReason == "" && len(msg.Content) == 0 {
		// El stream se cerró sin llegar ni al message_delta: un corte.
		return fail(endedError(c.base))
	}
	if msg.Model != "" {
		res.Model = string(msg.Model)
	}
	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		return fail(i18n.New(i18n.Msg{
			ES: "Anthropic rechazó este pedido por sus filtros de seguridad. Reformulá el pedido o prueba con otro modelo.",
			EN: "Anthropic declined this request with its safety filters. Rephrase the request or try another model.",
		}))
	case anthropic.StopReasonMaxTokens, anthropic.StopReasonModelContextWindowExceeded:
		// Un bloque de código cortado a la mitad se vería como respuesta
		// completa: se avisa en el propio texto.
		note := i18n.T(i18n.Msg{
			ES: "\n\n_⚠ La respuesta se cortó por el límite del modelo (tokens de salida o ventana de contexto)._",
			EN: "\n\n_⚠ The response was cut off by the model's limit (output tokens or context window)._",
		})
		text.WriteString(note)
		if onDelta != nil {
			onDelta(note)
		}
	}
	res.Text = text.String()
	return res, nil
}

// buildAnthropicMessages arma los mensajes de la API. La API exige que
// arranque con un mensaje de usuario y acepta turnos consecutivos del mismo
// rol (los funde), que es lo que pasa tras un turno fallido que no guardó
// respuesta; los mensajes vacíos se descartan porque la API los rechaza.
func buildAnthropicMessages(msgs []Message) []anthropic.MessageParam {
	var out []anthropic.MessageParam
	for _, m := range msgs {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		role := anthropic.MessageParamRoleUser
		if m.Role == RoleAssistant {
			role = anthropic.MessageParamRoleAssistant
		}
		if len(out) == 0 && role != anthropic.MessageParamRoleUser {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, anthropic.NewTextBlock(m.Content))
			continue
		}
		out = append(out, anthropic.MessageParam{Role: role, Content: []anthropic.ContentBlockParamUnion{anthropic.NewTextBlock(m.Content)}})
	}
	return out
}

// mapError traduce un error del SDK a los tipos de este paquete: un fallo HTTP
// de la API es un *StatusError con un mensaje claro y todo lo demás, un fallo
// de red clasificado (ConnError).
func (c *anthropicClient) mapError(ctx context.Context, err error) error {
	var ae *anthropic.Error
	if errors.As(err, &ae) {
		return c.statusError(ae)
	}
	return connError(ctx, c.base, err)
}

func (c *anthropicClient) statusError(ae *anthropic.Error) error {
	status := ae.StatusCode
	if status < 400 {
		// Un evento `error` en pleno stream llega con el 200 de la respuesta: el
		// tipo es lo único que dice qué pasó.
		switch string(ae.Type()) {
		case "overloaded_error":
			status = 529
		case "rate_limit_error":
			status = 429
		case "authentication_error":
			status = 401
		case "permission_error":
			status = 403
		case "not_found_error":
			status = 404
		case "invalid_request_error":
			status = 400
		default:
			status = 500
		}
	}
	detail := trimSnippet(errorFromBody([]byte(ae.RawJSON())), []byte(ae.RawJSON()))
	var msg string
	switch {
	case status == 401:
		msg = i18n.T(i18n.Msg{ES: "la clave de API no es válida o falta", EN: "the API key is invalid or missing"})
	case status == 403:
		msg = i18n.T(i18n.Msg{ES: "la clave no tiene permiso para esto (¿el modelo no está habilitado en tu organización?): %s", EN: "the key is not allowed to do this (is the model enabled for your organization?): %s"}, detail)
	case status == 404:
		msg = i18n.T(i18n.Msg{ES: "no se encontró el modelo: %s", EN: "the model was not found: %s"}, detail)
	case status == 429:
		msg = i18n.T(i18n.Msg{ES: "se alcanzó el límite de uso de la API (rate limit)", EN: "the API usage limit (rate limit) was reached"})
		if ae.Response != nil {
			if s, err := strconv.Atoi(strings.TrimSpace(ae.Response.Header.Get("retry-after"))); err == nil && s > 0 {
				msg += i18n.T(i18n.Msg{ES: "; reintenta en %d s", EN: "; retry in %d s"}, s)
			}
		}
	case status == 529:
		msg = i18n.T(i18n.Msg{ES: "la API de Anthropic está sobrecargada; reintenta en unos minutos", EN: "Anthropic's API is overloaded; retry in a few minutes"})
	case status >= 500:
		msg = i18n.T(i18n.Msg{ES: "error del servicio de Anthropic: %s", EN: "Anthropic service error: %s"}, detail)
	default:
		msg = detail
	}
	return &StatusError{Base: c.base, Status: status, Msg: msg}
}
