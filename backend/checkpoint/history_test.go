package checkpoint

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"scadlab/backend/workspace"
)

// clock fija now y lo hace avanzar step en cada lectura (un respaldo por
// segundo, como pasa en el uso real).
func clock(t *testing.T, start time.Time, step time.Duration) {
	t.Helper()
	cur := start
	now = func() time.Time {
		v := cur
		cur = cur.Add(step)
		return v
	}
	t.Cleanup(func() { now = time.Now })
}

var t0 = time.Date(2026, 10, 19, 22, 15, 0, 0, time.Local)

func histDir(dir, file string) string {
	return filepath.Join(dir, ".scadlab", "history", file)
}

func histFiles(t *testing.T, dir, file string) []string {
	t.Helper()
	ents, err := os.ReadDir(histDir(dir, file))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func mustUpdate(t *testing.T, dir, file, src, note string, max int) UpdateResult {
	t.Helper()
	res, err := Update(dir, file, src, note, max)
	if err != nil {
		t.Fatalf("Update(%s): %v", file, err)
	}
	return res
}

func TestUpdateKeepsHeaderAndParent(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	old := read(t, filepath.Join(dir, "003-b.scad"))
	// El código nuevo trae su propia cabecera (con otro padre): se descarta.
	res := mustUpdate(t, dir, "003-b.scad", "// @checkpoint parent=001-base.scad desc=\"otra\"\ncolor(\"red\") cube(5);\n", "", 0)
	if res.Unchanged || res.Parent != "002-a.scad" || res.Description != "desc de 003-b.scad" {
		t.Fatalf("resultado: %+v", res)
	}
	want := Header("002-a.scad", "desc de 003-b.scad") + "\ncolor(\"red\") cube(5);\n"
	if got := read(t, filepath.Join(dir, "003-b.scad")); got != want {
		t.Errorf("archivo:\n%q\nquiero\n%q", got, want)
	}
	// El respaldo es la versión anterior, entera, y el historial la lista.
	if got, err := ReadVersion(dir, "003-b.scad", res.Version.ID); err != nil || got != old {
		t.Errorf("respaldo: %q %v", got, err)
	}
	if res.Version.Reason != ReasonAI || res.Version.Tool != "update_checkpoint" || res.Version.Size != int64(len(old)) {
		t.Errorf("ficha: %+v", res.Version)
	}
	hist, err := History(dir, "003-b.scad")
	if err != nil || len(hist) != 1 || hist[0].ID != res.Version.ID || !hist[0].SavedAt.Equal(t0) {
		t.Errorf("historial: %+v %v", hist, err)
	}
	// El resto del árbol sigue igual y ningún .scad nuevo apareció.
	files, _ := workspace.Scan(dir)
	if len(files) != 4 {
		t.Errorf("Scan ve %d archivos: el historial no es un checkpoint", len(files))
	}
	for _, f := range files {
		if f.FileName == "003-b.scad" && (f.ParentFile != "002-a.scad" || f.Description != "desc de 003-b.scad") {
			t.Errorf("Scan lee mal la cabecera: %+v", f)
		}
	}
}

func TestUpdateNoteReplacesDescription(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	res := mustUpdate(t, dir, "004-lado.scad", "cube(2);\n", "  agrega \"ventilas\"\n y\ttapa  ", 0)
	if res.Description != `agrega "ventilas" y tapa` || res.Parent != "001-base.scad" {
		t.Fatalf("resultado: %+v", res)
	}
	files, _ := workspace.Scan(dir)
	for _, f := range files {
		if f.FileName == "004-lado.scad" && (f.Description != `agrega "ventilas" y tapa` || f.ParentFile != "001-base.scad") {
			t.Errorf("Scan: %+v", f)
		}
	}
	if res.Version.Note != `agrega "ventilas" y tapa` {
		t.Errorf("nota del respaldo: %q", res.Version.Note)
	}
	// Una nota de mil letras se acota en el historial.
	res2 := mustUpdate(t, dir, "004-lado.scad", "cube(3);\n", strings.Repeat("ñ", 1000), 0)
	if n := len([]rune(res2.Version.Note)); n != maxNoteRunes {
		t.Errorf("nota sin acotar: %d", n)
	}
}

func TestUpdateIdenticalWritesNothing(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	path := filepath.Join(dir, "002-a.scad")
	oldTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	same := read(t, path)
	// Con la cabecera propia y sin ella: el mismo resultado.
	for _, src := range []string{same, "cube(1);\n"} {
		res := mustUpdate(t, dir, "002-a.scad", src, "", 0)
		if !res.Unchanged || res.Version.ID != "" || res.Parent != "001-base.scad" || res.Description != "desc de 002-a.scad" {
			t.Fatalf("esperaba Unchanged: %+v", res)
		}
	}
	if st, _ := os.Stat(path); !st.ModTime().Equal(oldTime) {
		t.Error("un cambio nulo tocó el archivo")
	}
	if _, err := os.Stat(filepath.Join(dir, ".scadlab")); !os.IsNotExist(err) {
		t.Error("un cambio nulo creó .scadlab")
	}
	if h, _ := History(dir, "002-a.scad"); len(h) != 0 {
		t.Errorf("historial: %+v", h)
	}
	// Con la misma nota tampoco cambia.
	mustUpdate(t, dir, "002-a.scad", "cube(1);\n", "nuevo desc", 0)
	if res := mustUpdate(t, dir, "002-a.scad", "cube(1);\n", "nuevo desc", 0); !res.Unchanged {
		t.Errorf("misma nota y mismo código: %+v", res)
	}
	if h, _ := History(dir, "002-a.scad"); len(h) != 1 {
		t.Errorf("historial: %+v", h)
	}
}

func TestUpdateBacksUpBeforeWriting(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	path := filepath.Join(dir, "002-a.scad")
	before := read(t, path)
	// .scadlab/history es un archivo: el respaldo no se puede guardar y entonces
	// el archivo NO se toca.
	if err := os.MkdirAll(filepath.Join(dir, ".scadlab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".scadlab", "history"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(dir, "002-a.scad", "cube(99);\n", "", 0); err == nil {
		t.Fatal("quiero error: no se pudo respaldar")
	}
	if got := read(t, path); got != before {
		t.Errorf("el archivo cambió sin respaldo: %q", got)
	}
	// Ni queda un temporal suelto.
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporal suelto: %s", e.Name())
		}
	}
}

func TestUpdateKeepsPermissionsAndTouchesMtime(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	path := filepath.Join(dir, "002-a.scad")
	oldTime := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(path, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	mustUpdate(t, dir, "002-a.scad", "cube(7);\n", "", 0)
	st, _ := os.Stat(path)
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("permisos: %v", st.Mode().Perm())
	}
	// Es una edición: el árbol tiene que ver la fecha nueva.
	if st.ModTime().Equal(oldTime) {
		t.Error("la fecha de modificación no cambió")
	}
}

func TestUpdateHeaderlessBomAndCRLF(t *testing.T) {
	clock(t, t0, time.Second)
	dir := t.TempDir()
	// Sin cabecera: sigue sin ella (con nota se le pone una).
	write(t, dir, "001-x.scad", "cube(1);\n")
	res := mustUpdate(t, dir, "001-x.scad", "// @checkpoint parent=zzz.scad desc=\"y\"\ncube(2);\n", "", 0)
	if got := read(t, filepath.Join(dir, "001-x.scad")); got != "cube(2);\n" || res.Parent != "" {
		t.Errorf("sin cabecera: %q %+v", got, res)
	}
	mustUpdate(t, dir, "001-x.scad", "cube(3);\n", "la nota", 0)
	if got := read(t, filepath.Join(dir, "001-x.scad")); got != Header("", "la nota")+"\ncube(3);\n" {
		t.Errorf("con nota: %q", got)
	}
	// BOM y CRLF: se conserva el BOM, la cabecera tal cual y el fin de línea del código nuevo.
	write(t, dir, "002-y.scad", "\uFEFF// @checkpoint   parent=001-x.scad   desc=\"hola\"\r\ncube(1);\r\n")
	mustUpdate(t, dir, "002-y.scad", "cube(2);\r\ncube(3);\r\n", "", 0)
	want := "\uFEFF// @checkpoint   parent=001-x.scad   desc=\"hola\"\r\ncube(2);\r\ncube(3);\r\n"
	if got := read(t, filepath.Join(dir, "002-y.scad")); got != want {
		t.Errorf("BOM/CRLF:\n%q\nquiero\n%q", got, want)
	}
	// Un código nuevo vacío (o solo una cabecera) no borra el modelo.
	for _, src := range []string{"", "   \n", "// @checkpoint parent= desc=\"x\"\n"} {
		if _, err := Update(dir, "002-y.scad", src, "", 0); err == nil {
			t.Errorf("%q: quiero error", src)
		}
	}
	if got := read(t, filepath.Join(dir, "002-y.scad")); got != want {
		t.Errorf("el archivo cambió: %q", got)
	}
}

func TestUpdateMissingAndTrashed(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	if _, err := Update(dir, "009-no-existe.scad", "cube(1);\n", "", 0); err == nil || strings.Contains(err.Error(), "restore_checkpoint") {
		t.Errorf("inexistente: %v", err)
	}
	trashed, err := Trash(dir, "004-lado.scad")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Update(dir, "004-lado.scad", "cube(1);\n", "", 0)
	if err == nil || !strings.Contains(err.Error(), "restore_checkpoint") || !strings.Contains(err.Error(), trashed) {
		t.Errorf("en la papelera: el error debe sugerir restore_checkpoint con %s: %v", trashed, err)
	}
	if exists(dir, "004-lado.scad") {
		t.Error("Update resucitó el archivo de la papelera")
	}
	// Una carpeta con nombre de checkpoint no es un checkpoint.
	if err := os.Mkdir(filepath.Join(dir, "005-carpeta.scad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(dir, "005-carpeta.scad", "cube(1);\n", "", 0); err == nil {
		t.Error("una carpeta no se reescribe")
	}
}

func TestHistoryCapAndPrune(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	for i := 1; i <= 5; i++ {
		mustUpdate(t, dir, "003-b.scad", fmt.Sprintf("cube(%d);\n", i), "", 3)
	}
	hist, _ := History(dir, "003-b.scad")
	if len(hist) != 3 {
		t.Fatalf("con tope 3 quedaron %d: %+v", len(hist), hist)
	}
	// La más nueva primero: el respaldo de la versión 4 (cube(4)), luego 3 y 2.
	for i, want := range []string{"cube(4);", "cube(3);", "cube(2);"} {
		got, _ := ReadVersion(dir, "003-b.scad", hist[i].ID)
		if !strings.Contains(got, want) {
			t.Errorf("versión %d: %q no contiene %q", i, got, want)
		}
	}
	// En disco: solo seis archivos (tres .scad y tres .json): la poda borra los dos de cada versión.
	if names := histFiles(t, dir, "003-b.scad"); len(names) != 6 {
		t.Errorf("archivos en el historial: %v", names)
	}
	// El archivo en sí nunca se poda y el resto del árbol sigue ahí.
	if !exists(dir, "003-b.scad") || len(mustScan(t, dir)) != 4 {
		t.Error("la poda tocó checkpoints")
	}
	// Bajar el tope poda en la siguiente edición; 0 es el tope de fábrica (20).
	mustUpdate(t, dir, "003-b.scad", "cube(6);\n", "", 1)
	if hist, _ = History(dir, "003-b.scad"); len(hist) != 1 {
		t.Errorf("tope 1: %+v", hist)
	}
	for i := 0; i < 25; i++ {
		mustUpdate(t, dir, "002-a.scad", fmt.Sprintf("cube(%d);\n", 100+i), "", 0)
	}
	if hist, _ = History(dir, "002-a.scad"); len(hist) != DefaultHistoryMax {
		t.Errorf("tope de fábrica: %d versiones", len(hist))
	}
	// Los historiales son por archivo: 003 no se tocó al editar 002.
	if hist, _ = History(dir, "003-b.scad"); len(hist) != 1 {
		t.Errorf("003 cambió: %+v", hist)
	}
}

func mustScan(t *testing.T, dir string) []workspace.CheckpointFile {
	t.Helper()
	files, err := workspace.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestVersionIDsCollideAndNeverGoBackwards(t *testing.T) {
	// Reloj parado: tres respaldos en el mismo segundo → id, id-2, id-3.
	fixNow(t)
	dir := tree(t)
	var ids []string
	for i := 1; i <= 3; i++ {
		ids = append(ids, mustUpdate(t, dir, "002-a.scad", fmt.Sprintf("cube(%d);\n", i+10), "", 0).Version.ID)
	}
	if ids[0] != "20261015-100000" || ids[1] != "20261015-100000-2" || ids[2] != "20261015-100000-3" {
		t.Errorf("ids: %v", ids)
	}
	hist, _ := History(dir, "002-a.scad")
	if len(hist) != 3 || hist[0].ID != ids[2] || hist[2].ID != ids[0] {
		t.Errorf("orden: %+v", hist)
	}
	// El reloj retrocede (fin del horario de verano): el id nuevo sigue siendo el más grande.
	early := time.Date(2026, 10, 15, 9, 0, 0, 0, time.Local)
	now = func() time.Time { return early }
	back := mustUpdate(t, dir, "002-a.scad", "cube(50);\n", "", 0).Version.ID
	if back != "20261015-100000-4" {
		t.Errorf("con el reloj atrás: %s", back)
	}
	if hist, _ = History(dir, "002-a.scad"); hist[0].ID != back {
		t.Errorf("la más nueva no es la última: %+v", hist)
	}
	// Mismo número de id con sufijo > 9: el orden es numérico, no de texto.
	for i := 0; i < 8; i++ {
		mustUpdate(t, dir, "002-a.scad", fmt.Sprintf("cube(%d);\n", 200+i), "", 0)
	}
	hist, _ = History(dir, "002-a.scad")
	if hist[0].ID != "20261015-100000-12" || hist[1].ID != "20261015-100000-11" {
		t.Errorf("orden numérico: %v %v", hist[0].ID, hist[1].ID)
	}
	if !EditedByAI(dir, "002-a.scad") {
		t.Error("EditedByAI debe mirar la más nueva por número")
	}
}

func TestVersionIDsReservedByAnotherProcess(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	// Otro proceso ya tomó el id y el -2 (el .json reservado): se sigue de largo.
	hd := histDir(dir, "002-a.scad")
	if err := os.MkdirAll(hd, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"20261015-100000", "20261015-100000-2"} {
		if err := os.WriteFile(filepath.Join(hd, id+".json"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res := mustUpdate(t, dir, "002-a.scad", "cube(4);\n", "", 0)
	if res.Version.ID != "20261015-100000-3" {
		t.Errorf("id: %s", res.Version.ID)
	}
	// Las reservas a medias (json vacío) no se listan como versiones.
	hist, _ := History(dir, "002-a.scad")
	if len(hist) != 1 || hist[0].ID != res.Version.ID {
		t.Errorf("historial: %+v", hist)
	}
}

func TestRestoreVersionSavesCurrentAndKeepsCurrentParent(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	original := read(t, filepath.Join(dir, "003-b.scad"))
	if EditedByAI(dir, "003-b.scad") {
		t.Fatal("sin historial no está editado por la IA")
	}
	ai := mustUpdate(t, dir, "003-b.scad", "color(\"blue\") cube(9);\n", "cambia a azul", 0)
	if !EditedByAI(dir, "003-b.scad") {
		t.Fatal("tras update_checkpoint debe figurar editado por la IA")
	}
	edited := read(t, filepath.Join(dir, "003-b.scad"))
	// Mientras tanto el usuario cambia de rama el archivo (metadato del árbol).
	if err := SetParent(dir, "003-b.scad", "004-lado.scad"); err != nil {
		t.Fatal(err)
	}

	saved, err := RestoreVersion(dir, "003-b.scad", ai.Version.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Reason != ReasonRestore || saved.Tool != "" {
		t.Errorf("ficha: %+v", saved)
	}
	// Cuerpo y desc de la versión; padre ACTUAL.
	want := strings.Replace(original, "parent=002-a.scad", "parent=004-lado.scad", 1)
	if got := read(t, filepath.Join(dir, "003-b.scad")); got != want {
		t.Errorf("restaurado:\n%q\nquiero\n%q", got, want)
	}
	// Lo que había (editado por la IA, con el padre nuevo) quedó guardado.
	if got, _ := ReadVersion(dir, "003-b.scad", saved.ID); got != strings.Replace(edited, "parent=002-a.scad", "parent=004-lado.scad", 1) {
		t.Errorf("respaldo de la restauración: %q", got)
	}
	// El indicador se apaga solo y el deshacer se puede deshacer.
	if EditedByAI(dir, "003-b.scad") {
		t.Error("tras restaurar ya no es «editado por la IA»")
	}
	if _, err := RestoreVersion(dir, "003-b.scad", saved.ID, 0); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "003-b.scad")); !strings.Contains(got, `color("blue") cube(9);`) || !strings.Contains(got, "parent=004-lado.scad") {
		t.Errorf("rehacer: %q", got)
	}
	hist, _ := History(dir, "003-b.scad")
	if len(hist) != 3 || hist[0].Reason != ReasonRestore || hist[2].Reason != ReasonAI {
		t.Errorf("historial: %+v", hist)
	}
}

func TestRestoreVersionRespectsTheCap(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	first := mustUpdate(t, dir, "002-a.scad", "cube(11);\n", "", 2)
	for i := 0; i < 4; i++ {
		if _, err := RestoreVersion(dir, "002-a.scad", currentOldest(t, dir, "002-a.scad"), 2); err != nil {
			t.Fatal(err)
		}
	}
	hist, _ := History(dir, "002-a.scad")
	if len(hist) != 2 {
		t.Errorf("tope 2: %+v", hist)
	}
	if _, err := ReadVersion(dir, "002-a.scad", first.Version.ID); err == nil {
		t.Error("la versión más vieja debía podarse")
	}
}

func currentOldest(t *testing.T, dir, file string) string {
	t.Helper()
	hist, err := History(dir, file)
	if err != nil || len(hist) == 0 {
		t.Fatalf("historial vacío: %v", err)
	}
	return hist[len(hist)-1].ID
}

func TestRestoreVersionRebuildsHeaderWithoutParentToken(t *testing.T) {
	clock(t, t0, time.Second)
	dir := t.TempDir()
	write(t, dir, "001-a.scad", "// @checkpoint desc=\"sin padre\"\ncube(1);\n")
	write(t, dir, "002-b.scad", Header("001-a.scad", "b")+"\ncube(2);\n")
	// La versión vieja de 002 no tenía parent=: se rearma con el padre actual.
	hd := histDir(dir, "002-b.scad")
	if err := os.MkdirAll(hd, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, hd, "20260101-000000.scad", "// @checkpoint desc=\"viejo\"\ncube(0);\n")
	write(t, hd, "20260101-000000.json", `{"savedAt":"2026-01-01T00:00:00Z","reason":"ai","tool":"update_checkpoint","note":"","size":10}`)
	if _, err := RestoreVersion(dir, "002-b.scad", "20260101-000000", 0); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "002-b.scad")); got != Header("001-a.scad", "viejo")+"\ncube(0);\n" {
		t.Errorf("cabecera: %q", got)
	}
}

func TestRestoreVersionErrors(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	before := read(t, filepath.Join(dir, "002-a.scad"))
	for _, id := range []string{"20260101-000000", "", "../../etc/passwd", "20261015-100000/../x"} {
		if _, err := RestoreVersion(dir, "002-a.scad", id, 0); err == nil {
			t.Errorf("id %q: quiero error", id)
		}
	}
	if _, err := RestoreVersion(dir, "009-no.scad", "20260101-000000", 0); err == nil {
		t.Error("archivo inexistente: quiero error")
	}
	if got := read(t, filepath.Join(dir, "002-a.scad")); got != before {
		t.Errorf("un error tocó el archivo: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".scadlab")); !os.IsNotExist(err) {
		t.Error("un error creó .scadlab")
	}
}

func TestHostileNamesAndIDs(t *testing.T) {
	dir := tree(t)
	outside := t.TempDir()
	write(t, outside, "secreto.scad", "secreto")
	names := []string{
		"", ".scad", "../secreto.scad", filepath.Join(outside, "secreto.scad"),
		"a/b.scad", "nul\x00.scad", "x\n.scad", "sin-extension", "../../etc/passwd",
	}
	if runtime.GOOS == "windows" {
		names = append(names, "..\\secreto.scad") // en POSIX es un nombre normal, no sale de la carpeta
	}
	for _, n := range names {
		if _, err := Update(dir, n, "cube(1);\n", "", 0); err == nil {
			t.Errorf("Update(%q): quiero error", n)
		}
		if _, err := History(dir, n); err == nil {
			t.Errorf("History(%q): quiero error", n)
		}
		if _, err := ReadVersion(dir, n, "20261015-100000"); err == nil {
			t.Errorf("ReadVersion(%q): quiero error", n)
		}
		if _, err := RestoreVersion(dir, n, "20261015-100000", 0); err == nil {
			t.Errorf("RestoreVersion(%q): quiero error", n)
		}
		if EditedByAI(dir, n) {
			t.Errorf("EditedByAI(%q) = true", n)
		}
	}
	ids := []string{
		"", "..", "../x", "x/../y", "20261015-100000.scad", "20261015-100000/", "2026101-100000", "20261015-10000",
		"20261015-100000-0x", "20261015-100000-", " 20261015-100000", "20261015-100000\n", "20261015-100000-1234567", "ａ0261015-100000",
	}
	for _, id := range ids {
		if ValidVersionID(id) == nil {
			t.Errorf("ValidVersionID(%q) aceptó un id hostil", id)
		}
		if _, err := ReadVersion(dir, "002-a.scad", id); err == nil {
			t.Errorf("ReadVersion id %q: quiero error", id)
		}
	}
	for _, id := range []string{"20261015-100000", "20261015-100000-2", "20261015-100000-123456"} {
		if err := ValidVersionID(id); err != nil {
			t.Errorf("ValidVersionID(%q): %v", id, err)
		}
	}
	if got := read(t, filepath.Join(outside, "secreto.scad")); got != "secreto" {
		t.Error("tocó un archivo fuera del workspace")
	}
}

func TestUpdateRefusesSymlinks(t *testing.T) {
	skipNoSymlinks(t)
	clock(t, t0, time.Second)

	// 1) El archivo es un enlace: el rename lo reemplazaría y el destino quedaría sin tocar.
	dir := tree(t)
	outside := t.TempDir()
	target := filepath.Join(outside, "real.scad")
	if err := os.WriteFile(target, []byte("// @checkpoint parent= desc=\"x\"\ncube(1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "005-enlace.scad")); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(dir, "005-enlace.scad", "cube(2);\n", "", 0); err == nil {
		t.Error("un enlace no se reescribe")
	}
	if _, err := RestoreVersion(dir, "005-enlace.scad", "20261015-100000", 0); err == nil {
		t.Error("un enlace no se restaura")
	}
	if got := read(t, target); !strings.Contains(got, "cube(1)") {
		t.Errorf("el destino del enlace cambió: %q", got)
	}
	if fi, _ := os.Lstat(filepath.Join(dir, "005-enlace.scad")); fi.Mode()&os.ModeSymlink == 0 {
		t.Error("el enlace fue reemplazado")
	}

	// 2) .scadlab y .scadlab/history enlazados: nada se escribe fuera.
	for _, linkAt := range []string{".scadlab", filepath.Join(".scadlab", "history")} {
		d := tree(t)
		out := t.TempDir()
		if linkAt != ".scadlab" {
			if err := os.MkdirAll(filepath.Join(d, ".scadlab"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(out, filepath.Join(d, linkAt)); err != nil {
			t.Fatal(err)
		}
		before := read(t, filepath.Join(d, "002-a.scad"))
		if _, err := Update(d, "002-a.scad", "cube(2);\n", "", 0); err == nil {
			t.Errorf("%s enlazado: quiero error", linkAt)
		}
		if got := read(t, filepath.Join(d, "002-a.scad")); got != before {
			t.Errorf("%s enlazado: el archivo cambió", linkAt)
		}
		if ents, _ := os.ReadDir(out); len(ents) != 0 {
			t.Errorf("%s enlazado: se escribió fuera: %v", linkAt, ents)
		}
		if _, err := History(d, "002-a.scad"); err == nil {
			t.Errorf("%s enlazado: History debe negarse", linkAt)
		}
		if EditedByAI(d, "002-a.scad") {
			t.Errorf("%s enlazado: EditedByAI debe ser falso", linkAt)
		}
	}

	// 3) La carpeta del archivo (.scadlab/history/<archivo>) enlazada.
	d := tree(t)
	out := t.TempDir()
	write(t, out, "20261015-100000.scad", "ajeno")
	write(t, out, "20261015-100000.json", `{"reason":"ai"}`)
	if err := os.MkdirAll(filepath.Join(d, ".scadlab", "history"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, histDir(d, "002-a.scad")); err != nil {
		t.Fatal(err)
	}
	if _, err := Update(d, "002-a.scad", "cube(2);\n", "", 0); err == nil {
		t.Error("carpeta del archivo enlazada: quiero error")
	}
	if _, err := ReadVersion(d, "002-a.scad", "20261015-100000"); err == nil {
		t.Error("ReadVersion por una carpeta enlazada debe negarse")
	}
	if ents, _ := os.ReadDir(out); len(ents) != 2 {
		t.Errorf("se escribió fuera: %v", ents)
	}

	// 4) Una versión que es un enlace no se lee ni se lista.
	d = tree(t)
	secret := filepath.Join(t.TempDir(), "secreto.txt")
	if err := os.WriteFile(secret, []byte("secreto"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(histDir(d, "002-a.scad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(histDir(d, "002-a.scad"), "20261015-100000.scad")); err != nil {
		t.Fatal(err)
	}
	write(t, histDir(d, "002-a.scad"), "20261015-100000.json", `{"savedAt":"2026-10-15T10:00:00Z","reason":"ai","tool":"","note":"","size":7}`)
	if _, err := ReadVersion(d, "002-a.scad", "20261015-100000"); err == nil {
		t.Error("una versión enlazada no se lee")
	}
	if hist, _ := History(d, "002-a.scad"); len(hist) != 0 {
		t.Errorf("una versión enlazada no se lista: %+v", hist)
	}
	if _, err := RestoreVersion(d, "002-a.scad", "20261015-100000", 0); err == nil {
		t.Error("una versión enlazada no se restaura")
	}
}

func TestEditedByAIIsCheapAndTracksTheNewest(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	if EditedByAI(dir, "002-a.scad") {
		t.Error("sin historial")
	}
	ai := mustUpdate(t, dir, "002-a.scad", "cube(2);\n", "", 0)
	if !EditedByAI(dir, "002-a.scad") {
		t.Error("tras la IA")
	}
	// Un .scad de versión ilegible no cambia la respuesta: no se lee el código.
	if err := os.WriteFile(filepath.Join(histDir(dir, "002-a.scad"), ai.Version.ID+".scad"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !EditedByAI(dir, "002-a.scad") {
		t.Error("EditedByAI solo lee el .json")
	}
	// Una reserva a medias (json vacío, más nuevo) se salta: la última completa manda.
	if err := os.WriteFile(filepath.Join(histDir(dir, "002-a.scad"), "29990101-000000.json"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !EditedByAI(dir, "002-a.scad") {
		t.Error("una reserva a medias no apaga el indicador")
	}
	if _, err := RestoreVersion(dir, "002-a.scad", ai.Version.ID, 0); err != nil {
		// el .scad de la versión quedó vacío a propósito arriba: restaurar igual escribe vacío
		t.Fatal(err)
	}
}

func TestUpdateConcurrentSameFile(t *testing.T) {
	dir := tree(t) // reloj real: los ids chocan dentro del mismo segundo
	var wg sync.WaitGroup
	const n = 8
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Update(dir, "003-b.scad", fmt.Sprintf("cube(%d);\n", 1000+i), "", 50)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	hist, _ := History(dir, "003-b.scad")
	if len(hist) != n {
		t.Fatalf("esperaba %d respaldos, hay %d", n, len(hist))
	}
	seen := map[string]bool{}
	for _, v := range hist {
		if seen[v.ID] {
			t.Errorf("id repetido: %s", v.ID)
		}
		seen[v.ID] = true
	}
	if got := read(t, filepath.Join(dir, "003-b.scad")); !strings.HasPrefix(got, Header("002-a.scad", "desc de 003-b.scad")+"\ncube(10") {
		t.Errorf("archivo final roto: %q", got)
	}
}

// --- historial y papelera -------------------------------------------------------

func TestTrashAndRestoreCarryTheHistory(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	v1 := mustUpdate(t, dir, "004-lado.scad", "cube(21);\n", "uno", 0).Version
	mustUpdate(t, dir, "004-lado.scad", "cube(22);\n", "dos", 0)
	before, _ := History(dir, "004-lado.scad")

	name, err := Trash(dir, "004-lado.scad")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(histDir(dir, "004-lado.scad")); !os.IsNotExist(err) {
		t.Error("el historial sigue donde estaba")
	}
	moved := filepath.Join(dir, ".scadlab", "trash", name+".history")
	if _, err := os.Stat(filepath.Join(moved, v1.ID+".scad")); err != nil {
		t.Errorf("el historial no está junto al archivo de la papelera: %v", err)
	}
	// La papelera lista solo el checkpoint, no la carpeta del historial.
	items, err := ListTrash(dir)
	if err != nil || len(items) != 1 || items[0].Name != name {
		t.Errorf("ListTrash: %+v %v", items, err)
	}
	if h, _ := History(dir, "004-lado.scad"); len(h) != 0 {
		t.Errorf("History de un archivo que no está: %+v", h)
	}

	restored, err := Restore(dir, name)
	if err != nil || restored != "004-lado.scad" {
		t.Fatalf("Restore: %s %v", restored, err)
	}
	after, _ := History(dir, "004-lado.scad")
	if len(after) != len(before) || after[0].ID != before[0].ID || after[1].Note != "uno" {
		t.Errorf("el historial no volvió igual:\n%+v\n%+v", after, before)
	}
	if got, _ := ReadVersion(dir, "004-lado.scad", v1.ID); !strings.Contains(got, "cube(1);") {
		t.Errorf("contenido de la versión: %q", got)
	}
	if _, err := os.Stat(moved); !os.IsNotExist(err) {
		t.Error("la carpeta de la papelera debía irse con la restauración")
	}
	if !TrashEmpty(dir) {
		ents, _ := os.ReadDir(filepath.Join(dir, ".scadlab", "trash"))
		t.Errorf("la papelera debía quedar vacía: %v", ents)
	}
	// Y se sigue pudiendo deshacer lo de la IA.
	if _, err := RestoreVersion(dir, "004-lado.scad", v1.ID, 0); err != nil {
		t.Errorf("restaurar tras el viaje: %v", err)
	}
}

func TestRestoreWithAnotherNumberMovesHistoryToTheNewName(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	mustUpdate(t, dir, "004-lado.scad", "cube(21);\n", "", 0)
	name, err := Trash(dir, "004-lado.scad")
	if err != nil {
		t.Fatal(err)
	}
	// Alguien ocupó el nombre: vuelve con el siguiente número.
	mk(t, dir, "004-lado.scad", "001-base.scad")
	restored, err := Restore(dir, name)
	if err != nil || restored != "005-lado.scad" {
		t.Fatalf("Restore: %s %v", restored, err)
	}
	if h, _ := History(dir, "005-lado.scad"); len(h) != 1 {
		t.Errorf("historial bajo el nombre nuevo: %+v", h)
	}
	if h, _ := History(dir, "004-lado.scad"); len(h) != 0 {
		t.Errorf("el historial no es del 004 nuevo: %+v", h)
	}
}

func TestRestoreMergesHistoryWithoutOverwriting(t *testing.T) {
	fixNow(t) // todo en el mismo segundo: los ids de las dos historias chocan
	dir := tree(t)
	mustUpdate(t, dir, "004-lado.scad", "cube(21);\n", "viejo", 0)
	name, err := Trash(dir, "004-lado.scad")
	if err != nil {
		t.Fatal(err)
	}
	// Un 004-lado nuevo gana su propia historia y luego el usuario lo borra a mano:
	// la carpeta queda huérfana y el nombre libre.
	mk(t, dir, "004-lado.scad", "001-base.scad")
	mustUpdate(t, dir, "004-lado.scad", "cube(99);\n", "nuevo", 0)
	if err := os.Remove(filepath.Join(dir, "004-lado.scad")); err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(dir, name)
	if err != nil || restored != "004-lado.scad" {
		t.Fatalf("Restore: %s %v", restored, err)
	}
	hist, _ := History(dir, "004-lado.scad")
	if len(hist) != 2 {
		t.Fatalf("esperaba las dos historias juntas: %+v", hist)
	}
	notes := map[string]string{}
	for _, v := range hist {
		got, err := ReadVersion(dir, "004-lado.scad", v.ID)
		if err != nil {
			t.Fatal(err)
		}
		notes[v.Note] = got
	}
	if !strings.Contains(notes["nuevo"], "cube(1);") || !strings.Contains(notes["viejo"], "cube(1);") {
		t.Errorf("las versiones no son las de cada historia: %v", notes)
	}
	if hist[0].ID == hist[1].ID {
		t.Error("ids repetidos")
	}
	if _, err := os.Stat(filepath.Join(dir, ".scadlab", "trash", name+".history")); !os.IsNotExist(err) {
		t.Error("la carpeta de la papelera debía irse")
	}
}

func TestMoveHistoryMergesVersionByVersion(t *testing.T) {
	root := t.TempDir()
	src, dst := filepath.Join(root, "src"), filepath.Join(root, "dst")
	for _, d := range []string{src, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pair := func(dir, id, code, note string) {
		write(t, dir, id+".scad", code)
		write(t, dir, id+".json", fmt.Sprintf(`{"savedAt":"2026-10-15T10:00:00Z","reason":"ai","tool":"","note":%q,"size":%d}`, note, len(code)))
	}
	pair(src, "20261015-100000", "src-1", "s1")
	pair(src, "20261015-100000-2", "src-2", "s2")
	pair(dst, "20261015-100000", "dst-1", "d1")
	pair(dst, "20261015-100000-2", "dst-2", "d2")
	if err := moveHistory(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("la carpeta de origen debía irse")
	}
	// Nada se pisó: las dos que ya estaban siguen y las otras dos tomaron ids libres.
	if got := read(t, filepath.Join(dst, "20261015-100000.scad")); got != "dst-1" {
		t.Errorf("se pisó: %q", got)
	}
	if got := read(t, filepath.Join(dst, "20261015-100000-2.scad")); got != "dst-2" {
		t.Errorf("se pisó: %q", got)
	}
	ids := versionIDs(dst)
	if len(ids) != 4 {
		t.Fatalf("ids: %v", ids)
	}
	notes := map[string]bool{}
	for _, id := range ids {
		m, ok := readMeta(dst, id)
		if !ok {
			t.Fatalf("%s sin json", id)
		}
		notes[m.Note] = true
		if _, err := os.Stat(filepath.Join(dst, id+".scad")); err != nil {
			t.Errorf("%s sin .scad: %v", id, err)
		}
	}
	for _, n := range []string{"s1", "s2", "d1", "d2"} {
		if !notes[n] {
			t.Errorf("falta la versión %s: %v", n, notes)
		}
	}
}

func TestTrashBranchMovesEveryHistory(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	mustUpdate(t, dir, "002-a.scad", "cube(1002);\n", "", 0)
	mustUpdate(t, dir, "003-b.scad", "cube(1003);\n", "", 0)
	mustUpdate(t, dir, "004-lado.scad", "cube(1004);\n", "", 0)
	names, err := TrashBranch(dir, "002-a.scad")
	if err != nil || len(names) != 2 {
		t.Fatalf("TrashBranch: %v %v", names, err)
	}
	for _, n := range names {
		if _, err := os.Stat(filepath.Join(dir, ".scadlab", "trash", n+".history")); err != nil {
			t.Errorf("sin historial para %s: %v", n, err)
		}
	}
	if h, _ := History(dir, "004-lado.scad"); len(h) != 1 {
		t.Error("el historial de un checkpoint que se queda no se mueve")
	}
	for _, f := range []string{"002-a.scad", "003-b.scad"} {
		if _, err := os.Stat(histDir(dir, f)); !os.IsNotExist(err) {
			t.Errorf("el historial de %s sigue en su sitio", f)
		}
	}
	// Se restauran juntos y recuperan lo suyo.
	for _, n := range names {
		if _, err := Restore(dir, n); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"002-a.scad", "003-b.scad"} {
		if h, _ := History(dir, f); len(h) != 1 {
			t.Errorf("historial de %s: %+v", f, h)
		}
	}
}

func TestPurgeAndEmptyTrashTakeTheHistory(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	mustUpdate(t, dir, "003-b.scad", "cube(3);\n", "", 0)
	mustUpdate(t, dir, "004-lado.scad", "cube(4);\n", "", 0)
	n3, err := Trash(dir, "003-b.scad")
	if err != nil {
		t.Fatal(err)
	}
	n4, err := Trash(dir, "004-lado.scad")
	if err != nil {
		t.Fatal(err)
	}
	tdir := filepath.Join(dir, ".scadlab", "trash")
	if err := PurgeTrashed(dir, n3); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(tdir, n3+".history")); !os.IsNotExist(err) {
		t.Error("PurgeTrashed dejó el historial")
	}
	if _, err := os.Stat(filepath.Join(tdir, n4+".history")); err != nil {
		t.Error("PurgeTrashed se llevó el historial de otro")
	}
	count, err := EmptyTrash(dir)
	if err != nil || count != 1 {
		t.Fatalf("EmptyTrash: %d %v", count, err)
	}
	if ents, _ := os.ReadDir(tdir); len(ents) != 0 {
		t.Errorf("la papelera no quedó vacía: %v", ents)
	}
	if !StarterDeclined(dir) {
		t.Error("vaciar la papelera marca el starter")
	}
}

func TestPurgeAndEmptyTrashDoNotFollowLinkedHistory(t *testing.T) {
	skipNoSymlinks(t)
	clock(t, t0, time.Second)
	dir := tree(t)
	mustUpdate(t, dir, "003-b.scad", "cube(3);\n", "", 0)
	mustUpdate(t, dir, "004-lado.scad", "cube(4);\n", "", 0)
	n3, _ := Trash(dir, "003-b.scad")
	n4, _ := Trash(dir, "004-lado.scad")
	tdir := filepath.Join(dir, ".scadlab", "trash")
	outside := t.TempDir()
	write(t, outside, "importante.txt", "no me borres")
	for _, n := range []string{n3, n4} {
		if err := os.RemoveAll(filepath.Join(tdir, n+".history")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(tdir, n+".history")); err != nil {
			t.Fatal(err)
		}
	}
	if err := PurgeTrashed(dir, n3); err != nil {
		t.Fatal(err)
	}
	if _, err := EmptyTrash(dir); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(outside, "importante.txt")); got != "no me borres" {
		t.Error("se siguió un enlace y se borró fuera de la papelera")
	}
	if ents, _ := os.ReadDir(tdir); len(ents) != 0 {
		t.Errorf("la papelera no quedó vacía: %v", ents)
	}
}

func TestTrashReportsHistoryProblemsButTrashesAnyway(t *testing.T) {
	skipNoSymlinks(t)
	clock(t, t0, time.Second)
	dir := tree(t)
	out := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".scadlab", "history"), 0o755); err != nil {
		t.Fatal(err)
	}
	// El historial de 004 es un enlace: no se mueve (ni se sigue), pero el borrado vale.
	if err := os.Symlink(out, histDir(dir, "004-lado.scad")); err != nil {
		t.Fatal(err)
	}
	name, relinked, err := TrashRelinked(dir, "004-lado.scad")
	if err == nil {
		t.Error("quiero el error junto con lo hecho")
	}
	if name == "" || exists(dir, "004-lado.scad") {
		t.Errorf("el archivo debía ir a la papelera: %q", name)
	}
	_ = relinked
	if _, err := os.Stat(filepath.Join(dir, ".scadlab", "trash", name)); err != nil {
		t.Errorf("no está en la papelera: %v", err)
	}
	// Y los hijos se re-colgaron igual: un fallo del historial no frena el árbol.
	mk(t, dir, "006-hijo.scad", "003-b.scad")
	if err := os.Symlink(out, histDir(dir, "003-b.scad")); err != nil {
		t.Fatal(err)
	}
	_, relinked, err = TrashRelinked(dir, "003-b.scad")
	if err == nil || len(relinked) != 1 || parentOf(t, dir, "006-hijo.scad") != "002-a.scad" {
		t.Errorf("re-colgado con historial roto: %v %v parent=%s", relinked, err, parentOf(t, dir, "006-hijo.scad"))
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Errorf("se escribió fuera: %v", ents)
	}
}

func TestScanDoesNotSeeTheHistory(t *testing.T) {
	clock(t, t0, time.Second)
	dir := tree(t)
	for i := 0; i < 3; i++ {
		mustUpdate(t, dir, "003-b.scad", fmt.Sprintf("cube(%d);\n", 30+i), "", 0)
	}
	if _, err := Trash(dir, "004-lado.scad"); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range mustScan(t, dir) {
		got = append(got, f.FileName)
	}
	if strings.Join(got, ",") != "001-base.scad,002-a.scad,003-b.scad" {
		t.Errorf("Scan ve %v", got)
	}
	if latest := Latest(dir); latest != "003-b.scad" {
		t.Errorf("Latest = %q", latest)
	}
}
