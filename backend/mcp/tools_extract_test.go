package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/checkpoint"
	"scadlab/backend/textedit"
)

const modular = `a = 20;
b = 10;

// Una pieza simple.
module base() {
    cube([a, b, 5]);
}

module tapa(h = 2) {
    translate([0, 0, 5]) cube([a, b, h]);
}

function doble(x) = x * 2;

color("red") base();
color("blue") tapa();
`

func libText(t *testing.T, ws, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ws, "lib", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Mueve módulos a lib/, deja el include y el modelo es el mismo.
func TestExtractModuleMovesCodeAndVerifies(t *testing.T) {
	needOpenSCAD(t)
	tb, ws := newToolbox(t, false)
	name := tb.create(t, "modular", modular, nil)
	before, _ := os.ReadFile(filepath.Join(ws, name))
	sizeBefore := tb.mustJSON(t, "render", map[string]any{"file": name})["mesh"].(map[string]any)["sizeMm"]

	got := tb.mustJSON(t, "extract_module", map[string]any{"file": name, "modules": []any{"base", "tapa"}, "lib": "piezas"})
	if got["file"] != name || got["updated"] != true || got["lib"] != "lib/piezas.scad" || got["libCreated"] != true || got["mode"] != "include" {
		t.Fatalf("resultado: %v", got)
	}
	mv := got["moved"].([]any)
	if len(mv) != 2 || mv[0].(map[string]any)["name"] != "base" || mv[0].(map[string]any)["kind"] != "module" || mv[0].(map[string]any)["was"] != "5-8" {
		t.Errorf("moved: %v", mv)
	}
	if v, _ := got["verified"].(string); !strings.Contains(v, "identical") {
		t.Errorf("verified: %v", got["verified"])
	}
	// El checkpoint: sin los módulos, con el include en el lugar del primero y la cabecera intacta.
	main := string(mustRead(t, filepath.Join(ws, name)))
	wantMain := strings.Join([]string{
		strings.SplitN(string(before), "\n", 2)[0],
		"a = 20;", "b = 10;", "", "include <lib/piezas.scad>", "function doble(x) = x * 2;", "", `color("red") base();`, `color("blue") tapa();`, "",
	}, "\n")
	if main != wantMain {
		t.Errorf("checkpoint:\n%s\nquería:\n%s", main, wantMain)
	}
	lib := libText(t, ws, "piezas.scad")
	if !strings.Contains(lib, "// lib/piezas.scad") || !strings.Contains(lib, "// Una pieza simple.\nmodule base() {\n    cube([a, b, 5]);\n}\n\nmodule tapa(h = 2) {") || strings.Contains(lib, "function doble") {
		t.Errorf("lib:\n%s", lib)
	}
	// El modelo es el mismo y el checkpoint quedó con su respaldo.
	if size := tb.mustJSON(t, "render", map[string]any{"file": name})["mesh"].(map[string]any)["sizeMm"]; mustText(t, size) != mustText(t, sizeBefore) {
		t.Errorf("el modelo cambió: %v -> %v", sizeBefore, size)
	}
	hist, _ := checkpoint.History(ws, name)
	if len(hist) != 1 || hist[0].Tool != "extract_module" {
		t.Fatalf("historial: %+v", hist)
	}
	if v, _ := checkpoint.ReadVersion(ws, name, hist[0].ID); v != string(before) {
		t.Error("el respaldo no es lo que había")
	}
	// El outline del checkpoint ahora es corto y el de la librería lista lo movido.
	out := tb.must(t, "outline_checkpoint", map[string]any{"file": name}).Text
	if !strings.Contains(out, `"includes":[{"kind":"include","path":"lib/piezas.scad","line":5}]`) || strings.Contains(out, `"name":"base"`) {
		t.Errorf("outline del checkpoint: %s", out)
	}
	if out := tb.must(t, "outline_checkpoint", map[string]any{"file": "lib/piezas.scad"}).Text; !strings.Contains(out, `"name":"base"`) || !strings.Contains(out, `"name":"tapa"`) {
		t.Errorf("outline de la lib: %s", out)
	}

	// Una segunda extracción AGREGA a la misma lib (con respaldo) y no repite el include.
	got = tb.mustJSON(t, "extract_module", map[string]any{"file": name, "modules": "doble", "lib": "lib/piezas.scad"})
	if got["libCreated"] != false {
		t.Fatalf("segunda: %v", got)
	}
	main2 := string(mustRead(t, filepath.Join(ws, name)))
	if strings.Count(main2, "include <lib/piezas.scad>") != 1 || strings.Contains(main2, "function doble") {
		t.Errorf("checkpoint 2:\n%s", main2)
	}
	lib2 := libText(t, ws, "piezas.scad")
	if !strings.HasPrefix(lib2, lib) || !strings.HasSuffix(lib2, "function doble(x) = x * 2;\n") {
		t.Errorf("lib 2:\n%s", lib2)
	}
	lh, _ := checkpoint.History(ws, "lib/piezas.scad")
	if len(lh) != 1 || lh[0].Tool != "extract_module" {
		t.Fatalf("historial de la lib: %+v", lh)
	}
	if v, _ := checkpoint.ReadVersion(ws, "lib/piezas.scad", lh[0].ID); v != lib {
		t.Error("el respaldo de la lib no es lo que había")
	}
}

// Con `use`, lo que depende de variables globales o de módulos que se quedan se
// rechaza antes de tocar nada.
func TestExtractModuleUseRejectsDependencies(t *testing.T) {
	tb, ws := newToolbox(t, false)
	name := tb.create(t, "modular", modular, nil)
	before := mustRead(t, filepath.Join(ws, name))

	out := tb.fails(t, "extract_module", map[string]any{"file": name, "modules": []any{"base"}, "lib": "piezas", "mode": "use"})
	if !strings.Contains(out.Text, "“base” would not see a, b") || !strings.Contains(out.Text, "Nothing was touched") {
		t.Errorf("mensaje: %q", out.Text)
	}
	if string(mustRead(t, filepath.Join(ws, name))) != string(before) {
		t.Error("tocó el checkpoint")
	}
	if libs, _ := checkpoint.ListLibFiles(ws); len(libs) != 0 {
		t.Errorf("creó una lib: %v", libs)
	}
	// Un módulo que llama a otro que se queda tampoco.
	n2 := tb.create(t, "llama", "module a() { cube(1); }\nmodule b() { a(); }\ncolor(\"red\") b();\n", nil)
	if out := tb.fails(t, "extract_module", map[string]any{"file": n2, "modules": []any{"b"}, "lib": "x", "mode": "use"}); !strings.Contains(out.Text, "“b” would not see a") {
		t.Errorf("llamada: %q", out.Text)
	}
	// Autocontenido sí: con use queda `use <…>`.
	needOpenSCAD(t)
	n3 := tb.create(t, "propio", "module solo(w = 3) { cube(w); }\ncolor(\"red\") solo(7);\n", nil)
	got := tb.mustJSON(t, "extract_module", map[string]any{"file": n3, "modules": []any{"solo"}, "lib": "solos", "mode": "use"})
	if got["mode"] != "use" || !strings.Contains(string(mustRead(t, filepath.Join(ws, n3))), "use <lib/solos.scad>") {
		t.Errorf("use: %v", got)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestExtractModuleErrors(t *testing.T) {
	tb, ws := newToolbox(t, false)
	name := tb.create(t, "modular", modular, nil)
	before := mustRead(t, filepath.Join(ws, name))

	for label, tc := range map[string]struct {
		args map[string]any
		want string
	}{
		"no existe":        {map[string]any{"file": name, "modules": []any{"nada"}, "lib": "x"}, "I cannot find nada"},
		"lista lo que hay": {map[string]any{"file": name, "modules": []any{"nada"}, "lib": "x"}, "base, tapa, doble"},
		"sin modules":      {map[string]any{"file": name, "lib": "x"}, "`modules` is a list"},
		"modules vacío":    {map[string]any{"file": name, "modules": []any{}, "lib": "x"}, "`modules` is a list"},
		"sin lib":          {map[string]any{"file": name, "modules": []any{"base"}}, "invalid library name"},
		"lib hostil":       {map[string]any{"file": name, "modules": []any{"base"}, "lib": "../x"}, "invalid library name"},
		"lib subcarpeta":   {map[string]any{"file": name, "modules": []any{"base"}, "lib": "lib/BOSL2/std"}, "invalid library name"},
		"lib absoluta":     {map[string]any{"file": name, "modules": []any{"base"}, "lib": "/etc/x"}, "invalid library name"},
		"mode raro":        {map[string]any{"file": name, "modules": []any{"base"}, "lib": "x", "mode": "import"}, "`mode` is"},
		"file lib":         {map[string]any{"file": "lib/x.scad", "modules": []any{"base"}, "lib": "y"}, "was not found"},
		"sin file":         {map[string]any{"modules": []any{"base"}, "lib": "x"}, "`file` is required"},
		"archivo ajeno":    {map[string]any{"file": "../" + name, "modules": []any{"base"}, "lib": "x"}, "not a valid name"},
	} {
		out := tb.fails(t, "extract_module", tc.args)
		if !strings.Contains(out.Text, tc.want) {
			t.Errorf("%s: %q no contiene %q", label, out.Text, tc.want)
		}
	}
	if string(mustRead(t, filepath.Join(ws, name))) != string(before) {
		t.Error("un rechazo tocó el checkpoint")
	}
	if libs, _ := checkpoint.ListLibFiles(ws); len(libs) != 0 {
		t.Errorf("un rechazo creó una lib: %v", libs)
	}
	// Un nombre ya definido en la lib.
	writeLib(t, ws, "ya.scad", "module base() { cube(1); }\n")
	if out := tb.fails(t, "extract_module", map[string]any{"file": name, "modules": []any{"base"}, "lib": "ya"}); !strings.Contains(out.Text, "lib/ya.scad already defines “base”") {
		t.Errorf("choque: %q", out.Text)
	}
	if libText(t, ws, "ya.scad") != "module base() { cube(1); }\n" {
		t.Error("la lib existente cambió")
	}
	// Código en la misma línea que otra cosa: se pide separarlo.
	n2 := tb.create(t, "pegado", "x = 1; module a() { cube(x); }\ncolor(\"red\") a();\n", nil)
	if out := tb.fails(t, "extract_module", map[string]any{"file": n2, "modules": []any{"a"}, "lib": "z"}); !strings.Contains(out.Text, "shares a line with other code") {
		t.Errorf("misma línea: %q", out.Text)
	}
}

// Un checkpoint que no renderiza no se toca: no hay con qué comparar.
func TestExtractModuleNeedsAWorkingModel(t *testing.T) {
	needOpenSCAD(t)
	tb, ws := newToolbox(t, false)
	name := tb.create(t, "roto", "module a() { cube(1); }\ncolor(\"red\") a(\n", nil)
	before := mustRead(t, filepath.Join(ws, name))
	out := tb.fails(t, "extract_module", map[string]any{"file": name, "modules": []any{"a"}, "lib": "x"})
	if !strings.Contains(out.Text, "does not render") || !strings.Contains(out.Text, "Nothing was touched") {
		t.Errorf("mensaje: %q", out.Text)
	}
	if string(mustRead(t, filepath.Join(ws, name))) != string(before) {
		t.Error("tocó el checkpoint")
	}
	if libs, _ := checkpoint.ListLibFiles(ws); len(libs) != 0 {
		t.Errorf("creó una lib: %v", libs)
	}
}

// Si mover el código cambia el modelo, se deshace todo y se dice por qué. Un
// import() con ruta relativa es el caso real: dentro de lib/ la ruta ya no apunta
// a refs/.
func TestExtractModuleUndoesWhenTheModelChanges(t *testing.T) {
	needOpenSCAD(t)
	tb, ws := newToolbox(t, false)
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeASCIISTL(t, filepath.Join(ws, "refs", "soporte.stl"))
	src := "module soporte() { import(\"refs/soporte.stl\"); }\ncolor(\"red\") soporte();\ncolor(\"blue\") translate([0, 0, 5]) cube([4, 4, 4]);\n"
	name := tb.create(t, "con import", src, nil)
	before := mustRead(t, filepath.Join(ws, name))

	// Librería NUEVA: tras el deshacer no queda ni el archivo.
	out := tb.fails(t, "extract_module", map[string]any{"file": name, "modules": []any{"soporte"}, "lib": "nueva"})
	var got map[string]any
	if err := jsonUnmarshal(out.Text, &got); err != nil {
		t.Fatalf("no es JSON: %v\n%s", err, out.Text)
	}
	if got["undone"] != true || got["updated"] != false || got["reason"] == "" || got["before"] == nil {
		t.Errorf("resultado: %v", got)
	}
	if msg, _ := got["message"].(string); !strings.Contains(msg, "I undid the change") || !strings.Contains(msg, name) || !strings.Contains(msg, "relative to lib/") {
		t.Errorf("mensaje: %q", msg)
	}
	if string(mustRead(t, filepath.Join(ws, name))) != string(before) {
		t.Errorf("el checkpoint no volvió a lo que era:\n%s", mustRead(t, filepath.Join(ws, name)))
	}
	if libs, _ := checkpoint.ListLibFiles(ws); len(libs) != 0 {
		t.Errorf("la lib recién creada debía quitarse: %v", libs)
	}
	// El modelo sigue renderizando igual que antes.
	if r := tb.mustJSON(t, "render", map[string]any{"file": name}); r["status"] != "ok" {
		t.Errorf("render tras deshacer: %v", r)
	}

	// Librería que YA existía: vuelve a su versión anterior desde el historial.
	writeLib(t, ws, "vieja.scad", "module otro() { cube(1); }\n")
	out = tb.fails(t, "extract_module", map[string]any{"file": name, "modules": []any{"soporte"}, "lib": "vieja"})
	if !strings.Contains(out.Text, `"undone":true`) {
		t.Fatalf("segunda: %s", out.Text)
	}
	if libText(t, ws, "vieja.scad") != "module otro() { cube(1); }\n" {
		t.Errorf("la lib existente debía volver a lo que era: %q", libText(t, ws, "vieja.scad"))
	}
	if string(mustRead(t, filepath.Join(ws, name))) != string(before) {
		t.Error("el checkpoint no volvió a lo que era")
	}
	// Los intentos quedaron en los historiales (nada se perdió).
	if h, _ := checkpoint.History(ws, name); len(h) < 2 {
		t.Errorf("historial del checkpoint: %+v", h)
	}
}

// Sobre la maceta de verdad: se parte en un lib/ y el modelo no cambia.
func TestExtractModuleOnThePot(t *testing.T) {
	needOpenSCAD(t)
	tb, ws, name := potBox(t)
	before := tb.mustJSON(t, "measure", map[string]any{"file": name, "verbose": true})
	got := tb.mustJSON(t, "extract_module", map[string]any{
		"file": name, "lib": "relieve",
		"modules": []any{"cascaron_relieve", "texto_2d", "texto_plano", "relieve_texto", "marco_2d", "marco_plano", "relieve_marco", "relieve"},
	})
	if got["updated"] != true {
		t.Fatalf("resultado: %v", got)
	}
	disk := string(mustRead(t, filepath.Join(ws, name)))
	if n := textedit.CountLines(disk); n >= 520 || !strings.Contains(disk, "include <lib/relieve.scad>") || strings.Contains(disk, "module relieve()") {
		t.Errorf("checkpoint: %d líneas", n)
	}
	after := tb.mustJSON(t, "measure", map[string]any{"file": name, "verbose": true})
	if mustText(t, before["mesh"]) != mustText(t, after["mesh"]) {
		t.Errorf("la malla cambió:\n%v\n%v", before["mesh"], after["mesh"])
	}
	if len(after["parts"].([]any)) != len(before["parts"].([]any)) {
		t.Errorf("las partes cambiaron: %v", after["parts"])
	}
}
