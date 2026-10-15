// Package printers expone el catálogo de impresoras. La fuente es el mismo
// printers.json que lee el agente IA dentro de la skill bambu-lab-printers:
// una sola definición para la app y para la IA, así no se desincronizan.
package printers

import (
	"encoding/json"
	"io/fs"
	"sync"

	"scadlab/assets"
)

const catalogPath = "skills/bambu-lab-printers/references/printers.json"

// Volume es el volumen de impresión en mm.
type Volume struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Printer es una impresora del catálogo.
type Printer struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	BuildVolume     Volume   `json:"build_volume"`
	NozzleMM        float64  `json:"nozzle_mm"`
	MulticolorSlots int      `json:"multicolor_slots"`
	MulticolorSys   string   `json:"multicolor_system"`
	Enclosed        bool     `json:"enclosed"`
	HeatedChamber   bool     `json:"heated_chamber"`
	Notes           string   `json:"notes"`
	Verify          []string `json:"verify"`
}

type catalog struct {
	Printers []Printer `json:"printers"`
}

var (
	once    sync.Once
	list    []Printer
	loadErr error
)

// All devuelve el catálogo (cargado una vez desde el binario).
func All() ([]Printer, error) {
	once.Do(func() {
		raw, err := fs.ReadFile(assets.FS, catalogPath)
		if err != nil {
			loadErr = err
			return
		}
		var c catalog
		if err := json.Unmarshal(raw, &c); err != nil {
			loadErr = err
			return
		}
		list = c.Printers
	})
	return list, loadErr
}

// ByID busca una impresora; si no existe devuelve la primera del catálogo
// (la A1 mini), que es la más chica y por eso la elección segura.
func ByID(id string) (Printer, bool) {
	all, err := All()
	if err != nil || len(all) == 0 {
		return Printer{}, false
	}
	for _, p := range all {
		if p.ID == id {
			return p, true
		}
	}
	return all[0], false
}
