package threemf

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
)

// Nombres de las entradas del paquete.
const (
	pathContentTypes  = "[Content_Types].xml"
	pathRels          = "_rels/.rels"
	pathModel         = "3D/3dmodel.model"
	pathModelSettings = "Metadata/model_settings.config"
)

// WriteOptions ajusta el 3MF de salida.
type WriteOptions struct {
	// Title es el nombre del objeto en Bambu Studio (el del checkpoint).
	Title string
	// BedCenter es dónde poner el centro de la pieza en la cama (mm). Un
	// proyecto de Bambu guarda la posición de cada objeto; sin esto, un modelo
	// centrado en el origen de OpenSCAD queda medio fuera de la placa.
	// Se ignora si Place es false.
	BedCenter [2]float64
	Place     bool
	// CuraExtruders agrega a cada parte `extruder_nr` (0-based) dentro de un
	// <metadatagroup>, que es donde UltiMaker Cura lee a qué extrusor va cada
	// objeto. Orca, Bambu Studio y Creality Print ignoran ese bloque (leen
	// model_settings.config). Solo se escribe en Write; WritePrusa no lo usa.
	CuraExtruders bool
}

// Bounds es el cajón que envuelve las partes.
func Bounds(parts []Part) (min, max [3]float64, ok bool) {
	min = [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
	max = [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, p := range parts {
		for _, v := range p.Vertices {
			ok = true
			for k := 0; k < 3; k++ {
				min[k] = math.Min(min[k], v[k])
				max[k] = math.Max(max[k], v[k])
			}
		}
	}
	return
}

// WriteFile escribe el 3MF en disco de forma atómica (temporal + rename): una
// exportación cortada a mitad no deja un archivo roto con el nombre elegido.
func WriteFile(dest string, parts []Part, opts WriteOptions) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".scadlab-*.3mf.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := Write(tmp, parts, opts); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}

// Write escribe un 3MF estilo Bambu Studio con una parte por color.
func Write(w io.Writer, parts []Part, opts WriteOptions) error {
	if len(parts) == 0 {
		return errNoMesh
	}
	zw := zip.NewWriter(w)
	entry := func(name string, fn func(*bufio.Writer)) error {
		// Deflate: un 3MF es texto repetitivo y baja ~10×.
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
	if err := entry(pathModel, func(bw *bufio.Writer) { writeModel(bw, parts, opts) }); err != nil {
		return err
	}
	if err := entry(pathModelSettings, func(bw *bufio.Writer) { writeModelSettings(bw, parts, opts) }); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return i18n.Errorf(i18n.Msg{ES: "no se pudo cerrar el 3MF: %w", EN: "could not finish the 3MF: %w"}, err)
	}
	return nil
}

const xmlHeader = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

func writeContentTypes(w *bufio.Writer) {
	w.WriteString(xmlHeader)
	w.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n")
	w.WriteString(` <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` + "\n")
	w.WriteString(` <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/>` + "\n")
	w.WriteString(` <Default Extension="config" ContentType="text/xml"/>` + "\n")
	w.WriteString(`</Types>` + "\n")
}

func writeRels(w *bufio.Writer) {
	w.WriteString(xmlHeader)
	w.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + "\n")
	w.WriteString(` <Relationship Target="/3D/3dmodel.model" Id="rel-1" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>` + "\n")
	w.WriteString(`</Relationships>` + "\n")
}

func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// nsMaterials es el espacio de nombres de la extensión Materials del 3MF, donde
// vive <m:colorgroup>. El prefijo «m:» no es adorno: el lector de Bambu Studio
// (y el de Orca) abre el XML con expat sin procesar espacios de nombres y compara
// el nombre de la etiqueta literal con "m:colorgroup" / "m:color"
// (bbs_3mf.cpp, COLOR_GROUP_TAG y COLOR_TAG). Con otro prefijo no ve ningún color.
const nsMaterials = "http://schemas.microsoft.com/3dmanufacturing/material/2015/02"

// ids: 1..N = un <m:colorgroup> por parte (un solo color cada uno), N+1..2N = una
// malla por parte, 2N+1 = el objeto que las agrupa. Los ids de materiales y de
// objetos comparten espacio en el 3MF, por eso no se repiten.
func objectIDs(parts []Part) (groupIDs, partIDs []int, container int) {
	n := len(parts)
	for i := range parts {
		groupIDs = append(groupIDs, 1+i)
		partIDs = append(partIDs, 1+n+i)
	}
	return groupIDs, partIDs, 2*n + 1
}

func writeModel(w *bufio.Writer, parts []Part, opts WriteOptions) {
	groupIDs, partIDs, container := objectIDs(parts)
	w.WriteString(xmlHeader)
	w.WriteString(`<model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02" xmlns:m="` + nsMaterials + `">` + "\n")
	w.WriteString(` <metadata name="Application">` + applicationName + `</metadata>` + "\n")
	w.WriteString(` <metadata name="Title">` + esc(opts.Title) + `</metadata>` + "\n")
	w.WriteString(` <resources>` + "\n")
	// El color de cada parte va en su propio <m:colorgroup>, no en <basematerials>:
	// Bambu Studio solo lee el primero (ver el comentario de applicationName).
	for i, p := range parts {
		w.WriteString(`  <m:colorgroup id="` + strconv.Itoa(groupIDs[i]) + `"><m:color color="` + p.Color.Hex() + `FF"/></m:colorgroup>` + "\n")
	}

	for i, p := range parts {
		w.WriteString(`  <object id="` + strconv.Itoa(partIDs[i]) + `" name="` + esc(p.Name) + `" type="model" pid="` + strconv.Itoa(groupIDs[i]) + `" pindex="0">` + "\n")
		if opts.CuraExtruders {
			// Va antes de <mesh>: es el orden del esquema 3MF (metadatagroup, luego
			// mesh o components). Cura resta 1: su primer extrusor es el 0.
			w.WriteString(`   <metadatagroup><metadata name="extruder_nr">` + strconv.Itoa(max(p.Extruder-1, 0)) + `</metadata></metadatagroup>` + "\n")
		}
		writeMesh(w, p.Vertices, p.Triangles)
		w.WriteString("  </object>\n")
	}
	w.WriteString(`  <object id="` + strconv.Itoa(container) + `" name="` + esc(opts.Title) + `" type="model">` + "\n   <components>\n")
	for _, id := range partIDs {
		w.WriteString(`    <component objectid="` + strconv.Itoa(id) + `"/>` + "\n")
	}
	w.WriteString("   </components>\n  </object>\n </resources>\n <build>\n")
	w.WriteString(`  <item objectid="` + strconv.Itoa(container) + `" transform="` + itemTransform(parts, opts) + `" printable="1"/>` + "\n")
	w.WriteString(" </build>\n</model>\n")
}

// writeMesh escribe un <mesh> (vértices y triángulos) con sangría de objeto.
func writeMesh(w *bufio.Writer, verts [][3]float64, tris [][3]int32) {
	var buf []byte
	w.WriteString(`   <mesh>` + "\n    <vertices>\n")
	for _, v := range verts {
		buf = append(buf[:0], `     <vertex x="`...)
		buf = strconv.AppendFloat(buf, v[0], 'f', -1, 64)
		buf = append(buf, `" y="`...)
		buf = strconv.AppendFloat(buf, v[1], 'f', -1, 64)
		buf = append(buf, `" z="`...)
		buf = strconv.AppendFloat(buf, v[2], 'f', -1, 64)
		buf = append(buf, "\"/>\n"...)
		w.Write(buf)
	}
	w.WriteString("    </vertices>\n    <triangles>\n")
	for _, t := range tris {
		buf = append(buf[:0], `     <triangle v1="`...)
		buf = strconv.AppendInt(buf, int64(t[0]), 10)
		buf = append(buf, `" v2="`...)
		buf = strconv.AppendInt(buf, int64(t[1]), 10)
		buf = append(buf, `" v3="`...)
		buf = strconv.AppendInt(buf, int64(t[2]), 10)
		buf = append(buf, "\"/>\n"...)
		w.Write(buf)
	}
	w.WriteString("    </triangles>\n   </mesh>\n")
}

// itemTransform mueve la pieza al centro de la cama y la apoya en Z=0. Los
// vértices quedan con las coordenadas del .scad (lo que ve el usuario en
// OpenSCAD); el cambio de lugar viaja en el <item>, como hace Bambu Studio.
func itemTransform(parts []Part, opts WriteOptions) string {
	var tx, ty, tz float64
	if mn, mx, ok := Bounds(parts); ok && opts.Place {
		tx = opts.BedCenter[0] - (mn[0]+mx[0])/2
		ty = opts.BedCenter[1] - (mn[1]+mx[1])/2
		tz = -mn[2]
	}
	f := func(v float64) string {
		if v == 0 {
			return "0" // sin «-0»
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return "1 0 0 0 1 0 0 0 1 " + f(tx) + " " + f(ty) + " " + f(tz)
}

func writeModelSettings(w *bufio.Writer, parts []Part, opts WriteOptions) {
	_, partIDs, container := objectIDs(parts)
	cid := strconv.Itoa(container)
	w.WriteString(xmlHeader)
	w.WriteString("<config>\n")
	w.WriteString(`  <object id="` + cid + `">` + "\n")
	w.WriteString(`    <metadata key="name" value="` + esc(opts.Title) + `"/>` + "\n")
	w.WriteString(`    <metadata key="extruder" value="1"/>` + "\n")
	for i, p := range parts {
		w.WriteString(`    <part id="` + strconv.Itoa(partIDs[i]) + `" subtype="normal_part">` + "\n")
		w.WriteString(`      <metadata key="name" value="` + esc(p.Name) + `"/>` + "\n")
		w.WriteString(`      <metadata key="extruder" value="` + strconv.Itoa(p.Extruder) + `"/>` + "\n")
		w.WriteString(`    </part>` + "\n")
	}
	w.WriteString("  </object>\n")
	w.WriteString("  <plate>\n")
	w.WriteString(`    <metadata key="plater_id" value="1"/>` + "\n")
	w.WriteString(`    <model_instance>` + "\n")
	w.WriteString(`      <metadata key="object_id" value="` + cid + `"/>` + "\n")
	w.WriteString(`      <metadata key="instance_id" value="0"/>` + "\n")
	w.WriteString(`    </model_instance>` + "\n")
	w.WriteString("  </plate>\n")
	w.WriteString("</config>\n")
}

// applicationName NO empieza con «BambuStudio-» ni dice «PrusaSlicer», a propósito.
// Lo que hace cada valor en Bambu Studio 02.08.02.61 (bbs_3mf.cpp, Model.cpp y
// Plater.cpp del tag v02.08.02.61; el 02.08.04.61 es igual en lo que importa):
//
//   - «BambuStudio-x.y.z»: m_is_bbl_3mf = true. Entonces NO se guarda el color por
//     triángulo (`if (!m_is_bbl_3mf)` de _handle_start_triangle y de
//     _generate_volumes_new), así que el cuadro «colores del 3MF → filamentos» nunca
//     aparece, y además se lee Metadata/project_settings.config, que tiene que ser el
//     completo (el parcial hizo caerse la CLI 02.08.02.61).
//   - «PrusaSlicer…»: Model::read_from_archive lo manda por el lector de Prusa
//     (check_3mf_from_prusa) y Bambu muestra «el 3mf no es de Bambu Lab».
//   - Cualquier otro valor: archivo de «otro fabricante». Se carga geometría, las
//     partes y el extrusor de model_settings.config, y el color de cada parte se
//     lee de <m:colorgroup> (ver writeModel). Con esos colores Bambu Studio abre
//     «Standard 3mf Import color»: por defecto («Append») agrega los colores del modelo
//     como filamentos nuevos, o los empareja con los que hay («Color match»), y al
//     aceptar asigna el extrusor de cada parte (ObjColorPanel::apply_volume_filament_ids).
//
// Sin ese cuadro, el extrusor 2 de una parte se pierde si el proyecto tiene un solo
// filamento: GLCanvas3D::reload_scene llama a ModelVolume::update_extruder_count, que
// borra el `extruder` mayor que la cantidad de filamentos, y el bloque que agrega
// filamentos en Plater::load_files corre ANTES de decidir load_config=false. Así
// funcionaba el 3MF anterior (con <basematerials>, que Bambu no lee): una sola pieza
// de un solo color.
const applicationName = "ScadLab"
