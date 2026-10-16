package scadwasm

import (
	"bufio"
	"math"
	"strings"
	"testing"
)

func triArea2D(v [][3]float64, t [3]int32) float64 {
	a, b, c := v[t[0]], v[t[1]], v[t[2]]
	return math.Abs((b[0]-a[0])*(c[1]-a[1])-(b[1]-a[1])*(c[0]-a[0])) / 2
}

// Un polígono cóncavo (una L) se corta en n−2 triángulos que suman exactamente
// su área: un abanico desde el primer vértice se saldría de la L.
func TestTriangulateConcavePolygon(t *testing.T) {
	verts := [][3]float64{{0, 0, 0}, {4, 0, 0}, {4, 1, 0}, {1, 1, 0}, {1, 4, 0}, {0, 4, 0}}
	idx := []int32{0, 1, 2, 3, 4, 5}
	tris := triangulate(verts, idx)
	if len(tris) != 4 {
		t.Fatalf("triángulos: %d, quería 4", len(tris))
	}
	var area float64
	for _, tr := range tris {
		area += triArea2D(verts, tr)
	}
	if math.Abs(area-7) > 1e-9 { // 4×1 + 1×3
		t.Errorf("área = %v, quería 7", area)
	}
	// El mismo polígono en un plano vertical (x=0) y con el orden invertido.
	vert := make([][3]float64, len(verts))
	for i, v := range verts {
		vert[i] = [3]float64{0, v[0], v[1]}
	}
	rev := []int32{5, 4, 3, 2, 1, 0}
	if got := triangulate(vert, rev); len(got) != 4 {
		t.Errorf("plano vertical: %d triángulos", len(got))
	}
	// Un cuadrilátero y un triángulo.
	sq := [][3]float64{{0, 0, 0}, {1, 0, 0}, {1, 1, 0}, {0, 1, 0}}
	if got := triangulate(sq, []int32{0, 1, 2, 3}); len(got) != 2 {
		t.Errorf("cuadrilátero: %d", len(got))
	}
	if got := triangulate(sq, []int32{0, 1, 2}); len(got) != 1 {
		t.Errorf("triángulo: %d", len(got))
	}
	// Todo colineal: no se cuelga ni inventa área.
	line := [][3]float64{{0, 0, 0}, {1, 0, 0}, {2, 0, 0}, {3, 0, 0}}
	_ = triangulate(line, []int32{0, 1, 2, 3})
}

func TestReadOFFColorsAndDefaults(t *testing.T) {
	// El OFF de OpenSCAD: con algún color(), las caras sin color traen el
	// amarillo por defecto (249 215 44) y se leen como «sin color»; el alfa
	// (cuarto componente) no cuenta.
	off := "OFF 6 3 0\n0 0 0 \n1 0 0 \n0 1 0 \n0 0 1 \n1 1 0 \n1 1 1 \n" +
		"3 0 1 2 249 215 44\n" +
		"3 0 1 3 255 0 0\n" +
		"4 1 4 5 3 0 255 0 128\n"
	m, err := readOFF(bufio.NewReader(strings.NewReader(off)))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.tris) != 4 { // 1 + 1 + el cuadrilátero partido en 2
		t.Fatalf("triángulos: %d", len(m.tris))
	}
	if len(m.palette) != 2 || m.palette[0] != [3]uint8{255, 0, 0} || m.palette[1] != [3]uint8{0, 255, 0} {
		t.Errorf("paleta: %v", m.palette)
	}
	want := []int32{-1, 0, 1, 1}
	for i, w := range want {
		if m.triMat[i] != w {
			t.Errorf("material del triángulo %d = %d, quería %d", i, m.triMat[i], w)
		}
	}
}

func TestReadOFFRejectsGarbage(t *testing.T) {
	for name, in := range map[string]string{
		"vacío":              "",
		"sin cabecera":       "3 0 1 2\n",
		"vértice incompleto": "OFF 2 1 0\n0 0 0\n1 1\n",
		"índice fuera":       "OFF 3 1 0\n0 0 0\n1 0 0\n0 1 0\n3 0 1 9\n",
		"sin caras":          "OFF 3 0 0\n0 0 0\n1 0 0\n0 1 0\n",
		"NaN":                "OFF 3 1 0\n0 0 NaN\n1 0 0\n0 1 0\n3 0 1 2\n",
	} {
		if _, err := readOFF(bufio.NewReader(strings.NewReader(in))); err == nil {
			t.Errorf("%s: tendría que dar error", name)
		}
	}
}
