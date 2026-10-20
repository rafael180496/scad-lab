package skills

import (
	"io/fs"
	"path"
	"testing"

	"scadlab/assets"
)

// Todas las skills incluidas (las de modelado y las siete de marca de S14) tienen un
// frontmatter limpio: sin errores ni avisos (descripción demasiado larga, YAML
// dudoso…). Los agentes ignoran una skill con el frontmatter inválido en silencio.
func TestBuiltinSkillsHaveCleanFrontmatter(t *testing.T) {
	entries, err := fs.ReadDir(assets.FS, "skills")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, e := range entries {
		if !e.IsDir() || e.Name() == TemplateName {
			continue
		}
		raw, err := fs.ReadFile(assets.FS, path.Join("skills", e.Name(), "SKILL.md"))
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		seen++
		for _, d := range ValidateSkillMD(e.Name(), raw) {
			t.Errorf("%s: %s %s (línea %d)", e.Name(), d.Code, d.Message, d.Line)
		}
	}
	if seen < 12 {
		t.Errorf("hay %d skills incluidas, esperaba al menos 12 (5 de modelado + 7 de marca)", seen)
	}
}
