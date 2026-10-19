package meshedit

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"scadlab/backend/i18n"
)

func TestGenerateRejectsInvalidScenes(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	ok := func() Scene { return Scene{Ref: "refs/cubo.stl", Bounds: cubeBounds} }
	part := func(f func(p *Primitive)) Scene {
		s := ok()
		p := Primitive{Shape: ShapeCube, Mode: ModeUnion, Size: [3]float64{1, 1, 1}}
		f(&p)
		s.Parts = []Primitive{p}
		return s
	}
	cut := func(f func(c *Cut)) Scene {
		s := ok()
		c := Cut{Point: [3]float64{0, 0, 5}, Normal: [3]float64{0, 0, 1}, Keep: KeepAbove}
		f(&c)
		s.Cut = &c
		return s
	}
	model := func(f func(t *Transform)) Scene {
		s := ok()
		f(&s.Model)
		return s
	}
	refScene := func(ref string) Scene { s := ok(); s.Ref = ref; return s }

	cases := map[string]Scene{
		// Ref
		"ref vacía":                  refScene(""),
		"ref sin refs/":              refScene("cubo.stl"),
		"ref con ..":                 refScene("refs/../cubo.stl"),
		"ref con .. al principio":    refScene("../refs/cubo.stl"),
		"ref absoluta":               refScene("/etc/passwd"),
		"ref con subcarpeta":         refScene("refs/sub/cubo.stl"),
		"ref con barra invertida":    refScene(`refs\cubo.stl`),
		"ref con unidad":             refScene("refs/C:cubo.stl"),
		"ref no STL":                 refScene("refs/cubo.obj"),
		"ref 3MF":                    refScene("refs/cubo.3mf"),
		"ref sin nombre":             refScene("refs/.stl"),
		"ref oculta":                 refScene("refs/.cubo.stl"),
		"ref con comilla":            refScene(`refs/cu"bo.stl`),
		"ref con salto de línea":     refScene("refs/cubo.stl\nimport(\"/etc/passwd\")"),
		"ref con NUL":                refScene("refs/cu\x00bo.stl"),
		"ref enorme":                 refScene("refs/" + strings.Repeat("a", 300) + ".stl"),
		"ref que escapa del literal": refScene("refs/a\"); cube(1); import(\"x.stl"),
		// Colores
		"color del modelo":    func() Scene { s := ok(); s.Color = "red"; return s }(),
		"color corto":         func() Scene { s := ok(); s.Color = "#12345"; return s }(),
		"color no hex":        func() Scene { s := ok(); s.Color = "#gggggg"; return s }(),
		"color de pieza":      part(func(p *Primitive) { p.Color = "blue" }),
		"color con inyección": part(func(p *Primitive) { p.Color = `#ffffff"); cube(9); color("#000000` }),
		// Modelo
		"traslación NaN":    model(func(t *Transform) { t.Translate[1] = nan }),
		"rotación infinita": model(func(t *Transform) { t.Rotate[2] = inf }),
		"escala negativa":   model(func(t *Transform) { t.Scale[0] = -2 }),
		"escala NaN":        model(func(t *Transform) { t.Scale[1] = nan }),
		"escala ínfima":     model(func(t *Transform) { t.Scale[2] = 1e-12 }),
		"traslación enorme": model(func(t *Transform) { t.Translate[0] = 1e9 }),
		// Primitivas
		"modo desconocido":  part(func(p *Primitive) { p.Mode = "x" }),
		"forma desconocida": part(func(p *Primitive) { p.Shape = "torus" }),
		"cubo con cero":     part(func(p *Primitive) { p.Size = [3]float64{1, 0, 1} }),
		"cubo negativo":     part(func(p *Primitive) { p.Size = [3]float64{1, -1, 1} }),
		"cubo NaN":          part(func(p *Primitive) { p.Size = [3]float64{1, nan, 1} }),
		"cubo infinito":     part(func(p *Primitive) { p.Size = [3]float64{inf, 1, 1} }),
		"cilindro sin alto": part(func(p *Primitive) { p.Shape = ShapeCylinder; p.Size = [3]float64{5, 5, 0} }),
		"cilindro sin d":    part(func(p *Primitive) { p.Shape = ShapeCylinder; p.Size = [3]float64{0, 5, 5} }),
		"esfera sin d":      part(func(p *Primitive) { p.Shape = ShapeSphere; p.Size = [3]float64{0, 0, 0} }),
		"texto sin tamaño":  part(func(p *Primitive) { p.Shape = ShapeText; p.Text = "a"; p.Size = [3]float64{0, 0, 1} }),
		"texto sin alto":    part(func(p *Primitive) { p.Shape = ShapeText; p.Text = "a"; p.Size = [3]float64{4, 0, 0} }),
		"texto vacío":       part(func(p *Primitive) { p.Shape = ShapeText; p.Text = "  "; p.Size = [3]float64{4, 0, 1} }),
		"texto larguísimo": part(func(p *Primitive) {
			p.Shape = ShapeText
			p.Text = strings.Repeat("a", 500)
			p.Size = [3]float64{4, 0, 1}
		}),
		"primitiva movida a NaN": part(func(p *Primitive) { p.Transform.Translate[0] = nan }),
		// Corte
		"normal nula":       cut(func(c *Cut) { c.Normal = [3]float64{} }),
		"normal NaN":        cut(func(c *Cut) { c.Normal[0] = nan }),
		"punto infinito":    cut(func(c *Cut) { c.Point[1] = inf }),
		"keep desconocido":  cut(func(c *Cut) { c.Keep = "left" }),
		"keep vacío":        cut(func(c *Cut) { c.Keep = "" }),
		"split desconocido": cut(func(c *Cut) { c.Keep = KeepBoth; c.Split = "x" }),
		"color del corte":   cut(func(c *Cut) { c.Keep = KeepBoth; c.Split = SplitColors; c.Colors[0] = "rojo" }),
		"corte sin Bounds":  func() Scene { s := cut(func(c *Cut) {}); s.Bounds = nil; return s }(),
		"Bounds con NaN":    func() Scene { s := cut(func(c *Cut) {}); s.Bounds = &Bounds{Max: [3]float64{nan, 1, 1}}; return s }(),
		// Cantidades
		"demasiadas primitivas": func() Scene {
			s := ok()
			for i := 0; i < maxParts+1; i++ {
				s.Parts = append(s.Parts, Primitive{Shape: ShapeCube, Mode: ModeUnion, Size: [3]float64{1, 1, 1}})
			}
			return s
		}(),
		"demasiadas operaciones": func() Scene { s := ok(); s.Ops = make([]string, maxOps+1); return s }(),
	}
	for _, lang := range []string{"es", "en"} {
		i18n.SetLang(lang)
		for name, s := range cases {
			src, err := Generate(s, Header{Parent: "001-base.scad"}, lang)
			if err == nil {
				t.Errorf("%s (%s): debía rechazarse y generó:\n%s", name, lang, src)
				continue
			}
			if src != "" || strings.TrimSpace(err.Error()) == "" {
				t.Errorf("%s: error sin mensaje o con código a medias", name)
			}
		}
	}
	i18n.SetLang("en")

	// Una escena válida mínima sí pasa.
	if _, err := Generate(ok(), Header{}, "en"); err != nil {
		t.Errorf("la escena mínima válida falla: %v", err)
	}
}

// Una escena con valores raros pero válidos no rompe el literal ni el código.
func TestUnusualButValidSceneStaysWellFormed(t *testing.T) {
	s := Scene{
		Ref:   "refs/Pieza Ñ (1).stl",
		Color: "#ABCDEF",
		Model: Transform{Translate: [3]float64{-0.0000004, 1e-9, 99999}, Rotate: [3]float64{360, -720, 0.5}, Scale: [3]float64{0, 3, 0}},
	}
	src, err := Generate(s, Header{Parent: "001-base.scad", Desc: "x"}, "en")
	if err != nil {
		t.Fatal(err)
	}
	containsAll(t, src, `import("refs/Pieza Ñ (1).stl", convexity = 10)`, `color("#abcdef")`, "translate([0, 0, 99999])", "scale([1, 3, 1])")
	if strings.Contains(src, "-0") && strings.Contains(src, "[-0") {
		t.Errorf("salió un -0:\n%s", src)
	}
}

func TestBaseOfRecognizesOnlyWhatGenerateWrote(t *testing.T) {
	base := Scene{
		Ref: "refs/cubo.stl", Color: "#112233",
		Model: Transform{Translate: [3]float64{-5, -10, 0.123456789}, Rotate: [3]float64{0, 0, 90}, Scale: [3]float64{1, 2, 1}, Mirror: [3]bool{true, false, false}},
	}
	src, err := Generate(base, Header{Parent: "001-base.scad", Desc: "mover"}, "es")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := BaseOf(src)
	if !ok || !reflect.DeepEqual(got.Model, base.Model) || got.Ref != base.Ref || got.Color != base.Color {
		t.Fatalf("BaseOf = %+v, %v", got, ok)
	}
	// Los comentarios, la sangría y el idioma no cuentan; los finales \r\n tampoco.
	en, _ := Generate(base, Header{Parent: "x.scad", Desc: "other"}, "en")
	if _, ok := BaseOf(en); !ok {
		t.Error("el mismo import en inglés debe reconocerse")
	}
	if _, ok := BaseOf(strings.ReplaceAll(src, "\n", "\r\n")); !ok {
		t.Error("con finales de línea de Windows debe reconocerse")
	}
	if _, ok := BaseOf(src + "\n// y una nota más\n"); !ok {
		t.Error("un comentario de más no cambia el código")
	}

	// Cualquier cosa que cambie el código lo deja de ser, aunque conserve la marca.
	tampered := map[string]string{
		"geometría de más":   src + "\ncube(100);\n",
		"otra referencia":    strings.Replace(src, `import("refs/cubo.stl"`, `import("refs/otro.stl"`, 1),
		"transform cambiado": strings.Replace(src, "rotate([0, 0, 90])", "rotate([0, 0, 91])", 1),
		"sin color":          strings.Replace(src, `color("#112233") modelo();`, "modelo();", 1),
		"marca inválida":     strings.Replace(src, `{"ref"`, `{"re`, 1),
		"sin marca":          strings.Replace(src, MarkerPrefix, "// otra cosa ", 1),
	}
	for name, s := range tampered {
		if _, ok := BaseOf(s); ok {
			t.Errorf("%s: no debía reconocerse como import base:\n%s", name, s)
		}
	}

	// Una escena con primitivas o corte nunca es un import base.
	withPart := base
	withPart.Parts = []Primitive{{Shape: ShapeCube, Mode: ModeHole, Size: [3]float64{1, 1, 1}}}
	ps, _ := Generate(withPart, Header{}, "en")
	if _, ok := BaseOf(ps); ok {
		t.Error("una edición con agujero no es un import base")
	}
	if _, ok := BaseOf("// @checkpoint parent= desc=\"x\"\ncolor(\"red\") cube(1);\n"); ok {
		t.Error("un checkpoint cualquiera no es un import base")
	}
	if _, ok := BaseOf(""); ok {
		t.Error("vacío")
	}
}
