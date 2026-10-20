package mcp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"scadlab/backend/imagetools"
)

// --- fixtures -------------------------------------------------------------------

func paintPNG(w, h int, f func(x, y int) color.NRGBA) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, f(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

var (
	cWhite = color.NRGBA{255, 255, 255, 255}
	cBlack = color.NRGBA{0, 0, 0, 255}
)

// putImage guarda una imagen en <ws>/refs/images/ y devuelve su ruta relativa.
func putImage(t *testing.T, ws, name string, data []byte) string {
	t.Helper()
	dir := filepath.Join(ws, "refs", "images")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return "refs/images/" + name
}

// disc: círculo negro de radio r px sobre blanco.
func discPNG(w, h int, r float64) []byte {
	return paintPNG(w, h, func(x, y int) color.NRGBA {
		if math.Hypot(float64(x)+0.5-float64(w)/2, float64(y)+0.5-float64(h)/2) <= r {
			return cBlack
		}
		return cWhite
	})
}

func jsonOf(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("no es JSON: %v\n%s", err, truncate(s))
	}
	return m
}

func num(t *testing.T, m map[string]any, path ...string) float64 {
	t.Helper()
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("%v: %q no es un objeto en %v", path, p, cur)
		}
		cur = mm[p]
	}
	f, ok := cur.(float64)
	if !ok {
		t.Fatalf("%v no es un número: %v", path, cur)
	}
	return f
}

// --- image_info -------------------------------------------------------------------

func TestImageInfo(t *testing.T) {
	tb, ws := newToolbox(t, false)
	// Pieza roja de 100×60 sobre fondo gris claro, en una foto de 400×300.
	rel := putImage(t, ws, "pieza.png", paintPNG(400, 300, func(x, y int) color.NRGBA {
		if x >= 150 && x < 250 && y >= 120 && y < 180 {
			return color.NRGBA{200, 30, 30, 255}
		}
		return color.NRGBA{235, 235, 235, 255}
	}))

	out := tb.must(t, "image_info", map[string]any{"image": rel})
	if len(out.Images) != 1 || !bytes.HasPrefix(out.Images[0].Data, pngMagic) || out.Images[0].MIME != "image/png" {
		t.Fatalf("image_info debe devolver una imagen PNG: %+v", out.Images)
	}
	m := jsonOf(t, out.Text)
	if m["image"] != rel || m["format"] != "png" || num(t, m, "widthPx") != 400 || num(t, m, "heightPx") != 300 || math.Abs(num(t, m, "aspect")-1.333) > 0.001 {
		t.Errorf("ficha: %v", m)
	}
	fg := m["foreground"].(map[string]any)
	box := fg["box"].([]any)
	if math.Abs(box[0].(float64)-0.375) > 0.01 || math.Abs(box[2].(float64)-0.25) > 0.01 || math.Abs(fg["aspect"].(float64)-1.667) > 0.06 {
		t.Errorf("primer plano: %v", fg)
	}
	if m["background"].(map[string]any)["hex"] != "#EBEBEB" {
		t.Errorf("fondo: %v", m["background"])
	}
	dom := m["dominantColors"].([]any)
	if len(dom) < 2 || dom[0].(map[string]any)["hex"] != "#EBEBEB" {
		t.Errorf("colores dominantes: %v", dom)
	}

	// Con región: se amplía y se realza por defecto.
	out = tb.must(t, "image_info", map[string]any{"image": rel, "region": []any{0.375, 0.4, 0.25, 0.2}})
	m = jsonOf(t, out.Text)
	shown := m["shown"].(map[string]any)
	if shown["enhanced"] != true || num(t, shown, "widthPx") < 300 {
		t.Errorf("región: %v", shown)
	}
	im, err := png.Decode(bytes.NewReader(out.Images[0].Data))
	if err != nil || im.Bounds().Dx() != int(num(t, shown, "widthPx")) {
		t.Errorf("la imagen devuelta no mide lo que dice: %v", err)
	}
	// ... y se puede pedir sin realce.
	if shown := jsonOf(t, tb.must(t, "image_info", map[string]any{"image": rel, "region": "[0,0,0.5,0.5]", "enhance": "false"}).Text)["shown"].(map[string]any); shown["enhanced"] != false {
		t.Errorf("enhance=false: %v", shown)
	}

	// Errores.
	for name, args := range map[string]map[string]any{
		"sin imagen":        {},
		"región inválida":   {"image": rel, "region": []any{0, 0, 2, 2}},
		"región corta":      {"image": rel, "region": []any{0, 0, 1}},
		"región con texto":  {"image": rel, "region": []any{"a", 0, 1, 1}},
		"no existe":         {"image": "refs/images/nada.png"},
		"es un modelo 3D":   {"image": "refs/pieza.stl"},
		"un directorio":     {"image": "refs/images"},
		"vacío con espacio": {"image": "  "},
	} {
		if out := tb.fails(t, "image_info", args); out.Text == "" {
			t.Errorf("%s: sin mensaje", name)
		}
	}
}

// Rutas hostiles: solo se leen imágenes de refs/ y de refs/images/.
func TestImageToolsRejectHostilePaths(t *testing.T) {
	tb, ws := newToolbox(t, false)
	putImage(t, ws, "ok.png", discPNG(40, 40, 10))
	// Una imagen válida FUERA de refs/ (en la raíz del workspace y fuera del workspace).
	data := discPNG(40, 40, 10)
	if err := os.WriteFile(filepath.Join(ws, "raiz.png"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "fuera.png")
	if err := os.WriteFile(outside, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "refs", "images", "enlace.png")); err != nil {
		t.Logf("sin enlaces simbólicos: %v", err)
	}

	hostile := []string{
		"../raiz.png", "refs/../raiz.png", "raiz.png", "/etc/passwd", outside, "refs/images/../../raiz.png",
		"C:\\Windows\\x.png", "refs/images/enlace.png", ".scadlab/x.png", "refs/images/ok.png\x00",
	}
	for _, tool := range []string{"image_info", "compare_with_image", "trace_image_outline", "image_to_heightmap"} {
		for _, p := range hostile {
			args := map[string]any{"image": p, "width_mm": 20, "max_height_mm": 2}
			out, err := tb.Call(context.Background(), tool, args)
			if err != nil {
				t.Fatalf("%s %q: %v", tool, p, err)
			}
			if !out.IsError {
				t.Errorf("%s aceptó la ruta hostil %q", tool, p)
			}
		}
	}
	// Nada se escribió fuera de refs/.
	if _, err := os.Stat(filepath.Join(ws, "refs", "images", "ok.svg")); err == nil {
		t.Error("se escribió junto a la imagen")
	}
}

func pngHeaderOnly(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 0
	chunk := append([]byte("IHDR"), ihdr...)
	_ = binary.Write(&b, binary.BigEndian, uint32(13))
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

// Una imagen de más de 40 MP se rechaza leyendo solo su cabecera; una de más de
// 20 MB, sin leerla.
func TestImageToolsEnforceLimits(t *testing.T) {
	tb, ws := newToolbox(t, false)
	putImage(t, ws, "gigante.png", pngHeaderOnly(9000, 9000))
	out := tb.fails(t, "image_info", map[string]any{"image": "refs/images/gigante.png"})
	if !strings.Contains(out.Text, "too many pixels") {
		t.Errorf("límite de píxeles: %s", out.Text)
	}
	putImage(t, ws, "pesada.png", make([]byte, imagetools.MaxBytes+1))
	out = tb.fails(t, "trace_image_outline", map[string]any{"image": "pesada.png", "width_mm": 10})
	if !strings.Contains(out.Text, "too large") {
		t.Errorf("límite de bytes: %s", out.Text)
	}
	putImage(t, ws, "texto.png", []byte("no soy una imagen"))
	out = tb.fails(t, "image_to_heightmap", map[string]any{"image": "texto.png", "width_mm": 10, "max_height_mm": 1})
	if !strings.Contains(out.Text, "unrecognized format") {
		t.Errorf("contenido no imagen: %s", out.Text)
	}
}

// --- trace_image_outline ------------------------------------------------------------

func TestTraceImageOutline(t *testing.T) {
	tb, ws := newToolbox(t, false)
	rel := putImage(t, ws, "disco.png", discPNG(400, 400, 100))

	m := tb.mustJSON(t, "trace_image_outline", map[string]any{"image": rel, "width_mm": 50})
	layers := m["perLayer"].([]any)
	if len(layers) != 1 || num(t, m, "layers") != 1 {
		t.Fatalf("capas: %v", layers)
	}
	l := layers[0].(map[string]any)
	if files := m["files"].([]any); len(files) != 1 || files[0] != "refs/disco-outline.svg" || num(t, m, "contours") != 1 || num(t, m, "holes") != 0 {
		t.Errorf("resumen: %v %v", m["files"], m["contours"])
	}
	if a := num(t, m, "areaMm2"); a != l["areaMm2"].(float64) {
		t.Errorf("área total %v, de la capa %v", a, l["areaMm2"])
	}
	if !strings.Contains(m["snippet"].(string), "import(") {
		t.Errorf("snippet: %v", m["snippet"])
	}
	file := l["file"].(string)
	if file != "refs/disco-outline.svg" {
		t.Errorf("archivo: %s", file)
	}
	if a := l["areaMm2"].(float64); math.Abs(a-math.Pi*625)/(math.Pi*625) > 0.02 {
		t.Errorf("área = %v, esperaba %.1f ±2 %%", a, math.Pi*625)
	}
	if num(t, m, "sizeMm", "width") < 49.5 || num(t, m, "sizeMm", "width") > 50.5 {
		t.Errorf("ancho: %v", m["sizeMm"])
	}
	if !strings.Contains(l["snippet"].(string), `import("refs/disco-outline.svg")`) || !strings.Contains(l["snippet"].(string), "linear_extrude") {
		t.Errorf("snippet: %v", l["snippet"])
	}
	if m["shape"] != "dark" || m["inverted"] != false || m["threshold"] == nil {
		t.Errorf("forma: %v %v", m["shape"], m["threshold"])
	}
	svg, err := os.ReadFile(filepath.Join(ws, "refs", "disco-outline.svg"))
	if err != nil || !strings.Contains(string(svg), `fill-rule="evenodd"`) || !strings.Contains(string(svg), `width="50mm"`) {
		t.Errorf("SVG: %v\n%.200s", err, svg)
	}
	// La imagen de la forma trazada acompaña al resultado.
	out := tb.must(t, "trace_image_outline", map[string]any{"image": rel, "height_mm": 10, "name": "Mi disco"})
	if len(out.Images) != 1 || !bytes.HasPrefix(out.Images[0].Data, pngMagic) {
		t.Errorf("sin imagen de la forma trazada: %+v", out.Images)
	}
	m2 := jsonOf(t, out.Text)
	if f := m2["perLayer"].([]any)[0].(map[string]any)["file"]; f != "refs/mi-disco.svg" {
		t.Errorf("name: %v", f)
	}

	// Nunca pisa: el mismo nombre otra vez agrega -2 y -3, y el primero sigue igual.
	before, _ := os.ReadFile(filepath.Join(ws, "refs", "disco-outline.svg"))
	f2 := tb.mustJSON(t, "trace_image_outline", map[string]any{"image": rel, "width_mm": 30})["perLayer"].([]any)[0].(map[string]any)["file"]
	f3 := tb.mustJSON(t, "trace_image_outline", map[string]any{"image": rel, "width_mm": 20})["perLayer"].([]any)[0].(map[string]any)["file"]
	if f2 != "refs/disco-outline-2.svg" || f3 != "refs/disco-outline-3.svg" {
		t.Errorf("sin pisar: %v %v", f2, f3)
	}
	if after, _ := os.ReadFile(filepath.Join(ws, "refs", "disco-outline.svg")); !bytes.Equal(before, after) {
		t.Error("se pisó un archivo existente")
	}

	// Un nombre malicioso no sale de refs/.
	f4 := tb.mustJSON(t, "trace_image_outline", map[string]any{"image": rel, "width_mm": 20, "name": "../../escape"})["perLayer"].([]any)[0].(map[string]any)["file"].(string)
	if strings.Contains(f4, "..") || strings.Count(f4, "/") != 1 {
		t.Errorf("name hostil: %s", f4)
	}
	if _, err := os.Stat(filepath.Join(ws, "escape.svg")); err == nil {
		t.Error("escribió fuera de refs/")
	}

	// Errores de uso.
	for name, args := range map[string]map[string]any{
		"sin escala":      {"image": rel},
		"capas de más":    {"image": rel, "width_mm": 10, "layers": 9},
		"escala negativa": {"image": rel, "width_mm": -4},
		"umbral absurdo":  {"image": rel, "width_mm": 10, "threshold": 999},
	} {
		if out := tb.fails(t, "trace_image_outline", args); out.Text == "" {
			t.Errorf("%s: sin mensaje", name)
		}
	}
}

func TestTraceImageOutlineLayers(t *testing.T) {
	tb, ws := newToolbox(t, false)
	red, blue := color.NRGBA{220, 30, 30, 255}, color.NRGBA{20, 40, 200, 255}
	rel := putImage(t, ws, "logo.png", paintPNG(500, 250, func(x, y int) color.NRGBA {
		switch {
		case x >= 40 && x < 200 && y >= 45 && y < 205:
			return red
		case math.Hypot(float64(x)-350, float64(y)-125) <= 80:
			return blue
		}
		return cWhite
	}))
	m := tb.mustJSON(t, "trace_image_outline", map[string]any{"image": rel, "mm_per_px": 0.1, "layers": 2})
	layers := m["perLayer"].([]any)
	if len(layers) != 2 || num(t, m, "layers") != 2 || len(m["files"].([]any)) != 2 {
		t.Fatalf("capas: %v", layers)
	}
	files := map[string]bool{}
	for _, it := range layers {
		l := it.(map[string]any)
		files[l["file"].(string)] = true
		if l["color"] == nil || !strings.Contains(l["snippet"].(string), l["color"].(string)[1:3]) && !strings.Contains(strings.ToLower(l["snippet"].(string)), strings.ToLower(l["color"].(string))) {
			t.Errorf("la capa debe traer su color en el snippet: %v", l)
		}
		if _, err := os.Stat(filepath.Join(ws, filepath.FromSlash(l["file"].(string)))); err != nil {
			t.Errorf("falta %s", l["file"])
		}
	}
	if len(files) != 2 {
		t.Errorf("dos capas, dos archivos distintos: %v", files)
	}
	if num(t, m, "mmPerPx") != 0.1 {
		t.Errorf("mmPerPx: %v", m["mmPerPx"])
	}
}

// --- image_to_heightmap ----------------------------------------------------------------

func TestImageToHeightmap(t *testing.T) {
	tb, ws := newToolbox(t, false)
	rel := putImage(t, ws, "degradado.png", paintPNG(256, 128, func(x, y int) color.NRGBA {
		return color.NRGBA{uint8(x), uint8(x), uint8(x), 255}
	}))
	out := tb.must(t, "image_to_heightmap", map[string]any{"image": rel, "width_mm": 80, "max_height_mm": 3})
	m := jsonOf(t, out.Text)
	if m["file"] != "refs/degradado-heightmap.dat" || num(t, m, "columns") != 200 || num(t, m, "rows") != 100 {
		t.Errorf("resultado: %v", m)
	}
	if num(t, m, "sizeMm", "height") != 40 || num(t, m, "sizeMm", "base") != 0.6 || num(t, m, "sizeMm", "maxThickness") != 3.6 {
		t.Errorf("medidas: %v", m["sizeMm"])
	}
	if num(t, m, "valueRange", "min") > 1 || num(t, m, "valueRange", "max") < 99 {
		t.Errorf("rango: %v", m["valueRange"])
	}
	snip := m["snippet"].(string)
	for _, want := range []string{`surface(file = "refs/degradado-heightmap.dat", center = true)`, "scale([", "intersection()"} {
		if !strings.Contains(snip, want) {
			t.Errorf("el snippet no contiene %q:\n%s", want, snip)
		}
	}
	if len(out.Images) != 1 {
		t.Errorf("imagen del mapa: %d", len(out.Images))
	}
	dat, err := os.ReadFile(filepath.Join(ws, "refs", "degradado-heightmap.dat"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(dat)), "\n")
	if len(lines) != 101 || len(strings.Fields(lines[1])) != 200 {
		t.Errorf(".dat: %d líneas", len(lines))
	}

	// invert, resolution y un nombre repetido (nunca pisa).
	m = tb.mustJSON(t, "image_to_heightmap", map[string]any{"image": rel, "width_mm": 80, "max_height_mm": 3, "invert": true, "resolution": 50, "base_mm": 1.2, "height_mm": 30})
	if m["file"] != "refs/degradado-heightmap-2.dat" || num(t, m, "columns") != 50 || m["invert"] != true || num(t, m, "sizeMm", "base") != 1.2 || num(t, m, "sizeMm", "height") != 30 {
		t.Errorf("segundo mapa: %v", m)
	}
	if num(t, m, "valueRange", "max") < 99 {
		t.Errorf("invertido debía invertir los valores: %v", m["valueRange"])
	}

	for name, args := range map[string]map[string]any{
		"sin ancho":      {"image": rel, "max_height_mm": 2},
		"sin altura":     {"image": rel, "width_mm": 20},
		"resolución":     {"image": rel, "width_mm": 20, "max_height_mm": 2, "resolution": 900},
		"ancho negativo": {"image": rel, "width_mm": -1, "max_height_mm": 2},
	} {
		if out := tb.fails(t, "image_to_heightmap", args); out.Text == "" {
			t.Errorf("%s: sin mensaje", name)
		}
	}
}

// --- list_refs ---------------------------------------------------------------------------

func TestListRefsIncludesImages(t *testing.T) {
	tb, ws := newToolbox(t, false)
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeASCIISTL(t, filepath.Join(ws, "refs", "pieza.stl"))
	putImage(t, ws, "foto-ab12cd34.jpg", discPNG(20, 20, 5))
	m := tb.mustJSON(t, "list_refs", nil)
	var kinds []string
	for _, it := range m["refs"].([]any) {
		r := it.(map[string]any)
		kinds = append(kinds, r["kind"].(string)+":"+r["path"].(string))
	}
	slices.Sort(kinds)
	want := []string{"image:refs/images/foto-ab12cd34.jpg", "model:refs/pieza.stl"}
	if !slices.Equal(kinds, want) {
		t.Errorf("list_refs: %v, esperaba %v", kinds, want)
	}
	// Solo imágenes: la nota de «vacío» no aparece.
	tb2, ws2 := newToolbox(t, false)
	putImage(t, ws2, "sola.png", discPNG(20, 20, 5))
	if m := tb2.mustJSON(t, "list_refs", nil); m["note"] != nil {
		t.Errorf("con una imagen, refs/ no está vacío: %v", m["note"])
	}
	// Una imagen no es un modelo 3D para inspect_reference.
	if out := tb.fails(t, "inspect_reference", map[string]any{"file": "refs/images/foto-ab12cd34.jpg"}); !strings.Contains(out.Text, "image") {
		t.Errorf("inspect_reference con una imagen: %s", out.Text)
	}
}

// --- compare_with_image -----------------------------------------------------------------------

// needRenderEngine salta la prueba si no hay con qué renderizar (ni OpenSCAD ni el
// motor integrado).
func needRenderEngine(t *testing.T, tb *Toolbox) {
	t.Helper()
	if tool := tb.e.openscad(context.Background(), ""); !tool.Found && !tb.e.wasm.Available() {
		t.Skip("sin OpenSCAD ni motor integrado")
	}
}

// silhouettePNG dibuja una silueta negra sobre blanco: pieza(x, z) en unidades
// del modelo, con z hacia arriba y `px` píxeles por unidad, en un lienzo de w×h.
func silhouettePNG(w, h int, px float64, ox, oz float64, inside func(x, z float64) bool) []byte {
	return paintPNG(w, h, func(x, y int) color.NRGBA {
		mx := (float64(x) + 0.5 - ox) / px
		mz := (float64(h) - float64(y) - 0.5 - oz) / px
		if inside(mx, mz) {
			return cBlack
		}
		return cWhite
	})
}

const srcLBracket = "color(\"red\") union() { cube([40, 10, 10]); cube([10, 10, 40]); }\n"
const srcLongBox = "color(\"red\") cube([60, 20, 30]);\n"

func TestCompareWithImagePicksTheMatchingView(t *testing.T) {
	tb, ws := newToolbox(t, false)
	needRenderEngine(t, tb)

	// Un soporte en L visto de frente (desde −Y): X a la derecha, Z arriba, el
	// montante a la izquierda. La vista de atrás es su espejo; los costados y la
	// de arriba son rectángulos.
	l := putImage(t, ws, "l-frente.png", silhouettePNG(400, 400, 5, 100, 50, func(x, z float64) bool {
		return (x >= 0 && x <= 40 && z >= 0 && z <= 10) || (x >= 0 && x <= 10 && z >= 0 && z <= 40)
	}))
	file := tb.create(t, "soporte L", srcLBracket, nil)

	out := tb.must(t, "compare_with_image", map[string]any{"image": l, "file": file})
	m := jsonOf(t, out.Text)
	if m["view"] != "front" || m["autoSelected"] != true || m["mode"] != "side" || m["file"] != file {
		t.Fatalf("esperaba front elegida a solas: %v", m)
	}
	if iou := num(t, m, "silhouetteIoU"); iou < 0.9 {
		t.Errorf("IoU de la vista correcta = %.3f, esperaba ≥ 0,9", iou)
	}
	rank := m["viewRanking"].([]any)
	if len(rank) != 3 || rank[0].(map[string]any)["view"] != "front" || rank[0].(map[string]any)["iou"].(float64) <= rank[1].(map[string]any)["iou"].(float64) {
		t.Errorf("ranking: %v", rank)
	}
	if d := num(t, m, "proportions", "differencePercent"); math.Abs(d) > 6 {
		t.Errorf("las proporciones del modelo y la foto coinciden (dif. %.1f %%)", d)
	}
	if pa, ma := num(t, m, "proportions", "photo"), num(t, m, "proportions", "model"); math.Abs(pa-1) > 0.05 || math.Abs(ma-1) > 0.05 {
		t.Errorf("proporciones: foto %.3f, modelo %.3f (ambas cuadradas)", pa, ma)
	}
	if m["advice"] == "" {
		t.Error("sin consejo")
	}

	// La imagen: foto a la izquierda (negra donde está la pieza) y modelo a la derecha (rojo).
	if len(out.Images) != 1 {
		t.Fatalf("imágenes: %d", len(out.Images))
	}
	pic, err := png.Decode(bytes.NewReader(out.Images[0].Data))
	if err != nil {
		t.Fatal(err)
	}
	b := pic.Bounds()
	if b.Dx() != 3*8+2*560 || b.Dy() != 2*8+560 {
		t.Errorf("tamaño de la composición: %v", b)
	}
	// Foto a la izquierda (píxeles negros, sin rojo) y modelo a la derecha (rojo).
	var darkLeft, redRight int
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := pic.At(x, y).RGBA()
			r, g, bl = r>>8, g>>8, bl>>8
			switch {
			case x < b.Dx()/2 && r < 40 && g < 40 && bl < 40:
				darkLeft++
			case x >= b.Dx()/2 && r > 150 && g < 110 && bl < 110:
				redRight++
			case x < b.Dx()/2 && r > 150 && g < 110 && bl < 110:
				t.Fatalf("hay rojo del modelo en la mitad de la foto (%d,%d)", x, y)
			}
		}
	}
	if darkLeft < 5000 || redRight < 5000 {
		t.Errorf("la mitad izquierda debía ser la foto (negra: %d px) y la derecha el modelo (rojo: %d px)", darkLeft, redRight)
	}
	// Los dos objetos ocupan lo mismo en su panel (se recortan alrededor del objeto).
	if ratio := float64(darkLeft) / float64(redRight); ratio < 0.7 || ratio > 1.4 {
		t.Errorf("la foto y el modelo deberían verse del mismo tamaño: negro %d vs rojo %d", darkLeft, redRight)
	}

	// Una vista pedida se respeta (y su IoU es peor); sin ranking.
	m2 := tb.mustJSON(t, "compare_with_image", map[string]any{"image": l, "file": file, "view": "top"})
	if m2["view"] != "top" || m2["autoSelected"] != false || m2["viewRanking"] != nil || num(t, m2, "silhouetteIoU") > 0.6 {
		t.Errorf("vista top pedida: %v", m2)
	}
	// Una cámara libre.
	m3 := tb.mustJSON(t, "compare_with_image", map[string]any{"image": l, "file": file, "rotate": []any{90, 0, 0}, "ortho": true})
	if !strings.HasPrefix(m3["view"].(string), "custom-90_0_0") || num(t, m3, "silhouetteIoU") < 0.9 {
		t.Errorf("rotate libre: %v", m3)
	}

	// Superpuesta: una sola imagen cuadrada.
	ov := tb.must(t, "compare_with_image", map[string]any{"image": l, "file": file, "mode": "overlay"})
	if m := jsonOf(t, ov.Text); m["mode"] != "overlay" || m["view"] != "front" {
		t.Errorf("overlay: %v", m)
	}
	if pic, err := png.Decode(bytes.NewReader(ov.Images[0].Data)); err != nil || pic.Bounds().Dx() != pic.Bounds().Dy() {
		t.Errorf("la superposición es una sola imagen cuadrada: %v", err)
	}
}

// Un cubo alargado fotografiado de frente: el auto elige la frontal (o la trasera,
// idéntica) y no la de arriba ni las de un costado; y mide la diferencia de proporción.
func TestCompareWithImageElongatedBox(t *testing.T) {
	tb, ws := newToolbox(t, false)
	needRenderEngine(t, tb)
	// 60×30 de frente: 240×120 px; la foto lo deja más ancho (aspect 2,4 vs 2).
	rel := putImage(t, ws, "caja.png", silhouettePNG(400, 300, 4, 80, 90, func(x, z float64) bool {
		return x >= 0 && x <= 60 && z >= 0 && z <= 30
	}))
	file := tb.create(t, "caja larga", srcLongBox, nil)
	m := tb.mustJSON(t, "compare_with_image", map[string]any{"image": rel, "file": file})
	if v := m["view"]; v != "front" && v != "back" {
		t.Errorf("vista elegida: %v (ranking %v)", v, m["viewRanking"])
	}
	if iou := num(t, m, "silhouetteIoU"); iou < 0.95 {
		t.Errorf("IoU = %.3f", iou)
	}
	if pa := num(t, m, "proportions", "photo"); math.Abs(pa-2) > 0.05 {
		t.Errorf("proporción de la foto = %.3f, esperaba 2", pa)
	}

	// Foto más alta que el modelo: el IoU baja y la diferencia de proporciones se informa con signo.
	tall := putImage(t, ws, "alta.png", silhouettePNG(400, 300, 4, 80, 20, func(x, z float64) bool {
		return x >= 0 && x <= 60 && z >= 0 && z <= 60
	}))
	m = tb.mustJSON(t, "compare_with_image", map[string]any{"image": tall, "file": file, "view": "front"})
	if d := num(t, m, "proportions", "differencePercent"); d < 40 {
		t.Errorf("el modelo (2:1) es mucho más ancho que la foto (1:1): dif. %.1f %%", d)
	}
	if !strings.Contains(m["advice"].(string), "wider") {
		t.Errorf("consejo: %v", m["advice"])
	}

	// Errores: sin checkpoint, vista desconocida, modo desconocido, imagen que no existe.
	tb.fails(t, "compare_with_image", map[string]any{"image": rel, "file": "999-no.scad"})
	tb.fails(t, "compare_with_image", map[string]any{"image": rel, "file": file, "view": "lateral"})
	tb.fails(t, "compare_with_image", map[string]any{"image": rel, "file": file, "mode": "raro"})
	tb.fails(t, "compare_with_image", map[string]any{"image": "refs/images/nada.png", "file": file})
	// Un modelo roto no se compara: el error trae la línea.
	broken := tb.create(t, "roto", srcBroken, nil)
	if out := tb.fails(t, "compare_with_image", map[string]any{"image": rel, "file": broken}); out.Text == "" {
		t.Error("sin mensaje para el modelo roto")
	}
}

// Una foto sin fondo uniforme no se puede segmentar: la herramienta lo dice en vez de inventar un IoU.
func TestCompareWithImageWithoutForeground(t *testing.T) {
	tb, ws := newToolbox(t, false)
	needRenderEngine(t, tb)
	flat := putImage(t, ws, "lisa.png", paintPNG(200, 200, func(x, y int) color.NRGBA { return color.NRGBA{120, 120, 120, 255} }))
	file := tb.create(t, "cubo", srcCube, nil)
	m := tb.mustJSON(t, "compare_with_image", map[string]any{"image": flat, "file": file})
	if m["silhouetteIoU"] != nil {
		t.Errorf("sin primer plano no hay IoU: %v", m["silhouetteIoU"])
	}
	if notes, _ := m["notes"].([]any); len(notes) == 0 {
		t.Errorf("debía avisar: %v", m)
	}
}

// --- por --mcp -------------------------------------------------------------------------------------

// Las cuatro herramientas funcionan por el servidor `--mcp` (JSON-RPC), no solo en proceso.
func TestImageToolsOverMCP(t *testing.T) {
	c := setup(t, nil)
	rel := putImage(t, c.Ws, "disco.png", discPNG(300, 300, 80))

	info := c.tool("image_info", map[string]any{"image": rel})
	if info.IsError || len(info.Content) != 2 || info.Content[0]["type"] != "image" || info.Content[0]["mimeType"] != "image/png" {
		t.Fatalf("image_info: %+v", info)
	}
	if m := jsonOf(t, info.Text); num(t, m, "widthPx") != 300 {
		t.Errorf("image_info: %v", m)
	}
	tr := c.mustOK("trace_image_outline", map[string]any{"image": rel, "width_mm": 40})
	if !c.exists("refs/disco-outline.svg") || tr["files"] == nil {
		t.Errorf("trace_image_outline: %v", tr)
	}
	hm := c.mustOK("image_to_heightmap", map[string]any{"image": rel, "width_mm": 40, "max_height_mm": 2})
	if !c.exists("refs/disco-heightmap.dat") || hm["snippet"] == nil {
		t.Errorf("image_to_heightmap: %v", hm)
	}
	if refs := c.mustOK("list_refs", nil); len(refs["refs"].([]any)) != 1 {
		t.Errorf("list_refs: %v", refs)
	}
	if r := c.tool("image_info", map[string]any{"image": "../disco.png"}); !r.IsError {
		t.Errorf("ruta hostil por --mcp: %+v", r)
	}
	// compare_with_image por JSON-RPC (necesita un motor de render).
	tb, _ := newToolbox(t, false)
	needRenderEngine(t, tb)
	file := c.create("cubo", srcCube, nil)
	r := c.tool("compare_with_image", map[string]any{"image": rel, "file": file})
	if r.IsError || len(r.Content) != 2 || r.Content[0]["type"] != "image" {
		t.Errorf("compare_with_image: %+v", r)
	}
}
