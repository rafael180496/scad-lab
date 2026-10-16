package raster

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// box devuelve los 12 triángulos de una caja alineada a los ejes.
func box(x0, y0, z0, x1, y1, z1 float32) []Tri {
	v := [8][3]float32{
		{x0, y0, z0}, {x1, y0, z0}, {x1, y1, z0}, {x0, y1, z0},
		{x0, y0, z1}, {x1, y0, z1}, {x1, y1, z1}, {x0, y1, z1},
	}
	faces := [][4]int{{0, 3, 2, 1}, {4, 5, 6, 7}, {0, 1, 5, 4}, {1, 2, 6, 5}, {2, 3, 7, 6}, {3, 0, 4, 7}}
	var out []Tri
	for _, f := range faces {
		a, b, c, d := v[f[0]], v[f[1]], v[f[2]], v[f[3]]
		out = append(out,
			Tri{a[0], a[1], a[2], b[0], b[1], b[2], c[0], c[1], c[2]},
			Tri{a[0], a[1], a[2], c[0], c[1], c[2], d[0], d[1], d[2]})
	}
	return out
}

var (
	red   = [3]uint8{255, 0, 0}
	green = [3]uint8{0, 255, 0}
	blue  = [3]uint8{0, 0, 255}
	gray  = [3]uint8{200, 200, 200}
)

// scene: una caja gris en el origen y una marca de color en +X (rojo), +Y
// (verde) y +Z (azul).
func scene() []Part {
	return []Part{
		{Tris: box(-5, -5, -5, 5, 5, 5), Color: gray},
		{Tris: box(15, -2, -2, 19, 2, 2), Color: red},
		{Tris: box(-2, 15, -2, 2, 19, 2), Color: green},
		{Tris: box(-2, -2, 15, 2, 2, 19), Color: blue},
	}
}

// centroid devuelve el centro (en píxeles) de los píxeles de «predominantemente
// ese color», relativo al centro de la imagen, con y hacia arriba.
func centroid(img *image.RGBA, want [3]uint8) (dx, dy float64, n int) {
	b := img.Bounds()
	cx, cy := float64(b.Dx())/2, float64(b.Dy())/2
	var sx, sy float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			o := img.PixOffset(x, y)
			r, g, bl := img.Pix[o], img.Pix[o+1], img.Pix[o+2]
			match := false
			switch want {
			case red:
				match = r > 120 && g < 60 && bl < 60
			case green:
				match = g > 120 && r < 60 && bl < 60
			case blue:
				match = bl > 120 && r < 60 && g < 60
			}
			if match {
				sx += float64(x) + 0.5
				sy += float64(y) + 0.5
				n++
			}
		}
	}
	if n == 0 {
		return 0, 0, 0
	}
	return sx/float64(n) - cx, cy - sy/float64(n), n
}

// Las vistas del preview colocan cada marca del lado que dice la convención del
// preview nativo de OpenSCAD (ver backend/mcp/preview.go): front desde −Y,
// right desde +X, top desde +Z… Se compara la posición de las marcas entre sí
// (la pieza se centra en su caja, no en el origen).
func TestViewsPlaceMarksOnTheExpectedSide(t *testing.T) {
	type rel struct {
		a, b string
		axis byte // 'x' = a queda a la derecha de b; 'y' = a queda arriba de b
		neg  bool // al revés
	}
	cases := []struct {
		name string
		v    View
		rels []rel
	}{
		{"front (desde −Y)", View{RotX: 90, Ortho: true}, []rel{{"red", "blue", 'x', false}, {"blue", "red", 'y', false}}},
		{"back (desde +Y)", View{RotX: 90, RotZ: 180, Ortho: true}, []rel{{"red", "blue", 'x', true}, {"blue", "red", 'y', false}}},
		{"right (desde +X)", View{RotX: 90, RotZ: 90, Ortho: true}, []rel{{"green", "blue", 'x', false}, {"blue", "green", 'y', false}}},
		{"left (desde −X)", View{RotX: 90, RotZ: 270, Ortho: true}, []rel{{"green", "blue", 'x', true}, {"blue", "green", 'y', false}}},
		{"top (desde +Z)", View{Ortho: true}, []rel{{"red", "green", 'x', false}, {"green", "red", 'y', false}}},
		{"bottom (desde −Z)", View{RotX: 180, Ortho: true}, []rel{{"red", "green", 'x', false}, {"green", "red", 'y', true}}},
	}
	marks := map[string][3]uint8{"red": red, "green": green, "blue": blue}
	for _, c := range cases {
		img := Render(scene(), c.v, 400)
		type pt struct{ x, y float64 }
		at := map[string]pt{}
		for name, col := range marks {
			dx, dy, n := centroid(img, col)
			if n > 0 {
				at[name] = pt{dx, dy}
			}
		}
		for _, r := range c.rels {
			pa, oka := at[r.a]
			pb, okb := at[r.b]
			if !oka || !okb {
				t.Errorf("%s: no se ven las marcas %s y %s", c.name, r.a, r.b)
				continue
			}
			d := pa.x - pb.x
			side := "derecha"
			if r.axis == 'y' {
				d = pa.y - pb.y
				side = "arriba"
			}
			if r.neg {
				d = -d
				side = map[string]string{"derecha": "izquierda", "arriba": "abajo"}[side]
			}
			if d < 20 {
				t.Errorf("%s: %s debería quedar a la %s de %s (diferencia %.0f px)", c.name, r.a, side, r.b, d)
			}
		}
	}
}

func TestIsoViewsAndPerspectiveDrawSomething(t *testing.T) {
	for name, v := range map[string]View{"iso": {RotX: 55, RotZ: 25}, "iso-back": {RotX: 55, RotZ: 205}} {
		img := Render(scene(), v, 300)
		for cname, col := range map[string][3]uint8{"red": red, "green": green, "blue": blue} {
			if _, _, n := centroid(img, col); n < 30 {
				t.Errorf("%s: la marca %s no se ve (%d px)", name, cname, n)
			}
		}
	}
}

// Lo de adelante tapa lo de atrás (z-buffer): desde el frente, una caja azul
// delante de una roja tapa a la roja.
func TestZBuffer(t *testing.T) {
	parts := []Part{
		{Tris: box(-5, 5, -5, 5, 8, 5), Color: red},    // atrás (y mayor)
		{Tris: box(-5, -8, -5, 5, -5, 5), Color: blue}, // adelante (mirando desde −Y)
	}
	img := Render(parts, View{RotX: 90, Ortho: true}, 200)
	if _, _, n := centroid(img, red); n != 0 {
		t.Errorf("la caja de atrás se ve a través de la de adelante (%d px)", n)
	}
	if _, _, n := centroid(img, blue); n < 1000 {
		t.Errorf("la caja de adelante casi no se ve (%d px)", n)
	}
}

func TestEmptyAndDegenerateInput(t *testing.T) {
	img := Render(nil, View{Ortho: true}, 64)
	if img.Bounds().Dx() != 64 || img.Pix[0] != 255 {
		t.Error("sin geometría: imagen blanca del tamaño pedido")
	}
	// Un triángulo degenerado no rompe nada.
	Render([]Part{{Tris: []Tri{{0, 0, 0, 0, 0, 0, 0, 0, 0}}, Color: red}}, View{}, 64)
	for _, bad := range []float32{float32(math.NaN()), float32(math.Inf(1))} {
		Render([]Part{{Tris: []Tri{{0, 0, 0, bad, 1, 0, 0, 1, 1}}, Color: red}}, View{}, 64)
	}
}

func TestReadSTLBinaryAndASCII(t *testing.T) {
	dir := t.TempDir()
	tris := box(0, 0, 0, 1, 2, 3)
	var bin bytes.Buffer
	bin.Write(make([]byte, 80))
	_ = binary.Write(&bin, binary.LittleEndian, uint32(len(tris)))
	for _, tr := range tris {
		bin.Write(make([]byte, 12))
		for _, f := range tr {
			_ = binary.Write(&bin, binary.LittleEndian, f)
		}
		bin.Write([]byte{0, 0})
	}
	p := filepath.Join(dir, "b.stl")
	if err := os.WriteFile(p, bin.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSTL(p)
	if err != nil || len(got) != 12 || got[0] != tris[0] {
		t.Fatalf("binario: %d triángulos, err=%v", len(got), err)
	}

	asc := "solid x\n facet normal 0 0 1\n  outer loop\n   vertex 0 0 0\n   vertex 1 0 0\n   vertex 0 1 0\n  endloop\n endfacet\nendsolid x\n"
	pa := filepath.Join(dir, "a.stl")
	if err := os.WriteFile(pa, []byte(asc), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := ReadSTL(pa); err != nil || len(got) != 1 || got[0] != (Tri{0, 0, 0, 1, 0, 0, 0, 1, 0}) {
		t.Fatalf("ASCII: %v err=%v", got, err)
	}

	// Truncado: error, no pánico.
	pt := filepath.Join(dir, "t.stl")
	if err := os.WriteFile(pt, bin.Bytes()[:200], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSTL(pt); err == nil {
		t.Error("un STL truncado tiene que dar error")
	}
}

func TestEncodePNG(t *testing.T) {
	raw, err := EncodePNG(Render(scene(), View{Ortho: true}, 128))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != 128 || cfg.Height != 128 {
		t.Fatalf("png: %+v err=%v", cfg, err)
	}
}
