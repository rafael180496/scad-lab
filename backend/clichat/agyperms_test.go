package clichat

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// Pruebas de los permisos de las herramientas de ScadLab en el settings.json de
// Antigravity (S12.c, T14). TODAS trabajan en un directorio temporal: ninguna toca ni
// lee el ~/.gemini real, y ninguna lanza el `agy` real.

type permRig struct {
	t       *testing.T
	path    string
	backups string
}

func newPermRig(t *testing.T) *permRig {
	t.Helper()
	root := t.TempDir()
	return &permRig{t: t, path: filepath.Join(root, "antigravity-cli", "settings.json"), backups: filepath.Join(root, "datos", "backups")}
}

func (r *permRig) write(body string) {
	r.t.Helper()
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(r.path, []byte(body), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *permRig) read() string {
	r.t.Helper()
	raw, err := os.ReadFile(r.path)
	if err != nil {
		r.t.Fatal(err)
	}
	return string(raw)
}

func (r *permRig) sync() AgyPermReport {
	r.t.Helper()
	rep, err := SyncAgyPermissions(r.path, r.backups)
	if err != nil {
		r.t.Fatalf("sync: %v", err)
	}
	return rep
}

func (r *permRig) remove() AgyPermReport {
	r.t.Helper()
	rep, err := RemoveAgyPermissions(r.path, r.backups)
	if err != nil {
		r.t.Fatalf("remove: %v", err)
	}
	return rep
}

func (r *permRig) backupFiles() []string {
	files, _ := filepath.Glob(filepath.Join(r.backups, "settings-*.json"))
	return files
}

// parsed devuelve el archivo como mapa de valores crudos y compactos, para comparar
// el contenido sin depender del formato.
func parsed(t *testing.T, body string) map[string]string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("no es JSON: %v\n%s", err, body)
	}
	out := map[string]string{}
	for k, v := range m {
		var b bytes.Buffer
		if err := json.Compact(&b, v); err != nil {
			t.Fatal(err)
		}
		out[k] = b.String()
	}
	return out
}

func allowOf(t *testing.T, body string) []string {
	t.Helper()
	var f struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(body), &f); err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	return f.Permissions.Allow
}

func keysInOrder(t *testing.T, body string) []string {
	t.Helper()
	obj, err := parseObject([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, m := range obj {
		keys = append(keys, m.key)
	}
	return keys
}

// ── Las reglas ──────────────────────────────────────────────────────────────

func TestAgyRulesAreTheExplicitCatalogAndNothingElse(t *testing.T) {
	rules := AgyRules()
	var want []string
	for _, tool := range append(ReadTools(), WriteTools()...) {
		want = append(want, "mcp(scadlab/"+tool+")")
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("las reglas son exactamente las herramientas de modes.go, de lectura y de escritura:\n%v\n%v", rules, want)
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if !IsAgyRule(r) || strings.ContainsAny(r, "*?") || seen[r] {
			t.Errorf("regla inválida, con comodín o repetida: %q", r)
		}
		seen[r] = true
	}
	// De lectura Y de escritura: «Solo consulta» lo hace cumplir el servidor.
	if !seen["mcp(scadlab/list_checkpoints)"] || !seen["mcp(scadlab/create_checkpoint)"] || !seen["mcp(scadlab/export)"] {
		t.Errorf("faltan herramientas del catálogo: %v", rules)
	}
}

// update_checkpoint (S16) entra en la lista de reglas, y un settings.json que ya
// tenía las reglas de antes (sin ella) recibe SOLO esa regla al sincronizar.
func TestSyncAddsOnlyUpdateCheckpointToOldRules(t *testing.T) {
	rules := AgyRules()
	var old []string
	for _, rule := range rules {
		if rule != "mcp(scadlab/update_checkpoint)" {
			old = append(old, rule)
		}
	}
	if len(old) != len(rules)-1 {
		t.Fatalf("AgyRules debe incluir mcp(scadlab/update_checkpoint): %v", rules)
	}
	r := newPermRig(t)
	body, err := json.Marshal(map[string]any{
		"theme":       "dark",
		"permissions": map[string]any{"allow": append([]string{"command(git status)"}, old...), "deny": []string{"command(rm)"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.write(string(body))
	rep := r.sync()
	if !rep.Changed || !reflect.DeepEqual(rep.Added, []string{"mcp(scadlab/update_checkpoint)"}) || len(rep.Removed) != 0 {
		t.Fatalf("informe: %+v", rep)
	}
	allow := allowOf(t, r.read())
	if !slices.Contains(allow, "mcp(scadlab/update_checkpoint)") || !slices.Contains(allow, "command(git status)") || len(allow) != len(rules)+1 {
		t.Errorf("allow: %v", allow)
	}
	if got := parsed(t, r.read()); got["theme"] != `"dark"` {
		t.Errorf("lo ajeno cambió: %v", got)
	}
	if again := r.sync(); again.Changed {
		t.Errorf("segunda vez: %+v", again)
	}
}

func TestIsAgyRuleOnlyMatchesScadlabToolRules(t *testing.T) {
	for rule, want := range map[string]bool{
		"mcp(scadlab/render)":              true,
		"mcp(scadlab/trace_image_outline)": true,
		"mcp(scadlab/*)":                   false,
		"mcp(*)":                           false,
		"mcp(scadlab/)":                    false,
		"mcp(scadlab)":                     false,
		"mcp(otro/render)":                 false,
		"mcp(scadlab/a b)":                 false,
		"mcp(scadlab/render)x":             false,
		"xmcp(scadlab/render)":             false,
		"command(rm -rf /)":                false,
		"mcp(scadlab/render,x)":            false,
		"":                                 false,
	} {
		if got := IsAgyRule(rule); got != want {
			t.Errorf("IsAgyRule(%q) = %v, quería %v", rule, got, want)
		}
	}
}

// ── Sincronizar ─────────────────────────────────────────────────────────────

func TestSyncCreatesTheFileWhenThereIsNone(t *testing.T) {
	r := newPermRig(t)
	rep := r.sync()

	rules := AgyRules()
	if !rep.Changed || rep.Existed || len(rep.Added) != len(rules) || rep.Approved != len(rules) || rep.Total != len(rules) || rep.Backup != "" {
		t.Fatalf("informe: %+v", rep)
	}
	if got := allowOf(t, r.read()); !reflect.DeepEqual(got, rules) {
		t.Errorf("allow: %v", got)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(r.path); st.Mode().Perm() != 0o600 {
			t.Errorf("modo %v, como el de Antigravity", st.Mode().Perm())
		}
	}
	if len(r.backupFiles()) != 0 {
		t.Error("no había nada que copiar")
	}
	// Idempotente: no cambia nada y no reescribe.
	before := r.read()
	again := r.sync()
	if again.Changed || len(again.Added) != 0 || len(again.Removed) != 0 || again.Approved != len(rules) {
		t.Errorf("segunda vez: %+v", again)
	}
	if r.read() != before {
		t.Error("la segunda vez cambió el archivo")
	}
}

func TestSyncTreatsAnEmptyFileAsAnEmptyObject(t *testing.T) {
	r := newPermRig(t)
	r.write("  \n")
	if rep := r.sync(); !rep.Changed || len(allowOf(t, r.read())) != len(AgyRules()) {
		t.Errorf("%+v", rep)
	}
}

func TestSyncKeepsTrustedWorkspacesAndTheKeyOrder(t *testing.T) {
	r := newPermRig(t)
	orig := "{\n  \"trustedWorkspaces\": [\n    \"/Users/yo\",\n    \"/Users/yo/proyecto\"\n  ]\n}\n"
	r.write(orig)
	rep := r.sync()

	if !rep.Changed || !rep.Existed || rep.Backup == "" {
		t.Fatalf("informe: %+v", rep)
	}
	got := r.read()
	if k := keysInOrder(t, got); !reflect.DeepEqual(k, []string{"trustedWorkspaces", "permissions"}) {
		t.Errorf("claves: %v", k)
	}
	if p := parsed(t, got); p["trustedWorkspaces"] != `["/Users/yo","/Users/yo/proyecto"]` {
		t.Errorf("trustedWorkspaces cambió: %s", p["trustedWorkspaces"])
	}
	// La copia es el archivo original, byte a byte, en la carpeta de datos.
	if b, err := os.ReadFile(rep.Backup); err != nil || string(b) != orig {
		t.Errorf("copia: %q %v", b, err)
	}
	if filepath.Dir(rep.Backup) != r.backups {
		t.Errorf("la copia va a la carpeta de datos: %s", rep.Backup)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(r.path); st.Mode().Perm() != 0o600 {
			t.Errorf("conserva el modo: %v", st.Mode().Perm())
		}
	}
}

func TestSyncCombinesWithTheUsersOwnRulesAndLeavesTheRestAlone(t *testing.T) {
	r := newPermRig(t)
	r.write(`{
  "trustedWorkspaces": ["/a"],
  "model": "gemini-4",
  "temperature": 1.50,
  "permissions": {
    "deny": ["command(rm *)"],
    "allow": ["command(git status)", "mcp(github/search)", "mcp(scadlab/*)", "mcp(*)"],
    "ask": ["mcp(scadlab/export)"],
    "extra": {"a": [1, 2, 3]}
  },
  "toolPermission": "request-review"
}`)
	before := parsed(t, r.read())
	r.sync()
	got := r.read()
	after := parsed(t, got)

	for _, k := range []string{"trustedWorkspaces", "model", "temperature", "toolPermission"} {
		if before[k] != after[k] {
			t.Errorf("%s cambió: %s → %s", k, before[k], after[k])
		}
	}
	if after["temperature"] != "1.50" {
		t.Errorf("el número se conserva tal cual: %s", after["temperature"])
	}
	var perms map[string]json.RawMessage
	_ = json.Unmarshal([]byte(after["permissions"]), &perms)
	for _, k := range []string{"deny", "ask", "extra"} {
		var b bytes.Buffer
		json.Compact(&b, perms[k])
		var prev map[string]json.RawMessage
		json.Unmarshal([]byte(before["permissions"]), &prev)
		var pb bytes.Buffer
		json.Compact(&pb, prev[k])
		if b.String() != pb.String() {
			t.Errorf("permissions.%s cambió: %s → %s", k, pb.String(), b.String())
		}
	}
	if k := keysInOrder(t, got); !reflect.DeepEqual(k, []string{"trustedWorkspaces", "model", "temperature", "permissions", "toolPermission"}) {
		t.Errorf("el orden de las claves se conserva: %v", k)
	}
	allow := allowOf(t, got)
	if !reflect.DeepEqual(allow[:4], []string{"command(git status)", "mcp(github/search)", "mcp(scadlab/*)", "mcp(*)"}) {
		t.Errorf("las reglas del usuario quedan primero y en su lugar: %v", allow[:4])
	}
	if !reflect.DeepEqual(allow[4:], AgyRules()) {
		t.Errorf("las de ScadLab, al final: %v", allow[4:])
	}
}

func TestSyncDropsStaleAndDuplicatedScadlabRulesButNothingElse(t *testing.T) {
	r := newPermRig(t)
	r.write(`{"permissions": {"allow": [
  "mcp(scadlab/render)",
  "mcp(scadlab/herramienta_que_ya_no_existe)",
  "command(ls)",
  "mcp(scadlab/render)",
  "mcp(otro/render)",
  "mcp(scadlab/*)"
]}}`)
	rep := r.sync()

	allow := allowOf(t, r.read())
	if allow[0] != "mcp(scadlab/render)" || allow[1] != "command(ls)" || allow[2] != "mcp(otro/render)" || allow[3] != "mcp(scadlab/*)" {
		t.Errorf("lo del usuario y la regla que ya estaba no se mueven: %v", allow[:4])
	}
	count := map[string]int{}
	for _, a := range allow {
		count[a]++
	}
	if count["mcp(scadlab/render)"] != 1 || count["mcp(scadlab/herramienta_que_ya_no_existe)"] != 0 {
		t.Errorf("sin duplicados ni sobrantes: %v", allow)
	}
	if !reflect.DeepEqual(rep.Removed, []string{"mcp(scadlab/herramienta_que_ya_no_existe)"}) {
		t.Errorf("removed: %v", rep.Removed)
	}
	if len(rep.Added) != len(AgyRules())-1 {
		t.Errorf("added: %d", len(rep.Added))
	}
}

func TestSyncNeverWritesAnythingButScadlabToolRules(t *testing.T) {
	corpus := []string{
		``,
		`{}`,
		`{"trustedWorkspaces": ["/a"]}`,
		`{"permissions": {}}`,
		`{"permissions": {"allow": []}}`,
		`{"permissions": null}`,
		`{"permissions": {"allow": null, "deny": ["x"]}}`,
		`{"permissions": {"allow": ["command(ls)", 5, null, {"a": 1}, "mcp(scadlab/*)"]}}`,
		"\xef\xbb\xbf{\"x\": 1}",
		`{"permissions": {"allow": ["mcp(scadlab/render)", "mcp(scadlab/viejo)", "mcp(scadlab/render)"]}, "permissions2": 1}`,
	}
	for i, body := range corpus {
		r := newPermRig(t)
		if body != "" {
			r.write(body)
		}
		before := nonOurs(t, body)
		r.sync()
		assertNoDuplicateKeys(t, r.read())
		if got := nonOurs(t, r.read()); !reflect.DeepEqual(got, before) {
			t.Errorf("[%d] después de sync cambió algo que no es de ScadLab:\nantes   %v\ndespués %v\n%s", i, before, got, r.read())
		}
		r.remove()
		if _, err := os.Stat(r.path); err == nil {
			if got := nonOurs(t, r.read()); !reflect.DeepEqual(got, before) {
				t.Errorf("[%d] después de quitar cambió algo que no es de ScadLab:\nantes   %v\ndespués %v", i, before, got)
			}
			if strings.Contains(r.read(), "mcp(scadlab/render)") || strings.Contains(r.read(), "mcp(scadlab/viejo)") {
				t.Errorf("[%d] quedó una regla de ScadLab:\n%s", i, r.read())
			}
		}
	}
}

// nonOurs resume un settings.json SIN las reglas mcp(scadlab/<nombre>): cada clave
// con su valor compacto, y de permissions.allow lo que no es nuestro. Los objetos
// que solo quedaron vacíos porque se les quitó lo nuestro no cuentan como cambio.
func nonOurs(t *testing.T, body string) map[string]string {
	t.Helper()
	out := map[string]string{}
	body = strings.TrimPrefix(body, "\xef\xbb\xbf")
	if strings.TrimSpace(body) == "" {
		return out
	}
	obj, err := parseObject([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range obj {
		if m.key == "permissions" && isNull(m.val) {
			continue // `permissions: null` es lo mismo que no tenerlo
		}
		if m.key != "permissions" {
			var b bytes.Buffer
			json.Compact(&b, m.val)
			out[m.key] = b.String()
			continue
		}
		perms, err := parseObject(m.val)
		if err != nil {
			t.Fatal(err)
		}
		for _, pm := range perms {
			if pm.key == "allow" && isNull(pm.val) {
				continue // un `allow: null` es lo mismo que no tenerlo
			}
			if pm.key != "allow" {
				var b bytes.Buffer
				json.Compact(&b, pm.val)
				out["permissions."+pm.key] = b.String()
				continue
			}
			var items []json.RawMessage
			json.Unmarshal(pm.val, &items)
			var keep []string
			for _, it := range items {
				if _, ours := ruleOf(it); !ours {
					var b bytes.Buffer
					json.Compact(&b, it)
					keep = append(keep, b.String())
				}
			}
			if len(keep) > 0 {
				out["permissions.allow"] = strings.Join(keep, "|")
			}
		}
	}
	return out
}

func TestSyncKeepsAByteOrderMark(t *testing.T) {
	r := newPermRig(t)
	r.write("\xef\xbb\xbf{\"trustedWorkspaces\": [\"/a\"]}")
	r.sync()
	if raw := r.read(); !strings.HasPrefix(raw, "\xef\xbb\xbf{") || len(allowOf(t, strings.TrimPrefix(raw, "\xef\xbb\xbf"))) == 0 {
		t.Errorf("BOM conservado y reglas agregadas: %q", raw[:20])
	}
	if b, _ := os.ReadFile(r.backupFiles()[0]); !strings.HasPrefix(string(b), "\xef\xbb\xbf") {
		t.Error("la copia es el archivo tal cual, con su BOM")
	}
}

// ── Lo que no se pisa ───────────────────────────────────────────────────────

func TestSyncDoesNotOverwriteAFileItCannotRead(t *testing.T) {
	for name, body := range map[string]string{
		"no es JSON":            `{"trustedWorkspaces": [no es json`,
		"un arreglo":            `["a", "b"]`,
		"un texto":              `"hola"`,
		"basura al final":       `{"a": 1} {"b": 2}`,
		"permissions no objeto": `{"permissions": ["mcp(scadlab/render)"]}`,
		"permissions un texto":  `{"permissions": "todo"}`,
		"allow no es arreglo":   `{"permissions": {"allow": "mcp(scadlab/render)"}}`,
		"allow un objeto":       `{"permissions": {"allow": {"a": 1}}}`,
		"comentarios":           "{\n// hola\n\"a\": 1}",
	} {
		r := newPermRig(t)
		r.write(body)
		for opName, op := range map[string]func() (AgyPermReport, error){
			"sync":   func() (AgyPermReport, error) { return SyncAgyPermissions(r.path, r.backups) },
			"remove": func() (AgyPermReport, error) { return RemoveAgyPermissions(r.path, r.backups) },
		} {
			_, err := op()
			if err == nil || err.Error() != ErrAgySettingsUnreadable.Error() {
				t.Errorf("%s/%s: error %v, quería ErrAgySettingsUnreadable", name, opName, err)
			}
			if got := r.read(); got != body {
				t.Errorf("%s/%s: el archivo del usuario no se toca: %q", name, opName, got)
			}
		}
		if len(r.backupFiles()) != 0 {
			t.Errorf("%s: sin cambios no hay copias", name)
		}
		if _, err := InspectAgyPermissions(r.path); err == nil {
			t.Errorf("%s: inspeccionar también avisa", name)
		}
	}
}

func TestSyncRefusesToWriteWithoutABackupFolder(t *testing.T) {
	r := newPermRig(t)
	body := `{"trustedWorkspaces": ["/a"]}`
	r.write(body)
	if _, err := SyncAgyPermissions(r.path, ""); err == nil {
		t.Fatal("sin carpeta de copias no se modifica un archivo ajeno")
	}
	if r.read() != body {
		t.Error("el archivo no se tocó")
	}
}

func TestSyncWritesThroughASymlinkWithoutReplacingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("los enlaces simbólicos piden privilegios en Windows")
	}
	r := newPermRig(t)
	real := filepath.Join(t.TempDir(), "dotfiles", "agy-settings.json")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte(`{"a": 1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, r.path); err != nil {
		t.Fatal(err)
	}
	r.sync()
	if st, err := os.Lstat(r.path); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("el enlace sigue siendo un enlace: %v %v", st, err)
	}
	if raw, _ := os.ReadFile(real); len(allowOf(t, string(raw))) == 0 {
		t.Errorf("se escribió el destino del enlace: %s", raw)
	}
}

func TestBackupsKeepOnlyTheLastOnes(t *testing.T) {
	r := newPermRig(t)
	r.write(`{"a": 1}`)
	for i := 0; i < agyBackupsKept+5; i++ {
		// Cada vuelta: quitar y volver a poner hace dos cambios, dos copias. El nombre
		// de la copia lleva milisegundos: en una máquina rápida dos copias seguidas
		// caían en el mismo y se pisaban.
		r.sync()
		time.Sleep(2 * time.Millisecond)
		r.remove()
		time.Sleep(2 * time.Millisecond)
	}
	if n := len(r.backupFiles()); n != agyBackupsKept {
		t.Errorf("copias: %d, quería %d", n, agyBackupsKept)
	}
}

// ── Quitar ──────────────────────────────────────────────────────────────────

func TestRemoveDeletesOnlyScadlabRulesAndTidiesWhatItEmptied(t *testing.T) {
	r := newPermRig(t)
	r.write(`{"trustedWorkspaces": ["/a"]}`)
	r.sync()
	rep := r.remove()
	if !rep.Changed || len(rep.Removed) != len(AgyRules()) || rep.Approved != 0 {
		t.Fatalf("informe: %+v", rep)
	}
	// No queda ni `allow` ni `permissions` vacíos: el archivo vuelve a lo que tenía.
	if k := keysInOrder(t, r.read()); !reflect.DeepEqual(k, []string{"trustedWorkspaces"}) {
		t.Errorf("claves: %v\n%s", k, r.read())
	}

	// Con reglas y claves del usuario, solo se van las nuestras.
	r.write(`{"permissions": {"deny": ["command(rm *)"], "allow": ["command(ls)", "mcp(scadlab/render)", "mcp(scadlab/*)"]}}`)
	r.remove()
	if got := allowOf(t, r.read()); !reflect.DeepEqual(got, []string{"command(ls)", "mcp(scadlab/*)"}) {
		t.Errorf("allow: %v", got)
	}
	if p := parsed(t, r.read()); !strings.Contains(p["permissions"], `"deny":["command(rm *)"]`) {
		t.Errorf("deny intacto: %s", p["permissions"])
	}
}

func TestRemoveWithNothingToRemoveDoesNotTouchTheFile(t *testing.T) {
	r := newPermRig(t)
	body := "{\n\t\"trustedWorkspaces\": [ \"/a\" ]\n}"
	r.write(body)
	if rep := r.remove(); rep.Changed || r.read() != body || len(r.backupFiles()) != 0 {
		t.Errorf("%+v", rep)
	}
	// Y sin archivo no lo crea.
	gone := newPermRig(t)
	if rep := gone.remove(); rep.Changed {
		t.Errorf("%+v", rep)
	}
	if _, err := os.Stat(gone.path); err == nil {
		t.Error("quitar no crea el archivo")
	}
}

// ── Estado ──────────────────────────────────────────────────────────────────

func TestInspectCountsApprovedAndStaleWithoutTouchingTheFile(t *testing.T) {
	r := newPermRig(t)
	if info, err := InspectAgyPermissions(r.path); err != nil || info.Exists || info.Approved != 0 || info.Total != len(AgyRules()) {
		t.Errorf("sin archivo: %+v %v", info, err)
	}
	body := `{"permissions": {"allow": ["mcp(scadlab/render)", "mcp(scadlab/lint)", "mcp(scadlab/ya_no_existe)", "command(ls)"]}}`
	r.write(body)
	info, err := InspectAgyPermissions(r.path)
	if err != nil || !info.Exists || info.Approved != 2 || info.Stale != 1 {
		t.Errorf("%+v %v", info, err)
	}
	if r.read() != body {
		t.Error("inspeccionar no escribe")
	}
}

// ── Limpieza del hooks.json heredado ────────────────────────────────────────

func TestRemoveLegacyAgyHookKeepsTheUsersEntries(t *testing.T) {
	dir := t.TempDir()
	hooks := filepath.Join(dir, ".agents", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooks), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{
  "mi-hook": {"Stop": [{"hooks": [{"command": "echo hecho"}]}]},
  "scadlab-managed": {"PreToolUse": [{"matcher": "call_mcp_tool", "hooks": [{"type": "command", "command": "'/x/ScadLab' --agy-hook --mode agent --lang es"}]}]},
  "otro": {"a": 1.0}
}`
	if err := os.WriteFile(hooks, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	removed, err := RemoveLegacyAgyHook(dir)
	if err != nil || !removed {
		t.Fatalf("%v %v", removed, err)
	}
	raw, _ := os.ReadFile(hooks)
	if strings.Contains(string(raw), "scadlab-managed") || strings.Contains(string(raw), "--agy-hook") {
		t.Errorf("la entrada de ScadLab sigue:\n%s", raw)
	}
	if k := keysInOrder(t, string(raw)); !reflect.DeepEqual(k, []string{"mi-hook", "otro"}) {
		t.Errorf("claves del usuario y su orden: %v", k)
	}
	if !strings.Contains(string(raw), `"a": 1.0`) {
		t.Errorf("el resto se conserva tal cual:\n%s", raw)
	}
	// Segunda vez: nada que quitar.
	if removed, err := RemoveLegacyAgyHook(dir); removed || err != nil {
		t.Errorf("idempotente: %v %v", removed, err)
	}
}

func TestRemoveLegacyAgyHookDeletesTheFileWhenItWasOnlyOurs(t *testing.T) {
	dir := t.TempDir()
	hooks := filepath.Join(dir, ".agents", "hooks.json")
	os.MkdirAll(filepath.Dir(hooks), 0o755)
	os.WriteFile(hooks, []byte(`{"scadlab-managed": {"PreToolUse": []}}`), 0o644)
	if removed, err := RemoveLegacyAgyHook(dir); !removed || err != nil {
		t.Fatalf("%v %v", removed, err)
	}
	if _, err := os.Stat(hooks); err == nil {
		t.Error("el archivo era solo de ScadLab: se borra")
	}
	if _, err := os.Stat(filepath.Dir(hooks)); err == nil {
		t.Error(".agents/ quedó vacía: se quita también")
	}

	// Pero una .agents/ con otras cosas (las skills de Codex) se queda.
	os.MkdirAll(filepath.Join(dir, ".agents", "skills"), 0o755)
	os.WriteFile(hooks, []byte(`{"scadlab-managed": {}}`), 0o644)
	RemoveLegacyAgyHook(dir)
	if _, err := os.Stat(filepath.Join(dir, ".agents", "skills")); err != nil {
		t.Error(".agents/skills no se toca")
	}
}

func TestRemoveLegacyAgyHookLeavesWhatIsNotOurs(t *testing.T) {
	dir := t.TempDir()
	if removed, err := RemoveLegacyAgyHook(dir); removed || err != nil {
		t.Errorf("sin archivo: %v %v", removed, err)
	}
	hooks := filepath.Join(dir, ".agents", "hooks.json")
	os.MkdirAll(filepath.Dir(hooks), 0o755)
	for _, body := range []string{`{"mio": {}}`, `{"mio": [no es json`, `["scadlab-managed"]`} {
		os.WriteFile(hooks, []byte(body), 0o644)
		if removed, err := RemoveLegacyAgyHook(dir); removed || err != nil {
			t.Errorf("%q: %v %v", body, removed, err)
		}
		if raw, _ := os.ReadFile(hooks); string(raw) != body {
			t.Errorf("no se toca: %q", raw)
		}
	}
}

func assertNoDuplicateKeys(t *testing.T, body string) {
	t.Helper()
	body = strings.TrimPrefix(body, "\xef\xbb\xbf")
	obj, err := parseObject([]byte(body))
	if err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	seen := map[string]bool{}
	for _, m := range obj {
		if seen[m.key] {
			t.Errorf("clave repetida %q:\n%s", m.key, body)
		}
		seen[m.key] = true
		if m.key == "permissions" {
			assertNoDuplicateKeys(t, string(m.val))
		}
	}
}

// Si Antigravity escribe el archivo entre que ScadLab lo lee y lo reemplaza (guarda
// sus carpetas de confianza ahí), ScadLab vuelve a empezar y no pierde ese cambio.
func TestSyncRetriesWhenTheFileChangesUnderIt(t *testing.T) {
	r := newPermRig(t)
	r.write(`{"trustedWorkspaces": ["/a"]}`)
	calls := 0
	afterAgyPlan = func() {
		calls++
		if calls == 1 {
			r.write(`{"trustedWorkspaces": ["/a", "/nueva-que-agrego-agy"]}`)
		}
	}
	t.Cleanup(func() { afterAgyPlan = func() {} })

	r.sync()
	got := r.read()
	if !strings.Contains(got, "/nueva-que-agrego-agy") {
		t.Errorf("se perdió el cambio de Antigravity:\n%s", got)
	}
	if len(allowOf(t, got)) != len(AgyRules()) || calls != 2 {
		t.Errorf("reglas puestas tras reintentar (llamadas %d):\n%s", calls, got)
	}
}
