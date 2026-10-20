package main

import (
	"scadlab/backend/hermes"
	"scadlab/backend/mcp"
	"scadlab/backend/prompts"
)

// El perfil de herramientas de un turno con Hermes (S17): lo que se declara al
// modelo y el prompt que lo acompaña. La decisión (Auto / Esenciales / Todas) está en
// resolveToolProfile (app_chat_context.go); aquí se aplica.

// toolSpecsFor devuelve las herramientas que se declaran en el perfil esencial, o
// nil si el perfil es el completo o la caja no sabe ofrecer un perfil (la caja de
// mentira de una prueba): en ese caso Hermes declara todo el catálogo.
func toolSpecsFor(tools hermes.Toolset, profile string) []mcp.ToolSpec {
	if profile != profileEssential {
		return nil
	}
	pt, ok := tools.(interface {
		ToolsFor(mcp.ToolProfile) []mcp.ToolSpec
	})
	if !ok {
		return nil
	}
	return pt.ToolsFor(mcp.ProfileEssential)
}

// setCompactPrompt pide el prompt de sistema compacto (assets/prompts/agent-compact.*):
// las mismas reglas en menos de 1 200 tokens, para una ventana chica.
func setCompactPrompt(ctx *prompts.AgentContext, on bool) { ctx.Compact = on }
