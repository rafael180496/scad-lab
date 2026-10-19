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
			Description: "Choose which checkpoint the NEXT checkpoint will branch from (the user's «Branch from here»). create_checkpoint, set_parameters, create_from_template and edit_mesh use it as the default parent and it advances to each new child by itself. " +
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
			Description: "Change the display name (label) the checkpoint tree shows for a checkpoint, e.g. «Lid with hinge». Checkpoint files are immutable and their children reference them by file name, so the file name and the description written in its header never change: the label is stored by the app (scadlab.db) next to the checkpoint and shown in its place. " +
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
			Description: "Move a checkpoint to the workspace trash (.scadlab/trash/). It NEVER deletes anything: restore_checkpoint brings it back. Its children are not moved (they keep pointing at it) unless branch=true, which moves the checkpoint and everything that derives from it. " +
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
			Description: "Bring a checkpoint back from the trash (see list_trash). It returns to its original name if that is free; otherwise it takes the next free number with the same slug (and then its children, which reference the old name, no longer link to it). Never overwrites a checkpoint.",
			InputSchema: obj(map[string]any{"trashed_name": strProp("Name inside the trash, as listed by list_trash (e.g. 20260101-120000-004-lid.scad).")}, "trashed_name"),
			Annotations: annotations(annWrite),
			handler:     e.restoreCheckpoint,
		},
		{
			Name: "list_refs",
			Description: "List the 3D models in the workspace's refs/ folder (STL, 3MF, OBJ): name, path, format, size and date. These are the models the user attached or imported; the originals are never modified. " +
				"Analyze one with inspect_reference and make it editable with import_mesh.",
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
		ES: "El archivo y la descripción de su cabecera no cambian (el checkpoint es inmutable): el árbol muestra este nombre en su lugar.",
		EN: "The file and the description in its header do not change (the checkpoint is immutable): the tree shows this name instead.",
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
	var names []string
	if branch {
		names, err = checkpoint.TrashBranch(e.ws, name)
	} else {
		var one string
		one, err = checkpoint.Trash(e.ws, name)
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
	name, err := checkpoint.Restore(e.ws, trashed)
	if err != nil {
		return nil, err
	}
	if w, ok := e.storedWorkspace(); ok {
		if err := e.store.RestoreCheckpointMeta(w.ID, trashed, name); err != nil {
			e.log.Printf("recuperar los datos de %s: %v", name, err)
		}
	}
	return jsonResult(map[string]any{"file": name, "activeParent": checkpoint.ActiveParent(e.ws), "note": msgf(i18n.Msg{
		ES: "Restaurado como %s. No cambia el padre activo.", EN: "Restored as %s. The active parent is unchanged.",
	}, name)})
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
			ES: "refs/ está vacío: el usuario puede adjuntar un STL, 3MF u OBJ en el chat o arrastrarlo a la ventana.",
			EN: "refs/ is empty: the user can attach an STL, 3MF or OBJ in the chat or drag it onto the window.",
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
