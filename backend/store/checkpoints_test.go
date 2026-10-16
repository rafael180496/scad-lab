package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func newWS(t *testing.T, s *Store) int64 {
	t.Helper()
	id, err := s.InsertWorkspace(Workspace{Name: "w", Path: filepath.Join(t.TempDir(), "w"), PrinterID: "bambu-a1-mini"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func found(files ...string) []Checkpoint {
	out := make([]Checkpoint, len(files))
	for i, f := range files {
		out[i] = Checkpoint{FileName: f, FileMtime: int64(100 + i)}
	}
	return out
}

// La migración 3 sobre una base que ya estaba en la versión 2 y tenía filas:
// aditiva, no pierde nada y deja los valores por defecto.
func TestMigration3IsAdditive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ws := newWS(t, s)
	if err := s.SyncCheckpoints(ws, found("001-a.scad")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCheckpointUI(ws, "001-a.scad", "Base", true); err != nil {
		t.Fatal(err)
	}
	// Simula una base de antes de S3: sin las columnas, sin la tabla y sin la
	// fila de la versión 3.
	for _, q := range []string{
		`ALTER TABLE checkpoints DROP COLUMN agent_id`,
		`ALTER TABLE checkpoints DROP COLUMN chat_id`,
		`DROP TABLE checkpoint_trash`,
		`DELETE FROM schema_migrations WHERE version = 3`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	for i := 0; i < 2; i++ { // la segunda apertura no repite nada
		s, err = Open(path)
		if err != nil {
			t.Fatalf("apertura %d: %v", i, err)
		}
		// MAX(version): la 3 se reaplicó, y la base ya traía las siguientes.
		if v, _ := s.SchemaVersion(); v != migrations[len(migrations)-1].version {
			t.Errorf("versión: %d", v)
		}
		c, err := s.GetCheckpoint(ws, "001-a.scad")
		if err != nil || c.Label != "Base" || !c.Collapsed || c.AgentID != "" || c.ChatID != 0 {
			t.Fatalf("fila tras migrar: %+v, %v", c, err)
		}
		if err := s.StashCheckpointMeta(ws, "001-a.scad", "20260101-000000-001-a.scad"); err != nil {
			t.Errorf("checkpoint_trash: %v", err)
		}
		s.Close()
	}
}

// El rescan reescribe lo que sale del archivo pero no el vínculo con el agente,
// el chat, el rótulo ni la sesión.
func TestSyncKeepsLinks(t *testing.T) {
	s := openTemp(t)
	ws := newWS(t, s)
	if err := s.SyncCheckpoints(ws, found("001-a.scad", "002-b.scad")); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCheckpointLink(ws, "002-b.scad", "claude", "11111111-2222-3333-4444-555555555555"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCheckpointChat(ws, "001-a.scad", 7); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCheckpointUI(ws, "002-b.scad", "Tapa", true); err != nil {
		t.Fatal(err)
	}
	next := found("001-a.scad", "002-b.scad")
	next[1].ParentFile, next[1].Description = "001-a.scad", "nueva"
	if err := s.SyncCheckpoints(ws, next); err != nil {
		t.Fatal(err)
	}
	b, _ := s.GetCheckpoint(ws, "002-b.scad")
	if b.AgentID != "claude" || b.SessionID == "" || b.Label != "Tapa" || !b.Collapsed || b.ParentFile != "001-a.scad" || b.Description != "nueva" {
		t.Errorf("002 tras el rescan: %+v", b)
	}
	a, _ := s.GetCheckpoint(ws, "001-a.scad")
	if a.ChatID != 7 {
		t.Errorf("001 perdió el chat: %+v", a)
	}
}

// Un vínculo que llega antes que el rescan crea la fila; el rescan la completa.
func TestLinkBeforeScan(t *testing.T) {
	s := openTemp(t)
	ws := newWS(t, s)
	if _, err := s.GetCheckpoint(ws, "003-x.scad"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("esperaba ErrNotFound, hubo %v", err)
	}
	if err := s.SetCheckpointLink(ws, "003-x.scad", "codex", ""); err != nil {
		t.Fatal(err)
	}
	in := found("003-x.scad")
	in[0].ParentFile = "002-y.scad"
	if err := s.SyncCheckpoints(ws, in); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetCheckpoint(ws, "003-x.scad")
	if err != nil || c.AgentID != "codex" || c.ParentFile != "002-y.scad" || c.FileMtime != 100 {
		t.Fatalf("fila: %+v, %v", c, err)
	}
	// Y un archivo que no llegó a existir se va con el rescan.
	if err := s.SetCheckpointLink(ws, "999-ghost.scad", "codex", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncCheckpoints(ws, in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCheckpoint(ws, "999-ghost.scad"); !errors.Is(err, ErrNotFound) {
		t.Errorf("la fila fantasma debía irse: %v", err)
	}
}

// Papelera: el rescan borra la fila, pero rótulo, plegado y vínculo vuelven con
// el checkpoint, aunque al restaurarlo cambie de nombre.
func TestTrashMetaRoundTrip(t *testing.T) {
	s := openTemp(t)
	ws := newWS(t, s)
	if err := s.SyncCheckpoints(ws, found("001-a.scad", "002-b.scad")); err != nil {
		t.Fatal(err)
	}
	_ = s.SetCheckpointUI(ws, "002-b.scad", "Tapa", true)
	_ = s.SetCheckpointLink(ws, "002-b.scad", "claude", "sess")
	_ = s.SetCheckpointChat(ws, "002-b.scad", 4)

	if err := s.StashCheckpointMeta(ws, "002-b.scad", "20260101-101010-002-b.scad"); err != nil {
		t.Fatal(err)
	}
	// Un archivo sin fila no es un error.
	if err := s.StashCheckpointMeta(ws, "404.scad", "20260101-101010-404.scad"); err != nil {
		t.Fatal(err)
	}
	if err := s.SyncCheckpoints(ws, found("001-a.scad")); err != nil { // el rescan tras mandarlo a la papelera
		t.Fatal(err)
	}
	if _, err := s.GetCheckpoint(ws, "002-b.scad"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("la fila debía irse: %v", err)
	}
	if err := s.RestoreCheckpointMeta(ws, "20260101-101010-002-b.scad", "007-b.scad"); err != nil {
		t.Fatal(err)
	}
	c, err := s.GetCheckpoint(ws, "007-b.scad")
	if err != nil || c.Label != "Tapa" || !c.Collapsed || c.AgentID != "claude" || c.SessionID != "sess" || c.ChatID != 4 {
		t.Fatalf("restaurado: %+v, %v", c, err)
	}
	// Se consume una sola vez; sin nada guardado no hace nada.
	if err := s.RestoreCheckpointMeta(ws, "20260101-101010-002-b.scad", "008-b.scad"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetCheckpoint(ws, "008-b.scad"); !errors.Is(err, ErrNotFound) {
		t.Errorf("no debía crear una fila sin datos guardados: %v", err)
	}
}
