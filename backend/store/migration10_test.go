package store

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// seedV9 arma una base como la dejaba la versión 9 (modo por conversación):
// aplica solo las migraciones 1–9 y registra tres workspaces con su impresora,
// su descripción y su último checkpoint. withSkills agrega el registro de skills
// (dos encendidas, una apagada y una propia encendida), la base que un usuario
// real tiene al actualizar a la versión con la configuración por workspace.
func seedV9(t *testing.T, path string, withSkills bool) {
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
		if m.version > 9 {
			break
		}
		if err := s.apply(m); err != nil {
			t.Fatalf("migración %d: %v", m.version, err)
		}
	}
	qs := []string{
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at, last_checkpoint) VALUES (1, 'Caja', '/tmp/caja', 'bambu-a1', 'una caja', 5, 1, 1, '002-tapa.scad')`,
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (2, 'Soporte', '/tmp/soporte', 'bambu-x1c', '', 7, 2, 2)`,
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (3, 'Llavero', '/tmp/llavero', 'bambu-a1-mini', '', 9, 3, 3)`,
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (4, 'Engranes', '/tmp/engranes', 'bambu-p1s', '', 11, 4, 4)`,
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (5, 'Raro', '/tmp/raro', 'impresora-desconocida', '', 12, 5, 5)`,
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (6, 'Grande', '/tmp/grande', 'bambu-h2d', '', 13, 6, 6)`,
		`INSERT INTO checkpoints (workspace_id, file_name, parent_file, created_at) VALUES (1, '001-base.scad', '', 1)`,
		`INSERT INTO chats (id, workspace_id, provider_id, model, title, created_at, updated_at) VALUES (1, 1, 'ollama', 'qwen3', 'Una caja', 10, 20)`,
	}
	if withSkills {
		qs = append(qs,
			`DELETE FROM skills`,
			`INSERT INTO skills (name, source, description, enabled, updated_at) VALUES ('openscad-core', 'builtin', 'base', 1, 1)`,
			`INSERT INTO skills (name, source, description, enabled, updated_at) VALUES ('bambu-lab-printers', 'builtin', 'bambu', 1, 1)`,
			`INSERT INTO skills (name, source, description, enabled, updated_at) VALUES ('openscad-libraries', 'builtin', 'libs', 0, 1)`,
			`INSERT INTO skills (name, source, description, enabled, updated_at) VALUES ('mi-skill', 'user', 'propia', 1, 1)`,
		)
	}
	for _, q := range qs {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

// La migración 10 es aditiva: una base de la versión 9 con workspaces y skills
// sube a la 10 sin perder nada, cada workspace queda con la lista de skills que
// estaban encendidas en ese momento (backfill SOLO de la columna nueva), sin
// ajustes de impresión propios, y abrirla otra vez no hace nada.
func TestMigration10BackfillsSkillsKeepingEverythingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	seedV9(t, path, true)

	want := []string{"bambu-lab-printers", "mi-skill", "openscad-core"} // las encendidas, ordenadas
	for i := 0; i < 2; i++ {                                            // la segunda apertura comprueba la idempotencia
		s, err := Open(path)
		if err != nil {
			t.Fatalf("apertura %d: %v", i, err)
		}
		if v, err := s.SchemaVersion(); err != nil || v < 10 {
			t.Fatalf("versión %d, %v (esperaba al menos 10)", v, err)
		}
		list, err := s.ListWorkspaces()
		if err != nil || len(list) != 6 {
			t.Fatalf("workspaces: %+v, %v", list, err)
		}
		// Los colores que tenía cada impresora se congelan (antes eran su
		// multicolor_slots); un id desconocido no se toca. Nada más de los ajustes.
		wantColors := map[string]int{"Caja": 4, "Soporte": 16, "Llavero": 4, "Engranes": 16, "Raro": 0, "Grande": 24}
		for _, w := range list {
			if !reflect.DeepEqual(w.Skills, want) {
				t.Errorf("%s: skills %v, esperaba %v", w.Name, w.Skills, want)
			}
			if w.Print != (WorkspacePrint{Colors: wantColors[w.Name]}) {
				t.Errorf("%s: ajustes de impresión %+v, esperaba solo colors=%d", w.Name, w.Print, wantColors[w.Name])
			}
		}
		// Nada de lo anterior se reescribió.
		w, err := s.GetWorkspace(1)
		if err != nil || w.Name != "Caja" || w.Path != "/tmp/caja" || w.PrinterID != "bambu-a1" || w.Description != "una caja" ||
			w.LastOpened != 5 || w.CreatedAt != 1 || w.LastCheckpoint != "002-tapa.scad" {
			t.Errorf("el workspace cambió: %+v, %v", w, err)
		}
		if w2, _ := s.GetWorkspace(2); w2.PrinterID != "bambu-x1c" {
			t.Errorf("impresora del 2: %+v", w2)
		}
		if cps, _ := s.ListCheckpoints(1); len(cps) != 1 || cps[0].FileName != "001-base.scad" {
			t.Errorf("checkpoints: %+v", cps)
		}
		if c, _ := s.GetChat(1); c.Title != "Una caja" || c.Model != "qwen3" {
			t.Errorf("chat: %+v", c)
		}
		// Los registros de skills tampoco cambian: el backfill solo copia nombres.
		rows, _ := s.ListSkills()
		on := 0
		for _, r := range rows {
			if r.Enabled {
				on++
			}
		}
		if len(rows) != 4 || on != 3 {
			t.Errorf("registro de skills: %+v", rows)
		}
		s.Close()
	}
}

// El backfill de colores solo toca los workspaces que no fijaron nada: uno que ya
// tiene ajustes (se simula escribiéndolos antes de que corra la 10) los conserva.
func TestMigration10KeepsPrintSettingsThatAreAlreadySet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	s0 := &Store{db: db}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, desc TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version > 10 {
			break
		}
		if m.version == 10 {
			// Primero la parte aditiva (las columnas), luego los ajustes de un
			// workspace, y recién entonces el resto de la migración: reproduce el
			// UPDATE sobre una fila que ya tiene print_settings.
			if _, err := db.Exec(`ALTER TABLE workspaces ADD COLUMN skills TEXT NOT NULL DEFAULT ''`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`ALTER TABLE workspaces ADD COLUMN print_settings TEXT NOT NULL DEFAULT ''`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO workspaces (id, name, path, printer_id, last_opened, created_at, updated_at, print_settings) VALUES (1, 'Fijo', '/tmp/f', 'bambu-p1s', 1, 1, 1, '{"colors":2}')`); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO workspaces (id, name, path, printer_id, last_opened, created_at, updated_at) VALUES (2, 'Libre', '/tmp/l', 'bambu-p1s', 1, 1, 1)`); err != nil {
				t.Fatal(err)
			}
			// Solo el UPDATE de colores de la migración (la última sentencia).
			sql := m.sql[strings.LastIndex(m.sql, "UPDATE workspaces SET print_settings"):]
			if _, err := db.Exec(sql); err != nil {
				t.Fatalf("backfill de colores: %v", err)
			}
			continue
		}
		if err := s0.apply(m); err != nil {
			t.Fatalf("migración %d: %v", m.version, err)
		}
	}
	var fijo, libre string
	if err := db.QueryRow(`SELECT print_settings FROM workspaces WHERE id = 1`).Scan(&fijo); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT print_settings FROM workspaces WHERE id = 2`).Scan(&libre); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if fijo != `{"colors":2}` || libre != `{"colors":16}` {
		t.Errorf("fijo=%q libre=%q", fijo, libre)
	}
}

// Una base sin filas en `skills` no tiene nada que congelar: la columna queda
// vacía («sin definir») y se resuelve por defecto, no como «ninguna skill».
func TestMigration10WithoutSkillsLeavesTheColumnUndefined(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	seedV9(t, path, false)
	// La migración 1 no siembra skills, pero la prueba lo comprueba: el caso es
	// «registro vacío».
	db, _ := sql.Open("sqlite", "file:"+path)
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM skills`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("la base de partida debía tener el registro vacío: %d, %v", n, err)
	}
	db.Close()

	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		list, _ := s.ListWorkspaces()
		if len(list) != 6 {
			t.Fatalf("workspaces: %+v", list)
		}
		for _, w := range list {
			if w.Skills != nil {
				t.Errorf("%s: skills %v, esperaba nil (sin definir)", w.Name, w.Skills)
			}
		}
		var raw string
		if err := s.db.QueryRow(`SELECT skills FROM workspaces WHERE id = 1`).Scan(&raw); err != nil || raw != "" {
			t.Errorf("la columna debía quedar vacía: %q, %v", raw, err)
		}
		s.Close()
	}
}

// Todas las skills apagadas es una lista vacía legítima, distinta de «sin definir».
func TestMigration10AllSkillsOffIsAnEmptyList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	seedV9(t, path, true)
	db, _ := sql.Open("sqlite", "file:"+path)
	if _, err := db.Exec(`UPDATE skills SET enabled = 0`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, _ := s.GetWorkspace(1)
	if w.Skills == nil || len(w.Skills) != 0 {
		t.Errorf("esperaba una lista vacía, no nil: %#v", w.Skills)
	}
}

// SetWorkspaceConfig guarda impresora, skills y ajustes; nil y vacío no son lo
// mismo; los punteros distinguen «no fijado» de 0; un id inexistente es error.
func TestSetWorkspaceConfigRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.InsertWorkspace(Workspace{Name: "W", Path: t.TempDir(), PrinterID: "bambu-a1"})
	if err != nil {
		t.Fatal(err)
	}
	if w, _ := s.GetWorkspace(id); w.Skills != nil || w.Print != (WorkspacePrint{}) {
		t.Fatalf("un workspace nuevo sin config nace sin definir: %+v", w)
	}

	zero, three, wide, dia := 0.0, 3, 0.5, 2.85
	p := WorkspacePrint{Colors: 3, Material: "petg", InfillPercent: &zero, WallLoops: &three, LineWidthMm: &wide, FilamentDiameterMm: &dia}
	if err := s.SetWorkspaceConfig(id, "bambu-x1c", []string{"openscad-core", "z-skill"}, p); err != nil {
		t.Fatal(err)
	}
	w, _ := s.GetWorkspace(id)
	if w.PrinterID != "bambu-x1c" || !reflect.DeepEqual(w.Skills, []string{"openscad-core", "z-skill"}) {
		t.Errorf("config: %+v", w)
	}
	if w.Print.Colors != 3 || w.Print.Material != "petg" || w.Print.InfillPercent == nil || *w.Print.InfillPercent != 0 ||
		*w.Print.WallLoops != 3 || *w.Print.LineWidthMm != 0.5 || *w.Print.FilamentDiameterMm != 2.85 {
		t.Errorf("print: %+v", w.Print)
	}

	if err := s.SetWorkspaceConfig(id, "bambu-x1c", []string{}, WorkspacePrint{}); err != nil {
		t.Fatal(err)
	}
	if w, _ := s.GetWorkspace(id); w.Skills == nil || len(w.Skills) != 0 || w.Print != (WorkspacePrint{}) {
		t.Errorf("lista vacía y sin ajustes: %#v %+v", w.Skills, w.Print)
	}
	if err := s.SetWorkspaceConfig(id, "bambu-x1c", nil, WorkspacePrint{}); err != nil {
		t.Fatal(err)
	}
	if w, _ := s.GetWorkspace(id); w.Skills != nil {
		t.Errorf("nil vuelve a «sin definir»: %#v", w.Skills)
	}
	if err := s.SetWorkspaceConfig(999, "bambu-a1", nil, WorkspacePrint{}); err == nil {
		t.Error("un id inexistente debía fallar")
	}
}

// El alta guarda la lista de skills y los ajustes junto con el resto.
func TestInsertWorkspaceStoresItsConfig(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	id, err := s.InsertWorkspace(Workspace{Name: "W", Path: t.TempDir(), PrinterID: "bambu-p1s", Skills: []string{"a", "b"}, Print: WorkspacePrint{Colors: 2}})
	if err != nil {
		t.Fatal(err)
	}
	w, _ := s.GetWorkspace(id)
	if !reflect.DeepEqual(w.Skills, []string{"a", "b"}) || w.Print.Colors != 2 {
		t.Errorf("alta: %+v", w)
	}
	// Una columna con basura se lee como «sin definir», no impide abrir.
	if _, err := s.db.Exec(`UPDATE workspaces SET skills = 'no es json', print_settings = '{roto' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if w, err := s.GetWorkspace(id); err != nil || w.Skills != nil || w.Print != (WorkspacePrint{}) {
		t.Errorf("columnas rotas: %+v, %v", w, err)
	}
}

func TestSlicerPaths(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "scadlab.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	got, _ := s.GetSettings()
	if got.SlicerPaths == nil || len(got.SlicerPaths) != 0 {
		t.Errorf("por defecto un objeto vacío, no nil: %#v", got.SlicerPaths)
	}
	// Solo bambuStudioPath: vale para bambu-studio y para ningún otro.
	got.BambuStudioPath = " /Apps/Bambu.app "
	if got.SlicerPath("bambu-studio") != "/Apps/Bambu.app" || got.SlicerPath("orcaslicer") != "" {
		t.Errorf("compatibilidad: %q %q", got.SlicerPath("bambu-studio"), got.SlicerPath("orcaslicer"))
	}
	got.SlicerPaths = map[string]string{"orcaslicer": "  /Apps/Orca.app ", "vacio": "  ", " ": "x", "bambu-studio": "/Apps/Otro.app"}
	if err := s.SaveSettings(got); err != nil {
		t.Fatal(err)
	}
	back, _ := s.GetSettings()
	if !reflect.DeepEqual(back.SlicerPaths, map[string]string{"orcaslicer": "/Apps/Orca.app", "bambu-studio": "/Apps/Otro.app"}) {
		t.Errorf("rutas guardadas: %#v", back.SlicerPaths)
	}
	if back.SlicerPath("bambu-studio") != "/Apps/Otro.app" {
		t.Errorf("slicerPaths gana sobre bambuStudioPath: %q", back.SlicerPath("bambu-studio"))
	}
}
