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
// hacen. Dos pruebas comprueban que las dos listas juntas sean exactamente el
// catálogo del servidor (backend/mcp/modes_test.go y la de la app): una
// herramienta nueva sin clasificar no puede quedar fuera de «Solo consulta» ni
// colarse en él por descuido. La tabla con qué hace cada una está en
// claude/25-herramientas-ia.md.
var (
	// readTools se permiten en los dos modos. render, measure, preview,
	// estimate_print y suggest_orientation escriben su caché y RENDER_ERRORS.md,
	// pero no tocan el diseño: ningún checkpoint, ningún archivo del proyecto.
	readTools = []string{
		// Workspace y archivos
		"list_checkpoints", "read_checkpoint", "list_trash", "list_refs",
		// Modelado
		"lint", "format", "render", "preview", "measure", "get_parameters", "list_templates", "get_template", "list_libraries",
		// Referencias 3D
		"inspect_reference",
		// Imágenes (S15): miran y comparan; no tocan el diseño
		"image_info", "compare_with_image",
		// Impresión
		"printer_info", "list_printers", "estimate_print", "list_materials", "suggest_orientation",
		// Contexto
		"list_skills", "read_skill",
	}
	// writeTools solo en «Agente»: crean un checkpoint, cambian el estado del
	// árbol (padre activo, rótulo, papelera), escriben en exports/ o en lib/
	// (install_library descarga de internet) o dejan una malla congelada en refs/.
	writeTools = []string{
		"create_checkpoint", "set_parameters", "create_from_template", "import_mesh", "edit_mesh",
		"set_active_parent", "rename_checkpoint", "trash_checkpoint", "restore_checkpoint",
		"install_library", "export",
		// Escriben el SVG o el mapa de alturas en refs/ (S15).
		"trace_image_outline", "image_to_heightmap",
		// Cambia la impresora y los colores del workspace (S14).
		"set_workspace_printer",
	}
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

// codexEnabledTools es la lista blanca de herramientas del servidor MCP
// (`mcp_servers.scadlab.enabled_tools`) que el modo permite: las de lectura en
// «Solo consulta» y TODAS en «Agente». En «Agente» se nombran a propósito en vez
// de dejar el valor sin tocar: un `config.toml` del usuario que restrinja el
// servidor dejaría a Codex sin `create_checkpoint`, y en headless no hay quien
// pueda aprobarlo después (S12.c).
func codexEnabledTools(mode Mode) string {
	names := scadlabTools(mode)
	quoted := make([]string, len(names))
	for i, t := range names {
		quoted[i] = `"` + t + `"`
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
