package clichat

import (
	"strings"
	"testing"
)

// Pruebas del adaptador de Antigravity para las herramientas MCP (S12.c, T14):
// `call_mcp_tool` se muestra como la herramienta de ScadLab que es, y el detalle
// del rechazo de permisos llega al paso. Ninguna lanza el `agy` real.

// ── Adaptador: call_mcp_tool ───────────────────────────────────────────────

func TestAntigravityAdapterUnwrapsScadlabMCPCalls(t *testing.T) {
	adapt, _, _ := adapterFor("antigravity")
	evs := feed(t, adapt, "antigravity-mcp.jsonl")

	tools := find(evs, KindTool)
	if len(tools) != 3 {
		t.Fatalf("herramientas: %+v", tools)
	}
	if n := tools[0].Tool.Name; n != "printer_info" {
		t.Errorf("la llamada genérica se muestra como la herramienta real: %q", n)
	}
	cc := tools[1].Tool
	if cc.Name != "create_checkpoint" || cc.Args["label"] != "Caja" || cc.Args["source"] != "cube(10);" {
		t.Errorf("create_checkpoint con sus argumentos: %+v", cc)
	}
	if _, nested := cc.Args["ToolName"]; nested {
		t.Errorf("los argumentos son los de la herramienta, no el envoltorio: %+v", cc.Args)
	}
	// Otro servidor: se deja como llega, ScadLab no lo conoce.
	if gh := tools[2].Tool; gh.Name != "call_mcp_tool" || gh.Args["ServerName"] != "github" {
		t.Errorf("otro servidor intacto: %+v", gh)
	}
	// Los resultados emparejan por el id del paso.
	res := find(evs, KindToolResult)
	if len(res) != 3 || !strings.Contains(res[1].Tool.Result, "001-caja.scad") || res[1].Tool.ID != tools[1].Tool.ID {
		t.Errorf("resultados: %+v", res)
	}
	if text(evs) != "Listo: 001-caja.scad." {
		t.Errorf("texto: %q", text(evs))
	}
}

func TestUnwrapAgyMCPLeavesOtherThingsAlone(t *testing.T) {
	if unwrapAgyMCP(nil) != nil {
		t.Error("nil")
	}
	plain := &ToolCall{Name: "view_file", Args: map[string]any{"ServerName": "scadlab", "ToolName": "render"}}
	if got := unwrapAgyMCP(plain); got.Name != "view_file" {
		t.Errorf("solo call_mcp_tool se desenvuelve: %+v", got)
	}
	noTool := &ToolCall{Name: "call_mcp_tool", Args: map[string]any{"ServerName": "scadlab"}}
	if got := unwrapAgyMCP(noTool); got.Name != "call_mcp_tool" {
		t.Errorf("sin ToolName no hay nada que mostrar: %+v", got)
	}
	noArgs := &ToolCall{Name: "call_mcp_tool"}
	if got := unwrapAgyMCP(noArgs); got.Name != "call_mcp_tool" {
		t.Errorf("sin argumentos: %+v", got)
	}
	noInner := &ToolCall{Name: "call_mcp_tool", Args: map[string]any{"serverName": "scadlab", "tool_name": "lint"}}
	if got := unwrapAgyMCP(noInner); got.Name != "lint" || got.Args != nil {
		t.Errorf("sin Arguments: %+v", got)
	}
}

func TestAntigravityAdapterKeepsThePermissionDenialDetail(t *testing.T) {
	adapt, _, _ := adapterFor("antigravity")
	evs := feed(t, adapt, "antigravity-denied.jsonl")
	res := find(evs, KindToolResult)
	if len(res) != 2 || !res[1].Tool.IsError {
		t.Fatalf("resultados: %+v", res)
	}
	if !strings.Contains(res[1].Tool.Result, `permission check failed for mcp "scadlab/printer_info"`) {
		t.Errorf("el detalle real del rechazo llega al paso: %q", res[1].Tool.Result)
	}
	// El turno termina sin texto: no se pierde nada en el adaptador, Antigravity
	// no dijo nada (cerró en la primera herramienta rechazada).
	if text(evs) != "" || len(find(evs, KindDone)) != 1 {
		t.Errorf("sin texto y con cierre: %q %v", text(evs), kinds(evs))
	}
}
