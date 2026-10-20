package hermes

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

func TestCopyImageToRefsNamesByContentAndDeduplicates(t *testing.T) {
	ws := t.TempDir()
	a := mustPNG(t, 8, 8)
	b := mustPNG(t, 9, 9)

	rel1, err := CopyImageToRefs(ws, "Mi Foto ÁÉ 01.PNG", "image/png", a)
	if err != nil {
		t.Fatal(err)
	}
	// Nombre legible sin acentos + 8 dígitos del contenido + extensión; ruta con «/».
	if !strings.HasPrefix(rel1, "refs/images/mi-foto-ae-01-") || !strings.HasSuffix(rel1, ".png") || len(rel1) != len("refs/images/mi-foto-ae-01-")+8+len(".png") {
		t.Fatalf("ruta: %q", rel1)
	}
	if got, err := os.ReadFile(filepath.Join(ws, filepath.FromSlash(rel1))); err != nil || !bytes.Equal(got, a) {
		t.Fatalf("contenido de %s: %v", rel1, err)
	}

	// La misma foto, con otro nombre de archivo: otro slug pero un solo archivo por nombre+contenido.
	again, err := CopyImageToRefs(ws, "Mi Foto ÁÉ 01.PNG", "image/png", a)
	if err != nil || again != rel1 {
		t.Fatalf("la misma foto se reutiliza: %q vs %q (%v)", again, rel1, err)
	}
	// Otra foto con el MISMO nombre no pisa la primera.
	rel2, err := CopyImageToRefs(ws, "Mi Foto ÁÉ 01.PNG", "image/png", b)
	if err != nil || rel2 == rel1 {
		t.Fatalf("otra foto, otro archivo: %q %q (%v)", rel1, rel2, err)
	}
	if got, _ := os.ReadFile(filepath.Join(ws, filepath.FromSlash(rel1))); !bytes.Equal(got, a) {
		t.Error("la primera foto se pisó")
	}
	// Dos archivos y ningún temporal.
	files := listDir(t, filepath.Join(ws, "refs", "images"))
	if len(files) != 2 {
		t.Errorf("archivos en refs/images: %v", files)
	}
	for _, f := range files {
		if strings.HasPrefix(f, ".tmp") {
			t.Errorf("quedó un temporal: %s", f)
		}
	}
}

func TestCopyImageToRefsNeverOverwritesACollision(t *testing.T) {
	ws := t.TempDir()
	raw := mustPNG(t, 6, 6)
	rel, err := CopyImageToRefs(ws, "x.png", "image/png", raw)
	if err != nil {
		t.Fatal(err)
	}
	// Alguien dejó otro contenido con el nombre que le tocaría: se alarga el hash.
	abs := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.WriteFile(abs, []byte("otra cosa"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel2, err := CopyImageToRefs(ws, "x.png", "image/png", raw)
	if err != nil || rel2 == rel {
		t.Fatalf("colisión: %q %q (%v)", rel, rel2, err)
	}
	if got, _ := os.ReadFile(abs); string(got) != "otra cosa" {
		t.Error("se pisó un archivo ajeno")
	}
	if got, _ := os.ReadFile(filepath.Join(ws, filepath.FromSlash(rel2))); !bytes.Equal(got, raw) {
		t.Error("la copia alargada no tiene la foto")
	}
}

func TestCopyImageToRefsConcurrentSameImageMakesOneFile(t *testing.T) {
	ws := t.TempDir()
	raw := mustPNG(t, 10, 10)
	var wg sync.WaitGroup
	rels := make([]string, 8)
	for i := range rels {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := CopyImageToRefs(ws, "p.png", "image/png", raw)
			if err != nil {
				t.Error(err)
			}
			rels[i] = r
		}()
	}
	wg.Wait()
	for _, r := range rels {
		if r != rels[0] {
			t.Fatalf("rutas distintas: %v", rels)
		}
	}
	if files := listDir(t, filepath.Join(ws, "refs", "images")); len(files) != 1 {
		t.Errorf("archivos: %v", files)
	}
}

func TestCopyImageToRefsRejectsWhatIsNotAnImage(t *testing.T) {
	ws := t.TempDir()
	if _, err := CopyImageToRefs(ws, "x.bmp", "image/bmp", []byte("BM")); err == nil {
		t.Error("un formato que no se admite")
	}
	if _, err := CopyImageToRefs(ws, "x.png", "image/png", nil); err == nil {
		t.Error("vacío")
	}
	if _, err := os.Stat(filepath.Join(ws, "refs")); err == nil {
		t.Error("no se crea refs/ para nada")
	}
}

// refs/ es un enlace que sale del workspace: no se escribe afuera.
func TestCopyImageToRefsStaysInsideTheWorkspace(t *testing.T) {
	ws, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(ws, "refs")); err != nil {
		t.Skip("sin enlaces simbólicos:", err)
	}
	if _, err := CopyImageToRefs(ws, "x.png", "image/png", mustPNG(t, 4, 4)); err == nil {
		t.Fatal("debía negarse")
	}
	if files, _ := os.ReadDir(outside); len(files) != 0 {
		t.Errorf("se escribió fuera del workspace: %v", files)
	}
}

func TestPrepareInCopiesPhotosAndTellsThePaths(t *testing.T) {
	staging := t.TempDir()
	ws := t.TempDir()
	chatDir := filepath.Join(t.TempDir(), "chat-attachments", "9")
	mk := func(id, name string) *Pending {
		path := filepath.Join(staging, id+".png")
		if err := os.WriteFile(path, mustPNG(t, 5, 7), 0o600); err != nil {
			t.Fatal(err)
		}
		return &Pending{Attachment: Attachment{ID: id, Kind: KindImage, Name: name, Mime: "image/png", Size: 80}, Staged: path}
	}
	front, side := mk("att-1", "frente.png"), mk("att-2", "lado.png")
	// Dos adjuntos con el mismo contenido: un solo archivo en refs/images.
	side2 := mk("att-3", "lado.png")

	p, err := PrepareIn("  haz esta pieza ", []*Pending{front, side, side2}, PrepareOptions{Dir: chatDir, Vision: true, WorkspaceDir: ws})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ImageRefs) != 3 || p.ImageRefs[1] != p.ImageRefs[2] || p.ImageRefs[0] == p.ImageRefs[1] {
		t.Fatalf("rutas: %v", p.ImageRefs)
	}
	if files := listDir(t, filepath.Join(ws, "refs", "images")); len(files) != 2 {
		t.Errorf("deduplicadas por hash: %v", files)
	}
	// Las imágenes siguen viajando desde la carpeta del chat y las fichas guardan su ruta.
	if len(p.Images) != 3 || filepath.Dir(p.Images[0]) != chatDir {
		t.Errorf("imágenes: %v", p.Images)
	}
	for i, m := range p.Metas {
		if m.RefPath != p.ImageRefs[i] {
			t.Errorf("ficha %d: refPath %q", i, m.RefPath)
		}
	}
	// La línea con las rutas cierra el texto, después de lo que escribió la persona.
	want := "haz esta pieza\n\n[Photos of this message, in the project (pass these paths to the image tools): " + strings.Join(p.ImageRefs, ", ") + "]"
	if p.Text != want {
		t.Errorf("texto:\n%q\nquiero\n%q", p.Text, want)
	}
	// El historial que se reenvía conserva las rutas.
	if note := AttachmentNote(p.Metas); !strings.Contains(note, "frente.png ("+p.ImageRefs[0]+")") {
		t.Errorf("nota: %q", note)
	}
}

func TestPrepareInImagesAsFilesDoesNotSendPixels(t *testing.T) {
	staging, ws := t.TempDir(), t.TempDir()
	path := filepath.Join(staging, "a.png")
	if err := os.WriteFile(path, mustPNG(t, 5, 7), 0o600); err != nil {
		t.Fatal(err)
	}
	img := &Pending{Attachment: Attachment{ID: "att-1", Kind: KindImage, Name: "logo.png", Mime: "image/png"}, Staged: path}
	p, err := PrepareIn("sigue el contorno", []*Pending{img}, PrepareOptions{Dir: t.TempDir(), WorkspaceDir: ws, ImagesAsFiles: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Images) != 0 || len(p.ImageRefs) != 1 || !strings.Contains(p.Text, p.ImageRefs[0]) {
		t.Errorf("solo como archivo: %+v", p)
	}
}

// Si una foto ya no está, ninguna se movió: se puede reintentar sin perder las otras.
func TestPrepareInFailsBeforeMovingAnything(t *testing.T) {
	staging, ws, chatDir := t.TempDir(), t.TempDir(), t.TempDir()
	ok := filepath.Join(staging, "ok.png")
	if err := os.WriteFile(ok, mustPNG(t, 5, 7), 0o600); err != nil {
		t.Fatal(err)
	}
	good := &Pending{Attachment: Attachment{Kind: KindImage, Name: "buena.png", Mime: "image/png"}, Staged: ok}
	gone := &Pending{Attachment: Attachment{Kind: KindImage, Name: "vieja.png", Mime: "image/png"}, Staged: filepath.Join(staging, "no-existe.png")}
	if _, err := PrepareIn("hola", []*Pending{good, gone}, PrepareOptions{Dir: chatDir, WorkspaceDir: ws}); err == nil || !strings.Contains(err.Error(), "vieja.png") {
		t.Fatalf("error: %v", err)
	}
	if _, err := os.Stat(ok); err != nil {
		t.Errorf("la buena se movió: %v", err)
	}
}
