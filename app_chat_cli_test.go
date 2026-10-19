//go:build !windows

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"scadlab/backend/clichat"
	"scadlab/backend/detect"
	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
	"scadlab/backend/mcp"
	"scadlab/backend/store"
)

// Pruebas de los CLIs como proveedores del chat (S12.b). Los CLIs son de
// mentira: scripts en un PATH temporal que imprimen el NDJSON de
// backend/clichat/testdata y registran con qué argumentos los llamaron. NINGUNA
// prueba abre una sesión de un CLI real.

const fakeCLIScript = `#!/bin/sh
n=$(basename "$0")
d="$FAKE_DIR"
if [ "$1" = "--version" ]; then echo "$n 9.8.7 (de mentira)"; exit 0; fi
{ printf '<<CALL>>\0'; for a in "$@"; do printf '%s\0' "$a"; done; } >> "$d/argv.$n"
pwd > "$d/cwd.$n"
if [ -f "$d/failresume.$n" ] && printf '%s ' "$@" | grep -q -- '--resume'; then
  echo "No conversation found with session ID" >&2
  exit 1
fi
if [ "$1" = "session" ] && [ -f "$d/export.$n" ]; then cat "$d/export.$n"; exit 0; fi
if [ -f "$d/hang.$n" ]; then
  cat "$d/stream.$n"
  sleep 60 &
  echo $! > "$d/sleeper.$n"
  wait
  exit 0
fi
[ -f "$d/stream.$n" ] && cat "$d/stream.$n"
exit 0
`

type appFakeCLIs struct {
	t   *testing.T
	dir string
}

// isolateCLIs deja el PATH con solo los CLIs de mentira, sin rutas de
// instalación ni shell de login, y el servidor MCP apuntando a un ejecutable
// de mentira (la línea de comandos es lo que se comprueba, no su binario).
func isolateCLIs(t *testing.T) *appFakeCLIs {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("FAKE_DIR", dir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	t.Setenv(detect.NoExtraDirsEnv, "1")
	oldLogin := detect.LoginShellPath
	detect.LoginShellPath = func() string { return "" }
	oldBin := mcpBinary
	mcpBinary = func() (string, error) { return "/app/ScadLab", nil }
	clichat.Default.Reset()
	clichat.ResetModelsCache()
	i18n.SetLang("en")
	t.Cleanup(func() {
		detect.LoginShellPath = oldLogin
		mcpBinary = oldBin
		clichat.Default.Reset()
		clichat.ResetModelsCache()
		i18n.SetLang("en")
	})
	return &appFakeCLIs{t: t, dir: dir}
}

func (f *appFakeCLIs) install(bin, stream string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, bin), []byte(fakeCLIScript), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if stream != "" {
		raw, err := os.ReadFile(filepath.Join("backend", "clichat", "testdata", stream))
		if err != nil {
			f.t.Fatal(err)
		}
		f.put("stream."+bin, string(raw))
	}
	clichat.Default.Reset()
}

func (f *appFakeCLIs) put(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *appFakeCLIs) read(name string) string {
	raw, _ := os.ReadFile(filepath.Join(f.dir, name))
	return string(raw)
}

func (f *appFakeCLIs) calls(bin string) [][]string {
	var out [][]string
	for _, chunk := range strings.Split(f.read("argv."+bin), "<<CALL>>\x00") {
		if chunk == "" {
			continue
		}
		out = append(out, strings.Split(strings.TrimSuffix(chunk, "\x00"), "\x00"))
	}
	return out
}

func argHas(args []string, seq ...string) bool {
	for i := 0; i+len(seq) <= len(args); i++ {
		match := true
		for j, s := range seq {
			if args[i+j] != s {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func argValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// cliApp deja una App con un workspace y los CLIs de mentira aislados.
func cliApp(t *testing.T) (*App, int64, string, *hermesEvents, *appFakeCLIs) {
	t.Helper()
	f := isolateCLIs(t)
	a, wsID, dir := editorWorkspace(t)
	return a, wsID, dir, captureHermesEvents(a), f
}

func providerIDs(ps []ChatProvider) []string {
	ids := []string{}
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	return ids
}

// --- Proveedores -------------------------------------------------------------------

func TestListChatProvidersDetectedCLIsFirstThenEnabledServers(t *testing.T) {
	a, _, _, _, f := cliApp(t)
	f.install("claude", "")
	f.install("codex", "")

	// Un servidor remoto y uno deshabilitado.
	remote, err := a.SaveModelServer(store.ModelServer{Kind: "openai", Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", Enabled: true}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	off, err := a.SaveModelServer(store.ModelServer{Kind: "openai", Name: "Apagado", BaseURL: "http://localhost:1234/v1", Enabled: false}, "", false)
	if err != nil {
		t.Fatal(err)
	}

	ps, err := a.ListChatProviders()
	if err != nil {
		t.Fatal(err)
	}
	ids := providerIDs(ps)
	// Los CLIs primero (como mini-tools), en el orden del catálogo; después los
	// servidores habilitados. Sin los no instalados ni los deshabilitados.
	if len(ids) != 4 || ids[0] != "cli:claude" || ids[1] != "cli:codex" || ids[2] != "ollama" || ids[3] != remote.ID {
		t.Fatalf("proveedores: %v", ids)
	}
	for _, id := range ids {
		if id == off.ID || id == "cli:opencode" || id == "openai" || id == "anthropic" {
			t.Errorf("%s no debería aparecer", id)
		}
	}
	claude := ps[0]
	if claude.Kind != "cli" || claude.Name != "Claude Code" || claude.Vendor != "Anthropic" || !claude.Installed || !claude.Enabled || claude.Remote || claude.Path != filepath.Join(f.dir, "claude") {
		t.Errorf("claude: %+v", claude)
	}
	if ollama := ps[2]; ollama.Kind != "ollama" || ollama.Vendor != "" || ollama.Remote || !ollama.Installed || !ollama.Enabled {
		t.Errorf("ollama (loopback): %+v", ollama)
	}
	if !ps[3].Remote || ps[3].Kind != "openai" {
		t.Errorf("OpenRouter está fuera de loopback: %+v", ps[3])
	}

	// El JSON usa los nombres del contrato.
	raw, _ := json.Marshal(claude)
	for _, key := range []string{`"id":"cli:claude"`, `"kind":"cli"`, `"name":"Claude Code"`, `"vendor":"Anthropic"`, `"installed":true`, `"remote":false`, `"enabled":true`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("falta %s en %s", key, raw)
		}
	}

	// Un CLI no crea filas de servidor ni toca Configuración → Proveedores.
	servers, _ := a.store.ListModelServers()
	for _, s := range servers {
		if strings.HasPrefix(s.ID, "cli:") {
			t.Errorf("un CLI no es un servidor: %+v", s)
		}
	}
}

func TestListChatProvidersWithOneServerAndTwoFakeCLIs(t *testing.T) {
	a, _, _, _, f := cliApp(t)
	f.install("claude", "")
	f.install("agy", "")
	// Solo un servidor habilitado: se apagan los demás migrados.
	for _, id := range []string{"openai", "anthropic"} {
		s, _ := a.store.GetModelServer(id)
		s.Enabled = false
		if _, err := a.SaveModelServer(s, "", false); err != nil {
			t.Fatal(err)
		}
	}
	ps, err := a.ListChatProviders()
	if err != nil {
		t.Fatal(err)
	}
	ids := providerIDs(ps)
	if len(ids) != 3 || ids[0] != "cli:claude" || ids[1] != "cli:antigravity" || ids[2] != "ollama" {
		t.Fatalf("%v", ids)
	}
	if ps[1].Name != "Antigravity" || ps[1].Vendor != "Google" {
		t.Errorf("agy se detecta como Antigravity: %+v", ps[1])
	}
}

// La detección se recuerda ~30 s; al refrescar, un CLI recién instalado aparece
// sin reiniciar la app.
func TestChatProvidersDetectionRefreshes(t *testing.T) {
	a, _, _, _, f := cliApp(t)
	f.install("claude", "")
	ps, _ := a.ListChatProviders()
	if ids := providerIDs(ps); len(ids) != 2 || ids[0] != "cli:claude" {
		t.Fatalf("%v", ids)
	}
	// Se instala Codex: la caché todavía no lo sabe…
	if err := os.WriteFile(filepath.Join(f.dir, "codex"), []byte(fakeCLIScript), 0o755); err != nil {
		t.Fatal(err)
	}
	ps, _ = a.ListChatProviders()
	if len(ps) != 2 {
		t.Errorf("dentro de la ventana se reutiliza la detección: %v", providerIDs(ps))
	}
	// …y al abrir el selector se refresca.
	ps, err := a.RefreshChatProviders()
	if err != nil {
		t.Fatal(err)
	}
	if ids := providerIDs(ps); len(ids) != 3 || ids[1] != "cli:codex" {
		t.Errorf("tras refrescar: %v", ids)
	}
}

func TestListDetectedCLIsHasVersionsAndUninstalled(t *testing.T) {
	a, _, _, _, f := cliApp(t)
	f.install("claude", "")
	f.install("opencode", "")
	list, err := a.ListDetectedCLIs()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("el catálogo entero, instalados o no: %+v", list)
	}
	byID := map[string]DetectedCLI{}
	for _, c := range list {
		byID[c.ID] = c
	}
	if c := byID["claude"]; !c.Installed || c.Version != "9.8.7" || c.Path != filepath.Join(f.dir, "claude") || c.Name != "Claude Code" || c.Vendor != "Anthropic" {
		t.Errorf("claude: %+v", c)
	}
	if c := byID["codex"]; c.Installed || c.Version != "" || c.Path != "" {
		t.Errorf("codex no está instalado: %+v", c)
	}
	// Es de solo lectura: ni filas ni ajustes nuevos.
	if got := byID["opencode"]; !got.Installed {
		t.Errorf("opencode: %+v", got)
	}
}

func TestProviderHealthCLIAndServers(t *testing.T) {
	a, _, _, _, f := cliApp(t)
	f.install("claude", "")

	if h, err := a.ProviderHealth("cli:claude"); err != nil || !h.OK || h.LatencyMs != 0 || h.Error != "" {
		t.Errorf("un CLI instalado está ok, sin latencia: %+v %v", h, err)
	}
	if h, err := a.ProviderHealth("cli:codex"); err != nil || h.OK || !strings.Contains(h.Error, "Codex is not installed") {
		t.Errorf("un CLI ausente no es ok: %+v %v", h, err)
	}
	if _, err := a.ProviderHealth("cli:nada"); err == nil {
		t.Error("un CLI fuera del catálogo es un error")
	}
	if _, err := a.ProviderHealth("srv-no-existe"); err == nil {
		t.Error("un servidor que no existe es un error")
	}

	// Un servidor: el sondeo se mide y se recuerda 30 s.
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(15 * time.Millisecond)
		_, _ = w.Write([]byte(`{"version":"0.5.1"}`))
	}))
	defer srv.Close()
	s, _ := a.store.GetModelServer("ollama")
	s.BaseURL, s.Enabled = srv.URL, true
	if _, err := a.SaveModelServer(s, "", false); err != nil {
		t.Fatal(err)
	}
	h, err := a.ProviderHealth("ollama")
	if err != nil || !h.OK || h.LatencyMs < 10 {
		t.Fatalf("servidor ok con latencia: %+v %v", h, err)
	}
	h2, _ := a.ProviderHealth("ollama")
	if !h2.OK || h2.LatencyMs != h.LatencyMs || hits.Load() != 1 {
		t.Errorf("el sondeo vale 30 s: %+v / %d consultas", h2, hits.Load())
	}

	// Caído: no es un error de la llamada, es la respuesta.
	srv.Close()
	a.serverSvc().invalidate("ollama")
	h3, err := a.ProviderHealth("ollama")
	if err != nil || h3.OK || h3.Error == "" {
		t.Errorf("servidor caído: %+v %v", h3, err)
	}

	// Deshabilitado.
	s.Enabled = false
	if _, err := a.SaveModelServer(s, "", false); err != nil {
		t.Fatal(err)
	}
	if h, _ := a.ProviderHealth("ollama"); h.OK || !strings.Contains(h.Error, "disabled") {
		t.Errorf("deshabilitado: %+v", h)
	}
}

func TestChatModelCatalogForCLIAndServer(t *testing.T) {
	a, _, _, _, f := cliApp(t)
	f.install("claude", "")

	cat, err := a.ChatModelCatalog("cli:claude", false)
	if err != nil {
		t.Fatal(err)
	}
	if cat.DefaultHint != "Whatever the CLI has configured" || len(cat.Models) != 4 {
		t.Fatalf("%+v", cat)
	}
	if m := cat.Models[0]; m.ID != "opus" || m.Label != "Opus" || m.Description == "" || !m.Tools || !m.ToolsKnown {
		t.Errorf("un modelo de CLI lleva label y description: %+v", m)
	}
	i18n.SetLang("es")
	if cat, _ := a.ChatModelCatalog("cli:claude", true); cat.DefaultHint != "El que tenga configurado el CLI" {
		t.Errorf("hint en español: %q", cat.DefaultHint)
	}
	i18n.SetLang("en")
	raw, _ := json.Marshal(cat)
	if !strings.Contains(string(raw), `"defaultHint"`) || !strings.Contains(string(raw), `"label":"Opus"`) || !strings.Contains(string(raw), `"description"`) {
		t.Errorf("JSON del contrato: %s", raw)
	}
	if _, err := a.ChatModelCatalog("cli:nada", false); err == nil {
		t.Error("un CLI fuera del catálogo")
	}

	// Un servidor.
	sim := &fakeOllama{}
	srv := sim.server(t)
	s, _ := a.store.GetModelServer("ollama")
	s.BaseURL, s.Enabled, s.DefaultModel = srv.URL, true, ""
	if _, err := a.SaveModelServer(s, "", false); err != nil {
		t.Fatal(err)
	}
	cat, err = a.ChatModelCatalog("ollama", false)
	if err != nil || len(cat.Models) != 2 || cat.DefaultHint != "The first one the server returns" {
		t.Fatalf("servidor sin modelo por defecto: %+v %v", cat, err)
	}
	s.DefaultModel = "qwen3-coder:30b"
	if _, err := a.SaveModelServer(s, "", false); err != nil {
		t.Fatal(err)
	}
	cat, _ = a.ChatModelCatalog("ollama", true)
	if !strings.Contains(cat.DefaultHint, "qwen3-coder:30b") {
		t.Errorf("con modelo por defecto, el hint lo dice: %q", cat.DefaultHint)
	}
	if _, err := a.ChatModelCatalog("srv-no-existe", false); err == nil {
		t.Error("un servidor que no existe")
	}
}

// --- Un turno de punta a punta -----------------------------------------------------

func waitForSteps(t *testing.T, ev *hermesEvents, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ev.mu.Lock()
		got := len(ev.steps)
		ev.mu.Unlock()
		if got >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no llegaron %d eventos de paso", n)
}

func stepsOf(ev *hermesEvents) []ChatStepEvent {
	ev.mu.Lock()
	defer ev.mu.Unlock()
	return append([]ChatStepEvent(nil), ev.steps...)
}

// Un turno con Claude Code: streaming de texto, dos herramientas de ScadLab como
// pasos (la de preview con su imagen), uso y costo, la sesión guardada, los
// flags MCP del servidor y la carpeta del workspace.
func TestCLIChatTurnStreamsStepsUsageAndSavesTheSession(t *testing.T) {
	a, wsID, dir, ev, f := cliApp(t)
	f.install("claude", "claude-stream.jsonl")

	c, err := a.NewChat(wsID, "cli:claude", "opus")
	if err != nil || c.ProviderID != "cli:claude" || c.Model != "opus" {
		t.Fatalf("NewChat: %+v %v", c, err)
	}
	um, err := a.SendChatMessage(c.ID, "haz una caja de 40 mm", "001-base.scad", nil, SendOptions{Agent: true})
	if err != nil || um.Role != "user" {
		t.Fatalf("Send: %+v %v", um, err)
	}
	done := ev.wait(t)
	if done.Cancelled || done.MessageID == 0 {
		t.Fatalf("done: %+v", done)
	}

	if got := ev.text[c.ID]; got != "Voy a renderizar y quedó bien: una caja de 40 mm." {
		t.Errorf("deltas: %q", got)
	}
	steps := stepsOf(ev)
	// render: running → ok; preview: running → ok con imagen.
	if len(steps) != 4 || steps[0].Step.Tool != "render" || steps[0].Step.Status != "running" || steps[1].Step.Status != "ok" || steps[1].Step.ID != steps[0].Step.ID {
		t.Fatalf("pasos: %+v", steps)
	}
	if steps[1].Step.Args["file"] != "001-caja.scad" || !strings.Contains(steps[1].Step.Text, `"status":"ok"`) || steps[1].Step.Index != 1 {
		t.Errorf("render: %+v", steps[1].Step)
	}
	if p := steps[3].Step; p.Tool != "preview" || p.Status != "ok" || len(p.Images) != 1 || !strings.HasPrefix(p.Images[0], "data:image/png;base64,") || p.Index != 2 {
		t.Errorf("preview: %+v", p)
	}
	// TextAt: el primer trozo de texto («Voy a renderizar ») ya estaba cuando arrancó el render.
	if steps[0].Step.TextAt != len("Voy a renderizar ") {
		t.Errorf("TextAt = %d", steps[0].Step.TextAt)
	}
	ev.mu.Lock()
	usage := append([]ChatUsageEvent(nil), ev.usage...)
	ev.mu.Unlock()
	if len(usage) != 1 || usage[0].InputTokens != 2+7775 || usage[0].OutputTokens != 120 || usage[0].CostUSD != 0.088648 {
		t.Errorf("uso: %+v", usage)
	}

	// Lo guardado: texto, pasos, tokens y modelo, igual que en Hermes.
	view, err := a.GetChat(c.ID)
	if err != nil || len(view.Messages) != 2 {
		t.Fatalf("GetChat: %+v %v", view, err)
	}
	m := view.Messages[1]
	parts := hermes.ParseParts(m.Parts)
	if m.Role != "assistant" || m.Content != "Voy a renderizar y quedó bien: una caja de 40 mm." || m.InputTokens != 7777 || m.OutputTokens != 120 || m.CostUSD != 0.088648 {
		t.Errorf("mensaje: %+v", m)
	}
	if len(parts.Steps) != 2 || parts.Steps[0].Tool != "render" || parts.Steps[1].Tool != "preview" || len(parts.Steps[1].Images) != 1 || parts.Model != "claude-opus-5[1m]" {
		t.Errorf("parts: %+v", parts)
	}
	if view.Chat.CLISession != "9cb82bf7-284f-4617-a552-dfe908f87b7f" {
		t.Errorf("la sesión del CLI queda guardada: %+v", view.Chat)
	}
	if view.Chat.Title == "" {
		t.Error("el primer mensaje titula el chat")
	}

	// La línea de comandos: modo Agente, MCP de ScadLab, carpeta y checkpoint abierto.
	calls := f.calls("claude")
	if len(calls) != 1 {
		t.Fatalf("llamadas: %v", calls)
	}
	args := calls[0]
	if !argHas(args, "--permission-mode", "acceptEdits") || !argHas(args, "--model", "opus") || !argHas(args, "--output-format", "stream-json") {
		t.Errorf("argv: %v", args)
	}
	mcpCfg := argValue(args, "--mcp-config")
	if !strings.Contains(mcpCfg, `"scadlab"`) || !strings.Contains(mcpCfg, "/app/ScadLab") || !strings.Contains(mcpCfg, dir) {
		t.Errorf("--mcp-config: %q", mcpCfg)
	}
	allowed := argValue(args, "--allowedTools")
	if !strings.Contains(allowed, "mcp__scadlab__create_checkpoint") || !strings.Contains(allowed, "mcp__scadlab__render") {
		t.Errorf("«Agente» usa todas las herramientas de ScadLab: %q", allowed)
	}
	if !strings.Contains(argValue(args, "--disallowedTools"), "Bash") {
		t.Errorf("la shell se deniega siempre: %v", args)
	}
	prompt := args[1]
	if !strings.Contains(prompt, "haz una caja de 40 mm") || !strings.Contains(prompt, "001-base.scad") || !strings.Contains(prompt, "cube(10)") {
		t.Errorf("el prompt lleva el mensaje y el checkpoint abierto: %q", prompt)
	}
	if strings.Contains(prompt, "Query only") {
		t.Errorf("en modo Agente no se dice «solo consulta»: %q", prompt)
	}
	gotCwd, _ := filepath.EvalSymlinks(strings.TrimSpace(f.read("cwd.claude")))
	wantCwd, _ := filepath.EvalSymlinks(dir)
	if gotCwd != wantCwd {
		t.Errorf("cwd = %q, quería %q", gotCwd, wantCwd)
	}
}

// «Solo consulta» (Agent=false) es el modo por defecto: no acepta ediciones, no
// puede crear checkpoints, y el prompt se lo dice. Y un segundo mensaje continúa
// con la sesión guardada, sin reenviar el historial.
func TestCLIChatAskModeAndContinuingWithTheSession(t *testing.T) {
	a, wsID, _, ev, f := cliApp(t)
	f.install("claude", "claude-stream.jsonl")
	c, _ := a.NewChat(wsID, "cli:claude", "")
	if c.Model != "" {
		t.Fatalf("un CLI admite el modelo vacío (el que tenga configurado): %+v", c)
	}

	if _, err := a.SendChatMessage(c.ID, "¿cabe en la placa?", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if _, err := a.SendChatMessage(c.ID, "ahora hazla más alta", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)

	calls := f.calls("claude")
	if len(calls) != 2 {
		t.Fatalf("llamadas: %v", calls)
	}
	ask := calls[0]
	if argHas(ask, "--permission-mode") || argHas(ask, "--model") || argHas(ask, "--resume") {
		t.Errorf("conversación nueva en solo consulta, sin modelo: %v", ask)
	}
	if !strings.Contains(argValue(ask, "--disallowedTools"), "Edit") {
		t.Errorf("en solo consulta no se editan archivos: %v", ask)
	}
	allowed := argValue(ask, "--allowedTools")
	if strings.Contains(allowed, "create_checkpoint") || strings.Contains(allowed, "export") || !strings.Contains(allowed, "mcp__scadlab__render") {
		t.Errorf("solo las herramientas de lectura: %q", allowed)
	}
	if !strings.Contains(ask[1], "Query only") {
		t.Errorf("el prompt dice el modo: %q", ask[1])
	}
	second := calls[1]
	if !argHas(second, "--resume", "9cb82bf7-284f-4617-a552-dfe908f87b7f") {
		t.Errorf("el segundo turno continúa la sesión: %v", second)
	}
	if strings.Contains(second[1], "Earlier conversation") || strings.Contains(second[1], "cabe en la placa") {
		t.Errorf("con sesión no se reenvía el historial (el CLI ya lo tiene): %q", second[1])
	}
}

// Un chat que empezó con un servidor y sigue con un CLI le pasa el hilo anterior
// en el primer turno (el CLI no tiene su historial), y no vuelve a hacerlo.
func TestCLIChatSwitchingFromAServerCarriesTheThread(t *testing.T) {
	a, wsID, _, ev, f := cliApp(t)
	f.install("claude", "claude-stream.jsonl")
	c, _ := a.store.CreateChat(wsID, "ollama", "qwen3")
	_, _ = a.store.AddChatMessage(c.ID, "user", "quiero una caja con tapa")
	_, _ = a.store.AddChatMessage(c.ID, "assistant", "Listo: hice la base")

	if err := a.SetChatModel(c.ID, "cli:claude", "sonnet"); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.store.GetChat(c.ID); got.ProviderID != "cli:claude" || got.Model != "sonnet" || got.CLISession != "" {
		t.Fatalf("%+v", got)
	}
	if lm, _ := a.GetLastModel(wsID); lm.ServerID != "cli:claude" || lm.Model != "sonnet" {
		t.Errorf("la elección queda como último modelo: %+v", lm)
	}

	_, _ = a.SendChatMessage(c.ID, "ahora agrégale la tapa", "", nil, SendOptions{})
	ev.wait(t)
	prompt := f.calls("claude")[0][1]
	if !strings.Contains(prompt, "[Earlier conversation in this chat]") || !strings.Contains(prompt, "quiero una caja con tapa") || !strings.Contains(prompt, "Listo: hice la base") || !strings.Contains(prompt, "ahora agrégale la tapa") {
		t.Errorf("el primer turno lleva el hilo: %q", prompt)
	}

	// Volver a un servidor olvida la sesión del CLI.
	if err := a.SetChatModel(c.ID, "ollama", "qwen3"); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.store.GetChat(c.ID); got.CLISession != "" || got.ProviderID != "ollama" {
		t.Errorf("%+v", got)
	}
}

// Una sesión que el CLI ya no tiene se olvida y el turno se reintenta una vez
// como conversación nueva, con el hilo anterior en el prompt.
func TestCLIChatRetriesWithoutAStaleSession(t *testing.T) {
	a, wsID, _, ev, f := cliApp(t)
	f.install("claude", "claude-stream.jsonl")
	f.put("failresume.claude", "1")

	c, _ := a.NewChat(wsID, "cli:claude", "")
	_, _ = a.store.AddChatMessage(c.ID, "user", "primera pregunta")
	_, _ = a.store.AddChatMessage(c.ID, "assistant", "primera respuesta")
	_ = a.store.SetChatCLISession(c.ID, "sesion-vencida")

	if _, err := a.SendChatMessage(c.ID, "sigue", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	done := ev.wait(t)
	if done.MessageID == 0 {
		t.Fatalf("done: %+v", done)
	}
	calls := f.calls("claude")
	if len(calls) != 2 || !argHas(calls[0], "--resume", "sesion-vencida") || argHas(calls[1], "--resume") {
		t.Fatalf("debía reintentar sin sesión: %v", calls)
	}
	if !strings.Contains(calls[1][1], "primera pregunta") {
		t.Errorf("el reintento lleva el hilo: %q", calls[1][1])
	}
	if got, _ := a.store.GetChat(c.ID); got.CLISession != "9cb82bf7-284f-4617-a552-dfe908f87b7f" {
		t.Errorf("la sesión nueva reemplaza a la vencida: %+v", got)
	}
}

// Cancelar a mitad: el árbol de procesos muere (también el nieto), el texto y los
// pasos hasta ahí se guardan, y los pasos abiertos quedan como cancelados.
func TestCLIChatCancelMidTurnKeepsPartialAndKillsTheTree(t *testing.T) {
	a, wsID, _, ev, f := cliApp(t)
	f.install("claude", "claude-stream.jsonl")
	f.put("hang.claude", "1")
	// El stream se corta tras abrir un paso: sin su resultado ni el cierre.
	raw := f.read("stream.claude")
	lines := strings.Split(raw, "\n")
	f.put("stream.claude", strings.Join(lines[:4], "\n")+"\n") // init, rate limit, texto, tool_use(render)

	c, _ := a.NewChat(wsID, "cli:claude", "")
	if _, err := a.SendChatMessage(c.ID, "haz una caja", "", nil, SendOptions{Agent: true}); err != nil {
		t.Fatal(err)
	}
	waitForSteps(t, ev, 1)
	var pid int
	deadline := time.Now().Add(5 * time.Second)
	for pid == 0 && time.Now().Before(deadline) {
		pid, _ = strconv.Atoi(strings.TrimSpace(f.read("sleeper.claude")))
		time.Sleep(10 * time.Millisecond)
	}
	if pid == 0 || syscall.Kill(pid, 0) != nil {
		t.Fatal("el nieto debería estar vivo antes de cancelar")
	}

	a.CancelChat(c.ID)
	done := ev.wait(t)
	if !done.Cancelled || done.MessageID == 0 {
		t.Fatalf("done: %+v", done)
	}
	view, _ := a.GetChat(c.ID)
	m := view.Messages[len(view.Messages)-1]
	parts := hermes.ParseParts(m.Parts)
	if m.Content != "Voy a renderizar " || len(parts.Steps) != 1 || parts.Steps[0].Status != "error" || parts.Steps[0].Text != "cancelled" {
		t.Errorf("lo parcial se conserva: %+v / %+v", m, parts.Steps)
	}
	if view.Chat.CLISession == "" {
		t.Error("la sesión se guarda apenas aparece, también si se cancela")
	}
	deadline = time.Now().Add(3 * time.Second)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		t.Error("el nieto sobrevivió a la cancelación: quedaría un proceso huérfano")
	}
	if view.Running {
		t.Error("el chat sigue marcado como respondiendo")
	}
}

func TestCLIChatAbsentCLIFailsBeforeSavingAnything(t *testing.T) {
	a, wsID, _, ev, f := cliApp(t)
	_ = f // ningún CLI instalado

	if _, err := a.NewChat(wsID, "cli:claude", ""); err == nil || !strings.Contains(err.Error(), "Claude Code is not installed") {
		t.Fatalf("NewChat con un CLI ausente: %v", err)
	}
	if _, err := a.NewChat(wsID, "cli:nada", ""); err == nil {
		t.Error("un CLI fuera del catálogo")
	}

	// Un chat que ya era de un CLI que se desinstaló: el envío falla claro y no
	// deja un mensaje del usuario huérfano.
	c, _ := a.store.CreateChat(wsID, "cli:claude", "")
	_, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{})
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("SendChatMessage: %v", err)
	}
	if msgs, _ := a.store.ListChatMessages(c.ID); len(msgs) != 0 {
		t.Errorf("no se guarda el mensaje: %+v", msgs)
	}
	if err := a.SetChatModel(c.ID, "cli:codex", ""); err == nil {
		t.Error("cambiar a un CLI ausente")
	}
	select {
	case e := <-ev.errs:
		t.Errorf("no debería haber un turno: %+v", e)
	default:
	}
	// Y el modelo, aunque el CLI exista, tiene que ser un id válido.
	f.install("claude", "")
	if _, err := a.NewChat(wsID, "cli:claude", "--auto"); err == nil {
		t.Error("un modelo que parece una bandera")
	}
}

// Un fallo del CLI llega como chat:error con su nombre; si parece de login, dice qué hacer.
func TestCLIChatFailureShowsTheCLIsOwnMessage(t *testing.T) {
	a, wsID, _, ev, f := cliApp(t)
	f.install("claude", "claude-error.jsonl")
	c, _ := a.NewChat(wsID, "cli:claude", "")
	if _, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-ev.errs:
		if !strings.HasPrefix(e.Message, "Claude Code: Not logged in") || !strings.Contains(e.Message, "run claude and sign in") {
			t.Errorf("mensaje: %q", e.Message)
		}
	case <-ev.done:
		t.Fatal("un fallo no termina como hecho")
	case <-time.After(10 * time.Second):
		t.Fatal("sin respuesta")
	}
	if got, _ := a.GetChat(c.ID); got.Running {
		t.Error("el chat quedó respondiendo")
	}
}

// Codex y OpenCode: los flags MCP que ya arma la terminal llegan al CLI, y el
// interruptor del servidor MCP los quita.
func TestCLIChatPassesTheTerminalsMCPAndAddDirFlags(t *testing.T) {
	a, wsID, dir, ev, f := cliApp(t)
	f.install("codex", "codex-stream.jsonl")
	f.install("opencode", "opencode-stream.jsonl")
	f.put("export.opencode", "{}")

	cx, _ := a.NewChat(wsID, "cli:codex", "")
	if _, err := a.SendChatMessage(cx.ID, "mide la pieza", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	args := f.calls("codex")[0]
	if !argHas(args, "-c", `mcp_servers.scadlab.command="/app/ScadLab"`) {
		t.Errorf("codex no lee configuración de proyecto: recibe el MCP por -c: %v", args)
	}
	var sawArgs bool
	for _, x := range args {
		if strings.HasPrefix(x, "mcp_servers.scadlab.args=") && strings.Contains(x, dir) {
			sawArgs = true
		}
	}
	if !sawArgs || !argHas(args, "--sandbox", "read-only") {
		t.Errorf("codex: %v", args)
	}
	// Su sesión es el hilo.
	if got, _ := a.store.GetChat(cx.ID); got.CLISession != "01a00217-12d1-7492-b8df-b6a2c1f0bc86" {
		t.Errorf("%+v", got)
	}
	steps := stepsOf(ev)
	if len(steps) < 4 || steps[0].Step.Tool != "command_execution" || steps[2].Step.Tool != "measure" {
		t.Errorf("pasos de Codex: %+v", steps)
	}

	// Con el servidor MCP apagado no se pasa nada de MCP.
	if err := a.SetMCPEnabled(false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendChatMessage(cx.ID, "otra vez", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	for _, x := range f.calls("codex")[1] {
		if strings.Contains(x, "mcp_servers") || strings.Contains(x, "enabled_tools") {
			t.Errorf("con el MCP apagado no se pasa: %v", f.calls("codex")[1])
		}
	}

	// OpenCode lee su configuración de proyecto: no recibe flags MCP, pero sí el
	// agente del modo y la lista de permisos por entorno.
	oc, _ := a.NewChat(wsID, "cli:opencode", "")
	if _, err := a.SendChatMessage(oc.ID, "hola", "", nil, SendOptions{Agent: true}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if oargs := f.calls("opencode")[0]; !argHas(oargs, "--agent", "build") || argHas(oargs, "--mcp-config") {
		t.Errorf("opencode: %v", oargs)
	}
}

// Una referencia 3D adjunta viaja como texto; una imagen, como ruta (y su carpeta
// como lectura permitida).
func TestCLIChatAttachmentsAreTextAndPaths(t *testing.T) {
	a, wsID, _, ev, f := cliApp(t)
	f.install("claude", "claude-stream.jsonl")
	c, _ := a.NewChat(wsID, "cli:claude", "")

	img, err := a.AddChatAttachmentData(wsID, "captura.png", pngB64(t))
	if err != nil {
		t.Fatal(err)
	}
	stl, err := a.AddChatAttachmentData(wsID, "pieza.stl", b64(asciiSTL()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendChatMessage(c.ID, "mira esto", "", []string{img.ID, stl.ID}, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	args := f.calls("claude")[0]
	prompt := args[1]
	if !strings.Contains(prompt, "mira esto") || !strings.Contains(prompt, "Attached images") || !strings.Contains(prompt, a.chatDir(c.ID)) {
		t.Errorf("las imágenes van como rutas: %q", prompt)
	}
	if !strings.Contains(prompt, "refs/") {
		t.Errorf("el resumen de la referencia STL va como texto: %q", prompt)
	}
	if !argHas(args, "--add-dir", a.chatDir(c.ID)) {
		t.Errorf("la carpeta de las imágenes se puede leer: %v", args)
	}
}

// --- Último proveedor y modelo ---------------------------------------------------

func TestLastModelAcceptsCLIs(t *testing.T) {
	a, wsID, _, _, f := cliApp(t)
	f.install("claude", "")

	if err := a.SetLastModel(wsID, "cli:claude", "opus"); err != nil {
		t.Fatal(err)
	}
	if lm, err := a.GetLastModel(wsID); err != nil || lm.ServerID != "cli:claude" || lm.Model != "opus" {
		t.Fatalf("%+v %v", lm, err)
	}
	if err := a.SetLastModel(wsID, "cli:claude", ""); err != nil {
		t.Fatal("el modelo vacío (el del CLI) vale")
	}
	if err := a.SetLastModel(wsID, "cli:nada", "x"); err == nil {
		t.Error("un CLI fuera del catálogo")
	}
	if err := a.SetLastModel(wsID, "cli:claude", "--auto"); err == nil {
		t.Error("un modelo que parece una bandera")
	}
	// El CLI se desinstala: la elección vale como «ninguna».
	if err := os.Remove(filepath.Join(f.dir, "claude")); err != nil {
		t.Fatal(err)
	}
	clichat.Default.Reset()
	if lm, _ := a.GetLastModel(wsID); lm.ServerID != "" {
		t.Errorf("un CLI desinstalado no es el último proveedor: %+v", lm)
	}
	// ListProviderModels también entiende un CLI.
	f.install("claude", "")
	if ids, err := a.ListProviderModels("cli:claude"); err != nil || len(ids) != 4 || ids[0] != "opus" {
		t.Errorf("%v %v", ids, err)
	}
}

// --- Coherencia con el servidor MCP ----------------------------------------------

// Las herramientas que «Solo consulta» y «Agente» permiten son exactamente el
// catálogo del servidor MCP: una herramienta nueva sin clasificar no puede
// colarse en «Solo consulta» ni quedar fuera de «Agente».
func TestModeToolListsMatchTheMCPCatalog(t *testing.T) {
	if mcp.ServerName != "scadlab" {
		t.Fatalf("el nombre del servidor MCP cambió a %q: clichat usa «scadlab»", mcp.ServerName)
	}
	tb, err := mcp.NewToolbox(mcp.ToolboxOptions{WorkspaceDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer tb.Close()
	want := map[string]bool{}
	for _, tool := range tb.Tools() {
		want[tool.Name] = true
	}
	got := map[string]bool{}
	for _, n := range append(clichat.ReadTools(), clichat.WriteTools()...) {
		if got[n] {
			t.Errorf("%s está en las dos listas", n)
		}
		got[n] = true
	}
	for n := range want {
		if !got[n] && !strings.HasPrefix(n, "inspect_") {
			t.Errorf("la herramienta %q del servidor MCP no está clasificada como lectura o escritura", n)
		}
	}
	for n := range got {
		if !want[n] {
			t.Errorf("%q está en las listas de los modos pero el servidor MCP no la tiene", n)
		}
	}
}
