package scadwasm_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scadlab/backend/scadwasm"
)

// shared es el Runner que usan los tests: compilar el módulo cuesta segundos la
// primera vez (con la caché en disco de un directorio temporal del paquete,
// los Runner que se creen después la reutilizan).
var shared *scadwasm.Runner

// cacheDir es la caché de compilación del paquete de test.
var cacheDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "scadwasm-cache-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cacheDir = dir
	shared = scadwasm.New(dir)
	code := m.Run()
	_ = shared.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// scene es un workspace de prueba: un directorio con .scad, una carpeta de
// salida y los montajes habituales.
type scene struct {
	ws, out string
}

func newScene(t *testing.T, files map[string]string) *scene {
	t.Helper()
	s := &scene{ws: t.TempDir(), out: t.TempDir()}
	for name, body := range files {
		p := filepath.Join(s.ws, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func (s *scene) spec(args ...string) scadwasm.RunSpec {
	return scadwasm.RunSpec{
		Args: args,
		Cwd:  "/work",
		Mounts: []scadwasm.Mount{
			{Host: s.ws, Virtual: "/work", ReadOnly: true},
			{Host: s.out, Virtual: "/out"},
		},
	}
}

// run corre OpenSCAD con un plazo generoso.
func (s *scene) run(t *testing.T, args ...string) (string, int, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return shared.Run(ctx, s.spec(args...))
}

func (s *scene) outFile(name string) string { return filepath.Join(s.out, name) }
