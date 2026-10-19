package clichat

import (
	"encoding/json"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
)

// Adaptadores: una función por CLI que traduce UNA línea de su salida a cero o
// más eventos comunes.
//
// La forma es «línea → []Event» y no un parser con estado porque los formatos
// son NDJSON sin contexto entre líneas: cada una se entiende sola. Una línea que
// no se entiende devuelve nil y se descarta — nunca aborta el turno, porque un
// evento nuevo que agregue el CLI en su próxima versión no puede ser motivo de
// que la conversación se corte. (OpenCode es la excepción con estado: ver opencode.go.)

// maxToolInput recorta el argumento de una herramienta que se muestra como
// texto. Los argumentos como objeto (ToolCall.Args) van enteros: los recorta
// quien los muestra.
const maxToolInput = 400

type adapter func(line []byte) []Event

// mcpServerName es el nombre del servidor MCP de ScadLab ante los CLIs (el
// mismo de backend/mcp.ServerName; una prueba de la app comprueba que coincida).
const mcpServerName = "scadlab"

// --- Claude Code ----------------------------------------------------------

type claudeLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	Model   string `json:"model"`
	// SessionID es el id de conversación para encadenar con --resume.
	SessionID string   `json:"session_id"`
	Tools     []string `json:"tools"`
	MCP       []struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	} `json:"mcp_servers"`
	Message struct {
		Model   string          `json:"model"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
	Usage      claudeUsage `json:"usage"`
	TotalCost  float64     `json:"total_cost_usd"`
	IsError    bool        `json:"is_error"`
	Result     string      `json:"result"`
	StopReason string      `json:"stop_reason"`
}

type claudeBlock struct {
	Type     string          `json:"type"`
	Text     string          `json:"text"`
	Thinking string          `json:"thinking"`
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	// tool_result
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type claudeUsage struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
	Details    struct {
		Thinking int64 `json:"thinking_tokens"`
	} `json:"output_tokens_details"`
}

func claudeAdapter(line []byte) []Event {
	var l claudeLine
	if err := json.Unmarshal(line, &l); err != nil {
		return nil
	}

	switch l.Type {
	case "system":
		if l.Subtype != "init" {
			return nil
		}
		// El init trae SU visión de los servidores MCP, con el estado real de
		// cada uno: ahí se ve si el de ScadLab quedó conectado.
		servers := make([]string, 0, len(l.MCP))
		for _, s := range l.MCP {
			servers = append(servers, s.Name+" ("+s.Status+")")
		}
		return []Event{{Kind: KindStart, Model: l.Model, Tools: l.Tools, MCPServers: servers, ConversationID: l.SessionID}}

	case "assistant":
		var out []Event
		for _, c := range claudeBlocks(l.Message.Content) {
			switch c.Type {
			case "text":
				if c.Text != "" {
					out = append(out, Event{Kind: KindText, Text: c.Text})
				}
			case "thinking":
				if t := firstNonEmpty(c.Thinking, c.Text); t != "" {
					out = append(out, Event{Kind: KindThinking, Text: t})
				}
			case "tool_use":
				out = append(out, Event{Kind: KindTool, Tool: newToolCall(c.ID, c.Name, string(c.Input))})
			}
		}
		return out

	case "user":
		// Los resultados de las herramientas vuelven como un mensaje «user» con
		// bloques tool_result.
		var out []Event
		for _, c := range claudeBlocks(l.Message.Content) {
			if c.Type != "tool_result" {
				continue
			}
			text, images := resultContent(c.Content)
			out = append(out, Event{Kind: KindToolResult, Tool: &ToolCall{ID: c.ToolUseID, Result: text, Images: images, IsError: c.IsError}})
		}
		return out

	case "result":
		u := Usage{
			Input:     l.Usage.Input + l.Usage.CacheWrite,
			Output:    l.Usage.Output,
			CacheRead: l.Usage.CacheRead,
			// CacheWrite se informa aparte pero ya está sumado en Input.
			CacheWrite: l.Usage.CacheWrite,
			Thinking:   l.Usage.Details.Thinking,
			CostUSD:    l.TotalCost,
		}
		u.Total = u.Input + u.Output + u.CacheRead
		if l.IsError {
			return []Event{{Kind: KindError, Error: firstNonEmpty(l.Result, i18n.T(msgAgentFailed)), Usage: &u, ConversationID: l.SessionID}}
		}
		return []Event{{Kind: KindDone, Usage: &u, ConversationID: l.SessionID}}
	}
	return nil
}

// claudeBlocks lee el contenido de un mensaje: una lista de bloques, o un texto
// suelto (el eco del prompt), que no trae nada que mostrar.
func claudeBlocks(raw json.RawMessage) []claudeBlock {
	var blocks []claudeBlock
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &blocks) != nil {
		return nil
	}
	return blocks
}

// --- Antigravity ----------------------------------------------------------

type agyLine struct {
	Event string `json:"event"`
	Init  *struct {
		Cwd   string   `json:"cwd"`
		Tools []string `json:"tools"`
		Model string   `json:"model"`
	} `json:"init"`
	ConversationID string `json:"conversation_id"`
	StepUpdate     *struct {
		ConversationID string `json:"conversation_id"`
		StepIndex      int    `json:"step_index"`
		StepType       string `json:"step_type"`
		State          string `json:"state"`
		// TextDelta se lee CRUDO. Ver rawtext.go: el decodificador estándar
		// reemplaza por U+FFFD el byte suelto de un carácter que quedó partido
		// entre dos trozos, y ahí ya no hay nada que recuperar.
		TextDelta json.RawMessage `json:"text_delta"`
		ToolName  string          `json:"tool_name"`
		ToolInput any             `json:"tool_input"`
		Usage     *agyUsage       `json:"usage"`
	} `json:"step_update"`
	Result *struct {
		ConversationID string    `json:"conversation_id"`
		Status         string    `json:"status"`
		Response       string    `json:"response"`
		Usage          *agyUsage `json:"usage"`
	} `json:"result"`
}

type agyUsage struct {
	Input     int64 `json:"input_tokens"`
	Output    int64 `json:"output_tokens"`
	Thinking  int64 `json:"thinking_tokens"`
	CacheRead int64 `json:"cache_read_tokens"`
	Total     int64 `json:"total_tokens"`
}

func (u *agyUsage) common() *Usage {
	if u == nil {
		return nil
	}
	return &Usage{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, Thinking: u.Thinking, Total: u.Total}
}

func antigravityAdapter(line []byte) []Event {
	var l agyLine
	if err := json.Unmarshal(line, &l); err != nil {
		return nil
	}

	switch {
	case l.Init != nil:
		return []Event{{Kind: KindStart, Model: l.Init.Model, Tools: l.Init.Tools, ConversationID: l.ConversationID}}

	case l.StepUpdate != nil:
		s := l.StepUpdate
		// El texto llega como deltas incrementales, así que se reenvía tal cual y
		// quien lo muestra concatena. Acumularlo acá para mandarlo entero al
		// final tiraría abajo lo único que hace que un chat se sienta vivo.
		switch s.StepType {
		case "agent_response":
			if delta := jsonRawString(s.TextDelta); delta != "" {
				return []Event{{Kind: KindText, Text: delta}}
			}
		case "user_input", "checkpoint", "unknown":
			// Pasos internos: no son nada que mostrar.
			return nil
		default:
			// Cualquier otro step_type es una acción. Se toma el step_type como
			// nombre cuando no hay uno explícito: es preferible mostrar «algo
			// pasó y se llamaba así» a tragarse la acción entera. El índice del
			// paso empareja el arranque (ACTIVE, que puede repetirse) con el cierre.
			id := "step-" + strconv.Itoa(s.StepIndex)
			switch s.State {
			case "ACTIVE":
				name := firstNonEmpty(s.ToolName, s.StepType)
				return []Event{{Kind: KindTool, Tool: newToolCall(id, name, rawJSON(s.ToolInput))}}
			case "DONE":
				return []Event{{Kind: KindToolResult, Tool: &ToolCall{ID: id}}}
			case "ERROR", "FAILED", "CANCELLED":
				return []Event{{Kind: KindToolResult, Tool: &ToolCall{ID: id, IsError: true}}}
			}
		}
		return nil

	case l.Result != nil:
		u := l.Result.Usage.common()
		if l.Result.Status != "" && l.Result.Status != "SUCCESS" {
			return []Event{{Kind: KindError, Error: i18n.T(i18n.Msg{ES: "el agente terminó con estado %s", EN: "the agent finished with status %s"}, l.Result.Status), Usage: u, ConversationID: l.Result.ConversationID}}
		}
		return []Event{{Kind: KindDone, Usage: u, ConversationID: l.Result.ConversationID}}
	}
	return nil
}

// --- Codex ----------------------------------------------------------------

// codexLine es una línea de `codex exec --json`: eventos con `type` en
// `thread.started|turn.started|item.started|item.completed|turn.completed|
// turn.failed`, donde lo que pasó vive en `item` y el uso llega al cerrar el turno.
type codexLine struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Item     *struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		// Text lo trae agent_message.
		Text string `json:"text"`
		// Command y su salida los trae command_execution.
		Command    string `json:"command"`
		Aggregated string `json:"aggregated_output"`
		ExitCode   *int   `json:"exit_code"`
		Status     string `json:"status"`
		// Los cambios de archivo traen su ruta.
		Path    string `json:"path"`
		Changes any    `json:"changes"`
		// mcp_tool_call: el servidor, la herramienta, sus argumentos y su resultado.
		Server    string          `json:"server"`
		Tool      string          `json:"tool"`
		Arguments json.RawMessage `json:"arguments"`
		Result    json.RawMessage `json:"result"`
		Error     json.RawMessage `json:"error"`
	} `json:"item"`
	Usage *struct {
		Input      int64 `json:"input_tokens"`
		Cached     int64 `json:"cached_input_tokens"`
		CacheWrite int64 `json:"cache_write_input_tokens"`
		Output     int64 `json:"output_tokens"`
		Reasoning  int64 `json:"reasoning_output_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func codexAdapter(line []byte) []Event {
	var l codexLine
	if err := json.Unmarshal(line, &l); err != nil {
		return nil
	}

	switch l.Type {
	case "thread.started":
		// El thread_id es lo que permite retomar la conversación
		// (`codex exec resume <id>`).
		return []Event{{Kind: KindStart, ConversationID: l.ThreadID}}

	case "item.started", "item.completed":
		if l.Item == nil {
			return nil
		}
		it := l.Item
		switch it.Type {
		case "agent_message":
			// Solo al completarse: el mensaje no llega en deltas, así que
			// emitirlo dos veces lo duplicaría en pantalla.
			if l.Type == "item.completed" && it.Text != "" {
				return []Event{{Kind: KindText, Text: it.Text}}
			}
			return nil
		case "reasoning":
			if l.Type == "item.completed" && it.Text != "" {
				return []Event{{Kind: KindThinking, Text: it.Text}}
			}
			return nil
		}
		// Toda otra clase de item es una acción. Se muestra al EMPEZAR (que es
		// lo que hace que se vea trabajando en vez de aparecer todo junto al
		// final) y se cierra al completarse.
		name, input, args := codexAction(it.Type, it.Command, it.Path, it.Tool, it.Arguments)
		if l.Type == "item.started" {
			tc := newToolCall(it.ID, name, input)
			if args != nil {
				tc.Args = args
			}
			return []Event{{Kind: KindTool, Tool: tc}}
		}
		res := &ToolCall{ID: it.ID}
		switch {
		case it.Type == "mcp_tool_call":
			res.Result, res.Images = resultContent(it.Result)
			if msg := errorText(it.Error); msg != "" {
				res.IsError, res.Result = true, msg
			}
		default:
			res.Result = it.Aggregated
			res.IsError = it.Status == "failed" || (it.ExitCode != nil && *it.ExitCode != 0)
		}
		return []Event{{Kind: KindToolResult, Tool: res}}

	case "turn.completed":
		u := Usage{}
		if l.Usage != nil {
			u = Usage{
				// `input_tokens` de Codex ya incluye los cacheados, así que
				// restarlos es lo que hace comparable el número con el de los
				// otros CLIs — si no, la entrada se contaría dos veces.
				Input:     l.Usage.Input - l.Usage.Cached + l.Usage.CacheWrite,
				Output:    l.Usage.Output,
				CacheRead: l.Usage.Cached,
				Thinking:  l.Usage.Reasoning,
			}
			u.Total = u.Input + u.Output + u.CacheRead
		}
		return []Event{{Kind: KindDone, Usage: &u}}

	case "turn.failed":
		msg := i18n.T(msgAgentFailed)
		if l.Error != nil && l.Error.Message != "" {
			msg = l.Error.Message
		}
		return []Event{{Kind: KindError, Error: msg}}
	}
	return nil
}

// codexAction nombra una acción de Codex: la herramienta MCP por su nombre, y
// las demás por su clase (`command_execution`, `file_change`…).
func codexAction(typ, command, path, tool string, arguments json.RawMessage) (name, input string, args map[string]any) {
	if typ == "mcp_tool_call" && tool != "" {
		return tool, string(arguments), parseArgs(arguments)
	}
	raw := command
	if raw == "" {
		raw = path
	}
	return typ, raw, nil
}

var msgAgentFailed = i18n.Msg{ES: "el agente terminó con error", EN: "the agent finished with an error"}

// --- Registro -------------------------------------------------------------

// adapters son los CLIs con adaptador de una sola línea.
var adapters = map[string]adapter{
	"claude":      claudeAdapter,
	"antigravity": antigravityAdapter,
	"codex":       codexAdapter,
}

// finisher cierra un turno cuyo CLI terminó bien sin emitir un evento final
// propio. Corre después de que el proceso salió, así que puede leer lo que el
// CLI dejó guardado (el uso del turno, por ejemplo).
type finisher func(t Turn) []Event

// turnAdapters son los adaptadores que necesitan estado DURANTE un turno: uno
// nuevo por turno. Hoy solo OpenCode, que no cierra el turno en su stream.
var turnAdapters = map[string]func() (adapter, finisher){
	"opencode": newOpencodeTurn,
}

// adapterFor devuelve el adaptador de un turno nuevo y, si lo tiene, su cierre.
func adapterFor(cliID string) (adapter, finisher, bool) {
	if mk, ok := turnAdapters[cliID]; ok {
		a, f := mk()
		return a, f, true
	}
	a, ok := adapters[cliID]
	return a, nil, ok
}

// --- Utilidades -----------------------------------------------------------

// newToolCall arma la llamada con el nombre limpio y los argumentos
// (la cadena recortada para mostrarla y, si es un objeto JSON, el objeto).
func newToolCall(id, name, rawInput string) *ToolCall {
	tc := &ToolCall{ID: id, Name: CleanToolName(name), Input: trim(rawInput)}
	tc.Args = parseArgs(json.RawMessage(rawInput))
	return tc
}

// CleanToolName quita el prefijo con que un CLI nombra las herramientas del
// servidor MCP de ScadLab (`mcp__scadlab__render` en Claude Code, `scadlab_render`
// en OpenCode): el paso se llama `render`, como en Hermes.
func CleanToolName(name string) string {
	for _, p := range []string{"mcp__" + mcpServerName + "__", mcpServerName + "_", mcpServerName + "."} {
		if rest, ok := strings.CutPrefix(name, p); ok && rest != "" {
			return rest
		}
	}
	return name
}

// parseArgs devuelve el objeto JSON de los argumentos, o nil si no lo es.
func parseArgs(raw json.RawMessage) map[string]any {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || raw[0] != '{' {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// resultContent saca el texto (y las imágenes, como data URL) del contenido de
// un resultado de herramienta: una cadena, una lista de bloques `text`/`image`,
// o un objeto que trae esa lista en `content`.
func resultContent(raw json.RawMessage) (text string, images []string) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	switch raw[0] {
	case '"':
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return s, nil
		}
	case '[':
		var blocks []struct {
			Type   string `json:"type"`
			Text   string `json:"text"`
			Source struct {
				Type      string `json:"type"`
				MediaType string `json:"media_type"`
				Data      string `json:"data"`
			} `json:"source"`
			// Forma de MCP: {type:"image", data, mimeType}.
			Data     string `json:"data"`
			MimeType string `json:"mimeType"`
		}
		if json.Unmarshal(raw, &blocks) != nil {
			return string(raw), nil
		}
		var parts []string
		for _, b := range blocks {
			switch b.Type {
			case "text":
				if b.Text != "" {
					parts = append(parts, b.Text)
				}
			case "image":
				data, mime := b.Source.Data, b.Source.MediaType
				if data == "" {
					data, mime = b.Data, b.MimeType
				}
				if data != "" {
					if mime == "" {
						mime = "image/png"
					}
					images = append(images, "data:"+mime+";base64,"+data)
				}
			}
		}
		return strings.Join(parts, "\n"), images
	case '{':
		var obj struct {
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(raw, &obj) == nil && len(obj.Content) > 0 {
			return resultContent(obj.Content)
		}
	}
	return string(raw), nil
}

// errorText saca el mensaje de un error de herramienta: una cadena o un objeto
// con `message`. Vacío si no hay error.
func errorText(raw json.RawMessage) string {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && obj.Message != "" {
		return obj.Message
	}
	return string(raw)
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxToolInput {
		// Sin partir un carácter por la mitad.
		cut := maxToolInput
		for cut > 0 && !isRuneStart(s[cut]) {
			cut--
		}
		return s[:cut] + "…"
	}
	return s
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// rawJSON serializa el argumento sin recortarlo: el recorte lo hace newToolCall
// después de leerle las claves.
func rawJSON(v any) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
