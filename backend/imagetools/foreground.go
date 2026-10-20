package imagetools

import (
	"context"
	"math"
)

// Foreground es el «primer plano» de una imagen: lo que no es fondo. El fondo se
// toma del color del borde (la mediana del anillo exterior) y es primer plano
// todo píxel que se aleja de él más que un umbral. Es lo que sirve para una
// pieza fotografiada sobre una mesa lisa, un logo sobre blanco o una render de
// OpenSCAD; con un fondo de verdad irregular (una foto en un taller) el primer
// plano es poco fiable y Uniform lo dice.
type Foreground struct {
	Mask *Mask
	// Box es la caja del primer plano, en píxeles de la imagen de trabajo.
	Box Box
	// Background es el color del fondo (blanco si la imagen es transparente).
	Background [3]uint8
	// Transparent: el fondo es transparente y el primer plano es lo opaco.
	Transparent bool
	// Uniform: el anillo del borde es de un color casi único. Sin esto, el
	// primer plano no es de fiar.
	Uniform bool
	// Coverage es la fracción del área que ocupa el primer plano.
	Coverage float64
}

// minBgDistance es lo que un píxel tiene que alejarse del fondo (distancia RGB,
// 0–441) para contar como primer plano aunque Otsu proponga menos: el ruido de
// un JPEG y la textura de un papel no son objeto.
const minBgDistance = 18

// borderMedian es la mediana por canal del anillo exterior de la imagen y la
// fracción del anillo que queda a menos de 40 de ese color.
func borderMedian(rgb []uint8, w, h int) (bg [3]uint8, uniform float64) {
	t := max(1, min(w, h)/100)
	var hist [3][256]int
	n := 0
	visit := func(x, y int) {
		i := 3 * (y*w + x)
		for c := 0; c < 3; c++ {
			hist[c][rgb[i+c]]++
		}
		n++
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < t || y < t || x >= w-t || y >= h-t {
				visit(x, y)
			}
		}
	}
	for c := 0; c < 3; c++ {
		acc := 0
		for v := 0; v < 256; v++ {
			acc += hist[c][v]
			if acc*2 >= n {
				bg[c] = uint8(v)
				break
			}
		}
	}
	near := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < t || y < t || x >= w-t || y >= h-t {
				i := 3 * (y*w + x)
				if colorDist(rgb[i], rgb[i+1], rgb[i+2], bg) < 40 {
					near++
				}
			}
		}
	}
	return bg, float64(near) / float64(max(1, n))
}

func colorDist(r, g, b uint8, c [3]uint8) float64 {
	dr, dg, db := float64(r)-float64(c[0]), float64(g)-float64(c[1]), float64(b)-float64(c[2])
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// Foreground separa el primer plano de la imagen.
func (im *Image) Foreground(ctx context.Context) (*Foreground, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fg := &Foreground{Mask: newMask(im.W, im.H), Background: [3]uint8{255, 255, 255}}
	if im.HasAlpha {
		fg.Transparent, fg.Uniform = true, true
		for i := 0; i < im.W*im.H; i++ {
			fg.Mask.Bits[i] = im.Pix[4*i+3] >= 128
		}
	} else {
		rgb := im.flatten([3]uint8{255, 255, 255})
		bg, uni := borderMedian(rgb, im.W, im.H)
		fg.Background, fg.Uniform = bg, uni >= 0.8
		hist := make([]int, 443)
		dist := make([]uint16, im.W*im.H)
		maxD := 0
		for i := range dist {
			d := int(math.Round(colorDist(rgb[3*i], rgb[3*i+1], rgb[3*i+2], bg)))
			dist[i] = uint16(d)
			hist[d]++
			maxD = max(maxD, d)
		}
		if maxD < 2*minBgDistance {
			return fg, nil // imagen casi lisa: no hay primer plano
		}
		thr := max(otsu(hist), minBgDistance)
		for i, d := range dist {
			fg.Mask.Bits[i] = int(d) > thr
		}
	}
	// Limpieza: motas de fondo, huecos diminutos y polvo.
	minArea := max(4, im.W*im.H/5000)
	fg.Mask = keepMain(dropSpecks(fg.Mask, minArea), 0.02)
	fg.Box = fg.Mask.BBox()
	fg.Coverage = float64(fg.Mask.Count()) / float64(im.W*im.H)
	return fg, nil
}

// BoxFractions devuelve la caja como fracciones [x, y, ancho, alto] de la imagen.
func (f *Foreground) BoxFractions() [4]float64 {
	if !f.Box.Valid {
		return [4]float64{}
	}
	w, h := float64(f.Mask.W), float64(f.Mask.H)
	return [4]float64{
		float64(f.Box.X0) / w, float64(f.Box.Y0) / h,
		float64(f.Box.W()) / w, float64(f.Box.H()) / h,
	}
}

// Aspect es ancho/alto del primer plano en píxeles (0 si no hay).
func (f *Foreground) Aspect() float64 {
	if !f.Box.Valid || f.Box.H() == 0 {
		return 0
	}
	return float64(f.Box.W()) / float64(f.Box.H())
}
