package mcp

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Los argumentos con el nombre equivocado que mandaron Gemini (Antigravity) y
// modelos chicos en sesiones reales del usuario: antes cada uno era un «falta
// `x`» y una vuelta perdida.
func TestToolsAcceptArgumentAliasesFromRealSessions(t *testing.T) {
	tb, ws := newToolbox(t, true)
	src := "// @checkpoint parent= desc=\"maceta\"\nmodule detalles_decorativos_curvados() { cube(1); }\ndetalles_decorativos_curvados();\n"
	if err := os.WriteFile(filepath.Join(ws, "001-maceta.scad"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := 0; i < 8; i++ {
		img.Set(i, i, color.Black)
	}
	if err := os.MkdirAll(filepath.Join(ws, "refs", "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(ws, "refs", "images", "captura.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"image_info", map[string]any{"file": "refs/images/captura.png"}},
		{"find_in_checkpoint", map[string]any{"file": "001-maceta.scad", "pattern": "detalles_decorativos_curvados"}},
		{"read_checkpoint", map[string]any{"checkpoint": "001-maceta.scad", "start": 1, "end": 2}},
		{"read_skill", map[string]any{"skill": "openscad-core"}},
	}
	for _, c := range cases {
		out, err := tb.Call(t.Context(), c.tool, c.args)
		if err != nil {
			t.Fatalf("%s: %v", c.tool, err)
		}
		low := strings.ToLower(out.Text)
		if strings.Contains(low, "is required") || strings.Contains(low, "falta `") {
			t.Errorf("%s con %v sigue pidiendo el argumento: %s", c.tool, c.args, out.Text)
		}
	}
}

func TestNormalizeArgsKeepsDeclaredAndPresentArgs(t *testing.T) {
	tool := &Tool{Name: "x", InputSchema: map[string]any{"properties": map[string]any{
		"file": map[string]any{}, "image": map[string]any{}, "query": map[string]any{}, "from_line": map[string]any{},
	}}}
	got := normalizeArgs(tool, map[string]any{"file": "a.scad", "pattern": "p", "query": "q", "From-Line": 3})
	if got["file"] != "a.scad" || got["image"] != nil {
		t.Errorf("un sinónimo que la herramienta declara no se mueve: %v", got)
	}
	if got["query"] != "q" {
		t.Errorf("un argumento presente no se pisa: %v", got)
	}
	if got["from_line"] != 3 {
		t.Errorf("capitalización/guiones: %v", got)
	}
	got = normalizeArgs(tool, map[string]any{"query": "", "pattern": "p"})
	if got["query"] != "p" {
		t.Errorf("un \"\" no tapa al sinónimo: %v", got)
	}
}
