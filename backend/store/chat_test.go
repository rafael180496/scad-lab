package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func chatWorkspace(t *testing.T, s *Store) Workspace {
	t.Helper()
	return insertWS(t, s, "Caja")
}

func insertWS(t *testing.T, s *Store, name string) Workspace {
	t.Helper()
	w := Workspace{Name: name, Path: filepath.Join(t.TempDir(), name), PrinterID: "bambu-a1-mini"}
	id, err := s.InsertWorkspace(w)
	if err != nil {
		t.Fatal(err)
	}
	w.ID = id
	return w
}

func TestChatLifecycle(t *testing.T) {
	s := openTemp(t)
	w := chatWorkspace(t, s)

	c, err := s.CreateChat(w.ID, "ollama", "qwen3")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == 0 || c.Title != "" || c.ProviderID != "ollama" || c.Model != "qwen3" || c.WorkspaceID != w.ID {
		t.Fatalf("chat: %+v", c)
	}
	if err := s.SetChatTitle(c.ID, "Primero"); err != nil {
		t.Fatal(err)
	}
	// El título lo define el primer mensaje: uno posterior no lo pisa.
	if err := s.SetChatTitle(c.ID, "Segundo"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetChat(c.ID); got.Title != "Primero" {
		t.Errorf("título: %q", got.Title)
	}

	u, err := s.AddChatMessage(c.ID, "user", "hola")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.AddChatMessage(c.ID, "assistant", "```openscad\ncube(10);\n```")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID <= u.ID {
		t.Errorf("ids: %d, %d", u.ID, a.ID)
	}
	msgs, err := s.ListChatMessages(c.ID)
	if err != nil || len(msgs) != 2 || msgs[0].Role != "user" || msgs[1].Content != "```openscad\ncube(10);\n```" {
		t.Fatalf("mensajes: %+v, %v", msgs, err)
	}

	// Un role inventado lo rechaza el CHECK de la tabla.
	if _, err := s.AddChatMessage(c.ID, "system", "x"); err == nil {
		t.Error("el role 'system' no debería guardarse")
	}
}

func TestListChatsOrderAndIsolation(t *testing.T) {
	s := openTemp(t)
	w1 := chatWorkspace(t, s)
	w2 := insertWS(t, s, "Otra")
	a, _ := s.CreateChat(w1.ID, "ollama", "m")
	b, _ := s.CreateChat(w1.ID, "ollama", "m")
	other, _ := s.CreateChat(w2.ID, "ollama", "m")
	// El updated_at es en segundos: se fuerza para no depender del reloj.
	if _, err := s.db.Exec(`UPDATE chats SET updated_at = 100 WHERE id = ?`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE chats SET updated_at = 50 WHERE id = ?`, b.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.ListChats(w1.ID)
	if err != nil || len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("orden: %+v, %v", got, err)
	}
	if got, _ := s.ListChats(w2.ID); len(got) != 1 || got[0].ID != other.ID {
		t.Errorf("aislamiento: %+v", got)
	}
	// Escribir en el más viejo lo sube al tope.
	if _, err := s.AddChatMessage(b.ID, "user", "x"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListChats(w1.ID); got[0].ID != b.ID {
		t.Errorf("tras escribir: %+v", got)
	}
}

func TestChatCascade(t *testing.T) {
	s := openTemp(t)
	w := chatWorkspace(t, s)
	c, _ := s.CreateChat(w.ID, "ollama", "m")
	if _, err := s.AddChatMessage(c.ID, "user", "hola"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteChat(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetChat(c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetChat: %v", err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM chat_messages`).Scan(&n); err != nil || n != 0 {
		t.Errorf("mensajes huérfanos: %d, %v", n, err)
	}
	// Borrar uno inexistente no es un error.
	if err := s.DeleteChat(9999); err != nil {
		t.Error(err)
	}

	// Quitar el workspace se lleva sus chats.
	c2, _ := s.CreateChat(w.ID, "ollama", "m")
	if err := s.RemoveWorkspace(w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetChat(c2.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("el chat debería irse con su workspace: %v", err)
	}
}

// Migración 2 sobre una base que ya estaba en la versión 1: aditiva, conserva
// lo existente.
func TestMigration2IsAdditive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scadlab.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	w := chatWorkspace(t, s)
	// Simula una base de antes del chat: sin las tablas ni las migraciones que
	// las crearon o les agregaron columnas (2: tablas; 6: partes y tokens de los
	// mensajes; 8: la sesión del CLI de los chats; 9: el modo de cada chat).
	for _, q := range []string{`DROP TABLE chat_messages`, `DROP TABLE chats`, `DELETE FROM schema_migrations WHERE version IN (2, 6, 8, 9)`} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.SchemaVersion(); v != migrations[len(migrations)-1].version {
		t.Errorf("versión: %d", v)
	}
	if got, err := s.GetWorkspace(w.ID); err != nil || got.Name != "Caja" {
		t.Errorf("el workspace tiene que seguir: %+v, %v", got, err)
	}
	if _, err := s.CreateChat(w.ID, "ollama", "m"); err != nil {
		t.Error(err)
	}
}

// Una conversación nueva nace en «Agente»; elegir «Solo consulta» afecta solo a
// esa conversación, se puede volver, no mueve el orden del listado y un modo que
// no existe no cambia nada.
func TestSetChatModeIsPerConversation(t *testing.T) {
	s := openTemp(t)
	w := chatWorkspace(t, s)
	a, err := s.CreateChat(w.ID, "ollama", "qwen3")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateChat(w.ID, "cli:claude", "")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Agent() || !b.Agent() || a.Mode != "" || b.Mode != "" {
		t.Fatalf("un chat nuevo nace en Agente: %+v %+v", a, b)
	}
	before, _ := s.GetChat(a.ID)

	if err := s.SetChatMode(a.ID, "ask"); err != nil {
		t.Fatal(err)
	}
	ga, _ := s.GetChat(a.ID)
	gb, _ := s.GetChat(b.ID)
	if ga.Agent() || ga.Mode != ChatModeAsk || !gb.Agent() {
		t.Errorf("elegir consulta en uno no cambia el otro: a=%+v b=%+v", ga, gb)
	}
	if ga.UpdatedAt != before.UpdatedAt {
		t.Error("cambiar de modo no es actividad del chat: no toca updated_at")
	}
	list, _ := s.ListChats(w.ID)
	modes := map[int64]string{}
	for _, c := range list {
		modes[c.ID] = c.Mode
	}
	if modes[a.ID] != ChatModeAsk || modes[b.ID] != "" {
		t.Errorf("ListChats trae el modo de cada uno: %v", modes)
	}

	// Volver a Agente: "agent" y "" se guardan como la columna vacía.
	if err := s.SetChatMode(a.ID, "agent"); err != nil {
		t.Fatal(err)
	}
	if ga, _ := s.GetChat(a.ID); ga.Mode != "" || !ga.Agent() {
		t.Errorf("de vuelta a Agente: %+v", ga)
	}
	_ = s.SetChatMode(a.ID, "ask")
	if err := s.SetChatMode(a.ID, ""); err != nil {
		t.Fatal(err)
	}
	if ga, _ := s.GetChat(a.ID); !ga.Agent() {
		t.Errorf("la cadena vacía también es Agente: %+v", ga)
	}

	// Un modo inventado no se guarda; un chat inexistente es ErrNotFound.
	_ = s.SetChatMode(a.ID, "ask")
	if err := s.SetChatMode(a.ID, "plan"); err == nil {
		t.Error("un modo desconocido debía fallar")
	}
	if ga, _ := s.GetChat(a.ID); ga.Mode != ChatModeAsk {
		t.Errorf("un modo inválido no cambia el guardado: %+v", ga)
	}
	if err := s.SetChatMode(999, "ask"); err != ErrNotFound {
		t.Errorf("chat inexistente: %v", err)
	}
}
