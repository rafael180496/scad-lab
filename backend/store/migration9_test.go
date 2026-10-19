package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// seedV8 arma una base como la dejaba la versión 8 (sesión del CLI por chat):
// aplica solo las migraciones 1–8 y registra un workspace con un chat de un
// servidor, un chat de un CLI con su sesión y mensajes. Es la base que un usuario
// real tiene al actualizar a la versión con el modo por conversación.
func seedV8(t *testing.T, path string) {
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
		if m.version > 8 {
			break
		}
		if err := s.apply(m); err != nil {
			t.Fatalf("migración %d: %v", m.version, err)
		}
	}
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at) VALUES (1, 'Caja', '/tmp/caja', 'bambu-a1', '', 5, 1, 1)`,
		`INSERT INTO chats (id, workspace_id, provider_id, model, title, created_at, updated_at, cli_session) VALUES (1, 1, 'ollama', 'qwen3', 'Una caja', 10, 20, '')`,
		`INSERT INTO chats (id, workspace_id, provider_id, model, title, created_at, updated_at, cli_session) VALUES (2, 1, 'cli:claude', '', 'Con Claude', 11, 21, 'sesion-9')`,
		`INSERT INTO chat_messages (id, chat_id, role, content, created_at, parts, input_tokens, output_tokens, cost_usd) VALUES (1, 1, 'user', 'haz una caja', 11, '', 0, 0, 0)`,
		`INSERT INTO chat_messages (id, chat_id, role, content, created_at, parts, input_tokens, output_tokens, cost_usd) VALUES (2, 1, 'assistant', 'listo', 12, '{"steps":[]}', 120, 45, 0.01)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

// La migración 9 es aditiva: una base de la versión 8 con chats y mensajes sube a
// la 9 sin perder nada, TODAS las conversaciones existentes quedan en «Agente»
// (mode vacío; ninguna conserva un «Solo consulta» que nadie eligió) y el campo se
// escribe y se lee. Abrirla otra vez no hace nada.
func TestMigration9AddsChatModeKeepingRowsInAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	seedV8(t, path)

	for i := 0; i < 2; i++ { // la segunda apertura comprueba la idempotencia
		s, err := Open(path)
		if err != nil {
			t.Fatalf("apertura %d: %v", i, err)
		}
		if v, err := s.SchemaVersion(); err != nil || v < 9 {
			t.Fatalf("versión %d, %v (esperaba al menos 9)", v, err)
		}
		list, err := s.ListChats(1)
		if err != nil || len(list) != 2 {
			t.Fatalf("chats: %+v, %v", list, err)
		}
		for _, c := range list {
			if i == 0 && !c.Agent() {
				t.Errorf("el chat %d debía quedar en Agente: %+v", c.ID, c)
			}
		}
		if i == 0 {
			if c, _ := s.GetChat(1); c.Mode != "" || c.Title != "Una caja" || c.ProviderID != "ollama" || c.Model != "qwen3" {
				t.Errorf("el chat viejo debía quedar igual y sin modo: %+v", c)
			}
			if c, _ := s.GetChat(2); c.CLISession != "sesion-9" {
				t.Errorf("la sesión del CLI se perdió: %+v", c)
			}
			if err := s.SetChatMode(1, "ask"); err != nil {
				t.Fatal(err)
			}
		}
		msgs, _ := s.ListChatMessages(1)
		if len(msgs) != 2 || msgs[1].Content != "listo" || msgs[1].Parts != `{"steps":[]}` || msgs[1].InputTokens != 120 {
			t.Errorf("los mensajes cambiaron: %+v", msgs)
		}
		s.Close()
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if c, _ := s.GetChat(1); c.Agent() || c.Mode != ChatModeAsk {
		t.Errorf("la elección no persistió: %+v", c)
	}
	if c, _ := s.GetChat(2); !c.Agent() {
		t.Errorf("elegir consulta en un chat no cambia otro: %+v", c)
	}
}
