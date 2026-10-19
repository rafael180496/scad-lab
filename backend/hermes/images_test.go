package hermes

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

// mustPNG arma un PNG de w×h con una franja roja y el resto azul.
func mustPNG(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{30, 60, 200, 255}
			if x < w/2 {
				c = color.RGBA{220, 30, 30, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodeDataURL(t *testing.T, u, prefix string) image.Image {
	t.Helper()
	if !strings.HasPrefix(u, prefix) {
		t.Fatalf("data URL sin %q: %.40s", prefix, u)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(u, prefix))
	if err != nil {
		t.Fatal(err)
	}
	img, err := jpeg.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestThumbDataURLShrinksAndKeepsColors(t *testing.T) {
	u := ThumbDataURL(mustPNG(t, 400, 200))
	img := decodeDataURL(t, u, "data:image/jpeg;base64,")
	if b := img.Bounds(); b.Dx() != ThumbPx || b.Dy() != ThumbPx/2 {
		t.Errorf("miniatura de %v, esperaba %dx%d", b, ThumbPx, ThumbPx/2)
	}
	// Mitad izquierda roja, derecha azul: el filtro de caja conserva los colores.
	r, _, b, _ := img.At(10, 20).RGBA()
	if r>>8 < 150 || b>>8 > 100 {
		t.Errorf("a la izquierda debía ser rojo: r=%d b=%d", r>>8, b>>8)
	}
	r, _, b, _ = img.At(ThumbPx-10, 20).RGBA()
	if b>>8 < 130 || r>>8 > 100 {
		t.Errorf("a la derecha debía ser azul: r=%d b=%d", r>>8, b>>8)
	}
	// Una imagen chica no se agranda.
	small := decodeDataURL(t, ThumbDataURL(mustPNG(t, 40, 30)), "data:image/jpeg;base64,")
	if b := small.Bounds(); b.Dx() != 40 || b.Dy() != 30 {
		t.Errorf("no debía agrandar: %v", b)
	}
	if ThumbDataURL([]byte("no es una imagen")) != "" {
		t.Error("un archivo ilegible no tiene miniatura")
	}
}

func TestShrinkDataURL(t *testing.T) {
	in := "data:image/png;base64," + base64.StdEncoding.EncodeToString(mustPNG(t, 1400, 900))
	out := shrinkDataURL(in)
	img := decodeDataURL(t, out, "data:image/jpeg;base64,")
	if b := img.Bounds(); b.Dx() != StepImagePx || b.Dy() > StepImagePx {
		t.Errorf("tamaño guardado: %v", b)
	}
	if len(out) >= len(in) {
		t.Errorf("lo guardado debía ser más chico: %d vs %d", len(out), len(in))
	}
	for _, bad := range []string{"", "data:text/plain;base64,aGk=", "data:image/png;base64,@@@", "sin coma"} {
		if shrinkDataURL(bad) != "" {
			t.Errorf("%q debía descartarse", bad)
		}
	}
}
