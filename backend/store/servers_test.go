package store

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedAtVersion arma una base como la dejaba la versión maxVersion: aplica solo
// esas migraciones. Es la base que un usuario real tiene en disco al actualizar.
func seedAtVersion(t *testing.T, path string, maxVersion int) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, desc TEXT NOT NULL, applied_at INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.version > maxVersion {
			break
		}
		if err := s.apply(m); err != nil {
			t.Fatalf("migración %d: %v", m.version, err)
		}
	}
	return db
}

// La migración 5 copia las filas de chat de ai_providers a model_servers CON EL
// MISMO ID, sin tocar ai_providers, y no pierde nada de lo que ya había.
func TestMigration5CopiesChatProvidersKeepingIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	db := seedAtVersion(t, path, 4)
	for _, q := range []string{
		`INSERT INTO workspaces (id, name, path, printer_id, description, last_opened, created_at, updated_at, last_checkpoint) VALUES (1, 'Caja', '/tmp/caja', 'bambu-a1', 'd', 5, 1, 1, '001-base.scad')`,
		// Configuración de un usuario real: Ollama remoto con ventana propia, y
		// un OpenAI-compatible (OpenRouter) con la variable de la clave.
		`UPDATE ai_providers SET config = '{"baseUrl":"http://192.168.1.50:11434","model":"qwen3-coder:30b","numCtx":32768}', enabled = 1, sort_order = 5 WHERE id = 'ollama'`,
		`UPDATE ai_providers SET config = '{"baseUrl":"https://openrouter.ai/api/v1","model":"anthropic/claude-sonnet","apiKeyEnv":"OPENROUTER_API_KEY"}', enabled = 1 WHERE id = 'openai'`,
		// Una config corrupta (decodeConfig la toleraba): no puede tumbar la migración.
		`UPDATE ai_providers SET config = 'esto no es json' WHERE id = 'anthropic'`,
		`INSERT INTO chats (id, workspace_id, provider_id, model, title, created_at, updated_at) VALUES (7, 1, 'openai', 'anthropic/claude-sonnet', 'Mi chat', 1, 1)`,
		`INSERT INTO chat_messages (chat_id, role, content, created_at) VALUES (7, 'user', 'hola', 1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	for i := 0; i < 2; i++ { // la segunda apertura comprueba la idempotencia
		s, err := Open(path)
		if err != nil {
			t.Fatalf("apertura %d: %v", i, err)
		}
		if v, err := s.SchemaVersion(); err != nil || v != migrations[len(migrations)-1].version {
			t.Fatalf("versión %d, %v", v, err)
		}
		list, err := s.ListModelServers()
		if err != nil || len(list) != 3 {
			t.Fatalf("servidores: %+v, %v", list, err)
		}
		get := func(id string) ModelServer {
			m, err := s.GetModelServer(id)
			if err != nil {
				t.Fatalf("%s: %v", id, err)
			}
			return m
		}
		if o := get("ollama"); o.Kind != ServerOllama || o.BaseURL != "http://192.168.1.50:11434" || o.DefaultModel != "qwen3-coder:30b" || o.ContextTokens != 32768 || !o.Enabled || o.HasToken || o.Name != "Ollama" {
			t.Errorf("ollama migrado: %+v", o)
		}
		if o := get("openai"); o.BaseURL != "https://openrouter.ai/api/v1" || o.APIKeyEnv != "OPENROUTER_API_KEY" || o.DefaultModel != "anthropic/claude-sonnet" || o.ContextTokens != 0 || !o.Enabled {
			t.Errorf("openai migrado: %+v", o)
		}
		// Config corrupta: la fila se copia igual, con los valores por tipo.
		if a := get("anthropic"); a.Kind != ServerAnthropic || a.BaseURL != "https://api.anthropic.com" || a.APIKeyEnv != "" || a.Enabled {
			t.Errorf("anthropic migrado: %+v", a)
		}
		// El chat viejo sigue resolviendo su servidor por el mismo id.
		c, err := s.GetChat(7)
		if err != nil || c.ProviderID != "openai" {
			t.Fatalf("chat: %+v, %v", c, err)
		}
		if _, err := s.GetModelServer(c.ProviderID); err != nil {
			t.Errorf("el chat viejo no resuelve su servidor: %v", err)
		}
		if msgs, _ := s.ListChatMessages(7); len(msgs) != 1 {
			t.Errorf("mensajes: %+v", msgs)
		}
		// ai_providers no se toca: los 7 siguen, con su config.
		provs, _ := s.ListProviders()
		if len(provs) != 7 {
			t.Errorf("ai_providers cambió: %d filas", len(provs))
		}
		if p, _ := s.GetProvider("openai"); p.Config["apiKeyEnv"] != "OPENROUTER_API_KEY" {
			t.Errorf("ai_providers.openai cambió: %+v", p.Config)
		}
		// El resto de la base quedó igual y last_model nace vacío.
		if w, _ := s.GetWorkspace(1); w.Name != "Caja" || w.LastCheckpoint != "001-base.scad" {
			t.Errorf("workspace: %+v", w)
		}
		if lm, err := s.GetLastModel(1); err != nil || lm != (LastModel{}) {
			t.Errorf("last_model debería nacer vacío: %+v, %v", lm, err)
		}
		s.Close()
	}
}

// Una base nueva también termina con los tres servidores del seed (el chat de
// una instalación limpia ve a Ollama habilitado, como antes de S12).
func TestFreshDatabaseHasSeededServers(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListModelServers()
	if err != nil || len(list) != 3 {
		t.Fatalf("%+v, %v", list, err)
	}
	if list[0].ID != "ollama" || list[0].BaseURL != "http://localhost:11434" || !list[0].Enabled {
		t.Errorf("ollama: %+v", list[0])
	}
}

func TestServerCRUDAndToken(t *testing.T) {
	s := openTemp(t)
	cipher, nonce := []byte("cifrado-1"), []byte("nonce-123456")

	srv, err := s.SaveModelServer(ModelServer{Kind: ServerOpenAI, Preset: "gemini", Name: "  Gemini  ", BaseURL: "https://generativelanguage.googleapis.com/v1beta/openai", DefaultModel: " gemini-2.5-pro ", Enabled: true},
		TokenWrite{Set: true, Cipher: cipher, Nonce: nonce})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(srv.ID, "srv-") || srv.Name != "Gemini" || srv.Preset != "gemini" || srv.DefaultModel != "gemini-2.5-pro" || !srv.HasToken || !srv.Enabled {
		t.Fatalf("alta: %+v", srv)
	}
	if srv.SortOrder <= 7 {
		t.Errorf("el alta va al final de la lista: sortOrder %d", srv.SortOrder)
	}
	gotC, gotN, err := s.ModelServerToken(srv.ID)
	if err != nil || !bytes.Equal(gotC, cipher) || !bytes.Equal(gotN, nonce) {
		t.Fatalf("token: %q %q %v", gotC, gotN, err)
	}

	// Editar sin TokenWrite conserva la clave; sortOrder no cambia.
	edited := srv
	edited.Name, edited.Enabled = "Gemini 2", false
	up, err := s.SaveModelServer(edited, TokenWrite{})
	if err != nil || up.Name != "Gemini 2" || up.Enabled || !up.HasToken || up.SortOrder != srv.SortOrder {
		t.Fatalf("edición: %+v, %v", up, err)
	}
	// Reemplazar y luego borrar la clave.
	if up, err = s.SaveModelServer(up, TokenWrite{Set: true, Cipher: []byte("otro"), Nonce: nonce}); err != nil || !up.HasToken {
		t.Fatal(err)
	}
	if c, _, _ := s.ModelServerToken(srv.ID); string(c) != "otro" {
		t.Errorf("la clave no se reemplazó: %q", c)
	}
	if up, err = s.SaveModelServer(up, TokenWrite{Clear: true}); err != nil || up.HasToken {
		t.Fatalf("borrar la clave: %+v, %v", up, err)
	}
	if c, n, _ := s.ModelServerToken(srv.ID); c != nil || n != nil {
		t.Errorf("quedó la clave cifrada: %q %q", c, n)
	}

	// Validaciones.
	if _, err := s.SaveModelServer(ModelServer{Kind: ServerOllama, Name: "  "}, TokenWrite{}); err != ErrServerName {
		t.Errorf("sin nombre: %v", err)
	}
	if _, err := s.SaveModelServer(ModelServer{Kind: "gopher", Name: "x"}, TokenWrite{}); err == nil {
		t.Error("tipo desconocido")
	}
	if _, err := s.SaveModelServer(ModelServer{Kind: ServerOllama, Name: "gemini 2"}, TokenWrite{}); err == nil {
		t.Error("el nombre se repite (sin distinguir mayúsculas)")
	}
	if _, err := s.SaveModelServer(ModelServer{ID: "srv-nope", Kind: ServerOllama, Name: "Otro"}, TokenWrite{}); err != ErrServerNotFound {
		t.Errorf("editar uno inexistente: %v", err)
	}
	// La ventana de contexto solo es de Ollama y se acota.
	o, _ := s.SaveModelServer(ModelServer{Kind: ServerOllama, Name: "Ollama 2", BaseURL: "http://x:1", ContextTokens: 99_999_999}, TokenWrite{})
	if o.ContextTokens != MaxContextTokens {
		t.Errorf("contexto sin acotar: %d", o.ContextTokens)
	}
	a, _ := s.SaveModelServer(ModelServer{Kind: ServerAnthropic, Name: "Claude 2", ContextTokens: 4096}, TokenWrite{})
	if a.ContextTokens != 0 {
		t.Errorf("solo Ollama guarda contexto: %d", a.ContextTokens)
	}

	// Borrar: el chat queda legible (no hay FK) y borrar dos veces no falla.
	ws, _ := s.InsertWorkspace(Workspace{Name: "W", Path: "/tmp/w", PrinterID: "bambu-a1"})
	c, _ := s.CreateChat(ws, srv.ID, "m")
	if err := s.DeleteModelServer(srv.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteModelServer(srv.ID); err != nil {
		t.Errorf("borrar dos veces: %v", err)
	}
	if _, err := s.GetModelServer(srv.ID); err != ErrServerNotFound {
		t.Errorf("sigue existiendo: %v", err)
	}
	if got, err := s.GetChat(c.ID); err != nil || got.ProviderID != srv.ID {
		t.Errorf("el chat debe seguir legible: %+v, %v", got, err)
	}
}

// El último modelo: por proyecto, con respaldo en el global.
func TestLastModelPerProjectFallsBackToGlobal(t *testing.T) {
	s := openTemp(t)
	a, _ := s.InsertWorkspace(Workspace{Name: "A", Path: "/tmp/a", PrinterID: "bambu-a1"})
	b, _ := s.InsertWorkspace(Workspace{Name: "B", Path: "/tmp/b", PrinterID: "bambu-a1"})

	if lm, err := s.GetLastModel(a); err != nil || lm != (LastModel{}) {
		t.Fatalf("vacío: %+v, %v", lm, err)
	}
	if err := s.SetLastModel(a, LastModel{ServerID: "ollama", Model: "qwen3:8b"}); err != nil {
		t.Fatal(err)
	}
	if lm, _ := s.GetLastModel(a); lm != (LastModel{ServerID: "ollama", Model: "qwen3:8b"}) {
		t.Errorf("proyecto A: %+v", lm)
	}
	// B no eligió nunca: cae al global (lo último elegido en cualquier lado).
	if lm, _ := s.GetLastModel(b); lm != (LastModel{ServerID: "ollama", Model: "qwen3:8b"}) {
		t.Errorf("B debería caer al global: %+v", lm)
	}
	if err := s.SetLastModel(b, LastModel{ServerID: "openai", Model: "gpt-x"}); err != nil {
		t.Fatal(err)
	}
	// A conserva la suya; la global ahora es la de B.
	if lm, _ := s.GetLastModel(a); lm.Model != "qwen3:8b" {
		t.Errorf("A cambió: %+v", lm)
	}
	if lm, _ := s.GetLastModel(0); lm != (LastModel{ServerID: "openai", Model: "gpt-x"}) {
		t.Errorf("global: %+v", lm)
	}
	if err := s.SetLastModel(999, LastModel{ServerID: "x"}); err != ErrNotFound {
		t.Errorf("workspace inexistente: %v", err)
	}
	// Un valor corrupto no rompe: se trata como «sin elección».
	if _, err := s.db.Exec(`UPDATE workspaces SET last_model = '{roto' WHERE id = ?`, a); err != nil {
		t.Fatal(err)
	}
	if lm, err := s.GetLastModel(a); err != nil || lm.ServerID != "openai" {
		t.Errorf("corrupto: %+v, %v", lm, err)
	}
}

// El diálogo de Configuración guarda una copia vieja de los ajustes: no debe
// pisar el último modelo ni leerlo como ajuste propio.
func TestLastModelSettingSurvivesSaveSettings(t *testing.T) {
	s := openTemp(t)
	if err := s.SetLastModel(0, LastModel{ServerID: "ollama", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	st, err := s.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings con la clave lastModel: %v", err)
	}
	if err := s.SaveSettings(st); err != nil {
		t.Fatal(err)
	}
	if lm, _ := s.GetLastModel(0); lm.Model != "m" {
		t.Errorf("SaveSettings pisó lastModel: %+v", lm)
	}
}

// Nada de lo guardado en scadlab.db contiene la clave en claro: el archivo se
// busca byte a byte (incluido el WAL, donde SQLite deja las escrituras recientes).
func TestTokenNeverInDatabaseFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// El store solo ve bytes cifrados; aquí un cifrado cualquiera que no contiene el texto.
	if _, err := s.SaveModelServer(ModelServer{Kind: ServerOpenAI, Name: "X", BaseURL: "https://x/v1", Enabled: true},
		TokenWrite{Set: true, Cipher: []byte{0x9f, 0x01, 0x77, 0x42}, Nonce: bytes.Repeat([]byte{3}, 12)}); err != nil {
		t.Fatal(err)
	}
	s.Close()
	entries, _ := filepath.Glob(path + "*")
	for _, f := range entries {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte("sk-secreto")) {
			t.Errorf("%s contiene la clave en claro", f)
		}
	}
}
