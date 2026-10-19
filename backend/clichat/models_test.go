package clichat

import (
	"os"
	"path/filepath"
	"testing"

	"scadlab/backend/i18n"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fakeHome aísla la prueba del directorio del usuario real.
func fakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	old := homeDir
	homeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { homeDir = old; ResetModelsCache() })
	ResetModelsCache()
	return home
}

func TestClaudeModelsAreItsOwnAliases(t *testing.T) {
	fakeHome(t)
	ms := Models("claude", false)
	ids := []string{}
	for _, m := range ms {
		ids = append(ids, m.ID)
		if m.Label == "" || m.Description == "" {
			t.Errorf("%+v", m)
		}
	}
	if len(ids) != 4 || ids[0] != "opus" || ids[3] != "fable" {
		t.Errorf("alias: %v", ids)
	}
	// «Por defecto» no es parte de la lista: lo pone quien la muestra.
	for _, m := range ms {
		if m.ID == "" {
			t.Error("la lista no trae la opción vacía")
		}
	}
}

func TestCodexModelsComeFromItsCacheAndSkipHidden(t *testing.T) {
	home := fakeHome(t)
	writeFile(t, filepath.Join(home, ".codex", "models_cache.json"), `{"models":[
		{"slug":"gpt-5.2-codex","display_name":"GPT-5.2 Codex","description":"Para código","visibility":"list"},
		{"slug":"gpt-viejo","display_name":"Viejo","visibility":"hide"},
		{"slug":"gpt-5.2","display_name":"","description":"General","visibility":"list"},
		{"slug":"--malo","display_name":"x","visibility":"list"}]}`)
	ms := Models("codex", false)
	if len(ms) != 2 || ms[0].ID != "gpt-5.2-codex" || ms[0].Label != "GPT-5.2 Codex" || ms[0].Description != "Para código" || ms[1].Label != "gpt-5.2" {
		t.Fatalf("%+v", ms)
	}
}

func TestCodexModelsWithoutCacheAreEmptyNotAnError(t *testing.T) {
	fakeHome(t)
	if ms := Models("codex", false); len(ms) != 0 {
		t.Errorf("%+v", ms)
	}
	if ms := Models("nada", false); ms != nil {
		t.Errorf("%+v", ms)
	}
}

func TestOpencodeModelsOnlyOfProvidersWithCredentials(t *testing.T) {
	home := fakeHome(t)
	writeFile(t, filepath.Join(home, ".cache", "opencode", "models.json"), `{
		"anthropic":{"name":"Anthropic","models":{"claude-sonnet-5-5":{"name":"Claude Sonnet 5.5"}}},
		"openai":{"name":"OpenAI","models":{"gpt-5.2":{"name":"GPT-5.2"}}},
		"opencode":{"name":"OpenCode Zen","models":{"big-pickle":{"name":"Big Pickle"}}}}`)
	// Solo importan los NOMBRES del archivo de credenciales, nunca sus valores.
	writeFile(t, filepath.Join(home, ".local", "share", "opencode", "auth.json"), `{"anthropic":{"type":"api","key":"NO-DEBE-LEERSE"}}`)
	writeFile(t, filepath.Join(home, ".local", "state", "opencode", "model.json"), `{"recent":[{"providerID":"opencode","modelID":"big-pickle"}]}`)

	ms := Models("opencode", false)
	if len(ms) != 2 {
		t.Fatalf("OpenAI no tiene credenciales: %+v", ms)
	}
	// Los usados hace poco van primero (un proveedor gratuito cuenta como habilitado).
	if ms[0].ID != "opencode/big-pickle" || ms[0].Label != "Big Pickle" || ms[0].Description != "OpenCode Zen" || ms[1].ID != "anthropic/claude-sonnet-5-5" {
		t.Errorf("%+v", ms)
	}
	for _, m := range ms {
		if m.Description == "NO-DEBE-LEERSE" || m.Label == "NO-DEBE-LEERSE" {
			t.Error("se leyó un valor de credenciales")
		}
	}
}

func TestAntigravityModelsParsing(t *testing.T) {
	fakeHome(t)
	old := agyModelsExec
	defer func() { agyModelsExec = old }()
	agyModelsExec = func() []Model {
		return parseAgyModels("Fetching available models…\ngemini-4-argon-high\tGemini 4 Argon (High)\ngemini-4-argon-low\tGemini 4 Argon (Low)\n\n--raro\tx\n")
	}
	ms := Models("antigravity", false)
	if len(ms) != 2 || ms[0].ID != "gemini-4-argon-high" || ms[0].Label != "Gemini 4 Argon (High)" {
		t.Fatalf("una línea sin tabulador es ruido del CLI: %+v", ms)
	}
}

func TestModelsAreCachedAndRefreshable(t *testing.T) {
	home := fakeHome(t)
	path := filepath.Join(home, ".codex", "models_cache.json")
	writeFile(t, path, `{"models":[{"slug":"uno","visibility":"list"}]}`)
	if ms := Models("codex", false); len(ms) != 1 {
		t.Fatalf("%+v", ms)
	}
	writeFile(t, path, `{"models":[{"slug":"uno","visibility":"list"},{"slug":"dos","visibility":"list"}]}`)
	if ms := Models("codex", false); len(ms) != 1 {
		t.Errorf("se sirve la caché: %+v", ms)
	}
	if ms := Models("codex", true); len(ms) != 2 {
		t.Errorf("refresh la salta: %+v", ms)
	}
}

func TestModelDescriptionsFollowTheLanguage(t *testing.T) {
	fakeHome(t)
	i18n.SetLang("es")
	defer i18n.SetLang("en")
	es := Models("claude", false)[0].Description
	i18n.SetLang("en")
	en := Models("claude", false)[0].Description
	if es == en || es == "" {
		t.Errorf("la caché es por idioma: %q / %q", es, en)
	}
}
