package mcp

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"

	"scadlab/backend/render"
)

func TestPickDiagsErrorsFirstAndCapped(t *testing.T) {
	var ds []diagOut
	for i := 1; i <= 4; i++ {
		ds = append(ds, diagOut{Severity: render.SeverityWarning, Line: i, Message: "aviso " + itoa(i), Code: "codigo"})
	}
	for i := 5; i <= 9; i++ {
		ds = append(ds, diagOut{Severity: render.SeverityError, Line: i, Message: "error " + itoa(i), Code: "linea " + itoa(i)})
	}
	shown, more := pickDiags(ds, maxShownDiag)
	if len(shown) != 5 || more != 4 {
		t.Fatalf("mostrados %d, restantes %d", len(shown), more)
	}
	for i, d := range shown {
		if d.Severity != render.SeverityError || d.Line != 5+i || d.Code != "linea "+itoa(5+i) {
			t.Errorf("diagnóstico %d: %+v", i, d)
		}
	}
	// El código de la línea solo va en los errores, y lo largo se recorta.
	long := strings.Repeat("x", 500)
	shown, _ = pickDiags([]diagOut{{Severity: render.SeverityWarning, Message: long, Code: "c"}, {Severity: render.SeverityError, Message: long, Code: long}}, 5)
	if len(shown[1].Message) > 4*diagMsgRunes || shown[0].Code != "" && shown[0].Severity == render.SeverityWarning {
		t.Errorf("recorte: %+v", shown)
	}
	if got, more := pickDiags(nil, 5); got == nil || len(got) != 0 || more != 0 {
		t.Errorf("sin diagnósticos debe ser una lista vacía (no null): %v", got)
	}
}

func TestRenderIsCompactByDefaultAndVerboseGivesTheDetail(t *testing.T) {
	needOpenSCAD(t)
	tb, _ := newToolbox(t, false)
	name := tb.create(t, "cubo", "color(\"red\") cube([20, 10, 5]);\ncolor(\"blue\") translate([30, 0, 0]) cube(5);\n", nil)
	out := tb.must(t, "render", map[string]any{"file": name})
	if strings.Contains(out.Text, "\n") || len(out.Text) > 600 {
		t.Errorf("render compacto: %d bytes\n%s", len(out.Text), out.Text)
	}
	var m map[string]any
	if err := jsonUnmarshal(out.Text, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"file", "status", "durationMs", "mesh", "fitsBuildVolume"} {
		if m[k] == nil {
			t.Errorf("falta %s", k)
		}
	}
	for _, k := range []string{"printer", "openscadVersion", "backend", "cacheHit", "engine", "diagnostics"} {
		if m[k] != nil {
			t.Errorf("el resumen no trae %s: %v", k, m[k])
		}
	}
	mm := m["mesh"].(map[string]any)
	if mm["sizeMm"] == nil || mm["minMm"] == nil || mm["maxMm"] == nil || mm["volumeCm3"] == nil || mm["areaMm2"] != nil {
		t.Errorf("malla: %v", mm)
	}
	v := tb.mustJSON(t, "render", map[string]any{"file": name, "verbose": true})
	for _, k := range []string{"printer", "openscadVersion", "backend", "cacheHit", "diagnostics"} {
		if v[k] == nil {
			t.Errorf("verbose sin %s", k)
		}
	}
	// Las partes por color salen cuando ya están en la caché (measure las genera).
	if _, ok := m["parts"]; ok {
		t.Error("sin la caché de partes no hay partes en el resumen")
	}
	ms := tb.mustJSON(t, "measure", map[string]any{"file": name})
	parts := ms["parts"].([]any)
	if len(parts) != 2 || parts[0].(map[string]any)["color"] != "#FF0000" || parts[0].(map[string]any)["sizeMm"] == nil || parts[0].(map[string]any)["g"] == nil {
		t.Errorf("measure compacto, partes: %v", ms["parts"])
	}
	if ms["estimate"] != nil || ms["printer"] != nil || ms["material"] != "PLA" || ms["estimatedWeightG"] == nil {
		t.Errorf("measure compacto: %v", ms)
	}
	again := tb.mustJSON(t, "render", map[string]any{"file": name})
	ps, _ := again["parts"].([]any)
	if len(ps) != 2 || ps[0].(map[string]any)["color"] != "#FF0000" || ps[1].(map[string]any)["cm3"] == nil {
		t.Errorf("tras measure, render trae las partes cacheadas: %v", again["parts"])
	}
}

// Un render con muchos avisos trae 5 y dice cuántos más hay.
func TestRenderSummaryCapsDiagnostics(t *testing.T) {
	needOpenSCAD(t)
	tb, _ := newToolbox(t, false)
	var b strings.Builder
	b.WriteString("color(\"red\") cube(1);\n")
	for i := 1; i <= 8; i++ {
		b.WriteString("modulo_que_no_existe_" + itoa(i) + "();\n")
	}
	name := tb.create(t, "avisos", b.String(), nil)
	m := tb.mustJSON(t, "render", map[string]any{"file": name})
	ds, _ := m["diagnostics"].([]any)
	if len(ds) != 5 || m["moreDiagnostics"] != 3.0 {
		t.Errorf("avisos: %d y más %v", len(ds), m["moreDiagnostics"])
	}
	v := tb.mustJSON(t, "render", map[string]any{"file": name, "verbose": true})
	if vd, _ := v["diagnostics"].([]any); len(vd) != 8 {
		t.Errorf("verbose trae todos: %d", len(vd))
	}
}

func TestLintIsCompactAndCapped(t *testing.T) {
	needOpenSCAD(t)
	tb, _ := newToolbox(t, false)
	src := strings.Repeat("cube(1);\n", 8)
	m := tb.mustJSON(t, "lint", map[string]any{"source": src})
	ds, _ := m["diagnostics"].([]any)
	if m["warnings"] != 8.0 || len(ds) != 5 || m["moreDiagnostics"] != 3.0 {
		t.Errorf("lint compacto: %v", m)
	}
	if d := ds[0].(map[string]any); d["code"] != "SL003" || d["line"] == nil || d["message"] == nil {
		t.Errorf("diagnóstico: %v", d)
	}
	if v := tb.mustJSON(t, "lint", map[string]any{"source": src, "verbose": true}); len(v["diagnostics"].([]any)) != 8 {
		t.Errorf("verbose trae los 8: %v", v["diagnostics"])
	}
}

// preview: una vista de 384 px por defecto; views y all piden más.
func TestPreviewDefaultsToOneSmallView(t *testing.T) {
	needOpenSCAD(t)
	tb, _ := newToolbox(t, false)
	name := tb.create(t, "cubo", srcCube, nil)
	side := func(img ToolImage) int {
		cfg, err := png.DecodeConfig(bytes.NewReader(img.Data))
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Width != cfg.Height {
			t.Errorf("imagen no cuadrada: %dx%d", cfg.Width, cfg.Height)
		}
		return cfg.Width
	}
	one := tb.must(t, "preview", map[string]any{"file": name})
	if len(one.Images) != 1 || side(one.Images[0]) != 384 || !strings.Contains(one.Text, "iso view") {
		t.Fatalf("por defecto: %d imágenes, texto %q", len(one.Images), one.Text)
	}
	two := tb.must(t, "preview", map[string]any{"file": name, "views": []any{"front", "top", "front"}, "size": 256})
	if len(two.Images) != 2 || side(two.Images[0]) != 256 || !strings.Contains(two.Text, "front view") || !strings.Contains(two.Text, "top view") {
		t.Errorf("views: %d imágenes, texto %q", len(two.Images), two.Text)
	}
	// La lista como texto JSON también.
	if o := tb.must(t, "preview", map[string]any{"file": name, "views": `["left"]`, "size": 256}); len(o.Images) != 1 {
		t.Errorf("views como texto: %d", len(o.Images))
	}
	all := tb.must(t, "preview", map[string]any{"file": name, "all": true, "size": 256})
	if len(all.Images) != len(viewNames) {
		t.Errorf("all: %d imágenes, esperaba %d", len(all.Images), len(viewNames))
	}
	// Errores.
	tb.fails(t, "preview", map[string]any{"file": name, "views": []any{"diagonal"}})
	tb.fails(t, "preview", map[string]any{"file": name, "views": []any{}})
	tb.fails(t, "preview", map[string]any{"file": name, "views": []any{"iso", "iso-back", "front", "back", "right", "left", "top", "bottom", "iso"}})
	// El tamaño pedido sigue mandando.
	if o := tb.must(t, "preview", map[string]any{"file": name, "size": 640}); side(o.Images[0]) != 640 {
		t.Error("size=640")
	}
	// La exportación PNG conserva su tamaño de siempre.
	exp := tb.mustJSON(t, "export", map[string]any{"file": name, "format": "png"})
	if exp["sizePx"] != float64(defaultPreviewSize) {
		t.Errorf("export png: %v", exp["sizePx"])
	}
	// La imagen es base64 de un PNG válido (servidor).
	_ = base64.StdEncoding
}

// update_checkpoint también es compacto, y verbose lo trae completo.
func TestUpdateCheckpointIsCompact(t *testing.T) {
	needOpenSCAD(t)
	tb, _ := newToolbox(t, false)
	name := tb.create(t, "caja", srcCube, nil)
	out := tb.must(t, "update_checkpoint", map[string]any{"file": name, "source": srcBigger})
	if len(out.Text) > 900 || strings.Contains(out.Text, "\n") || strings.Contains(out.Text, `"next"`) {
		t.Errorf("update compacto: %d bytes\n%s", len(out.Text), out.Text)
	}
	if !strings.HasPrefix(out.Text, `{"file":"`+name+`","updated":true`) {
		t.Errorf("`updated` debe ir entre las primeras claves: %s", out.Text[:80])
	}
	v := tb.mustJSON(t, "update_checkpoint", map[string]any{"file": name, "source": srcCube, "verbose": true})
	if v["next"] == nil || v["render"].(map[string]any)["printer"] == nil {
		t.Errorf("verbose: %v", v)
	}
}
