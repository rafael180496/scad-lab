// Package imagetools procesa las imágenes que el usuario le da a la IA para que
// modele a partir de ellas: una foto de una pieza, un logo, un boceto. Es Go puro
// (PNG, JPEG y GIF de la biblioteca estándar; WebP con golang.org/x/image/webp) y
// calcula lo que un modelo de lenguaje no saca de unos píxeles: el tamaño, los
// colores dominantes, dónde está el objeto, su contorno en milímetros (SVG para
// `import()`), un mapa de alturas para `surface()` y la comparación con el
// render del modelo.
//
// Lo usan las herramientas `image_info`, `compare_with_image`,
// `trace_image_outline` e `image_to_heightmap` (backend/mcp). Spec:
// .claude/specs/s15-modelo-desde-imagen.md.
//
// Límites duros, todos comprobados ANTES de gastar memoria o tiempo:
//
//   - el archivo pesa a lo sumo MaxBytes (20 MB);
//   - tiene a lo sumo MaxPixels (40 MP): se lee con image.DecodeConfig, que solo
//     mira la cabecera, antes de decodificar nada (una «bomba» de 60000×60000
//     píxeles cabe en unos pocos KB de PNG);
//   - todo el trabajo se hace sobre una copia reducida a MaxWork (2048 px) de
//     lado;
//   - cada operación corre con un plazo de OpTimeout (10 s).
package imagetools

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	_ "image/gif" // registra el decodificador GIF
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"time"

	_ "golang.org/x/image/webp" // registra el decodificador WebP (Go puro)

	"scadlab/backend/i18n"
)

// Límites del paquete (ver la documentación del paquete).
const (
	MaxBytes  int64 = 20 << 20
	MaxPixels       = 40_000_000
	MaxWork         = 2048
	OpTimeout       = 10 * time.Second
)

// Formatos que se reconocen, por contenido.
const (
	FormatPNG  = "png"
	FormatJPEG = "jpeg"
	FormatGIF  = "gif"
	FormatWebP = "webp"
)

// Image es una imagen ya decodificada y reducida a lo más a MaxWork píxeles de
// lado. Pix son W×H píxeles NRGBA (4 bytes, sin premultiplicar), de arriba hacia
// abajo.
type Image struct {
	W, H int
	Pix  []uint8

	// OrigW y OrigH son las medidas del archivo ya orientado (con la rotación
	// EXIF aplicada): es lo que el usuario y `image_info` llaman «píxeles».
	OrigW, OrigH int
	Format       string
	Bytes        int64
	// Orientation es la etiqueta EXIF (1 = normal). Ya está aplicada a Pix.
	Orientation int
	// HasAlpha: una parte significativa de la imagen es transparente (un logo
	// sobre fondo transparente): ahí el primer plano es lo opaco.
	HasAlpha bool
}

// ScalePx es cuántos píxeles del archivo original equivale un píxel de trabajo.
func (im *Image) ScalePx() float64 { return float64(im.OrigW) / float64(im.W) }

func errTooBig(size int64) error {
	return i18n.Errorf(i18n.Msg{
		ES: "la imagen es demasiado grande (%d MB; máximo %d MB)",
		EN: "the image is too large (%d MB; maximum %d MB)",
	}, size>>20, MaxBytes>>20)
}

var errNotImage = i18n.New(i18n.Msg{
	ES: "formato no reconocido: usa una imagen PNG, JPEG, GIF o WebP",
	EN: "unrecognized format: use a PNG, JPEG, GIF or WebP image",
})

// Load lee una imagen del disco. El que llama ya validó que la ruta es suya
// (backend/refmodel); aquí solo se comprueban los límites.
func Load(ctx context.Context, path string) (*Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, i18n.Errorf(i18n.Msg{ES: "%s no es un archivo", EN: "%s is not a file"}, st.Name())
	}
	if st.Size() > MaxBytes {
		return nil, errTooBig(st.Size())
	}
	// Se lee hasta MaxBytes+1: si el archivo creció mientras se leía, no pasa.
	data, err := io.ReadAll(io.LimitReader(f, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxBytes {
		return nil, errTooBig(int64(len(data)))
	}
	return Decode(ctx, data)
}

// Decode decodifica los bytes de una imagen con los límites del paquete. La
// cabecera se lee primero: si declara más de MaxPixels, no se decodifica.
func Decode(ctx context.Context, data []byte) (*Image, error) {
	if int64(len(data)) > MaxBytes {
		return nil, errTooBig(int64(len(data)))
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		if errors.Is(err, image.ErrFormat) {
			return nil, errNotImage
		}
		return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo leer la imagen: %w", EN: "could not read the image: %w"}, err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, i18n.New(i18n.Msg{ES: "la imagen no tiene píxeles", EN: "the image has no pixels"})
	}
	if int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return nil, i18n.Errorf(i18n.Msg{
			ES: "la imagen tiene demasiados píxeles (%d×%d = %d MP; máximo %d MP)",
			EN: "the image has too many pixels (%d×%d = %d MP; maximum %d MP)",
		}, cfg.Width, cfg.Height, int64(cfg.Width)*int64(cfg.Height)/1_000_000, MaxPixels/1_000_000)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	orientation := 1
	if format == FormatJPEG {
		orientation = exifOrientation(data)
	}

	// La decodificación no se puede interrumpir desde fuera: corre en su propia
	// goroutine para que el plazo de la operación se cumpla igual. La goroutine
	// termina sola y libera su memoria.
	type result struct {
		img image.Image
		err error
	}
	ch := make(chan result, 1)
	go func() {
		img, _, err := image.Decode(bytes.NewReader(data))
		ch <- result{img, err}
	}()
	var src image.Image
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, i18n.Errorf(i18n.Msg{ES: "no se pudo decodificar la imagen: %w", EN: "could not decode the image: %w"}, r.err)
		}
		src = r.img
	}

	im, err := fromImage(ctx, src, orientation)
	if err != nil {
		return nil, err
	}
	im.Format = format
	im.Bytes = int64(len(data))
	return im, nil
}

// FromImage envuelve una imagen de la biblioteca estándar (por ejemplo, la que
// dibuja backend/raster) con los mismos límites y la misma reducción.
func FromImage(ctx context.Context, src image.Image) (*Image, error) {
	b := src.Bounds()
	if b.Dx() <= 0 || b.Dy() <= 0 || int64(b.Dx())*int64(b.Dy()) > MaxPixels {
		return nil, i18n.New(i18n.Msg{ES: "la imagen no tiene un tamaño válido", EN: "the image does not have a valid size"})
	}
	im, err := fromImage(ctx, src, 1)
	if err != nil {
		return nil, err
	}
	im.Format = FormatPNG
	return im, nil
}

// New arma una Image a partir de píxeles NRGBA ya reducidos (para las pruebas y
// para las operaciones internas).
func New(w, h int, pix []uint8) *Image {
	im := &Image{W: w, H: h, Pix: pix, OrigW: w, OrigH: h, Orientation: 1}
	im.HasAlpha = detectAlpha(pix)
	return im
}

// fromImage reduce src a lo más a MaxWork de lado y le aplica la orientación.
func fromImage(ctx context.Context, src image.Image, orientation int) (*Image, error) {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	dw, dh := sw, sh
	if m := max(sw, sh); m > MaxWork {
		dw = max(1, sw*MaxWork/m)
		dh = max(1, sh*MaxWork/m)
	}
	pix, err := resizeBox(ctx, newRowReader(src), sw, sh, dw, dh)
	if err != nil {
		return nil, err
	}
	// La orientación se aplica ya reducida (es mucho más barato) y las medidas
	// «originales» se dan con ella aplicada.
	pix, dw, dh = orient(pix, dw, dh, orientation)
	ow, oh := sw, sh
	if orientation >= 5 {
		ow, oh = sh, sw
	}
	im := &Image{W: dw, H: dh, Pix: pix, OrigW: ow, OrigH: oh, Orientation: orientation}
	im.HasAlpha = detectAlpha(pix)
	return im, nil
}

// detectAlpha dice si al menos el 2 % de los píxeles es translúcido o
// transparente.
func detectAlpha(pix []uint8) bool {
	n := len(pix) / 4
	if n == 0 {
		return false
	}
	cnt := 0
	for i := 3; i < len(pix); i += 4 {
		if pix[i] < 250 {
			cnt++
		}
	}
	return cnt*50 >= n
}

// rowReader entrega la fila y de una imagen como NRGBA (4 bytes por píxel).
type rowReader func(y int, dst []uint8)

func newRowReader(src image.Image) rowReader {
	b := src.Bounds()
	w := b.Dx()
	switch s := src.(type) {
	case *image.NRGBA:
		return func(y int, dst []uint8) {
			o := s.PixOffset(b.Min.X, b.Min.Y+y)
			copy(dst, s.Pix[o:o+4*w])
		}
	case *image.RGBA:
		return func(y int, dst []uint8) {
			o := s.PixOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < w; x++ {
				r, g, bl, a := uint32(s.Pix[o]), uint32(s.Pix[o+1]), uint32(s.Pix[o+2]), uint32(s.Pix[o+3])
				if a == 255 || a == 0 {
					dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = uint8(r), uint8(g), uint8(bl), uint8(a)
				} else {
					dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = uint8(r*255/a), uint8(g*255/a), uint8(bl*255/a), uint8(a)
				}
				o += 4
			}
		}
	case *image.Gray:
		return func(y int, dst []uint8) {
			o := s.PixOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < w; x++ {
				v := s.Pix[o+x]
				dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = v, v, v, 255
			}
		}
	case *image.YCbCr:
		return func(y int, dst []uint8) {
			for x := 0; x < w; x++ {
				yi := s.YOffset(b.Min.X+x, b.Min.Y+y)
				ci := s.COffset(b.Min.X+x, b.Min.Y+y)
				r, g, bl := color.YCbCrToRGB(s.Y[yi], s.Cb[ci], s.Cr[ci])
				dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = r, g, bl, 255
			}
		}
	case *image.Paletted:
		pal := make([][4]uint8, len(s.Palette))
		for i, c := range s.Palette {
			n := color.NRGBAModel.Convert(c).(color.NRGBA)
			pal[i] = [4]uint8{n.R, n.G, n.B, n.A}
		}
		return func(y int, dst []uint8) {
			o := s.PixOffset(b.Min.X, b.Min.Y+y)
			for x := 0; x < w; x++ {
				idx := int(s.Pix[o+x])
				if idx >= len(pal) {
					dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = 0, 0, 0, 0
					continue
				}
				p := pal[idx]
				dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = p[0], p[1], p[2], p[3]
			}
		}
	}
	return func(y int, dst []uint8) {
		for x := 0; x < w; x++ {
			n := color.NRGBAModel.Convert(src.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			dst[4*x], dst[4*x+1], dst[4*x+2], dst[4*x+3] = n.R, n.G, n.B, n.A
		}
	}
}

// resizeBox reduce (o copia, si dw×dh = sw×sh) promediando el área que cubre
// cada píxel de destino, con el alfa premultiplicado para que un borde
// transparente no tiña el color. Comprueba el contexto cada 64 filas.
func resizeBox(ctx context.Context, read rowReader, sw, sh, dw, dh int) ([]uint8, error) {
	out := make([]uint8, 4*dw*dh)
	row := make([]uint8, 4*sw)
	acc := make([]uint64, 4*dw) // r, g, b premultiplicados por alfa, y alfa
	cnt := make([]uint64, dw)
	x0s := make([]int, dw+1)
	for dx := 0; dx <= dw; dx++ {
		x0s[dx] = dx * sw / dw
	}
	for dy := 0; dy < dh; dy++ {
		if dy&63 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		y0, y1 := dy*sh/dh, (dy+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for i := range acc {
			acc[i] = 0
		}
		for i := range cnt {
			cnt[i] = 0
		}
		for y := y0; y < y1; y++ {
			read(y, row)
			for dx := 0; dx < dw; dx++ {
				x0, x1 := x0s[dx], x0s[dx+1]
				if x1 <= x0 {
					x1 = x0 + 1
				}
				var r, g, b, a uint64
				for x := x0; x < x1; x++ {
					pa := uint64(row[4*x+3])
					r += uint64(row[4*x]) * pa
					g += uint64(row[4*x+1]) * pa
					b += uint64(row[4*x+2]) * pa
					a += pa
				}
				acc[4*dx] += r
				acc[4*dx+1] += g
				acc[4*dx+2] += b
				acc[4*dx+3] += a
				cnt[dx] += uint64(x1 - x0)
			}
		}
		for dx := 0; dx < dw; dx++ {
			o := 4 * (dy*dw + dx)
			a := acc[4*dx+3]
			if a == 0 {
				continue // transparente: negro con alfa 0
			}
			out[o] = uint8((acc[4*dx] + a/2) / a)
			out[o+1] = uint8((acc[4*dx+1] + a/2) / a)
			out[o+2] = uint8((acc[4*dx+2] + a/2) / a)
			out[o+3] = uint8((a + cnt[dx]/2) / cnt[dx])
		}
	}
	return out, nil
}

// orient aplica la orientación EXIF (1–8) a unos píxeles NRGBA y devuelve las
// medidas nuevas.
func orient(pix []uint8, w, h, o int) ([]uint8, int, int) {
	if o < 2 || o > 8 {
		return pix, w, h
	}
	nw, nh := w, h
	if o >= 5 {
		nw, nh = h, w
	}
	out := make([]uint8, len(pix))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2: // espejo horizontal
				dx, dy = w-1-x, y
			case 3: // 180°
				dx, dy = w-1-x, h-1-y
			case 4: // espejo vertical
				dx, dy = x, h-1-y
			case 5: // transpuesta
				dx, dy = y, x
			case 6: // 90° horario
				dx, dy = h-1-y, x
			case 7: // transversa
				dx, dy = h-1-y, w-1-x
			case 8: // 90° antihorario
				dx, dy = y, w-1-x
			}
			copy(out[4*(dy*nw+dx):4*(dy*nw+dx)+4], pix[4*(y*w+x):4*(y*w+x)+4])
		}
	}
	return out, nw, nh
}

// exifOrientation lee la etiqueta de orientación EXIF de un JPEG (1 si no hay).
// Un teléfono guarda las fotos «de lado» y anota aquí cómo girarlas: ignorarlo
// compararía la pieza acostada contra el modelo de pie.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xD9 || marker == 0xDA { // fin o datos de imagen
			return 1
		}
		if marker == 0xFF {
			i++
			continue
		}
		n := int(data[i+2])<<8 | int(data[i+3])
		if n < 2 || i+2+n > len(data) {
			return 1
		}
		seg := data[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			return parseTIFFOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

func parseTIFFOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var u16 func([]byte) int
	var u32 func([]byte) int
	switch string(t[:2]) {
	case "II":
		u16 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 }
		u32 = func(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24 }
	case "MM":
		u16 = func(b []byte) int { return int(b[0])<<8 | int(b[1]) }
		u32 = func(b []byte) int { return int(b[0])<<24 | int(b[1])<<16 | int(b[2])<<8 | int(b[3]) }
	default:
		return 1
	}
	off := u32(t[4:8])
	if off < 8 || off+2 > len(t) {
		return 1
	}
	n := u16(t[off : off+2])
	for k := 0; k < n; k++ {
		e := off + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if u16(t[e:e+2]) == 0x0112 { // Orientation
			if v := u16(t[e+8 : e+10]); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// toNRGBA copia la imagen de trabajo a una image.NRGBA de la biblioteca estándar
// (para codificarla o dibujar sobre ella).
func (im *Image) toNRGBA() *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, im.W, im.H))
	copy(out.Pix, im.Pix)
	return out
}

// fromNRGBA es la inversa de toNRGBA.
func fromNRGBA(n *image.NRGBA) *Image {
	pix := make([]uint8, 4*n.Rect.Dx()*n.Rect.Dy())
	w := n.Rect.Dx()
	for y := 0; y < n.Rect.Dy(); y++ {
		o := n.PixOffset(n.Rect.Min.X, n.Rect.Min.Y+y)
		copy(pix[4*y*w:4*(y+1)*w], n.Pix[o:o+4*w])
	}
	return New(n.Rect.Dx(), n.Rect.Dy(), pix)
}
