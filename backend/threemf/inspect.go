package threemf

import (
	"math"
	"slices"
)

// PartCheck es el resultado de revisar una parte como la revisaría un slicer antes de
// laminarla. Una parte que Bambu Studio lamina bien es un sólido cerrado: cada arista
// dirigida (a→b) aparece en una sola cara y su opuesta (b→a) en otra, y el volumen con
// signo es positivo (las normales miran hacia afuera).
//
// Existe por un caso concreto (S17, claude/11-exportacion.md): OpenSCAD funde los color()
// del nivel superior en un solo sólido y marca cada cara con su color. Al repartir esa
// malla por color, la cara donde dos colores se tocan ya no existe en ninguna de las dos
// partes, y cada una queda como una cáscara abierta. Bambu lamina cada parte por
// separado: el contorno abierto no se cierra y rellena lo que no debe.
type PartCheck struct {
	Name      string
	Triangles int
	Min, Max  [3]float64
	// SignedVolume es el volumen con signo en mm³: positivo = normales hacia afuera.
	SignedVolume float64
	// OpenEdges son las aristas dirigidas cuya opuesta no existe: el borde de un hueco.
	OpenEdges int
	// NonManifoldEdges son las aristas dirigidas que aparecen más de una vez (más de dos
	// caras en la arista, o dos caras que la recorren en el mismo sentido).
	NonManifoldEdges int
	// Degenerate son los triángulos sin área (un vértice repetido o tres puntos en línea).
	Degenerate int
	// Duplicates son los triángulos repetidos dentro de la parte (mismos tres vértices).
	Duplicates int
}

// Closed dice si la parte es un sólido cerrado y bien orientado: sin bordes abiertos, sin
// aristas repetidas y con volumen positivo.
func (c PartCheck) Closed() bool {
	return c.Triangles > 0 && c.OpenEdges == 0 && c.NonManifoldEdges == 0 && c.SignedVolume > 0
}

// degenerateArea2 es el cuadrado del doble del área (mm⁴) bajo el cual un triángulo cuenta
// como degenerado: 1e-12 mm² de área es invisible para cualquier slicer.
const degenerateArea2 = 1e-24

// Inspect revisa la topología de una parte. Los vértices se sueldan por posición exacta
// (dos índices en el mismo punto son el mismo vértice), como hace mesh.InfoFromIndexed.
func Inspect(p Part) PartCheck {
	c := PartCheck{Name: p.Name, Triangles: len(p.Triangles)}
	if len(p.Vertices) > 0 {
		c.Min = [3]float64{math.Inf(1), math.Inf(1), math.Inf(1)}
		c.Max = [3]float64{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	}
	weld := make(map[[3]float64]int32, len(p.Vertices))
	id := make([]int32, len(p.Vertices))
	for i, v := range p.Vertices {
		w, ok := weld[v]
		if !ok {
			w = int32(len(weld))
			weld[v] = w
		}
		id[i] = w
		for k := 0; k < 3; k++ {
			c.Min[k] = math.Min(c.Min[k], v[k])
			c.Max[k] = math.Max(c.Max[k], v[k])
		}
	}

	edges := make([]uint64, 0, 3*len(p.Triangles))
	seen := make(map[[3]int32]struct{}, len(p.Triangles))
	var vol6 float64
	for _, t := range p.Triangles {
		if t[0] < 0 || t[1] < 0 || t[2] < 0 || int(t[0]) >= len(id) || int(t[1]) >= len(id) || int(t[2]) >= len(id) {
			c.Degenerate++
			continue
		}
		a, b, d := p.Vertices[t[0]], p.Vertices[t[1]], p.Vertices[t[2]]
		ia, ib, id3 := id[t[0]], id[t[1]], id[t[2]]
		cr := cross3(sub3(b, a), sub3(d, a))
		if ia == ib || ib == id3 || ia == id3 || dot3(cr, cr) < degenerateArea2 {
			c.Degenerate++
			continue
		}
		key := sortedTriple(ia, ib, id3)
		if _, dup := seen[key]; dup {
			c.Duplicates++
		}
		seen[key] = struct{}{}
		vol6 += dot3(a, cross3(b, d))
		edges = append(edges,
			uint64(ia)<<32|uint64(ib),
			uint64(ib)<<32|uint64(id3),
			uint64(id3)<<32|uint64(ia))
	}
	c.SignedVolume = vol6 / 6

	slices.Sort(edges)
	for i := 0; i < len(edges); {
		j := i
		for j < len(edges) && edges[j] == edges[i] {
			j++
		}
		if j-i > 1 {
			c.NonManifoldEdges++
		}
		rev := edges[i]<<32 | edges[i]>>32
		if _, found := slices.BinarySearch(edges, rev); !found {
			c.OpenEdges++
		}
		i = j
	}
	return c
}

// InspectAll revisa cada parte y cuenta los triángulos que dos partes distintas tienen
// idénticos (mismos tres puntos): una cara compartida entre partes es una cara que el
// slicer vería dos veces.
func InspectAll(parts []Part) (checks []PartCheck, sharedTriangles int) {
	checks = make([]PartCheck, len(parts))
	owner := map[[3][3]float64]int{}
	for i, p := range parts {
		checks[i] = Inspect(p)
		for _, t := range p.Triangles {
			if t[0] < 0 || t[1] < 0 || t[2] < 0 || int(t[0]) >= len(p.Vertices) || int(t[1]) >= len(p.Vertices) || int(t[2]) >= len(p.Vertices) {
				continue
			}
			k := sortedPoints(p.Vertices[t[0]], p.Vertices[t[1]], p.Vertices[t[2]])
			if j, ok := owner[k]; ok && j != i {
				sharedTriangles++
			}
			owner[k] = i
		}
	}
	return checks, sharedTriangles
}

// OpenEdgeCount suma los bordes abiertos y las aristas repetidas de todas las partes: 0 =
// todas son sólidos cerrados.
func OpenEdgeCount(parts []Part) int {
	n := 0
	for _, p := range parts {
		c := Inspect(p)
		n += c.OpenEdges + c.NonManifoldEdges
	}
	return n
}

func sortedTriple(a, b, c int32) [3]int32 {
	t := [3]int32{a, b, c}
	if t[0] > t[1] {
		t[0], t[1] = t[1], t[0]
	}
	if t[1] > t[2] {
		t[1], t[2] = t[2], t[1]
	}
	if t[0] > t[1] {
		t[0], t[1] = t[1], t[0]
	}
	return t
}

func sortedPoints(a, b, c [3]float64) [3][3]float64 {
	t := [3][3]float64{a, b, c}
	less := func(x, y [3]float64) bool {
		for k := 0; k < 3; k++ {
			if x[k] != y[k] {
				return x[k] < y[k]
			}
		}
		return false
	}
	if less(t[1], t[0]) {
		t[0], t[1] = t[1], t[0]
	}
	if less(t[2], t[1]) {
		t[1], t[2] = t[2], t[1]
	}
	if less(t[1], t[0]) {
		t[0], t[1] = t[1], t[0]
	}
	return t
}

func sub3(a, b [3]float64) [3]float64 { return [3]float64{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
func dot3(a, b [3]float64) float64    { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
func cross3(a, b [3]float64) [3]float64 {
	return [3]float64{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}
