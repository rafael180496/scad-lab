package clichat

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTrustSettings(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "antigravity-cli", "settings.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAgyTrustedIsPerExactFolderAndOnlyInformational(t *testing.T) {
	proj := t.TempDir()
	home := filepath.Dir(proj)
	settings := writeTrustSettings(t, `{"trustedWorkspaces": ["`+filepath.ToSlash(proj)+`"]}`)
	if tr, known := AgyTrusted(settings, proj); !tr || !known {
		t.Errorf("la carpeta listada: %v %v", tr, known)
	}
	sub := filepath.Join(proj, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if tr, known := AgyTrusted(settings, sub); tr || !known {
		t.Errorf("una subcarpeta no hereda: %v %v", tr, known)
	}
	other := writeTrustSettings(t, `{"trustedWorkspaces": ["`+filepath.ToSlash(home)+`"]}`)
	if tr, known := AgyTrusted(other, proj); tr || !known {
		t.Errorf("el home en la lista no cubre sus subcarpetas: %v %v", tr, known)
	}
	// «Workspace Trust» apagado: no pide confianza.
	off := writeTrustSettings(t, `{"workspaceTrust": false}`)
	if tr, known := AgyTrusted(off, proj); !tr || !known {
		t.Errorf("workspaceTrust=false: %v %v", tr, known)
	}
}

func TestAgyTrustedUnknownWhenItCannotBeRead(t *testing.T) {
	proj := t.TempDir()
	if _, known := AgyTrusted("", proj); known {
		t.Error("sin ruta no se sabe")
	}
	if _, known := AgyTrusted(writeTrustSettings(t, `{no es json`), proj); known {
		t.Error("JSON roto: no se sabe")
	}
	if _, known := AgyTrusted(filepath.Join(t.TempDir(), "no-existe", "settings.json"), proj); known {
		t.Error("Antigravity nunca corrió aquí: no se sabe")
	}
	// Su carpeta existe pero nunca guardó confianza: sabido, sin confianza.
	dir := filepath.Join(t.TempDir(), "antigravity-cli")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if tr, known := AgyTrusted(filepath.Join(dir, "settings.json"), proj); tr || !known {
		t.Errorf("sin archivo pero Antigravity ya corrió: %v %v", tr, known)
	}
}

func TestAgySettingsPathIsUnderTheHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	want := filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
	if got := AgySettingsPath(); got != want {
		t.Errorf("%q, quería %q", got, want)
	}
}
