package scad

import (
	"strings"

	"scadlab/backend/textedit"
)

// Esquema de un .scad (S17): lo que la herramienta outline_checkpoint le da a un
// modelo para orientarse en un archivo de 600 líneas SIN leerlo: los parámetros
// del Customizer por sección, los módulos y funciones con sus líneas de inicio y
// fin, los include/use y las partes de color. Con eso y find_in_checkpoint, el
// modelo lee solo el rango que va a tocar.
//
// Reutiliza el lexer, el esqueleto de sentencias del linter y scad.Parameters.
// Las líneas son las de textedit (1-based, sin contar el BOM).

// OutlineItem es un parámetro del Customizer.
type OutlineItem struct {
	Name  string `json:"name"`
	Line  int    `json:"line"`
	Value string `json:"value,omitempty"` // el literal tal cual está escrito
}

// OutlineSection agrupa los parámetros de una sección `/* [Nombre] */`. Los que
// están antes de la primera sección llevan Section vacío.
type OutlineSection struct {
	Section string        `json:"section,omitempty"`
	Items   []OutlineItem `json:"items"`
}

// OutlineDef es un module o una function.
type OutlineDef struct {
	Name string `json:"name"`
	Line int    `json:"line"`
	End  int    `json:"end"`
	Args string `json:"args,omitempty"`
}

// OutlineInclude es un include o un use.
type OutlineInclude struct {
	Kind string `json:"kind"` // "include" | "use"
	Path string `json:"path"`
	Line int    `json:"line"`
}

// OutlinePart es una llamada a color() fuera de los módulos: una parte del
// modelo, que el 3MF multicolor reparte por color.
type OutlinePart struct {
	Color string `json:"color"`           // el argumento: red, #ff8800, [1,0,0], mi_color
	Line  int    `json:"line"`            // dónde está el color()
	Value string `json:"value,omitempty"` // si Color es una variable con valor literal, ese valor
}

// Outline es el esquema de un archivo.
type Outline struct {
	Lines     int              `json:"lines"`
	Bytes     int              `json:"bytes"`
	Params    []OutlineSection `json:"params"`
	Modules   []OutlineDef     `json:"modules"`
	Functions []OutlineDef     `json:"functions"`
	Includes  []OutlineInclude `json:"includes"`
	Parts     []OutlinePart    `json:"parts"`
}

// BuildOutline arma el esquema de src. Un string o un comentario sin cerrar
// impide leerlo: error (*LexError). Nunca devuelve nil en las listas, para que el
// JSON no cambie de forma.
func BuildOutline(src string) (Outline, error) {
	out := Outline{
		Lines: textedit.CountLines(src), Bytes: len(src),
		Params: []OutlineSection{}, Modules: []OutlineDef{}, Functions: []OutlineDef{}, Includes: []OutlineInclude{}, Parts: []OutlinePart{},
	}
	toks, err := lex(src)
	if err != nil {
		return out, err
	}
	p := &parser{sig: significant(toks)}
	top := p.parseAll()

	// Parámetros por sección, en el orden del archivo.
	params, err := Parameters(src)
	if err != nil {
		return out, err
	}
	literal := map[string]string{}
	for _, pr := range params {
		literal[pr.Name] = pr.Literal
		if n := len(out.Params); n == 0 || out.Params[n-1].Section != pr.Group {
			out.Params = append(out.Params, OutlineSection{Section: pr.Group, Items: []OutlineItem{}})
		}
		sec := &out.Params[len(out.Params)-1]
		sec.Items = append(sec.Items, OutlineItem{Name: pr.Name, Line: pr.Line, Value: pr.Literal})
	}

	var collectParts func(s *stmt)
	collectParts = func(s *stmt) {
		if s.kind == stmtInst && p.headName(s) == "color" {
			out.Parts = append(out.Parts, p.colorPart(s, src, literal))
		}
		if s.child != nil {
			collectParts(s.child)
		}
		if s.elseChild != nil {
			collectParts(s.elseChild)
		}
		for i := range s.body {
			collectParts(&s.body[i])
		}
	}

	for i := range top {
		s := &top[i]
		switch s.kind {
		case stmtModule, stmtFunction:
			if s.name < 0 {
				continue
			}
			d := OutlineDef{Name: p.sig[s.name].Text, Line: p.sig[s.start].Line, End: p.lastLine(s), Args: p.argsText(src, s.name+1)}
			if s.kind == stmtModule {
				out.Modules = append(out.Modules, d)
			} else {
				out.Functions = append(out.Functions, d)
			}
		case stmtInclude:
			if s.name < 0 {
				continue
			}
			path := strings.TrimSuffix(strings.TrimPrefix(p.sig[s.name].Text, "<"), ">")
			out.Includes = append(out.Includes, OutlineInclude{Kind: p.sig[s.start].Text, Path: path, Line: p.sig[s.start].Line})
		default:
			collectParts(s)
		}
	}
	return out, nil
}

// lastLine es la línea del último token de la sentencia.
func (p *parser) lastLine(s *stmt) int {
	if s.end > s.start && s.end-1 < len(p.sig) {
		return p.sig[s.end-1].Line
	}
	return p.sig[s.start].Line
}

// argsText devuelve lo que hay entre los paréntesis que abren en sig[open], en
// una línea y sin los paréntesis; "" si no abre uno.
func (p *parser) argsText(src string, open int) string {
	if open >= len(p.sig) || !p.sig[open].is(TokPunct, "(") {
		return ""
	}
	closeIdx := p.skipBalanced(open) - 1
	if closeIdx <= open || closeIdx >= len(p.sig) {
		return ""
	}
	return strings.Join(strings.Fields(src[p.sig[open].end:p.sig[closeIdx].off]), " ")
}

// colorPart arma la parte de un color(…).
func (p *parser) colorPart(s *stmt, src string, literal map[string]string) OutlinePart {
	part := OutlinePart{Line: p.sig[s.name].Line}
	args := p.argsText(src, s.name+1)
	// Solo el primer argumento (color("red", 0.5) → "red"; c = "red" nombrado también).
	first := args
	depth := 0
	for i, r := range args {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				first = args[:i]
			}
		}
		if first != args {
			break
		}
	}
	first = strings.TrimSpace(first)
	first = strings.TrimSpace(strings.TrimPrefix(first, "c ="))
	first = strings.TrimSpace(strings.TrimPrefix(first, "c="))
	if len(first) >= 2 && first[0] == '"' && first[len(first)-1] == '"' {
		first = first[1 : len(first)-1]
	} else if v, ok := literal[first]; ok {
		part.Value = strings.Trim(v, `"`)
	}
	part.Color = first
	return part
}

// Def es un module o una function del nivel superior con lo que necesita
// extract_module para moverla: dónde está y de qué depende.
type Def struct {
	Kind   string   // "module" | "function"
	Name   string   // nombre
	Line   int      // línea de la palabra module/function
	End    int      // última línea
	Off    int      // desplazamiento en bytes de la palabra module/function
	EndOff int      // desplazamiento en bytes donde termina (después del último token)
	Params []string // nombres de sus parámetros
	// Vars son los nombres que usa y no son suyos (ni parámetros ni asignados
	// adentro): variables globales del archivo, o de OpenSCAD.
	Vars []string
	// Calls son los módulos y funciones que llama.
	Calls []string
}

// FileDefs es lo que extract_module necesita saber del archivo.
type FileDefs struct {
	Defs    []Def
	Globals []string // variables asignadas en el nivel superior (parámetros incluidos)
}

// ListDefs lee los module/function del nivel superior de src con sus
// dependencias. El análisis es léxico: un nombre es «propio» si es un parámetro
// o si aparece a la izquierda de un «=» dentro de la definición (una variable
// local o un argumento con nombre). Es conservador a propósito: lo usa para
// avisar antes de mover un módulo con `use`, que no ve las variables globales.
func ListDefs(src string) (FileDefs, error) {
	var out FileDefs
	toks, err := lex(src)
	if err != nil {
		return out, err
	}
	p := &parser{sig: significant(toks)}
	top := p.parseAll()
	seenGlobal := map[string]bool{}
	for i := range top {
		s := &top[i]
		switch s.kind {
		case stmtAssign:
			if name := p.sig[s.name].Text; !strings.HasPrefix(name, "$") && !seenGlobal[name] {
				seenGlobal[name] = true
				out.Globals = append(out.Globals, name)
			}
		case stmtModule, stmtFunction:
			if s.name < 0 {
				continue
			}
			d := Def{Name: p.sig[s.name].Text, Line: p.sig[s.start].Line, End: p.lastLine(s), Off: p.sig[s.start].off, EndOff: p.sig[max(s.start, s.end-1)].end}
			d.Kind = "module"
			if s.kind == stmtFunction {
				d.Kind = "function"
			}
			p.fillDeps(s, &d)
			out.Defs = append(out.Defs, d)
		}
	}
	return out, nil
}

func (p *parser) fillDeps(s *stmt, d *Def) {
	own := map[string]bool{}
	// Parámetros: los identificadores de la lista que no son parte de un valor por
	// defecto (los que están a profundidad 1 y siguen a «(» o «,»).
	open := s.name + 1
	if open < len(p.sig) && p.sig[open].is(TokPunct, "(") {
		depth := 0
		for i := open; i < s.end && i < len(p.sig); i++ {
			t := p.sig[i]
			switch {
			case t.Kind == TokPunct && (t.Text == "(" || t.Text == "[" || t.Text == "{"):
				depth++
			case t.Kind == TokPunct && (t.Text == ")" || t.Text == "]" || t.Text == "}"):
				depth--
			case t.Kind == TokIdent && depth == 1 && (p.sig[i-1].is(TokPunct, "(") || p.sig[i-1].is(TokPunct, ",")):
				own[t.Text] = true
				d.Params = append(d.Params, t.Text)
			}
			if depth == 0 && i > open {
				break
			}
		}
	}

	// A la izquierda de un «=»: una variable local (en el cuerpo, o en la cabeza de
	// un for/let) o un argumento con nombre de una llamada (cylinder(r = r)). Solo
	// la primera es «propia»; en la segunda el nombre del argumento no es una
	// variable de nadie, y el valor de la derecha sí se mira como uso.
	type frame struct {
		open string // "(", "[" o "{"
		head string // identificador o palabra clave que precede al «(»
	}
	var stack []frame
	namedArg := map[int]bool{} // índices de tokens que son el nombre de un argumento
	for i := s.name + 1; i < s.end && i < len(p.sig); i++ {
		t := p.sig[i]
		if t.Kind == TokPunct {
			switch t.Text {
			case "(", "[", "{":
				head := ""
				if t.Text == "(" && i > 0 && (p.sig[i-1].Kind == TokIdent || p.sig[i-1].Kind == TokKeyword) {
					head = p.sig[i-1].Text
				}
				stack = append(stack, frame{t.Text, head})
			case ")", "]", "}":
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
			continue
		}
		if t.Kind != TokIdent || i+1 >= len(p.sig) || !p.sig[i+1].is(TokOp, "=") {
			continue
		}
		top := frame{}
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		switch {
		case top.open == "(" && (top.head == "for" || top.head == "let" || top.head == "intersection_for"):
			own[t.Text] = true
		case top.open == "(" && top.head != d.Name:
			namedArg[i] = true
		case top.open == "(": // la lista de parámetros: ya está arriba
		default:
			own[t.Text] = true
		}
	}

	seenVar, seenCall := map[string]bool{}, map[string]bool{}
	for i := s.name + 1; i < s.end && i < len(p.sig); i++ {
		t := p.sig[i]
		if t.Kind != TokIdent || strings.HasPrefix(t.Text, "$") || namedArg[i] {
			continue
		}
		isCall := i+1 < len(p.sig) && p.sig[i+1].is(TokPunct, "(")
		switch {
		case isCall:
			if !seenCall[t.Text] && t.Text != d.Name {
				seenCall[t.Text] = true
				d.Calls = append(d.Calls, t.Text)
			}
		case !own[t.Text] && !seenVar[t.Text] && !(i > 0 && p.sig[i-1].is(TokPunct, ".")):
			seenVar[t.Text] = true
			d.Vars = append(d.Vars, t.Text)
		}
	}
}
