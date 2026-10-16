package workspace

import (
	"os"
	"path/filepath"
	"strings"
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

func TestRulesBlockPreservesUserContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "CLAUDE.md")
	read := func() string { b, _ := os.ReadFile(path); return string(b) }

	// 1. Archivo nuevo: solo el bloque.
	if err := WriteRules(dir, "Caja", "bambu-a1-mini"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read(), rulesBegin) || !strings.Contains(read(), rulesEnd) {
		t.Fatalf("sin marcas:\n%s", read())
	}
	// 2. El usuario agrega notas fuera del bloque; cambiar de impresora
	// actualiza el bloque y conserva las notas.
	os.WriteFile(path, []byte(read()+"\n## Mis notas\nusar PETG\n"), 0o644)
	if err := WriteRules(dir, "Caja", "bambu-a1"); err != nil {
		t.Fatal(err)
	}
	got := read()
	if !strings.Contains(got, "usar PETG") || !strings.Contains(got, "256") || strings.Count(got, rulesBegin) != 1 {
		t.Errorf("notas perdidas o bloque duplicado:\n%s", got)
	}
	// 3. Idempotente: sin cambios no reescribe.
	st1, _ := os.Stat(path)
	if err := WriteRules(dir, "Caja", "bambu-a1"); err != nil {
		t.Fatal(err)
	}
	if st2, _ := os.Stat(path); !st2.ModTime().Equal(st1.ModTime()) {
		t.Error("reescribió un archivo que no cambió")
	}

	// 4. Archivo de una versión anterior (sin marcas, con nuestro título):
	// se respalda y se reemplaza.
	legacy := "# Caja — workspace de ScadLab\n\nreglas viejas editadas por el usuario\n"
	os.WriteFile(path, []byte(legacy), 0o644)
	if err := WriteRules(dir, "Caja", "bambu-a1"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read(), "reglas viejas") {
		t.Error("el archivo viejo no se reemplazó")
	}
	backups, _ := filepath.Glob(filepath.Join(dir, ".scadlab", "backup", "CLAUDE.md.*"))
	if len(backups) != 1 {
		t.Fatalf("respaldo: %v", backups)
	}
	if b, _ := os.ReadFile(backups[0]); string(b) != legacy {
		t.Error("el respaldo no es el archivo original")
	}

	// 5. CLAUDE.md propio del usuario (carpeta importada): bloque arriba, lo
	// suyo debajo, intacto.
	own := "# Mi proyecto\n\nInstrucciones mías.\n"
	os.WriteFile(path, []byte(own), 0o644)
	if err := WriteRules(dir, "Caja", "bambu-a1"); err != nil {
		t.Fatal(err)
	}
	got = read()
	if !strings.HasPrefix(got, rulesBegin) || !strings.HasSuffix(got, own) {
		t.Errorf("contenido propio no conservado:\n%s", got)
	}
}
