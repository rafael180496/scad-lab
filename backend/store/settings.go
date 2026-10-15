package store

import (
	"database/sql"
	"encoding/json"
	"errors"
)

// Settings es la configuración de la app. Se guarda campo por campo en la
// tabla key/value `settings` (clave = tag json), así que un campo nuevo no
// necesita migración: si no está en la base, vale su default.
type Settings struct {
	Language        string  `json:"language"`        // "en" | "es"
	Theme           string  `json:"theme"`           // "dark" | "light" | "system"
	OpenSCADPath    string  `json:"openscadPath"`    // vacío = autodetectar
	RenderBackend   string  `json:"renderBackend"`   // "manifold" | "cgal"
	WorkspacesRoot  string  `json:"workspacesRoot"`  // vacío = ~/Documents/ScadLab
	DefaultPrinter  string  `json:"defaultPrinter"`  // id de printers.json
	DefaultMaterial string  `json:"defaultMaterial"` // id de materials
	InfillPercent   float64 `json:"infillPercent"`
	DefaultAgent    string  `json:"defaultAgent"` // id de ai_providers
	// AutoRender renderiza solo cada checkpoint nuevo que aparece en el
	// workspace (y el que se elige en el árbol si todavía no tiene resultado).
	AutoRender bool `json:"autoRender"`
	// RenderTimeoutSec es el tope de un render, en segundos (10–3600). Un
	// modelo con minkowski() o muchas esferas puede tardar minutos con CGAL.
	RenderTimeoutSec int `json:"renderTimeoutSec"`
}

// DefaultSettings son los valores de una instalación nueva. Inglés por
// defecto, igual que mini-tools: desde Configuración se pasa a español.
func DefaultSettings() Settings {
	return Settings{
		Language:         "en",
		Theme:            "dark",
		RenderBackend:    "manifold",
		DefaultPrinter:   "bambu-a1-mini",
		DefaultMaterial:  "pla",
		InfillPercent:    15,
		DefaultAgent:     "claude",
		AutoRender:       true,
		RenderTimeoutSec: 300,
	}
}

// GetSettings lee la configuración aplicando defaults a lo que falte.
func (s *Store) GetSettings() (Settings, error) {
	out := DefaultSettings()
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	stored := map[string]json.RawMessage{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return out, err
		}
		stored[k] = json.RawMessage(v)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	// Re-armar un objeto JSON con lo guardado y decodificarlo sobre los
	// defaults: los campos ausentes conservan su valor por defecto.
	blob, err := json.Marshal(stored)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(blob, &out)
}

// SaveSettings guarda todos los campos.
func (s *Store) SaveSettings(in Settings) error {
	blob, err := json.Marshal(in)
	if err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(blob, &fields); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	ts := now()
	for k, v := range fields {
		if _, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, k, string(v), ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetSetting guarda un solo campo (p. ej. el idioma) sin reescribir el resto.
func (s *Store) SetSetting(key string, value any) error {
	blob, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, string(blob), now())
	return err
}

// ErrNotFound es la fila pedida que no existe.
var ErrNotFound = errors.New("not found")

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
