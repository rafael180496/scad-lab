package store

import (
	"path/filepath"
	"reflect"
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
	if err != nil || !reflect.DeepEqual(got, DefaultSettings()) {
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

// El tope del historial de un checkpoint (S16): 20 de fábrica, 1–200, y una base
// anterior que no tiene la clave (o con un valor roto) cae en 20.
func TestHistoryMaxVersions(t *testing.T) {
	s := openTemp(t)
	got, _ := s.GetSettings()
	if got.HistoryMaxVersions != DefaultHistoryVersions || DefaultHistoryVersions != 20 {
		t.Fatalf("de fábrica: %d", got.HistoryMaxVersions)
	}
	for in, want := range map[int]int{1: 1, 7: 7, 200: 200, 201: 200, 99999: 200, 0: 20, -3: 20} {
		got.HistoryMaxVersions = in
		if err := s.SaveSettings(got); err != nil {
			t.Fatal(err)
		}
		again, _ := s.GetSettings()
		if again.HistoryMaxVersions != want {
			t.Errorf("tope %d -> %d, quería %d", in, again.HistoryMaxVersions, want)
		}
	}
	// Un valor roto escrito a mano en la base se normaliza al leer.
	if err := s.SetSetting("historyMaxVersions", 5000); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetSettings(); again.HistoryMaxVersions != 200 {
		t.Errorf("valor roto: %d", again.HistoryMaxVersions)
	}
	if err := s.SetSetting("historyMaxVersions", 0); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.GetSettings(); again.HistoryMaxVersions != 20 {
		t.Errorf("cero: %d", again.HistoryMaxVersions)
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

// Las muestras del visor: default de fábrica, round trip, y un cliente viejo
// que manda null no las pisa.
func TestSwatches(t *testing.T) {
	s := openTemp(t)
	got, _ := s.GetSettings()
	if !reflect.DeepEqual(got.Swatches, DefaultSwatches()) {
		t.Fatalf("default: %v", got.Swatches)
	}
	custom := []string{"#000000", "#111111", "#222222", "#333333", "#444444", "#ABCDEF"}
	if err := s.SetSetting("swatches", custom); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSettings()
	if !reflect.DeepEqual(got.Swatches, custom) {
		t.Fatalf("round trip: %v", got.Swatches)
	}
	got.Swatches = nil
	if err := s.SaveSettings(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetSettings()
	if !reflect.DeepEqual(again.Swatches, custom) {
		t.Errorf("SaveSettings sin muestras las pisó: %v", again.Swatches)
	}
	if err := s.SetSetting("swatches", []string{"#fff"}); err != nil {
		t.Fatal(err)
	}
	again, _ = s.GetSettings()
	if !reflect.DeepEqual(again.Swatches, DefaultSwatches()) {
		t.Errorf("muestras rotas no volvieron al default: %v", again.Swatches)
	}
}

func TestValidSwatches(t *testing.T) {
	cases := []struct {
		in   []string
		want bool
	}{
		{DefaultSwatches(), true},
		{nil, false},
		{[]string{"#ff7a1a"}, false},
		{[]string{"#ff7a1a", "#e7e9ee", "#2b2f38", "#3ccf7a", "#5aa2ff", "red"}, false},
		{[]string{"#ff7a1a", "#e7e9ee", "#2b2f38", "#3ccf7a", "#5aa2ff", "#d32f2"}, false},
		{[]string{"#ff7a1a", "#e7e9ee", "#2b2f38", "#3ccf7a", "#5aa2ff", "#d32f2f", "#000000"}, false},
	}
	for _, c := range cases {
		if got := ValidSwatches(c.in); got != c.want {
			t.Errorf("ValidSwatches(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// Los campos del editor (S4) tienen defaults sin migración y se acotan: un
// valor roto en la base no llega al editor.
func TestEditorSettings(t *testing.T) {
	s := openTemp(t)
	got, _ := s.GetSettings()
	if got.EditorIndent != 4 || got.EditorFontSize != 13 || got.EditorWordWrap || got.EditorMinimap {
		t.Fatalf("defaults del editor: %+v", got)
	}
	got.EditorIndent, got.EditorFontSize, got.EditorWordWrap, got.EditorMinimap = 2, 15, true, true
	if err := s.SaveSettings(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetSettings()
	if again.EditorIndent != 2 || again.EditorFontSize != 15 || !again.EditorWordWrap || !again.EditorMinimap {
		t.Errorf("round trip del editor: %+v", again)
	}
	for _, kv := range []struct {
		key string
		val any
	}{{"editorIndent", 3}, {"editorFontSize", 99}} {
		if err := s.SetSetting(kv.key, kv.val); err != nil {
			t.Fatal(err)
		}
	}
	again, _ = s.GetSettings()
	if again.EditorIndent != 4 || again.EditorFontSize != 13 {
		t.Errorf("valores rotos no se normalizaron: %+v", again)
	}
}

// La ruta de Bambu Studio (S9) no necesita migración: vacía = autodetectar, y
// lo que el usuario guarda vuelve igual.
func TestBambuStudioPath(t *testing.T) {
	s := openTemp(t)
	got, _ := s.GetSettings()
	if got.BambuStudioPath != "" {
		t.Fatalf("default: %q", got.BambuStudioPath)
	}
	got.BambuStudioPath = "/Applications/BambuStudio.app"
	if err := s.SaveSettings(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetSettings()
	if again.BambuStudioPath != got.BambuStudioPath {
		t.Errorf("round trip: %q", again.BambuStudioPath)
	}
	again.BambuStudioPath = ""
	if err := s.SaveSettings(again); err != nil {
		t.Fatal(err)
	}
	final, _ := s.GetSettings()
	if final.BambuStudioPath != "" {
		t.Errorf("vaciar la ruta no se guardó: %q", final.BambuStudioPath)
	}
}

func TestLayout(t *testing.T) {
	s := openTemp(t)
	got, _ := s.GetSettings()
	if got.Layout != DefaultLayout() {
		t.Fatalf("layout por defecto: %+v", got.Layout)
	}
	custom := Layout{SidebarWidth: 300, SidebarCollapsed: true, CodeWidth: 600, BottomHeight: 260, DimsCollapsed: true, StatusBarHidden: true, WorkspacesHeight: 220}
	if err := s.SetSetting("layout", custom); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSettings()
	if got.Layout != custom {
		t.Fatalf("round trip: %+v", got.Layout)
	}
	// Un cliente que no conoce el layout (cero) no pisa el guardado.
	got.Layout = Layout{}
	if err := s.SaveSettings(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetSettings()
	if again.Layout != custom {
		t.Errorf("SaveSettings sin layout lo pisó: %+v", again.Layout)
	}
	// Fuera de rango se acota; 0 vuelve al default.
	n := Layout{SidebarWidth: 5000, CodeWidth: 10, BottomHeight: 0}.Normalize()
	if n.SidebarWidth != MaxSidebarWidth || n.CodeWidth != MinCodeWidth || n.BottomHeight != DefaultLayout().BottomHeight {
		t.Errorf("Normalize: %+v", n)
	}
}

// dockLayout y dockLayouts: se guardan solo por sus propios métodos y
// SaveSettings (cuyo diálogo tiene una copia vieja) no los pisa.
func TestDockLayoutSettings(t *testing.T) {
	s := openTemp(t)
	got, _ := s.GetSettings()
	if got.DockLayout != "" || len(got.DockLayouts) != 0 {
		t.Fatalf("por defecto: %q %v", got.DockLayout, got.DockLayouts)
	}
	layout := `{"v":1,"dock":{"panels":{}}}`
	list := []DockLayoutEntry{{Name: "Modelado 2", JSON: layout}}
	if err := s.SetSetting("dockLayout", layout); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSetting("dockLayouts", list); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSettings()
	if got.DockLayout != layout || !reflect.DeepEqual(got.DockLayouts, list) {
		t.Fatalf("round trip: %q %v", got.DockLayout, got.DockLayouts)
	}
	// Un diálogo con la copia vieja (vacía) guarda la configuración: no pisa el diseño.
	got.DockLayout = ""
	got.DockLayouts = nil
	if err := s.SaveSettings(got); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetSettings()
	if again.DockLayout != layout || !reflect.DeepEqual(again.DockLayouts, list) {
		t.Errorf("SaveSettings pisó el diseño: %q %v", again.DockLayout, again.DockLayouts)
	}
}

func TestValidDockLayoutJSON(t *testing.T) {
	for in, want := range map[string]bool{
		`{}`:                true,
		`{"v":1,"dock":{}}`: true,
		``:                  false,
		`[]`:                false,
		`"texto"`:           false,
		`null`:              false,
		`{"v":`:             false,
		`{"a":"` + string(make([]byte, MaxDockLayoutBytes)) + `"}`: false,
	} {
		if got := ValidDockLayoutJSON(in); got != want {
			t.Errorf("ValidDockLayoutJSON(%.30q) = %v, quiero %v", in, got, want)
		}
	}
}

func TestNormalizeDockLayouts(t *testing.T) {
	ok := `{"v":1}`
	got, err := NormalizeDockLayouts([]DockLayoutEntry{
		{Name: "  Mi diseño  ", JSON: ok},
		{Name: "otro", JSON: ok},
		{Name: "MI DISEÑO", JSON: `{"v":2}`}, // mismo nombre sin distinguir mayúsculas: gana el último
	})
	if err != nil || len(got) != 2 || got[0].Name != "MI DISEÑO" || got[0].JSON != `{"v":2}` || got[1].Name != "otro" {
		t.Fatalf("normalizar: %+v, %v", got, err)
	}
	long := ""
	for i := 0; i < 60; i++ {
		long += "ñ"
	}
	got, err = NormalizeDockLayouts([]DockLayoutEntry{{Name: long, JSON: ok}})
	if err != nil || len([]rune(got[0].Name)) != MaxDockLayoutName {
		t.Fatalf("nombre largo: %q, %v", got, err)
	}
	for name, in := range map[string][]DockLayoutEntry{
		"nombre vacío": {{Name: "  ", JSON: ok}},
		"JSON roto":    {{Name: "x", JSON: `{"v":`}},
		"no objeto":    {{Name: "x", JSON: `[1]`}},
	} {
		if _, err := NormalizeDockLayouts(in); err == nil {
			t.Errorf("%s: debería fallar", name)
		}
	}
	many := make([]DockLayoutEntry, MaxDockLayouts+1)
	for i := range many {
		many[i] = DockLayoutEntry{Name: string(rune('a'+i%26)) + string(rune('a'+i/26)), JSON: ok}
	}
	if _, err := NormalizeDockLayouts(many); err == nil {
		t.Error("más de 30 diseños debería fallar")
	}
	if out, err := NormalizeDockLayouts(nil); err != nil || out == nil || len(out) != 0 {
		t.Errorf("lista vacía: %v %v", out, err)
	}
}
