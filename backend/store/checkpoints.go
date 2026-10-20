package store

import (
	"database/sql"
	"errors"
)

// Checkpoint es la fila del índice de un .scad del workspace.
type Checkpoint struct {
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspaceId"`
	FileName    string `json:"fileName"`
	ParentFile  string `json:"parentFile"`
	Label       string `json:"label"`
	Description string `json:"description"`
	SessionID   string `json:"sessionId"` // sesión del agente que lo creó ("" si no se sabe)
	AgentID     string `json:"agentId"`   // agente (ai_providers.id) que lo creó
	ChatID      int64  `json:"chatId"`    // chat de diseño desde el que se guardó (0 = ninguno)
	Collapsed   bool   `json:"collapsed"`
	FileMtime   int64  `json:"fileMtime"`
	CreatedAt   int64  `json:"createdAt"`
}

const checkpointCols = `id, workspace_id, file_name, parent_file, label, description, session_id, agent_id, chat_id, collapsed, file_mtime, created_at`

func scanCheckpoint(sc interface{ Scan(...any) error }) (Checkpoint, error) {
	var c Checkpoint
	err := sc.Scan(&c.ID, &c.WorkspaceID, &c.FileName, &c.ParentFile, &c.Label, &c.Description, &c.SessionID, &c.AgentID, &c.ChatID, &c.Collapsed, &c.FileMtime, &c.CreatedAt)
	return c, err
}

// ListCheckpoints devuelve el índice de un workspace ordenado por archivo
// (NNN-slug.scad ordena cronológicamente por su prefijo).
func (s *Store) ListCheckpoints(workspaceID int64) ([]Checkpoint, error) {
	rows, err := s.db.Query(`SELECT `+checkpointCols+`
		FROM checkpoints WHERE workspace_id = ? ORDER BY file_name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Checkpoint{}
	for rows.Next() {
		c, err := scanCheckpoint(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SyncCheckpoints reemplaza el índice de un workspace con lo que se leyó del
// disco, conservando lo que es solo de la UI (label, collapsed, session_id,
// agent_id, chat_id) para los archivos que siguen existiendo: el ON CONFLICT
// solo actualiza lo que sale del archivo.
func (s *Store) SyncCheckpoints(workspaceID int64, found []Checkpoint) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	keep := make([]any, 0, len(found)+1)
	keep = append(keep, workspaceID)
	ph := ""
	for i, c := range found {
		if _, err := tx.Exec(`INSERT INTO checkpoints (workspace_id, file_name, parent_file, description, file_mtime, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(workspace_id, file_name) DO UPDATE SET
				parent_file = excluded.parent_file,
				description = excluded.description,
				file_mtime  = excluded.file_mtime`,
			workspaceID, c.FileName, c.ParentFile, c.Description, c.FileMtime, now()); err != nil {
			return err
		}
		if i > 0 {
			ph += ","
		}
		ph += "?"
		keep = append(keep, c.FileName)
	}
	del := `DELETE FROM checkpoints WHERE workspace_id = ?`
	if ph != "" {
		del += ` AND file_name NOT IN (` + ph + `)`
	}
	if _, err := tx.Exec(del, keep...); err != nil {
		return err
	}
	return tx.Commit()
}

// SetCheckpointUI guarda el estado de UI de un checkpoint (rótulo, plegado).
func (s *Store) SetCheckpointUI(workspaceID int64, fileName, label string, collapsed bool) error {
	_, err := s.db.Exec(`UPDATE checkpoints SET label = ?, collapsed = ? WHERE workspace_id = ? AND file_name = ?`,
		label, collapsed, workspaceID, fileName)
	return err
}

// GetCheckpoint lee la fila de un checkpoint; ErrNotFound si todavía no está en
// el índice (un archivo recién creado que el rescan no vio).
func (s *Store) GetCheckpoint(workspaceID int64, fileName string) (Checkpoint, error) {
	c, err := scanCheckpoint(s.db.QueryRow(`SELECT `+checkpointCols+` FROM checkpoints WHERE workspace_id = ? AND file_name = ?`, workspaceID, fileName))
	return c, notFound(err)
}

// SetCheckpointLink guarda qué agente y qué sesión crearon el checkpoint. Crea
// la fila si el rescan todavía no la vio (el vínculo se calcula en paralelo al
// escaneo): SyncCheckpoints completa después el resto de las columnas sin
// tocar estas.
func (s *Store) SetCheckpointLink(workspaceID int64, fileName, agentID, sessionID string) error {
	_, err := s.db.Exec(`INSERT INTO checkpoints (workspace_id, file_name, agent_id, session_id, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, file_name) DO UPDATE SET agent_id = excluded.agent_id, session_id = excluded.session_id`,
		workspaceID, fileName, agentID, sessionID, now())
	return err
}

// SetCheckpointChat guarda desde qué chat de diseño se creó el checkpoint (misma
// regla de fila temprana que SetCheckpointLink).
func (s *Store) SetCheckpointChat(workspaceID int64, fileName string, chatID int64) error {
	_, err := s.db.Exec(`INSERT INTO checkpoints (workspace_id, file_name, chat_id, created_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(workspace_id, file_name) DO UPDATE SET chat_id = excluded.chat_id`,
		workspaceID, fileName, chatID, now())
	return err
}

// StashCheckpointMeta copia a checkpoint_trash lo que solo vive en la base de un
// checkpoint que va a la papelera (rótulo, plegado, agente, sesión, chat), con
// el nombre que le queda allí. Si no hay fila —nunca se escaneó— no hay nada que
// guardar y no es un error.
func (s *Store) StashCheckpointMeta(workspaceID int64, fileName, trashedName string) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO checkpoint_trash (workspace_id, trashed_name, label, collapsed, session_id, agent_id, chat_id)
		SELECT workspace_id, ?, label, collapsed, session_id, agent_id, chat_id
		FROM checkpoints WHERE workspace_id = ? AND file_name = ?`, trashedName, workspaceID, fileName)
	return err
}

// RestoreCheckpointMeta devuelve a restoredFile lo que StashCheckpointMeta
// guardó bajo trashedName, y lo olvida. Sin nada guardado, no hace nada.
func (s *Store) RestoreCheckpointMeta(workspaceID int64, trashedName, restoredFile string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	var (
		label, session, agent string
		collapsed             bool
		chat                  int64
	)
	err = tx.QueryRow(`SELECT label, collapsed, session_id, agent_id, chat_id FROM checkpoint_trash WHERE workspace_id = ? AND trashed_name = ?`,
		workspaceID, trashedName).Scan(&label, &collapsed, &session, &agent, &chat)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO checkpoints (workspace_id, file_name, label, collapsed, session_id, agent_id, chat_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(workspace_id, file_name) DO UPDATE SET
			label = excluded.label, collapsed = excluded.collapsed,
			session_id = excluded.session_id, agent_id = excluded.agent_id, chat_id = excluded.chat_id`,
		workspaceID, restoredFile, label, collapsed, session, agent, chat, now()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM checkpoint_trash WHERE workspace_id = ? AND trashed_name = ?`, workspaceID, trashedName); err != nil {
		return err
	}
	return tx.Commit()
}

// ForgetCheckpointMeta olvida lo que StashCheckpointMeta guardó bajo trashedName:
// el elemento se eliminó de la papelera para siempre y ya no hay a quién devolver
// su rótulo. trashedName vacío = todos los de la papelera del workspace.
func (s *Store) ForgetCheckpointMeta(workspaceID int64, trashedName string) error {
	if trashedName == "" {
		_, err := s.db.Exec(`DELETE FROM checkpoint_trash WHERE workspace_id = ?`, workspaceID)
		return err
	}
	_, err := s.db.Exec(`DELETE FROM checkpoint_trash WHERE workspace_id = ? AND trashed_name = ?`, workspaceID, trashedName)
	return err
}
