package imagetools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"image/color"
	"image/gif"
	"image/jpeg"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// pngHeaderOnly arma un PNG que solo tiene la cabecera IHDR de w×h: lo que lee
// image.DecodeConfig. Si el límite de píxeles se comprobara DESPUÉS de decodificar,
// esta imagen fallaría con «truncada»; si se comprueba antes, falla con el límite.
func pngHeaderOnly(w, h uint32) []byte {
	var b bytes.Buffer
	b.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 0 // 8 bits, gris
	chunk := append([]byte("IHDR"), ihdr...)
	_ = binary.Write(&b, binary.BigEndian, uint32(13))
	b.Write(chunk)
	_ = binary.Write(&b, binary.BigEndian, crc32.ChecksumIEEE(chunk))
	return b.Bytes()
}

func TestLimitsCheckedBeforeDecoding(t *testing.T) {
	dir := t.TempDir()
	// Demasiados píxeles: 8000×8000 = 64 MP, declarados en una cabecera de 33 bytes.
	big := filepath.Join(dir, "gigante.png")
	if err := os.WriteFile(big, pngHeaderOnly(8000, 8000), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	_, err := Load(context.Background(), big)
	if err == nil || !strings.Contains(err.Error(), "too many pixels") {
		t.Fatalf("esperaba el límite de píxeles, llegó: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("rechazar tardó %v: se decodificó antes de mirar", time.Since(start))
	}
	// Justo en el límite (40 MP) pasa la comprobación y falla después por truncada.
	ok := filepath.Join(dir, "justo.png")
	if err := os.WriteFile(ok, pngHeaderOnly(8000, 5000), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), ok); err == nil || strings.Contains(err.Error(), "too many pixels") {
		t.Errorf("40 MP exactos no deben rechazarse por tamaño: %v", err)
	}

	// Demasiado pesada.
	heavy := filepath.Join(dir, "pesada.png")
	if err := os.WriteFile(heavy, make([]byte, MaxBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), heavy); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Errorf("archivo de más de 20 MB: %v", err)
	}
	// No es una imagen.
	txt := filepath.Join(dir, "nota.png")
	_ = os.WriteFile(txt, []byte("esto no es una imagen"), 0o644)
	if _, err := Load(context.Background(), txt); err == nil || !strings.Contains(err.Error(), "unrecognized format") {
		t.Errorf("no es una imagen: %v", err)
	}
	// Directorio.
	if _, err := Load(context.Background(), dir); err == nil {
		t.Error("un directorio no es una imagen")
	}
}

// Se trabaja reducido a 2048 px; las medidas «originales» son las del archivo.
func TestLoadReducesToWorkingSize(t *testing.T) {
	dir := t.TempDir()
	p := writePNG(t, dir, "ancha.png", paint(3000, 1000, func(x, y float64) color.NRGBA {
		if x < 1500 {
			return color.NRGBA{200, 0, 0, 255}
		}
		return color.NRGBA{0, 0, 200, 255}
	}))
	im, err := Load(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if im.W != 2048 || im.H != 682 || im.OrigW != 3000 || im.OrigH != 1000 || im.Format != FormatPNG {
		t.Errorf("trabajo %d×%d, original %d×%d, formato %s", im.W, im.H, im.OrigW, im.OrigH, im.Format)
	}
	// El color se conserva al promediar.
	if im.Pix[4*(300*im.W+100)] != 200 || im.Pix[4*(300*im.W+1900)+2] != 200 {
		t.Error("el promedio por área alteró los colores")
	}
	info, err := im.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.WidthPx != 3000 || info.HeightPx != 1000 || info.Aspect != 3 || info.WorkWidth != 2048 {
		t.Errorf("info: %+v", info)
	}
}

// JPEG, GIF y WebP (Go puro) se leen; el formato sale del contenido, no de la extensión.
func TestDecodeFormats(t *testing.T) {
	src := paint(40, 30, circle(20, 15, 10, color.NRGBA{200, 50, 50, 255}, white))
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	var gb bytes.Buffer
	if err := gif.Encode(&gb, src, nil); err != nil {
		t.Fatal(err)
	}
	// Un WebP sin pérdida de 1×1 (rojo), lo mínimo para probar el decodificador.
	webp, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		data   []byte
		format string
		w, h   int
	}{"jpeg": {jb.Bytes(), FormatJPEG, 40, 30}, "gif": {gb.Bytes(), FormatGIF, 40, 30}, "webp": {webp, FormatWebP, 1, 1}} {
		im, err := Decode(context.Background(), c.data)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if im.Format != c.format || im.OrigW != c.w || im.OrigH != c.h {
			t.Errorf("%s: formato %s %d×%d", name, im.Format, im.OrigW, im.OrigH)
		}
	}
}

// Un JPEG de teléfono guarda la foto de lado y anota cómo girarla en EXIF:
// se aplica, y las medidas «originales» ya son las de la foto derecha.
func TestEXIFOrientationIsApplied(t *testing.T) {
	// 40×20: izquierda roja, derecha azul.
	src := paint(40, 20, func(x, y float64) color.NRGBA {
		if x < 20 {
			return color.NRGBA{255, 0, 0, 255}
		}
		return color.NRGBA{0, 0, 255, 255}
	})
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, src, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	raw := jb.Bytes()
	// APP1 con una IFD de una entrada: Orientation = 6 (girar 90° horario).
	var tiff bytes.Buffer
	tiff.WriteString("MM\x00\x2a\x00\x00\x00\x08")
	tiff.Write([]byte{0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, 6, 0, 0, 0, 0, 0, 0})
	seg := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	app1 := append([]byte{0xFF, 0xE1, byte((len(seg) + 2) >> 8), byte(len(seg) + 2)}, seg...)
	withExif := append(append(append([]byte{}, raw[:2]...), app1...), raw[2:]...)

	im, err := Decode(context.Background(), withExif)
	if err != nil {
		t.Fatal(err)
	}
	if im.Orientation != 6 || im.W != 20 || im.H != 40 || im.OrigW != 20 || im.OrigH != 40 {
		t.Fatalf("orientación %d, %d×%d (orig %d×%d)", im.Orientation, im.W, im.H, im.OrigW, im.OrigH)
	}
	// Girada 90° horario: lo que estaba a la izquierda (rojo) queda arriba.
	top, bottom := im.Pix[4*(5*im.W+10):], im.Pix[4*(35*im.W+10):]
	if top[0] < 200 || top[2] > 60 || bottom[2] < 200 || bottom[0] > 60 {
		t.Errorf("arriba %v, abajo %v: esperaba rojo arriba y azul abajo", top[:3], bottom[:3])
	}
	// Sin EXIF queda como estaba.
	plain, _ := Decode(context.Background(), raw)
	if plain.W != 40 || plain.Orientation != 1 {
		t.Errorf("sin EXIF: %d×%d o=%d", plain.W, plain.H, plain.Orientation)
	}
}

// image_info: tamaño, proporción, colores dominantes y caja del primer plano.
func TestDescribeForegroundAndColors(t *testing.T) {
	// Pieza roja de 100×60 px dentro de una foto de 400×300 sobre fondo gris claro.
	bg := color.NRGBA{235, 235, 235, 255}
	img := paint(400, 300, func(x, y float64) color.NRGBA {
		if x >= 150 && x < 250 && y >= 120 && y < 180 {
			return color.NRGBA{200, 30, 30, 255}
		}
		return bg
	})
	info, err := load(t, img).Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Foreground == nil {
		t.Fatal("sin primer plano")
	}
	b := info.Foreground.Box
	want := [4]float64{150.0 / 400, 120.0 / 300, 100.0 / 400, 60.0 / 300}
	for i := range b {
		if d := b[i] - want[i]; d > 0.01 || d < -0.01 {
			t.Errorf("caja[%d] = %.3f, esperaba %.3f (%v)", i, b[i], want[i], b)
		}
	}
	if a := info.Foreground.Aspect; a < 1.6 || a > 1.72 {
		t.Errorf("proporción del primer plano %.3f, esperaba ≈ 1,667", a)
	}
	if info.Background.Hex != "#EBEBEB" || !info.Background.Uniform {
		t.Errorf("fondo: %+v", info.Background)
	}
	if len(info.Dominant) < 2 || info.Dominant[0].Hex != "#EBEBEB" || info.Dominant[0].Percent < 90 {
		t.Errorf("dominantes: %+v", info.Dominant)
	}
	if info.Foreground.Coverage < 0.04 || info.Foreground.Coverage > 0.06 {
		t.Errorf("cobertura: %.3f", info.Foreground.Coverage)
	}
}

func TestCropAndEnhance(t *testing.T) {
	// Gradiente de poco contraste (100–150).
	img := paint(200, 100, func(x, y float64) color.NRGBA {
		v := uint8(100 + x/200*50)
		return color.NRGBA{v, v, v, 255}
	})
	im := load(t, img)
	region, err := im.Crop([4]float64{0.5, 0.25, 0.25, 0.5})
	if err != nil {
		t.Fatal(err)
	}
	if region.W != 50 || region.H != 50 {
		t.Errorf("recorte %d×%d, esperaba 50×50", region.W, region.H)
	}
	enh := im.Enhance()
	lo, hi := 255, 0
	for i := 0; i < enh.W*enh.H; i++ {
		lo, hi = min(lo, int(enh.Pix[4*i])), max(hi, int(enh.Pix[4*i]))
	}
	if lo > 5 || hi < 250 {
		t.Errorf("realce: rango %d–%d, esperaba ≈ 0–255", lo, hi)
	}
	// Ampliar un recorte de 50 px: hasta 4×, no más.
	big, err := region.FitSide(context.Background(), 800)
	if err != nil || big.W != 200 {
		t.Errorf("ampliación: %v", err)
	}
	if big, err = region.FitSide(context.Background(), 120); err != nil || big.W != 120 {
		t.Errorf("ampliación a 120: %v", err)
	}
	for _, bad := range [][4]float64{{0, 0, 0, 1}, {0.5, 0.5, 0.6, 0.2}, {-0.1, 0, 0.5, 0.5}, {0, 0, 1.5, 1}} {
		if _, err := im.Crop(bad); err == nil {
			t.Errorf("la región %v es inválida", bad)
		}
	}
	// Cabe en un PNG.
	if png, err := enh.PNG(); err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Errorf("PNG: %v", err)
	}
}

// Mapa de alturas: dimensiones, rango 0–100, `invert` y la orientación del
// archivo (lo de arriba de la foto va en la última fila, que surface() pone en +Y).
func TestHeightmap(t *testing.T) {
	// 256×128: gradiente horizontal de negro a blanco.
	grad := paint(256, 128, func(x, y float64) color.NRGBA {
		v := uint8(x)
		return color.NRGBA{v, v, v, 255}
	})
	im := load(t, grad)
	h, err := BuildHeightmap(context.Background(), im, HeightmapOptions{WidthMM: 80, MaxHeightMM: 3})
	if err != nil {
		t.Fatal(err)
	}
	if h.Cols != 200 || h.Rows != 100 {
		t.Errorf("dimensiones %d×%d, esperaba 200×100", h.Cols, h.Rows)
	}
	if h.HeightMM != 40 || h.BaseMM != DefaultBaseMM {
		t.Errorf("alto %.1f, base %.2f", h.HeightMM, h.BaseMM)
	}
	if h.Min < 0 || h.Min > 1 || h.Max < 99 || h.Max > 100 {
		t.Errorf("rango %.1f–%.1f, esperaba ≈ 0–100", h.Min, h.Max)
	}
	if h.Values[0] > 2 || h.Values[h.Cols-1] < 98 {
		t.Errorf("el gradiente va de oscuro a claro: %.1f … %.1f", h.Values[0], h.Values[h.Cols-1])
	}
	inv, err := BuildHeightmap(context.Background(), im, HeightmapOptions{WidthMM: 80, MaxHeightMM: 3, Invert: true})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Values[0] < 98 || inv.Values[inv.Cols-1] > 2 {
		t.Errorf("invertido: %.1f … %.1f", inv.Values[0], inv.Values[inv.Cols-1])
	}
	for i := range h.Values {
		if d := h.Values[i] + inv.Values[i] - 100; d > 0.2 || d < -0.2 {
			t.Fatalf("invert no es 100 - v en %d: %.1f + %.1f", i, h.Values[i], inv.Values[i])
		}
	}

	// El archivo: cabecera de comentario, una fila por línea, mismas columnas.
	dat := string(h.DAT())
	lines := strings.Split(strings.TrimSpace(dat), "\n")
	if !strings.HasPrefix(lines[0], "#") || len(lines)-1 != 100 || len(strings.Fields(lines[1])) != 200 {
		t.Fatalf("formato del .dat: %d líneas, %d columnas", len(lines)-1, len(strings.Fields(lines[1])))
	}
	for _, f := range strings.Fields(strings.Join(lines[1:], " ")) {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil || v < 0 || v > 100 {
			t.Fatalf("valor fuera de 0–100: %q", f)
		}
	}
	// Mitad de arriba clara y de abajo oscura: la última línea del archivo es la de arriba.
	half := load(t, paint(100, 100, func(x, y float64) color.NRGBA {
		if y < 50 {
			return white
		}
		return black
	}))
	hh, err := BuildHeightmap(context.Background(), half, HeightmapOptions{WidthMM: 50, MaxHeightMM: 2, Resolution: 20})
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(hh.DAT())), "\n")[1:]
	first, last := strings.Fields(rows[0])[5], strings.Fields(rows[len(rows)-1])[5]
	if first != "0" || last != "100" {
		t.Errorf("primera fila del archivo %s (abajo de la foto, oscura), última %s (arriba, clara)", first, last)
	}
}

func TestHeightmapLimitsAndStretch(t *testing.T) {
	im := load(t, paint(100, 100, func(x, y float64) color.NRGBA {
		v := uint8(100 + x/100*40)
		return color.NRGBA{v, v, v, 255}
	}))
	ctx := context.Background()
	for name, o := range map[string]HeightmapOptions{
		"sin ancho":      {MaxHeightMM: 2},
		"sin altura":     {WidthMM: 20},
		"resolución":     {WidthMM: 20, MaxHeightMM: 2, Resolution: 501},
		"base negativa":  {WidthMM: 20, MaxHeightMM: 2, BaseMM: -1},
		"ancho absurdo":  {WidthMM: 1e9, MaxHeightMM: 2},
		"resolución neg": {WidthMM: 20, MaxHeightMM: 2, Resolution: -3},
	} {
		if _, err := BuildHeightmap(ctx, im, o); err == nil {
			t.Errorf("%s: esperaba un error", name)
		}
	}
	plain, _ := BuildHeightmap(ctx, im, HeightmapOptions{WidthMM: 20, MaxHeightMM: 2})
	str, _ := BuildHeightmap(ctx, im, HeightmapOptions{WidthMM: 20, MaxHeightMM: 2, Stretch: true})
	if plain.Max-plain.Min > 25 || str.Max-str.Min < 90 {
		t.Errorf("sin estirar %.1f–%.1f, estirado %.1f–%.1f", plain.Min, plain.Max, str.Min, str.Max)
	}
	// Una imagen muy alta no pasa de 500 filas.
	tall := load(t, paint(60, 900, func(x, y float64) color.NRGBA { return white }))
	h, err := BuildHeightmap(ctx, tall, HeightmapOptions{WidthMM: 20, MaxHeightMM: 2, Resolution: 60})
	if err != nil || h.Rows > MaxHeightmapSide || h.Cols > MaxHeightmapSide {
		t.Errorf("imagen alta: %v %v", h, err)
	}
}

// Composición: tamaño, la mitad izquierda es la foto y la derecha el modelo.
func TestComposeSideBySide(t *testing.T) {
	photo := load(t, paint(300, 200, func(x, y float64) color.NRGBA { return color.NRGBA{200, 30, 30, 255} }))
	model := load(t, paint(200, 300, func(x, y float64) color.NRGBA { return color.NRGBA{30, 30, 200, 255} }))
	out, err := Compose(context.Background(), photo, model, ComposeOptions{PhotoLabel: "PHOTO", ModelLabel: "MODEL (front)"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Bounds().Dx() != 3*8+2*560 || out.Bounds().Dy() != 2*8+560 {
		t.Errorf("tamaño %v", out.Bounds())
	}
	left := out.NRGBAAt(out.Bounds().Dx()/4, out.Bounds().Dy()/2)
	right := out.NRGBAAt(3*out.Bounds().Dx()/4, out.Bounds().Dy()/2)
	if left.R < 180 || left.B > 60 {
		t.Errorf("la mitad izquierda debía ser la foto (roja): %v", left)
	}
	if right.B < 180 || right.R > 60 {
		t.Errorf("la mitad derecha debía ser el modelo (azul): %v", right)
	}
	// La foto apaisada se centra en su panel: arriba y abajo sobra panel gris.
	pad := out.NRGBAAt(out.Bounds().Dx()/4, 8+30)
	if pad != panelBG {
		t.Errorf("fuera de la foto debía verse el panel: %v", pad)
	}
	// Los rótulos dibujan algo (hay píxeles oscuros arriba a la izquierda del panel).
	dark := 0
	for y := 8 + 6; y < 8+6+24; y++ {
		for x := 8 + 6; x < 8+6+60; x++ {
			if c := out.NRGBAAt(x, y); c.R < 100 {
				dark++
			}
		}
	}
	if dark < 200 {
		t.Errorf("no se dibujó el rótulo (%d píxeles oscuros)", dark)
	}

	ov, err := Compose(context.Background(), photo, model, ComposeOptions{Mode: ModeOverlay, PhotoLabel: "PHOTO + MODEL", Cell: 400})
	if err != nil {
		t.Fatal(err)
	}
	if ov.Bounds().Dx() != 400 || ov.Bounds().Dy() != 400 {
		t.Errorf("superpuesta %v", ov.Bounds())
	}
	if _, err := Compose(context.Background(), photo, model, ComposeOptions{Mode: "raro"}); err == nil {
		t.Error("un modo desconocido es un error")
	}
}

// La superposición alinea las cajas: una silueta idéntica a la de la foto, de
// otro tamaño, cae encima de ella.
func TestComposeOverlayAlignsByBox(t *testing.T) {
	// Foto: círculo negro grande descentrado sobre blanco.
	photo := load(t, paint(400, 300, circle(120, 150, 80, black, white)))
	// Modelo: el mismo círculo, pequeño y en otro lugar, en rojo.
	model := load(t, paint(100, 100, circle(70, 30, 20, color.NRGBA{255, 0, 0, 255}, white)))
	out, err := Compose(context.Background(), photo, model, ComposeOptions{Mode: ModeOverlay, Cell: 400})
	if err != nil {
		t.Fatal(err)
	}
	// El centro del círculo de la foto (120,150) está en el lienzo en (120,150)+desplazamiento.
	cx, cy := 120+0, 150+(400-300)/2
	c := out.NRGBAAt(cx, cy)
	if c.R < 100 || c.G > 80 {
		t.Errorf("el centro del círculo de la foto debería llevar el rojo del modelo encima (mezclado con negro): %v", c)
	}
	far := out.NRGBAAt(350, 100+50)
	if far.R < 200 || far.G < 200 {
		t.Errorf("lejos del círculo debía verse la foto blanca: %v", far)
	}
}

func TestSilhouetteIoU(t *testing.T) {
	mk := func(w, h, x0, y0, x1, y1 int) *Mask {
		m := newMask(w, h)
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				m.Bits[y*w+x] = true
			}
		}
		return m
	}
	a := mk(200, 200, 20, 50, 120, 110) // 100×60
	b := mk(300, 100, 100, 20, 200, 80) // 100×60 en otro lienzo y lugar: misma forma
	c := mk(200, 200, 50, 20, 110, 120) // 60×100: la proporción cambia
	if iou := SilhouetteIoU(a, b); iou < 0.97 {
		t.Errorf("la misma forma en otro lugar: IoU = %.3f", iou)
	}
	if iou := SilhouetteIoU(a, c); iou > 0.5 {
		t.Errorf("proporciones distintas: IoU = %.3f (debía ser baja)", iou)
	}
	if SilhouetteIoU(a, newMask(10, 10)) != 0 {
		t.Error("una silueta vacía da 0")
	}
}
