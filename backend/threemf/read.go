// Package threemf lee el 3MF que exporta OpenSCAD (un objeto con color por
// triángulo) y escribe un 3MF multicolor en el formato que Bambu Studio
// entiende: un objeto con una parte por color y el extrusor de cada parte.
//
// Solo cubre lo que ScadLab necesita; no es una librería 3MF general. Ver
// claude/11-exportacion.md y assets/skills/openscad-3mf-multicolor.
package threemf

import (
	"archive/zip"
	"bufio"
	"encoding/xml"
	"io"
	"math"
	"os"
	"path"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
)

// RGB es un color sRGB sin alfa: para imprimir, dos colores que solo difieren
// en la transparencia son el mismo filamento.
type RGB [3]uint8

// Hex devuelve «#RRGGBB» en mayúsculas.
func (c RGB) Hex() string {
	const digits = "0123456789ABCDEF"
	b := [7]byte{'#'}
	for i, v := range c {
		b[1+2*i] = digits[v>>4]
		b[2+2*i] = digits[v&15]
	}
	return string(b[:])
}

// Material es una entrada de <basematerials> (o <m:colorgroup>).
type Material struct {
	Name  string
	Color RGB
}

// Triangle son tres índices a Mesh.Vertices y el material (índice a
// Mesh.Materials) con que se pintó; -1 si no trae color.
type Triangle struct {
	V   [3]int32
	Mat int32
}

// Mesh es el contenido del 3MF aplanado: todos los objetos de <build> ya
// transformados a un solo espacio, en mm.
type Mesh struct {
	Title     string
	Vertices  [][3]float64
	Triangles []Triangle
	Materials []Material
}

var errNoMesh = i18n.New(i18n.Msg{
	ES: "el 3MF no contiene ninguna malla",
	EN: "the 3MF does not contain any mesh",
})

// ReadFile lee un 3MF de disco.
func ReadFile(p string) (*Mesh, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return Read(f, st.Size())
}

// Read lee un 3MF (zip). Sigue la relación raíz de _rels/.rels hasta el modelo;
// si falta, usa 3D/3dmodel.model. Resuelve <components> (también los de otro
// archivo del paquete, como hace Bambu), transformaciones y el color por
// triángulo o por objeto.
func Read(r io.ReaderAt, size int64) (*Mesh, error) {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "el 3MF no es un zip válido: %w", EN: "the 3MF is not a valid zip: %w"}, err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[strings.ToLower(strings.TrimPrefix(f.Name, "/"))] = f
	}
	rootName := rootModel(files)
	root, ok := files[strings.ToLower(rootName)]
	if !ok {
		return nil, i18n.Errorf(i18n.Msg{ES: "el 3MF no tiene %s", EN: "the 3MF has no %s"}, rootName)
	}
	d := &doc{files: files, models: map[string]*model{}}
	m, err := d.load(root, rootName)
	if err != nil {
		return nil, err
	}
	return d.flatten(m)
}

// rootModel busca en _rels/.rels el destino de la relación 3dmodel.
func rootModel(files map[string]*zip.File) string {
	const def = "3D/3dmodel.model"
	f := files["_rels/.rels"]
	if f == nil {
		return def
	}
	rc, err := f.Open()
	if err != nil {
		return def
	}
	defer rc.Close()
	dec := xml.NewDecoder(bufio.NewReader(rc))
	for {
		tok, err := dec.Token()
		if err != nil {
			return def
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		var target, typ string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Target":
				target = a.Value
			case "Type":
				typ = a.Value
			}
		}
		if strings.HasSuffix(typ, "/3dmodel") && target != "" {
			return strings.TrimPrefix(target, "/")
		}
	}
}

type matrix [12]float64 // m00 m01 m02 m10 m11 m12 m20 m21 m22 m30 m31 m32 (vector fila)

var identity = matrix{1, 0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0}

func (m matrix) apply(p [3]float64) [3]float64 {
	return [3]float64{
		p[0]*m[0] + p[1]*m[3] + p[2]*m[6] + m[9],
		p[0]*m[1] + p[1]*m[4] + p[2]*m[7] + m[10],
		p[0]*m[2] + p[1]*m[5] + p[2]*m[8] + m[11],
	}
}

// mul devuelve «primero m, después o» (convención de vector fila de 3MF).
func (m matrix) mul(o matrix) matrix {
	var r matrix
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			r[i*3+j] = m[i*3]*o[j] + m[i*3+1]*o[3+j] + m[i*3+2]*o[6+j]
		}
	}
	for j := 0; j < 3; j++ {
		r[9+j] = m[9]*o[j] + m[10]*o[3+j] + m[11]*o[6+j] + o[9+j]
	}
	return r
}

func parseMatrix(s string) matrix {
	f := strings.Fields(s)
	if len(f) != 12 {
		return identity
	}
	var m matrix
	for i, v := range f {
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return identity
		}
		m[i] = x
	}
	return m
}

type component struct {
	path  string // "" = el mismo archivo
	id    string
	trans matrix
}

type object struct {
	pid, pindex string
	verts       [][3]float64
	tris        []rawTri
	comps       []component
}

type rawTri struct {
	v   [3]int32
	pid string // internado: millones de triángulos repiten el mismo
	p1  int32  // -1 = el triángulo no trae color propio
}

type colorGroup []Material

type model struct {
	title   string
	groups  map[string]colorGroup
	objects map[string]*object
	build   []component
}

type doc struct {
	files  map[string]*zip.File
	models map[string]*model
}

func (d *doc) load(f *zip.File, name string) (*model, error) {
	key := strings.ToLower(name)
	if m, ok := d.models[key]; ok {
		return m, nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	m, err := parseModel(bufio.NewReaderSize(rc, 1<<16))
	if err != nil {
		return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo leer %s: %w", EN: "could not read %s: %w"}, name, err)
	}
	d.models[key] = m
	return m, nil
}

func attr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local && a.Name.Space != "xmlns" {
			return a.Value
		}
	}
	return ""
}

func parseModel(r io.Reader) (*model, error) {
	dec := xml.NewDecoder(r)
	m := &model{groups: map[string]colorGroup{}, objects: map[string]*object{}}
	interned := map[string]string{}
	var (
		curGroup  string
		inGroup   bool
		curObj    *object
		inBuild   bool
		inTitle   bool
		titleBuf  strings.Builder
		curObjID  string
		inObjMesh bool
	)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "metadata":
				if attr(t, "name") == "Title" {
					inTitle = true
					titleBuf.Reset()
				}
			case "basematerials", "colorgroup":
				curGroup = attr(t, "id")
				inGroup = true
				m.groups[curGroup] = nil
			case "base":
				if inGroup {
					m.groups[curGroup] = append(m.groups[curGroup], Material{
						Name: attr(t, "name"), Color: parseColor(attr(t, "displaycolor")),
					})
				}
			case "color":
				if inGroup {
					m.groups[curGroup] = append(m.groups[curGroup], Material{Color: parseColor(attr(t, "color"))})
				}
			case "object":
				curObjID = attr(t, "id")
				curObj = &object{pid: attr(t, "pid"), pindex: attr(t, "pindex")}
				m.objects[curObjID] = curObj
			case "mesh":
				inObjMesh = curObj != nil
			case "vertex":
				if inObjMesh {
					curObj.verts = append(curObj.verts, [3]float64{num(attr(t, "x")), num(attr(t, "y")), num(attr(t, "z"))})
				}
			case "triangle":
				if inObjMesh {
					rt := rawTri{v: [3]int32{int32(num(attr(t, "v1"))), int32(num(attr(t, "v2"))), int32(num(attr(t, "v3")))}, p1: -1}
					if p1 := attr(t, "p1"); p1 != "" {
						rt.p1 = int32(num(p1))
						if pid := attr(t, "pid"); pid != "" {
							if _, ok := interned[pid]; !ok {
								interned[pid] = pid
							}
							rt.pid = interned[pid]
						}
					}
					curObj.tris = append(curObj.tris, rt)
				}
			case "component":
				if curObj != nil {
					curObj.comps = append(curObj.comps, component{
						path: strings.TrimPrefix(attr(t, "path"), "/"), id: attr(t, "objectid"),
						trans: parseMatrix(attr(t, "transform")),
					})
				}
			case "build":
				inBuild = true
			case "item":
				if inBuild {
					m.build = append(m.build, component{id: attr(t, "objectid"), trans: parseMatrix(attr(t, "transform"))})
				}
			}
		case xml.CharData:
			if inTitle {
				titleBuf.Write(t)
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "metadata":
				if inTitle {
					m.title = strings.TrimSpace(titleBuf.String())
					inTitle = false
				}
			case "basematerials", "colorgroup":
				inGroup = false
			case "object":
				curObj = nil
				inObjMesh = false
			case "mesh":
				inObjMesh = false
			case "build":
				inBuild = false
			}
		}
	}
	return m, nil
}

func num(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

// parseColor acepta «#RRGGBB» y «#RRGGBBAA»; el alfa se descarta. Algo ilegible
// es gris medio (un color que se note, no negro invisible).
func parseColor(s string) RGB {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 && len(s) != 8 {
		return RGB{128, 128, 128}
	}
	v, err := strconv.ParseUint(s[:6], 16, 32)
	if err != nil {
		return RGB{128, 128, 128}
	}
	return RGB{uint8(v >> 16), uint8(v >> 8), uint8(v)}
}

// flatten arma el Mesh final: recorre <build>, expande componentes y pinta
// cada triángulo con su material (el suyo, o el del objeto).
func (d *doc) flatten(root *model) (*Mesh, error) {
	out := &Mesh{Title: root.title}
	type matKey struct {
		file, pid string
		i         int
	}
	matIdx := map[matKey]int32{} // (archivo, grupo, índice) → índice en out.Materials
	material := func(file string, m *model, pid string, i int) int32 {
		if pid == "" {
			return -1
		}
		g, ok := m.groups[pid]
		if !ok || i < 0 || i >= len(g) {
			return -1
		}
		k := matKey{file, pid, i}
		if v, ok := matIdx[k]; ok {
			return v
		}
		v := int32(len(out.Materials))
		out.Materials = append(out.Materials, g[i])
		matIdx[k] = v
		return v
	}

	var emit func(file string, m *model, id string, tr matrix, depth int) error
	emit = func(file string, m *model, id string, tr matrix, depth int) error {
		o := m.objects[id]
		if o == nil || depth > 16 {
			return nil
		}
		if len(o.verts) > 0 {
			base := int32(len(out.Vertices))
			for _, v := range o.verts {
				out.Vertices = append(out.Vertices, tr.apply(v))
			}
			objMat := int32(-1)
			if o.pid != "" {
				pi, _ := strconv.Atoi(o.pindex) // sin pindex = 0
				objMat = material(file, m, o.pid, pi)
			}
			for _, t := range o.tris {
				mat := objMat
				if t.p1 >= 0 {
					pid := t.pid
					if pid == "" {
						pid = o.pid
					}
					mat = material(file, m, pid, int(t.p1))
				}
				n := len(o.verts)
				if int(t.v[0]) >= n || int(t.v[1]) >= n || int(t.v[2]) >= n || t.v[0] < 0 || t.v[1] < 0 || t.v[2] < 0 {
					continue
				}
				out.Triangles = append(out.Triangles, Triangle{
					V:   [3]int32{base + t.v[0], base + t.v[1], base + t.v[2]},
					Mat: mat,
				})
			}
		}
		for _, c := range o.comps {
			cm, cfile := m, file
			if c.path != "" {
				f := d.files[strings.ToLower(c.path)]
				if f == nil {
					continue
				}
				loaded, err := d.load(f, c.path)
				if err != nil {
					return err
				}
				cm, cfile = loaded, path.Clean(c.path)
			}
			if err := emit(cfile, cm, c.id, c.trans.mul(tr), depth+1); err != nil {
				return err
			}
		}
		return nil
	}

	items := root.build
	if len(items) == 0 { // sin <build>: todos los objetos de nivel superior sirven de poco; se toman todos
		for id := range root.objects {
			items = append(items, component{id: id, trans: identity})
		}
	}
	for _, it := range items {
		if err := emit("", root, it.id, it.trans, 0); err != nil {
			return nil, err
		}
	}
	if len(out.Triangles) == 0 {
		return nil, errNoMesh
	}
	for _, v := range out.Vertices {
		for _, c := range v {
			if math.IsNaN(c) || math.IsInf(c, 0) {
				return nil, i18n.New(i18n.Msg{ES: "el 3MF tiene coordenadas no finitas", EN: "the 3MF has non-finite coordinates"})
			}
		}
	}
	return out, nil
}
