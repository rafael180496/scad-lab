// Package llm habla con proveedores de modelos por HTTP para el chat de diseño
// de ScadLab: Ollama, cualquier servidor compatible con la API de OpenAI (LM
// Studio, vLLM, llama.cpp server, OpenRouter…) y la API de Anthropic.
//
// Es un puerto reducido de backend/llm de mini-tools: solo conversación de
// texto con streaming y cancelación. No hay adjuntos, herramientas ni
// pensamiento visible; lo que el modelo "piensa" antes de contestar se descarta
// y al usuario le llega únicamente la respuesta.
//
// # Por qué a mano (Ollama y OpenAI) y con el SDK (Anthropic)
//
// Ollama transmite NDJSON y la API de OpenAI, SSE: los dos caben en net/http y
// encoding/json, y lo único que importa mirar es qué llega en cada trozo. La
// API de Anthropic, en cambio, tiene eventos tipados, errores con tipo y
// reintentos con retry-after: reimplementarlo a mano es código que parece
// andar y se rompe con la siguiente versión de la API, así que ese cliente usa
// el SDK oficial.
//
// # Qué NO hace
//
// No conoce la base ni los workspaces: recibe una URL, un token opcional y
// mensajes, y devuelve texto en trozos. De dónde sale el token lo decide quien
// arma el cliente (app_chat.go lo lee de la variable de entorno que nombra
// apiKeyEnv); aquí nunca se guarda ni se escribe en un log ni en un error.
package llm

import (
	"context"
	"net/url"
	"strings"
	"time"

	"scadlab/backend/i18n"
)

// Tipos de servidor (coinciden con ai_providers.kind).
const (
	KindOllama    = "ollama"
	KindOpenAI    = "openai"
	KindAnthropic = "anthropic"
)

// Roles de un mensaje.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Message es un mensaje de la conversación, común a los tres protocolos.
type Message struct {
	Role    string
	Content string
}

// ChatRequest es un turno a enviar. System va aparte porque cada protocolo lo
// pone en un lugar distinto (primer mensaje o parámetro propio).
type ChatRequest struct {
	Model    string
	System   string
	Messages []Message
}

// Result es la respuesta. Text trae lo recibido hasta donde se llegó, también
// cuando Chat devuelve error: quien llama decide si conserva el parcial (una
// cancelación a mitad de respuesta conserva lo ya escrito).
type Result struct {
	Text string
	// Model es el que respondió de verdad, que puede diferir del pedido.
	Model string
}

// Client es lo que ofrece un servidor de modelos.
type Client interface {
	// Models lista los ids de los modelos disponibles.
	Models(ctx context.Context) ([]string, error)
	// Chat envía la conversación y transmite la respuesta por onDelta (puede
	// ser nil) a medida que llega. Devuelve cuando el stream termina.
	Chat(ctx context.Context, req ChatRequest, onDelta func(text string)) (Result, error)
}

// Options son los datos de conexión de un servidor.
type Options struct {
	// Token se manda como `Authorization: Bearer <token>` (o x-api-key en
	// Anthropic). Vacío = sin autenticación, lo normal en un LM Studio local.
	Token string
	// NumCtx es la ventana de contexto que se le pide a Ollama (options.num_ctx).
	// 0 = la del servidor. Un servidor compatible con OpenAI la ignora.
	NumCtx int
}

// DefaultAnthropicBaseURL es la API oficial de Anthropic.
const DefaultAnthropicBaseURL = "https://api.anthropic.com"

// New arma el cliente del tipo pedido.
func New(kind, baseURL string, opt Options) (Client, error) {
	baseURL = normalizeFor(kind, baseURL)
	base, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	r := requester{base: base, token: opt.Token, http: newHTTPClient()}
	switch kind {
	case KindOllama:
		return &ollamaClient{requester: r, numCtx: opt.NumCtx}, nil
	case KindOpenAI:
		return &openaiClient{requester: r}, nil
	case KindAnthropic:
		return newAnthropicClient(r), nil
	}
	return nil, i18n.Errorf(i18n.Msg{ES: "llm: tipo de servidor desconocido %q", EN: "llm: unknown server kind %q"}, kind)
}

// normalizeFor aplica a la URL lo propio de cada tipo antes de validarla:
// Anthropic tiene una URL por defecto (vacía = la oficial) y el SDK agrega
// `/v1/...` por su cuenta, así que un `/v1` final —como se copia la URL de un
// proxy— se saca para no pedir `/v1/v1/messages`.
func normalizeFor(kind, raw string) string {
	if kind != KindAnthropic {
		return raw
	}
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return DefaultAnthropicBaseURL
	}
	return strings.TrimRight(strings.TrimSuffix(raw, "/v1"), "/")
}

// NormalizeBaseURL valida que sea http(s) con host y le saca la barra final.
func NormalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", i18n.New(i18n.Msg{ES: "la URL del servidor tiene que ser http:// o https:// con un host", EN: "the server URL must be http:// or https:// with a host"})
	}
	return strings.TrimRight(raw, "/"), nil
}

// controlTimeout acota las llamadas cortas (lista de modelos). El chat no lo
// usa: un modelo grande puede tardar minutos en cargarse antes del primer
// byte, y ahí manda el contexto de quien llama.
const controlTimeout = 10 * time.Second

// maxLine acota una línea del stream. Un trozo normal no se acerca a esto;
// una línea de 8 MB significa que no es lo que creemos.
const maxLine = 8 << 20

// AnthropicModels es la lista fija que ofrece ScadLab para la API de Anthropic
// (el modelo más capaz primero). Es fija a propósito: listar por red exigiría
// la clave solo para llenar un desplegable, y quien use otro modelo (o un
// proxy) lo escribe a mano en Configuración → IA.
var AnthropicModels = []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5"}
