package checkpoint

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mkLib(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "lib"), name, body)
}

func TestValidLibName(t *testing.T) {
	for _, ok := range []string{"lib/x.scad", "lib/mi-modulo_2.scad", "lib/Ñandú.scad", "lib/x.SCAD"} {
		if err := ValidLibName(ok); err != nil {
			t.Errorf("%q debía ser válido: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"", "x.scad", "lib/", "lib/.scad", "lib/x", "lib/x.stl",
		"lib/BOSL2/std.scad", "lib/sub/x.scad", "lib//x.scad", "lib/../x.scad", "lib/..",
		"../lib/x.scad", "/lib/x.scad", "/etc/passwd", "lib/../../etc/x.scad",
		`lib\x.scad`, `lib/..\x.scad`, "C:/lib/x.scad", `C:\lib\x.scad`, "lib/C:/x.scad",
		"lib/x\x00.scad", "lib/x\n.scad", "Lib/x.scad", "lib2/x.scad", "./lib/x.scad",
	} {
		if err := ValidLibName(bad); err == nil {
			t.Errorf("%q debía rechazarse", bad)
		}
	}
	if !IsLibFile("lib/x.scad") || IsLibFile("001-a.scad") || IsLibFile("lib") {
		t.Error("IsLibFile")
	}
}

func TestListLibFilesOnlyDirectChildren(t *testing.T) {
	dir := t.TempDir()
	if got, err := ListLibFiles(dir); err != nil || got != nil {
		t.Fatalf("sin lib/: %v %v", got, err)
	}
	mkLib(t, dir, "b.scad", "x")
	mkLib(t, dir, "a.scad", "x")
	mkLib(t, dir, "notas.txt", "x")
	if err := os.MkdirAll(filepath.Join(dir, "lib", "BOSL2"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "lib", "BOSL2"), "std.scad", "x")
	got, err := ListLibFiles(dir)
	if err != nil || strings.Join(got, ",") != "lib/a.scad,lib/b.scad" {
		t.Errorf("lista: %v %v", got, err)
	}
}

// Un archivo de lib/ se reescribe con respaldo en .scadlab/history/lib/<nombre>/,
// sin cabecera, con la herramienta anotada.
func TestUpdateLibKeepsHistoryUnderLib(t *testing.T) {
	clock(t, t0, time.Second)
	dir := t.TempDir()
	mkLib(t, dir, "tapa.scad", "module tapa() { cube(1); }\n")

	res, err := UpdateWith(dir, "lib/tapa.scad", "module tapa() { cube(2); }\n", 0, UpdateOptions{Tool: "edit_checkpoint", Note: "más grande", HistoryNote: "ignorada"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Unchanged || res.Version.Tool != "edit_checkpoint" || res.Version.Reason != ReasonAI || res.Version.Note != "más grande" {
		t.Fatalf("resultado: %+v", res)
	}
	// El archivo tiene EXACTAMENTE lo pedido: ni cabecera ni línea extra, aunque haya nota.
	if got := read(t, filepath.Join(dir, "lib", "tapa.scad")); got != "module tapa() { cube(2); }\n" {
		t.Errorf("archivo: %q", got)
	}
	// El respaldo vive bajo history/lib/tapa.scad/.
	files := histFiles(t, dir, "lib/tapa.scad")
	if len(files) != 2 {
		t.Fatalf("respaldo en history/lib/tapa.scad/: %v", files)
	}
	if got, err := ReadVersion(dir, "lib/tapa.scad", res.Version.ID); err != nil || got != "module tapa() { cube(1); }\n" {
		t.Errorf("versión: %q %v", got, err)
	}
	if h, err := History(dir, "lib/tapa.scad"); err != nil || len(h) != 1 || h[0].Tool != "edit_checkpoint" {
		t.Errorf("historial: %+v %v", h, err)
	}
	if !EditedByAI(dir, "lib/tapa.scad") {
		t.Error("EditedByAI")
	}
	// Nada en el historial de la raíz.
	if _, err := os.Stat(filepath.Join(dir, ".scadlab", "history", "tapa.scad")); err == nil {
		t.Error("el historial no debe estar en la raíz del history")
	}

	// Idéntico: no respalda.
	if r2, err := UpdateWith(dir, "lib/tapa.scad", "module tapa() { cube(2); }\n", 0, UpdateOptions{}); err != nil || !r2.Unchanged {
		t.Errorf("idéntico: %+v %v", r2, err)
	}
	if h, _ := History(dir, "lib/tapa.scad"); len(h) != 1 {
		t.Errorf("un cambio nulo no respalda: %+v", h)
	}
	// Vacío: no se escribe.
	if _, err := UpdateWith(dir, "lib/tapa.scad", " \n", 0, UpdateOptions{}); err == nil {
		t.Error("un archivo vacío no debe escribirse")
	}

	// Restaurar vuelve a lo anterior y se puede deshacer.
	if _, err := RestoreVersion(dir, "lib/tapa.scad", res.Version.ID, 0); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "lib", "tapa.scad")); got != "module tapa() { cube(1); }\n" {
		t.Errorf("restaurado: %q", got)
	}
	if h, _ := History(dir, "lib/tapa.scad"); len(h) != 2 || h[0].Reason != ReasonRestore {
		t.Errorf("restaurar guarda lo que había: %+v", h)
	}
}

func TestUpdateLibTopeDelHistorial(t *testing.T) {
	clock(t, t0, time.Second)
	dir := t.TempDir()
	mkLib(t, dir, "m.scad", "a\n")
	for _, c := range []string{"b\n", "c\n", "d\n", "e\n"} {
		if _, err := UpdateWith(dir, "lib/m.scad", c, 2, UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	if h, _ := History(dir, "lib/m.scad"); len(h) != 2 {
		t.Errorf("con tope 2 quedaron %d", len(h))
	}
}

func TestUpdateLibRejectsHostileAndMissing(t *testing.T) {
	dir := t.TempDir()
	mkLib(t, dir, "ok.scad", "a\n")
	if err := os.MkdirAll(filepath.Join(dir, "lib", "BOSL2"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(dir, "lib", "BOSL2"), "std.scad", "libreria\n")
	write(t, dir, "001-a.scad", "// @checkpoint parent= desc=\"a\"\ncube(1);\n")
	for _, bad := range []string{"lib/BOSL2/std.scad", "lib/../001-a.scad", "../lib/ok.scad", "/etc/passwd", `lib\ok.scad`, "lib/ok.stl", "lib/"} {
		if _, err := UpdateWith(dir, bad, "x\n", 0, UpdateOptions{}); err == nil {
			t.Errorf("%q debía rechazarse", bad)
		}
	}
	if got := read(t, filepath.Join(dir, "lib", "BOSL2", "std.scad")); got != "libreria\n" {
		t.Errorf("una librería instalada se tocó: %q", got)
	}
	// Inexistente: el error dice qué hay en lib/.
	_, err := UpdateWith(dir, "lib/nada.scad", "x\n", 0, UpdateOptions{})
	if err == nil || !strings.Contains(err.Error(), "lib/ok.scad") {
		t.Errorf("error útil: %v", err)
	}
	if _, serr := os.Stat(filepath.Join(dir, "lib", "nada.scad")); serr == nil {
		t.Error("Update no crea archivos")
	}
	if h, _ := History(dir, "lib/ok.scad"); len(h) != 0 {
		t.Errorf("un rechazo no deja historial: %+v", h)
	}
}

func TestLibSymlinksAreRejected(t *testing.T) {
	skipNoSymlinks(t)
	dir := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "secreto.scad", "secreto\n")
	mkLib(t, dir, "ok.scad", "a\n")
	if err := os.Symlink(filepath.Join(outside, "secreto.scad"), filepath.Join(dir, "lib", "fuga.scad")); err != nil {
		t.Fatal(err)
	}
	// Se reescribe solo un archivo normal; ni se lee uno que sale del workspace.
	if _, err := UpdateWith(dir, "lib/fuga.scad", "x\n", 0, UpdateOptions{}); err == nil {
		t.Error("un enlace no se reescribe")
	}
	if _, err := LibPath(dir, "lib/fuga.scad"); err == nil {
		t.Error("un enlace que sale no se lee")
	}
	if got := read(t, filepath.Join(outside, "secreto.scad")); got != "secreto\n" {
		t.Errorf("el destino del enlace cambió: %q", got)
	}
	// Un enlace que se queda dentro se puede leer pero no reescribir.
	if err := os.Symlink(filepath.Join(dir, "lib", "ok.scad"), filepath.Join(dir, "lib", "alias.scad")); err != nil {
		t.Fatal(err)
	}
	if _, err := LibPath(dir, "lib/alias.scad"); err != nil {
		t.Errorf("enlace interno legible: %v", err)
	}
	if _, err := UpdateWith(dir, "lib/alias.scad", "x\n", 0, UpdateOptions{}); err == nil {
		t.Error("un enlace no se reescribe")
	}

	// lib/ misma como enlace hacia afuera.
	dir2 := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir2, "lib")); err != nil {
		t.Fatal(err)
	}
	if _, err := LibPath(dir2, "lib/secreto.scad"); err == nil {
		t.Error("lib/ que apunta afuera no se usa")
	}
	if _, err := UpdateWith(dir2, "lib/secreto.scad", "x\n", 0, UpdateOptions{}); err == nil {
		t.Error("lib/ que apunta afuera no se escribe")
	}
	if err := CreateLib(dir2, "lib/nuevo.scad", "x\n"); err == nil {
		t.Error("lib/ que apunta afuera no se crea")
	}
	if _, err := os.Stat(filepath.Join(outside, "nuevo.scad")); err == nil {
		t.Error("se escribió fuera del workspace")
	}
	// El historial de lib/ tampoco puede ser un enlace.
	dir3 := t.TempDir()
	mkLib(t, dir3, "m.scad", "a\n")
	if err := os.MkdirAll(filepath.Join(dir3, ".scadlab", "history"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir3, ".scadlab", "history", "lib")); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateWith(dir3, "lib/m.scad", "b\n", 0, UpdateOptions{}); err == nil {
		t.Error("un history/lib que es un enlace no se usa")
	}
	if got := read(t, filepath.Join(dir3, "lib", "m.scad")); got != "a\n" {
		t.Errorf("sin respaldo no se escribe: %q", got)
	}
}

func TestCreateLibNeverOverwritesAndUndoIsExact(t *testing.T) {
	dir := t.TempDir()
	if err := CreateLib(dir, "lib/nuevo.scad", "module m() {}\n"); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "lib", "nuevo.scad")); got != "module m() {}\n" {
		t.Errorf("contenido: %q", got)
	}
	// Existe: no pisa.
	err := CreateLib(dir, "lib/nuevo.scad", "otro\n")
	if !errors.Is(err, fs.ErrExist) {
		t.Errorf("debía fallar con ErrExist: %v", err)
	}
	if got := read(t, filepath.Join(dir, "lib", "nuevo.scad")); got != "module m() {}\n" {
		t.Errorf("se pisó: %q", got)
	}
	// Sin temporales sueltos.
	ents, _ := os.ReadDir(filepath.Join(dir, "lib"))
	if len(ents) != 1 {
		t.Errorf("temporales en lib/: %v", ents)
	}
	// Nombres inválidos.
	for _, bad := range []string{"lib/BOSL2/x.scad", "x.scad", "lib/../x.scad"} {
		if err := CreateLib(dir, bad, "x"); err == nil {
			t.Errorf("%q debía rechazarse", bad)
		}
	}
	// El deshacer borra solo lo que escribimos.
	if err := RemoveCreatedLib(dir, "lib/nuevo.scad", "contenido distinto\n"); err == nil {
		t.Error("no debe borrar un archivo cuyo contenido cambió")
	}
	if !fileExists(filepath.Join(dir, "lib", "nuevo.scad")) {
		t.Fatal("se borró un archivo que no era el creado")
	}
	if err := RemoveCreatedLib(dir, "lib/nuevo.scad", "module m() {}\n"); err != nil {
		t.Fatal(err)
	}
	if fileExists(filepath.Join(dir, "lib", "nuevo.scad")) {
		t.Error("RemoveCreatedLib no borró lo creado")
	}
}

// UpdateOptions también sirve para un checkpoint normal: la herramienta anotada y
// una nota solo de historial que no toca la descripción de la cabecera.
func TestUpdateWithToolAndHistoryNote(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	before := read(t, filepath.Join(dir, "003-b.scad"))
	res, err := UpdateWith(dir, "003-b.scad", before+"cube(9);\n", 0, UpdateOptions{Tool: "set_parameters", HistoryNote: "ancho 60 → 80"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Version.Tool != "set_parameters" || res.Version.Note != "ancho 60 → 80" || res.Description != "desc de 003-b.scad" {
		t.Fatalf("ficha: %+v", res)
	}
	if got := read(t, filepath.Join(dir, "003-b.scad")); !strings.HasPrefix(got, Header("002-a.scad", "desc de 003-b.scad")+"\n") {
		t.Errorf("la cabecera cambió: %q", got)
	}
	// Con Note sí cambia la descripción y esa es la nota del historial.
	res, err = UpdateWith(dir, "003-b.scad", before+"cube(10);\n", 0, UpdateOptions{Tool: "edit_checkpoint", Note: "nueva desc", HistoryNote: "ignorada"})
	if err != nil || res.Description != "nueva desc" || res.Version.Note != "nueva desc" || res.Version.Tool != "edit_checkpoint" {
		t.Fatalf("con nota: %+v %v", res, err)
	}
	// Update de siempre sigue anotando update_checkpoint.
	r3 := mustUpdate(t, dir, "003-b.scad", before+"cube(11);\n", "", 0)
	if r3.Version.Tool != "update_checkpoint" {
		t.Errorf("tool por defecto: %q", r3.Version.Tool)
	}
}

// Base: la escritura solo ocurre si el archivo sigue siendo el que se leyó. Un
// cambio entre la lectura y la escritura no se pisa.
func TestUpdateWithBaseRejectsAChangedFile(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	before := read(t, filepath.Join(dir, "003-b.scad"))
	stale := before
	// Otro proceso cambia el archivo después de que se leyó `stale`.
	mustUpdate(t, dir, "003-b.scad", "color(\"red\") cube(77);\n", "", 0)
	_, err := UpdateWith(dir, "003-b.scad", before+"cube(1);\n", 0, UpdateOptions{Verbatim: true, Base: &stale})
	if err == nil || !strings.Contains(err.Error(), "changed while") && !strings.Contains(err.Error(), "cambió mientras") {
		t.Fatalf("debía rechazarse: %v", err)
	}
	if got := read(t, filepath.Join(dir, "003-b.scad")); !strings.Contains(got, "cube(77)") || strings.Contains(got, "cube(1)") {
		t.Errorf("se pisó el cambio ajeno: %q", got)
	}
	if h, _ := History(dir, "003-b.scad"); len(h) != 1 {
		t.Errorf("el rechazo no deja respaldo: %+v", h)
	}
	// Con la base vigente sí escribe; y en lib/ igual.
	cur := read(t, filepath.Join(dir, "003-b.scad"))
	if _, err := UpdateWith(dir, "003-b.scad", cur+"cube(2);\n", 0, UpdateOptions{Verbatim: true, Base: &cur}); err != nil {
		t.Fatal(err)
	}
	mkLib(t, dir, "m.scad", "a\n")
	old := "otra cosa\n"
	if _, err := UpdateWith(dir, "lib/m.scad", "b\n", 0, UpdateOptions{Base: &old}); err == nil {
		t.Error("lib/ con una base vieja debía rechazarse")
	}
	now := "a\n"
	if _, err := UpdateWith(dir, "lib/m.scad", "b\n", 0, UpdateOptions{Base: &now}); err != nil {
		t.Errorf("lib/ con la base vigente: %v", err)
	}
}

// Verbatim: la cabecera no se puede cambiar por esta vía; un archivo con fines de
// línea mezclados queda intacto fuera de lo editado.
func TestUpdateVerbatimKeepsTheHeaderAndMixedEndings(t *testing.T) {
	clock(t, t0, time.Second)
	dir := t.TempDir()
	raw := "\uFEFF// @checkpoint parent= desc=\"mezcla\"\nx = 1;\r\ny = 2;\nz = 3;\r\n"
	write(t, dir, "001-mezcla.scad", raw)
	if _, err := UpdateWith(dir, "001-mezcla.scad", strings.Replace(raw, "y = 2;", "y = 5;", 1), 0, UpdateOptions{Verbatim: true, Tool: "edit_checkpoint"}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "001-mezcla.scad")); got != strings.Replace(raw, "y = 2;", "y = 5;", 1) {
		t.Errorf("bytes: %q", got)
	}
	// Cambiar la cabecera por esta vía se rechaza.
	bad := strings.Replace(raw, `desc="mezcla"`, `desc="otra"`, 1)
	if _, err := UpdateWith(dir, "001-mezcla.scad", bad, 0, UpdateOptions{Verbatim: true}); err == nil {
		t.Error("la cabecera no se cambia con Verbatim")
	}
	if _, err := UpdateWith(dir, "001-mezcla.scad", "x = 1;\n", 0, UpdateOptions{Verbatim: true}); err == nil {
		t.Error("quitar la cabecera no se permite con Verbatim")
	}
	// Con Note se reescribe solo la descripción, con el mismo fin de línea y BOM.
	cur := read(t, filepath.Join(dir, "001-mezcla.scad"))
	res, err := UpdateWith(dir, "001-mezcla.scad", strings.Replace(cur, "z = 3;", "z = 9;", 1), 0, UpdateOptions{Verbatim: true, Note: "nueva"})
	if err != nil || res.Description != "nueva" {
		t.Fatalf("con nota: %+v %v", res, err)
	}
	want := "\uFEFF// @checkpoint parent= desc=\"nueva\"\nx = 1;\r\ny = 5;\nz = 9;\r\n"
	if got := read(t, filepath.Join(dir, "001-mezcla.scad")); got != want {
		t.Errorf("bytes con nota:\n%q\n%q", got, want)
	}
	// Un archivo vacío no se escribe.
	if _, err := UpdateWith(dir, "001-mezcla.scad", " ", 0, UpdateOptions{Verbatim: true}); err == nil {
		t.Error("vacío")
	}
}
