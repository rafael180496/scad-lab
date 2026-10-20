package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// install copia la skill en <ws>/.claude/skills como lo hace Sync y devuelve la
// ruta de la copia.
func install(t *testing.T, ws string, s Skill) string {
	t.Helper()
	if _, err := Sync(ws, []Skill{s}, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(ws, ".claude", "skills", s.Name)
}

func fixedNow(t *testing.T) {
	t.Helper()
	now = func() time.Time { return time.Date(2026, 10, 16, 9, 30, 0, 0, time.Local) }
	t.Cleanup(func() { now = time.Now })
}

func TestDeleteUserMovesToTrashAndRemovesOnlyUntouchedCopies(t *testing.T) {
	fixedNow(t)
	data := t.TempDir()
	userDir := filepath.Join(data, "skills")
	s := newUserSkill(t, userDir, "gear-helper")
	_ = os.MkdirAll(filepath.Join(s.Dir, "references"), 0o755)
	_ = os.WriteFile(filepath.Join(s.Dir, "references", "t.md"), []byte("tabla"), 0o644)

	ws1, ws2, ws3, ws4 := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	c1 := install(t, ws1, s)
	c2 := install(t, ws2, s)
	install(t, ws3, s)
	// ws2: alguien editó la copia a mano. ws3: la copia de otro agente también se retira.
	if err := os.WriteFile(filepath.Join(c2, "SKILL.md"), []byte("editado a mano"), 0o644); err != nil {
		t.Fatal(err)
	}
	// ws4: una carpeta con el mismo nombre que NO es de ScadLab (sin marca).
	mine := filepath.Join(ws4, ".claude", "skills", "gear-helper")
	_ = os.MkdirAll(mine, 0o755)
	_ = os.WriteFile(filepath.Join(mine, "SKILL.md"), []byte("mía"), 0o644)

	res, err := DeleteUser(userDir, data, "gear-helper", []string{ws1, ws2, ws3, ws4})
	if err != nil {
		t.Fatal(err)
	}
	// La carpeta ya no está y sí en la papelera de la app, entera.
	if _, err := os.Stat(s.Dir); err == nil {
		t.Error("la skill sigue en su carpeta")
	}
	wantTrash := filepath.Join(data, "trash", "skills", "20261016-093000-gear-helper")
	if res.Trash != wantTrash {
		t.Errorf("papelera: %s (quería %s)", res.Trash, wantTrash)
	}
	if b, err := os.ReadFile(filepath.Join(res.Trash, "references", "t.md")); err != nil || string(b) != "tabla" {
		t.Errorf("la papelera no conserva los archivos: %v", err)
	}
	// Copias: la sin tocar se va (en las 4 carpetas de agente del ws), la editada y la ajena se quedan.
	if _, err := os.Stat(c1); err == nil {
		t.Error("la copia sin tocar de ws1 debía retirarse")
	}
	for _, rel := range AgentDirs {
		if _, err := os.Stat(filepath.Join(ws1, rel, "gear-helper")); err == nil {
			t.Errorf("ws1/%s: quedó una copia", rel)
		}
	}
	if _, err := os.Stat(c2); err != nil {
		t.Error("la copia editada a mano no debía borrarse")
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("una carpeta que no es de ScadLab no debía tocarse")
	}
	// ws1 y ws3 con sus 4 carpetas de agente, más las 3 copias de ws2 que nadie editó.
	if res.Removed != 11 {
		t.Errorf("retiradas: %d", res.Removed)
	}
	// Solo la carpeta editada de ws2 difiere.
	if len(res.Kept) != 1 || res.Kept[0] != c2 {
		t.Errorf("conservadas: %v", res.Kept)
	}
}

// Una copia instalada por una versión anterior (marca sin hash) se compara con la original.
func TestPlanInstalledLegacyMarker(t *testing.T) {
	user := t.TempDir()
	s := newUserSkill(t, user, "gear-helper")
	ws := t.TempDir()
	c := install(t, ws, s)
	// Marca de antes: solo el origen.
	if err := os.WriteFile(filepath.Join(c, managedMarker), []byte("user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := PlanInstalled([]string{ws}, "gear-helper", s.Dir)
	if len(plan.Remove) != 4 || len(plan.Keep) != 0 {
		t.Fatalf("copia idéntica: %+v", plan)
	}
	_ = os.WriteFile(filepath.Join(c, "extra.md"), []byte("x"), 0o644)
	plan = PlanInstalled([]string{ws}, "gear-helper", s.Dir)
	if len(plan.Keep) != 1 || len(plan.Remove) != 3 {
		t.Fatalf("copia con un archivo de más: %+v", plan)
	}
	// Una copia que instaló la app desde una incluida no es de una skill propia.
	_ = os.WriteFile(filepath.Join(c, managedMarker), []byte("builtin\n"), 0o644)
	plan = PlanInstalled([]string{ws}, "gear-helper", s.Dir)
	if len(plan.Keep) != 0 || len(plan.Remove) != 3 {
		t.Fatalf("origen builtin: %+v", plan)
	}
}

// Editar la skill en la app y no reinstalarla en un workspace cerrado no cuenta como «editada a mano».
func TestStaleButUntouchedCopyIsRemovable(t *testing.T) {
	user := t.TempDir()
	s := newUserSkill(t, user, "gear-helper")
	ws := t.TempDir()
	install(t, ws, s)
	if err := WriteFile(s.Dir, "SKILL.md", "---\nname: gear-helper\ndescription: otra\n---\n"); err != nil {
		t.Fatal(err)
	}
	plan := PlanInstalled([]string{ws}, "gear-helper", s.Dir)
	if len(plan.Remove) != 4 || len(plan.Keep) != 0 {
		t.Fatalf("copia desactualizada pero sin tocar: %+v", plan)
	}
}

func TestDeleteUserRejectsBadNamesAndMissing(t *testing.T) {
	data := t.TempDir()
	userDir := filepath.Join(data, "skills")
	newUserSkill(t, userDir, "gear-helper")
	for _, n := range []string{"", "..", ".", "../skills", "a/b", "no-existe"} {
		if _, err := DeleteUser(userDir, data, n, nil); err == nil {
			t.Errorf("%q debería fallar", n)
		}
	}
	if _, err := os.Stat(filepath.Join(userDir, "gear-helper")); err != nil {
		t.Error("una petición inválida borró la skill")
	}
}

func TestRenameUser(t *testing.T) {
	user := t.TempDir()
	s := newUserSkill(t, user, "gear-helper")
	newUserSkill(t, user, "otra-skill")
	ws := t.TempDir()
	old := install(t, ws, s)

	for _, bad := range []string{"Gear Helper", "x", "otra-skill", "openscad-core", "gear-helper", "nul"} {
		if _, err := RenameUser(user, "gear-helper", bad, []string{ws}); err == nil {
			t.Errorf("renombrar a %q debería fallar", bad)
		}
	}
	if _, err := os.Stat(s.Dir); err != nil {
		t.Fatal("un rechazo cambió la carpeta")
	}
	if _, err := RenameUser(user, "no-existe", "nuevo-nombre", nil); err == nil {
		t.Error("renombrar una skill inexistente debería fallar")
	}

	res, err := RenameUser(user, "gear-helper", "engranajes", []string{ws})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Dir); err == nil {
		t.Error("la carpeta vieja sigue")
	}
	raw, err := os.ReadFile(filepath.Join(user, "engranajes", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	n, d := frontmatter(raw)
	if n != "engranajes" || !strings.Contains(d, "gear-helper") {
		t.Errorf("frontmatter tras renombrar: %q / %q", n, d)
	}
	if ds := ValidateSkillMD("engranajes", raw); HasErrors(ds) {
		t.Errorf("la skill renombrada no valida: %+v", ds)
	}
	if res.Removed != 4 || len(res.Kept) != 0 {
		t.Errorf("copias: %+v", res)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("la copia con el nombre viejo sigue en el workspace")
	}
}

func TestCheckFolder(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "skills")
	_ = os.MkdirAll(old, 0o755)
	file := filepath.Join(root, "archivo")
	_ = os.WriteFile(file, []byte("x"), 0o644)

	bad := map[string]string{
		"relativa":          "skills-nuevas",
		"vacía":             "",
		"la misma":          old,
		"la misma con /":    old + string(filepath.Separator),
		"dentro de ella":    filepath.Join(old, "sub"),
		"un archivo":        file,
		"la raíz del disco": string(filepath.Separator),
	}
	for name, dir := range bad {
		if _, err := CheckFolder(old, dir); err == nil {
			t.Errorf("%s (%q) debería rechazarse", name, dir)
		}
	}
	got, err := CheckFolder(old, filepath.Join(root, "otras", "..", "nuevas"))
	if err != nil || got != filepath.Join(root, "nuevas") {
		t.Errorf("carpeta válida: %q, %v", got, err)
	}
	// Un hermano con el mismo prefijo no está «dentro».
	if _, err := CheckFolder(old, old+"-bis"); err != nil {
		t.Errorf("skills-bis no está dentro de skills: %v", err)
	}
}

func TestMoveAllMovesSkillsAndSkipsConflicts(t *testing.T) {
	root := t.TempDir()
	oldDir, newDir := filepath.Join(root, "viejas"), filepath.Join(root, "nuevas")
	newUserSkill(t, oldDir, "gear-helper")
	newUserSkill(t, oldDir, "choca")
	_ = os.MkdirAll(filepath.Join(oldDir, "no-es-skill"), 0o755) // sin SKILL.md: no se toca
	_ = os.WriteFile(filepath.Join(oldDir, "suelto.txt"), []byte("x"), 0o644)
	newUserSkill(t, newDir, "choca") // ya hay una con ese nombre
	_ = os.WriteFile(filepath.Join(newDir, "choca", "marca.txt"), []byte("la de destino"), 0o644)

	res, err := MoveAll(oldDir, newDir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Moved, []string{"gear-helper"}) || !reflect.DeepEqual(res.Skipped, []string{"choca"}) {
		t.Fatalf("resultado: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(newDir, "gear-helper", "SKILL.md")); err != nil {
		t.Error("no llegó a la carpeta nueva")
	}
	if _, err := os.Stat(filepath.Join(oldDir, "gear-helper")); err == nil {
		t.Error("sigue en la vieja")
	}
	// El conflicto no pisa nada y la de origen queda donde estaba.
	if b, _ := os.ReadFile(filepath.Join(newDir, "choca", "marca.txt")); string(b) != "la de destino" {
		t.Error("se pisó la skill del destino")
	}
	if _, err := os.Stat(filepath.Join(oldDir, "choca", "SKILL.md")); err != nil {
		t.Error("la skill en conflicto debía quedarse en la carpeta vieja")
	}
	for _, keep := range []string{"no-es-skill", "suelto.txt"} {
		if _, err := os.Stat(filepath.Join(oldDir, keep)); err != nil {
			t.Errorf("%s no debía moverse", keep)
		}
	}
	// Sin carpeta vieja no hay nada que mover (y la nueva se crea).
	res, err = MoveAll(filepath.Join(root, "no-existe"), filepath.Join(root, "creada"))
	if err != nil || len(res.Moved) != 0 {
		t.Errorf("sin carpeta vieja: %+v, %v", res, err)
	}
	if st, err := os.Stat(filepath.Join(root, "creada")); err != nil || !st.IsDir() {
		t.Error("no se creó la carpeta nueva")
	}
}

// El respaldo de «copiar y borrar» (otro disco) conserva todo y no pisa.
func TestMovePathCopyFallbackKeepsContent(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	_ = os.MkdirAll(filepath.Join(src, "references"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("uno"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "references", "a.md"), []byte("dos"), 0o644)
	dst := filepath.Join(root, "dst")
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"SKILL.md": "uno", "references/a.md": "dos"} {
		if b, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(rel))); err != nil || string(b) != want {
			t.Errorf("%s: %q, %v", rel, b, err)
		}
	}
	if err := MovePath(src, dst); err == nil {
		t.Error("mover sobre un destino existente debería fallar")
	}
	if _, err := os.Stat(src); err != nil {
		t.Error("un movimiento rechazado no debe tocar el origen")
	}
}

func TestMoveToTrashDoesNotCollide(t *testing.T) {
	fixedNow(t)
	data := t.TempDir()
	var got []string
	for i := 0; i < 3; i++ {
		src := filepath.Join(t.TempDir(), "x")
		_ = os.MkdirAll(src, 0o755)
		dst, err := MoveToTrash(data, "skills", "gear-helper", src)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.Base(dst))
	}
	want := []string{"20261016-093000-gear-helper", "20261016-093001-gear-helper", "20261016-093002-gear-helper"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nombres: %v", got)
	}
}

func TestSyncMarkerRecordsInstalledHash(t *testing.T) {
	user := t.TempDir()
	s := newUserSkill(t, user, "gear-helper")
	c := install(t, t.TempDir(), s)
	src, hash := readMarker(c)
	cur, err := treeHash(c)
	if err != nil || src != "user" || hash == "" || hash != cur {
		t.Fatalf("marca: %q %q (hash real %q) %v", src, hash, cur, err)
	}
}
