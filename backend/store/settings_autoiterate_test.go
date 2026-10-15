package store

import "testing"

// La auto-iteración arranca encendida, también en una base anterior a S5 (que
// no tiene la clave), y apagarla tiene que persistir aunque false sea el valor
// cero del campo.
func TestAutoIterateDefaultsOnAndPersistsOff(t *testing.T) {
	s := openTemp(t)
	got, err := s.GetSettings()
	if err != nil || !got.AutoIterate || !DefaultSettings().AutoIterate {
		t.Fatalf("default: %+v, %v", got.AutoIterate, err)
	}

	got.AutoIterate = false
	if err := s.SaveSettings(got); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetSettings(); again.AutoIterate {
		t.Error("apagar la auto-iteración con SaveSettings no persistió")
	}

	// El interruptor de la terminal usa SetSetting, sin tocar el resto.
	if err := s.SetSetting("autoIterate", true); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetSettings(); !again.AutoIterate {
		t.Error("encender con SetSetting no persistió")
	}
}
