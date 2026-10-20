package mcp

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/refmodel"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// Herramientas de archivos y de árbol del workspace: elegir el padre activo,
// rotular, mandar a la papelera (y traer de vuelta) y listar refs/. Todas
// reutilizan backend/checkpoint y backend/refmodel — la misma lógica de los
// bindings de la app — y la misma seguridad de rutas que el resto del servidor:
// solo nombres sueltos validados por checkpoint.ValidName, y nada que salga del
// workspace.

func (e *env) workspaceTools() []Tool {
	return []Tool{
		{
			Name: "set_active_parent",
			Description: "Choose which checkpoint the NEXT checkpoint will branch from (the user's «Branch from here»). create_checkpoint, set_parameters (with variant=true), create_from_template and edit_mesh use it as the default parent and it advances to each new child by itself. " +
				"Use it to go back to an earlier version and try another direction. Pass clear=true to go back to «the newest checkpoint».",
			InputSchema: obj(map[string]any{
				"file":  strProp("Checkpoint to branch from (e.g. 003-base-box.scad)."),
				"clear": map[string]any{"type": "boolean", "description": "Forget the choice: new checkpoints continue from the newest one."},
			}),
			Annotations: annotations(annWrite),
			handler:     e.setActiveParent,
		},
		{
			Name: "rename_checkpoint",
			Description: "Change the display name (label) the checkpoint tree shows for a checkpoint, e.g. «Lid with hinge». The file is NOT renamed (its children reference it by file name) and the description in its header is not touched: the label is stored by the app (scadlab.db) next to the checkpoint and shown in its place. " +
				"An empty label removes it. Only works for a workspace registered in the ScadLab app.",
			InputSchema: obj(map[string]any{
				"file":  strProp("Checkpoint to rename (e.g. 003-base-box.scad)."),
				"label": strProp("New display name, up to 80 characters. Empty to remove it."),
			}, "file", "label"),
			Annotations: annotations(annWrite),
			handler:     e.renameCheckpoint,
		},
		{
			Name: "trash_checkpoint",
			Description: "Move a checkpoint to the workspace trash (.scadlab/trash/). It NEVER deletes anything: restore_checkpoint brings it back. Its direct children are not moved: they are re-hung onto its parent (or become roots if it was a root) so the tree never points at a missing file; their grandchildren are untouched. With branch=true it moves the checkpoint and everything that derives from it instead, and nothing is re-hung. " +
				"If it was the active parent, the active parent moves to its parent. Only trash what the user asked to discard.",
			InputSchema: obj(map[string]any{
				"file":   strProp("Checkpoint to move to the trash (e.g. 004-lid.scad)."),
				"branch": map[string]any{"type": "boolean", "description": "Also move all its descendants (children, grandchildren…). Default false."},
			}, "file"),
			Annotations: annotations(annWrite),
			handler:     e.trashCheckpoint,
		},
		{
			Name:        "list_trash",
			Description: "List the checkpoints in the workspace trash, newest first: the name inside the trash (use it with restore_checkpoint), the original file name, its parent and description.",
			InputSchema: obj(map[string]any{}),
			Annotations: annotations(annReadOnly),
			handler:     e.listTrash,
		},
		{
			Name:        "restore_checkpoint",
			Description: "Bring a checkpoint back from the trash (see list_trash). It returns to its original name if that is free; otherwise it takes the next free number with the same slug (children that were re-hung when it was trashed are hung back onto it, under whatever name it came back with, if they still hang where they were left). Never overwrites a checkpoint.",
			InputSchema: obj(map[string]any{"trashed_name": strProp("Name inside the trash, as listed by list_trash (e.g. 20260101-120000-004-lid.scad).")}, "trashed_name"),
			Annotations: annotations(annWrite),
			handler:     e.restoreCheckpoint,
		},
		{
			Name: "list_refs",
			Description: "List the files the user put in the workspace's refs/ folder: 3D models (STL, 3MF, OBJ; `kind` \"model\") and images (PNG, JPEG, GIF, WebP in refs/ or refs/images/; `kind` \"image\"): name, path, format, kind, size and date. The originals are never modified. " +
				"Analyze a model with inspect_reference and make it editable with import_mesh; look at an image with image_info and model from it following the openscad-from-image skill (compare_with_image, trace_image_outline, image_to_heightmap).",
			InputSchema: obj(map[string]any{}),
			Annotations: annotations(annReadOnly),
			handler:     e.listRefs,
		},
	}
}

func (e *env) setActiveParent(_ context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	if boolArg(args, "clear", false) {
		if err := checkpoint.SetActiveParent(e.ws, ""); err != nil {
			return nil, err
		}
		return jsonResult(map[string]any{"activeParent": "", "latest": checkpoint.Latest(e.ws), "note": msgf(i18n.Msg{
			ES: "Sin padre activo: los checkpoints nuevos continúan desde el más nuevo.",
			EN: "No active parent: new checkpoints continue from the newest one.",
		})})
	}
	name, err := e.explicitCheckpoint(args)
	if err != nil {
		return nil, err
	}
	if err := checkpoint.SetActiveParent(e.ws, name); err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{"activeParent": checkpoint.ActiveParent(e.ws), "next": msgf(i18n.Msg{
		ES: "El próximo checkpoint cuelga de %s.", EN: "The next checkpoint will branch from %s.",
	}, name)})
}

// maxLabelRunes es el largo máximo de un rótulo (el mismo de la app: se muestra
// en una fila del árbol, no es una descripción).
const maxLabelRunes = 80

// cleanLabel normaliza un rótulo como RenameCheckpoint de la app: sin saltos ni
// caracteres de control y con los espacios recortados. Vacío es válido.
func cleanLabel(label string) (string, error) {
	label = strings.Join(strings.FieldsFunc(label, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
	if utf8.RuneCountInString(label) > maxLabelRunes {
		return "", i18n.Errorf(i18n.Msg{ES: "el nombre es demasiado largo (máximo %d letras)", EN: "the name is too long (at most %d characters)"}, maxLabelRunes)
	}
	return label, nil
}

// indexedWorkspace devuelve el workspace registrado en la app, o un error que
// dice por qué no se puede: rótulos y metadatos de la papelera viven en la base
// de la app.
func (e *env) indexedWorkspace() (store.Workspace, error) {
	w, ok := e.storedWorkspace()
	if !ok {
		return w, i18n.New(i18n.Msg{
			ES: "este workspace no está registrado en ScadLab (o la base de la app no existe): ábrelo en la app para poder rotular checkpoints",
			EN: "this workspace is not registered in ScadLab (or the app database does not exist): open it in the app to label checkpoints",
		})
	}
	return w, nil
}

// explicitCheckpoint resuelve un `file` que HAY que indicar. checkpointFile lee un
// nombre vacío como «el más nuevo», que sirve para leer o renderizar pero no para
// rotular ni mandar a la papelera: ahí un argumento olvidado no puede caer sobre
// un checkpoint que nadie nombró.
func (e *env) explicitCheckpoint(args map[string]any) (string, error) {
	file := strArg(args, "file")
	if strings.TrimSpace(file) == "" {
		return "", i18n.New(i18n.Msg{ES: "falta `file`: el nombre del checkpoint (ver list_checkpoints)", EN: "`file` is required: the checkpoint name (see list_checkpoints)"})
	}
	name, _, err := e.checkpointFile(file)
	return name, err
}

func (e *env) renameCheckpoint(_ context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	name, err := e.explicitCheckpoint(args)
	if err != nil {
		return nil, err
	}
	// Un modelo que no distingue label de description manda cualquiera de los dos.
	raw, given := args["label"].(string)
	if !given {
		for _, k := range []string{"description", "name"} {
			if v, ok := args[k].(string); ok {
				raw, given = v, true
				break
			}
		}
	}
	if !given {
		return nil, i18n.New(i18n.Msg{ES: "falta `label`: el nombre nuevo (vacío para quitarlo)", EN: "`label` is required: the new name (empty to remove it)"})
	}
	label, err := cleanLabel(raw)
	if err != nil {
		return nil, err
	}
	w, err := e.indexedWorkspace()
	if err != nil {
		return nil, err
	}
	cp, err := e.store.GetCheckpoint(w.ID, name)
	if errors.Is(err, store.ErrNotFound) { // recién creado: todavía sin escanear
		if err := e.syncIndex(w); err != nil {
			return nil, err
		}
		cp, err = e.store.GetCheckpoint(w.ID, name)
	}
	if err != nil {
		return nil, err
	}
	if err := e.store.SetCheckpointUI(w.ID, name, label, cp.Collapsed); err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{"file": name, "label": label, "note": msgf(i18n.Msg{
		ES: "El archivo no cambia de nombre ni la descripción de su cabecera (sus hijos lo referencian por el nombre del archivo): el árbol muestra este nombre en su lugar.",
		EN: "The file keeps its name and the description in its header is unchanged (its children reference it by file name): the tree shows this name instead.",
	})})
}

// syncIndex reescanea el workspace al índice de la base, como rescan de la app.
func (e *env) syncIndex(w store.Workspace) error {
	files, err := workspace.Scan(e.ws)
	if err != nil {
		return err
	}
	found := make([]store.Checkpoint, len(files))
	for i, f := range files {
		found[i] = store.Checkpoint{FileName: f.FileName, ParentFile: f.ParentFile, Description: f.Description, FileMtime: f.Mtime}
	}
	return e.store.SyncCheckpoints(w.ID, found)
}

// labels devuelve el rótulo de cada checkpoint según la base de la app ("" si no
// hay base o el workspace no está registrado).
func (e *env) labels() map[string]string {
	w, ok := e.storedWorkspace()
	if !ok {
		return nil
	}
	list, err := e.store.ListCheckpoints(w.ID)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, c := range list {
		if c.Label != "" {
			out[c.FileName] = c.Label
		}
	}
	return out
}

func (e *env) trashCheckpoint(_ context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	name, err := e.explicitCheckpoint(args)
	if err != nil {
		return nil, err
	}
	branch := boolArg(args, "branch", false)
	var names, relinked []string
	if branch {
		names, err = checkpoint.TrashBranch(e.ws, name)
	} else {
		var one string
		one, relinked, err = checkpoint.TrashRelinked(e.ws, name)
		if one != "" {
			names = []string{one}
		}
	}
	// Antes de que un rescan borre sus filas del índice se guarda lo que solo
	// vive en la base (rótulo, plegado, vínculo con el agente), también si la
	// operación quedó a medias: lo que se movió tiene que poder volver igual.
	e.stashTrashedMeta(names)
	if err != nil && len(names) == 0 {
		return nil, err
	}
	out := map[string]any{
		"trashed":      names,
		"activeParent": checkpoint.ActiveParent(e.ws),
		"latest":       checkpoint.Latest(e.ws),
		"note": msgf(i18n.Msg{
			ES: "Nada se borró: restore_checkpoint (trashed_name=%s) lo trae de vuelta.",
			EN: "Nothing was deleted: restore_checkpoint (trashed_name=%s) brings it back.",
		}, names[0]),
	}
	if len(relinked) > 0 {
		out["relinked"] = relinked
		out["relinkedNote"] = msgf(i18n.Msg{
			ES: "%d hijo(s) directo(s) pasaron a colgar del padre de %s (o quedaron como raíz): %s. restore_checkpoint los devuelve.",
			EN: "%d direct child(ren) now hang from the parent of %s (or became roots): %s. restore_checkpoint hangs them back.",
		}, len(relinked), name, strings.Join(relinked, ", "))
	}
	if err != nil { // una rama que se movió a medias
		out["error"] = err.Error()
		r, jerr := jsonResult(out)
		if jerr != nil {
			return nil, jerr
		}
		r.IsError = true
		return r, nil
	}
	return jsonResult(out)
}

// stashTrashedMeta guarda en la base lo que solo vive ahí de cada checkpoint que
// va a la papelera. Un fallo no impide nada: perder el rótulo es mejor que no
// poder descartar un checkpoint.
func (e *env) stashTrashedMeta(names []string) {
	if len(names) == 0 {
		return
	}
	w, ok := e.storedWorkspace()
	if !ok {
		return
	}
	items, err := checkpoint.ListTrash(e.ws)
	if err != nil {
		e.log.Printf("leer la papelera: %v", err)
		return
	}
	original := make(map[string]string, len(items))
	for _, it := range items {
		original[it.Name] = it.Original
	}
	for _, n := range names {
		if orig, ok := original[n]; ok {
			if err := e.store.StashCheckpointMeta(w.ID, orig, n); err != nil {
				e.log.Printf("guardar los datos de %s: %v", orig, err)
			}
		}
	}
}

func (e *env) listTrash(_ context.Context, _ map[string]any) (*toolResult, error) {
	e.settings()
	items, err := checkpoint.ListTrash(e.ws)
	if err != nil {
		return nil, err
	}
	return jsonResult(map[string]any{"trash": items})
}

func (e *env) restoreCheckpoint(_ context.Context, args map[string]any) (*toolResult, error) {
	e.settings()
	trashed := strings.TrimSpace(strArg(args, "trashed_name"))
	if trashed == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `trashed_name`: el nombre dentro de la papelera (ver list_trash)", EN: "`trashed_name` is required: the name inside the trash (see list_trash)"})
	}
	name, relinked, relinkErr := checkpoint.RestoreRelinked(e.ws, trashed)
	if name == "" {
		return nil, relinkErr
	}
	if w, ok := e.storedWorkspace(); ok {
		if err := e.store.RestoreCheckpointMeta(w.ID, trashed, name); err != nil {
			e.log.Printf("recuperar los datos de %s: %v", name, err)
		}
	}
	out := map[string]any{"file": name, "activeParent": checkpoint.ActiveParent(e.ws), "note": msgf(i18n.Msg{
		ES: "Restaurado como %s. No cambia el padre activo.", EN: "Restored as %s. The active parent is unchanged.",
	}, name)}
	if len(relinked) > 0 {
		out["relinked"] = relinked // hijos que volvieron a colgar de él
	}
	if relinkErr != nil { // volvió, pero algún hijo no se pudo re-colgar
		out["warning"] = relinkErr.Error()
	}
	return jsonResult(out)
}

func (e *env) listRefs(_ context.Context, _ map[string]any) (*toolResult, error) {
	e.settings()
	list, err := refmodel.List(e.ws)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"refs": list}
	if len(list) == 0 {
		out["note"] = msgf(i18n.Msg{
			ES: "refs/ está vacío: el usuario puede adjuntar un STL, 3MF u OBJ, o una imagen, en el chat o arrastrarlo a la ventana.",
			EN: "refs/ is empty: the user can attach an STL, 3MF or OBJ, or an image, in the chat or drag it onto the window.",
		})
	}
	return jsonResult(out)
}

// defaultParent es el padre por defecto de un checkpoint nuevo: el padre activo
// y, si no hay, el más nuevo ("" en un workspace vacío).
func (e *env) defaultParent() string {
	if p := checkpoint.ActiveParent(e.ws); p != "" {
		return p
	}
	return checkpoint.Latest(e.ws)
}
