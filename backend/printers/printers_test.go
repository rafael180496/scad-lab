package printers

import "testing"

func TestCatalog(t *testing.T) {
	all, err := All()
	if err != nil || len(all) == 0 {
		t.Fatalf("catálogo vacío: %v", err)
	}
	p, ok := ByID("bambu-a1-mini")
	if !ok || p.BuildVolume.X != 180 || p.BuildVolume.Z != 180 {
		t.Errorf("A1 mini: %+v", p)
	}
	for _, p := range all {
		if p.ID == "" || p.BuildVolume.X <= 0 || p.BuildVolume.Y <= 0 || p.BuildVolume.Z <= 0 {
			t.Errorf("impresora incompleta: %+v", p)
		}
	}
	if _, ok := ByID("no-existe"); ok {
		t.Error("ByID de un id inexistente devolvió ok")
	}
}
