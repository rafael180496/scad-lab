package clichat

import (
	"context"
	"encoding/json"
	"os/exec"
	"strconv"
	"time"

	"scadlab/backend/i18n"
)

// --- OpenCode -------------------------------------------------------------
//
// `opencode run --format json`, formato verificado en mini-tools contra corridas
// reales. Una línea por evento con `type` en `step_start|text|reasoning|tool_use|
// step_finish|error`, el `sessionID` en todas, y lo que pasó dentro de `part`.
//
// Tres cosas que lo distinguen de los otros y que explican este archivo:
//
//   - **No cierra el turno.** El `step_finish` del ÚLTIMO paso no se emite: la
//     salida termina en el `text` de la respuesta y el proceso sale con 0. Por
//     eso este adaptador tiene estado y un cierre (finisher) que corre cuando el
//     proceso terminó bien; sin él, el chat quedaría «escribiendo» para siempre.
//   - **El uso se lee de la sesión, no del stream.** Sumar los `step_finish`
//     daría un total sin el último paso —que en una respuesta sin herramientas
//     es el único—, y un número parcial rotulado como total es peor que no darlo.
//     `opencode session export` trae los tokens y el modelo de cada mensaje, el
//     último incluido.
//   - **No hay un modo «solo lectura» por bandera.** Por defecto actúa: edita
//     archivos y corre comandos sin preguntar. El modo se arma con su agente
//     `plan` (no edita) y con su configuración de permisos por entorno — ver
//     opencodeEnv y buildArgs.

// opencodeLine es una línea del stream. Solo los campos que se usan.
type opencodeLine struct {
	Type      string `json:"type"`
	SessionID string `json:"sessionID"`
	Part      struct {
		ID     string `json:"id"`
		PartID string `json:"partID"`
		Text   string `json:"text"`
		Tool   string `json:"tool"`
		State  struct {
			Status string          `json:"status"`
			Input  json.RawMessage `json:"input"`
			Output json.RawMessage `json:"output"`
			Error  json.RawMessage `json:"error"`
		} `json:"state"`
	} `json:"part"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// opencodeTurn es el estado de UN turno: qué sesión se abrió (para encadenar y
// para leer el uso al final) y si el CLI ya informó un error.
type opencodeTurn struct {
	session string
	started bool
	failed  bool
	tools   int
}

func newOpencodeTurn() (adapter, finisher) {
	o := &opencodeTurn{}
	return o.line, o.finish
}

func (o *opencodeTurn) line(raw []byte) []Event {
	var l opencodeLine
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil
	}
	if l.SessionID != "" {
		o.session = l.SessionID
	}

	var out []Event
	// El primer evento con sesión abre el turno: es el id que permite retomar la
	// conversación (`--session`). Un solo start por turno, aunque haya un
	// step_start por cada vuelta del modelo.
	if !o.started && o.session != "" && l.Type != "error" {
		o.started = true
		out = append(out, Event{Kind: KindStart, ConversationID: o.session})
	}

	switch l.Type {
	case "text":
		if l.Part.Text != "" {
			out = append(out, Event{Kind: KindText, Text: l.Part.Text})
		}
	case "reasoning":
		if l.Part.Text != "" {
			out = append(out, Event{Kind: KindThinking, Text: l.Part.Text})
		}
	case "tool_use":
		// Llega UNA vez por llamada, ya resuelta (completed o error): no hay un
		// started aparte. Se emite la llamada y su resultado juntos.
		o.tools++
		id := firstNonEmpty(l.Part.ID, l.Part.PartID)
		if id == "" {
			id = "tool-" + strconv.Itoa(o.tools)
		}
		call := newToolCall(id, l.Part.Tool, string(l.Part.State.Input))
		out = append(out, Event{Kind: KindTool, Tool: call})
		res := &ToolCall{ID: id, IsError: l.Part.State.Status == "error"}
		res.Result, res.Images = resultContent(l.Part.State.Output)
		if res.IsError {
			if msg := errorText(l.Part.State.Error); msg != "" {
				res.Result = msg
			}
		}
		out = append(out, Event{Kind: KindToolResult, Tool: res})
	case "error":
		o.failed = true
		msg := i18n.T(msgAgentFailed)
		if l.Error != nil && l.Error.Message != "" {
			msg = l.Error.Message
		}
		out = append(out, Event{Kind: KindError, Error: msg, ConversationID: l.SessionID})
	}
	return out
}

// opencodeExportTimeout acota la lectura del uso. Es un proceso local que tarda
// menos de un segundo; si tarda más, el turno cierra sin uso en vez de dejar el
// chat esperando.
const opencodeExportTimeout = 15 * time.Second

// finish cierra el turno con el uso real de la sesión.
func (o *opencodeTurn) finish(t Turn) []Event {
	if o.failed {
		return nil
	}
	done := Event{Kind: KindDone, ConversationID: o.session}
	if o.session != "" {
		done.Usage = opencodeTurnUsage(t, o.session)
	}
	return []Event{done}
}

// opencodeTurnUsage lee la sesión exportada y suma los mensajes del asistente
// posteriores al último mensaje del usuario: eso es ESTE turno, aunque la sesión
// venga de antes. nil = no se pudo saber (no «cero tokens»).
func opencodeTurnUsage(t Turn, session string) *Usage {
	argv := Launcher(t.Exe)
	if t.Exe == "" || len(argv) == 0 || !validSession(session) {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), opencodeExportTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], append(argv[1:], "session", "export", session, "--standalone")...)
	cmd.Dir = t.Cwd
	cmd.Env = append(t.environ(), opencodeEnv(t)...)
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return opencodeUsageFromExport(out)
}

type opencodeTokens struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache"`
}

func opencodeUsageFromExport(raw []byte) *Usage {
	var doc struct {
		Messages []struct {
			Type  string `json:"type"`
			Model struct {
				ID         string `json:"id"`
				ProviderID string `json:"providerID"`
			} `json:"model"`
			Cost   float64         `json:"cost"`
			Tokens *opencodeTokens `json:"tokens"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}

	from := 0
	for i, m := range doc.Messages {
		if m.Type == "user" {
			from = i + 1
		}
	}
	var u Usage
	seen := false
	for _, m := range doc.Messages[from:] {
		if m.Type != "assistant" || m.Tokens == nil {
			continue
		}
		seen = true
		// `input` NO incluye lo leído de la caché (se comprobó: input + cache.read
		// es el prompt entero). Lo escrito en caché se suma a la entrada, igual
		// que en Claude Code, para que los números de los CLIs se comparen.
		u.Input += m.Tokens.Input + m.Tokens.Cache.Write
		u.CacheWrite += m.Tokens.Cache.Write
		u.Output += m.Tokens.Output
		u.CacheRead += m.Tokens.Cache.Read
		u.Thinking += m.Tokens.Reasoning
		u.CostUSD += m.Cost
		if m.Model.ID != "" {
			u.Model = m.Model.ID
			if m.Model.ProviderID != "" {
				u.Model = m.Model.ProviderID + "/" + m.Model.ID
			}
		}
	}
	if !seen {
		return nil
	}
	u.Total = u.Input + u.Output + u.CacheRead
	return &u
}

// opencodeConfig es la configuración de permisos de un turno (ver opencodeEnv).
// «Solo consulta» le niega a OpenCode las herramientas del servidor MCP de
// ScadLab que escriben (crear un checkpoint, exportar); OpenCode nombra una
// herramienta MCP `<servidor>_<herramienta>`.
func opencodeConfig(mode Mode, mcpOn bool) string {
	perm := map[string]string{"bash": "deny"}
	if mode != ModeEdit && mcpOn {
		for _, tool := range writeTools {
			perm[mcpServerName+"_"+tool] = "deny"
		}
	}
	raw, _ := json.Marshal(map[string]any{"permission": perm}) // un mapa de strings no falla
	return string(raw)
}

// opencodeEnv es lo que se le agrega al entorno de cada turno de OpenCode:
//
//   - OPENCODE_CONFIG_CONTENT, que OpenCode mezcla por encima de la del usuario.
//     Le quita la herramienta de comandos (`bash` en su configuración, `shell` en
//     el stream) en TODOS los modos: sin esto su modo por defecto corre cualquier
//     comando sin preguntar, y en headless no hay a quién preguntarle. «Solo
//     consulta» además le niega las herramientas de ScadLab que escriben.
//     Requiere `--standalone`: el servicio en segundo plano de OpenCode ya arrancó
//     con su propio entorno e ignora el de quien lo llama.
//   - PWD: OpenCode toma el directorio de trabajo de esa variable y no del
//     directorio real del proceso. Heredada de la app, apunta a donde se abrió
//     ScadLab, y el agente leería rutas relativas de otro proyecto.
func opencodeEnv(t Turn) []string {
	env := []string{"OPENCODE_CONFIG_CONTENT=" + opencodeConfig(t.Mode, t.MCPEnabled)}
	if t.Cwd != "" {
		env = append(env, "PWD="+t.Cwd)
	}
	return env
}
