package meshedit

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"scadlab/backend/mesh"
	"scadlab/backend/parts"
	"scadlab/backend/render"
	"scadlab/backend/scad"
	"scadlab/backend/scadwasm"
	"scadlab/backend/threemf"
)

// wasmShared es el motor integrado de todos los tests del paquete: compilar el
// módulo cuesta segundos y se hace una sola vez.
var (
	wasmShared   *scadwasm.Runner
	wasmCacheDir string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "meshedit-wasm-cache-")
	if err != nil {
		panic(err)
	}
	wasmCacheDir = dir
	wasmShared = scadwasm.New(dir)
	code := m.Run()
	_ = wasmShared.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// engineCase es un motor contra el que se renderiza: el OpenSCAD nativo
// instalado o el integrado (WebAssembly).
type engineCase struct {
	name string
	opts render.Options
}

// engines devuelve los motores disponibles. El nativo se busca en el PATH y en
// la ruta de Homebrew; sin él, solo corre el integrado (y se avisa).
func engines(t *testing.T) []engineCase {
	t.Helper()
	var out []engineCase
	bin, err := exec.LookPath("openscad")
	if err != nil {
		if _, serr := os.Stat("/opt/homebrew/bin/openscad"); serr == nil {
			bin, err = "/opt/homebrew/bin/openscad", nil
		}
	}
	if err == nil {
		out = append(out, engineCase{name: "native", opts: render.Options{OpenSCADPath: bin, Backend: "manifold", TimeoutSeconds: 120}})
	} else {
		t.Log("OpenSCAD nativo no instalado: solo se prueba el integrado")
	}
	if wasmShared.Available() {
		out = append(out, engineCase{name: "wasm", opts: render.Options{WASM: wasmShared, Backend: "manifold", TimeoutSeconds: 180}})
	}
	if len(out) == 0 {
		t.Skip("ni OpenSCAD nativo ni el motor integrado están disponibles")
	}
	return out
}

// cubeSTL escribe un STL binario de una caja alineada a los ejes.
func cubeSTL(t *testing.T, path string, x0, y0, z0, x1, y1, z1 float64) {
	t.Helper()
	v := [][3]float64{
		{x0, y0, z0}, {x1, y0, z0}, {x1, y1, z0}, {x0, y1, z0},
		{x0, y0, z1}, {x1, y0, z1}, {x1, y1, z1}, {x0, y1, z1},
	}
	tr := [][3]int32{
		{0, 3, 2}, {0, 2, 1}, {4, 5, 6}, {4, 6, 7},
		{0, 1, 5}, {0, 5, 4}, {3, 7, 6}, {3, 6, 2},
		{0, 4, 7}, {0, 7, 3}, {1, 2, 6}, {1, 6, 5},
	}
	var buf bytes.Buffer
	if err := threemf.WriteSTL(&buf, threemf.Part{Vertices: v, Triangles: tr}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

// openCubeSTL: caja de 10 × 20 × 30 con una esquina en el origen. Lo bastante
// asimétrica para que cada eje se distinga de los otros.
const (
	cubeX, cubeY, cubeZ = 10.0, 20.0, 30.0
	cubeVolume          = cubeX * cubeY * cubeZ
)

var cubeBounds = &Bounds{Min: [3]float64{0, 0, 0}, Max: [3]float64{cubeX, cubeY, cubeZ}}

// workspaceWith arma un workspace temporal con refs/cubo.stl y un checkpoint
// padre.
func workspaceWith(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	cubeSTL(t, filepath.Join(ws, "refs", "cubo.stl"), 0, 0, 0, cubeX, cubeY, cubeZ)
	if err := os.WriteFile(filepath.Join(ws, "001-base.scad"), []byte("// @checkpoint parent= desc=\"base\"\ncolor(\"gray\") cube(1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

var renderSeq atomic.Int64

// rendered es lo que sale de renderizar una escena.
type rendered struct {
	src  string
	res  render.Result
	info mesh.Info
}

// renderScene genera el código de la escena, lo guarda como 002-edit.scad y lo
// renderiza con el motor dado.
func renderScene(t *testing.T, ws string, e engineCase, s Scene) rendered {
	t.Helper()
	if s.Ref == "" {
		s.Ref = "refs/cubo.stl"
	}
	src, err := Generate(s, Header{Parent: "001-base.scad", Desc: "prueba"}, "es")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	name := fmt.Sprintf("%03d-edit.scad", 2+renderSeq.Add(1))
	if err := os.WriteFile(filepath.Join(ws, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	eng, err := render.NewEngine(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	res, err := eng.Render(context.Background(), render.Request{WorkspaceKey: "t:" + name, WorkspaceDir: ws, FileName: name}, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != render.StatusOK {
		t.Fatalf("render %s: %s\n%+v\n--- código ---\n%s", e.name, res.Status, res.Diagnostics, src)
	}
	for _, d := range res.Diagnostics {
		if d.Severity == render.SeverityWarning {
			t.Errorf("%s: aviso de OpenSCAD en el código generado: %+v\n%s", e.name, d, src)
		}
	}
	info, err := mesh.InfoFromFile(res.MeshPath)
	if err != nil {
		t.Fatal(err)
	}
	return rendered{src: src, res: res, info: info}
}

// colorsOf renderiza el 3MF de la escena y devuelve los colores de sus partes
// (ordenados) y la caja de cada una.
func colorsOf(t *testing.T, ws string, e engineCase, src string) ([]string, map[string][2][3]float64) {
	t.Helper()
	name := fmt.Sprintf("%03d-colors.scad", 2+renderSeq.Add(1))
	if err := os.WriteFile(filepath.Join(ws, name), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	eng, err := render.NewEngine(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	ps, _, err := parts.Load3MF(context.Background(), eng, render.Request{WorkspaceKey: "t:" + name, WorkspaceDir: ws, FileName: name}, e.opts, t.TempDir())
	if err != nil {
		t.Fatalf("partes por color (%s): %v", e.name, err)
	}
	var colors []string
	boxes := map[string][2][3]float64{}
	for _, p := range ps {
		// La caja y no el volumen: la parte de un color es una cáscara abierta
		// (sus caras con el resto del cuerpo son de otro color).
		info, err := mesh.InfoFromIndexed(p.Vertices, p.Triangles)
		if err != nil {
			t.Fatal(err)
		}
		c := p.Color.Hex()
		if p.Uncolored {
			c = "DEFAULT"
		}
		colors = append(colors, c)
		boxes[c] = [2][3]float64{info.Min, info.Max}
	}
	sort.Strings(colors)
	return colors, boxes
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func nearVec(a, b [3]float64, tol float64) bool {
	return near(a[0], b[0], tol) && near(a[1], b[1], tol) && near(a[2], b[2], tol)
}

// lintClean revisa el código con el linter de ScadLab: ni errores ni avisos.
func lintClean(t *testing.T, ws string, e engineCase, src string) {
	t.Helper()
	diags := scad.Lint(context.Background(), src, scad.LintOptions{
		WorkspaceDir: ws, FileName: "002-edit.scad", OpenSCADPath: e.opts.OpenSCADPath, WASM: e.opts.WASM,
		ExistingFiles: []string{"001-base.scad", "002-edit.scad"},
	})
	for _, d := range diags {
		if d.Severity == scad.SeverityError || d.Severity == scad.SeverityWarning {
			t.Errorf("lint (%s): %s %s línea %d: %s\n%s", e.name, d.Severity, d.Code, d.Line, d.Message, src)
		}
	}
}

func containsAll(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("falta %q en:\n%s", w, got)
		}
	}
}

// renderSource renderiza código OpenSCAD cualquiera (no una escena) y exige que salga bien.
func renderSource(t *testing.T, ws string, e engineCase, code string) rendered {
	t.Helper()
	res := tryRender(t, ws, e, code)
	if res.Status != render.StatusOK {
		t.Fatalf("render %s: %s\n%+v\n--- código ---\n%s", e.name, res.Status, res.Diagnostics, code)
	}
	for _, d := range res.Diagnostics {
		if d.Severity == render.SeverityWarning {
			t.Errorf("%s: aviso de OpenSCAD: %+v", e.name, d)
		}
	}
	info, err := mesh.InfoFromFile(res.MeshPath)
	if err != nil {
		t.Fatal(err)
	}
	return rendered{src: code, res: res, info: info}
}

// tryRender renderiza y devuelve el resultado sea cual sea.
func tryRender(t *testing.T, ws string, e engineCase, code string) render.Result {
	t.Helper()
	name := fmt.Sprintf("%03d-src.scad", 2+renderSeq.Add(1))
	if err := os.WriteFile(filepath.Join(ws, name), []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	eng, err := render.NewEngine(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	res, err := eng.Render(context.Background(), render.Request{WorkspaceKey: "t:" + name, WorkspaceDir: ws, FileName: name}, e.opts)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// openSTL escribe la caja de 10 × 20 × 30 SIN la tapa de arriba: una malla abierta.
func openSTL(t *testing.T, path string) {
	t.Helper()
	v := [][3]float64{
		{0, 0, 0}, {10, 0, 0}, {10, 20, 0}, {0, 20, 0},
		{0, 0, 30}, {10, 0, 30}, {10, 20, 30}, {0, 20, 30},
	}
	tr := [][3]int32{{0, 3, 2}, {0, 2, 1}, {0, 1, 5}, {0, 5, 4}, {3, 7, 6}, {3, 6, 2}, {0, 4, 7}, {0, 7, 3}, {1, 2, 6}, {1, 6, 5}}
	var buf bytes.Buffer
	if err := threemf.WriteSTL(&buf, threemf.Part{Vertices: v, Triangles: tr}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
