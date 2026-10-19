package meshedit

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
)

// MarkerPrefix abre la línea que marca un checkpoint como «import base»: solo
// el `import()` de un STL con su transformación y su color. Ver BaseOf.
const MarkerPrefix = "// @scadlab-mesh "

// baseMarker es lo que lleva la línea marcadora: lo justo para regenerar el
// código del checkpoint.
type baseMarker struct {
	Ref   string    `json:"ref"`
	Model Transform `json:"model"`
	Color string    `json:"color"`
}

// Generate convierte la escena en el código de un checkpoint: OpenSCAD legible
// y comentado (en español si lang == "es"; si no, en inglés), que empieza con la
// cabecera `// @checkpoint`. Valida la escena y devuelve un error i18n si algo
// no sirve; la misma escena da siempre el mismo código.
//
// Estructura del resultado (cada cuerpo se resta lo suyo; ver colored):
//
//	module modelo() { translate() rotate() scale() mirror() import("refs/x.stl", convexity = 10); }
//	module huecos() { <primitivas de agujero> }
//	module semiespacio(side) { multmatrix(...) cube(...); }   // el corte: un cubo grande
//	module pieza(lado = 0) {
//	    color(c)  difference() { modelo();            huecos(); if (lado != 0) semiespacio(lado); }
//	    color(c2) difference() { <primitiva de unión>; huecos(); if (lado != 0) semiespacio(lado); }
//	}
//	pieza();
//
// Con un corte «ambos lados»: como objetos, dos piezas (pieza(-1) y pieza(1))
// separadas en X por el ancho de la caja más 10 mm; como colores, las dos en su
// sitio, cada una con su color(). Sin primitivas ni cortes se reduce a
// `color(c) modelo();`.
//
// Una escena sin primitivas ni corte es un import base y lleva la línea
// marcadora (MarkerPrefix).
//
// La rotación sigue la convención de este paquete (ver la documentación del
// paquete): `rotate([x,y,z])` de OpenSCAD = Euler 'ZYX' de three.js.
func Generate(s Scene, h Header, lang string) (string, error) {
	g := &gen{s: s, es: lang == "es"}
	if err := g.resolve(); err != nil {
		return "", err
	}
	return g.render(h), nil
}

type gen struct {
	s  Scene
	es bool

	color      string   // color del modelo, ya con el de fábrica
	partColors []string // color normalizado de cada primitiva ("" = el del modelo)
	normal     [3]float64
	cutColors  [2]string
	split      string
	extent     bbox // caja de la escena en el mundo (solo con corte)
}

type bbox struct{ min, max [3]float64 }

func (g *gen) tr(es, en string) string {
	if g.es {
		return es
	}
	return en
}

// resolve valida y normaliza.
func (g *gen) resolve() error {
	s := g.s
	if err := validRef(s.Ref); err != nil {
		return err
	}
	if err := s.Model.validate(i18n.T(i18n.Msg{ES: "modelo", EN: "model"})); err != nil {
		return err
	}
	c, ok := normColor(s.Color)
	if !ok {
		return errBad(i18n.Msg{ES: "color inválido %q (usa #rrggbb)", EN: "invalid color %q (use #rrggbb)"}, s.Color)
	}
	if c == "" {
		c = DefaultColor
	}
	g.color = c

	if len(s.Parts) > maxParts {
		return errBad(i18n.Msg{ES: "demasiadas primitivas (máximo %d)", EN: "too many primitives (maximum %d)"}, maxParts)
	}
	if len(s.Ops) > maxOps {
		return errBad(i18n.Msg{ES: "demasiadas operaciones (máximo %d)", EN: "too many operations (maximum %d)"}, maxOps)
	}
	g.partColors = make([]string, len(s.Parts))
	for i, p := range s.Parts {
		pc, err := p.validate(i + 1)
		if err != nil {
			return err
		}
		g.partColors[i] = pc
	}
	if s.Cut != nil {
		n, cols, err := s.Cut.validate()
		if err != nil {
			return err
		}
		g.normal, g.cutColors = n, cols
		g.split = s.Cut.Split
		if g.split == "" {
			g.split = SplitObjects
		}
		if s.Bounds == nil || !finite3(s.Bounds.Min) || !finite3(s.Bounds.Max) {
			return errBad(i18n.Msg{
				ES: "corte: falta el tamaño de la malla (Bounds) para dimensionar el plano",
				EN: "cut: the mesh size (Bounds) is missing, needed to size the plane",
			})
		}
		g.extent = g.sceneBox()
	}
	return nil
}

// sceneBox es la caja, en el mundo, del modelo y de las primitivas que se le
// suman. Para un cuerpo rotado es la caja de sus 8 esquinas transformadas: exacta
// para una malla convexa y siempre una cota segura, que es lo que pide dimensionar
// un plano de corte.
func (g *gen) sceneBox() bbox {
	b := bbox{min: [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}, max: [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}}
	grow := func(t Transform, lo, hi [3]float64) {
		for i := 0; i < 8; i++ {
			p := [3]float64{lo[0], lo[1], lo[2]}
			for k := 0; k < 3; k++ {
				if i>>k&1 == 1 {
					p[k] = hi[k]
				}
			}
			w := t.Point(p)
			for k := 0; k < 3; k++ {
				b.min[k] = math.Min(b.min[k], w[k])
				b.max[k] = math.Max(b.max[k], w[k])
			}
		}
	}
	grow(g.s.Model, g.s.Bounds.Min, g.s.Bounds.Max)
	for _, p := range g.s.Parts {
		if p.Mode == ModeUnion {
			lo, hi := p.localBox()
			grow(p.Transform, lo, hi)
		}
	}
	return b
}

// localBox es una caja que contiene a la primitiva en su propio sistema (algo
// holgada para el texto, cuyo ancho depende de la fuente).
func (p Primitive) localBox() (lo, hi [3]float64) {
	span := func(size float64, center bool) (float64, float64) {
		if center {
			return -size / 2, size / 2
		}
		return 0, size
	}
	switch p.Shape {
	case ShapeCube:
		for i := 0; i < 3; i++ {
			lo[i], hi[i] = span(p.Size[i], p.Center)
		}
	case ShapeCylinder:
		lo[0], hi[0] = -p.Size[0]/2, p.Size[0]/2
		lo[1], hi[1] = lo[0], hi[0]
		lo[2], hi[2] = span(p.Size[2], p.Center)
	case ShapeSphere:
		lo = [3]float64{-p.Size[0] / 2, -p.Size[0] / 2, -p.Size[0] / 2}
		hi = [3]float64{p.Size[0] / 2, p.Size[0] / 2, p.Size[0] / 2}
	case ShapeText:
		w := p.Size[0] * float64(len([]rune(p.Text)))
		if p.Center {
			lo[0], hi[0] = -w/2, w/2
			lo[1], hi[1] = -p.Size[0], p.Size[0]
		} else {
			lo[0], hi[0] = 0, w
			lo[1], hi[1] = -p.Size[0], 1.5*p.Size[0]
		}
		lo[2], hi[2] = span(p.Size[2], p.Center)
	}
	return lo, hi
}

// --- escritura -----------------------------------------------------------------

type lines []string

func (l *lines) add(indent int, s string) {
	if s == "" {
		*l = append(*l, "")
		return
	}
	*l = append(*l, strings.Repeat("    ", indent)+s)
}

func (l *lines) addAll(indent int, o lines) {
	for _, s := range o {
		l.add(indent, s)
	}
}

// num escribe un número con hasta 6 decimales y sin ceros de sobra; nunca «-0».
func num(v float64) string {
	v = math.Round(v*1e6) / 1e6
	if v == 0 {
		return "0"
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func vec(v [3]float64) string { return "[" + num(v[0]) + ", " + num(v[1]) + ", " + num(v[2]) + "]" }

// lit escribe un texto como literal de OpenSCAD.
func lit(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
		default:
			if r < 0x20 || r == 0x7f {
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

var axisNames = [3]string{"X", "Y", "Z"}

// transformChain escribe `translate() rotate() scale() mirror()` (solo lo que no
// es neutro), cada llamada con su comentario. Devuelve las líneas de la cadena.
func (g *gen) transformChain(t Transform) lines {
	var out lines
	if t.Translate != [3]float64{} {
		var parts []string
		for i, v := range t.Translate {
			if math.Round(v*1e6) != 0 {
				parts = append(parts, fmt.Sprintf(g.tr("%s mm en %s", "%s mm along %s"), num(v), axisNames[i]))
			}
		}
		if len(parts) > 0 {
			out.add(0, "// "+g.tr("Mover ", "Move ")+strings.Join(parts, g.tr(" y ", " and ")))
			out.add(0, "translate("+vec(t.Translate)+")")
		}
	}
	if t.Rotate != [3]float64{} {
		var parts []string
		for i, v := range t.Rotate {
			if math.Round(v*1e6) != 0 {
				parts = append(parts, fmt.Sprintf(g.tr("%s° en %s", "%s° around %s"), num(v), axisNames[i]))
			}
		}
		if len(parts) > 0 {
			out.add(0, "// "+g.tr("Rotar ", "Rotate ")+strings.Join(parts, g.tr(" y ", " and "))+
				g.tr(" (gira X, luego Y, luego Z)", " (turns X, then Y, then Z)"))
			out.add(0, "rotate("+vec(t.Rotate)+")")
		}
	}
	if s := t.scale(); s != [3]float64{1, 1, 1} {
		if s[0] == s[1] && s[1] == s[2] {
			out.add(0, "// "+g.tr("Escalar ×", "Scale ×")+num(s[0]))
		} else {
			out.add(0, "// "+g.tr("Escalar ", "Scale ")+"X ×"+num(s[0])+", Y ×"+num(s[1])+", Z ×"+num(s[2]))
		}
		out.add(0, "scale("+vec(s)+")")
	}
	for i, m := range t.Mirror {
		if !m {
			continue
		}
		axis := [3]float64{}
		axis[i] = 1
		plane := [3]string{"YZ", "XZ", "XY"}[i]
		out.add(0, "// "+fmt.Sprintf(g.tr("Espejar respecto del plano %s (invierte %s)", "Mirror across the %s plane (flips %s)"), plane, axisNames[i]))
		out.add(0, "mirror("+vec(axis)+")")
	}
	return out
}

// chained pone una cadena de transformaciones delante de un objeto: las llamadas
// a la misma altura y el objeto una más a la derecha.
func chained(chain lines, obj ...string) lines {
	var out lines
	for _, l := range chain {
		out.add(0, l)
	}
	ind := 0
	if len(chain) > 0 {
		ind = 1
	}
	for _, o := range obj {
		out.add(ind, o)
	}
	return out
}

// fnFor elige los lados de un círculo según su diámetro: múltiplo de 4 para que
// los vértices caigan sobre los ejes y la caja del cilindro sea el diámetro
// exacto, y sin pasarse (cada lado cuesta render).
func fnFor(d float64) int {
	switch {
	case d <= 10:
		return 32
	case d <= 30:
		return 48
	case d <= 80:
		return 64
	}
	return 96
}

// shapeLines es el código de la primitiva (sin transformación ni color).
func (p Primitive) shapeLines() lines {
	var out lines
	switch p.Shape {
	case ShapeCube:
		c := ""
		if p.Center {
			c = ", center = true"
		}
		out.add(0, "cube("+vec(p.Size)+c+");")
	case ShapeCylinder:
		c := ""
		if p.Center {
			c = ", center = true"
		}
		out.add(0, fmt.Sprintf("cylinder(d = %s, h = %s%s, $fn = %d);", num(p.Size[0]), num(p.Size[2]), c, fnFor(p.Size[0])))
	case ShapeSphere:
		out.add(0, fmt.Sprintf("sphere(d = %s, $fn = %d);", num(p.Size[0]), fnFor(p.Size[0])))
	case ShapeText:
		c, align := "", ""
		if p.Center {
			c = ", center = true"
			align = `, halign = "center", valign = "center"`
		}
		out.add(0, fmt.Sprintf("linear_extrude(height = %s%s)", num(p.Size[2]), c))
		out.add(1, fmt.Sprintf("text(%s, size = %s%s);", lit(p.Text), num(p.Size[0]), align))
	}
	return out
}

// describe es la etiqueta de la primitiva para el comentario.
func (g *gen) describe(p Primitive) string {
	switch p.Shape {
	case ShapeCube:
		return fmt.Sprintf(g.tr("cubo %s × %s × %s mm", "cube %s × %s × %s mm"), num(p.Size[0]), num(p.Size[1]), num(p.Size[2]))
	case ShapeCylinder:
		return fmt.Sprintf(g.tr("cilindro Ø %s × %s mm de alto", "cylinder Ø %s × %s mm tall"), num(p.Size[0]), num(p.Size[2]))
	case ShapeSphere:
		return fmt.Sprintf(g.tr("esfera Ø %s mm", "sphere Ø %s mm"), num(p.Size[0]))
	}
	return fmt.Sprintf(g.tr("texto %q de %s mm, %s mm de alto", "text %q, %s mm size, %s mm tall"), cleanLine(p.Text, 40), num(p.Size[0]), num(p.Size[2]))
}

// primitive escribe el cuerpo de una primitiva: su comentario, su transformación
// y su forma. El color y los recortes los pone quien la arma (colored).
func (g *gen) primitive(p Primitive, n int) lines {
	var out lines
	kind := g.tr("Unión", "Addition")
	if p.Mode == ModeHole {
		kind = g.tr("Agujero", "Hole")
	}
	out.add(0, fmt.Sprintf("// %s %d: %s", kind, n, g.describe(p)))
	out.addAll(0, chained(g.transformChain(p.Transform), p.shapeLines()...))
	return out
}

// importModel escribe `module modelo()`.
func (g *gen) importModel() lines {
	var out lines
	out.add(0, g.tr("// La malla original con su transformación (mover, rotar, escalar, espejar).", "// The original mesh with its transform (move, rotate, scale, mirror)."))
	out.add(0, "module modelo() {")
	obj := fmt.Sprintf("import(%s, convexity = 10);", lit(g.s.Ref))
	out.addAll(1, chained(g.transformChain(g.s.Model), obj))
	out.add(0, "}")
	return out
}

// halfSpace escribe `module semiespacio(side)`: un cubo grande con una cara sobre
// el plano. side = 1 cubre el lado hacia el que apunta la normal; side = -1, el
// contrario.
func (g *gen) halfSpace() lines {
	n := g.normal
	// Base ortonormal (u, v, n) con u × v = n. Para la normal +Z sale la identidad.
	h := [3]float64{0, 0, 1}
	if math.Abs(n[2]) >= 0.9 {
		h = [3]float64{1, 0, 0}
	}
	d := h[0]*n[0] + h[1]*n[1] + h[2]*n[2]
	u := [3]float64{h[0] - d*n[0], h[1] - d*n[1], h[2] - d*n[2]}
	ul := math.Sqrt(u[0]*u[0] + u[1]*u[1] + u[2]*u[2])
	for i := range u {
		u[i] /= ul
	}
	v := [3]float64{n[1]*u[2] - n[2]*u[1], n[2]*u[0] - n[0]*u[2], n[0]*u[1] - n[1]*u[0]}

	// Lado del cubo: tiene que cubrir una bola alrededor del punto del plano que
	// alcance toda la escena.
	var r float64
	for i := 0; i < 8; i++ {
		c := g.extent.min
		for k := 0; k < 3; k++ {
			if i>>k&1 == 1 {
				c[k] = g.extent.max[k]
			}
		}
		r = math.Max(r, math.Sqrt(sq(c[0]-g.s.Cut.Point[0])+sq(c[1]-g.s.Cut.Point[1])+sq(c[2]-g.s.Cut.Point[2])))
	}
	size := math.Ceil((2*r+20)/10) * 10
	p := g.s.Cut.Point

	var out lines
	out.add(0, g.tr(
		fmt.Sprintf("// Semiespacio de corte: un cubo grande con una cara sobre el plano que pasa por %s con normal %s.", vec(p), vec(roundVec(n))),
		fmt.Sprintf("// Cutting half-space: a big cube with one face on the plane through %s with normal %s.", vec(p), vec(roundVec(n)))))
	out.add(0, g.tr(
		"// side = 1 cubre el lado hacia el que apunta la normal; side = -1, el contrario.",
		"// side = 1 covers the side the normal points to; side = -1, the opposite."))
	out.add(0, "module semiespacio(side = 1) {")
	out.add(1, "multmatrix([")
	for i := 0; i < 3; i++ {
		out.add(2, fmt.Sprintf("[%s, %s, %s, %s],", num(u[i]), num(v[i]), num(n[i]), num(p[i])))
	}
	out.add(2, "[0, 0, 0, 1]")
	out.add(1, "])")
	out.add(2, fmt.Sprintf("translate([%s, %s, side > 0 ? 0 : %s])", num(-size/2), num(-size/2), num(-size)))
	out.add(3, fmt.Sprintf("cube([%s, %s, %s]);", num(size), num(size), num(size)))
	out.add(0, "}")
	return out
}

func sq(v float64) float64 { return v * v }

func roundVec(v [3]float64) [3]float64 {
	for i := range v {
		v[i] = math.Round(v[i]*1e6) / 1e6
	}
	return v
}

// holes son los agujeros, uno tras otro (el módulo huecos() los junta).
func (g *gen) holes() lines {
	var out lines
	n := 0
	for _, p := range g.s.Parts {
		if p.Mode == ModeHole {
			n++
			out.addAll(0, g.primitive(p, n))
		}
	}
	return out
}

func (g *gen) hasHoles() bool {
	for _, p := range g.s.Parts {
		if p.Mode == ModeHole {
			return true
		}
	}
	return false
}

// colored envuelve un cuerpo en color(), restándole lo de sub (agujeros y corte).
//
// Cada cuerpo se resta lo suyo POR SEPARADO, y no una vez sobre la unión de todos,
// por dos cosas que se comprobaron con OpenSCAD 2026 (nativo y WebAssembly):
//
//   - el color() de más afuera manda sobre los de adentro, y el cuerpo que se
//     resta hereda el color del que lo envuelve;
//   - las caras nuevas que deja un cortador salen con SU color. Un cortador sin
//     color las deja en el amarillo-verde por defecto (#9DCB51): un color de más
//     que el 3MF multicolor convertiría en otro filamento.
//
// Con la resta dentro del color() de cada cuerpo, las paredes de un agujero y la
// cara de un corte salen del color del cuerpo que cortan.
func colored(col string, inner, sub lines) lines {
	var out lines
	if len(sub) == 0 {
		if len(inner) == 1 {
			out.add(0, "color("+col+") "+inner[0])
		} else {
			out.add(0, "color("+col+")")
			out.addAll(1, inner)
		}
		return out
	}
	out.add(0, "color("+col+") difference() {")
	out.addAll(1, inner)
	out.addAll(1, sub)
	out.add(0, "}")
	return out
}

// bodies escribe el interior de `module pieza()`: el modelo y cada primitiva de
// unión, con lo que se les resta. modelCol es la expresión OpenSCAD del color de
// lo que no trae uno propio.
func (g *gen) bodies(modelCol string) lines {
	var sub lines
	if g.hasHoles() {
		sub.add(0, "huecos();")
	}
	if g.s.Cut != nil {
		sub.add(0, "if (lado != 0) semiespacio(lado);")
	}
	var out lines
	out.addAll(0, colored(modelCol, lines{"modelo();"}, sub))
	n := 0
	for i, p := range g.s.Parts {
		if p.Mode != ModeUnion {
			continue
		}
		n++
		col := modelCol
		if g.partColors[i] != "" {
			col = lit(g.partColors[i])
		}
		out.addAll(0, colored(col, g.primitive(p, n), sub))
	}
	return out
}

func (g *gen) cutComment(keep string) string {
	if keep == KeepAbove {
		return g.tr(`// Corte: se quita el lado contrario a la normal (se queda "arriba").`, `// Cut: the side opposite to the normal is removed ("above" stays).`)
	}
	return g.tr(`// Corte: se quita el lado de la normal (se queda "abajo").`, `// Cut: the side the normal points to is removed ("below" stays).`)
}

func (g *gen) render(h Header) string {
	var out lines
	out.add(0, checkpoint.Header(h.Parent, h.Desc))
	base := len(g.s.Parts) == 0 && g.s.Cut == nil
	if base {
		m, _ := json.Marshal(baseMarker{Ref: g.s.Ref, Model: g.s.Model, Color: g.markerColor()})
		out.add(0, MarkerPrefix+string(m))
	}
	out.add(0, fmt.Sprintf(g.tr("// Modelo basado en %s (el STL original no se modifica: esta edición es solo código).", "// Model based on %s (the original STL is not modified: this edit is just code)."), g.s.Ref))
	if len(g.s.Ops) > 0 {
		out.add(0, g.tr("// Operaciones:", "// Operations:"))
		for i, op := range g.s.Ops {
			out.add(0, fmt.Sprintf("//   %d. %s", i+1, cleanLine(op, maxOpRunes)))
		}
	}
	out.add(0, "")
	out.addAll(0, g.importModel())

	cut := g.s.Cut
	if len(g.s.Parts) == 0 && cut == nil {
		out.add(0, "")
		out.addAll(0, colored(lit(g.color), lines{"modelo();"}, nil))
		return strings.Join(out, "\n") + "\n"
	}

	if g.hasHoles() {
		out.add(0, "")
		out.add(0, g.tr("// Los agujeros: se restan de cada cuerpo.", "// The holes: subtracted from each body."))
		out.add(0, "module huecos() {")
		out.addAll(1, g.holes())
		out.add(0, "}")
	}
	if cut != nil {
		out.add(0, "")
		out.addAll(0, g.halfSpace())
	}

	colorsMode := cut != nil && cut.Keep == KeepBoth && g.split == SplitColors
	var params []string
	if cut != nil {
		params = append(params, "lado = 0")
	}
	modelCol := lit(g.color)
	if colorsMode {
		params = append(params, "c = "+lit(g.color))
		modelCol = "c"
	}
	out.add(0, "")
	out.add(0, g.tr(
		"// El modelo y las uniones, cada uno con sus agujeros y su corte restados: así las caras nuevas llevan el color del cuerpo que cortan.",
		"// The model and the additions, each with its holes and cut subtracted: the new faces take the color of the body they cut."))
	if cut != nil {
		out.add(0, g.tr("// lado = -1 quita el lado contrario a la normal; 1, el de la normal; 0, no corta.", "// lado = -1 removes the side opposite to the normal; 1, the normal's side; 0, no cut."))
	}
	out.add(0, "module pieza("+strings.Join(params, ", ")+") {")
	out.addAll(1, g.bodies(modelCol))
	out.add(0, "}")
	out.add(0, "")

	switch {
	case cut == nil:
		out.add(0, "pieza();")
	case cut.Keep == KeepAbove:
		out.add(0, g.cutComment(KeepAbove))
		out.add(0, "pieza(-1);")
	case cut.Keep == KeepBelow:
		out.add(0, g.cutComment(KeepBelow))
		out.add(0, "pieza(1);")
	case colorsMode:
		c1, c2 := g.cutColors[0], g.cutColors[1]
		if c1 == "" {
			c1 = g.color
		}
		if c2 == "" {
			c2 = g.color
		}
		out.add(0, g.tr("// Mitad de arriba (el lado de la normal), en su sitio.", "// Upper half (the side the normal points to), in place."))
		out.add(0, "pieza(-1, "+lit(c1)+");")
		out.add(0, g.tr("// Mitad de abajo, en su sitio.", "// Lower half, in place."))
		out.add(0, "pieza(1, "+lit(c2)+");")
	default:
		dx := g.extent.max[0] - g.extent.min[0] + 10
		out.add(0, g.tr("// Mitad de arriba (el lado de la normal), en su sitio.", "// Upper half (the side the normal points to), in place."))
		out.add(0, "pieza(-1);")
		out.add(0, fmt.Sprintf(g.tr("// Mitad de abajo, %s mm a un lado en X para imprimirla aparte (ancho de la pieza + 10 mm).", "// Lower half, %s mm to the side in X so it can be printed apart (part width + 10 mm)."), num(dx)))
		out.add(0, fmt.Sprintf("translate([%s, 0, 0])", num(dx)))
		out.add(1, "pieza(1);")
	}
	return strings.Join(out, "\n") + "\n"
}

// markerColor: el color que va en la línea marcadora es el de la escena, no el
// de fábrica, para que regenerar el código dé lo mismo que ya está escrito.
func (g *gen) markerColor() string {
	c, _ := normColor(g.s.Color)
	return c
}

// --- import base -----------------------------------------------------------------

// BaseOf dice si source es un import base: el código que Generate escribe para una
// escena sin primitivas ni corte (lo que deja ImportModel, o una edición que solo
// movió el modelo). Si lo es, devuelve la escena (referencia, transformación y
// color) para seguir editando desde el STL original y no desde una copia
// congelada del render.
//
// La línea marcadora no basta: un agente podría copiarla en un archivo con otro
// contenido. Por eso se REGENERA el código desde lo que dice la marca y se compara
// con el archivo, ignorando comentarios y espacios; si algo difiere (otra
// geometría, o una versión del generador que escribía distinto) no es un import
// base y el editor cae al camino general, que congela la malla renderizada.
func BaseOf(source string) (Scene, bool) {
	var marker string
	for i, l := range strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n") {
		if i >= 8 {
			break
		}
		if strings.HasPrefix(l, MarkerPrefix) {
			marker = strings.TrimSpace(strings.TrimPrefix(l, MarkerPrefix))
			break
		}
	}
	if marker == "" {
		return Scene{}, false
	}
	var m baseMarker
	if json.Unmarshal([]byte(marker), &m) != nil {
		return Scene{}, false
	}
	s := Scene{Ref: m.Ref, Model: m.Model, Color: m.Color}
	want, err := Generate(s, Header{}, "en")
	if err != nil || codeOnly(want) != codeOnly(source) {
		return Scene{}, false
	}
	return s, true
}

// codeOnly deja el código sin comentarios de línea completa, sin líneas vacías y
// sin sangría.
func codeOnly(src string) string {
	var out []string
	for _, l := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
