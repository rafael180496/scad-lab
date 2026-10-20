package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/printers"
)

// pr devuelve una impresora del catálogo (la de reglas por defecto de las pruebas).
func pr(id string) printers.Printer {
	p, _ := printers.ByID(id)
	return p
}

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
	if err := WriteRules(dir, "Caja", pr("bambu-a1-mini")); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(read(), rulesBegin) || !strings.Contains(read(), rulesEnd) {
		t.Fatalf("sin marcas:\n%s", read())
	}
	// 2. El usuario agrega notas fuera del bloque; cambiar de impresora
	// actualiza el bloque y conserva las notas.
	os.WriteFile(path, []byte(read()+"\n## Mis notas\nusar PETG\n"), 0o644)
	if err := WriteRules(dir, "Caja", pr("bambu-a1")); err != nil {
		t.Fatal(err)
	}
	got := read()
	if !strings.Contains(got, "usar PETG") || !strings.Contains(got, "256") || strings.Count(got, rulesBegin) != 1 {
		t.Errorf("notas perdidas o bloque duplicado:\n%s", got)
	}
	// 3. Idempotente: sin cambios no reescribe.
	st1, _ := os.Stat(path)
	if err := WriteRules(dir, "Caja", pr("bambu-a1")); err != nil {
		t.Fatal(err)
	}
	if st2, _ := os.Stat(path); !st2.ModTime().Equal(st1.ModTime()) {
		t.Error("reescribió un archivo que no cambió")
	}

	// 4. Archivo de una versión anterior (sin marcas, con nuestro título):
	// se respalda y se reemplaza.
	legacy := "# Caja — workspace de ScadLab\n\nreglas viejas editadas por el usuario\n"
	os.WriteFile(path, []byte(legacy), 0o644)
	if err := WriteRules(dir, "Caja", pr("bambu-a1")); err != nil {
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
	if err := WriteRules(dir, "Caja", pr("bambu-a1")); err != nil {
		t.Fatal(err)
	}
	got = read()
	if !strings.HasPrefix(got, rulesBegin) || !strings.HasSuffix(got, own) {
		t.Errorf("contenido propio no conservado:\n%s", got)
	}
}

func TestTargetDir(t *testing.T) {
	root := t.TempDir()
	cases := []struct{ parent, name, want string }{
		{root, "Caja de Electrónica", filepath.Join(root, "caja-de-electronica")},
		{root + "/a/../b", "  Soporte / Pared: v2 ", filepath.Join(root, "b", "soporte-pared-v2")},
		{root, "???", ""},           // sin slug
		{"relativa", "Caja", ""},    // padre no absoluto
		{"", "Caja", ""},            // sin padre
		{"~/Documents", "Caja", ""}, // «~» no se expande
	}
	for _, c := range cases {
		if got := TargetDir(c.parent, c.name); got != c.want {
			t.Errorf("TargetDir(%q, %q) = %q, esperaba %q", c.parent, c.name, got, c.want)
		}
	}
}

func TestCreateValidatesLocation(t *testing.T) {
	root := t.TempDir()
	if _, err := Create("relativa", "Caja", pr("bambu-a1")); !errors.Is(err, ErrParentNotAbs) {
		t.Errorf("padre relativo: %v", err)
	}
	// Padre nuevo, varios niveles: se crea entero.
	parent := filepath.Join(root, "a", "b", "c")
	dir, err := Create(parent, "Ñu Único", pr("bambu-a1"))
	if err != nil || dir != filepath.Join(parent, "nu-unico") {
		t.Fatalf("padre nuevo: %q, %v", dir, err)
	}
	// El mismo destino, ya con contenido: el error nombra la carpeta.
	if _, err := Create(parent, "Ñu Único", pr("bambu-a1")); err == nil || !strings.Contains(err.Error(), dir) {
		t.Errorf("destino no vacío: %v", err)
	}
	// Destino que es un archivo.
	if err := os.WriteFile(filepath.Join(parent, "archivo"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(parent, "archivo", pr("bambu-a1")); err == nil || !strings.Contains(err.Error(), "archivo") {
		t.Errorf("destino que es un archivo: %v", err)
	}
	// Padre que es un archivo.
	if _, err := Create(filepath.Join(parent, "archivo"), "x", pr("bambu-a1")); err == nil {
		t.Error("padre que es un archivo")
	}
}

// S12.c: las reglas para todos los CLIs traen el flujo obligatorio con el MCP.
func TestRulesMandateCheckpointFlow(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRules(dir, "Caja", pr("bambu-a1-mini")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{
		"Flujo obligatorio", "`create_checkpoint`", "`render`", "`preview`", "`measure`",
		"sin preguntar", "Nunca termines solo con código en el texto", "No leas ni escribas `.scad`", "Cuando el usuario nombra un archivo existente",
		"`update_checkpoint`", "Ante la duda, edita el que nombró", "No hacer commits",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("falta %q en las reglas", want)
		}
	}
	if i, j := strings.Index(got, "`create_checkpoint`"), strings.Index(got, "`openscad -o"); i < 0 || j < 0 || i > j {
		t.Error("el MCP debe preferirse antes que la CLI de OpenSCAD")
	}
}

// printer.json trae los campos de S14 con la impresora EFECTIVA: los colores son
// los del workspace y max_colors el tope de la máquina.
func TestWriteRulesPrinterJSONCarriesTheEffectivePrinter(t *testing.T) {
	dir := t.TempDir()
	p := pr("bambu-h2d")
	p.MulticolorSlots = 6 // los colores que eligió el workspace
	if err := WriteRules(dir, "Caja", p); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "printer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var pj map[string]any
	if err := json.Unmarshal(raw, &pj); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"printer": "bambu-h2d", "brand": "Bambu Lab", "model": "H2D", "colors": 6.0, "multicolor_slots": 6.0,
		"max_colors": 25.0, "filament_diameter_mm": 1.75, "slicer": "Bambu Studio", "nozzle_mm": 0.4,
		"multicolor_system": "AMS 2 Pro / AMS HT (hasta 4 AMS 2 Pro + 8 AMS HT; 25 colores con una bobina externa)",
	}
	for k, v := range want {
		if pj[k] != v {
			t.Errorf("printer.json[%s] = %v, esperaba %v", k, pj[k], v)
		}
	}
	// El volumen y el nombre siguen donde estaban.
	if bv, _ := pj["build_volume"].(map[string]any); bv["x"] != 325.0 {
		t.Errorf("build_volume: %v", pj["build_volume"])
	}
}

// Las reglas leen la cantidad de colores del workspace, no el máximo de la máquina.
func TestRulesUseTheWorkspaceColors(t *testing.T) {
	d := rulesData("Caja", func() printers.Printer { p := pr("bambu-p1s"); p.MulticolorSlots = 2; return p }())
	if d["ColorSlots"] != 2 || d["MaxColors"] != 16 {
		t.Errorf("colores: %v / %v", d["ColorSlots"], d["MaxColors"])
	}
	// Las variables que la plantilla puede pedir existen y vienen resueltas.
	for k, want := range map[string]any{
		"Brand": "Bambu Lab", "PrinterModel": "P1S", "BrandSkill": "bambu-lab-printers",
		"Slicer": "Bambu Studio", "NozzleMM": 0.4, "FilamentDiameter": 1.75,
		"MulticolorSystem": "AMS (hasta 4 unidades de 4 = 16)", "PrinterName": "Bambu Lab P1S",
	} {
		if d[k] != want {
			t.Errorf("%s = %v, esperaba %v", k, d[k], want)
		}
	}
	// Sin datos (impresora vacía) nada queda en cero donde la IA lo leería.
	z := rulesData("X", printers.Printer{})
	if z["ColorSlots"] != 1 || z["MaxColors"] != 1 || z["FilamentDiameter"] != 1.75 || z["Slicer"] != "" {
		t.Errorf("impresora vacía: %v", z)
	}
}

// Una plantilla sin las variables nuevas rinde igual que antes (el cambio es
// compatible hacia atrás): las reglas siguen nombrando la impresora y su volumen.
func TestRulesStillNameThePrinter(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRules(dir, "Caja", pr("bambu-a1")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
	if !strings.Contains(string(b), "Bambu Lab A1") || !strings.Contains(string(b), "256") {
		t.Errorf("las reglas no nombran la impresora:\n%s", b)
	}
}

// S14: las reglas de un workspace Prusa MK4S nombran la marca, el modelo, el
// volumen, el sistema multicolor, el slicer y la skill de la marca, y no hablan
// del AMS ni de Bambu Studio como si fueran universales.
func TestRulesNameThePrusaPrinter(t *testing.T) {
	dir := t.TempDir()
	mk4s := pr("prusa-mk4s")
	mk4s.MulticolorSlots = mk4s.Colors(0) // la impresora efectiva: sin MMU3 elegido, un color
	if err := WriteRules(dir, "Soporte", mk4s); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	head := got[:strings.Index(got, "## Flujo obligatorio")]
	for _, want := range []string{
		"Original Prusa MK4S", "250 × 210 × 220 mm", "Prusa Research MK4S", "Prusa MMU3 (opcional)", "**1 color(es)**",
		"hasta 5", "0.4 mm", "1.75 mm", "**Slicer recomendado**: PrusaSlicer", "`prusa-printers`",
	} {
		if !strings.Contains(head, want) {
			t.Errorf("el encabezado de las reglas no dice %q:\n%s", want, head)
		}
	}
	for _, bad := range []string{"AMS", "Bambu"} {
		if strings.Contains(head, bad) {
			t.Errorf("el encabezado de las reglas de una Prusa dice %q:\n%s", bad, head)
		}
	}
	if strings.Contains(got, "slots del AMS") || strings.Contains(got, "Abrir el archivo en\n   Bambu Studio") {
		t.Error("las reglas siguen hablando del AMS o de Bambu Studio como algo universal")
	}
	for _, want := range []string{
		"máximo\n   1 color(es) en este workspace; sistema: Prusa MMU3 (opcional)", "`list_printers`", "`set_workspace_printer`",
		"`3mf-prusa` (PrusaSlicer)", "Abrir el archivo en el slicer (PrusaSlicer)", "la de tu impresora (`prusa-printers`)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("las reglas no dicen %q", want)
		}
	}
	// Con una impresora de otra marca cambia todo lo anterior.
	if err := WriteRules(dir, "Soporte", pr("ultimaker-s5")); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	for _, want := range []string{"UltiMaker S5", "330 × 240 × 300 mm", "**Slicer recomendado**: UltiMaker Cura", "2.85 mm", "`ultimaker-printers`"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("reglas de la UltiMaker S5 sin %q", want)
		}
	}
}

// Las reglas se renderizan para toda impresora del catálogo, con todas las
// variables resueltas (ninguna plantilla imprime «<no value>» ni un formato roto).
func TestRulesRenderForEveryPrinter(t *testing.T) {
	all, err := printers.All()
	if err != nil || len(all) == 0 {
		t.Fatalf("catálogo: %v", err)
	}
	for _, p := range all {
		p.MulticolorSlots = p.Colors(0)
		dir := t.TempDir()
		if err := WriteRules(dir, "Caja", p); err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}
		raw, _ := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
		got := string(raw)
		if strings.Contains(got, "<no value>") || strings.Contains(got, "%!") || strings.Contains(got, "{{") {
			t.Errorf("%s: plantilla sin resolver", p.ID)
		}
		for _, want := range []string{p.Name, p.MulticolorSys, "`" + p.Skill + "`"} {
			if !strings.Contains(got, want) {
				t.Errorf("%s: las reglas no nombran %q", p.ID, want)
			}
		}
	}
}
