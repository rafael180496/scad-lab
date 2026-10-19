// Package hermes es el bucle agéntico del chat de ScadLab: manda la
// conversación a un modelo con las herramientas de OpenSCAD (la caja de
// herramientas de backend/mcp), ejecuta las que el modelo pide, le devuelve los
// resultados —y la imagen de `preview` si el modelo ve— y repite hasta que
// contesta con texto, llega al tope de pasos o lo cancelan.
//
// Es el runner de mini-tools (backend/agentchat/llmrunner.go) portado: un error
// de herramienta vuelve al modelo como «error: …» en vez de cortar el turno, el
// resultado se recorta, un modelo sin herramientas cae a modo texto con un
// aviso, y los errores de conexión transitorios se reintentan una vez. Lo
// específico de ScadLab es de dónde salen las herramientas (en proceso, no por
// MCP) y las imágenes (a un archivo, porque llm.Message.Images son rutas).
//
// No conoce Wails ni la base: recibe un llm.Client, una caja de herramientas y
// callbacks, y devuelve el resultado del turno. app_chat.go persiste y emite.
// Spec: .claude/specs/s12-hermes-proveedores.md.
package hermes

// Estados de un paso.
const (
	StepRunning = "running"
	StepOK      = "ok"
	StepError   = "error"
)

// Step es una llamada a herramienta dentro de un turno. Viaja al frontend por el
// evento chat:step y, al terminar el turno, se guarda dentro de los Parts del
// mensaje del asistente. Los nombres JSON son contrato con
// frontend/src/components/chat/chatStreams.ts.
type Step struct {
	// ID es único dentro del turno: el frontend actualiza por él el paso que
	// pasó de running a ok o error.
	ID string `json:"id"`
	// Index es el número del paso en el turno, desde 1.
	Index int    `json:"index"`
	Tool  string `json:"tool"`
	// Args son los argumentos que mandó el modelo (los textos muy largos van
	// recortados: ver capArgs).
	Args   map[string]any `json:"args"`
	Status string         `json:"status"`
	// Text es el resultado de la herramienta (o su error), recortado para la
	// pantalla. Lo que el modelo recibe es el resultado completo hasta
	// maxToolResultRunes.
	Text string `json:"text"`
	// Images son data URL (PNG en vivo; JPEG reducido al guardarlo).
	Images     []string `json:"images"`
	DurationMs int64    `json:"durationMs"`
	// TextAt es cuánto texto visible llevaba el turno cuando arrancó el paso, en
	// unidades UTF-16 (lo que cuenta `String.length` en JavaScript): el
	// frontend parte el texto ahí para intercalar los pasos donde ocurrieron.
	TextAt int `json:"textAt"`
}

// Usage son los tokens y el costo de un turno (suma de todas sus vueltas).
type Usage struct {
	Input      int64 `json:"inputTokens"`
	Output     int64 `json:"outputTokens"`
	CacheRead  int64 `json:"cacheRead"`
	CacheWrite int64 `json:"cacheWrite"`
	Thinking   int64 `json:"thinking"`
	// CostUSD solo se calcula con precios que el proveedor publicó (OpenRouter);
	// 0 significa «no informado», no «gratis».
	CostUSD float64 `json:"costUsd"`
}

// Price son los dólares por millón de tokens de un modelo; 0 = desconocido.
type Price struct {
	PromptPerM     float64
	CompletionPerM float64
}

// Cost calcula el costo de u. Sin precios conocidos es 0.
func (p Price) Cost(u Usage) float64 {
	if p.PromptPerM <= 0 && p.CompletionPerM <= 0 {
		return 0
	}
	return float64(u.Input)*p.PromptPerM/1e6 + float64(u.Output)*p.CompletionPerM/1e6
}

// utf16Len cuenta las unidades UTF-16 de s (el `length` de un string de JS).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}
