package refmodel

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/i18n"
	"scadlab/backend/threemf"
)

// boxMesh es una caja alineada a los ejes como malla indexada: 8 vértices y 12
// triángulos orientados hacia afuera.
func boxMesh(x0, y0, z0, x1, y1, z1 float64) ([][3]float64, [][3]int32) {
	v := [][3]float64{
		{x0, y0, z0}, {x1, y0, z0}, {x1, y1, z0}, {x0, y1, z0},
		{x0, y0, z1}, {x1, y0, z1}, {x1, y1, z1}, {x0, y1, z1},
	}
	t := [][3]int32{
		{0, 3, 2}, {0, 2, 1}, {4, 5, 6}, {4, 6, 7},
		{0, 1, 5}, {0, 5, 4}, {3, 7, 6}, {3, 6, 2},
		{0, 4, 7}, {0, 7, 3}, {1, 2, 6}, {1, 6, 5},
	}
	return v, t
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func asciiSTL(v [][3]float64, tris [][3]int32) []byte {
	var b strings.Builder
	b.WriteString("solid prueba\n")
	for _, t := range tris {
		b.WriteString("facet normal 0 0 0\n outer loop\n")
		for _, i := range t {
			fmt.Fprintf(&b, "  vertex %g %g %g\n", v[i][0], v[i][1], v[i][2])
		}
		b.WriteString(" endloop\nendfacet\n")
	}
	b.WriteString("endsolid prueba\n")
	return []byte(b.String())
}

func binarySTL(v [][3]float64, tris [][3]int32) []byte {
	var b bytes.Buffer
	hdr := make([]byte, 80)
	copy(hdr, "solid binario") // muchos exportadores escriben «solid» también aquí
	b.Write(hdr)
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(tris)))
	for _, t := range tris {
		_ = binary.Write(&b, binary.LittleEndian, [3]float32{})
		for _, i := range t {
			_ = binary.Write(&b, binary.LittleEndian, [3]float32{float32(v[i][0]), float32(v[i][1]), float32(v[i][2])})
		}
		_ = binary.Write(&b, binary.LittleEndian, uint16(0))
	}
	return b.Bytes()
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func checkBox(t *testing.T, r Report, wantFormat string) {
	t.Helper()
	if r.Format != wantFormat || r.Triangles != 12 || !r.Watertight {
		t.Errorf("%s: %+v", wantFormat, r)
	}
	if r.Size != [3]float64{20, 10, 5} || r.Min != [3]float64{0, 0, 0} || r.Max != [3]float64{20, 10, 5} {
		t.Errorf("%s: caja %v %v %v", wantFormat, r.Min, r.Max, r.Size)
	}
	if !near(r.VolumeMm3, 1000) || !near(r.AreaMm2, 2*(200+100+50)) {
		t.Errorf("%s: volumen %v área %v", wantFormat, r.VolumeMm3, r.AreaMm2)
	}
	if len(r.Parts) != 1 || r.Parts[0].Triangles != 12 || r.Parts[0].Color != "" || !near(r.Parts[0].VolumeMm3, 1000) {
		t.Errorf("%s: partes %+v", wantFormat, r.Parts)
	}
}

func TestAnalyzeSTLAsciiAndBinary(t *testing.T) {
	dir := t.TempDir()
	v, tr := boxMesh(0, 0, 0, 20, 10, 5)
	for name, data := range map[string][]byte{"ascii.stl": asciiSTL(v, tr), "binario.STL": binarySTL(v, tr)} {
		r, err := Analyze(writeFile(t, dir, name, data))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		checkBox(t, r, FormatSTL)
		if r.Parts[0].Name != strings.TrimSuffix(name, filepath.Ext(name)) {
			t.Errorf("%s: nombre de la parte %q", name, r.Parts[0].Name)
		}
	}
}

// Un OBJ con caras de 4 lados (abanico), índices con v/vt/vn, un índice
// negativo, comentarios, líneas ignoradas y una continuación con «\».
func TestAnalyzeOBJQuadsAndNegativeIndexes(t *testing.T) {
	obj := `# caja de 20x10x5
mtllib caja.mtl
o caja
v 0 0 0
v 20 0 0
v 20 10 0
v 0 10 0
v 0 0 5 0.5 0.5 0.5
v 20 0 5
v 20 10 5
v 0 10 5
vt 0 0
vn 0 0 1
usemtl rojo
s off
f 1/1/1 4/1/1 3/1/1 2/1/1
f 5//1 6//1 7//1 8//1
f 1 2 6 5
f 2 3 7 \
  6
f 3 4 8 7
f -8 -4 -1 -5
`
	r, err := Analyze(writeFile(t, t.TempDir(), "caja.obj", []byte(obj)))
	if err != nil {
		t.Fatal(err)
	}
	// 6 caras de 4 lados = 12 triángulos. La última cara usa índices negativos
	// (v1, v5, v8, v4), que es el lado izquierdo de la caja.
	checkBox(t, r, FormatOBJ)

	// Un polígono de 5 vértices da 3 triángulos.
	pent := "v 0 0 0\nv 1 0 0\nv 2 1 0\nv 1 2 0\nv 0 1 0\nf 1 2 3 4 5\n"
	r2, err := Analyze(writeFile(t, t.TempDir(), "p.obj", []byte(pent)))
	if err != nil || r2.Triangles != 3 || r2.Watertight {
		t.Errorf("pentágono: %+v %v", r2, err)
	}
}

func TestAnalyzeOBJErrors(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"sin-caras.obj":   "v 0 0 0\nv 1 0 0\n",
		"fuera.obj":       "v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 9\n",
		"negativo.obj":    "v 0 0 0\nv 1 0 0\nf 1 2 -9\n",
		"coordenada.obj":  "v 0 0 x\n",
		"cara-chica.obj":  "v 0 0 0\nv 1 0 0\nf 1 2\n",
		"vacio.obj":       "",
		"solo-comentario": "# nada\n",
	} {
		if !strings.HasSuffix(name, ".obj") {
			name += ".obj"
		}
		if _, err := Analyze(writeFile(t, dir, name, []byte(body))); err == nil {
			t.Errorf("%s: debería fallar", name)
		}
	}
	// Un índice positivo hacia un vértice declarado más abajo es válido.
	late := "f 1 2 3\nv 0 0 0\nv 1 0 0\nv 0 1 0\n"
	if r, err := Analyze(writeFile(t, dir, "tarde.obj", []byte(late))); err != nil || r.Triangles != 1 {
		t.Errorf("vértices tardíos: %+v %v", r, err)
	}
}

// Un 3MF de dos colores generado con threemf.Write: una parte por color, con su
// volumen, y el total cerrado.
func make3MF(t *testing.T, dir string) string {
	t.Helper()
	v1, t1 := boxMesh(0, 0, 0, 10, 10, 10)
	v2, t2 := boxMesh(20, 0, 0, 30, 10, 5)
	parts := []threemf.Part{
		{Name: "Color 1 (#FF0000)", Color: threemf.RGB{0xFF, 0, 0}, Extruder: 1, Vertices: v1, Triangles: t1},
		{Name: "Color 2 (#0000FF)", Color: threemf.RGB{0, 0, 0xFF}, Extruder: 2, Vertices: v2, Triangles: t2},
	}
	p := filepath.Join(dir, "dos-colores.3mf")
	if err := threemf.WriteFile(p, parts, threemf.WriteOptions{Title: "dos colores"}); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAnalyze3MFTwoColors(t *testing.T) {
	r, err := Analyze(make3MF(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	if r.Format != Format3MF || r.Triangles != 24 || !r.Watertight {
		t.Fatalf("total: %+v", r)
	}
	if r.Size != [3]float64{30, 10, 10} || !near(r.VolumeMm3, 1000+500) {
		t.Errorf("total: tamaño %v volumen %v", r.Size, r.VolumeMm3)
	}
	if len(r.Parts) != 2 {
		t.Fatalf("partes: %+v", r.Parts)
	}
	p0, p1 := r.Parts[0], r.Parts[1]
	if p0.Color != "#FF0000" || p1.Color != "#0000FF" || !near(p0.VolumeMm3, 1000) || !near(p1.VolumeMm3, 500) ||
		p0.Triangles != 12 || p1.Size != [3]float64{10, 10, 5} {
		t.Errorf("partes: %+v %+v", p0, p1)
	}
}

func TestAnalyzeRejectsUnknownMissingAndTooBig(t *testing.T) {
	i18n.SetLang("es")
	t.Cleanup(func() { i18n.SetLang("en") })
	dir := t.TempDir()

	_, err := Analyze(writeFile(t, dir, "nota.txt", []byte("hola")))
	if err == nil || !strings.Contains(err.Error(), "formato no reconocido") {
		t.Errorf("formato desconocido: %v", err)
	}
	if _, err := Analyze(filepath.Join(dir, "no-existe.stl")); err == nil {
		t.Error("archivo inexistente debería fallar")
	}
	if _, err := Analyze(dir); err == nil {
		t.Error("una carpeta no es una referencia")
	}
	for name, body := range map[string]string{"roto.stl": "solid x\nfacet", "roto.3mf": "no soy un zip"} {
		if _, err := Analyze(writeFile(t, dir, name, []byte(body))); err == nil {
			t.Errorf("%s: debería fallar", name)
		}
	}

	// Un archivo más grande que MaxBytes (disperso: no ocupa disco).
	big := filepath.Join(dir, "enorme.stl")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxBytes + 1); err != nil {
		f.Close()
		t.Skipf("el sistema de archivos no admite archivos dispersos: %v", err)
	}
	f.Close()
	for _, fn := range []func() error{
		func() error { _, err := Analyze(big); return err },
		func() error { _, err := Preview(big, "", 0); return err },
		func() error { _, err := CopyToRefs(t.TempDir(), big); return err },
	} {
		if err := fn(); err == nil || !strings.Contains(err.Error(), "demasiado grande") {
			t.Errorf("tamaño máximo: %v", err)
		}
	}
	// En inglés también.
	i18n.SetLang("en")
	if _, err := Analyze(big); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("tamaño máximo (en): %v", err)
	}
	// Justo en el límite sí pasa el control de tamaño (falla después, por no ser un STL).
	if err := os.Truncate(big, MaxBytes); err != nil {
		t.Fatal(err)
	}
	if _, err := Analyze(big); err == nil || strings.Contains(err.Error(), "too large") {
		t.Errorf("en el límite: %v", err)
	}
}

func TestAnalyze3MFRejectsZipBomb(t *testing.T) {
	// Se baja el tope para no tener que comprimir medio gigabyte en la prueba:
	// lo que se comprueba es que se mire el tamaño descomprimido ANTES de leer.
	old := maxUnzipped
	maxUnzipped = 1 << 20
	t.Cleanup(func() { maxUnzipped = old })

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("relleno.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(make([]byte, 4<<20)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if buf.Len() > 64<<10 {
		t.Fatalf("la bomba ocupa %d bytes comprimida: el caso no prueba lo que dice", buf.Len())
	}
	p := writeFile(t, t.TempDir(), "bomba.3mf", buf.Bytes())
	i18n.SetLang("en")
	if _, err := Analyze(p); err == nil || !strings.Contains(err.Error(), "once unzipped") {
		t.Errorf("zip bomb: %v", err)
	}
	if _, err := Preview(p, "", 0); err == nil || !strings.Contains(err.Error(), "once unzipped") {
		t.Errorf("zip bomb (preview): %v", err)
	}
}

func TestPreviewAllFormatsAndViews(t *testing.T) {
	dir := t.TempDir()
	v, tr := boxMesh(0, 0, 0, 20, 10, 5)
	files := map[string]string{
		"stl": writeFile(t, dir, "a.stl", binarySTL(v, tr)),
		"obj": writeFile(t, dir, "b.obj", []byte("v 0 0 0\nv 20 0 0\nv 20 10 0\nv 0 10 0\nv 0 0 5\nv 20 0 5\nv 20 10 5\nv 0 10 5\nf 1 4 3 2\nf 5 6 7 8\nf 1 2 6 5\nf 2 3 7 6\nf 3 4 8 7\nf 4 1 5 8\n")),
		"3mf": make3MF(t, dir),
	}
	for format, p := range files {
		for _, view := range []string{"", "iso", "FRONT", "top"} {
			data, err := Preview(p, view, 300)
			if err != nil {
				t.Fatalf("%s/%q: %v", format, view, err)
			}
			img, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("%s/%q: no es un PNG: %v", format, view, err)
			}
			if b := img.Bounds(); b.Dx() != 300 || b.Dy() != 300 {
				t.Errorf("%s/%q: %v", format, view, b)
			}
		}
	}
	// El tamaño se acota a 256–1024 y 0 es el de por defecto.
	for size, want := range map[int]int{0: 640, 10: 256, 5000: 1024} {
		data, err := Preview(files["stl"], "iso", size)
		if err != nil {
			t.Fatal(err)
		}
		img, _ := png.Decode(bytes.NewReader(data))
		if img.Bounds().Dx() != want {
			t.Errorf("size %d: lado %d, quería %d", size, img.Bounds().Dx(), want)
		}
	}
	if _, err := Preview(files["stl"], "diagonal", 300); err == nil || !strings.Contains(err.Error(), "iso-back") {
		t.Errorf("vista desconocida: %v", err)
	}
}

// El 3MF se dibuja con el color de cada parte.
func TestPreview3MFUsesPartColors(t *testing.T) {
	data, err := Preview(make3MF(t, t.TempDir()), "front", 400)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var red, blue int
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			switch {
			case r>>8 > 150 && g>>8 < 90 && bl>>8 < 90:
				red++
			case bl>>8 > 150 && r>>8 < 90 && g>>8 < 90:
				blue++
			}
		}
	}
	if red < 500 || blue < 300 {
		t.Errorf("píxeles rojos %d, azules %d: faltan los colores de las partes", red, blue)
	}
}

func TestCopyToRefsNeverOverwrites(t *testing.T) {
	ws := t.TempDir()
	src := t.TempDir()
	v, tr := boxMesh(0, 0, 0, 20, 10, 5)
	a := writeFile(t, src, "Pieza Ñandú (1).STL", binarySTL(v, tr))

	first, err := CopyToRefs(ws, a)
	if err != nil {
		t.Fatal(err)
	}
	if first != "refs/pieza-nandu-1.stl" {
		t.Errorf("primer nombre: %q", first)
	}
	// Otro archivo distinto con el mismo nombre seguro no pisa al primero.
	other := writeFile(t, t.TempDir(), "pieza-nandu-1.stl", asciiSTL(v, tr))
	second, err := CopyToRefs(ws, other)
	if err != nil || second != "refs/pieza-nandu-1-2.stl" {
		t.Errorf("segundo nombre: %q %v", second, err)
	}
	third, err := CopyToRefs(ws, a)
	if err != nil || third != "refs/pieza-nandu-1-3.stl" {
		t.Errorf("tercer nombre: %q %v", third, err)
	}
	got, _ := os.ReadFile(filepath.Join(ws, "refs", "pieza-nandu-1.stl"))
	if !bytes.Equal(got, binarySTL(v, tr)) {
		t.Error("el primero se modificó")
	}
	copied, _ := os.ReadFile(filepath.Join(ws, "refs", "pieza-nandu-1-2.stl"))
	if !bytes.Equal(copied, asciiSTL(v, tr)) {
		t.Error("la copia no es idéntica a su origen")
	}
	// Una referencia copiada se analiza igual.
	if r, err := Analyze(filepath.Join(ws, filepath.FromSlash(first))); err != nil || r.Triangles != 12 {
		t.Errorf("analizar la copia: %+v %v", r, err)
	}

	// Nombres raros: reservado de Windows y sin letras ASCII.
	for in, want := range map[string]string{"con.obj": "refs/con-ref.obj", "彫刻.3mf": "refs/reference.3mf", "LPT1.stl": "refs/lpt1-ref.stl"} {
		body := []byte("v 0 0 0\nv 1 0 0\nv 0 1 0\nf 1 2 3\n")
		if strings.HasSuffix(in, ".stl") {
			body = binarySTL(v, tr)
		}
		got, err := CopyToRefs(t.TempDir(), writeFile(t, t.TempDir(), in, body))
		if err != nil || got != want {
			t.Errorf("%q: %q %v (quería %q)", in, got, err, want)
		}
	}
	if _, err := CopyToRefs(ws, writeFile(t, src, "nota.txt", []byte("x"))); err == nil {
		t.Error("un formato desconocido no se copia")
	}
	if ents, _ := os.ReadDir(filepath.Join(ws, "refs")); len(ents) != 3 {
		t.Errorf("refs/ tiene %d archivos, esperaba 3", len(ents))
	}
}

func TestCopyToRefsRefusesRefsOutsideWorkspace(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws, "refs")); err != nil {
		t.Skipf("sin enlaces simbólicos: %v", err)
	}
	v, tr := boxMesh(0, 0, 0, 1, 1, 1)
	if _, err := CopyToRefs(ws, writeFile(t, t.TempDir(), "a.stl", binarySTL(v, tr))); err == nil {
		t.Error("refs/ apunta fuera del workspace: no debería copiar")
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Errorf("se escribió fuera del workspace: %v", ents)
	}
}

func TestResolveRefHostileNames(t *testing.T) {
	ws := t.TempDir()
	v, tr := boxMesh(0, 0, 0, 1, 1, 1)
	rel, err := CopyToRefs(ws, writeFile(t, t.TempDir(), "ok.stl", binarySTL(v, tr)))
	if err != nil {
		t.Fatal(err)
	}
	// Fuera de refs/: un archivo hermano y un enlace que escapa.
	secret := writeFile(t, ws, "secreto.stl", binarySTL(v, tr))
	if err := os.Symlink(secret, filepath.Join(ws, "refs", "enlace.stl")); err != nil {
		t.Logf("sin enlaces simbólicos: %v", err)
	}

	for _, name := range []string{"ok.stl", "refs/ok.stl", " refs/ok.stl "} {
		abs, got, err := ResolveRef(ws, name)
		if err != nil || got != rel || !strings.HasSuffix(abs, filepath.FromSlash(rel)) {
			t.Errorf("%q: %q %q %v", name, abs, got, err)
		}
	}
	for _, name := range []string{
		"", ".", "..", "../secreto.stl", "refs/../secreto.stl", "refs/../../etc/passwd", "/etc/passwd",
		`..\secreto.stl`, `C:\Windows\x.stl`, "C:x.stl", "sub/ok.stl", ".oculto.stl", "ok.stl\x00.txt",
		"nota.txt", "refs/", "enlace.stl", "no-existe.stl", "refs",
	} {
		if abs, _, err := ResolveRef(ws, name); err == nil {
			t.Errorf("%q: debería rechazarse (resolvió %q)", name, abs)
		}
	}
	// Sin carpeta refs/.
	if _, _, err := ResolveRef(t.TempDir(), "ok.stl"); err == nil {
		t.Error("sin refs/ debería fallar")
	}
}

func TestPromptText(t *testing.T) {
	r, err := Analyze(make3MF(t, t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	es := r.PromptText("refs/dos-colores.3mf", "es")
	for _, want := range []string{
		"refs/dos-colores.3mf", "3MF", "24 triángulos", "X 30.00 × Y 10.00 × Z 10.00", "Malla cerrada",
		"Partes (2)", "#FF0000", "#0000FF", "NO es fiable con un 3MF", "color()",
	} {
		if !strings.Contains(es, want) {
			t.Errorf("es: falta %q\n%s", want, es)
		}
	}
	en := r.PromptText("refs/dos-colores.3mf", "en")
	for _, want := range []string{"Reference: refs/dos-colores.3mf", "24 triangles", "Parts (2)", "NOT reliable with a 3MF", "Watertight mesh"} {
		if !strings.Contains(en, want) {
			t.Errorf("en: falta %q\n%s", want, en)
		}
	}
	if strings.Contains(en, "triángulos") || strings.Contains(es, "triangles") {
		t.Error("idiomas mezclados")
	}

	v, tr := boxMesh(0, 0, 0, 20, 10, 5)
	stl, _ := Analyze(writeFile(t, t.TempDir(), "caja.stl", binarySTL(v, tr)))
	txt := stl.PromptText("refs/caja.stl", "es")
	for _, want := range []string{`import("refs/caja.stl")`, `%import("refs/caja.stl")`, "z=0"} {
		if !strings.Contains(txt, want) {
			t.Errorf("stl: falta %q\n%s", want, txt)
		}
	}
	if strings.Contains(txt, "Partes (") {
		t.Error("un STL de una sola parte no lista partes")
	}
	open := Report{Format: FormatSTL, Triangles: 1, Parts: []Part{{Name: "x"}}}
	if !strings.Contains(open.PromptText("refs/x.stl", "es"), "NO cerrada") {
		t.Error("falta el aviso de malla abierta")
	}
	if strings.Contains(r.PromptText("refs/a.3mf", "fr"), "triángulos") {
		t.Error("un idioma desconocido es inglés")
	}
}

func TestFormatOfAndViews(t *testing.T) {
	for in, want := range map[string]string{"a.STL": FormatSTL, "a.3mf": Format3MF, "x/y.Obj": FormatOBJ, "a.txt": "", "stl": ""} {
		if got := FormatOf(in); got != want {
			t.Errorf("FormatOf(%q) = %q", in, got)
		}
	}
	for _, n := range ViewNames {
		if _, ok := Views[n]; !ok {
			t.Errorf("vista %s sin definir", n)
		}
	}
	if len(ViewNames) != len(Views) {
		t.Errorf("%d nombres y %d vistas", len(ViewNames), len(Views))
	}
	name, v, err := ParseView("  Iso ")
	if err != nil || name != "iso" || v.Ortho {
		t.Errorf("ParseView: %q %+v %v", name, v, err)
	}
}
