package hermes

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

func pngOf(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.NoCompression}
	if err := enc.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// noisy es una foto «de teléfono»: ruido que no comprime, así el PNG pasa de 5 MB.
func noisy(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewSource(1))
	r.Read(img.Pix)
	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}
	return img
}

func sizeOf(t *testing.T, raw []byte) (int, int) {
	t.Helper()
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return cfg.Width, cfg.Height
}

func TestReduceImageLeavesSmallImagesAlone(t *testing.T) {
	raw := pngOf(t, noisy(300, 200))
	out, mime, reduced, err := ReduceImage(raw, "image/png")
	if err != nil || reduced || mime != "image/png" || !bytes.Equal(out, raw) {
		t.Fatalf("una imagen chica no se toca: reduced=%v mime=%s err=%v", reduced, mime, err)
	}
}

// Una foto grande y pesada (3000×2000, más de 5 MB) se reduce a ≤ 2048 px de lado,
// conserva la proporción y queda por debajo de 5 MB como JPEG.
func TestReduceImageShrinksLargePhotos(t *testing.T) {
	raw := pngOf(t, noisy(3000, 2000))
	if len(raw) <= MaxImageBytes {
		t.Fatalf("la foto de prueba tiene que pasar de 5 MB: %d", len(raw))
	}
	out, mime, reduced, err := ReduceImage(raw, "image/png")
	if err != nil || !reduced {
		t.Fatalf("reduced=%v err=%v", reduced, err)
	}
	if mime != "image/jpeg" || len(out) > MaxImageBytes {
		t.Fatalf("salida %s de %d bytes", mime, len(out))
	}
	w, h := sizeOf(t, out)
	if w != 2048 || h < 1364 || h > 1366 {
		t.Errorf("medidas %dx%d, se esperaba 2048×1365", w, h)
	}
}

// Liviana pero enorme en píxeles (un PNG plano de 4000 px): también se reduce.
func TestReduceImageShrinksByPixels(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4000, 1000))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	out, _, reduced, err := ReduceImage(buf.Bytes(), "image/png")
	if err != nil || !reduced {
		t.Fatalf("reduced=%v err=%v", reduced, err)
	}
	if w, h := sizeOf(t, out); w != 2048 || h != 512 {
		t.Errorf("medidas %dx%d", w, h)
	}
}

// Un logo con transparencia sigue siendo PNG (con su canal alfa) si cabe.
func TestReduceImageKeepsTransparency(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 3000, 3000))
	for y := 1000; y < 2000; y++ {
		for x := 1000; x < 2000; x++ {
			img.SetNRGBA(x, y, color.NRGBA{200, 30, 30, 255})
		}
	}
	var src bytes.Buffer
	if err := png.Encode(&src, img); err != nil {
		t.Fatal(err)
	}
	out, mime, reduced, err := ReduceImage(src.Bytes(), "image/png")
	if err != nil || !reduced || mime != "image/png" {
		t.Fatalf("reduced=%v mime=%s err=%v", reduced, mime, err)
	}
	dec, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := dec.At(5, 5).RGBA(); a != 0 {
		t.Errorf("la esquina tenía que seguir transparente (alfa %d)", a)
	}
	if dec.Bounds().Dx() != 2048 {
		t.Errorf("ancho %d", dec.Bounds().Dx())
	}
}

// Una «bomba» (la cabecera declara 60000×60000) se rechaza sin decodificarla.
func TestReduceImageRejectsPixelBombs(t *testing.T) {
	raw := pngOf(t, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	// IHDR empieza en el byte 8: largo(4) "IHDR"(4) ancho(4) alto(4)… y su CRC.
	binary.BigEndian.PutUint32(raw[16:], 60000)
	binary.BigEndian.PutUint32(raw[20:], 60000)
	binary.BigEndian.PutUint32(raw[29:], crc32.ChecksumIEEE(raw[12:29]))
	if _, _, _, err := ReduceImage(raw, "image/png"); err == nil {
		t.Fatal("una imagen de 3600 MP tenía que rechazarse")
	}
}

// Al enviar, refs/images/ recibe el ORIGINAL (no la copia reducida) y el modelo,
// la copia reducida. El original en preparación desaparece.
func TestPrepareStoresOriginalAndSendsReduced(t *testing.T) {
	stage, ws, chat := t.TempDir(), t.TempDir(), t.TempDir()
	orig := pngOf(t, noisy(3000, 2000))
	small, _, _, err := ReduceImage(orig, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(stage, "a1.jpg")
	original := filepath.Join(stage, "a1-original.png")
	if err := os.WriteFile(staged, small, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	pend := &Pending{Attachment: Attachment{ID: "a1", Kind: KindImage, Name: "foto.png", Mime: "image/png", Size: int64(len(orig))}, Staged: staged, Original: original}
	p, err := PrepareIn("copia esto", []*Pending{pend}, PrepareOptions{Dir: chat, Vision: true, WorkspaceDir: ws})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ImageRefs) != 1 || len(p.Images) != 1 {
		t.Fatalf("refs=%v images=%v", p.ImageRefs, p.Images)
	}
	inRefs, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(p.ImageRefs[0])))
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(inRefs) != sha256.Sum256(orig) {
		t.Error("refs/images/ tiene que guardar el original")
	}
	sent, err := os.ReadFile(p.Images[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sent, small) {
		t.Error("al modelo tiene que ir la copia reducida")
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Error("el original en preparación tenía que borrarse")
	}
}
