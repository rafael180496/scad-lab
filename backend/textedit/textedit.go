// Package textedit aplica ediciones pequeñas a un texto sin tener que volver a
// escribirlo entero (S17). Es lógica pura, sin disco ni OpenSCAD: la usa la
// herramienta MCP edit_checkpoint, y existe para que un modelo chico (un Ollama
// de 8k de contexto) cambie tres líneas de un .scad de 600 sin leerlo ni
// regenerarlo.
//
// Una edición es de una de dos formas:
//
//   - {Old, New}: reemplazo exacto; Old tiene que aparecer exactamente UNA vez.
//   - {FromLine, ToLine, New}: reemplaza el rango de líneas inclusivo (1-based).
//     ToLine = FromLine-1 inserta antes de FromLine; FromLine = líneas+1 agrega al
//     final. Los modelos chicos copian mal el texto exacto pero leen bien los
//     números de línea que se les dieron.
//
// Todas se resuelven contra el texto TAL COMO ESTÁ antes del cambio (los números
// de línea son los que devolvió la lectura), no una tras otra. Si dos se solapan
// o una falla, no se aplica ninguna: todo o nada.
//
// Se conserva byte a byte lo que no se toca: el BOM, el fin de línea de cada
// línea (\n, \r\n, mezclados), el "sin salto final" y los tabuladores. Los fines
// de línea de lo que se inserta toman el estilo dominante del texto.
package textedit

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"scadlab/backend/i18n"
)

// Edit es una edición. Exactamente una de las dos formas: OldSet (Old/New) o
// LinesSet (FromLine/ToLine/New).
type Edit struct {
	Old      string
	New      string
	OldSet   bool
	FromLine int
	ToLine   int
	LinesSet bool
}

// Options ajusta cómo se aplican las ediciones.
type Options struct {
	// ProtectHeader: si la primera línea es una cabecera `// @checkpoint …` no se
	// puede editar ni insertar nada antes de ella (es metadato del árbol, no del
	// modelo).
	ProtectHeader bool
}

// Change es lo que hizo una edición, para mostrarlo al modelo.
type Change struct {
	Index    int    // posición de la edición en la lista pedida, desde 1
	Kind     string // "text" | "lines"
	From, To int    // líneas ORIGINALES que tocó (To = From-1 si solo insertó)
	Old      string // esas líneas, sin saltos finales, unidas con "\n" ("" si insertó)
	NewFrom  int    // líneas en el RESULTADO donde quedó lo nuevo
	NewTo    int    // (NewTo = NewFrom-1 si se borró)
	New      string // esas líneas del resultado
}

// Result es el texto resultante y qué se cambió, en el orden del archivo.
type Result struct {
	Text      string
	Changes   []Change
	Unchanged bool // el resultado es idéntico al original
}

// maxEdits acota cuántas ediciones entran en una llamada.
const maxEdits = 200

var headerRe = regexp.MustCompile(`^\s*//\s*@checkpoint\b`)

// --- índice de líneas -------------------------------------------------------

// index sabe dónde empieza y termina cada línea de s. s no lleva BOM.
type index struct {
	s      string
	starts []int // inicio de cada línea
	ends   []int // fin del texto de la línea (antes de \r\n o \n)
	nexts  []int // inicio de la siguiente (después del terminador)
}

func newIndex(s string) index {
	ix := index{s: s}
	pos := 0
	for pos < len(s) {
		nl := strings.IndexByte(s[pos:], '\n')
		if nl < 0 {
			ix.starts = append(ix.starts, pos)
			ix.ends = append(ix.ends, len(s))
			ix.nexts = append(ix.nexts, len(s))
			break
		}
		end := pos + nl
		next := end + 1
		if end > pos && s[end-1] == '\r' {
			end--
		}
		ix.starts = append(ix.starts, pos)
		ix.ends = append(ix.ends, end)
		ix.nexts = append(ix.nexts, next)
		pos = next
	}
	return ix
}

func (ix index) count() int { return len(ix.starts) }

// line devuelve el texto de la línea n (1-based) sin su terminador.
func (ix index) line(n int) string { return ix.s[ix.starts[n-1]:ix.ends[n-1]] }

// lineOf devuelve la línea (1-based) que contiene el desplazamiento off. Un
// desplazamiento al final de un texto que termina en salto cae en la línea
// siguiente a la última (count+1), que es donde se escribiría.
func (ix index) lineOf(off int) int {
	i := sort.Search(len(ix.starts), func(i int) bool { return ix.starts[i] > off })
	return i // i es la cantidad de líneas que empiezan en o antes de off
}

// stripBOM separa el BOM del texto.
func stripBOM(s string) (bom, rest string) {
	if strings.HasPrefix(s, "\uFEFF") {
		return "\uFEFF", s[len("\uFEFF"):]
	}
	return "", s
}

// Lines devuelve las líneas de src sin terminadores ni BOM. Un texto vacío no
// tiene líneas; un texto que termina en salto no tiene una última línea vacía.
// Es la numeración de todas las herramientas de lectura y de edición.
func Lines(src string) []string {
	_, rest := stripBOM(src)
	ix := newIndex(rest)
	out := make([]string, ix.count())
	for i := range out {
		out[i] = ix.line(i + 1)
	}
	return out
}

// CountLines es len(Lines(src)) sin armar la lista.
func CountLines(src string) int {
	_, rest := stripBOM(src)
	return newIndex(rest).count()
}

// dominantEOL es "\r\n" si el texto tiene más finales CRLF que LF sueltos.
func dominantEOL(s string) string {
	crlf := strings.Count(s, "\r\n")
	lf := strings.Count(s, "\n") - crlf
	if crlf > lf {
		return "\r\n"
	}
	return "\n"
}

// toEOL lleva todos los saltos de s al estilo eol.
func toEOL(s, eol string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if eol == "\r\n" {
		s = strings.ReplaceAll(s, "\n", "\r\n")
	}
	return s
}

// --- aplicar ----------------------------------------------------------------

// span es una edición ya resuelta a un rango de bytes del texto original.
type span struct {
	idx    int // índice en la lista pedida
	kind   string
	s, e   int // rango [s,e) en el texto original
	repl   string
	insert bool
	line   int // inserción: la línea ANTES de la cual se inserta
	lead   int // bytes iniciales de repl que son solo el separador de línea
}

// Apply aplica edits a src. Ver el comentario del paquete. Los errores nombran
// la edición (desde 1) y dicen cómo corregirla.
func Apply(src string, edits []Edit, opts Options) (Result, error) {
	if len(edits) == 0 {
		return Result{}, i18n.New(i18n.Msg{ES: "no hay ediciones: `edits` está vacío", EN: "there are no edits: `edits` is empty"})
	}
	if len(edits) > maxEdits {
		return Result{}, i18n.Errorf(i18n.Msg{ES: "demasiadas ediciones (%d): el máximo es %d por llamada", EN: "too many edits (%d): the limit is %d per call"}, len(edits), maxEdits)
	}
	bom, body := stripBOM(src)
	ix := newIndex(body)
	eol := dominantEOL(body)

	protEnd := -1
	if opts.ProtectHeader && ix.count() > 0 && headerRe.MatchString(ix.line(1)) {
		protEnd = ix.ends[0]
	}

	spans := make([]span, 0, len(edits))
	for i, ed := range edits {
		n := i + 1
		var sp span
		var err error
		switch {
		case ed.OldSet && ed.LinesSet:
			return Result{}, i18n.Errorf(i18n.Msg{
				ES: "edición %d: usa una sola forma, {old, new} o {from_line, to_line, new}, no las dos",
				EN: "edit %d: use one form only, {old, new} or {from_line, to_line, new}, not both",
			}, n)
		case ed.OldSet:
			sp, err = resolveText(ix, eol, n, ed)
		case ed.LinesSet:
			sp, err = resolveLines(ix, eol, n, ed)
		default:
			return Result{}, i18n.Errorf(i18n.Msg{
				ES: "edición %d: falta `old` (texto exacto a reemplazar) o `from_line`/`to_line` (rango de líneas)",
				EN: "edit %d: `old` (exact text to replace) or `from_line`/`to_line` (line range) is missing",
			}, n)
		}
		if err != nil {
			return Result{}, err
		}
		if protEnd >= 0 && sp.s < protEnd {
			return Result{}, i18n.Errorf(i18n.Msg{
				ES: "edición %d: la línea 1 es la cabecera `// @checkpoint` y no se edita (es metadato del árbol). Empieza desde la línea 2",
				EN: "edit %d: line 1 is the `// @checkpoint` header and cannot be edited (it is tree metadata). Start from line 2",
			}, n)
		}
		spans = append(spans, sp)
	}

	// Orden del archivo; a igualdad, las inserciones (rango vacío) van antes y se
	// respeta el orden pedido.
	order := make([]span, len(spans))
	copy(order, spans)
	sort.SliceStable(order, func(a, b int) bool {
		if order[a].s != order[b].s {
			return order[a].s < order[b].s
		}
		return order[a].e < order[b].e
	})
	for i := 1; i < len(order); i++ {
		prev, cur := order[i-1], order[i]
		if cur.s < prev.e {
			return Result{}, i18n.Errorf(i18n.Msg{
				ES: "las ediciones %d y %d se solapan (tocan las mismas líneas): únelas en una sola o separa los rangos. Todas se resuelven contra el archivo original, no una tras otra",
				EN: "edits %d and %d overlap (they touch the same lines): merge them into one or separate the ranges. All of them are resolved against the original file, not one after another",
			}, min(prev.idx, cur.idx), max(prev.idx, cur.idx))
		}
	}

	var out strings.Builder
	out.Grow(len(body))
	type placed struct {
		sp       span
		newStart int
	}
	placedAt := make([]placed, 0, len(order))
	cursor := 0
	for _, sp := range order {
		out.WriteString(body[cursor:sp.s])
		placedAt = append(placedAt, placed{sp: sp, newStart: out.Len()})
		out.WriteString(sp.repl)
		cursor = sp.e
	}
	out.WriteString(body[cursor:])
	result := out.String()

	res := Result{Text: bom + result}
	if result == body {
		res.Unchanged = true
	}
	nx := newIndex(result)
	for _, p := range placedAt {
		sp := p.sp
		ch := Change{Index: sp.idx, Kind: sp.kind}
		if sp.insert {
			ch.From = sp.line
			ch.To = sp.line - 1
		} else {
			ch.From = ix.lineOf(sp.s)
			ch.To = ix.lineOf(sp.e - 1)
			ch.Old = joinLines(ix, ch.From, ch.To)
		}
		ch.NewFrom = nx.lineOf(p.newStart + sp.lead)
		if len(sp.repl) > sp.lead {
			ch.NewTo = nx.lineOf(p.newStart + len(sp.repl) - 1)
			ch.New = joinLines(nx, ch.NewFrom, ch.NewTo)
		} else {
			ch.NewTo = ch.NewFrom - 1
		}
		res.Changes = append(res.Changes, ch)
	}
	return res, nil
}

func joinLines(ix index, from, to int) string {
	if from < 1 {
		from = 1
	}
	if to > ix.count() {
		to = ix.count()
	}
	if to < from {
		return ""
	}
	parts := make([]string, 0, to-from+1)
	for n := from; n <= to; n++ {
		parts = append(parts, ix.line(n))
	}
	return strings.Join(parts, "\n")
}

// resolveText resuelve {old, new} a un rango de bytes.
func resolveText(ix index, eol string, n int, ed Edit) (span, error) {
	if ed.Old == "" {
		return span{}, i18n.Errorf(i18n.Msg{
			ES: "edición %d: `old` está vacío; para insertar usa from_line/to_line (to_line = from_line - 1 inserta antes de from_line)",
			EN: "edit %d: `old` is empty; to insert use from_line/to_line (to_line = from_line - 1 inserts before from_line)",
		}, n)
	}
	if ed.Old == ed.New {
		return span{}, i18n.Errorf(i18n.Msg{
			ES: "edición %d: `old` y `new` son idénticos, no cambiaría nada",
			EN: "edit %d: `old` and `new` are identical, nothing would change",
		}, n)
	}
	// El texto tal cual y, si el archivo usa CRLF, el mismo con sus saltos: un
	// modelo escribe "\n" aunque el archivo sea de Windows.
	cands := []string{ed.Old}
	if alt := toEOL(ed.Old, eol); alt != ed.Old {
		cands = append(cands, alt)
	}
	for _, old := range cands {
		hits := occurrences(ix.s, old)
		switch len(hits) {
		case 0:
			continue
		case 1:
			return span{idx: n, kind: "text", s: hits[0], e: hits[0] + len(old), repl: toEOL(ed.New, eol)}, nil
		default:
			lines := make([]int, 0, len(hits))
			for _, h := range hits {
				lines = append(lines, ix.lineOf(h))
			}
			return span{}, ambiguous(n, lines)
		}
	}
	return span{}, notFound(ix, n, ed.Old)
}

// occurrences devuelve dónde aparece sub en s, contando también las apariciones
// que se solapan ("aa" en "aaa" son dos: no es una edición inequívoca).
func occurrences(s, sub string) []int {
	var out []int
	from := 0
	for from <= len(s)-len(sub) {
		i := strings.Index(s[from:], sub)
		if i < 0 {
			break
		}
		out = append(out, from+i)
		from += i + 1
	}
	return out
}

func ambiguous(n int, lines []int) error {
	const show = 8
	list := make([]string, 0, show)
	for i, l := range lines {
		if i >= show {
			break
		}
		list = append(list, itoa(l))
	}
	more := ""
	if len(lines) > show {
		more = " (+" + itoa(len(lines)-show) + ")"
	}
	return i18n.Errorf(i18n.Msg{
		ES: "edición %d: `old` aparece %d veces, en las líneas %s%s: agrega contexto (la línea de arriba o de abajo) o usa from_line/to_line",
		EN: "edit %d: `old` appears %d times, at lines %s%s: add context (the line above or below) or use from_line/to_line",
	}, n, len(lines), strings.Join(list, ", "), more)
}

func notFound(ix index, n int, old string) error {
	key := longestLine(old)
	best := closest(ix, key, 1)
	if len(best) == 0 {
		return i18n.Errorf(i18n.Msg{
			ES: "edición %d: `old` no aparece en el archivo y no hay ninguna línea parecida. Usa find_in_checkpoint o read_checkpoint con from/to y edita por líneas (from_line/to_line)",
			EN: "edit %d: `old` does not appear in the file and no line is similar. Use find_in_checkpoint or read_checkpoint with from/to and edit by lines (from_line/to_line)",
		}, n)
	}
	return i18n.Errorf(i18n.Msg{
		ES: "edición %d: `old` no aparece tal cual; la línea más parecida es la %d: `%s`. Copia el texto exacto (espacios y tabuladores incluidos) o usa from_line/to_line",
		EN: "edit %d: `old` does not appear as written; the most similar line is %d: `%s`. Copy the exact text (spaces and tabs included) or use from_line/to_line",
	}, n, best[0].Line, Clip(best[0].Text, 120))
}

// longestLine devuelve la línea no vacía más larga de s (recortada): es la mejor
// pista para buscar «dónde quiso decir».
func longestLine(s string) string {
	best := ""
	for _, l := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if t := strings.TrimSpace(l); len(t) > len(best) {
			best = t
		}
	}
	return best
}

// resolveLines resuelve {from_line, to_line, new} a un rango de bytes.
func resolveLines(ix index, eol string, n int, ed Edit) (span, error) {
	total := ix.count()
	f, t := ed.FromLine, ed.ToLine
	switch {
	case f < 1:
		return span{}, i18n.Errorf(i18n.Msg{
			ES: "edición %d: from_line debe ser 1 o más (recibí %d)",
			EN: "edit %d: from_line must be 1 or more (got %d)",
		}, n, f)
	case f > total+1:
		return span{}, i18n.Errorf(i18n.Msg{
			ES: "edición %d: from_line %d está fuera del archivo, que tiene %d líneas (para agregar al final usa from_line = %d)",
			EN: "edit %d: from_line %d is outside the file, which has %d lines (to append use from_line = %d)",
		}, n, f, total, total+1)
	case t < f-1:
		return span{}, i18n.Errorf(i18n.Msg{
			ES: "edición %d: to_line (%d) no puede ser menor que from_line - 1 (%d): to_line = from_line - 1 inserta antes de from_line; para reemplazar usa to_line >= from_line",
			EN: "edit %d: to_line (%d) cannot be less than from_line - 1 (%d): to_line = from_line - 1 inserts before from_line; to replace use to_line >= from_line",
		}, n, t, f-1)
	case t > total:
		return span{}, i18n.Errorf(i18n.Msg{
			ES: "edición %d: to_line %d está fuera del archivo, que tiene %d líneas",
			EN: "edit %d: to_line %d is outside the file, which has %d lines",
		}, n, t, total)
	}

	newText := ed.New
	var lines []string
	if newText != "" {
		norm := strings.ReplaceAll(newText, "\r\n", "\n")
		norm = strings.TrimSuffix(norm, "\n")
		lines = strings.Split(norm, "\n")
	}
	joined := strings.Join(lines, eol)

	if t == f-1 { // insertar
		if len(lines) == 0 {
			return span{}, i18n.Errorf(i18n.Msg{
				ES: "edición %d: no hay nada que insertar (`new` está vacío)",
				EN: "edit %d: there is nothing to insert (`new` is empty)",
			}, n)
		}
		if f <= total {
			at := ix.starts[f-1]
			return span{idx: n, kind: "lines", s: at, e: at, repl: joined + eol, insert: true, line: f}, nil
		}
		// Al final del archivo.
		at := len(ix.s)
		if total > 0 && ix.nexts[total-1] == ix.ends[total-1] { // la última línea no termina en salto
			return span{idx: n, kind: "lines", s: at, e: at, repl: eol + joined, insert: true, line: f, lead: len(eol)}, nil
		}
		return span{idx: n, kind: "lines", s: at, e: at, repl: joined + eol, insert: true, line: f}, nil
	}

	s, e := ix.starts[f-1], ix.nexts[t-1]
	if len(lines) == 0 { // borrar las líneas
		return span{idx: n, kind: "lines", s: s, e: e, repl: ""}, nil
	}
	term := ix.s[ix.ends[t-1]:ix.nexts[t-1]] // el terminador de la última línea reemplazada
	return span{idx: n, kind: "lines", s: s, e: e, repl: joined + term}, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// Clip recorta s a n runas con «…» si se pasó.
func Clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimRight(string(r[:n-1]), " ") + "…"
}
