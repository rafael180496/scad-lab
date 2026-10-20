package refmodel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putFile(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestImageFormatOf(t *testing.T) {
	for name, want := range map[string]string{
		"a.png": ImagePNG, "A.PNG": ImagePNG, "b.jpg": ImageJPEG, "b.JPEG": ImageJPEG, "c.gif": ImageGIF,
		"d.webp": ImageWebP, "e.stl": "", "f": "", "g.png.txt": "", "h.svg": "",
	} {
		if got := ImageFormatOf(name); got != want {
			t.Errorf("ImageFormatOf(%q) = %q, esperaba %q", name, got, want)
		}
	}
}

func TestResolveImage(t *testing.T) {
	ws := t.TempDir()
	putFile(t, filepath.Join(ws, "refs", "images", "foto-1.png"), []byte("x"))
	putFile(t, filepath.Join(ws, "refs", "suelta.JPG"), []byte("x"))
	putFile(t, filepath.Join(ws, "refs", "images", "ambas.png"), []byte("images"))
	putFile(t, filepath.Join(ws, "refs", "ambas.png"), []byte("refs"))
	putFile(t, filepath.Join(ws, "refs", "pieza.stl"), []byte("x"))
	putFile(t, filepath.Join(ws, "secreto.png"), []byte("x"))

	good := map[string]string{
		"refs/images/foto-1.png":    "refs/images/foto-1.png",
		"images/foto-1.png":         "refs/images/foto-1.png",
		"foto-1.png":                "refs/images/foto-1.png", // solo el nombre: se busca en refs/images/
		"refs/foto-1.png":           "refs/images/foto-1.png",
		"refs/suelta.JPG":           "refs/suelta.JPG",
		"suelta.JPG":                "refs/suelta.JPG",
		"  refs/images/foto-1.png ": "refs/images/foto-1.png",
		"ambas.png":                 "refs/ambas.png", // refs/ primero
		"refs/images/ambas.png":     "refs/images/ambas.png",
	}
	for in, wantRel := range good {
		abs, rel, err := ResolveImage(ws, in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if rel != wantRel {
			t.Errorf("%q: rel = %q, esperaba %q", in, rel, wantRel)
		}
		if !strings.HasSuffix(filepath.ToSlash(abs), wantRel) {
			t.Errorf("%q: abs = %q", in, abs)
		}
	}

	bad := []string{
		"", ".", "..", "../secreto.png", "refs/../secreto.png", "refs/images/../../secreto.png", "images/../foto-1.png",
		"/etc/passwd", "/tmp/foto.png", "C:/Users/x/foto.png", `refs\images\foto-1.png`, "refs/images/sub/foto.png",
		"images/images/foto-1.png", "refs/images/.oculta.png", ".oculta.png", "refs//foto-1.png", "foto-1.png\x00.png",
		"refs/images", "refs/images/", "otra/foto-1.png",
		"refs/pieza.stl",     // no es una imagen
		"refs/images/no.png", // no existe
		"secreto.png",        // está en la raíz del workspace, no en refs/
	}
	for _, in := range bad {
		if abs, _, err := ResolveImage(ws, in); err == nil {
			t.Errorf("%q debía rechazarse y dio %q", in, abs)
		}
	}
	// Sin refs/.
	if _, _, err := ResolveImage(t.TempDir(), "refs/images/foto.png"); err == nil {
		t.Error("sin carpeta refs/ debía fallar")
	}
	// Un modelo no es una imagen y una imagen no es un modelo (con un mensaje que lo dice).
	if _, _, err := ResolveRef(ws, "refs/images/foto-1.png"); err == nil || !strings.Contains(err.Error(), "image") {
		t.Errorf("ResolveRef con una imagen: %v", err)
	}
}

// Un enlace simbólico que saca la imagen (o refs/images entera) de refs/ se rechaza.
func TestResolveImageRejectsSymlinksOutOfRefs(t *testing.T) {
	ws := t.TempDir()
	outside := t.TempDir()
	putFile(t, filepath.Join(outside, "fuera.png"), []byte("x"))
	putFile(t, filepath.Join(ws, "refs", "images", "ok.png"), []byte("x"))
	if err := os.Symlink(filepath.Join(outside, "fuera.png"), filepath.Join(ws, "refs", "images", "enlace.png")); err != nil {
		t.Skip("sin enlaces simbólicos:", err)
	}
	if err := os.Symlink(filepath.Join(ws, "refs", "images", "ok.png"), filepath.Join(ws, "refs", "alias.png")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveImage(ws, "refs/images/enlace.png"); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Errorf("enlace que sale de refs/: %v", err)
	}
	if _, rel, err := ResolveImage(ws, "refs/alias.png"); err != nil || rel != "refs/alias.png" {
		t.Errorf("enlace que se queda dentro de refs/: %q %v", rel, err)
	}

	// refs/images es un enlace a una carpeta de fuera.
	ws2 := t.TempDir()
	putFile(t, filepath.Join(ws2, "refs", "x.txt"), []byte("x"))
	if err := os.Symlink(outside, filepath.Join(ws2, "refs", "images")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveImage(ws2, "refs/images/fuera.png"); err == nil {
		t.Error("refs/images apuntando fuera del workspace debía rechazarse")
	}
	// ... y tampoco se lista.
	list, err := List(ws2)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range list {
		if r.Kind == KindImage {
			t.Errorf("se listó una imagen de fuera de refs/: %+v", r)
		}
	}
	// Una refs/ que apunta fuera del workspace tampoco.
	ws3 := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws3, "refs")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ResolveImage(ws3, "refs/fuera.png"); err == nil {
		t.Error("refs/ apuntando fuera del workspace debía rechazarse")
	}
}

func TestReserveRefNeverOverwrites(t *testing.T) {
	ws := t.TempDir()
	var rels []string
	for i := 0; i < 3; i++ {
		f, rel, err := ReserveRef(ws, "Mi Logo (grande)", ".SVG")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString("v" + string(rune('1'+i))); err != nil {
			t.Fatal(err)
		}
		f.Close()
		rels = append(rels, rel)
	}
	want := []string{"refs/mi-logo-grande.svg", "refs/mi-logo-grande-2.svg", "refs/mi-logo-grande-3.svg"}
	for i := range want {
		if rels[i] != want[i] {
			t.Errorf("nombre %d = %q, esperaba %q", i, rels[i], want[i])
		}
	}
	if raw, _ := os.ReadFile(filepath.Join(ws, "refs", "mi-logo-grande.svg")); string(raw) != "v1" {
		t.Errorf("el primero se pisó: %q", raw)
	}
	// Nombres peligrosos quedan en un nombre simple dentro de refs/.
	for _, stem := range []string{"../../fuera", "/etc/x", "con", `a\b`} {
		f, rel, err := ReserveRef(ws, stem, "dat")
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		if strings.Count(rel, "/") != 1 || !strings.HasPrefix(rel, "refs/") || strings.Contains(rel, "..") {
			t.Errorf("%q → %q", stem, rel)
		}
	}
	// Un enlace simbólico puesto en el nombre libre no se sigue (creación exclusiva).
	outside := filepath.Join(t.TempDir(), "victima.txt")
	putFile(t, outside, []byte("intacto"))
	if err := os.Symlink(outside, filepath.Join(ws, "refs", "trampa.svg")); err == nil {
		f, rel, err := ReserveRef(ws, "trampa", "svg")
		if err != nil {
			t.Fatal(err)
		}
		f.WriteString("pisado")
		f.Close()
		if rel != "refs/trampa-2.svg" {
			t.Errorf("rel = %q", rel)
		}
		if raw, _ := os.ReadFile(outside); string(raw) != "intacto" {
			t.Errorf("se escribió a través del enlace: %q", raw)
		}
	}
}

func TestListIncludesImagesWithKind(t *testing.T) {
	ws := t.TempDir()
	putFile(t, filepath.Join(ws, "refs", "pieza.stl"), []byte("stl"))
	putFile(t, filepath.Join(ws, "refs", "plano.PNG"), []byte("png"))
	putFile(t, filepath.Join(ws, "refs", "images", "foto-ab12cd34.jpg"), []byte("jpg"))
	putFile(t, filepath.Join(ws, "refs", "images", ".oculta.png"), []byte("x"))
	putFile(t, filepath.Join(ws, "refs", "images", "nota.txt"), []byte("x"))
	putFile(t, filepath.Join(ws, "refs", "images", "sub", "x.png"), []byte("x"))
	putFile(t, filepath.Join(ws, "refs", "contorno.svg"), []byte("x")) // salida de trace_image_outline: no se lista

	got, err := List(ws)
	if err != nil {
		t.Fatal(err)
	}
	type row struct{ path, kind, format string }
	var rows []row
	for _, r := range got {
		rows = append(rows, row{r.Path, r.Kind, r.Format})
	}
	want := []row{
		{"refs/images/foto-ab12cd34.jpg", "image", "jpeg"},
		{"refs/pieza.stl", "model", "stl"},
		{"refs/plano.PNG", "image", "png"},
	}
	if len(rows) != len(want) {
		t.Fatalf("lista: %v", rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("fila %d = %v, esperaba %v", i, rows[i], want[i])
		}
	}
	// Solo imágenes, sin ningún modelo.
	ws2 := t.TempDir()
	putFile(t, filepath.Join(ws2, "refs", "images", "solo.webp"), []byte("x"))
	if l, err := List(ws2); err != nil || len(l) != 1 || l[0].Kind != KindImage || l[0].Bytes != 1 {
		t.Errorf("solo imágenes: %v %v", l, err)
	}
}
