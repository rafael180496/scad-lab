package clichat

import "strings"

// Mode es qué tiene permitido hacer el CLI en un turno. Son dos y no cinco a
// propósito: mini-tools ofrece ask, plan, approve, auto y edit, pero ScadLab no
// tiene un canal para aprobar acciones una por una y el modelo mental del chat
// ya es el interruptor «Agente» de Hermes:
//
//	«Solo consulta»  ModeAsk   el ask de mini-tools: lee, razona y usa las
//	                           herramientas de lectura de ScadLab. No escribe.
//	«Agente»         ModeEdit  el edit de mini-tools: además crea checkpoints y
//	                           usa todas las herramientas de ScadLab.
//
// **En ninguno corre comandos de shell.** Es la misma línea que mini-tools traza
// entre «edita el workspace, se ve en el árbol de checkpoints» y «hace cualquier
// cosa en la máquina». Las banderas que saltean todos los permisos
// (`--dangerously-skip-permissions`, `bypassPermissions`,
// `--dangerously-bypass-approvals-and-sandbox`, `--auto`) no se pasan nunca.
type Mode string

const (
	ModeAsk  Mode = "ask"
	ModeEdit Mode = "edit"
)

// ModeFor traduce el interruptor «Agente» del chat.
func ModeFor(agent bool) Mode {
	if agent {
		return ModeEdit
	}
	return ModeAsk
}

// Las herramientas del servidor MCP de ScadLab (backend/mcp) según lo que
// hacen. Una prueba de la app comprueba que las dos listas juntas sean
// exactamente el catálogo del servidor: una herramienta nueva sin clasificar no
// puede quedar fuera de «Solo consulta» ni colarse en él por descuido.
var (
	// readTools se permiten en los dos modos. render, measure y preview escriben
	// su caché y RENDER_ERRORS.md, pero no tocan el diseño.
	readTools = []string{"list_checkpoints", "read_checkpoint", "lint", "format", "render", "measure", "preview", "printer_info", "list_templates", "get_template"}
	// writeTools solo en «Agente»: crean un checkpoint (create_checkpoint, import_mesh) o escriben en exports/.
	writeTools = []string{"create_checkpoint", "import_mesh", "export"}
)

// ReadTools y WriteTools devuelven copias de las listas (para las pruebas).
func ReadTools() []string  { return append([]string(nil), readTools...) }
func WriteTools() []string { return append([]string(nil), writeTools...) }

// scadlabTools son las herramientas de ScadLab que el modo permite.
func scadlabTools(mode Mode) []string {
	if mode == ModeEdit {
		return append(append([]string(nil), readTools...), writeTools...)
	}
	return append([]string(nil), readTools...)
}

// claudeAllowed son las reglas `--allowedTools` de Claude Code: sin ellas, una
// herramienta MCP pide permiso y en headless la petición se deniega sola.
func claudeAllowed(mode Mode) string {
	var rules []string
	for _, t := range scadlabTools(mode) {
		rules = append(rules, "mcp__"+mcpServerName+"__"+t)
	}
	return strings.Join(rules, ",")
}

// claudeDenied son las herramientas propias de Claude Code que se le quitan: la
// shell siempre; en «Solo consulta», además las que editan archivos. Una regla
// de denegación gana a cualquier permiso que el usuario tenga en su
// configuración global.
func claudeDenied(mode Mode) string {
	tools := []string{"Bash", "PowerShell"}
	if mode != ModeEdit {
		tools = append(tools, "Edit", "Write", "MultiEdit", "NotebookEdit")
	}
	return strings.Join(tools, ",")
}

// codexEnabledTools es la lista blanca de herramientas del servidor MCP en
// «Solo consulta» (`mcp_servers.scadlab.enabled_tools`). Vacía en «Agente»: ahí
// valen todas.
func codexEnabledTools(mode Mode) string {
	if mode == ModeEdit {
		return ""
	}
	quoted := make([]string, len(readTools))
	for i, t := range readTools {
		quoted[i] = `"` + t + `"`
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
