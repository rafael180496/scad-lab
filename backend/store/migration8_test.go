package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// seedV7 arma una base como la dejaba la versión 7 (llavero propio): aplica solo
// las migraciones 1–7 y registra un workspace con un chat de Ollama, un chat de
// un servidor y dos mensajes. Es la base que un usuario real tiene al actualizar.
func seedV7(t *testing.T, path string) {
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
		if m.version > 7 {
			break
		}
		if err := s.apply(m); err != nil {
			t.Fatalf("migración %d: %v", m.version, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (1, 'Caja', '/tmp/caja', 'bambu-a1', '', 5, 1, 1)`,
		`INSERT INTO chats (id, workspace_id, provider_id, model, title, created_at, updated_at) VALUES (1, 1, 'ollama', 'qwen3', 'Una caja', 10, 20)`,
		`INSERT INTO chat_messages (id, chat_id, role, content, created_at, parts, input_tokens, output_tokens, cost_usd) VALUES (1, 1, 'user', 'haz una caja', 11, '', 0, 0, 0)`,
		`INSERT INTO chat_messages (id, chat_id, role, content, created_at, parts, input_tokens, output_tokens, cost_usd) VALUES (2, 1, 'assistant', 'listo', 12, '{"steps":[]}', 120, 45, 0.01)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// La migración 8 es aditiva: una base de la versión 7 sube a la 8 sin perder
// nada, los chats viejos quedan sin sesión de CLI y el campo se escribe y se lee.
// Abrirla otra vez no hace nada.
func TestMigration8AddsCLISessionKeepingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	seedV7(t, path)

	for i := 0; i < 2; i++ { // la segunda apertura comprueba la idempotencia
		s, err := Open(path)
		if err != nil {
			t.Fatalf("apertura %d: %v", i, err)
		}
		if v, err := s.SchemaVersion(); err != nil || v < 8 {
			t.Fatalf("versión %d, %v (esperaba al menos 8)", v, err)
		}
		c, err := s.GetChat(1)
		if err != nil {
			t.Fatal(err)
		}
		if (i == 0 && c.CLISession != "") || c.Title != "Una caja" || c.ProviderID != "ollama" || c.Model != "qwen3" {
			t.Errorf("el chat viejo debía quedar igual y, de entrada, sin sesión: %+v", c)
		}
		msgs, _ := s.ListChatMessages(1)
		if len(msgs) != 2 || msgs[1].Content != "listo" || msgs[1].Parts != `{"steps":[]}` || msgs[1].InputTokens != 120 {
			t.Errorf("los mensajes cambiaron: %+v", msgs)
		}
		if i == 0 {
			if err := s.SetChatCLISession(1, "9cb82bf7-284f-4617-a552-dfe908f87b7f"); err != nil {
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
	c, _ := s.GetChat(1)
	if c.CLISession != "9cb82bf7-284f-4617-a552-dfe908f87b7f" {
		t.Errorf("la sesión no persistió: %+v", c)
	}
	list, _ := s.ListChats(1)
	if len(list) != 1 || list[0].CLISession != c.CLISession {
		t.Errorf("ListChats también la trae: %+v", list)
	}
	if err := s.SetChatCLISession(999, "x"); err != ErrNotFound {
		t.Errorf("chat inexistente: %v", err)
	}
}

// Cambiar de modelo dentro del mismo CLI conserva su sesión; cambiar de
// proveedor la olvida (su id solo vale para el CLI que lo dio).
func TestSetChatModelForgetsTheCLISessionOnlyWhenTheProviderChanges(t *testing.T) {
	s := openTemp(t)
	w := chatWorkspace(t, s)
	c, err := s.CreateChat(w.ID, "cli:claude", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatCLISession(c.ID, "sesion-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetChatModel(c.ID, "cli:claude", "opus"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetChat(c.ID); got.CLISession != "sesion-1" || got.Model != "opus" {
		t.Errorf("mismo CLI, otro modelo: %+v", got)
	}
	if err := s.SetChatModel(c.ID, "cli:codex", ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetChat(c.ID); got.CLISession != "" || got.ProviderID != "cli:codex" {
		t.Errorf("otro CLI: %+v", got)
	}
	// De un CLI a un servidor, y de vuelta.
	_ = s.SetChatCLISession(c.ID, "sesion-2")
	if err := s.SetChatModel(c.ID, "ollama", "qwen3"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetChat(c.ID); got.CLISession != "" {
		t.Errorf("a un servidor: %+v", got)
	}
	// Una sesión vacía la olvida.
	_ = s.SetChatCLISession(c.ID, "sesion-3")
	_ = s.SetChatCLISession(c.ID, "")
	if got, _ := s.GetChat(c.ID); got.CLISession != "" {
		t.Errorf("%+v", got)
	}
}
