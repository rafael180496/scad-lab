package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/checkpoint"
)

func writeLib(t *testing.T, ws, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(ws, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "lib", name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const libLid = `// Tapa de la caja
module tapa(w = 20) {
    cube([w, 10, 2]);
}

module asa_tapa() {
    cylinder(d = 4, h = 6);
}
`

// read, outline, find y edit aceptan lib/<nombre>.scad.
func TestLibFilesAreReadableAndEditable(t *testing.T) {
	needOpenSCAD(t)
	tb, ws := newToolbox(t, false)
	writeLib(t, ws, "tapa.scad", libLid)
	// Un checkpoint que la incluye (para que la edición se verifique con un render).
	user := tb.create(t, "con tapa", "include <lib/tapa.scad>\ncolor(\"red\") tapa(30);\n", nil)

	if got := tb.must(t, "read_checkpoint", map[string]any{"file": "lib/tapa.scad"}).Text; got != libLid {
		t.Errorf("read: %q", got)
	}
	// La barra invertida de Windows se normaliza.
	if got := tb.must(t, "read_checkpoint", map[string]any{"file": `lib\tapa.scad`, "from": 2, "to": 3}).Text; !strings.Contains(got, "[lib/tapa.scad, lines 2-3 of 8]") || !strings.Contains(got, "3|     cube([w, 10, 2]);") {
		t.Errorf("lib\\\\tapa.scad: %q", got)
	}
	out := tb.must(t, "outline_checkpoint", map[string]any{"file": "lib/tapa.scad"}).Text
	if !strings.Contains(out, `"file":"lib/tapa.scad"`) || !strings.Contains(out, `{"name":"tapa","line":2,"end":4,"args":"w = 20"}`) || !strings.Contains(out, `"name":"asa_tapa","line":6,"end":8`) {
		t.Errorf("outline: %s", out)
	}
	if got := tb.must(t, "find_in_checkpoint", map[string]any{"file": "lib/tapa.scad", "query": "cylinder", "context": 0}).Text; !strings.Contains(got, "7:     cylinder(d = 4, h = 6);") {
		t.Errorf("find: %q", got)
	}

	// Editar: respaldo en .scadlab/history/lib/tapa.scad/, sin cabecera.
	got := tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": "lib/tapa.scad", "edits": []any{map[string]any{"old": "cube([w, 10, 2]);", "new": "cube([w, 12, 2]);"}}})
	if got["file"] != "lib/tapa.scad" || got["updated"] != true || got["description"] != nil || got["checked"] != user {
		t.Fatalf("resultado: %v", got)
	}
	if r := got["render"].(map[string]any); r["ok"] != true || r["mesh"].(map[string]any)["sizeMm"].(map[string]any)["y"] != 12.0 {
		t.Errorf("el render del checkpoint que la incluye debía reflejar el cambio: %v", got["render"])
	}
	if now, _ := os.ReadFile(filepath.Join(ws, "lib", "tapa.scad")); string(now) != strings.Replace(libLid, "[w, 10, 2]", "[w, 12, 2]", 1) {
		t.Errorf("archivo: %q", now)
	}
	hist, err := checkpoint.History(ws, "lib/tapa.scad")
	if err != nil || len(hist) != 1 || hist[0].Tool != "edit_checkpoint" {
		t.Fatalf("historial: %+v %v", hist, err)
	}
	if v, _ := checkpoint.ReadVersion(ws, "lib/tapa.scad", hist[0].ID); v != libLid {
		t.Errorf("respaldo: %q", v)
	}
	if _, err := os.Stat(filepath.Join(ws, ".scadlab", "history", "lib", "tapa.scad", hist[0].ID+".scad")); err != nil {
		t.Errorf("el respaldo debe estar en .scadlab/history/lib/tapa.scad/: %v", err)
	}
	// La línea 1 de un lib/ SÍ se edita (no hay cabecera).
	tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": "lib/tapa.scad", "edits": []any{map[string]any{"from_line": 1, "to_line": 1, "new": "// Tapa v2"}}})
	if !strings.HasPrefix(tb.must(t, "read_checkpoint", map[string]any{"file": "lib/tapa.scad"}).Text, "// Tapa v2\n") {
		t.Error("línea 1 de lib/")
	}
	// Una edición de lib/ sin ningún checkpoint que la incluya lo dice en vez de renderizar.
	writeLib(t, ws, "suelta.scad", "module s() { cube(1); }\n")
	got = tb.mustJSON(t, "edit_checkpoint", map[string]any{"file": "lib/suelta.scad", "edits": []any{map[string]any{"old": "cube(1)", "new": "cube(2)"}}})
	if r := got["render"].(map[string]any); r["ok"] != nil || !strings.Contains(r["message"].(string), "No checkpoint includes lib/suelta.scad") {
		t.Errorf("sin dependientes: %v", got["render"])
	}
}

// Rutas hostiles y librerías instaladas: rechazadas por todas las herramientas.
func TestLibPathsAreSafe(t *testing.T) {
	tb, ws := newToolbox(t, false)
	writeLib(t, ws, "ok.scad", "module ok() { cube(1); }\n")
	if err := os.MkdirAll(filepath.Join(ws, "lib", "BOSL2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "lib", "BOSL2", "std.scad"), []byte("// instalada\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secreto.scad"), []byte("secreto\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hostile := []string{
		"lib/BOSL2/std.scad", "lib/../001-a.scad", "lib/../../etc/passwd", "../lib/ok.scad", "/lib/ok.scad", "/etc/passwd",
		filepath.Join(ws, "lib", "ok.scad"), "C:/lib/ok.scad", `C:\lib\ok.scad`, `lib\..\ok.scad`, `lib\BOSL2\std.scad`, "lib/ok.stl", "lib/", "lib", "lib//ok.scad", "lib/ok.scad\x00",
		"./lib/ok.scad", "lib/nada.scad",
	}
	for _, f := range hostile {
		for _, tool := range []string{"read_checkpoint", "outline_checkpoint"} {
			if out, err := tb.Call(testCtx(), tool, map[string]any{"file": f}); err != nil || !out.IsError {
				t.Errorf("%s(%q) debía rechazarse: %+v", tool, f, out)
			}
		}
		tb.fails(t, "find_in_checkpoint", map[string]any{"file": f, "query": "x"})
		tb.fails(t, "edit_checkpoint", map[string]any{"file": f, "edits": []any{map[string]any{"old": "x", "new": "y"}}})
	}
	if b, _ := os.ReadFile(filepath.Join(ws, "lib", "BOSL2", "std.scad")); string(b) != "// instalada\n" {
		t.Errorf("una librería instalada se modificó: %q", b)
	}
	// Un nombre que no existe lista lo que hay.
	if out := tb.fails(t, "read_checkpoint", map[string]any{"file": "lib/nada.scad"}); !strings.Contains(out.Text, "lib/ok.scad") {
		t.Errorf("el error debe listar lib/: %q", out.Text)
	}
	// Enlaces: uno que sale del workspace no se lee ni se edita.
	if err := os.Symlink(filepath.Join(outside, "secreto.scad"), filepath.Join(ws, "lib", "fuga.scad")); err == nil {
		tb.fails(t, "read_checkpoint", map[string]any{"file": "lib/fuga.scad"})
		tb.fails(t, "edit_checkpoint", map[string]any{"file": "lib/fuga.scad", "edits": []any{map[string]any{"old": "secreto", "new": "x"}}})
		if b, _ := os.ReadFile(filepath.Join(outside, "secreto.scad")); string(b) != "secreto\n" {
			t.Error("se escribió fuera del workspace")
		}
	}
	// Y la carpeta lib/ que sale del workspace.
	tb2, ws2 := newToolbox(t, false)
	if err := os.RemoveAll(filepath.Join(ws2, "lib")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws2, "lib")); err == nil {
		tb2.fails(t, "read_checkpoint", map[string]any{"file": "lib/secreto.scad"})
		tb2.fails(t, "edit_checkpoint", map[string]any{"file": "lib/secreto.scad", "edits": []any{map[string]any{"old": "secreto", "new": "x"}}})
		tb2.fails(t, "extract_module", map[string]any{"file": "001-x.scad", "modules": []any{"m"}, "lib": "secreto"})
	}
}
