package mcp

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/scad"
	"scadlab/backend/textedit"
)

// Leer menos (S17, B). Un modelo chico no puede leer un .scad de 600 líneas cada
// vez que quiere cambiar tres: outline_checkpoint le da el mapa, find_in_checkpoint
// le dice dónde está lo que busca y read_checkpoint con from/to le trae solo ese
// rango, con los números de línea que edit_checkpoint usa después.

// Topes de lo que devuelven las herramientas de lectura.
const (
	readMaxBytes    = 12 << 10 // read_checkpoint sin rango: el archivo entero si cabe, si no el comienzo
	readMaxLines    = 400      // read_checkpoint con rango: líneas por llamada
	readRangeBytes  = 24 << 10 // y bytes, por si las líneas son larguísimas
	outlineMaxBytes = 5 << 10  // el plan dice «~4 KB»: una maceta de 600 líneas con 50 parámetros y 30 módulos entra con sus valores
	findMaxMatches  = 30
	findMaxBytes    = 4 << 10
	findMaxContext  = 10
	lineClipRunes   = 240 // una línea más larga se recorta al mostrarla
)

func (e *env) outlineCheckpointTool() Tool {
	return Tool{
		Name: "outline_checkpoint",
		Description: "Map of a .scad WITHOUT reading it (a few KB for a 600-line file, instead of its 20+ KB): Customizer parameters by section (name, line, value), modules and functions with their first and last line and arguments, include/use lines and the color() parts. " +
			"Call it first on a big file; then read just the range you will change with read_checkpoint(from, to) or locate text with find_in_checkpoint, and change it with edit_checkpoint. `file` is a checkpoint (003-box.scad) or a library file of the workspace (lib/lid.scad).",
		InputSchema: obj(map[string]any{"file": strProp(fileHelp)}),
		Annotations: annotations(annReadOnly),
		handler:     e.outlineCheckpoint,
	}
}

func (e *env) findInCheckpointTool() Tool {
	return Tool{
		Name: "find_in_checkpoint",
		Description: "Search a .scad for text (case-insensitive; regex=true for a regular expression) and get numbered windows `N| code` with `context` lines around each match (match lines are marked `N:`), merged, at most 30 matches and about 4 KB. " +
			"Use it to find the lines to change instead of reading the file; the numbers are the ones edit_checkpoint takes as from_line/to_line. With no match it suggests the most similar lines.",
		InputSchema: obj(map[string]any{
			"file":    strProp(fileHelp),
			"query":   strProp("Text to look for (or a Go/RE2 regular expression with regex=true)."),
			"context": map[string]any{"type": "integer", "description": "Lines of context around each match. Default 2, at most 10.", "minimum": 0, "maximum": findMaxContext},
			"regex":   map[string]any{"type": "boolean", "description": "Treat `query` as a regular expression. Default false."},
		}, "query"),
		Annotations: annotations(annReadOnly),
		handler:     e.findInCheckpoint,
	}
}

func (e *env) readCheckpointTool() Tool {
	return Tool{
		Name: "read_checkpoint",
		Description: "Return the OpenSCAD source of a checkpoint by its file name (e.g. 003-add-hinge.scad; no paths), or of a library file of the workspace (lib/lid.scad). This is the way to read a .scad: never use the CLI's own file tools or a path for it. " +
			"Without from/to: the code as is if it is up to 12 KB, otherwise its first 12 KB and a note that it was cut. With from/to (1-based, inclusive): numbered lines `N| code`, at most 400 per call — read just the part you will change, after outline_checkpoint or find_in_checkpoint. " +
			"full=true returns the whole file whatever its size (only when you will rewrite all of it with update_checkpoint).",
		InputSchema: obj(map[string]any{
			"file": strProp(fileHelp),
			"from": map[string]any{"type": "integer", "description": "First line to read (1-based). Gives numbered lines `N| code`.", "minimum": 1},
			"to":   map[string]any{"type": "integer", "description": "Last line to read, inclusive. Default: from + 399 (at most 400 lines per call).", "minimum": 1},
			"full": map[string]any{"type": "boolean", "description": "Return the whole file, however big. Default false."},
		}),
		Annotations: annotations(annReadOnly),
		handler:     e.readCheckpoint,
	}
}

func (e *env) readCheckpoint(_ context.Context, args map[string]any) (*toolResult, error) {
	ref, err := e.targetFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	src, err := readTarget(ref)
	if err != nil {
		return nil, err
	}
	from, to := intArg(args, "from", 0), intArg(args, "to", 0)
	if from > 0 || to > 0 {
		text, rerr := readRange(ref.Key, src, from, to)
		if rerr != nil {
			return nil, rerr
		}
		return textResult(text), nil
	}
	if boolArg(args, "full", false) || len(src) <= readMaxBytes {
		return textResult(src), nil
	}
	return textResult(truncatedRead(src)), nil
}

// readRange devuelve las líneas from..to numeradas `N| código`.
func readRange(key, src string, from, to int) (string, error) {
	lines := textedit.Lines(src)
	total := len(lines)
	if from < 1 {
		from = 1
	}
	if total == 0 {
		return msgf(i18n.Msg{ES: "[%s está vacío: 0 líneas]", EN: "[%s is empty: 0 lines]"}, key), nil
	}
	if from > total {
		return "", i18n.Errorf(i18n.Msg{ES: "%s tiene %d líneas: from=%d está fuera del archivo", EN: "%s has %d lines: from=%d is outside the file"}, key, total, from)
	}
	if to > 0 && to < from {
		return "", i18n.Errorf(i18n.Msg{ES: "to (%d) no puede ser menor que from (%d)", EN: "to (%d) cannot be less than from (%d)"}, to, from)
	}
	capped := false
	if to < from {
		to = from + readMaxLines - 1
	}
	if to > total {
		to = total
	}
	if to-from+1 > readMaxLines {
		to, capped = from+readMaxLines-1, true
	}
	var b strings.Builder
	shown := from - 1
	for n := from; n <= to; n++ {
		l := clipKeep(lines[n-1], lineClipRunes*4)
		if n > from && b.Len()+len(l) > readRangeBytes {
			capped = true
			break
		}
		fmt.Fprintf(&b, "%d| %s\n", n, l)
		shown = n
	}
	head := msgf(i18n.Msg{ES: "[%s, líneas %d-%d de %d]", EN: "[%s, lines %d-%d of %d]"}, key, from, shown, total)
	tail := ""
	switch {
	case capped && shown < total:
		tail = msgf(i18n.Msg{ES: "[sigue: from=%d (máximo %d líneas por llamada)]", EN: "[continues: from=%d (at most %d lines per call)]"}, shown+1, readMaxLines)
	case shown < total:
		tail = msgf(i18n.Msg{ES: "[sigue: from=%d]", EN: "[continues: from=%d]"}, shown+1)
	}
	out := head + "\n" + strings.TrimRight(b.String(), "\n")
	if tail != "" {
		out += "\n" + tail
	}
	return out, nil
}

// truncatedRead devuelve el comienzo del archivo (hasta readMaxBytes, cortado en
// una línea completa) y la nota de que hay más.
func truncatedRead(src string) string {
	cut := readMaxBytes
	if i := strings.LastIndexByte(src[:cut], '\n'); i > 0 {
		cut = i + 1
	}
	// No partir una runa.
	for cut > 0 && cut < len(src) && !isRuneStart(src[cut]) {
		cut--
	}
	head := src[:cut]
	total := textedit.CountLines(src)
	note := msgf(i18n.Msg{
		ES: "[truncado: %d líneas, %.1f KB; usa outline_checkpoint / find_in_checkpoint y read_checkpoint con from/to, o full=true]",
		EN: "[truncated: %d lines, %.1f KB; use outline_checkpoint / find_in_checkpoint and read_checkpoint with from/to, or full=true]",
	}, total, float64(len(src))/1024)
	return strings.TrimRight(head, "\n") + "\n" + note
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// --- outline ----------------------------------------------------------------

func (e *env) outlineCheckpoint(_ context.Context, args map[string]any) (*toolResult, error) {
	ref, err := e.targetFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	src, err := readTarget(ref)
	if err != nil {
		return nil, err
	}
	o, err := scad.BuildOutline(src)
	if err != nil {
		// Un string o un comentario sin cerrar impide leer la estructura: se dice
		// dónde y se dan al menos las medidas del archivo.
		return compactResult(ordered{
			{"file", ref.Key}, {"lines", o.Lines}, {"bytes", o.Bytes},
			{"error", err.Error()},
			{"note", msgf(i18n.Msg{
				ES: "No se pudo leer la estructura hasta arreglar eso. Usa find_in_checkpoint o read_checkpoint con from/to.",
				EN: "The structure cannot be read until that is fixed. Use find_in_checkpoint or read_checkpoint with from/to.",
			})},
		})
	}
	return compactResult(fitOutline(ref.Key, o))
}

// fitOutline arma el esquema y lo achica hasta que quepa en outlineMaxBytes:
// primero recorta valores y argumentos, después los quita y, si aun así no cabe,
// deja solo los primeros parámetros de cada sección.
func fitOutline(file string, o scad.Outline) ordered {
	build := func(valueRunes, argRunes, maxItems int) ordered {
		params := make([]scad.OutlineSection, len(o.Params))
		omitted := 0
		for i, s := range o.Params {
			items := make([]scad.OutlineItem, 0, len(s.Items))
			for j, it := range s.Items {
				if maxItems > 0 && j >= maxItems {
					omitted += len(s.Items) - j
					break
				}
				if valueRunes == 0 {
					it.Value = ""
				} else {
					it.Value = clipRunes(it.Value, valueRunes)
				}
				items = append(items, it)
			}
			params[i] = scad.OutlineSection{Section: s.Section, Items: items}
		}
		clipDefs := func(in []scad.OutlineDef) []scad.OutlineDef {
			out := make([]scad.OutlineDef, len(in))
			for i, d := range in {
				d.Args = clipRunes(d.Args, argRunes)
				if argRunes == 0 {
					d.Args = ""
				}
				out[i] = d
			}
			return out
		}
		res := ordered{{"file", file}, {"lines", o.Lines}, {"bytes", o.Bytes}}
		res.set("params", params)
		res.set("modules", clipDefs(o.Modules))
		res.set("functions", clipDefs(o.Functions))
		res.set("includes", o.Includes)
		res.set("parts", o.Parts)
		if omitted > 0 {
			res.set("omitted", msgf(i18n.Msg{ES: "%d parámetros más; usa get_parameters o find_in_checkpoint", EN: "%d more parameters; use get_parameters or find_in_checkpoint"}, omitted))
		}
		return res
	}
	size := func(o ordered) int { b, _ := marshalNoEscape(o); return len(b) }
	for _, step := range [][3]int{{40, 60, 0}, {24, 36, 0}, {24, 0, 0}, {12, 0, 0}, {0, 0, 0}, {0, 0, 12}, {0, 0, 6}, {0, 0, 3}} {
		out := build(step[0], step[1], step[2])
		if size(out) <= outlineMaxBytes {
			return out
		}
	}
	return build(0, 0, 3)
}

// --- find -------------------------------------------------------------------

func (e *env) findInCheckpoint(_ context.Context, args map[string]any) (*toolResult, error) {
	query := strArg(args, "query")
	if strings.TrimSpace(query) == "" {
		return nil, i18n.New(i18n.Msg{ES: "falta `query`: el texto que hay que buscar", EN: "`query` is required: the text to look for"})
	}
	ref, err := e.targetFile(strArg(args, "file"))
	if err != nil {
		return nil, err
	}
	src, err := readTarget(ref)
	if err != nil {
		return nil, err
	}
	ctxLines := intArg(args, "context", 2)
	ctxLines = max(0, min(findMaxContext, ctxLines))

	var match func(string) bool
	if boolArg(args, "regex", false) {
		if len(query) > 300 {
			return nil, i18n.New(i18n.Msg{ES: "la expresión regular es demasiado larga (máximo 300 caracteres)", EN: "the regular expression is too long (300 characters at most)"})
		}
		re, rerr := regexp.Compile(query)
		if rerr != nil {
			return nil, i18n.Errorf(i18n.Msg{ES: "expresión regular inválida: %v", EN: "invalid regular expression: %v"}, rerr)
		}
		match = re.MatchString
	} else {
		q := strings.ToLower(query)
		match = func(l string) bool { return strings.Contains(strings.ToLower(l), q) }
	}
	return textResult(findWindows(ref.Key, src, query, ctxLines, match)), nil
}

// findWindows arma la respuesta de find_in_checkpoint.
func findWindows(key, src, query string, ctxLines int, match func(string) bool) string {
	lines := textedit.Lines(src)
	var hits []int
	for i, l := range lines {
		if match(l) {
			hits = append(hits, i+1)
		}
	}
	if len(hits) == 0 {
		out := msgf(i18n.Msg{ES: "[%s: sin coincidencias de «%s» en %d líneas]", EN: "[%s: no matches of «%s» in %d lines]"}, key, clipRunes(query, 60), len(lines))
		if sim := textedit.Closest(src, query, 3); len(sim) > 0 {
			var b strings.Builder
			b.WriteString("\n" + msgf(i18n.Msg{ES: "Las líneas más parecidas:", EN: "The most similar lines:"}))
			for _, m := range sim {
				fmt.Fprintf(&b, "\n%d| %s", m.Line, clipKeep(m.Text, lineClipRunes))
			}
			out += b.String()
		}
		return out
	}
	shownHits := hits
	if len(shownHits) > findMaxMatches {
		shownHits = shownHits[:findMaxMatches]
	}
	isHit := make(map[int]bool, len(shownHits))
	for _, h := range shownHits {
		isHit[h] = true
	}
	// Ventanas fusionadas.
	type win struct{ from, to int }
	var wins []win
	for _, h := range shownHits {
		f, t := max(1, h-ctxLines), min(len(lines), h+ctxLines)
		if n := len(wins); n > 0 && f <= wins[n-1].to+1 {
			wins[n-1].to = max(wins[n-1].to, t)
			continue
		}
		wins = append(wins, win{f, t})
	}
	sort.Slice(wins, func(a, b int) bool { return wins[a].from < wins[b].from })

	var b strings.Builder
	shown := 0
	for wi, w := range wins {
		var wb strings.Builder
		if wi > 0 {
			wb.WriteString("--\n")
		}
		winHits := 0
		for n := w.from; n <= w.to; n++ {
			sep := "|"
			if isHit[n] {
				sep, winHits = ":", winHits+1
			}
			fmt.Fprintf(&wb, "%d%s %s\n", n, sep, clipKeep(lines[n-1], lineClipRunes))
		}
		if wi > 0 && b.Len()+wb.Len() > findMaxBytes {
			break
		}
		b.WriteString(wb.String())
		shown += winHits
	}
	head := msgf(i18n.Msg{ES: "[%s: %d coincidencias de «%s»]", EN: "[%s: %d matches of «%s»]"}, key, len(hits), clipRunes(query, 60))
	out := head + "\n" + strings.TrimRight(b.String(), "\n")
	if shown < len(hits) {
		out += "\n" + msgf(i18n.Msg{
			ES: "[se muestran %d de %d coincidencias: afina la búsqueda o usa context=0]",
			EN: "[showing %d of %d matches: narrow the query or use context=0]",
		}, shown, len(hits))
	}
	return out
}
