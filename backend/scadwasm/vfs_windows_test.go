//go:build windows

package scadwasm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// En Windows el disco habla con letra de unidad y barras invertidas, y el
// motor con rutas virtuales de Unix: las dos mitades tienen que encontrarse.
func TestWindowsHostPaths(t *testing.T) {
	ws := t.TempDir() // C:\Users\...\Temp\...
	if !strings.Contains(ws, `:\`) {
		t.Skipf("la ruta del disco no lleva letra de unidad: %s", ws)
	}
	if err := os.MkdirAll(filepath.Join(ws, "lib", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "lib", "sub", "a.scad"), []byte("cube(1);"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := newProc(RunSpec{Cwd: "/work", Mounts: []Mount{{Host: ws, Virtual: "/work", ReadOnly: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if st, e := p.fs.stat("/work/lib/sub/a.scad"); e != errnoOK || st.size != 8 {
		t.Fatalf("stat por ruta virtual: %+v %d", st, e)
	}
	if _, e := p.fs.stat("/work/lib/../../Windows"); e != errnoNoEnt {
		t.Errorf(".. no puede salir del montaje: %d", e)
	}
	got := rewritePaths("WARNING: x in file /work/lib/sub/a.scad, line 3", p.mounts)
	want := "WARNING: x in file " + filepath.Join(ws, "lib", "sub", "a.scad") + ", line 3"
	if got != want {
		t.Errorf("reescritura:\n got %q\nwant %q", got, want)
	}
}
