package hermes

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClassifyByContentThenExtension(t *testing.T) {
	png := mustPNG(t, 4, 4)
	zip := []byte("PK\x03\x04rest-of-zip")
	cases := []struct {
		name string
		head []byte
		kind string
		mime string
		bad  string // fragmento del error esperado ("" = sin error)
	}{
		{"foto.png", png, KindImage, "image/png", ""},
		// El contenido manda: un PNG con extensión de modelo es una imagen.
		{"pieza.stl", png, KindImage, "image/png", ""},
		{"sin-extension", png, KindImage, "image/png", ""},
		{"soporte.stl", []byte("solid soporte\nfacet normal 0 0 1"), KindModel, "model/stl", ""},
		{"SOPORTE.STL", []byte{0, 0, 0, 0}, KindModel, "model/stl", ""},
		{"pieza.obj", []byte("v 0 0 0\n"), KindModel, "model/obj", ""},
		{"logo.3mf", zip, KindModel, "model/3mf", ""},
		{"falso.3mf", []byte("no es un zip"), "", "", "not a valid 3MF"},
		{"informe.pdf", []byte("%PDF-1.7"), "", "", "cannot be attached"},
		{"notas.txt", []byte("hola"), "", "", "cannot be attached"},
		{"icono.bmp", []byte("BM\x00\x00\x00\x00\x00\x00\x00\x00"), "", "", "unsupported image format"},
	}
	for _, c := range cases {
		kind, mime, err := Classify(c.name, c.head)
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("%s: error %v, esperaba %q", c.name, err, c.bad)
			}
			continue
		}
		if err != nil || kind != c.kind || mime != c.mime {
			t.Errorf("%s: %s %s %v (esperaba %s %s)", c.name, kind, mime, err, c.kind, c.mime)
		}
	}
}

func TestLimitsAndNames(t *testing.T) {
	if LimitFor(KindImage) != 20<<20 || LimitFor(KindModel) != 64<<20 {
		t.Errorf("topes: %d, %d", LimitFor(KindImage), LimitFor(KindModel))
	}
	err := ErrTooBig("grande.png", KindImage, MaxImageBytes)
	if !strings.Contains(err.Error(), "5 MB") || !strings.Contains(err.Error(), "grande.png") {
		t.Errorf("error: %v", err)
	}
	for in, want := range map[string]string{
		"../../etc/passwd":       "passwd",
		`C:\Users\yo\foto.png`:   "foto.png",
		"  ..oculto.stl.. ":      "oculto.stl",
		"":                       "adjunto",
		"a\x00b\x07c.png":        "abc.png",
		strings.Repeat("x", 300): strings.Repeat("x", 100),
	} {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q)=%q, esperaba %q", in, got, want)
		}
	}
	if got := FSName("mi pieza (1).stl"); got != "mi pieza _1_.stl" {
		t.Errorf("FSName: %q", got)
	}
	if FormatBytes(5<<20) != "5 MB" || FormatBytes(1536) != "1.5 KB" || FormatBytes(12) != "12 B" {
		t.Errorf("FormatBytes: %s %s %s", FormatBytes(5<<20), FormatBytes(1536), FormatBytes(12))
	}
}

func TestCheckVision(t *testing.T) {
	img := &Pending{Attachment: Attachment{Kind: KindImage, Name: "foto.png"}}
	model := &Pending{Attachment: Attachment{Kind: KindModel, Name: "pieza.stl"}}
	// Un modelo que informó que no ve rechaza la imagen…
	err := CheckVision([]*Pending{model, img}, false, true, "Ollama · llama3.2")
	if err == nil || !strings.Contains(err.Error(), "foto.png") || !strings.Contains(err.Error(), "Ollama · llama3.2") {
		t.Errorf("sin visión conocida: %v", err)
	}
	// …uno que no lo dice, no (es una suposición, no un «no»)…
	if err := CheckVision([]*Pending{img}, false, false, "x"); err != nil {
		t.Errorf("visión desconocida no bloquea: %v", err)
	}
	if err := CheckVision([]*Pending{img}, true, true, "x"); err != nil {
		t.Errorf("con visión: %v", err)
	}
	// …y un modelo de referencia nunca se rechaza: el informe es texto.
	if err := CheckVision([]*Pending{model}, false, true, "x"); err != nil {
		t.Errorf("un modelo 3D solo manda texto: %v", err)
	}
}

func TestPrepareMovesImagesAndBuildsTheMessage(t *testing.T) {
	staging, chatDir := t.TempDir(), filepath.Join(t.TempDir(), "chat-attachments", "9")
	staged := filepath.Join(staging, "adj-1.png")
	if err := os.WriteFile(staged, mustPNG(t, 4, 4), 0o600); err != nil {
		t.Fatal(err)
	}
	img := &Pending{Attachment: Attachment{ID: "att-1", Kind: KindImage, Name: "foto.png", Mime: "image/png", Size: 80, ThumbDataURL: "data:image/jpeg;base64,AAA"}, Staged: staged}
	model := &Pending{Attachment: Attachment{ID: "att-2", Kind: KindModel, Name: "soporte.stl", Mime: "model/stl", Size: 500, RefPath: "refs/soporte.stl", Summary: "Referencia: refs/soporte.stl"}, Preview: mustPNG(t, 8, 8)}

	p, err := Prepare("  haz un soporte como este ", []*Pending{img, model}, true, chatDir)
	if err != nil {
		t.Fatal(err)
	}
	// La imagen se movió a la carpeta del chat y la vista previa del modelo
	// viaja también (hay visión).
	if len(p.Images) != 2 || filepath.Dir(p.Images[0]) != chatDir || !strings.HasSuffix(p.Images[1], "ref-att-2.png") {
		t.Fatalf("imágenes: %v", p.Images)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Error("la imagen en preparación debía moverse, no copiarse")
	}
	for _, path := range p.Images {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("falta %s: %v", path, err)
		}
	}
	// El informe del modelo entra al mensaje, después de lo que escribió la persona.
	if p.Text != "haz un soporte como este\n\nReferencia: refs/soporte.stl" {
		t.Errorf("texto: %q", p.Text)
	}
	if len(p.Metas) != 2 || p.Metas[0].Thumb == "" || p.Metas[1].RefPath != "refs/soporte.stl" || p.Metas[1].Summary == "" {
		t.Errorf("metadatos: %+v", p.Metas)
	}
	if m := UserMessage(p); m.Role != "user" || m.Content != p.Text || len(m.Images) != 2 {
		t.Errorf("mensaje: %+v", m)
	}
}

func TestPrepareWithoutVisionSendsOnlyTheReport(t *testing.T) {
	model := &Pending{Attachment: Attachment{ID: "att-2", Kind: KindModel, Name: "soporte.stl", Summary: "Referencia: refs/soporte.stl"}, Preview: mustPNG(t, 8, 8)}
	p, err := Prepare("", []*Pending{model}, false, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Images) != 0 {
		t.Errorf("sin visión no viaja la vista previa: %v", p.Images)
	}
	if !strings.HasPrefix(p.Text, "Take a look at the attached files.") || !strings.Contains(p.Text, "refs/soporte.stl") {
		t.Errorf("texto: %q", p.Text)
	}
}

func TestPrepareErrors(t *testing.T) {
	gone := &Pending{Attachment: Attachment{Kind: KindImage, Name: "vieja.png"}, Staged: filepath.Join(t.TempDir(), "no-existe.png")}
	if _, err := Prepare("hola", []*Pending{gone}, true, t.TempDir()); err == nil || !strings.Contains(err.Error(), "vieja.png") {
		t.Errorf("imagen que ya no está: %v", err)
	}
	many := make([]*Pending, MaxAttachments+1)
	for i := range many {
		many[i] = &Pending{Attachment: Attachment{Kind: KindModel, Name: "x.stl"}}
	}
	if _, err := Prepare("hola", many, true, t.TempDir()); err == nil || !strings.Contains(err.Error(), "up to 10") {
		t.Errorf("demasiados: %v", err)
	}
}

func TestRegistryAndDiscard(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "a.png")
	ref := filepath.Join(dir, "refs-copy.stl")
	preexisting := filepath.Join(dir, "mine.stl")
	for _, p := range []string{staged, ref, preexisting} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r := NewRegistry()
	r.Add(&Pending{Attachment: Attachment{ID: "a"}, Staged: staged})
	r.Add(&Pending{Attachment: Attachment{ID: "b"}, RefAbs: ref, Owned: true})
	r.Add(&Pending{Attachment: Attachment{ID: "c"}, RefAbs: preexisting, Owned: false})
	if _, ok := r.Get("a"); !ok {
		t.Fatal("falta a")
	}
	for _, id := range []string{"a", "b", "c"} {
		p, ok := r.Take(id)
		if !ok {
			t.Fatalf("Take(%s)", id)
		}
		p.Discard()
	}
	if _, ok := r.Take("a"); ok {
		t.Error("Take quita del registro")
	}
	for _, gone := range []string{staged, ref} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s debía borrarse", gone)
		}
	}
	// Una referencia que ya estaba en refs/ (no la creó el adjunto) nunca se borra.
	if _, err := os.Stat(preexisting); err != nil {
		t.Errorf("no debía borrar una referencia ajena: %v", err)
	}
}

func TestRegistryExpired(t *testing.T) {
	r := NewRegistry()
	r.Add(&Pending{Attachment: Attachment{ID: "viejo"}})
	r.mu.Lock()
	r.m["viejo"].At = time.Now().Add(-48 * time.Hour)
	r.mu.Unlock()
	r.Add(&Pending{Attachment: Attachment{ID: "nuevo"}})
	exp := r.Expired(24 * time.Hour)
	if len(exp) != 1 || exp[0].ID != "viejo" {
		t.Fatalf("vencidos: %+v", exp)
	}
	if _, ok := r.Get("nuevo"); !ok {
		t.Error("el reciente se queda")
	}
	if _, ok := r.Get("viejo"); ok {
		t.Error("el vencido se quita")
	}
}

func TestPruneDir(t *testing.T) {
	dir := t.TempDir()
	old, fresh := filepath.Join(dir, "old.png"), filepath.Join(dir, "fresh.png")
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	PruneDir(dir, 24*time.Hour)
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Error("el viejo debía borrarse")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("el reciente se queda")
	}
}
