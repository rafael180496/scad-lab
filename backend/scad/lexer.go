// Package scad entiende código OpenSCAD sin ejecutarlo: lexer, linter
// (OpenSCAD real + reglas propias SL001–SL009), formateador y catálogo del
// lenguaje para el autocompletado del editor. Lo usan el editor (S4), el MCP
// de los agentes (S6) y la auto-iteración (S5): una sola implementación.
//
// Detalle y cómo agregar una regla: claude/10-editor.md.
package scad

import (
	"strings"
	"unicode/utf8"

	"scadlab/backend/i18n"
)

// TokenKind es la clase de un token.
type TokenKind int

const (
	TokIdent   TokenKind = iota // nombre: cube, $fn, mi_modulo (OpenSCAD admite que empiece con dígito: 3d)
	TokNumber                   // 12, 1.5, .5, 1e-3, 0x1F
	TokString                   // "texto", con las comillas y los escapes tal cual
	TokComment                  // "// …" o "/* … */", con los delimitadores
	TokOp                       // + - * / % ^ < > <= >= == != && || ! ? : = # ~ & | << >>
	TokPunct                    // ( ) [ ] { } , ; . y cualquier carácter que OpenSCAD no conoce
	TokKeyword                  // module function if else for let each include use true false undef assert echo
	TokNewline                  // fin de línea fuera de strings y comentarios de bloque
	TokPath                     // <archivo> de include/use, con los ángulos
)

var kindNames = [...]string{"ident", "number", "string", "comment", "op", "punct", "keyword", "newline", "path"}

func (k TokenKind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "unknown"
}

// MarshalText hace que el JSON lleve el nombre ("ident") y no el número.
func (k TokenKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

// Token es una pieza del código. Line y Col son 1-based; Col cuenta unidades
// UTF-16, igual que Monaco, para que un diagnóstico caiga en la columna justa
// aunque la línea tenga acentos o emojis.
type Token struct {
	Kind TokenKind `json:"kind"`
	Text string    `json:"text"`
	Line int       `json:"line"`
	Col  int       `json:"col"`
}

// tok es Token con su posición en bytes, que el formateador necesita para
// saber si había espacio entre dos tokens.
type tok struct {
	Token
	off, end int
}

// LexError es un string o un comentario de bloque sin cerrar: lo único que
// impide leer el archivo.
type LexError struct {
	Line, Col int
	msg       i18n.Msg
}

func (e *LexError) Error() string { return i18n.T(e.msg, e.Line, e.Col) }

var (
	msgUnterminatedString  = i18n.Msg{ES: "línea %d, columna %d: string sin cerrar", EN: "line %d, column %d: unterminated string"}
	msgUnterminatedComment = i18n.Msg{ES: "línea %d, columna %d: comentario /* sin cerrar", EN: "line %d, column %d: unterminated /* comment"}
)

var keywords = map[string]bool{
	"module": true, "function": true, "if": true, "else": true, "for": true, "let": true,
	"each": true, "include": true, "use": true, "true": true, "false": true, "undef": true,
	"assert": true, "echo": true,
}

// Lex parte src en tokens. Ante un string o un comentario de bloque sin
// cerrar devuelve los tokens leídos hasta ahí y un *LexError.
func Lex(src string) ([]Token, error) {
	ts, err := lex(src)
	out := make([]Token, len(ts))
	for i, t := range ts {
		out[i] = t.Token
	}
	return out, err
}

type lexer struct {
	src       string
	pos       int
	line, col int
	toks      []tok
}

func lex(src string) ([]tok, error) {
	l := &lexer{src: src, line: 1, col: 1}
	if strings.HasPrefix(src, "\uFEFF") {
		l.pos = len("\uFEFF")
	}
	for l.pos < len(l.src) {
		if err := l.next(); err != nil {
			return l.toks, err
		}
	}
	return l.toks, nil
}

// advance mueve n bytes llevando línea y columna (UTF-16).
func (l *lexer) advance(n int) {
	end := l.pos + n
	for l.pos < end {
		r, size := utf8.DecodeRuneInString(l.src[l.pos:])
		l.pos += size
		switch {
		case r == '\n':
			l.line++
			l.col = 1
		case r >= 0x10000:
			l.col += 2
		default:
			l.col++
		}
	}
}

func (l *lexer) emit(kind TokenKind, n int) {
	t := tok{Token: Token{Kind: kind, Text: l.src[l.pos : l.pos+n], Line: l.line, Col: l.col}, off: l.pos, end: l.pos + n}
	l.toks = append(l.toks, t)
	l.advance(n)
}

func (l *lexer) peek(i int) byte {
	if l.pos+i < len(l.src) {
		return l.src[l.pos+i]
	}
	return 0
}

func isDigit(c byte) bool     { return c >= '0' && c <= '9' }
func isWordByte(c byte) bool  { return c == '_' || isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'z') }
func isHexDigit(c byte) bool  { return isDigit(c) || (c|0x20 >= 'a' && c|0x20 <= 'f') }
func isSpaceByte(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v' }

func (l *lexer) next() error {
	c := l.peek(0)
	switch {
	case c == '\n':
		l.emit(TokNewline, 1)
	case isSpaceByte(c):
		l.advance(1)
	case c == '/' && l.peek(1) == '/':
		n := strings.IndexByte(l.src[l.pos:], '\n')
		if n < 0 {
			n = len(l.src) - l.pos
		}
		// El \r de un fin de línea Windows no es parte del comentario.
		for n > 2 && l.src[l.pos+n-1] == '\r' {
			n--
		}
		l.emit(TokComment, n)
	case c == '/' && l.peek(1) == '*':
		n := strings.Index(l.src[l.pos+2:], "*/")
		if n < 0 {
			return &LexError{Line: l.line, Col: l.col, msg: msgUnterminatedComment}
		}
		l.emit(TokComment, n+4)
	case c == '"':
		i := 1
		for {
			if l.pos+i >= len(l.src) {
				return &LexError{Line: l.line, Col: l.col, msg: msgUnterminatedString}
			}
			b := l.src[l.pos+i]
			if b == '\\' {
				i += 2
				continue
			}
			i++
			if b == '"' {
				break
			}
		}
		if l.pos+i > len(l.src) { // barra invertida al final del archivo
			return &LexError{Line: l.line, Col: l.col, msg: msgUnterminatedString}
		}
		l.emit(TokString, i)
	case isDigit(c) || (c == '.' && isDigit(l.peek(1))):
		num := l.numberLen()
		word := 0
		if c != '.' {
			word = l.wordLen(0)
		}
		// Como flex: gana el más largo y, si empatan, el número. Así "3d" es
		// un identificador (OpenSCAD lo admite, con DEPRECATED) y "1e5" un número.
		if word > num {
			l.emitWord(word)
		} else {
			l.emit(TokNumber, num)
		}
	case c == '$' && isWordByte(l.peek(1)):
		l.emitWord(1 + l.wordLen(1))
	case isWordByte(c):
		l.emitWord(l.wordLen(0))
	default:
		l.emitOp()
	}
	return nil
}

func (l *lexer) wordLen(from int) int {
	i := from
	for l.pos+i < len(l.src) && isWordByte(l.src[l.pos+i]) {
		i++
	}
	return i - from
}

func (l *lexer) numberLen() int {
	s := l.src[l.pos:]
	if len(s) > 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') && isHexDigit(s[2]) {
		i := 2
		for i < len(s) && isHexDigit(s[i]) {
			i++
		}
		return i
	}
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && isDigit(s[i]) {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && isDigit(s[j]) {
			for j < len(s) && isDigit(s[j]) {
				j++
			}
			i = j
		}
	}
	return i
}

func (l *lexer) emitWord(n int) {
	word := l.src[l.pos : l.pos+n]
	if !keywords[word] {
		l.emit(TokIdent, n)
		return
	}
	l.emit(TokKeyword, n)
	if word != "include" && word != "use" {
		return
	}
	// include <ruta> / use <ruta>: la ruta es un solo token. Leída como
	// operadores, "<lib/BOSL2/std.scad>" serían divisiones y comparaciones, y
	// el formateador le metería espacios.
	i := 0
	for l.pos+i < len(l.src) && isSpaceByte(l.src[l.pos+i]) {
		i++
	}
	if l.peek(i) != '<' {
		return
	}
	rest := l.src[l.pos+i:]
	end := strings.IndexAny(rest, ">\n")
	if end < 0 || rest[end] != '>' {
		return
	}
	l.advance(i)
	l.emit(TokPath, end+1)
}

var twoCharOps = []string{"<=", ">=", "==", "!=", "&&", "||", "<<", ">>"}

func (l *lexer) emitOp() {
	rest := l.src[l.pos:]
	for _, op := range twoCharOps {
		if strings.HasPrefix(rest, op) {
			l.emit(TokOp, 2)
			return
		}
	}
	switch c := rest[0]; c {
	case '+', '-', '*', '/', '%', '^', '<', '>', '!', '?', ':', '=', '#', '~', '&', '|':
		l.emit(TokOp, 1)
	default:
		_, size := utf8.DecodeRuneInString(rest)
		l.emit(TokPunct, size)
	}
}

// endCol es la columna siguiente al último carácter del token (exclusiva),
// válida solo si el token está en una sola línea.
func (t Token) endCol() int {
	n := 0
	for _, r := range t.Text {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return t.Col + n
}

func (t Token) is(kind TokenKind, text string) bool { return t.Kind == kind && t.Text == text }
