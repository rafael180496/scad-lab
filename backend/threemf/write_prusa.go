package threemf

import (
	"archive/zip"
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"scadlab/backend/i18n"
)

// pathPrusaModelConfig es donde PrusaSlicer lee las partes (volúmenes) de un
// objeto y el extrusor de cada una. PrusaSlicer NO lee model_settings.config (el
// archivo de Bambu/Orca): sin éste, las partes de nuestro 3MF normal llegan como
// objetos separados y sin extrusor.
const pathPrusaModelConfig = "Metadata/Slic3r_PE_model.config"

// WritePrusaFile escribe el 3MF para PrusaSlicer en disco de forma atómica
// (temporal + rename), igual que WriteFile.
func WritePrusaFile(dest string, parts []Part, opts WriteOptions) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".scadlab-*.3mf.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := WritePrusa(tmp, parts, opts); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}

// WritePrusa escribe un 3MF que PrusaSlicer abre como UN objeto con una parte
// (volumen) por color y su extrusor.
//
// Estructura (leída del código de PrusaSlicer 2.9, 3mf.cpp; no probada con la
// aplicación, ver claude/11-exportacion.md):
//
//   - 3D/3dmodel.model: UN solo <object> con una sola malla en la que están los
//     vértices y los triángulos de todas las partes, uno tras otro. Sin
//     <components>: PrusaSlicer instancia cada componente como un objeto aparte,
//     y lo que se quiere es un objeto con varios volúmenes.
//   - Metadata/Slic3r_PE_model.config: un <volume firstid lastid> por parte, con
//     el rango de triángulos de esa parte (contiguo, sin huecos), su nombre y su
//     extrusor (1-based). Los rangos cubren la malla entera.
//
// No lleva <basematerials> ni slic3rpe:Version3mf a propósito: el color del
// filamento lo da el perfil del usuario, y sin la versión PrusaSlicer trata el
// archivo como «de otro fabricante» (que es lo que es) en vez de esperar los
// metadatos de uno propio.
func WritePrusa(w io.Writer, parts []Part, opts WriteOptions) error {
	// Un volumen sin triángulos daría un rango firstid > lastid, que PrusaSlicer
	// rechaza; Split no los produce, pero WritePrusa recibe partes de cualquiera.
	nonEmpty := make([]Part, 0, len(parts))
	for _, p := range parts {
		if len(p.Triangles) > 0 {
			nonEmpty = append(nonEmpty, p)
		}
	}
	if parts = nonEmpty; len(parts) == 0 {
		return errNoMesh
	}
	verts, tris, ranges := concatParts(parts)
	zw := zip.NewWriter(w)
	entry := func(name string, fn func(*bufio.Writer)) error {
		ew, err := zw.Create(name)
		if err != nil {
			return err
		}
		bw := bufio.NewWriterSize(ew, 1<<16)
		fn(bw)
		return bw.Flush()
	}
	if err := entry(pathContentTypes, writeContentTypes); err != nil {
		return err
	}
	if err := entry(pathRels, writeRels); err != nil {
		return err
	}
	if err := entry(pathModel, func(bw *bufio.Writer) { writePrusaModel(bw, parts, verts, tris, opts) }); err != nil {
		return err
	}
	if err := entry(pathPrusaModelConfig, func(bw *bufio.Writer) { writePrusaConfig(bw, parts, ranges, opts) }); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no se pudo cerrar el 3MF: %w", EN: "could not finish the 3MF: %w"}, err)
	}
	return nil
}

// triRange es el rango de triángulos [first, last] (inclusivo) de una parte
// dentro de la malla concatenada.
type triRange struct{ first, last int }

// concatParts junta los vértices y triángulos de todas las partes en una sola
// malla. Los índices de los triángulos de cada parte se desplazan por la
// cantidad de vértices anteriores; el orden de partes y de triángulos se
// conserva, así cada parte ocupa un tramo contiguo.
func concatParts(parts []Part) (verts [][3]float64, tris [][3]int32, ranges []triRange) {
	var nv, nt int
	for _, p := range parts {
		nv += len(p.Vertices)
		nt += len(p.Triangles)
	}
	verts = make([][3]float64, 0, nv)
	tris = make([][3]int32, 0, nt)
	ranges = make([]triRange, len(parts))
	for i, p := range parts {
		base := int32(len(verts))
		verts = append(verts, p.Vertices...)
		ranges[i].first = len(tris)
		for _, t := range p.Triangles {
			tris = append(tris, [3]int32{t[0] + base, t[1] + base, t[2] + base})
		}
		ranges[i].last = len(tris) - 1
	}
	return verts, tris, ranges
}

// prusaObjectID es el id del único objeto: el mismo en el modelo y en el config.
const prusaObjectID = "1"

func writePrusaModel(w *bufio.Writer, parts []Part, verts [][3]float64, tris [][3]int32, opts WriteOptions) {
	w.WriteString(xmlHeader)
	w.WriteString(`<model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">` + "\n")
	w.WriteString(` <metadata name="Application">` + applicationName + `</metadata>` + "\n")
	w.WriteString(` <metadata name="Title">` + esc(opts.Title) + `</metadata>` + "\n")
	w.WriteString(` <resources>` + "\n")
	w.WriteString(`  <object id="` + prusaObjectID + `" name="` + esc(opts.Title) + `" type="model">` + "\n")
	writeMesh(w, verts, tris)
	w.WriteString("  </object>\n </resources>\n <build>\n")
	w.WriteString(`  <item objectid="` + prusaObjectID + `" transform="` + itemTransform(parts, opts) + `" printable="1"/>` + "\n")
	w.WriteString(" </build>\n</model>\n")
}

func writePrusaConfig(w *bufio.Writer, parts []Part, ranges []triRange, opts WriteOptions) {
	w.WriteString(xmlHeader)
	w.WriteString("<config>\n")
	w.WriteString(` <object id="` + prusaObjectID + `" instances_count="1">` + "\n")
	w.WriteString(`  <metadata type="object" key="name" value="` + esc(opts.Title) + `"/>` + "\n")
	w.WriteString(`  <metadata type="object" key="extruder" value="1"/>` + "\n")
	for i, p := range parts {
		w.WriteString(`  <volume firstid="` + strconv.Itoa(ranges[i].first) + `" lastid="` + strconv.Itoa(ranges[i].last) + `">` + "\n")
		w.WriteString(`   <metadata type="volume" key="name" value="` + esc(p.Name) + `"/>` + "\n")
		w.WriteString(`   <metadata type="volume" key="volume_type" value="ModelPart"/>` + "\n")
		w.WriteString(`   <metadata type="volume" key="extruder" value="` + strconv.Itoa(p.Extruder) + `"/>` + "\n")
		w.WriteString("  </volume>\n")
	}
	w.WriteString(" </object>\n")
	w.WriteString("</config>\n")
}
