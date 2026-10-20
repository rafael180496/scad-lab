package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/checkpoint"
	"scadlab/backend/clichat"
	"scadlab/backend/textedit"
	"scadlab/backend/turnmark"
)

// macetaSource es la maceta de prueba (≈600 líneas) con la que se miden las
// herramientas de S17: cónica hueca con difference, drenajes, parámetros del
// Customizer en secciones y un texto/marco en relieve curvado (intersection con un
// cascarón cónico) en otro color().
func macetaSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "maceta.scad"))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// potBox es un workspace con la maceta ya creada como checkpoint.
func potBox(t *testing.T) (*Toolbox, string, string) {
	t.Helper()
	tb, ws := newToolbox(t, false)
	name := tb.create(t, "maceta", macetaSource(t), nil)
	return tb, ws, name
}

func lineOfIn(t *testing.T, src, needle string) int {
	t.Helper()
	for i, l := range textedit.Lines(src) {
		if strings.Contains(l, needle) {
			return i + 1
		}
	}
	t.Fatalf("no encuentro %q", needle)
	return 0
}

// --- read_checkpoint --------------------------------------------------------

func TestReadCheckpointByRangeAndTruncation(t *testing.T) {
	tb, ws, name := potBox(t)
	disk, _ := os.ReadFile(filepath.Join(ws, name))
	total := textedit.CountLines(string(disk))
	if total < 550 || len(disk) < 3*readMaxBytes/2 {
		t.Fatalf("la maceta de prueba debía ser grande: %d líneas, %d bytes", total, len(disk))
	}

	// Sin rango y grande: el comienzo crudo (cortado en una línea) y la nota.
	out := tb.must(t, "read_checkpoint", map[string]any{"file": name}).Text
	if !strings.HasPrefix(out, "// @checkpoint parent= desc=\"maceta\"\n") || !strings.Contains(out, "[truncated: ") || !strings.Contains(out, "outline_checkpoint") || !strings.Contains(out, "full=true") {
		t.Errorf("sin rango: %q …", truncate(out))
	}
	if len(out) > readMaxBytes+400 {
		t.Errorf("la lectura sin rango pasa el tope: %d bytes", len(out))
	}
	if !strings.Contains(out, itoa(total)+" lines") {
		t.Errorf("la nota debe decir cuántas líneas tiene: %q", out[len(out)-200:])
	}
	// full=true: el archivo entero, tal cual.
	if full := tb.must(t, "read_checkpoint", map[string]any{"file": name, "full": true}).Text; full != string(disk) {
		t.Errorf("full no es el archivo entero (%d vs %d bytes)", len(full), len(disk))
	}

	// Con rango: líneas numeradas N| código.
	out = tb.must(t, "read_checkpoint", map[string]any{"file": name, "from": 10, "to": 13}).Text
	lines := strings.Split(out, "\n")
	want := textedit.Lines(string(disk))
	if len(lines) != 6 || lines[0] != "["+name+", lines 10-13 of "+itoa(total)+"]" || lines[5] != "[continues: from=14]" {
		t.Fatalf("rango: %q", out)
	}
	for i := 0; i < 4; i++ {
		if lines[1+i] != itoa(10+i)+"| "+want[9+i] {
			t.Errorf("línea %d: %q", 10+i, lines[1+i])
		}
	}
	// Solo `from`: hasta 400 líneas, y dice cómo seguir.
	out = tb.must(t, "read_checkpoint", map[string]any{"file": name, "from": 5}).Text
	if n := strings.Count(out, "\n| "); n != 0 { // formato sin sangrías raras
		t.Errorf("formato: %q", truncate(out))
	}
	if c := strings.Count(out, "\n"); c < readMaxLines || c > readMaxLines+3 || !strings.Contains(out, "[continues: from=405") {
		t.Errorf("from sin to: %d saltos, cola %q", c, out[max(0, len(out)-120):])
	}
	// Pedir más de 400 se recorta a 400.
	out = tb.must(t, "read_checkpoint", map[string]any{"file": name, "from": 1, "to": 600}).Text
	if !strings.Contains(out, "lines 1-400 of") || !strings.Contains(out, "[continues: from=401 (at most 400 lines per call)]") {
		t.Errorf("tope de 400: %q", out[:80]+"…"+out[len(out)-100:])
	}
	// Hasta el final: sin nota de «sigue».
	out = tb.must(t, "read_checkpoint", map[string]any{"file": name, "from": total - 1, "to": total + 50}).Text
	if strings.Contains(out, "continues") || !strings.HasSuffix(out, itoa(total)+"| "+want[total-1]) {
		t.Errorf("final: %q", out)
	}
	// Errores.
	for name2, args := range map[string]map[string]any{
		"fuera":      {"file": name, "from": total + 5},
		"to<from":    {"file": name, "from": 20, "to": 10},
		"sin nada":   {"file": "../" + name, "from": 1},
		"subcarpeta": {"file": "sub/" + name, "from": 1},
	} {
		if out := tb.fails(t, "read_checkpoint", args); out.Text == "" {
			t.Errorf("%s: sin mensaje", name2)
		}
	}
}

// Un archivo chico se sigue leyendo crudo, como antes.
func TestReadCheckpointSmallFileIsRaw(t *testing.T) {
	tb, ws := newToolbox(t, false)
	name := tb.create(t, "cubo", srcCube, nil)
	disk, _ := os.ReadFile(filepath.Join(ws, name))
	if got := tb.must(t, "read_checkpoint", map[string]any{"file": name}).Text; got != string(disk) {
		t.Errorf("crudo: %q", got)
	}
	// Archivo CRLF y con Unicode: los números de línea no cuentan el \r.
	crlf := tb.create(t, "crlf", "color(\"red\")\r\n  cube(1); // ñandú\r\n", nil)
	got := tb.must(t, "read_checkpoint", map[string]any{"file": crlf, "from": 2, "to": 3}).Text
	if !strings.Contains(got, "\n2| color(\"red\")\n3|   cube(1); // ñandú") || strings.Contains(got, "\r") {
		t.Errorf("CRLF: %q", got)
	}
}

// --- outline_checkpoint -----------------------------------------------------

func TestOutlineCheckpointOfThePot(t *testing.T) {
	tb, _, name := potBox(t)
	out := tb.must(t, "outline_checkpoint", map[string]any{"file": name})
	if len(out.Text) > outlineMaxBytes {
		t.Errorf("el esquema pasa el tope de %d bytes: %d", outlineMaxBytes, len(out.Text))
	}
	if strings.Contains(out.Text, "\n") {
		t.Error("el esquema va en una línea (JSON sin sangría)")
	}
	var o struct {
		File   string `json:"file"`
		Lines  int    `json:"lines"`
		Bytes  int    `json:"bytes"`
		Params []struct {
			Section string `json:"section"`
			Items   []struct {
				Name  string `json:"name"`
				Line  int    `json:"line"`
				Value string `json:"value"`
			} `json:"items"`
		} `json:"params"`
		Modules []struct {
			Name      string `json:"name"`
			Line, End int
		} `json:"modules"`
		Functions []struct{ Name string } `json:"functions"`
		Includes  []any                   `json:"includes"`
		Parts     []struct {
			Color string `json:"color"`
			Line  int    `json:"line"`
			Value string `json:"value"`
		} `json:"parts"`
	}
	if err := json.Unmarshal([]byte(out.Text), &o); err != nil {
		t.Fatalf("no es JSON: %v\n%s", err, out.Text)
	}
	if o.File != name || o.Lines < 550 || o.Bytes < 20000 {
		t.Errorf("tamaño: %+v", o)
	}
	sections := []string{}
	for _, s := range o.Params {
		sections = append(sections, s.Section)
	}
	if got := strings.Join(sections, ","); got != "Maceta,Reborde,Drenaje,Costillas,Patas,Asas,Textura,Base,Relieve,Plato,Colores,Hidden" {
		t.Errorf("secciones: %s", got)
	}
	src := macetaSource(t)
	var altura *struct {
		Name  string `json:"name"`
		Line  int    `json:"line"`
		Value string `json:"value"`
	}
	for _, s := range o.Params {
		for i := range s.Items {
			if s.Items[i].Name == "altura" {
				altura = &s.Items[i]
			}
		}
	}
	// La línea que dice el esquema es la del archivo en disco (el checkpoint cambió la cabecera).
	disk := tb.must(t, "read_checkpoint", map[string]any{"file": name, "full": true}).Text
	if altura == nil || altura.Value != "85" || textedit.Lines(disk)[altura.Line-1] != "altura = 85; // [30:1:160]" {
		t.Errorf("altura: %+v", altura)
	}
	_ = src
	mods := map[string][2]int{}
	for _, m := range o.Modules {
		mods[m.Name] = [2]int{m.Line, m.End}
	}
	for _, n := range []string{"maceta", "relieve", "cascaron_relieve", "drenajes", "texto_2d"} {
		r, ok := mods[n]
		if !ok || r[1] <= r[0] && n != "texto_2d" {
			t.Errorf("módulo %s: %v", n, r)
		}
	}
	m := mods["maceta"]
	lines := textedit.Lines(disk)
	if !strings.HasPrefix(lines[m[0]-1], "module maceta()") || strings.TrimSpace(lines[m[1]-1]) != "}" {
		t.Errorf("maceta %v: %q ... %q", m, lines[m[0]-1], lines[m[1]-1])
	}
	if len(o.Functions) < 5 || len(o.Parts) != 3 || o.Parts[0].Color != "color_maceta" || o.Parts[0].Value != "seagreen" || o.Parts[1].Value != "orange" {
		t.Errorf("funciones/partes: %d %+v", len(o.Functions), o.Parts)
	}
	if len(o.Includes) != 0 {
		t.Errorf("includes: %v", o.Includes)
	}
}

func TestOutlineOfABrokenFileSaysWhere(t *testing.T) {
	tb, ws := newToolbox(t, false)
	name := tb.create(t, "rota", "color(\"red\") cube(1);\ntexto = \"sin cerrar;\n", nil)
	out := tb.must(t, "outline_checkpoint", map[string]any{"file": name}).Text
	if !strings.Contains(out, `"error"`) || !strings.Contains(out, "line 3") || !strings.Contains(out, `"lines":3`) {
		t.Errorf("archivo roto: %s", out)
	}
	_ = ws
}

// El esquema se achica solo si no cabe en 4 KB.
func TestOutlineShrinksToFit(t *testing.T) {
	tb, _ := newToolbox(t, false)
	var b strings.Builder
	b.WriteString("/* [Muchos] */\n")
	for i := 0; i < 400; i++ {
		b.WriteString("parametro_numero_" + itoa(i) + " = " + itoa(i*7) + "; // [0:1:10000]\n")
	}
	b.WriteString("color(\"red\") cube(1);\n")
	name := tb.create(t, "enorme", b.String(), nil)
	out := tb.must(t, "outline_checkpoint", map[string]any{"file": name}).Text
	if len(out) > outlineMaxBytes {
		t.Errorf("pasa el tope: %d", len(out))
	}
	if !strings.Contains(out, "omitted") {
		t.Errorf("debe decir que omitió parámetros: %s", truncate(out))
	}
}

// --- find_in_checkpoint -----------------------------------------------------

func TestFindInCheckpoint(t *testing.T) {
	tb, _, name := potBox(t)
	disk := tb.must(t, "read_checkpoint", map[string]any{"file": name, "full": true}).Text
	lines := textedit.Lines(disk)

	out := tb.must(t, "find_in_checkpoint", map[string]any{"file": name, "query": "MODULE CASCARON_relieve"}).Text // sin distinguir mayúsculas
	l := lineOfIn(t, disk, "module cascaron_relieve")
	if !strings.HasPrefix(out, "["+name+": 1 matches of «MODULE CASCARON_relieve»]") ||
		!strings.Contains(out, itoa(l)+": "+lines[l-1]) || !strings.Contains(out, itoa(l-2)+"| "+lines[l-3]) || !strings.Contains(out, itoa(l+2)+"| "+lines[l+1]) {
		t.Errorf("ventana: %q", out)
	}
	// context=0: solo la línea.
	out = tb.must(t, "find_in_checkpoint", map[string]any{"file": name, "query": "module cascaron_relieve", "context": 0}).Text
	if strings.Count(out, "\n") != 1 {
		t.Errorf("context=0: %q", out)
	}
	// Ventanas fusionadas y muchas coincidencias: tope de 30 y de bytes.
	out = tb.must(t, "find_in_checkpoint", map[string]any{"file": name, "query": "altura"}).Text
	if len(out) > findMaxBytes+300 {
		t.Errorf("pasa el tope de bytes: %d", len(out))
	}
	if !strings.Contains(out, "showing ") {
		t.Errorf("con muchas coincidencias dice cuántas muestra: %q", out[len(out)-200:])
	}
	if c := strings.Count(out, "\n--\n"); c < 1 {
		t.Errorf("ventanas separadas por --: %d", c)
	}
	// Regex.
	out = tb.must(t, "find_in_checkpoint", map[string]any{"file": name, "query": `^module (drenajes|agujero_drenaje)\(`, "regex": true, "context": 0}).Text
	if !strings.Contains(out, ": 2 matches of") || strings.Count(out, ": module ") != 2 {
		t.Errorf("regex: %q", out)
	}
	tb.fails(t, "find_in_checkpoint", map[string]any{"file": name, "query": "(", "regex": true})
	tb.fails(t, "find_in_checkpoint", map[string]any{"file": name, "query": "  "})
	tb.fails(t, "find_in_checkpoint", map[string]any{"file": name})
	// Sin coincidencias: las líneas más parecidas.
	out = tb.must(t, "find_in_checkpoint", map[string]any{"file": name, "query": "module cascaron_relieves(z0)"}).Text
	if !strings.Contains(out, "no matches") || !strings.Contains(out, "most similar lines") || !strings.Contains(out, "module cascaron_relieve(z0, z1)") {
		t.Errorf("sin coincidencias: %q", out)
	}
}

// --- edit_checkpoint --------------------------------------------------------

func TestEditCheckpointFormsAndResult(t *testing.T) {
	needOpenSCAD(t)
	tb, ws, name := potBox(t)
	before, _ := os.ReadFile(filepath.Join(ws, name))
	lw := lineOfIn(t, string(before), "altura = 85;")
	lrow := lineOfIn(t, string(before), "color_plato = \"white\";")

	got := tb.mustJSON(t, "edit_checkpoint", map[string]any{
		"file": name,
		"edits": []any{
			map[string]any{"old": "altura = 85;", "new": "altura = 95;"},
			map[string]any{"from_line": lrow, "to_line": lrow, "new": "color_plato = \"gray\";"},
			map[string]any{"from_line": lrow + 1, "to_line": lrow, "new": "// agregado por edit_checkpoint"},
		},
		"note": "más alta y plato gris",
	})
	if got["file"] != name || got["updated"] != true || got["unchanged"] != false || got["description"] != "más alta y plato gris" {
		t.Fatalf("resultado: %v", got)
	}
	after, _ := os.ReadFile(filepath.Join(ws, name))
	wantText := strings.Replace(string(before), "altura = 85;", "altura = 95;", 1)
	wantText = strings.Replace(wantText, "color_plato = \"white\";", "color_plato = \"gray\";\n// agregado por edit_checkpoint", 1)
	wantText = strings.Replace(wantText, "desc=\"maceta\"", "desc=\"más alta y plato gris\"", 1)
	if string(after) != wantText {
		t.Errorf("el archivo no quedó como se esperaba")
	}
	if int(got["lines"].(float64)) != textedit.CountLines(string(after)) {
		t.Errorf("lines: %v", got["lines"])
	}
	// El resultado trae las líneas reemplazadas (y las nuevas).
	chs := got["changes"].([]any)
	if len(chs) != 3 {
		t.Fatalf("cambios: %v", chs)
	}
	c0 := chs[0].(map[string]any)
	if c0["edit"] != 1.0 || c0["was"] != itoa(lw) || c0["now"] != itoa(lw) || c0["old"] != "altura = 85; // [30:1:160]" || !strings.HasPrefix(c0["new"].(string), "altura = 95;") {
		t.Errorf("cambio 1: %v", c0)
	}
	c2 := chs[2].(map[string]any)
	if c2["was"] != "+"+itoa(lrow+1) || c2["old"] != nil || c2["new"] != "// agregado por edit_checkpoint" {
		t.Errorf("inserción: %v", c2)
	}
	// El historial: una versión, con la herramienta y un resumen.
	hist, err := checkpoint.History(ws, name)
	if err != nil || len(hist) != 1 || hist[0].Tool != "edit_checkpoint" || hist[0].Reason != "ai" || hist[0].Note != "más alta y plato gris" {
		t.Fatalf("historial: %+v %v", hist, err)
	}
	if v, _ := checkpoint.ReadVersion(ws, name, hist[0].ID); v != string(before) {
		t.Error("el respaldo no es lo que había")
	}
	if got["version"] != hist[0].ID {
		t.Errorf("version: %v", got["version"])
	}
	// Lint + render del resultado, compacto.
	r := got["render"].(map[string]any)
	if r["ok"] != true || r["status"] != "ok" {
		t.Fatalf("render: %v", r)
	}
	if size := r["mesh"].(map[string]any)["sizeMm"].(map[string]any); size["z"] != 95.0 {
		t.Errorf("la maceta debía medir 95 de alto: %v", size)
	}
	if _, ok := got["lint"].([]any); !ok {
		t.Errorf("lint: %v", got["lint"])
	}
	// Sin nota: la descripción de la cabecera no cambia y la nota del historial resume las ediciones.
	tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": name, "edits": []any{map[string]any{"old": "altura = 95;", "new": "altura = 90;"}}})
	hist, _ = checkpoint.History(ws, name)
	if len(hist) != 2 || !strings.Contains(hist[0].Note, "1 edit(s)") {
		t.Errorf("nota del historial: %+v", hist)
	}
	if h := strings.SplitN(tb.must(t, "read_checkpoint", map[string]any{"file": name, "full": true}).Text, "\n", 2)[0]; h != `// @checkpoint parent= desc="más alta y plato gris"` {
		t.Errorf("la cabecera cambió: %q", h)
	}
}

func TestEditCheckpointInsertDeleteAppend(t *testing.T) {
	tb, ws := newToolbox(t, false)
	name := tb.create(t, "chico", "a = 1;\nb = 2;\nc = 3;\ncolor(\"red\") cube(a + b + c);\n", nil)
	read := func() string { return tb.must(t, "read_checkpoint", map[string]any{"file": name}).Text }
	body := func() string { _, b, _ := strings.Cut(read(), "\n"); return b }

	tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": name, "edits": []any{map[string]any{"from_line": 3, "to_line": 3, "new": ""}}}) // borra b = 2;
	if body() != "a = 1;\nc = 3;\ncolor(\"red\") cube(a + b + c);\n" {
		t.Errorf("borrar: %q", body())
	}
	n := textedit.CountLines(read())
	tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": name, "edits": []any{map[string]any{"from_line": n + 1, "to_line": n, "new": "// final"}}})
	if !strings.HasSuffix(read(), "cube(a + b + c);\n// final\n") {
		t.Errorf("agregar al final: %q", read())
	}
	// to_line por defecto = from_line.
	tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": name, "edits": []any{map[string]any{"from_line": 2, "new": "a = 5;"}}})
	if !strings.Contains(read(), "\na = 5;\n") || strings.Contains(read(), "a = 1;") {
		t.Errorf("una línea: %q", read())
	}
	// `edits` como un solo objeto y como texto JSON: los modelos chicos lo mandan así.
	tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": name, "edits": map[string]any{"old": "a = 5;", "new": "a = 6;"}})
	tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": name, "edits": `[{"old": "a = 6;", "new": "a = 7;"}]`})
	if !strings.Contains(read(), "a = 7;") {
		t.Errorf("formas de edits: %q", read())
	}
	// Sin cambios reales: no escribe ni respalda.
	hist, _ := checkpoint.History(ws, name)
	out := tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": name, "edits": []any{map[string]any{"from_line": 2, "to_line": 2, "new": "a = 7;"}}})
	if out["unchanged"] != true || out["updated"] != false {
		t.Errorf("idéntico: %v", out)
	}
	if h2, _ := checkpoint.History(ws, name); len(h2) != len(hist) {
		t.Error("un cambio nulo no respalda")
	}
	if UpdatesInPlace("edit_checkpoint", mustText(t, out)) {
		t.Error("un cambio nulo no refresca la interfaz")
	}
}

func mustText(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Todo o nada, y los errores le sirven a un modelo chico.
func TestEditCheckpointAllOrNothingAndErrors(t *testing.T) {
	tb, ws, name := potBox(t)
	before, _ := os.ReadFile(filepath.Join(ws, name))
	disk := string(before)
	lh := lineOfIn(t, disk, "altura = 85;")

	check := func(label, want string, args map[string]any) {
		t.Helper()
		args["file"] = name
		out := tb.fails(t, "edit_checkpoint", args)
		if !strings.Contains(out.Text, want) {
			t.Errorf("%s: el error no dice %q: %s", label, want, out.Text)
		}
		// Los errores de resolver una edición dicen que no se escribió nada; los de
		// forma (antes de leer el archivo) no escriben nada por definición.
		if !strings.Contains(out.Text, "Nothing was written") && !strings.Contains(out.Text, "missing") && !strings.Contains(out.Text, "empty") && !strings.Contains(out.Text, "must be an object") {
			t.Errorf("%s: el error no dice que no se escribió nada: %s", label, out.Text)
		}
		now, _ := os.ReadFile(filepath.Join(ws, name))
		if string(now) != disk {
			t.Fatalf("%s: el archivo cambió", label)
		}
		if h, _ := checkpoint.History(ws, name); len(h) != 0 {
			t.Fatalf("%s: dejó historial: %+v", label, h)
		}
	}
	ed := func(o, n string) map[string]any { return map[string]any{"old": o, "new": n} }
	rng := func(f, to int, n string) map[string]any {
		return map[string]any{"from_line": f, "to_line": to, "new": n}
	}

	// La segunda edición falla: la primera no se aplica.
	check("todo o nada", "edit 2", map[string]any{"edits": []any{ed("altura = 85;", "altura = 99;"), ed("no existe esto en el archivo", "x")}})
	check("ambiguo", "appears ", map[string]any{"edits": []any{ed("eps", "e")}})
	// Más parecida: un espacio de más en `old`.
	check("más parecida", "the most similar line is "+itoa(lh), map[string]any{"edits": []any{ed("altura  =  85;", "altura = 1;")}})
	check("solape", "overlap", map[string]any{"edits": []any{rng(lh, lh+2, "x"), rng(lh+2, lh+3, "y")}})
	check("cabecera", "header", map[string]any{"edits": []any{rng(1, 1, "// otra")}})
	check("cabecera insertar", "header", map[string]any{"edits": []any{rng(1, 0, "// otra")}})
	check("fuera del archivo", "outside the file", map[string]any{"edits": []any{rng(9999, 9999, "x")}})
	check("sin new", "`new` is missing", map[string]any{"edits": []any{map[string]any{"old": "altura = 85;"}}})
	check("ni old ni líneas", "missing", map[string]any{"edits": []any{map[string]any{"new": "x"}}})
	check("dos formas", "one form only", map[string]any{"edits": []any{map[string]any{"old": "a", "new": "b", "from_line": 3}}})
	check("vacío", "empty", map[string]any{"edits": []any{}})
	check("no es un objeto", "must be an object", map[string]any{"edits": []any{"altura"}})

	// Argumentos y rutas.
	tb.fails(t, "edit_checkpoint", map[string]any{"edits": []any{ed("a", "b")}}) // sin file
	tb.fails(t, "edit_checkpoint", map[string]any{"file": name})                 // sin edits
	for _, f := range []string{"../" + name, filepath.Join(ws, name), "sub/" + name, "009-nada.scad", "lib/BOSL2/std.scad", "lib/../" + name, `lib\..\x.scad`, "lib/nada.scad", "/etc/passwd"} {
		tb.fails(t, "edit_checkpoint", map[string]any{"file": f, "edits": []any{ed("a", "b")}})
	}
	if now, _ := os.ReadFile(filepath.Join(ws, name)); string(now) != disk {
		t.Error("un rechazo tocó el archivo")
	}
}

// Un cambio que rompe el modelo no es un error de la herramienta (el archivo SÍ
// cambió): trae hasta 5 errores con su línea y el consejo.
func TestEditCheckpointBrokenRender(t *testing.T) {
	needOpenSCAD(t)
	tb, ws, name := potBox(t)
	disk, _ := os.ReadFile(filepath.Join(ws, name))
	l := lineOfIn(t, string(disk), "module perfil_cuerpo() {")
	got := tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": name, "edits": []any{map[string]any{"from_line": l, "to_line": l, "new": "module perfil_cuerpo( {"}}})
	if got["updated"] != true {
		t.Fatalf("el archivo sí cambió: %v", got)
	}
	r := got["render"].(map[string]any)
	if r["ok"] != false || r["status"] != "error" {
		t.Fatalf("render: %v", r)
	}
	ds := r["diagnostics"].([]any)
	if len(ds) == 0 || len(ds) > maxShownDiag {
		t.Fatalf("diagnósticos: %d", len(ds))
	}
	if d := ds[0].(map[string]any); d["severity"] != "error" || d["line"] == nil {
		t.Errorf("primer diagnóstico: %v", d)
	}
	if msg, _ := r["message"].(string); !strings.Contains(msg, "edit_checkpoint") || !strings.Contains(msg, name) {
		t.Errorf("consejo: %q", msg)
	}
	// Se arregla con otra edición y el historial conserva los dos intentos.
	fixed := tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": name, "edits": []any{map[string]any{"from_line": l, "to_line": l, "new": "module perfil_cuerpo() {"}}})
	if fixed["render"].(map[string]any)["ok"] != true {
		t.Errorf("arreglado: %v", fixed["render"])
	}
	if h, _ := checkpoint.History(ws, name); len(h) != 2 {
		t.Errorf("historial: %+v", h)
	}
}

// «Solo consulta» las rechaza el servidor; en «Agente» pasan.
func TestS17WriteToolsAreAgentOnly(t *testing.T) {
	c := setup(t, nil)
	base := c.create("caja", "a = 1;\ncolor(\"red\") cube(a);\n", nil)
	before := c.readWs(base)

	end, err := turnmark.Begin(c.Ws, turnmark.Ask)
	if err != nil {
		t.Fatal(err)
	}
	for tool, args := range map[string]map[string]any{
		"edit_checkpoint": {"file": base, "edits": []any{map[string]any{"old": "a = 1;", "new": "a = 2;"}}},
		"extract_module":  {"file": base, "modules": []any{"m"}, "lib": "x"},
	} {
		r := c.tool(tool, args)
		if !r.IsError || !strings.Contains(r.Text, "“"+tool+"” tool changes the project") {
			t.Errorf("%s en consulta: isError=%v %q", tool, r.IsError, truncate(r.Text))
		}
	}
	if libs, _ := checkpoint.ListLibFiles(c.Ws); c.readWs(base) != before || c.exists(".scadlab/history") || len(libs) != 0 {
		t.Error("el rechazo tocó el proyecto")
	}
	// La lectura sí pasa.
	for _, tool := range []string{"outline_checkpoint", "find_in_checkpoint", "read_checkpoint"} {
		if r := c.tool(tool, map[string]any{"file": base, "query": "cube"}); r.IsError {
			t.Errorf("%s debía pasar en consulta: %s", tool, r.Text)
		}
	}
	end()

	r := c.mustOK("edit_checkpoint", map[string]any{"file": base, "edits": []any{map[string]any{"old": "a = 1;", "new": "a = 2;"}}})
	if r["updated"] != true || !strings.Contains(c.readWs(base), "a = 2;") {
		t.Errorf("en Agente: %v", r)
	}
	for tool, kind := range map[string]string{"edit_checkpoint": "write", "extract_module": "write", "outline_checkpoint": "read", "find_in_checkpoint": "read"} {
		w := containsStr(clichat.WriteTools(), tool)
		rd := containsStr(clichat.ReadTools(), tool)
		if (kind == "write") != w || (kind == "read") != rd {
			t.Errorf("clichat: %s write=%v read=%v", tool, w, rd)
		}
	}
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func testCtx() context.Context { return context.Background() }

func jsonUnmarshal(text string, v any) error { return json.Unmarshal([]byte(text), v) }
