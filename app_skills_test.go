package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/skills"
)

// noOpen reemplaza openFolder: ninguna prueba abre el Finder del usuario.
func noOpen(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	old := openFolder
	openFolder = func(p string) error {
		calls = append(calls, p)
		return nil
	}
	t.Cleanup(func() { openFolder = old })
	return &calls
}

const goodSkillMD = "---\nname: %s\ndescription: Úsala para probar el editor de skills\n---\n# Skill\n"

func skillMD(name string) string { return strings.Replace(goodSkillMD, "%s", name, 1) }

// El botón de carpeta: con la carpeta de skills sin crear (nadie hizo una skill
// todavía) la crea y abre ESA carpeta. Antes se le pasaba al sistema una ruta
// inexistente, `open` se negaba y nadie veía el error.
func TestOpenSkillsFolderCreatesMissingFolder(t *testing.T) {
	calls := noOpen(t)
	a := newTestApp(t)
	want := filepath.Join(a.dataDir, "skills")
	if _, err := os.Stat(want); err == nil {
		t.Fatal("el caso de prueba necesita la carpeta sin crear")
	}
	if err := a.OpenSkillsFolder(); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(want); err != nil || !st.IsDir() {
		t.Fatalf("la carpeta no se creó: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0] != want {
		t.Errorf("abrió %v, quería [%s]", *calls, want)
	}
}

// Con una carpeta elegida por el usuario abre esa, y un fallo del sistema llega
// a la interfaz como error (no se traga).
func TestOpenSkillsFolderUsesChosenFolderAndReportsFailure(t *testing.T) {
	calls := noOpen(t)
	a := newTestApp(t)
	chosen := filepath.Join(t.TempDir(), "mis", "skills")
	if _, err := a.ChangeSkillsFolder(chosen, false); err != nil {
		t.Fatal(err)
	}
	if err := a.OpenSkillsFolder(); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 1 || (*calls)[0] != chosen {
		t.Errorf("abrió %v, quería [%s]", *calls, chosen)
	}
	openFolder = func(string) error { return errors.New("sin gestor de archivos") }
	if err := a.OpenSkillsFolder(); err == nil || !strings.Contains(err.Error(), "sin gestor") {
		t.Errorf("el error del sistema debe llegar: %v", err)
	}
	// No se pudo crear la carpeta (su padre es un archivo): error claro, no un silencio.
	file := filepath.Join(t.TempDir(), "archivo")
	_ = os.WriteFile(file, []byte("x"), 0o644)
	if err := a.store.SetSetting("skillsDir", filepath.Join(file, "skills")); err != nil {
		t.Fatal(err)
	}
	openFolder = func(string) error { return nil }
	if err := a.OpenSkillsFolder(); err == nil {
		t.Error("crear la carpeta bajo un archivo debería fallar")
	}
}

func TestChangeSkillsFolderMoveAndPointOnly(t *testing.T) {
	noOpen(t)
	a := newTestApp(t)
	if _, err := a.CreateSkill("gear-helper"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateSkill("hinge-kit"); err != nil {
		t.Fatal(err)
	}
	def := filepath.Join(a.dataDir, "skills")
	f, err := a.GetSkillsFolder()
	if err != nil || f.Path != def || f.Custom || f.Count != 2 || !f.Exists {
		t.Fatalf("carpeta inicial: %+v, %v", f, err)
	}

	// Mover: las dos skills viajan y la elección se guarda.
	dest := filepath.Join(t.TempDir(), "Taller", "skills")
	res, err := a.ChangeSkillsFolder(dest, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Moved) != 2 || len(res.Skipped) != 0 || res.Path != dest {
		t.Errorf("mover: %+v", res)
	}
	for _, n := range []string{"gear-helper", "hinge-kit"} {
		if _, err := os.Stat(filepath.Join(dest, n, "SKILL.md")); err != nil {
			t.Errorf("%s no llegó", n)
		}
		if _, err := os.Stat(filepath.Join(def, n)); err == nil {
			t.Errorf("%s sigue en la carpeta vieja", n)
		}
	}
	if s, _ := a.store.GetSettings(); s.SkillsDir != dest {
		t.Errorf("no se guardó: %q", s.SkillsDir)
	}
	list, err := a.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	if n := countUser(list); n != 2 {
		t.Errorf("tras mover se ven %d propias", n)
	}
	if f, _ := a.GetSkillsFolder(); !f.Custom || f.Path != dest || f.Count != 2 {
		t.Errorf("GetSkillsFolder: %+v", f)
	}
	// Guardar Configuración con una copia vieja no deshace el cambio.
	stale, _ := a.GetSettings()
	stale.SkillsDir = ""
	if err := a.SaveSettings(stale); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.store.GetSettings(); s.SkillsDir != dest {
		t.Errorf("SaveSettings pisó la carpeta: %q", s.SkillsDir)
	}

	// Solo apuntar: las de la carpeta anterior se quedan donde están y dejan de verse.
	other := filepath.Join(t.TempDir(), "vacia")
	res, err = a.ChangeSkillsFolder(other, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Moved) != 0 {
		t.Errorf("sin mover no se mueve nada: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dest, "gear-helper", "SKILL.md")); err != nil {
		t.Error("apuntar a otra carpeta no debe tocar la anterior")
	}
	list, _ = a.ListSkills()
	if n := countUser(list); n != 0 {
		t.Errorf("apuntando a una carpeta vacía se ven %d propias", n)
	}

	// Rechazos: relativa, dentro de la actual, la misma.
	for _, bad := range []string{"relativa/skills", filepath.Join(other, "sub"), other} {
		if _, err := a.ChangeSkillsFolder(bad, true); err == nil {
			t.Errorf("%q debería rechazarse", bad)
		}
	}
	// Volver a la predeterminada ("" = <datos>/skills), moviendo las de la carpeta actual (vacía).
	res, err = a.ChangeSkillsFolder("", true)
	if err != nil || res.Path != def {
		t.Fatalf("volver a la predeterminada: %+v, %v", res, err)
	}
	if s, _ := a.store.GetSettings(); s.SkillsDir != "" {
		t.Errorf("la predeterminada se guarda como vacío: %q", s.SkillsDir)
	}
}

func countUser(list []SkillView) int {
	n := 0
	for _, s := range list {
		if s.Source == "user" {
			n++
		}
	}
	return n
}

// Un nombre repetido en la carpeta nueva no se pisa: se informa.
func TestChangeSkillsFolderReportsConflicts(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.CreateSkill("gear-helper"); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "skills")
	if _, err := skills.CreateFromTemplate(dest, "gear-helper"); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dest, "gear-helper", "marca.txt"), []byte("destino"), 0o644)
	res, err := a.ChangeSkillsFolder(dest, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != "gear-helper" || len(res.Moved) != 0 {
		t.Errorf("conflicto: %+v", res)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "gear-helper", "marca.txt")); string(b) != "destino" {
		t.Error("se pisó la skill del destino")
	}
}

// Un disco externo desmontado no hace olvidar qué skills estaban apagadas.
func TestUnavailableSkillsFolderKeepsSkillState(t *testing.T) {
	a := newTestApp(t)
	disk := filepath.Join(t.TempDir(), "disco")
	dir := filepath.Join(disk, "skills")
	if _, err := a.ChangeSkillsFolder(dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateSkill("gear-helper"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSkillEnabled("gear-helper", false); err != nil {
		t.Fatal(err)
	}
	moved := disk + "-desmontado"
	if err := os.Rename(disk, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListSkills(); err != nil { // con la carpeta ausente
		t.Fatal(err)
	}
	if err := os.Rename(moved, disk); err != nil {
		t.Fatal(err)
	}
	list, err := a.ListSkills()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Name == "gear-helper" && s.Enabled {
			t.Error("la skill volvió encendida: se perdió su estado")
		}
	}
}

// installedAt devuelve la copia de la skill en un workspace (carpeta de Claude Code).
func installedAt(w string, name string) string { return filepath.Join(w, ".claude", "skills", name) }

func TestDeleteSkillFlow(t *testing.T) {
	a := newTestApp(t)
	dir, err := a.CreateSkill("gear-helper")
	if err != nil {
		t.Fatal(err)
	}
	// Se guarda con una descripción de verdad para que se instale en los workspaces.
	if r, err := a.SaveSkillFile("gear-helper", "SKILL.md", skillMD("gear-helper")); err != nil || !r.Saved {
		t.Fatalf("guardar: %+v, %v", r, err)
	}
	w1, err := a.CreateWorkspace("Uno", "bambu-a1")
	if err != nil {
		t.Fatal(err)
	}
	w2, err := a.CreateWorkspace("Dos", "bambu-a1")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{w1.Path, w2.Path} {
		if _, err := os.Stat(installedAt(w, "gear-helper")); err != nil {
			t.Fatalf("la skill no se instaló en %s: %v", w, err)
		}
	}
	// Alguien editó a mano la copia del segundo workspace.
	edited := filepath.Join(installedAt(w2.Path, "gear-helper"), "SKILL.md")
	if err := os.WriteFile(edited, []byte("retocada a mano"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := a.DeleteSkill("gear-helper")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("la carpeta de la skill sigue")
	}
	if !strings.HasPrefix(res.Trash, filepath.Join(a.dataDir, "trash", "skills")) {
		t.Errorf("papelera fuera de los datos de la app: %s", res.Trash)
	}
	if b, err := os.ReadFile(filepath.Join(res.Trash, "SKILL.md")); err != nil || !strings.Contains(string(b), "gear-helper") {
		t.Errorf("la papelera no tiene el SKILL.md: %v", err)
	}
	if _, err := os.Stat(installedAt(w1.Path, "gear-helper")); err == nil {
		t.Error("la copia sin tocar del primer workspace debía retirarse")
	}
	if b, err := os.ReadFile(edited); err != nil || string(b) != "retocada a mano" {
		t.Errorf("la copia editada debía quedar intacta: %v", err)
	}
	if len(res.Kept) != 1 || res.Kept[0] != installedAt(w2.Path, "gear-helper") {
		t.Errorf("debía avisar de la copia editada: %+v", res.Kept)
	}
	list, _ := a.ListSkills()
	for _, s := range list {
		if s.Name == "gear-helper" {
			t.Error("la skill borrada sigue en la lista")
		}
	}
}

func TestDeleteAndEditRefuseBuiltins(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.DeleteSkill("openscad-core"); err == nil {
		t.Error("las incluidas no se borran")
	}
	if _, err := a.RenameSkill("openscad-core", "otro-nombre"); err == nil {
		t.Error("las incluidas no se renombran")
	}
	if _, err := a.SaveSkillFile("openscad-core", "SKILL.md", skillMD("openscad-core")); err == nil {
		t.Error("las incluidas no se escriben")
	}
	if err := a.CreateSkillFile("openscad-core", "nuevo.md"); err == nil {
		t.Error("las incluidas no admiten archivos nuevos")
	}
	if _, err := a.DeleteSkillFile("openscad-core", "README.md"); err == nil {
		t.Error("las incluidas no pierden archivos")
	}
	// Pero se pueden ver.
	files, err := a.ListSkillFiles("openscad-core")
	if err != nil || len(files) == 0 || files[0].Path != "SKILL.md" {
		t.Fatalf("listar incluida: %+v, %v", files, err)
	}
	if md, err := a.ReadSkillFile("openscad-core", "SKILL.md"); err != nil || !strings.Contains(md, "openscad-core") {
		t.Errorf("leer incluida: %v", err)
	}
	if _, err := a.DeleteSkill("no-existe"); err == nil {
		t.Error("borrar una skill inexistente debería fallar")
	}
}

func TestRenameSkillKeepsStateAndFixesFrontmatter(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.CreateSkill("gear-helper"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateSkill("otra-skill"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSkillEnabled("gear-helper", false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"Gear Helper", "otra-skill", "openscad-core", ""} {
		if _, err := a.RenameSkill("gear-helper", bad); err == nil {
			t.Errorf("renombrar a %q debería fallar", bad)
		}
	}
	res, err := a.RenameSkill("gear-helper", "engranajes")
	if err != nil || res.Name != "engranajes" {
		t.Fatalf("renombrar: %+v, %v", res, err)
	}
	md, err := a.ReadSkillFile("engranajes", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if ds := a.ValidateSkillMarkdown("engranajes", md); skills.HasErrors(ds) {
		t.Errorf("tras renombrar el frontmatter no valida: %+v", ds)
	}
	list, _ := a.ListSkills()
	found := false
	for _, s := range list {
		if s.Name == "gear-helper" {
			t.Error("el nombre viejo sigue")
		}
		if s.Name == "engranajes" {
			found = true
			if s.Enabled {
				t.Error("estaba apagada y el renombrado la encendió")
			}
		}
	}
	if !found {
		t.Error("no aparece con el nombre nuevo")
	}
}

func TestSaveSkillFileValidatesAndSyncsOpenProjects(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.CreateSkill("gear-helper"); err != nil {
		t.Fatal(err)
	}
	w, err := a.CreateWorkspace("Uno", "bambu-a1")
	if err != nil {
		t.Fatal(err)
	}
	closed, err := a.CreateWorkspace("Cerrado", "bambu-a1")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.SetOpenProjects([]int64{w.ID}, w.ID); err != nil { // solo «Uno» está abierto
		t.Fatal(err)
	}

	// Frontmatter roto: no se escribe, y dice qué está mal.
	before, _ := a.ReadSkillFile("gear-helper", "SKILL.md")
	r, err := a.SaveSkillFile("gear-helper", "SKILL.md", "---\nname: otra-cosa\ndescription:\n---\nx")
	if err != nil || r.Saved {
		t.Fatalf("un frontmatter con errores no debe guardarse: %+v, %v", r, err)
	}
	codes := map[string]bool{}
	for _, d := range r.Diagnostics {
		codes[d.Code] = true
	}
	if !codes["SK005"] || !codes["SK006"] {
		t.Errorf("diagnósticos: %+v", r.Diagnostics)
	}
	if after, _ := a.ReadSkillFile("gear-helper", "SKILL.md"); after != before {
		t.Error("se escribió un SKILL.md inválido")
	}

	// Válido: se guarda, la descripción nueva se ve en la lista y el proyecto abierto la recibe.
	r, err = a.SaveSkillFile("gear-helper", "SKILL.md", skillMD("gear-helper"))
	if err != nil || !r.Saved || r.SyncError != "" {
		t.Fatalf("guardar: %+v, %v", r, err)
	}
	list, _ := a.ListSkills()
	for _, s := range list {
		if s.Name == "gear-helper" && s.Description != "Úsala para probar el editor de skills" {
			t.Errorf("la lista muestra %q", s.Description)
		}
	}
	got, err := os.ReadFile(filepath.Join(installedAt(w.Path, "gear-helper"), "SKILL.md"))
	if err != nil || !strings.Contains(string(got), "probar el editor") {
		t.Errorf("el proyecto abierto no recibió la skill: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(installedAt(closed.Path, "gear-helper"), "SKILL.md")); err == nil && strings.Contains(string(b), "probar el editor") {
		t.Error("un proyecto cerrado no se sincroniza al guardar (lo hará al abrirse)")
	}

	// Otro archivo: crear, guardar, renombrar y borrar también llegan al proyecto abierto.
	if err := a.CreateSkillFile("gear-helper", "references/tabla.md"); err != nil {
		t.Fatal(err)
	}
	if r, err := a.SaveSkillFile("gear-helper", "references/tabla.md", "| a |"); err != nil || !r.Saved {
		t.Fatalf("guardar referencia: %+v, %v", r, err)
	}
	if _, err := os.Stat(filepath.Join(installedAt(w.Path, "gear-helper"), "references", "tabla.md")); err != nil {
		t.Errorf("la referencia no llegó al proyecto: %v", err)
	}
	if _, err := a.RenameSkillFile("gear-helper", "references/tabla.md", "references/medidas.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(installedAt(w.Path, "gear-helper"), "references", "medidas.md")); err != nil {
		t.Errorf("el renombrado no llegó al proyecto: %v", err)
	}
	trash, err := a.DeleteSkillFile("gear-helper", "references/medidas.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(trash); err != nil {
		t.Errorf("el archivo borrado no está en la papelera: %v", err)
	}
	if _, err := os.Stat(filepath.Join(installedAt(w.Path, "gear-helper"), "references", "medidas.md")); err == nil {
		t.Error("el borrado no llegó al proyecto")
	}
}

// Las rutas del editor no salen de la carpeta de la skill.
func TestSkillFileBindingsRejectTraversal(t *testing.T) {
	a := newTestApp(t)
	if _, err := a.CreateSkill("gear-helper"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateSkill("hinge-kit"); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(a.dataDir, "scadlab.db")
	for _, rel := range []string{"../hinge-kit/SKILL.md", "../../scadlab.db", outside, "/etc/hosts", `..\hinge-kit\SKILL.md`} {
		if _, err := a.ReadSkillFile("gear-helper", rel); err == nil {
			t.Errorf("leer %q debería fallar", rel)
		}
		if r, err := a.SaveSkillFile("gear-helper", rel, "x"); err == nil || r.Saved {
			t.Errorf("guardar %q debería fallar", rel)
		}
		if err := a.CreateSkillFile("gear-helper", rel); err == nil {
			t.Errorf("crear %q debería fallar", rel)
		}
		if _, err := a.RenameSkillFile("gear-helper", "SKILL.md", rel); err == nil {
			t.Errorf("renombrar a %q debería fallar", rel)
		}
		if _, err := a.DeleteSkillFile("gear-helper", rel); err == nil {
			t.Errorf("borrar %q debería fallar", rel)
		}
	}
	// Un nombre de skill con «..» tampoco sirve para salir.
	for _, name := range []string{"..", "../skills/gear-helper", "gear-helper/.."} {
		if _, err := a.ReadSkillFile(name, "SKILL.md"); err == nil {
			t.Errorf("la skill %q no existe", name)
		}
	}
	if md, _ := a.ReadSkillFile("hinge-kit", "SKILL.md"); !strings.Contains(md, "hinge-kit") {
		t.Error("la otra skill se modificó")
	}
}

// Una skill nueva: SKILL.md con el nombre elegido, sin la descripción de la plantilla.
func TestCreateSkillDoesNotOpenFolderNorKeepTemplateText(t *testing.T) {
	calls := noOpen(t)
	a := newTestApp(t)
	dir, err := a.CreateSkill("gear-helper")
	if err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 0 {
		t.Errorf("crear una skill no debe abrir el Finder: %v", *calls)
	}
	md, err := a.ReadSkillFile("gear-helper", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "name: gear-helper") || strings.Contains(md, "no es una skill operativa") {
		t.Errorf("SKILL.md nuevo: %.200s", md)
	}
	list, _ := a.ListSkills()
	for _, s := range list {
		if s.Name == "gear-helper" && (s.Description != skills.DescriptionPlaceholder || s.Dir != dir) {
			t.Errorf("descripción en la lista: %q", s.Description)
		}
	}
	if _, err := a.CreateSkill("gear-helper"); err == nil {
		t.Error("crear dos veces la misma debería fallar")
	}
}
