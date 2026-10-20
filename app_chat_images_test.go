package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/clichat"
	"scadlab/backend/hermes"
	"scadlab/backend/store"
)

// Imágenes del chat en el workspace (S15, 15.2): al enviar, cada foto se copia a
// <ws>/refs/images/<nombre>-<hash8>.<ext>, sin pisar y deduplicada por contenido; la
// ficha guarda esa ruta y el modelo la lee al final del mensaje.

func refImages(t *testing.T, ws string) []string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(ws, "refs", "images"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// Hermes: la foto se copia, la ruta llega al modelo en el texto del turno y la misma
// foto enviada otra vez reutiliza el archivo.
func TestSendImageCopiesItToRefsImagesAndTellsTheModel(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Listo."}, {text: "Otra vez."}}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)

	att, err := a.AddChatAttachmentData(wsID, "Mi Pieza.png", pngB64(t))
	if err != nil {
		t.Fatal(err)
	}
	// Mientras es un chip pendiente todavía no está en el proyecto.
	if got := refImages(t, dir); len(got) != 0 {
		t.Fatalf("antes de enviar no hay copia: %v", got)
	}
	um, err := a.SendChatMessage(c.ID, "hazla en 3D", "", []string{att.ID}, SendOptions{Agent: boolp(false)})
	if err != nil {
		t.Fatal(err)
	}
	ev.wait(t)

	files := refImages(t, dir)
	if len(files) != 1 || !strings.HasPrefix(files[0], "mi-pieza-") || !strings.HasSuffix(files[0], ".png") {
		t.Fatalf("refs/images: %v", files)
	}
	rel := "refs/images/" + files[0]
	if um.Content != "hazla en 3D" {
		t.Errorf("lo guardado como texto es lo que escribió la persona: %q", um.Content)
	}
	// El modelo ve la foto (viaja como imagen) y lee la ruta al final del texto.
	last := sim.messages(0)
	content := last[len(last)-1]["content"].(string)
	if !strings.HasPrefix(content, "hazla en 3D") || !strings.HasSuffix(content, rel+"]") || !strings.Contains(content, "image tools") {
		t.Errorf("mensaje al modelo: %q", content)
	}
	if sim.imagesIn(0) != 1 {
		t.Errorf("la foto también viaja como imagen: %d", sim.imagesIn(0))
	}
	// La ficha guardada lleva la ruta, y la miniatura sigue ahí.
	v, _ := a.GetChat(c.ID)
	p := hermes.ParseParts(v.Messages[0].Parts)
	if len(p.Attachments) != 1 || p.Attachments[0].RefPath != rel || p.Attachments[0].Thumb == "" {
		t.Errorf("ficha: %+v", p.Attachments)
	}
	// Lo de <datos>/chat-attachments/<chat>/ sigue como antes (el historial y la miniatura).
	if ents, _ := os.ReadDir(a.chatDir(c.ID)); len(ents) != 1 {
		t.Errorf("la imagen del chat sigue en su carpeta: %v", ents)
	}

	// La misma foto en otro mensaje: mismo archivo, y el historial que se reenvía conserva la ruta.
	again, err := a.AddChatAttachmentData(wsID, "Mi Pieza.png", pngB64(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendChatMessage(c.ID, "ahora el lado", "", []string{again.ID}, SendOptions{Agent: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if got := refImages(t, dir); len(got) != 1 {
		t.Errorf("deduplicada por contenido: %v", got)
	}
	second := sim.messages(1)
	var history string
	for _, m := range second[:len(second)-1] {
		if s, _ := m["content"].(string); s != "" {
			history += s + "\n"
		}
	}
	if !strings.Contains(history, "Mi Pieza.png ("+rel+")") {
		t.Errorf("el historial nombra la ruta de la foto: %q", history)
	}
}

// «Usar trazado de contorno»: con un modelo que informó que no ve, la foto se envía igual
// pero solo como archivo.
func TestSendImagesAsFilesToAModelWithoutVision(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Voy a trazar el contorno."}}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsBlind)
	att, err := a.AddChatAttachmentData(wsID, "logo.png", pngB64(t))
	if err != nil {
		t.Fatal(err)
	}
	// Sin la opción se rechaza, como siempre.
	if _, err := a.SendChatMessage(c.ID, "traza el logo", "", []string{att.ID}, SendOptions{Agent: boolp(false)}); err == nil {
		t.Fatal("un modelo sin visión rechaza la imagen")
	}
	if got := refImages(t, dir); len(got) != 0 {
		t.Errorf("un envío rechazado no deja copias: %v", got)
	}
	if _, err := a.SendChatMessage(c.ID, "traza el logo", "", []string{att.ID}, SendOptions{Agent: boolp(false), ImagesAsFiles: true}); err != nil {
		t.Fatalf("como archivo debía enviarse: %v", err)
	}
	ev.wait(t)
	files := refImages(t, dir)
	if len(files) != 1 {
		t.Fatalf("refs/images: %v", files)
	}
	if sim.imagesIn(0) != 0 {
		t.Errorf("no viaja como imagen: %d", sim.imagesIn(0))
	}
	last := sim.messages(0)
	if content := last[len(last)-1]["content"].(string); !strings.Contains(content, "refs/images/"+files[0]) {
		t.Errorf("el modelo recibe la ruta: %q", content)
	}
}

func TestCliImagesAndReadDirs(t *testing.T) {
	ws := t.TempDir()
	chat := t.TempDir()
	p := hermes.Prepared{
		Images:    []string{filepath.Join(chat, "a.png"), filepath.Join(chat, "ref-1.png")},
		ImageRefs: []string{"refs/images/a-11223344.png", "refs/images/b-55667788.png"},
		Previews:  []string{filepath.Join(chat, "ref-1.png")},
	}
	got := cliImages(ws, p)
	want := []string{
		filepath.Join(absDir(ws), "refs", "images", "a-11223344.png"),
		filepath.Join(absDir(ws), "refs", "images", "b-55667788.png"),
		filepath.Join(chat, "ref-1.png"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("imágenes del CLI:\n%v\nquiero\n%v", got, want)
	}
	// Las fotos están dentro del cwd: no piden lectura extra; la vista previa del chat, sí.
	dirs := readDirsFor(absDir(ws), got)
	if len(dirs) != 1 || dirs[0] != chat {
		t.Errorf("carpetas de lectura: %v", dirs)
	}
	// Sin copias en el workspace (no se pidieron) van las de la carpeta del chat.
	if got := cliImages(ws, hermes.Prepared{Images: []string{"/x/a.png"}}); len(got) != 1 || got[0] != "/x/a.png" {
		t.Errorf("sin copias: %v", got)
	}
}

// El Turn que se arma para el CLI: las fotos de refs/images (absolutas, dentro del cwd) en
// Images y ninguna carpeta extra en ReadDirs; la vista previa de un modelo, en la del chat.
func TestCLITurnPassesPhotosFromTheWorkspace(t *testing.T) {
	ws, chat := t.TempDir(), t.TempDir()
	cwd := absDir(ws)
	photo := filepath.Join(cwd, "refs", "images", "frente-11223344.png")
	preview := filepath.Join(chat, "ref-att-9.png")
	j := chatJob{
		workspace: store.Workspace{Path: ws},
		cli:       &cliJob{det: clichat.Detected{ID: "claude"}, images: []string{photo}},
	}
	turn := (&App{}).cliTurn(j, "", "hola")
	if turn.Cwd != cwd || len(turn.Images) != 1 || turn.Images[0] != photo || len(turn.ReadDirs) != 0 {
		t.Errorf("solo la foto, sin carpetas extra: cwd=%s images=%v readDirs=%v", turn.Cwd, turn.Images, turn.ReadDirs)
	}
	j.cli.images = []string{photo, preview}
	turn = (&App{}).cliTurn(j, "", "hola")
	if len(turn.ReadDirs) != 1 || turn.ReadDirs[0] != chat {
		t.Errorf("la vista previa del chat sí pide su carpeta: %v", turn.ReadDirs)
	}
}

// El prompt de sistema nombra las fotos del turno y la skill: en Hermes (modo agente) y en el contexto que arma chatPromptContext.
func TestPromptContextAndSystemPromptNameThePhotos(t *testing.T) {
	ctx := chatPromptContext(store.Workspace{Name: "Pieza"}, "", "refs/images/a-11223344.png", "refs/images/b-55667788.png")
	if len(ctx.Images) != 2 || ctx.Images[0] != "refs/images/a-11223344.png" {
		t.Fatalf("Images: %v", ctx.Images)
	}
	if got := chatPromptContext(store.Workspace{Name: "Pieza"}, "").Images; len(got) != 0 {
		t.Errorf("sin fotos no hay Images: %v", got)
	}

	sim := newSim()
	sim.script = []simReply{{text: "Listo."}}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	att, err := a.AddChatAttachmentData(wsID, "pieza.png", pngB64(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendChatMessage(c.ID, "hazla en 3D", "", []string{att.ID}, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	files := refImages(t, dir)
	if len(files) != 1 {
		t.Fatalf("refs/images: %v", files)
	}
	sys := sim.text(0)
	if !strings.Contains(sys, "openscad-from-image") || !strings.Contains(sys, "refs/images/"+files[0]) {
		t.Errorf("el prompt de sistema debía nombrar la skill y la foto: %.600q", sys)
	}
}
