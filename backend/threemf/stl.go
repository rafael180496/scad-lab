package threemf

import (
	"bufio"
	"encoding/binary"
	"io"
	"math"
)

// WriteSTL escribe la parte como STL binario (encabezado de 80 bytes, contador
// y 50 bytes por triángulo). El orden de los vértices se conserva —sale
// antihorario visto desde fuera, como en el 3MF— y la normal se recalcula: los
// slicers la ignoran y la recalculan, pero otros visores sí la leen.
func WriteSTL(w io.Writer, p Part) error {
	bw := bufio.NewWriterSize(w, 1<<16)
	var head [84]byte
	copy(head[:], "ScadLab STL")
	binary.LittleEndian.PutUint32(head[80:], uint32(len(p.Triangles)))
	if _, err := bw.Write(head[:]); err != nil {
		return err
	}
	var rec [50]byte
	put := func(off int, v float64) {
		binary.LittleEndian.PutUint32(rec[off:], math.Float32bits(float32(v)))
	}
	for _, t := range p.Triangles {
		a, b, c := p.Vertices[t[0]], p.Vertices[t[1]], p.Vertices[t[2]]
		u := [3]float64{b[0] - a[0], b[1] - a[1], b[2] - a[2]}
		v := [3]float64{c[0] - a[0], c[1] - a[1], c[2] - a[2]}
		n := [3]float64{u[1]*v[2] - u[2]*v[1], u[2]*v[0] - u[0]*v[2], u[0]*v[1] - u[1]*v[0]}
		if l := math.Sqrt(n[0]*n[0] + n[1]*n[1] + n[2]*n[2]); l > 0 {
			n = [3]float64{n[0] / l, n[1] / l, n[2] / l}
		}
		for k := 0; k < 3; k++ {
			put(k*4, n[k])
			put(12+k*4, a[k])
			put(24+k*4, b[k])
			put(36+k*4, c[k])
		}
		rec[48], rec[49] = 0, 0
		if _, err := bw.Write(rec[:]); err != nil {
			return err
		}
	}
	return bw.Flush()
}
