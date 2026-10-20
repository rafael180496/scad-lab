package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/render"
)

// update_checkpoint (S16): la IA edita EN EL LUGAR el archivo que el usuario
// nombró. La lógica (cabecera, respaldo en el historial, escritura atómica, tope)
// es checkpoint.Update, la misma que usa la interfaz; esta herramienta solo
// valida los argumentos y devuelve el resultado ya verificado con lint y render,
// para que el modelo vea de una vez si el cambio quedó bien.

func (e *env) updateCheckpointTool() Tool {
	return Tool{
		Name: "update_checkpoint",
		Description: "Edit an EXISTING checkpoint IN PLACE: replaces its code with `source` (the COMPLETE new file, not a patch). Use it when the user names an existing file, or refers to the open/selected one («this», «the current one»), and asks for changes; " +
			"create a NEW checkpoint (create_checkpoint) only for something new, a variant or «another version». When in doubt, edit the one they named. It is also how you fix a model that does not render, including the file you just created. " +
			"The header `// @checkpoint parent=… desc=…` is kept (a header line in `source` is ignored; `note` replaces the description). The previous version is saved in the file's history BEFORE writing, so the user can see the differences and undo: nothing is lost. " +
			"If the code is identical nothing is written (`unchanged`). Read the file first with read_checkpoint. It does not change the active parent. " +
			"It then runs lint and render on the result and returns both: if the render fails, call update_checkpoint again on the same file.",
		InputSchema: obj(map[string]any{
			"file":   strProp("The existing checkpoint to edit (e.g. 002-rpi5-snap-case.scad), as named by the user or shown by list_checkpoints."),
			"source": strProp("Complete OpenSCAD source of the file after the change (2 MB at most). Every top-level part must use color()."),
			"note":   strProp("Optional one-line description of the change; it replaces `desc` in the header and is kept in the history."),
		}, "file", "source"),
		Annotations: annotations(annWrite),
		handler:     e.updateCheckpoint,
	}
}

func (e *env) updateCheckpoint(ctx context.Context, args map[string]any) (*toolResult, error) {
	s := e.settings()
	file := strings.TrimSpace(strArg(args, "file"))
	if file == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `file`: el checkpoint que hay que editar (ver list_checkpoints)", EN: "`file` is required: the checkpoint to edit (see list_checkpoints)"})
	}
	source := strArg(args, "source")
	if strings.TrimSpace(source) == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `source`: el código OpenSCAD completo del archivo", EN: "`source` is required: the complete OpenSCAD source of the file"})
	}
	if len(source) > maxSourceBytes {
		return nil, i18n.New(i18n.Msg{ES: "el código es demasiado grande (máximo 2 MB)", EN: "the source is too large (2 MB maximum)"})
	}
	up, err := checkpoint.Update(e.ws, file, source, strArg(args, "note"), s.HistoryMaxVersions)
	if err != nil {
		return nil, err
	}

	out := map[string]any{
		"file":        file,
		"updated":     true,
		"unchanged":   up.Unchanged,
		"parent":      up.Parent,
		"description": up.Description,
	}
	if !up.Unchanged {
		out["version"] = up.Version
	}

	// Lint y render sobre lo que quedó en el disco (con su cabecera).
	if raw, rerr := os.ReadFile(filepath.Join(e.ws, file)); rerr == nil {
		if ds, _, lerr := e.lintSource(ctx, s, string(raw), file); lerr == nil {
			if len(ds) > maxDiagnostics {
				ds = ds[:maxDiagnostics]
			}
			out["lint"] = ds
		}
	}
	if _, has := out["lint"]; !has {
		out["lint"] = []any{}
	}
	res, _, rerr := e.render(ctx, file)
	switch {
	case rerr != nil:
		// El archivo ya cambió: el fallo es de verificar, no de editar.
		out["render"] = map[string]any{"ok": false, "message": rerr.Error()}
	case res.Status != render.StatusOK:
		fail := e.renderFailure(file, res)
		fail["ok"] = false
		out["render"] = fail
	default:
		sum, serr := e.renderSummary(file, res, s)
		if serr != nil {
			out["render"] = map[string]any{"ok": false, "message": serr.Error()}
			break
		}
		sum["ok"] = true
		out["render"] = sum
	}

	switch {
	case up.Unchanged:
		out["next"] = msgf(i18n.Msg{
			ES: "El código es idéntico al que ya tiene %s: no se escribió nada ni se guardó respaldo.",
			EN: "The code is identical to what %s already has: nothing was written and no backup was saved.",
		}, file)
	default:
		out["next"] = msgf(i18n.Msg{
			ES: "%s quedó actualizado; la versión anterior está en su historial (se puede deshacer desde ScadLab).",
			EN: "%s is updated; the previous version is in its history (it can be undone from ScadLab).",
		}, file)
	}
	return jsonResult(out)
}
