package scad

// Esqueleto de sentencias para las reglas del linter. No es un parser de
// OpenSCAD: las expresiones se saltan con paréntesis balanceados y solo se
// reconoce la forma de cada sentencia (asignación, definición, include,
// cadena de módulos). Alcanza para «nivel superior», «envuelto en color()»,
// «mismo ámbito» y «anidado dentro de»; lo que no entiende lo saltea hasta el
// próximo «;» o «}» en vez de fallar, porque el editor lo llama mientras se
// escribe y el código casi nunca es válido.

type stmtKind int

const (
	stmtEmpty    stmtKind = iota // ;
	stmtBlock                    // { … }
	stmtInclude                  // include <x> / use <x>
	stmtModule                   // module nombre(…) cuerpo
	stmtFunction                 // function nombre(…) = …;
	stmtAssign                   // nombre = …;
	stmtInst                     // [modificador] cabeza(…) hijo — cube(1); translate(…) {…}; if (…) … else …
	stmtUnknown                  // lo que no se entendió, hasta el próximo ; o }
)

type stmt struct {
	kind       stmtKind
	start, end int // índices en sig, end exclusivo
	name       int // índice del nombre (assign, module, function, cabeza de inst, ruta de include); -1 si no hay
	modifier   string
	value      [2]int // assign: rango de la expresión [desde, hasta)
	child      *stmt  // inst/module: el hijo (nil = hoja terminada en ;)
	elseChild  *stmt
	body       []stmt // block
}

// parser recorre solo los tokens significativos (sin comentarios ni saltos).
type parser struct {
	sig []tok
}

func significant(ts []tok) []tok {
	out := make([]tok, 0, len(ts))
	for _, t := range ts {
		if t.Kind != TokComment && t.Kind != TokNewline {
			out = append(out, t)
		}
	}
	return out
}

func (p *parser) at(i int, kind TokenKind, text string) bool {
	return i < len(p.sig) && p.sig[i].is(kind, text)
}

// parseList lee sentencias hasta un «}» (sin consumirlo) o el final.
func (p *parser) parseList(i int) ([]stmt, int) {
	var out []stmt
	for i < len(p.sig) && !p.at(i, TokPunct, "}") {
		s, next := p.parseStmt(i)
		out = append(out, s)
		if next <= i { // nunca debería pasar; protege contra un bucle infinito
			next = i + 1
		}
		i = next
	}
	return out, i
}

// skipBalanced devuelve el índice siguiente al cierre que balancea la
// apertura en i. Si el archivo termina antes, devuelve len(sig).
func (p *parser) skipBalanced(i int) int {
	depth := 0
	for ; i < len(p.sig); i++ {
		if p.sig[i].Kind != TokPunct {
			continue
		}
		switch p.sig[i].Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
			if depth <= 0 {
				return i + 1
			}
		}
	}
	return i
}

// skipToSemicolon salta una expresión hasta el «;» de profundidad 0
// (incluido). Se detiene antes de un «}» que cierre el ámbito.
func (p *parser) skipToSemicolon(i int) int {
	depth := 0
	for ; i < len(p.sig); i++ {
		t := p.sig[i]
		if t.Kind != TokPunct {
			continue
		}
		switch t.Text {
		case "(", "[", "{":
			depth++
		case ")", "]":
			depth--
		case "}":
			if depth <= 0 {
				return i
			}
			depth--
		case ";":
			if depth <= 0 {
				return i + 1
			}
		}
	}
	return i
}

func isModifier(t tok) bool {
	return t.Kind == TokOp && (t.Text == "!" || t.Text == "#" || t.Text == "%" || t.Text == "*")
}

func (p *parser) parseStmt(i int) (stmt, int) {
	s := stmt{start: i, name: -1}
	t := p.sig[i]
	switch {
	case t.is(TokPunct, ";"):
		s.kind, s.end = stmtEmpty, i+1
	case t.is(TokPunct, "{"):
		s.kind = stmtBlock
		s.body, i = p.parseList(i + 1)
		if p.at(i, TokPunct, "}") {
			i++
		}
		s.end = i
	case t.Kind == TokKeyword && (t.Text == "include" || t.Text == "use"):
		s.kind, s.end = stmtInclude, i+1
		if i+1 < len(p.sig) && p.sig[i+1].Kind == TokPath {
			s.name, s.end = i+1, i+2
		}
	case t.is(TokKeyword, "module"):
		s.kind = stmtModule
		i++
		if i < len(p.sig) && p.sig[i].Kind == TokIdent {
			s.name = i
			i++
		}
		if p.at(i, TokPunct, "(") {
			i = p.skipBalanced(i)
		}
		if i < len(p.sig) && !p.at(i, TokPunct, "}") {
			c, next := p.parseStmt(i)
			s.child, i = &c, next
		}
		s.end = i
	case t.is(TokKeyword, "function"):
		s.kind = stmtFunction
		if i+1 < len(p.sig) && p.sig[i+1].Kind == TokIdent {
			s.name = i + 1
		}
		s.end = p.skipToSemicolon(i + 1)
	case t.Kind == TokIdent && p.at(i+1, TokOp, "="):
		s.kind, s.name = stmtAssign, i
		s.end = p.skipToSemicolon(i + 2)
		s.value = [2]int{i + 2, s.end}
		if s.end > i+2 && p.sig[s.end-1].is(TokPunct, ";") {
			s.value[1] = s.end - 1
		}
	default:
		return p.parseInst(i)
	}
	return s, s.end
}

// parseInst lee una cadena de instanciación: modificadores, cabeza con sus
// argumentos y el hijo (otra sentencia, un bloque o «;»).
func (p *parser) parseInst(i int) (stmt, int) {
	s := stmt{kind: stmtInst, start: i, name: -1}
	for i < len(p.sig) && isModifier(p.sig[i]) {
		s.modifier += p.sig[i].Text
		i++
	}
	if i >= len(p.sig) {
		s.end = i
		return s, i
	}
	h := p.sig[i]
	isHead := h.Kind == TokIdent || (h.Kind == TokKeyword && (h.Text == "if" || h.Text == "for" || h.Text == "let" ||
		h.Text == "assert" || h.Text == "echo"))
	if !isHead || !p.at(i+1, TokPunct, "(") {
		s.kind = stmtUnknown
		end := p.skipToSemicolon(i)
		if end == i { // un «}» suelto o algo que no avanza
			end = i + 1
		}
		s.end = end
		return s, end
	}
	s.name = i
	i = p.skipBalanced(i + 1)
	switch {
	case i >= len(p.sig):
	case p.at(i, TokPunct, ";"):
		i++
	case p.at(i, TokPunct, "}"):
	default:
		c, next := p.parseStmt(i)
		s.child, i = &c, next
	}
	if h.Text == "if" && p.at(i, TokKeyword, "else") && i+1 < len(p.sig) {
		c, next := p.parseStmt(i + 1)
		s.elseChild, i = &c, next
	}
	s.end = i
	return s, i
}

// headName es el nombre de la cabeza de una instanciación ("" si no hay).
func (p *parser) headName(s *stmt) string {
	if s.kind != stmtInst || s.name < 0 {
		return ""
	}
	return p.sig[s.name].Text
}

// walk recorre s y todos sus descendientes (hijos, else, cuerpos de bloque y
// de módulo) en orden.
func walk(s *stmt, fn func(*stmt)) {
	fn(s)
	if s.child != nil {
		walk(s.child, fn)
	}
	if s.elseChild != nil {
		walk(s.elseChild, fn)
	}
	for i := range s.body {
		walk(&s.body[i], fn)
	}
}
