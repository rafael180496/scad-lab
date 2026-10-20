package store

import (
	"encoding/json"
	"strings"
)

// WorkspacePrint son los ajustes de impresión propios de un workspace (S14).
// Todo lo que está en su valor cero se hereda: de la impresora (colores,
// diámetro del filamento) o de Configuración → Impresión (el resto). Los
// punteros distinguen «no fijado» de un 0 legítimo (relleno 0 % = solo cáscara).
type WorkspacePrint struct {
	Colors             int      `json:"colors,omitempty"`             // 0 = default_colors de la impresora
	Material           string   `json:"material,omitempty"`           // "" = el global
	InfillPercent      *float64 `json:"infillPercent,omitempty"`      // 0–100
	WallLoops          *int     `json:"wallLoops,omitempty"`          // 1–10
	LineWidthMm        *float64 `json:"lineWidthMm,omitempty"`        // 0,2–1,5
	FilamentDiameterMm *float64 `json:"filamentDiameterMm,omitempty"` // 1,0–3,5
}

// Workspace es una carpeta de proyecto registrada en ScadLab.
type Workspace struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	PrinterID   string `json:"printerId"`
	Description string `json:"description"`
	LastOpened  int64  `json:"lastOpened"`
	CreatedAt   int64  `json:"createdAt"`
	// LastCheckpoint es el .scad que estaba elegido al cerrar o cambiar de
	// proyecto ("" = ninguno guardado aún). Al reabrir, la interfaz vuelve a él
	// si todavía existe.
	LastCheckpoint string `json:"lastCheckpoint"`
	// Skills son las skills activas de ESTE workspace (S14). nil = sin definir: se
	// resuelve por defecto en tiempo de ejecución (wsconfig.EffectiveSkills). Un
	// workspace creado desde S14 la guarda explícita, así que cambiar Configuración
	// → Skills después no lo altera.
	Skills []string `json:"skills"`
	// Print son sus ajustes de impresión propios; lo que no fija, lo hereda.
	Print WorkspacePrint `json:"print"`
}

const workspaceCols = `id, name, path, printer_id, description, last_opened, created_at, last_checkpoint, skills, print_settings`

func scanWorkspace(sc interface{ Scan(...any) error }) (Workspace, error) {
	var w Workspace
	var skills, printSettings string
	err := sc.Scan(&w.ID, &w.Name, &w.Path, &w.PrinterID, &w.Description, &w.LastOpened, &w.CreatedAt, &w.LastCheckpoint, &skills, &printSettings)
	if err != nil {
		return w, err
	}
	w.Skills = decodeSkills(skills)
	w.Print = decodePrint(printSettings)
	return w, nil
}

// decodeSkills lee la columna skills: vacía (o un JSON que no es una lista de
// textos, p. ej. una base editada a mano) es «sin definir», no un error que
// impida abrir el workspace.
func decodeSkills(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

// encodeSkills es el inverso: nil se guarda vacío (sin definir) y una lista
// vacía como [] (el usuario apagó todas).
func encodeSkills(skills []string) string {
	if skills == nil {
		return ""
	}
	blob, err := json.Marshal(skills)
	if err != nil {
		return ""
	}
	return string(blob)
}

func decodePrint(raw string) WorkspacePrint {
	var p WorkspacePrint
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &p)
	}
	return p
}

// encodePrint guarda vacío cuando no hay nada fijado.
func encodePrint(p WorkspacePrint) string {
	if p == (WorkspacePrint{}) {
		return ""
	}
	blob, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(blob)
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

// InsertWorkspace registra un workspace y devuelve su id. Guarda también su
// lista de skills y sus ajustes de impresión, así el alta es una sola escritura.
func (s *Store) InsertWorkspace(w Workspace) (int64, error) {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO workspaces (name, path, printer_id, description, last_opened, created_at, updated_at, skills, print_settings)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, w.Name, w.Path, w.PrinterID, w.Description, ts, ts, ts, encodeSkills(w.Skills), encodePrint(w.Print))
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

// SetWorkspaceConfig guarda de una vez la configuración propia del workspace:
// impresora, skills activas (nil = sin definir) y ajustes de impresión. Una sola
// sentencia: la impresora y los colores que dependen de ella no pueden quedar a
// medias. No valida nada (eso es de backend/wsconfig).
func (s *Store) SetWorkspaceConfig(id int64, printerID string, skills []string, p WorkspacePrint) error {
	res, err := s.db.Exec(`UPDATE workspaces SET printer_id = ?, skills = ?, print_settings = ?, updated_at = ? WHERE id = ?`,
		printerID, encodeSkills(skills), encodePrint(p), now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
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

// SetLastCheckpoint anota el checkpoint elegido de un workspace. No toca
// updated_at: elegir un archivo en el árbol no es editar el workspace.
func (s *Store) SetLastCheckpoint(id int64, fileName string) error {
	res, err := s.db.Exec(`UPDATE workspaces SET last_checkpoint = ? WHERE id = ?`, fileName, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
