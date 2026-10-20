package mcp

import (
	"context"
	"math"
	"regexp"
	"sort"
	"strings"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/render"
	"scadlab/backend/scad"
	"scadlab/backend/textedit"
)

// extract_module (S17, E): partir un modelo grande en módulos. Mueve los
// module/function que se nombran a lib/<nombre>.scad y deja en el checkpoint un
// include (por defecto) o un use. Un archivo de 600 líneas es caro de leer y de
// editar para un modelo chico; uno de 150 más un lib/ de 450 se edita por partes.
//
// La garantía es de equivalencia: se renderiza ANTES y DESPUÉS, y si cambia la
// caja o el volumen (tolerancia 1e-3) se restauran los dos archivos desde el
// historial y se dice por qué. Mover código no puede cambiar el modelo.

const (
	extractMaxModules = 20
	// Tolerancias de la comprobación de equivalencia.
	extractBoxTolMM  = 1e-3
	extractVolAbsMM3 = 1e-3
	extractVolRel    = 1e-5
)

var libNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func (e *env) extractModuleTool() Tool {
	return Tool{
		Name: "extract_module",
		Description: "Split a big checkpoint: MOVE the named modules/functions (top level) of `file` into lib/<lib>.scad and leave an `include <lib/<lib>.scad>` line in their place (mode \"use\" leaves `use <…>` instead). " +
			"The new lib file is created; if it already exists the code is appended after saving its previous version in its history (it never overwrites). Use it when a .scad is so long that reading or editing it costs too much: afterwards edit each part with outline_checkpoint / read_checkpoint / edit_checkpoint (file=\"lib/<lib>.scad\"). " +
			"It renders the checkpoint BEFORE and AFTER: if the bounding box or the volume change (tolerance 1e-3) both files are restored from their history and the tool says so, so moving code can never change the model. " +
			"Prefer include (default): the module still sees the file's variables (the Customizer parameters). With use, a module cannot see global variables or modules left behind, so name only self-contained ones (or pass what they need as arguments). Customizer parameters and the assembly (color() parts) stay in the checkpoint.",
		InputSchema: obj(map[string]any{
			"file":    strProp("Checkpoint to split (e.g. 003-pot.scad). Not a lib/ file."),
			"modules": map[string]any{"type": "array", "description": "Names of the modules and/or functions to move, e.g. [\"drain_holes\", \"rim\"]. A module and a function with the same name both move.", "items": map[string]any{"type": "string"}, "minItems": 1, "maxItems": extractMaxModules},
			"lib":     strProp("Library file name: \"parts\" (or lib/parts.scad). Letters, digits, - and _ only."),
			"mode":    enumProp("How the checkpoint refers to the library. Default include.", "include", "use"),
			"verbose": verboseProp,
		}, "file", "modules", "lib"),
		Annotations: annotations(annWrite),
		handler:     e.extractModule,
	}
}

// stringsArg saca una lista de textos: una lista, un texto con JSON de una lista o
// nombres separados por comas.
func stringsArg(args map[string]any, key string) []string {
	var out []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	switch v := args[key].(type) {
	case []any:
		for _, it := range v {
			if s, ok := it.(string); ok {
				add(s)
			}
		}
	case string:
		if list, ok := listArg(args, key); ok {
			for _, it := range list {
				if s, ok := it.(string); ok {
					add(s)
				}
			}
		} else {
			for _, s := range strings.Split(v, ",") {
				add(s)
			}
		}
	}
	return out
}

// libKeyFrom normaliza el argumento `lib` a «lib/<nombre>.scad».
func libKeyFrom(lib string) (string, error) {
	n := strings.TrimSpace(lib)
	n = strings.TrimPrefix(strings.ReplaceAll(n, `\`, "/"), "lib/")
	n = strings.TrimSuffix(n, ".scad")
	if !libNameRe.MatchString(n) {
		return "", i18n.Errorf(i18n.Msg{
			ES: "nombre de librería inválido: %q (usa letras, números, - y _, p. ej. \"piezas\" → lib/piezas.scad)",
			EN: "invalid library name: %q (use letters, digits, - and _, e.g. \"parts\" → lib/parts.scad)",
		}, clipRunes(lib, 60))
	}
	return checkpoint.LibDir + "/" + n + ".scad", nil
}

// defBlock es un module/function a mover: las líneas completas que ocupa, con los
// comentarios `//` que lo anteceden sin línea en blanco de por medio.
type defBlock struct {
	def        scad.Def
	from, to   int // líneas, 1-based, inclusivas
	text       string
	crossLines bool // comparte línea con otro código
}

// blockOf arma el bloque de la definición; ok=false si comparte línea con otro
// código (mover «x = 1; module a() {}» arrastraría x).
func blockOf(src string, lines []string, d scad.Def) (defBlock, bool) {
	b := defBlock{def: d, from: d.Line, to: d.End}
	startOfLine := strings.LastIndexByte(src[:d.Off], '\n') + 1
	if strings.TrimSpace(src[startOfLine:d.Off]) != "" {
		return b, false
	}
	endOfLine := len(src)
	if i := strings.IndexByte(src[d.EndOff:], '\n'); i >= 0 {
		endOfLine = d.EndOff + i
	}
	if rest := strings.TrimSpace(src[d.EndOff:endOfLine]); rest != "" && !strings.HasPrefix(rest, "//") {
		return b, false
	}
	// Comentarios de documentación inmediatamente arriba (no las secciones del
	// Customizer ni los comentarios de bloque).
	for b.from > 1 {
		prev := strings.TrimSpace(lines[b.from-2])
		// La cabecera `// @checkpoint` (línea 1) no es un comentario de nadie.
		if !strings.HasPrefix(prev, "//") || strings.Contains(prev, "@checkpoint") {
			break
		}
		b.from--
	}
	b.text = strings.Join(lines[b.from-1:b.to], "\n")
	return b, true
}

func (e *env) extractModule(ctx context.Context, args map[string]any) (*toolResult, error) {
	s := e.settings()
	verbose := verboseArg(args)
	ref, err := e.explicitTarget(strArg(args, "file"), "extract_module")
	if err != nil {
		return nil, err
	}
	if ref.Lib {
		return nil, i18n.New(i18n.Msg{
			ES: "extract_module trabaja sobre un checkpoint de la raíz, no sobre un archivo de lib/",
			EN: "extract_module works on a checkpoint from the workspace root, not on a lib/ file",
		})
	}
	libKey, err := libKeyFrom(strArg(args, "lib"))
	if err != nil {
		return nil, err
	}
	mode := strings.ToLower(strings.TrimSpace(strArg(args, "mode")))
	if mode == "" {
		mode = "include"
	}
	if mode != "include" && mode != "use" {
		return nil, i18n.New(i18n.Msg{ES: "`mode` es \"include\" (por defecto) o \"use\"", EN: "`mode` is \"include\" (default) or \"use\""})
	}
	names := stringsArg(args, "modules")
	if len(names) == 0 || len(names) > extractMaxModules {
		return nil, i18n.Errorf(i18n.Msg{ES: "`modules` es una lista de 1 a %d nombres de module o function", EN: "`modules` is a list of 1 to %d module or function names"}, extractMaxModules)
	}

	src, err := readTarget(ref)
	if err != nil {
		return nil, err
	}
	fd, err := scad.ListDefs(src)
	if err != nil {
		return nil, err
	}
	lines := textedit.Lines(src)
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var blocks []defBlock
	found := map[string]bool{}
	var remaining []scad.Def
	for _, d := range fd.Defs {
		if !want[d.Name] {
			remaining = append(remaining, d)
			continue
		}
		found[d.Name] = true
		b, ok := blockOf(src, lines, d)
		if !ok {
			return nil, i18n.Errorf(i18n.Msg{
				ES: "%s «%s» comparte línea con otro código (línea %d): sepáralo en su propia línea con edit_checkpoint y vuelve a intentarlo",
				EN: "%s “%s” shares a line with other code (line %d): put it on its own line with edit_checkpoint and try again",
			}, d.Kind, d.Name, d.Line)
		}
		blocks = append(blocks, b)
	}
	var missing []string
	for _, n := range names {
		if !found[n] {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		have := make([]string, 0, len(fd.Defs))
		for _, d := range fd.Defs {
			have = append(have, d.Name)
		}
		return nil, i18n.Errorf(i18n.Msg{
			ES: "no encuentro %s en el nivel superior de %s; hay: %s",
			EN: "I cannot find %s at the top level of %s; there are: %s",
		}, strings.Join(missing, ", "), ref.Key, clipRunes(strings.Join(have, ", "), 400))
	}
	sort.Slice(blocks, func(i, j int) bool { return blocks[i].from < blocks[j].from })

	// Con `use` lo movido no ve las variables globales ni los módulos que quedan.
	if mode == "use" {
		if err := checkUseIndependence(blocks, fd, remaining); err != nil {
			return nil, err
		}
	}

	// Contenido de la librería.
	var blockTexts []string
	for _, b := range blocks {
		blockTexts = append(blockTexts, b.text)
	}
	eol := "\n"
	if strings.Contains(src, "\r\n") {
		eol = "\r\n"
	}
	moved := strings.Join(blockTexts, "\n\n")
	libExists, err := checkpoint.LibExists(e.ws, libKey)
	if err != nil {
		return nil, err
	}
	var newLib, curLib string
	if libExists {
		libPath, perr := checkpoint.LibPath(e.ws, libKey)
		if perr != nil {
			return nil, perr
		}
		cur, rerr := readTarget(fileRef{Key: libKey, Path: libPath, Lib: true})
		if rerr != nil {
			return nil, rerr
		}
		curLib = cur
		if lfd, lerr := scad.ListDefs(cur); lerr == nil {
			have := map[string]bool{}
			for _, d := range lfd.Defs {
				have[d.Name] = true
			}
			for _, b := range blocks {
				if have[b.def.Name] {
					return nil, i18n.Errorf(i18n.Msg{
						ES: "%s ya define «%s»: elige otro nombre de librería o renombra uno de los dos",
						EN: "%s already defines “%s”: pick another library name or rename one of them",
					}, libKey, b.def.Name)
				}
			}
		}
		sep := "\n"
		if !strings.HasSuffix(cur, "\n") {
			sep = "\n\n"
		}
		newLib = cur + sep + moved + "\n"
	} else {
		newLib = msgf(i18n.Msg{
			ES: "// %s — módulos movidos desde %s por ScadLab (extract_module).\n// Se incluye desde el checkpoint con %s <%s>.\n\n",
			EN: "// %s — modules moved from %s by ScadLab (extract_module).\n// Pulled in from the checkpoint with %s <%s>.\n\n",
		}, libKey, ref.Key, mode, libKey) + moved + "\n"
	}
	if eol == "\r\n" {
		newLib = strings.ReplaceAll(strings.ReplaceAll(newLib, "\r\n", "\n"), "\n", "\r\n")
	}

	// Edición del checkpoint: el primer bloque se reemplaza por la línea de
	// include/use (si no estaba ya) y los demás se borran.
	includeLine := mode + " <" + libKey + ">"
	needInclude := !includesLib(src, libKey)
	var edits []textedit.Edit
	for i, b := range blocks {
		to := b.to
		// Un blanco de sobra: si queda una línea en blanco arriba y otra abajo, se va una.
		blankAbove := b.from == 1 || strings.TrimSpace(lines[b.from-2]) == ""
		if blankAbove && to < len(lines) && strings.TrimSpace(lines[to]) == "" {
			to++
		}
		repl := ""
		if i == 0 && needInclude {
			repl = includeLine
		}
		edits = append(edits, textedit.Edit{FromLine: b.from, ToLine: to, New: repl, LinesSet: true})
	}
	res, err := textedit.Apply(src, edits, textedit.Options{ProtectHeader: true})
	if err != nil {
		return nil, err
	}

	// Antes: el modelo tiene que renderizar para poder compararlo.
	before, err := e.renderInfo(ctx, ref.Key)
	if err != nil {
		return nil, err
	}

	// Escribir: primero la librería (el checkpoint nunca apunta a algo que no
	// existe), con respaldo previo si ya existía.
	note := msgf(i18n.Msg{ES: "extract_module: %s → %s", EN: "extract_module: %s → %s"}, strings.Join(names, ", "), libKey)
	var libVer, mainVer string
	if libExists {
		up, uerr := checkpoint.UpdateWith(e.ws, libKey, newLib, s.HistoryMaxVersions, checkpoint.UpdateOptions{Tool: "extract_module", HistoryNote: note, Base: &curLib})
		if uerr != nil {
			return nil, uerr
		}
		libVer = up.Version.ID
	} else if cerr := checkpoint.CreateLib(e.ws, libKey, newLib); cerr != nil {
		return nil, cerr
	}
	up, err := checkpoint.UpdateWith(e.ws, ref.Key, res.Text, s.HistoryMaxVersions, checkpoint.UpdateOptions{Tool: "extract_module", HistoryNote: note, Verbatim: true, Base: &src})
	if err != nil {
		e.undoExtract(ref.Key, "", libKey, newLib, libExists, libVer, s.HistoryMaxVersions)
		return nil, err
	}
	mainVer = up.Version.ID

	// Después: la caja y el volumen no pueden haber cambiado.
	after, aerr := e.renderInfo(ctx, ref.Key)
	if aerr != nil {
		e.undoExtract(ref.Key, mainVer, libKey, newLib, libExists, libVer, s.HistoryMaxVersions)
		return e.extractUndone(ref.Key, libKey, "", before.info, nil, aerr.Error())
	}
	if same, why := sameGeometry(before.info, after.info); !same {
		e.undoExtract(ref.Key, mainVer, libKey, newLib, libExists, libVer, s.HistoryMaxVersions)
		return e.extractUndone(ref.Key, libKey, why, before.info, &after.info, "")
	}
	return e.extractOK(ref, libKey, mode, blocks, libExists, src, res.Text, after, verbose)
}

// checkUseIndependence rechaza mover con `use` código que depende de variables
// globales o de módulos que se quedan en el checkpoint.
func checkUseIndependence(blocks []defBlock, fd scad.FileDefs, remaining []scad.Def) error {
	globals := map[string]bool{}
	for _, g := range fd.Globals {
		globals[g] = true
	}
	stay := map[string]bool{}
	for _, d := range remaining {
		stay[d.Name] = true
	}
	for _, b := range blocks {
		var vars, calls []string
		for _, v := range b.def.Vars {
			if globals[v] {
				vars = append(vars, v)
			}
		}
		for _, c := range b.def.Calls {
			if stay[c] {
				calls = append(calls, c)
			}
		}
		if len(vars) > 0 || len(calls) > 0 {
			dep := append(append([]string{}, vars...), calls...)
			return i18n.Errorf(i18n.Msg{
				ES: "con mode=use, «%s» no vería %s (se queda en el checkpoint): usa mode=include (por defecto), muévelo también, o pásalo como argumento. No se tocó nada",
				EN: "with mode=use, “%s” would not see %s (it stays in the checkpoint): use mode=include (default), move it too, or pass it as an argument. Nothing was touched",
			}, b.def.Name, strings.Join(dep, ", "))
		}
	}
	return nil
}

// renderedInfo es un render comparable.
type renderedInfo struct {
	info mesh.Info
	res  render.Result
}

// renderInfo renderiza name y devuelve su malla; un render que no sale bien es un
// error (no se puede comparar).
func (e *env) renderInfo(ctx context.Context, name string) (renderedInfo, error) {
	res, _, err := e.render(ctx, name)
	if err != nil {
		return renderedInfo{}, err
	}
	if res.Status != render.StatusOK {
		first := ""
		for _, d := range res.Diagnostics {
			if d.Severity == render.SeverityError {
				first = d.Message
				if d.Line > 0 {
					first = msgf(i18n.Msg{ES: "línea %d: %s", EN: "line %d: %s"}, d.Line, first)
				}
				break
			}
		}
		return renderedInfo{}, i18n.Errorf(i18n.Msg{
			ES: "%s no renderiza (%s): arréglalo antes de extraer módulos, para poder comprobar que el resultado es equivalente. No se tocó nada",
			EN: "%s does not render (%s): fix it before extracting modules, so the result can be checked for equivalence. Nothing was touched",
		}, name, clipRunes(first, 160))
	}
	info, err := meshInfoOf(res)
	if err != nil {
		return renderedInfo{}, err
	}
	return renderedInfo{info: info, res: res}, nil
}

// sameGeometry compara la caja y el volumen de dos mallas.
func sameGeometry(a, b mesh.Info) (bool, string) {
	for i, axis := range []string{"x", "y", "z"} {
		if math.Abs(a.Min[i]-b.Min[i]) > extractBoxTolMM || math.Abs(a.Max[i]-b.Max[i]) > extractBoxTolMM {
			return false, msgf(i18n.Msg{ES: "la caja cambió en %s", EN: "the bounding box changed in %s"}, axis)
		}
	}
	if math.Abs(a.VolumeMM3-b.VolumeMM3) > extractVolAbsMM3+extractVolRel*math.Abs(a.VolumeMM3) {
		return false, msgf(i18n.Msg{ES: "el volumen cambió", EN: "the volume changed"})
	}
	return true, ""
}

// undoExtract restaura lo que escribió extract_module: el checkpoint desde su
// historial y la librería, o desde el suyo si ya existía o —si la creamos
// nosotros— quitando el archivo recién creado (solo si sigue siendo exactamente lo
// que escribimos).
func (e *env) undoExtract(file, mainVer, libKey, libContent string, libExisted bool, libVer string, max int) {
	if mainVer != "" {
		if _, err := checkpoint.RestoreVersion(e.ws, file, mainVer, max); err != nil {
			e.log.Printf("extract_module: restaurar %s: %v", file, err)
		}
	}
	if libExisted {
		if libVer != "" {
			if _, err := checkpoint.RestoreVersion(e.ws, libKey, libVer, max); err != nil {
				e.log.Printf("extract_module: restaurar %s: %v", libKey, err)
			}
		}
		return
	}
	if err := checkpoint.RemoveCreatedLib(e.ws, libKey, libContent); err != nil {
		e.log.Printf("extract_module: deshacer la creación de %s: %v", libKey, err)
	}
}

func (e *env) extractUndone(file, libKey, why string, before mesh.Info, after *mesh.Info, renderErr string) (*toolResult, error) {
	out := ordered{{"file", file}, {"updated", false}, {"undone", true}}
	reason := why
	if renderErr != "" {
		reason = msgf(i18n.Msg{ES: "el checkpoint no renderiza tras mover el código: %s", EN: "the checkpoint does not render after moving the code: %s"}, clipRunes(renderErr, 200))
	}
	out.set("reason", reason)
	out.set("before", ordered{{"sizeMm", arr3(before.Size, 3)}, {"volumeCm3", round(before.VolumeMM3/1000, 3)}})
	if after != nil {
		out.set("after", ordered{{"sizeMm", arr3(after.Size, 3)}, {"volumeCm3", round(after.VolumeMM3/1000, 3)}})
	}
	out.set("message", msgf(i18n.Msg{
		ES: "Deshice el cambio: %s y %s volvieron a su versión anterior (queda en su historial). Lo movido dependía de algo que no viaja con el código; lo más común es una ruta relativa dentro del módulo (import(\"refs/x.stl\"), surface(), include): dentro de lib/ la ruta es relativa a lib/ (usa \"../refs/x.stl\"). Corrígelo con edit_checkpoint y vuelve a intentar, o deja ese módulo en el checkpoint.",
		EN: "I undid the change: %s and %s are back to their previous version (it stays in their history). What moved depended on something that does not travel with the code; most often a relative path inside the module (import(\"refs/x.stl\"), surface(), include): inside lib/ the path is relative to lib/ (use \"../refs/x.stl\"). Fix it with edit_checkpoint and try again, or keep that module in the checkpoint.",
	}, file, libKey))
	r, err := compactResult(out)
	if err != nil {
		return nil, err
	}
	r.IsError = true
	return r, nil
}

func (e *env) extractOK(ref fileRef, libKey, mode string, blocks []defBlock, libExisted bool, before, after string, ri renderedInfo, verbose bool) (*toolResult, error) {
	out := ordered{{"file", ref.Key}, {"updated", true}, {"lib", libKey}, {"libCreated", !libExisted}, {"mode", mode}}
	mv := make([]ordered, 0, len(blocks))
	for _, b := range blocks {
		mv = append(mv, ordered{{"name", b.def.Name}, {"kind", b.def.Kind}, {"was", rangeText(b.from, b.to)}})
	}
	out.set("moved", mv)
	out.set("lines", ordered{{"before", textedit.CountLines(before)}, {"after", textedit.CountLines(after)}})
	out.set("verified", msgf(i18n.Msg{ES: "caja y volumen idénticos antes y después", EN: "bounding box and volume identical before and after"}))
	s := e.settings()
	var ok ordered
	var cerr error
	if verbose {
		sum, serr := e.renderSummary(ref.Key, ri.res, s)
		if serr != nil {
			cerr = serr
		} else {
			ok = ordered{{"ok", true}}
			for _, k := range sortedKeys(sum) {
				ok.set(k, sum[k])
			}
		}
	} else {
		ok, cerr = e.compactRenderOK(ref.Key, ri.res, s)
		if cerr == nil {
			ok = append(ordered{{"ok", true}}, ok...)
		}
	}
	if cerr == nil {
		out.set("render", ok)
	}
	return compactResult(out)
}
