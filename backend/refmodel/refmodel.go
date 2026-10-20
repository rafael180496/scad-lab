// Package refmodel analiza las referencias 3D que el usuario le da a la IA: un
// STL, un 3MF o un OBJ de una pieza que quiere copiar, completar o encajar con
// otra. Calcula en Go lo que un modelo de lenguaje no puede sacar de un
// archivo binario —medidas, volumen, triángulos, si la malla es cerrada, las
// partes por color de un 3MF—, la dibuja con backend/raster para que un modelo
// con visión la vea, y la copia a <workspace>/refs/ para que OpenSCAD pueda
// hacer `import("refs/pieza.stl")`.
//
// Lo usan la herramienta `inspect_reference` (backend/mcp) y los adjuntos del
// chat de Hermes. Spec: .claude/specs/s12-hermes-proveedores.md.
package refmodel

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/mesh"
	"scadlab/backend/raster"
	"scadlab/backend/threemf"
	"scadlab/backend/workspace"
)

// MaxBytes es el tamaño máximo de una referencia: un STL de 64 MB son más de un
// millón de triángulos, que ya es mucho para dibujar y medir sin colgar la app.
const MaxBytes = 64 << 20

// maxUnzipped acota lo que un 3MF puede ocupar descomprimido: un zip chico
// puede expandirse a gigabytes (zip bomb), y threemf.Read lo lee entero.
var maxUnzipped uint64 = 8 * MaxBytes

// RefsDir es la carpeta del workspace donde viven las referencias.
const RefsDir = "refs"

// Formatos que se reconocen, por extensión.
const (
	FormatSTL = "stl"
	Format3MF = "3mf"
	FormatOBJ = "obj"
)

// Part es una parte de la referencia: un color de un 3MF, o la malla entera de
// un STL u OBJ.
type Part struct {
	Name      string     `json:"name"`
	Color     string     `json:"color"` // «#RRGGBB»; vacío en STL y OBJ, que no traen color
	Triangles int        `json:"triangles"`
	Size      [3]float64 `json:"size"`
	VolumeMm3 float64    `json:"volumeMm3"`
}

// Report es lo que se sabe de una referencia. Las medidas están en las
// unidades del archivo, que en STL y OBJ se leen como mm (lo que asume
// OpenSCAD al importar).
//
// Las etiquetas JSON existen desde el editor de STL (S13): Report cruza Wails
// en AnalyzeModelFile / ImportModel / PrepareMeshEdit.
type Report struct {
	Format     string     `json:"format"`
	Triangles  int        `json:"triangles"`
	Min        [3]float64 `json:"min"`
	Max        [3]float64 `json:"max"`
	Size       [3]float64 `json:"size"`
	VolumeMm3  float64    `json:"volumeMm3"`
	AreaMm2    float64    `json:"areaMm2"`
	Watertight bool       `json:"watertight"`
	Parts      []Part     `json:"parts"` // 3MF: una por color; STL y OBJ: una sola
}

var errNotRef = i18n.New(i18n.Msg{
	ES: "formato no reconocido: usa un archivo STL, 3MF u OBJ",
	EN: "unrecognized format: use an STL, 3MF or OBJ file",
})

func errTooBig(size int64) error {
	return i18n.Errorf(i18n.Msg{
		ES: "el archivo es demasiado grande (%d MB; máximo %d MB)",
		EN: "the file is too large (%d MB; maximum %d MB)",
	}, size>>20, MaxBytes>>20)
}

// FormatOf dice qué formato es un archivo según su extensión ("" si no es
// ninguno de los tres).
func FormatOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".stl":
		return FormatSTL
	case ".3mf":
		return Format3MF
	case ".obj":
		return FormatOBJ
	}
	return ""
}

// check abre la puerta de entrada: el formato se reconoce, es un archivo
// regular y no pasa de MaxBytes.
func check(path string) (string, error) {
	format := FormatOf(path)
	if format == "" {
		return "", errNotRef
	}
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", i18n.Errorf(i18n.Msg{ES: "%s no es un archivo", EN: "%s is not a file"}, filepath.Base(path))
	}
	if st.Size() > MaxBytes {
		return "", errTooBig(st.Size())
	}
	return format, nil
}

// Analyze mide una referencia STL, 3MF u OBJ.
func Analyze(path string) (Report, error) {
	format, err := check(path)
	if err != nil {
		return Report{}, err
	}
	switch format {
	case FormatSTL:
		info, err := mesh.InfoFromFile(path)
		if err != nil {
			return Report{}, err
		}
		if info.Triangles == 0 {
			return Report{}, errEmpty
		}
		return singlePart(format, path, info), nil
	case FormatOBJ:
		m, err := readOBJ(path)
		if err != nil {
			return Report{}, err
		}
		info, err := mesh.InfoFromIndexed(m.verts, m.tris)
		if err != nil {
			return Report{}, err
		}
		return singlePart(format, path, info), nil
	}
	return analyze3MF(path)
}

var errEmpty = i18n.New(i18n.Msg{ES: "la malla no tiene triángulos", EN: "the mesh has no triangles"})

func singlePart(format, path string, info mesh.Info) Report {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return Report{
		Format: format, Triangles: info.Triangles, Min: info.Min, Max: info.Max, Size: info.Size,
		VolumeMm3: info.VolumeMM3, AreaMm2: info.AreaMM2, Watertight: info.Watertight,
		Parts: []Part{{Name: name, Triangles: info.Triangles, Size: info.Size, VolumeMm3: info.VolumeMM3}},
	}
}

// read3MF lee el 3MF y lo reparte en partes por color. Antes mira cuánto
// ocuparía descomprimido.
func read3MF(path string) (*threemf.Mesh, []threemf.Part, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, nil, i18n.Errorf(i18n.Msg{ES: "el 3MF no es un zip válido: %w", EN: "the 3MF is not a valid zip: %w"}, err)
	}
	var total uint64
	for _, f := range zr.File {
		total += f.UncompressedSize64
	}
	_ = zr.Close()
	if total > maxUnzipped {
		return nil, nil, i18n.New(i18n.Msg{ES: "el 3MF ocupa demasiado descomprimido", EN: "the 3MF is too large once unzipped"})
	}
	m, err := threemf.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	parts := threemf.Split(m)
	if len(parts) == 0 {
		return nil, nil, errEmpty
	}
	return m, parts, nil
}

func analyze3MF(path string) (Report, error) {
	m, parts, err := read3MF(path)
	if err != nil {
		return Report{}, err
	}
	tris := make([][3]int32, len(m.Triangles))
	for i, t := range m.Triangles {
		tris[i] = t.V
	}
	total, err := mesh.InfoFromIndexed(m.Vertices, tris)
	if err != nil {
		return Report{}, err
	}
	r := Report{
		Format: Format3MF, Triangles: total.Triangles, Min: total.Min, Max: total.Max, Size: total.Size,
		VolumeMm3: total.VolumeMM3, AreaMm2: total.AreaMM2, Watertight: total.Watertight,
	}
	for _, p := range parts {
		info, err := mesh.InfoFromIndexed(p.Vertices, p.Triangles)
		if err != nil {
			return Report{}, err
		}
		r.Parts = append(r.Parts, Part{
			Name: p.Name, Color: p.Color.Hex(), Triangles: info.Triangles, Size: info.Size, VolumeMm3: info.VolumeMM3,
		})
	}
	return r, nil
}

// Tamaños de la vista previa.
const (
	minPreviewSize     = 256
	maxPreviewSize     = 1024
	defaultPreviewSize = 640
)

// defaultColor es el amarillo por defecto de OpenSCAD (#F9D72C): el de lo que
// no tiene color.
var defaultColor = [3]uint8{0xF9, 0xD7, 0x2C}

// Preview dibuja la referencia como un PNG cuadrado de size×size píxeles (0 =
// 640; se acota a 256–1024), con las mismas vistas que la herramienta
// `preview` (vacío = iso). Un 3MF se pinta por partes, cada una con su color;
// un STL u OBJ, en el amarillo por defecto de OpenSCAD.
func Preview(path, view string, size int) ([]byte, error) {
	format, err := check(path)
	if err != nil {
		return nil, err
	}
	_, v, err := ParseView(view)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		size = defaultPreviewSize
	}
	size = max(minPreviewSize, min(maxPreviewSize, size))

	var parts []raster.Part
	switch format {
	case FormatSTL:
		tris, err := raster.ReadSTL(path)
		if err != nil {
			return nil, err
		}
		parts = []raster.Part{{Tris: tris, Color: defaultColor}}
	case FormatOBJ:
		m, err := readOBJ(path)
		if err != nil {
			return nil, err
		}
		parts = []raster.Part{{Tris: rasterTris(m.verts, m.tris), Color: defaultColor}}
	default:
		_, split, err := read3MF(path)
		if err != nil {
			return nil, err
		}
		for _, p := range split {
			col := [3]uint8(p.Color)
			if p.Uncolored {
				col = defaultColor
			}
			parts = append(parts, raster.Part{Tris: rasterTris(p.Vertices, p.Triangles), Color: col})
		}
	}
	for _, p := range parts {
		if len(p.Tris) == 0 {
			return nil, errEmpty
		}
	}
	img := raster.Render(parts, raster.View{
		RotX: float64(v.Rot[0]), RotY: float64(v.Rot[1]), RotZ: float64(v.Rot[2]), Ortho: v.Ortho,
	}, size)
	return raster.EncodePNG(img)
}

func rasterTris(verts [][3]float64, tris [][3]int32) []raster.Tri {
	out := make([]raster.Tri, 0, len(tris))
	for _, t := range tris {
		var r raster.Tri
		for k, i := range t {
			if i < 0 || int(i) >= len(verts) {
				continue
			}
			for c := 0; c < 3; c++ {
				r[3*k+c] = float32(verts[i][c])
			}
		}
		out = append(out, r)
	}
	return out
}

// CopyToRefs copia src a <workspaceDir>/refs/ con un nombre seguro
// (refs/<slug>.<ext>, sin acentos ni espacios) y devuelve la ruta relativa al
// workspace, con «/», lista para un `import()`. Nunca pisa: si el nombre ya
// existe agrega -2, -3… Crea refs/ si falta.
func CopyToRefs(workspaceDir, src string) (rel string, err error) {
	if _, err := check(src); err != nil {
		return "", err
	}
	dir, err := refsDir(workspaceDir, true)
	if err != nil {
		return "", err
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()

	ext := strings.ToLower(filepath.Ext(src))
	stem := safeStem(strings.TrimSuffix(filepath.Base(src), filepath.Ext(src)))
	for n := 1; n < 1000; n++ {
		name := stem + ext
		if n > 1 {
			name = stem + "-" + strconv.Itoa(n) + ext
		}
		out, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		// Se copia hasta MaxBytes+1: si el archivo creció mientras se copiaba, no
		// se deja pasar de largo.
		written, cerr := io.Copy(out, io.LimitReader(in, MaxBytes+1))
		if cerr == nil {
			cerr = out.Close()
		} else {
			out.Close()
		}
		if cerr == nil && written > MaxBytes {
			cerr = errTooBig(written)
		}
		if cerr != nil {
			_ = os.Remove(filepath.Join(dir, name))
			return "", cerr
		}
		return RefsDir + "/" + name, nil
	}
	return "", i18n.New(i18n.Msg{ES: "ya hay demasiadas referencias con ese nombre", EN: "there are too many references with that name already"})
}

// ResolveRef resuelve el nombre de una referencia que ya está en
// <workspaceDir>/refs/ (con o sin el prefijo «refs/») a su ruta absoluta y su
// ruta relativa. Es la única puerta por la que un nombre que escribió un modelo
// llega al disco: un solo nombre de archivo, formato conocido, y si es un
// enlace simbólico su destino tiene que seguir dentro de refs/.
func ResolveRef(workspaceDir, file string) (abs, rel string, err error) {
	name := strings.TrimSpace(file)
	name = strings.TrimPrefix(name, RefsDir+"/")
	if ImageFormatOf(name) != "" {
		return "", "", i18n.Errorf(i18n.Msg{
			ES: "%s es una imagen, no un modelo 3D: usa image_info o compare_with_image",
			EN: "%s is an image, not a 3D model: use image_info or compare_with_image",
		}, file)
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00") || strings.HasPrefix(name, ".") {
		return "", "", i18n.Errorf(i18n.Msg{
			ES: "nombre de referencia inválido: %q (usa un archivo de refs/, p. ej. refs/pieza.stl)",
			EN: "invalid reference name: %q (use a file in refs/, e.g. refs/part.stl)",
		}, file)
	}
	if FormatOf(name) == "" {
		return "", "", errNotRef
	}
	dir, err := refsDir(workspaceDir, false)
	if err != nil {
		return "", "", err
	}
	abs = filepath.Join(dir, name)
	st, err := os.Lstat(abs)
	if err != nil || st.IsDir() {
		return "", "", i18n.Errorf(i18n.Msg{ES: "no se encontró la referencia %s", EN: "reference %s was not found"}, RefsDir+"/"+name)
	}
	if st.Mode()&os.ModeSymlink != 0 {
		real, err := filepath.EvalSymlinks(abs)
		if err != nil || !within(dir, real) {
			return "", "", i18n.Errorf(i18n.Msg{ES: "la referencia %s apunta fuera de refs/", EN: "reference %s points outside refs/"}, RefsDir+"/"+name)
		}
		abs = real
	}
	return abs, RefsDir + "/" + name, nil
}

// refsDir devuelve <ws>/refs (opcionalmente la crea) comprobando que no sea un
// enlace que saque las referencias del workspace.
func refsDir(workspaceDir string, create bool) (string, error) {
	root, err := filepath.EvalSymlinks(workspaceDir)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, RefsDir)
	if create {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", i18n.New(i18n.Msg{ES: "el workspace no tiene referencias (carpeta refs/)", EN: "the workspace has no references (refs/ folder)"})
		}
		return "", err
	}
	if !within(root, real) {
		return "", i18n.New(i18n.Msg{ES: "la carpeta refs apunta fuera del workspace", EN: "the refs folder points outside the workspace"})
	}
	return real, nil
}

func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

// windowsReserved son los nombres que Windows no deja usar como archivo, con
// cualquier extensión.
var windowsReserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}

// safeStem deja el nombre de una referencia en [a-z0-9-]: sin acentos, sin
// espacios y sin nombres reservados de Windows.
func safeStem(name string) string {
	s := workspace.Slug(name)
	if s == "" {
		s = "reference"
	}
	l := strings.ToLower(s)
	if windowsReserved[l] || ((strings.HasPrefix(l, "com") || strings.HasPrefix(l, "lpt")) && len(l) == 4 && l[3] >= '0' && l[3] <= '9') {
		s += "-ref"
	}
	return s
}
