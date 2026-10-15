package store

// Material es un filamento con densidad y costo, para estimar peso y precio.
type Material struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	DensityGCm3 float64 `json:"densityGCm3"`
	CostPerKg   float64 `json:"costPerKg"`
}

// ListMaterials devuelve los materiales en su orden.
func (s *Store) ListMaterials() ([]Material, error) {
	rows, err := s.db.Query(`SELECT id, name, density_g_cm3, cost_per_kg FROM materials ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Material{}
	for rows.Next() {
		var m Material
		if err := rows.Scan(&m.ID, &m.Name, &m.DensityGCm3, &m.CostPerKg); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SaveMaterial crea o actualiza un material.
func (s *Store) SaveMaterial(m Material) error {
	_, err := s.db.Exec(`INSERT INTO materials (id, name, density_g_cm3, cost_per_kg, sort_order)
		VALUES (?, ?, ?, ?, (SELECT COALESCE(MAX(sort_order),0)+1 FROM materials))
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, density_g_cm3 = excluded.density_g_cm3, cost_per_kg = excluded.cost_per_kg`,
		m.ID, m.Name, m.DensityGCm3, m.CostPerKg)
	return err
}

// SkillRow es el registro de una skill (incluida o del usuario).
type SkillRow struct {
	Name        string `json:"name"`
	Source      string `json:"source"` // "builtin" | "user"
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

// ListSkills devuelve el registro de skills.
func (s *Store) ListSkills() ([]SkillRow, error) {
	rows, err := s.db.Query(`SELECT name, source, description, enabled FROM skills ORDER BY source, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SkillRow{}
	for rows.Next() {
		var r SkillRow
		if err := rows.Scan(&r.Name, &r.Source, &r.Description, &r.Enabled); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UpsertSkill registra una skill descubierta. Conserva `enabled` si ya existía:
// apagar una skill es una decisión del usuario que un rescan no debe pisar.
func (s *Store) UpsertSkill(r SkillRow) error {
	_, err := s.db.Exec(`INSERT INTO skills (name, source, description, enabled, updated_at) VALUES (?, ?, ?, 1, ?)
		ON CONFLICT(name) DO UPDATE SET source = excluded.source, description = excluded.description, updated_at = excluded.updated_at`,
		r.Name, r.Source, r.Description, now())
	return err
}

// SetSkillEnabled prende o apaga una skill.
func (s *Store) SetSkillEnabled(name string, enabled bool) error {
	_, err := s.db.Exec(`UPDATE skills SET enabled = ?, updated_at = ? WHERE name = ?`, enabled, now(), name)
	return err
}

// PruneSkills borra del registro las skills que ya no existen.
func (s *Store) PruneSkills(existing []string) error {
	if len(existing) == 0 {
		_, err := s.db.Exec(`DELETE FROM skills`)
		return err
	}
	args := make([]any, len(existing))
	ph := ""
	for i, n := range existing {
		if i > 0 {
			ph += ","
		}
		ph += "?"
		args[i] = n
	}
	_, err := s.db.Exec(`DELETE FROM skills WHERE name NOT IN (`+ph+`)`, args...)
	return err
}
