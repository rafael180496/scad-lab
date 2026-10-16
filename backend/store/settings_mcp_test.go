package store

import "testing"

// El servidor MCP arranca encendido (también en una base anterior a S6, sin la
// clave), se apaga y se enciende con SetSetting, y SaveSettings —que manda la
// copia vieja del diálogo de Configuración— no lo pisa.
func TestMCPEnabledDefaultsOnAndOnlySetSettingChangesIt(t *testing.T) {
	s := openTemp(t)
	got, err := s.GetSettings()
	if err != nil || !got.MCPEnabled || !DefaultSettings().MCPEnabled {
		t.Fatalf("default: %+v, %v", got.MCPEnabled, err)
	}

	if err := s.SetSetting("mcpEnabled", false); err != nil {
		t.Fatal(err)
	}
	off, _ := s.GetSettings()
	if off.MCPEnabled {
		t.Fatal("apagar con SetSetting no persistió")
	}

	// Una copia vieja del diálogo (mcpEnabled=true) no lo vuelve a encender.
	stale := off
	stale.MCPEnabled = true
	stale.Theme = "light"
	if err := s.SaveSettings(stale); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetSettings()
	if again.MCPEnabled {
		t.Error("SaveSettings pisó mcpEnabled")
	}
	if again.Theme != "light" {
		t.Error("SaveSettings dejó de guardar el resto")
	}

	if err := s.SetSetting("mcpEnabled", true); err != nil {
		t.Fatal(err)
	}
	if on, _ := s.GetSettings(); !on.MCPEnabled {
		t.Error("encender con SetSetting no persistió")
	}
}
