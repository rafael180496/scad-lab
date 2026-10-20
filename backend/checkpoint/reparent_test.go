package checkpoint

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"scadlab/backend/workspace"
)

func parentOf(t *testing.T, dir, file string) string {
	t.Helper()
	files, err := workspace.Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f.FileName == file {
			return f.ParentFile
		}
	}
	t.Fatalf("%s no está en el workspace", file)
	return ""
}

func ls(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// SetParent toca solo el valor de parent=: lo demás, byte a byte.
func TestSetParentKeepsEveryOtherByte(t *testing.T) {
	cases := []struct {
		name, in, parent, want string
	}{
		{"lf", "// @checkpoint parent=001-a.scad desc=\"x\"\ncube(1);\n", "", "// @checkpoint parent= desc=\"x\"\ncube(1);\n"},
		{"crlf", "// @checkpoint parent=001-a.scad desc=\"x\"\r\ncube(1);\r\nsphere(2);\r\n", "009-z.scad", "// @checkpoint parent=009-z.scad desc=\"x\"\r\ncube(1);\r\nsphere(2);\r\n"},
		{"bom", "\xEF\xBB\xBF// @checkpoint parent=001-a.scad desc=\"x\"\ncube(1);\n", "", "\xEF\xBB\xBF// @checkpoint parent= desc=\"x\"\ncube(1);\n"},
		{"bom y crlf", "\xEF\xBB\xBF// @checkpoint parent=001-a.scad desc=\"x\"\r\ncube(1);\r\n", "", "\xEF\xBB\xBF// @checkpoint parent= desc=\"x\"\r\ncube(1);\r\n"},
		{"desc con comillas escapadas", `// @checkpoint parent=001-a.scad desc="Cubo de 12 \"mm\" y \\ barra"` + "\ncube(1);\n", "", `// @checkpoint parent= desc="Cubo de 12 \"mm\" y \\ barra"` + "\ncube(1);\n"},
		{"sin salto final", "// @checkpoint parent=001-a.scad desc=\"x\"", "", "// @checkpoint parent= desc=\"x\""},
		{"espacios raros", "//   @checkpoint   parent=001-a.scad    desc=\"x\"  \ncube(1);\n", "", "//   @checkpoint   parent=    desc=\"x\"  \ncube(1);\n"},
		{"desde raíz a padre", "// @checkpoint parent= desc=\"x\"\ncube(1);\n", "001-a.scad", "// @checkpoint parent=001-a.scad desc=\"x\"\ncube(1);\n"},
		{"parent= dentro del código no se toca", "// @checkpoint parent=001-a.scad desc=\"x\"\n// parent=001-a.scad\n", "", "// @checkpoint parent= desc=\"x\"\n// parent=001-a.scad\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, "002-b.scad", tc.in)
			if err := SetParent(dir, "002-b.scad", tc.parent); err != nil {
				t.Fatal(err)
			}
			if got := read(t, filepath.Join(dir, "002-b.scad")); got != tc.want {
				t.Errorf("\n%q\nquiero\n%q", got, tc.want)
			}
			// Lo que escribe lo lee Scan igual.
			if got := parentOf(t, dir, "002-b.scad"); got != tc.parent {
				t.Errorf("Scan lee parent=%q, quiero %q", got, tc.parent)
			}
			if names := ls(t, dir); !reflect.DeepEqual(names, []string{"002-b.scad"}) {
				t.Errorf("quedaron temporales: %v", names)
			}
		})
	}
}

func TestSetParentKeepsModeAndMtimeAndSkipsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "002-b.scad")
	write(t, dir, "002-b.scad", Header("001-a.scad", "x")+"\ncube(1);\n")
	old := time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(path, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetParent(dir, "002-b.scad", ""); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if !st.ModTime().Equal(old) {
		t.Errorf("la fecha cambió: %v", st.ModTime())
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Errorf("los permisos cambiaron: %v", st.Mode().Perm())
	}
	// Ya era ese padre: no escribe (la fecha quedaría igual de todas formas, así
	// que se mide por el inodo/temporales: ningún temporal ni cambio).
	before := read(t, path)
	if err := SetParent(dir, "002-b.scad", ""); err != nil || read(t, path) != before {
		t.Errorf("sin cambio debía quedar igual: %v", err)
	}
}

func TestSetParentRejects(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "002-b.scad", Header("001-a.scad", "x")+"\n")
	write(t, dir, "003-sin-cabecera.scad", "cube(1);\n")
	for _, tc := range []struct{ file, parent string }{
		{"../002-b.scad", ""},
		{"002-b.scad", "../x.scad"},
		{"002-b.scad", "con espacio.scad"},
		{"002-b.scad", "002-b.scad"},
		{"002-b.scad", "no-es-scad.txt"},
		{"009-no-existe.scad", ""},
		{"003-sin-cabecera.scad", ""},
	} {
		err := SetParent(dir, tc.file, tc.parent)
		if err == nil {
			t.Errorf("%+v: quiero error", tc)
		}
	}
	if got := read(t, filepath.Join(dir, "003-sin-cabecera.scad")); got != "cube(1);\n" {
		t.Errorf("un archivo sin cabecera no se toca: %q", got)
	}
}

func TestSetParentRefusesSymlink(t *testing.T) {
	skipNoSymlinks(t)
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "afuera.scad")
	orig := Header("001-a.scad", "x") + "\ncube(1);\n"
	if err := os.WriteFile(outside, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "002-b.scad")); err != nil {
		t.Fatal(err)
	}
	if err := SetParent(dir, "002-b.scad", ""); err == nil {
		t.Fatal("un enlace no se reescribe")
	}
	if got := read(t, outside); got != orig {
		t.Errorf("el destino del enlace cambió: %q", got)
	}
	if st, _ := os.Lstat(filepath.Join(dir, "002-b.scad")); st.Mode()&os.ModeSymlink == 0 {
		t.Error("el enlace fue reemplazado")
	}
}

// ---- papelera + re-colgado

// big arma: 001 ← 002 ← 003 ← 005, 001 ← 004, y 006 raíz con hijo 007.
func big(t *testing.T) string {
	t.Helper()
	dir := tree(t)
	mk(t, dir, "005-nieto.scad", "003-b.scad")
	mk(t, dir, "006-otra-raiz.scad", "")
	mk(t, dir, "007-hijo.scad", "006-otra-raiz.scad")
	return dir
}

func TestTrashIntermediateRelinksOnlyDirectChildren(t *testing.T) {
	fixNow(t)
	dir := big(t)
	// 003 (hijo de 002) tiene al nieto 005; se borra 002: solo 003 se re-cuelga.
	name, relinked, err := TrashRelinked(dir, "002-a.scad")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(relinked, []string{"003-b.scad"}) {
		t.Errorf("relinked: %v", relinked)
	}
	if got := parentOf(t, dir, "003-b.scad"); got != "001-base.scad" {
		t.Errorf("003: %q", got)
	}
	if got := parentOf(t, dir, "005-nieto.scad"); got != "003-b.scad" {
		t.Errorf("el nieto no debía tocarse: %q", got)
	}
	// El archivo borrado conserva su cabecera tal cual.
	if got := read(t, filepath.Join(dir, ".scadlab", "trash", name)); got != Header("001-base.scad", "desc de 002-a.scad")+"\ncube(1);\n" {
		t.Errorf("el borrado cambió: %q", got)
	}
	// El registro no aparece como elemento de la papelera.
	items, _ := ListTrash(dir)
	if len(items) != 1 || items[0].Name != name {
		t.Errorf("ListTrash: %+v", items)
	}
}

func TestTrashRootChildrenBecomeRoots(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	_, relinked, err := TrashRelinked(dir, "001-base.scad")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(relinked, []string{"002-a.scad", "004-lado.scad"}) {
		t.Errorf("relinked: %v", relinked)
	}
	for _, f := range []string{"002-a.scad", "004-lado.scad"} {
		if got := parentOf(t, dir, f); got != "" {
			t.Errorf("%s debía quedar como raíz: %q", f, got)
		}
	}
	if got := parentOf(t, dir, "003-b.scad"); got != "002-a.scad" {
		t.Errorf("003: %q", got)
	}
	// Ningún padre apunta a un archivo ausente.
	assertNoOrphans(t, dir)
}

func assertNoOrphans(t *testing.T, dir string) {
	t.Helper()
	files, _ := workspace.Scan(dir)
	have := map[string]bool{}
	for _, f := range files {
		have[f.FileName] = true
	}
	for _, f := range files {
		if f.ParentFile != "" && !have[f.ParentFile] {
			t.Errorf("%s cuelga de %s, que no existe", f.FileName, f.ParentFile)
		}
	}
}

func TestTrashLeafChangesNothing(t *testing.T) {
	fixNow(t)
	dir := big(t)
	before := map[string]string{}
	for _, n := range ls(t, dir) {
		if strings.HasSuffix(n, ".scad") {
			before[n] = read(t, filepath.Join(dir, n))
		}
	}
	name, relinked, err := TrashRelinked(dir, "005-nieto.scad")
	if err != nil || len(relinked) != 0 {
		t.Fatalf("%v %v", relinked, err)
	}
	for n, body := range before {
		if n == "005-nieto.scad" {
			continue
		}
		if read(t, filepath.Join(dir, n)) != body {
			t.Errorf("%s cambió", n)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, ".scadlab", "trash", name+relinkSuffix)); err == nil {
		t.Error("una hoja no deja registro")
	}
}

func TestTrashParentAlreadyMissingMakesRoots(t *testing.T) {
	fixNow(t)
	dir := t.TempDir()
	mk(t, dir, "002-b.scad", "001-ausente.scad") // el abuelo ya no está
	mk(t, dir, "003-c.scad", "002-b.scad")
	if _, err := Trash(dir, "002-b.scad"); err != nil {
		t.Fatal(err)
	}
	if got := parentOf(t, dir, "003-c.scad"); got != "" {
		t.Errorf("colgarlo de un ausente repetiría el problema: %q", got)
	}
}

func TestRestoreRelinksChildrenBack(t *testing.T) {
	fixNow(t)
	dir := big(t)
	name, _, err := TrashRelinked(dir, "002-a.scad")
	if err != nil {
		t.Fatal(err)
	}
	got, relinked, err := RestoreRelinked(dir, name)
	if err != nil || got != "002-a.scad" {
		t.Fatalf("%q %v", got, err)
	}
	if !reflect.DeepEqual(relinked, []string{"003-b.scad"}) {
		t.Errorf("relinked: %v", relinked)
	}
	if p := parentOf(t, dir, "003-b.scad"); p != "002-a.scad" {
		t.Errorf("003 debía volver a 002: %q", p)
	}
	// El registro se consumió y no quedan restos.
	if !TrashEmpty(dir) {
		t.Errorf("la papelera debía quedar vacía: %v", ls(t, filepath.Join(dir, ".scadlab", "trash")))
	}
}

func TestRestoreRootRelinksChildren(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	name, _, _ := TrashRelinked(dir, "001-base.scad")
	if _, err := Restore(dir, name); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"002-a.scad", "004-lado.scad"} {
		if p := parentOf(t, dir, f); p != "001-base.scad" {
			t.Errorf("%s: %q", f, p)
		}
	}
}

// El hijo que el usuario movió a otra rama mientras tanto no se toca; el que ya
// no existe tampoco.
func TestRestoreSkipsChildrenThatChanged(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	mk(t, dir, "005-c.scad", "001-base.scad")
	name, relinked, err := TrashRelinked(dir, "001-base.scad")
	if err != nil || len(relinked) != 3 {
		t.Fatalf("%v %v", relinked, err)
	}
	// 004 desaparece (a la papelera) y 002 se muda a otra rama; 005 sigue donde
	// se lo dejó.
	if _, err := Trash(dir, "004-lado.scad"); err != nil {
		t.Fatal(err)
	}
	mk(t, dir, "008-otra.scad", "")
	if err := SetParent(dir, "002-a.scad", "008-otra.scad"); err != nil {
		t.Fatal(err)
	}
	got, back, err := RestoreRelinked(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, []string{"005-c.scad"}) {
		t.Errorf("solo 005 seguía donde se lo dejó: %v", back)
	}
	if p := parentOf(t, dir, "002-a.scad"); p != "008-otra.scad" {
		t.Errorf("002 estaba en otra rama y no se toca: %q", p)
	}
	if p := parentOf(t, dir, "005-c.scad"); p != got {
		t.Errorf("005: %q", p)
	}
}

// Si el nombre original estaba ocupado, los hijos cuelgan del nombre NUEVO.
func TestRestoreRenamedRelinksToNewName(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	name, _, _ := TrashRelinked(dir, "002-a.scad")
	write(t, dir, "002-a.scad", Header("001-base.scad", "otro")+"\nsphere(1);\n")
	got, relinked, err := RestoreRelinked(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	if got == "002-a.scad" || !reflect.DeepEqual(relinked, []string{"003-b.scad"}) {
		t.Fatalf("%q %v", got, relinked)
	}
	if p := parentOf(t, dir, "003-b.scad"); p != got {
		t.Errorf("003 debía colgar del restaurado %s: %q", got, p)
	}
}

func TestRestoreWithoutCycle(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	name, _, _ := TrashRelinked(dir, "002-a.scad") // 003 ahora cuelga de 001
	// El usuario cuelga 001 de 003 (invierte la rama); restaurar 002 no puede
	// colgar 003 de 002 si 002 (hijo de 001) desciende de 003.
	if err := SetParent(dir, "001-base.scad", "003-b.scad"); err != nil {
		t.Fatal(err)
	}
	// 003 ya no cuelga de 001 (cuelga de 001, que cuelga de 003: ciclo del usuario);
	// el registro exige que 003 siga colgando de 001, y colgarlo de 002 (descendiente
	// suyo) lo cerraría otra vez.
	if _, back, err := RestoreRelinked(dir, name); err != nil || len(back) != 0 {
		t.Errorf("no debía re-colgar: %v %v", back, err)
	}
}

func TestTrashBranchDoesNotRelinkAndRestoreIsExact(t *testing.T) {
	fixNow(t)
	dir := big(t)
	before := read(t, filepath.Join(dir, "003-b.scad"))
	names, err := TrashBranch(dir, "002-a.scad")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range ls(t, filepath.Join(dir, ".scadlab", "trash")) {
		if strings.HasSuffix(n, relinkSuffix) {
			t.Errorf("TrashBranch no re-cuelga nada: %s", n)
		}
	}
	for _, n := range names {
		if _, err := Restore(dir, n); err != nil {
			t.Fatal(err)
		}
	}
	if read(t, filepath.Join(dir, "003-b.scad")) != before {
		t.Error("003 cambió")
	}
}

func TestTrashRelinkKeepsActiveParentRule(t *testing.T) {
	fixNow(t)
	dir := big(t)
	// El activo era el borrado: pasa a su padre.
	if err := SetActiveParent(dir, "002-a.scad"); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(dir, "002-a.scad"); err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != "001-base.scad" {
		t.Errorf("activo: %q", got)
	}
	// El activo era un hijo re-colgado: sigue siendo el mismo.
	if err := SetActiveParent(dir, "003-b.scad"); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(dir, "001-base.scad"); err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != "003-b.scad" {
		t.Errorf("activo: %q", got)
	}
	// Una raíz activa borrada: sin padre, se limpia.
	if err := SetActiveParent(dir, "006-otra-raiz.scad"); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(dir, "006-otra-raiz.scad"); err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != "" {
		t.Errorf("activo: %q", got)
	}
	if got := parentOf(t, dir, "007-hijo.scad"); got != "" {
		t.Errorf("007 debía quedar como raíz: %q", got)
	}
}

// Un hijo con BOM, CRLF y una descripción con comillas escapadas sale del
// re-colgado igual que entró, salvo el valor de parent=.
func TestTrashChildWithCRLFAndBOM(t *testing.T) {
	fixNow(t)
	dir := t.TempDir()
	mk(t, dir, "001-a.scad", "")
	write(t, dir, "002-b.scad", "\xEF\xBB\xBF"+Header("001-a.scad", `con "comillas" y \ barra`)+"\r\ncube(1);\r\n")
	if _, err := Trash(dir, "001-a.scad"); err != nil {
		t.Fatal(err)
	}
	want := "\xEF\xBB\xBF" + Header("", `con "comillas" y \ barra`) + "\r\ncube(1);\r\n"
	if got := read(t, filepath.Join(dir, "002-b.scad")); got != want {
		t.Errorf("\n%q\nquiero\n%q", got, want)
	}
	files, _ := workspace.Scan(dir)
	if len(files) != 1 || files[0].Description != `con "comillas" y \ barra` {
		t.Errorf("la descripción cambió: %+v", files)
	}
}
