package store

import "testing"

// La carpeta de skills propias solo cambia por su binding: el diálogo de
// Configuración guarda una copia vieja y la devolvería a la anterior.
func TestSaveSettingsDoesNotOverwriteSkillsDir(t *testing.T) {
	s := openTemp(t)
	if got, _ := s.GetSettings(); got.SkillsDir != "" {
		t.Fatalf("por defecto debe ser vacía (rige <datos>/skills): %q", got.SkillsDir)
	}
	if err := s.SetSetting("skillsDir", "/Volumes/Taller/skills"); err != nil {
		t.Fatal(err)
	}
	stale := DefaultSettings() // copia vieja: sin carpeta propia
	stale.Language = "es"
	if err := s.SaveSettings(stale); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings()
	if got.SkillsDir != "/Volumes/Taller/skills" {
		t.Errorf("SaveSettings pisó skillsDir: %q", got.SkillsDir)
	}
	if got.Language != "es" {
		t.Error("el resto de los ajustes sí se guarda")
	}
}
