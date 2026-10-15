package meshserve

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const key = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// setup arma la caché en t.TempDir() con una malla válida y, al lado (fuera
// de la caché), un archivo que ninguna ruta debería alcanzar.
func setup(t *testing.T, body []byte) (http.Handler, *bool) {
	t.Helper()
	root := t.TempDir()
	cache := filepath.Join(root, "cache", "meshes")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, key+".stl"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, key+".json"), []byte(`{"status":"ok"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "secret.stl"), []byte("secreto"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})
	return Middleware(cache)(next), &called
}

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestServesMesh(t *testing.T) {
	body := []byte("binario de prueba \x00\x01\x02")
	h, called := setup(t, body)
	rec := do(h, http.MethodGet, "/mesh/"+key+".stl")
	if rec.Code != http.StatusOK {
		t.Fatalf("código %d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), body) {
		t.Error("el cuerpo no es la malla")
	}
	if ct := rec.Header().Get("Content-Type"); ct != ContentType {
		t.Errorf("Content-Type %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control %q", cc)
	}
	if *called {
		t.Error("una malla no debe llegar al siguiente handler")
	}

	rec = do(h, http.MethodHead, "/mesh/"+key+".stl")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != ContentType {
		t.Errorf("HEAD: código %d, cuerpo %d bytes, tipo %q", rec.Code, rec.Body.Len(), rec.Header().Get("Content-Type"))
	}
}

func TestRejectsBadNames(t *testing.T) {
	h, called := setup(t, []byte("x"))
	upper := strings.ToUpper(key)
	for _, target := range []string{
		"/mesh/",
		"/mesh/" + key,                    // sin extensión
		"/mesh/" + key + ".json",          // otra extensión de la caché
		"/mesh/" + key + ".STL",           // extensión en mayúsculas
		"/mesh/" + upper + ".stl",         // clave en mayúsculas
		"/mesh/" + key[:63] + ".stl",      // clave corta
		"/mesh/" + key + "0.stl",          // clave larga
		"/mesh/" + key + ".stl/",          // barra final
		"/mesh/" + key + "-001.stl",       // temporal de render
		"/mesh/../secret.stl",             // escape literal
		"/mesh/%2e%2e/secret.stl",         // escape codificado
		"/mesh/%2E%2E%2Fsecret.stl",       // escape con barra codificada
		"/mesh/..%5csecret.stl",           // barra invertida (Windows)
		"/mesh/sub/" + key + ".stl",       // subcarpeta
		"/mesh/" + key + ".stl%00.png",    // NUL
		"/mesh/" + key[:60] + "zzzz.stl",  // no hex
		"/mesh/%30" + key[1:] + ".stl.gz", // otra extensión con codificación
	} {
		rec := do(h, http.MethodGet, target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: código %d, quiero 404", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "secreto") {
			t.Errorf("%s: filtró un archivo fuera de la caché", target)
		}
	}
	if *called {
		t.Error("una ruta /mesh/ inválida no debe caer al siguiente handler (Vite devolvería index.html)")
	}
}

func TestMissingMeshIs404(t *testing.T) {
	h, _ := setup(t, []byte("x"))
	other := strings.Repeat("a", 64)
	if rec := do(h, http.MethodGet, "/mesh/"+other+".stl"); rec.Code != http.StatusNotFound {
		t.Errorf("código %d, quiero 404", rec.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h, _ := setup(t, []byte("x"))
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		rec := do(h, m, "/mesh/"+key+".stl")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: código %d, quiero 405", m, rec.Code)
		}
		if rec.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: Allow %q", m, rec.Header().Get("Allow"))
		}
	}
}

func TestPassesThroughOtherPaths(t *testing.T) {
	h, called := setup(t, []byte("x"))
	for _, target := range []string{"/", "/index.html", "/assets/app.js", "/mesh", "/meshes/" + key + ".stl", "/MESH/" + key + ".stl"} {
		*called = false
		rec := do(h, http.MethodGet, target)
		if !*called || rec.Code != http.StatusTeapot {
			t.Errorf("%s: no llegó al siguiente handler (código %d)", target, rec.Code)
		}
	}
	// Un POST fuera de /mesh/ tampoco es asunto del middleware.
	*called = false
	if do(h, http.MethodPost, "/wails/ipc"); !*called {
		t.Error("POST fuera de /mesh/ tiene que pasar")
	}
}

// Prueba de humo: una malla real de OpenSCAD, servida byte a byte.
func TestServesRealOpenSCADMesh(t *testing.T) {
	bin, err := exec.LookPath("openscad")
	if err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	dir := t.TempDir()
	scad := filepath.Join(dir, "m.scad")
	src := "color(\"orange\") difference(){ cylinder(r=10, h=5, $fn=96); translate([0,0,-1]) cylinder(r=4, h=7, $fn=48); }\n"
	if err := os.WriteFile(scad, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.stl")
	if b, err := exec.Command(bin, "-o", out, "--export-format", "binstl", scad).CombinedOutput(); err != nil {
		t.Fatalf("openscad: %v\n%s", err, b)
	}
	want, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(dir, "cache", "meshes")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, key+".stl"), want, 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Middleware(cache)(http.NotFoundHandler()))
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/mesh/" + key + ".stl")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != ContentType {
		t.Fatalf("código %d tipo %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("servido %d bytes, el STL tiene %d: no coinciden", len(got), len(want))
	}
	if resp.ContentLength != int64(len(want)) {
		t.Errorf("Content-Length %d, quiero %d", resp.ContentLength, len(want))
	}
}
