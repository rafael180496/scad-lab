package skills

import (
	"os"
	"path/filepath"
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
	if err := Sync(ws, list, func(n string) bool { return n != "openscad-libraries" }); err != nil {
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
