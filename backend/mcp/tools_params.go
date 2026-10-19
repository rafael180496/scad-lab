package mcp

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/scad"
)

// Parámetros del Customizer y plantillas: cambiar una medida no debería
// costarle a la IA reescribir el modelo. get_parameters lee los parámetros de un
// checkpoint; set_parameters crea un checkpoint HIJO con los valores nuevos (el
// resto del archivo, byte a byte); create_from_template arranca de una plantilla
// de la skill openscad-templates con sus parámetros ya ajustados.

func (e *env) parameterTools() []Tool {
	return []Tool{
		{
			Name: "get_parameters",
			Description: "Read a checkpoint's Customizer parameters: the top-level variables with a literal value (number, text, true/false or a vector of numbers) before the first module, with their group, description, range [min:step:max] and list of options. " +
				"Use it before set_parameters to see what can be changed. Variables in the Hidden section are listed with hidden=true and cannot be changed with set_parameters.",
			InputSchema: obj(map[string]any{"file": strProp(fileHelp)}),
			Annotations: annotations(annReadOnly),
			handler:     e.getParameters,
		},
		{
			Name: "set_parameters",
			Description: "Change Customizer parameter values in a NEW child checkpoint: reads the checkpoint, replaces only those literals and keeps everything else byte for byte (comments, ranges, order). The original is never modified. " +
				"This is the right tool for «make it 10 mm wider», «use the slide lid», «turn off the logo». Values must match the parameter type (number, text, true/false, or a list of numbers of the same length); a value outside its range or options list is applied with a warning. Then call render.",
			InputSchema: obj(map[string]any{
				"values": map[string]any{
					"type":                 "object",
					"description":          "Parameter name -> new value, e.g. {\"width\": 80, \"lid_type\": \"slide\", \"logo\": false, \"size\": [10, 20, 5]}. Names come from get_parameters.",
					"additionalProperties": true,
				},
				"file":        strProp("Checkpoint to start from (default: the active parent, else the newest). It becomes the parent of the new one."),
				"label":       strProp("Short title for the new checkpoint (default: «params» plus the names changed)."),
				"description": strProp("One-line description for the header (default: the changes made)."),
			}, "values"),
			Annotations: annotations(annWrite),
			handler:     e.setParameters,
		},
		{
			Name: "create_from_template",
			Description: "Create a NEW checkpoint from one of ScadLab's parametric templates (see list_templates), with its Customizer parameters already adjusted, instead of writing the OpenSCAD yourself. " +
				"Pass `parameters` to set values (names come from get_template or get_parameters of the result); the template's PLATE constant is set to the workspace printer's build volume. " +
				"Without `parent` it branches from the active parent / newest checkpoint, and it is a root checkpoint in an empty workspace; root=true forces a new root. Then call render and preview.",
			InputSchema: obj(map[string]any{
				"name": strProp("Template name from list_templates (e.g. knob)."),
				"parameters": map[string]any{
					"type":                 "object",
					"description":          "Optional parameter name -> value overrides, e.g. {\"knob_d\": 40, \"flutes\": 12}.",
					"additionalProperties": true,
				},
				"label":       strProp("Short title; becomes the slug of the file (default: the template name)."),
				"description": strProp("One-line description for the header (default: the template's)."),
				"parent":      strProp("Checkpoint it derives from (default: the active parent, else the newest; none in an empty workspace)."),
				"root":        map[string]any{"type": "boolean", "description": "Create it as a new root checkpoint (no parent) even if the workspace already has checkpoints. Default false."},
			}, "name"),
			Annotations: annotations(annWrite),
			handler:     e.createFromTemplate,
		},
	}
}

// objArg saca un argumento que es un objeto. Acepta también un texto con JSON:
// los modelos chicos mandan el objeto serializado.
func objArg(args map[string]any, key string) (map[string]any, bool) {
	switch v := args[key].(type) {
	case map[string]any:
		return v, true
	case string:
		var m map[string]any
		if json.Unmarshal([]byte(v), &m) == nil {
			return m, true
		}
	}
	return nil, false
}

// listArg saca un argumento que es una lista (o un texto con JSON de una lista).
func listArg(args map[string]any, key string) ([]any, bool) {
	switch v := args[key].(type) {
	case []any:
		return v, true
	case string:
		var l []any
		if json.Unmarshal([]byte(v), &l) == nil {
			return l, true
		}
	}
	return nil, false
}

func (e *env) getParameters(_ context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	file := strArg(args, "file")
	name, path, err := e.checkpointFile(file)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	params, err := scad.Parameters(string(raw))
	if err != nil {
		return nil, err
	}
	out := map[string]any{"file": name, "count": len(params), "parameters": params}
	if len(params) == 0 {
		out["note"] = msgf(i18n.Msg{
			ES: "Este checkpoint no declara parámetros del Customizer (variables con un valor literal arriba del archivo, antes del primer module). Para cambiar una medida, crea un checkpoint nuevo con create_checkpoint.",
			EN: "This checkpoint declares no Customizer parameters (variables with a literal value at the top of the file, before the first module). To change a measurement, create a new checkpoint with create_checkpoint.",
		})
	}
	return jsonResult(out)
}

// paramSummary describe los cambios: «ancho 60 → 80, tapa "snap" → "slide"».
func paramSummary(changes []scad.ParamChange) string {
	parts := make([]string, len(changes))
	for i, c := range changes {
		parts[i] = c.Name + " " + c.Old + " → " + c.New
	}
	return strings.Join(parts, ", ")
}

func (e *env) setParameters(_ context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	values, ok := objArg(args, "values")
	if !ok || len(values) == 0 {
		return nil, i18n.New(i18n.Msg{ES: "falta `values`: un objeto nombre → valor (los nombres salen de get_parameters)", EN: "`values` is required: an object name -> value (names come from get_parameters)"})
	}
	file := strArg(args, "file")
	if file == "" {
		file = e.defaultParent()
	}
	name, path, err := e.checkpointFile(file)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	src, changes, warnings, err := scad.SetParameters(string(raw), values)
	if err != nil {
		return nil, err
	}
	if len(changes) == 0 {
		return nil, i18n.New(i18n.Msg{ES: "los valores pedidos ya son los actuales: no hay nada que cambiar", EN: "the requested values are already the current ones: there is nothing to change"})
	}
	names := make([]string, len(changes))
	for i, c := range changes {
		names[i] = c.Name
	}
	sort.Strings(names)
	label := strings.TrimSpace(strArg(args, "label"))
	if label == "" {
		label = "params " + strings.Join(names, " ")
	}
	desc := strings.TrimSpace(strArg(args, "description"))
	if desc == "" {
		desc = msgf(i18n.Msg{ES: "Parámetros: ", EN: "Parameters: "}) + paramSummary(changes)
	}
	child, err := checkpoint.Create(e.ws, name, label, desc, src)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"file":         child,
		"parent":       name,
		"activeParent": checkpoint.ActiveParent(e.ws),
		"changes":      changes,
		"next":         msgf(i18n.Msg{ES: "Valídalo con render (file=%s).", EN: "Validate it with render (file=%s)."}, child),
	}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	return jsonResult(out)
}

// templateDesc saca la descripción de la cabecera de una plantilla.
func templateDesc(src string) string {
	first, _, _ := strings.Cut(src, "\n")
	if m := templateDescRe.FindStringSubmatch(first); m != nil {
		return strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(m[1])
	}
	return ""
}

func (e *env) createFromTemplate(_ context.Context, args map[string]any) (*toolResult, error) {
	s := e.settings()
	want := strings.TrimSuffix(strings.TrimSpace(strArg(args, "name")), ".scad")
	if want == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `name`: la plantilla (ver list_templates)", EN: "`name` is required: the template (see list_templates)"})
	}
	// La plantilla se busca en la lista embebida y no se arma una ruta con lo que
	// llega: un nombre con «..» o barras no puede salirse de la carpeta.
	names, err := templateNames()
	if err != nil {
		return nil, err
	}
	var src string
	found := false
	for _, n := range names {
		if n == want {
			if src, err = readTemplate(n); err != nil {
				return nil, err
			}
			found = true
			break
		}
	}
	if !found {
		return nil, i18n.Errorf(i18n.Msg{ES: "plantilla desconocida: %q (disponibles: %s)", EN: "unknown template: %q (available: %s)"}, want, strings.Join(names, ", "))
	}

	out := map[string]any{"template": want}
	var changes []scad.ParamChange
	var warnings []string
	if raw, has := args["parameters"]; has && raw != nil {
		values, ok := objArg(args, "parameters")
		if !ok {
			return nil, i18n.New(i18n.Msg{ES: "`parameters` tiene que ser un objeto nombre → valor", EN: "`parameters` must be an object name -> value"})
		}
		if len(values) > 0 {
			var err error
			if src, changes, warnings, err = scad.SetParameters(src, values); err != nil {
				return nil, err
			}
		}
	}
	// PLATE: el volumen de la impresora de este workspace, no el de la A1 mini
	// con el que se escribieron las plantillas.
	if pr := e.printer(s); pr.BuildVolume.X > 0 && pr.BuildVolume.Y > 0 && pr.BuildVolume.Z > 0 {
		if params, err := scad.Parameters(src); err == nil {
			for _, p := range params {
				if p.Name == "PLATE" && p.Type == scad.ParamVector {
					if v, _ := p.Value.([]float64); len(v) == 3 {
						if s2, ch, err := scad.SetHiddenParameter(src, "PLATE", []any{pr.BuildVolume.X, pr.BuildVolume.Y, pr.BuildVolume.Z}); err == nil && len(ch) > 0 {
							src = s2
							out["plate"] = []float64{pr.BuildVolume.X, pr.BuildVolume.Y, pr.BuildVolume.Z}
						}
					}
				}
			}
		}
	}

	label := strings.TrimSpace(strArg(args, "label"))
	if label == "" {
		label = want
	}
	desc := strings.TrimSpace(strArg(args, "description"))
	if desc == "" {
		desc = templateDesc(src)
	}
	if desc == "" {
		desc = label
	}

	var file, parent string
	switch parent = strArg(args, "parent"); {
	case boolArg(args, "root", false):
		parent = ""
		file, err = checkpoint.CreateDetached(e.ws, label, desc, src)
	default:
		if parent == "" {
			parent = e.defaultParent()
		}
		if parent == "" {
			file, err = checkpoint.CreateRoot(e.ws, label, desc, src)
		} else {
			if _, _, perr := e.checkpointFile(parent); perr != nil {
				return nil, perr
			}
			file, err = checkpoint.Create(e.ws, parent, label, desc, src)
		}
	}
	if err != nil {
		return nil, err
	}
	out["file"], out["parent"], out["activeParent"] = file, parent, checkpoint.ActiveParent(e.ws)
	if len(changes) > 0 {
		out["changes"] = changes
	}
	if len(warnings) > 0 {
		out["warnings"] = warnings
	}
	out["next"] = msgf(i18n.Msg{
		ES: "Renderízalo con render (file=%s), míralo con preview y ajústalo con set_parameters.",
		EN: "Render it with render (file=%s), look at it with preview and adjust it with set_parameters.",
	}, file)
	return jsonResult(out)
}
