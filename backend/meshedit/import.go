package meshedit

import (
	"math"
	"os"
	"path/filepath"
	"strings"

	"scadlab/backend/checkpoint"
	"scadlab/backend/i18n"
	"scadlab/backend/parts"
	"scadlab/backend/printers"
	"scadlab/backend/refmodel"
)

// BigMeshTriangles es el tamaño a partir del cual se avisa que la malla es muy
// grande: el render, los cortes y los agujeros crecen con los triángulos, y con
// CGAL un STL de medio millón puede tardar muchos minutos.
const BigMeshTriangles = 500_000

// ImportOptions ajusta ImportRef.
type ImportOptions struct {
	// CenterOnBed centra el modelo en XY y lo apoya en Z = 0: se traduce a un
	// translate() en el import base, el STL no se toca.
	CenterOnBed bool
	// Label es el título del checkpoint (su slug es el nombre del archivo). Vacío =
	// el nombre del STL.
	Label string
	// Source es el nombre con el que el usuario conoce el archivo (el original,
	// antes de que refs/ le cambiara el nombre). Vacío = el de refs/.
	Source string
	// Plate es el volumen de impresión de la impresora del workspace, para avisar
	// si el modelo no cabe. Cero = no se comprueba. PlateName es su nombre.
	Plate     printers.Volume
	PlateName string
}

// Imported es el resultado de importar un modelo.
type Imported struct {
	File     string          // el checkpoint base creado
	Ref      string          // el STL que importa (refs/…): el original, o el convertido de un 3MF/OBJ
	Report   refmodel.Report // análisis del archivo original
	Warnings []string        // en el idioma de la interfaz
}

// ImportRef crea el checkpoint base de un modelo que YA está en refs/ (file se
// resuelve con refmodel.ResolveRef: un solo nombre, formato conocido, nada fuera
// de refs/).
//
//   - Un 3MF o un OBJ se convierte a STL en refs/ (el original queda al lado):
//     `import()` de un 3MF no existe en todas las versiones de OpenSCAD.
//   - El checkpoint nace como raíz nueva del árbol (el modelo no es hijo de
//     ningún diseño), con la cabecera, un color() y la línea marcadora que lo
//     reconoce como import base (BaseOf).
//   - Nada se sobrescribe: ni el STL ni ningún .scad.
func ImportRef(wsDir, file string, o ImportOptions) (Imported, error) {
	abs, rel, err := refmodel.ResolveRef(wsDir, file)
	if err != nil {
		return Imported{}, err
	}
	report, err := refmodel.Analyze(abs)
	if err != nil {
		return Imported{}, err
	}
	ref, converted := rel, ""
	if refmodel.FormatOf(rel) != refmodel.FormatSTL {
		if ref, err = refmodel.ConvertToSTL(wsDir, rel); err != nil {
			return Imported{}, err
		}
		converted = ref
	}

	scene := BaseScene(ref, report, o.CenterOnBed)
	label := strings.TrimSpace(o.Label)
	if label == "" {
		label = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	}
	shown := o.Source
	if shown == "" {
		shown = filepath.Base(rel)
	}
	desc := i18n.T(i18n.Msg{ES: "Importa %s", EN: "Imports %s"}, shown)
	src, err := Generate(scene, Header{Desc: desc}, i18n.Lang())
	if err == nil {
		var name string
		if name, err = checkpoint.CreateDetached(wsDir, label, desc, src); err == nil {
			return Imported{File: name, Ref: ref, Report: report, Warnings: ImportWarnings(report, o)}, nil
		}
	}
	if converted != "" {
		_ = os.Remove(filepath.Join(wsDir, filepath.FromSlash(converted))) // lo creamos nosotros recién
	}
	return Imported{}, err
}

// BaseScene es la escena de un import base: el modelo tal cual, o centrado en XY
// y apoyado en Z = 0 según su caja.
func BaseScene(ref string, r refmodel.Report, centerOnBed bool) Scene {
	s := Scene{Ref: ref, Model: Transform{Scale: [3]float64{1, 1, 1}}}
	if centerOnBed {
		s.Model.Translate = [3]float64{tidy(-(r.Min[0] + r.Max[0]) / 2), tidy(-(r.Min[1] + r.Max[1]) / 2), tidy(-r.Min[2])}
	}
	return s
}

// tidy redondea a 6 decimales (los del código generado) y evita el «-0».
func tidy(v float64) float64 {
	v = math.Round(v*1e6) / 1e6
	if v == 0 {
		return 0
	}
	return v
}

// ImportWarnings son los avisos de antes de importar: lo que puede salir mal más
// adelante con ese modelo. Texto en el idioma de la interfaz.
func ImportWarnings(r refmodel.Report, o ImportOptions) []string {
	var out []string
	if !r.Watertight {
		out = append(out, i18n.T(i18n.Msg{
			ES: "La malla no es cerrada (tiene huecos o aristas sueltas): su volumen no es fiable y, aunque se ve bien, cualquier agujero, corte o unión fallará («mesh is not closed»: Manifold y CGAL la rechazan). Repárala antes, por ejemplo con la reparación del laminador.",
			EN: "The mesh is not watertight (it has holes or loose edges): its volume is unreliable and, although it looks fine, any hole, cut or union will fail (\"mesh is not closed\": Manifold and CGAL both reject it). Repair it first, for example with your slicer's repair.",
		}))
	}
	if r.Triangles > BigMeshTriangles {
		out = append(out, i18n.T(i18n.Msg{
			ES: "La malla tiene %d triángulos: renderizarla y operar sobre ella puede tardar mucho. Considera simplificarla antes.",
			EN: "The mesh has %d triangles: rendering it and operating on it can take a long time. Consider simplifying it first.",
		}, r.Triangles))
	}
	if r.Format == refmodel.Format3MF && len(r.Parts) > 1 {
		out = append(out, i18n.T(i18n.Msg{
			ES: "El 3MF tiene %d colores: al importarlo se junta todo en una sola malla y se pierden. En el editor puedes recolorear por pieza.",
			EN: "The 3MF has %d colors: importing flattens it into a single mesh and they are lost. In the editor you can recolor each piece.",
		}, len(r.Parts)))
	}
	if o.Plate.X > 0 || o.Plate.Y > 0 || o.Plate.Z > 0 {
		if fits, over := parts.Fit(r.Size, o.Plate); !fits {
			name := o.PlateName
			if name == "" {
				name = i18n.T(i18n.Msg{ES: "la impresora", EN: "the printer"})
			}
			out = append(out, i18n.T(i18n.Msg{
				ES: "Mide %.1f × %.1f × %.1f mm y no cabe en la placa de %s (%.0f × %.0f × %.0f mm): se pasa %.1f × %.1f × %.1f mm por eje. Escálalo o pártelo.",
				EN: "It measures %.1f × %.1f × %.1f mm and does not fit on the %s plate (%.0f × %.0f × %.0f mm): it overflows by %.1f × %.1f × %.1f mm per axis. Scale it or split it.",
			}, r.Size[0], r.Size[1], r.Size[2], name, o.Plate.X, o.Plate.Y, o.Plate.Z, over.X, over.Y, over.Z))
		}
	}
	return out
}
