// Package mesh analiza mallas STL: dimensiones, volumen, área y si la malla es
// cerrada. Lo usan el visor (S2), la medición del MCP (S6), el peso y costo
// (S7) y el aviso de placa: por eso vive en Go y no en el frontend.
//
// Lee STL binario y ASCII. El binario se detecta por TAMAÑO (84 + 50·n bytes,
// con n el contador del encabezado) y no por el «solid» del principio: muchos
// exportadores escriben «solid …» también en el encabezado de 80 bytes de un
// binario.
package mesh

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"os"
	"slices"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
)

// Info resume una malla. Unidades: las del STL, que en OpenSCAD son mm.
type Info struct {
	Triangles  int        `json:"triangles"`
	Min        [3]float64 `json:"min"`
	Max        [3]float64 `json:"max"`
	Size       [3]float64 `json:"size"`       // Max-Min, mm
	VolumeMM3  float64    `json:"volumeMm3"`  // tetraedros con signo; |v|
	AreaMM2    float64    `json:"areaMm2"`    // suma de las áreas de los triángulos
	Watertight bool       `json:"watertight"` // cada arista compartida por exactamente 2 caras
}

const (
	binaryHeaderLen = 84 // 80 de encabezado + uint32 con la cantidad de triángulos
	binaryTriLen    = 50 // normal + 3 vértices (12 float32) + uint16 de atributos
	maxASCIILine    = 1 << 20
)

var (
	// ErrTruncated: el archivo no tiene el tamaño de un STL binario y tampoco
	// es un STL ASCII. Es lo que deja un render cortado a mitad de escritura.
	ErrTruncated = i18n.New(i18n.Msg{ES: "el STL está truncado o no es un STL", EN: "the STL is truncated or is not an STL"})
	// ErrNonFinite: una coordenada NaN o infinita. No se puede medir ni
	// mandar a la interfaz (JSON no admite NaN).
	ErrNonFinite = i18n.New(i18n.Msg{ES: "el STL tiene coordenadas no finitas (NaN o infinito)", EN: "the STL has non-finite coordinates (NaN or infinity)"})
)

// InfoFromFile analiza un STL en disco. El binario se lee en streaming: no se
// carga el archivo entero en memoria.
func InfoFromFile(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	return ParseSTL(f)
}

// ParseSTL analiza un STL binario o ASCII. Si r se puede posicionar (un
// archivo, un bytes.Reader) se mide su tamaño y se lee en streaming; si no
// (un pipe), se lee entero en memoria, porque sin el tamaño no se puede
// distinguir un binario de un ASCII antes de consumirlo.
func ParseSTL(r io.Reader) (Info, error) {
	if s, ok := r.(io.ReadSeeker); ok {
		if start, err := s.Seek(0, io.SeekCurrent); err == nil {
			end, err := s.Seek(0, io.SeekEnd)
			if err != nil {
				return Info{}, err
			}
			if _, err := s.Seek(start, io.SeekStart); err != nil {
				return Info{}, err
			}
			return parse(s, end-start)
		}
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return Info{}, err
	}
	return parse(bytes.NewReader(b), int64(len(b)))
}

func parse(r io.Reader, size int64) (Info, error) {
	br := bufio.NewReaderSize(r, 1<<16)
	if size >= binaryHeaderLen {
		head, err := br.Peek(binaryHeaderLen)
		if err != nil {
			return Info{}, err
		}
		n := int64(binary.LittleEndian.Uint32(head[80:84]))
		if size == binaryHeaderLen+binaryTriLen*n {
			if _, err := br.Discard(binaryHeaderLen); err != nil {
				return Info{}, err
			}
			return parseBinary(br, n)
		}
	}
	// No cuadra con un binario. Es ASCII solo si empieza con «solid» y no
	// tiene bytes NUL al principio: un binario truncado cuyo encabezado dice
	// «solid» (lo hacen varios exportadores) tiene ceros en el relleno y en el
	// contador, y reportarlo como «ASCII inválido en la línea 2» confundiría.
	start, _ := br.Peek(512)
	if !bytes.HasPrefix(bytes.TrimLeft(start, " \t\r\n\ufeff"), []byte("solid")) || bytes.IndexByte(start, 0) >= 0 {
		return Info{}, ErrTruncated
	}
	return parseASCII(br)
}

func parseBinary(r io.Reader, n int64) (Info, error) {
	acc := newAccumulator(int(n))
	var rec [binaryTriLen]byte
	for i := int64(0); i < n; i++ {
		if _, err := io.ReadFull(r, rec[:]); err != nil {
			// El tamaño ya cuadraba: esto es un archivo que cambió mientras se
			// leía o un error de E/S.
			if err == io.ErrUnexpectedEOF || err == io.EOF {
				return Info{}, ErrTruncated
			}
			return Info{}, err
		}
		var tri [3][3]float64
		for v := 0; v < 3; v++ {
			for c := 0; c < 3; c++ {
				off := 12 + v*12 + c*4 // los primeros 12 bytes son la normal: no se usa
				tri[v][c] = float64(math.Float32frombits(binary.LittleEndian.Uint32(rec[off : off+4])))
			}
		}
		if err := acc.add(tri); err != nil {
			return Info{}, err
		}
	}
	return acc.info(), nil
}

// parseASCII sigue la gramática de un STL ASCII línea por línea. Es estricto a
// propósito: un binario truncado que empieza con «solid» tiene que dar error,
// no una malla vacía o a medias.
func parseASCII(r io.Reader) (Info, error) {
	acc := newAccumulator(0)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxASCIILine)
	var loop [][3]float64
	inLoop, ended, started := false, false, false
	line := 0
	for sc.Scan() {
		line++
		fields := strings.Fields(sc.Text())
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "solid":
			started, ended = true, false
		case "endsolid":
			ended = true
		case "facet", "outer", "endfacet":
			// La normal no se usa: se recalcula del orden de los vértices.
		case "vertex":
			if !inLoop || len(fields) != 4 {
				return Info{}, asciiError(line)
			}
			var p [3]float64
			for c := 0; c < 3; c++ {
				f, err := strconv.ParseFloat(fields[1+c], 64)
				if err != nil {
					return Info{}, asciiError(line)
				}
				p[c] = f
			}
			loop = append(loop, p)
		case "endloop":
			if !inLoop || len(loop) < 3 {
				return Info{}, asciiError(line)
			}
			// Un polígono de más de 3 vértices (no estándar, pero hay
			// exportadores que lo escriben) se triangula en abanico.
			for i := 1; i+1 < len(loop); i++ {
				if err := acc.add([3][3]float64{loop[0], loop[i], loop[i+1]}); err != nil {
					return Info{}, err
				}
			}
			loop = loop[:0]
			inLoop = false
			continue
		default:
			return Info{}, asciiError(line)
		}
		if strings.EqualFold(fields[0], "outer") {
			inLoop = true
		}
	}
	if err := sc.Err(); err != nil {
		if err == bufio.ErrTooLong {
			return Info{}, ErrTruncated
		}
		return Info{}, err
	}
	if !started || !ended || inLoop {
		return Info{}, ErrTruncated
	}
	return acc.info(), nil
}

// ErrBadIndex: un triángulo apunta a un vértice que no existe.
var ErrBadIndex = i18n.New(i18n.Msg{ES: "la malla tiene un triángulo con un vértice inexistente", EN: "the mesh has a triangle that points to a missing vertex"})

// InfoFromIndexed analiza una malla indexada (la que sale de un OBJ o de un
// 3MF) con las mismas cuentas que un STL: los vértices se sueldan por posición
// exacta, así que dos vértices repetidos en el mismo punto no rompen la
// estanqueidad.
func InfoFromIndexed(verts [][3]float64, tris [][3]int32) (Info, error) {
	acc := newAccumulator(len(tris))
	for _, t := range tris {
		var tri [3][3]float64
		for k, i := range t {
			if i < 0 || int(i) >= len(verts) {
				return Info{}, ErrBadIndex
			}
			tri[k] = verts[i]
		}
		if err := acc.add(tri); err != nil {
			return Info{}, err
		}
	}
	return acc.info(), nil
}

func asciiError(line int) error {
	return i18n.Errorf(i18n.Msg{ES: "STL ASCII inválido en la línea %d", EN: "invalid ASCII STL at line %d"}, line)
}

// accumulator suma triángulo a triángulo. Lo único que guarda por triángulo
// son las 3 aristas (un uint64 cada una) y los vértices soldados en un mapa;
// las coordenadas no se retienen.
type accumulator struct {
	tris    int
	min     [3]float64
	max     [3]float64
	volume6 float64 // 6·volumen con signo
	area2   float64 // 2·área
	verts   map[[3]float64]uint32
	edges   []uint64
}

func newAccumulator(hint int) *accumulator {
	return &accumulator{
		verts: make(map[[3]float64]uint32, hint/2+1),
		edges: make([]uint64, 0, 3*hint),
	}
}

func (a *accumulator) add(t [3][3]float64) error {
	for _, p := range t {
		for c := 0; c < 3; c++ {
			if math.IsNaN(p[c]) || math.IsInf(p[c], 0) {
				return ErrNonFinite
			}
		}
	}
	if a.tris == 0 {
		a.min, a.max = t[0], t[0]
	}
	a.tris++
	var ids [3]uint32
	for i, p := range t {
		for c := 0; c < 3; c++ {
			a.min[c] = math.Min(a.min[c], p[c])
			a.max[c] = math.Max(a.max[c], p[c])
		}
		// Soldadura por posición exacta. +0 y -0 son la misma clave (Go
		// compara floats con ==, también en los mapas).
		id, ok := a.verts[p]
		if !ok {
			id = uint32(len(a.verts))
			a.verts[p] = id
		}
		ids[i] = id
	}
	for i := 0; i < 3; i++ {
		u, v := ids[i], ids[(i+1)%3]
		if u > v {
			u, v = v, u
		}
		a.edges = append(a.edges, uint64(u)<<32|uint64(v))
	}
	e1 := sub(t[1], t[0])
	e2 := sub(t[2], t[0])
	cr := cross(e1, e2)
	a.area2 += math.Sqrt(dot(cr, cr))
	a.volume6 += dot(t[0], cross(t[1], t[2]))
	return nil
}

func (a *accumulator) info() Info {
	in := Info{
		Triangles: a.tris,
		Min:       a.min,
		Max:       a.max,
		VolumeMM3: math.Abs(a.volume6) / 6,
		AreaMM2:   a.area2 / 2,
	}
	for c := 0; c < 3; c++ {
		in.Size[c] = a.max[c] - a.min[c]
	}
	in.Watertight = a.tris > 0 && everyEdgeTwice(a.edges)
	return in
}

// everyEdgeTwice ordena las aristas y exige que cada una aparezca exactamente
// dos veces. Ordenar un slice de uint64 gasta 8 bytes por arista; un mapa de
// conteos gastaría varias veces eso en una malla de millones de triángulos.
func everyEdgeTwice(edges []uint64) bool {
	slices.Sort(edges)
	for i := 0; i < len(edges); {
		j := i
		for j < len(edges) && edges[j] == edges[i] {
			j++
		}
		if j-i != 2 {
			return false
		}
		i = j
	}
	return true
}

func sub(a, b [3]float64) [3]float64 { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func dot(a, b [3]float64) float64    { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func cross(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
