package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// seedV5 arma una base como la dejaba la versión 5 (servidores de modelos):
// aplica solo las migraciones 1–5 y registra un workspace con un chat y dos
// mensajes. Es la base que un usuario real tiene en disco al actualizar a Hermes.
func seedV5(t *testing.T, path string) {
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
		if m.version > 5 {
			break
		}
		if err := s.apply(m); err != nil {
			t.Fatalf("migración %d: %v", m.version, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (1, 'Caja', '/tmp/caja', 'bambu-a1', '', 5, 1, 1)`,
		`INSERT INTO chats (id, workspace_id, provider_id, model, title, created_at, updated_at) VALUES (1, 1, 'ollama', 'qwen3', 'Una caja', 10, 20)`,
		`INSERT INTO chat_messages (id, chat_id, role, content, created_at) VALUES (1, 1, 'user', 'haz una caja', 11)`,
		`INSERT INTO chat_messages (id, chat_id, role, content, created_at) VALUES (2, 1, 'assistant', 'listo', 12)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// La migración 6 es aditiva: una base de la versión 5 con mensajes sube a la 6,
// no pierde nada, los mensajes viejos quedan sin partes ni tokens y los
// campos nuevos se pueden escribir y leer. Abrirla otra vez no hace nada.
func TestMigration6AddsMessagePartsKeepingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	seedV5(t, path)

	for i := 0; i < 2; i++ { // la segunda apertura comprueba la idempotencia
		s, err := Open(path)
		if err != nil {
			t.Fatalf("apertura %d: %v", i, err)
		}
		if v, err := s.SchemaVersion(); err != nil || v < 6 {
			t.Fatalf("versión %d, %v (esperaba al menos 6)", v, err)
		}
		msgs, err := s.ListChatMessages(1)
		if err != nil || len(msgs) < 2 {
			t.Fatalf("mensajes: %+v, %v", msgs, err)
		}
		for _, m := range msgs[:2] {
			if m.Parts != "" || m.InputTokens != 0 || m.OutputTokens != 0 || m.CostUSD != 0 {
				t.Errorf("el mensaje viejo %d debía quedar con los defaults: %+v", m.ID, m)
			}
		}
		if msgs[0].Content != "haz una caja" || msgs[1].Role != "assistant" || msgs[1].Content != "listo" {
			t.Errorf("los mensajes cambiaron: %+v", msgs[:2])
		}
		if c, err := s.GetChat(1); err != nil || c.Title != "Una caja" || c.ProviderID != "ollama" || c.Model != "qwen3" {
			t.Errorf("el chat cambió: %+v, %v", c, err)
		}
		if i == 0 {
			m, err := s.AddChatMessageFull(1, NewChatMessage{Role: "assistant", Content: "hecho", Parts: `{"steps":[]}`, InputTokens: 120, OutputTokens: 45, CostUSD: 0.0123})
			if err != nil {
				t.Fatal(err)
			}
			if m.Parts != `{"steps":[]}` || m.InputTokens != 120 {
				t.Errorf("mensaje nuevo: %+v", m)
			}
		}
		s.Close()
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	msgs, _ := s.ListChatMessages(1)
	last := msgs[len(msgs)-1]
	if last.Content != "hecho" || last.Parts != `{"steps":[]}` || last.InputTokens != 120 || last.OutputTokens != 45 || last.CostUSD != 0.0123 {
		t.Errorf("lo guardado no persistió: %+v", last)
	}
	// role sigue siendo user|assistant: el CHECK de la migración 2 no cambió.
	if _, err := s.AddChatMessageFull(1, NewChatMessage{Role: "tool", Content: "x"}); err == nil {
		t.Error("el role 'tool' no debería guardarse")
	}
}

func TestRenameChatAndSetChatModel(t *testing.T) {
	s := openTemp(t)
	w := chatWorkspace(t, s)
	c, err := s.CreateChat(w.ID, "ollama", "qwen3")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatTitle(c.ID, "Automático"); err != nil {
		t.Fatal(err)
	}
	if err := s.RenameChat(c.ID, "Mi caja"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetChat(c.ID); got.Title != "Mi caja" {
		t.Errorf("RenameChat debe pisar el título automático: %q", got.Title)
	}
	if err := s.SetChatModel(c.ID, "anthropic", "claude-sonnet-5-5"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetChat(c.ID)
	if got.ProviderID != "anthropic" || got.Model != "claude-sonnet-5-5" || got.Title != "Mi caja" {
		t.Errorf("SetChatModel: %+v", got)
	}
	if err := s.RenameChat(9999, "x"); err != ErrNotFound {
		t.Errorf("RenameChat inexistente: %v", err)
	}
	if err := s.SetChatModel(9999, "a", "b"); err != ErrNotFound {
		t.Errorf("SetChatModel inexistente: %v", err)
	}
}
