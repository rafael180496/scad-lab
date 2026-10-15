package main

import (
	"strings"
	"testing"

	"scadlab/backend/store"
)

// SaveDockLayout guarda el diseño y lo devuelve GetSettings; vacío lo borra; un
// valor que no es un objeto JSON se rechaza sin tocar lo guardado.
func TestSaveDockLayout(t *testing.T) {
	a := newTestApp(t)
	layout := `{"v":1,"dock":{"panels":{}}}`
	if err := a.SaveDockLayout(layout); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.store.GetSettings(); s.DockLayout != layout {
		t.Fatalf("guardado: %q", s.DockLayout)
	}
	for _, bad := range []string{"[]", "no es json", `"texto"`, `{"a":"` + strings.Repeat("x", store.MaxDockLayoutBytes) + `"}`} {
		if err := a.SaveDockLayout(bad); err == nil {
			t.Errorf("%.20q debería rechazarse", bad)
		}
	}
	if s, _ := a.store.GetSettings(); s.DockLayout != layout {
		t.Errorf("un rechazo pisó lo guardado: %q", s.DockLayout)
	}
	if err := a.SaveDockLayout(""); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.store.GetSettings(); s.DockLayout != "" {
		t.Errorf("vacío debería borrar: %q", s.DockLayout)
	}
}

// SaveDockLayouts recorta nombres, reemplaza repetidos y devuelve lo guardado.
func TestSaveDockLayouts(t *testing.T) {
	a := newTestApp(t)
	got, err := a.SaveDockLayouts([]store.DockLayoutEntry{
		{Name: " Modelado ", JSON: `{"v":1}`},
		{Name: "modelado", JSON: `{"v":2}`},
		{Name: "Revisión", JSON: `{"v":1}`},
	})
	if err != nil || len(got) != 2 || got[0].Name != "modelado" || got[0].JSON != `{"v":2}` {
		t.Fatalf("resultado: %+v, %v", got, err)
	}
	if s, _ := a.store.GetSettings(); len(s.DockLayouts) != 2 || s.DockLayouts[1].Name != "Revisión" {
		t.Fatalf("guardado: %+v", s.DockLayouts)
	}
	if _, err := a.SaveDockLayouts([]store.DockLayoutEntry{{Name: "", JSON: `{}`}}); err == nil {
		t.Error("nombre vacío debería rechazarse")
	}
	if s, _ := a.store.GetSettings(); len(s.DockLayouts) != 2 {
		t.Errorf("un rechazo pisó la lista: %+v", s.DockLayouts)
	}
	// Borrar todos: la lista vacía se guarda.
	if out, err := a.SaveDockLayouts(nil); err != nil || len(out) != 0 {
		t.Fatalf("vaciar: %v %v", out, err)
	}
	if s, _ := a.store.GetSettings(); len(s.DockLayouts) != 0 {
		t.Errorf("no se vació: %+v", s.DockLayouts)
	}
}
