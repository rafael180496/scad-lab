package imagetools

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"math"
	"sort"

	"scadlab/backend/i18n"
)

// DominantColor es un color predominante y el porcentaje de la imagen que cubre.
type DominantColor struct {
	Hex     string  `json:"hex"`
	Percent float64 `json:"percent"`
}

// Info es lo que se sabe de una imagen sin mirarla.
type Info struct {
	Format      string          `json:"format"`
	WidthPx     int             `json:"widthPx"`
	HeightPx    int             `json:"heightPx"`
	Aspect      float64         `json:"aspect"` // ancho / alto
	Bytes       int64           `json:"bytes"`
	Megapixels  float64         `json:"megapixels"`
	WorkWidth   int             `json:"workWidthPx"`  // medidas sobre las que se trabaja
	WorkHeight  int             `json:"workHeightPx"` // (reducidas a ≤ 2048 de lado)
	Orientation int             `json:"exifOrientation,omitempty"`
	HasAlpha    bool            `json:"hasTransparency"`
	Dominant    []DominantColor `json:"dominantColors"`
	Background  BackgroundInfo  `json:"background"`
	Foreground  *ForegroundInfo `json:"foreground,omitempty"`
}

// BackgroundInfo describe el fondo que se detectó en el borde.
type BackgroundInfo struct {
	Hex         string `json:"hex"`
	Transparent bool   `json:"transparent,omitempty"`
	Uniform     bool   `json:"uniform"`
}

// ForegroundInfo es el primer plano: su caja en fracciones de la imagen
// ([x, y, ancho, alto], 0–1), su proporción y cuánto ocupa.
type ForegroundInfo struct {
	Box      [4]float64 `json:"box"`
	Aspect   float64    `json:"aspect"`
	Coverage float64    `json:"coverage"`
}

func hex(c [3]uint8) string { return fmt.Sprintf("#%02X%02X%02X", c[0], c[1], c[2]) }

// Describe calcula la ficha de una imagen.
func (im *Image) Describe(ctx context.Context) (Info, error) {
	info := Info{
		Format: im.Format, WidthPx: im.OrigW, HeightPx: im.OrigH,
		Aspect: round3(float64(im.OrigW) / float64(im.OrigH)), Bytes: im.Bytes,
		Megapixels: round3(float64(im.OrigW) * float64(im.OrigH) / 1e6),
		WorkWidth:  im.W, WorkHeight: im.H, HasAlpha: im.HasAlpha,
	}
	if im.Orientation > 1 {
		info.Orientation = im.Orientation
	}
	small := im
	if m := max(im.W, im.H); m > 256 {
		var err error
		small, err = im.Resize(ctx, max(1, im.W*256/m), max(1, im.H*256/m))
		if err != nil {
			return info, err
		}
	}
	info.Dominant = small.dominantColors(5)
	fg, err := small.Foreground(ctx)
	if err != nil {
		return info, err
	}
	info.Background = BackgroundInfo{Hex: hex(fg.Background), Transparent: fg.Transparent, Uniform: fg.Uniform}
	if fg.Box.Valid {
		info.Foreground = &ForegroundInfo{
			Box:      round3s(fg.BoxFractions()),
			Aspect:   round3(fg.Aspect()),
			Coverage: round3(fg.Coverage),
		}
	}
	return info, nil
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

func round3s(a [4]float64) [4]float64 {
	for i := range a {
		a[i] = round3(a[i])
	}
	return a
}

// dominantColors agrupa los colores en cubos de 4 bits por canal, elige hasta n
// centros bien separados (a más de 60 de distancia) por orden de frecuencia y
// asigna cada píxel opaco al más cercano: los porcentajes suman 100.
func (im *Image) dominantColors(n int) []DominantColor {
	var bins [4096]int
	total := 0
	for i := 0; i < im.W*im.H; i++ {
		if im.Pix[4*i+3] < 128 {
			continue
		}
		bins[int(im.Pix[4*i]>>4)<<8|int(im.Pix[4*i+1]>>4)<<4|int(im.Pix[4*i+2]>>4)]++
		total++
	}
	if total == 0 {
		return nil
	}
	order := make([]int, 0, 4096)
	for b, c := range bins {
		if c > 0 {
			order = append(order, b)
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if bins[order[i]] != bins[order[j]] {
			return bins[order[i]] > bins[order[j]]
		}
		return order[i] < order[j]
	})
	center := func(b int) [3]float64 {
		return [3]float64{float64(b>>8&15)*17 + 8, float64(b>>4&15)*17 + 8, float64(b&15)*17 + 8}
	}
	var centers [][3]float64
	for _, b := range order {
		c := center(b)
		ok := true
		for _, o := range centers {
			if dist3(c, o) < 60 {
				ok = false
				break
			}
		}
		if ok {
			centers = append(centers, c)
			if len(centers) == n {
				break
			}
		}
	}
	sums := make([][3]float64, len(centers))
	cnt := make([]int, len(centers))
	for i := 0; i < im.W*im.H; i++ {
		if im.Pix[4*i+3] < 128 {
			continue
		}
		p := [3]float64{float64(im.Pix[4*i]), float64(im.Pix[4*i+1]), float64(im.Pix[4*i+2])}
		best, bd := 0, math.MaxFloat64
		for k, c := range centers {
			if d := dist3(p, c); d < bd {
				best, bd = k, d
			}
		}
		for c := 0; c < 3; c++ {
			sums[best][c] += p[c]
		}
		cnt[best]++
	}
	out := make([]DominantColor, 0, len(centers))
	for k := range centers {
		if cnt[k] == 0 {
			continue
		}
		m := [3]uint8{uint8(sums[k][0] / float64(cnt[k])), uint8(sums[k][1] / float64(cnt[k])), uint8(sums[k][2] / float64(cnt[k]))}
		out = append(out, DominantColor{Hex: hex(m), Percent: math.Round(1000*float64(cnt[k])/float64(total)) / 10})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Percent > out[j].Percent })
	return out
}

func dist3(a, b [3]float64) float64 {
	return math.Sqrt((a[0]-b[0])*(a[0]-b[0]) + (a[1]-b[1])*(a[1]-b[1]) + (a[2]-b[2])*(a[2]-b[2]))
}

// Resize devuelve la imagen a otra medida: por área al reducir, bilineal al
// ampliar.
func (im *Image) Resize(ctx context.Context, nw, nh int) (*Image, error) {
	if nw < 1 || nh < 1 {
		return nil, i18n.New(i18n.Msg{ES: "medida de imagen inválida", EN: "invalid image size"})
	}
	if nw == im.W && nh == im.H {
		return im, nil
	}
	var pix []uint8
	if nw <= im.W && nh <= im.H {
		var err error
		pix, err = resizeBox(ctx, func(y int, dst []uint8) { copy(dst, im.Pix[4*y*im.W:4*(y+1)*im.W]) }, im.W, im.H, nw, nh)
		if err != nil {
			return nil, err
		}
	} else {
		pix = resizeBilinear(im, nw, nh)
	}
	out := New(nw, nh, pix)
	out.OrigW, out.OrigH, out.Format, out.Bytes, out.Orientation = im.OrigW, im.OrigH, im.Format, im.Bytes, im.Orientation
	return out, nil
}

// resizeBilinear amplía con interpolación bilineal (alfa premultiplicado).
func resizeBilinear(im *Image, nw, nh int) []uint8 {
	out := make([]uint8, 4*nw*nh)
	for y := 0; y < nh; y++ {
		fy := (float64(y)+0.5)*float64(im.H)/float64(nh) - 0.5
		y0 := int(math.Floor(fy))
		ty := fy - float64(y0)
		y1 := min(im.H-1, y0+1)
		y0 = max(0, y0)
		for x := 0; x < nw; x++ {
			fx := (float64(x)+0.5)*float64(im.W)/float64(nw) - 0.5
			x0 := int(math.Floor(fx))
			tx := fx - float64(x0)
			x1 := min(im.W-1, x0+1)
			x0 = max(0, x0)
			var acc [4]float64
			for _, s := range [4]struct {
				x, y int
				w    float64
			}{{x0, y0, (1 - tx) * (1 - ty)}, {x1, y0, tx * (1 - ty)}, {x0, y1, (1 - tx) * ty}, {x1, y1, tx * ty}} {
				i := 4 * (s.y*im.W + s.x)
				a := float64(im.Pix[i+3])
				acc[0] += s.w * float64(im.Pix[i]) * a
				acc[1] += s.w * float64(im.Pix[i+1]) * a
				acc[2] += s.w * float64(im.Pix[i+2]) * a
				acc[3] += s.w * a
			}
			o := 4 * (y*nw + x)
			if acc[3] > 0 {
				out[o], out[o+1], out[o+2] = uint8(acc[0]/acc[3]+0.5), uint8(acc[1]/acc[3]+0.5), uint8(acc[2]/acc[3]+0.5)
			}
			out[o+3] = uint8(acc[3] + 0.5)
		}
	}
	return out
}

// Crop recorta una región dada en fracciones [x, y, ancho, alto] (0–1).
func (im *Image) Crop(region [4]float64) (*Image, error) {
	x, y, w, h := region[0], region[1], region[2], region[3]
	const eps = 1e-6
	if !(w > 0 && h > 0 && x >= -eps && y >= -eps && x+w <= 1+eps && y+h <= 1+eps) {
		return nil, i18n.New(i18n.Msg{
			ES: "`region` lleva [x, y, ancho, alto] en fracciones de la imagen (0 a 1), por ejemplo [0.25, 0.25, 0.5, 0.5]",
			EN: "`region` takes [x, y, width, height] as fractions of the image (0 to 1), e.g. [0.25, 0.25, 0.5, 0.5]",
		})
	}
	x0 := int(math.Round(math.Max(0, x) * float64(im.W)))
	y0 := int(math.Round(math.Max(0, y) * float64(im.H)))
	x1 := int(math.Round(math.Min(1, x+w) * float64(im.W)))
	y1 := int(math.Round(math.Min(1, y+h) * float64(im.H)))
	x1, y1 = max(x1, x0+1), max(y1, y0+1)
	x1, y1 = min(x1, im.W), min(y1, im.H)
	x0, y0 = min(x0, x1-1), min(y0, y1-1)
	cw, ch := x1-x0, y1-y0
	pix := make([]uint8, 4*cw*ch)
	for r := 0; r < ch; r++ {
		copy(pix[4*r*cw:4*(r+1)*cw], im.Pix[4*((y0+r)*im.W+x0):4*((y0+r)*im.W+x1)])
	}
	out := New(cw, ch, pix)
	out.OrigW = max(1, int(math.Round(float64(cw)*im.ScalePx())))
	out.OrigH = max(1, int(math.Round(float64(ch)*im.ScalePx())))
	out.Format, out.Bytes, out.Orientation = im.Format, im.Bytes, im.Orientation
	return out, nil
}

// Enhance estira el contraste: lleva el percentil 1 de la luminancia a 0 y el 99
// a 255 en los tres canales a la vez (el tono no cambia). Una imagen que ya usa
// todo el rango queda igual. Devuelve una copia.
func (im *Image) Enhance() *Image {
	lum := make([]int, 256)
	for i := 0; i < im.W*im.H; i++ {
		if im.Pix[4*i+3] < 128 {
			continue
		}
		lum[(299*int(im.Pix[4*i])+587*int(im.Pix[4*i+1])+114*int(im.Pix[4*i+2]))/1000]++
	}
	total := 0
	for _, c := range lum {
		total += c
	}
	out := New(im.W, im.H, append([]uint8(nil), im.Pix...))
	out.OrigW, out.OrigH, out.Format, out.Bytes, out.Orientation = im.OrigW, im.OrigH, im.Format, im.Bytes, im.Orientation
	if total == 0 {
		return out
	}
	lo, hi, acc := 0, 255, 0
	for v := 0; v < 256; v++ {
		acc += lum[v]
		if acc*100 >= total {
			lo = v
			break
		}
	}
	acc = 0
	for v := 255; v >= 0; v-- {
		acc += lum[v]
		if acc*100 >= total {
			hi = v
			break
		}
	}
	if hi-lo < 16 {
		return out
	}
	for i := 0; i < im.W*im.H; i++ {
		for c := 0; c < 3; c++ {
			v := (int(out.Pix[4*i+c]) - lo) * 255 / (hi - lo)
			out.Pix[4*i+c] = uint8(max(0, min(255, v)))
		}
	}
	return out
}

// FitSide lleva la imagen a que su lado mayor sea side (ampliando hasta 4×
// lo que se recortó, o reduciendo).
func (im *Image) FitSide(ctx context.Context, side int) (*Image, error) {
	m := max(im.W, im.H)
	target := side
	if up := 4 * m; target > up {
		target = up
	}
	if target == m {
		return im, nil
	}
	return im.Resize(ctx, max(1, im.W*target/m), max(1, im.H*target/m))
}

// PNG codifica la imagen, compuesta sobre blanco, como PNG.
func (im *Image) PNG() ([]byte, error) {
	rgb := im.flatten([3]uint8{255, 255, 255})
	out := image.NewNRGBA(image.Rect(0, 0, im.W, im.H))
	for i := 0; i < im.W*im.H; i++ {
		out.Pix[4*i], out.Pix[4*i+1], out.Pix[4*i+2], out.Pix[4*i+3] = rgb[3*i], rgb[3*i+1], rgb[3*i+2], 255
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Reduced devuelve la imagen con su lado mayor de a lo más side píxeles (nunca
// la agranda). Es lo que se usa para calcular siluetas y colores sin pagar la
// resolución de trabajo completa.
func (im *Image) Reduced(ctx context.Context, side int) (*Image, error) {
	m := max(im.W, im.H)
	if m <= side {
		return im, nil
	}
	return im.Resize(ctx, max(1, im.W*side/m), max(1, im.H*side/m))
}
