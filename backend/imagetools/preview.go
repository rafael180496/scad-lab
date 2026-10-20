package imagetools

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
	"sort"
)

func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.DefaultCompression}).Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PreviewPNG dibuja lo trazado (cada capa con su color, o gris oscuro si es una
// sola) sobre blanco, con el lado mayor de side píxeles. Es lo que ve una IA con
// visión para comprobar que el contorno se parece a la foto sin tener que
// importar el SVG a OpenSCAD.
func (r *TraceResult) PreviewPNG(side int) ([]byte, error) {
	side = max(64, min(1200, side))
	const margin = 10
	p := float64(side-2*margin) / math.Max(r.WidthMM, r.HeightMM)
	w, h := int(math.Ceil(r.WidthMM*p))+2*margin, int(math.Ceil(r.HeightMM*p))+2*margin
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for _, l := range r.Layers {
		col := color.NRGBA{31, 41, 55, 255}
		if l.Color != "" {
			var c [3]uint8
			if parseHex(l.Color, &c) {
				col = color.NRGBA{c[0], c[1], c[2], 255}
			}
		}
		fillEvenOdd(img, l, r.HeightMM, p, margin, col)
	}
	return encodePNG(img)
}

func parseHex(s string, out *[3]uint8) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for i := 0; i < 3; i++ {
		v := 0
		for _, ch := range s[1+2*i : 3+2*i] {
			v <<= 4
			switch {
			case ch >= '0' && ch <= '9':
				v |= int(ch - '0')
			case ch >= 'A' && ch <= 'F':
				v |= int(ch-'A') + 10
			case ch >= 'a' && ch <= 'f':
				v |= int(ch-'a') + 10
			default:
				return false
			}
		}
		out[i] = uint8(v)
	}
	return true
}

// fillEvenOdd rellena los contornos de una capa con la regla par-impar, por
// líneas de barrido.
func fillEvenOdd(img *image.NRGBA, l Layer, heightMM, p float64, margin int, col color.NRGBA) {
	type edge struct{ x0, y0, x1, y1 float64 }
	var edges []edge
	for _, c := range l.Contours {
		n := len(c.Points)
		for i := 0; i < n; i++ {
			a, b := c.Points[i], c.Points[(i+1)%n]
			edges = append(edges, edge{
				float64(margin) + a[0]*p, float64(margin) + (heightMM-a[1])*p,
				float64(margin) + b[0]*p, float64(margin) + (heightMM-b[1])*p,
			})
		}
	}
	for y := 0; y < img.Rect.Dy(); y++ {
		fy := float64(y) + 0.5
		var xs []float64
		for _, e := range edges {
			if (e.y0 <= fy) == (e.y1 <= fy) {
				continue
			}
			xs = append(xs, e.x0+(fy-e.y0)/(e.y1-e.y0)*(e.x1-e.x0))
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			for x := int(math.Round(xs[i])); x < int(math.Round(xs[i+1])); x++ {
				if x >= 0 && x < img.Rect.Dx() {
					img.SetNRGBA(x, y, col)
				}
			}
		}
	}
}

// PreviewPNG dibuja el mapa de alturas en grises (claro = alto), ampliado hasta
// que su lado mayor ronde side píxeles.
func (h *Heightmap) PreviewPNG(side int) ([]byte, error) {
	k := max(1, side/max(h.Cols, h.Rows))
	img := image.NewGray(image.Rect(0, 0, h.Cols*k, h.Rows*k))
	for y := 0; y < h.Rows*k; y++ {
		for x := 0; x < h.Cols*k; x++ {
			v := h.Values[(y/k)*h.Cols+x/k]
			img.SetGray(x, y, color.Gray{uint8(math.Round(float64(v) * 2.55))})
		}
	}
	return encodePNG(img)
}
