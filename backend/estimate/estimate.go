// Package estimate calcula cuánto filamento, cuánto pesa y cuánto cuesta
// imprimir un modelo, a partir del volumen y el área de cada parte. Lo usan el
// visor y el panel «Impresión» (por el binding EstimatePrint) y la herramienta
// MCP measure: una sola fórmula, un solo lugar.
//
// Es una **estimación**, no una medida: un modelo FDM simple y explicable, sin
// soportes, sin balsa ni borde, sin la purga del AMS al cambiar de color y sin
// mirar la forma del relleno. El número exacto lo da el laminador.
//
// Spec: .claude/specs/s7-medicion-impresion.md. Notas: claude/14-medicion.md.
//
// Fórmula, por parte (volumen V mm³, área A mm², relleno r en [0,1]):
//
//	pared     = perímetros × anchoDeLínea                (mm)
//	shell     = min(V, A × pared)                        (mm³ de cáscara maciza)
//	interior  = V − shell
//	efectivo  = shell + interior × r
//	masa      = efectivo / 1000 × densidad               (g, densidad en g/cm³)
//	largo     = efectivo / (π × (diámetro/2)²) / 1000    (m de filamento)
//	costo     = masa / 1000 × costoPorKg
//
// El min() cubre la pieza delgada: si la pared ocupa más que el volumen, la
// pieza sale maciza y no se cuenta material de más.
package estimate

import (
	"encoding/json"
	"math"

	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
)

// Valores por defecto de los parámetros que no tienen sentido en cero.
const (
	DefaultInfillPercent      = 15.0
	DefaultWallLoops          = 2
	DefaultLineWidthMm        = 0.42
	DefaultFilamentDiameterMm = 1.75
	DefaultCurrency           = "USD"
)

// Options son los parámetros de la estimación (contrato con el binding
// EstimatePrint y con el panel Impresión).
type Options struct {
	// InfillPercent es el relleno en % (0–100). 0 es válido: pieza hueca.
	InfillPercent float64 `json:"infillPercent"`
	// WallLoops son los perímetros (paredes) de la cáscara.
	WallLoops int `json:"wallLoops"`
	// LineWidthMm es el ancho de línea de extrusión.
	LineWidthMm float64 `json:"lineWidthMm"`
	// FilamentDiameterMm es el diámetro del filamento (1,75 o 2,85).
	FilamentDiameterMm float64 `json:"filamentDiameterMm"`
	// DefaultMaterialID es el material de las partes sin uno propio.
	DefaultMaterialID string `json:"defaultMaterialId"`
	// PartMaterials asigna un material a una parte (índice → id de material).
	PartMaterials map[int]string `json:"partMaterials"`
	// Currency es solo un rótulo que viaja al Result ("USD", "EUR"…): la
	// estimación no convierte monedas, el costo por kg ya está en la del usuario.
	Currency string `json:"currency"`
}

// Part es lo que hace falta saber de una parte para estimarla.
type Part struct {
	Index     int
	VolumeMm3 float64
	AreaMm2   float64
	// OpenMesh avisa que la malla no es cerrada: el volumen deja de ser fiable.
	OpenMesh bool
}

// PartOf arma la entrada de una parte desde su análisis de malla.
func PartOf(index int, in mesh.Info) Part {
	return Part{Index: index, VolumeMm3: in.VolumeMM3, AreaMm2: in.AreaMM2, OpenMesh: !in.Watertight}
}

// ClosedAsWhole corrige el aviso de malla abierta en modelos de varios colores.
// Al separar por color, la cara donde dos colores se tocan desaparece (OpenSCAD
// la eliminó al unir), así que cada parte queda abierta justo ahí aunque el
// modelo entero sea cerrado. Si el todo es estanco, esas aberturas son
// fronteras entre colores y no un defecto: no se avisa. Si el todo está
// abierto, el aviso sigue (ahí sí el volumen no es fiable).
func ClosedAsWhole(ps []Part, wholeWatertight bool) []Part {
	if !wholeWatertight {
		return ps
	}
	out := append([]Part(nil), ps...)
	for i := range out {
		out[i].OpenMesh = false
	}
	return out
}

// Material es un filamento con su densidad y su costo.
type Material struct {
	ID          string
	Name        string
	DensityGCm3 float64
	CostPerKg   float64
}

// PartResult es la estimación de una parte.
type PartResult struct {
	Index        int     `json:"index"`
	MaterialID   string  `json:"materialId"`
	VolumeMm3    float64 `json:"volumeMm3"`
	EffectiveMm3 float64 `json:"effectiveMm3"`
	MassG        float64 `json:"massG"`
	LengthM      float64 `json:"lengthM"`
	Cost         float64 `json:"cost"`
}

// Total suma las partes.
type Total struct {
	MassG   float64 `json:"massG"`
	LengthM float64 `json:"lengthM"`
	Cost    float64 `json:"cost"`
}

// Result es la estimación completa.
type Result struct {
	Parts       []PartResult `json:"parts"`
	Total       Total        `json:"total"`
	Currency    string       `json:"currency"`
	Assumptions []string     `json:"assumptions"` // textos en el idioma activo
}

// Normalized rellena con su default lo que no puede ser cero (perímetros,
// ancho de línea, diámetro, moneda). El relleno no se toca: 0 % es una opción.
func (o Options) Normalized() Options {
	if o.WallLoops <= 0 {
		o.WallLoops = DefaultWallLoops
	}
	if o.LineWidthMm <= 0 {
		o.LineWidthMm = DefaultLineWidthMm
	}
	if o.FilamentDiameterMm <= 0 {
		o.FilamentDiameterMm = DefaultFilamentDiameterMm
	}
	if o.Currency == "" {
		o.Currency = DefaultCurrency
	}
	return o
}

// WallThicknessMm es el espesor de pared que usa la fórmula.
func (o Options) WallThicknessMm() float64 {
	o = o.Normalized()
	return float64(o.WallLoops) * o.LineWidthMm
}

// Estimate calcula la estimación de todas las partes. Es una función pura: no
// toca disco ni la base.
//
// Un material desconocido (o sin densidad) es un error con el nombre del
// material: estimar con otra densidad en silencio daría un peso que no
// corresponde a nada.
func Estimate(parts []Part, materials []Material, opts Options) (Result, error) {
	opts = opts.Normalized()
	if math.IsNaN(opts.InfillPercent) || opts.InfillPercent < 0 || opts.InfillPercent > 100 {
		return Result{}, i18n.Errorf(i18n.Msg{
			ES: "el relleno tiene que estar entre 0 y 100 %% (llegó %v)",
			EN: "infill must be between 0 and 100 %% (got %v)",
		}, opts.InfillPercent)
	}
	if opts.LineWidthMm > 5 || opts.FilamentDiameterMm > 10 || opts.WallLoops > 100 {
		return Result{}, i18n.New(i18n.Msg{
			ES: "los parámetros de impresión están fuera de rango (perímetros, ancho de línea o diámetro de filamento)",
			EN: "the print parameters are out of range (perimeters, line width or filament diameter)",
		})
	}
	byID := make(map[string]Material, len(materials))
	for _, m := range materials {
		byID[m.ID] = m
	}
	wall := opts.WallThicknessMm()
	infill := opts.InfillPercent / 100
	filamentArea := math.Pi * math.Pow(opts.FilamentDiameterMm/2, 2) // mm²

	res := Result{Parts: make([]PartResult, 0, len(parts)), Currency: opts.Currency}
	open := false
	for _, p := range parts {
		id := opts.DefaultMaterialID
		if own, ok := opts.PartMaterials[p.Index]; ok && own != "" {
			id = own
		}
		mat, ok := byID[id]
		if !ok || mat.DensityGCm3 <= 0 {
			if id == "" {
				return Result{}, i18n.New(i18n.Msg{
					ES: "no hay un material elegido para estimar: elige uno en Configuración → Impresión",
					EN: "no material is selected for the estimate: pick one in Settings → Printing",
				})
			}
			return Result{}, i18n.Errorf(i18n.Msg{
				ES: "el material %q no existe o no tiene densidad: revisa Configuración → Impresión",
				EN: "material %q does not exist or has no density: check Settings → Printing",
			}, id)
		}
		vol := math.Max(p.VolumeMm3, 0)
		shell := math.Min(vol, math.Max(p.AreaMm2, 0)*wall)
		effective := shell + (vol-shell)*infill
		mass := effective / 1000 * mat.DensityGCm3
		pr := PartResult{
			Index:        p.Index,
			MaterialID:   mat.ID,
			VolumeMm3:    vol,
			EffectiveMm3: effective,
			MassG:        mass,
			LengthM:      effective / filamentArea / 1000,
			Cost:         mass / 1000 * mat.CostPerKg,
		}
		res.Parts = append(res.Parts, pr)
		res.Total.MassG += pr.MassG
		res.Total.LengthM += pr.LengthM
		res.Total.Cost += pr.Cost
		open = open || p.OpenMesh
	}
	res.Assumptions = assumptions(opts, open)
	return res, nil
}

// assumptions son los supuestos que el usuario tiene que leer junto al número.
func assumptions(o Options, open bool) []string {
	out := []string{
		i18n.T(i18n.Msg{
			ES: "Es una estimación, no una medida: el número exacto lo da el laminador (Bambu Studio) al preparar la impresión.",
			EN: "This is an estimate, not a measurement: the exact number comes from the slicer (Bambu Studio) when you prepare the print.",
		}),
		i18n.T(i18n.Msg{
			ES: "Las paredes se calculan como %d perímetros de %.2f mm (%.2f mm de espesor) sobre toda la superficie, tapas incluidas; el resto del volumen se rellena al %s %%.",
			EN: "Walls are computed as %d perimeters of %.2f mm (%.2f mm thick) over the whole surface, top and bottom included; the rest of the volume is filled at %s %%.",
		}, o.WallLoops, o.LineWidthMm, o.WallThicknessMm(), formatPercent(o.InfillPercent)),
		i18n.T(i18n.Msg{
			ES: "No cuenta soportes, balsa ni borde, ni la purga del AMS al cambiar de color, ni la forma del patrón de relleno.",
			EN: "It does not count supports, raft or brim, nor the AMS purge when switching colors, nor the shape of the infill pattern.",
		}),
		i18n.T(i18n.Msg{
			ES: "El largo usa un filamento de %.2f mm de diámetro; el peso, la densidad de cada material, y el costo, su precio por kg.",
			EN: "Length uses a %.2f mm diameter filament; weight uses each material's density, and cost its price per kg.",
		}, o.FilamentDiameterMm),
	}
	if open {
		out = append(out, i18n.T(i18n.Msg{
			ES: "Alguna parte no es una malla cerrada: su volumen no es fiable y la estimación puede quedar lejos de la real.",
			EN: "Some part is not a closed mesh: its volume is unreliable and the estimate may be far off.",
		}))
	}
	return out
}

// formatPercent muestra 15 como «15» y 12,5 como «12.5» (sin ceros de más).
func formatPercent(v float64) string {
	b, _ := json.Marshal(math.Round(v*100) / 100)
	return string(b)
}

// OptionsFromSettings arma las Options desde la configuración de la app. Recibe
// `any` (la store.Settings) y la lee por sus nombres JSON: así este paquete no
// depende de la base, y un setting que todavía no existe vale su default
// (mismo criterio que FormatScad con editorIndent). wallLoops, lineWidthMm,
// filamentDiameterMm y currency los agrega la tanda 7.B.
func OptionsFromSettings(settings any) Options {
	o := Options{
		InfillPercent:      DefaultInfillPercent,
		WallLoops:          DefaultWallLoops,
		LineWidthMm:        DefaultLineWidthMm,
		FilamentDiameterMm: DefaultFilamentDiameterMm,
		DefaultMaterialID:  "pla",
		Currency:           DefaultCurrency,
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		return o
	}
	var s struct {
		InfillPercent      *float64 `json:"infillPercent"`
		WallLoops          int      `json:"wallLoops"`
		LineWidthMm        float64  `json:"lineWidthMm"`
		FilamentDiameterMm float64  `json:"filamentDiameterMm"`
		DefaultMaterial    string   `json:"defaultMaterial"`
		Currency           string   `json:"currency"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return o
	}
	if s.InfillPercent != nil && *s.InfillPercent >= 0 && *s.InfillPercent <= 100 {
		o.InfillPercent = *s.InfillPercent
	}
	if s.WallLoops > 0 {
		o.WallLoops = s.WallLoops
	}
	if s.LineWidthMm > 0 {
		o.LineWidthMm = s.LineWidthMm
	}
	if s.FilamentDiameterMm > 0 {
		o.FilamentDiameterMm = s.FilamentDiameterMm
	}
	if s.DefaultMaterial != "" {
		o.DefaultMaterialID = s.DefaultMaterial
	}
	if s.Currency != "" {
		o.Currency = s.Currency
	}
	return o
}

// Merge completa las Options que llegan del frontend con las de la
// configuración. Los campos que no pueden ser cero (perímetros, ancho de
// línea, diámetro, material, moneda) se completan cuando faltan.
//
// El relleno es el caso delicado: 0 % es válido y también es el valor de un
// campo ausente. Se toma tal cual si el llamador especificó cualquiera de los
// otros tres números (el panel Impresión manda siempre el conjunto completo);
// si no especificó ninguno, un 0 se lee como «sin dato» y vale el de la
// configuración.
func Merge(given, base Options) Options {
	explicit := given.WallLoops > 0 || given.LineWidthMm > 0 || given.FilamentDiameterMm > 0
	out := given
	if !explicit && given.InfillPercent == 0 {
		out.InfillPercent = base.InfillPercent
	}
	if out.WallLoops <= 0 {
		out.WallLoops = base.WallLoops
	}
	if out.LineWidthMm <= 0 {
		out.LineWidthMm = base.LineWidthMm
	}
	if out.FilamentDiameterMm <= 0 {
		out.FilamentDiameterMm = base.FilamentDiameterMm
	}
	if out.DefaultMaterialID == "" {
		out.DefaultMaterialID = base.DefaultMaterialID
	}
	if out.Currency == "" {
		out.Currency = base.Currency
	}
	return out
}
