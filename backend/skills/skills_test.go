package skills

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDiscoverBuiltinsExcludesTemplate(t *testing.T) {
	list, err := Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range list {
		names[s.Name] = true
		if s.Description == "" {
			t.Errorf("%s sin description en el frontmatter", s.Name)
		}
	}
	for _, want := range []string{"openscad-core", "bambu-lab-printers", "openscad-3mf-multicolor", "openscad-templates", "openscad-libraries"} {
		if !names[want] {
			t.Errorf("falta la skill incluida %s", want)
		}
	}
	if names[TemplateName] {
		t.Error("la plantilla no debe listarse como skill")
	}
}

func TestCreateFromTemplateAndSync(t *testing.T) {
	data := t.TempDir()
	dir, err := CreateFromTemplate(data, "gear-helper")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || !strings.Contains(string(raw), "name: gear-helper") {
		t.Fatalf("frontmatter sin el nombre nuevo: %v", err)
	}
	if _, err := CreateFromTemplate(data, "gear-helper"); err == nil {
		t.Error("crear dos veces la misma skill debería fallar")
	}
	if _, err := CreateFromTemplate(data, "Bad Name"); err == nil {
		t.Error("un nombre que no es kebab-case debería fallar")
	}

	ws := t.TempDir()
	// Una skill que el usuario dejó a mano en el workspace no se toca.
	manual := filepath.Join(ws, ".claude", "skills", "mine")
	if err := os.MkdirAll(manual, 0o755); err != nil {
		t.Fatal(err)
	}
	list, _ := Discover(data)
	if _, err := Sync(ws, list, func(n string) bool { return n != "openscad-libraries" }); err != nil {
		t.Fatal(err)
	}
	for _, rel := range AgentDirs {
		if _, err := os.Stat(filepath.Join(ws, rel, "openscad-core", "SKILL.md")); err != nil {
			t.Errorf("%s: falta openscad-core", rel)
		}
		if _, err := os.Stat(filepath.Join(ws, rel, "gear-helper", "SKILL.md")); err != nil {
			t.Errorf("%s: falta la skill del usuario", rel)
		}
		if _, err := os.Stat(filepath.Join(ws, rel, "openscad-libraries")); err == nil {
			t.Errorf("%s: se instaló una skill deshabilitada", rel)
		}
	}
	if _, err := os.Stat(manual); err != nil {
		t.Error("Sync borró una skill que no gestiona")
	}
}

// builtinSkill es una skill incluida real, para probar Sync sobre el embed.
func builtinSkill(t *testing.T, name string) Skill {
	t.Helper()
	list, err := Discover(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no hay una skill incluida %q", name)
	return Skill{}
}

// Apagar una skill retira su copia solo si nadie la editó (hash de la marca); si
// la editaron queda donde está y va en Kept. Una skill que el usuario dejó a mano
// (sin marca) no se mira, y una skill que sigue encendida se reescribe.
func TestSyncRemovesOnlyUntouchedCopiesOfSwitchedOffSkills(t *testing.T) {
	core := builtinSkill(t, "openscad-core")
	libs := builtinSkill(t, "openscad-libraries")
	multi := builtinSkill(t, "openscad-3mf-multicolor")
	list := []Skill{core, libs, multi}
	ws := t.TempDir()

	all := func(string) bool { return true }
	if res, err := Sync(ws, list, all); err != nil || len(res.Removed) != 0 || len(res.Kept) != 0 {
		t.Fatalf("instalar todo: %+v, %v", res, err)
	}
	// libs: editada a mano en UNA de las cuatro carpetas. multi: intacta.
	edited := filepath.Join(ws, ".opencode", "skills", "openscad-libraries", "SKILL.md")
	if err := os.WriteFile(edited, []byte("cambio mío\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Una skill ajena (sin marca) con el nombre de otra apagada no se toca.
	foreign := filepath.Join(ws, ".claude", "skills", "mine")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Sync(ws, list, func(n string) bool { return n == "openscad-core" })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Removed, []string{"openscad-3mf-multicolor", "openscad-libraries"}) {
		t.Errorf("Removed %v", res.Removed)
	}
	if !reflect.DeepEqual(res.Kept, []string{"openscad-libraries"}) {
		t.Errorf("Kept %v (solo la editada)", res.Kept)
	}
	for _, rel := range AgentDirs {
		if _, err := os.Stat(filepath.Join(ws, rel, "openscad-3mf-multicolor")); err == nil {
			t.Errorf("%s: la copia intacta debía retirarse", rel)
		}
		_, err := os.Stat(filepath.Join(ws, rel, "openscad-libraries"))
		if rel == filepath.Join(".opencode", "skills") && err != nil {
			t.Errorf("la copia editada tenía que quedar")
		}
		if rel != filepath.Join(".opencode", "skills") && err == nil {
			t.Errorf("%s: la copia sin editar debía retirarse", rel)
		}
		if _, err := os.Stat(filepath.Join(ws, rel, "openscad-core", "SKILL.md")); err != nil {
			t.Errorf("%s: la encendida tenía que seguir", rel)
		}
	}
	if raw, _ := os.ReadFile(edited); string(raw) != "cambio mío\n" {
		t.Errorf("la copia editada cambió: %q", raw)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Error("Sync tocó una skill que no gestiona")
	}

	// Repetir no cambia el resultado de lo ya retirado, y la editada se sigue avisando.
	res, err = Sync(ws, list, func(n string) bool { return n == "openscad-core" })
	if err != nil || len(res.Removed) != 0 || !reflect.DeepEqual(res.Kept, []string{"openscad-libraries"}) {
		t.Errorf("segunda pasada: %+v, %v", res, err)
	}
}

// Una marca de antes del hash (solo el origen) se compara con la skill original:
// la copia igual se retira y la distinta se conserva.
func TestSyncComparesOldMarkersWithTheOriginal(t *testing.T) {
	core := builtinSkill(t, "openscad-core")
	ws := t.TempDir()
	if _, err := Sync(ws, []Skill{core}, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	for _, rel := range AgentDirs {
		marker := filepath.Join(ws, rel, "openscad-core", managedMarker)
		if err := os.WriteFile(marker, []byte("builtin\n"), 0o644); err != nil { // marca vieja, sin hash
			t.Fatal(err)
		}
	}
	// Editada en una de las carpetas.
	if err := os.WriteFile(filepath.Join(ws, ".agent", "skills", "openscad-core", "extra.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(ws, []Skill{core}, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(res.Removed, []string{"openscad-core"}) || !reflect.DeepEqual(res.Kept, []string{"openscad-core"}) {
		t.Errorf("%+v", res)
	}
	for _, rel := range AgentDirs {
		_, err := os.Stat(filepath.Join(ws, rel, "openscad-core"))
		if edited := rel == filepath.Join(".agent", "skills"); edited != (err == nil) {
			t.Errorf("%s: existe=%v, la editada es la única que debía quedar", rel, err == nil)
		}
	}
}

// Lo mismo para una skill propia: instalada, apagada y sin tocar, se retira.
func TestSyncRemovesAnUntouchedUserSkill(t *testing.T) {
	data := t.TempDir()
	if _, err := CreateFromTemplate(data, "gear-helper"); err != nil {
		t.Fatal(err)
	}
	list, _ := Discover(data)
	var mine Skill
	for _, s := range list {
		if s.Name == "gear-helper" {
			mine = s
		}
	}
	ws := t.TempDir()
	if _, err := Sync(ws, []Skill{mine}, func(string) bool { return true }); err != nil {
		t.Fatal(err)
	}
	res, err := Sync(ws, []Skill{mine}, func(string) bool { return false })
	if err != nil || !reflect.DeepEqual(res.Removed, []string{"gear-helper"}) || len(res.Kept) != 0 {
		t.Errorf("%+v, %v", res, err)
	}
}
