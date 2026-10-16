package store

import (
	"reflect"
	"testing"
)

// Una base anterior a S8 no tiene las claves: ningún proyecto abierto, y la
// lista es [] (no null) para que la barra de pestañas la lea sin casos
// especiales.
func TestOpenProjectsDefaultEmpty(t *testing.T) {
	s := openTemp(t)
	got, err := s.GetSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.OpenProjects == nil || len(got.OpenProjects) != 0 || got.ActiveProject != 0 {
		t.Fatalf("default: %#v, activo %d", got.OpenProjects, got.ActiveProject)
	}
}

func TestSetOpenProjectsPersistsOrderAndActive(t *testing.T) {
	s := openTemp(t)
	if err := s.SetOpenProjects([]int64{7, 3, 9}, 3); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings()
	if !reflect.DeepEqual(got.OpenProjects, []int64{7, 3, 9}) || got.ActiveProject != 3 {
		t.Fatalf("persistido: %v activo %d", got.OpenProjects, got.ActiveProject)
	}
	// Un activo que no está en la lista no se guarda.
	if err := s.SetOpenProjects([]int64{7, 3}, 99); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetSettings(); got.ActiveProject != 0 || len(got.OpenProjects) != 2 {
		t.Errorf("activo ajeno: %v activo %d", got.OpenProjects, got.ActiveProject)
	}
}

// El diálogo de Configuración guarda una copia vieja de los ajustes: no debe
// cerrar pestañas ni cambiar la activa (igual que dockLayout).
func TestSaveSettingsDoesNotOverwriteOpenProjects(t *testing.T) {
	s := openTemp(t)
	if err := s.SetOpenProjects([]int64{4, 5}, 5); err != nil {
		t.Fatal(err)
	}
	stale := DefaultSettings() // copia vieja: sin proyectos abiertos
	stale.Language = "es"
	if err := s.SaveSettings(stale); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings()
	if got.Language != "es" {
		t.Fatal("SaveSettings no guardó el resto")
	}
	if !reflect.DeepEqual(got.OpenProjects, []int64{4, 5}) || got.ActiveProject != 5 {
		t.Errorf("SaveSettings pisó los proyectos: %v activo %d", got.OpenProjects, got.ActiveProject)
	}
}

func TestNormalizeOpenProjects(t *testing.T) {
	got := NormalizeOpenProjects([]int64{3, 0, -1, 3, 8, 9, 10, 11, 12})
	if !reflect.DeepEqual(got, []int64{3, 8, 9, 10}) {
		t.Errorf("sin ids inválidos ni repetidos, hasta %d: %v", MaxOpenProjects, got)
	}
	if got := NormalizeOpenProjects(nil); got == nil || len(got) != 0 {
		t.Errorf("nil debe volver como lista vacía: %#v", got)
	}
}
