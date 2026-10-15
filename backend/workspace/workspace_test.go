package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Caja de Electrónica":    "caja-de-electronica",
		"  Soporte / Pared: v2 ": "soporte-pared-v2",
		"CON":                    "con",
		"???":                    "",
	}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestScanAndNext(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("001-base-box.scad", "// @checkpoint parent= desc=\"Caja base\"\ncube(10);\n")
	write("002-add-lid.scad", "\uFEFF// @checkpoint parent=001-base-box.scad desc=\"Tapa \\\"deslizante\\\"\"\n")
	write("notes.txt", "x")
	write("003-path.scad", "// @checkpoint parent=002-add-lid.scad desc=\"ruta C:\\\\tmp y \\\"cita\\\"\"\n")

	files, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("esperaba 3 checkpoints, hay %d", len(files))
	}
	if got := files[2].Description; got != `ruta C:\tmp y "cita"` {
		t.Errorf("desc con barra invertida: %q", got)
	}
	if files[1].ParentFile != "001-base-box.scad" || files[1].Description != `Tapa "deslizante"` {
		t.Errorf("encabezado mal leído: %+v", files[1])
	}
	if files[0].ParentFile != "" {
		t.Errorf("el primero no tiene parent: %+v", files[0])
	}
	next, err := NextFileName(dir, "Agregar bisagra")
	if err != nil || next != "004-agregar-bisagra.scad" {
		t.Errorf("NextFileName = %q, %v", next, err)
	}
}
