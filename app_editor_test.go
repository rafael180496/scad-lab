package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/checkpoint"
	"scadlab/backend/scad"
	"scadlab/backend/workspace"
)

func editorWorkspace(t *testing.T) (*App, int64, string) {
	t.Helper()
	a := newTestApp(t)
	w, err := a.CreateWorkspace("Editor", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(w.Path, "001-base.scad"), []byte("// @checkpoint parent= desc=\"base\"\ncolor(\"red\") cube(10);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return a, w.ID, w.Path
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// Guardar crea un checkpoint hijo nuevo con el encabezado correcto, que
// workspace.Scan lee de vuelta.
func TestSaveAsCheckpointCreatesChild(t *testing.T) {
	a, id, dir := editorWorkspace(t)
	src := "// @checkpoint parent=viejo.scad desc=\"lo que traía\"\ncolor(\"red\") cube(12);\n"
	name, err := a.SaveAsCheckpoint(id, "001-base.scad", "Más grande", `Cubo de 12 "mm"`, src)
	if err != nil {
		t.Fatal(err)
	}
	if name != "002-mas-grande.scad" {
		t.Fatalf("nombre: %s", name)
	}
	want := "// @checkpoint parent=001-base.scad desc=\"Cubo de 12 \\\"mm\\\"\"\ncolor(\"red\") cube(12);\n"
	if got := readFile(t, filepath.Join(dir, name)); got != want {
		t.Fatalf("contenido:\n%q\nquiero\n%q", got, want)
	}
	files, err := workspace.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.FileName == name && (f.ParentFile != "001-base.scad" || f.Description != `Cubo de 12 "mm"`) {
			t.Errorf("Scan: %+v", f)
		}
	}
	// Sin encabezado, se antepone; sin descripción, va el rótulo.
	name, err = a.SaveAsCheckpoint(id, "002-mas-grande.scad", "Esfera", "", "sphere(5);")
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, name)); got != "// @checkpoint parent=002-mas-grande.scad desc=\"Esfera\"\nsphere(5);" {
		t.Errorf("antepuesto: %q", got)
	}
}

func TestSaveAsCheckpointNeverOverwrites(t *testing.T) {
	a, id, dir := editorWorkspace(t)
	if err := os.WriteFile(filepath.Join(dir, "002-otro.scad"), []byte("// del agente\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	name, err := a.SaveAsCheckpoint(id, "001-base.scad", "otro", "x", "cube(1);")
	if err != nil {
		t.Fatal(err)
	}
	if name != "003-otro.scad" {
		t.Errorf("nombre: %s", name)
	}
	if got := readFile(t, filepath.Join(dir, "002-otro.scad")); got != "// del agente\n" {
		t.Errorf("pisó un checkpoint: %q", got)
	}
}

// Carrera con el agente: el nombre calculado aparece justo antes de crearlo.
func TestSaveAsCheckpointRace(t *testing.T) {
	a, id, dir := editorWorkspace(t)
	calls := 0
	checkpoint.BeforeCreate = func(path string) {
		calls++
		if calls == 1 {
			if err := os.WriteFile(path, []byte("// del agente\n"), 0o644); err != nil {
				t.Error(err)
			}
		}
	}
	defer func() { checkpoint.BeforeCreate = nil }()
	name, err := a.SaveAsCheckpoint(id, "001-base.scad", "cambio", "x", "cube(1);")
	if err != nil {
		t.Fatal(err)
	}
	if name != "003-cambio.scad" || calls != 2 {
		t.Errorf("nombre %s tras %d intentos", name, calls)
	}
	if got := readFile(t, filepath.Join(dir, "002-cambio.scad")); got != "// del agente\n" {
		t.Errorf("pisó el del agente: %q", got)
	}
}

func TestSaveAsCheckpointValidatesParent(t *testing.T) {
	a, id, _ := editorWorkspace(t)
	for _, parent := range []string{"009-no-existe.scad", "../001-base.scad", "", "001-base.txt"} {
		if _, err := a.SaveAsCheckpoint(id, parent, "x", "x", "cube(1);"); err == nil {
			t.Errorf("parent %q: quiero error", parent)
		}
	}
}

func TestCheckpointHeaderEscapes(t *testing.T) {
	got := checkpoint.WithHeader("// @checkpoint vieja\r\ncube(1);\r\n", "001-a.scad", "a\\b \"c\"\nd")
	want := "// @checkpoint parent=001-a.scad desc=\"a\\\\b \\\"c\\\" d\"\r\ncube(1);\r\n"
	if got != want {
		t.Errorf("%q\nquiero\n%q", got, want)
	}
	if got := checkpoint.WithHeader("// @checkpoint sola", "001-a.scad", "x"); got != "// @checkpoint parent=001-a.scad desc=\"x\"\n" {
		t.Errorf("solo encabezado: %q", got)
	}
}

func TestEditorLintFormatCatalog(t *testing.T) {
	a, id, _ := editorWorkspace(t)
	ds, err := a.LintCheckpointSource(id, "002-x.scad", "// @checkpoint parent=001-base.scad desc=\"x\"\ncube(1);\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 || ds[0].Code != scad.CodeNoColor || ds[0].Line != 2 {
		t.Errorf("lint: %+v", ds)
	}
	ds, err = a.LintCheckpointSource(id, "002-x.scad", "// @checkpoint parent=007-nada.scad desc=\"x\"\ncolor(\"red\") cube(1);\n")
	if err != nil || len(ds) != 1 || ds[0].Code != scad.CodeParentMissing {
		t.Errorf("SL002 con los archivos del workspace: %+v %v", ds, err)
	}
	if _, err := a.LintCheckpointSource(id, "../x.scad", "cube(1);"); err == nil {
		t.Error("nombre inválido: quiero error")
	}
	out, err := a.FormatScad("module m(){\ncube(1);\n}")
	if err != nil || out != "module m() {\n    cube(1);\n}\n" {
		t.Errorf("format (indentación 4 por defecto): %q %v", out, err)
	}
	if _, err := a.FormatScad("s = \"sin cerrar"); err == nil {
		t.Error("format de un string sin cerrar: quiero error")
	}
	c, err := a.GetScadCatalog()
	if err != nil || len(c.Entries) == 0 || !strings.HasPrefix(c.Version, "20") {
		t.Errorf("catálogo: %d entradas, %v", len(c.Entries), err)
	}
}
