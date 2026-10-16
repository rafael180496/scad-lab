package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

// seedV3 arma una base como la dejaba la versión 3: aplica solo las migraciones
// 1–3 y registra un workspace con checkpoints, un ajuste y un chat. Es la base
// que un usuario real tiene en disco cuando actualiza.
func seedV3(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Store{db: db}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, desc TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version > 3 {
			break
		}
		if err := s.apply(m); err != nil {
			t.Fatalf("migración %d: %v", m.version, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (1, 'Caja', '/tmp/caja', 'bambu-a1', 'd', 5, 1, 1)`,
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (2, 'Soporte', '/tmp/soporte', 'bambu-a1-mini', '', 0, 2, 2)`,
		`INSERT INTO checkpoints (workspace_id, file_name, created_at) VALUES (1, '001-base.scad', 1)`,
		`INSERT INTO settings (key, value, updated_at) VALUES ('theme', '"light"', 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// La migración 4 es aditiva: una base de la versión 3 con filas sube a la 4, no
// pierde nada y cada workspace queda con last_checkpoint vacío. Abrirla otra vez
// no hace nada.
func TestMigration4AddsLastCheckpointKeepingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	seedV3(t, path)

	for i := 0; i < 2; i++ { // la segunda apertura comprueba la idempotencia
		s, err := Open(path)
		if err != nil {
			t.Fatalf("apertura %d: %v", i, err)
		}
		if v, err := s.SchemaVersion(); err != nil || v != 4 {
			t.Fatalf("versión %d, %v (esperaba 4)", v, err)
		}
		ws, err := s.ListWorkspaces()
		if err != nil || len(ws) != 2 {
			t.Fatalf("workspaces: %+v, %v", ws, err)
		}
		for _, w := range ws {
			if i == 0 && w.LastCheckpoint != "" {
				t.Errorf("%s: last_checkpoint %q, esperaba vacío", w.Name, w.LastCheckpoint)
			}
		}
		w, _ := s.GetWorkspace(1)
		if w.Name != "Caja" || w.Path != "/tmp/caja" || w.PrinterID != "bambu-a1" || w.Description != "d" || w.LastOpened != 5 {
			t.Errorf("la fila cambió: %+v", w)
		}
		if cps, _ := s.ListCheckpoints(1); len(cps) != 1 {
			t.Errorf("checkpoints perdidos: %+v", cps)
		}
		if got, _ := s.GetSettings(); got.Theme != "light" {
			t.Errorf("ajuste perdido: %q", got.Theme)
		}
		if i == 0 {
			if err := s.SetLastCheckpoint(1, "001-base.scad"); err != nil {
				t.Fatal(err)
			}
		}
		s.Close()
	}
	// Lo guardado en la primera sesión sigue ahí tras reabrir.
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if w, _ := s.GetWorkspace(1); w.LastCheckpoint != "001-base.scad" {
		t.Errorf("last_checkpoint no persistió: %q", w.LastCheckpoint)
	}
	if w, _ := s.GetWorkspace(2); w.LastCheckpoint != "" {
		t.Errorf("el otro workspace cambió: %q", w.LastCheckpoint)
	}
}

func TestSetLastCheckpointUnknownWorkspace(t *testing.T) {
	s := openTemp(t)
	if err := s.SetLastCheckpoint(99, "001-x.scad"); err != ErrNotFound {
		t.Errorf("workspace inexistente: %v", err)
	}
}

// El estado del visor: defaults de una instalación nueva, ida y vuelta, y la
// muestra fuera de rango vuelve a la primera.
func TestViewerStateDefaultsAndRoundTrip(t *testing.T) {
	s := openTemp(t)
	got, _ := s.GetSettings()
	if got.Viewer != DefaultViewer() || !got.Viewer.ModelColors {
		t.Fatalf("default: %+v", got.Viewer)
	}
	want := ViewerState{Wireframe: true, Edges: true, Ortho: [2]bool{true, false}, Split: true, ColorIndex: 4, ModelColors: false}
	if err := s.SetSetting("viewer", want); err != nil {
		t.Fatal(err)
	}
	again, _ := s.GetSettings()
	if !reflect.DeepEqual(again.Viewer, want) {
		t.Errorf("ida y vuelta: %+v", again.Viewer)
	}
	if err := s.SetSetting("viewer", ViewerState{ColorIndex: 17}); err != nil {
		t.Fatal(err)
	}
	if again, _ = s.GetSettings(); again.Viewer.ColorIndex != 0 {
		t.Errorf("muestra fuera de rango: %d", again.Viewer.ColorIndex)
	}
}

// Una base de antes del visor guardado no tiene la clave: ModelColors conserva
// su default (true) en vez de caer al cero de Go. Y un JSON parcial tampoco lo apaga.
func TestViewerStatePartialJSONKeepsModelColors(t *testing.T) {
	s := openTemp(t)
	if _, err := s.db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ('viewer', '{"wireframe":true}', 1)`); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings()
	if !got.Viewer.Wireframe || !got.Viewer.ModelColors {
		t.Errorf("parcial: %+v", got.Viewer)
	}
}

// El diálogo de Configuración guarda una copia vieja de los ajustes: no debe
// pisar el estado del visor.
func TestSaveSettingsDoesNotOverwriteViewer(t *testing.T) {
	s := openTemp(t)
	want := ViewerState{Wireframe: true, ColorIndex: 2, ModelColors: true}
	if err := s.SetSetting("viewer", want); err != nil {
		t.Fatal(err)
	}
	stale := DefaultSettings()
	stale.Language = "es"
	if err := s.SaveSettings(stale); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings()
	if got.Language != "es" {
		t.Fatal("SaveSettings no guardó el resto")
	}
	if got.Viewer != want {
		t.Errorf("SaveSettings pisó el visor: %+v", got.Viewer)
	}
}
