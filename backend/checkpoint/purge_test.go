package checkpoint

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func trashPath(dir string, parts ...string) string {
	return filepath.Join(append([]string{dir, ".scadlab", "trash"}, parts...)...)
}

func TestPurgeTrashedRemovesItAndItsRelink(t *testing.T) {
	fixNow(t)
	dir := big(t)
	n1, _, err := TrashRelinked(dir, "002-a.scad") // deja registro
	if err != nil {
		t.Fatal(err)
	}
	n2, err := Trash(dir, "004-lado.scad")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(trashPath(dir, n1+relinkSuffix)); err != nil {
		t.Fatalf("debía haber registro: %v", err)
	}
	if err := PurgeTrashed(dir, n1); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(trashPath(dir, n1)); err == nil {
		t.Error("el archivo sigue en la papelera")
	}
	if _, err := os.Lstat(trashPath(dir, n1+relinkSuffix)); err == nil {
		t.Error("el registro de re-colgado sigue")
	}
	// El otro elemento sigue, y nada del workspace se tocó.
	items, _ := ListTrash(dir)
	if len(items) != 1 || items[0].Name != n2 {
		t.Errorf("ListTrash: %+v", items)
	}
	if !exists(dir, "003-b.scad") || parentOf(t, dir, "003-b.scad") != "001-base.scad" {
		t.Error("los hijos re-colgados no deben cambiar al eliminar")
	}
	if StarterDeclined(dir) {
		t.Error("aún queda un elemento: no se vació la papelera")
	}
	// Ya no se puede restaurar ni volver a eliminar.
	if _, err := Restore(dir, n1); err == nil {
		t.Error("restaurar lo eliminado debe fallar")
	}
	if err := PurgeTrashed(dir, n1); err == nil {
		t.Error("eliminar dos veces debe fallar")
	}
}

func TestPurgeLastItemMarksStarterDeclined(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	n, _ := Trash(dir, "004-lado.scad")
	if err := PurgeTrashed(dir, n); err != nil {
		t.Fatal(err)
	}
	if !TrashEmpty(dir) || !StarterDeclined(dir) {
		t.Errorf("vacía=%v declinado=%v", TrashEmpty(dir), StarterDeclined(dir))
	}
}

func TestPurgeTrashedRejectsHostileNames(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	outside := filepath.Join(dir, "001-base.scad")
	n, _ := Trash(dir, "004-lado.scad")
	for _, bad := range []string{
		"", "..", "../001-base.scad", "../../001-base.scad.scad", ".scadlab/trash/" + n,
		"/etc/passwd.scad", `..\001-base.scad`, "001-base.scad", // sin marca de fecha
		"20261015-100000-../001-base.scad", n + "\x00", "20261015-100000-.scad",
	} {
		if err := PurgeTrashed(dir, bad); err == nil {
			t.Errorf("PurgeTrashed(%q): quiero error", bad)
		}
	}
	if !exists(dir, "001-base.scad") || read(t, outside) == "" {
		t.Error("tocó algo fuera de la papelera")
	}
	if items, _ := ListTrash(dir); len(items) != 1 {
		t.Errorf("el elemento debía seguir: %+v", items)
	}
}

func TestPurgeTrashedDoesNotFollowSymlinks(t *testing.T) {
	skipNoSymlinks(t)
	fixNow(t)
	dir := tree(t)
	outside := filepath.Join(t.TempDir(), "secreto.scad")
	if err := os.WriteFile(outside, []byte("// no tocar\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, _ := Trash(dir, "004-lado.scad")
	// Un elemento de la papelera que en realidad es un enlace hacia afuera.
	if err := os.Remove(trashPath(dir, n)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, trashPath(dir, n)); err != nil {
		t.Fatal(err)
	}
	if err := PurgeTrashed(dir, n); err != nil {
		t.Fatal(err)
	}
	if read(t, outside) != "// no tocar\n" {
		t.Error("se borró el destino del enlace")
	}
	if _, err := os.Lstat(trashPath(dir, n)); err == nil {
		t.Error("el enlace debía quitarse")
	}
}

func TestPurgeRefusesTrashThatIsALink(t *testing.T) {
	skipNoSymlinks(t)
	fixNow(t)
	dir := tree(t)
	victim := t.TempDir()
	keep := filepath.Join(victim, "20261015-100000-001-base.scad")
	if err := os.WriteFile(keep, []byte("// ajeno\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".scadlab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, trashPath(dir)); err != nil {
		t.Fatal(err)
	}
	if err := PurgeTrashed(dir, "20261015-100000-001-base.scad"); err == nil {
		t.Error("PurgeTrashed sobre una papelera enlazada debe fallar")
	}
	if n, err := EmptyTrash(dir); err == nil || n != 0 {
		t.Errorf("EmptyTrash sobre una papelera enlazada debe fallar: %d %v", n, err)
	}
	if read(t, keep) != "// ajeno\n" {
		t.Error("tocó una carpeta fuera del workspace")
	}
	if StarterDeclined(dir) {
		t.Error("no debía marcar nada")
	}
}

func TestEmptyTrash(t *testing.T) {
	fixNow(t)
	dir := big(t)
	if _, _, err := TrashRelinked(dir, "002-a.scad"); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(dir, "004-lado.scad"); err != nil {
		t.Fatal(err)
	}
	// Cosas ajenas dentro de la papelera: un archivo suelto y una carpeta con contenido.
	if err := os.WriteFile(trashPath(dir, "raro.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trashPath(dir, "refs-viejas", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trashPath(dir, "refs-viejas", "sub", "a.stl"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := ls(t, dir)

	n, err := EmptyTrash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("eliminó %d checkpoints, quiero 2", n)
	}
	if left := ls(t, trashPath(dir)); len(left) != 0 {
		t.Errorf("la papelera debía quedar vacía (y existir): %v", left)
	}
	if !TrashEmpty(dir) || !StarterDeclined(dir) {
		t.Error("vacía y marcada")
	}
	// El workspace no se tocó.
	if after := ls(t, dir); !reflect.DeepEqual(before, after) {
		t.Errorf("el workspace cambió:\n%v\n%v", before, after)
	}
	// Vaciar otra vez no falla y devuelve 0.
	if n, err := EmptyTrash(dir); err != nil || n != 0 {
		t.Errorf("segunda vez: %d %v", n, err)
	}
	// Sin papelera: 0 y sin error.
	if n, err := EmptyTrash(t.TempDir()); err != nil || n != 0 {
		t.Errorf("sin papelera: %d %v", n, err)
	}
}

func TestEmptyTrashDoesNotFollowLinksInside(t *testing.T) {
	skipNoSymlinks(t)
	fixNow(t)
	dir := tree(t)
	if _, err := Trash(dir, "004-lado.scad"); err != nil {
		t.Fatal(err)
	}
	victim := t.TempDir()
	if err := os.WriteFile(filepath.Join(victim, "importante.scad"), []byte("// ajeno\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, trashPath(dir, "enlace-a-carpeta")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(victim, "importante.scad"), trashPath(dir, "enlace-a-archivo")); err != nil {
		t.Fatal(err)
	}
	if _, err := EmptyTrash(dir); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(victim, "importante.scad")); got != "// ajeno\n" {
		t.Errorf("EmptyTrash siguió un enlace: %q", got)
	}
	if left := ls(t, trashPath(dir)); len(left) != 0 {
		t.Errorf("los enlaces debían quitarse: %v", left)
	}
}

func TestEmptyTrashWithNoItemsListedStillEmptiesStrays(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(trashPath(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trashPath(dir, "raro.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := EmptyTrash(dir)
	if err != nil || n != 0 || !TrashEmpty(dir) {
		t.Errorf("%d %v %v", n, err, TrashEmpty(dir))
	}
}

func TestStarterDeclinedIgnoresLinkedMarker(t *testing.T) {
	skipNoSymlinks(t)
	dir := t.TempDir()
	meta := filepath.Join(dir, ".scadlab")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(real, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(meta, starterDeclinedName)); err != nil {
		t.Fatal(err)
	}
	if StarterDeclined(dir) {
		t.Error("una marca que es un enlace no cuenta")
	}
}
