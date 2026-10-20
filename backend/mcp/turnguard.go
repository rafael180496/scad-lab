package mcp

import (
	"context"

	"scadlab/backend/i18n"
	"scadlab/backend/turnmark"
)

// «Solo consulta» en Antigravity (S12.c, T14). Antigravity aprueba las herramientas
// de ScadLab con reglas globales (`mcp(scadlab/<herramienta>)` en su settings.json),
// iguales para «Agente» y «Solo consulta»: el modo no puede viajar en la regla. Lo
// hace cumplir este servidor. Durante un turno de consulta el chat deja una marca en
// el workspace (backend/turnmark) y, antes de ejecutar una herramienta de ESCRITURA,
// el servidor la lee: si el turno es de consulta, la herramienta se rechaza con el
// motivo. Sin marca (o con una vencida) todo pasa, como siempre.
//
// Solo lo aplica el servidor `--mcp`, el que lanza Antigravity (y cualquier otro
// cliente MCP): la caja en proceso de Hermes decide el modo por su cuenta y no lee
// la marca. «Escritura» es la definición de effects.go (toda herramienta con efecto),
// que una prueba obliga a coincidir con `clichat.WriteTools()`.

var msgTurnReadOnly = i18n.Msg{
	ES: "Este turno es «Solo consulta» y la herramienta «%s» de ScadLab modifica el proyecto: no está permitida. Entrega el diseño en UN bloque ```openscad con el código completo y la cabecera `// @checkpoint`; ScadLab ofrece guardarlo.",
	EN: "This turn is in “Query only” mode and ScadLab's “%s” tool changes the project: it is not allowed. Deliver the design in ONE ```openscad block with the complete code and the `// @checkpoint` header; ScadLab offers to save it.",
}

// guardTurnMode envuelve el handler de cada herramienta de escritura con la
// comprobación de la marca del turno. Las de lectura quedan intactas.
func (e *env) guardTurnMode(tools []Tool) []Tool {
	for i := range tools {
		name, inner := tools[i].Name, tools[i].handler
		if EffectOf(name) == EffectNone || inner == nil {
			continue
		}
		tools[i].handler = func(ctx context.Context, args map[string]any) (*toolResult, error) {
			if turnmark.ReadOnly(e.ws) {
				return nil, i18n.Errorf(msgTurnReadOnly, name)
			}
			return inner(ctx, args)
		}
	}
	return tools
}
