package threemf_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"scadlab/backend/mesh"
	. "scadlab/backend/threemf"
	"scadlab/backend/threemf/bbscheck"
	"scadlab/backend/threemf/macetatest"
)

// Pruebas de «¿cada parte es un sólido que un slicer sabe laminar?» (S17, tarea 17.2).
//
// El caso que las originó: la maceta verde con un relieve naranja curvado se abría en Bambu
// Studio con el hueco relleno y muchos soportes. Los números del diagnóstico están en
// claude/11-exportacion.md; aquí se fijan con una maceta reducida (macetatest) y sus dos 3MF
// de OpenSCAD: el de por defecto (maceta-union.3mf) y el de --enable=lazy-union.

func loadParts(t *testing.T, path string) []Part {
	t.Helper()
	m, err := ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return Split(m)
}

// loadFixture lee un 3MF embebido (macetatest.Union / Lazy).
func loadFixture(t *testing.T, b []byte) []Part {
	t.Helper()
	m, err := Read(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	return Split(m)
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

func nearVec(a, b [3]float64, tol float64) bool {
	return near(a[0], b[0], tol) && near(a[1], b[1], tol) && near(a[2], b[2], tol)
}

func cubePart(x float64) Part {
	v := func(a, b, c float64) [3]float64 { return [3]float64{x + a, b, c} }
	return Part{
		Name: "cubo",
		Vertices: [][3]float64{
			v(0, 0, 0), v(1, 0, 0), v(1, 1, 0), v(0, 1, 0), v(0, 0, 1), v(1, 0, 1), v(1, 1, 1), v(0, 1, 1),
		},
		Triangles: [][3]int32{
			{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4},
			{1, 2, 6}, {1, 6, 5}, {2, 3, 7}, {2, 7, 6}, {3, 0, 4}, {3, 4, 7},
		},
	}
}

func TestInspectClosedCube(t *testing.T) {
	c := Inspect(cubePart(0))
	if !c.Closed() || c.OpenEdges != 0 || c.NonManifoldEdges != 0 || c.Degenerate != 0 || c.Duplicates != 0 {
		t.Fatalf("un cubo bien formado tiene que ser cerrado: %+v", c)
	}
	if !near(c.SignedVolume, 1, 1e-12) || c.Min != [3]float64{0, 0, 0} || c.Max != [3]float64{1, 1, 1} {
		t.Errorf("volumen/caja: %+v", c)
	}
}

// Normales hacia adentro: el volumen con signo es negativo y la parte no se considera
// cerrada (el slicer la vería como un hueco).
func TestInspectInvertedCube(t *testing.T) {
	p := cubePart(0)
	for i, t3 := range p.Triangles {
		p.Triangles[i] = [3]int32{t3[0], t3[2], t3[1]}
	}
	c := Inspect(p)
	if c.SignedVolume > -0.99 || c.Closed() {
		t.Fatalf("cubo invertido: %+v", c)
	}
	if c.OpenEdges != 0 || c.NonManifoldEdges != 0 {
		t.Errorf("invertido pero sin huecos: %+v", c)
	}
}

// Una cara menos = un borde abierto de 3 aristas dirigidas sin opuesta.
func TestInspectOpenCube(t *testing.T) {
	p := cubePart(0)
	p.Triangles = p.Triangles[1:]
	c := Inspect(p)
	if c.Closed() || c.OpenEdges != 3 {
		t.Fatalf("cubo sin una cara: %+v", c)
	}
}

// Un triángulo repetido y uno degenerado se cuentan, y el repetido rompe la variedad.
func TestInspectDuplicateAndDegenerate(t *testing.T) {
	p := cubePart(0)
	p.Triangles = append(p.Triangles, p.Triangles[0], [3]int32{0, 0, 1})
	c := Inspect(p)
	if c.Duplicates != 1 || c.Degenerate != 1 || c.NonManifoldEdges == 0 || c.Closed() {
		t.Fatalf("duplicado/degenerado: %+v", c)
	}
}

// Los vértices repetidos en el mismo punto se sueldan: un cubo con índices duplicados sigue
// siendo cerrado.
func TestInspectWeldsCoincidentVertices(t *testing.T) {
	p := cubePart(0)
	p.Vertices = append(p.Vertices, p.Vertices[0]) // índice 8 = índice 0
	p.Triangles[0] = [3]int32{8, 2, 1}
	if c := Inspect(p); !c.Closed() {
		t.Fatalf("vértice duplicado en el mismo punto: %+v", c)
	}
}

func TestInspectAllCountsSharedTriangles(t *testing.T) {
	a, b := cubePart(0), cubePart(0)
	b.Name = "otro"
	_, shared := InspectAll([]Part{a, b})
	if shared != 12 {
		t.Errorf("triángulos compartidos %d, quería 12", shared)
	}
	if _, shared := InspectAll([]Part{cubePart(0), cubePart(5)}); shared != 0 {
		t.Errorf("cubos separados no comparten nada: %d", shared)
	}
}

// LA CAUSA. Lo que OpenSCAD escribe por defecto (todo lo del nivel superior fundido en un
// solo sólido) repartido por color deja dos cáscaras abiertas: la cara por la que el relieve
// naranja se apoya en la pared verde ya no existe en ninguna.
func TestMacetaUnionSplitLeavesOpenShells(t *testing.T) {
	parts := loadFixture(t, macetatest.Union)
	if len(parts) != 2 {
		t.Fatalf("partes: %d", len(parts))
	}
	checks, _ := InspectAll(parts)
	for _, c := range checks {
		if c.Closed() || c.OpenEdges == 0 {
			t.Errorf("%s: esperaba una cáscara abierta: %+v", c.Name, c)
		}
		if c.NonManifoldEdges != 0 || c.Degenerate != 0 || c.Duplicates != 0 {
			t.Errorf("%s: el defecto es solo el borde abierto: %+v", c.Name, c)
		}
	}
	// Los dos bordes son el mismo: los lazos de contacto entre los colores.
	if checks[0].OpenEdges != checks[1].OpenEdges {
		t.Errorf("bordes abiertos distintos: %d y %d", checks[0].OpenEdges, checks[1].OpenEdges)
	}
	if OpenEdgeCount(parts) == 0 {
		t.Error("OpenEdgeCount tiene que verlos")
	}
}

// Con lazy-union cada color() del nivel superior llega como su propio sólido: partes
// cerradas, con el volumen y la caja de ese color solo.
func TestMacetaLazySplitIsClosed(t *testing.T) {
	macetatest.AssertSolids(t, loadFixture(t, macetatest.Lazy), 1e-3)
}

// El 3MF que se escribe: mallas con las coordenadas del .scad (sin doble traslación), un
// <item> que solo traslada (determinante +1, sin espejado), componentes sin transformación,
// partes «normal_part» y el mismo sólido al releerlo.
func TestMacetaWrittenFileTransformsAndVolumes(t *testing.T) {
	parts := loadFixture(t, macetatest.Lazy)
	bed := [2]float64{90, 90}
	dest := filepath.Join(t.TempDir(), "maceta.3mf")
	if err := WriteFile(dest, parts, WriteOptions{Title: "maceta", Place: true, BedCenter: bed}); err != nil {
		t.Fatal(err)
	}
	CheckPackage(t, dest, 2)

	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var model struct {
		Objects []struct {
			ID         string `xml:"id,attr"`
			Transform  string `xml:"transform,attr"`
			Components []struct {
				ObjectID  string `xml:"objectid,attr"`
				Transform string `xml:"transform,attr"`
			} `xml:"components>component"`
		} `xml:"resources>object"`
		Items []struct {
			ObjectID  string `xml:"objectid,attr"`
			Transform string `xml:"transform,attr"`
		} `xml:"build>item"`
	}
	if err := xml.Unmarshal([]byte(ReadEntry(t, &zr.Reader, "3D/3dmodel.model")), &model); err != nil {
		t.Fatal(err)
	}
	if len(model.Items) != 1 {
		t.Fatalf("items: %+v", model.Items)
	}
	f := strings.Fields(model.Items[0].Transform)
	if len(f) != 12 {
		t.Fatalf("transform del item: %q", model.Items[0].Transform)
	}
	var m [12]float64
	for i, s := range f {
		m[i], _ = strconv.ParseFloat(s, 64)
	}
	rot := [9]float64{m[0], m[1], m[2], m[3], m[4], m[5], m[6], m[7], m[8]}
	if rot != [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1} {
		t.Errorf("el item solo debe trasladar; matriz %v", rot)
	}
	det := rot[0]*(rot[4]*rot[8]-rot[5]*rot[7]) - rot[1]*(rot[3]*rot[8]-rot[5]*rot[6]) + rot[2]*(rot[3]*rot[7]-rot[4]*rot[6])
	if det != 1 {
		t.Errorf("determinante %v: un valor negativo es un espejado que invierte las normales", det)
	}
	// La traslación pone el centro de la caja en el de la cama y apoya la pieza en Z=0 una
	// sola vez (si las mallas ya vinieran trasladadas, la caja saldría corrida el doble).
	mn, mx, _ := Bounds(parts)
	wantT := [3]float64{bed[0] - (mn[0]+mx[0])/2, bed[1] - (mn[1]+mx[1])/2, -mn[2]}
	if !nearVec([3]float64{m[9], m[10], m[11]}, wantT, 1e-9) {
		t.Errorf("traslación %v, esperaba %v", m[9:], wantT)
	}
	for _, o := range model.Objects {
		if o.Transform != "" {
			t.Errorf("objeto %s con transform propio: %q", o.ID, o.Transform)
		}
		for _, c := range o.Components {
			if c.Transform != "" {
				t.Errorf("componente %s con transform: %q (se sumaría a la del item)", c.ObjectID, c.Transform)
			}
		}
	}

	// Releído (con las transformaciones aplicadas): mismo sólido, desplazado a la cama.
	back := loadParts(t, dest)
	if len(back) != 2 {
		t.Fatalf("partes al releer: %d", len(back))
	}
	for i, c := range func() []PartCheck { cs, _ := InspectAll(back); return cs }() {
		orig := Inspect(parts[i])
		if !c.Closed() || !near(c.SignedVolume, orig.SignedVolume, 1e-6*orig.SignedVolume) {
			t.Errorf("parte %d releída: %+v (original %+v)", i, c, orig)
		}
		shift := [3]float64{wantT[0], wantT[1], wantT[2]}
		if !nearVec(c.Min, [3]float64{orig.Min[0] + shift[0], orig.Min[1] + shift[1], orig.Min[2] + shift[2]}, 1e-6) {
			t.Errorf("parte %d releída: mínimo %v, esperaba el original desplazado %v", i, c.Min, shift)
		}
	}
	if bmn, bmx, _ := Bounds(back); !near(bmn[2], 0, 1e-9) || !near((bmn[0]+bmx[0])/2, bed[0], 1e-6) || !near((bmn[1]+bmx[1])/2, bed[1], 1e-6) {
		t.Errorf("caja releída %v–%v: tiene que apoyarse en Z=0 y centrarse en la cama", bmn, bmx)
	}

	// Como lo lee Bambu Studio: un objeto, dos partes, extrusores 1 y 2, cada una con su color.
	res, err := bbscheck.Import(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects) != 1 || len(res.Objects[0].Volumes) != 2 {
		t.Fatalf("objetos: %+v", res.Objects)
	}
	for i, v := range res.Objects[0].Volumes {
		if v.Extruder != i+1 {
			t.Errorf("parte %d: extrusor %d", i, v.Extruder)
		}
	}
}

// El 3MF de PrusaSlicer comparte la malla entre volúmenes: cada rango de triángulos
// (volumen) del config tiene que ser también un sólido cerrado.
func TestMacetaPrusaVolumesAreClosed(t *testing.T) {
	parts := loadFixture(t, macetatest.Lazy)
	dest := filepath.Join(t.TempDir(), "maceta-prusa.3mf")
	if err := WritePrusaFile(dest, parts, WriteOptions{Title: "maceta"}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var model struct {
		Vertices []struct {
			X float64 `xml:"x,attr"`
			Y float64 `xml:"y,attr"`
			Z float64 `xml:"z,attr"`
		} `xml:"resources>object>mesh>vertices>vertex"`
		Tris []struct {
			V1 int32 `xml:"v1,attr"`
			V2 int32 `xml:"v2,attr"`
			V3 int32 `xml:"v3,attr"`
		} `xml:"resources>object>mesh>triangles>triangle"`
	}
	if err := xml.Unmarshal([]byte(ReadEntry(t, &zr.Reader, "3D/3dmodel.model")), &model); err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Volumes []struct {
			First int `xml:"firstid,attr"`
			Last  int `xml:"lastid,attr"`
		} `xml:"object>volume"`
	}
	if err := xml.Unmarshal([]byte(ReadEntry(t, &zr.Reader, "Metadata/Slic3r_PE_model.config")), &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Volumes) != 2 {
		t.Fatalf("volúmenes: %+v", cfg.Volumes)
	}
	var verts [][3]float64
	for _, v := range model.Vertices {
		verts = append(verts, [3]float64{v.X, v.Y, v.Z})
	}
	for i, v := range cfg.Volumes {
		vol := Part{Name: parts[i].Name, Vertices: verts}
		for _, tr := range model.Tris[v.First : v.Last+1] {
			vol.Triangles = append(vol.Triangles, [3]int32{tr.V1, tr.V2, tr.V3})
		}
		if c := Inspect(vol); !c.Closed() {
			t.Errorf("volumen %d del 3MF de Prusa: %+v", i, c)
		}
	}
}

// --- OpenSCAD real (ruta «OpenSCAD nativo», sin código de ScadLab de por medio) ---------

func requireOpenSCAD(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("openscad")
	if err != nil {
		t.Skip("OpenSCAD no instalado")
	}
	return bin
}

func runOpenSCAD(t *testing.T, bin string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("openscad %v: %v\n%s", args, err, out)
	}
}

// openscad -o x.3mf con color, con y sin lazy-union, contra el STL de cada color y el del
// modelo entero. Es la comprobación de que el diagnóstico no depende de nuestro código.
func TestNativeOpenSCADMacetaRoutes(t *testing.T) {
	bin := requireOpenSCAD(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "maceta.scad")
	if err := os.WriteFile(src, []byte(macetatest.Scad), 0o644); err != nil {
		t.Fatal(err)
	}
	union := filepath.Join(dir, "union.3mf")
	lazy := filepath.Join(dir, "lazy.3mf")
	runOpenSCAD(t, bin, "-o", union, src)
	runOpenSCAD(t, bin, "--enable=lazy-union", "-o", lazy, src)

	// El STL de cada color por separado es la referencia: bbox y volumen de cada parte.
	ref := map[string]mesh.Info{}
	for _, c := range []string{"verde", "naranja", ""} {
		stl := filepath.Join(dir, "ref-"+c+".stl")
		runOpenSCAD(t, bin, "-D", `solo="`+c+`"`, "--export-format", "binstl", "-o", stl, src)
		info, err := mesh.InfoFromFile(stl)
		if err != nil || !info.Watertight {
			t.Fatalf("STL de referencia %q: %+v err=%v", c, info, err)
		}
		ref[c] = info
	}

	uparts := loadParts(t, union)
	if OpenEdgeCount(uparts) == 0 {
		t.Fatal("el 3MF por defecto de OpenSCAD debería dejar las partes abiertas; si ya no pasa, esta versión de OpenSCAD cambió y el arreglo sobra")
	}
	lparts := loadParts(t, lazy)
	checks, shared := InspectAll(lparts)
	if len(lparts) != 2 || shared != 0 {
		t.Fatalf("partes %d, triángulos compartidos %d", len(lparts), shared)
	}
	for i, c := range checks {
		r := ref[[]string{"verde", "naranja"}[i]]
		if !c.Closed() || c.SignedVolume <= 0 {
			t.Errorf("%s no es un sólido cerrado: %+v", c.Name, c)
		}
		if math.Abs(c.SignedVolume-r.VolumeMM3)/r.VolumeMM3 > 1e-4 {
			t.Errorf("%s: volumen %.3f, el STL de ese color %.3f", c.Name, c.SignedVolume, r.VolumeMM3)
		}
		if !nearVec(c.Min, r.Min, 1e-3) || !nearVec(c.Max, r.Max, 1e-3) {
			t.Errorf("%s: caja %v–%v, la del STL %v–%v", c.Name, c.Min, c.Max, r.Min, r.Max)
		}
	}
	if sum := checks[0].SignedVolume + checks[1].SignedVolume; sum < ref[""].VolumeMM3 {
		t.Errorf("las partes suman %.3f y el sólido fundido %.3f", sum, ref[""].VolumeMM3)
	}
}
