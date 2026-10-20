package mcp

// Perfiles de herramientas (S17, D). El catálogo completo pesa varios miles de
// tokens solo en descripciones y esquemas: un Ollama de 8k de contexto no tiene
// espacio para eso y para trabajar. El perfil esencial ofrece las once
// herramientas que alcanzan para modificar un modelo —leer por partes, editar,
// cambiar parámetros, verificar— con descripciones de una o dos frases y
// esquemas mínimos.
//
// Es solo lo que se le OFRECE al modelo (Toolbox.ToolsFor): `Call` acepta
// cualquier herramienta del catálogo, y el servidor `--mcp` de los CLIs sigue
// ofreciendo todo. La decisión de cuándo usarlo (contexto < 32 768 tokens o el
// interruptor de la conversación) es de quien llama (backend/hermes).

// ToolProfile elige qué herramientas se le ofrecen al modelo.
type ToolProfile string

const (
	// ProfileFull: el catálogo completo, con sus descripciones.
	ProfileFull ToolProfile = "full"
	// ProfileEssential: las herramientas de EssentialTools, con descripción y
	// esquema cortos.
	ProfileEssential ToolProfile = "essential"
)

// essentialTools son las herramientas del perfil esencial, en el orden en que se
// ofrecen: primero las que ahorran contexto al leer, después las que cambian el
// modelo y al final las que lo verifican y lo crean.
var essentialTools = []string{
	"outline_checkpoint", "find_in_checkpoint", "read_checkpoint",
	"edit_checkpoint", "set_parameters", "get_parameters",
	"render", "preview", "measure",
	"create_checkpoint", "list_checkpoints",
}

// EssentialTools devuelve los nombres de las herramientas del perfil esencial.
func EssentialTools() []string { return append([]string(nil), essentialTools...) }

// shortSpecs es la descripción y el esquema mínimo de cada herramienta esencial.
// Van en inglés, como el resto del catálogo. Una descripción tiene 1-2 frases: lo
// que la herramienta hace y cuándo se usa; el esquema conserva los tipos y lo
// obligatorio, y casi nada de texto.
var shortSpecs = map[string]struct {
	short  string
	schema map[string]any
}{
	"outline_checkpoint": {
		"Map of a .scad without reading it: parameters by section, modules/functions with first and last line, includes and color parts. Use it first on a big file.",
		obj(map[string]any{"file": sp("file name, or lib/x.scad")}),
	},
	"find_in_checkpoint": {
		"Search text in a .scad; returns numbered lines around each match. Use it to find what to change.",
		obj(map[string]any{
			"file": sp("file name, or lib/x.scad"), "query": sp("text to find"),
			"context": ip("lines around each match, default 2"), "regex": bp("query is a regular expression"),
		}, "query"),
	},
	"read_checkpoint": {
		"Read a .scad by file name. With from/to it returns numbered lines `N| code` (400 max); without them the whole file (first 12 KB if it is bigger).",
		obj(map[string]any{"file": sp("file name, or lib/x.scad"), "from": ip("first line"), "to": ip("last line"), "full": bp("whole file, any size")}),
	},
	"edit_checkpoint": {
		"Change a .scad in place with small edits, without rewriting it. Each edit is {old,new} (exact text that appears once) or {from_line,to_line,new} (inclusive line range; to_line=from_line-1 inserts; new=\"\" deletes). Line numbers are those of the file now. It renders and returns a short result.",
		obj(map[string]any{
			"file": sp("checkpoint file name, or lib/x.scad"),
			"edits": map[string]any{"type": "array", "items": obj(map[string]any{
				"old": sp("exact text"), "new": sp("replacement"), "from_line": ip("first line"), "to_line": ip("last line"),
			})},
			"note": sp("one-line description"),
		}, "file", "edits"),
	},
	"set_parameters": {
		"Change Customizer values (sizes, colors, texts) in place. values = {name: value}, names from get_parameters. variant=true creates a new checkpoint instead. Renders and returns a short result.",
		obj(map[string]any{
			"values":  map[string]any{"type": "object", "description": "name -> new value", "additionalProperties": true},
			"file":    sp("checkpoint file name"),
			"variant": bp("new child checkpoint instead of editing"),
		}, "values"),
	},
	"get_parameters": {
		"List a checkpoint's Customizer parameters: name, type, value, range or options.",
		obj(map[string]any{"file": sp("checkpoint file name")}),
	},
	"render": {
		"Render a checkpoint with OpenSCAD. Returns a short summary: size, volume, fits the plate, and errors with line numbers.",
		obj(map[string]any{"file": sp("checkpoint file name"), "verbose": bp("full detail")}),
	},
	"preview": {
		"Picture of the model: one 384 px iso view by default; views=[...] for more.",
		obj(map[string]any{
			"file": sp("checkpoint file name"), "view": sp("iso, front, back, left, right, top, bottom"),
			"views": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "size": ip("pixels"),
		}),
	},
	"measure": {
		"Size, volume, estimated weight, parts by color and whether the model fits the printer plate.",
		obj(map[string]any{"file": sp("checkpoint file name")}),
	},
	"create_checkpoint": {
		"Create a NEW checkpoint from complete OpenSCAD source (a new design or a variant). Every top-level part needs color().",
		obj(map[string]any{
			"label": sp("short title"), "source": sp("complete OpenSCAD source"),
			"parent": sp("checkpoint it derives from"), "description": sp("one line"),
		}, "label", "source"),
	},
	"list_checkpoints": {
		"List the checkpoints as a tree, with the active parent.",
		obj(map[string]any{}),
	},
}

// sp, ip y bp son propiedades mínimas de un esquema corto.
func sp(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func ip(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
func bp(desc string) map[string]any { return map[string]any{"type": "boolean", "description": desc} }

// applyShort llena Short y ShortSchema de las herramientas esenciales.
func applyShort(tools []Tool) []Tool {
	for i := range tools {
		if sh, ok := shortSpecs[tools[i].Name]; ok {
			tools[i].Short, tools[i].ShortSchema = sh.short, sh.schema
		}
	}
	return tools
}

// ToolsFor devuelve las herramientas que se le ofrecen al modelo según el perfil.
// ProfileFull es Tools(). ProfileEssential trae solo las de EssentialTools, en ese
// orden, con la descripción y el esquema cortos. Un perfil desconocido es el
// completo. Call acepta cualquier herramienta, esté o no en el perfil.
func (t *Toolbox) ToolsFor(p ToolProfile) []ToolSpec {
	if p != ProfileEssential {
		return t.Tools()
	}
	byName := make(map[string]Tool, len(t.tools))
	for _, tool := range t.tools {
		byName[tool.Name] = tool
	}
	out := make([]ToolSpec, 0, len(essentialTools))
	for _, name := range essentialTools {
		tool, ok := byName[name]
		if !ok || tool.Short == "" {
			continue
		}
		out = append(out, ToolSpec{Name: tool.Name, Description: tool.Short, InputSchema: tool.ShortSchema})
	}
	return out
}
