package imagetools

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	"scadlab/backend/i18n"
)

// Valores por omisión del trazado.
const (
	DefaultSimplifyMM = 0.1
	DefaultMinAreaMM2 = 0.25
	MaxLayers         = 6
)

// TraceOptions son los argumentos de Trace. La escala se da con UNO de WidthMM,
// HeightMM o MMPerPx (si vienen varios gana MMPerPx, luego WidthMM).
//
// WidthMM y HeightMM son el ancho y el alto de la FORMA trazada (la caja que
// envuelve todos sus contornos), no los de la foto: así «el logo mide 80 mm de
// ancho» funciona aunque la foto tenga márgenes. MMPerPx, en cambio, es la
// escala de la foto (milímetros por píxel del archivo original), para cuando la
// escala sale de un objeto conocido que aparece en ella.
type TraceOptions struct {
	WidthMM, HeightMM, MMPerPx float64
	// Threshold (1–255) de la escala de grises; 0 o menos = Otsu (un umbral de 0
	// no dejaría pasar nada).
	Threshold int
	// Invert traza lo contrario de lo que se eligió como forma.
	Invert bool
	// Layers: 1 (por defecto) = una sola silueta por umbral; 2–6 = separa por
	// color con k-medias y traza cada color por separado.
	Layers int
	// SimplifyMM es la tolerancia de Douglas-Peucker; 0 = DefaultSimplifyMM.
	SimplifyMM float64
	// MinAreaMM2 descarta motas y rellena huecos más chicos; 0 = DefaultMinAreaMM2.
	MinAreaMM2 float64
}

// Contour es un contorno cerrado en milímetros, con Y hacia arriba. Un hueco
// (Hole) es un contorno interior: el relleno es par-impar.
type Contour struct {
	Points  [][2]float64 `json:"-"`
	Hole    bool         `json:"hole"`
	AreaMM2 float64      `json:"areaMm2"` // siempre positiva
}

// Layer es una silueta (o un color) trazada.
type Layer struct {
	Index    int       // desde 1
	Color    string    // «#RRGGBB» del color de la capa; vacío con una sola
	Contours []Contour // exteriores y huecos
	AreaMM2  float64   // área neta: exteriores menos huecos
	Percent  float64   // cuánto del primer plano son sus píxeles (capas por color)
}

// TraceResult es lo que devuelve Trace.
type TraceResult struct {
	Layers []Layer
	// WidthMM y HeightMM son la caja de toda la forma (todas las capas).
	WidthMM, HeightMM float64
	// MMPerPx es la escala aplicada, en milímetros por píxel del archivo original.
	MMPerPx float64
	// Threshold es el nivel de gris usado (-1 cuando no se usó: capas por color o
	// imagen transparente).
	Threshold int
	// ShapeIs dice qué se trazó: «dark», «light» o «opaque» (transparencia).
	ShapeIs string
	// Inverted: se trazó lo contrario de lo habitual.
	Inverted bool
	// Notes son avisos para quien lee el resultado.
	Notes []string

	imgW, imgH int
}

// Trace traza el contorno de la forma de una imagen y lo escala a milímetros:
//
//  1. escala de grises y umbral de Otsu (o el dado);
//  2. la forma es la clase contraria a la del borde de la foto (Invert la cambia);
//  3. apertura y cierre morfológicos y descarte de motas;
//  4. marching squares con huecos (campo suavizado, contornos sub-píxel);
//  5. Douglas-Peucker;
//  6. píxeles a milímetros con Y hacia arriba.
func Trace(ctx context.Context, im *Image, o TraceOptions) (*TraceResult, error) {
	if o.MMPerPx <= 0 && o.WidthMM <= 0 && o.HeightMM <= 0 {
		return nil, i18n.New(i18n.Msg{
			ES: "falta la escala: indica `width_mm`, `height_mm` o `mm_per_px`",
			EN: "the scale is missing: give `width_mm`, `height_mm` or `mm_per_px`",
		})
	}
	for _, v := range []float64{o.WidthMM, o.HeightMM, o.MMPerPx} {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) || v > 100000 {
			return nil, i18n.New(i18n.Msg{ES: "la escala tiene que ser un número positivo razonable", EN: "the scale must be a reasonable positive number"})
		}
	}
	if o.Layers == 0 {
		o.Layers = 1
	}
	if o.Layers < 1 || o.Layers > MaxLayers {
		return nil, i18n.Errorf(i18n.Msg{ES: "`layers` va de 1 a %d", EN: "`layers` goes from 1 to %d"}, MaxLayers)
	}
	if o.SimplifyMM <= 0 {
		o.SimplifyMM = DefaultSimplifyMM
	}
	if o.MinAreaMM2 <= 0 {
		o.MinAreaMM2 = DefaultMinAreaMM2
	}
	if o.Threshold > 255 {
		return nil, i18n.New(i18n.Msg{ES: "`threshold` va de 0 a 255", EN: "`threshold` goes from 0 to 255"})
	}

	res := &TraceResult{Threshold: -1, imgW: im.W, imgH: im.H}
	var masks []*Mask
	var colors []string
	var percents []float64
	if o.Layers == 1 {
		m, thr, shape := shapeMask(im, o)
		res.Threshold, res.ShapeIs, res.Inverted = thr, shape, o.Invert
		masks, colors, percents = []*Mask{m}, []string{""}, []float64{100}
	} else {
		var err error
		masks, colors, percents, err = colorLayers(ctx, im, o.Layers)
		if err != nil {
			return nil, err
		}
		if o.Threshold > 0 || o.Invert {
			res.Notes = append(res.Notes, i18n.T(i18n.Msg{
				ES: "`threshold` e `invert` no se usan con capas por color.",
				EN: "`threshold` and `invert` are not used with color layers.",
			}))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Limpieza y contornos en píxeles (y hacia abajo, coordenadas continuas).
	r := max(0, min(2, max(im.W, im.H)/700))
	minPx := max(6, im.W*im.H/20000)
	type loop struct {
		pts  []pt
		area float64 // con signo, en px²: exteriores > 0, huecos < 0
	}
	layerLoops := make([][]loop, len(masks))
	for i, m := range masks {
		m = dropSpecks(closeMask(open(m, r), r), minPx)
		field, fw, fh := blurField(m)
		for _, l := range marchingSquares(ctx, field, fw, fh) {
			// Del retículo del campo (con margen) a píxeles continuos.
			for k := range l {
				l[k].X -= 0.5
				l[k].Y -= 0.5
			}
			layerLoops[i] = append(layerLoops[i], loop{pts: l})
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	// Escala. Con width/height la escala depende de la caja de la forma, y la
	// caja depende de qué contornos sobreviven al filtro de área (que depende de
	// la escala): se itera hasta que se estabiliza (la caja solo puede achicarse).
	px := func(mm float64, s float64) float64 { return mm / s }
	scale := 0.0
	var minX, minY, maxX, maxY float64
	keep := make([][]bool, len(layerLoops))
	for i := range layerLoops {
		keep[i] = make([]bool, len(layerLoops[i]))
		for k := range keep[i] {
			keep[i][k] = true
		}
	}
	bbox := func() bool {
		minX, minY, maxX, maxY = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		any := false
		for i := range layerLoops {
			for k, l := range layerLoops[i] {
				if !keep[i][k] {
					continue
				}
				for _, p := range l.pts {
					minX, minY, maxX, maxY = math.Min(minX, p.X), math.Min(minY, p.Y), math.Max(maxX, p.X), math.Max(maxY, p.Y)
					any = true
				}
			}
		}
		return any
	}
	if !bbox() {
		return nil, i18n.New(i18n.Msg{
			ES: "no se encontró ninguna forma que trazar: prueba con `invert`, otro `threshold` o `layers`",
			EN: "no shape was found to trace: try `invert`, another `threshold` or `layers`",
		})
	}
	setScale := func() {
		switch {
		case o.MMPerPx > 0:
			scale = o.MMPerPx * im.ScalePx()
		case o.WidthMM > 0:
			scale = o.WidthMM / math.Max(maxX-minX, 1e-9)
		default:
			scale = o.HeightMM / math.Max(maxY-minY, 1e-9)
		}
	}
	// Se simplifica con la escala de la caja sin simplificar y después se
	// recalcula con la caja de lo ya simplificado, así `width_mm` sale exacto (la
	// simplificación puede comerse un poco las puntas de un círculo).
	setScale()
	for i := range layerLoops {
		for k := range layerLoops[i] {
			l := &layerLoops[i][k]
			l.pts = simplifyClosed(l.pts, px(o.SimplifyMM, scale))
			l.area = signedArea(l.pts)
		}
	}
	bbox()
	setScale()
	// El filtro de área depende de la escala y la escala de la caja que sobrevive
	// al filtro: se itera hasta que se estabiliza (la caja solo puede achicarse).
	for iter := 0; iter < 4; iter++ {
		removed := false
		for i := range layerLoops {
			for k := range layerLoops[i] {
				if keep[i][k] && math.Abs(layerLoops[i][k].area)*scale*scale < o.MinAreaMM2 {
					keep[i][k] = false
					removed = true
				}
			}
		}
		if !removed || !bbox() {
			break
		}
		setScale()
	}
	if !bbox() {
		return nil, i18n.New(i18n.Msg{
			ES: "todas las formas son más chicas que `min_area_mm2`: baja ese valor o revisa la escala",
			EN: "every shape is smaller than `min_area_mm2`: lower it or check the scale",
		})
	}
	setScale()
	res.MMPerPx = scale / im.ScalePx()
	res.WidthMM, res.HeightMM = round3((maxX-minX)*scale), round3((maxY-minY)*scale)

	// A milímetros: el origen es la esquina inferior izquierda de la caja.
	for i := range layerLoops {
		layer := Layer{Index: i + 1, Color: colors[i], Percent: percents[i]}
		for k, l := range layerLoops[i] {
			if !keep[i][k] || len(l.pts) < 3 {
				continue
			}
			c := Contour{Hole: l.area < 0, AreaMM2: round3(math.Abs(l.area) * scale * scale)}
			c.Points = make([][2]float64, len(l.pts))
			for j, p := range l.pts {
				c.Points[j] = [2]float64{round3((p.X - minX) * scale), round3((maxY - p.Y) * scale)}
			}
			layer.Contours = append(layer.Contours, c)
			if c.Hole {
				layer.AreaMM2 -= c.AreaMM2
			} else {
				layer.AreaMM2 += c.AreaMM2
			}
		}
		layer.AreaMM2 = round3(layer.AreaMM2)
		if len(layer.Contours) == 0 {
			continue // un color que no deja forma (borde suavizado, polvo)
		}
		layer.Index = len(res.Layers) + 1
		res.Layers = append(res.Layers, layer)
	}
	if o.Layers > 1 && len(res.Layers) < o.Layers {
		res.Notes = append(res.Notes, i18n.T(i18n.Msg{
			ES: "Se pidieron %d capas y quedaron %d con forma: la imagen no tiene tantos colores distintos.",
			EN: "%d layers were requested and %d have a shape: the image does not have that many distinct colors.",
		}, o.Layers, len(res.Layers)))
	}
	return res, nil
}

// shapeMask elige qué píxeles son la forma: el umbral separa claros de oscuros y
// la forma es la clase contraria a la del borde de la foto (un logo negro sobre
// blanco y uno blanco sobre negro dan lo mismo). Con transparencia, es lo opaco.
func shapeMask(im *Image, o TraceOptions) (*Mask, int, string) {
	m := newMask(im.W, im.H)
	if im.HasAlpha && o.Threshold <= 0 {
		for i := 0; i < im.W*im.H; i++ {
			m.Bits[i] = (im.Pix[4*i+3] >= 128) != o.Invert
		}
		return m, -1, "opaque"
	}
	g := im.gray()
	thr := o.Threshold
	if thr <= 0 {
		hist := make([]int, 256)
		for _, v := range g {
			hist[v]++
		}
		thr = otsu(hist)
	}
	// ¿El borde es oscuro o claro?
	t := max(1, min(im.W, im.H)/100)
	dark, n := 0, 0
	for y := 0; y < im.H; y++ {
		for x := 0; x < im.W; x++ {
			if x < t || y < t || x >= im.W-t || y >= im.H-t {
				n++
				if int(g[y*im.W+x]) <= thr {
					dark++
				}
			}
		}
	}
	bgDark := dark*2 > n
	shapeDark := !bgDark != o.Invert // la forma es la clase contraria al borde
	for i, v := range g {
		m.Bits[i] = (int(v) <= thr) == shapeDark
	}
	if shapeDark {
		return m, thr, "dark"
	}
	return m, thr, "light"
}

// colorLayers reparte el primer plano en hasta k colores con k-medias en RGB
// (semilla fija: dos corridas dan lo mismo) y devuelve una máscara por color, de
// la capa con más píxeles a la de menos.
func colorLayers(ctx context.Context, im *Image, k int) ([]*Mask, []string, []float64, error) {
	fg, err := im.Foreground(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	rgb := im.flatten([3]uint8{255, 255, 255})
	var idx []int
	for i, b := range fg.Mask.Bits {
		if b {
			idx = append(idx, i)
		}
	}
	if len(idx) == 0 {
		return nil, nil, nil, i18n.New(i18n.Msg{
			ES: "no se encontró primer plano que separar por colores",
			EN: "no foreground to split by color was found",
		})
	}
	// Muestra de hasta 50 000 píxeles, a paso fijo.
	step := max(1, len(idx)/50000)
	var sample [][3]float64
	for j := 0; j < len(idx); j += step {
		i := idx[j]
		sample = append(sample, [3]float64{float64(rgb[3*i]), float64(rgb[3*i+1]), float64(rgb[3*i+2])})
	}
	centers := kmeans(sample, min(k, len(sample)), 1, 25)
	labels := make([]int, im.W*im.H)
	counts := make([]int, len(centers))
	for i := range labels {
		labels[i] = -1
	}
	for _, i := range idx {
		p := [3]float64{float64(rgb[3*i]), float64(rgb[3*i+1]), float64(rgb[3*i+2])}
		best, bd := 0, math.MaxFloat64
		for c, ce := range centers {
			if d := dist3(p, ce); d < bd {
				best, bd = c, d
			}
		}
		labels[i] = best
		counts[best]++
	}
	order := make([]int, 0, len(centers))
	for c := range centers {
		if counts[c] > 0 {
			order = append(order, c)
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return counts[order[a]] > counts[order[b]] })
	var masks []*Mask
	var cols []string
	var pcts []float64
	for _, c := range order {
		m := newMask(im.W, im.H)
		for i, l := range labels {
			m.Bits[i] = l == c
		}
		masks = append(masks, m)
		cols = append(cols, hex([3]uint8{uint8(centers[c][0] + 0.5), uint8(centers[c][1] + 0.5), uint8(centers[c][2] + 0.5)}))
		pcts = append(pcts, math.Round(1000*float64(counts[c])/float64(len(idx)))/10)
	}
	return masks, cols, pcts, nil
}

// kmeans agrupa puntos RGB. Inicialización k-means++ con una semilla fija.
func kmeans(pts [][3]float64, k int, seed int64, iters int) [][3]float64 {
	if k < 1 || len(pts) == 0 {
		return nil
	}
	rng := rand.New(rand.NewSource(seed))
	centers := [][3]float64{pts[rng.Intn(len(pts))]}
	d2 := make([]float64, len(pts))
	for len(centers) < k {
		var sum float64
		for i, p := range pts {
			best := math.MaxFloat64
			for _, c := range centers {
				if d := dist3(p, c); d*d < best {
					best = d * d
				}
			}
			d2[i] = best
			sum += best
		}
		if sum == 0 {
			break // todos los puntos son un color ya elegido
		}
		r := rng.Float64() * sum
		pick := len(pts) - 1
		for i, d := range d2 {
			r -= d
			if r <= 0 {
				pick = i
				break
			}
		}
		centers = append(centers, pts[pick])
	}
	assign := make([]int, len(pts))
	for it := 0; it < iters; it++ {
		moved := false
		sums := make([][3]float64, len(centers))
		cnt := make([]int, len(centers))
		for i, p := range pts {
			best, bd := 0, math.MaxFloat64
			for c, ce := range centers {
				if d := dist3(p, ce); d < bd {
					best, bd = c, d
				}
			}
			if assign[i] != best || it == 0 {
				moved = true
			}
			assign[i] = best
			for ch := 0; ch < 3; ch++ {
				sums[best][ch] += p[ch]
			}
			cnt[best]++
		}
		for c := range centers {
			if cnt[c] > 0 {
				for ch := 0; ch < 3; ch++ {
					centers[c][ch] = sums[c][ch] / float64(cnt[c])
				}
			}
		}
		if !moved {
			break
		}
	}
	return centers
}

// --- marching squares ---------------------------------------------------------

type pt struct{ X, Y float64 }

// marchingSquares recorre el campo escalar (w×h) y devuelve los contornos
// cerrados del nivel 0.5 en coordenadas del retículo, con interpolación lineal.
// Cada segmento se orienta con lo de dentro a la izquierda (en el sentido
// aritmético de los ejes del retículo): los contornos exteriores salen con área
// positiva y los huecos con área negativa. El campo tiene que traer un margen
// apagado, para que todo contorno cierre.
func marchingSquares(ctx context.Context, f []float32, w, h int) [][]pt {
	// Una arista se identifica por (x, y, dirección): 0 = horizontal de (x,y) a
	// (x+1,y); 1 = vertical de (x,y) a (x,y+1).
	edgeID := func(x, y, dir int) int32 { return int32((y*w+x)*2 + dir) }
	val := func(x, y int) float64 { return float64(f[y*w+x]) }
	pos := func(id int32) pt {
		dir := int(id & 1)
		c := int(id >> 1)
		x, y := c%w, c/w
		if dir == 0 {
			fa, fb := val(x, y), val(x+1, y)
			return pt{float64(x) + (0.5-fa)/(fb-fa), float64(y)}
		}
		fa, fb := val(x, y), val(x, y+1)
		return pt{float64(x), float64(y) + (0.5-fa)/(fb-fa)}
	}
	next := map[int32]int32{}
	var order []int32
	// La orientación se decide con los PUNTOS MEDIOS de las aristas, no con la
	// posición interpolada: si un valor del campo vale justo 0.5, dos puntos
	// pueden coincidir con un vértice y el producto cruz daría 0.
	mid := func(id int32) pt {
		c := int(id >> 1)
		x, y := float64(c%w), float64(c/w)
		if id&1 == 0 {
			return pt{x + 0.5, y}
		}
		return pt{x, y + 0.5}
	}
	add := func(a, b int32, inside pt) {
		pa, pb := mid(a), mid(b)
		cross := (pb.X-pa.X)*(inside.Y-pa.Y) - (pb.Y-pa.Y)*(inside.X-pa.X)
		if cross < 0 {
			a, b = b, a
		}
		next[a] = b
		order = append(order, a)
	}
	for y := 0; y+1 < h; y++ {
		if y&63 == 0 && ctx.Err() != nil {
			return nil // el que llama mira ctx.Err()
		}
		for x := 0; x+1 < w; x++ {
			v := [4]float64{val(x, y), val(x+1, y), val(x+1, y+1), val(x, y+1)}
			var in [4]bool
			n := 0
			for k, c := range v {
				in[k] = c >= 0.5
				if in[k] {
					n++
				}
			}
			if n == 0 || n == 4 {
				continue
			}
			// Aristas de la celda, entre el vértice k y el k+1.
			e := [4]int32{edgeID(x, y, 0), edgeID(x+1, y, 1), edgeID(x, y+1, 0), edgeID(x, y, 1)}
			corner := [4]pt{{float64(x), float64(y)}, {float64(x + 1), float64(y)}, {float64(x + 1), float64(y + 1)}, {float64(x), float64(y + 1)}}
			prev := func(k int) int { return (k + 3) % 4 }
			switch n {
			case 1:
				for k := 0; k < 4; k++ {
					if in[k] {
						add(e[prev(k)], e[k], corner[k])
					}
				}
			case 3:
				for k := 0; k < 4; k++ {
					if !in[k] {
						add(e[prev(k)], e[k], corner[(k+2)%4])
					}
				}
			case 2:
				if in[0] == in[2] { // diagonal: caso ambiguo
					centerIn := (v[0]+v[1]+v[2]+v[3])/4 >= 0.5
					for k := 0; k < 4; k++ {
						if centerIn && !in[k] {
							// el centro está dentro: los dos dentro se unen y cada
							// esquina de fuera queda aislada.
							add(e[prev(k)], e[k], corner[(k+1)%4])
						} else if !centerIn && in[k] {
							add(e[prev(k)], e[k], corner[k])
						}
					}
				} else {
					// Dos vértices contiguos dentro: k y k+1.
					for k := 0; k < 4; k++ {
						if in[k] && in[(k+1)%4] {
							add(e[prev(k)], e[(k+1)%4], corner[k])
						}
					}
				}
			}
		}
	}
	var loops [][]pt
	seen := map[int32]bool{}
	for _, start := range order {
		if seen[start] {
			continue
		}
		var l []pt
		cur := start
		for {
			seen[cur] = true
			l = append(l, pos(cur))
			nx, ok := next[cur]
			if !ok {
				l = nil // contorno abierto (no debería ocurrir con el margen)
				break
			}
			cur = nx
			if cur == start {
				break
			}
			if seen[cur] {
				l = nil
				break
			}
		}
		if len(l) >= 3 {
			loops = append(loops, l)
		}
	}
	return loops
}

// signedArea es el área con signo (fórmula del cordón de zapato).
func signedArea(p []pt) float64 {
	var a float64
	for i := range p {
		j := (i + 1) % len(p)
		a += p[i].X*p[j].Y - p[j].X*p[i].Y
	}
	return a / 2
}

// --- Douglas-Peucker ---------------------------------------------------------

// simplifyClosed simplifica un contorno cerrado con Douglas-Peucker: lo parte en
// dos mitades por el punto más lejano del primero y simplifica cada una.
func simplifyClosed(p []pt, tol float64) []pt {
	if len(p) <= 4 || tol <= 0 {
		return p
	}
	far, fd := 0, -1.0
	for i, q := range p {
		if d := math.Hypot(q.X-p[0].X, q.Y-p[0].Y); d > fd {
			far, fd = i, d
		}
	}
	if far == 0 {
		return p
	}
	a := dp(p[:far+1], tol)
	second := append(append([]pt(nil), p[far:]...), p[0])
	b := dp(second, tol)
	out := append(a[:len(a):len(a)], b[1:len(b)-1]...)
	return out
}

// dp es Douglas-Peucker sobre una polilínea abierta (sin recursión).
func dp(p []pt, tol float64) []pt {
	n := len(p)
	if n <= 2 {
		return append([]pt(nil), p...)
	}
	keep := make([]bool, n)
	keep[0], keep[n-1] = true, true
	type span struct{ a, b int }
	stack := []span{{0, n - 1}}
	for len(stack) > 0 {
		s := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		idx, md := -1, tol
		for i := s.a + 1; i < s.b; i++ {
			if d := segDist(p[i], p[s.a], p[s.b]); d > md {
				idx, md = i, d
			}
		}
		if idx >= 0 {
			keep[idx] = true
			stack = append(stack, span{s.a, idx}, span{idx, s.b})
		}
	}
	var out []pt
	for i, k := range keep {
		if k {
			out = append(out, p[i])
		}
	}
	return out
}

// segDist es la distancia de q al segmento a-b.
func segDist(q, a, b pt) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(q.X-a.X, q.Y-a.Y)
	}
	t := math.Max(0, math.Min(1, ((q.X-a.X)*dx+(q.Y-a.Y)*dy)/l2))
	return math.Hypot(q.X-(a.X+t*dx), q.Y-(a.Y+t*dy))
}

// --- salida --------------------------------------------------------------------

func num(v float64) string {
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "" || s == "-0" {
		return "0"
	}
	return s
}

// SVG escribe una capa como un SVG en milímetros (el tamaño de la caja de la
// forma, con el origen arriba a la izquierda como pide SVG). Todos los
// contornos van en un solo <path> con fill-rule="evenodd": los huecos se hacen
// solos. OpenSCAD voltea la Y al importarlo, así que `import("x.svg")` queda con
// la forma derecha y su esquina inferior izquierda en el origen.
func (r *TraceResult) SVG(layer int) []byte {
	if layer < 0 || layer >= len(r.Layers) {
		return nil
	}
	l := r.Layers[layer]
	fill := l.Color
	if fill == "" {
		fill = "#000000"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	fmt.Fprintf(&b, "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"%smm\" height=\"%smm\" viewBox=\"0 0 %s %s\">\n",
		num(r.WidthMM), num(r.HeightMM), num(r.WidthMM), num(r.HeightMM))
	b.WriteString("  <path fill=\"" + fill + "\" fill-rule=\"evenodd\" d=\"")
	for _, c := range l.Contours {
		for i, p := range c.Points {
			if i == 0 {
				b.WriteString("M")
			} else {
				b.WriteString(" L")
			}
			b.WriteString(num(p[0]) + "," + num(r.HeightMM-p[1]))
		}
		b.WriteString(" Z ")
	}
	b.WriteString("\"/>\n</svg>\n")
	return b.Bytes()
}

// Polygon devuelve la capa como una llamada `polygon(points=…, paths=…)` de
// OpenSCAD (Y hacia arriba), o "" si tiene más de maxPoints puntos.
func (r *TraceResult) Polygon(layer, maxPoints int) string {
	if layer < 0 || layer >= len(r.Layers) {
		return ""
	}
	l := r.Layers[layer]
	total := 0
	for _, c := range l.Contours {
		total += len(c.Points)
	}
	if total == 0 || total > maxPoints {
		return ""
	}
	var pts, paths []string
	idx := 0
	for _, c := range l.Contours {
		var ids []string
		for _, p := range c.Points {
			pts = append(pts, "["+num(p[0])+","+num(p[1])+"]")
			ids = append(ids, strconv.Itoa(idx))
			idx++
		}
		paths = append(paths, "["+strings.Join(ids, ",")+"]")
	}
	return "polygon(points=[" + strings.Join(pts, ",") + "], paths=[" + strings.Join(paths, ",") + "]);"
}

// PointCount es el total de puntos de una capa.
func (l Layer) PointCount() int {
	n := 0
	for _, c := range l.Contours {
		n += len(c.Points)
	}
	return n
}
