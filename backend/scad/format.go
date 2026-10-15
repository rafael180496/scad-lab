package scad

import (
	"strings"
	"unicode/utf8"
)

// Formateador conservador sobre el lexer (el AST de `openscad -o x.ast` no
// sirve: descarta comentarios y reordena). Reindenta por llaves, paréntesis y
// corchetes; normaliza espacios entre tokens; colapsa líneas en blanco
// (máximo una); quita espacios finales. Nunca une ni parte líneas de código y
// no toca el interior de strings ni de comentarios. Es idempotente:
// Format(Format(x)) == Format(x).
//
// Indentación de una línea:
//
//   - Empieza con un cierre (`}`, `)`, `]`): la de la línea que abrió.
//   - Dentro de `(` o `[`: si después de la apertura sigue código en la misma
//     línea, alineada con ese código (`f(a,` → la siguiente arranca bajo la
//     `a`); si la apertura termina la línea, un nivel más que la línea que
//     abrió (un nivel por línea, no por paréntesis: `f([` sube uno).
//   - En un bloque, al empezar una sentencia: la del bloque.
//   - Continuación de una sentencia con `=` en su nivel (asignación,
//     function): bloque + 1, todas iguales.
//   - Continuación de una cadena de módulos (`if (…)` / `translate(…)` /
//     `cube();` en líneas distintas): una más que la cabeza anterior, en
//     escalera, porque cada una es hija de la anterior. Un `{` que empieza
//     línea va a la altura de su cabeza (estilo Allman).

// FormatOptions configura el formateador.
type FormatOptions struct {
	IndentSize int // espacios por nivel; fuera de 1–8 = 4
}

// Los «niveles» de fmtFrame están en columnas (espacios), no en pasos de
// indentación: la alineación con un paréntesis cae en cualquier columna.
type fmtFrame struct {
	kind      byte // '{' (bloque o raíz), '(' o '['
	openLevel int  // indentación de la línea que abrió: la de su cierre
	inner     int  // indentación del contenido
	inStmt    bool // bloque: hay una sentencia a medio escribir
	exprMode  bool // bloque: la sentencia tiene un `=` en su nivel
	stmtLevel int  // bloque: indentación de la última línea cabeza de la sentencia
}

type formatter struct {
	src       string
	indent    int
	stack     []*fmtFrame
	prevSig   *tok
	prevUnary bool // el token significativo anterior fue un operador unario o modificador
}

// Format formatea src. Solo falla si el lexer falla (string o comentario sin
// cerrar): en ese caso no hay forma segura de saber qué es código.
func Format(src string, o FormatOptions) (string, error) {
	toks, err := lex(src)
	if err != nil {
		return "", err
	}
	f := &formatter{src: src, indent: o.IndentSize, stack: []*fmtFrame{{kind: '{'}}}
	if f.indent < 1 || f.indent > 8 {
		f.indent = 4
	}

	var lines [][]tok
	var cur []tok
	for _, t := range toks {
		if t.Kind == TokNewline {
			lines = append(lines, cur)
			cur = nil
			continue
		}
		cur = append(cur, t)
	}
	lines = append(lines, cur)

	var out []string
	pendingBlank := false
	for _, line := range lines {
		if len(line) == 0 {
			pendingBlank = len(out) > 0
			continue
		}
		if pendingBlank {
			out = append(out, "")
			pendingBlank = false
		}
		out = append(out, f.formatLine(line))
	}
	if len(out) == 0 {
		return "", nil
	}
	nl := "\n"
	if strings.Contains(src, "\r\n") {
		nl = "\r\n"
	}
	return strings.Join(out, nl) + nl, nil
}

func (f *formatter) top() *fmtFrame { return f.stack[len(f.stack)-1] }

// codeFollows: queda código (no solo un comentario) en el resto de la línea
// y no es una comprensión de listas (`[for (…)`, `[each …`), cuyo cuerpo se
// lee mejor un nivel adentro que alineado bajo el `for`.
func codeFollows(rest []tok) bool {
	for _, t := range rest {
		if t.Kind == TokComment {
			continue
		}
		return !(t.Kind == TokKeyword && (t.Text == "for" || t.Text == "each" || t.Text == "let" || t.Text == "if"))
	}
	return false
}

func isOpener(t tok) bool {
	return t.Kind == TokPunct && (t.Text == "(" || t.Text == "[" || t.Text == "{")
}

func isCloser(t tok) bool {
	return t.Kind == TokPunct && (t.Text == ")" || t.Text == "]" || t.Text == "}")
}

func (f *formatter) lineLevel(first tok) int {
	top := f.top()
	switch {
	case isCloser(first):
		return top.openLevel
	case top.kind != '{':
		return top.inner
	case !top.inStmt:
		return top.inner
	case first.is(TokPunct, "{"):
		return top.stmtLevel
	case top.exprMode:
		return top.inner + f.indent
	default:
		return top.stmtLevel + f.indent
	}
}

func (f *formatter) formatLine(line []tok) string {
	level := f.lineLevel(line[0])
	if level < 0 {
		level = 0
	}
	indent := strings.Repeat(" ", level)
	lineFrame := f.top()
	headDone := false

	var b strings.Builder
	b.WriteString(indent)
	for i, t := range line {
		unary := f.isUnary(t)
		if i > 0 {
			sp := f.space(line[i-1], t, unary)
			if sp == "" && fuses(line[i-1], t) {
				sp = " "
			}
			b.WriteString(sp)
		}
		b.WriteString(f.text(t, i == 0, indent))
		if t.Kind == TokComment {
			continue
		}
		align := -1
		if (t.is(TokPunct, "(") || t.is(TokPunct, "[")) && codeFollows(line[i+1:]) {
			s := b.String()
			align = utf8.RuneCountInString(s[strings.LastIndexByte(s, '\n')+1:])
		}
		f.update(t, level, align, lineFrame, !headDone)
		headDone = true
		tt := t
		f.prevSig = &tt
		f.prevUnary = unary
	}
	return b.String()
}

// text es el texto de un token tal como se escribe. Un comentario de línea
// pierde los espacios finales. Un comentario de bloque de varias líneas que
// empieza la línea se corre entero lo mismo que su primera línea, para no
// desalinear los « * » de adentro; si correrlo hacia la izquierda le comería
// texto a alguna línea, sus líneas interiores quedan como estaban.
func (f *formatter) text(t tok, first bool, indent string) string {
	if t.Kind != TokComment {
		return t.Text
	}
	if strings.HasPrefix(t.Text, "//") {
		return strings.TrimRight(t.Text, " \t")
	}
	if !first || !strings.Contains(t.Text, "\n") {
		return t.Text
	}
	ls := strings.LastIndexByte(f.src[:t.off], '\n') + 1
	orig := f.src[ls:t.off]
	if strings.Trim(orig, " ") != "" { // tabs u otra cosa: no se adivina
		return t.Text
	}
	delta := len(indent) - len(orig)
	if delta == 0 {
		return t.Text
	}
	parts := strings.Split(t.Text, "\n")
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		if strings.TrimSpace(p) == "" {
			continue
		}
		if delta < 0 && !strings.HasPrefix(p, strings.Repeat(" ", -delta)) {
			return t.Text
		}
	}
	for i := 1; i < len(parts); i++ {
		p := parts[i]
		if strings.TrimSpace(p) == "" {
			continue
		}
		if delta > 0 {
			parts[i] = strings.Repeat(" ", delta) + p
		} else {
			parts[i] = p[-delta:]
		}
	}
	return strings.Join(parts, "\n")
}

// update avanza el estado de indentación con un token significativo.
// align es la columna a la que se alinea el contenido de un ( o [ abierto
// por t; -1 = un nivel más que la línea.
func (f *formatter) update(t tok, level, align int, lineFrame *fmtFrame, firstOfLine bool) {
	top := f.top()
	if top.kind == '{' && !isCloser(t) {
		switch {
		case !top.inStmt && !t.is(TokPunct, ";"):
			top.inStmt, top.exprMode, top.stmtLevel = true, false, level
		case top.inStmt && firstOfLine && top == lineFrame:
			top.stmtLevel = level // nueva cabeza en la escalera
		}
	}
	switch {
	case isOpener(t):
		inner := level + f.indent
		if align >= 0 {
			inner = align
		}
		f.stack = append(f.stack, &fmtFrame{kind: t.Text[0], openLevel: level, inner: inner})
	case isCloser(t):
		want := byte('{')
		switch t.Text {
		case ")":
			want = '('
		case "]":
			want = '['
		}
		for i := len(f.stack) - 1; i > 0; i-- {
			if f.stack[i].kind == want {
				f.stack = f.stack[:i]
				if want == '{' && f.top().kind == '{' {
					f.top().inStmt = false // el bloque cierra la sentencia que lo abrió
				}
				break
			}
		}
	case t.is(TokPunct, ";") || t.Kind == TokPath:
		if top.kind == '{' {
			top.inStmt = false
		}
	case t.is(TokOp, "="):
		if top.kind == '{' && top.inStmt {
			top.exprMode = true
		}
	}
}

// isUnary dice si el operador t (todavía no procesado) es unario o un
// modificador de módulo (! # % *).
func (f *formatter) isUnary(t tok) bool {
	if t.Kind != TokOp {
		return false
	}
	p := f.prevSig
	switch t.Text {
	case "!", "~", "#":
		return true
	case "-", "+":
		if p == nil {
			return true
		}
		switch p.Kind {
		case TokOp, TokPath:
			return true
		case TokKeyword:
			return p.Text != "true" && p.Text != "false" && p.Text != "undef"
		case TokPunct:
			return p.Text != ")" && p.Text != "]" && p.Text != "."
		}
		return false
	case "*", "%":
		top := f.top()
		if top.kind != '{' || top.exprMode {
			return false
		}
		if p == nil {
			return true
		}
		switch {
		case p.Kind == TokPunct:
			return p.Text == ";" || p.Text == "{" || p.Text == "}" || p.Text == ")"
		case p.is(TokKeyword, "else"):
			return true
		case p.Kind == TokOp:
			return f.prevUnary // ** o #% encadenados
		}
	}
	return false
}

var knownPunct = map[string]bool{"(": true, ")": true, "[": true, "]": true, "{": true, "}": true, ",": true, ";": true, ".": true}

// space es el espacio entre dos tokens de la misma línea.
func (f *formatter) space(p, n tok, nUnary bool) string {
	gap := f.src[p.end:n.off]
	if p.Kind == TokComment || n.Kind == TokComment {
		if n.Kind == TokComment && strings.HasPrefix(n.Text, "//") && gap == "" {
			return " "
		}
		return keepGap(gap)
	}
	if (p.Kind == TokPunct && !knownPunct[p.Text]) || (n.Kind == TokPunct && !knownPunct[n.Text]) {
		return keepGap(gap)
	}
	pp := func(s string) bool { return p.Kind == TokPunct && p.Text == s }
	np := func(s string) bool { return n.Kind == TokPunct && n.Text == s }
	switch {
	case f.prevUnary && p.Kind == TokOp:
		return ""
	case np(",") || np(";"):
		return ""
	case pp("(") || pp("["):
		return ""
	case np(")") || np("]"):
		return ""
	case pp(",") || pp(";"):
		return " "
	case nUnary:
		return " "
	case np(".") || pp("."):
		return ""
	case np("("):
		switch p.Kind {
		case TokKeyword:
			if p.Text == "if" || p.Text == "for" || p.Text == "else" || p.Text == "each" {
				return " "
			}
			return ""
		case TokIdent:
			return ""
		case TokOp:
			return " "
		}
		return oneOrNone(gap)
	case np("["):
		switch p.Kind {
		case TokIdent, TokString:
			return ""
		case TokKeyword, TokOp:
			return " "
		}
		if pp("]") {
			return ""
		}
		return oneOrNone(gap)
	case pp("{") && np("}"):
		return oneOrNone(gap)
	case np("{") || pp("{") || np("}") || pp("}"):
		return " "
	}
	return " "
}

// fuses dice si escribir p y n pegados cambiaría cómo se leen: `!` + `=` es
// `!=`, `/` + `/` abre un comentario, `1` + `e5` es otro número. En ese caso
// el espacio es obligatorio aunque la regla diga «pegados».
func fuses(p, n tok) bool {
	ts, err := lex(p.Text + n.Text)
	return err != nil || len(ts) != 2 || ts[0].Text != p.Text || ts[1].Text != n.Text
}

// keepGap conserva el espacio original si es solo de espacios (alineación de
// comentarios finales); con tabs, uno.
func keepGap(gap string) string {
	if gap == "" || strings.Trim(gap, " ") == "" {
		return gap
	}
	return " "
}

func oneOrNone(gap string) string {
	if gap == "" {
		return ""
	}
	return " "
}
