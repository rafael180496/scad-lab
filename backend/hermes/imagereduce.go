package hermes

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"

	"scadlab/backend/i18n"
	"scadlab/backend/imagetools"
)

// Imágenes grandes del chat (S15): no se rechazan, se reducen al adjuntar.
//
// Una foto de un teléfono pesa 6–15 MB y mide 4000 px o más de lado; la API de
// Anthropic acepta 5 MB por imagen y ningún modelo necesita más de ~2000 px
// para verla. Así que al adjuntar:
//
//   - se acepta hasta MaxImageInputBytes (20 MB, el mismo tope que
//     backend/imagetools, que es quien la decodifica con sus defensas: píxeles
//     contados en la cabecera antes de decodificar, plazo, orientación EXIF);
//   - al modelo va una copia reducida a MaxImageSide px de lado y recomprimida
//     (PNG si tiene transparencia y cabe, si no JPEG) por debajo de MaxImageBytes;
//   - el ORIGINAL es el que se guarda en refs/images/, porque es el que leen las
//     herramientas (trazar un contorno o medir en la foto entera da más detalle).
const (
	MaxImageInputBytes = imagetools.MaxBytes
	MaxImageSide       = 2048
)

// ReduceImage devuelve la versión de la imagen que va al modelo. Si ya cabe
// (lado ≤ MaxImageSide y ≤ MaxImageBytes) es la misma, sin tocar (reduced=false).
// Si no, una copia reducida y recomprimida con su tipo MIME.
func ReduceImage(raw []byte, mime string) (out []byte, outMime string, reduced bool, err error) {
	if int64(len(raw)) <= MaxImageBytes {
		if cfg, _, cerr := image.DecodeConfig(bytes.NewReader(raw)); cerr == nil && cfg.Width <= MaxImageSide && cfg.Height <= MaxImageSide {
			return raw, mime, false, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), imagetools.OpTimeout)
	defer cancel()
	im, err := imagetools.Decode(ctx, raw)
	if err != nil {
		return nil, "", false, err
	}
	src := &image.NRGBA{Pix: im.Pix, Stride: im.W * 4, Rect: image.Rect(0, 0, im.W, im.H)}
	if im.HasAlpha {
		var buf bytes.Buffer
		if err := png.Encode(&buf, src); err == nil && int64(buf.Len()) <= MaxImageBytes {
			return buf.Bytes(), "image/png", true, nil
		}
	}
	// JPEG sobre blanco; si todavía pesa de más (una foto con mucho ruido), se
	// baja la calidad y después el tamaño.
	var img image.Image = src
	for side := MaxImageSide; side >= 512; side /= 2 {
		if max(im.W, im.H) > side {
			img = resizeOnWhite(src, side)
		}
		for _, q := range []int{88, 80, 70} {
			var buf bytes.Buffer
			if err := jpeg.Encode(&buf, flattenOnWhite(img), &jpeg.Options{Quality: q}); err != nil {
				return nil, "", false, err
			}
			if int64(buf.Len()) <= MaxImageBytes {
				return buf.Bytes(), "image/jpeg", true, nil
			}
		}
	}
	return nil, "", false, i18n.New(i18n.Msg{
		ES: "no se pudo reducir la imagen por debajo de 5 MB",
		EN: "the image could not be reduced below 5 MB",
	})
}

// resizeOnWhite reduce src a side px en su lado mayor (filtro de caja de
// scaleBox, sobre blanco).
func resizeOnWhite(src image.Image, side int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nw, nh := side, max(1, side*h/w)
	if h > w {
		nw, nh = max(1, side*w/h), side
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	scaleBox(dst, src)
	return dst
}

// flattenOnWhite compone una imagen con transparencia sobre blanco (el JPEG no
// tiene canal alfa). Una ya opaca (*image.RGBA de resizeOnWhite) vuelve igual.
func flattenOnWhite(src image.Image) image.Image {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, a := src.At(b.Min.X+x, b.Min.Y+y).RGBA()
			k := uint32(0xffff) - a
			dst.Pix[(y*dst.Stride)+x*4+0] = uint8((r + k) >> 8)
			dst.Pix[(y*dst.Stride)+x*4+1] = uint8((g + k) >> 8)
			dst.Pix[(y*dst.Stride)+x*4+2] = uint8((bl + k) >> 8)
			dst.Pix[(y*dst.Stride)+x*4+3] = 255
		}
	}
	return dst
}
