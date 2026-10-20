//go:build !windows

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// Un CLI recibe las fotos desde refs/images/ del workspace (dentro de su carpeta de
// trabajo): las abre sin permiso extra, el prompt las nombra con su ruta absoluta y la
// línea con las rutas relativas, y no hace falta ningún --add-dir para ellas.
func TestCLIChatReceivesPhotosFromRefsImages(t *testing.T) {
	a, wsID, dir, ev, f := cliApp(t)
	f.install("claude", "claude-stream.jsonl")
	f.install("codex", "codex-stream.jsonl")

	for _, bin := range []string{"claude", "codex"} {
		c, _ := a.NewChat(wsID, "cli:"+bin, "")
		att, err := a.AddChatAttachmentData(wsID, "frente.png", pngB64(t))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.SendChatMessage(c.ID, "hazla en 3D", "", []string{att.ID}, SendOptions{}); err != nil {
			t.Fatal(err)
		}
		ev.wait(t)

		files := refImages(t, dir)
		if len(files) != 1 || !strings.HasPrefix(files[0], "frente-") {
			t.Fatalf("refs/images: %v", files)
		}
		rel := "refs/images/" + files[0]
		abs := filepath.Join(absDir(dir), "refs", "images", files[0])
		args := f.calls(bin)[0]
		joined := strings.Join(args, "\x00")
		// La ruta absoluta de la copia (la que el CLI abre) y la relativa (la de las herramientas).
		if !strings.Contains(joined, rel+"]") {
			t.Errorf("%s: falta la línea con la ruta relativa %s en %q", bin, rel, args)
		}
		if bin == "codex" {
			if !argHas(args, "-i", abs) {
				t.Errorf("codex recibe la foto por -i desde el workspace: %v", args)
			}
		} else if !strings.Contains(joined, abs) {
			t.Errorf("%s: el prompt nombra la ruta absoluta %s: %q", bin, abs, args)
		}
		// Nada de la carpeta del chat: la foto está dentro del cwd.
		if argHas(args, "--add-dir", a.chatDir(c.ID)) || strings.Contains(joined, a.chatDir(c.ID)) {
			t.Errorf("%s: las fotos del workspace no necesitan la carpeta del chat: %v", bin, args)
		}
	}
	if got := refImages(t, dir); len(got) != 1 {
		t.Errorf("la misma foto de dos chats es un solo archivo: %v", got)
	}
}
