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
	// CLISession es el id de conversación del CLI agéntico con que sigue el chat
	// (provider_id `cli:*`); vacío si no hay o si el chat es de un servidor.
	CLISession string `json:"cliSession"`
}

// ChatMessage es un mensaje del chat: role es "user" o "assistant". Los pasos de
// herramienta de Hermes no son filas aparte (el CHECK de role no se puede
// ampliar de forma aditiva): viajan dentro de Parts.
type ChatMessage struct {
	ID        int64  `json:"id"`
	ChatID    int64  `json:"chatId"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"createdAt"`
	// Parts es un JSON opaco para este paquete: en un mensaje del usuario, los
	// metadatos de sus adjuntos; en uno del asistente, los pasos del turno. ""
	// = nada (los mensajes anteriores a Hermes).
	Parts string `json:"parts"`
	// Tokens y costo del turno que cerró este mensaje. 0 = el servidor no lo
	// informó (CostUSD solo existe si el proveedor publica precios).
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	CostUSD      float64 `json:"costUsd"`
}

// NewChatMessage es lo que se guarda al agregar un mensaje.
type NewChatMessage struct {
	Role         string
	Content      string
	Parts        string
	InputTokens  int64
	OutputTokens int64
	CostUSD      float64
}

const chatCols = `id, workspace_id, provider_id, model, title, created_at, updated_at, cli_session`

func scanChat(sc interface{ Scan(...any) error }) (Chat, error) {
	var c Chat
	err := sc.Scan(&c.ID, &c.WorkspaceID, &c.ProviderID, &c.Model, &c.Title, &c.CreatedAt, &c.UpdatedAt, &c.CLISession)
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

// RenameChat pone el título del chat. A diferencia de SetChatTitle (que solo
// completa un título vacío) pisa el que haya: es el nombre que eligió la
// persona. quien llama valida el texto. ErrNotFound si el chat no existe.
func (s *Store) RenameChat(id int64, title string) error {
	res, err := s.db.Exec(`UPDATE chats SET title = ? WHERE id = ?`, title, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetChatModel cambia el servidor y el modelo con que sigue el chat. No toca
// updated_at: elegir otro modelo no es actividad de la conversación. Si cambia el
// proveedor, la sesión del CLI se olvida: su id solo vale para el CLI que lo dio.
func (s *Store) SetChatModel(id int64, providerID, model string) error {
	res, err := s.db.Exec(`UPDATE chats SET provider_id = ?, model = ?,
		cli_session = CASE WHEN provider_id = ? THEN cli_session ELSE '' END WHERE id = ?`, providerID, model, providerID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetChatCLISession guarda el id de conversación del CLI con que sigue el chat
// ("" lo olvida: la próxima vez arranca una conversación nueva). No toca
// updated_at.
func (s *Store) SetChatCLISession(id int64, session string) error {
	res, err := s.db.Exec(`UPDATE chats SET cli_session = ? WHERE id = ?`, session, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

const messageCols = `id, chat_id, role, content, created_at, parts, input_tokens, output_tokens, cost_usd`

// ListChatMessages devuelve la conversación en orden cronológico.
func (s *Store) ListChatMessages(chatID int64) ([]ChatMessage, error) {
	rows, err := s.db.Query(`SELECT `+messageCols+` FROM chat_messages WHERE chat_id = ? ORDER BY id`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChatMessage{}
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.ChatID, &m.Role, &m.Content, &m.CreatedAt, &m.Parts, &m.InputTokens, &m.OutputTokens, &m.CostUSD); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddChatMessage agrega un mensaje de solo texto (ver AddChatMessageFull).
func (s *Store) AddChatMessage(chatID int64, role, content string) (ChatMessage, error) {
	return s.AddChatMessageFull(chatID, NewChatMessage{Role: role, Content: content})
}

// AddChatMessageFull agrega un mensaje y sube el chat al tope de la lista
// (updated_at). Las dos escrituras van en una transacción: un mensaje sin su
// fecha de chat dejaría el listado desordenado.
func (s *Store) AddChatMessageFull(chatID int64, m NewChatMessage) (ChatMessage, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ChatMessage{}, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op tras Commit
	t := now()
	role, content := m.Role, m.Content
	res, err := tx.Exec(`INSERT INTO chat_messages (chat_id, role, content, created_at, parts, input_tokens, output_tokens, cost_usd) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		chatID, role, content, t, m.Parts, m.InputTokens, m.OutputTokens, m.CostUSD)
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
	return ChatMessage{ID: id, ChatID: chatID, Role: role, Content: content, CreatedAt: t,
		Parts: m.Parts, InputTokens: m.InputTokens, OutputTokens: m.OutputTokens, CostUSD: m.CostUSD}, nil
}
