package refmodel

import (
	"fmt"
	"strings"
)

// PromptText es el resumen de la referencia para el modelo de lenguaje: medidas
// en mm, partes con su color y cómo usarla desde OpenSCAD. rel es la ruta
// relativa al workspace ("refs/pieza.stl"); lang, "es" o cualquier otro valor
// para inglés.
//
// No pasa por i18n.Msg a propósito: el idioma del texto es el del prompt de
// Hermes (el que pidió quien llama), no el de la interfaz en este instante.
func (r Report) PromptText(rel, lang string) string {
	es := lang == "es"
	tr := func(esText, enText string) string {
		if es {
			return esText
		}
		return enText
	}
	var b strings.Builder
	format := strings.ToUpper(r.Format)

	fmt.Fprintf(&b, tr("Referencia: %s (%s, %d triángulos).\n", "Reference: %s (%s, %d triangles).\n"), rel, format, r.Triangles)
	fmt.Fprintf(&b, tr(
		"Medidas (se leen como mm): X %.2f × Y %.2f × Z %.2f. Caja: de (%.2f, %.2f, %.2f) a (%.2f, %.2f, %.2f).\n",
		"Size (read as mm): X %.2f × Y %.2f × Z %.2f. Bounding box: from (%.2f, %.2f, %.2f) to (%.2f, %.2f, %.2f).\n"),
		r.Size[0], r.Size[1], r.Size[2], r.Min[0], r.Min[1], r.Min[2], r.Max[0], r.Max[1], r.Max[2])
	fmt.Fprintf(&b, tr("Volumen: %.1f mm³ (%.2f cm³). Superficie: %.1f mm². ", "Volume: %.1f mm³ (%.2f cm³). Surface area: %.1f mm². "),
		r.VolumeMm3, r.VolumeMm3/1000, r.AreaMm2)
	if r.Watertight {
		b.WriteString(tr("Malla cerrada.\n", "Watertight mesh.\n"))
	} else {
		b.WriteString(tr(
			"Malla NO cerrada: el volumen y la superficie no son fiables.\n",
			"Mesh is NOT watertight: volume and surface area are unreliable.\n"))
	}

	if r.Format == Format3MF || len(r.Parts) > 1 {
		fmt.Fprintf(&b, tr("Partes (%d):\n", "Parts (%d):\n"), len(r.Parts))
		for _, p := range r.Parts {
			color := ""
			if p.Color != "" {
				color = " " + p.Color
			}
			fmt.Fprintf(&b, tr("- %s%s: %d triángulos, %.2f × %.2f × %.2f mm, %.1f mm³\n", "- %s%s: %d triangles, %.2f × %.2f × %.2f mm, %.1f mm³\n"),
				p.Name, color, p.Triangles, p.Size[0], p.Size[1], p.Size[2], p.VolumeMm3)
		}
	}

	b.WriteString(tr("Cómo usarla en OpenSCAD:\n", "How to use it in OpenSCAD:\n"))
	switch r.Format {
	case Format3MF:
		fmt.Fprintf(&b, tr(
			"- `import(\"%s\")` NO es fiable con un 3MF: no todas las versiones de OpenSCAD lo importan y pierde los colores. Reconstruye cada parte con primitivas y las medidas de arriba, un `color()` por parte.\n",
			"- `import(\"%s\")` is NOT reliable with a 3MF: not every OpenSCAD version imports it and it drops the colors. Rebuild each part with primitives and the sizes above, one `color()` per part.\n"), rel)
	case FormatOBJ:
		fmt.Fprintf(&b, tr(
			"- `import(\"%s\");` solo funciona en versiones recientes de OpenSCAD; si falla, reconstruye la pieza con primitivas. Entra en una sola pieza y sin color: envuélvela en `color()`.\n",
			"- `import(\"%s\");` only works in recent OpenSCAD versions; if it fails, rebuild the part with primitives. It comes in as one piece without color: wrap it in `color()`.\n"), rel)
	default:
		fmt.Fprintf(&b, tr(
			"- `color(\"gray\") import(\"%s\");` carga la malla tal cual, en una sola pieza y sin color propio; la ruta es relativa al .scad (los checkpoints están en la raíz del workspace).\n",
			"- `color(\"gray\") import(\"%s\");` loads the mesh as is, as one piece with no color of its own; the path is relative to the .scad (checkpoints live in the workspace root).\n"), rel)
	}
	if r.Format != Format3MF {
		fmt.Fprintf(&b, tr(
			"- Para compararla mientras modelas, déjala como fantasma con `%%import(\"%s\");` (no se renderiza ni cuenta en la medida).\n",
			"- To compare while you model, keep it as a ghost with `%%import(\"%s\");` (it is not rendered and does not count in measurements).\n"), rel)
	}
	b.WriteString(tr(
		"- Para copiar la forma: modélala con primitivas y parámetros que reproduzcan estas medidas, y compárala con `preview` desde las mismas vistas. Si el origen de la referencia no está en la placa, traslada por `-Min` para apoyarla en z=0.\n",
		"- To copy the shape: model it with primitives and parameters that reproduce these sizes, and compare it with `preview` from the same views. If the reference's origin is not on the plate, translate by `-Min` so it rests on z=0.\n"))
	return b.String()
}
