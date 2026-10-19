package mcp

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeLib arma un zip con la forma del de GitHub (<repo>-<commit>/…) y lo deja
// como la única librería del catálogo mientras dura la prueba. Nada sale a la red.
type zipEntry struct {
	name string
	body string
	mode os.FileMode // 0 = archivo normal
}

func fakeLib(t *testing.T, entries []zipEntry) libSpec {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(e.body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	spec := libSpec{
		Name: "FakeLib", Description: "x", Version: "v1", Commit: "abc", License: "MIT",
		URL: "https://example.invalid/fake.zip", SHA256: hex.EncodeToString(sum[:]), Size: int64(buf.Len()),
		Marker: "std.scad", Include: "include <lib/FakeLib/std.scad>",
	}
	oldCatalog, oldFetch := libraryCatalog, libraryFetch
	libraryCatalog = []libSpec{spec}
	data := buf.Bytes()
	libraryFetch = func(ctx context.Context, url string) (io.ReadCloser, int64, error) {
		if url != spec.URL {
			t.Errorf("se pidió una URL que no es la del catálogo: %s", url)
		}
		return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
	}
	t.Cleanup(func() { libraryCatalog, libraryFetch = oldCatalog, oldFetch })
	return spec
}

var goodLib = []zipEntry{
	{name: "Fake-abc/"},
	{name: "Fake-abc/std.scad", body: "module caja() { cube(1); }\n"},
	{name: "Fake-abc/sub/"},
	{name: "Fake-abc/sub/extra.scad", body: "x = 1;\n"},
	{name: "Fake-abc/README.md", body: "hola"},
}

func TestInstallLibraryExtractsWithoutTheRootFolder(t *testing.T) {
	spec := fakeLib(t, goodLib)
	tb, ws := newToolbox(t, false)

	before := tb.mustJSON(t, "list_libraries", nil)
	libs := before["libraries"].([]any)
	if len(libs) != 1 || libs[0].(map[string]any)["installed"] != false || libs[0].(map[string]any)["include"] != spec.Include {
		t.Fatalf("list_libraries: %v", before)
	}

	got := tb.mustJSON(t, "install_library", map[string]any{"name": "fakelib"}) // sin importar mayúsculas
	if got["installed"] != true || got["alreadyInstalled"] != false || got["path"] != "lib/FakeLib" || got["files"].(float64) != 3 || got["include"] != spec.Include {
		t.Fatalf("install_library: %v", got)
	}
	for rel, want := range map[string]string{"lib/FakeLib/std.scad": "module caja()", "lib/FakeLib/sub/extra.scad": "x = 1;", "lib/LIBRARIES.txt": "FakeLib v1 (MIT) commit abc sha256 " + spec.SHA256} {
		raw, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(rel)))
		if err != nil || !strings.Contains(string(raw), want) {
			t.Errorf("%s: %v %q", rel, err, raw)
		}
	}
	// Sin restos del temporal ni de la descarga.
	ents, _ := os.ReadDir(filepath.Join(ws, "lib"))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".") {
			t.Errorf("quedó un temporal: %s", e.Name())
		}
	}
	if l := tb.mustJSON(t, "list_libraries", nil)["libraries"].([]any)[0].(map[string]any); l["installed"] != true {
		t.Errorf("list_libraries no la ve instalada: %v", l)
	}

	// Segunda vez: no se toca (los checkpoints que la usan son inmutables).
	if err := os.WriteFile(filepath.Join(ws, "lib", "FakeLib", "mio.scad"), []byte("// del usuario"), 0o644); err != nil {
		t.Fatal(err)
	}
	again := tb.mustJSON(t, "install_library", map[string]any{"name": "FakeLib"})
	if again["alreadyInstalled"] != true {
		t.Errorf("segunda instalación: %v", again)
	}
	if _, err := os.Stat(filepath.Join(ws, "lib", "FakeLib", "mio.scad")); err != nil {
		t.Error("la segunda instalación tocó la carpeta")
	}

	// Una carpeta propia de lib/ aparece aparte.
	if err := os.MkdirAll(filepath.Join(ws, "lib", "MiLibreria"), 0o755); err != nil {
		t.Fatal(err)
	}
	if o := tb.mustJSON(t, "list_libraries", nil)["otherInLib"].([]any); len(o) != 1 || o[0] != "lib/MiLibreria" {
		t.Errorf("otherInLib: %v", o)
	}

}

// Un checkpoint que incluye la librería instalada renderiza (la ruta relativa
// lib/… funciona sin configurar nada).
func TestInstalledLibraryRendersThroughInclude(t *testing.T) {
	needOpenSCAD(t)
	fakeLib(t, goodLib)
	tb, _ := newToolbox(t, false)
	tb.mustJSON(t, "install_library", map[string]any{"name": "FakeLib"})
	src := "include <lib/FakeLib/std.scad>\ncolor(\"red\") caja();\n"
	name := tb.mustJSON(t, "create_checkpoint", map[string]any{"label": "usa lib", "source": src})["file"].(string)
	if r := tb.mustJSON(t, "render", map[string]any{"file": name}); r["status"] != "ok" {
		t.Errorf("el include de la librería instalada no renderiza: %v", r)
	}
}

func TestInstallLibraryRejectsUnknownNamesAndNeverFetchesAnArbitraryURL(t *testing.T) {
	spec := fakeLib(t, goodLib) // su libraryFetch comprueba que solo se pida la URL del catálogo
	tb, _ := newToolbox(t, false)
	for _, n := range []string{"", "BOSL3", "../lib", "https://evil.example/x.zip", "FakeLib/../x", "FakeLib\x00", "lib/FakeLib"} {
		tb.fails(t, "install_library", map[string]any{"name": n})
	}
	// Un argumento `url` de más se ignora: se baja la del catálogo.
	got := tb.mustJSON(t, "install_library", map[string]any{"name": "FakeLib", "url": "https://evil.example/x.zip"})
	if got["installed"] != true || got["include"] != spec.Include {
		t.Errorf("install: %v", got)
	}
}

func TestInstallLibraryChecksTheSHABeforeExtracting(t *testing.T) {
	spec := fakeLib(t, goodLib)
	bad := spec
	bad.SHA256 = strings.Repeat("0", 64)
	libraryCatalog = []libSpec{bad}
	tb, ws := newToolbox(t, false)
	out := tb.fails(t, "install_library", map[string]any{"name": "FakeLib"})
	if !strings.Contains(out.Text, "SHA-256") {
		t.Errorf("mensaje: %q", out.Text)
	}
	if _, err := os.Stat(filepath.Join(ws, "lib", "FakeLib")); err == nil {
		t.Error("se instaló aunque el SHA-256 no coincidía")
	}
	assertNoLibLeftovers(t, ws)
}

func assertNoLibLeftovers(t *testing.T, ws string) {
	t.Helper()
	ents, _ := os.ReadDir(filepath.Join(ws, "lib"))
	for _, e := range ents {
		t.Errorf("lib/ debía quedar vacía: %s", e.Name())
	}
}

func TestInstallLibraryRejectsHostileZips(t *testing.T) {
	cases := map[string][]zipEntry{
		"zip slip":            {{name: "R/std.scad", body: "x"}, {name: "R/../../fuera.scad", body: "x"}},
		"ruta absoluta":       {{name: "R/std.scad", body: "x"}, {name: "/etc/passwd", body: "x"}},
		"barra invertida":     {{name: "R/std.scad", body: "x"}, {name: "R\\..\\..\\x.scad", body: "x"}},
		"unidad de Windows":   {{name: "R/std.scad", body: "x"}, {name: "C:/x.scad", body: "x"}},
		"sin el archivo base": {{name: "R/otra.scad", body: "x"}},
		"dentro de ..":        {{name: "R/std.scad", body: "x"}, {name: "R/a/../../b.scad", body: "x"}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			fakeLib(t, entries)
			tb, ws := newToolbox(t, false)
			tb.fails(t, "install_library", map[string]any{"name": "FakeLib"})
			if _, err := os.Stat(filepath.Join(ws, "lib", "FakeLib")); err == nil {
				t.Error("se instaló un zip hostil")
			}
			assertNoLibLeftovers(t, ws)
			// Nada salió del workspace.
			if _, err := os.Stat(filepath.Join(filepath.Dir(ws), "fuera.scad")); err == nil {
				t.Error("se escribió fuera del workspace")
			}
		})
	}
	t.Run("enlaces simbólicos se omiten", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip()
		}
		fakeLib(t, append(append([]zipEntry{}, goodLib...), zipEntry{name: "Fake-abc/enlace", body: "/etc/passwd", mode: os.ModeSymlink | 0o777}))
		tb, ws := newToolbox(t, false)
		got := tb.mustJSON(t, "install_library", map[string]any{"name": "FakeLib"})
		if got["files"].(float64) != 3 {
			t.Errorf("el enlace no cuenta: %v", got)
		}
		if _, err := os.Lstat(filepath.Join(ws, "lib", "FakeLib", "enlace")); err == nil {
			t.Error("se extrajo un enlace simbólico")
		}
	})
}

func TestInstallLibraryNetworkAndLinkedLibFolder(t *testing.T) {
	fakeLib(t, goodLib)
	tb, ws := newToolbox(t, false)

	libraryFetch = func(ctx context.Context, url string) (io.ReadCloser, int64, error) {
		return nil, 0, errors.New("dial tcp: no such host")
	}
	out := tb.fails(t, "install_library", map[string]any{"name": "FakeLib"})
	if !strings.Contains(out.Text, "internet") {
		t.Errorf("sin red: %q", out.Text)
	}
	assertNoLibLeftovers(t, ws)

	// Descarga cortada a la mitad: tampoco instala nada.
	libraryFetch = func(ctx context.Context, url string) (io.ReadCloser, int64, error) {
		return io.NopCloser(io.MultiReader(strings.NewReader("PK\x03\x04"), errReader{})), 0, nil
	}
	tb.fails(t, "install_library", map[string]any{"name": "FakeLib"})
	assertNoLibLeftovers(t, ws)

	// Cancelada: no instala ni deja restos.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	libraryFetch = func(ctx context.Context, url string) (io.ReadCloser, int64, error) { return nil, 0, ctx.Err() }
	if out, _ := tb.Call(ctx, "install_library", map[string]any{"name": "FakeLib"}); !out.IsError {
		t.Error("cancelada debía ser un error")
	}

	// lib/ es un enlace que sale del workspace: no se usa.
	if runtime.GOOS == "windows" {
		return
	}
	spec := fakeLib(t, goodLib)
	_ = spec
	_ = os.RemoveAll(filepath.Join(ws, "lib"))
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws, "lib")); err != nil {
		t.Skip(err)
	}
	tb.fails(t, "install_library", map[string]any{"name": "FakeLib"})
	tb.fails(t, "list_libraries", nil)
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Errorf("se escribió fuera del workspace: %v", ents)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

func TestLibraryCatalogIsPinned(t *testing.T) {
	seen := map[string]bool{}
	for _, l := range libraryCatalog {
		if seen[l.Name] {
			t.Errorf("%s repetida", l.Name)
		}
		seen[l.Name] = true
		if len(l.SHA256) != 64 || len(l.Commit) != 40 || l.Size <= 0 || l.Marker == "" || l.License == "" || l.Version == "" {
			t.Errorf("%s: ficha incompleta: %+v", l.Name, l)
		}
		// Una URL fija a un commit, no a una rama: lo bajado no cambia solo.
		if !strings.HasPrefix(l.URL, "https://codeload.github.com/") || !strings.HasSuffix(l.URL, "/zip/"+l.Commit) {
			t.Errorf("%s: la URL tiene que ser https y apuntar al commit %s: %s", l.Name, l.Commit, l.URL)
		}
		if !strings.Contains(l.Include, "lib/"+l.Name+"/") {
			t.Errorf("%s: include %q", l.Name, l.Include)
		}
	}
	for _, want := range []string{"BOSL2", "MCAD", "NopSCADlib"} {
		if !seen[want] {
			t.Errorf("falta %s en el catálogo", want)
		}
	}
}

// Con SCADLAB_REAL_DOWNLOAD=1 baja las tres librerías del catálogo de verdad
// (7 + 30 MB), comprueba sus SHA-256 y renderiza algo con cada una usando el
// OpenSCAD instalado. La red no es parte de la verificación normal.
func TestRealInstallCatalog(t *testing.T) {
	if os.Getenv("SCADLAB_REAL_DOWNLOAD") != "1" {
		t.Skip("define SCADLAB_REAL_DOWNLOAD=1 para descargar las librerías de verdad")
	}
	needOpenSCAD(t)
	uses := map[string]string{
		"BOSL2":      "include <lib/BOSL2/std.scad>\ninclude <lib/BOSL2/threading.scad>\ncolor(\"orange\") threaded_rod(d = 12, l = 20, pitch = 1.75, anchor = BOTTOM);\n",
		"MCAD":       "use <lib/MCAD/boxes.scad>\ncolor(\"gray\") roundedBox([40, 30, 10], 3, true);\n",
		"NopSCADlib": "include <lib/NopSCADlib/lib.scad>\ncolor(\"silver\") screw(M3_cap_screw, 10);\n",
	}
	for _, spec := range libraryCatalog {
		t.Run(spec.Name, func(t *testing.T) {
			tb, ws := newToolbox(t, false)
			got := tb.mustJSON(t, "install_library", map[string]any{"name": spec.Name})
			t.Logf("instalada: files=%v bytes=%v", got["files"], got["sizeBytes"])
			if got["installed"] != true || got["files"].(float64) < 20 {
				t.Fatalf("install: %v", got)
			}
			if _, err := os.Stat(filepath.Join(ws, "lib", spec.Name, spec.Marker)); err != nil {
				t.Fatal(err)
			}
			name := tb.mustJSON(t, "create_checkpoint", map[string]any{"label": "usa " + spec.Name, "source": uses[spec.Name]})["file"].(string)
			if r := tb.mustJSON(t, "render", map[string]any{"file": name}); r["status"] != "ok" {
				t.Fatalf("render con %s: %v", spec.Name, r)
			}
		})
	}
}
