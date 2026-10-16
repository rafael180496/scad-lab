package checkpoint

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// mk escribe un checkpoint con su encabezado.
func mk(t *testing.T, dir, name, parent string) {
	t.Helper()
	write(t, dir, name, Header(parent, "desc de "+name)+"\ncube(1);\n")
}

// tree arma: 001 ← 002 ← 003, 001 ← 004.
func tree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mk(t, dir, "001-base.scad", "")
	mk(t, dir, "002-a.scad", "001-base.scad")
	mk(t, dir, "003-b.scad", "002-a.scad")
	mk(t, dir, "004-lado.scad", "001-base.scad")
	return dir
}

func skipNoSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("crear enlaces simbólicos en Windows exige privilegios")
	}
}

func TestActiveParentBasics(t *testing.T) {
	dir := tree(t)
	if got := ActiveParent(dir); got != "" {
		t.Fatalf("sin archivo: %q", got)
	}
	if err := SetActiveParent(dir, "002-a.scad"); err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != "002-a.scad" {
		t.Errorf("tras Set: %q", got)
	}
	if got := read(t, filepath.Join(dir, ".scadlab", "active-parent")); got != "002-a.scad\n" {
		t.Errorf("contenido: %q", got)
	}
	// Sin temporales a medias en .scadlab.
	ents, _ := os.ReadDir(filepath.Join(dir, ".scadlab"))
	if len(ents) != 1 {
		t.Errorf("sobraron archivos en .scadlab: %v", ents)
	}
	if err := SetActiveParent(dir, ""); err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != "" {
		t.Errorf("tras limpiar: %q", got)
	}
	if err := SetActiveParent(dir, ""); err != nil { // limpiar dos veces no es un error
		t.Errorf("limpiar de nuevo: %v", err)
	}
	if err := SetActiveParent(t.TempDir(), ""); err != nil { // ni sin .scadlab
		t.Errorf("limpiar sin .scadlab: %v", err)
	}
}

func TestActiveParentDangling(t *testing.T) {
	dir := tree(t)
	meta := filepath.Join(dir, ".scadlab")
	if err := os.MkdirAll(meta, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"inexistente": "009-no-existe.scad\n",
		"con ..":      "../001-base.scad\n",
		"absoluto":    filepath.Join(dir, "001-base.scad") + "\n",
		"con NUL":     "001-base.scad\x00\n",
		"vacío":       "",
		"basura":      "no soy un nombre",
		"carpeta":     "lib\n",
		"enorme":      string(make([]byte, 5000)),
	} {
		if err := os.WriteFile(filepath.Join(meta, "active-parent"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := ActiveParent(dir); got != "" {
			t.Errorf("%s: ActiveParent = %q, quiero vacío", name, got)
		}
	}
	// CRLF y BOM se toleran.
	if err := os.WriteFile(filepath.Join(meta, "active-parent"), []byte("\xef\xbb\xbf001-base.scad\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != "001-base.scad" {
		t.Errorf("CRLF/BOM: %q", got)
	}
}

func TestSetActiveParentRejects(t *testing.T) {
	dir := tree(t)
	if err := os.Mkdir(filepath.Join(dir, "lib.scad"), 0o755); err != nil { // carpeta con nombre de checkpoint
		t.Fatal(err)
	}
	for _, name := range []string{"009-no-existe.scad", "..", "../001-base.scad", filepath.Join(dir, "001-base.scad"), "sub/001-base.scad", "001-base.txt", "001\x00.scad", "lib.scad"} {
		if err := SetActiveParent(dir, name); err == nil {
			t.Errorf("SetActiveParent(%q): quiero error", name)
		}
	}
	if got := ActiveParent(dir); got != "" {
		t.Errorf("un rechazo no debe dejar activo: %q", got)
	}
}

func TestAdvance(t *testing.T) {
	dir := tree(t)
	// Sin activo, crear un hijo no fija nada: rige «el más nuevo».
	n1, err := Create(dir, "003-b.scad", "uno", "uno", "cube(2);")
	if err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != "" {
		t.Errorf("sin activo no debe aparecer uno: %q", got)
	}
	// Con activo = 003, crear su hijo lo mueve.
	if err := SetActiveParent(dir, "003-b.scad"); err != nil {
		t.Fatal(err)
	}
	n2, err := Create(dir, "003-b.scad", "dos", "dos", "cube(3);")
	if err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != n2 {
		t.Errorf("el activo debía avanzar a %s: %q", n2, got)
	}
	// Crear un hijo de OTRO checkpoint no lo mueve.
	if _, err := Create(dir, n1, "lateral", "lateral", "cube(4);"); err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != n2 {
		t.Errorf("un hijo de otro no mueve el activo: %q", got)
	}
	// Otro hijo del padre viejo (003) tampoco: el activo ya es n2.
	if _, err := Create(dir, "003-b.scad", "hermano", "hermano", "cube(5);"); err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != n2 {
		t.Errorf("un hermano no mueve el activo: %q", got)
	}
	// Advance es idempotente (el watcher de la app lo repite).
	if err := Advance(dir, "003-b.scad", n2); err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != n2 {
		t.Errorf("idempotencia: %q", got)
	}
	if err := Advance(dir, n2, n2); err != nil || ActiveParent(dir) != n2 {
		t.Errorf("parent == newFile no hace nada: %v %q", err, ActiveParent(dir))
	}
	if err := Advance(dir, "", n2); err != nil {
		t.Errorf("sin padre no hace nada: %v", err)
	}
}

func TestAdvanceWithAgentFile(t *testing.T) {
	// El agente escribe el archivo directo; el watcher llama a Advance.
	dir := tree(t)
	if err := SetActiveParent(dir, "004-lado.scad"); err != nil {
		t.Fatal(err)
	}
	mk(t, dir, "005-agente.scad", "004-lado.scad")
	if err := Advance(dir, "004-lado.scad", "005-agente.scad"); err != nil {
		t.Fatal(err)
	}
	if got := ActiveParent(dir); got != "005-agente.scad" {
		t.Errorf("activo: %q", got)
	}
	// Un hijo que no existe no puede ser el activo.
	if err := Advance(dir, "005-agente.scad", "006-fantasma.scad"); err == nil {
		t.Error("avanzar a un archivo inexistente debería fallar")
	}
	if got := ActiveParent(dir); got != "005-agente.scad" {
		t.Errorf("el fallo no debe mover el activo: %q", got)
	}
	for _, bad := range []string{"../x.scad", "", "x\x00.scad"} {
		if err := Advance(dir, "005-agente.scad", bad); err == nil {
			t.Errorf("Advance(newFile=%q): quiero error", bad)
		}
	}
}

func TestCreateDoesNotFailIfMetaIsBroken(t *testing.T) {
	skipNoSymlinks(t)
	dir := tree(t)
	if err := SetActiveParent(dir, "003-b.scad"); err != nil {
		t.Fatal(err)
	}
	// .scadlab pasa a ser un enlace hacia afuera: el checkpoint se crea igual.
	outside := t.TempDir()
	if err := os.Rename(filepath.Join(dir, ".scadlab"), filepath.Join(outside, "real")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "real"), filepath.Join(dir, ".scadlab")); err != nil {
		t.Fatal(err)
	}
	name, err := Create(dir, "003-b.scad", "hijo", "hijo", "cube(1);")
	if err != nil {
		t.Fatalf("Create no debe depender del activo: %v", err)
	}
	if read(t, filepath.Join(outside, "real", "active-parent")) != "003-b.scad\n" {
		t.Errorf("Create escribió fuera del workspace (activo con %s)", name)
	}
}

func TestSymlinkedMetaIsRefused(t *testing.T) {
	skipNoSymlinks(t)
	t.Run(".scadlab enlazado", func(t *testing.T) {
		dir := tree(t)
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(dir, ".scadlab")); err != nil {
			t.Fatal(err)
		}
		if err := SetActiveParent(dir, "001-base.scad"); err == nil {
			t.Error("SetActiveParent por un .scadlab enlazado debería fallar")
		}
		if _, err := Trash(dir, "004-lado.scad"); err == nil {
			t.Error("Trash por un .scadlab enlazado debería fallar")
		}
		if _, err := TrashBranch(dir, "002-a.scad"); err == nil {
			t.Error("TrashBranch por un .scadlab enlazado debería fallar")
		}
		if _, err := ListTrash(dir); err == nil {
			t.Error("ListTrash por un .scadlab enlazado debería fallar")
		}
		if _, err := Restore(dir, "20261015-100000-004-lado.scad"); err == nil {
			t.Error("Restore por un .scadlab enlazado debería fallar")
		}
		if ents, _ := os.ReadDir(outside); len(ents) != 0 {
			t.Errorf("se escribió fuera del workspace: %v", ents)
		}
		for _, n := range []string{"001-base.scad", "002-a.scad", "003-b.scad", "004-lado.scad"} {
			if !exists(dir, n) {
				t.Errorf("%s se movió a pesar del rechazo", n)
			}
		}
		if got := ActiveParent(dir); got != "" {
			t.Errorf("ActiveParent por un enlace: %q", got)
		}
	})
	t.Run("trash enlazada", func(t *testing.T) {
		dir := tree(t)
		outside := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, ".scadlab"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, ".scadlab", "trash")); err != nil {
			t.Fatal(err)
		}
		if _, err := Trash(dir, "004-lado.scad"); err == nil {
			t.Error("Trash hacia una papelera enlazada debería fallar")
		}
		if _, err := ListTrash(dir); err == nil {
			t.Error("ListTrash de una papelera enlazada debería fallar")
		}
		if ents, _ := os.ReadDir(outside); len(ents) != 0 {
			t.Errorf("se escribió fuera del workspace: %v", ents)
		}
		if !exists(dir, "004-lado.scad") {
			t.Error("el checkpoint se movió a pesar del rechazo")
		}
	})
	t.Run("active-parent enlazado", func(t *testing.T) {
		dir := tree(t)
		outside := filepath.Join(t.TempDir(), "otro")
		if err := os.WriteFile(outside, []byte("002-a.scad\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dir, ".scadlab"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, ".scadlab", "active-parent")); err != nil {
			t.Fatal(err)
		}
		if got := ActiveParent(dir); got != "" {
			t.Errorf("no se debe seguir un active-parent enlazado: %q", got)
		}
	})
}
