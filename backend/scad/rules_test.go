package scad

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/i18n"
)

const okHeader = "// @checkpoint parent= desc=\"base\"\n"

// own corre solo las reglas propias (sin OpenSCAD) y devuelve los
// diagnósticos con ese código.
func own(t *testing.T, src string, o LintOptions, code string) []Diagnostic {
	t.Helper()
	o.OpenSCADPath = ""
	var out []Diagnostic
	for _, d := range Lint(context.Background(), src, o) {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

func lines(ds []Diagnostic) []int {
	out := []int{}
	for _, d := range ds {
		out = append(out, d.Line)
	}
	return out
}

func expectLines(t *testing.T, name string, got []Diagnostic, want ...int) {
	t.Helper()
	gl := lines(got)
	if len(gl) != len(want) {
		t.Errorf("%s: líneas %v, quiero %v (%+v)", name, gl, want, got)
		return
	}
	for i := range want {
		if gl[i] != want[i] {
			t.Errorf("%s: líneas %v, quiero %v", name, gl, want)
			return
		}
	}
}

func TestSL001Header(t *testing.T) {
	o := LintOptions{FileName: "002-x.scad", ExistingFiles: []string{"001-a.scad", "002-x.scad"}}
	cases := []struct {
		src  string
		want int
	}{
		{"color(\"red\") cube(1);\n", 1},
		{"// @checkpoint parent=001-a.scad desc=\"sin cerrar\ncube(1);", 1},
		{"// @checkpoint desc=\"sin parent\"\n", 1},
		{"//@checkpoint parent=001-a.scad\n", 1},
		{"// @checkpoint parent= desc=\"primero\"\n", 0},
		{"// @checkpoint parent=001-a.scad desc=\"con \\\"comillas\\\"\"\n", 0},
		{"\uFEFF// @checkpoint parent=001-a.scad desc=\"bom y crlf\"\r\ncube(1);\r\n", 0},
	}
	for _, c := range cases {
		if got := own(t, c.src, o, CodeHeader); len(got) != c.want {
			t.Errorf("%q: %d SL001, quiero %d", c.src, len(got), c.want)
		}
	}
	// Sin nombre de checkpoint no se exige encabezado.
	if got := own(t, "cube(1);", LintOptions{}, CodeHeader); len(got) != 0 {
		t.Errorf("sin FileName: %+v", got)
	}
}

func TestSL002Parent(t *testing.T) {
	o := LintOptions{FileName: "002-x.scad", ExistingFiles: []string{"001-a.scad", "002-x.scad"}}
	expectLines(t, "existe", own(t, "// @checkpoint parent=001-a.scad desc=\"x\"\n", o, CodeParentMissing))
	expectLines(t, "no existe", own(t, "// @checkpoint parent=009-z.scad desc=\"x\"\n", o, CodeParentMissing), 1)
	expectLines(t, "a sí mismo", own(t, "// @checkpoint parent=002-x.scad desc=\"x\"\n", o, CodeParentMissing), 1)
	expectLines(t, "parent vacío", own(t, okHeader, o, CodeParentMissing))
	// Sin lista, se mira el disco.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "001-a.scad"), []byte(okHeader), 0o644); err != nil {
		t.Fatal(err)
	}
	disk := LintOptions{FileName: "002-x.scad", WorkspaceDir: dir}
	expectLines(t, "disco existe", own(t, "// @checkpoint parent=001-a.scad desc=\"x\"\n", disk, CodeParentMissing))
	expectLines(t, "disco no existe", own(t, "// @checkpoint parent=003-b.scad desc=\"x\"\n", disk, CodeParentMissing), 1)
}

// El padre que está en la papelera se dice distinto: no se perdió, se puede
// restaurar.
func TestSL002ParentInTrash(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, ".scadlab", "trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trash, "20261015-100000-001-prueba.scad"), []byte(okHeader), 0o644); err != nil {
		t.Fatal(err)
	}
	o := LintOptions{FileName: "002-x.scad", WorkspaceDir: dir}
	got := own(t, "// @checkpoint parent=001-prueba.scad desc=\"x\"\n", o, CodeParentMissing)
	expectLines(t, "en la papelera", got, 1)
	if !strings.Contains(got[0].Message, "papelera") && !strings.Contains(got[0].Message, "trash") {
		t.Errorf("el mensaje debía decir que está en la papelera: %q", got[0].Message)
	}
	// Otro nombre que no está en ningún lado: el mensaje de siempre.
	got = own(t, "// @checkpoint parent=009-z.scad desc=\"x\"\n", o, CodeParentMissing)
	expectLines(t, "ausente", got, 1)
	if strings.Contains(got[0].Message, "papelera") || strings.Contains(got[0].Message, "trash") {
		t.Errorf("no está en la papelera: %q", got[0].Message)
	}
}

func TestSL003Color(t *testing.T) {
	positives := []struct {
		name, src string
		lines     []int
	}{
		{"primitiva suelta", "cube(10);", []int{1}},
		{"transformada", "translate([1, 0, 0])\n    cube(1);", []int{2}},
		{"if sin color", "if (a) cube(1);", []int{1}},
		{"else sin color", "if (a) color(\"red\") cube();\nelse sphere();", []int{2}},
		{"for sin color", "for (i = [0 : 2]) translate([i, 0, 0]) cube(1);", []int{1}},
		{"difference sin color", "difference() { cube(10); sphere(6); }", []int{1}},
		{"módulo sin color", "module m() cube(1);\nm();", []int{2}},
		{"bloque con uno sin color", "{ color(\"red\") cube(1); sphere(1); }", []int{1}},
		{"debug # sigue saliendo", "#cube(1);", []int{1}},
		{"módulo de librería", "use <x.scad>\nthing();", []int{2}},
		{"importado", "import(\"a.stl\");", []int{1}},
	}
	for _, c := range positives {
		got := own(t, c.src, LintOptions{}, CodeNoColor)
		expectLines(t, c.name, got, c.lines...)
	}
	negatives := map[string]string{
		"color directo":               "color(\"red\") cube(10);",
		"color afuera":                "color(\"red\") translate([1, 0, 0]) cube(1);",
		"color adentro":               "translate([1, 0, 0]) color(\"red\") cube(1);",
		"color aplicado a un módulo":  "module m() cube(1);\ncolor(\"red\") m();",
		"módulo que colorea":          "module m() color(\"red\") cube(1);\nm();",
		"módulo que colorea por otro": "module a() { color(\"red\") cube(1); }\nmodule b() a();\nb();",
		"for con color adentro":       "for (i = [0 : 2]) color(c[i]) translate([i, 0, 0]) cube(1);",
		"for con bloque coloreado":    "for (i = [0 : 2]) { color(\"red\") cube(i); }",
		"if en el nivel superior":     "if (PART == \"all\") color(\"white\") base();\nelse color(\"red\") decor();",
		"difference con color":        "difference() { color(\"red\") cube(10); sphere(6); }",
		"desactivado":                 "*cube(1);",
		"de fondo":                    "%cube(1);",
		"echo y assert":               "echo(\"hola\");\nassert(true, \"x\");\nx = 3;",
		"definiciones":                "module m() { cube(1); }\nfunction f(x) = x;\ninclude <a.scad>",
		"comentarios y strings":       "// cube(10);\n/* sphere(1); */\ns = \"cube(1);\";",
		"transformación vacía":        "translate([1, 0, 0]);",
		"let transparente":            "let (a = 1) color(\"red\") cube(a);",
	}
	for name, src := range negatives {
		if got := own(t, src, LintOptions{}, CodeNoColor); len(got) != 0 {
			t.Errorf("%s: falso positivo %+v", name, got)
		}
	}
	// La marca va sobre la cabeza del objeto, con su rango.
	d := own(t, "  sphere(1);", LintOptions{}, CodeNoColor)
	if len(d) != 1 || d[0].Col != 3 || d[0].EndCol != 9 || d[0].Severity != SeverityWarning || d[0].Source != SourceScadLab {
		t.Errorf("rango: %+v", d)
	}
}

func TestSL004SL005Includes(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"parts.scad", "lib/libpart.scad", "002-base.scad", "lib/sub/003-x.scad"} {
		p := filepath.Join(dir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("module p() cube(1);\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	o := LintOptions{WorkspaceDir: dir}
	src := strings.Join([]string{
		"include <parts.scad>",
		"use <libpart.scad>",
		"include <nope-zz9.scad>",
		"// include <nope-zz9.scad>",
		"s = \"use <nope-zz9.scad>\";",
		"include <002-base.scad>",
		"use <sub/003-x.scad>",
		"use <nope-zz9/sub.scad>",
	}, "\n")
	missing := own(t, src, o, CodeIncludeMissing)
	expectLines(t, "SL004", missing, 3, 8)
	if len(missing) > 0 && (missing[0].Severity != SeverityError || missing[0].Col != 9) {
		t.Errorf("SL004: %+v", missing[0])
	}
	expectLines(t, "SL005", own(t, src, o, CodeIncludesCkpt), 6, 7)
}

func TestSL006FnTooHigh(t *testing.T) {
	expectLines(t, "literal", own(t, "$fn = 300;", LintOptions{}, CodeFnTooHigh), 1)
	expectLines(t, "ternario", own(t, "x = 1;\n$fn = $preview ? 32 : 512;", LintOptions{}, CodeFnTooHigh), 2)
	expectLines(t, "razonable", own(t, "$fn = 64;", LintOptions{}, CodeFnTooHigh))
	expectLines(t, "en un módulo", own(t, "module m() { $fn = 500; sphere(1); }", LintOptions{}, CodeFnTooHigh))
	expectLines(t, "argumento", own(t, "sphere(1, $fn = 300);", LintOptions{}, CodeFnTooHigh))
	if d := own(t, "$fn = 300;", LintOptions{}, CodeFnTooHigh); len(d) == 1 && d[0].Severity != SeverityInfo {
		t.Errorf("severidad: %+v", d[0])
	}
}

func TestSL007NestedMinkowski(t *testing.T) {
	expectLines(t, "minkowski en minkowski",
		own(t, "minkowski() {\n    minkowski() { cube(1); sphere(1); }\n    sphere(1);\n}", LintOptions{}, CodeNestedMinkowski), 2)
	expectLines(t, "minkowski en hull", own(t, "hull()\n    minkowski() { cube(1); sphere(1); }", LintOptions{}, CodeNestedMinkowski), 2)
	expectLines(t, "profundo", own(t, "color(\"r\") minkowski() { translate([1, 0, 0]) { if (a) minkowski() { cube(); sphere(); } } sphere(); }", LintOptions{}, CodeNestedMinkowski), 1)
	expectLines(t, "solo", own(t, "minkowski() { cube(1); sphere(1); }", LintOptions{}, CodeNestedMinkowski))
	expectLines(t, "hull adentro", own(t, "minkowski() { hull() { cube(1); sphere(1); } sphere(1); }", LintOptions{}, CodeNestedMinkowski))
	expectLines(t, "módulo aparte", own(t, "module m() minkowski() { cube(); sphere(); }\nhull() m();", LintOptions{}, CodeNestedMinkowski))
	expectLines(t, "hermanos", own(t, "minkowski() { cube(); sphere(); }\nminkowski() { cube(); sphere(); }", LintOptions{}, CodeNestedMinkowski))
}

func TestSL008DuplicateAssign(t *testing.T) {
	expectLines(t, "nivel superior", own(t, "x = 1;\ny = 2;\nx = 3;", LintOptions{}, CodeDuplicateAssign), 3)
	expectLines(t, "en un módulo", own(t, "module m() {\n    y = 1;\n    y = 2;\n}", LintOptions{}, CodeDuplicateAssign), 3)
	expectLines(t, "módulo no es nivel superior", own(t, "x = 1;\nmodule m() { x = 2; }", LintOptions{}, CodeDuplicateAssign))
	expectLines(t, "function y let", own(t, "x = 1;\nfunction f(x) = let(x = 2) x;\nlet (x = 3) cube(x);", LintOptions{}, CodeDuplicateAssign))
	expectLines(t, "bloque de if", own(t, "x = 1;\nif (a) { x = 2; cube(x); }", LintOptions{}, CodeDuplicateAssign))
	expectLines(t, "dos módulos", own(t, "module a() { z = 1; }\nmodule b() { z = 2; }", LintOptions{}, CodeDuplicateAssign))
	expectLines(t, "comentado", own(t, "x = 1;\n// x = 2;\ns = \"x = 3;\";", LintOptions{}, CodeDuplicateAssign))
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	if d := own(t, "x = 1;\nx = 2;", LintOptions{}, CodeDuplicateAssign); len(d) != 1 || !strings.Contains(d[0].Message, "línea 1") {
		t.Errorf("mensaje: %+v", d)
	}
}

func TestSL009CustomizerRange(t *testing.T) {
	src := strings.Join([]string{
		"antes = 4;",
		"/* [Medidas] */",
		"w = 10;     // [1:100]",
		"h = 5;",
		"d = -5;     // [-10:10]",
		"name = \"x\";",
		"flag = true;",
		"$fn = 32;",
		"calc = w * 2;",
		"opt = 20;   // [14.5, 20, 25]",
		"nota = 3;   // sin rango",
		"/* [Hidden] */",
		"eps = 0.01;",
		"/* [Otra] */",
		"z = 7;",
		"module m() {}",
		"despues = 3;",
	}, "\n")
	expectLines(t, "SL009", own(t, src, LintOptions{}, CodeCustomizerRange), 1, 4, 11, 15)
	expectLines(t, "sin secciones", own(t, "w = 10;\nh = 5;", LintOptions{}, CodeCustomizerRange))
}

// Las 10 plantillas no disparan ninguna regla propia: son el ejemplo de cómo
// se escribe un checkpoint.
func TestTemplatesAreClean(t *testing.T) {
	for _, f := range templateFiles(t) {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		ds := Lint(context.Background(), string(raw), LintOptions{FileName: "001-x.scad", WorkspaceDir: t.TempDir()})
		if len(ds) != 0 {
			t.Errorf("%s: %+v", filepath.Base(f), ds)
		}
	}
}

func TestLintLexErrorKeepsHeaderRules(t *testing.T) {
	ds := Lint(context.Background(), "cube(1);\ns = \"sin cerrar;\n", LintOptions{FileName: "001-x.scad"})
	var codes []string
	for _, d := range ds {
		codes = append(codes, d.Code+"@"+d.Source)
	}
	if strings.Join(codes, ",") != "SL001@scadlab,syntax@scadlab" {
		t.Errorf("diagnósticos: %v", codes)
	}
}
