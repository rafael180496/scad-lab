package mcp

import "strings"

// Los modelos (sobre todo los chicos, y Gemini vía Antigravity) adivinan el
// nombre de los argumentos: mandan `file` donde la herramienta pide `image`,
// `pattern` en vez de `query`, `skill` en vez de `name`. Antes eso era un error
// («falta `image`») y una vuelta perdida por cada tropiezo, medido en el
// transcript real del usuario. normalizeArgs lo corrige antes de llamar a la
// herramienta: por cada propiedad del esquema que falta, toma el valor de un
// sinónimo conocido que el esquema NO declara. Un sinónimo que la herramienta sí
// declara (p. ej. `file` en una herramienta que pide `file` e `image`) nunca se
// mueve, y un argumento ya presente no se pisa.

// argAliases: nombre canónico → sinónimos que mandan los modelos.
var argAliases = map[string][]string{
	"image":        {"file", "path", "image_path", "filename", "ref", "photo", "picture", "img"},
	"file":         {"checkpoint", "path", "filename", "file_name", "name", "target"},
	"query":        {"pattern", "text", "search", "q", "term", "string", "needle"},
	"name":         {"skill", "skill_name", "template", "template_name", "id"},
	"source":       {"code", "content", "scad", "text", "contents"},
	"from":         {"start", "start_line", "from_line", "line_start", "begin"},
	"to":           {"end", "end_line", "to_line", "line_end", "until"},
	"edits":        {"changes", "edit", "replacements", "patches"},
	"values":       {"parameters", "params", "set"},
	"label":        {"title", "desc", "description", "slug"},
	"printer_id":   {"printer", "id", "model"},
	"view":         {"angle", "camera"},
	"note":         {"message", "comment", "reason"},
	"operations":   {"ops", "steps"},
	"trashed_name": {"file", "name", "trash_name"},
}

// normalizeArgs devuelve args con los sinónimos movidos a su nombre canónico,
// según las propiedades que declara el esquema de la herramienta. No modifica
// el mapa recibido.
func normalizeArgs(tool *Tool, args map[string]any) map[string]any {
	props := schemaProps(tool)
	if len(props) == 0 || len(args) == 0 {
		return args
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		out[k] = v
	}
	// Claves con otra capitalización o con guiones: `Image`, `from-line`.
	for k, v := range args {
		if _, ok := props[k]; ok {
			continue
		}
		norm := strings.ReplaceAll(strings.ToLower(k), "-", "_")
		if _, ok := props[norm]; ok {
			if _, taken := out[norm]; !taken {
				out[norm] = v
				delete(out, k)
			}
		}
	}
	for canon := range props {
		if present(out[canon]) {
			continue
		}
		for _, alias := range argAliases[canon] {
			if _, declared := props[alias]; declared {
				continue // la herramienta lo usa con su propio sentido
			}
			if v, ok := out[alias]; ok && present(v) {
				out[canon] = v
				delete(out, alias)
				break
			}
		}
	}
	return out
}

// present: el valor cuenta como dado (un "" o null no tapa un sinónimo).
func present(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(x) != ""
	}
	return true
}

func schemaProps(tool *Tool) map[string]any {
	if tool == nil || tool.InputSchema == nil {
		return nil
	}
	p, _ := tool.InputSchema["properties"].(map[string]any)
	return p
}
