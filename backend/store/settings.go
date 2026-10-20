package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"scadlab/backend/i18n"
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
	// SkillsDir es la carpeta de las skills propias; vacío = <datos>/skills.
	// Solo cambia por App.ChangeSkillsFolder (que además puede mover las skills
	// que ya hay): SaveSettings no la escribe. Es un campo más de la tabla
	// key/value, así que no necesita migración.
	SkillsDir string `json:"skillsDir"`

	// Parámetros de la estimación de peso y costo (S7, backend/estimate). Son los
	// valores por defecto: el panel «Impresión» los puede cambiar solo para un
	// cálculo. Fuera de rango no es un error: Normalize deja el de fábrica.
	WallLoops          int     `json:"wallLoops"`          // perímetros, 1–10 (default 2)
	LineWidthMm        float64 `json:"lineWidthMm"`        // ancho de línea, 0,2–1,5 mm (default 0,42)
	FilamentDiameterMm float64 `json:"filamentDiameterMm"` // 1,0–3,5 mm (default 1,75)
	Currency           string  `json:"currency"`           // código ISO 4217 de 3 letras (default "USD")
	// BambuStudioPath es la app de Bambu Studio para «Abrir en Bambu Studio»
	// (S9): en macOS el .app, en Windows el .exe. Vacío = autodetectar en las
	// rutas de instalación habituales; si la ruta no existe se ignora.
	BambuStudioPath string `json:"bambuStudioPath"`
	// SlicerPaths es la ruta de cada slicer que el usuario fijó a mano, por id del
	// registro (backend/slicers): "orcaslicer" → "/Applications/OrcaSlicer.app".
	// Vacío = autodetectar. Para "bambu-studio" vale también bambuStudioPath (la
	// clave anterior a S14): ver SlicerPath. Un campo más de la tabla key/value,
	// sin migración.
	SlicerPaths map[string]string `json:"slicerPaths"`
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
	// MCPEnabled deja configurado el servidor MCP de ScadLab en los CLIs de IA
	// de cada workspace (S6): .mcp.json, opencode.json y .gemini/settings.json,
	// y los flags -c de Codex. Default true. Solo cambia por App.SetMCPEnabled
	// (que además sincroniza los workspaces); SaveSettings no lo escribe.
	MCPEnabled bool `json:"mcpEnabled"`
	// Swatches son las 6 muestras de color del visor 3D (#rrggbb), pensadas
	// como los filamentos cargados en el AMS. Se cambian desde el visor
	// (App.SaveSwatches), no desde el diálogo de Configuración.
	Swatches []string `json:"swatches"`

	// Editor de código (S4). EditorIndent lo usa también el formateador
	// (App.FormatScad lo lee por su nombre JSON): 2 o 4 espacios.
	EditorIndent   int  `json:"editorIndent"`
	EditorFontSize int  `json:"editorFontSize"` // px, 9–28
	EditorWordWrap bool `json:"editorWordWrap"`
	EditorMinimap  bool `json:"editorMinimap"`

	// Apariencia. UIScale es el tamaño de la interfaz en % (uno de
	// UIScalePresets): escala el texto y los íconos, no los espaciados. La
	// letra de la terminal y la del chat son propias de cada área, independientes
	// de UIScale (y del editor, que tiene EditorFontSize). Se cambian en vivo desde
	// Configuración → General y se guardan al instante (App.SaveAppearance).
	UIScale          int `json:"uiScale"`          // %, uno de UIScalePresets
	TerminalFontSize int `json:"terminalFontSize"` // px, 9–28
	ChatFontSize     int `json:"chatFontSize"`     // px, 9–28

	// Layout es la disposición de los paneles de la ventana principal. Se
	// guarda desde la propia ventana al soltar un divisor (App.SaveLayout),
	// no desde el diálogo de Configuración.
	Layout Layout `json:"layout"`

	// DockLayout es la disposición de los paneles acoplables (S4b): el JSON
	// que arma el frontend (sobre dockview). Va guardado como texto opaco: el
	// frontend lo valida al cargar y, si no lo entiende, cae al diseño
	// predeterminado; acá solo se acota el tamaño. Vacío = predeterminado.
	// Se guarda desde la propia ventana (App.SaveDockLayout), nunca con
	// SaveSettings: el diálogo de Configuración tiene una copia vieja.
	DockLayout string `json:"dockLayout"`
	// DockLayouts son los diseños propios con nombre (App.SaveDockLayouts).
	DockLayouts []DockLayoutEntry `json:"dockLayouts"`

	// Proyectos abiertos (S8): ids de workspace en el orden de las pestañas y
	// el que está al frente. Solo cambian por los bindings de proyecto
	// (App.OpenProject, CloseProject, ReorderProjects, SetActiveProject);
	// SaveSettings no los escribe, igual que el diseño de paneles.
	OpenProjects  []int64 `json:"openProjects"`
	ActiveProject int64   `json:"activeProject"`

	// Viewer es el estado del visor 3D (alambre, aristas, proyección, vista
	// dividida, muestra y colores del modelo). Se guarda desde el propio visor
	// (App.SaveViewer); SaveSettings no lo escribe.
	Viewer ViewerState `json:"viewer"`
}

// ViewerState es lo que el visor 3D recuerda entre sesiones. La cámara no: cada
// modelo se encuadra solo al abrirlo, y una cámara vieja sobre otra pieza
// dejaría el modelo fuera de cuadro.
type ViewerState struct {
	Wireframe bool `json:"wireframe"`
	Edges     bool `json:"edges"`
	// Ortho es la proyección de cada vista: [principal, segunda de la vista
	// dividida]. true = ortográfica, false = perspectiva.
	Ortho [2]bool `json:"ortho"`
	// Split: la vista dividida está abierta.
	Split bool `json:"split"`
	// ColorIndex es la muestra elegida (0–5) y ModelColors si el modelo se pinta
	// con sus propios colores (true) o con la muestra (false).
	ColorIndex  int  `json:"colorIndex"`
	ModelColors bool `json:"modelColors"`
}

// DefaultViewer es el visor de una instalación nueva: sólido, sin aristas, en
// perspectiva, una sola vista y con los colores del propio modelo.
func DefaultViewer() ViewerState { return ViewerState{ModelColors: true} }

// Normalize acota la muestra elegida a las seis que existen.
func (v ViewerState) Normalize() ViewerState {
	if v.ColorIndex < 0 || v.ColorIndex >= SwatchCount {
		v.ColorIndex = 0
	}
	return v
}

// MaxOpenProjects es el tope de proyectos abiertos a la vez (pestañas).
const MaxOpenProjects = 4

// NormalizeOpenProjects deja la lista de proyectos abiertos lista para usar: sin
// ids no positivos ni repetidos y de hasta MaxOpenProjects. Nunca devuelve nil
// (el frontend espera una lista, no null). Una base editada a mano no debe
// romper la barra de pestañas.
func NormalizeOpenProjects(in []int64) []int64 {
	out := make([]int64, 0, len(in))
	seen := map[int64]bool{}
	for _, id := range in {
		if id <= 0 || seen[id] || len(out) >= MaxOpenProjects {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// DockLayoutEntry es un diseño guardado por el usuario: un nombre y el mismo
// JSON que DockLayout.
type DockLayoutEntry struct {
	Name string `json:"name"`
	JSON string `json:"json"`
}

// Layout: tamaños en píxeles CSS. El frontend los vuelve a acotar al tamaño
// real de la ventana; acá solo se descartan valores absurdos.
type Layout struct {
	SidebarWidth     int  `json:"sidebarWidth"`
	SidebarCollapsed bool `json:"sidebarCollapsed"`
	// CodeWidth y BottomHeight son OBSOLETOS desde S4b: el código y el panel
	// inferior ahora son paneles acoplables (DockLayout). Se conservan sin uso
	// (regla de datos: no se borra lo que el usuario ya guardó).
	CodeWidth     int  `json:"codeWidth"`
	BottomHeight  int  `json:"bottomHeight"`
	DimsCollapsed bool `json:"dimsCollapsed"`
	// StatusBarHidden oculta la barra de estado (menú Ver). La barra lateral
	// usa SidebarCollapsed, que ya existía.
	StatusBarHidden bool `json:"statusBarHidden"`
	// WorkspacesHeight es el alto de la lista de workspaces dentro de la barra
	// lateral; los checkpoints ocupan el resto.
	WorkspacesHeight int `json:"workspacesHeight"`
}

// Límites del layout y del editor.
const (
	MinSidebarWidth, MaxSidebarWidth = 200, 480
	MinCodeWidth, MaxCodeWidth       = 280, 1400
	MinBottomHeight, MaxBottomHeight = 120, 800
	MinWorkspacesHeight              = 56
	MaxWorkspacesHeight              = 640
	// Letra propia de cada área (editor, terminal, chat), en px.
	MinFontSize     = 9
	MaxFontSize     = 28
	DefaultFontSize = 13
	// DefaultUIScale es el 100 %: el tamaño de siempre.
	DefaultUIScale = 100
)

// UIScalePresets son los tamaños de interfaz que ofrece Configuración
// (Compacto, Normal, Grande, Más grande, Máximo). Un valor fuera de la lista no
// se guarda: vuelve al 100 %.
var UIScalePresets = []int{90, 100, 115, 130, 150}

// ValidUIScale dice si v es uno de los presets.
func ValidUIScale(v int) bool {
	for _, p := range UIScalePresets {
		if p == v {
			return true
		}
	}
	return false
}

// NormalizeFontSize deja una letra en 9–28 px. Fuera de rango (o 0, un campo
// que nunca se guardó) vuelve al valor de fábrica, igual que la sangría del
// editor: un número roto en la base no debe dejar una letra ilegible.
func NormalizeFontSize(v int) int {
	if v < MinFontSize || v > MaxFontSize {
		return DefaultFontSize
	}
	return v
}

// Appearance es lo que cambia la pantalla de Configuración → Apariencia. Va en
// un binding aparte (App.SaveAppearance) para guardarse al instante, sin
// reescribir el resto de la configuración con lo que el diálogo tenga abierto.
type Appearance struct {
	UIScale          int `json:"uiScale"`
	EditorFontSize   int `json:"editorFontSize"`
	TerminalFontSize int `json:"terminalFontSize"`
	ChatFontSize     int `json:"chatFontSize"`
}

// DefaultAppearance es la apariencia de fábrica.
func DefaultAppearance() Appearance {
	return Appearance{UIScale: DefaultUIScale, EditorFontSize: DefaultFontSize, TerminalFontSize: DefaultFontSize, ChatFontSize: DefaultFontSize}
}

// Normalize deja la apariencia en valores que la interfaz entiende: una escala
// fuera de los presets vuelve al 100 % y las letras fuera de 9–28 vuelven a 13.
func (a Appearance) Normalize() Appearance {
	if !ValidUIScale(a.UIScale) {
		a.UIScale = DefaultUIScale
	}
	a.EditorFontSize = NormalizeFontSize(a.EditorFontSize)
	a.TerminalFontSize = NormalizeFontSize(a.TerminalFontSize)
	a.ChatFontSize = NormalizeFontSize(a.ChatFontSize)
	return a
}

// DefaultLayout es la disposición de fábrica (la de S0–S2).
func DefaultLayout() Layout {
	return Layout{SidebarWidth: 264, CodeWidth: 420, BottomHeight: 220, WorkspacesHeight: 150}
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
	l.WorkspacesHeight = clampInt(l.WorkspacesHeight, MinWorkspacesHeight, MaxWorkspacesHeight, d.WorkspacesHeight)
	return l
}

// normalizeEditor deja los campos del editor en valores que el editor
// entiende: una base editada a mano o de una versión vieja no lo rompe.
func (s *Settings) normalizeEditor() {
	if s.EditorIndent != 2 && s.EditorIndent != 4 {
		s.EditorIndent = 4
	}
	a := Appearance{UIScale: s.UIScale, EditorFontSize: s.EditorFontSize, TerminalFontSize: s.TerminalFontSize, ChatFontSize: s.ChatFontSize}.Normalize()
	s.UIScale, s.EditorFontSize, s.TerminalFontSize, s.ChatFontSize = a.UIScale, a.EditorFontSize, a.TerminalFontSize, a.ChatFontSize
	s.Layout = s.Layout.Normalize()
	s.normalizePrint()
	s.normalizeSlicerPaths()
}

// normalizeSlicerPaths deja el mapa listo para usar: nunca nil (el frontend
// espera un objeto) y sin entradas vacías, con las rutas sin espacios sobrantes.
func (s *Settings) normalizeSlicerPaths() {
	out := make(map[string]string, len(s.SlicerPaths))
	for id, p := range s.SlicerPaths {
		if id, p = strings.TrimSpace(id), strings.TrimSpace(p); id != "" && p != "" {
			out[id] = p
		}
	}
	s.SlicerPaths = out
}

// SlicerPath es la ruta que el usuario fijó para un slicer ("" = autodetectar).
// Bambu Studio conserva su clave anterior: slicerPaths["bambu-studio"] gana y, si
// no está, vale bambuStudioPath.
func (s Settings) SlicerPath(id string) string {
	if p := strings.TrimSpace(s.SlicerPaths[id]); p != "" {
		return p
	}
	if id == "bambu-studio" {
		return strings.TrimSpace(s.BambuStudioPath)
	}
	return ""
}

// Límites y valores de fábrica de la estimación de impresión.
const (
	MinWallLoops, MaxWallLoops               = 1, 10
	DefaultWallLoops                         = 2
	MinLineWidthMm, MaxLineWidthMm           = 0.2, 1.5
	DefaultLineWidthMm                       = 0.42
	MinFilamentDiameter, MaxFilamentDiameter = 1.0, 3.5
	DefaultFilamentDiameter                  = 1.75
	DefaultCurrency                          = "USD"
)

var currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)

// normalizePrint deja los parámetros de la estimación en valores que
// backend/estimate entiende. Un campo que nunca se guardó llega en cero y toma
// el de fábrica, igual que el editor: una base editada a mano o de una versión
// vieja no debe romper el cálculo. La moneda se guarda en mayúsculas.
func (s *Settings) normalizePrint() {
	if s.WallLoops < MinWallLoops || s.WallLoops > MaxWallLoops {
		s.WallLoops = DefaultWallLoops
	}
	if !(s.LineWidthMm >= MinLineWidthMm && s.LineWidthMm <= MaxLineWidthMm) { // también atrapa NaN
		s.LineWidthMm = DefaultLineWidthMm
	}
	if !(s.FilamentDiameterMm >= MinFilamentDiameter && s.FilamentDiameterMm <= MaxFilamentDiameter) {
		s.FilamentDiameterMm = DefaultFilamentDiameter
	}
	s.Currency = strings.ToUpper(strings.TrimSpace(s.Currency))
	if !currencyCode.MatchString(s.Currency) {
		s.Currency = DefaultCurrency
	}
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
		Language:           "en",
		Theme:              "dark",
		RenderBackend:      "manifold",
		DefaultPrinter:     "bambu-a1-mini",
		DefaultMaterial:    "pla",
		InfillPercent:      15,
		DefaultAgent:       "claude",
		WallLoops:          DefaultWallLoops,
		LineWidthMm:        DefaultLineWidthMm,
		FilamentDiameterMm: DefaultFilamentDiameter,
		Currency:           DefaultCurrency,
		SlicerPaths:        map[string]string{},
		AutoRender:         true,
		RenderTimeoutSec:   300,
		AutoIterate:        true,
		MCPEnabled:         true,
		Swatches:           DefaultSwatches(),
		EditorIndent:       4,
		EditorFontSize:     DefaultFontSize,
		UIScale:            DefaultUIScale,
		TerminalFontSize:   DefaultFontSize,
		ChatFontSize:       DefaultFontSize,
		Layout:             DefaultLayout(),
		OpenProjects:       []int64{},
		Viewer:             DefaultViewer(),
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
	out.OpenProjects = NormalizeOpenProjects(out.OpenProjects)
	out.Viewer = out.Viewer.Normalize()
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
	// Los diseños de paneles solo cambian por sus propios bindings: el diálogo
	// de Configuración guarda una copia vieja y pisaría el diseño actual.
	delete(fields, "dockLayout")
	delete(fields, "dockLayouts")
	// Los proyectos abiertos solo cambian por los bindings de proyecto: el
	// diálogo de Configuración guarda una copia vieja y cerraría pestañas.
	delete(fields, "openProjects")
	delete(fields, "activeProject")
	// El visor se guarda solo, desde el visor (SaveViewer): la copia que tiene el
	// diálogo de Configuración vuelve el alambre y las aristas a como estaban al abrirlo.
	delete(fields, "viewer")
	// La carpeta de skills propias solo cambia por su binding (puede mover las
	// skills): el diálogo guarda una copia vieja y la devolvería a la anterior.
	delete(fields, "skillsDir")
	// El interruptor del servidor MCP aplica al instante (reescribe los
	// archivos de cada workspace): el diálogo guarda una copia vieja.
	delete(fields, "mcpEnabled")
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

// SetOpenProjects guarda de una vez los proyectos abiertos y el activo (una sola
// transacción: el orden y el activo no deben quedar a medias). La lista se
// normaliza; un activo que no está en ella se guarda como 0.
func (s *Store) SetOpenProjects(ids []int64, active int64) error {
	ids = NormalizeOpenProjects(ids)
	in := false
	for _, id := range ids {
		in = in || id == active
	}
	if !in {
		active = 0
	}
	list, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	act, err := json.Marshal(active)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	ts := now()
	for k, v := range map[string]string{"openProjects": string(list), "activeProject": string(act)} {
		if _, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, k, v, ts); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ErrNotFound es la fila pedida que no existe.
var ErrNotFound = i18n.New(i18n.Msg{ES: "no se encontró", EN: "not found"})

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Límites del diseño de paneles. El JSON de dockview de un diseño real mide
// pocos KB; el tope solo evita que un valor absurdo llene la base.
const (
	MaxDockLayoutBytes = 256 * 1024
	MaxDockLayouts     = 30
	MaxDockLayoutName  = 40
)

// ErrDockLayout es un diseño de paneles que no se puede guardar.
var ErrDockLayout = i18n.New(i18n.Msg{ES: "el diseño de paneles no es válido", EN: "invalid dock layout"})

// ValidDockLayoutJSON dice si s es un objeto JSON dentro del tope de tamaño.
// No mira qué hay dentro: eso lo valida el frontend, que es quien lo entiende
// (y que cae al diseño predeterminado si no).
func ValidDockLayoutJSON(s string) bool {
	if len(s) == 0 || len(s) > MaxDockLayoutBytes {
		return false
	}
	var m map[string]json.RawMessage
	// "null" decodifica sin error a un mapa nil: no es un objeto.
	return json.Unmarshal([]byte(s), &m) == nil && m != nil
}

// NormalizeDockLayouts deja la lista de diseños propios lista para guardar:
// nombres sin espacios sobrantes y de hasta 40 letras, sin vacíos ni repetidos
// (sin distinguir mayúsculas; gana el último, así guardar con un nombre que ya
// existe lo reemplaza) y con un JSON válido. Devuelve ErrDockLayout si alguno
// no lo es o si hay más de MaxDockLayouts.
func NormalizeDockLayouts(in []DockLayoutEntry) ([]DockLayoutEntry, error) {
	out := make([]DockLayoutEntry, 0, len(in))
	pos := map[string]int{}
	for _, e := range in {
		name := strings.TrimSpace(e.Name)
		if r := []rune(name); len(r) > MaxDockLayoutName {
			name = strings.TrimSpace(string(r[:MaxDockLayoutName]))
		}
		if name == "" || !ValidDockLayoutJSON(e.JSON) {
			return nil, ErrDockLayout
		}
		key := strings.ToLower(name)
		if i, ok := pos[key]; ok {
			out[i] = DockLayoutEntry{Name: name, JSON: e.JSON}
			continue
		}
		pos[key] = len(out)
		out = append(out, DockLayoutEntry{Name: name, JSON: e.JSON})
	}
	if len(out) > MaxDockLayouts {
		return nil, ErrDockLayout
	}
	return out, nil
}
