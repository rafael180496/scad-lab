package scaddownload

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeZip arma un zip como el de Windows: una carpeta raíz con openscad.exe.
func fakeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// testAsset sirve body por HTTP y devuelve el Asset de prueba que lo describe
// (la tabla real queda fija: Install se prueba con un Asset inyectado).
func testAsset(t *testing.T, body []byte) Asset {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return Asset{OS: "windows", Arch: "amd64", URL: srv.URL + "/x.zip", SHA256: sum(body), Kind: KindZIP, Size: int64(len(body))}
}

func goodZip(t *testing.T) []byte {
	return fakeZip(t, map[string]string{
		"OpenSCAD-test/openscad.exe":         "MZ fake",
		"OpenSCAD-test/color-schemes/a.json": "{}",
	})
}

func noValidate(t *testing.T) {
	t.Helper()
	old := validateBinary
	validateBinary = func(context.Context, string) error { return nil }
	t.Cleanup(func() { validateBinary = old })
}

// entries lista lo que hay en la carpeta (vacía o inexistente = nada).
func entries(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func assertNothingLeft(t *testing.T, data string) {
	t.Helper()
	var left []string
	_ = filepath.WalkDir(data, func(p string, d os.DirEntry, err error) error {
		if err == nil && p != data {
			left = append(left, p)
		}
		return nil
	})
	if len(left) > 0 {
		t.Errorf("quedó basura: %v", left)
	}
}

func TestInstallZip(t *testing.T) {
	noValidate(t)
	data := t.TempDir()
	a := testAsset(t, goodZip(t))
	var phases []string
	bin, err := Install(context.Background(), data, a, func(p Progress) {
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(InstallDir(data), "OpenSCAD-test", "openscad.exe")
	if bin != want {
		t.Errorf("binario = %q, quería %q", bin, want)
	}
	if got := findBinary(InstallDir(data), "windows"); got != want {
		t.Errorf("findBinary = %q", got)
	}
	if strings.Join(phases, ",") != "download,verify,extract,done" {
		t.Errorf("fases = %v", phases)
	}
	// Solo la carpeta de la versión: ningún .tmp- ni .part.
	if got := entries(t, toolsDir(data)); len(got) != 1 || got[0] != Version {
		t.Errorf("contenido de tools/openscad = %v", got)
	}
	if _, err := os.Stat(filepath.Join(InstallDir(data), "OpenSCAD-test", "color-schemes", "a.json")); err != nil {
		t.Errorf("no se extrajo un archivo anidado: %v", err)
	}
	if err := Remove(data); err != nil {
		t.Fatal(err)
	}
	if got := findBinary(InstallDir(data), "windows"); got != "" {
		t.Errorf("Remove dejó %q", got)
	}
	if err := Remove(data); err != nil {
		t.Errorf("quitar dos veces no es un error: %v", err)
	}
}

func TestInstallBadChecksum(t *testing.T) {
	noValidate(t)
	data := t.TempDir()
	a := testAsset(t, goodZip(t))
	a.SHA256 = strings.Repeat("0", 64)
	_, err := Install(context.Background(), data, a, nil)
	if !errors.Is(err, ErrChecksum) {
		t.Fatalf("error = %v, quería ErrChecksum", err)
	}
	assertNothingLeft(t, data)
}

func TestInstallHTTPError(t *testing.T) {
	data := t.TempDir()
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	a := Asset{OS: "windows", Arch: "amd64", URL: srv.URL, SHA256: strings.Repeat("a", 64), Kind: KindZIP, Size: 10}
	_, err := Install(context.Background(), data, a, nil)
	if !errors.Is(err, ErrHTTP) || !strings.Contains(err.Error(), "404") {
		t.Fatalf("error = %v, quería ErrHTTP con 404", err)
	}
	assertNothingLeft(t, data)
}

func TestInstallNoNetwork(t *testing.T) {
	data := t.TempDir()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nadie escucha
	a := Asset{OS: "windows", Arch: "amd64", URL: url, SHA256: strings.Repeat("a", 64), Kind: KindZIP, Size: 10}
	_, err := Install(context.Background(), data, a, nil)
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("error = %v, quería ErrNetwork", err)
	}
	assertNothingLeft(t, data)
}

// Cancelar a mitad de la descarga: el error es la cancelación y no queda nada.
func TestInstallCancelMidway(t *testing.T) {
	data := t.TempDir()
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "10000000")
		_, _ = w.Write(bytes.Repeat([]byte("x"), 1<<20))
		w.(http.Flusher).Flush()
		started <- struct{}{}
		<-r.Context().Done()
	}))
	defer srv.Close()
	a := Asset{OS: "windows", Arch: "amd64", URL: srv.URL, SHA256: strings.Repeat("a", 64), Kind: KindZIP, Size: 10000000}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-started; cancel() }()
	_, err := Install(ctx, data, a, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, quería context.Canceled", err)
	}
	assertNothingLeft(t, data)
}

// Sin datos durante stallTimeout la descarga se da por colgada.
func TestInstallStall(t *testing.T) {
	old := stallTimeout
	stallTimeout = 150 * time.Millisecond
	t.Cleanup(func() { stallTimeout = old })
	data := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("abc"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	a := Asset{OS: "windows", Arch: "amd64", URL: srv.URL, SHA256: strings.Repeat("a", 64), Kind: KindZIP, Size: 1000}
	_, err := Install(context.Background(), data, a, nil)
	if !errors.Is(err, ErrNetwork) || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("error = %v, quería ErrNetwork por corte", err)
	}
	assertNothingLeft(t, data)
}

// Sin espacio libre: falla antes de pedir nada al servidor.
func TestInstallNoSpace(t *testing.T) {
	data := t.TempDir()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1) }))
	defer srv.Close()
	a := Asset{OS: "windows", Arch: "amd64", URL: srv.URL, SHA256: strings.Repeat("a", 64), Kind: KindZIP, Size: 1 << 55}
	_, err := Install(context.Background(), data, a, nil)
	if !errors.Is(err, ErrNoSpace) {
		t.Fatalf("error = %v, quería ErrNoSpace", err)
	}
	if hits.Load() != 0 {
		t.Error("pidió el archivo aun sin espacio")
	}
	assertNothingLeft(t, data)
}

func TestInstallUnsupportedAsset(t *testing.T) {
	_, err := Install(context.Background(), t.TempDir(), Asset{}, nil)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error = %v", err)
	}
}

// Una reinstalación fallida no toca la anterior.
func TestInstallFailureKeepsPrevious(t *testing.T) {
	noValidate(t)
	data := t.TempDir()
	if _, err := Install(context.Background(), data, testAsset(t, goodZip(t)), nil); err != nil {
		t.Fatal(err)
	}
	before := findBinary(InstallDir(data), "windows")
	bad := testAsset(t, goodZip(t))
	bad.SHA256 = strings.Repeat("0", 64)
	if _, err := Install(context.Background(), data, bad, nil); !errors.Is(err, ErrChecksum) {
		t.Fatalf("error = %v", err)
	}
	if got := findBinary(InstallDir(data), "windows"); got != before || got == "" {
		t.Errorf("la instalación anterior cambió: %q → %q", before, got)
	}
	if got := entries(t, toolsDir(data)); len(got) != 1 {
		t.Errorf("contenido = %v", got)
	}
}

// Reinstalar sobre una instalación existente la reemplaza.
func TestInstallReplaces(t *testing.T) {
	noValidate(t)
	data := t.TempDir()
	if _, err := Install(context.Background(), data, testAsset(t, goodZip(t)), nil); err != nil {
		t.Fatal(err)
	}
	z2 := fakeZip(t, map[string]string{"OpenSCAD-new/openscad.exe": "MZ new"})
	if _, err := Install(context.Background(), data, testAsset(t, z2), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(InstallDir(data), "OpenSCAD-test")); err == nil {
		t.Error("quedó la carpeta de la instalación anterior")
	}
	if findBinary(InstallDir(data), "windows") == "" {
		t.Error("no hay binario tras reinstalar")
	}
	if got := entries(t, toolsDir(data)); len(got) != 1 {
		t.Errorf("contenido = %v", got)
	}
}

func TestInstallZipSlip(t *testing.T) {
	noValidate(t)
	data := t.TempDir()
	z := fakeZip(t, map[string]string{"../evil.txt": "x", "OpenSCAD-test/openscad.exe": "MZ"})
	_, err := Install(context.Background(), data, testAsset(t, z), nil)
	if err == nil {
		t.Fatal("un zip con ../ debería rechazarse")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(data), "evil.txt")); err == nil {
		t.Error("escribió fuera de la carpeta de destino")
	}
	assertNothingLeft(t, data)
}

func TestInstallMissingBinary(t *testing.T) {
	noValidate(t)
	data := t.TempDir()
	z := fakeZip(t, map[string]string{"OpenSCAD-test/readme.txt": "x"})
	if _, err := Install(context.Background(), data, testAsset(t, z), nil); err == nil {
		t.Fatal("un zip sin openscad.exe debería fallar")
	}
	assertNothingLeft(t, data)
}

func TestInstallValidationFails(t *testing.T) {
	old := validateBinary
	validateBinary = func(context.Context, string) error { return errors.New("no arranca") }
	t.Cleanup(func() { validateBinary = old })
	data := t.TempDir()
	if _, err := Install(context.Background(), data, testAsset(t, goodZip(t)), nil); err == nil {
		t.Fatal("si el binario no valida no se instala")
	}
	assertNothingLeft(t, data)
}

// El progreso se acota a ~10 avisos por segundo aunque lleguen miles de lecturas.
func TestThrottle(t *testing.T) {
	n := 0
	th := &throttle{f: func(Progress) { n++ }, every: 50 * time.Millisecond}
	th.emit(Progress{Phase: PhaseDownload}, true) // el primero pasa
	for i := 0; i < 10000; i++ {
		th.emit(Progress{Phase: PhaseDownload, Received: int64(i)}, false)
	}
	if n != 1 {
		t.Fatalf("pasaron %d avisos en un instante, quería 1", n)
	}
	time.Sleep(60 * time.Millisecond)
	th.emit(Progress{Phase: PhaseDownload}, false)
	th.emit(Progress{Phase: PhaseVerify}, true) // una fase nueva siempre pasa
	if n != 3 {
		t.Fatalf("avisos = %d, quería 3", n)
	}
	var nilT *throttle
	nilT.emit(Progress{}, true) // sin callback no hace nada
}

func TestForPlatform(t *testing.T) {
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, c := range []struct{ os, arch, kind string }{
		{"darwin", "arm64", KindDMG}, {"darwin", "amd64", KindDMG}, {"windows", "amd64", KindZIP},
	} {
		a, ok := ForPlatform(c.os, c.arch)
		if !ok {
			t.Errorf("%s/%s debería tener descarga", c.os, c.arch)
			continue
		}
		if a.Kind != c.kind || !hex64.MatchString(a.SHA256) || !strings.HasPrefix(a.URL, "https://files.openscad.org/snapshots/") ||
			!strings.Contains(a.URL, Version) || a.Size <= 0 {
			t.Errorf("%s/%s: asset inválido %+v", c.os, c.arch, a)
		}
	}
	for _, c := range [][2]string{{"linux", "amd64"}, {"windows", "arm64"}, {"darwin", "386"}} {
		if _, ok := ForPlatform(c[0], c[1]); ok {
			t.Errorf("%s/%s no debería tener descarga", c[0], c[1])
		}
	}
}

func TestInstallDirLayout(t *testing.T) {
	d := InstallDir(filepath.Join("x", "datos"))
	if want := filepath.Join("x", "datos", "tools", "openscad", Version); d != want {
		t.Errorf("InstallDir = %q, quería %q", d, want)
	}
	if InstalledBinary("") != "" || InstalledBinary(t.TempDir()) != "" {
		t.Error("sin instalación debería devolver vacío")
	}
}

func TestSweepStale(t *testing.T) {
	tools := t.TempDir()
	old := filepath.Join(tools, ".tmp-old")
	fresh := filepath.Join(tools, ".tmp-fresh")
	keep := filepath.Join(tools, Version)
	for _, d := range []string{old, fresh, keep} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	sweepStale(tools)
	if got := entries(t, tools); len(got) != 2 {
		t.Errorf("tras barrer: %v (debería quedar la fresca y la instalada)", got)
	}
}
