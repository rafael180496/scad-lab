package checkpoint

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fixNow fija la hora del borrado y la devuelve al terminar.
func fixNow(t *testing.T) time.Time {
	t.Helper()
	at := time.Date(2026, 10, 15, 10, 0, 0, 0, time.Local)
	now = func() time.Time { return at }
	t.Cleanup(func() { now = time.Now })
	return at
}

func TestTrashAndRestoreFreeName(t *testing.T) {
	at := fixNow(t)
	dir := tree(t)
	before := read(t, filepath.Join(dir, "004-lado.scad"))

	name, err := Trash(dir, "004-lado.scad")
	if err != nil {
		t.Fatal(err)
	}
	if name != "20261015-100000-004-lado.scad" {
		t.Fatalf("nombre en la papelera: %s", name)
	}
	if exists(dir, "004-lado.scad") {
		t.Error("el archivo sigue en el workspace")
	}
	if got := read(t, filepath.Join(dir, ".scadlab", "trash", name)); got != before {
		t.Errorf("el contenido cambió al mover: %q", got)
	}
	items, err := ListTrash(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []TrashedItem{{Name: name, Original: "004-lado.scad", Parent: "001-base.scad", Description: "desc de 004-lado.scad", TrashedAt: at.Unix()}}
	if !reflect.DeepEqual(items, want) {
		t.Errorf("ListTrash:\n%+v\nquiero\n%+v", items, want)
	}

	restored, err := Restore(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	if restored != "004-lado.scad" {
		t.Errorf("restaurado como %s", restored)
	}
	if got := read(t, filepath.Join(dir, restored)); got != before {
		t.Errorf("el encabezado/contenido cambió: %q", got)
	}
	if items, _ := ListTrash(dir); len(items) != 0 {
		t.Errorf("la papelera debería quedar vacía: %+v", items)
	}
}

func TestRestoreOccupiedName(t *testing.T) {
	fixNow(t)
	tests := []struct {
		name      string
		occupant  string // lo que ocupa el nombre original
		extra     []string
		want      string
		wantTrash string
	}{
		{"siguiente número", "004-lado.scad", nil, "005-lado.scad", ""},
		{"salta los usados", "004-lado.scad", []string{"005-otro.scad", "009-mas.scad"}, "010-lado.scad", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := tree(t)
			orig := read(t, filepath.Join(dir, "004-lado.scad"))
			trashed, err := Trash(dir, "004-lado.scad")
			if err != nil {
				t.Fatal(err)
			}
			// Mientras tanto alguien ocupó el nombre con OTRO contenido.
			write(t, dir, tc.occupant, "// del agente\n")
			for _, e := range tc.extra {
				write(t, dir, e, "x")
			}
			got, err := Restore(dir, trashed)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("restaurado como %s, quiero %s", got, tc.want)
			}
			if read(t, filepath.Join(dir, got)) != orig {
				t.Error("el encabezado no quedó intacto")
			}
			if read(t, filepath.Join(dir, tc.occupant)) != "// del agente\n" {
				t.Error("Restore pisó un checkpoint existente")
			}
			if items, _ := ListTrash(dir); len(items) != 0 {
				t.Errorf("la papelera debería quedar vacía: %+v", items)
			}
		})
	}
}

func TestRestoreNameWithoutNumber(t *testing.T) {
	fixNow(t)
	dir := t.TempDir()
	write(t, dir, "caja.scad", "// @checkpoint parent= desc=\"x\"\ncube(1);\n")
	trashed, err := Trash(dir, "caja.scad")
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "caja.scad", "otro")
	got, err := Restore(dir, trashed)
	if err != nil || got != "001-caja.scad" {
		t.Errorf("Restore = %q, %v", got, err)
	}
}

func TestTrashTwiceSameSecond(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	n1, err := Trash(dir, "004-lado.scad")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(dir, n1); err != nil {
		t.Fatal(err)
	}
	n2, err := Trash(dir, "004-lado.scad")
	if err != nil {
		t.Fatal(err)
	}
	if n1 != n2 { // al restaurar la papelera quedó libre: el nombre se reutiliza
		t.Fatalf("n1=%s n2=%s", n1, n2)
	}
	// Dos archivos con el mismo nombre en la papelera: no se pisa ninguno.
	write(t, dir, "004-lado.scad", "// segunda vida\n")
	n3, err := Trash(dir, "004-lado.scad")
	if err != nil {
		t.Fatal(err)
	}
	items, _ := ListTrash(dir)
	if len(items) != 2 || n3 == n2 || n3 != "20261015-100001-004-lado.scad" {
		t.Errorf("papelera: %+v (n2=%s n3=%s)", items, n2, n3)
	}
	if read(t, filepath.Join(dir, ".scadlab", "trash", n2)) == "// segunda vida\n" {
		t.Error("el segundo borrado pisó el primero")
	}
}

func TestListTrashOrderAndIgnored(t *testing.T) {
	dir := t.TempDir()
	trash := filepath.Join(dir, ".scadlab", "trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		t.Fatal(err)
	}
	if items, err := ListTrash(t.TempDir()); err != nil || items == nil || len(items) != 0 {
		t.Errorf("sin papelera: %v %v", items, err)
	}
	write(t, trash, "20260101-120000-001-a.scad", Header("", "vieja")+"\n")
	write(t, trash, "20261015-090000-002-b.scad", Header("001-a.scad", "nueva")+"\n")
	write(t, trash, "suelta.scad", "x")                // sin el formato: se ignora
	write(t, trash, "20261015-090000-.scad", "x")      // original vacío de nombre
	write(t, trash, "20261399-999999-003-c.scad", "x") // fecha imposible
	if err := os.Mkdir(filepath.Join(trash, "20260101-120000-carpeta.scad"), 0o755); err != nil {
		t.Fatal(err)
	}
	items, err := ListTrash(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, it := range items {
		names = append(names, it.Name)
	}
	if !reflect.DeepEqual(names, []string{"20261015-090000-002-b.scad", "20260101-120000-001-a.scad"}) {
		t.Errorf("orden/filtro: %v", names)
	}
	if items[0].Parent != "001-a.scad" || items[0].Description != "nueva" {
		t.Errorf("encabezado: %+v", items[0])
	}
}

func TestTrashHostileNames(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	outside := filepath.Join(t.TempDir(), "afuera.scad")
	write(t, filepath.Dir(outside), "afuera.scad", "secreto")
	hostile := []string{"", "..", ".", "../001-base.scad", outside, "sub/001-base.scad", `x\y.scad`, "001-base.txt", "001-base.scad\x00", "no-existe.scad"}
	for _, n := range hostile {
		if n == `x\y.scad` && filepath.Separator == '/' {
			continue // en macOS/Linux la barra invertida es un carácter válido
		}
		if _, err := Trash(dir, n); err == nil {
			t.Errorf("Trash(%q): quiero error", n)
		}
		if _, err := TrashBranch(dir, n); err == nil {
			t.Errorf("TrashBranch(%q): quiero error", n)
		}
		if n != "no-existe.scad" { // un nombre válido que no existe no es hostil para Descendants
			if _, err := Descendants(dir, n); err == nil {
				t.Errorf("Descendants(%q): quiero error", n)
			}
		}
	}
	hostileTrashed := []string{"", "..", "../20261015-100000-001-base.scad", outside, "20261015-100000-../x.scad", "sin-formato.scad", "20261015-100000-001-base.scad\x00", "20261015-100000-001-base.txt"}
	for _, n := range hostileTrashed {
		if _, err := Restore(dir, n); err == nil {
			t.Errorf("Restore(%q): quiero error", n)
		}
	}
	if read(t, outside) != "secreto" {
		t.Error("se tocó un archivo fuera del workspace")
	}
	if _, err := os.Stat(filepath.Join(dir, ".scadlab", "trash")); err == nil {
		if ents, _ := os.ReadDir(filepath.Join(dir, ".scadlab", "trash")); len(ents) != 0 {
			t.Errorf("algo cayó en la papelera: %v", ents)
		}
	}
}

func TestTrashDirectoryIsNotACheckpoint(t *testing.T) {
	dir := tree(t)
	if err := os.Mkdir(filepath.Join(dir, "carpeta.scad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Trash(dir, "carpeta.scad"); err == nil {
		t.Error("una carpeta no se manda a la papelera")
	}
}

func TestTrashCreatesMetaAndMovesActive(t *testing.T) {
	fixNow(t)
	tests := []struct {
		name   string
		active string
		trash  string
		branch bool
		want   string // activo después
	}{
		{"no era el activo", "003-b.scad", "004-lado.scad", false, "003-b.scad"},
		{"el activo pasa a su padre", "003-b.scad", "003-b.scad", false, "002-a.scad"},
		{"activo de una raíz se limpia", "001-base.scad", "001-base.scad", false, ""},
		{"rama con el activo dentro", "003-b.scad", "002-a.scad", true, "001-base.scad"},
		{"rama sin el activo", "004-lado.scad", "002-a.scad", true, "004-lado.scad"},
		{"rama desde la raíz limpia", "003-b.scad", "001-base.scad", true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := tree(t)
			if err := SetActiveParent(dir, tc.active); err != nil {
				t.Fatal(err)
			}
			var err error
			if tc.branch {
				_, err = TrashBranch(dir, tc.trash)
			} else {
				_, err = Trash(dir, tc.trash)
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := ActiveParent(dir); got != tc.want {
				t.Errorf("activo = %q, quiero %q", got, tc.want)
			}
		})
	}
}

func TestTrashCreatesScadlabDir(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	if _, err := os.Stat(filepath.Join(dir, ".scadlab")); err == nil {
		t.Fatal("no debería existir todavía")
	}
	if _, err := Trash(dir, "004-lado.scad"); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(filepath.Join(dir, ".scadlab", "trash")); err != nil || !st.IsDir() {
		t.Errorf("no se creó .scadlab/trash: %v", err)
	}
}

func TestTrashBranchWithGrandchildren(t *testing.T) {
	at := fixNow(t)
	dir := tree(t)
	mk(t, dir, "005-nieto.scad", "003-b.scad")   // 001 ← 002 ← 003 ← 005
	mk(t, dir, "006-hijo2.scad", "002-a.scad")   // 001 ← 002 ← 006
	mk(t, dir, "007-otro.scad", "004-lado.scad") // 001 ← 004 ← 007

	got, err := Descendants(dir, "002-a.scad")
	if err != nil || !reflect.DeepEqual(got, []string{"003-b.scad", "005-nieto.scad", "006-hijo2.scad"}) {
		t.Fatalf("Descendants(002) = %v, %v", got, err)
	}
	if got, _ := Descendants(dir, "005-nieto.scad"); got == nil || len(got) != 0 {
		t.Errorf("una hoja no tiene descendientes (y la lista no es nil): %#v", got)
	}

	if err := SetActiveParent(dir, "005-nieto.scad"); err != nil {
		t.Fatal(err)
	}
	names, err := TrashBranch(dir, "002-a.scad")
	if err != nil {
		t.Fatal(err)
	}
	stamp := at.Format(trashStamp) + "-"
	want := []string{stamp + "002-a.scad", stamp + "003-b.scad", stamp + "005-nieto.scad", stamp + "006-hijo2.scad"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("nombres:\n%v\nquiero\n%v", names, want)
	}
	for _, n := range []string{"002-a.scad", "003-b.scad", "005-nieto.scad", "006-hijo2.scad"} {
		if exists(dir, n) {
			t.Errorf("%s sigue en el workspace", n)
		}
	}
	for _, n := range []string{"001-base.scad", "004-lado.scad", "007-otro.scad"} {
		if !exists(dir, n) {
			t.Errorf("%s no debía tocarse", n)
		}
	}
	if got := ActiveParent(dir); got != "001-base.scad" {
		t.Errorf("el activo debía pasar al padre de la rama: %q", got)
	}
	if got, _ := Descendants(dir, "001-base.scad"); !reflect.DeepEqual(got, []string{"004-lado.scad", "007-otro.scad"}) {
		t.Errorf("Descendants(001) tras borrar la rama: %v", got)
	}

	// Restaurar la rama entera la reconecta: mismos nombres, mismos parent=.
	for _, n := range names {
		if _, err := Restore(dir, n); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := Descendants(dir, "001-base.scad"); len(got) != 6 {
		t.Errorf("tras restaurar: %v", got)
	}
}

func TestTrashSingleLeavesChildrenOrphan(t *testing.T) {
	fixNow(t)
	dir := tree(t)
	if _, err := Trash(dir, "002-a.scad"); err != nil {
		t.Fatal(err)
	}
	// 003 sigue ahí y sigue apuntando a 002: se puede listar como descendiente.
	if !exists(dir, "003-b.scad") {
		t.Fatal("el hijo no debía moverse")
	}
	if got, _ := Descendants(dir, "002-a.scad"); !reflect.DeepEqual(got, []string{"003-b.scad"}) {
		t.Errorf("Descendants de un archivo en la papelera: %v", got)
	}
}

func TestDescendantsCycleAndMissingFile(t *testing.T) {
	dir := t.TempDir()
	mk(t, dir, "001-a.scad", "002-b.scad")
	mk(t, dir, "002-b.scad", "001-a.scad")
	got, err := Descendants(dir, "001-a.scad")
	if err != nil || !reflect.DeepEqual(got, []string{"002-b.scad"}) {
		t.Errorf("ciclo: %v, %v", got, err)
	}
	// TrashBranch sobre un ciclo termina.
	fixNow(t)
	names, err := TrashBranch(dir, "001-a.scad")
	if err != nil || len(names) != 2 {
		t.Errorf("TrashBranch con ciclo: %v, %v", names, err)
	}
}

func TestTrashBranchMissingFile(t *testing.T) {
	dir := tree(t)
	names, err := TrashBranch(dir, "009-no-existe.scad")
	if err == nil || len(names) != 0 {
		t.Errorf("quiero error sin mover nada: %v %v", names, err)
	}
	if !strings.Contains(err.Error(), "009-no-existe.scad") {
		t.Errorf("el error debería nombrar el archivo: %v", err)
	}
	for _, n := range []string{"001-base.scad", "002-a.scad", "003-b.scad", "004-lado.scad"} {
		if !exists(dir, n) {
			t.Errorf("%s desapareció", n)
		}
	}
}

func TestMoveNoReplace(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a", "A")
	write(t, dir, "b", "B")
	if err := moveNoReplace(filepath.Join(dir, "a"), filepath.Join(dir, "b")); err == nil {
		t.Fatal("no debe pisar")
	}
	if read(t, filepath.Join(dir, "a")) != "A" || read(t, filepath.Join(dir, "b")) != "B" {
		t.Error("un movimiento rechazado no debe tocar nada")
	}
	if err := moveNoReplace(filepath.Join(dir, "a"), filepath.Join(dir, "c")); err != nil {
		t.Fatal(err)
	}
	if exists(dir, "a") || read(t, filepath.Join(dir, "c")) != "A" {
		t.Error("el movimiento no se hizo")
	}
}
