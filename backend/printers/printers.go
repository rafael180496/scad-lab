// Package printers expone el catálogo de impresoras. La fuente son los
// references/printers.json de cada skill de marca (assets/skills/<marca>-printers):
// el mismo archivo que lee el agente IA dentro de su skill, así la app y la IA
// tienen una sola definición y no se desincronizan.
//
// El catálogo está partido por marca (esquema v2, ver
// .claude/specs/s14-impresoras-multimarca.md): cada archivo trae un bloque
// `brand` {id, name, skill} y sus impresoras. El cargador junta todos los que
// encuentre en el binario (skills/*/references/printers.json), así una marca
// nueva es una carpeta nueva y no un cambio de Go. Las skills de usuario no
// cambian el catálogo: solo se lee lo embebido.
package printers

import (
	"encoding/json"
	"io/fs"
	"path"
	"sort"
	"sync"

	"scadlab/assets"
	"scadlab/backend/i18n"
)

// catalogGlob busca el catálogo de cada marca dentro del binario.
const catalogGlob = "skills/*/references/printers.json"

// fallbackID es la impresora que devuelve ByID cuando el id no existe: la más
// chica del catálogo, y por eso la elección segura (nada que se diseñe para ella
// deja de caber en otra).
const fallbackID = "bambu-a1-mini"

// brandOrder es el orden fijo de las marcas en la app. Una marca que no figure
// aquí va después de estas, por id.
var brandOrder = []string{"bambu", "prusa", "creality", "elegoo", "anycubic", "snapmaker", "ultimaker"}

// Volume es el volumen de impresión en mm.
type Volume struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Brand es una marca del catálogo y la skill que la documenta.
type Brand struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Skill string `json:"skill"`
}

// Printer es una impresora del catálogo (una entrada por modelo comercial).
//
// MulticolorSlots es, en el catálogo, el MÁXIMO de colores de la máquina; en una
// impresora EFECTIVA de un workspace (wsconfig.EffectivePrinter) es la cantidad
// de colores que ese workspace eligió. MaxColors conserva siempre el máximo.
type Printer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Model string `json:"model"`
	// BrandID, BrandName y Skill salen del bloque `brand` del archivo: el
	// cargador los copia a cada impresora para que quien la reciba no tenga que
	// volver a buscar su marca.
	BrandID   string `json:"brand_id"`
	BrandName string `json:"brand_name"`
	Skill     string `json:"skill"`

	BuildVolume        Volume  `json:"build_volume"`
	NozzleMM           float64 `json:"nozzle_mm"`
	NozzleCount        int     `json:"nozzle_count"`
	FilamentDiameterMM float64 `json:"filament_diameter_mm"`

	// MulticolorKind: ams | mmu | toolchanger | cfs | ace | idex | dual | none.
	MulticolorKind  string `json:"multicolor_kind"`
	MulticolorSys   string `json:"multicolor_system"`
	MulticolorSlots int    `json:"multicolor_slots"`
	MaxColors       int    `json:"max_colors"`
	DefaultColors   int    `json:"default_colors"`

	Enclosed       bool   `json:"enclosed"`
	HeatedChamber  bool   `json:"heated_chamber"`
	BedSurface     string `json:"bed_surface"`
	MaxNozzleTempC int    `json:"max_nozzle_temp_c"`
	MaxBedTempC    int    `json:"max_bed_temp_c"`

	// Slicers son ids del registro de backend/slicers; el primero es el
	// recomendado.
	Slicers []string `json:"slicers"`
	Notes   string   `json:"notes"`
	Sources []string `json:"sources"`
	// Verify lista los campos que no se pudieron confirmar con la ficha oficial.
	Verify []string `json:"verify"`
}

// file es la forma de un printers.json v2.
type file struct {
	SchemaVersion int       `json:"schema_version"`
	Brand         Brand     `json:"brand"`
	Printers      []Printer `json:"printers"`
}

var (
	once      sync.Once
	list      []Printer
	brandList []Brand
	loadErr   error
)

func load() {
	files, err := fs.Glob(assets.FS, catalogGlob)
	if err != nil {
		loadErr = err
		return
	}
	var (
		brands = map[string]Brand{}
		byID   = map[string]bool{}
		all    []Printer
	)
	for _, name := range files {
		raw, err := fs.ReadFile(assets.FS, name)
		if err != nil {
			loadErr = err
			return
		}
		var f file
		if err := json.Unmarshal(raw, &f); err != nil {
			loadErr = i18n.Errorf(i18n.Msg{ES: "catálogo de impresoras %s: %w", EN: "printer catalog %s: %w"}, name, err)
			return
		}
		if f.Brand.ID == "" {
			loadErr = i18n.Errorf(i18n.Msg{ES: "catálogo de impresoras %s: falta el bloque brand", EN: "printer catalog %s: the brand block is missing"}, name)
			return
		}
		if f.Brand.Skill == "" {
			// Por convención la skill de la marca es la carpeta del archivo.
			f.Brand.Skill = path.Base(path.Dir(path.Dir(name)))
		}
		if _, dup := brands[f.Brand.ID]; dup {
			loadErr = i18n.Errorf(i18n.Msg{ES: "la marca %q está en dos catálogos", EN: "brand %q appears in two catalogs"}, f.Brand.ID)
			return
		}
		brands[f.Brand.ID] = f.Brand
		for _, p := range f.Printers {
			if byID[p.ID] {
				loadErr = i18n.Errorf(i18n.Msg{ES: "el id de impresora %q está repetido", EN: "printer id %q is duplicated"}, p.ID)
				return
			}
			byID[p.ID] = true
			p.BrandID, p.BrandName, p.Skill = f.Brand.ID, f.Brand.Name, f.Brand.Skill
			fill(&p)
			all = append(all, p)
		}
	}
	rank := func(id string) int {
		for i, b := range brandOrder {
			if b == id {
				return i
			}
		}
		return len(brandOrder)
	}
	less := func(a, b string) bool {
		if ra, rb := rank(a), rank(b); ra != rb {
			return ra < rb
		}
		return a < b
	}
	for _, b := range brands {
		brandList = append(brandList, b)
	}
	sort.Slice(brandList, func(i, j int) bool { return less(brandList[i].ID, brandList[j].ID) })
	// SliceStable: dentro de una marca se respeta el orden del archivo.
	sort.SliceStable(all, func(i, j int) bool { return less(all[i].BrandID, all[j].BrandID) })
	list = all
}

// fill completa lo que el archivo puede dejar sin decir.
func fill(p *Printer) {
	// Un número desconocido viene como 0 (y su campo en verify): el máximo de
	// colores nunca baja de 1, o un workspace no podría elegir ninguno.
	p.MaxColors = p.MulticolorSlots
	if p.MaxColors <= 0 {
		p.MaxColors = max(p.DefaultColors, 1)
	}
	if p.NozzleCount <= 0 {
		p.NozzleCount = 1
	}
	if p.Slicers == nil {
		p.Slicers = []string{}
	}
	if p.Sources == nil {
		p.Sources = []string{}
	}
	if p.Verify == nil {
		p.Verify = []string{}
	}
}

// All devuelve el catálogo, ordenado por marca (ver brandOrder) y, dentro de
// cada una, como lo trae su archivo. Se carga una sola vez desde el binario.
func All() ([]Printer, error) {
	once.Do(load)
	return list, loadErr
}

// Brands devuelve las marcas del catálogo en el orden de la app.
func Brands() ([]Brand, error) {
	once.Do(load)
	return brandList, loadErr
}

// ByID busca una impresora. Si no existe devuelve la A1 mini (y false): la más
// chica, y por eso la elección segura. Solo si el catálogo no la trae devuelve la
// primera.
func ByID(id string) (Printer, bool) {
	all, err := All()
	if err != nil || len(all) == 0 {
		return Printer{}, false
	}
	var fallback *Printer
	for i, p := range all {
		if p.ID == id {
			return p, true
		}
		if p.ID == fallbackID {
			fallback = &all[i]
		}
	}
	if fallback != nil {
		return *fallback, false
	}
	return all[0], false
}

// BrandSkills son los nombres de las skills de marca (una por marca del
// catálogo). Un workspace no las recibe todas: se elige la de su impresora.
func BrandSkills() map[string]bool {
	out := map[string]bool{}
	bs, _ := Brands()
	for _, b := range bs {
		if b.Skill != "" {
			out[b.Skill] = true
		}
	}
	return out
}

// Colors es la cantidad de colores a usar con esta impresora: n si está entre 1
// y el máximo; n <= 0 pide el valor por defecto (DefaultColors, o el máximo si
// no lo trae, o 1). Fuera de rango se recorta a 1..máximo.
func (p Printer) Colors(n int) int {
	top := p.MaxColors
	if top <= 0 {
		top = p.MulticolorSlots
	}
	if top <= 0 {
		top = 1
	}
	if n <= 0 {
		n = p.DefaultColors
		if n <= 0 {
			n = top
		}
	}
	return max(1, min(n, top))
}
