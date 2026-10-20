package imagetools

import (
	"context"
	"image"
	"image/color"
	"image/draw"
	"math"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"scadlab/backend/i18n"
)

// SilhouetteFromBlack saca la silueta de una imagen dibujada con la pieza en
// negro sobre blanco (como la que hace backend/raster con un solo color negro):
// un píxel es de la pieza si su gris baja de 128, que con el suavizado de bordes
// es cubrir más de la mitad del píxel.
func SilhouetteFromBlack(img image.Image) *Mask {
	b := img.Bounds()
	m := newMask(b.Dx(), b.Dy())
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			m.Bits[y*m.W+x] = (299*r+587*g+114*bl)/1000 < 0x8000
		}
	}
	return m
}

// iouSide es el lado del lienzo en el que se comparan dos siluetas.
const iouSide = 160

// SilhouetteIoU mide cuánto se parecen dos siluetas, de 0 a 1, una vez
// normalizadas: cada una se recorta a su caja y se lleva, SIN deformarla, a un
// lienzo cuadrado, centrada. Así no importa a qué distancia se sacó la foto ni
// con qué encuadre se dibujó el modelo, pero sí la proporción: una pieza más
// ancha que la foto da un IoU menor aunque tenga la misma forma.
func SilhouetteIoU(a, b *Mask) float64 {
	ba, bb := a.BBox(), b.BBox()
	if !ba.Valid || !bb.Valid {
		return 0
	}
	ca, cb := normalizeSilhouette(a, ba), normalizeSilhouette(b, bb)
	inter, union := 0, 0
	for i := range ca {
		x, y := ca[i], cb[i]
		if x && y {
			inter++
		}
		if x || y {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

func normalizeSilhouette(m *Mask, b Box) []bool {
	n := iouSide
	pad := float64(n) / 16
	s := (float64(n) - 2*pad) / float64(max(b.W(), b.H()))
	ox := (float64(n) - float64(b.W())*s) / 2
	oy := (float64(n) - float64(b.H())*s) / 2
	out := make([]bool, n*n)
	for j := 0; j < n; j++ {
		for i := 0; i < n; i++ {
			sx := float64(b.X0) + (float64(i)+0.5-ox)/s
			sy := float64(b.Y0) + (float64(j)+0.5-oy)/s
			if sx < float64(b.X0) || sy < float64(b.Y0) || sx >= float64(b.X1) || sy >= float64(b.Y1) {
				continue
			}
			out[j*n+i] = m.at(int(sx), int(sy))
		}
	}
	return out
}

// Modos de la composición.
const (
	ModeSide    = "side"
	ModeOverlay = "overlay"
)

// ComposeOptions son los argumentos de Compose.
type ComposeOptions struct {
	// Mode es ModeSide (foto y modelo lado a lado) o ModeOverlay (la silueta del
	// modelo encima de la foto, alineada por caja).
	Mode string
	// PhotoLabel y ModelLabel son los rótulos (solo ASCII: la letra incluida no
	// trae acentos).
	PhotoLabel, ModelLabel string
	// Cell es el lado de cada panel en píxeles; 0 = 560.
	Cell int
	// FullFrame deja las dos imágenes enteras en el modo lado a lado. Por omisión
	// cada una se recorta alrededor de su objeto (con un margen) para que los dos
	// ocupen lo mismo en su panel y se puedan comparar a simple vista: una foto
	// con la pieza chica en un rincón no se ve igual de grande que el modelo, que
	// el render encuadra entero.
	FullFrame bool
}

// Compose arma la imagen que mira la IA para comparar la foto con el modelo.
func Compose(ctx context.Context, photo, model *Image, o ComposeOptions) (*image.NRGBA, error) {
	cell := o.Cell
	if cell <= 0 {
		cell = 560
	}
	cell = max(160, min(1024, cell))
	switch o.Mode {
	case ModeOverlay:
		return composeOverlay(ctx, photo, model, cell, o)
	case ModeSide, "":
		return composeSide(ctx, photo, model, cell, o)
	}
	return nil, i18n.Errorf(i18n.Msg{ES: "modo desconocido: %q (usa side u overlay)", EN: "unknown mode: %q (use side or overlay)"}, o.Mode)
}

var (
	panelBG  = color.NRGBA{236, 236, 236, 255}
	frameCol = color.NRGBA{120, 120, 120, 255}
)

// fitInto devuelve la imagen reducida (o ampliada hasta 4×) para que quepa en un
// cuadrado de lado cell, y su posición centrada.
func fitInto(ctx context.Context, im *Image, cell int) (*Image, image.Point, error) {
	s := math.Min(float64(cell)/float64(im.W), float64(cell)/float64(im.H))
	s = math.Min(s, 4)
	w, h := max(1, int(math.Round(float64(im.W)*s))), max(1, int(math.Round(float64(im.H)*s)))
	r, err := im.Resize(ctx, w, h)
	if err != nil {
		return nil, image.Point{}, err
	}
	return r, image.Pt((cell-w)/2, (cell-h)/2), nil
}

// focusOnObject recorta la imagen alrededor de su primer plano, con un margen del
// 8 % de su lado mayor. Solo si el primer plano es fiable (borde de un color
// uniforme) y no ocupa casi todo el cuadro: ante la duda deja la imagen entera.
func focusOnObject(ctx context.Context, im *Image) (*Image, error) {
	small, err := im.Reduced(ctx, 512)
	if err != nil {
		return nil, err
	}
	fg, err := small.Foreground(ctx)
	if err != nil {
		return nil, err
	}
	if !fg.Box.Valid || !fg.Uniform || fg.Coverage > 0.85 || fg.Box.W() >= small.W && fg.Box.H() >= small.H {
		return im, nil
	}
	f := float64(im.W) / float64(small.W)
	margin := int(math.Ceil(0.08 * f * float64(max(fg.Box.W(), fg.Box.H()))))
	x0 := max(0, int(float64(fg.Box.X0)*f)-margin)
	y0 := max(0, int(float64(fg.Box.Y0)*f)-margin)
	x1 := min(im.W, int(math.Ceil(float64(fg.Box.X1)*f))+margin)
	y1 := min(im.H, int(math.Ceil(float64(fg.Box.Y1)*f))+margin)
	if x1-x0 < 8 || y1-y0 < 8 {
		return im, nil
	}
	return im.Crop([4]float64{float64(x0) / float64(im.W), float64(y0) / float64(im.H), float64(x1-x0) / float64(im.W), float64(y1-y0) / float64(im.H)})
}

// blit dibuja la imagen sobre dst en (x0,y0), compuesta sobre blanco.
func blit(dst *image.NRGBA, im *Image, x0, y0 int) {
	rgb := im.flatten([3]uint8{255, 255, 255})
	for y := 0; y < im.H; y++ {
		for x := 0; x < im.W; x++ {
			i := 3 * (y*im.W + x)
			dst.SetNRGBA(x0+x, y0+y, color.NRGBA{rgb[i], rgb[i+1], rgb[i+2], 255})
		}
	}
}

func composeSide(ctx context.Context, photo, model *Image, cell int, o ComposeOptions) (*image.NRGBA, error) {
	const gap = 8
	out := image.NewNRGBA(image.Rect(0, 0, 3*gap+2*cell, 2*gap+cell))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.NRGBA{255, 255, 255, 255}), image.Point{}, draw.Src)
	for k, item := range []struct {
		im    *Image
		label string
	}{{photo, o.PhotoLabel}, {model, o.ModelLabel}} {
		x0, y0 := gap+k*(cell+gap), gap
		draw.Draw(out, image.Rect(x0, y0, x0+cell, y0+cell), image.NewUniform(panelBG), image.Point{}, draw.Src)
		src := item.im
		if !o.FullFrame {
			var err error
			if src, err = focusOnObject(ctx, src); err != nil {
				return nil, err
			}
		}
		fit, at, err := fitInto(ctx, src, cell)
		if err != nil {
			return nil, err
		}
		blit(out, fit, x0+at.X, y0+at.Y)
		frame(out, image.Rect(x0, y0, x0+cell, y0+cell))
		label(out, x0+6, y0+6, item.label)
	}
	return out, nil
}

func composeOverlay(ctx context.Context, photo, model *Image, cell int, o ComposeOptions) (*image.NRGBA, error) {
	pfg, err := photo.Foreground(ctx)
	if err != nil {
		return nil, err
	}
	mfg, err := model.Foreground(ctx)
	if err != nil {
		return nil, err
	}
	out := image.NewNRGBA(image.Rect(0, 0, cell, cell))
	draw.Draw(out, out.Bounds(), image.NewUniform(panelBG), image.Point{}, draw.Src)
	fit, at, err := fitInto(ctx, photo, cell)
	if err != nil {
		return nil, err
	}
	blit(out, fit, at.X, at.Y)

	pb, mb := pfg.Box, mfg.Box
	if !pb.Valid {
		pb = Box{0, 0, photo.W, photo.H, true}
	}
	if !mb.Valid {
		mb = Box{0, 0, model.W, model.H, true}
	}
	fs := float64(fit.W) / float64(photo.W) // foto original → lienzo
	// Escala modelo → foto: la caja del modelo cabe en la de la foto, sin deformarla.
	s := math.Min(float64(pb.W())/float64(mb.W()), float64(pb.H())/float64(mb.H()))
	pcx, pcy := float64(pb.X0+pb.X1)/2, float64(pb.Y0+pb.Y1)/2
	mcx, mcy := float64(mb.X0+mb.X1)/2, float64(mb.Y0+mb.Y1)/2
	inModel := func(cx, cy int) (int, bool) {
		px := (float64(cx-at.X) + 0.5) / fs
		py := (float64(cy-at.Y) + 0.5) / fs
		mx := mcx + (px-pcx)/s
		my := mcy + (py-pcy)/s
		ix, iy := int(math.Floor(mx)), int(math.Floor(my))
		if ix < 0 || iy < 0 || ix >= model.W || iy >= model.H || !mfg.Mask.Bits[iy*model.W+ix] {
			return 0, false
		}
		return iy*model.W + ix, true
	}
	edge := color.NRGBA{255, 0, 170, 255}
	for cy := at.Y; cy < at.Y+fit.H; cy++ {
		for cx := at.X; cx < at.X+fit.W; cx++ {
			idx, ok := inModel(cx, cy)
			if !ok {
				continue
			}
			_, l := inModel(cx-1, cy)
			_, r := inModel(cx+1, cy)
			_, u := inModel(cx, cy-1)
			_, d := inModel(cx, cy+1)
			if !(l && r && u && d) {
				out.SetNRGBA(cx, cy, edge)
				continue
			}
			base := out.NRGBAAt(cx, cy)
			a := float64(model.Pix[4*idx+3]) / 255 * 0.5
			out.SetNRGBA(cx, cy, color.NRGBA{
				uint8(float64(base.R)*(1-a) + float64(model.Pix[4*idx])*a),
				uint8(float64(base.G)*(1-a) + float64(model.Pix[4*idx+1])*a),
				uint8(float64(base.B)*(1-a) + float64(model.Pix[4*idx+2])*a),
				255,
			})
		}
	}
	frame(out, out.Bounds())
	label(out, 6, 6, o.PhotoLabel)
	return out, nil
}

func frame(dst *image.NRGBA, r image.Rectangle) {
	for x := r.Min.X; x < r.Max.X; x++ {
		dst.SetNRGBA(x, r.Min.Y, frameCol)
		dst.SetNRGBA(x, r.Max.Y-1, frameCol)
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		dst.SetNRGBA(r.Min.X, y, frameCol)
		dst.SetNRGBA(r.Max.X-1, y, frameCol)
	}
}

// label dibuja un rótulo en letra blanca sobre una barra oscura, al doble del
// tamaño de la fuente incluida (7×13), que es la única que no necesita archivos.
func label(dst *image.NRGBA, x, y int, text string) {
	if text == "" {
		return
	}
	face := basicfont.Face7x13
	small := image.NewNRGBA(image.Rect(0, 0, len(text)*7+8, 17))
	draw.Draw(small, small.Bounds(), image.NewUniform(color.NRGBA{0, 0, 0, 255}), image.Point{}, draw.Src)
	d := &font.Drawer{
		Dst: small, Src: image.NewUniform(color.NRGBA{255, 255, 255, 255}), Face: face,
		Dot: fixed.P(4, 13),
	}
	d.DrawString(text)
	const scale = 2
	for sy := 0; sy < small.Rect.Dy()*scale; sy++ {
		for sx := 0; sx < small.Rect.Dx()*scale; sx++ {
			c := small.NRGBAAt(sx/scale, sy/scale)
			bg := dst.NRGBAAt(x+sx, y+sy)
			const a = 0.78
			dst.SetNRGBA(x+sx, y+sy, color.NRGBA{
				uint8(float64(bg.R)*(1-a) + float64(c.R)*a),
				uint8(float64(bg.G)*(1-a) + float64(c.G)*a),
				uint8(float64(bg.B)*(1-a) + float64(c.B)*a),
				255,
			})
		}
	}
}
