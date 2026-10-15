package store

// Chat es una conversación con un modelo, dentro de un workspace.
type Chat struct {
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspaceId"`
	ProviderID  string `json:"providerId"`
	Model       string `json:"model"`
	Title       string `json:"title"`
	CreatedAt   int64  `json:"createdAt"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// ChatMessage es un mensaje del chat: role es "user" o "assistant".
type ChatMessage struct {
	ID        int64  `json:"id"`
	ChatID    int64  `json:"chatId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"createdAt"`
}

const chatCols = `id, workspace_id, provider_id, model, title, created_at, updated_at`

func scanChat(sc interface{ Scan(...any) error }) (Chat, error) {
	var c Chat
	err := sc.Scan(&c.ID, &c.WorkspaceID, &c.ProviderID, &c.Model, &c.Title, &c.CreatedAt, &c.UpdatedAt)
	return c, err
}

// CreateChat abre un chat vacío (sin título: se pone con el primer mensaje).
func (s *Store) CreateChat(workspaceID int64, providerID, model string) (Chat, error) {
	t := now()
	res, err := s.db.Exec(`INSERT INTO chats (workspace_id, provider_id, model, title, created_at, updated_at) VALUES (?, ?, ?, '', ?, ?)`,
		workspaceID, providerID, model, t, t)
	if err != nil {
		return Chat{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Chat{}, err
	}
	return s.GetChat(id)
}

// GetChat lee uno por id.
func (s *Store) GetChat(id int64) (Chat, error) {
	c, err := scanChat(s.db.QueryRow(`SELECT `+chatCols+` FROM chats WHERE id = ?`, id))
	return c, notFound(err)
}

// ListChats devuelve los chats del workspace, el más reciente primero.
func (s *Store) ListChats(workspaceID int64) ([]Chat, error) {
	rows, err := s.db.Query(`SELECT `+chatCols+` FROM chats WHERE workspace_id = ? ORDER BY updated_at DESC, id DESC`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Chat{}
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteChat borra el chat y, en cascada, sus mensajes. Borrar uno que no
// existe no es un error.
func (s *Store) DeleteChat(id int64) error {
	_, err := s.db.Exec(`DELETE FROM chats WHERE id = ?`, id)
	return err
}

// SetChatTitle pone el título solo si todavía no tiene: el primer mensaje lo
// define y un reintento no lo pisa.
func (s *Store) SetChatTitle(id int64, title string) error {
	_, err := s.db.Exec(`UPDATE chats SET title = ? WHERE id = ? AND title = ''`, title, id)
	return err
}

// ListChatMessages devuelve la conversación en orden cronológico.
func (s *Store) ListChatMessages(chatID int64) ([]ChatMessage, error) {
	rows, err := s.db.Query(`SELECT id, chat_id, role, content, created_at FROM chat_messages WHERE chat_id = ? ORDER BY id`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddChatMessage agrega un mensaje y sube el chat al tope de la lista
// (updated_at). Las dos escrituras van en una transacción: un mensaje sin su
// fecha de chat dejaría el listado desordenado.
func (s *Store) AddChatMessage(chatID int64, role, content string) (ChatMessage, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ChatMessage{}, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op tras Commit
	t := now()
	res, err := tx.Exec(`INSERT INTO chat_messages (chat_id, role, content, created_at) VALUES (?, ?, ?, ?)`, chatID, role, content, t)
	if err != nil {
		return ChatMessage{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return ChatMessage{}, err
	}
	if _, err := tx.Exec(`UPDATE chats SET updated_at = ? WHERE id = ?`, t, chatID); err != nil {
		return ChatMessage{}, err
	}
	if err := tx.Commit(); err != nil {
		return ChatMessage{}, err
	}
	return ChatMessage{ID: id, ChatID: chatID, Role: role, Content: content, CreatedAt: t}, nil
}
