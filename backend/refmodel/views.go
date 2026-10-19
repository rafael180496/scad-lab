package refmodel

import (
	"strings"

	"scadlab/backend/i18n"
)

// View es una cámara: la rotación (rx, ry, rz) del `--camera` de OpenSCAD y si
// la proyección es ortogonal. Es la tabla que comparte la herramienta `preview`
// del servidor MCP (que la pasa a `openscad --camera`) y la vista previa de una
// referencia (que la pasa a backend/raster): una referencia y el modelo que se
// dibuja a partir de ella se miran desde los mismos ángulos.
//
// Las vistas por eje son ortogonales (se pueden medir a ojo); iso es en
// perspectiva, como la vista por defecto de OpenSCAD. Los ángulos se
// comprobaron renderizando una pieza asimétrica. Ojo con right/left: en la GUI
// de OpenSCAD «Right» es 90,0,270, que mira desde −X (la convención contraria
// a la de los planos). Aquí `right` se ve desde +X.
type View struct {
	Rot   [3]int
	Ortho bool
}

// Views son las cámaras por nombre.
var Views = map[string]View{
	"iso":      {Rot: [3]int{55, 0, 25}},
	"iso-back": {Rot: [3]int{55, 0, 205}},
	"front":    {Rot: [3]int{90, 0, 0}, Ortho: true},   // desde −Y
	"back":     {Rot: [3]int{90, 0, 180}, Ortho: true}, // desde +Y
	"right":    {Rot: [3]int{90, 0, 90}, Ortho: true},  // desde +X
	"left":     {Rot: [3]int{90, 0, 270}, Ortho: true}, // desde −X
	"top":      {Rot: [3]int{0, 0, 0}, Ortho: true},    // desde +Z
	"bottom":   {Rot: [3]int{180, 0, 0}, Ortho: true},  // desde −Z
}

// ViewNames es el orden en que se ofrecen las vistas.
var ViewNames = []string{"iso", "iso-back", "front", "back", "right", "left", "top", "bottom"}

// ParseView resuelve el nombre de una vista (vacío = iso, sin distinguir
// mayúsculas) y devuelve el nombre normalizado.
func ParseView(name string) (string, View, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		name = "iso"
	}
	v, ok := Views[name]
	if !ok {
		return "", View{}, i18n.Errorf(i18n.Msg{ES: "vista desconocida: %q (usa %s)", EN: "unknown view: %q (use %s)"}, name, strings.Join(ViewNames, ", "))
	}
	return name, v, nil
}
