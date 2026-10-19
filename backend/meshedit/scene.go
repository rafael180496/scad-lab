// Package meshedit es el núcleo del editor de STL (S13). Editar un STL NO lo
// toca: el STL vive intacto en refs/ y cada edición es código OpenSCAD en un
// checkpoint nuevo que hace `import("refs/x.stl")` y le aplica las operaciones.
// Así la edición queda en el árbol con su padre, se deshace volviendo al padre,
// se renderiza con el motor nativo o el integrado y se exporta a STL/3MF como
// cualquier checkpoint (regla 3 de technical.md).
//
// El frontend lleva la pila de operaciones y manda aquí el ESTADO FINAL como una
// Scene; Generate la convierte en código. La misma escena da siempre el mismo
// código. Spec: .claude/specs/s13-edicion-stl.md.
//
// # Convención de transformaciones (contrato con el frontend)
//
// OpenSCAD aplica `translate(t) rotate([rx,ry,rz]) scale(s) mirror(m) objeto`:
// primero se espeja, luego se escala, luego se rota y al final se mueve. En
// three.js eso es:
//
//	object.position = t
//	object.rotation = Euler(rx°, ry°, rz° en radianes, orden 'ZYX')
//	object.scale    = s, con el signo cambiado en cada eje espejado
//
// `rotate([x,y,z])` gira X, luego Y, luego Z, todos sobre los ejes FIJOS del
// mundo; en notación de matrices R = Rz·Ry·Rx, que es exactamente el orden de
// Euler 'ZYX' de three. Un orden 'XYZ' (el de por defecto de three) da otra
// pose en cuanto hay dos ejes distintos de cero. Los ángulos viajan en grados
// y los radianes quedan dentro de three. Las coordenadas del mundo son las de
// OpenSCAD (Z arriba, como el visor).
package meshedit

import (
	"math"
	"regexp"
	"strings"
	"unicode"

	"scadlab/backend/i18n"
)

// Transform es la pose de un objeto. Ver la convención en la documentación del
// paquete.
type Transform struct {
	Translate [3]float64 `json:"translate"`
	Rotate    [3]float64 `json:"rotate"` // grados, rotate([x,y,z]) de OpenSCAD
	Scale     [3]float64 `json:"scale"`  // 0 = 1
	Mirror    [3]bool    `json:"mirror"`
}

// Formas de una primitiva.
const (
	ShapeCube     = "cube"
	ShapeCylinder = "cylinder"
	ShapeSphere   = "sphere"
	ShapeText     = "text"
)

// Modos de una primitiva: sumada al modelo o restada de todo.
const (
	ModeUnion = "union"
	ModeHole  = "hole"
)

// Primitive es una pieza que se suma al modelo (union) o se le resta (hole).
type Primitive struct {
	ID    string     `json:"id"`
	Shape string     `json:"shape"` // cube | cylinder | sphere | text
	Mode  string     `json:"mode"`  // union | hole
	Size  [3]float64 `json:"size"`  // cubo x,y,z; cilindro [d, d, h]; esfera [d]; texto [tamaño, -, alto de extrusión]
	Text  string     `json:"text"`
	// Center centra la primitiva en su origen (cubo en los tres ejes, cilindro y
	// texto en Z; la esfera siempre lo está).
	Center    bool      `json:"center"`
	Color     string    `json:"color"` // #rrggbb, "" = el del modelo
	Transform Transform `json:"transform"`
}

// Lados del corte.
const (
	KeepAbove = "above" // se queda el lado hacia el que apunta la normal
	KeepBelow = "below" // se queda el lado contrario
	KeepBoth  = "both"
)

// Qué hacer con las dos mitades de un corte «both».
const (
	SplitObjects = "objects" // dos piezas separadas en X, para imprimirlas aparte
	SplitColors  = "colors"  // dos colores, cada mitad en su sitio (3MF multicolor)
)

// Cut es un corte con un plano: pasa por Point y su normal es Normal. «Arriba»
// (above) es el lado hacia el que apunta la normal.
type Cut struct {
	Point  [3]float64 `json:"point"`
	Normal [3]float64 `json:"normal"`
	Keep   string     `json:"keep"`  // above | below | both
	Split  string     `json:"split"` // objects | colors (solo con both; vacío = objects)
	Colors [2]string  `json:"colors"`
}

// Bounds es la caja de la malla ORIGINAL (sin transformar), en mm. Generate la
// necesita para dimensionar el corte y separar las dos mitades; no viaja desde
// el frontend: la app la lee del STL antes de generar.
type Bounds struct {
	Min [3]float64 `json:"min"`
	Max [3]float64 `json:"max"`
}

// Scene es el estado final de una edición.
type Scene struct {
	Ref   string      `json:"ref"` // refs/x.stl (relativa al workspace)
	Model Transform   `json:"model"`
	Color string      `json:"color"` // color del modelo; "" = el de fábrica
	Parts []Primitive `json:"parts"`
	Cut   *Cut        `json:"cut"`
	Ops   []string    `json:"ops"` // etiquetas legibles, en orden, para los comentarios

	// Bounds: ver el tipo. Obligatorio si hay Cut. json:"-" a propósito: lo pone
	// quien tiene acceso al archivo, no el frontend.
	Bounds *Bounds `json:"-"`
}

// Header es la cabecera `// @checkpoint parent=… desc="…"` del archivo.
// (checkpoint.Header es la función que la escribe; aquí viaja su contenido.)
type Header struct {
	Parent string
	Desc   string
}

// DefaultColor es el color de un modelo editado cuando la escena no trae uno.
// Cada pieza de primer nivel lleva color() (regla del workspace) y el 3MF
// multicolor depende de eso.
const DefaultColor = "#9ca3af"

// Límites de sanidad: protegen el código generado y el render, no son reglas de
// impresión.
const (
	maxAbs      = 1e5 // mm: ninguna coordenada ni tamaño pasa de 100 m
	minScale    = 1e-6
	maxParts    = 200
	maxOps      = 500
	maxOpRunes  = 160
	maxTextLen  = 200
	maxRefRunes = 200
)

var colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && math.Abs(v) <= maxAbs }

func finite3(v [3]float64) bool { return finite(v[0]) && finite(v[1]) && finite(v[2]) }

// normColor valida «#rrggbb» (vacío es válido: «el de siempre») y lo pasa a
// minúsculas.
func normColor(c string) (string, bool) {
	c = strings.TrimSpace(c)
	if c == "" {
		return "", true
	}
	if !colorRe.MatchString(c) {
		return "", false
	}
	return strings.ToLower(c), true
}

func errBad(msg i18n.Msg, args ...any) error { return i18n.Errorf(msg, args...) }

// validRef comprueba que ref sea refs/<archivo>.stl: bajo refs/, un solo nombre
// (sin subcarpetas ni «..»), sin caracteres que rompan el literal de OpenSCAD.
func validRef(ref string) error {
	bad := func() error {
		return errBad(i18n.Msg{
			ES: "referencia inválida: %q (usa un STL de refs/, p. ej. refs/pieza.stl)",
			EN: "invalid reference: %q (use an STL in refs/, e.g. refs/part.stl)",
		}, ref)
	}
	if ref == "" || len([]rune(ref)) > maxRefRunes || !strings.HasPrefix(ref, "refs/") {
		return bad()
	}
	name := strings.TrimPrefix(ref, "refs/")
	if len(name) <= len(".stl") || strings.HasPrefix(name, ".") || strings.Contains(name, "..") ||
		strings.ContainsAny(name, "/\\:\"") || !strings.HasSuffix(strings.ToLower(name), ".stl") ||
		strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) }) {
		return bad()
	}
	return nil
}

// validate revisa la transformación y devuelve la escala efectiva (0 → 1).
func (t Transform) validate(what string) error {
	if !finite3(t.Translate) || !finite3(t.Rotate) {
		return errBad(i18n.Msg{
			ES: "%s: la posición o la rotación no son números finitos",
			EN: "%s: the position or rotation are not finite numbers",
		}, what)
	}
	for _, s := range t.Scale {
		switch {
		case s == 0:
		case !finite(s) || s < 0:
			return errBad(i18n.Msg{
				ES: "%s: la escala tiene que ser positiva (para voltear un eje usa el espejo)",
				EN: "%s: the scale must be positive (use the mirror to flip an axis)",
			}, what)
		case s < minScale:
			return errBad(i18n.Msg{ES: "%s: la escala es demasiado chica", EN: "%s: the scale is too small"}, what)
		}
	}
	return nil
}

// scale es la escala efectiva: 0 se lee como 1.
func (t Transform) scale() [3]float64 {
	out := t.Scale
	for i := range out {
		if out[i] == 0 {
			out[i] = 1
		}
	}
	return out
}

// Point lleva un punto del objeto a su posición en el mundo: espejo, escala,
// rotación ZYX y traslación, en ese orden (lo que hace OpenSCAD con
// `translate rotate scale mirror`, y three con position/rotation 'ZYX'/scale).
// Lo usan el cálculo de la caja de la escena y las pruebas que comparan el
// render con la cuenta a mano.
func (t Transform) Point(p [3]float64) [3]float64 {
	s := t.scale()
	for i := range p {
		if t.Mirror[i] {
			p[i] = -p[i]
		}
		p[i] *= s[i]
	}
	// rotate([x,y,z]): X, luego Y, luego Z, sobre los ejes fijos.
	rad := func(d float64) (float64, float64) {
		r := d * math.Pi / 180
		return math.Sin(r), math.Cos(r)
	}
	sx, cx := rad(t.Rotate[0])
	p[1], p[2] = p[1]*cx-p[2]*sx, p[1]*sx+p[2]*cx
	sy, cy := rad(t.Rotate[1])
	p[0], p[2] = p[0]*cy+p[2]*sy, -p[0]*sy+p[2]*cy
	sz, cz := rad(t.Rotate[2])
	p[0], p[1] = p[0]*cz-p[1]*sz, p[0]*sz+p[1]*cz
	return [3]float64{p[0] + t.Translate[0], p[1] + t.Translate[1], p[2] + t.Translate[2]}
}

func (t Transform) isIdentity() bool {
	return t.Translate == [3]float64{} && t.Rotate == [3]float64{} && t.scale() == [3]float64{1, 1, 1} && t.Mirror == [3]bool{}
}

// validate revisa una primitiva y devuelve su color normalizado.
func (p Primitive) validate(n int) (string, error) {
	what := i18n.T(i18n.Msg{ES: "primitiva %d", EN: "primitive %d"}, n)
	if p.Mode != ModeUnion && p.Mode != ModeHole {
		return "", errBad(i18n.Msg{ES: "%s: el modo tiene que ser union o hole", EN: "%s: the mode must be union or hole"}, what)
	}
	pos := func(v float64) bool { return finite(v) && v > 0 }
	ok := false
	switch p.Shape {
	case ShapeCube:
		ok = pos(p.Size[0]) && pos(p.Size[1]) && pos(p.Size[2])
	case ShapeCylinder:
		ok = pos(p.Size[0]) && pos(p.Size[2])
	case ShapeSphere:
		ok = pos(p.Size[0])
	case ShapeText:
		ok = pos(p.Size[0]) && pos(p.Size[2])
		if strings.TrimSpace(p.Text) == "" || len([]rune(p.Text)) > maxTextLen {
			return "", errBad(i18n.Msg{
				ES: "%s: el texto tiene que tener entre 1 y %d caracteres",
				EN: "%s: the text must have between 1 and %d characters",
			}, what, maxTextLen)
		}
	default:
		return "", errBad(i18n.Msg{
			ES: "%s: forma desconocida %q (cube, cylinder, sphere o text)",
			EN: "%s: unknown shape %q (cube, cylinder, sphere or text)",
		}, what, p.Shape)
	}
	if !ok {
		return "", errBad(i18n.Msg{
			ES: "%s: los tamaños tienen que ser números positivos",
			EN: "%s: the sizes must be positive numbers",
		}, what)
	}
	if err := p.Transform.validate(what); err != nil {
		return "", err
	}
	c, okc := normColor(p.Color)
	if !okc {
		return "", errBad(i18n.Msg{ES: "%s: color inválido %q (usa #rrggbb)", EN: "%s: invalid color %q (use #rrggbb)"}, what, p.Color)
	}
	return c, nil
}

// validate revisa el corte y devuelve la normal normalizada y los colores.
func (c Cut) validate() (n [3]float64, colors [2]string, err error) {
	if !finite3(c.Point) || !finite3(c.Normal) {
		return n, colors, errBad(i18n.Msg{ES: "corte: el punto o la normal no son números finitos", EN: "cut: the point or the normal are not finite numbers"})
	}
	l := math.Sqrt(c.Normal[0]*c.Normal[0] + c.Normal[1]*c.Normal[1] + c.Normal[2]*c.Normal[2])
	if l < 1e-9 {
		return n, colors, errBad(i18n.Msg{ES: "corte: la normal del plano no puede ser nula", EN: "cut: the plane normal cannot be zero"})
	}
	n = [3]float64{c.Normal[0] / l, c.Normal[1] / l, c.Normal[2] / l}
	switch c.Keep {
	case KeepAbove, KeepBelow, KeepBoth:
	default:
		return n, colors, errBad(i18n.Msg{
			ES: "corte: qué lado conservar tiene que ser above, below o both",
			EN: "cut: which side to keep must be above, below or both",
		})
	}
	if c.Split != "" && c.Split != SplitObjects && c.Split != SplitColors {
		return n, colors, errBad(i18n.Msg{ES: "corte: split tiene que ser objects o colors", EN: "cut: split must be objects or colors"})
	}
	for i, col := range c.Colors {
		v, ok := normColor(col)
		if !ok {
			return n, colors, errBad(i18n.Msg{ES: "corte: color inválido %q (usa #rrggbb)", EN: "cut: invalid color %q (use #rrggbb)"}, col)
		}
		colors[i] = v
	}
	return n, colors, nil
}

// cleanLine deja un texto en una sola línea, sin caracteres de control, para
// usarlo en un comentario.
func cleanLine(s string, max int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return s
}
