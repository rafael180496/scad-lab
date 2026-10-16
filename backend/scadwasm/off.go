package scadwasm

import (
	"archive/zip"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
)

// El OpenSCAD en WebAssembly no puede exportar 3MF: su lib3mf registra el
// callback de escritura con una firma distinta de la que lib3mf espera al
// llamarlo (i32,i32,i32 contra i64,i64,i32), y el módulo muere con «indirect
// call type mismatch» en cualquier 3MF —con o sin color—. Es un defecto del
// binario oficial, no del host: ningún motor de WebAssembly lo arregla.
//
// Lo que sí exporta bien, y con el color de cada cara, es OFF. Este archivo lee
// ese OFF y escribe un 3MF equivalente al de OpenSCAD (un objeto, un
// <basematerials> con un «Color N» por cada color() y el material de cada
// triángulo), que es lo que lee threemf.ReadFile y reparte threemf.Split. El
// resto de ScadLab no se entera de la diferencia.

// defaultFaceColor es el amarillo «cornfield» de OpenSCAD (#F9D72C). Cuando
// algún color() existe, el OFF escribe las caras SIN color con este valor
// explícito; se vuelve a tratar como «sin color» (el material Default del 3MF
// nativo). Un color() elegido a propósito con exactamente este valor se
// confunde con el defecto: es el precio de no tener otra señal en el OFF.
var defaultFaceColor = [3]uint8{0xF9, 0xD7, 0x2C}

type offMesh struct {
	verts   [][3]float64
	tris    [][3]int32
	triMat  []int32 // índice en palette; -1 = sin color
	palette [][3]uint8
}

// ConvertOFFTo3MF lee un OFF con colores por cara (el de OpenSCAD) y escribe
// dest como un 3MF en el estilo de OpenSCAD. Escribe a un temporal y renombra:
// un 3MF a medias nunca queda con el nombre final.
func ConvertOFFTo3MF(offPath, dest, title string) error {
	f, err := os.Open(offPath)
	if err != nil {
		return err
	}
	defer f.Close()
	m, err := readOFF(bufio.NewReaderSize(f, 1<<20))
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".off3mf-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := m.write3MF(tmp, title); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, dest)
}

var errBadOFF = i18n.New(i18n.Msg{ES: "el motor integrado escribió un OFF ilegible", EN: "the built-in engine wrote an unreadable OFF file"})

// offTokens lee palabras separadas por espacios; las líneas en blanco y los
// comentarios (#…) no existen en lo que escribe OpenSCAD, pero se toleran.
type offTokens struct {
	r   *bufio.Reader
	buf []byte
}

func (t *offTokens) next() (string, bool) {
	t.buf = t.buf[:0]
	for {
		c, err := t.r.ReadByte()
		if err != nil {
			return string(t.buf), len(t.buf) > 0
		}
		switch c {
		case ' ', '\t', '\r', '\n':
			if len(t.buf) > 0 {
				return string(t.buf), true
			}
		case '#':
			_, _ = t.r.ReadString('\n')
			if len(t.buf) > 0 {
				return string(t.buf), true
			}
		default:
			t.buf = append(t.buf, c)
		}
	}
}

func (t *offTokens) nextLine() ([]string, bool) {
	line, err := t.r.ReadString('\n')
	if err != nil && line == "" {
		return nil, false
	}
	return strings.Fields(line), true
}

func readOFF(r *bufio.Reader) (*offMesh, error) {
	t := &offTokens{r: r}
	head, ok := t.next()
	if !ok || head != "OFF" {
		return nil, errBadOFF
	}
	counts := make([]int, 3)
	for i := range counts {
		s, ok := t.next()
		if !ok {
			return nil, errBadOFF
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil, errBadOFF
		}
		counts[i] = n
	}
	nv, nf := counts[0], counts[1]
	if nv > 1<<28 || nf > 1<<28 {
		return nil, errBadOFF
	}
	m := &offMesh{verts: make([][3]float64, 0, nv)}
	for i := 0; i < nv; i++ {
		var v [3]float64
		for k := range v {
			s, ok := t.next()
			if !ok {
				return nil, errBadOFF
			}
			x, err := strconv.ParseFloat(s, 64)
			if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
				return nil, errBadOFF
			}
			v[k] = x
		}
		m.verts = append(m.verts, v)
	}
	// Las caras son una por línea: el número de componentes de color (0, 3 o 4)
	// se deduce de cuántos campos sobran tras los índices.
	palIdx := map[[3]uint8]int32{}
	var idx []int32
	for i := 0; i < nf; i++ {
		fields, ok := t.nextLine()
		for ok && len(fields) == 0 { // línea en blanco
			fields, ok = t.nextLine()
		}
		if !ok {
			return nil, errBadOFF
		}
		n, err := strconv.Atoi(fields[0])
		if err != nil || n < 3 || len(fields) < 1+n {
			if err == nil && n < 3 && len(fields) >= 1+n {
				continue // cara degenerada
			}
			return nil, errBadOFF
		}
		idx = idx[:0]
		for _, s := range fields[1 : 1+n] {
			v, err := strconv.Atoi(s)
			if err != nil || v < 0 || v >= nv {
				return nil, errBadOFF
			}
			idx = append(idx, int32(v))
		}
		mat := int32(-1)
		if rest := fields[1+n:]; len(rest) >= 3 {
			var c [3]uint8
			for k := 0; k < 3; k++ {
				v, err := strconv.Atoi(rest[k])
				if err != nil {
					return nil, errBadOFF
				}
				c[k] = uint8(max(0, min(255, v)))
			}
			if c != defaultFaceColor {
				pi, ok := palIdx[c]
				if !ok {
					pi = int32(len(m.palette))
					palIdx[c] = pi
					m.palette = append(m.palette, c)
				}
				mat = pi
			}
		}
		for _, tri := range triangulate(m.verts, idx) {
			m.tris = append(m.tris, tri)
			m.triMat = append(m.triMat, mat)
		}
	}
	if len(m.tris) == 0 {
		return nil, errBadOFF
	}
	return m, nil
}

// triangulate parte un polígono en triángulos. Un triángulo se devuelve tal
// cual; el resto se corta por orejas sobre el plano del polígono (no basta un
// abanico: un cuadrilátero o un polígono de polyhedron() puede ser cóncavo).
func triangulate(verts [][3]float64, idx []int32) [][3]int32 {
	n := len(idx)
	if n == 3 {
		return [][3]int32{{idx[0], idx[1], idx[2]}}
	}
	// Normal de Newell: robusta con vértices casi colineales.
	var nx, ny, nz float64
	for i := 0; i < n; i++ {
		a, b := verts[idx[i]], verts[idx[(i+1)%n]]
		nx += (a[1] - b[1]) * (a[2] + b[2])
		ny += (a[2] - b[2]) * (a[0] + b[0])
		nz += (a[0] - b[0]) * (a[1] + b[1])
	}
	// Se descarta el eje de mayor componente y se trabaja en 2D.
	ax, ay := 0, 1
	switch {
	case math.Abs(nz) >= math.Abs(nx) && math.Abs(nz) >= math.Abs(ny):
		ax, ay = 0, 1
		if nz < 0 {
			ax, ay = 1, 0
		}
	case math.Abs(nx) >= math.Abs(ny):
		ax, ay = 1, 2
		if nx < 0 {
			ax, ay = 2, 1
		}
	default:
		ax, ay = 2, 0
		if ny < 0 {
			ax, ay = 0, 2
		}
	}
	pt := func(i int32) (float64, float64) { v := verts[i]; return v[ax], v[ay] }
	cross := func(a, b, c int32) float64 {
		ax_, ay_ := pt(a)
		bx, by := pt(b)
		cx, cy := pt(c)
		return (bx-ax_)*(cy-ay_) - (by-ay_)*(cx-ax_)
	}
	inside := func(p, a, b, c int32) bool {
		d1, d2, d3 := cross(a, b, p), cross(b, c, p), cross(c, a, p)
		neg := d1 < 0 || d2 < 0 || d3 < 0
		pos := d1 > 0 || d2 > 0 || d3 > 0
		return !(neg && pos)
	}

	poly := append([]int32(nil), idx...)
	out := make([][3]int32, 0, n-2)
	for len(poly) > 3 {
		clipped := false
		for i := 0; i < len(poly); i++ {
			a, b, c := poly[(i+len(poly)-1)%len(poly)], poly[i], poly[(i+1)%len(poly)]
			if cross(a, b, c) <= 0 { // cóncavo o colineal
				continue
			}
			ear := true
			for _, p := range poly {
				if p == a || p == b || p == c {
					continue
				}
				if inside(p, a, b, c) {
					ear = false
					break
				}
			}
			if !ear {
				continue
			}
			out = append(out, [3]int32{a, b, c})
			poly = append(poly[:i], poly[i+1:]...)
			clipped = true
			break
		}
		if !clipped {
			// Polígono degenerado (todo colineal o con autointersección): se
			// quita un vértice sin emitir nada y, si no queda otra, abanico.
			removed := false
			for i := 0; i < len(poly); i++ {
				a, b, c := poly[(i+len(poly)-1)%len(poly)], poly[i], poly[(i+1)%len(poly)]
				if cross(a, b, c) == 0 {
					poly = append(poly[:i], poly[i+1:]...)
					removed = true
					break
				}
			}
			if !removed {
				for i := 1; i+1 < len(poly); i++ {
					out = append(out, [3]int32{poly[0], poly[i], poly[i+1]})
				}
				return out
			}
		}
	}
	if len(poly) == 3 {
		out = append(out, [3]int32{poly[0], poly[1], poly[2]})
	}
	return out
}

func (m *offMesh) write3MF(w io.Writer, title string) error {
	zw := zip.NewWriter(w)
	put := func(name, body string) error {
		ew, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(ew, body)
		return err
	}
	if err := put("[Content_Types].xml", xmlHead+`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
 <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
 <Default Extension="model" ContentType="application/vnd.ms-package.3dmanufacturing-3dmodel+xml"/>
</Types>
`); err != nil {
		return err
	}
	if err := put("_rels/.rels", xmlHead+`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
 <Relationship Target="/3D/3dmodel.model" Id="rel0" Type="http://schemas.microsoft.com/3dmanufacturing/2013/01/3dmodel"/>
</Relationships>
`); err != nil {
		return err
	}
	ew, err := zw.Create("3D/3dmodel.model")
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(ew, 1<<20)
	bw.WriteString(xmlHead)
	bw.WriteString(`<model unit="millimeter" xml:lang="en-US" xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02">` + "\n")
	var esc bytes.Buffer
	_ = xmlEscape(&esc, title)
	fmt.Fprintf(bw, ` <metadata name="Title">%s</metadata>`+"\n", esc.String())
	bw.WriteString(" <resources>\n")
	if len(m.palette) > 0 {
		bw.WriteString(`  <basematerials id="1">` + "\n")
		for i, c := range m.palette {
			fmt.Fprintf(bw, `   <base name="Color %d" displaycolor="#%02X%02X%02XFF"/>`+"\n", i+1, c[0], c[1], c[2])
		}
		bw.WriteString("  </basematerials>\n")
	}
	bw.WriteString(`  <object id="2" type="model">` + "\n   <mesh>\n    <vertices>\n")
	var num [32]byte
	for _, v := range m.verts {
		bw.WriteString(`     <vertex x="`)
		bw.Write(strconv.AppendFloat(num[:0], v[0], 'g', -1, 64))
		bw.WriteString(`" y="`)
		bw.Write(strconv.AppendFloat(num[:0], v[1], 'g', -1, 64))
		bw.WriteString(`" z="`)
		bw.Write(strconv.AppendFloat(num[:0], v[2], 'g', -1, 64))
		bw.WriteString(`"/>` + "\n")
	}
	bw.WriteString("    </vertices>\n    <triangles>\n")
	for i, t := range m.tris {
		fmt.Fprintf(bw, `     <triangle v1="%d" v2="%d" v3="%d"`, t[0], t[1], t[2])
		if mat := m.triMat[i]; mat >= 0 {
			fmt.Fprintf(bw, ` pid="1" p1="%d"`, mat)
		}
		bw.WriteString("/>\n")
	}
	bw.WriteString("    </triangles>\n   </mesh>\n  </object>\n </resources>\n <build>\n  <item objectid=\"2\"/>\n </build>\n</model>\n")
	if err := bw.Flush(); err != nil {
		return err
	}
	return zw.Close()
}

const xmlHead = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"

func xmlEscape(w *bytes.Buffer, s string) error {
	for _, r := range s {
		switch r {
		case '&':
			w.WriteString("&amp;")
		case '<':
			w.WriteString("&lt;")
		case '>':
			w.WriteString("&gt;")
		case '"':
			w.WriteString("&quot;")
		default:
			if r >= 0x20 || r == '\t' || r == '\n' {
				w.WriteRune(r)
			}
		}
	}
	return nil
}
