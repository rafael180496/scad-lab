package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
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
	// AutoIterate avisa al agente de la terminal cuando falla el render del
	// checkpoint más nuevo del workspace (S5): corrige solo, sin que el
	// usuario copie el error. Se enciende y apaga desde la barra de la
	// terminal (App.SetAutoIterate).
	AutoIterate bool `json:"autoIterate"`
	// Swatches son las 6 muestras de color del visor 3D (#rrggbb), pensadas
	// como los filamentos cargados en el AMS. Se cambian desde el visor
	// (App.SaveSwatches), no desde el diálogo de Configuración.
	Swatches []string `json:"swatches"`

	// Editor de código (S4). EditorIndent lo usa también el formateador
	// (App.FormatScad lo lee por su nombre JSON): 2 o 4 espacios.
	EditorIndent   int  `json:"editorIndent"`
	EditorFontSize int  `json:"editorFontSize"` // px, 10–24
	EditorWordWrap bool `json:"editorWordWrap"`
	EditorMinimap  bool `json:"editorMinimap"`

	// Layout es la disposición de los paneles de la ventana principal. Se
	// guarda desde la propia ventana al soltar un divisor (App.SaveLayout),
	// no desde el diálogo de Configuración.
	Layout Layout `json:"layout"`
}

// Layout: tamaños en píxeles CSS. El frontend los vuelve a acotar al tamaño
// real de la ventana; acá solo se descartan valores absurdos.
type Layout struct {
	SidebarWidth     int  `json:"sidebarWidth"`
	SidebarCollapsed bool `json:"sidebarCollapsed"`
	CodeWidth        int  `json:"codeWidth"`    // ancho del panel de código
	BottomHeight     int  `json:"bottomHeight"` // alto del panel inferior (agente / problemas)
	DimsCollapsed    bool `json:"dimsCollapsed"`
}

// Límites del layout y del editor.
const (
	MinSidebarWidth, MaxSidebarWidth = 200, 480
	MinCodeWidth, MaxCodeWidth       = 280, 1400
	MinBottomHeight, MaxBottomHeight = 120, 800
	MinEditorFontSize                = 10
	MaxEditorFontSize                = 24
)

// DefaultLayout es la disposición de fábrica (la de S0–S2).
func DefaultLayout() Layout {
	return Layout{SidebarWidth: 264, CodeWidth: 420, BottomHeight: 220}
}

func clampInt(v, lo, hi, def int) int {
	if v == 0 {
		return def
	}
	return max(lo, min(hi, v))
}

// Normalize acota el layout a sus límites; un 0 (campo que no se guardó)
// toma el valor de fábrica.
func (l Layout) Normalize() Layout {
	d := DefaultLayout()
	l.SidebarWidth = clampInt(l.SidebarWidth, MinSidebarWidth, MaxSidebarWidth, d.SidebarWidth)
	l.CodeWidth = clampInt(l.CodeWidth, MinCodeWidth, MaxCodeWidth, d.CodeWidth)
	l.BottomHeight = clampInt(l.BottomHeight, MinBottomHeight, MaxBottomHeight, d.BottomHeight)
	return l
}

// normalizeEditor deja los campos del editor en valores que el editor
// entiende: una base editada a mano o de una versión vieja no lo rompe.
func (s *Settings) normalizeEditor() {
	if s.EditorIndent != 2 && s.EditorIndent != 4 {
		s.EditorIndent = 4
	}
	if s.EditorFontSize < MinEditorFontSize || s.EditorFontSize > MaxEditorFontSize {
		s.EditorFontSize = 13
	}
	s.Layout = s.Layout.Normalize()
}

// SwatchCount es la cantidad fija de muestras del visor.
const SwatchCount = 6

var hexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// DefaultSwatches son las muestras de una instalación nueva: el naranja del
// acento, blanco, grafito, verde, azul y rojo — filamentos comunes.
func DefaultSwatches() []string {
	return []string{"#ff7a1a", "#e7e9ee", "#2b2f38", "#3ccf7a", "#5aa2ff", "#d32f2f"}
}

// ValidSwatches dice si la lista tiene exactamente 6 colores #rrggbb.
func ValidSwatches(colors []string) bool {
	if len(colors) != SwatchCount {
		return false
	}
	for _, c := range colors {
		if !hexColor.MatchString(c) {
			return false
		}
	}
	return true
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
		AutoIterate:      true,
		Swatches:         DefaultSwatches(),
		EditorIndent:     4,
		EditorFontSize:   13,
		Layout:           DefaultLayout(),
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
	if err := json.Unmarshal(blob, &out); err != nil {
		return out, err
	}
	// Unas muestras guardadas rotas (o null) no deben dejar al visor sin
	// paleta: vuelven a las de fábrica.
	if !ValidSwatches(out.Swatches) {
		out.Swatches = DefaultSwatches()
	}
	out.normalizeEditor()
	return out, nil
}

// SaveSettings guarda todos los campos.
func (s *Store) SaveSettings(in Settings) error {
	// Un cliente que no conoce el layout lo manda en cero: no pisa el que
	// el usuario dejó arrastrando los divisores.
	keepLayout := in.Layout == (Layout{})
	in.normalizeEditor()
	blob, err := json.Marshal(in)
	if err != nil {
		return err
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(blob, &fields); err != nil {
		return err
	}
	// Un cliente que no conoce las muestras (o las manda vacías) no pisa las
	// que eligió el usuario: solo se escriben si son válidas.
	if !ValidSwatches(in.Swatches) {
		delete(fields, "swatches")
	}
	if keepLayout {
		delete(fields, "layout")
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
