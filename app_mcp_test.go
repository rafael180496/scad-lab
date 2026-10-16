package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"scadlab/backend/mcpconf"
)

// withMCPBinary fija el ejecutable que ScadLab cree ser durante la prueba.
func withMCPBinary(t *testing.T, bin string) {
	t.Helper()
	prev := mcpBinary
	mcpBinary = func() (string, error) { return bin, nil }
	t.Cleanup(func() { mcpBinary = prev })
}

func mcpFiles(dir string) []string {
	return []string{
		filepath.Join(dir, ".mcp.json"),
		filepath.Join(dir, "opencode.json"),
		filepath.Join(dir, ".gemini", "settings.json"), // Gemini CLI anterior
	}
}

func fileStates(t *testing.T, a *App, id int64) map[string]string {
	t.Helper()
	st, err := a.GetMCPStatus(id)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range st.Files {
		out[f.Name] = f.Status
	}
	return out
}

func TestWorkspaceLifecycleConfiguresMCP(t *testing.T) {
	withMCPBinary(t, "/Apps/ScadLab")
	a := newTestApp(t)

	w, err := a.CreateWorkspace("Caja", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range mcpFiles(w.Path) {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("crear el workspace no dejó %s", p)
		}
	}
	for name, s := range fileStates(t, a, w.ID) {
		if s != mcpconf.StateOK {
			t.Errorf("%s: %s", name, s)
		}
	}

	// Abrir con la app movida reescribe la ruta del ejecutable.
	withMCPBinary(t, "/Nuevo/ScadLab")
	if s := fileStates(t, a, w.ID)[".mcp.json"]; s != mcpconf.StateOutdated {
		t.Fatalf("tras mover la app: %s", s)
	}
	if _, err := a.OpenWorkspace(w.ID); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(w.Path, ".mcp.json")); !strings.Contains(string(raw), "/Nuevo/ScadLab") {
		t.Errorf("abrir no actualizó .mcp.json: %s", raw)
	}
	for name, s := range fileStates(t, a, w.ID) {
		if s != mcpconf.StateOK {
			t.Errorf("tras abrir %s: %s", name, s)
		}
	}
}

func TestImportWorkspaceKeepsUserConfig(t *testing.T) {
	withMCPBinary(t, "/Apps/ScadLab")
	a := newTestApp(t)
	dir := t.TempDir()
	mine := "{\n  \"mcpServers\": {\n    \"mio\": {\"command\": \"x\"}\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mine), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := a.ImportWorkspace(dir, "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if !strings.Contains(string(raw), `"mio"`) || !strings.Contains(string(raw), `"scadlab"`) {
		t.Errorf("importar no fusionó:\n%s", raw)
	}
	if fileStates(t, a, w.ID)["opencode.json"] != mcpconf.StateOK {
		t.Error("importar no creó opencode.json")
	}
}

func TestSetMCPEnabledSyncsEveryWorkspace(t *testing.T) {
	withMCPBinary(t, "/Apps/ScadLab")
	a := newTestApp(t)
	w1, err := a.CreateWorkspace("Uno", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	w2, err := a.CreateWorkspace("Dos", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	// Un archivo del usuario con otra clave sobrevive al apagado.
	other := "{\n  \"mcpServers\": {\n    \"mio\": {\"command\": \"x\"},\n    \"scadlab\": {\"command\": \"y\"}\n  }\n}\n"
	if err := os.WriteFile(filepath.Join(w2.Path, ".mcp.json"), []byte(other), 0o644); err != nil {
		t.Fatal(err)
	}
	// Una carpeta que ya no existe no rompe el barrido.
	gone, err := a.CreateWorkspace("Borrada", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone.Path); err != nil {
		t.Fatal(err)
	}

	if err := a.SetMCPEnabled(false); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.store.GetSettings(); s.MCPEnabled {
		t.Error("no guardó mcpEnabled=false")
	}
	for _, p := range mcpFiles(w1.Path) {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("sigue %s", p)
		}
	}
	raw, _ := os.ReadFile(filepath.Join(w2.Path, ".mcp.json"))
	if strings.Contains(string(raw), "scadlab") || !strings.Contains(string(raw), `"mio"`) {
		t.Errorf("al apagar tocó lo ajeno o no quitó lo propio:\n%s", raw)
	}
	for name, s := range fileStates(t, a, w1.ID) {
		if s != mcpconf.StateOff {
			t.Errorf("apagado %s: %s", name, s)
		}
	}
	// Con el MCP apagado, abrir un workspace no lo vuelve a escribir.
	if _, err := a.OpenWorkspace(w1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(w1.Path, ".mcp.json")); !os.IsNotExist(err) {
		t.Error("abrir con el MCP apagado lo escribió")
	}

	if err := a.SetMCPEnabled(true); err != nil {
		t.Fatal(err)
	}
	for _, p := range mcpFiles(w1.Path) {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("al encender falta %s", p)
		}
	}
}

func TestSyncWorkspaceMCPReportsEachFile(t *testing.T) {
	withMCPBinary(t, "/Apps/ScadLab")
	a := newTestApp(t)
	w, err := a.CreateWorkspace("Caja", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	// Un JSON roto del usuario se informa y no se toca.
	broken := filepath.Join(w.Path, "opencode.json")
	if err := os.WriteFile(broken, []byte("{ roto"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := a.SyncWorkspaceMCP(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, f := range rep.Files {
		got[f.Name] = f.Status
	}
	if got["opencode.json"] != mcpconf.StatusInvalid || got[".mcp.json"] != mcpconf.StatusUnchange {
		t.Errorf("informe: %v", got)
	}
	if b, _ := os.ReadFile(broken); string(b) != "{ roto" {
		t.Error("tocó el JSON roto")
	}
	if fileStates(t, a, w.ID)["opencode.json"] != mcpconf.StateInvalid {
		t.Error("el estado no refleja el JSON roto")
	}
}

func TestGetMCPStatusCommandAndFlags(t *testing.T) {
	withMCPBinary(t, "/Apps/Scad Lab/ScadLab")
	a := newTestApp(t)
	w, err := a.CreateWorkspace("Caja", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	st, err := a.GetMCPStatus(w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !st.Enabled || st.Binary != "/Apps/Scad Lab/ScadLab" || st.WorkspaceDir != w.Path {
		t.Errorf("estado: %+v", st)
	}
	if want := `"/Apps/Scad Lab/ScadLab" --mcp --workspace ` + w.Path; !strings.Contains(st.Command, "--mcp --workspace") || !strings.HasPrefix(st.Command, `"/Apps/Scad Lab/ScadLab"`) {
		t.Errorf("comando %q, quería algo como %q", st.Command, want)
	}
	if len(st.CodexFlags) != 4 || st.CodexFlags[0] != "-c" {
		t.Errorf("flags de Codex: %q", st.CodexFlags)
	}
	// Sin workspace: estado general.
	general, err := a.GetMCPStatus(0)
	if err != nil || len(general.Files) != 0 || general.WorkspaceDir != "" {
		t.Errorf("sin workspace: %+v, %v", general, err)
	}
}

func TestMCPLaunchArgsForCodexAndClaude(t *testing.T) {
	withMCPBinary(t, "/Apps/ScadLab")
	a := newTestApp(t)
	dir := t.TempDir()

	want := []string{
		"-c", `mcp_servers.scadlab.command="/Apps/ScadLab"`,
		"-c", `mcp_servers.scadlab.args=["--mcp","--workspace",` + tomlQuote(dir) + `]`,
	}
	if got := a.mcpLaunchArgs("codex", dir); !reflect.DeepEqual(got, want) {
		t.Errorf("codex:\n got %q\nwant %q", got, want)
	}
	// Claude: --mcp-config con el servidor en JSON (sin la aprobación del
	// .mcp.json del proyecto).
	if got := a.mcpLaunchArgs("claude", dir); len(got) != 2 || got[0] != "--mcp-config" || !strings.Contains(got[1], `"scadlab"`) {
		t.Errorf("claude: %q", got)
	}
	for _, id := range []string{"opencode", "antigravity", "gemini"} {
		if got := a.mcpLaunchArgs(id, dir); got != nil {
			t.Errorf("%s no usa flags: %q", id, got)
		}
	}

	// El argv final: ejecutable, flags -c, y luego el subcomando de reanudar.
	argv := launchArgv("/bin/codex", append(a.mcpLaunchArgs("codex", dir), "resume", "--last"))
	if argv[0] != "/bin/codex" || argv[1] != "-c" || argv[3] != "-c" || argv[len(argv)-2] != "resume" || argv[len(argv)-1] != "--last" {
		t.Errorf("argv: %q", argv)
	}

	if err := a.store.SetSetting("mcpEnabled", false); err != nil {
		t.Fatal(err)
	}
	if got := a.mcpLaunchArgs("codex", dir); got != nil {
		t.Errorf("apagado: %q", got)
	}
}

// tomlQuote escribe s como cadena TOML (lo que espera Codex en -c).
func tomlQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// ---- Antigravity (configuración global) ----

// fakeAgy imita `agy` con la salida real de 1.3.6 y registra cada argv. Los
// tests nunca lanzan el agy de verdad: tocaría ~/.gemini del usuario.
type fakeAgy struct {
	calls   [][]string
	servers map[string]string // nombre -> «ejecutable args» (siempre stdio, activo)
	fail    map[string]error  // subcomando -> error
	out     map[string]string // subcomando -> salida con la que falla
}

func (f *fakeAgy) run(_ context.Context, path string, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string{path}, args...))
	if len(args) < 2 || args[0] != "mcp" {
		return "", errors.New("uso")
	}
	if err := f.fail[args[1]]; err != nil {
		return f.out[args[1]], err
	}
	switch args[1] {
	case "list":
		if len(f.servers) == 0 {
			return "No MCP servers configured.\n", nil
		}
		s := "NAME     TYPE   STATUS   COMMAND/URL\n"
		for n, c := range f.servers {
			s += fmt.Sprintf("%-8s stdio  enabled  %s\n", n, c)
		}
		return s, nil
	case "add": // add -- <name> <cmd> [args...]
		f.servers[args[3]] = strings.Join(args[4:], " ")
		return "Added MCP server\n", nil
	case "remove":
		if _, ok := f.servers[args[2]]; !ok {
			return "Error: MCP server not found\n", errors.New("exit status 1")
		}
		delete(f.servers, args[2])
		return "Removed MCP server\n", nil
	}
	return "", errors.New("subcomando")
}

func withFakeAgy(t *testing.T, path string) *fakeAgy {
	t.Helper()
	f := &fakeAgy{servers: map[string]string{}, fail: map[string]error{}, out: map[string]string{}}
	prevExec, prevPath := agyExec, agyPath
	agyExec = f.run
	agyPath = func() string { return path }
	t.Cleanup(func() { agyExec, agyPath = prevExec, prevPath })
	return f
}

func TestAntigravityMCPLifecycle(t *testing.T) {
	const bin = "/Applications/My Apps/ScadLab"
	withMCPBinary(t, bin)
	a := newTestApp(t)

	// Sin agy instalado: ni se lo llama.
	f := withFakeAgy(t, "")
	if st, _ := a.AntigravityMCPStatus(); st.State != "not-installed" || st.Command != bin+" --mcp" {
		t.Fatalf("sin agy: %+v", st)
	}
	if _, err := a.AddAntigravityMCP(); err == nil {
		t.Error("agregar sin agy debe fallar")
	}
	if _, err := a.RemoveAntigravityMCP(); err == nil {
		t.Error("quitar sin agy debe fallar")
	}
	if len(f.calls) != 0 {
		t.Fatalf("no debía lanzar agy: %q", f.calls)
	}

	f = withFakeAgy(t, "/usr/local/bin/agy")
	if st, _ := a.AntigravityMCPStatus(); st.State != "missing" || st.AgyPath != "/usr/local/bin/agy" {
		t.Fatalf("sin entrada: %+v", st)
	}
	// Quitar sin entrada no llama a remove.
	if st, err := a.RemoveAntigravityMCP(); err != nil || st.State != "missing" {
		t.Fatalf("quitar sin entrada: %+v %v", st, err)
	}
	for _, c := range f.calls {
		if c[2] == "remove" {
			t.Errorf("no había nada que quitar: %q", c)
		}
	}

	// Agregar: argv exacto (sin shell, sin --workspace) y estado final ok.
	f.calls = nil
	st, err := a.AddAntigravityMCP()
	if err != nil || st.State != "ok" || st.Current != bin+" --mcp" {
		t.Fatalf("agregar: %+v %v", st, err)
	}
	wantAdd := []string{"/usr/local/bin/agy", "mcp", "add", "--", "scadlab", bin, "--mcp"}
	if !reflect.DeepEqual(f.calls[0], wantAdd) {
		t.Errorf("add argv:\n got %q\nwant %q", f.calls[0], wantAdd)
	}
	if last := f.calls[len(f.calls)-1]; !reflect.DeepEqual(last, []string{"/usr/local/bin/agy", "mcp", "list"}) {
		t.Errorf("tras agregar se vuelve a leer el estado: %q", last)
	}

	// La app se movió: la entrada queda con otra ruta y agregar la corrige.
	withMCPBinary(t, "/Nuevo/ScadLab")
	if st, _ := a.AntigravityMCPStatus(); st.State != "other" || st.Current != bin+" --mcp" {
		t.Fatalf("otra ruta: %+v", st)
	}
	if st, err := a.AddAntigravityMCP(); err != nil || st.State != "ok" || len(f.servers) != 1 {
		t.Fatalf("corregir: %+v %v %v", st, err, f.servers)
	}

	// Quitar: argv exacto y vuelve a «missing».
	f.calls = nil
	st, err = a.RemoveAntigravityMCP()
	if err != nil || st.State != "missing" {
		t.Fatalf("quitar: %+v %v", st, err)
	}
	var removes [][]string
	for _, c := range f.calls {
		if c[2] == "remove" {
			removes = append(removes, c)
		}
	}
	if want := [][]string{{"/usr/local/bin/agy", "mcp", "remove", "scadlab"}}; !reflect.DeepEqual(removes, want) {
		t.Errorf("remove argv: %q", removes)
	}
}

func TestAntigravityMCPKeepsOtherServers(t *testing.T) {
	withMCPBinary(t, "/Apps/ScadLab")
	a := newTestApp(t)
	f := withFakeAgy(t, "/bin/agy")
	f.servers["fs"] = "npx -y server-filesystem /work"
	if _, err := a.AddAntigravityMCP(); err != nil {
		t.Fatal(err)
	}
	if _, err := a.RemoveAntigravityMCP(); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.servers["fs"]; !ok || len(f.servers) != 1 {
		t.Errorf("los otros servidores quedan como estaban: %v", f.servers)
	}
}

func TestAntigravityMCPErrors(t *testing.T) {
	withMCPBinary(t, "/Apps/ScadLab")
	a := newTestApp(t)
	f := withFakeAgy(t, "/bin/agy")

	f.fail["list"], f.out["list"] = errors.New("exit status 2"), "boom en list\n"
	st, _ := a.AntigravityMCPStatus()
	if st.State != "error" || !strings.Contains(st.Detail, "boom en list") {
		t.Errorf("list falla: %+v", st)
	}
	if _, err := a.RemoveAntigravityMCP(); err == nil {
		t.Error("quitar con agy roto debe fallar")
	}
	delete(f.fail, "list")

	f.fail["add"], f.out["add"] = errors.New("exit status 1"), "no se pudo escribir\n"
	if _, err := a.AddAntigravityMCP(); err == nil || !strings.Contains(err.Error(), "no se pudo escribir") {
		t.Errorf("add falla: %v", err)
	}
	delete(f.fail, "add")

	// Sin ejecutable de ScadLab no se agrega nada.
	prev := mcpBinary
	mcpBinary = func() (string, error) { return "", errors.New("sin exe") }
	t.Cleanup(func() { mcpBinary = prev })
	f.calls = nil
	if _, err := a.AddAntigravityMCP(); err == nil || len(f.calls) != 0 {
		t.Errorf("sin ejecutable: %v %q", err, f.calls)
	}
}
