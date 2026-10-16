package mcpconf

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testBin = "/Applications/ScadLab.app/Contents/MacOS/ScadLab"

func write(t *testing.T, dir, rel, body string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func status(t *testing.T, r Report, rel string) string {
	t.Helper()
	for _, f := range r.Files {
		if f.Name == rel {
			return f.Status
		}
	}
	t.Fatalf("el informe no trae %s: %+v", rel, r.Files)
	return ""
}

func mustSync(t *testing.T, dir string, enabled bool) Report {
	t.Helper()
	r, err := Sync(dir, testBin, enabled)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("JSON inválido: %v\n%s", err, s)
	}
	return m
}

func TestSyncCreatesThreeFiles(t *testing.T) {
	dir := t.TempDir()
	r := mustSync(t, dir, true)
	for _, rel := range []string{".mcp.json", "opencode.json", ".gemini/settings.json"} {
		if got := status(t, r, rel); got != StatusCreated {
			t.Errorf("%s: %s, quería created", rel, got)
		}
	}
	wantArgs := []any{"--mcp", "--workspace", dir}

	claude := decode(t, read(t, dir, ".mcp.json"))
	srv := claude["mcpServers"].(map[string]any)[ServerName].(map[string]any)
	if srv["command"] != testBin || !reflect.DeepEqual(srv["args"], wantArgs) {
		t.Errorf(".mcp.json: %v", srv)
	}

	oc := decode(t, read(t, dir, "opencode.json"))
	e := oc["mcp"].(map[string]any)[ServerName].(map[string]any)
	wantCmd := append([]any{testBin}, wantArgs...)
	if e["type"] != "local" || e["enabled"] != true || !reflect.DeepEqual(e["command"], wantCmd) {
		t.Errorf("opencode.json: %v", e)
	}
	if oc["$schema"] == nil {
		t.Error("opencode.json nuevo sin $schema")
	}

	gem := decode(t, read(t, dir, ".gemini/settings.json"))
	g := gem["mcpServers"].(map[string]any)[ServerName].(map[string]any)
	if g["command"] != testBin || !reflect.DeepEqual(g["args"], wantArgs) {
		t.Errorf(".gemini/settings.json: %v", g)
	}
}

func TestSyncIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	mustSync(t, dir, true)
	before := map[string]string{}
	for _, rel := range []string{".mcp.json", "opencode.json", ".gemini/settings.json"} {
		before[rel] = read(t, dir, rel)
	}
	r := mustSync(t, dir, true)
	for rel, body := range before {
		if got := status(t, r, rel); got != StatusUnchange {
			t.Errorf("%s: %s, quería unchanged", rel, got)
		}
		if read(t, dir, rel) != body {
			t.Errorf("%s cambió en una segunda pasada", rel)
		}
	}
}

func TestSyncMergePreservesEverythingElse(t *testing.T) {
	cases := []struct {
		name string
		rel  string
		in   string
	}{
		{"dos espacios, otro servidor", ".mcp.json", "{\n  \"zeta\": 1,\n  \"mcpServers\": {\n    \"otro\": {\n      \"command\": \"x\"\n    }\n  },\n  \"alfa\": [1, 2]\n}\n"},
		{"cuatro espacios", ".mcp.json", "{\n    \"mcpServers\": {\n        \"otro\": {\"command\": \"x\"}\n    }\n}\n"},
		{"tabuladores", ".gemini/settings.json", "{\n\t\"theme\": \"dark\",\n\t\"mcpServers\": {\n\t\t\"otro\": {\"command\": \"x\"}\n\t}\n}\n"},
		{"sin el bloque", ".gemini/settings.json", "{\n  \"theme\": \"dark\",\n  \"general\": {\"vimMode\": true}\n}\n"},
		{"bloque vacío", ".mcp.json", "{\n  \"mcpServers\": {}\n}\n"},
		{"bloque null", ".mcp.json", "{\n  \"a\": 1,\n  \"mcpServers\": null\n}\n"},
		{"objeto vacío", ".mcp.json", "{}\n"},
		{"todo en una línea", ".mcp.json", `{"a":1,"mcpServers":{"otro":{"command":"x"}}}`},
		{"CRLF", ".mcp.json", "{\r\n  \"a\": 1,\r\n  \"mcpServers\": {\r\n    \"otro\": {\"command\": \"x\"}\r\n  }\r\n}\r\n"},
		{"opencode con otras claves", "opencode.json", "{\n  \"$schema\": \"https://opencode.ai/config.json\",\n  \"model\": \"anthropic/x\",\n  \"mcp\": {\n    \"otro\": {\"type\": \"remote\", \"url\": \"https://e.com\"}\n  },\n  \"theme\": \"tokyonight\"\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, dir, c.rel, c.in)
			r := mustSync(t, dir, true)
			if got := status(t, r, c.rel); got != StatusUpdated {
				t.Fatalf("estado %s", got)
			}
			out := read(t, dir, c.rel)
			var before, after map[string]any
			_ = json.Unmarshal([]byte(c.in), &before)
			after = decode(t, out)
			// Todo lo que había sigue igual (salvo el bloque, que gana scadlab).
			top := "mcpServers"
			if c.rel == "opencode.json" {
				top = "mcp"
			}
			for k, v := range before {
				if k == top {
					continue
				}
				if !reflect.DeepEqual(after[k], v) {
					t.Errorf("clave %q cambió: %v -> %v", k, v, after[k])
				}
			}
			if bm, ok := before[top].(map[string]any); ok {
				am := after[top].(map[string]any)
				for k, v := range bm {
					if !reflect.DeepEqual(am[k], v) {
						t.Errorf("servidor %q cambió", k)
					}
				}
			}
			if after[top].(map[string]any)[ServerName] == nil {
				t.Error("falta scadlab")
			}
			// El orden de las claves de la raíz y su estilo se conservan: el
			// archivo viejo es prefijo del nuevo salvo el empalme.
			if keys := rootKeys(t, c.in); len(keys) > 0 {
				got := rootKeys(t, out)
				if !reflect.DeepEqual(got[:len(keys)], keys) {
					t.Errorf("orden de claves: %v -> %v", keys, got)
				}
			}
			if strings.Contains(c.in, "\r\n") && strings.Contains(strings.ReplaceAll(out, "\r\n", ""), "\n") {
				t.Errorf("mezcló saltos de línea:\n%q", out)
			}
			// Segunda pasada: nada que hacer.
			if got := status(t, mustSync(t, dir, true), c.rel); got != StatusUnchange {
				t.Errorf("segunda pasada: %s", got)
			}
		})
	}
}

// rootKeys devuelve las claves de la raíz en el orden en que están escritas.
func rootKeys(t *testing.T, s string) []string {
	t.Helper()
	d := []byte(s)
	o, err := scanObject(d, skipWS(d, 0))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, m := range o.members {
		keys = append(keys, m.key)
	}
	return keys
}

func TestSyncKeepsFormattingByteForByte(t *testing.T) {
	dir := t.TempDir()
	in := "{\n  \"a\":   1,\n  \"mcpServers\": {\n      \"otro\" : {\"command\":\"x\"}\n  }\n}\n"
	write(t, dir, ".mcp.json", in)
	mustSync(t, dir, true)
	out := read(t, dir, ".mcp.json")
	// El texto anterior a la inserción no cambia ni un byte.
	prefix := strings.TrimSuffix(in, "\n  }\n}\n")
	if !strings.HasPrefix(out, prefix) {
		t.Errorf("se reformateó lo ajeno:\n%s", out)
	}
	mustSync(t, dir, false)
	if got := read(t, dir, ".mcp.json"); got != in {
		t.Errorf("al quitar scadlab no volvió al original:\n%s", got)
	}
}

func TestSyncUpdatesWhenBinaryMoves(t *testing.T) {
	dir := t.TempDir()
	if _, err := Sync(dir, "/old/ScadLab", true); err != nil {
		t.Fatal(err)
	}
	r := mustSync(t, dir, true)
	for _, rel := range []string{".mcp.json", "opencode.json", ".gemini/settings.json"} {
		if got := status(t, r, rel); got != StatusUpdated {
			t.Errorf("%s: %s", rel, got)
		}
	}
	if !strings.Contains(read(t, dir, ".mcp.json"), testBin) || strings.Contains(read(t, dir, ".mcp.json"), "/old/") {
		t.Error("no actualizó la ruta")
	}
	if got := status(t, mustSync(t, dir, true), ".mcp.json"); got != StatusUnchange {
		t.Errorf("tras mover: %s", got)
	}
}

func TestSyncKeepsUserFieldsInsideOurEntry(t *testing.T) {
	dir := t.TempDir()
	in := `{"mcpServers":{"scadlab":{"command":"/viejo","args":[],"timeout":60000,"env":{"A":"1"}}}}`
	write(t, dir, ".mcp.json", in)
	mustSync(t, dir, true)
	e := decode(t, read(t, dir, ".mcp.json"))["mcpServers"].(map[string]any)[ServerName].(map[string]any)
	if e["command"] != testBin || e["timeout"] != float64(60000) || e["env"] == nil {
		t.Errorf("entrada: %v", e)
	}
}

func TestSyncInvalidJSONIsLeftAlone(t *testing.T) {
	for _, body := range []string{"{ no es json", `{"a": 1,}`, `[1,2]`, `"texto"`} {
		dir := t.TempDir()
		p := write(t, dir, ".mcp.json", body)
		r := mustSync(t, dir, true)
		if got := status(t, r, ".mcp.json"); got != StatusInvalid {
			t.Errorf("%q: %s", body, got)
		}
		if b, _ := os.ReadFile(p); string(b) != body {
			t.Errorf("tocó un archivo roto: %q", b)
		}
		// Los demás archivos sí se crean.
		if got := status(t, r, "opencode.json"); got != StatusCreated {
			t.Errorf("opencode.json: %s", got)
		}
		// Y apagado tampoco lo toca.
		mustSync(t, dir, false)
		if b, _ := os.ReadFile(p); string(b) != body {
			t.Errorf("apagar tocó un archivo roto: %q", b)
		}
	}
}

func TestSyncBlockThatIsNotAnObject(t *testing.T) {
	dir := t.TempDir()
	body := `{"mcpServers": ["x"]}`
	p := write(t, dir, ".mcp.json", body)
	if got := status(t, mustSync(t, dir, true), ".mcp.json"); got != StatusInvalid {
		t.Errorf("estado %s", got)
	}
	if b, _ := os.ReadFile(p); string(b) != body {
		t.Error("lo tocó")
	}
}

func TestSyncRemove(t *testing.T) {
	t.Run("deja las otras claves", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, ".mcp.json", "{\n  \"mcpServers\": {\n    \"otro\": {\"command\": \"x\"},\n    \"scadlab\": {\"command\": \"y\"}\n  },\n  \"z\": 1\n}\n")
		r := mustSync(t, dir, false)
		if got := status(t, r, ".mcp.json"); got != StatusRemoved {
			t.Fatal(got)
		}
		m := decode(t, read(t, dir, ".mcp.json"))
		srv := m["mcpServers"].(map[string]any)
		if srv[ServerName] != nil || srv["otro"] == nil || m["z"] != float64(1) {
			t.Errorf("resultado: %v", m)
		}
	})
	t.Run("único servidor con más claves: quita el bloque", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, ".mcp.json", "{\n  \"a\": 1,\n  \"mcpServers\": {\n    \"scadlab\": {\"command\": \"y\"}\n  }\n}\n")
		mustSync(t, dir, false)
		m := decode(t, read(t, dir, ".mcp.json"))
		if _, ok := m["mcpServers"]; ok || m["a"] != float64(1) {
			t.Errorf("resultado: %v", m)
		}
	})
	t.Run("archivo que solo era nuestro se borra", func(t *testing.T) {
		dir := t.TempDir()
		mustSync(t, dir, true)
		r := mustSync(t, dir, false)
		for _, rel := range []string{".mcp.json", "opencode.json", ".gemini/settings.json"} {
			if got := status(t, r, rel); got != StatusRemoved {
				t.Errorf("%s: %s", rel, got)
			}
			if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); !os.IsNotExist(err) {
				t.Errorf("%s sigue en disco", rel)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, ".gemini")); !os.IsNotExist(err) {
			t.Error(".gemini vacío quedó en disco")
		}
	})
	t.Run("sin nada que quitar", func(t *testing.T) {
		dir := t.TempDir()
		r := mustSync(t, dir, false)
		for _, f := range r.Files {
			if f.Status != StatusUnchange {
				t.Errorf("%s: %s", f.Name, f.Status)
			}
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("creó archivos: %v", entries)
		}
	})
}

func TestOpenCodeJSONC(t *testing.T) {
	t.Run("con comentarios se salta y no se crea otro", func(t *testing.T) {
		dir := t.TempDir()
		body := "{\n  // mi modelo\n  \"model\": \"x\"\n}\n"
		p := write(t, dir, "opencode.jsonc", body)
		r := mustSync(t, dir, true)
		if got := status(t, r, "opencode.jsonc"); got != StatusInvalid {
			t.Fatalf("estado %s", got)
		}
		if b, _ := os.ReadFile(p); string(b) != body {
			t.Error("tocó el .jsonc")
		}
		if _, err := os.Stat(filepath.Join(dir, "opencode.json")); !os.IsNotExist(err) {
			t.Error("creó opencode.json junto al .jsonc")
		}
	})
	t.Run("sin comentarios se edita", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "opencode.jsonc", "{\n  \"model\": \"x\"\n}\n")
		if got := status(t, mustSync(t, dir, true), "opencode.jsonc"); got != StatusUpdated {
			t.Fatal(got)
		}
		if decode(t, read(t, dir, "opencode.jsonc"))["mcp"] == nil {
			t.Error("sin entrada")
		}
	})
	t.Run("opencode.json gana si existen los dos", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "opencode.json", "{}\n")
		write(t, dir, "opencode.jsonc", "{ // c\n}\n")
		r := mustSync(t, dir, true)
		if got := status(t, r, "opencode.json"); got != StatusUpdated {
			t.Fatal(got)
		}
	})
}

func TestBOMIsKept(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".mcp.json", "\xef\xbb\xbf{\n  \"a\": 1\n}\n")
	mustSync(t, dir, true)
	if out := read(t, dir, ".mcp.json"); !strings.HasPrefix(out, "\xef\xbb\xbf{") {
		t.Errorf("perdió el BOM: %q", out)
	}
}

func TestEmptyFileIsFilled(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, ".mcp.json", "  \n")
	if got := status(t, mustSync(t, dir, true), ".mcp.json"); got != StatusUpdated {
		t.Fatal(got)
	}
	decode(t, read(t, dir, ".mcp.json"))
}

func TestWindowsPathsAndAmpersand(t *testing.T) {
	dir := t.TempDir()
	bin := `C:\Program Files\ScadLab & Co\ScadLab.exe`
	if _, err := Sync(dir, bin, true); err != nil {
		t.Fatal(err)
	}
	raw := read(t, dir, ".mcp.json")
	if strings.Contains(raw, `\u0026`) {
		t.Errorf("escapó el &: %s", raw)
	}
	e := decode(t, raw)["mcpServers"].(map[string]any)[ServerName].(map[string]any)
	if e["command"] != bin {
		t.Errorf("command: %v", e["command"])
	}
}

func TestInspectStates(t *testing.T) {
	dir := t.TempDir()
	check := func(enabled bool, want map[string]string) {
		t.Helper()
		r, err := Inspect(dir, testBin, enabled)
		if err != nil {
			t.Fatal(err)
		}
		for rel, w := range want {
			if got := status(t, r, rel); got != w {
				t.Errorf("enabled=%v %s: %s, quería %s", enabled, rel, got, w)
			}
		}
	}
	all := func(s string) map[string]string {
		return map[string]string{".mcp.json": s, "opencode.json": s, ".gemini/settings.json": s}
	}
	check(true, all(StateMissing))
	check(false, all(StateOff))
	// Inspect no escribe.
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("Inspect escribió: %v", entries)
	}
	mustSync(t, dir, true)
	check(true, all(StateOK))
	check(false, all(StateStale))
	write(t, dir, ".mcp.json", `{"mcpServers":{"scadlab":{"command":"/otro","args":[]}}}`)
	write(t, dir, ".gemini/settings.json", `{"theme":"x"}`)
	check(true, map[string]string{".mcp.json": StateOutdated, ".gemini/settings.json": StateMissing, "opencode.json": StateOK})
	write(t, dir, "opencode.json", "no json")
	check(true, map[string]string{"opencode.json": StateInvalid})
}

func TestSyncMissingWorkspace(t *testing.T) {
	if _, err := Sync(filepath.Join(t.TempDir(), "no-existe"), testBin, true); err == nil {
		t.Error("quería error con un workspace inexistente")
	}
	if _, err := Sync(t.TempDir(), "", true); err == nil {
		t.Error("quería error sin ruta del binario")
	}
}

func TestCodexArgs(t *testing.T) {
	cases := []struct {
		bin, dir string
		want     []string
	}{
		{"/Apps/ScadLab", "/ws/a", []string{
			"-c", `mcp_servers.scadlab.command="/Apps/ScadLab"`,
			"-c", `mcp_servers.scadlab.args=["--mcp","--workspace","/ws/a"]`,
		}},
		{`C:\Program Files\ScadLab\ScadLab.exe`, `C:\Users\ana\ScadLab\caja "x"`, []string{
			"-c", `mcp_servers.scadlab.command="C:\\Program Files\\ScadLab\\ScadLab.exe"`,
			"-c", `mcp_servers.scadlab.args=["--mcp","--workspace","C:\\Users\\ana\\ScadLab\\caja \"x\""]`,
		}},
	}
	for _, c := range cases {
		if got := CodexArgs(c.bin, c.dir); !reflect.DeepEqual(got, c.want) {
			t.Errorf("CodexArgs(%q, %q)\n got %q\nwant %q", c.bin, c.dir, got, c.want)
		}
	}
}

func TestCommandQuotesSpaces(t *testing.T) {
	got := Command("/Apps/Scad Lab/ScadLab", "/ws/a")
	if got != `"/Apps/Scad Lab/ScadLab" --mcp --workspace /ws/a` {
		t.Errorf("Command: %s", got)
	}
}

func TestClaudeArgs(t *testing.T) {
	got := ClaudeArgs("/Apps/Scad Lab/ScadLab", "/ws/mi caja")
	if len(got) != 2 || got[0] != "--mcp-config" {
		t.Fatalf("flags: %q", got)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(got[1]), &cfg); err != nil {
		t.Fatalf("JSON inválido: %v", err)
	}
	s := cfg.MCPServers[ServerName]
	if s.Command != "/Apps/Scad Lab/ScadLab" || strings.Join(s.Args, "|") != "--mcp|--workspace|/ws/mi caja" {
		t.Errorf("servidor: %+v", s)
	}
}

// .gemini/settings.json es del Gemini CLI anterior: Antigravity (agy) no lo lee.
func TestGeminiFileIsNotAntigravity(t *testing.T) {
	r, err := Inspect(t.TempDir(), testBin, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range r.Files {
		if f.Name == ".gemini/settings.json" && f.Agent != AgentGemini {
			t.Errorf("agente %q, quería %q", f.Agent, AgentGemini)
		}
		if f.Agent == "antigravity" {
			t.Errorf("ningún archivo de proyecto es de Antigravity: %+v", f)
		}
	}
}
