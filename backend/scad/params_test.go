package scad

import (
	"io/fs"
	"path"
	"strings"
	"testing"

	"scadlab/assets"
)

const paramsSample = `// @checkpoint parent= desc="x"
/* [Caja] */
// Ancho interior en mm
ancho = 60;      // [20:1:180]
alto = 25.5;     // [10:0.5:80]
tapa = "snap";   // [snap, slide:Corrediza]
con_logo = true;
tam = [10, -20, 30.5];
derivado = ancho * 2;   // una expresión no es parámetro
$fn = 32;

/* [Hidden] */
eps = 0.01;

module m() { cube(1); }
tarde = 5;
color("red") cube([ancho, alto, 5]);
`

func TestParametersReadsTheCustomizer(t *testing.T) {
	ps, err := Parameters(paramsSample)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Param{}
	var order []string
	for _, p := range ps {
		by[p.Name] = p
		order = append(order, p.Name)
	}
	if got := strings.Join(order, ","); got != "ancho,alto,tapa,con_logo,tam,eps" {
		t.Fatalf("parámetros: %s", got)
	}
	a := by["ancho"]
	if a.Type != ParamNumber || a.Value != 60.0 || a.Group != "Caja" || a.Description != "Ancho interior en mm" || a.Line != 4 {
		t.Errorf("ancho: %+v", a)
	}
	if a.Min == nil || *a.Min != 20 || *a.Step != 1 || *a.Max != 180 {
		t.Errorf("rango de ancho: %+v", a)
	}
	if by["alto"].Value != 25.5 {
		t.Errorf("alto: %+v", by["alto"])
	}
	tapa := by["tapa"]
	if tapa.Type != ParamString || tapa.Value != "snap" || len(tapa.Options) != 2 || tapa.Options[1].Value != "slide" || tapa.Options[1].Label != "Corrediza" {
		t.Errorf("tapa: %+v", tapa)
	}
	if by["con_logo"].Type != ParamBool || by["con_logo"].Value != true {
		t.Errorf("con_logo: %+v", by["con_logo"])
	}
	v, _ := by["tam"].Value.([]float64)
	if by["tam"].Type != ParamVector || len(v) != 3 || v[1] != -20 || v[2] != 30.5 {
		t.Errorf("tam: %+v", by["tam"])
	}
	if !by["eps"].Hidden {
		t.Errorf("eps debe estar oculto: %+v", by["eps"])
	}
	if _, has := by["tarde"]; has {
		t.Error("lo que viene después del primer module no es del Customizer")
	}
	if _, has := by["derivado"]; has {
		t.Error("una expresión no es un parámetro")
	}
}

func TestSetParametersKeepsEverythingElse(t *testing.T) {
	out, changes, warns, err := SetParameters(paramsSample, map[string]any{
		"ancho": 80.0, "tapa": "slide", "con_logo": "false", "tam": []any{1.0, 2.0, "3.5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 4 || len(warns) != 0 {
		t.Fatalf("cambios %+v avisos %v", changes, warns)
	}
	for _, want := range []string{"ancho = 80;      // [20:1:180]", `tapa = "slide";   // [snap, slide:Corrediza]`, "con_logo = false;", "tam = [1, 2, 3.5];", "alto = 25.5;     // [10:0.5:80]"} {
		if !strings.Contains(out, want) {
			t.Errorf("falta %q en:\n%s", want, out)
		}
	}
	// Solo cambió lo pedido: al deshacer los 4 valores, el archivo es el original.
	back, _, _, err := SetParameters(out, map[string]any{"ancho": 60.0, "tapa": "snap", "con_logo": true, "tam": []any{10.0, -20.0, 30.5}})
	if err != nil || back != paramsSample {
		t.Errorf("volver al original: %v\n%s", err, back)
	}
	// Sin cambios reales: el mismo código y ningún cambio.
	same, ch, _, err := SetParameters(paramsSample, map[string]any{"ancho": 60})
	if err != nil || same != paramsSample || len(ch) != 0 {
		t.Errorf("valor igual: %v %v", ch, err)
	}
}

func TestSetParametersWarnsAndRejects(t *testing.T) {
	_, _, warns, err := SetParameters(paramsSample, map[string]any{"ancho": 500.0, "tapa": "otra"})
	if err != nil || len(warns) != 2 {
		t.Fatalf("fuera de rango y de opciones se avisan, no se rechazan: %v %v", warns, err)
	}
	bad := []map[string]any{
		{"no_existe": 1},
		{"eps": 0.5},         // Hidden
		{"derivado": 1},      // expresión
		{"tarde": 1},         // después de un module
		{"$fn": 64},          // variable especial
		{"ancho": "mucho"},   // texto en un número
		{"ancho": true},      // bool en un número
		{"tapa": 3},          // número en un texto
		{"con_logo": "tal"},  // texto que no es bool
		{"tam": []any{1, 2}}, // largo equivocado
		{"tam": 5},
		{"tapa": "a\x01b"},
		{},
	}
	for _, v := range bad {
		if out, ch, _, err := SetParameters(paramsSample, v); err == nil || out != "" || ch != nil {
			t.Errorf("%v debía fallar sin cambiar nada", v)
		}
	}
	// El texto con comillas y barras se escapa.
	out, _, _, err := SetParameters(paramsSample, map[string]any{"tapa": `a"b\c`})
	if err != nil || !strings.Contains(out, `tapa = "a\"b\\c";`) {
		t.Errorf("escape: %v\n%s", err, out)
	}
	if _, err := Parameters("x = \"sin cerrar;"); err == nil {
		t.Error("un string sin cerrar no se puede leer")
	}
}

// Las plantillas que se entregan declaran sus parámetros con el estilo del
// Customizer: todas tienen que leerse, y cambiar un número tiene que dejar un
// código que sigue leyéndose igual salvo ese valor.
func TestParametersOfEveryTemplate(t *testing.T) {
	const dir = "skills/openscad-templates/templates"
	ents, err := fs.ReadDir(assets.FS, dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range ents {
		if !strings.HasSuffix(e.Name(), ".scad") {
			continue
		}
		raw, err := fs.ReadFile(assets.FS, path.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		ps, err := Parameters(string(raw))
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		visible, first := 0, Param{}
		for _, p := range ps {
			if !p.Hidden {
				if visible == 0 {
					first = p
				}
				visible++
			}
		}
		if visible < 3 {
			t.Errorf("%s: solo %d parámetros visibles", e.Name(), visible)
			continue
		}
		n++
		// Reescribir el primero con su propio valor no cambia el archivo.
		out, ch, _, err := SetParameters(string(raw), map[string]any{first.Name: first.Value})
		if err != nil || out != string(raw) || len(ch) != 0 {
			t.Errorf("%s: reescribir %s con su valor cambió algo (%v, %v)", e.Name(), first.Name, ch, err)
		}
	}
	if n < 8 {
		t.Errorf("solo %d plantillas con parámetros", n)
	}
}
