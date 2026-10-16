package scad

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"scadlab/backend/scadwasm"
)

// El linter con el motor integrado (OpenSCAD en WebAssembly) da los mismos
// diagnósticos que con el nativo: lo que prueban estos tests son los mismos
// casos de lint_test.go, sin OpenSCAD instalado.

var (
	wasmShared   *scadwasm.Runner
	wasmCacheDir string
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "scad-wasm-cache-")
	if err != nil {
		panic(err)
	}
	wasmCacheDir = dir
	wasmShared = scadwasm.New(dir)
	// Se compila antes de los tests: con -race la compilación pasa de los 5 s
	// que el linter le da a OpenSCAD, y el primer test quedaría sin chequeo.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	if _, _, err := wasmShared.Run(ctx, scadwasm.RunSpec{Args: []string{"--version"}}); err != nil {
		panic(err)
	}
	cancel()
	code := m.Run()
	_ = wasmShared.Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func TestLintSyntaxWithWASM(t *testing.T) {
	ws := t.TempDir()
	src := okHeader + "color(\"red\")\n    cube(10;\n"
	ds := Lint(context.Background(), src, LintOptions{WorkspaceDir: ws, FileName: "001-a.scad", WASM: wasmShared})
	if len(ds) != 1 {
		t.Fatalf("diagnósticos: %+v", ds)
	}
	d := ds[0]
	if d.Code != CodeSyntax || d.Source != SourceOpenSCAD || d.Severity != SeverityError || d.Line != 3 || d.Col != 1 || d.EndLine != 0 {
		t.Errorf("error de sintaxis: %+v", d)
	}
	if strings.Contains(d.Message, "lint-") || strings.Contains(d.Message, "line 3") {
		t.Errorf("el mensaje nombra el temporal o repite la ubicación: %q", d.Message)
	}
	assertNoTemps(t, ws)
}

func TestLintIncludesWithWASM(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "parts.scad"), []byte("module part() cube(1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "lib", "util.scad"), []byte("function twice(x) = 2 * x;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := okHeader + "use <parts.scad>\ninclude <util.scad>\ninclude <falta.scad>\ncolor(\"red\") part();\ncolor(\"blue\") cube(twice(2));\n"
	ds := Lint(context.Background(), src, LintOptions{WorkspaceDir: ws, FileName: "001-a.scad", WASM: wasmShared})
	if len(ds) != 1 {
		t.Fatalf("quiero un solo diagnóstico (el include que falta): %+v", ds)
	}
	if d := ds[0]; d.Code != CodeIncludeMissing || d.Line != 4 || d.Severity != SeverityError || d.Col != 9 {
		t.Errorf("SL004: %+v", d)
	}
	assertNoTemps(t, ws)
}

func TestLintMergesWASMWarnings(t *testing.T) {
	ws := t.TempDir()
	src := okHeader + "x = 1;\nx = 2;\ncolor(\"red\") cube(x);\ncolor(\"red\") no_existe();\n"
	ds := Lint(context.Background(), src, LintOptions{WorkspaceDir: ws, FileName: "001-a.scad", WASM: wasmShared})
	var got []string
	for _, d := range ds {
		got = append(got, d.Code+"@"+strconv.Itoa(d.Line))
	}
	if strings.Join(got, ",") != "SL008@3,syntax@5" {
		t.Errorf("diagnósticos: %v (%+v)", got, ds)
	}
	if len(ds) == 2 && (ds[1].Severity != SeverityWarning || !strings.Contains(ds[1].Message, "no_existe")) {
		t.Errorf("warning de OpenSCAD: %+v", ds[1])
	}
}

// El nativo, si hay ruta, gana al integrado; y un integrado no disponible deja
// solo las reglas propias.
func TestLintWithoutAnyEngineKeepsOwnRules(t *testing.T) {
	ws := t.TempDir()
	ds := Lint(context.Background(), "cube(1);", LintOptions{WorkspaceDir: ws, WASM: (*scadwasm.Runner)(nil)})
	if len(ds) != 1 || ds[0].Code != CodeNoColor {
		t.Errorf("quiero solo las reglas propias: %+v", ds)
	}
}
