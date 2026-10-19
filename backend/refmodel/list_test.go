package refmodel

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListRefs(t *testing.T) {
	ws := t.TempDir()
	if got, err := List(ws); err != nil || len(got) != 0 {
		t.Fatalf("sin refs/: %v %v", got, err)
	}
	refs := filepath.Join(ws, "refs")
	if err := os.MkdirAll(filepath.Join(refs, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, size := range map[string]int{"b.stl": 10, "a.3mf": 20, "c.OBJ": 30, "nota.txt": 5, ".oculto.stl": 5} {
		if err := os.WriteFile(filepath.Join(refs, name), make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(refs, "sub", "x.stl"), []byte("x"), 0o644)
	outside := filepath.Join(t.TempDir(), "fuera.stl")
	_ = os.WriteFile(outside, []byte("x"), 0o644)
	_ = os.Symlink(outside, filepath.Join(refs, "enlace.stl")) // no se lista: sale de refs/
	_ = os.Symlink(filepath.Join(refs, "b.stl"), filepath.Join(refs, "alias.stl"))

	got, err := List(ws)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range got {
		names = append(names, r.Name)
	}
	want := []string{"a.3mf", "alias.stl", "b.stl", "c.OBJ"}
	if len(names) != len(want) && len(names) != len(want)-1 { // sin enlaces simbólicos (Windows)
		t.Fatalf("lista: %v", names)
	}
	if got[0].Name != "a.3mf" || got[0].Path != "refs/a.3mf" || got[0].Format != Format3MF || got[0].Bytes != 20 {
		t.Errorf("primera: %+v", got[0])
	}
	for _, n := range names {
		if n == "enlace.stl" || n == "nota.txt" || n == ".oculto.stl" {
			t.Errorf("%s no debía listarse", n)
		}
	}
}
