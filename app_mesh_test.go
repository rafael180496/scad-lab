package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/meshserve"
	"scadlab/backend/render"
)

// writeTetraSTL escribe un tetraedro rectángulo de catetos s (volumen s³/6)
// como STL binario.
func writeTetraSTL(t *testing.T, path string, s float32) {
	t.Helper()
	o, x, y, z := [3]float32{0, 0, 0}, [3]float32{s, 0, 0}, [3]float32{0, s, 0}, [3]float32{0, 0, s}
	tris := [][3][3]float32{{o, y, x}, {o, x, z}, {o, z, y}, {x, y, z}}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_, _ = f.Write(make([]byte, 80))
	_ = binary.Write(f, binary.LittleEndian, uint32(len(tris)))
	for _, tr := range tris {
		_ = binary.Write(f, binary.LittleEndian, [3]float32{})
		_ = binary.Write(f, binary.LittleEndian, tr)
		_ = binary.Write(f, binary.LittleEndian, uint16(0))
	}
}

func TestGetMeshInfo(t *testing.T) {
	data := t.TempDir()
	cache := filepath.Join(data, "cache", "meshes")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{dataDir: data}
	key := strings.Repeat("ab", 32)
	p := filepath.Join(cache, key+".stl")
	writeTetraSTL(t, p, 6)

	in, err := a.GetMeshInfo(p)
	if err != nil {
		t.Fatal(err)
	}
	if in.Triangles != 4 || math.Abs(in.VolumeMM3-36) > 1e-9 || !in.Watertight || in.Size != [3]float64{6, 6, 6} {
		t.Fatalf("info inesperada: %+v", in)
	}

	// Caché por clave: sin el archivo, la misma clave sigue respondiendo.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if again, err := a.GetMeshInfo(p); err != nil || again != in {
		t.Fatalf("la segunda lectura no salió de la caché: %+v, %v", again, err)
	}

	// Una clave nueva sin archivo es un error, no una Info vacía.
	missing := filepath.Join(cache, strings.Repeat("cd", 32)+".stl")
	if _, err := a.GetMeshInfo(missing); err == nil {
		t.Error("malla inexistente: quería error")
	}

	// Un STL roto dentro de la caché también.
	broken := filepath.Join(cache, strings.Repeat("ef", 32)+".stl")
	if err := os.WriteFile(broken, []byte("no es un stl"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetMeshInfo(broken); err == nil {
		t.Error("STL roto: quería error")
	}
}

func TestGetMeshInfoRejectsPathsOutsideCache(t *testing.T) {
	data := t.TempDir()
	cache := filepath.Join(data, "cache", "meshes")
	if err := os.MkdirAll(filepath.Join(cache, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := &App{dataDir: data}
	key := strings.Repeat("12", 32) + ".stl"
	// Un STL válido en cada lugar prohibido: el rechazo tiene que ser por la
	// ruta, no porque el archivo no exista.
	outside := filepath.Join(data, key)
	writeTetraSTL(t, outside, 1)
	writeTetraSTL(t, filepath.Join(cache, "sub", key), 1)
	other := filepath.Join(cache, "modelo.stl")
	writeTetraSTL(t, other, 1)

	for _, p := range []string{
		outside,
		filepath.Join(cache, "sub", key),
		other,
		filepath.Join(cache, strings.ToUpper(strings.Repeat("12", 32))+".stl"),
		filepath.Join(cache, strings.Repeat("12", 32)+".json"),
		key, // relativa
		"",
	} {
		if _, err := a.GetMeshInfo(p); err == nil {
			t.Errorf("%q: quería error", p)
		}
	}

	if _, err := (&App{}).GetMeshInfo(filepath.Join(cache, key)); err == nil {
		t.Error("sin directorio de datos: quería error")
	}
}

// De punta a punta con OpenSCAD real: el MeshPath que devuelve el render es
// aceptado por GetMeshInfo y servido por el middleware con la misma URL que
// arma el frontend ('/mesh/' + basename).
func TestRenderedMeshInfoAndServe(t *testing.T) {
	if _, err := exec.LookPath("openscad"); err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	a := newTestApp(t)
	// newTestApp pone la caché en <datos>/cache; la app real (app.go) la pone
	// en <datos>/cache/meshes, que es lo único que acepta GetMeshInfo.
	cacheDir := filepath.Join(a.dataDir, "cache", "meshes")
	eng, err := render.NewEngine(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	a.engine = eng

	w, err := a.CreateWorkspace("Prueba malla", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	src := "// @checkpoint parent= desc=\"cubo\"\ncolor(\"orange\") cube([20, 10, 5]);\n"
	if err := os.WriteFile(filepath.Join(w.Path, "001-cubo.scad"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := a.RenderCheckpoint(w.ID, "001-cubo.scad")
	if err != nil || r.Status != render.StatusOK {
		t.Fatalf("render: %v %+v", err, r)
	}
	in, err := a.GetMeshInfo(r.MeshPath)
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(in.VolumeMM3-1000) > 1e-6 || in.Size != [3]float64{20, 10, 5} || !in.Watertight || in.Triangles != 12 {
		t.Fatalf("info inesperada: %+v", in)
	}

	want, err := os.ReadFile(r.MeshPath)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	meshserve.Middleware(cacheDir)(http.NotFoundHandler()).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/mesh/"+filepath.Base(r.MeshPath), nil))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), want) {
		t.Fatalf("middleware: código %d, %d bytes de %d", rec.Code, rec.Body.Len(), len(want))
	}
}
