// Package clichat corre los CLIs agénticos (Claude Code, Codex, Antigravity y
// OpenCode) en modo NO interactivo y traduce lo que imprimen a un solo
// vocabulario de eventos, para usarlos como proveedores del chat de diseño (S12.b).
//
// Es el backend/agentchat de mini-tools (adapters.go, opencode.go, rawtext.go,
// session.go, codetools.go) y backend/agents + backend/agentmodels portados, no
// reinventados. Lo que cambia respecto de mini-tools:
//
//   - Dos modos, no cinco: «Solo consulta» (el ask de mini-tools) y «Agente»
//     (su edit). En ninguno el CLI puede correr comandos de shell y no se porta
//     la aprobación por acción: ScadLab no tiene un canal para preguntarla.
//   - El CLI corre en la carpeta del workspace y con el servidor MCP de ScadLab
//     conectado; los flags los arma quien llama (la app) y aquí solo se colocan.
//   - Un evento de resultado de herramienta (KindToolResult): mini-tools solo
//     mostraba la llamada; el chat de ScadLab muestra el paso con su resultado.
//   - Cancelar mata el grupo de procesos (macOS) o el árbol (Windows): un CLI de
//     node deja vivos los servidores MCP que lanzó.
//
// Los formatos son de otros programas y cambian sin avisar: un adaptador nunca
// aborta el turno por una línea que no entiende, la descarta.
package clichat

// EventKind es el vocabulario común al que se traducen los cuatro formatos. Es
// deliberadamente chico: lo que no se pueda expresar con honestidad, no se muestra.
type EventKind string

const (
	// KindStart abre el turno con lo que el CLI informa de sí mismo.
	KindStart EventKind = "start"
	// KindText es texto de la respuesta, incremental.
	KindText EventKind = "text"
	// KindThinking es razonamiento, cuando el CLI lo distingue del texto. El
	// chat no lo muestra (igual que Hermes con los modelos de servidor).
	KindThinking EventKind = "thinking"
	// KindTool es una llamada a herramienta que arranca.
	KindTool EventKind = "tool"
	// KindToolResult es el resultado de una llamada anterior (mismo Tool.ID).
	KindToolResult EventKind = "toolresult"
	// KindDone cierra el turno con el uso total.
	KindDone EventKind = "done"
	// KindError es un fallo del proceso o de la conversación.
	KindError EventKind = "error"
)

// Usage son los tokens de un turno, con los nombres del modelo común: cada
// adaptador traduce los suyos.
type Usage struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	CacheRead int64 `json:"cacheRead"`
	// CacheWrite son los tokens escritos en la caché del prompt (Anthropic).
	// Input ya los incluye, igual que a CacheRead no.
	CacheWrite int64 `json:"cacheWrite"`
	Thinking   int64 `json:"thinking"`
	Total      int64 `json:"total"`
	// CostUSD solo lo informan Claude Code y OpenCode; 0 es «no lo dice», no «gratis».
	CostUSD float64 `json:"costUsd"`
	// Model es el modelo que respondió, si el CLI lo dice al cerrar (OpenCode).
	Model string `json:"model,omitempty"`
}

// ToolCall es una llamada a herramienta ya normalizada, o su resultado.
type ToolCall struct {
	// ID empareja la llamada con su resultado. Vacío en un CLI que no lo da: en
	// ese caso el paso queda abierto hasta que el turno termina.
	ID string `json:"id"`
	// Name es el nombre sin el prefijo del servidor MCP de ScadLab (`render`, no
	// `mcp__scadlab__render`): así coincide con las etiquetas de los pasos de Hermes.
	Name string `json:"name"`
	// Input son los argumentos serializados y recortados; Args, los mismos como
	// objeto cuando el CLI los dio como un objeto JSON.
	Input string         `json:"input"`
	Args  map[string]any `json:"args,omitempty"`
	// Result, Images e IsError solo vienen en un KindToolResult.
	Result  string   `json:"result,omitempty"`
	Images  []string `json:"images,omitempty"`
	IsError bool     `json:"isError,omitempty"`
}

// Event es lo que produce un adaptador.
type Event struct {
	Kind EventKind
	Text string
	Tool *ToolCall
	// Usage viene en KindDone (y, en un fallo, si el CLI lo alcanzó a decir).
	Usage *Usage
	// ConversationID es el id que devuelve el CLI, necesario para encadenar el
	// turno siguiente. Llega con KindStart o con KindDone según el CLI.
	ConversationID string
	// Model, Tools y MCPServers los informa el CLI al arrancar: es SU visión.
	Model      string
	Tools      []string
	MCPServers []string
	// Error acompaña a KindError.
	Error string
}
