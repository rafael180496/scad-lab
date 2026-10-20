package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/checkpoint"
	"scadlab/backend/detect"
)

// eachEngine corre fn con el OpenSCAD instalado (si está) y con el motor
// integrado (WebAssembly), cada uno con su caja y su workspace.
func eachEngine(t *testing.T, fn func(t *testing.T, tb *Toolbox, ws string)) {
	t.Helper()
	t.Run("native", func(t *testing.T) {
		needOpenSCAD(t)
		tb, ws := newToolbox(t, false)
		fn(t, tb, ws)
	})
	t.Run("wasm", func(t *testing.T) {
		t.Setenv(detect.NoSystemOpenSCADEnv, "1")
		tb, ws := newToolbox(t, false)
		if !tb.e.wasm.Available() {
			t.Skip("motor integrado no disponible")
		}
		fn(t, tb, ws)
	})
}

// meshSize renderiza el checkpoint y devuelve el tamaño de su malla.
func (tb *Toolbox) meshSize(t *testing.T, file string) (x, y, z float64) {
	t.Helper()
	r := tb.mustJSON(t, "render", map[string]any{"file": file})
	if r["status"] != "ok" {
		t.Fatalf("render de %s: %v", file, r)
	}
	s := r["mesh"].(map[string]any)["sizeMm"].(map[string]any)
	return s["x"].(float64), s["y"].(float64), s["z"].(float64)
}

const paramsModel = `// @checkpoint parent= desc="caja con parámetros"
/* [Caja] */
// Ancho en mm
width = 20; // [5:1:100]
depth = 10; // [5:1:100]
tall = 5;   // [1:1:50]
lid = "snap"; // [snap, slide]
/* [Hidden] */
eps = 0.01;
color("red") cube([width, depth, tall]);
`

func TestGetAndSetParameters(t *testing.T) {
	tb, ws := newToolbox(t, false)
	a := tb.create(t, "caja", paramsModel, nil)

	// Compacto por defecto: por sección, con rango como min:step:max.
	compact := tb.mustJSON(t, "get_parameters", map[string]any{"file": a})
	secs := compact["sections"].([]any)
	if compact["count"].(float64) != 5 || len(secs) != 2 || secs[0].(map[string]any)["section"] != "Caja" || secs[1].(map[string]any)["section"] != "Hidden" {
		t.Fatalf("get_parameters compacto: %v", compact)
	}
	w0 := secs[0].(map[string]any)["params"].([]any)[0].(map[string]any)
	if w0["name"] != "width" || w0["value"] != 20.0 || w0["range"] != "5:1:100" || w0["desc"] != "Ancho en mm" || w0["line"] != 4.0 {
		t.Errorf("width compacto: %v", w0)
	}
	lid := secs[0].(map[string]any)["params"].([]any)[3].(map[string]any)
	if lid["name"] != "lid" || lid["options"].([]any)[1] != "slide" {
		t.Errorf("lid compacto: %v", lid)
	}

	got := tb.mustJSON(t, "get_parameters", map[string]any{"file": a, "verbose": true})
	if got["count"].(float64) != 5 {
		t.Fatalf("get_parameters: %v", got)
	}
	var width map[string]any
	for _, p := range got["parameters"].([]any) {
		if m := p.(map[string]any); m["name"] == "width" {
			width = m
		}
	}
	if width["value"] != 20.0 || width["min"] != 5.0 || width["max"] != 100.0 || width["step"] != 1.0 || width["group"] != "Caja" || width["description"] != "Ancho en mm" {
		t.Errorf("width: %v", width)
	}
	// Sin archivo: el más nuevo.
	if g := tb.mustJSON(t, "get_parameters", nil); g["file"] != a {
		t.Errorf("sin file: %v", g["file"])
	}
	// Un checkpoint sin parámetros lo dice.
	plain := tb.create(t, "lisa", srcCube, nil)
	if g := tb.mustJSON(t, "get_parameters", map[string]any{"file": plain}); g["count"].(float64) != 0 || g["note"] == nil || g["sections"] != nil && len(g["sections"].([]any)) != 0 {
		t.Errorf("sin parámetros: %v", g)
	}

	// set_parameters EN EL LUGAR (S17): ningún hijo, el archivo cambia y la versión
	// anterior queda en su historial.
	filesBefore := scadCount(t, ws)
	parentBefore, _ := os.ReadFile(filepath.Join(ws, a))
	set := tb.mustJSON(t, "set_parameters", map[string]any{"file": a, "values": map[string]any{"width": 35, "lid": "slide", "tall": "8"}})
	if set["file"] != a || set["updated"] != true || set["created"] != nil || len(set["changes"].([]any)) != 3 || set["warnings"] != nil {
		t.Fatalf("set_parameters: %v", set)
	}
	if scadCount(t, ws) != filesBefore {
		t.Error("set_parameters en el lugar no crea checkpoints")
	}
	if r, _ := set["render"].(map[string]any); r["ok"] != true {
		t.Errorf("render del resultado: %v", set["render"])
	}
	src := tb.must(t, "read_checkpoint", map[string]any{"file": a}).Text
	if !strings.HasPrefix(src, `// @checkpoint parent= desc="caja"`) {
		t.Errorf("la cabecera no se toca: %q", strings.SplitN(src, "\n", 2)[0])
	}
	for _, want := range []string{"width = 35; // [5:1:100]", `lid = "slide"; // [snap, slide]`, "tall = 8;   // [1:1:50]", "depth = 10; // [5:1:100]", "eps = 0.01;", "// Ancho en mm"} {
		if !strings.Contains(src, want) {
			t.Errorf("falta %q en:\n%s", want, src)
		}
	}
	hist, err := checkpoint.History(ws, a)
	if err != nil || len(hist) != 1 || hist[0].Tool != "set_parameters" || !strings.Contains(hist[0].Note, "width 20 → 35") {
		t.Fatalf("historial: %+v %v", hist, err)
	}
	if v, _ := checkpoint.ReadVersion(ws, a, hist[0].ID); v != string(parentBefore) {
		t.Errorf("respaldo: %q", v)
	}
	// Un segundo cambio: otra versión.
	tb.mustJSON(t, "set_parameters", map[string]any{"file": a, "values": `{"depth": 50}`})
	if h, _ := checkpoint.History(ws, a); len(h) != 2 {
		t.Errorf("historial tras el segundo cambio: %+v", h)
	}
	// Fuera de rango: se aplica con aviso.
	set3 := tb.mustJSON(t, "set_parameters", map[string]any{"file": a, "values": map[string]any{"width": 500}})
	if w, _ := set3["warnings"].([]any); len(w) != 1 || !strings.Contains(w[0].(string), "outside") {
		t.Errorf("fuera de rango: %v", set3)
	}
	// Sin `file` parte del padre activo (el más nuevo si no hay): se edita ese, no se crea nada.
	tb.mustJSON(t, "set_active_parent", map[string]any{"file": a})
	before2 := scadCount(t, ws)
	if got := tb.mustJSON(t, "set_parameters", map[string]any{"values": map[string]any{"width": 21}}); got["file"] != tb.e.defaultParent() || got["updated"] != true {
		t.Errorf("sin file: %v", got)
	}
	if scadCount(t, ws) != before2 {
		t.Error("sin file tampoco crea un checkpoint")
	}

	// variant=true: el comportamiento de antes (un hijo nuevo; el original no cambia).
	aBefore, _ := os.ReadFile(filepath.Join(ws, a))
	v := tb.mustJSON(t, "set_parameters", map[string]any{"file": a, "values": map[string]any{"width": 60}, "variant": true})
	child, _ := v["file"].(string)
	if child == "" || child == a || v["created"] != true || v["updated"] != nil || v["parent"] != a || len(v["changes"].([]any)) != 1 {
		t.Fatalf("variante: %v", v)
	}
	if after, _ := os.ReadFile(filepath.Join(ws, a)); string(after) != string(aBefore) {
		t.Error("variant=true modificó el checkpoint original")
	}
	csrc := tb.must(t, "read_checkpoint", map[string]any{"file": child}).Text
	if !strings.HasPrefix(csrc, "// @checkpoint parent="+a+` desc="Parameters: `) || !strings.Contains(csrc, "width = 60;") {
		t.Errorf("hijo: %q", strings.SplitN(csrc, "\n", 2)[0])
	}
	if h, _ := checkpoint.History(ws, child); len(h) != 0 {
		t.Errorf("el hijo nace sin historial: %+v", h)
	}

	// Errores: no crean ni cambian nada.
	before := scadCount(t, ws)
	snapshot, _ := os.ReadFile(filepath.Join(ws, a))
	for _, vals := range []any{
		map[string]any{}, nil, "no es json",
		map[string]any{"nada": 1}, map[string]any{"eps": 1}, map[string]any{"width": "ancho"}, map[string]any{"lid": 3},
	} {
		args := map[string]any{"file": a}
		if vals != nil {
			args["values"] = vals
		}
		tb.fails(t, "set_parameters", args)
	}
	cur := tb.mustJSON(t, "get_parameters", map[string]any{"file": a, "verbose": true})
	var curWidth float64
	for _, p := range cur["parameters"].([]any) {
		if m := p.(map[string]any); m["name"] == "width" {
			curWidth = m["value"].(float64)
		}
	}
	tb.fails(t, "set_parameters", map[string]any{"file": a, "values": map[string]any{"width": curWidth}}) // igual al actual: nada que cambiar
	for _, f := range []string{"../" + a, "/etc/passwd", "sub/" + a, "no-existe.scad", a + "\x00"} {
		tb.fails(t, "set_parameters", map[string]any{"file": f, "values": map[string]any{"width": 1}})
		tb.fails(t, "get_parameters", map[string]any{"file": f})
	}
	if scadCount(t, ws) != before {
		t.Error("un set_parameters que falla creó un checkpoint")
	}
	if after, _ := os.ReadFile(filepath.Join(ws, a)); string(after) != string(snapshot) {
		t.Error("un set_parameters que falla modificó el archivo")
	}
}

// set_parameters renderiza con los valores nuevos, en el lugar y como variante, en los dos motores.
func TestSetParametersRendersWithNewValues(t *testing.T) {
	eachEngine(t, func(t *testing.T, tb *Toolbox, ws string) {
		a := tb.create(t, "caja", paramsModel, nil)
		if x, y, z := tb.meshSize(t, a); x != 20 || y != 10 || z != 5 {
			t.Fatalf("original: %v×%v×%v", x, y, z)
		}
		// Variante: el original queda igual.
		child := tb.mustJSON(t, "set_parameters", map[string]any{"file": a, "values": map[string]any{"width": 42, "tall": 7.5}, "variant": true})["file"].(string)
		if x, y, z := tb.meshSize(t, child); x != 42 || y != 10 || z != 7.5 {
			t.Errorf("hijo: %v×%v×%v", x, y, z)
		}
		if x, _, _ := tb.meshSize(t, a); x != 20 {
			t.Errorf("el original cambió: %v", x)
		}
		// En el lugar: el mismo archivo ahora mide otra cosa, y el resultado ya lo dice.
		got := tb.mustJSON(t, "set_parameters", map[string]any{"file": a, "values": map[string]any{"width": 30}})
		size := got["render"].(map[string]any)["mesh"].(map[string]any)["sizeMm"].(map[string]any)
		if got["file"] != a || size["x"] != 30.0 || size["y"] != 10.0 {
			t.Errorf("resultado en el lugar: %v", got)
		}
		if x, _, _ := tb.meshSize(t, a); x != 30 {
			t.Errorf("en el lugar: %v", x)
		}
	})
}

func TestCreateFromTemplate(t *testing.T) {
	tb, ws := newToolbox(t, false)
	if err := os.WriteFile(filepath.Join(ws, "printer.json"), []byte(`{"printer":"bambu-x1c"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Workspace vacío (workspace.Create no crea checkpoints): el primero es raíz.
	got := tb.mustJSON(t, "create_from_template", map[string]any{"name": "knob", "parameters": map[string]any{"knob_d": 40, "flutes": 12, "set_screw": true}})
	file, _ := got["file"].(string)
	if file == "" || got["parent"] != "" || got["template"] != "knob" || len(got["changes"].([]any)) != 3 {
		t.Fatalf("create_from_template: %v", got)
	}
	if p, _ := got["plate"].([]any); len(p) != 3 || p[0] != 256.0 {
		t.Errorf("PLATE debía ser el de la X1C: %v", got["plate"])
	}
	src := tb.must(t, "read_checkpoint", map[string]any{"file": file}).Text
	if !strings.HasPrefix(src, "// @checkpoint parent= desc=\"Perilla estriada") {
		t.Errorf("cabecera: %q", strings.SplitN(src, "\n", 2)[0])
	}
	for _, want := range []string{"knob_d = 40;", "flutes = 12;", "set_screw = true;", "PLATE = [256, 256, 256];"} {
		if !strings.Contains(src, want) {
			t.Errorf("falta %q", want)
		}
	}
	if !strings.HasPrefix(file, "001-knob") {
		t.Errorf("nombre: %s", file)
	}

	// Con checkpoints ya hechos: cuelga del padre por defecto; root=true es raíz aparte;
	// parent explícito se respeta.
	second := tb.mustJSON(t, "create_from_template", map[string]any{"name": "box-with-lid.scad", "label": "mi caja"})
	if second["parent"] != file {
		t.Errorf("debía colgar de %v: %v", file, second)
	}
	root := tb.mustJSON(t, "create_from_template", map[string]any{"name": "cable-clip", "root": true})
	if root["parent"] != "" {
		t.Errorf("root=true: %v", root)
	}
	if h := strings.SplitN(tb.must(t, "read_checkpoint", map[string]any{"file": root["file"].(string)}).Text, "\n", 2)[0]; !strings.HasPrefix(h, "// @checkpoint parent= ") {
		t.Errorf("la raíz lleva parent vacío: %q", h)
	}
	explicit := tb.mustJSON(t, "create_from_template", map[string]any{"name": "knob", "parent": file})
	if explicit["parent"] != file {
		t.Errorf("parent explícito: %v", explicit)
	}

	// Errores: nada se crea.
	before := scadCount(t, ws)
	for _, args := range []map[string]any{
		{}, {"name": ""}, {"name": "no-existe"}, {"name": "../knob"}, {"name": "templates/knob"}, {"name": "/etc/passwd"}, {"name": "knob\x00"},
		{"name": "knob", "parameters": map[string]any{"nada": 1}},
		{"name": "knob", "parameters": map[string]any{"PLATE": []any{1, 2, 3}}}, // Hidden
		{"name": "knob", "parameters": map[string]any{"knob_d": "grande"}},
		{"name": "knob", "parameters": "no es json"},
		{"name": "knob", "parent": "../001-knob.scad"},
		{"name": "knob", "parent": "no-existe.scad"},
	} {
		tb.fails(t, "create_from_template", args)
	}
	if scadCount(t, ws) != before {
		t.Error("una llamada que falla creó un checkpoint")
	}
}

// Todas las plantillas se crean y renderizan con el OpenSCAD instalado, y con sus
// parámetros cambiados siguen renderizando.
func TestCreateFromEveryTemplateRenders(t *testing.T) {
	needOpenSCAD(t)
	names, err := templateNames()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		t.Run(n, func(t *testing.T) {
			tb, _ := newToolbox(t, false)
			got := tb.mustJSON(t, "create_from_template", map[string]any{"name": n})
			r := tb.mustJSON(t, "render", map[string]any{"file": got["file"].(string)})
			if r["status"] != "ok" {
				t.Fatalf("%s no renderiza: %v", n, r)
			}
		})
	}
}

// La plantilla creada renderiza con sus valores cambiados, en los dos motores.
func TestCreateFromTemplateRendersWithParameters(t *testing.T) {
	eachEngine(t, func(t *testing.T, tb *Toolbox, ws string) {
		got := tb.mustJSON(t, "create_from_template", map[string]any{"name": "knob", "parameters": map[string]any{"knob_d": 44, "knob_h": 20, "flutes": 10}})
		x, y, z := tb.meshSize(t, got["file"].(string))
		if x < 43.5 || x > 44.5 || y < 43.5 || y > 44.5 || z < 19.9 || z > 20.1 {
			t.Errorf("knob 44×20: %v×%v×%v", x, y, z)
		}
		// Y set_parameters sobre el resultado (en el lugar).
		file := tb.mustJSON(t, "set_parameters", map[string]any{"values": map[string]any{"knob_d": 30}})["file"].(string)
		if x, _, _ := tb.meshSize(t, file); x < 29.5 || x > 30.5 {
			t.Errorf("knob 30: %v", x)
		}
	})
}
