package mesh

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type tri [3][3]float64

// cube10 son los 12 triángulos de un cubo de 10 mm en el origen, orientados
// hacia afuera.
func cube10() []tri {
	p := func(x, y, z float64) [3]float64 { return [3]float64{x * 10, y * 10, z * 10} }
	quads := [][4][3]float64{
		{p(0, 0, 0), p(0, 1, 0), p(1, 1, 0), p(1, 0, 0)}, // abajo
		{p(0, 0, 1), p(1, 0, 1), p(1, 1, 1), p(0, 1, 1)}, // arriba
		{p(0, 0, 0), p(1, 0, 0), p(1, 0, 1), p(0, 0, 1)}, // frente
		{p(0, 1, 0), p(0, 1, 1), p(1, 1, 1), p(1, 1, 0)}, // atrás
		{p(0, 0, 0), p(0, 0, 1), p(0, 1, 1), p(0, 1, 0)}, // izquierda
		{p(1, 0, 0), p(1, 1, 0), p(1, 1, 1), p(1, 0, 1)}, // derecha
	}
	var out []tri
	for _, q := range quads {
		out = append(out, tri{q[0], q[1], q[2]}, tri{q[0], q[2], q[3]})
	}
	return out
}

// binarySTL escribe un STL binario con un encabezado que empieza con «solid»,
// como hacen muchos exportadores: la detección tiene que ir por tamaño.
func binarySTL(tris []tri) []byte {
	var b bytes.Buffer
	head := make([]byte, 80)
	copy(head, "solid exported-by-something")
	b.Write(head)
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(tris)))
	for _, t := range tris {
		_ = binary.Write(&b, binary.LittleEndian, [3]float32{}) // normal
		for _, v := range t {
			_ = binary.Write(&b, binary.LittleEndian, [3]float32{float32(v[0]), float32(v[1]), float32(v[2])})
		}
		_ = binary.Write(&b, binary.LittleEndian, uint16(0))
	}
	return b.Bytes()
}

func asciiSTL(tris []tri) []byte {
	var b strings.Builder
	b.WriteString("solid cube\r\n") // \r\n tolerado (archivos de Windows)
	for _, t := range tris {
		b.WriteString("  facet normal 0 0 0\n    outer loop\n")
		for _, v := range t {
			fmt.Fprintf(&b, "      vertex %g %g %g\n", v[0], v[1], v[2])
		}
		b.WriteString("    endloop\n  endfacet\n")
	}
	b.WriteString("endsolid cube\n")
	return []byte(b.String())
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol*math.Max(1, math.Abs(b)) }

func checkCube(t *testing.T, in Info) {
	t.Helper()
	if in.Triangles != 12 {
		t.Errorf("triángulos = %d, quiero 12", in.Triangles)
	}
	if !near(in.VolumeMM3, 1000, 1e-9) || !near(in.AreaMM2, 600, 1e-9) {
		t.Errorf("volumen %v área %v, quiero 1000 y 600", in.VolumeMM3, in.AreaMM2)
	}
	if in.Min != [3]float64{0, 0, 0} || in.Max != [3]float64{10, 10, 10} || in.Size != [3]float64{10, 10, 10} {
		t.Errorf("bbox %v..%v tamaño %v", in.Min, in.Max, in.Size)
	}
	if !in.Watertight {
		t.Error("el cubo cerrado tiene que ser watertight")
	}
}

func TestCubeBinaryAndASCII(t *testing.T) {
	for name, data := range map[string][]byte{"binario": binarySTL(cube10()), "ascii": asciiSTL(cube10())} {
		t.Run(name, func(t *testing.T) {
			in, err := ParseSTL(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			checkCube(t, in)
			// Un lector sin Seek (pipe) pasa por el camino que lee todo.
			in, err = ParseSTL(io.MultiReader(bytes.NewReader(data)))
			if err != nil {
				t.Fatal(err)
			}
			checkCube(t, in)
		})
	}
}

func TestInfoFromFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cube.stl")
	if err := os.WriteFile(p, binarySTL(cube10()), 0o644); err != nil {
		t.Fatal(err)
	}
	in, err := InfoFromFile(p)
	if err != nil {
		t.Fatal(err)
	}
	checkCube(t, in)
	if _, err := InfoFromFile(filepath.Join(t.TempDir(), "no-existe.stl")); err == nil {
		t.Error("un archivo inexistente tiene que dar error")
	}
}

// La orientación invertida da volumen con signo negativo: se reporta |v|.
func TestVolumeIsAbsolute(t *testing.T) {
	var flipped []tri
	for _, tr := range cube10() {
		flipped = append(flipped, tri{tr[0], tr[2], tr[1]})
	}
	in, err := ParseSTL(bytes.NewReader(binarySTL(flipped)))
	if err != nil {
		t.Fatal(err)
	}
	if !near(in.VolumeMM3, 1000, 1e-9) {
		t.Errorf("volumen %v, quiero 1000", in.VolumeMM3)
	}
}

func TestOpenCubeIsNotWatertight(t *testing.T) {
	open := cube10()[2:] // sin la tapa de abajo
	in, err := ParseSTL(bytes.NewReader(asciiSTL(open)))
	if err != nil {
		t.Fatal(err)
	}
	if in.Watertight {
		t.Error("un cubo sin tapa no es watertight")
	}
	if in.Triangles != 10 || !near(in.AreaMM2, 500, 1e-9) {
		t.Errorf("triángulos %d área %v", in.Triangles, in.AreaMM2)
	}
	// Una cara de más (arista compartida por 3) tampoco.
	extra := append(cube10(), cube10()[0])
	in, _ = ParseSTL(bytes.NewReader(binarySTL(extra)))
	if in.Watertight {
		t.Error("una arista con 3 caras no es watertight")
	}
}

func TestTruncatedAndInvalid(t *testing.T) {
	bin := binarySTL(cube10())
	asc := asciiSTL(cube10())
	cases := map[string][]byte{
		"binario sin el último triángulo": bin[:len(bin)-50],
		"binario cortado a mitad":         bin[:len(bin)-17],
		"solo encabezado incompleto":      bin[:40],
		"ascii sin endsolid":              asc[:bytes.LastIndex(asc, []byte("endsolid"))],
		"ascii cortado en un vértice":     asc[:bytes.Index(asc, []byte("vertex 10"))+9],
		"ascii con basura":                []byte("solid x\nfacet normal 0 0 1\nouter loop\nvertex 1 2\nendloop\n"),
		"vacío":                           nil,
		"otra cosa":                       []byte("PK\x03\x04 un zip"),
	}
	for name, data := range cases {
		if _, err := ParseSTL(bytes.NewReader(data)); err == nil {
			t.Errorf("%s: quería error", name)
		}
	}
	if _, err := ParseSTL(bytes.NewReader(bin[:len(bin)-50])); !errors.Is(err, ErrTruncated) {
		t.Errorf("binario truncado: %v, quería ErrTruncated", err)
	}
}

func TestNonFiniteCoordinates(t *testing.T) {
	bad := cube10()
	bad[3][1][2] = math.NaN()
	if _, err := ParseSTL(bytes.NewReader(binarySTL(bad))); !errors.Is(err, ErrNonFinite) {
		t.Errorf("NaN: %v, quería ErrNonFinite", err)
	}
}

func TestEmptyBinaryIsNotWatertight(t *testing.T) {
	in, err := ParseSTL(bytes.NewReader(binarySTL(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if in.Triangles != 0 || in.Watertight {
		t.Errorf("malla vacía: %+v", in)
	}
}

// Mallas reales de OpenSCAD, en binario y en ASCII, contra la fórmula.
func TestRealOpenSCADMeshes(t *testing.T) {
	bin, err := exec.LookPath("openscad")
	if err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	const n = 64
	cyl := 0.5 * n * 25 * math.Sin(2*math.Pi/n) * 10 // prisma de n lados inscrito, r=5 h=10
	cases := []struct {
		name, src string
		volume    float64
		size      [3]float64
	}{
		{"cubo", "cube(10);", 1000, [3]float64{10, 10, 10}},
		{"cilindro", "cylinder(r=5, h=10, $fn=64);", cyl, [3]float64{10, 10, 10}},
		{"diferencia", "difference(){ cube(20, center=true); cube([30,10,10], center=true); }", 8000 - 2000, [3]float64{20, 20, 20}},
	}
	dir := t.TempDir()
	for _, c := range cases {
		for _, format := range []string{"binstl", "asciistl"} {
			t.Run(c.name+"-"+format, func(t *testing.T) {
				scad := filepath.Join(dir, c.name+".scad")
				out := filepath.Join(dir, c.name+"-"+format+".stl")
				if err := os.WriteFile(scad, []byte(c.src+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(bin, "-o", out, "--export-format", format, scad)
				if b, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("openscad: %v\n%s", err, b)
				}
				in, err := InfoFromFile(out)
				if err != nil {
					t.Fatal(err)
				}
				if !near(in.VolumeMM3, c.volume, 1e-4) {
					t.Errorf("volumen %v, quiero %v", in.VolumeMM3, c.volume)
				}
				for i := 0; i < 3; i++ {
					// El cilindro de $fn=64 tiene un vértice en x=±r pero no en
					// y=±r exacto: tolerancia de cuerda.
					if math.Abs(in.Size[i]-c.size[i]) > 0.05 {
						t.Errorf("tamaño %v, quiero %v", in.Size, c.size)
						break
					}
				}
				if !in.Watertight {
					t.Error("una malla de OpenSCAD tiene que ser watertight")
				}
			})
		}
	}
}

// Una malla indexada (OBJ, 3MF) da las mismas cuentas que el STL del mismo
// cubo, y un índice fuera de rango es un error y no un pánico.
func TestInfoFromIndexed(t *testing.T) {
	verts := [][3]float64{{0, 0, 0}, {10, 0, 0}, {10, 10, 0}, {0, 10, 0}, {0, 0, 10}, {10, 0, 10}, {10, 10, 10}, {0, 10, 10}}
	tris := [][3]int32{
		{0, 3, 2}, {0, 2, 1}, {4, 5, 6}, {4, 6, 7},
		{0, 1, 5}, {0, 5, 4}, {3, 7, 6}, {3, 6, 2},
		{0, 4, 7}, {0, 7, 3}, {1, 2, 6}, {1, 6, 5},
	}
	got, err := InfoFromIndexed(verts, tris)
	if err != nil {
		t.Fatal(err)
	}
	if got.Triangles != 12 || !got.Watertight || math.Abs(got.VolumeMM3-1000) > 1e-9 || math.Abs(got.AreaMM2-600) > 1e-9 {
		t.Errorf("cubo indexado: %+v", got)
	}
	if got.Size != [3]float64{10, 10, 10} {
		t.Errorf("tamaño: %v", got.Size)
	}
	if _, err := InfoFromIndexed(verts, [][3]int32{{0, 1, 99}}); !errors.Is(err, ErrBadIndex) {
		t.Errorf("índice fuera de rango: %v", err)
	}
	if _, err := InfoFromIndexed(verts, [][3]int32{{0, 1, -1}}); !errors.Is(err, ErrBadIndex) {
		t.Errorf("índice negativo: %v", err)
	}
}
