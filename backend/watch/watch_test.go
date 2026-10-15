package watch

import (
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

// recorder junta las llamadas a onChange para inspeccionarlas desde el test.
type recorder struct {
	mu    sync.Mutex
	calls [][]string
}

func (r *recorder) on(added []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, added)
}

func (r *recorder) snapshot() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.calls...)
}

// waitCalls espera hasta tener n llamadas (o falla a los 5 s).
func (r *recorder) waitCalls(t *testing.T, n int) [][]string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if c := r.snapshot(); len(c) >= n {
			return c
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("se esperaban %d avisos, hubo %v", n, r.snapshot())
	return nil
}

func write(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("cube(1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func start(t *testing.T, dir string, r *recorder) *Watcher {
	t.Helper()
	w, err := Start(dir, r.on)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func TestExistingFilesAreNotNew(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "001-base.scad")
	r := &recorder{}
	start(t, dir, r)
	write(t, dir, "002-lid.scad")
	got := r.waitCalls(t, 1)
	if !reflect.DeepEqual(got[0], []string{"002-lid.scad"}) {
		t.Fatalf("nuevos = %v, se esperaba solo 002-lid.scad", got[0])
	}
}

func TestBurstIsDebouncedIntoOneCall(t *testing.T) {
	dir := t.TempDir()
	r := &recorder{}
	start(t, dir, r)
	// Tres archivos seguidos (un agente que escribe varios) dentro de la
	// ventana de debounce: un solo aviso con los tres.
	write(t, dir, "001-a.scad")
	write(t, dir, "002-b.scad")
	write(t, dir, "003-c.scad")
	got := r.waitCalls(t, 1)
	time.Sleep(2 * Debounce)
	if n := len(r.snapshot()); n != 1 {
		t.Fatalf("hubo %d avisos, se esperaba 1: %v", n, r.snapshot())
	}
	if !reflect.DeepEqual(got[0], []string{"001-a.scad", "002-b.scad", "003-c.scad"}) {
		t.Fatalf("nuevos = %v", got[0])
	}
}

func TestNonScadIsIgnored(t *testing.T) {
	dir := t.TempDir()
	r := &recorder{}
	start(t, dir, r)
	if err := os.WriteFile(filepath.Join(dir, "RENDER_ERRORS.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "printer.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * Debounce)
	if c := r.snapshot(); len(c) != 0 {
		t.Fatalf("no debería avisar por archivos que no son .scad: %v", c)
	}
}

func TestModifyAndDeleteNotifyWithoutNewFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "001-base.scad")
	r := &recorder{}
	start(t, dir, r)
	if err := os.Remove(filepath.Join(dir, "001-base.scad")); err != nil {
		t.Fatal(err)
	}
	got := r.waitCalls(t, 1)
	if len(got[0]) != 0 {
		t.Fatalf("un borrado no trae archivos nuevos: %v", got[0])
	}
}

func TestCloseIsIdempotentAndStops(t *testing.T) {
	dir := t.TempDir()
	r := &recorder{}
	w := start(t, dir, r)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	write(t, dir, "001-a.scad")
	time.Sleep(3 * Debounce)
	if c := r.snapshot(); len(c) != 0 {
		t.Fatalf("avisó después de Close: %v", c)
	}
}

func TestNewest(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"002-b.scad", "010-z.scad", "009-y.scad"}, "010-z.scad"},
		{[]string{"1000-big.scad", "999-small.scad"}, "1000-big.scad"},
		{[]string{"sketch.scad", "001-a.scad"}, "001-a.scad"},
		{[]string{"a.scad", "b.scad"}, "b.scad"},
	}
	for _, c := range cases {
		if got := Newest(c.in); got != c.want {
			t.Errorf("Newest(%v) = %q, se esperaba %q", c.in, got, c.want)
		}
	}
}
