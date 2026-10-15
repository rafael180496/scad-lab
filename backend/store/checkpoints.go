package store

// Checkpoint es la fila del índice de un .scad del workspace.
type Checkpoint struct {
	ID          int64  `json:"id"`
	WorkspaceID int64  `json:"workspaceId"`
	FileName    string `json:"fileName"`
	ParentFile  string `json:"parentFile"`
	Label       string `json:"label"`
	Description string `json:"description"`
	SessionID   string `json:"sessionId"`
	Collapsed   bool   `json:"collapsed"`
	FileMtime   int64  `json:"fileMtime"`
	CreatedAt   int64  `json:"createdAt"`
}

// ListCheckpoints devuelve el índice de un workspace ordenado por archivo
// (NNN-slug.scad ordena cronológicamente por su prefijo).
func (s *Store) ListCheckpoints(workspaceID int64) ([]Checkpoint, error) {
	rows, err := s.db.Query(`SELECT id, workspace_id, file_name, parent_file, label, description, session_id, collapsed, file_mtime, created_at
		FROM checkpoints WHERE workspace_id = ? ORDER BY file_name`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Checkpoint{}
	for rows.Next() {
		var c Checkpoint
		if err := rows.Scan(&c.ID, &c.WorkspaceID, &c.FileName, &c.ParentFile, &c.Label, &c.Description, &c.SessionID, &c.Collapsed, &c.FileMtime, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SyncCheckpoints reemplaza el índice de un workspace con lo que se leyó del
// disco, conservando lo que es solo de la UI (label, collapsed, session_id)
// para los archivos que siguen existiendo.
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
