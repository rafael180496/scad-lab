package imagetools

import (
	"math"
	"sort"
)

// flatten devuelve el color de cada píxel con el alfa compuesto sobre bg.
func (im *Image) flatten(bg [3]uint8) []uint8 {
	out := make([]uint8, 3*im.W*im.H)
	for i := 0; i < im.W*im.H; i++ {
		a := int(im.Pix[4*i+3])
		for c := 0; c < 3; c++ {
			out[3*i+c] = uint8((int(im.Pix[4*i+c])*a + int(bg[c])*(255-a) + 127) / 255)
		}
	}
	return out
}

// gray es la luminancia (0–255) de la imagen compuesta sobre blanco.
func (im *Image) gray() []uint8 {
	rgb := im.flatten([3]uint8{255, 255, 255})
	out := make([]uint8, im.W*im.H)
	for i := range out {
		out[i] = uint8((299*int(rgb[3*i]) + 587*int(rgb[3*i+1]) + 114*int(rgb[3*i+2]) + 500) / 1000)
	}
	return out
}

// otsu devuelve el umbral que separa mejor las dos clases de un histograma de
// 256 niveles (el que maximiza la varianza entre clases): lo de ≤ umbral es una
// clase y lo de > umbral la otra.
func otsu(hist []int) int {
	total := 0
	var sum float64
	for i, c := range hist {
		total += c
		sum += float64(i) * float64(c)
	}
	if total == 0 {
		return 0
	}
	var wB, sumB, best float64
	thr := 0
	for t := 0; t < len(hist); t++ {
		wB += float64(hist[t])
		if wB == 0 {
			continue
		}
		wF := float64(total) - wB
		if wF == 0 {
			break
		}
		sumB += float64(t) * float64(hist[t])
		mB := sumB / wB
		mF := (sum - sumB) / wF
		v := wB * wF * (mB - mF) * (mB - mF)
		if v > best {
			best, thr = v, t
		}
	}
	return thr
}

// Mask es una imagen binaria.
type Mask struct {
	W, H int
	Bits []bool
}

func newMask(w, h int) *Mask { return &Mask{W: w, H: h, Bits: make([]bool, w*h)} }

func (m *Mask) at(x, y int) bool {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return false
	}
	return m.Bits[y*m.W+x]
}

// Count son los píxeles encendidos.
func (m *Mask) Count() int {
	n := 0
	for _, b := range m.Bits {
		if b {
			n++
		}
	}
	return n
}

// Box es un rectángulo en píxeles; Valid dice si hay algo.
type Box struct {
	X0, Y0, X1, Y1 int // X1/Y1 exclusivos
	Valid          bool
}

func (b Box) W() int { return b.X1 - b.X0 }
func (b Box) H() int { return b.Y1 - b.Y0 }

// BBox es la caja que envuelve los píxeles encendidos.
func (m *Mask) BBox() Box {
	b := Box{X0: m.W, Y0: m.H, X1: 0, Y1: 0}
	for y := 0; y < m.H; y++ {
		for x := 0; x < m.W; x++ {
			if m.Bits[y*m.W+x] {
				if x < b.X0 {
					b.X0 = x
				}
				if x+1 > b.X1 {
					b.X1 = x + 1
				}
				if y < b.Y0 {
					b.Y0 = y
				}
				if y+1 > b.Y1 {
					b.Y1 = y + 1
				}
				b.Valid = true
			}
		}
	}
	return b
}

// dilate agranda lo encendido con un cuadrado de radio r.
func dilate(m *Mask, r int) *Mask { return window(m, r, true) }

// erode lo achica.
func erode(m *Mask, r int) *Mask { return window(m, r, false) }

// window aplica el máximo (any=true) o el mínimo de una ventana cuadrada de radio
// r, separable: primero en horizontal y luego en vertical. Solo cuentan los
// píxeles dentro de la imagen: una forma que toca el borde no se come una franja
// al erosionar ni «crece» hacia fuera al dilatar.
func window(m *Mask, r int, any bool) *Mask {
	if r <= 0 {
		return m
	}
	tmp := newMask(m.W, m.H)
	for y := 0; y < m.H; y++ {
		row := m.Bits[y*m.W : (y+1)*m.W]
		// Suma acumulada de la fila.
		pre := make([]int, m.W+1)
		for x := 0; x < m.W; x++ {
			pre[x+1] = pre[x]
			if row[x] {
				pre[x+1]++
			}
		}
		for x := 0; x < m.W; x++ {
			lo, hi := max(0, x-r), min(m.W, x+r+1)
			c := pre[hi] - pre[lo]
			if any {
				tmp.Bits[y*m.W+x] = c > 0
			} else {
				tmp.Bits[y*m.W+x] = c == hi-lo
			}
		}
	}
	out := newMask(m.W, m.H)
	pre := make([]int, m.H+1)
	for x := 0; x < m.W; x++ {
		for y := 0; y < m.H; y++ {
			pre[y+1] = pre[y]
			if tmp.Bits[y*m.W+x] {
				pre[y+1]++
			}
		}
		for y := 0; y < m.H; y++ {
			lo, hi := max(0, y-r), min(m.H, y+r+1)
			c := pre[hi] - pre[lo]
			if any {
				out.Bits[y*m.W+x] = c > 0
			} else {
				out.Bits[y*m.W+x] = c == hi-lo
			}
		}
	}
	return out
}

// open quita lo más fino que 2r+1 píxeles; closeMask rellena los huecos más
// finos que eso.
func open(m *Mask, r int) *Mask      { return dilate(erode(m, r), r) }
func closeMask(m *Mask, r int) *Mask { return erode(dilate(m, r), r) }

// component es un conjunto conexo de píxeles del mismo valor.
type component struct {
	Pixels  []int32
	Touches bool // toca el borde de la imagen
}

// components etiqueta los conjuntos conexos de los píxeles que valen val. Los de
// primer plano se unen en 8 direcciones y los de fondo en 4 (la convención que
// evita que un hueco y la forma que lo rodea queden conectados por una
// diagonal).
func components(m *Mask, val bool) []component {
	conn8 := val
	seen := make([]bool, m.W*m.H)
	var out []component
	stack := make([]int32, 0, 1024)
	for start := 0; start < m.W*m.H; start++ {
		if seen[start] || m.Bits[start] != val {
			continue
		}
		var comp component
		stack = append(stack[:0], int32(start))
		seen[start] = true
		for len(stack) > 0 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			comp.Pixels = append(comp.Pixels, p)
			x, y := int(p)%m.W, int(p)/m.W
			if x == 0 || y == 0 || x == m.W-1 || y == m.H-1 {
				comp.Touches = true
			}
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if (dx == 0 && dy == 0) || (!conn8 && dx != 0 && dy != 0) {
						continue
					}
					nx, ny := x+dx, y+dy
					if nx < 0 || ny < 0 || nx >= m.W || ny >= m.H {
						continue
					}
					q := ny*m.W + nx
					if !seen[q] && m.Bits[q] == val {
						seen[q] = true
						stack = append(stack, int32(q))
					}
				}
			}
		}
		out = append(out, comp)
	}
	return out
}

// dropSpecks apaga los conjuntos de primer plano de menos de minArea píxeles y
// enciende los huecos (fondo rodeado por la forma) de menos de minArea píxeles.
// El fondo que toca el borde nunca es un hueco.
func dropSpecks(m *Mask, minArea int) *Mask {
	if minArea <= 1 {
		return m
	}
	out := &Mask{W: m.W, H: m.H, Bits: append([]bool(nil), m.Bits...)}
	for _, c := range components(m, true) {
		if len(c.Pixels) < minArea {
			for _, p := range c.Pixels {
				out.Bits[p] = false
			}
		}
	}
	for _, c := range components(out, false) {
		if !c.Touches && len(c.Pixels) < minArea {
			for _, p := range c.Pixels {
				out.Bits[p] = true
			}
		}
	}
	return out
}

// keepMain deja los conjuntos de primer plano de al menos frac del más grande:
// el polvo de una foto no es parte del objeto.
func keepMain(m *Mask, frac float64) *Mask {
	comps := components(m, true)
	largest := 0
	for _, c := range comps {
		largest = max(largest, len(c.Pixels))
	}
	out := newMask(m.W, m.H)
	for _, c := range comps {
		if float64(len(c.Pixels)) >= frac*float64(largest) {
			for _, p := range c.Pixels {
				out.Bits[p] = true
			}
		}
	}
	return out
}

// blurField suaviza una máscara con el núcleo binomial [1 2 1]/4 en las dos
// direcciones y la devuelve como campo escalar de (w+2)×(h+2) con un margen de
// un píxel apagado, para que el contorno de una forma que toca el borde cierre.
func blurField(m *Mask) ([]float32, int, int) {
	W, H := m.W+2, m.H+2
	src := make([]float32, W*H)
	for y := 0; y < m.H; y++ {
		for x := 0; x < m.W; x++ {
			if m.Bits[y*m.W+x] {
				src[(y+1)*W+x+1] = 1
			}
		}
	}
	k := [3]float32{1, 2, 1}
	at := func(buf []float32, x, y int) float32 {
		if x < 0 || y < 0 || x >= W || y >= H {
			return 0
		}
		return buf[y*W+x]
	}
	tmp := make([]float32, W*H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			var s float32
			for i := -1; i <= 1; i++ {
				s += k[i+1] * at(src, x+i, y)
			}
			tmp[y*W+x] = s / 4
		}
	}
	out := make([]float32, W*H)
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			var s float32
			for i := -1; i <= 1; i++ {
				s += k[i+1] * at(tmp, x, y+i)
			}
			out[y*W+x] = s / 4
		}
	}
	return out, W, H
}

// percentile devuelve el valor del cuantil q (0–1) de una copia ordenada.
func percentile(vals []float64, q float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	i := int(math.Round(q * float64(len(s)-1)))
	return s[max(0, min(len(s)-1, i))]
}
