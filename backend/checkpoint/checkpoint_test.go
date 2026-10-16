package checkpoint

import (
	"os"
	"path/filepath"
	"testing"

	"scadlab/backend/workspace"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestCreateChild(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "001-base.scad", "// @checkpoint parent= desc=\"base\"\ncolor(\"red\") cube(10);\n")
	name, err := Create(dir, "001-base.scad", "Más grande", `Cubo de 12 "mm"`, "// @checkpoint parent=viejo.scad desc=\"x\"\ncube(12);\n")
	if err != nil {
		t.Fatal(err)
	}
	if name != "002-mas-grande.scad" {
		t.Fatalf("nombre: %s", name)
	}
	want := "// @checkpoint parent=001-base.scad desc=\"Cubo de 12 \\\"mm\\\"\"\ncube(12);\n"
	if got := read(t, filepath.Join(dir, name)); got != want {
		t.Fatalf("contenido:\n%q\nquiero\n%q", got, want)
	}
	files, _ := workspace.Scan(dir)
	for _, f := range files {
		if f.FileName == name && (f.ParentFile != "001-base.scad" || f.Description != `Cubo de 12 "mm"`) {
			t.Errorf("Scan: %+v", f)
		}
	}
}

func TestCreateValidatesParent(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "001-base.scad", "cube(1);")
	for _, parent := range []string{"009-no-existe.scad", "../001-base.scad", "", "001-base.txt"} {
		if _, err := Create(dir, parent, "x", "x", "cube(1);"); err == nil {
			t.Errorf("parent %q: quiero error", parent)
		}
	}
}

func TestCreateNeverOverwritesAndRaces(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "001-base.scad", "cube(1);")
	calls := 0
	BeforeCreate = func(path string) {
		calls++
		if calls == 1 {
			write(t, filepath.Dir(path), filepath.Base(path), "// del agente\n")
		}
	}
	defer func() { BeforeCreate = nil }()
	name, err := Create(dir, "001-base.scad", "cambio", "x", "cube(2);")
	if err != nil {
		t.Fatal(err)
	}
	if name != "003-cambio.scad" || calls != 2 {
		t.Errorf("nombre %s tras %d intentos", name, calls)
	}
	if got := read(t, filepath.Join(dir, "002-cambio.scad")); got != "// del agente\n" {
		t.Errorf("pisó el del agente: %q", got)
	}
}

func TestCreateRoot(t *testing.T) {
	dir := t.TempDir()
	name, err := CreateRoot(dir, "Caja", "", "cube(3);")
	if err != nil {
		t.Fatal(err)
	}
	if name != "001-caja.scad" {
		t.Fatalf("nombre: %s", name)
	}
	if got := read(t, filepath.Join(dir, name)); got != "// @checkpoint parent= desc=\"Caja\"\ncube(3);" {
		t.Errorf("contenido: %q", got)
	}
	files, _ := workspace.Scan(dir)
	if len(files) != 1 || files[0].ParentFile != "" || files[0].Description != "Caja" {
		t.Errorf("Scan: %+v", files)
	}
	if _, err := CreateRoot(dir, "Otra", "", "cube(1);"); err == nil {
		t.Error("una segunda raíz tiene que fallar")
	}
}

func TestHeaderEscapes(t *testing.T) {
	got := WithHeader("// @checkpoint vieja\r\ncube(1);\r\n", "001-a.scad", "a\\b \"c\"\nd")
	want := "// @checkpoint parent=001-a.scad desc=\"a\\\\b \\\"c\\\" d\"\r\ncube(1);\r\n"
	if got != want {
		t.Errorf("%q\nquiero\n%q", got, want)
	}
	if got := WithHeader("// @checkpoint sola", "001-a.scad", "x"); got != "// @checkpoint parent=001-a.scad desc=\"x\"\n" {
		t.Errorf("solo encabezado: %q", got)
	}
}

func TestValidNameAndLatest(t *testing.T) {
	for _, n := range []string{"", "a/b.scad", "../a.scad", "a.txt", `..\a.scad`, ".scad", "a\x00.scad", "a\n.scad"} {
		if filepath.Separator == '/' && n == `..\a.scad` {
			continue // en macOS/Linux la barra invertida es un carácter válido de nombre
		}
		if ValidName(n) == nil {
			t.Errorf("ValidName(%q) debería fallar", n)
		}
	}
	if ValidName("001-a.scad") != nil || ValidName("A.SCAD") != nil {
		t.Error("nombres válidos rechazados")
	}
	dir := t.TempDir()
	if Latest(dir) != "" {
		t.Error("sin checkpoints, Latest es vacío")
	}
	write(t, dir, "002-b.scad", "x")
	write(t, dir, "010-c.scad", "x")
	write(t, dir, "003-d.scad", "x")
	if got := Latest(dir); got != "010-c.scad" {
		t.Errorf("Latest = %q", got)
	}
}
