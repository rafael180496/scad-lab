package clichat

import (
	"testing"
	"time"
)

// La detección se recuerda un rato y se vuelve a hacer al refrescar: un CLI
// recién instalado aparece sin reiniciar la app.
func TestDetectorCachesAndRefreshes(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	installed := map[string]string{"claude": "/bin/claude"}
	resolves := 0
	d := NewDetector(30*time.Second, func(c CLI) string { resolves++; return installed[c.ID] })
	d.now = func() time.Time { return now }

	list := d.List(false)
	if resolves != 4 || len(list) != 4 {
		t.Fatalf("la primera vez detecta los cuatro: %d, %+v", resolves, list)
	}
	if !list[0].Installed || list[0].ID != "claude" || list[0].Path != "/bin/claude" || list[1].Installed {
		t.Errorf("lista: %+v", list)
	}

	// Dentro de la ventana no vuelve a buscar, aunque el CLI ya esté instalado.
	installed["codex"] = "/bin/codex"
	now = now.Add(10 * time.Second)
	if got := d.List(false); got[1].Installed || resolves != 4 {
		t.Errorf("dentro de los 30 s se reutiliza: %+v (%d búsquedas)", got[1], resolves)
	}
	// Al abrir el selector se refresca a pedido.
	if got := d.List(true); !got[1].Installed || resolves != 8 {
		t.Errorf("refresh=true busca de nuevo: %+v (%d búsquedas)", got[1], resolves)
	}
	// Y sola, pasada la ventana.
	installed["opencode"] = "/bin/opencode"
	now = now.Add(31 * time.Second)
	if got := d.List(false); !got[3].Installed || resolves != 12 {
		t.Errorf("vencida la caché se vuelve a detectar: %+v (%d búsquedas)", got[3], resolves)
	}

	// Un CLI desinstalado deja de aparecer al refrescar.
	delete(installed, "claude")
	if got := d.List(true); got[0].Installed {
		t.Error("desinstalado")
	}
	if c, ok := d.Get("codex", false); !ok || !c.Installed {
		t.Errorf("Get: %+v %v", c, ok)
	}
	if _, ok := d.Get("nada", false); ok {
		t.Error("un id fuera del catálogo no existe")
	}
}

func TestCatalogAndProviderIDs(t *testing.T) {
	ids := []string{}
	for _, c := range Catalog() {
		ids = append(ids, c.ID)
		if c.Name == "" || c.Vendor == "" || c.Bin == "" {
			t.Errorf("entrada incompleta: %+v", c)
		}
	}
	if len(ids) != 4 || ids[0] != "claude" || ids[1] != "codex" || ids[2] != "antigravity" || ids[3] != "opencode" {
		t.Errorf("catálogo: %v", ids)
	}
	if ag, _ := Lookup("antigravity"); ag.Bin != "agy" || len(ag.AltBins) != 1 || ag.AltBins[0] != "gemini" {
		t.Errorf("Antigravity es `agy` y conserva el alias `gemini`: %+v", ag)
	}
	if ProviderID("claude") != "cli:claude" {
		t.Error("ProviderID")
	}
	for in, want := range map[string]string{"cli:claude": "claude", "cli:opencode": "opencode"} {
		if id, ok := ParseProviderID(in); !ok || id != want {
			t.Errorf("ParseProviderID(%q) = %q, %v", in, id, ok)
		}
	}
	for _, in := range []string{"claude", "cli:", "cli:gemini", "cli:nada", "srv-abc", "ollama", ""} {
		if _, ok := ParseProviderID(in); ok {
			t.Errorf("%q no es un CLI del catálogo", in)
		}
	}
	if !IsProviderID("cli:nada") || IsProviderID("ollama") {
		t.Error("IsProviderID mira solo la forma")
	}
	// Un CLI de la lista modifica una copia, no el catálogo.
	cat := Catalog()
	cat[0].Name = "otro"
	if Catalog()[0].Name == "otro" {
		t.Error("Catalog devuelve una copia")
	}
}

func TestEnvKeepsHomeAndExtendsPath(t *testing.T) {
	t.Setenv("HOME", "/home/prueba")
	env := Env("/opt/cli/bin/claude", "FOO=bar")
	var home, path string
	paths := 0
	for _, kv := range env {
		switch {
		case kv == "HOME=/home/prueba":
			home = kv
		case len(kv) > 5 && kv[:5] == "PATH=":
			path = kv[5:]
			paths++
		}
	}
	if home == "" {
		t.Error("sin HOME ningún CLI encuentra su sesión")
	}
	if paths != 1 {
		t.Errorf("PATH aparece %d veces", paths)
	}
	if len(path) < len("/opt/cli/bin:") || path[:len("/opt/cli/bin")] != "/opt/cli/bin" {
		t.Errorf("el directorio del propio ejecutable va primero: %q", path)
	}
	if env[len(env)-1] != "FOO=bar" {
		t.Error("las variables extra llegan")
	}
}
