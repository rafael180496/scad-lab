package scad

import (
	"encoding/json"
	"io/fs"
	"sync"

	"scadlab/assets"
)

// Catálogo del lenguaje (assets/scad/catalog.json, embebido): palabras clave,
// módulos y funciones integrados y variables especiales, con firma, parámetros
// y documentación en los dos idiomas. Lo usa el editor para el autocompletado
// y el hover; el idioma lo elige el frontend, por eso viajan los dos.

// Catalog es el catálogo completo.
type Catalog struct {
	Version string         `json:"version"` // versión de OpenSCAD que describe (mínima soportada)
	Entries []CatalogEntry `json:"entries"`
}

// CatalogEntry es una palabra del lenguaje.
type CatalogEntry struct {
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`      // "module" | "function" | "keyword" | "special"
	Signature string         `json:"signature"` // cylinder(h, r1, r2, center, r, d, d1, d2)
	Params    []CatalogParam `json:"params"`
	Doc       LocalizedText  `json:"doc"`
	Since     string         `json:"since,omitempty"` // versión de OpenSCAD que lo agregó, si es posterior a Version
}

// CatalogParam es un parámetro con su descripción.
type CatalogParam struct {
	Name string        `json:"name"`
	Doc  LocalizedText `json:"doc"`
}

// LocalizedText es un texto en español e inglés.
type LocalizedText struct {
	ES string `json:"es"`
	EN string `json:"en"`
}

var (
	catalogOnce sync.Once
	catalog     Catalog
	catalogErr  error
)

// LoadCatalog lee el catálogo embebido (una sola vez por proceso).
func LoadCatalog() (Catalog, error) {
	catalogOnce.Do(func() {
		raw, err := fs.ReadFile(assets.FS, "scad/catalog.json")
		if err != nil {
			catalogErr = err
			return
		}
		catalogErr = json.Unmarshal(raw, &catalog)
	})
	return catalog, catalogErr
}
