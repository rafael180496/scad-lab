package render

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scadlab/backend/mesh"
	"scadlab/backend/scadwasm"
	"scadlab/backend/threemf"
)

// wasmShared es el motor integrado que comparten los tests de este paquete:
// compilar el módulo cuesta segundos y se hace una sola vez. Su caché de
// compilación vive en un directorio temporal que TestMain borra.
var (
	wasmShared   *scadwasm.Runner
	wasmCacheDir string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "render-wasm-cache-")
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

func writeScad(t *testing.T, files map[string]string) string {
	t.Helper()
	ws := t.TempDir()
	for n, body := range files {
		if err := os.WriteFile(filepath.Join(ws, n), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

func newTestEngine(t *testing.T) *Engine {
	t.Helper()
	eng, err := NewEngine(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	return eng
}

func wasmOpts() Options {
	return Options{WASM: wasmShared, Backend: "manifold", TimeoutSeconds: 120}
}

// Sin OpenSCAD nativo, el render cae al motor integrado: malla válida, motor,
// versión y backend informados, y la caché devuelve el mismo motor.
func TestRenderFallsBackToWASM(t *testing.T) {
	ws := writeScad(t, map[string]string{
		"001-ok.scad":     "color(\"red\") cube([10, 20, 30]);\n",
		"002-syntax.scad": "cube(1);\ncube(;\n",
		"003-empty.scad":  "translate([1,0,0]);\n",
		"004-2d.scad":     "square(10);\n",
	})
	eng := newTestEngine(t)
	render := func(f string) Result {
		r, err := eng.Render(context.Background(), Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: f}, wasmOpts())
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		return r
	}

	ok := render("001-ok.scad")
	if ok.Status != StatusOK || ok.Engine != EngineWASM || ok.Version != scadwasm.Version || ok.Backend != "manifold" {
		t.Fatalf("ok: %+v", ok)
	}
	info, err := mesh.InfoFromFile(ok.MeshPath)
	if err != nil || info.Triangles != 12 || !info.Watertight || info.Size != [3]float64{10, 20, 30} {
		t.Fatalf("malla: %+v err=%v", info, err)
	}
	again := render("001-ok.scad")
	if !again.CacheHit || again.Engine != EngineWASM || again.MeshPath != ok.MeshPath {
		t.Fatalf("segundo render: %+v", again)
	}

	if r := render("002-syntax.scad"); r.Status != StatusError || len(r.Diagnostics) == 0 || r.Diagnostics[0].Line != 2 || r.Engine != EngineWASM {
		t.Errorf("syntax: %+v", r)
	}
	if r := render("003-empty.scad"); r.Status != StatusError || len(r.Diagnostics) == 0 {
		t.Errorf("vacío: %+v", r)
	}
	if r := render("004-2d.scad"); r.Status != StatusError || !strings.Contains(r.Diagnostics[len(r.Diagnostics)-1].Message, "linear_extrude") {
		t.Errorf("2D: %+v", r.Diagnostics)
	}
	// Ni carpetas temporales de salida ni archivos sueltos quedan en la caché.
	ents, _ := os.ReadDir(filepath.Join(eng.impl.cacheDir))
	for _, e := range ents {
		if e.IsDir() || strings.HasSuffix(e.Name(), ".stl") && len(strings.TrimSuffix(e.Name(), ".stl")) != 64 {
			t.Errorf("quedó %s en la caché", e.Name())
		}
	}
}

// «cgal» en Configuración se respeta también con el motor integrado.
func TestWASMHonorsCGALBackend(t *testing.T) {
	ws := writeScad(t, map[string]string{"001-a.scad": "difference() { cube(10); sphere(6, $fn = 24); }\n"})
	eng := newTestEngine(t)
	opts := wasmOpts()
	opts.Backend = "cgal"
	r, err := eng.Render(context.Background(), Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: "001-a.scad"}, opts)
	if err != nil || r.Status != StatusOK || r.Backend != "cgal" || !strings.Contains(r.Log, "Nef polyhedron") {
		t.Fatalf("cgal: %+v err=%v", r, err)
	}
}

// La malla del integrado y la del nativo viven bajo claves distintas: la
// versión del wasm entra en la clave.
func TestWASMVersionIsPartOfTheCacheKey(t *testing.T) {
	sum := "abc"
	if cacheKey(sum, "manifold", scadwasm.Version) == cacheKey(sum, "manifold", "2025.03.15") {
		t.Fatal("el wasm y un OpenSCAD nativo comparten clave de caché")
	}
}

func TestMissingWhenNothingAvailable(t *testing.T) {
	ws := writeScad(t, map[string]string{"001-a.scad": "cube(1);\n"})
	eng := newTestEngine(t)
	for name, opts := range map[string]Options{
		"sin nada":       {},
		"wasm nil":       {WASM: (*scadwasm.Runner)(nil)},
		"wasm no existe": {WASM: unavailableWASM{}},
	} {
		r, err := eng.Render(context.Background(), Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: "001-a.scad"}, opts)
		if err != nil || r.Status != StatusMissing || r.Engine != "" {
			t.Errorf("%s: %+v err=%v", name, r, err)
		}
		a, err := eng.RenderArtifact(context.Background(), Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: "001-a.scad"}, opts, ArtifactExt3MF, t.TempDir())
		if err != nil || a.Status != StatusMissing || a.Engine != "" {
			t.Errorf("%s (artefacto): %+v err=%v", name, a, err)
		}
	}
}

type unavailableWASM struct{}

func (unavailableWASM) Available() bool { return false }
func (unavailableWASM) Run(context.Context, scadwasm.RunSpec) (string, int, error) {
	panic("no debería ejecutarse")
}

// El OpenSCAD nativo gana al integrado, y su resultado dice «native» (también
// al salir de la caché y en un .json anterior al motor integrado).
func TestNativeWinsAndReportsEngine(t *testing.T) {
	f := newFake(t, false)
	ws := writeScad(t, map[string]string{"001-a.scad": "cube(1);\n"})
	eng := newTestEngine(t)
	opts := Options{OpenSCADPath: f.bin, WASM: unavailableWASM{}, Backend: "manifold"}
	req := Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: "001-a.scad"}
	r, err := eng.Render(context.Background(), req, opts)
	if err != nil || r.Status != StatusOK || r.Engine != EngineNative {
		t.Fatalf("nativo: %+v err=%v", r, err)
	}
	hit, _ := eng.Render(context.Background(), req, opts)
	if !hit.CacheHit || hit.Engine != EngineNative {
		t.Fatalf("hit: %+v", hit)
	}

	// Un .json de antes del motor integrado no trae «engine»: se lee como nativo.
	meta := strings.TrimSuffix(r.MeshPath, ".stl") + ".json"
	raw, err := os.ReadFile(meta)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	delete(m, "engine")
	raw, _ = json.Marshal(m)
	if err := os.WriteFile(meta, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	old, _ := eng.Render(context.Background(), req, opts)
	if !old.CacheHit || old.Engine != EngineNative {
		t.Fatalf("json viejo: %+v", old)
	}
}

// El 3MF del integrado: dos color() dan dos partes con sus colores, aunque el
// lib3mf del wasm no pueda escribir 3MF (ver scadwasm/off.go).
func TestWASMArtifact3MFHasOnePartPerColor(t *testing.T) {
	ws := writeScad(t, map[string]string{
		"001-two.scad": "color(\"red\") cube(5);\ncolor(\"blue\") translate([10, 0, 0]) sphere(3, $fn = 16);\n",
		"002-bad.scad": "cube(;\n",
		"003-2d.scad":  "square(4);\n",
	})
	eng := newTestEngine(t)
	dir := filepath.Join(t.TempDir(), "exports")
	req := func(f string) Request { return Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: f} }

	r, err := eng.RenderArtifact(context.Background(), req("001-two.scad"), wasmOpts(), ArtifactExt3MF, dir)
	if err != nil || r.Status != StatusOK || r.Engine != EngineWASM || !strings.HasSuffix(r.MeshPath, ".3mf") {
		t.Fatalf("3MF: %+v err=%v", r, err)
	}
	m, err := threemf.ReadFile(r.MeshPath)
	if err != nil {
		t.Fatal(err)
	}
	parts := threemf.Split(m)
	if len(parts) != 2 || parts[0].Color != (threemf.RGB{255, 0, 0}) || parts[1].Color != (threemf.RGB{0, 0, 255}) {
		t.Fatalf("partes: %+v", parts)
	}
	again, _ := eng.RenderArtifact(context.Background(), req("001-two.scad"), wasmOpts(), ArtifactExt3MF, dir)
	if !again.CacheHit || again.Engine != EngineWASM {
		t.Fatalf("segundo 3MF: %+v", again)
	}

	if bad, _ := eng.RenderArtifact(context.Background(), req("002-bad.scad"), wasmOpts(), ArtifactExt3MF, dir); bad.Status != StatusError || len(bad.Diagnostics) == 0 || bad.Diagnostics[0].Line != 1 {
		t.Errorf("error de sintaxis: %+v", bad)
	}
	if flat, _ := eng.RenderArtifact(context.Background(), req("003-2d.scad"), wasmOpts(), ArtifactExt3MF, dir); flat.Status != StatusError || len(flat.Diagnostics) == 0 {
		t.Errorf("2D: %+v", flat)
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if e.IsDir() {
			t.Errorf("quedó la carpeta temporal %s", e.Name())
		}
	}
}

// Cancelar el contexto de un render del integrado devuelve «cancelled» sin
// esperar a que el módulo termine.
func TestWASMRenderCancel(t *testing.T) {
	ws := writeScad(t, map[string]string{"001-long.scad": "minkowski() { difference() { sphere(10, $fn = 60); sphere(8, $fn = 60); } sphere(1, $fn = 40); }\n"})
	eng := newTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(1500 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	r, err := eng.Render(ctx, Request{WorkspaceKey: "w", WorkspaceDir: ws, FileName: "001-long.scad"}, wasmOpts())
	if err != nil || r.Status != StatusCancelled {
		t.Fatalf("render cancelado: %+v err=%v", r, err)
	}
	if d := time.Since(start); d > 20*time.Second {
		t.Fatalf("tardó %v en volver", d)
	}
}

func TestWASMSpec(t *testing.T) {
	t.Setenv("OPENSCADPATH", "")
	spec := WASMSpec("/ws", "/out", "-o", "/out/x.stl", "a.scad")
	if spec.Cwd != "/work" || spec.Mounts[0].Virtual != "/work" || !spec.Mounts[0].ReadOnly || spec.Mounts[0].Host != "/ws" {
		t.Fatalf("spec: %+v", spec)
	}
	if spec.Mounts[1].Virtual != "/out" || spec.Mounts[1].ReadOnly {
		t.Fatalf("salida: %+v", spec.Mounts)
	}
	if got := spec.Env["OPENSCADPATH"]; !strings.HasPrefix(got, "/work:/work/lib") {
		t.Fatalf("OPENSCADPATH = %q", got)
	}

	// Una carpeta de OPENSCADPATH existente se monta como /lib/0, solo lectura.
	lib := t.TempDir()
	t.Setenv("OPENSCADPATH", lib)
	spec = WASMSpec("/ws", "", "a.scad")
	var found bool
	for _, m := range spec.Mounts {
		if m.Host == lib && m.Virtual == "/lib/0" && m.ReadOnly {
			found = true
		}
		if m.Virtual == "/out" {
			t.Error("sin outDir no hay montaje /out")
		}
	}
	if !found || !strings.Contains(spec.Env["OPENSCADPATH"], "/lib/0") {
		t.Fatalf("spec: %+v", spec)
	}
}

// Una carpeta temporal de salida de un proceso que murió se limpia al podar; la
// joven (de un render en curso) no se toca.
func TestPruneRemovesStaleWASMDirs(t *testing.T) {
	eng := newTestEngine(t)
	stale := filepath.Join(eng.impl.cacheDir, wasmTempPrefix+"viejo")
	fresh := filepath.Join(eng.impl.cacheDir, wasmTempPrefix+"nuevo")
	for _, d := range []string{stale, fresh} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	eng.impl.prune()
	if _, err := os.Stat(stale); err == nil {
		t.Error("la carpeta vieja sigue ahí")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("se borró la carpeta reciente")
	}
}
