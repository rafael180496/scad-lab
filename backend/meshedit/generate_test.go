package meshedit

import (
	"math"
	"strings"
	"testing"
)

// expect es lo que tiene que medir el render de una escena.
type expect struct {
	min, max   [3]float64 // caja
	boxTol     float64
	volume     float64 // 0 = no se comprueba
	volTol     float64
	watertight bool
	colors     []string // colores de las partes (ordenados); nil = no se comprueba
}

type sceneCase struct {
	name  string
	scene Scene
	want  expect
	// code: trozos que tiene que traer el código generado.
	code []string
}

func cutScene(c Cut) Scene {
	return Scene{Ref: "refs/cubo.stl", Bounds: cubeBounds, Cut: &c}
}

func ident() Transform { return Transform{} }

func sceneCases() []sceneCase {
	half := cubeVolume / 2
	return []sceneCase{
		{
			name:  "sin cambios (import base)",
			scene: Scene{Ref: "refs/cubo.stl"},
			want:  expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, cubeZ}, boxTol: 1e-3, volume: cubeVolume, volTol: 0.01, watertight: true, colors: []string{"#9CA3AF"}},
			code:  []string{"@scadlab-mesh", `import("refs/cubo.stl", convexity = 10)`, `color("#9ca3af") modelo();`},
		},
		{
			name: "mover, escalar y espejar",
			scene: Scene{Ref: "refs/cubo.stl", Color: "#FF8800", Model: Transform{
				Translate: [3]float64{5, 0, 0}, Scale: [3]float64{2, 1, 1}, Mirror: [3]bool{true, false, false},
			}},
			// Espejo en X: [-10,0]; escala x2: [-20,0]; mover 5: [-15,5].
			want: expect{min: [3]float64{-15, 0, 0}, max: [3]float64{5, cubeY, cubeZ}, boxTol: 1e-3, volume: 2 * cubeVolume, volTol: 0.01, watertight: true, colors: []string{"#FF8800"}},
			code: []string{"Mover 5 mm en X", "translate([5, 0, 0])", "scale([2, 1, 1])", "mirror([1, 0, 0])", `color("#ff8800")`},
		},
		{
			name:  "corte: se queda arriba",
			scene: cutScene(Cut{Point: [3]float64{0, 0, 15}, Normal: [3]float64{0, 0, 1}, Keep: KeepAbove}),
			want:  expect{min: [3]float64{0, 0, 15}, max: [3]float64{cubeX, cubeY, cubeZ}, boxTol: 1e-3, volume: half, volTol: 0.01, watertight: true},
			code:  []string{"multmatrix", "pieza(-1);", "module semiespacio"},
		},
		{
			name:  "corte: se queda abajo",
			scene: cutScene(Cut{Point: [3]float64{0, 0, 15}, Normal: [3]float64{0, 0, 1}, Keep: KeepBelow}),
			want:  expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, 15}, boxTol: 1e-3, volume: half, volTol: 0.01, watertight: true},
			code:  []string{"pieza(1);"},
		},
		{
			// La normal no está normalizada ni alineada a un eje: el plano x = 5
			// con normal (-3, 0, 0) deja el lado de x < 5.
			name:  "corte: normal sin normalizar y hacia -X",
			scene: cutScene(Cut{Point: [3]float64{5, 7, 7}, Normal: [3]float64{-3, 0, 0}, Keep: KeepAbove}),
			want:  expect{min: [3]float64{0, 0, 0}, max: [3]float64{5, cubeY, cubeZ}, boxTol: 1e-3, volume: half, volTol: 0.01, watertight: true},
		},
		{
			// Plano oblicuo por el centro de la caja: la mitad exacta en volumen.
			name:  "corte: plano oblicuo por el centro",
			scene: cutScene(Cut{Point: [3]float64{5, 10, 15}, Normal: [3]float64{1, 1, 1}, Keep: KeepAbove}),
			want:  expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, cubeZ}, boxTol: 1e-3, volume: half, volTol: 0.05, watertight: true},
		},
		{
			// Como objetos: la mitad de abajo se mueve (10 de ancho + 10 de margen)
			// en X. Caja total: X de 0 a 30, Z de 0 a 30 (la de arriba queda en su sitio).
			name:  "partir en dos objetos",
			scene: cutScene(Cut{Point: [3]float64{0, 0, 15}, Normal: [3]float64{0, 0, 1}, Keep: KeepBoth, Split: SplitObjects}),
			want:  expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX + 20, cubeY, cubeZ}, boxTol: 1e-3, volume: cubeVolume, volTol: 0.02, watertight: true, colors: []string{"#9CA3AF"}},
			code:  []string{"module pieza(lado = 0)", "pieza(-1);", "translate([20, 0, 0])", "pieza(1);"},
		},
		{
			name: "partir en dos colores",
			scene: cutScene(Cut{
				Point: [3]float64{0, 0, 15}, Normal: [3]float64{0, 0, 1}, Keep: KeepBoth, Split: SplitColors,
				Colors: [2]string{"#FF0000", "#0000ff"},
			}),
			want: expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, cubeZ}, boxTol: 1e-3, volume: cubeVolume, volTol: 0.02, watertight: true, colors: []string{"#0000FF", "#FF0000"}},
			code: []string{`pieza(-1, "#ff0000");`, `pieza(1, "#0000ff");`},
		},
		{
			name: "cubo como unión, con su color",
			scene: Scene{Ref: "refs/cubo.stl", Parts: []Primitive{{
				ID: "a", Shape: ShapeCube, Mode: ModeUnion, Size: [3]float64{5, 5, 5}, Color: "#ff0000",
				Transform: Transform{Translate: [3]float64{12, 0, 0}},
			}}},
			want: expect{min: [3]float64{0, 0, 0}, max: [3]float64{17, cubeY, cubeZ}, boxTol: 1e-3, volume: cubeVolume + 125, volTol: 0.02, watertight: false, colors: []string{"#9CA3AF", "#FF0000"}},
			code: []string{"Unión 1: cubo 5 × 5 × 5 mm", `color("#ff0000")`},
		},
		{
			name: "cubo como agujero",
			scene: Scene{Ref: "refs/cubo.stl", Parts: []Primitive{{
				Shape: ShapeCube, Mode: ModeHole, Size: [3]float64{4, 4, 4}, Center: true,
				Transform: Transform{Translate: [3]float64{5, 10, 15}},
			}}},
			// Un hueco cerrado dentro: la caja no cambia, el volumen baja 64.
			want: expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, cubeZ}, boxTol: 1e-3, volume: cubeVolume - 64, volTol: 0.02, watertight: true},
			code: []string{"Agujero 1: cubo 4 × 4 × 4 mm", "module huecos()", "difference() {", "center = true"},
		},
		{
			name: "cilindro como unión",
			scene: Scene{Ref: "refs/cubo.stl", Parts: []Primitive{{
				Shape: ShapeCylinder, Mode: ModeUnion, Size: [3]float64{10, 10, 8}, Color: "#00ff00",
				Transform: Transform{Translate: [3]float64{5, 10, 30}},
			}}},
			// Ø10 con $fn múltiplo de 4: la caja es el diámetro exacto.
			want: expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, 38}, boxTol: 1e-3, watertight: false, colors: []string{"#00FF00", "#9CA3AF"}},
			code: []string{"cylinder(d = 10, h = 8, $fn = 32)"},
		},
		{
			// Agujero pasante Ø4 (con 32 lados): el volumen baja el área del polígono por 30.
			name: "cilindro como agujero pasante",
			scene: Scene{Ref: "refs/cubo.stl", Parts: []Primitive{{
				Shape: ShapeCylinder, Mode: ModeHole, Size: [3]float64{4, 4, 40}, Center: true,
				Transform: Transform{Translate: [3]float64{5, 10, 15}},
			}}},
			want: expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, cubeZ}, boxTol: 1e-3, volume: cubeVolume - polygonArea(2, 32)*cubeZ, volTol: 0.05, watertight: true},
		},
		{
			name: "esfera como unión",
			scene: Scene{Ref: "refs/cubo.stl", Parts: []Primitive{{
				Shape: ShapeSphere, Mode: ModeUnion, Size: [3]float64{10, 0, 0}, Color: "#0000ff",
				Transform: Transform{Translate: [3]float64{5, 10, 35}},
			}}},
			// OpenSCAD no pone vértices en los polos: la cima queda 0,006 mm por debajo.
			want: expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, 40}, boxTol: 0.05, watertight: false, colors: []string{"#0000FF", "#9CA3AF"}},
			code: []string{"sphere(d = 10, $fn = 32)"},
		},
		{
			name: "esfera como agujero",
			scene: Scene{Ref: "refs/cubo.stl", Parts: []Primitive{{
				Shape: ShapeSphere, Mode: ModeHole, Size: [3]float64{12, 0, 0},
				Transform: Transform{Translate: [3]float64{5, 10, 30}},
			}}},
			// Una cavidad en la cara superior: la caja no cambia, el volumen baja (menos de media esfera).
			want: expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, cubeZ}, boxTol: 1e-3, volume: cubeVolume - sphereHalf(6), volTol: 25, watertight: true},
		},
		{
			name: "texto en relieve",
			scene: Scene{Ref: "refs/cubo.stl", Parts: []Primitive{{
				Shape: ShapeText, Mode: ModeUnion, Size: [3]float64{4, 0, 2}, Text: "Hi", Center: false, Color: "#ffffff",
				Transform: Transform{Translate: [3]float64{1, 8, 30}},
			}}},
			want: expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, 32}, boxTol: 1e-3, watertight: false, colors: []string{"#9CA3AF", "#FFFFFF"}},
			code: []string{`text("Hi", size = 4)`, "linear_extrude(height = 2)"},
		},
		{
			name: "texto grabado",
			scene: Scene{Ref: "refs/cubo.stl", Parts: []Primitive{{
				Shape: ShapeText, Mode: ModeHole, Size: [3]float64{4, 0, 3}, Text: `A"B\n`, Center: true,
				Transform: Transform{Translate: [3]float64{5, 10, 30}},
			}}},
			want: expect{min: [3]float64{0, 0, 0}, max: [3]float64{cubeX, cubeY, cubeZ}, boxTol: 1e-3, watertight: true},
			code: []string{`text("A\"B\\n", size = 4, halign = "center", valign = "center")`},
		},
		{
			name: "varias primitivas con corte y agujero (unión + agujero + corte)",
			scene: Scene{
				Ref: "refs/cubo.stl", Color: "#112233", Bounds: cubeBounds,
				Parts: []Primitive{
					{Shape: ShapeCube, Mode: ModeUnion, Size: [3]float64{10, 20, 5}, Color: "#ff0000", Transform: Transform{Translate: [3]float64{0, 0, 30}}},
					{Shape: ShapeCylinder, Mode: ModeHole, Size: [3]float64{4, 4, 100}, Center: true, Transform: Transform{Translate: [3]float64{5, 10, 0}}},
				},
				Cut: &Cut{Point: [3]float64{0, 0, 15}, Normal: [3]float64{0, 0, 1}, Keep: KeepAbove},
			},
			// Se queda de z = 15 a 35, con el agujero Ø4 pasando por todo.
			want: expect{min: [3]float64{0, 0, 15}, max: [3]float64{cubeX, cubeY, 35}, boxTol: 1e-3, volume: cubeX*cubeY*20 - polygonArea(2, 32)*20, volTol: 0.1, watertight: true, colors: []string{"#112233", "#FF0000"}},
		},
		{
			// Las primitivas que se suman también se parten: la mitad de abajo
			// lleva el cubo rojo que sobresale por abajo.
			name: "partir en dos colores con una primitiva que cruza el plano",
			scene: Scene{
				Ref: "refs/cubo.stl", Bounds: cubeBounds,
				Parts: []Primitive{{Shape: ShapeCube, Mode: ModeUnion, Size: [3]float64{4, 4, 10}, Color: "#00ff00", Transform: Transform{Translate: [3]float64{12, 0, 10}}}},
				Cut:   &Cut{Point: [3]float64{0, 0, 15}, Normal: [3]float64{0, 0, 1}, Keep: KeepBoth, Split: SplitColors, Colors: [2]string{"#ff0000", "#0000ff"}},
			},
			want: expect{min: [3]float64{0, 0, 0}, max: [3]float64{16, cubeY, cubeZ}, boxTol: 1e-3, volume: cubeVolume + 160, volTol: 0.05, colors: []string{"#0000FF", "#00FF00", "#FF0000"}},
		},
	}
}

func polygonArea(r float64, n int) float64 {
	return 0.5 * float64(n) * r * r * math.Sin(2*math.Pi/float64(n))
}

// sphereHalf es el volumen de la cavidad que deja una esfera de radio r centrada
// en la cara superior: media esfera.
func sphereHalf(r float64) float64 { return 2.0 / 3.0 * math.Pi * r * r * r }

// Cada operación se renderiza de verdad —con el OpenSCAD nativo y con el motor
// integrado— y se compara con la cuenta a mano: caja, volumen, si la malla es
// cerrada y las partes por color.
func TestGenerateRendersWhatItPromises(t *testing.T) {
	for _, e := range engines(t) {
		for _, c := range sceneCases() {
			t.Run(e.name+"/"+c.name, func(t *testing.T) {
				ws := workspaceWith(t)
				r := renderScene(t, ws, e, c.scene)
				containsAll(t, r.src, c.code...)
				if !nearVec(r.info.Min, c.want.min, c.want.boxTol) || !nearVec(r.info.Max, c.want.max, c.want.boxTol) {
					t.Errorf("caja = %v … %v, esperaba %v … %v\n%s", r.info.Min, r.info.Max, c.want.min, c.want.max, r.src)
				}
				if c.want.volume > 0 && !near(r.info.VolumeMM3, c.want.volume, c.want.volTol+c.want.volume*0.0005) {
					t.Errorf("volumen = %.2f, esperaba %.2f ± %.2f\n%s", r.info.VolumeMM3, c.want.volume, c.want.volTol, r.src)
				}
				if c.want.watertight && !r.info.Watertight {
					t.Errorf("la malla no es cerrada\n%s", r.src)
				}
				if c.want.colors != nil {
					got, _ := colorsOf(t, ws, e, r.src)
					if strings.Join(got, ",") != strings.Join(c.want.colors, ",") {
						t.Errorf("colores = %v, esperaba %v\n%s", got, c.want.colors, r.src)
					}
				}
				lintClean(t, ws, e, r.src)
			})
		}
	}
}

// En un corte por colores cada mitad lleva su color y queda de su lado del plano.
func TestSplitColorsEachHalfOnItsSide(t *testing.T) {
	for _, e := range engines(t) {
		t.Run(e.name, func(t *testing.T) {
			ws := workspaceWith(t)
			r := renderScene(t, ws, e, cutScene(Cut{
				Point: [3]float64{0, 0, 15}, Normal: [3]float64{0, 0, 1}, Keep: KeepBoth, Split: SplitColors,
				Colors: [2]string{"#ff0000", "#0000ff"},
			}))
			_, boxes := colorsOf(t, ws, e, r.src)
			red, blue := boxes["#FF0000"], boxes["#0000FF"]
			if !near(red[0][2], 15, 1e-3) || !near(red[1][2], 30, 1e-3) {
				t.Errorf("la mitad de arriba (rojo) debía ir de z=15 a 30: %v", red)
			}
			if !near(blue[0][2], 0, 1e-3) || !near(blue[1][2], 15, 1e-3) {
				t.Errorf("la mitad de abajo (azul) debía ir de z=0 a 15: %v", blue)
			}
		})
	}
}

// La rotación de OpenSCAD es Euler 'ZYX' de three: rotate([x,y,z]) gira X,
// luego Y, luego Z sobre los ejes fijos. Esta prueba fija la convención con una
// rotación en DOS ejes, donde 'XYZ' daría otra pose, y compara el render con la
// cuenta hecha a mano.
func TestRotationIsEulerZYX(t *testing.T) {
	// A mano, para la caja (10, 20, 30) con rotate([90, 0, 90]):
	//   Rx(90): (x, y, z) → (x, -z, y)
	//   Rz(90): (x, y, z) → (-y, x, z)
	//   juntas: (x, y, z) → (z, x, y)
	// Es decir X' ∈ [0, 30], Y' ∈ [0, 10], Z' ∈ [0, 20].
	// El orden contrario ('XYZ') daría X' ∈ [-20, 0], Y' ∈ [-30, 0], Z' ∈ [0, 10].
	hand := struct{ min, max [3]float64 }{[3]float64{0, 0, 0}, [3]float64{30, 10, 20}}
	tf := Transform{Rotate: [3]float64{90, 0, 90}}
	corners := func(tf Transform) (lo, hi [3]float64) {
		lo = [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
		hi = [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
		for i := 0; i < 8; i++ {
			p := [3]float64{0, 0, 0}
			for k, s := range [3]float64{cubeX, cubeY, cubeZ} {
				if i>>k&1 == 1 {
					p[k] = s
				}
			}
			w := tf.Point(p)
			for k := range w {
				lo[k], hi[k] = math.Min(lo[k], w[k]), math.Max(hi[k], w[k])
			}
		}
		return
	}
	if lo, hi := corners(tf); !nearVec(lo, hand.min, 1e-9) || !nearVec(hi, hand.max, 1e-9) {
		t.Fatalf("Transform.Point no sigue ZYX: %v … %v", lo, hi)
	}

	cases := []Transform{
		tf,
		{Rotate: [3]float64{30, 40, 50}, Translate: [3]float64{1, 2, 3}},
		{Rotate: [3]float64{-20, 70, 15}, Scale: [3]float64{1.5, 1, 2}, Mirror: [3]bool{false, true, false}, Translate: [3]float64{-4, 0, 9}},
	}
	for _, e := range engines(t) {
		for i, c := range cases {
			t.Run(e.name+"/"+string(rune('a'+i)), func(t *testing.T) {
				ws := workspaceWith(t)
				r := renderScene(t, ws, e, Scene{Ref: "refs/cubo.stl", Model: c})
				lo, hi := corners(c)
				if !nearVec(r.info.Min, lo, 2e-3) || !nearVec(r.info.Max, hi, 2e-3) {
					t.Errorf("render = %v … %v, a mano = %v … %v\n%s", r.info.Min, r.info.Max, lo, hi, r.src)
				}
				if !r.info.Watertight {
					t.Error("la malla rotada no es cerrada")
				}
			})
		}
	}
}

// El mismo escenario dos veces da el mismo código (la edición es una función de
// la escena) y no hay nada que dependa del idioma salvo los comentarios.
func TestGenerateIsDeterministicAndOnlyCommentsChangeWithLanguage(t *testing.T) {
	s := Scene{
		Ref: "refs/cubo.stl", Bounds: cubeBounds, Color: "#abcdef",
		Model: Transform{Translate: [3]float64{1, 2, 3}, Rotate: [3]float64{0, 0, 45}},
		Parts: []Primitive{
			{Shape: ShapeCube, Mode: ModeUnion, Size: [3]float64{1, 2, 3}},
			{Shape: ShapeText, Mode: ModeHole, Size: [3]float64{4, 0, 1}, Text: "x"},
		},
		Cut: &Cut{Point: [3]float64{0, 0, 5}, Normal: [3]float64{0, 1, 0}, Keep: KeepBoth},
		Ops: []string{"Mover", "Rotar"},
	}
	h := Header{Parent: "001-base.scad", Desc: "x"}
	a, err := Generate(s, h, "es")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Generate(s, h, "es")
	if a != b {
		t.Error("la misma escena dio dos códigos distintos")
	}
	en, _ := Generate(s, h, "en")
	if a == en || codeOnly(a) != codeOnly(en) {
		t.Error("el idioma solo debe cambiar los comentarios")
	}
	containsAll(t, a, "// Mover 1 mm en X", "// Rotar 45° en Z", "//   1. Mover", "//   2. Rotar")
	containsAll(t, en, "// Move 1 mm along X", "// Rotate 45° around Z", "// Operations:")
	if !strings.HasPrefix(a, `// @checkpoint parent=001-base.scad desc="x"`+"\n") {
		t.Errorf("cabecera: %q", strings.SplitN(a, "\n", 2)[0])
	}
	// Con primitivas no es un import base.
	if strings.Contains(a, MarkerPrefix) {
		t.Error("una escena con primitivas no lleva la marca de import base")
	}
}

// Las comillas, las barras y los saltos de línea de un texto no rompen el código.
func TestTextAndLabelsAreEscaped(t *testing.T) {
	s := Scene{
		Ref: "refs/cubo.stl",
		Ops: []string{"línea 1\nlínea 2 // y esto", strings.Repeat("x", 500)},
		Parts: []Primitive{{
			Shape: ShapeText, Mode: ModeUnion, Size: [3]float64{4, 0, 1},
			Text: "a\"b\\c\nd\te\x00f",
		}},
	}
	src, err := Generate(s, Header{Parent: "001-base.scad", Desc: `con "comillas"`}, "es")
	if err != nil {
		t.Fatal(err)
	}
	containsAll(t, src, `text("a\"b\\c\nd\tef"`)
	for _, l := range strings.Split(src, "\n") {
		if strings.Contains(l, "línea 2") && !strings.HasPrefix(l, "//") {
			t.Errorf("una etiqueta se coló fuera de un comentario: %q", l)
		}
		if len([]rune(l)) > 400 {
			t.Errorf("línea larguísima (%d)", len(l))
		}
	}
}
