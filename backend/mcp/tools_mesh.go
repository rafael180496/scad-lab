package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/meshedit"
	"scadlab/backend/parts"
)

// edit_mesh: el editor 3D de la app (S13) como herramienta. La edición es la de
// siempre —un checkpoint hijo que hace import("refs/x.stl") y le aplica
// operaciones—, sale del mismo meshedit.Generate y el STL nunca se toca. Aquí
// solo se resuelve el checkpoint de partida, se traducen las operaciones de la
// IA a una meshedit.Scene (meshedit.BuildScene) y se guarda el hijo.

const editMeshOpsHelp = "A list of steps applied in order, each an object with `op` and its fields. " +
	"POSE steps (about the center of the model's bounding box unless noted): " +
	"{op:move, by:[x,y,z]} (mm, added to the position) · {op:rotate, degrees:[rx,ry,rz]} (fixed world axes) · {op:scale, factor: n | [fx,fy,fz]} (along the model's own axes; positive) · " +
	"{op:resize, axis:x|y|z|longest, mm:n} (uniform scale so that side measures n mm) · {op:mirror, axis:x|y|z} (along the model's own axes) · " +
	"{op:orient, down:+x|-x|+y|-y|+z|-z} (that face of the model goes down) · {op:center, axes:\"xy\"} (bounding-box center to the origin; default xy) · {op:to_bed} (lowest point at z=0). " +
	"{op:color, color:#rrggbb} sets the model color. " +
	"Primitives and the cut are placed in the FINAL frame (after every pose step, wherever they are in the list): " +
	"{op:add|hole, shape:cube|cylinder|sphere|text, size:[x,y,z] (cube) | diameter,height (cylinder; axis:x|y|z lays it along that axis) | diameter (sphere) | text,text_size,height (text), " +
	"at:[x,y,z] (mm), at_rel:[fx,fy,fz] (fractions of the model's box, e.g. [0.5,0.5,1] = center of the top face; at then adds an offset), rotate:[rx,ry,rz], center:true (default: at is the center of the shape), color:#rrggbb} — add unions it, hole subtracts it (a through hole: height longer than the part). " +
	"{op:cut, axis:x|y|z with at:mm or at_rel:fraction, or point:[x,y,z] with normal:[x,y,z]; keep:above|below|both (above = the side the normal points to); with both, split:objects (two parts side by side to print apart) or colors with colors:[above,below]} — ONE cut per call."

func (e *env) meshTools() []Tool {
	return []Tool{
		{
			Name: "edit_mesh",
			Description: "Edit the 3D model of a checkpoint — move, rotate, scale, mirror, center, rest on the plate, cut with a plane, add shapes or drill holes, recolor — as a NEW child checkpoint, using the same engine as ScadLab's 3D editor. " +
				"Works on an imported STL/3MF/OBJ (import_mesh) and also on any checkpoint: its rendered mesh is frozen into refs/ (nothing is overwritten) and the new checkpoint imports that, so the result is ONE single-color mesh; to keep a design parametric, change its OpenSCAD code instead (set_parameters / create_checkpoint). " +
				"The STL is never modified. A mesh that is not watertight can be moved, rotated, scaled and mirrored but not drilled, added to or cut. Returns the new checkpoint and the model's bounding box after the poses, so you can place holes and cuts; then render and preview it.",
			InputSchema: obj(map[string]any{
				"file": strProp("Checkpoint to edit (default: the active parent, else the newest). It becomes the parent of the new one."),
				"operations": map[string]any{
					"type":        "array",
					"description": editMeshOpsHelp,
					"items":       map[string]any{"type": "object", "properties": map[string]any{"op": enumProp("Operation.", meshedit.OpNames...)}, "required": []string{"op"}, "additionalProperties": true},
					"minItems":    1,
					"maxItems":    meshedit.MaxToolOps,
				},
				"label":       strProp("Short title for the new checkpoint (default: «STL edit»)."),
				"description": strProp("One-line description for the header (default: the label)."),
			}, "operations"),
			Annotations: annotations(annWrite),
			handler:     e.editMesh,
		},
	}
}

// opsArg decodifica `operations`: una lista de objetos (o un texto con JSON).
func opsArg(args map[string]any) ([]meshedit.Op, error) {
	list, ok := listArg(args, "operations")
	if !ok {
		// Un objeto suelto se lee como una lista de uno.
		if one, isObj := objArg(args, "operations"); isObj {
			list, ok = []any{one}, true
		}
	}
	if !ok || len(list) == 0 {
		return nil, i18n.New(i18n.Msg{
			ES: "falta `operations`: una lista de pasos, p. ej. [{\"op\":\"orient\",\"down\":\"+x\"},{\"op\":\"center\"},{\"op\":\"to_bed\"}]",
			EN: "`operations` is required: a list of steps, e.g. [{\"op\":\"orient\",\"down\":\"+x\"},{\"op\":\"center\"},{\"op\":\"to_bed\"}]",
		})
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return nil, err
	}
	var ops []meshedit.Op
	if err := json.Unmarshal(raw, &ops); err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "`operations` no se pudo leer: %v", EN: "`operations` could not be read: %v"}, err)
	}
	return ops, nil
}

func (e *env) editMesh(ctx context.Context, args map[string]any) (*toolResult, error) {
	s := e.settings()
	ops, err := opsArg(args)
	if err != nil {
		return nil, err
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

	base, err := meshedit.ResolveBase(e.ws, name, string(raw), func() (string, error) {
		res, _, err := e.render(ctx, name)
		if err != nil {
			return "", err
		}
		if err := parts.RenderFailure(res); err != nil {
			return "", err
		}
		if res.MeshPath == "" {
			return "", i18n.New(i18n.Msg{ES: "el checkpoint no produjo una malla", EN: "the checkpoint did not produce a mesh"})
		}
		return res.MeshPath, nil
	})
	if err != nil {
		return nil, err
	}
	// La malla congelada es lo único que esta herramienta puede dejar atrás si algo
	// falla después: si la creó esta llamada, se quita (es una copia nuestra,
	// recién hecha; una que ya existía se reutiliza y no se toca).
	done := false
	defer func() {
		if !done && base.Created {
			_ = os.Remove(filepath.Join(e.ws, filepath.FromSlash(base.Ref)))
		}
	}()

	edit, err := meshedit.BuildScene(base, ops, i18n.Lang())
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(strArg(args, "label"))
	if label == "" {
		label = msgf(i18n.Msg{ES: "Edición de STL", EN: "STL edit"})
	}
	desc := strings.TrimSpace(strArg(args, "description"))
	if desc == "" {
		desc = label
	}
	src, err := meshedit.Generate(edit.Scene, meshedit.Header{Parent: name, Desc: desc}, i18n.Lang())
	if err != nil {
		return nil, err
	}
	child, err := checkpoint.Create(e.ws, name, label, desc, src)
	if err != nil {
		return nil, err
	}
	done = true

	pr := e.printer(s)
	size := [3]float64{edit.Max[0] - edit.Min[0], edit.Max[1] - edit.Min[1], edit.Max[2] - edit.Min[2]}
	ok, over := fits(size, pr.BuildVolume)
	out := map[string]any{
		"file":         child,
		"parent":       name,
		"activeParent": checkpoint.ActiveParent(e.ws),
		"ref":          base.Ref,
		"frozen":       !base.FromImport,
		"operations":   edit.Scene.Ops,
		"model": map[string]any{
			"minMm": vecOf(edit.Min), "maxMm": vecOf(edit.Max), "sizeMm": vecOf(size),
			"centerMm": vecOf([3]float64{(edit.Min[0] + edit.Max[0]) / 2, (edit.Min[1] + edit.Max[1]) / 2, (edit.Min[2] + edit.Max[2]) / 2}),
			"note": msgf(i18n.Msg{
				ES: "Caja del modelo base tras mover/girar/escalar (sin agujeros, uniones ni corte).",
				EN: "Box of the base model after moving/rotating/scaling (without holes, additions or cut).",
			}),
		},
		"fitsBuildVolume": ok,
		"printer":         printerOf(pr),
		"next":            msgf(i18n.Msg{ES: "Renderízalo con render (file=%s) y mira el resultado con preview.", EN: "Render it with render (file=%s) and look at the result with preview."}, child),
	}
	if !ok {
		out["overflowMm"] = over
	}
	var warns []string
	warns = append(warns, edit.Warnings...)
	if !base.FromImport {
		warns = append(warns, msgf(i18n.Msg{
			ES: "%s no era un import de un STL: su malla renderizada se congeló en %s y el checkpoint nuevo la importa. Ahora es UNA malla de un solo color (los colores y los parámetros del código original no están en el hijo); para cambiar el diseño paramétrico, edita el código del checkpoint original.",
			EN: "%s was not an STL import: its rendered mesh was frozen into %s and the new checkpoint imports it. It is now ONE single-color mesh (the colors and parameters of the original code are not in the child); to change the parametric design, edit the original checkpoint's code.",
		}, name, base.Ref))
	}
	if len(warns) > 0 {
		out["warnings"] = warns
	}
	return jsonResult(out)
}
