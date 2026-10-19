package meshedit

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/refmodel"
)

// Operaciones de edit_mesh (herramienta MCP): la misma edición que hace el
// editor 3D, pero descrita como una lista de pasos que una IA puede escribir.
// BuildScene los convierte en una Scene y el código sale del mismo Generate que
// usa la interfaz: una sola implementación, el mismo resultado.
//
// # Marco de referencia
//
// Las operaciones de POSE (move, rotate, scale, resize, mirror, orient, center,
// to_bed) se aplican en orden sobre la transformación del modelo (Scene.Model).
// Las que cambian el cuerpo (rotate, scale, resize, mirror, orient) lo hacen
// sobre el CENTRO de su caja, para que «girarla» o «hacerla más grande» no la
// mande a otro lado; move, center y to_bed son las que la colocan.
//
// Las primitivas (add, hole) y el corte (cut) se colocan en el marco FINAL del
// mundo —después de todas las operaciones de pose, sin importar en qué lugar de
// la lista estén—, así que at_rel (una fracción de la caja del modelo) y el
// plano del corte apuntan al modelo tal como va a quedar.

// Ops reconocidas.
const (
	OpMove   = "move"
	OpRotate = "rotate"
	OpScale  = "scale"
	OpResize = "resize"
	OpMirror = "mirror"
	OpOrient = "orient"
	OpCenter = "center"
	OpToBed  = "to_bed"
	OpColor  = "color"
	OpAdd    = "add"
	OpHole   = "hole"
	OpCut    = "cut"
)

// OpNames es el orden en que se listan las operaciones en los mensajes.
var OpNames = []string{OpMove, OpRotate, OpScale, OpResize, OpMirror, OpOrient, OpCenter, OpToBed, OpColor, OpAdd, OpHole, OpCut}

// MaxToolOps acota la lista de una llamada.
const MaxToolOps = 50

// Vec es un vector que acepta [x,y,z], {"x":…,"y":…,"z":…} o un número suelto
// (los tres ejes iguales; Scalar lo recuerda). Los números pueden venir como
// texto: los modelos chicos mandan "40".
type Vec struct {
	V      [3]float64
	Scalar bool
}

func lenientNumber(raw json.RawMessage) (float64, error) {
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return f, nil
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return v, nil
		}
	}
	return 0, i18n.Errorf(i18n.Msg{ES: "no es un número: %s", EN: "not a number: %s"}, string(raw))
}

// UnmarshalJSON implementa json.Unmarshaler.
func (v *Vec) UnmarshalJSON(b []byte) error {
	var list []json.RawMessage
	if err := json.Unmarshal(b, &list); err == nil {
		if len(list) != 3 {
			return i18n.Errorf(i18n.Msg{ES: "un vector necesita 3 números y llegaron %d", EN: "a vector needs 3 numbers, got %d"}, len(list))
		}
		for i, r := range list {
			n, err := lenientNumber(r)
			if err != nil {
				return err
			}
			v.V[i] = n
		}
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err == nil {
		for i, k := range []string{"x", "y", "z"} {
			if r, ok := obj[k]; ok {
				n, err := lenientNumber(r)
				if err != nil {
					return err
				}
				v.V[i] = n
			}
		}
		return nil
	}
	n, err := lenientNumber(b)
	if err != nil {
		return err
	}
	v.V, v.Scalar = [3]float64{n, n, n}, true
	return nil
}

// Num es un número que acepta también un texto numérico.
type Num float64

// UnmarshalJSON implementa json.Unmarshaler.
func (n *Num) UnmarshalJSON(b []byte) error {
	f, err := lenientNumber(b)
	if err != nil {
		return err
	}
	*n = Num(f)
	return nil
}

// Flag es un booleano que acepta también "true"/"false" entre comillas.
type Flag bool

// UnmarshalJSON implementa json.Unmarshaler.
func (f *Flag) UnmarshalJSON(b []byte) error {
	var v bool
	if err := json.Unmarshal(b, &v); err == nil {
		*f = Flag(v)
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		switch strings.ToLower(strings.TrimSpace(s)) {
		case "true", "yes", "1":
			*f = true
			return nil
		case "false", "no", "0":
			*f = false
			return nil
		}
	}
	return i18n.Errorf(i18n.Msg{ES: "no es un booleano: %s", EN: "not a boolean: %s"}, string(b))
}

// Op es un paso de edit_mesh. Cada operación usa solo sus campos; los demás se
// ignoran.
type Op struct {
	Op string `json:"op"`

	By      *Vec   `json:"by,omitempty"`      // move: milímetros a sumar
	Degrees *Vec   `json:"degrees,omitempty"` // rotate: grados en X, Y, Z (ejes fijos del mundo)
	Factor  *Vec   `json:"factor,omitempty"`  // scale: un factor o [fx,fy,fz] sobre los ejes del propio modelo
	Mm      *Num   `json:"mm,omitempty"`      // resize: medida final del eje
	Axis    string `json:"axis,omitempty"`    // resize/mirror/cut/cylinder: x, y, z (resize también longest)
	Axes    string `json:"axes,omitempty"`    // center: ejes a centrar (por defecto "xy")
	Down    string `json:"down,omitempty"`    // orient: +x -x +y -y +z -z, la cara que baja a la placa

	Color string `json:"color,omitempty"` // color/add/hole: #rrggbb

	// add / hole
	Shape    string `json:"shape,omitempty"`     // cube | cylinder | sphere | text
	Size     *Vec   `json:"size,omitempty"`      // cube: ancho, fondo, alto
	Diameter *Num   `json:"diameter,omitempty"`  // cylinder, sphere
	Height   *Num   `json:"height,omitempty"`    // cylinder; text: alto de la extrusión
	Text     string `json:"text,omitempty"`      // text
	TextSize *Num   `json:"text_size,omitempty"` // text: tamaño de letra (def. 10)
	At       *Vec   `json:"at,omitempty"`        // add/hole: posición; cut: milímetros sobre el eje
	AtRel    *Vec   `json:"at_rel,omitempty"`    // add/hole: fracción de la caja del modelo; cut: fracción sobre el eje (un número)
	Rotate   *Vec   `json:"rotate,omitempty"`    // add/hole: grados
	Center   *Flag  `json:"center,omitempty"`    // add/hole: la posición es el centro (def. true)

	// cut
	Point  *Vec     `json:"point,omitempty"`
	Normal *Vec     `json:"normal,omitempty"`
	Keep   string   `json:"keep,omitempty"`   // above | below | both
	Split  string   `json:"split,omitempty"`  // objects | colors (con both)
	Colors []string `json:"colors,omitempty"` // [color de arriba, color de abajo] con split colors
}

// Base es el punto de partida de una edición: el STL de refs/, la pose con la
// que ya se importaba y su caja original.
type Base struct {
	Ref        string
	Model      Transform
	Color      string
	Bounds     Bounds // caja de la malla ORIGINAL, sin transformar
	Watertight bool
	Triangles  int
	// Report es el análisis completo de la malla de Ref (la app lo muestra tal cual).
	Report refmodel.Report
	// FromImport: el checkpoint era un import base y Ref es el STL original. Si es
	// falso, Ref es la malla renderizada del checkpoint, congelada en refs/.
	FromImport bool
	// Created: la copia congelada de refs/ se creó en esta llamada (no existía). Si
	// la edición falla después, quien llama puede quitarla: es suya, recién hecha.
	Created bool
}

// Edit es lo que sale de BuildScene.
type Edit struct {
	Scene Scene
	// Min y Max son la caja del modelo base en el mundo tras la pose (sin
	// agujeros, uniones ni corte): lo que la IA necesita para medir antes de
	// seguir.
	Min, Max [3]float64
	Warnings []string
}

// ResolveBase decide desde qué se edita el checkpoint `file` (cuyo código es
// source), igual que PrepareMeshEdit en la app:
//
//   - un import base (solo el import() de un STL con su pose y color) sigue desde
//     ese STL original;
//   - cualquier otro checkpoint se CONGELA: frozen() devuelve la malla de su render
//     (quien llama lo renderiza) y se copia a refs/<checkpoint>.stl sin pisar nada
//     (se reutiliza si ya existe con el mismo contenido). Los colores de un modelo
//     multicolor se pierden en la base: es una sola malla.
func ResolveBase(wsDir, file, source string, frozen func() (meshPath string, err error)) (Base, error) {
	if sc, ok := BaseOf(source); ok {
		if abs, rel, err := refmodel.ResolveRef(wsDir, sc.Ref); err == nil {
			if rep, err := refmodel.Analyze(abs); err == nil {
				return baseFrom(rel, normalizeScale(sc.Model), sc.Color, rep, true), nil
			}
		}
		// El STL ya no está o no se lee: se cae al camino general, que renderiza el
		// checkpoint y dice por qué no se puede.
	}
	meshPath, err := frozen()
	if err != nil {
		return Base{}, err
	}
	rel, reused, err := refmodel.FreezeMesh(wsDir, meshPath, strings.TrimSuffix(file, ".scad"))
	if err != nil {
		return Base{}, err
	}
	abs, _, err := refmodel.ResolveRef(wsDir, rel)
	if err != nil {
		return Base{}, err
	}
	rep, err := refmodel.Analyze(abs)
	if err != nil {
		return Base{}, err
	}
	b := baseFrom(rel, normalizeScale(Transform{}), "", rep, false)
	b.Created = !reused
	return b, nil
}

func baseFrom(ref string, model Transform, color string, r refmodel.Report, fromImport bool) Base {
	return Base{
		Ref: ref, Model: model, Color: color, FromImport: fromImport,
		Bounds:     Bounds{Min: r.Min, Max: r.Max},
		Watertight: r.Watertight, Triangles: r.Triangles, Report: r,
	}
}

// normalizeScale escribe la escala neutra como 1 (el contrato dice 0 = 1).
func normalizeScale(t Transform) Transform {
	for i, s := range t.Scale {
		if s == 0 {
			t.Scale[i] = 1
		}
	}
	return t
}

// --- álgebra mínima ----------------------------------------------------------------

type mat3 [3][3]float64

func rad(d float64) (sin, cos float64) {
	r := d * math.Pi / 180
	return math.Sin(r), math.Cos(r)
}

// rotMatrix es Rz·Ry·Rx: lo que hace rotate([x,y,z]) de OpenSCAD (ver la
// documentación del paquete).
func rotMatrix(d [3]float64) mat3 {
	sx, cx := rad(d[0])
	sy, cy := rad(d[1])
	sz, cz := rad(d[2])
	return mat3{
		{cz * cy, cz*sy*sx - sz*cx, cz*sy*cx + sz*sx},
		{sz * cy, sz*sy*sx + cz*cx, sz*sy*cx - cz*sx},
		{-sy, cy * sx, cy * cx},
	}
}

func (a mat3) mul(b mat3) mat3 {
	var out mat3
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			for k := 0; k < 3; k++ {
				out[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return out
}

// euler es la inversa de rotMatrix (grados). En el bloqueo de cardán (Y = ±90°)
// el reparto entre X y Z no es único: se deja Z en cero.
func (m mat3) euler() [3]float64 {
	deg := func(r float64) float64 { return tidy(r * 180 / math.Pi) }
	if math.Abs(m[2][0]) > 1-1e-9 {
		if m[2][0] < 0 { // Y = +90
			return [3]float64{deg(math.Atan2(m[0][1], m[1][1])), 90, 0}
		}
		return [3]float64{deg(math.Atan2(-m[0][1], m[1][1])), -90, 0}
	}
	return [3]float64{
		deg(math.Atan2(m[2][1], m[2][2])),
		deg(-math.Asin(math.Max(-1, math.Min(1, m[2][0])))),
		deg(math.Atan2(m[1][0], m[0][0])),
	}
}

// worldBox es la caja, en el mundo, de la malla b con la pose t: la de sus 8
// esquinas transformadas (exacta para una caja alineada con los ejes y una cota
// segura para cualquier malla).
func worldBox(t Transform, b Bounds) (lo, hi [3]float64) {
	lo = [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi = [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for i := 0; i < 8; i++ {
		p := [3]float64{b.Min[0], b.Min[1], b.Min[2]}
		for a := 0; a < 3; a++ {
			if i&(1<<a) != 0 {
				p[a] = b.Max[a]
			}
		}
		w := t.Point(p)
		for a := 0; a < 3; a++ {
			lo[a], hi[a] = math.Min(lo[a], w[a]), math.Max(hi[a], w[a])
		}
	}
	return lo, hi
}

func boxCenter(lo, hi [3]float64) [3]float64 {
	return [3]float64{(lo[0] + hi[0]) / 2, (lo[1] + hi[1]) / 2, (lo[2] + hi[2]) / 2}
}

var axisIndex = map[string]int{"x": 0, "y": 1, "z": 2}

func axisOf(s string) (int, bool) {
	i, ok := axisIndex[strings.ToLower(strings.TrimSpace(s))]
	return i, ok
}

// orientRotation es el giro que baja a la placa la cara indicada del cuerpo.
var orientRotation = map[string][3]float64{
	"+x": {0, 90, 0}, "-x": {0, -90, 0},
	"+y": {-90, 0, 0}, "-y": {90, 0, 0},
	"+z": {180, 0, 0}, "-z": {0, 0, 0},
}

// OrientRotation devuelve los grados de la operación orient para esa cara
// ("+x", "-z"…): lo usa suggest_orientation para describir cada candidata.
func OrientRotation(down string) ([3]float64, bool) {
	r, ok := orientRotation[strings.ToLower(strings.TrimSpace(down))]
	return r, ok
}

// --- construcción de la escena ----------------------------------------------------

type builder struct {
	base  Base
	lang  string
	scene Scene
	warns []string
}

func (b *builder) tr(es, en string) string {
	if b.lang == "es" {
		return es
	}
	return en
}

func (b *builder) box() (lo, hi [3]float64) { return worldBox(b.scene.Model, b.base.Bounds) }

func (b *builder) fail(i int, op string, es, en string, args ...any) error {
	prefix := i18n.T(i18n.Msg{ES: "operación %d (%s): ", EN: "operation %d (%s): "}, i+1, op)
	return errBad(i18n.Msg{ES: prefix + es, EN: prefix + en}, args...)
}

// keepingCenter aplica fn a la pose y luego la mueve para que el centro de la
// caja del cuerpo siga donde estaba.
func (b *builder) keepingCenter(fn func(t *Transform)) {
	lo, hi := b.box()
	before := boxCenter(lo, hi)
	fn(&b.scene.Model)
	lo, hi = b.box()
	after := boxCenter(lo, hi)
	for a := 0; a < 3; a++ {
		b.scene.Model.Translate[a] = tidy(b.scene.Model.Translate[a] + before[a] - after[a])
	}
}

// rotateWorld compone una rotación de ejes fijos DESPUÉS de la actual.
func rotateWorld(t *Transform, deg [3]float64) {
	t.Rotate = rotMatrix(deg).mul(rotMatrix(t.Rotate)).euler()
}

func fmtNum(v float64) string { return num(tidy(v)) }

func fmtVec(v [3]float64) string {
	return "[" + fmtNum(v[0]) + ", " + fmtNum(v[1]) + ", " + fmtNum(v[2]) + "]"
}

// BuildScene aplica las operaciones sobre la base y devuelve la escena lista para
// Generate. lang es el idioma de las etiquetas que van de comentario en el código.
func BuildScene(base Base, ops []Op, lang string) (Edit, error) {
	if len(ops) == 0 {
		return Edit{}, errBad(i18n.Msg{
			ES: "falta `operations`: una lista de pasos (move, rotate, scale, resize, mirror, orient, center, to_bed, color, add, hole, cut)",
			EN: "`operations` is required: a list of steps (move, rotate, scale, resize, mirror, orient, center, to_bed, color, add, hole, cut)",
		})
	}
	if len(ops) > MaxToolOps {
		return Edit{}, errBad(i18n.Msg{ES: "demasiadas operaciones (máximo %d por llamada)", EN: "too many operations (maximum %d per call)"}, MaxToolOps)
	}
	b := &builder{base: base, lang: lang, scene: Scene{Ref: base.Ref, Model: normalizeScale(base.Model), Color: base.Color}}

	var pending []int // índices de add/hole/cut: se resuelven con la pose final
	for i, op := range ops {
		kind := strings.ToLower(strings.TrimSpace(op.Op))
		var label string
		var err error
		switch kind {
		case OpMove:
			label, err = b.move(i, op)
		case OpRotate:
			label, err = b.rotate(i, op)
		case OpScale:
			label, err = b.scale(i, op)
		case OpResize:
			label, err = b.resize(i, op)
		case OpMirror:
			label, err = b.mirror(i, op)
		case OpOrient:
			label, err = b.orient(i, op)
		case OpCenter:
			label, err = b.center(i, op)
		case OpToBed:
			lo, _ := b.box()
			b.scene.Model.Translate[2] = tidy(b.scene.Model.Translate[2] - lo[2])
			label = b.tr("Apoyar en la placa (z mínima = 0)", "Rest on the plate (minimum z = 0)")
		case OpColor:
			c, ok := normColor(op.Color)
			if !ok || c == "" {
				return Edit{}, b.fail(i, kind, "falta `color` (#rrggbb)", "`color` is required (#rrggbb)")
			}
			b.scene.Color = c
			label = b.tr("Color del modelo "+c, "Model color "+c)
		case OpAdd, OpHole, OpCut:
			pending = append(pending, i)
			continue
		default:
			return Edit{}, b.fail(i, kind, "operación desconocida (usa %s)", "unknown operation (use %s)", strings.Join(OpNames, ", "))
		}
		if err != nil {
			return Edit{}, err
		}
		b.scene.Ops = append(b.scene.Ops, label)
	}

	// Primitivas y corte, en el marco final.
	lo, hi := b.box()
	cuts := 0
	for _, i := range pending {
		op := ops[i]
		kind := strings.ToLower(strings.TrimSpace(op.Op))
		var label string
		var err error
		if kind == OpCut {
			cuts++
			if cuts > 1 {
				return Edit{}, b.fail(i, kind,
					"solo se admite un corte por llamada: haz el segundo con otra llamada a edit_mesh sobre el checkpoint que resulte",
					"only one cut per call is supported: make the second one with another edit_mesh call on the resulting checkpoint")
			}
			label, err = b.cut(i, op, lo, hi)
		} else {
			label, err = b.primitive(i, op, kind, lo, hi)
		}
		if err != nil {
			return Edit{}, err
		}
		b.scene.Ops = append(b.scene.Ops, label)
	}

	if (len(b.scene.Parts) > 0 || b.scene.Cut != nil) && !base.Watertight {
		return Edit{}, errBad(i18n.Msg{
			ES: "la malla base no es cerrada: se puede mover, girar, escalar y espejar, pero no perforar, sumar ni cortar («mesh is not closed»). Repárala antes, por ejemplo con la reparación del laminador",
			EN: "the base mesh is not watertight: it can be moved, rotated, scaled and mirrored, but not drilled, added to or cut (\"mesh is not closed\"). Repair it first, for example with your slicer's repair",
		})
	}
	if b.scene.Cut != nil {
		bounds := base.Bounds
		b.scene.Bounds = &bounds
	}
	lo, hi = b.box()
	return Edit{Scene: b.scene, Min: lo, Max: hi, Warnings: b.warns}, nil
}

func (b *builder) move(i int, op Op) (string, error) {
	if op.By == nil {
		return "", b.fail(i, OpMove, "falta `by`: [x, y, z] en mm", "`by` is required: [x, y, z] in mm")
	}
	for a := 0; a < 3; a++ {
		b.scene.Model.Translate[a] = tidy(b.scene.Model.Translate[a] + op.By.V[a])
	}
	return b.tr("Mover ", "Move ") + fmtVec(op.By.V) + " mm", nil
}

func (b *builder) rotate(i int, op Op) (string, error) {
	if op.Degrees == nil {
		return "", b.fail(i, OpRotate, "falta `degrees`: [rx, ry, rz] en grados", "`degrees` is required: [rx, ry, rz] in degrees")
	}
	if !finite3(op.Degrees.V) {
		return "", b.fail(i, OpRotate, "los grados no son números finitos", "the degrees are not finite numbers")
	}
	b.keepingCenter(func(t *Transform) { rotateWorld(t, op.Degrees.V) })
	return b.tr("Girar ", "Rotate ") + fmtVec(op.Degrees.V) + "°", nil
}

func (b *builder) scale(i int, op Op) (string, error) {
	if op.Factor == nil {
		return "", b.fail(i, OpScale, "falta `factor`: un número (parejo) o [fx, fy, fz]", "`factor` is required: a number (uniform) or [fx, fy, fz]")
	}
	f := op.Factor.V
	for _, v := range f {
		if !finite(v) || v <= 0 {
			return "", b.fail(i, OpScale, "el factor tiene que ser positivo (para voltear un eje usa mirror)", "the factor must be positive (use mirror to flip an axis)")
		}
	}
	b.keepingCenter(func(t *Transform) {
		s := t.scale()
		for a := 0; a < 3; a++ {
			s[a] *= f[a]
		}
		t.Scale = s
	})
	if err := b.scene.Model.validate(b.tr("modelo", "model")); err != nil {
		return "", err
	}
	if op.Factor.Scalar {
		return b.tr("Escalar ×", "Scale ×") + fmtNum(f[0]), nil
	}
	return b.tr("Escalar ×", "Scale ×") + fmtVec(f), nil
}

func (b *builder) resize(i int, op Op) (string, error) {
	if op.Mm == nil || float64(*op.Mm) <= 0 || !finite(float64(*op.Mm)) {
		return "", b.fail(i, OpResize, "falta `mm`: la medida final, positiva", "`mm` is required: the final size, positive")
	}
	lo, hi := b.box()
	size := [3]float64{hi[0] - lo[0], hi[1] - lo[1], hi[2] - lo[2]}
	axis, name := 0, ""
	if strings.EqualFold(strings.TrimSpace(op.Axis), "longest") {
		for a := 1; a < 3; a++ {
			if size[a] > size[axis] {
				axis = a
			}
		}
		name = b.tr("el lado más largo", "the longest side")
	} else {
		a, ok := axisOf(op.Axis)
		if !ok {
			return "", b.fail(i, OpResize, "`axis` tiene que ser x, y, z o longest", "`axis` must be x, y, z or longest")
		}
		axis, name = a, strings.ToUpper(strings.TrimSpace(op.Axis))
	}
	if size[axis] < 1e-9 {
		return "", b.fail(i, OpResize, "el modelo no tiene tamaño en ese eje", "the model has no size on that axis")
	}
	f := float64(*op.Mm) / size[axis]
	b.keepingCenter(func(t *Transform) {
		s := t.scale()
		for a := 0; a < 3; a++ {
			s[a] *= f
		}
		t.Scale = s
	})
	if err := b.scene.Model.validate(b.tr("modelo", "model")); err != nil {
		return "", err
	}
	return fmt.Sprintf(b.tr("Ajustar %s a %s mm (escala ×%s, proporcional)", "Fit %s to %s mm (scale ×%s, uniform)"), name, fmtNum(float64(*op.Mm)), fmtNum(f)), nil
}

func (b *builder) mirror(i int, op Op) (string, error) {
	a, ok := axisOf(op.Axis)
	if !ok {
		return "", b.fail(i, OpMirror, "`axis` tiene que ser x, y o z", "`axis` must be x, y or z")
	}
	b.keepingCenter(func(t *Transform) { t.Mirror[a] = !t.Mirror[a] })
	return fmt.Sprintf(b.tr("Espejar en %s (sobre los ejes del propio modelo)", "Mirror on %s (along the model's own axes)"), strings.ToUpper(strings.TrimSpace(op.Axis))), nil
}

func (b *builder) orient(i int, op Op) (string, error) {
	down := strings.ToLower(strings.TrimSpace(op.Down))
	deg, ok := orientRotation[down]
	if !ok {
		return "", b.fail(i, OpOrient, "`down` tiene que ser +x, -x, +y, -y, +z o -z", "`down` must be +x, -x, +y, -y, +z or -z")
	}
	b.keepingCenter(func(t *Transform) { rotateWorld(t, deg) })
	return fmt.Sprintf(b.tr("Poner la cara %s hacia la placa", "Put the %s face down on the plate"), down), nil
}

func (b *builder) center(i int, op Op) (string, error) {
	axes := strings.ToLower(strings.TrimSpace(op.Axes))
	if axes == "" {
		axes = "xy"
	}
	lo, hi := b.box()
	c := boxCenter(lo, hi)
	seen := ""
	for _, r := range axes {
		a, ok := axisOf(string(r))
		if !ok {
			return "", b.fail(i, OpCenter, "`axes` solo puede llevar x, y, z (p. ej. \"xy\")", "`axes` can only contain x, y, z (e.g. \"xy\")")
		}
		if strings.ContainsRune(seen, r) {
			continue
		}
		seen += string(r)
		b.scene.Model.Translate[a] = tidy(b.scene.Model.Translate[a] - c[a])
	}
	return fmt.Sprintf(b.tr("Centrar en %s", "Center on %s"), strings.ToUpper(seen)), nil
}

// primitive resuelve una unión o un agujero. lo/hi es la caja final del modelo.
func (b *builder) primitive(i int, op Op, kind string, lo, hi [3]float64) (string, error) {
	p := Primitive{
		ID:    "p" + strconv.Itoa(len(b.scene.Parts)+1),
		Shape: strings.ToLower(strings.TrimSpace(op.Shape)),
		Mode:  ModeUnion,
		Color: op.Color,
		// La posición es el centro de la forma, salvo que se pida lo contrario: con
		// at_rel y at es mucho más fácil acertar que con la esquina.
		Center: op.Center == nil || bool(*op.Center),
		Text:   op.Text,
	}
	if kind == OpHole {
		p.Mode = ModeHole
	}
	pos := func(n *Num) (float64, bool) {
		if n == nil {
			return 0, false
		}
		return float64(*n), float64(*n) > 0 && finite(float64(*n))
	}
	switch p.Shape {
	case ShapeCube:
		if op.Size == nil || op.Size.V[0] <= 0 || op.Size.V[1] <= 0 || op.Size.V[2] <= 0 {
			return "", b.fail(i, kind, "un cubo necesita `size`: [ancho, fondo, alto] en mm, positivos", "a cube needs `size`: [width, depth, height] in mm, positive")
		}
		p.Size = op.Size.V
	case ShapeCylinder:
		if op.Diameter == nil || op.Height == nil {
			return "", b.fail(i, kind, "un cilindro necesita `diameter` y `height` en mm", "a cylinder needs `diameter` and `height` in mm")
		}
		d, okd := pos(op.Diameter)
		h, okh := pos(op.Height)
		if !okd || !okh {
			return "", b.fail(i, kind, "`diameter` y `height` tienen que ser positivos", "`diameter` and `height` must be positive")
		}
		p.Size = [3]float64{d, d, h}
	case ShapeSphere:
		if op.Diameter == nil {
			return "", b.fail(i, kind, "una esfera necesita `diameter` en mm", "a sphere needs `diameter` in mm")
		}
		d, ok := pos(op.Diameter)
		if !ok {
			return "", b.fail(i, kind, "`diameter` tiene que ser positivo", "`diameter` must be positive")
		}
		p.Size = [3]float64{d, d, d}
	case ShapeText:
		ts, h := 10.0, 1.0
		if op.TextSize != nil {
			ts = float64(*op.TextSize)
		}
		if op.Height != nil {
			h = float64(*op.Height)
		}
		p.Size = [3]float64{ts, ts, h}
	default:
		return "", b.fail(i, kind, "`shape` tiene que ser cube, cylinder, sphere o text", "`shape` must be cube, cylinder, sphere or text")
	}

	// Posición: la caja del modelo (at_rel) más un desplazamiento en mm (at).
	var at [3]float64
	if op.AtRel != nil {
		for a := 0; a < 3; a++ {
			at[a] = lo[a] + op.AtRel.V[a]*(hi[a]-lo[a])
		}
	}
	if op.At != nil {
		for a := 0; a < 3; a++ {
			at[a] += op.At.V[a]
		}
	} else if op.AtRel == nil {
		at = boxCenter(lo, hi)
	}
	p.Transform.Translate = [3]float64{tidy(at[0]), tidy(at[1]), tidy(at[2])}

	// Un cilindro horizontal: axis en vez de rotar a mano.
	if op.Axis != "" && p.Shape == ShapeCylinder {
		if op.Rotate != nil {
			return "", b.fail(i, kind, "usa `axis` o `rotate`, no los dos", "use `axis` or `rotate`, not both")
		}
		switch strings.ToLower(strings.TrimSpace(op.Axis)) {
		case "z":
		case "x":
			p.Transform.Rotate = [3]float64{0, 90, 0}
		case "y":
			p.Transform.Rotate = [3]float64{-90, 0, 0}
		default:
			return "", b.fail(i, kind, "`axis` tiene que ser x, y o z", "`axis` must be x, y or z")
		}
	}
	if op.Rotate != nil {
		p.Transform.Rotate = op.Rotate.V
	}
	if _, err := p.validate(len(b.scene.Parts) + 1); err != nil {
		return "", b.fail(i, kind, "%v", "%v", err)
	}
	b.scene.Parts = append(b.scene.Parts, p)

	what := map[string][2]string{
		ShapeCube:     {"cubo " + fmtVec(p.Size) + " mm", "cube " + fmtVec(p.Size) + " mm"},
		ShapeCylinder: {"cilindro Ø" + fmtNum(p.Size[0]) + " × " + fmtNum(p.Size[2]) + " mm", "cylinder Ø" + fmtNum(p.Size[0]) + " × " + fmtNum(p.Size[2]) + " mm"},
		ShapeSphere:   {"esfera Ø" + fmtNum(p.Size[0]) + " mm", "sphere Ø" + fmtNum(p.Size[0]) + " mm"},
		ShapeText:     {fmt.Sprintf("texto %q", cleanLine(p.Text, 30)), fmt.Sprintf("text %q", cleanLine(p.Text, 30))},
	}[p.Shape]
	verb := b.tr("Sumar ", "Add ")
	if p.Mode == ModeHole {
		verb = b.tr("Agujero: ", "Hole: ")
	}
	return verb + b.tr(what[0], what[1]) + b.tr(" en ", " at ") + fmtVec(p.Transform.Translate), nil
}

// cut resuelve el corte con un plano, en el marco final.
func (b *builder) cut(i int, op Op, lo, hi [3]float64) (string, error) {
	c := Cut{Keep: strings.ToLower(strings.TrimSpace(op.Keep)), Split: strings.ToLower(strings.TrimSpace(op.Split))}
	if c.Keep == "" {
		return "", b.fail(i, OpCut, "falta `keep`: above (el lado hacia el que apunta la normal), below o both", "`keep` is required: above (the side the normal points to), below or both")
	}
	if len(op.Colors) > 0 {
		if len(op.Colors) != 2 {
			return "", b.fail(i, OpCut, "`colors` lleva dos colores: [arriba, abajo]", "`colors` takes two colors: [above, below]")
		}
		copy(c.Colors[:], op.Colors)
		if c.Split == "" {
			c.Split = SplitColors
		}
	}
	var desc string
	switch {
	case op.Axis != "":
		a, ok := axisOf(op.Axis)
		if !ok {
			return "", b.fail(i, OpCut, "`axis` tiene que ser x, y o z", "`axis` must be x, y or z")
		}
		c.Point = boxCenter(lo, hi)
		switch {
		case op.AtRel != nil:
			c.Point[a] = lo[a] + op.AtRel.V[0]*(hi[a]-lo[a])
		case op.At != nil:
			c.Point[a] = op.At.V[0]
		default:
			return "", b.fail(i, OpCut, "con `axis` indica `at` (mm) o `at_rel` (fracción 0–1 de la caja)", "with `axis` give `at` (mm) or `at_rel` (0–1 fraction of the box)")
		}
		c.Normal[a] = 1
		desc = fmt.Sprintf(b.tr("Cortar con un plano en %s = %s mm", "Cut with a plane at %s = %s mm"), strings.ToUpper(strings.TrimSpace(op.Axis)), fmtNum(c.Point[a]))
	case op.Point != nil && op.Normal != nil:
		c.Point, c.Normal = op.Point.V, op.Normal.V
		desc = fmt.Sprintf(b.tr("Cortar con un plano por %s, normal %s", "Cut with a plane through %s, normal %s"), fmtVec(c.Point), fmtVec(c.Normal))
	default:
		return "", b.fail(i, OpCut, "indica el plano: `axis` con `at`/`at_rel`, o `point` y `normal`", "give the plane: `axis` with `at`/`at_rel`, or `point` and `normal`")
	}
	for a := 0; a < 3; a++ {
		c.Point[a] = tidy(c.Point[a])
	}
	if _, _, err := c.validate(); err != nil {
		return "", b.fail(i, OpCut, "%v", "%v", err)
	}
	b.scene.Cut = &c
	keep := map[string][2]string{
		KeepAbove: {"se queda el lado de la normal", "keeps the normal's side"},
		KeepBelow: {"se queda el lado contrario", "keeps the opposite side"},
		KeepBoth:  {"se quedan los dos lados", "keeps both sides"},
	}[c.Keep]
	return desc + " (" + b.tr(keep[0], keep[1]) + ")", nil
}
