// Package bbscheck imita, en Go, lo que el lector de 3MF de Bambu Studio hace con un
// archivo de otro fabricante. Existe SOLO para las pruebas: ninguna prueba del repo abre
// Bambu Studio (regla 9 de .claude/rules/technical.md), así que la forma de comprobar que
// el 3MF de ScadLab llega como se espera es releerlo con las mismas reglas del código de
// Bambu. La app no lo importa.
//
// Fuente: github.com/bambulab/BambuStudio, tag v02.08.02.61 (la versión instalada del
// usuario; el 02.08.04.61 es igual en todo lo de abajo salvo lo que se indica):
//
//	src/libslic3r/Format/bbs_3mf.cpp  _BBS_3MF_Importer (lector del paquete)
//	src/libslic3r/Model.cpp           read_from_archive, looks_like_multipart_object,
//	                                  ModelVolume::update_extruder_count
//	src/libslic3r/Format/3mf.cpp      PrusaFileParser::check_3mf_from_prusa
//	src/libslic3r/ObjColorUtils.cpp   extract_colors_to_obj_dialog
//	src/slic3r/GUI/ObjColorDialog.cpp get_object_level_volume_color, apply_volume_filament_ids
//	src/slic3r/GUI/Plater.cpp         priv::load_files, check_project_config
//	src/slic3r/GUI/GLCanvas3D.cpp     reload_scene (normaliza el extrusor con la cantidad de filamentos)
//
// Los números de línea de los comentarios son los del tag v02.08.02.61, salvo los de
// 3mf.cpp, que son del v02.08.04.61 (ObjColorUtils.cpp y Color.cpp no cambian entre los dos).
package bbscheck

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// importError es un fallo del port (no un texto para el usuario: el paquete solo se usa en pruebas).
type importError struct{ msg string }

func (e *importError) Error() string { return e.msg }

func fail(format string, args ...any) error { return &importError{fmt.Sprintf(format, args...)} }

func wrap(what string, err error) error { return &importError{what + ": " + err.Error()} }

// Result es lo que Bambu Studio sacaría del paquete.
type Result struct {
	// Application es el metadato «Application» del 3dmodel.model.
	Application string
	// IsBBL: m_is_bbl_3mf (bbs_3mf.cpp:4226, «BambuStudio-» al principio de Application).
	IsBBL bool
	// FromPrusa: check_3mf_from_prusa (3mf.cpp:329 del 02.08.04.61, «PrusaSlicer» dentro de Application).
	FromPrusa bool
	// HasProjectConfig: hay Metadata/project_settings.config. Sin él config_loaded queda
	// vacío y, en 02.08.02.60/.61, Plater.cpp:8449 muestra «El archivo 3mf tiene una
	// configuración no válida» (check_project_config, Plater.cpp:320, devuelve false si no
	// hay nozzle_diameter). Es un defecto de esas dos versiones —el 02.08.03.65 lo corrige
	// con `if (config_loaded.empty()) return true`—; carga igual, con load_config=false.
	HasProjectConfig bool
	// ColorGroups: id de <m:colorgroup> → colores (m_group_id_to_color, bbs_3mf.cpp:3961).
	ColorGroups map[int][]string
	Objects     []Object
}

// Object es un objeto de la placa (un <item> del <build>).
type Object struct {
	ID       int
	Name     string
	Extruder int // `extruder` del objeto en model_settings.config (0 = no vino)
	Volumes  []Volume
}

// Volume es una parte (un <component>) del objeto.
type Volume struct {
	SubID     int
	Name      string
	Extruder  int // `extruder` de la parte en model_settings.config (0 = no vino)
	Triangles int
	// PID/PIndex son los del objeto malla; los triángulos sin pid/p1 propios los heredan
	// (bbs_3mf.cpp:4078-4107, solo `if (!m_is_bbl_3mf)`).
	PID, PIndex int
	// TriangleColors guarda (pid, p1) de cada triángulo; vacío si el archivo es de Bambu.
	TriangleColors []TriangleColor
}

// TriangleColor es TriangleColor de Bambu: el grupo y el índice de color (p1) de un triángulo.
type TriangleColor struct{ PID, Index int }

// Import lee un 3MF como lo hace Bambu Studio.
func Import(path string) (*Result, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	files := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
		files[f.Name] = b
	}
	model, ok := files["3D/3dmodel.model"]
	if !ok {
		return nil, fail("sin 3D/3dmodel.model")
	}
	r := &Result{ColorGroups: map[int][]string{}}
	_, r.HasProjectConfig = files["Metadata/project_settings.config"]

	parsed, err := parseModel(model)
	if err != nil {
		return nil, wrap("3dmodel.model", err)
	}
	r.Application = parsed.application
	r.IsBBL = strings.HasPrefix(parsed.application, "BambuStudio-")
	r.FromPrusa = strings.Contains(parsed.application, "PrusaSlicer")
	r.ColorGroups = parsed.groups

	var meta *settings
	if b, ok := files["Metadata/model_settings.config"]; ok {
		if meta, err = parseSettings(b); err != nil {
			return nil, wrap("model_settings.config", err)
		}
	}

	// _load_model_from_file, bbs_3mf.cpp:2230-2330: por cada <item> del <build>.
	for _, item := range parsed.items {
		obj, ok := parsed.objects[item]
		if !ok {
			return nil, fail("el <item> apunta a un objeto inexistente: %d", item)
		}
		out := Object{ID: item, Name: obj.name}
		// _generate_current_object_list: un objeto con <components> aporta sus
		// componentes; una malla suelta se aporta a sí misma.
		subs := []int{item}
		if len(obj.components) > 0 {
			subs = obj.components
		}
		var om *objectSettings
		if meta != nil {
			om = meta.objects[item]
		}
		if om != nil { // «config data found»: nombre y config del objeto, volúmenes de las <part>
			for _, kv := range om.meta {
				switch kv.key {
				case "name":
					out.Name = kv.value
				case "extruder":
					out.Extruder, _ = strconv.Atoi(kv.value)
				}
			}
		}
		for idx, sid := range subs {
			sub, ok := parsed.objects[sid]
			if !ok {
				return nil, fail("componente a un objeto inexistente: %d", sid)
			}
			if len(sub.triangles) == 0 {
				return nil, fail("el objeto %d no tiene triángulos (bbs_3mf.cpp: «found no trianges»)", sid)
			}
			v := Volume{SubID: sid, Name: sub.name, Triangles: len(sub.triangles), PID: sub.pid, PIndex: sub.pindex}
			// _generate_volumes_new, bbs_3mf.cpp:5236-5262: el metadato de volumen se busca
			// primero por posición y, si el id no coincide, por id.
			if om != nil {
				var vm *partSettings
				if idx < len(om.parts) && om.parts[idx].id == sid {
					vm = &om.parts[idx]
				} else {
					for i := range om.parts {
						if om.parts[i].id == sid {
							vm = &om.parts[i]
							break
						}
					}
				}
				if vm != nil {
					for _, kv := range vm.meta {
						switch kv.key {
						case "name":
							v.Name = kv.value
						case "extruder":
							v.Extruder, _ = strconv.Atoi(kv.value)
						}
					}
				}
			}
			if !r.IsBBL { // bbs_3mf.cpp:4078: el color por triángulo solo se guarda en archivos ajenos
				v.TriangleColors = triangleColors(sub)
			}
			out.Volumes = append(out.Volumes, v)
		}
		r.Objects = append(r.Objects, out)
	}
	if len(r.Objects) == 0 {
		return nil, fail("el <build> no tiene objetos")
	}
	return r, nil
}

// triangleColors es _handle_start_triangle (bbs_3mf.cpp:4078-4107): el pid del triángulo o,
// si no lo trae, el del objeto; p1 del triángulo o, si no lo trae, el pindex del objeto;
// y modify_import_color (los índices ausentes toman el primero válido).
func triangleColors(o *meshObject) []TriangleColor {
	var out []TriangleColor
	for _, t := range o.triangles {
		pid := o.pid
		if t.hasPID {
			pid = t.pid
		}
		if pid < 0 {
			continue // el lector no agrega color para este triángulo
		}
		idx := t.p1
		if !t.hasP1 {
			idx = o.pindex
		}
		out = append(out, TriangleColor{PID: pid, Index: idx})
	}
	return out
}

// rgba imita Slic3r::convert_color_string_to_rgba (Color.cpp:22): «#RRGGBB», «#RRGGBBAA»,
// con o sin «#». ok=false es UNDEFINE_COLOR.
func rgba(s string) (c [4]uint8, ok bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 && len(s) != 8 {
		return c, false
	}
	c[3] = 255
	for i := 0; i < len(s)/2; i++ {
		v, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			return c, false
		}
		c[i] = uint8(v)
	}
	return c, true
}

// Dialog es lo que Plater::load_files hace con los colores del archivo
// (Plater.cpp:8655-8680, `en_3mf_file_type != From_BBS`).
type Dialog struct {
	// Shown: `!color_group_map.empty() && !volume_color_data.empty()` y
	// extract_colors_to_obj_dialog devuelve true (algún color definido). Es el cuadro
	// «Standard 3mf Import color» (ObjColorDialog), que asigna cada color a un filamento.
	Shown bool
	// Colors son los colores distintos que pide emparejar (out.input_colors).
	Colors [][4]uint8
	// VolumeColors: el color de cada parte, por objeto, en el orden de las partes, si
	// get_object_level_volume_color (ObjColorDialog.cpp:43-62) lo resuelve; nil si no.
	VolumeColors [][]*[4]uint8
	// AssignsVolumeExtruders: apply_volume_filament_ids (ObjColorDialog.cpp:647) asigna
	// el extrusor de cada parte a partir de su color: todas las partes de un objeto con
	// más de una tienen color de objeto resuelto.
	AssignsVolumeExtruders bool
}

// ColorDialog calcula lo que pasaría con los colores del archivo.
func (r *Result) ColorDialog() Dialog {
	var d Dialog
	if r.IsBBL || len(r.ColorGroups) == 0 {
		return d
	}
	hasVolumeColors := false
	for _, o := range r.Objects {
		for _, v := range o.Volumes {
			if len(v.TriangleColors) == v.Triangles && v.Triangles > 0 {
				hasVolumeColors = true
			}
		}
	}
	if !hasVolumeColors {
		return d
	}
	// extract_colors_to_obj_dialog, ObjColorUtils.cpp:76-: colores usados por triángulo.
	used := false
	for _, o := range r.Objects {
		for _, v := range o.Volumes {
			if len(v.TriangleColors) != v.Triangles {
				continue
			}
			for _, tc := range v.TriangleColors {
				grp, ok := r.ColorGroups[tc.PID]
				if !ok || tc.Index < 0 || tc.Index >= len(grp) {
					continue
				}
				c, ok := rgba(grp[tc.Index])
				if !ok {
					continue
				}
				used = true
				dup := false
				for _, e := range d.Colors {
					if e == c {
						dup = true
					}
				}
				if !dup {
					d.Colors = append(d.Colors, c)
				}
			}
		}
	}
	d.Shown = used
	if !used {
		return d
	}
	d.AssignsVolumeExtruders = true
	for _, o := range r.Objects {
		var row []*[4]uint8
		for _, v := range o.Volumes {
			row = append(row, r.objectLevelColor(v))
			if row[len(row)-1] == nil {
				d.AssignsVolumeExtruders = false
			}
		}
		if len(o.Volumes) <= 1 {
			d.AssignsVolumeExtruders = false // `mapped_volumes.size() > 1`
		}
		d.VolumeColors = append(d.VolumeColors, row)
	}
	return d
}

// objectLevelColor es get_object_level_volume_color (ObjColorDialog.cpp:43-62): el color
// único de una parte cuyos triángulos usan todos el pid/pindex del objeto.
func (r *Result) objectLevelColor(v Volume) *[4]uint8 {
	if len(v.TriangleColors) != v.Triangles || v.PID < 0 || v.PIndex < 0 {
		return nil
	}
	grp, ok := r.ColorGroups[v.PID]
	if !ok || v.PIndex >= len(grp) {
		return nil
	}
	for _, tc := range v.TriangleColors {
		if tc.PID != v.PID || tc.Index != v.PIndex {
			return nil
		}
	}
	c, ok := rgba(grp[v.PIndex])
	if !ok {
		return nil
	}
	return &c
}

// ExtruderAfterLoad devuelve el extrusor efectivo de cada parte (por objeto) cuando el
// usuario NO usa el cuadro de colores (lo cancela, o su versión no lo tiene) y el
// proyecto tiene `filaments` filamentos. Sigue el final de _load_model_from_file
// (bbs_3mf.cpp:2364-2395) y GLCanvas3D::reload_scene (GLCanvas3D.cpp:3581), que llama a
// ModelVolume::update_extruder_count (Model.cpp:3417): un `extruder` mayor que la cantidad
// de filamentos se borra y la parte vuelve al filamento 1.
func (r *Result) ExtruderAfterLoad(filaments int) [][]int {
	var out [][]int
	for _, o := range r.Objects {
		row := make([]int, len(o.Volumes))
		for i, v := range o.Volumes {
			e := v.Extruder
			if len(o.Volumes) == 1 || e == 0 || e > filaments {
				e = 1
			}
			row[i] = e
		}
		out = append(out, row)
	}
	return out
}

// ---------------------------------------------------------------------------
// XML: expat SIN espacios de nombres (XML_ParserCreate(nullptr), bbs_3mf.cpp:2601). El
// nombre de la etiqueta se compara tal cual viene escrito, prefijo incluido; en Go eso es
// RawToken (no traduce prefijos) y reconstruir «prefijo:local».
// ---------------------------------------------------------------------------

type triangle struct {
	hasPID, hasP1 bool
	pid, p1       int
}

type meshObject struct {
	id           int
	name         string
	pid, pindex  int
	components   []int
	triangles    []triangle
	validType    bool
	declaredType string
}

type modelFile struct {
	application string
	groups      map[int][]string
	objects     map[int]*meshObject
	items       []int
}

func literal(n xml.Name) string {
	if n.Space != "" {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

func attr(se xml.StartElement, name string) (string, bool) {
	for _, a := range se.Attr {
		if literal(a.Name) == name {
			return a.Value, true
		}
	}
	return "", false
}

func attrInt(se xml.StartElement, name string, def int) (int, bool) {
	s, ok := attr(se, name)
	if !ok {
		return def, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def, false
	}
	return n, true
}

// bbs_is_valid_object_type (bbs_3mf.cpp:664): vacío o uno de los tipos del estándar.
func validObjectType(t string) bool {
	switch t {
	case "", "model", "solidsupport", "support", "surface", "other":
		return true
	}
	return false
}

func parseModel(b []byte) (*modelFile, error) {
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	m := &modelFile{groups: map[int][]string{}, objects: map[int]*meshObject{}}
	var cur *meshObject
	curGroup := -1
	inAppMeta := false
	for {
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch literal(t.Name) {
			case "metadata": // _handle_start_metadata: solo el nombre
				n, _ := attr(t, "name")
				inAppMeta = n == "Application"
			case "m:colorgroup": // COLOR_GROUP_TAG, bbs_3mf.cpp:198
				curGroup, _ = attrInt(t, "id", -1)
			case "m:color": // COLOR_TAG, bbs_3mf.cpp:199 → _handle_start_color:3961
				c, _ := attr(t, "color")
				m.groups[curGroup] = append(m.groups[curGroup], c)
			case "object": // _handle_start_object:3799
				typ, _ := attr(t, "type")
				if !validObjectType(typ) {
					cur = nil
					continue
				}
				o := &meshObject{pid: -1, pindex: -1, declaredType: typ}
				o.id, _ = attrInt(t, "id", -1)
				o.name, _ = attr(t, "name")
				o.pid, _ = attrInt(t, "pid", -1)
				o.pindex, _ = attrInt(t, "pindex", -1)
				cur = o
				m.objects[o.id] = o
			case "component":
				if cur != nil {
					id, _ := attrInt(t, "objectid", -1)
					cur.components = append(cur.components, id)
				}
			case "triangle": // _handle_start_triangle:4055
				if cur != nil {
					tr := triangle{}
					tr.pid, tr.hasPID = attrInt(t, "pid", -1)
					tr.p1, tr.hasP1 = attrInt(t, "p1", -1)
					cur.triangles = append(cur.triangles, tr)
				}
			case "item": // _handle_start_item: solo cuenta lo que está en el <build>
				id, _ := attrInt(t, "objectid", -1)
				m.items = append(m.items, id)
			}
		case xml.CharData:
			if inAppMeta {
				m.application += string(t)
			}
		case xml.EndElement:
			switch literal(t.Name) {
			case "metadata":
				inAppMeta = false
			case "object":
				cur = nil
			case "m:colorgroup":
				curGroup = -1
			}
		}
	}
	m.application = strings.TrimSpace(m.application)
	return m, nil
}

// ---------------------------------------------------------------------------
// Metadata/model_settings.config: _handle_start_config_xml_element (bbs_3mf.cpp:3655-).
// ---------------------------------------------------------------------------

type kv struct{ key, value string }

type partSettings struct {
	id   int
	meta []kv
}

type objectSettings struct {
	meta  []kv
	parts []partSettings
}

type settings struct{ objects map[int]*objectSettings }

func parseSettings(b []byte) (*settings, error) {
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	s := &settings{objects: map[int]*objectSettings{}}
	var obj *objectSettings
	inPlate := false
	inPart := false
	for {
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch literal(t.Name) {
			case "object": // _handle_start_config_object:4469: id repetido = error
				id, _ := attrInt(t, "id", -1)
				if _, dup := s.objects[id]; dup {
					return nil, fail("Duplicated object id: %d in model_settings.config", id)
				}
				obj = &objectSettings{}
				s.objects[id] = obj
			case "part", "volume": // _handle_start_config_volume:4494
				if obj == nil {
					return nil, fail("can not find object for part")
				}
				id, _ := attrInt(t, "id", -1)
				obj.parts = append(obj.parts, partSettings{id: id})
				inPart = true
			case "plate":
				inPlate = true
			case "metadata": // _handle_start_config_metadata:4561
				if inPlate || obj == nil {
					continue
				}
				k, _ := attr(t, "key")
				v, _ := attr(t, "value")
				if k == "" {
					continue
				}
				if inPart {
					p := &obj.parts[len(obj.parts)-1]
					p.meta = append(p.meta, kv{k, v})
				} else {
					obj.meta = append(obj.meta, kv{k, v})
				}
			}
		case xml.EndElement:
			switch literal(t.Name) {
			case "part", "volume":
				inPart = false
			case "object":
				obj = nil
			case "plate":
				inPlate = false
			}
		}
	}
	return s, nil
}
