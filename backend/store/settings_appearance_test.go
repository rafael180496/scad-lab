package store

import "testing"

// Una base anterior a la apariencia no tiene las claves: valen los defaults.
func TestAppearanceDefaults(t *testing.T) {
	s := openTemp(t)
	got, err := s.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.UIScale != 100 || got.EditorFontSize != 13 || got.TerminalFontSize != 13 || got.ChatFontSize != 13 {
		t.Errorf("defaults: %+v", got)
	}
	if got.Layout.WorkspacesHeight != DefaultLayout().WorkspacesHeight {
		t.Errorf("alto de workspaces: %d", got.Layout.WorkspacesHeight)
	}
}

func TestAppearanceNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   Appearance
		want Appearance
	}{
		{"cero = fábrica", Appearance{}, DefaultAppearance()},
		{"presets válidos", Appearance{UIScale: 150, EditorFontSize: 9, TerminalFontSize: 28, ChatFontSize: 16}, Appearance{UIScale: 150, EditorFontSize: 9, TerminalFontSize: 28, ChatFontSize: 16}},
		{"escala fuera de presets", Appearance{UIScale: 120, EditorFontSize: 13, TerminalFontSize: 13, ChatFontSize: 13}, DefaultAppearance()},
		{"escala negativa", Appearance{UIScale: -5}, DefaultAppearance()},
		{"letra demasiado chica", Appearance{UIScale: 90, EditorFontSize: 4, TerminalFontSize: 1, ChatFontSize: 8}, Appearance{UIScale: 90, EditorFontSize: 13, TerminalFontSize: 13, ChatFontSize: 13}},
		{"límites incluidos", Appearance{UIScale: 100, EditorFontSize: 9, TerminalFontSize: 28, ChatFontSize: 28}, Appearance{UIScale: 100, EditorFontSize: 9, TerminalFontSize: 28, ChatFontSize: 28}},
		{"letra demasiado grande", Appearance{UIScale: 130, EditorFontSize: 99, TerminalFontSize: 29, ChatFontSize: 100}, Appearance{UIScale: 130, EditorFontSize: 13, TerminalFontSize: 13, ChatFontSize: 13}},
	}
	for _, c := range cases {
		if got := c.in.Normalize(); got != c.want {
			t.Errorf("%s: %+v, se esperaba %+v", c.name, got, c.want)
		}
	}
}

// SaveSettings valida lo mismo que SaveAppearance, y SetSetting (que usa el
// binding) persiste cada clave por separado.
func TestAppearancePersists(t *testing.T) {
	s := openTemp(t)
	in := DefaultSettings()
	in.UIScale = 999
	in.TerminalFontSize = 3
	in.ChatFontSize = 200
	if err := s.SaveSettings(in); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings()
	if got.UIScale != 100 || got.TerminalFontSize != 13 || got.ChatFontSize != 13 {
		t.Errorf("SaveSettings no normalizó: %+v", got)
	}

	for k, v := range map[string]int{"uiScale": 130, "terminalFontSize": 16, "chatFontSize": 11, "editorFontSize": 20} {
		if err := s.SetSetting(k, v); err != nil {
			t.Fatal(err)
		}
	}
	got, _ = s.GetSettings()
	if got.UIScale != 130 || got.TerminalFontSize != 16 || got.ChatFontSize != 11 || got.EditorFontSize != 20 {
		t.Errorf("SetSetting no persistió: %+v", got)
	}
}

func TestLayoutNormalizeWorkspacesHeight(t *testing.T) {
	if got := (Layout{WorkspacesHeight: 5}).Normalize().WorkspacesHeight; got != MinWorkspacesHeight {
		t.Errorf("mínimo: %d", got)
	}
	if got := (Layout{WorkspacesHeight: 9999}).Normalize().WorkspacesHeight; got != MaxWorkspacesHeight {
		t.Errorf("máximo: %d", got)
	}
	// Un layout guardado antes de este campo lo trae en 0: toma el de fábrica.
	if got := (Layout{SidebarWidth: 300}).Normalize().WorkspacesHeight; got != DefaultLayout().WorkspacesHeight {
		t.Errorf("default: %d", got)
	}
}
