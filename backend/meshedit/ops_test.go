package meshedit

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/i18n"
)

func cubeBase() Base {
	return Base{
		Ref: "refs/cubo.stl", Model: Transform{Scale: [3]float64{1, 1, 1}},
		Bounds:     *cubeBounds,
		Watertight: true, Triangles: 12, FromImport: true,
	}
}

func opsFromJSON(t *testing.T, raw string) []Op {
	t.Helper()
	var ops []Op
	if err := json.Unmarshal([]byte(raw), &ops); err != nil {
		t.Fatalf("JSON de las operaciones: %v\n%s", err, raw)
	}
	return ops
}

func build(t *testing.T, raw string) Edit {
	t.Helper()
	e, err := BuildScene(cubeBase(), opsFromJSON(t, raw), "en")
	if err != nil {
		t.Fatalf("BuildScene: %v\n%s", err, raw)
	}
	return e
}

func TestOpsParseLenientValues(t *testing.T) {
	ops := opsFromJSON(t, `[
		{"op":"move","by":["1","2",3]},
		{"op":"scale","factor":"2"},
		{"op":"hole","shape":"cylinder","diameter":"3.4","height":"30","at":{"x":1,"z":2},"center":"false"}
	]`)
	if ops[0].By.V != [3]float64{1, 2, 3} {
		t.Errorf("by: %+v", ops[0].By)
	}
	if f := ops[1].Factor; !f.Scalar || f.V != [3]float64{2, 2, 2} {
		t.Errorf("factor escalar: %+v", f)
	}
	if h := ops[2]; float64(*h.Diameter) != 3.4 || float64(*h.Height) != 30 || h.At.V != [3]float64{1, 0, 2} || bool(*h.Center) {
		t.Errorf("hole: %+v", h)
	}
	for _, bad := range []string{`[{"op":"move","by":[1,2]}]`, `[{"op":"move","by":"x"}]`, `[{"op":"hole","diameter":{}}]`} {
		var o []Op
		if err := json.Unmarshal([]byte(bad), &o); err == nil {
			t.Errorf("%s debía fallar", bad)
		}
	}
}

func TestPoseOpsAreExactAndKeepTheCenter(t *testing.T) {
	// La caja 10 × 20 × 30 con una esquina en el origen: su centro es (5, 10, 15).
	e := build(t, `[{"op":"rotate","degrees":[0,0,90]}]`)
	if !nearVec(boxCenterOf(e), [3]float64{5, 10, 15}, 1e-6) {
		t.Errorf("girar no mueve el centro: %v %v", e.Min, e.Max)
	}
	if size := sizeOf(e); !nearVec(size, [3]float64{20, 10, 30}, 1e-6) {
		t.Errorf("giro de 90° en Z: %v", size)
	}

	// Cada cara que baja a la placa deja la medida correcta como alto.
	for down, wantH := range map[string]float64{"+x": cubeX, "-x": cubeX, "+y": cubeY, "-y": cubeY, "+z": cubeZ, "-z": cubeZ} {
		e := build(t, `[{"op":"orient","down":"`+down+`"},{"op":"to_bed"}]`)
		if !near(e.Min[2], 0, 1e-6) {
			t.Errorf("%s: no apoya en z=0: %v", down, e.Min)
		}
		// La cara +x baja → el lado en X queda como alto.
		if got := sizeOf(e)[2]; !near(got, wantH, 1e-6) {
			t.Errorf("orient %s: alto %v, esperaba %v", down, got, wantH)
		}
	}
	// Los dos giros se componen: X 90 y luego Z 90 no es lo mismo que al revés.
	a := build(t, `[{"op":"rotate","degrees":[90,0,0]},{"op":"rotate","degrees":[0,0,90]}]`)
	b := build(t, `[{"op":"rotate","degrees":[0,0,90]},{"op":"rotate","degrees":[90,0,0]}]`)
	if !nearVec(sizeOf(a), [3]float64{30, 10, 20}, 1e-6) || !nearVec(sizeOf(b), [3]float64{20, 30, 10}, 1e-6) {
		t.Errorf("composición: %v / %v", sizeOf(a), sizeOf(b))
	}
	// Y = ±90° (bloqueo de cardán) también se compone.
	for _, y := range []string{"90", "-90"} {
		g := build(t, `[{"op":"rotate","degrees":[10,`+y+`,20]},{"op":"rotate","degrees":[30,0,40]}]`)
		for _, v := range g.Scene.Model.Rotate {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("ángulo no finito: %v", g.Scene.Model.Rotate)
			}
		}
	}

	c := build(t, `[{"op":"center"},{"op":"to_bed"}]`)
	if !nearVec(c.Min, [3]float64{-5, -10, 0}, 1e-6) || !nearVec(c.Max, [3]float64{5, 10, 30}, 1e-6) {
		t.Errorf("center + to_bed: %v %v", c.Min, c.Max)
	}
	// center solo en un eje.
	cx := build(t, `[{"op":"center","axes":"x"}]`)
	if !near(cx.Min[0], -5, 1e-6) || !near(cx.Min[1], 0, 1e-6) {
		t.Errorf("center x: %v %v", cx.Min, cx.Max)
	}
	// resize: el lado más largo a 60 mm → ×2 proporcional, en su sitio.
	r := build(t, `[{"op":"resize","axis":"longest","mm":60}]`)
	if !nearVec(sizeOf(r), [3]float64{20, 40, 60}, 1e-6) || !nearVec(boxCenterOf(r), [3]float64{5, 10, 15}, 1e-6) {
		t.Errorf("resize: %v centro %v", sizeOf(r), boxCenterOf(r))
	}
	// scale no uniforme sobre los ejes del propio modelo.
	s := build(t, `[{"op":"scale","factor":[2,1,0.5]}]`)
	if !nearVec(sizeOf(s), [3]float64{20, 20, 15}, 1e-6) {
		t.Errorf("scale: %v", sizeOf(s))
	}
	// mirror, move.
	m := build(t, `[{"op":"mirror","axis":"x"},{"op":"move","by":[100,0,0]}]`)
	if !m.Scene.Model.Mirror[0] || !near(m.Min[0], 100, 1e-6) {
		t.Errorf("mirror + move: %+v %v", m.Scene.Model, m.Min)
	}
}

func boxCenterOf(e Edit) [3]float64 { return boxCenter(e.Min, e.Max) }
func sizeOf(e Edit) [3]float64 {
	return [3]float64{e.Max[0] - e.Min[0], e.Max[1] - e.Min[1], e.Max[2] - e.Min[2]}
}

func TestPrimitivesAndCutUseTheFinalFrame(t *testing.T) {
	// El agujero va ANTES de to_bed en la lista, pero se coloca con la pose final.
	e := build(t, `[
		{"op":"hole","shape":"cylinder","diameter":4,"height":50,"at_rel":[0.5,0.5,0.5]},
		{"op":"center"},{"op":"to_bed"},
		{"op":"add","shape":"cube","size":[2,2,2],"at_rel":[0.5,0.5,1],"at":[0,0,1],"color":"#ff0000"},
		{"op":"cut","axis":"z","at_rel":0.5,"keep":"both","split":"colors","colors":["#111111","#222222"]}
	]`)
	if len(e.Scene.Parts) != 2 || e.Scene.Cut == nil {
		t.Fatalf("escena: %+v", e.Scene)
	}
	hole, add := e.Scene.Parts[0], e.Scene.Parts[1]
	if hole.Mode != ModeHole || hole.Transform.Translate != [3]float64{0, 0, 15} || !hole.Center {
		t.Errorf("agujero: %+v", hole)
	}
	if add.Mode != ModeUnion || add.Transform.Translate != [3]float64{0, 0, 31} || add.Color != "#ff0000" {
		t.Errorf("unión: %+v", add)
	}
	c := e.Scene.Cut
	if c.Normal != [3]float64{0, 0, 1} || c.Point != [3]float64{0, 0, 15} || c.Keep != KeepBoth || c.Split != SplitColors || c.Colors[0] != "#111111" {
		t.Errorf("corte: %+v", c)
	}
	if e.Scene.Bounds == nil || *e.Scene.Bounds != *cubeBounds {
		t.Errorf("Bounds del corte: %+v", e.Scene.Bounds)
	}
	if len(e.Scene.Ops) != 5 {
		t.Errorf("etiquetas: %v", e.Scene.Ops)
	}
	if _, err := Generate(e.Scene, Header{Desc: "x"}, "en"); err != nil {
		t.Errorf("Generate: %v", err)
	}

	// Cilindro horizontal con axis.
	h := build(t, `[{"op":"hole","shape":"cylinder","diameter":3,"height":40,"axis":"x"}]`)
	if h.Scene.Parts[0].Transform.Rotate != [3]float64{0, 90, 0} || h.Scene.Parts[0].Transform.Translate != [3]float64{5, 10, 15} {
		t.Errorf("axis x: %+v", h.Scene.Parts[0].Transform)
	}
	// Corte con punto y normal.
	pn := build(t, `[{"op":"cut","point":[0,0,12],"normal":[0,0,2],"keep":"below"}]`)
	if pn.Scene.Cut.Point != [3]float64{0, 0, 12} || pn.Scene.Cut.Keep != KeepBelow {
		t.Errorf("point/normal: %+v", pn.Scene.Cut)
	}
}

func TestOpsRejectBadInput(t *testing.T) {
	i18n.SetLang("en")
	cases := map[string]string{
		`[]`:                                                          "operations",
		`[{"op":"explode"}]`:                                          "unknown operation",
		`[{"op":"move"}]`:                                             "`by`",
		`[{"op":"rotate"}]`:                                           "`degrees`",
		`[{"op":"scale","factor":-1}]`:                                "positive",
		`[{"op":"scale","factor":0}]`:                                 "positive",
		`[{"op":"resize","axis":"w","mm":10}]`:                        "`axis`",
		`[{"op":"resize","axis":"x"}]`:                                "`mm`",
		`[{"op":"mirror","axis":"q"}]`:                                "`axis`",
		`[{"op":"orient","down":"up"}]`:                               "`down`",
		`[{"op":"center","axes":"xq"}]`:                               "`axes`",
		`[{"op":"color","color":"red"}]`:                              "color",
		`[{"op":"add","shape":"torus"}]`:                              "`shape`",
		`[{"op":"add","shape":"cube"}]`:                               "`size`",
		`[{"op":"hole","shape":"cylinder","diameter":3}]`:             "`height`",
		`[{"op":"hole","shape":"cylinder","diameter":-3,"height":4}]`: "positive",
		`[{"op":"hole","shape":"cylinder","diameter":3,"height":4,"axis":"x","rotate":[1,2,3]}]`:        "not both",
		`[{"op":"hole","shape":"sphere","diameter":3,"color":"rojo"}]`:                                  "color",
		`[{"op":"cut","axis":"z","at":5}]`:                                                              "`keep`",
		`[{"op":"cut","keep":"above"}]`:                                                                 "plane",
		`[{"op":"cut","axis":"z","keep":"above"}]`:                                                      "`at`",
		`[{"op":"cut","axis":"z","at":1,"keep":"sideways"}]`:                                            "keep",
		`[{"op":"cut","axis":"z","at":1,"keep":"both","colors":["#fff"]}]`:                              "two colors",
		`[{"op":"cut","axis":"z","at":1,"keep":"above"},{"op":"cut","axis":"x","at":1,"keep":"above"}]`: "only one cut",
	}
	for raw, want := range cases {
		_, err := BuildScene(cubeBase(), opsFromJSON(t, raw), "en")
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: esperaba un error con %q, obtuve %v", raw, want, err)
		}
	}
	// Demasiadas operaciones.
	many := make([]Op, MaxToolOps+1)
	for i := range many {
		many[i] = Op{Op: OpToBed}
	}
	if _, err := BuildScene(cubeBase(), many, "en"); err == nil {
		t.Error("más de MaxToolOps debía fallar")
	}
	// Una malla abierta se mueve pero no se perfora ni se corta.
	open := cubeBase()
	open.Watertight = false
	if _, err := BuildScene(open, opsFromJSON(t, `[{"op":"move","by":[1,0,0]},{"op":"scale","factor":2}]`), "en"); err != nil {
		t.Errorf("mover/escalar una malla abierta sí se puede: %v", err)
	}
	for _, raw := range []string{
		`[{"op":"hole","shape":"sphere","diameter":2}]`,
		`[{"op":"cut","axis":"z","at":5,"keep":"above"}]`,
	} {
		if _, err := BuildScene(open, opsFromJSON(t, raw), "en"); err == nil || !strings.Contains(err.Error(), "not watertight") {
			t.Errorf("%s en una malla abierta: %v", raw, err)
		}
	}
}

// Lo que BuildScene calcula (la caja del modelo tras la pose) coincide con lo que
// OpenSCAD dibuja al renderizar el código generado, en el motor nativo y en el
// integrado: la álgebra de los giros compuestos es la de OpenSCAD.
func TestOpsMatchTheRender(t *testing.T) {
	for _, e := range engines(t) {
		t.Run(e.name, func(t *testing.T) {
			ws := workspaceWith(t)
			ed := build(t, `[
				{"op":"orient","down":"+x"},
				{"op":"rotate","degrees":[0,0,35]},
				{"op":"mirror","axis":"y"},
				{"op":"scale","factor":[1.5,1,2]},
				{"op":"center"},{"op":"to_bed"}
			]`)
			got := renderScene(t, ws, e, ed.Scene)
			// Una caja girada 35° tiene una caja envolvente mayor que la exacta
			// de sus 8 esquinas solo si la malla no es una caja: aquí es exacta.
			if !nearVec(got.info.Min, ed.Min, 1e-3) || !nearVec(got.info.Max, ed.Max, 1e-3) {
				t.Errorf("render %v … %v, cálculo %v … %v\n%s", got.info.Min, got.info.Max, ed.Min, ed.Max, got.src)
			}
			lintClean(t, ws, e, got.src)

			// Agujero + corte + unión: el código renderiza y la caja es la esperada.
			hc := build(t, `[
				{"op":"hole","shape":"cylinder","diameter":4,"height":60,"at_rel":[0.5,0.5,0.5]},
				{"op":"add","shape":"cube","size":[4,4,4],"at":[20,5,2],"color":"#2266cc"},
				{"op":"cut","axis":"z","at":20,"keep":"below"}
			]`)
			r := renderScene(t, ws, e, hc.Scene)
			if !r.info.Watertight || r.info.Max[2] > 20+1e-3 {
				t.Errorf("agujero + corte: %+v", r.info)
			}
			if r.info.VolumeMM3 >= cubeX*cubeY*20 { // menos que la caja recortada: el agujero resta
				t.Errorf("el volumen debía bajar por el agujero: %v", r.info.VolumeMM3)
			}
			lintClean(t, ws, e, r.src)
		})
	}
}

func TestResolveBaseFromAnImportAndFrozen(t *testing.T) {
	ws := workspaceWith(t)
	// Un import base se reconoce y sigue desde el STL original.
	scene := Scene{Ref: "refs/cubo.stl", Model: Transform{Translate: [3]float64{1, 2, 0}, Scale: [3]float64{1, 1, 1}}, Color: "#aa0000"}
	src, err := Generate(scene, Header{Desc: "x"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	b, err := ResolveBase(ws, "002-imp.scad", src, func() (string, error) { called = true; return "", nil })
	if err != nil || called || !b.FromImport || b.Ref != "refs/cubo.stl" || b.Color != "#aa0000" || b.Model.Translate != [3]float64{1, 2, 0} || !b.Watertight {
		t.Fatalf("import base: %+v %v (congeló: %v)", b, err, called)
	}
	if b.Bounds.Max != [3]float64{cubeX, cubeY, cubeZ} {
		t.Errorf("bounds: %+v", b.Bounds)
	}

	// Un diseño cualquiera: se congela su malla en refs/<checkpoint>.stl.
	mesh := filepath.Join(t.TempDir(), "render.stl")
	cubeSTL(t, mesh, 0, 0, 0, 7, 8, 9)
	b, err = ResolveBase(ws, "003-caja.scad", "cube(1);", func() (string, error) { return mesh, nil })
	if err != nil || b.FromImport || b.Ref != "refs/003-caja.stl" || b.Model.Scale != [3]float64{1, 1, 1} || b.Bounds.Max != [3]float64{7, 8, 9} {
		t.Fatalf("congelado: %+v %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(ws, "refs", "003-caja.stl")); err != nil {
		t.Error(err)
	}
	// Un render que falla no deja nada en refs/.
	_, err = ResolveBase(ws, "004-roto.scad", "cube(1);", func() (string, error) { return "", os.ErrNotExist })
	if err == nil {
		t.Error("sin malla no hay base")
	}
	if _, err := os.Stat(filepath.Join(ws, "refs", "004-roto.stl")); err == nil {
		t.Error("no debía dejar un STL")
	}
}
