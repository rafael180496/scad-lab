package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"sync"

	"scadlab/backend/i18n"
)

// Protocolo MCP (Model Context Protocol) sobre stdio, escrito a mano.
//
// **JSON-RPC 2.0, un mensaje por línea.** No se suma un SDK por la misma razón
// que en mini-tools (el repo hermano, de donde se porta esto): lo necesario son
// `initialize`, `tools/list`, `tools/call` y `ping` sobre un formato que
// `encoding/json` ya sabe hablar, y un SDK traería su propio modelo de
// transporte y de sesiones.
//
// **stdout solo lleva JSON-RPC.** Cualquier otra cosa que se escriba ahí rompe
// al cliente (lee cada línea como un mensaje). Los logs van a stderr, y
// OpenSCAD —que imprime por stdout— corre siempre con la salida capturada.
//
// **Sin puerto TCP.** El servidor habla por los descriptores del proceso que lo
// lanzó (el agente), así que no hay nada a lo que conectarse desde afuera.

// Versiones del protocolo que se hablan. Se responde la más nueva que el
// cliente pida y conozcamos; si pide una anterior a 2025-06-18 (o algo que no es
// una fecha), la base.
const (
	protocolBase   = "2024-11-05"
	protocolLatest = "2025-06-18"
)

// negotiate elige la versión que se declara en initialize. Las versiones de MCP
// son fechas ISO, así que comparar el texto es comparar fechas.
func negotiate(requested string) string {
	if len(requested) == len(protocolLatest) && requested >= protocolLatest {
		return protocolLatest
	}
	return protocolBase
}

// Códigos estándar de JSON-RPC.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// response lleva `id` sin omitempty: un error de parseo se contesta con id
// null, y un RawMessage nil se serializa justamente como null.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handlerFunc ejecuta una herramienta. Un error se le devuelve al agente como
// contenido con isError, no como fallo de JSON-RPC: tiene que poder leer «el
// checkpoint no existe» y corregirlo, no recibir un -32603 opaco.
type handlerFunc func(ctx context.Context, args map[string]any) (*toolResult, error)

// Tool es una herramienta expuesta al agente.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Annotations map[string]any `json:"annotations,omitempty"`
	handler     handlerFunc

	// Short y ShortSchema son la descripción (1-2 frases) y el esquema mínimo de
	// las herramientas del perfil esencial (ProfileEssential, S17): un modelo de 8k
	// de contexto no puede pagar el catálogo completo. Vacíos = la herramienta no
	// está en el perfil esencial. No viajan por el servidor `--mcp`, que ofrece
	// siempre el catálogo completo.
	Short       string         `json:"-"`
	ShortSchema map[string]any `json:"-"`
}

// toolResult es el resultado de una herramienta, en el formato de contenido de
// MCP (texto e imágenes).
type toolResult struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

func textContent(s string) map[string]any {
	return map[string]any{"type": "text", "text": s}
}

func imageContent(png []byte, mime string) map[string]any {
	return map[string]any{"type": "image", "data": encodeBase64(png), "mimeType": mime}
}

func textResult(s string) *toolResult { return &toolResult{Content: []map[string]any{textContent(s)}} }

func errorResult(s string) *toolResult {
	return &toolResult{Content: []map[string]any{textContent(s)}, IsError: true}
}

// jsonResult serializa v con sangría: lo lee un modelo, y un JSON en una línea
// de 3 KB se lee peor que uno con estructura.
func jsonResult(v any) (*toolResult, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return textResult(string(bytes.TrimRight(buf.Bytes(), "\n"))), nil
}

// server atiende a un cliente MCP sobre un par de streams.
type server struct {
	name         string
	version      string
	instructions string
	tools        []Tool
	log          *log.Logger

	outMu sync.Mutex
	enc   *json.Encoder

	inflightMu sync.Mutex
	inflight   map[string]context.CancelFunc // id de la petición (texto JSON) → su cancelación
}

func newServer(name, version, instructions string, tools []Tool, lg *log.Logger) *server {
	return &server{name: name, version: version, instructions: instructions, tools: tools, log: lg, inflight: map[string]context.CancelFunc{}}
}

// send escribe un mensaje. Las respuestas las escriben varias goroutines (las
// herramientas largas corren aparte para no bloquear ping ni cancelaciones) y
// dos escrituras encimadas darían una línea ilegible.
func (s *server) send(v any) error {
	s.outMu.Lock()
	defer s.outMu.Unlock()
	return s.enc.Encode(v)
}

// serve lee peticiones de r y escribe respuestas en w hasta que r se cierra. Una
// línea ilegible se contesta con un error de parseo y NO corta la sesión.
//
// Al llegar a EOF espera a las herramientas en curso en vez de cancelarlas:
// `printf '…' | scadlab --mcp` es una forma legítima de probar el servidor y
// perdería la respuesta. El plazo de un render lo acota igual (ctx se cancela al
// terminar el proceso o por señal).
func (s *server) serve(ctx context.Context, r io.Reader, w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	s.enc = enc

	scanner := bufio.NewScanner(r)
	// Un mensaje puede traer un fuente entero (create_checkpoint): el buffer de
	// 64 KB por defecto se queda corto.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var wg sync.WaitGroup
	var writeErr error
	var errMu sync.Mutex
	fail := func(err error) {
		if err != nil {
			errMu.Lock()
			if writeErr == nil {
				writeErr = err
			}
			errMu.Unlock()
		}
	}

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			fail(s.send(response{JSONRPC: "2.0", Error: &rpcError{Code: codeParseError, Message: i18n.T(i18n.Msg{ES: "JSON inválido", EN: "invalid JSON"})}}))
			continue
		}
		if req.Method == "" {
			// Una respuesta del cliente o basura: no hay nada que contestar a
			// menos que traiga id.
			if len(req.ID) > 0 {
				fail(s.send(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInvalidRequest, Message: i18n.T(i18n.Msg{ES: "falta el método", EN: "missing method"})}}))
			}
			continue
		}
		if len(req.ID) == 0 { // notificación: no lleva respuesta
			s.notification(req)
			continue
		}
		if req.Method == "tools/call" {
			callCtx, cancel := context.WithCancel(ctx)
			key := string(req.ID)
			s.inflightMu.Lock()
			s.inflight[key] = cancel
			s.inflightMu.Unlock()
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					s.inflightMu.Lock()
					delete(s.inflight, key)
					s.inflightMu.Unlock()
					cancel()
				}()
				fail(s.send(s.callTool(callCtx, req)))
			}()
			continue
		}
		fail(s.send(s.handle(req)))
	}
	scanErr := scanner.Err()
	wg.Wait()
	if scanErr != nil {
		return scanErr
	}
	return writeErr
}

// notification procesa un mensaje sin id. Solo importa la cancelación: el
// cliente avisa que ya no quiere el resultado de una petición larga (un render
// de minutos) y se mata el OpenSCAD en vez de dejarlo consumiendo CPU.
func (s *server) notification(req request) {
	if req.Method != "notifications/cancelled" {
		return
	}
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(req.Params, &p) != nil || len(p.RequestID) == 0 {
		return
	}
	s.inflightMu.Lock()
	cancel := s.inflight[string(p.RequestID)]
	s.inflightMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (s *server) handle(req request) response {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		result := map[string]any{
			"protocolVersion": negotiate(p.ProtocolVersion),
			// Solo `tools`: declarar resources o prompts que no se atienden
			// haría que el cliente pregunte y se lleve un error. `listChanged`
			// es false porque el catálogo es fijo.
			"capabilities": map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":   map[string]any{"name": s.name, "version": s.version},
		}
		if s.instructions != "" {
			result["instructions"] = s.instructions
		}
		resp.Result = result
	case "tools/list":
		resp.Result = map[string]any{"tools": s.tools}
	case "ping":
		resp.Result = map[string]any{}
	default:
		resp.Error = &rpcError{Code: codeMethodNotFound, Message: i18n.T(i18n.Msg{ES: "método no soportado: %s", EN: "unsupported method: %s"}, req.Method)}
	}
	return resp
}

// callTool ejecuta una herramienta. Un nombre desconocido o argumentos que no
// son un objeto son errores de protocolo (-32602); todo lo que falla DENTRO de
// la herramienta vuelve como contenido con isError.
func (s *server) callTool(ctx context.Context, req request) response {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		resp.Error = &rpcError{Code: codeInvalidParams, Message: i18n.T(i18n.Msg{ES: "parámetros inválidos", EN: "invalid params"})}
		return resp
	}
	var tool *Tool
	for i := range s.tools {
		if s.tools[i].Name == p.Name {
			tool = &s.tools[i]
			break
		}
	}
	if tool == nil {
		resp.Error = &rpcError{Code: codeInvalidParams, Message: i18n.T(i18n.Msg{ES: "herramienta desconocida: %s", EN: "unknown tool: %s"}, p.Name)}
		return resp
	}
	args := map[string]any{}
	if t := bytes.TrimSpace(p.Arguments); len(t) > 0 && string(t) != "null" {
		if err := json.Unmarshal(t, &args); err != nil {
			resp.Error = &rpcError{Code: codeInvalidParams, Message: i18n.T(i18n.Msg{ES: "los argumentos tienen que ser un objeto JSON", EN: "arguments must be a JSON object"})}
			return resp
		}
	}
	res, err := s.run(ctx, tool, args)
	if err != nil {
		res = errorResult(err.Error())
	}
	resp.Result = res
	return resp
}

// run protege al servidor de un panic dentro de una herramienta: un agente que
// pide algo raro no debe tumbar la sesión entera (y con ella las demás
// llamadas en vuelo).
func (s *server) run(ctx context.Context, tool *Tool, args map[string]any) (res *toolResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Printf("panic en la herramienta %s: %v", tool.Name, r)
			res, err = nil, i18n.Errorf(i18n.Msg{ES: "error interno en la herramienta %s", EN: "internal error in tool %s"}, tool.Name)
		}
	}()
	return tool.handler(ctx, normalizeArgs(tool, args))
}
