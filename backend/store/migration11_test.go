package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// seedV10 arma una base como la dejaba la versión 10: solo las migraciones 1–10, con
// un chat de un servidor y otro de un CLI y mensajes.
func seedV10(t *testing.T, path string) {
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
		if m.version > 10 {
			break
		}
		if err := s.apply(m); err != nil {
			t.Fatalf("migración %d: %v", m.version, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (1, 'Maceta', '/tmp/maceta', 'bambu-a1', '', 5, 1, 1)`,
		`INSERT INTO chats (id, workspace_id, provider_id, model, title, created_at, updated_at, cli_session, mode) VALUES (1, 1, 'ollama', 'qwen3', 'Una maceta', 10, 20, '', 'ask')`,
		`INSERT INTO chats (id, workspace_id, provider_id, model, title, created_at, updated_at, cli_session, mode) VALUES (2, 1, 'cli:claude', '', 'Con Claude', 11, 21, 'sesion-9', '')`,
		`INSERT INTO chat_messages (id, chat_id, role, content, created_at, parts, input_tokens, output_tokens, cost_usd) VALUES (1, 1, 'user', 'haz una maceta', 11, '', 0, 0, 0)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// La migración 11 es aditiva: una base de la versión 10 sube sin perder nada, toda
// conversación queda en automático (tools vacío), el perfil se guarda por
// conversación, un valor desconocido se rechaza y abrirla otra vez no hace nada.
func TestMigration11AddsChatToolsKeepingRowsAutomatic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	seedV10(t, path)

	for i := 0; i < 2; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("apertura %d: %v", i, err)
		}
		if v, err := s.SchemaVersion(); err != nil || v < 11 {
			t.Fatalf("versión %d, %v (esperaba al menos 11)", v, err)
		}
		if i == 0 {
			c, err := s.GetChat(1)
			if err != nil || c.Tools != "" || c.Mode != ChatModeAsk || c.Title != "Una maceta" {
				t.Fatalf("el chat viejo debía quedar igual y automático: %+v, %v", c, err)
			}
			if c, _ := s.GetChat(2); c.Tools != "" || c.CLISession != "sesion-9" {
				t.Errorf("el chat del CLI cambió: %+v", c)
			}
			if err := s.SetChatTools(1, "essential"); err != nil {
				t.Fatal(err)
			}
		}
		s.Close()
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if c, _ := s.GetChat(1); c.Tools != ChatToolsEssential {
		t.Errorf("la elección no persistió: %+v", c)
	}
	if c, _ := s.GetChat(2); c.Tools != "" {
		t.Errorf("elegir en un chat no cambia otro: %+v", c)
	}
	if err := s.SetChatTools(1, "auto"); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.GetChat(1); c.Tools != "" {
		t.Errorf("auto debe guardar vacío: %+v", c)
	}
	if err := s.SetChatTools(1, "todas"); err == nil {
		t.Error("un perfil desconocido debía rechazarse")
	}
	if err := s.SetChatTools(999, "full"); err != ErrNotFound {
		t.Errorf("chat inexistente: %v", err)
	}
	if c, _ := s.GetChat(1); c.Tools != "" {
		t.Errorf("un perfil rechazado no debe cambiar nada: %+v", c)
	}
}
