package meshedit

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/printers"
	"scadlab/backend/refmodel"
	"scadlab/backend/threemf"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// box devuelve una caja como malla indexada.
func box(x0, y0, z0, x1, y1, z1 float64) ([][3]float64, [][3]int32) {
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

// Importar un STL: copia, checkpoint raíz con cabecera, marca y color, y «centrar
// y apoyar» como un translate. El STL no se toca y el render lo confirma.
func TestImportRefSTLCentersOnBed(t *testing.T) {
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	ws := workspaceWith(t) // ya tiene 001-base.scad: la importación es una raíz nueva
	stlBefore := readFile(t, filepath.Join(ws, "refs", "cubo.stl"))

	got, err := ImportRef(ws, "refs/cubo.stl", ImportOptions{CenterOnBed: true, Label: "Mi soporte"})
	if err != nil {
		t.Fatal(err)
	}
	if got.File != "002-mi-soporte.scad" || got.Ref != "refs/cubo.stl" || !got.Report.Watertight || len(got.Warnings) != 0 {
		t.Fatalf("resultado: %+v", got)
	}
	src := readFile(t, filepath.Join(ws, got.File))
	first := strings.SplitN(src, "\n", 2)[0]
	if first != `// @checkpoint parent= desc="Importa cubo.stl"` {
		t.Errorf("cabecera: %q", first)
	}
	// Centrar: X e Y por el medio de la caja (10 × 20) y Z apoyado en 0.
	containsAll(t, src, MarkerPrefix, "translate([-5, -10, 0])", `color("#9ca3af") modelo();`)
	if base, ok := BaseOf(src); !ok || base.Model.Translate != [3]float64{-5, -10, 0} || base.Ref != "refs/cubo.stl" {
		t.Errorf("BaseOf: %+v %v", base, ok)
	}
	if after := readFile(t, filepath.Join(ws, "refs", "cubo.stl")); after != stlBefore {
		t.Error("el STL original cambió")
	}
	if _, err := os.Stat(filepath.Join(ws, "001-base.scad")); err != nil {
		t.Error("el checkpoint anterior desapareció")
	}

	// Sin centrar queda donde estaba.
	got2, err := ImportRef(ws, "cubo.stl", ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if src := readFile(t, filepath.Join(ws, got2.File)); strings.Contains(src, "translate(") || got2.File != "003-cubo.scad" {
		t.Errorf("sin centrar: %s\n%s", got2.File, src)
	}

	for _, e := range engines(t) {
		t.Run(e.name, func(t *testing.T) {
			r := renderScene(t, ws, e, Scene{Ref: "refs/cubo.stl", Model: BaseScene("refs/cubo.stl", got.Report, true).Model})
			if !nearVec(r.info.Min, [3]float64{-5, -10, 0}, 1e-3) || !nearVec(r.info.Max, [3]float64{5, 10, 30}, 1e-3) {
				t.Errorf("caja centrada = %v … %v", r.info.Min, r.info.Max)
			}
		})
	}
}

// Un 3MF de dos colores se convierte a STL (el original queda), se avisa que los
// colores se juntan y el checkpoint base importa el STL convertido.
func TestImportRef3MFConvertsToSTL(t *testing.T) {
	i18n.SetLang("en")
	ws := t.TempDir()
	v1, t1 := box(0, 0, 0, 10, 10, 10)
	v2, t2 := box(20, 0, 0, 30, 10, 5)
	err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	three := filepath.Join(ws, "refs", "dos-colores.3mf")
	if err := threemf.WriteFile(three, []threemf.Part{
		{Name: "Color 1", Color: threemf.RGB{0xFF, 0, 0}, Extruder: 1, Vertices: v1, Triangles: t1},
		{Name: "Color 2", Color: threemf.RGB{0, 0, 0xFF}, Extruder: 2, Vertices: v2, Triangles: t2},
	}, threemf.WriteOptions{Title: "dos"}); err != nil {
		t.Fatal(err)
	}
	original := readFile(t, three)

	got, err := ImportRef(ws, "refs/dos-colores.3mf", ImportOptions{CenterOnBed: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref != "refs/dos-colores.stl" || got.File != "001-dos-colores.scad" || len(got.Report.Parts) != 2 {
		t.Fatalf("resultado: %+v", got)
	}
	if readFile(t, three) != original {
		t.Error("el 3MF original cambió")
	}
	info, err := mesh.InfoFromFile(filepath.Join(ws, "refs", "dos-colores.stl"))
	if err != nil {
		t.Fatal(err)
	}
	// Las dos cajas, juntas en una sola malla: 30 × 10 × 10 y 1000 + 500 mm³.
	if !nearVec(info.Size, [3]float64{30, 10, 10}, 1e-4) || !near(info.VolumeMM3, 1500, 0.01) || !info.Watertight {
		t.Errorf("STL convertido: %+v", info)
	}
	joined := strings.Join(got.Warnings, "\n")
	if !strings.Contains(joined, "2 colors") || strings.Contains(joined, "not watertight") {
		t.Errorf("avisos: %q", joined)
	}
	src := readFile(t, filepath.Join(ws, got.File))
	containsAll(t, src, `import("refs/dos-colores.stl"`, "translate([-15, -5, 0])")
	if strings.Contains(src, `import("refs/dos-colores.3mf`) {
		t.Error("el checkpoint no debe importar el 3MF")
	}
	// Un segundo import no pisa el STL convertido.
	again, err := ImportRef(ws, "dos-colores.3mf", ImportOptions{})
	if err != nil || again.Ref != "refs/dos-colores-2.stl" {
		t.Fatalf("segundo import: %+v %v", again, err)
	}

	for _, e := range engines(t) {
		t.Run(e.name, func(t *testing.T) {
			r := renderScene(t, ws, e, Scene{Ref: got.Ref, Model: BaseScene(got.Ref, got.Report, true).Model})
			if !nearVec(r.info.Size, [3]float64{30, 10, 10}, 1e-3) || !r.info.Watertight {
				t.Errorf("render del 3MF convertido: %+v", r.info)
			}
		})
	}
}

// Un OBJ (con caras de cuatro vértices) se convierte a un STL cerrado.
func TestImportRefOBJConvertsToSTL(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString("# cubo de 10 × 20 × 30\n")
	for _, v := range [][3]int{{0, 0, 0}, {10, 0, 0}, {10, 20, 0}, {0, 20, 0}, {0, 0, 30}, {10, 0, 30}, {10, 20, 30}, {0, 20, 30}} {
		fmt.Fprintf(&b, "v %d %d %d\n", v[0], v[1], v[2])
	}
	// Cuatro vértices por cara, hacia afuera.
	b.WriteString("f 1 4 3 2\nf 5 6 7 8\nf 1 2 6 5\nf 4 8 7 3\nf 1 5 8 4\nf 2 3 7 6\n")
	if err := os.WriteFile(filepath.Join(ws, "refs", "caja.obj"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ImportRef(ws, "refs/caja.obj", ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref != "refs/caja.stl" || got.Report.Format != refmodel.FormatOBJ || len(got.Warnings) != 0 {
		t.Fatalf("resultado: %+v", got)
	}
	info, err := mesh.InfoFromFile(filepath.Join(ws, "refs", "caja.stl"))
	if err != nil || !info.Watertight || !near(info.VolumeMM3, cubeVolume, 0.01) || info.Triangles != 12 {
		t.Errorf("STL convertido: %+v %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(ws, "refs", "caja.obj")); err != nil {
		t.Error("el OBJ original desapareció")
	}
}

// Una malla abierta se importa, pero avisa; una que no cabe en la placa o es
// enorme también.
func TestImportWarnings(t *testing.T) {
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Una caja sin su tapa de arriba: abierta.
	v, tr := box(0, 0, 0, 300, 20, 30)
	tr = append(tr[:2:2], tr[4:]...)
	var buf bytes.Buffer
	if err := threemf.WriteSTL(&buf, threemf.Part{Vertices: v, Triangles: tr}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "refs", "abierta.stl"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	plate := printers.Volume{X: 180, Y: 180, Z: 180}
	got, err := ImportRef(ws, "abierta.stl", ImportOptions{Plate: plate, PlateName: "Bambu Lab A1 mini"})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got.Warnings, "\n")
	for _, want := range []string{"no es cerrada", "Manifold", "CGAL", "no cabe en la placa de Bambu Lab A1 mini", "300.0 × 20.0 × 30.0", "120.0 × 0.0 × 0.0"} {
		if !strings.Contains(joined, want) {
			t.Errorf("falta %q en los avisos:\n%s", want, joined)
		}
	}
	if got.Report.Watertight {
		t.Error("la caja sin tapa no debía ser cerrada")
	}

	// En inglés y con una malla grande.
	i18n.SetLang("en")
	w := ImportWarnings(refmodel.Report{Watertight: true, Triangles: BigMeshTriangles + 1}, ImportOptions{})
	if len(w) != 1 || !strings.Contains(w[0], "500001 triangles") {
		t.Errorf("malla grande: %q", w)
	}
	if w := ImportWarnings(refmodel.Report{Watertight: true, Triangles: 1000}, ImportOptions{Plate: plate}); len(w) != 0 {
		t.Errorf("una malla cerrada que cabe no avisa nada: %q", w)
	}
}

// Una referencia inválida o hostil no crea ningún checkpoint.
func TestImportRefRejectsHostileFiles(t *testing.T) {
	ws := workspaceWith(t)
	outside := filepath.Join(t.TempDir(), "afuera.stl")
	cubeSTL(t, outside, 0, 0, 0, 1, 1, 1)
	if err := os.WriteFile(filepath.Join(ws, "refs", "nota.txt"), []byte("hola"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "refs", "roto.stl"), []byte("solid x\nfacet"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "refs", "roto.obj"), []byte("v 0 0 0\nf 1 2 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	symlinks := os.Symlink(outside, filepath.Join(ws, "refs", "enlace.stl")) == nil
	bad := []string{"", "../afuera.stl", "refs/../001-base.scad", outside, "/etc/passwd", `..\x.stl`, "refs/sub/x.stl", "nota.txt", "no-existe.stl", "refs/", ".", "..", "roto.stl", "roto.obj"}
	if symlinks {
		bad = append(bad, "enlace.stl")
	}
	for _, f := range bad {
		if got, err := ImportRef(ws, f, ImportOptions{}); err == nil {
			t.Errorf("file=%q debía rechazarse: %+v", f, got)
		}
	}
	files, _ := filepath.Glob(filepath.Join(ws, "*.scad"))
	if len(files) != 1 {
		t.Errorf("se crearon checkpoints de más: %v", files)
	}
	if _, err := os.Stat(filepath.Join(ws, "refs", "roto.stl")); err != nil {
		t.Error("no debe borrar lo que no creó")
	}
}
