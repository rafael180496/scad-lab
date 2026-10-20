package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

// .agents/ (las skills de Codex y de Antigravity) no aporta checkpoints: el escaneo
// solo lee los .scad de la raíz, así que ni un .scad escondido ahí ni un hooks.json
// aparecen en el árbol.
func TestScanIgnoresTheAgentsFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "001-base.scad"), []byte("// @checkpoint parent= desc=\"base\"\ncube(1);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".agents", "skills", "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".agents/hooks.json", ".agents/skills/x/ejemplo.scad", ".agents/escondido.scad"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("cube(2);\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].FileName != "001-base.scad" {
		t.Errorf("solo la raíz: %+v", files)
	}
}
