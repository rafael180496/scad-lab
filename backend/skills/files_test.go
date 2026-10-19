package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// newUserSkill crea <userDir>/<name> con un SKILL.md válido y devuelve el Skill.
func newUserSkill(t *testing.T, userDir, name string) Skill {
	t.Helper()
	dir := filepath.Join(userDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "---\nname: " + name + "\ndescription: Úsala para probar " + name + "\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, MainFile), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	return Skill{Name: name, Source: "user", Dir: dir}
}

func TestCleanRelRejectsEscapes(t *testing.T) {
	bad := []string{
		"", "..", "../x", "a/../../x", "a/..", "/etc/passwd", `C:\Windows`, "C:/Windows", `a\b`, `..\x`,
		"a//b", "./a", "a/./b", "a/", "x:y", "a?b", "con\x00trol", "a/b ", "a.", managedMarker, "a/" + managedMarker,
		strings.Repeat("a/", 9) + "b", strings.Repeat("x", 300),
	}
	for _, rel := range bad {
		if got, err := CleanRel(rel); err == nil {
			t.Errorf("%q debería rechazarse (devolvió %q)", rel, got)
		}
	}
	for _, rel := range []string{"SKILL.md", "README.md", "references/tabla.md", "templates/a-b.scad", ".gitignore"} {
		if got, err := CleanRel(rel); err != nil || got != rel {
			t.Errorf("%q: %q, %v", rel, got, err)
		}
	}
}

func TestReadWriteConfinedToSkillFolder(t *testing.T) {
	user := t.TempDir()
	s := newUserSkill(t, user, "gear-helper")
	secret := filepath.Join(user, "secreto.txt") // fuera de la skill, en la carpeta de skills
	if err := os.WriteFile(secret, []byte("no tocar"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadFile(s, "SKILL.md")
	if err != nil || !strings.Contains(got, "gear-helper") {
		t.Fatalf("leer SKILL.md: %q, %v", got, err)
	}
	for _, rel := range []string{"../secreto.txt", "../../etc/passwd", "/etc/passwd", secret, "references/../../secreto.txt"} {
		if _, err := ReadFile(s, rel); err == nil {
			t.Errorf("leer %q debería fallar", rel)
		}
		if err := WriteFile(s.Dir, rel, "pisado"); err == nil {
			t.Errorf("escribir %q debería fallar", rel)
		}
		if err := CreateFile(s.Dir, rel); err == nil {
			t.Errorf("crear %q debería fallar", rel)
		}
	}
	if b, _ := os.ReadFile(secret); string(b) != "no tocar" {
		t.Fatalf("un archivo de fuera se modificó: %q", b)
	}

	// Escribir reemplaza, de forma atómica: no queda ningún temporal.
	if err := WriteFile(s.Dir, "SKILL.md", "nuevo contenido"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadFile(s, "SKILL.md"); got != "nuevo contenido" {
		t.Errorf("contenido: %q", got)
	}
	ents, _ := os.ReadDir(s.Dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".scadlab-save-") {
			t.Errorf("quedó un temporal: %s", e.Name())
		}
	}
	// Guardar no crea archivos: un nombre mal escrito es un error, no un archivo nuevo.
	if err := WriteFile(s.Dir, "SKILL.mdd", "x"); err == nil {
		t.Error("escribir un archivo inexistente debería fallar")
	}
	// Binarios y contenido que no es UTF-8.
	if err := os.WriteFile(filepath.Join(s.Dir, "bin.dat"), []byte{0, 1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(s, "bin.dat"); err == nil {
		t.Error("un binario no se abre en el editor")
	}
	if err := WriteFile(s.Dir, "SKILL.md", string([]byte{0xff, 0xfe})); err == nil {
		t.Error("texto no UTF-8 debería rechazarse")
	}
}

// Un enlace simbólico dentro de la skill que apunta fuera no deja salir.
func TestSymlinkCannotEscapeSkillFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("crear enlaces simbólicos requiere privilegios en Windows")
	}
	user := t.TempDir()
	s := newUserSkill(t, user, "gear-helper")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "fuera.md"), []byte("ajeno"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.Dir, "enlace")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "fuera.md"), filepath.Join(s.Dir, "atajo.md")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"enlace/fuera.md", "atajo.md", "enlace/nuevo.md"} {
		if _, err := ReadFile(s, rel); err == nil {
			t.Errorf("leer %q por un enlace debería fallar", rel)
		}
		if err := WriteFile(s.Dir, rel, "pisado"); err == nil {
			t.Errorf("escribir %q por un enlace debería fallar", rel)
		}
		if err := CreateFile(s.Dir, rel); err == nil && rel == "enlace/nuevo.md" {
			t.Errorf("crear %q por un enlace debería fallar", rel)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(outside, "fuera.md")); string(b) != "ajeno" {
		t.Errorf("el archivo de fuera cambió: %q", b)
	}
	if _, err := os.Stat(filepath.Join(outside, "nuevo.md")); err == nil {
		t.Error("se creó un archivo fuera de la skill")
	}
	// Los enlaces no se listan.
	files, err := ListFiles(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Path, "enlace") || f.Path == "atajo.md" {
			t.Errorf("se listó un enlace: %s", f.Path)
		}
	}
}

func TestListFilesOrderAndKinds(t *testing.T) {
	s := newUserSkill(t, t.TempDir(), "gear-helper")
	for rel, body := range map[string]string{"README.md": "# hola", "references/zeta.md": "z", "references/Alfa.md": "a", ".DS_Store": "x"} {
		p := filepath.Join(s.Dir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(filepath.Join(s.Dir, "logo.png"), []byte{0x89, 'P', 'N', 'G', 0, 0}, 0o644)
	files, err := ListFiles(s)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	text := map[string]bool{}
	for _, f := range files {
		names = append(names, f.Path)
		text[f.Path] = f.Text
	}
	want := "SKILL.md,logo.png,README.md,references/Alfa.md,references/zeta.md"
	if strings.Join(names, ",") != want {
		t.Errorf("orden: %v", names)
	}
	if !text["SKILL.md"] || text["logo.png"] {
		t.Errorf("Text: %v", text)
	}
}

func TestBuiltinSkillsAreReadableNotWritable(t *testing.T) {
	list, err := Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var core Skill
	for _, s := range list {
		if s.Name == "openscad-core" {
			core = s
		}
	}
	if core.Name == "" {
		t.Fatal("falta openscad-core")
	}
	files, err := ListFiles(core)
	if err != nil || len(files) == 0 || files[0].Path != MainFile {
		t.Fatalf("archivos: %+v, %v", files, err)
	}
	if md, err := ReadFile(core, "SKILL.md"); err != nil || !strings.Contains(md, "openscad-core") {
		t.Errorf("leer SKILL.md incluida: %v", err)
	}
	if _, err := ReadFile(core, "../openscad-libraries/SKILL.md"); err == nil {
		t.Error("una skill incluida no puede leer a otra con ..")
	}
}

func TestCreateRenameDeleteFiles(t *testing.T) {
	data := t.TempDir()
	s := newUserSkill(t, filepath.Join(data, "skills"), "gear-helper")

	if err := CreateFile(s.Dir, "references/tabla.md"); err != nil {
		t.Fatal(err)
	}
	if err := CreateFile(s.Dir, "references/tabla.md"); err == nil {
		t.Error("crear dos veces el mismo archivo debería fallar")
	}
	if err := WriteFile(s.Dir, "references/tabla.md", "| a | b |"); err != nil {
		t.Fatal(err)
	}

	// Renombrar y mover.
	if err := RenameFile(s.Dir, "references/tabla.md", "references/medidas.md"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadFile(s, "references/medidas.md"); got != "| a | b |" {
		t.Errorf("tras renombrar: %q", got)
	}
	if err := RenameFile(s.Dir, "references/medidas.md", "templates/medidas.md"); err != nil {
		t.Fatal(err)
	}
	// No pisa, no sale de la carpeta, no toca SKILL.md.
	_ = CreateFile(s.Dir, "otro.md")
	if err := RenameFile(s.Dir, "otro.md", "templates/medidas.md"); err == nil {
		t.Error("renombrar sobre un archivo existente debería fallar")
	}
	if err := RenameFile(s.Dir, "otro.md", "../fuera.md"); err == nil {
		t.Error("renombrar fuera de la skill debería fallar")
	}
	if err := RenameFile(s.Dir, "SKILL.md", "LEEME.md"); err == nil {
		t.Error("SKILL.md no se renombra")
	}
	if err := RenameFile(s.Dir, "otro.md", "skill.md"); err == nil {
		t.Error("nada se renombra a SKILL.md")
	}
	if _, err := os.Stat(filepath.Join(data, "fuera.md")); err == nil {
		t.Error("se creó un archivo fuera de la skill")
	}
	// Cambiar solo mayúsculas es renombrar, no «ya existe».
	if err := RenameFile(s.Dir, "otro.md", "Otro.md"); err != nil {
		t.Errorf("cambio de mayúsculas: %v", err)
	}

	// Borrar = a la papelera de la app, recuperable.
	now = func() time.Time { return time.Date(2026, 10, 16, 9, 30, 0, 0, time.Local) }
	t.Cleanup(func() { now = time.Now })
	trash, err := DeleteFile(s.Dir, data, "gear-helper", "templates/medidas.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "templates", "medidas.md")); err == nil {
		t.Error("el archivo sigue en la skill")
	}
	if b, err := os.ReadFile(trash); err != nil || string(b) != "| a | b |" {
		t.Errorf("papelera %s: %q, %v", trash, b, err)
	}
	if !strings.HasPrefix(trash, TrashDir(data, "skill-files")) || !strings.Contains(trash, "20261016-093000-gear-helper-templates__medidas.md") {
		t.Errorf("ruta en la papelera: %s", trash)
	}
	if _, err := DeleteFile(s.Dir, data, "gear-helper", "SKILL.md"); err == nil {
		t.Error("SKILL.md no se borra")
	}
	if _, err := DeleteFile(s.Dir, data, "gear-helper", "../x"); err == nil {
		t.Error("borrar fuera de la skill debería fallar")
	}
}
