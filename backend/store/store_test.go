package store

import (
	"path/filepath"
	"testing"
)

// Toda prueba abre su base en t.TempDir(): nunca contra scadlab.db real.
func openTemp(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("apertura %d: %v", i, err)
		}
		v, err := s.SchemaVersion()
		if err != nil || v != migrations[len(migrations)-1].version {
			t.Fatalf("versión %d, %v", v, err)
		}
		s.Close()
	}
}

func TestSettingsDefaultsAndRoundTrip(t *testing.T) {
	s := openTemp(t)
	got, err := s.GetSettings()
	if err != nil || got != DefaultSettings() {
		t.Fatalf("defaults: %+v, %v", got, err)
	}
	got.Language, got.InfillPercent = "es", 20
	if err := s.SaveSettings(got); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("theme", "light"); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetSettings()
	if again.Language != "es" || again.InfillPercent != 20 || again.Theme != "light" || again.DefaultPrinter != "bambu-a1-mini" {
		t.Errorf("round trip: %+v", again)
	}
}

// Una base de antes de S1 no tiene las claves de render: valen sus defaults.
// Y apagar el auto-render tiene que persistir aunque false sea el valor cero.
func TestRenderSettingsDefaultsAndFalse(t *testing.T) {
	s := openTemp(t)
	got, _ := s.GetSettings()
	if !got.AutoRender || got.RenderTimeoutSec != 300 {
		t.Fatalf("defaults de render: %+v", got)
	}
	got.AutoRender, got.RenderTimeoutSec = false, 60
	if err := s.SaveSettings(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetSettings()
	if again.AutoRender || again.RenderTimeoutSec != 60 {
		t.Errorf("round trip de render: %+v", again)
	}
}

func TestSeededCatalogs(t *testing.T) {
	s := openTemp(t)
	provs, err := s.ListProviders()
	if err != nil || len(provs) != 7 {
		t.Fatalf("proveedores: %d, %v", len(provs), err)
	}
	mats, err := s.ListMaterials()
	if err != nil || len(mats) == 0 || mats[0].ID != "pla" {
		t.Fatalf("materiales: %+v, %v", mats, err)
	}
}

func TestCheckpointSyncKeepsUIState(t *testing.T) {
	s := openTemp(t)
	id, err := s.InsertWorkspace(Workspace{Name: "w", Path: t.TempDir(), PrinterID: "bambu-a1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SyncCheckpoints(id, []Checkpoint{{FileName: "001-a.scad"}, {FileName: "002-b.scad", ParentFile: "001-a.scad"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCheckpointUI(id, "001-a.scad", "Base", true); err != nil {
		t.Fatal(err)
	}
	// 002 desaparece del disco; 001 sigue: conserva su rótulo y plegado.
	if err := s.SyncCheckpoints(id, []Checkpoint{{FileName: "001-a.scad", Description: "nueva"}}); err != nil {
		t.Fatal(err)
	}
	cps, _ := s.ListCheckpoints(id)
	if len(cps) != 1 || cps[0].Label != "Base" || !cps[0].Collapsed || cps[0].Description != "nueva" {
		t.Errorf("sync: %+v", cps)
	}
}
