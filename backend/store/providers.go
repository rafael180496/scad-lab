package store

import "encoding/json"

// AIProvider es un CLI agéntico (claude, opencode, codex, antigravity) o un
// servidor de modelos (ollama). Config es JSON propio de cada tipo: para
// ollama {"baseUrl","model"}; para un CLI, argumentos extra.
type AIProvider struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Name      string         `json:"name"`
	Command   string         `json:"command"`
	Config    map[string]any `json:"config"`
	Enabled   bool           `json:"enabled"`
	SortOrder int            `json:"sortOrder"`
}

// ListProviders devuelve los proveedores en su orden.
func (s *Store) ListProviders() ([]AIProvider, error) {
	rows, err := s.db.Query(`SELECT id, kind, name, command, config, enabled, sort_order FROM ai_providers ORDER BY sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AIProvider{}
	for rows.Next() {
		var p AIProvider
		var cfg string
		if err := rows.Scan(&p.ID, &p.Kind, &p.Name, &p.Command, &cfg, &p.Enabled, &p.SortOrder); err != nil {
			return nil, err
		}
		p.Config = decodeConfig(cfg)
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProvider lee uno por id.
func (s *Store) GetProvider(id string) (AIProvider, error) {
	var p AIProvider
	var cfg string
	err := s.db.QueryRow(`SELECT id, kind, name, command, config, enabled, sort_order FROM ai_providers WHERE id = ?`, id).
		Scan(&p.ID, &p.Kind, &p.Name, &p.Command, &cfg, &p.Enabled, &p.SortOrder)
	p.Config = decodeConfig(cfg)
	return p, notFound(err)
}

// UpdateProvider guarda comando, config y habilitado. El id y el tipo no
// cambian: son la identidad del proveedor.
func (s *Store) UpdateProvider(p AIProvider) error {
	if p.Config == nil {
		p.Config = map[string]any{}
	}
	raw, err := json.Marshal(p.Config)
	if err != nil {
		return err
	}
	cfg := string(raw)
	_, err = s.db.Exec(`UPDATE ai_providers SET command = ?, config = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		p.Command, cfg, p.Enabled, now(), p.ID)
	return err
}

// decodeConfig tolera una config corrupta: devuelve un mapa vacío en vez de
// romper el listado entero de proveedores.
func decodeConfig(raw string) map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &m)
	return m
}
