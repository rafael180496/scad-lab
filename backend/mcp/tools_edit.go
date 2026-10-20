package mcp

import (
	"context"
	"strings"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/textedit"
)

// edit_checkpoint (S17): cambiar unas líneas de un .scad sin regenerar el archivo.
// Medido con Antigravity, cada cambio a una maceta de 600 líneas pasaba por
// update_checkpoint con el archivo completo y tardaba entre 1:42 y 3:39 minutos.
// Con esto el modelo manda solo lo que cambia. La lógica (resolver cada edición
// contra el original, solapes, todo o nada, errores con la línea más parecida) es
// backend/textedit; la escritura es checkpoint.UpdateWith (respaldo → atómica).

func (e *env) editCheckpointTool() Tool {
	return Tool{
		Name: "edit_checkpoint",
		Description: "Change part of an existing .scad IN PLACE without rewriting it: send only the edits. Prefer it to update_checkpoint for any change that is not a full rewrite (a value, a few lines, a new module). " +
			"Each edit is ONE of two forms: {old, new} replaces the exact text `old`, which must appear exactly once (add the line above or below to make it unique); or {from_line, to_line, new} replaces that inclusive line range with `new` (numbers from read_checkpoint / outline_checkpoint / find_in_checkpoint; to_line = from_line - 1 INSERTS before from_line; from_line = number of lines + 1 appends at the end; `new` = \"\" deletes the lines; to_line defaults to from_line). " +
			"All edits are resolved against the file AS IT IS NOW (not one after another), so use the numbers you read; overlapping edits are an error; if any edit fails nothing is written. Line 1 (the `// @checkpoint` header) cannot be edited. " +
			"The previous version is saved in the file's history first (the user can undo). Then it runs lint and render and returns a short result with the lines you replaced. After an edit the line numbers below it may have moved: re-read before editing by lines again. `file` can also be a library file lib/<name>.scad.",
		InputSchema: obj(map[string]any{
			"file": strProp("Checkpoint to edit (e.g. 003-pot.scad) or a library file (lib/lid.scad)."),
			"edits": map[string]any{
				"type":        "array",
				"description": "The edits, each {old, new} or {from_line, to_line, new}.",
				"items": obj(map[string]any{
					"old":       strProp("Exact text to replace; must appear exactly once."),
					"new":       strProp("Replacement text (\"\" deletes)."),
					"from_line": map[string]any{"type": "integer", "description": "First line of the range (1-based).", "minimum": 1},
					"to_line":   map[string]any{"type": "integer", "description": "Last line of the range, inclusive. from_line - 1 inserts before from_line."},
				}),
				"minItems": 1,
			},
			"note":    strProp("Optional one-line description of the change; it replaces `desc` in the header and is kept in the history."),
			"verbose": verboseProp,
		}, "file", "edits"),
		Annotations: annotations(annWrite),
		handler:     e.editCheckpoint,
	}
}

// parseEdits convierte el argumento `edits` en ediciones de textedit.
func parseEdits(args map[string]any) ([]textedit.Edit, error) {
	var raw []any
	switch v := args["edits"].(type) {
	case map[string]any: // un solo objeto en vez de una lista
		raw = []any{v}
	default:
		list, ok := listArg(args, "edits")
		if !ok {
			return nil, i18n.New(i18n.Msg{
				ES: "falta `edits`: una lista de ediciones, cada una {old, new} o {from_line, to_line, new}",
				EN: "`edits` is required: a list of edits, each {old, new} or {from_line, to_line, new}",
			})
		}
		raw = list
	}
	if len(raw) == 0 {
		return nil, i18n.New(i18n.Msg{ES: "`edits` está vacío: manda al menos una edición", EN: "`edits` is empty: send at least one edit"})
	}
	out := make([]textedit.Edit, 0, len(raw))
	total := 0
	for i, it := range raw {
		m, ok := it.(map[string]any)
		if !ok {
			return nil, i18n.Errorf(i18n.Msg{ES: "edición %d: tiene que ser un objeto {old, new} o {from_line, to_line, new}", EN: "edit %d: must be an object {old, new} or {from_line, to_line, new}"}, i+1)
		}
		var ed textedit.Edit
		_, hasNew := m["new"]
		if !hasNew {
			return nil, i18n.Errorf(i18n.Msg{ES: "edición %d: falta `new` (usa \"\" para borrar)", EN: "edit %d: `new` is missing (use \"\" to delete)"}, i+1)
		}
		ed.New = strArg(m, "new")
		if _, has := m["old"]; has {
			ed.OldSet, ed.Old = true, strArg(m, "old")
		}
		if _, has := m["from_line"]; has {
			ed.LinesSet = true
			ed.FromLine = intArg(m, "from_line", 0)
			ed.ToLine = ed.FromLine
			if _, has := m["to_line"]; has {
				ed.ToLine = intArg(m, "to_line", ed.FromLine)
			}
		} else if _, has := m["to_line"]; has {
			return nil, i18n.Errorf(i18n.Msg{ES: "edición %d: `to_line` sin `from_line`", EN: "edit %d: `to_line` without `from_line`"}, i+1)
		}
		total += len(ed.Old) + len(ed.New)
		if total > maxSourceBytes {
			return nil, i18n.New(i18n.Msg{ES: "las ediciones son demasiado grandes (máximo 2 MB)", EN: "the edits are too large (2 MB at most)"})
		}
		out = append(out, ed)
	}
	return out, nil
}

func (e *env) editCheckpoint(ctx context.Context, args map[string]any) (*toolResult, error) {
	s := e.settings()
	ref, err := e.explicitTarget(strArg(args, "file"), "edit_checkpoint")
	if err != nil {
		return nil, err
	}
	edits, err := parseEdits(args)
	if err != nil {
		return nil, err
	}
	src, err := readTarget(ref)
	if err != nil {
		return nil, err
	}
	res, err := textedit.Apply(src, edits, textedit.Options{ProtectHeader: !ref.Lib})
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "%w. No se escribió nada", EN: "%w. Nothing was written"}, err)
	}
	if len(res.Text) > maxSourceBytes {
		return nil, i18n.New(i18n.Msg{ES: "el archivo quedaría demasiado grande (máximo 2 MB)", EN: "the file would be too large (2 MB at most)"})
	}
	verbose := verboseArg(args)
	if res.Unchanged {
		return compactResult(ordered{
			{"file", ref.Key}, {"updated", false}, {"unchanged", true},
			{"note", msgf(i18n.Msg{
				ES: "Las ediciones dejan el archivo idéntico: no se escribió nada ni se guardó respaldo.",
				EN: "The edits leave the file identical: nothing was written and no backup was saved.",
			})},
		})
	}
	up, err := checkpoint.UpdateWith(e.ws, ref.Key, res.Text, s.HistoryMaxVersions, checkpoint.UpdateOptions{
		Tool: "edit_checkpoint", Note: strArg(args, "note"), HistoryNote: editSummary(res.Changes), Verbatim: true, Base: &src,
	})
	if err != nil {
		return nil, err
	}

	out := ordered{{"file", ref.Key}, {"updated", true}, {"unchanged", up.Unchanged}}
	out.set("lines", textedit.CountLines(res.Text))
	if !up.Unchanged {
		out.set("version", up.Version.ID)
	}
	if !ref.Lib {
		out.set("description", up.Description)
	}
	out.set("changes", changesOut(res.Changes))
	e.verifyWritten(ctx, &out, ref, verbose)
	return compactResult(out)
}

// editSummary es la nota que queda en el historial: cuántas ediciones y dónde.
func editSummary(chs []textedit.Change) string {
	parts := make([]string, 0, len(chs))
	for _, c := range chs {
		parts = append(parts, rangeText(c.From, c.To))
		if len(parts) >= 8 {
			break
		}
	}
	return msgf(i18n.Msg{ES: "%d edición(es), líneas %s", EN: "%d edit(s), lines %s"}, len(chs), strings.Join(parts, ", "))
}

// rangeText escribe un rango de líneas: «12», «12-14» o «+12» para una inserción
// antes de la 12.
func rangeText(from, to int) string {
	switch {
	case to < from:
		return "+" + itoa(from)
	case to == from:
		return itoa(from)
	}
	return itoa(from) + "-" + itoa(to)
}

// Topes de lo que se muestra de cada cambio.
const (
	changeShownLines = 6
	changeShownRunes = 400
)

// clipBlock deja a lo más maxLines líneas y maxRunes runas de un bloque de texto.
func clipBlock(s string, maxLines, maxRunes int) string {
	lines := strings.Split(s, "\n")
	extra := 0
	if len(lines) > maxLines {
		extra = len(lines) - maxLines
		lines = lines[:maxLines]
	}
	out := clipKeep(strings.Join(lines, "\n"), maxRunes)
	if extra > 0 {
		out += "\n" + msgf(i18n.Msg{ES: "… (+%d líneas)", EN: "… (+%d lines)"}, extra)
	}
	return out
}

// changesOut es el detalle de cada cambio: qué líneas tocó, dónde quedaron y el
// texto de antes y de ahora (recortado), para que el modelo vea qué hizo.
func changesOut(chs []textedit.Change) []ordered {
	out := make([]ordered, 0, len(chs))
	for _, c := range chs {
		o := ordered{{"edit", c.Index}, {"was", rangeText(c.From, c.To)}}
		if c.NewTo < c.NewFrom {
			o.set("now", "deleted")
		} else {
			o.set("now", rangeText(c.NewFrom, c.NewTo))
		}
		if c.Old != "" {
			o.set("old", clipBlock(c.Old, changeShownLines, changeShownRunes))
		}
		if c.New != "" {
			o.set("new", clipBlock(c.New, changeShownLines, changeShownRunes))
		}
		out = append(out, o)
	}
	return out
}
