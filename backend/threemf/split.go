package threemf

import (
	"math"
	"sort"
	"strconv"
	"strings"
)

// defaultMaterialName es el nombre que OpenSCAD le da al color de lo que no
// pasó por color() (amarillo «cornfield»).
const defaultMaterialName = "Default"

// Part es la geometría de un color: lo que Bambu Studio carga como una parte
// del objeto, con su extrusor. Los vértices están compactados (solo los que
// usa esta parte), así que cada parte es una malla independiente.
type Part struct {
	Name      string
	Color     RGB
	Uncolored bool // geometría que no pasó por color(): lleva el color por defecto de OpenSCAD
	Extruder  int  // 1..N, en el orden en que se declararon los colores
	Vertices  [][3]float64
	Triangles [][3]int32
}

type group struct {
	color     RGB
	uncolored bool
	order     int // clave de orden: Color N de OpenSCAD, o su posición en basematerials
	first     int // primer índice de material, para desempatar
	tris      []int32
}

// Split agrupa los triángulos por color (RGB; el alfa no cuenta: dos colores
// que difieren solo en la transparencia son el mismo filamento) y devuelve una
// Part por color, ya numeradas con su extrusor.
//
// El orden es el de declaración de los color() en el .scad. OpenSCAD los
// nombra «Color 1», «Color 2»… en ese orden, pero escribe <basematerials> en
// otro (en un modelo con tres colores los listó 3, 2, 1), así que se usa el
// número del nombre y no la posición. La geometría sin color queda última: es
// un olvido del modelo, no un filamento que el usuario haya elegido.
func Split(m *Mesh) []Part {
	groups := map[RGB]*group{}
	// Todo lo que no es un color elegido (material «Default» de OpenSCAD o
	// triángulo sin material) va a un grupo aparte, aunque su RGB coincida con
	// el de un color() explícito: son decisiones distintas.
	def := &group{color: defaultColor, uncolored: true, order: math.MaxInt32, first: math.MaxInt32}
	byMat := make([]*group, len(m.Materials))
	for i, mat := range m.Materials {
		if mat.Name == defaultMaterialName {
			byMat[i] = def
			def.color = mat.Color
			continue
		}
		g := groups[mat.Color]
		if g == nil {
			g = &group{color: mat.Color, order: materialOrder(mat, i), first: i}
			groups[mat.Color] = g
		} else if o := materialOrder(mat, i); o < g.order {
			g.order = o
		}
		byMat[i] = g
	}
	for i, t := range m.Triangles {
		g := def
		if t.Mat >= 0 && int(t.Mat) < len(byMat) {
			g = byMat[t.Mat]
		}
		g.tris = append(g.tris, int32(i))
	}
	list := make([]*group, 0, len(groups)+1)
	for _, g := range groups {
		if len(g.tris) > 0 {
			list = append(list, g)
		}
	}
	if len(def.tris) > 0 {
		list = append(list, def)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].order != list[j].order {
			return list[i].order < list[j].order
		}
		return list[i].first < list[j].first
	})

	parts := make([]Part, len(list))
	for i, g := range list {
		p := Part{Color: g.color, Uncolored: g.uncolored, Extruder: i + 1}
		p.Name = partName(p)
		remap := map[int32]int32{}
		p.Triangles = make([][3]int32, 0, len(g.tris))
		for _, ti := range g.tris {
			t := m.Triangles[ti]
			var nt [3]int32
			for k, v := range t.V {
				nv, ok := remap[v]
				if !ok {
					nv = int32(len(p.Vertices))
					p.Vertices = append(p.Vertices, m.Vertices[v])
					remap[v] = nv
				}
				nt[k] = nv
			}
			p.Triangles = append(p.Triangles, nt)
		}
		parts[i] = p
	}
	return parts
}

// defaultColor es #F9D72C, el «cornfield» con que OpenSCAD pinta lo que no
// tiene color().
var defaultColor = RGB{0xF9, 0xD7, 0x2C}

// materialOrder: «Color N» → N; cualquier otro nombre → 10000 + posición.
func materialOrder(m Material, idx int) int {
	if n, ok := strings.CutPrefix(m.Name, "Color "); ok {
		if v, err := strconv.Atoi(n); err == nil && v >= 0 {
			return v
		}
	}
	return 10000 + idx
}

func partName(p Part) string {
	if p.Uncolored {
		return defaultMaterialName
	}
	return "Color " + strconv.Itoa(p.Extruder) + " (" + p.Color.Hex() + ")"
}

// NearColors lista los pares de colores casi indistinguibles (distancia RGB ≤
// threshold sobre 441): suelen ser un solo color escrito dos veces, y cada uno
// ocuparía un filamento del AMS.
func NearColors(parts []Part, threshold float64) [][2]RGB {
	var out [][2]RGB
	for i := range parts {
		for j := i + 1; j < len(parts); j++ {
			if parts[i].Uncolored || parts[j].Uncolored {
				continue
			}
			a, b := parts[i].Color, parts[j].Color
			var d float64
			for k := 0; k < 3; k++ {
				x := float64(a[k]) - float64(b[k])
				d += x * x
			}
			if math.Sqrt(d) <= threshold {
				out = append(out, [2]RGB{a, b})
			}
		}
	}
	return out
}
