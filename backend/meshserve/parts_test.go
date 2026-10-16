package meshserve

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupParts arma <partes>/<clave>.parts/00.stl y 01.stl, y al lado, fuera de
// la caché, un archivo que ninguna ruta debería alcanzar.
func setupParts(t *testing.T) (http.Handler, *bool, string) {
	t.Helper()
	root := t.TempDir()
	meshes := filepath.Join(root, "cache", "meshes")
	parts := filepath.Join(root, "cache", "parts")
	dir := filepath.Join(parts, key+".parts")
	for _, d := range []string{meshes, dir, filepath.Join(parts, "sub")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		filepath.Join(dir, "00.stl"):           "parte cero \x00\x01",
		filepath.Join(dir, "01.stl"):           "parte uno",
		filepath.Join(dir, "manifest.json"):    `{"version":1}`,
		filepath.Join(dir, "100.stl"):          "tres dígitos",
		filepath.Join(parts, "sub", "00.stl"):  "otra carpeta",
		filepath.Join(root, "secret.stl"):      "secreto",
		filepath.Join(parts, "secret.stl"):     "secreto",
		filepath.Join(meshes, key+".stl"):      "malla",
		filepath.Join(parts, key+".parts.stl"): "secreto",
	}
	for p, body := range files {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})
	return MiddlewareWithParts(meshes, parts)(next), &called, dir
}

func TestServesParts(t *testing.T) {
	h, called, _ := setupParts(t)
	for name, body := range map[string]string{"00.stl": "parte cero \x00\x01", "01.stl": "parte uno"} {
		rec := do(h, http.MethodGet, "/parts/"+key+"/"+name)
		if rec.Code != http.StatusOK || rec.Body.String() != body {
			t.Fatalf("%s: código %d cuerpo %q", name, rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != ContentType {
			t.Errorf("%s: Content-Type %q", name, ct)
		}
		if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("%s: Cache-Control %q", name, cc)
		}
	}
	rec := do(h, http.MethodHead, "/parts/"+key+"/00.stl")
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != ContentType {
		t.Errorf("HEAD: código %d, cuerpo %d bytes", rec.Code, rec.Body.Len())
	}
	if *called {
		t.Error("una parte no debe llegar al siguiente handler")
	}
	// /mesh/ sigue sirviendo con el middleware combinado.
	if rec := do(h, http.MethodGet, "/mesh/"+key+".stl"); rec.Code != http.StatusOK || rec.Body.String() != "malla" {
		t.Errorf("/mesh/: código %d", rec.Code)
	}
}

func TestPartsRejectBadPaths(t *testing.T) {
	h, called, _ := setupParts(t)
	upper := strings.ToUpper(key)
	for _, target := range []string{
		"/parts/",
		"/parts/" + key,                           // sin archivo
		"/parts/" + key + "/",                     // sin nombre
		"/parts/" + key + "/00",                   // sin extensión
		"/parts/" + key + "/0.stl",                // un dígito
		"/parts/" + key + "/100.stl",              // tres dígitos
		"/parts/" + key + "/ab.stl",               // no numérico
		"/parts/" + key + "/00.STL",               // mayúsculas
		"/parts/" + key + "/00.stl/",              // barra final
		"/parts/" + key + "/manifest.json",        // el manifest no es una parte
		"/parts/" + key + "/00.stl.json",          // otra extensión
		"/parts/" + key + "/../secret.stl",        // escape literal
		"/parts/" + key + "/..%2fsecret.stl",      // escape codificado
		"/parts/" + key + "/%2e%2e/secret.stl",    // puntos codificados
		"/parts/" + key + "/..%5c00.stl",          // barra invertida
		"/parts/" + key + "/sub/00.stl",           // subcarpeta
		"/parts/" + key + "/00.stl%00.png",        // NUL
		"/parts/" + upper + "/00.stl",             // clave en mayúsculas
		"/parts/" + key[:63] + "/00.stl",          // clave corta
		"/parts/" + key + "0/00.stl",              // clave larga
		"/parts/" + key[:60] + "zzzz/00.stl",      // no hex
		"/parts/../secret.stl",                    // escape sin clave
		"/parts/%2e%2e/%2e%2e/secret.stl",         // escape doble codificado
		"/parts/sub/00.stl",                       // carpeta que no es una clave
		"/parts/" + key + ".parts/00.stl",         // el nombre interno de la carpeta
		"/parts/" + key + "/00.stl/../01.stl",     // normalizable pero no canónico
		"/parts//" + key + "/00.stl",              // barra doble
		"/parts/%30" + key[1:] + "/00.stl.gz",     // codificación + otra extensión
		"/parts/" + key + "%2f00.stl/../../x.stl", // barra codificada con escape
	} {
		rec := do(h, http.MethodGet, target)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: código %d, quiero 404", target, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "secreto") || strings.Contains(rec.Body.String(), "otra carpeta") {
			t.Errorf("%s: filtró un archivo que no es una parte", target)
		}
	}
	if *called {
		t.Error("una ruta /parts/ inválida no debe caer al siguiente handler (Vite devolvería index.html)")
	}
}

func TestPartsMissingAndMethods(t *testing.T) {
	h, _, _ := setupParts(t)
	other := strings.Repeat("a", 64)
	if rec := do(h, http.MethodGet, "/parts/"+other+"/00.stl"); rec.Code != http.StatusNotFound {
		t.Errorf("clave sin carpeta: código %d", rec.Code)
	}
	if rec := do(h, http.MethodGet, "/parts/"+key+"/07.stl"); rec.Code != http.StatusNotFound {
		t.Errorf("parte inexistente: código %d", rec.Code)
	}
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		rec := do(h, m, "/parts/"+key+"/00.stl")
		if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != "GET, HEAD" {
			t.Errorf("%s: código %d Allow %q", m, rec.Code, rec.Header().Get("Allow"))
		}
	}
}

func TestPartsDirectoryIsNotServed(t *testing.T) {
	h, _, dir := setupParts(t)
	// Una carpeta con nombre de parte no se sirve como archivo.
	if err := os.Mkdir(filepath.Join(dir, "05.stl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if rec := do(h, http.MethodGet, "/parts/"+key+"/05.stl"); rec.Code != http.StatusNotFound {
		t.Errorf("carpeta 05.stl: código %d", rec.Code)
	}
}

func TestPartsPassThroughWithoutDir(t *testing.T) {
	// Middleware(cacheDir) sin carpeta de partes: /parts/ no es asunto suyo.
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := Middleware(t.TempDir())(next)
	do(h, http.MethodGet, "/parts/"+key+"/00.stl")
	if !called {
		t.Error("sin carpeta de partes, /parts/ tiene que pasar al siguiente handler")
	}
	// Y otros prefijos parecidos pasan siempre.
	h, called2, _ := setupParts(t)
	for _, target := range []string{"/parts", "/partsx/" + key + "/00.stl", "/PARTS/" + key + "/00.stl", "/part/" + key + "/00.stl"} {
		*called2 = false
		do(h, http.MethodGet, target)
		if !*called2 {
			t.Errorf("%s: tiene que pasar al siguiente handler", target)
		}
	}
}

func TestValidPartPath(t *testing.T) {
	if k, f, ok := ValidPartPath(key + "/03.stl"); !ok || k != key || f != "03.stl" {
		t.Errorf("válida: %q %q %v", k, f, ok)
	}
	if _, _, ok := ValidPartPath(key + "/3.stl"); ok {
		t.Error("un dígito no es válido")
	}
}
