package hermes

import (
	"encoding/json"
	"unicode/utf8"

	"scadlab/backend/llm"
)

// EstimateTokens estima los tokens de un texto como caracteres/4, redondeando
// hacia arriba: la misma regla que usa el contador del chat (lib/chatContext.ts) y
// la que usa la prueba del Ollama de 8k. Es una estimación —un tokenizador real
// cuenta distinto el código y el español—, pero sirve para decidir cuánto cabe.
func EstimateTokens(s string) int {
	return (utf8.RuneCountInString(s) + 3) / 4
}

// estimateMessage son los tokens aproximados de un mensaje: su texto y los
// argumentos de las llamadas que pidió (el código de un create_checkpoint es lo
// que más pesa en un turno largo).
func estimateMessage(m llm.Message) int {
	n := EstimateTokens(m.Content)
	for _, c := range m.ToolCalls {
		n += EstimateTokens(c.Name) + EstimateTokens(argsJSON(c.Arguments))
	}
	return n
}

func argsJSON(args map[string]any) string {
	raw, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return string(raw)
}

// EstimateToolDefs son los tokens aproximados de las definiciones de
// herramientas que viajan en CADA pedido (nombre, descripción y esquema).
func EstimateToolDefs(defs []llm.ToolDef) int {
	n := 0
	for _, d := range defs {
		raw, _ := json.Marshal(d.Parameters)
		n += EstimateTokens(d.Name) + EstimateTokens(d.Description) + EstimateTokens(string(raw)) + 8
	}
	return n
}

// EstimateRequest son los tokens aproximados de un pedido completo.
func EstimateRequest(req llm.ChatRequest) int {
	n := EstimateToolDefs(req.Tools)
	for _, m := range req.Messages {
		n += estimateMessage(m) + 4 // el marco de cada mensaje
	}
	return n
}
