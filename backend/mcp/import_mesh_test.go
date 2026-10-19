package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func scadCount(t *testing.T, ws string) int {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(ws, "*.scad"))
	return len(files)
}

// import_mesh deja un modelo de refs/ listo para editar: un checkpoint raíz que
// lo importa. Un 3MF o un OBJ se convierte a STL y el original se conserva.
func TestToolboxImportMesh(t *testing.T) {
	tb, ws := newToolbox(t, false)
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeASCIISTL(t, filepath.Join(ws, "refs", "soporte.stl"))
	before := scadCount(t, ws)

	// Por defecto no se mueve: queda donde está en el archivo.
	got := tb.mustJSON(t, "import_mesh", map[string]any{"file": "refs/soporte.stl"})
	if got["ref"] != "refs/soporte.stl" || got["converted"] != false || got["centeredOnBed"] != false || got["format"] != "stl" {
		t.Fatalf("import_mesh: %v", got)
	}
	m := got["mesh"].(map[string]any)
	if size := m["sizeMm"].(map[string]any); size["x"] != 20.0 || size["y"] != 10.0 || size["z"] != 5.0 || m["watertight"] != true || m["triangles"] != 12.0 {
		t.Errorf("mesh: %v", m)
	}
	if _, has := got["warnings"]; has {
		t.Errorf("un cubo cerrado que cabe no avisa nada: %v", got["warnings"])
	}
	file := got["file"].(string)
	if !strings.HasSuffix(file, "-soporte.scad") || scadCount(t, ws) != before+1 {
		t.Fatalf("checkpoint: %q", file)
	}
	src := tb.must(t, "read_checkpoint", map[string]any{"file": file}).Text
	if !strings.HasPrefix(src, "// @checkpoint parent= desc=\"Imports soporte.stl\"\n") {
		t.Errorf("cabecera: %q", strings.SplitN(src, "\n", 2)[0])
	}
	for _, want := range []string{`import("refs/soporte.stl", convexity = 10)`, `color("#9ca3af") modelo();`, "@scadlab-mesh"} {
		if !strings.Contains(src, want) {
			t.Errorf("falta %q en:\n%s", want, src)
		}
	}
	if strings.Contains(src, "translate(") {
		t.Error("sin center_on_bed no debe mover el modelo")
	}
	// Aparece en el árbol como raíz.
	list := tb.mustJSON(t, "list_checkpoints", nil)["checkpoints"].([]any)
	var found bool
	for _, c := range list {
		if c.(map[string]any)["file"] == file && c.(map[string]any)["parent"] == "" {
			found = true
		}
	}
	if !found {
		t.Errorf("list_checkpoints: %v", list)
	}

	// Centrado en la placa; el flag también se acepta como texto.
	for _, v := range []any{true, "true"} {
		c := tb.mustJSON(t, "import_mesh", map[string]any{"file": "soporte.stl", "center_on_bed": v})
		if c["centeredOnBed"] != true {
			t.Fatalf("center_on_bed=%v: %v", v, c)
		}
		if s := tb.must(t, "read_checkpoint", map[string]any{"file": c["file"].(string)}).Text; !strings.Contains(s, "translate([-10, -5, 0])") {
			t.Errorf("centrado:\n%s", s)
		}
	}

	// 3MF de dos colores: se convierte a STL, se avisa y el original queda.
	three := filepath.Join(ws, "refs", "colores.3mf")
	write3MFTwoColors(t, three)
	c3 := tb.mustJSON(t, "import_mesh", map[string]any{"file": "refs/colores.3mf"})
	if c3["ref"] != "refs/colores.stl" || c3["converted"] != true || c3["format"] != "3mf" {
		t.Fatalf("3mf: %v", c3)
	}
	if w, _ := c3["warnings"].([]any); len(w) != 1 || !strings.Contains(w[0].(string), "2 colors") {
		t.Errorf("avisos del 3MF: %v", c3["warnings"])
	}
	for _, f := range []string{"colores.3mf", "colores.stl"} {
		if _, err := os.Stat(filepath.Join(ws, "refs", f)); err != nil {
			t.Errorf("falta refs/%s: %v", f, err)
		}
	}
	if s := tb.must(t, "read_checkpoint", map[string]any{"file": c3["file"].(string)}).Text; !strings.Contains(s, `import("refs/colores.stl"`) {
		t.Errorf("el checkpoint debe importar el STL convertido:\n%s", s)
	}
}

// Las rutas hostiles y los archivos que no son modelos se rechazan como en
// inspect_reference, sin crear checkpoints ni tocar nada.
func TestToolboxImportMeshRejectsHostilePaths(t *testing.T) {
	tb, ws := newToolbox(t, false)
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeASCIISTL(t, filepath.Join(ws, "refs", "ok.stl"))
	secret := filepath.Join(ws, "secreto.stl")
	writeASCIISTL(t, secret)
	outside := filepath.Join(t.TempDir(), "afuera.stl")
	writeASCIISTL(t, outside)
	symlinks := os.Symlink(outside, filepath.Join(ws, "refs", "enlace.stl")) == nil
	_ = os.WriteFile(filepath.Join(ws, "refs", "nota.txt"), []byte("hola"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "refs", "roto.stl"), []byte("solid x\nfacet"), 0o644)
	_ = os.WriteFile(filepath.Join(ws, "refs", "roto.obj"), []byte("v 0 0 0\nf 1 2 3\n"), 0o644)
	bad := []string{
		"", "../secreto.stl", "refs/../secreto.stl", "refs/../../secreto.stl", "/etc/passwd", secret, outside, `..\secreto.stl`,
		"C:\\x.stl", "refs/sub/x.stl", "nota.txt", "no-existe.stl", "refs/", ".", "..", ".oculto.stl", "roto.stl", "roto.obj",
		"ok.stl\x00.txt", "001-algo.scad", "refs/ok.stl/../../secreto.stl",
	}
	if symlinks {
		bad = append(bad, "enlace.stl")
	}
	before := scadCount(t, ws)
	refsBefore, _ := os.ReadDir(filepath.Join(ws, "refs"))
	for _, f := range bad {
		out := tb.fails(t, "import_mesh", map[string]any{"file": f})
		if strings.Contains(out.Text, "scadlab-mesh") {
			t.Errorf("file=%q: la respuesta no debe traer un checkpoint", f)
		}
	}
	// El argumento con el tipo equivocado tampoco hace nada raro.
	tb.fails(t, "import_mesh", map[string]any{"file": []any{"refs/ok.stl"}})
	tb.fails(t, "import_mesh", nil)
	if scadCount(t, ws) != before {
		t.Error("se crearon checkpoints con rutas hostiles")
	}
	if refsAfter, _ := os.ReadDir(filepath.Join(ws, "refs")); len(refsAfter) != len(refsBefore) {
		t.Errorf("cambió refs/: %d → %d archivos", len(refsBefore), len(refsAfter))
	}
	// Sin carpeta refs/.
	if err := os.RemoveAll(filepath.Join(ws, "refs")); err != nil {
		t.Fatal(err)
	}
	if out := tb.fails(t, "import_mesh", map[string]any{"file": "ok.stl"}); !strings.Contains(out.Text, "refs") {
		t.Errorf("sin refs/: %q", out.Text)
	}
}

// El checkpoint que crea import_mesh renderiza, centrado, con el OpenSCAD real.
func TestToolboxImportMeshRenders(t *testing.T) {
	needOpenSCAD(t)
	tb, ws := newToolbox(t, false)
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeASCIISTL(t, filepath.Join(ws, "refs", "soporte.stl"))
	file := tb.mustJSON(t, "import_mesh", map[string]any{"file": "soporte.stl", "center_on_bed": true})["file"].(string)
	r := tb.mustJSON(t, "render", map[string]any{"file": file})
	if r["status"] != "ok" {
		t.Fatalf("render: %v", r)
	}
	mm := r["mesh"].(map[string]any)
	min, max := mm["minMm"].(map[string]any), mm["maxMm"].(map[string]any)
	if min["x"] != -10.0 || min["y"] != -5.0 || min["z"] != 0.0 || max["x"] != 10.0 || max["y"] != 5.0 || max["z"] != 5.0 {
		t.Errorf("caja centrada: %v … %v", min, max)
	}
}
