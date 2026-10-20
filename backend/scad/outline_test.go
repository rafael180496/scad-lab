package scad

import (
	"errors"
	"strings"
	"testing"
)

const outlineSrc = `// @checkpoint parent= desc="maceta"
include <lib/base.scad>
use <lib/otros.scad>

/* [Maceta] */
// Diámetro superior
d_top = 80; // [40:1:200]
altura = 90;
nombre = "Rosa";

/* [Colores] */
color_body = "green";
color_txt = "orange";

/* [Hidden] */
eps = 0.01;
$fn = 64;

function radio(z) = d_top / 2 - z * 0.1;

module cuerpo(h = 10, r = 5) {
    difference() {
        cylinder(h = h, r = r);
        translate([0, 0, 1]) cylinder(h = h, r = r - 1);
    }
}

// el relieve
module texto(
    t = "hola",
    s = 5
) {
    linear_extrude(1) text(t, size = s);
}

color(color_body) cuerpo();
color("#ff8800", 0.5) texto();
if (altura > 10) {
    color([0.1, 0.2, 0.3]) cube(1);
}
`

func TestBuildOutline(t *testing.T) {
	o, err := BuildOutline(outlineSrc)
	if err != nil {
		t.Fatal(err)
	}
	if o.Lines != strings.Count(outlineSrc, "\n") || o.Bytes != len(outlineSrc) {
		t.Errorf("tamaño: %d líneas, %d bytes", o.Lines, o.Bytes)
	}

	// Parámetros por sección, con su línea y su literal.
	if len(o.Params) != 3 {
		t.Fatalf("secciones: %+v", o.Params)
	}
	if s := o.Params[0]; s.Section != "Maceta" || len(s.Items) != 3 || s.Items[0] != (OutlineItem{Name: "d_top", Line: 7, Value: "80"}) || s.Items[2].Value != `"Rosa"` {
		t.Errorf("Maceta: %+v", s)
	}
	if s := o.Params[1]; s.Section != "Colores" || len(s.Items) != 2 || s.Items[0].Name != "color_body" || s.Items[0].Line != 12 {
		t.Errorf("Colores: %+v", s)
	}
	if s := o.Params[2]; s.Section != "Hidden" || len(s.Items) != 1 || s.Items[0].Name != "eps" {
		t.Errorf("Hidden ($fn no cuenta): %+v", s)
	}

	// Módulos y funciones con inicio, fin y argumentos.
	if len(o.Modules) != 2 {
		t.Fatalf("módulos: %+v", o.Modules)
	}
	if m := o.Modules[0]; m.Name != "cuerpo" || m.Line != 21 || m.End != 26 || m.Args != "h = 10, r = 5" {
		t.Errorf("cuerpo: %+v", m)
	}
	if m := o.Modules[1]; m.Name != "texto" || m.Line != 29 || m.End != 34 || m.Args != `t = "hola", s = 5` {
		t.Errorf("texto: %+v", m)
	}
	if len(o.Functions) != 1 || o.Functions[0] != (OutlineDef{Name: "radio", Line: 19, End: 19, Args: "z"}) {
		t.Errorf("funciones: %+v", o.Functions)
	}

	// include / use.
	if len(o.Includes) != 2 || o.Includes[0] != (OutlineInclude{Kind: "include", Path: "lib/base.scad", Line: 2}) || o.Includes[1] != (OutlineInclude{Kind: "use", Path: "lib/otros.scad", Line: 3}) {
		t.Errorf("includes: %+v", o.Includes)
	}

	// Partes: color() fuera de los módulos, también dentro de un if.
	if len(o.Parts) != 3 {
		t.Fatalf("partes: %+v", o.Parts)
	}
	if p := o.Parts[0]; p.Color != "color_body" || p.Value != "green" || p.Line != 36 {
		t.Errorf("parte 1: %+v", p)
	}
	if p := o.Parts[1]; p.Color != "#ff8800" || p.Value != "" || p.Line != 37 {
		t.Errorf("parte 2 (el alfa no se mezcla): %+v", p)
	}
	if p := o.Parts[2]; p.Color != "[0.1, 0.2, 0.3]" || p.Line != 39 {
		t.Errorf("parte 3: %+v", p)
	}
}

func TestOutlineEmptyAndPlain(t *testing.T) {
	o, err := BuildOutline("")
	if err != nil || o.Lines != 0 || o.Params == nil || o.Modules == nil || o.Functions == nil || o.Includes == nil || o.Parts == nil {
		t.Errorf("vacío: %+v %v", o, err)
	}
	o, err = BuildOutline("cube(1);\n")
	if err != nil || o.Lines != 1 || len(o.Parts) != 0 || len(o.Modules) != 0 {
		t.Errorf("simple: %+v %v", o, err)
	}
}

func TestOutlineUnterminatedIsLexError(t *testing.T) {
	_, err := BuildOutline("a = 1;\nb = \"sin cerrar;\n")
	var le *LexError
	if err == nil || !errors.As(err, &le) || le.Line != 2 {
		t.Errorf("error: %v", err)
	}
	if _, err := BuildOutline("/* sin cerrar\ncube(1);"); err == nil {
		t.Error("comentario sin cerrar")
	}
}

func TestOutlineCRLFBOMAndUnicode(t *testing.T) {
	src := "\uFEFFancho = 5; // [1:10]\r\n\r\nmodule nandu(x = \"ü\") {\r\n  cube(x);\r\n}\r\ncolor(\"red\") nandu();"
	o, err := BuildOutline(src)
	if err != nil {
		t.Fatal(err)
	}
	if o.Lines != 6 {
		t.Errorf("líneas: %d", o.Lines)
	}
	if len(o.Modules) != 1 || o.Modules[0].Line != 3 || o.Modules[0].End != 5 || o.Modules[0].Args != `x = "ü"` {
		t.Errorf("módulos: %+v", o.Modules)
	}
	if len(o.Params) != 1 || o.Params[0].Items[0].Line != 1 {
		t.Errorf("params: %+v", o.Params)
	}
	if len(o.Parts) != 1 || o.Parts[0].Line != 6 || o.Parts[0].Color != "red" {
		t.Errorf("partes: %+v", o.Parts)
	}
}

// Módulos de una línea, anidados y sin cerrar no rompen el esquema.
func TestOutlineTolerantOfBrokenCode(t *testing.T) {
	src := "module a() cube(1);\nmodule b() {\n  cube(2);\n"
	o, err := BuildOutline(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Modules) != 2 || o.Modules[0].Line != 1 || o.Modules[0].End != 1 || o.Modules[1].Line != 2 {
		t.Errorf("módulos: %+v", o.Modules)
	}
}

func TestListDefsDependencies(t *testing.T) {
	src := `ancho = 10;
alto = 20;
module a(x = ancho, y) {
    r = x * 2;
    translate([x, y, alto]) cylinder(h = r, r = ancho);
    b(x);
    $fn = 32;
}
module b(z) { cube(z); }
function f(q) = q * ancho + g(q);
function g(q) = q;
`
	fd, err := ListDefs(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(fd.Globals) != 2 || fd.Globals[0] != "ancho" || fd.Globals[1] != "alto" {
		t.Errorf("globales: %v", fd.Globals)
	}
	byName := map[string]Def{}
	for _, d := range fd.Defs {
		byName[d.Name] = d
	}
	a := byName["a"]
	if a.Kind != "module" || a.Line != 3 || a.End != 8 || strings.Join(a.Params, ",") != "x,y" {
		t.Errorf("a: %+v", a)
	}
	// `x` es parámetro; `r`, `h` son locales o argumentos con nombre; `ancho` y `alto` son globales.
	if strings.Join(a.Vars, ",") != "ancho,alto" {
		t.Errorf("a.Vars: %v", a.Vars)
	}
	if strings.Join(a.Calls, ",") != "translate,cylinder,b" {
		t.Errorf("a.Calls: %v", a.Calls)
	}
	f := byName["f"]
	if f.Kind != "function" || f.Line != 10 || strings.Join(f.Vars, ",") != "ancho" || strings.Join(f.Calls, ",") != "g" {
		t.Errorf("f: %+v", f)
	}
	if b := byName["b"]; len(b.Vars) != 0 || len(b.Params) != 1 {
		t.Errorf("b: %+v", b)
	}
	if _, err := ListDefs("module a( {"); err != nil {
		t.Errorf("código roto no es un error léxico: %v", err)
	}
	if _, err := ListDefs("a = \"sin cerrar"); err == nil {
		t.Error("string sin cerrar")
	}
}

func TestListDefsOffsets(t *testing.T) {
	src := "x = 1;\n  module a() { cube(1); } // fin\nfunction f(q) = q;\n"
	fd, err := ListDefs(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(fd.Defs) != 2 {
		t.Fatalf("defs: %+v", fd.Defs)
	}
	a := fd.Defs[0]
	if got := src[a.Off:a.EndOff]; got != "module a() { cube(1); }" {
		t.Errorf("módulo: %q", got)
	}
	f := fd.Defs[1]
	if got := src[f.Off:f.EndOff]; got != "function f(q) = q;" {
		t.Errorf("función: %q", got)
	}
}

// `cylinder(r = r)` es la forma más común de usar una variable global dentro de un
// módulo: el nombre del argumento no es una variable propia, el valor sí es un uso.
func TestListDefsNamedArgumentsAreNotLocals(t *testing.T) {
	src := `r = 10;
h = 5;
module m() {
    cylinder(r = r, h = 2);
    for (i = [0:2]) translate([i, 0, 0]) cube(h);
    let (k = 3) cube(k);
}
`
	fd, err := ListDefs(src)
	if err != nil {
		t.Fatal(err)
	}
	m := fd.Defs[0]
	if strings.Join(m.Vars, ",") != "r,h" {
		t.Errorf("Vars = %v, esperaba r y h (globales usadas); i y k son propias", m.Vars)
	}
}
