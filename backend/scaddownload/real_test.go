package scaddownload

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestRealInstall descarga el OpenSCAD de verdad (60 MB) a una carpeta
// temporal, lo ejecuta y lo borra. Solo corre con SCADLAB_REAL_DOWNLOAD=1: la
// red no es parte de la verificación normal.
func TestRealInstall(t *testing.T) {
	if os.Getenv("SCADLAB_REAL_DOWNLOAD") != "1" {
		t.Skip("define SCADLAB_REAL_DOWNLOAD=1 para descargar OpenSCAD de verdad")
	}
	a, ok := ForPlatform(runtime.GOOS, runtime.GOARCH)
	if !ok {
		t.Skip("sin descarga para esta plataforma")
	}
	data := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	events, last := 0, Progress{}
	bin, err := Install(ctx, data, a, func(p Progress) {
		events++
		last = p
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("instalado en %s (%d avisos, último %+v)", bin, events, last)
	if bin != InstalledBinary(data) {
		t.Errorf("InstalledBinary = %q, Install = %q", InstalledBinary(data), bin)
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	t.Logf("--version: %s", strings.TrimSpace(string(out)))
	if err != nil || !strings.Contains(string(out), Version) {
		t.Fatalf("--version: %v %q", err, out)
	}
	scad := filepath.Join(data, "cube.scad")
	stl := filepath.Join(data, "cube.stl")
	if err := os.WriteFile(scad, []byte("cube([10,10,10]);"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "-o", stl, scad).CombinedOutput(); err != nil {
		t.Fatalf("render: %v %s", err, out)
	}
	if st, err := os.Stat(stl); err != nil || st.Size() < 100 {
		t.Fatalf("stl: %v %v", st, err)
	}
	if runtime.GOOS == "darwin" {
		lipo, _ := exec.Command("/usr/bin/lipo", "-info", bin).CombinedOutput()
		t.Logf("lipo: %s", strings.TrimSpace(string(lipo)))
		du, _ := exec.Command("/usr/bin/du", "-sh", InstallDir(data)).CombinedOutput()
		t.Logf("tamaño instalado: %s", strings.TrimSpace(string(du)))
	}
	if err := Remove(data); err != nil {
		t.Fatal(err)
	}
	if InstalledBinary(data) != "" {
		t.Error("Remove no quitó la instalación")
	}
}
