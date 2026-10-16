package mcp

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/detect"
	"scadlab/backend/scadwasm"
)

// Estas pruebas corren el servidor SIN OpenSCAD instalado
// (SCADLAB_NO_SYSTEM_OPENSCAD=1 y un directorio de datos vacío): todo sale del
// motor integrado (OpenSCAD en WebAssembly).

func setupNoNative(t *testing.T) *client {
	t.Helper()
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	return setup(t, nil)
}

const twoColors = "color(\"red\") cube([20, 10, 5]);\ncolor(\"blue\") translate([0, 0, 5]) cube([20, 10, 5]);\n"

func TestWASMRenderMeasureLintExport(t *testing.T) {
	c := setupNoNative(t)

	// printer_info dice qué motor hay.
	info := c.mustOK("printer_info", nil)
	sc, _ := info["openscad"].(map[string]any)
	if sc["found"] != false || sc["engine"] != "wasm" || sc["version"] != scadwasm.Version {
		t.Errorf("printer_info.openscad: %v", sc)
	}

	name := c.create("dos colores", twoColors, nil)
	out := c.mustOK("render", map[string]any{"file": name})
	if out["status"] != "ok" || out["engine"] != "wasm" || out["openscadVersion"] != scadwasm.Version {
		t.Fatalf("render: %v", out)
	}
	if note, _ := out["engineNote"].(string); !strings.Contains(note, "built-in engine") {
		t.Errorf("falta la nota del motor integrado: %v", out)
	}
	m, _ := out["mesh"].(map[string]any)
	if m["volumeMm3"] != 2000.0 || m["watertight"] != true {
		t.Errorf("malla: %v", m)
	}
	if again := c.mustOK("render", nil); again["cacheHit"] != true || again["engine"] != "wasm" {
		t.Errorf("segundo render: %v", again)
	}

	// measure: dos partes por color (vía el 3MF del integrado) y estimación.
	// Cubos separados: cada parte es una malla cerrada con su propio volumen.
	sep := c.create("separados", "color(\"red\") cube(10);\ntranslate([20,0,0]) color(\"blue\") cube(10);\n", nil)
	ms := c.mustOK("measure", map[string]any{"file": sep})
	parts, _ := ms["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("partes: %v", ms["parts"])
	}
	p0, _ := parts[0].(map[string]any)
	p1, _ := parts[1].(map[string]any)
	if p0["color"] != "#FF0000" || p1["color"] != "#0000FF" || p0["volumeMm3"] != 1000.0 || p1["volumeMm3"] != 1000.0 || p0["watertight"] != true {
		t.Errorf("partes: %v %v", p0, p1)
	}
	if w, _ := ms["estimatedWeightG"].(float64); w <= 0 {
		t.Errorf("peso estimado: %v", ms["estimatedWeightG"])
	}

	// lint: error de sintaxis con línea, hecho por el integrado.
	l := c.mustOK("lint", map[string]any{"source": "color(\"red\")\n  cube(1\n"})
	if l["errors"].(float64) < 1 || !strings.Contains(l["note"].(string), "built-in engine") {
		t.Errorf("lint: %v", l)
	}
	for _, d := range l["diagnostics"].([]any) {
		if dm := d.(map[string]any); dm["severity"] == "error" && dm["line"].(float64) < 1 {
			t.Errorf("error sin línea: %v", dm)
		}
	}

	// export: STL cerrado, 3MF con las dos partes y STL por color.
	stl := c.mustOK("export", map[string]any{"file": name, "format": "stl"})
	if st, err := os.Stat(stl["path"].(string)); err != nil || st.Size() < 84 {
		t.Errorf("STL: %v", err)
	}
	tm := c.mustOK("export", map[string]any{"file": name})
	if ps, _ := tm["parts"].([]any); len(ps) != 2 {
		t.Errorf("partes del 3MF: %v", tm["parts"])
	}
	zr, err := zip.OpenReader(tm["path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	has := map[string]bool{}
	for _, f := range zr.File {
		has[f.Name] = true
	}
	zr.Close()
	if !has["3D/3dmodel.model"] || !has["Metadata/model_settings.config"] {
		t.Errorf("3MF sin sus partes: %v", has)
	}
	pc := c.mustOK("export", map[string]any{"file": name, "format": "stl-per-color"})
	if zr, err := zip.OpenReader(pc["path"].(string)); err != nil || len(zr.File) != 2 {
		t.Errorf("stl-per-color: %v", err)
	}

	// Un modelo roto da el error con su línea (y RENDER_ERRORS.md).
	bad := c.create("roto", srcBroken, nil)
	r := c.tool("render", map[string]any{"file": bad})
	if !r.IsError || !strings.Contains(r.Text, "diagnostics") {
		t.Errorf("render roto: %v %q", r.IsError, r.Text)
	}
	if !c.exists("RENDER_ERRORS.md") {
		t.Error("falta RENDER_ERRORS.md")
	}
}

func TestWASMPreviewRastersTheMesh(t *testing.T) {
	c := setupNoNative(t)
	name := c.create("dos colores", twoColors, nil)

	decode := func(r toolResp) (w, h int, colors map[[3]uint8]int) {
		t.Helper()
		var img map[string]any
		for _, ct := range r.Content {
			if ct["type"] == "image" {
				img = ct
			}
		}
		if img == nil || img["mimeType"] != "image/png" {
			t.Fatalf("falta image/png: %v", r.Content)
		}
		raw, err := base64.StdEncoding.DecodeString(img["data"].(string))
		if err != nil {
			t.Fatal(err)
		}
		im, err := png.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		b := im.Bounds()
		colors = map[[3]uint8]int{}
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				r, g, bl, _ := im.At(x, y).RGBA()
				colors[[3]uint8{uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)}]++
			}
		}
		return b.Dx(), b.Dy(), colors
	}

	r := c.tool("preview", map[string]any{"file": name, "view": "front", "size": 300})
	if r.IsError {
		t.Fatalf("preview: %s", r.Text)
	}
	w, h, colors := decode(r)
	if w != 300 || h != 300 {
		t.Errorf("tamaño %dx%d", w, h)
	}
	if !strings.Contains(r.Text, "built-in engine") {
		t.Errorf("el texto no dice que se usó el motor integrado: %q", r.Text)
	}
	// Ocupa buena parte de la imagen y hay rojo y azul (sombreados) además del
	// fondo blanco: el color por parte llegó.
	var red, blue, white int
	for col, n := range colors {
		switch {
		case col == [3]uint8{255, 255, 255}:
			white += n
		case col[0] > 2*col[2] && col[1] < col[0]/2:
			red += n
		case col[2] > 2*col[0] && col[1] < col[2]/2:
			blue += n
		}
	}
	if red < 2000 || blue < 2000 || white < 2000 {
		t.Errorf("píxeles rojos %d, azules %d, fondo %d", red, blue, white)
	}
	// «front» mira desde −Y con +Z arriba: el cubo azul está encima del rojo.
	for _, v := range viewNames {
		if r := c.tool("preview", map[string]any{"file": name, "view": v, "size": 256}); r.IsError {
			t.Errorf("vista %s: %s", v, r.Text)
		}
	}
	if r := c.tool("preview", map[string]any{"file": name, "view": "diagonal-rara"}); !r.IsError {
		t.Error("vista desconocida: quiero isError")
	}

	// Un modelo roto no da imagen: da el error con su línea.
	bad := c.create("roto", srcBroken, nil)
	br := c.tool("preview", map[string]any{"file": bad})
	if !br.IsError || !strings.Contains(br.Text, "line") {
		t.Errorf("preview de un modelo roto: %v %q", br.IsError, br.Text)
	}
	for _, ct := range br.Content {
		if ct["type"] == "image" {
			t.Error("un modelo roto no puede devolver imagen")
		}
	}
	// No deja temporales en el workspace.
	if ents, _ := os.ReadDir(filepath.Join(c.Ws, ".scadlab", "preview")); len(ents) != 0 {
		t.Errorf("temporales del preview: %v", ents)
	}
}
