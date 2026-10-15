package scad

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func realOpenSCAD(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("openscad")
	if err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	return bin
}

// assertNoTemps: el temporal del lint se borra siempre.
func assertNoTemps(t *testing.T, ws string) {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(ws, ".scadlab", "lint"))
	if err != nil {
		t.Fatalf("no se creó .scadlab/lint: %v", err)
	}
	if len(ents) != 0 {
		t.Errorf("quedaron temporales: %v", ents)
	}
}

func TestLintSyntaxWithOpenSCAD(t *testing.T) {
	bin := realOpenSCAD(t)
	ws := t.TempDir()
	src := okHeader + "color(\"red\")\n    cube(10;\n"
	ds := Lint(context.Background(), src, LintOptions{WorkspaceDir: ws, FileName: "001-a.scad", OpenSCADPath: bin})
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

// Los include se resuelven contra el workspace y lib/ aunque el temporal
// viva en .scadlab/lint/ (OPENSCADPATH); uno que falta es un solo SL004.
func TestLintIncludesWithOpenSCAD(t *testing.T) {
	bin := realOpenSCAD(t)
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
	ds := Lint(context.Background(), src, LintOptions{WorkspaceDir: ws, FileName: "001-a.scad", OpenSCADPath: bin})
	if len(ds) != 1 {
		t.Fatalf("quiero un solo diagnóstico (el include que falta): %+v", ds)
	}
	if d := ds[0]; d.Code != CodeIncludeMissing || d.Line != 4 || d.Severity != SeverityError || d.Col != 9 {
		t.Errorf("SL004: %+v", d)
	}
	assertNoTemps(t, ws)
}

// La reasignación la avisan OpenSCAD y SL008: queda una sola. Un módulo
// desconocido es un warning de OpenSCAD en su línea.
func TestLintMergesOpenSCADWarnings(t *testing.T) {
	bin := realOpenSCAD(t)
	ws := t.TempDir()
	src := okHeader + "x = 1;\nx = 2;\ncolor(\"red\") cube(x);\ncolor(\"red\") no_existe();\n"
	ds := Lint(context.Background(), src, LintOptions{WorkspaceDir: ws, FileName: "001-a.scad", OpenSCADPath: bin})
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

// Un OpenSCAD colgado no cuelga el editor: vence el plazo, se devuelven las
// reglas propias y el temporal se borra igual.
func TestLintTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("openscad falso con sh")
	}
	ws := t.TempDir()
	fake := filepath.Join(t.TempDir(), "openscad")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prev := lintTimeout
	lintTimeout = 300 * time.Millisecond
	defer func() { lintTimeout = prev }()
	start := time.Now()
	ds := Lint(context.Background(), "cube(1);", LintOptions{WorkspaceDir: ws, OpenSCADPath: fake})
	if time.Since(start) > 5*time.Second {
		t.Errorf("tardó %v", time.Since(start))
	}
	if len(ds) != 1 || ds[0].Code != CodeNoColor {
		t.Errorf("quiero solo las reglas propias: %+v", ds)
	}
	assertNoTemps(t, ws)
}

// El OpenSCAD falso recibe OPENSCADPATH con el workspace y lib/, y la salida
// sobre el temporal se mapea al archivo editado.
func TestLintFakeOpenSCADEnvAndMapping(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("openscad falso con sh")
	}
	ws := t.TempDir()
	envOut := filepath.Join(t.TempDir(), "env.txt")
	fake := filepath.Join(t.TempDir(), "openscad")
	script := "#!/bin/sh\necho \"$OPENSCADPATH\" > " + envOut + "\n" +
		"echo \"ERROR: Parser error: syntax error in file $3, line 2\" >&2\nexit 1\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	ds := Lint(context.Background(), "color(\"r\") cube(1);\nx = ;\n", LintOptions{WorkspaceDir: ws, OpenSCADPath: fake, FileName: ""})
	raw, err := os.ReadFile(envOut)
	if err != nil {
		t.Fatal(err)
	}
	want := ws + string(os.PathListSeparator) + filepath.Join(ws, "lib")
	if !strings.HasPrefix(strings.TrimSpace(string(raw)), want) {
		t.Errorf("OPENSCADPATH = %q, quiero que empiece con %q", raw, want)
	}
	if len(ds) != 1 || ds[0].Line != 2 || ds[0].Source != SourceOpenSCAD {
		t.Errorf("diagnósticos: %+v", ds)
	}
	assertNoTemps(t, ws)
}
