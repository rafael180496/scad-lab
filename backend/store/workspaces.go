package store

// Workspace es una carpeta de proyecto registrada en ScadLab.
type Workspace struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	PrinterID   string `json:"printerId"`
	Description string `json:"description"`
	LastOpened  int64  `json:"lastOpened"`
	CreatedAt   int64  `json:"createdAt"`
}

const workspaceCols = `id, name, path, printer_id, description, last_opened, created_at`

func scanWorkspace(sc interface{ Scan(...any) error }) (Workspace, error) {
	var w Workspace
	err := sc.Scan(&w.ID, &w.Name, &w.Path, &w.PrinterID, &w.Description, &w.LastOpened, &w.CreatedAt)
	return w, err
}

// ListWorkspaces devuelve los workspaces, los abiertos más recientemente primero.
func (s *Store) ListWorkspaces() ([]Workspace, error) {
	rows, err := s.db.Query(`SELECT ` + workspaceCols + ` FROM workspaces ORDER BY last_opened DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// GetWorkspace lee uno por id.
func (s *Store) GetWorkspace(id int64) (Workspace, error) {
	w, err := scanWorkspace(s.db.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, id))
	return w, notFound(err)
}

// InsertWorkspace registra un workspace y devuelve su id.
func (s *Store) InsertWorkspace(w Workspace) (int64, error) {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO workspaces (name, path, printer_id, description, last_opened, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, w.Name, w.Path, w.PrinterID, w.Description, ts, ts, ts)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateWorkspace cambia nombre, impresora y descripción.
func (s *Store) UpdateWorkspace(w Workspace) error {
	_, err := s.db.Exec(`UPDATE workspaces SET name = ?, printer_id = ?, description = ?, updated_at = ? WHERE id = ?`,
		w.Name, w.PrinterID, w.Description, now(), w.ID)
	return err
}

// TouchWorkspace marca el workspace como abierto ahora.
func (s *Store) TouchWorkspace(id int64) error {
	_, err := s.db.Exec(`UPDATE workspaces SET last_opened = ? WHERE id = ?`, now(), id)
	return err
}

// RemoveWorkspace lo quita del registro. NO borra la carpeta: los .scad son
// del usuario.
func (s *Store) RemoveWorkspace(id int64) error {
	_, err := s.db.Exec(`DELETE FROM workspaces WHERE id = ?`, id)
	return err
}
