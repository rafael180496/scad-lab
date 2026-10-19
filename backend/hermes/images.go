package hermes

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"strings"

	// Los decodificadores que reconoce image.Decode: PNG (lo que dibuja
	// ScadLab), JPEG y GIF (adjuntos). WebP no está en la biblioteca estándar y
	// no se agrega una dependencia solo para una miniatura: sin miniatura el
	// chip muestra un ícono.
	_ "image/gif"
	_ "image/png"
)

// Tamaños de las imágenes que se guardan en la base (dentro de Parts).
const (
	// ThumbPx es el lado de la miniatura de un chip de adjunto.
	ThumbPx = 96
	// StepImagePx es el lado máximo de la imagen de un paso al guardarlo. La
	// vista en vivo lleva el PNG completo; lo guardado es un JPEG reducido para
	// que una conversación de decenas de turnos no engorde la base.
	StepImagePx = 640
)

// shrink decodifica una imagen y la devuelve como JPEG de a lo más max píxeles
// de lado, sobre fondo blanco (el JPEG no tiene transparencia). ok=false si no
// se pudo decodificar (WebP, un archivo corrupto).
func shrink(raw []byte, max, quality int) (jpg []byte, ok bool) {
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, false
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, false
	}
	nw, nh := w, h
	if w > max || h > max {
		if w >= h {
			nw, nh = max, max*h/w
		} else {
			nw, nh = max*w/h, max
		}
		if nw < 1 {
			nw = 1
		}
		if nh < 1 {
			nh = 1
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	if nw == w && nh == h {
		draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	} else {
		scaleBox(dst, src)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: quality}); err != nil {
		return nil, false
	}
	return buf.Bytes(), true
}

// scaleBox reduce src a dst promediando los píxeles de origen que caen en cada
// píxel de destino (un filtro de caja): suficiente para una miniatura y sin
// depender de golang.org/x/image.
func scaleBox(dst *image.RGBA, src image.Image) {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	dw, dh := dst.Bounds().Dx(), dst.Bounds().Dy()
	for y := 0; y < dh; y++ {
		y0, y1 := y*sh/dh, (y+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dw; x++ {
			x0, x1 := x*sw/dw, (x+1)*sw/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, b, n uint64
			for sy := y0; sy < y1 && sy < sh; sy++ {
				for sx := x0; sx < x1 && sx < sw; sx++ {
					cr, cg, cb, ca := src.At(sb.Min.X+sx, sb.Min.Y+sy).RGBA()
					// Compuesto sobre blanco: lo transparente queda blanco.
					r += uint64(cr) + uint64(0xffff-ca)
					g += uint64(cg) + uint64(0xffff-ca)
					b += uint64(cb) + uint64(0xffff-ca)
					n++
				}
			}
			if n == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(b / n >> 8), 255})
		}
	}
}

// ThumbDataURL es la miniatura (JPEG como data URL) de una imagen, o "" si no se
// pudo decodificar.
func ThumbDataURL(raw []byte) string {
	jpg, ok := shrink(raw, ThumbPx, 70)
	if !ok {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpg)
}

// shrinkDataURL reduce la imagen de un data URL (PNG en vivo) al tamaño con que
// se guarda. Un data URL que no se puede leer se descarta (""): guardar un
// PNG de megabytes sin reducir es peor que no mostrar la imagen al reabrir.
func shrinkDataURL(u string) string {
	_, b64, ok := strings.Cut(u, ",")
	if !ok || !strings.HasPrefix(u, "data:image/") {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ""
	}
	jpg, ok := shrink(raw, StepImagePx, 72)
	if !ok {
		return ""
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpg)
}
