package clichat

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"scadlab/backend/i18n"
)

// Permisos de las herramientas de ScadLab en Antigravity (S12.c, T14).
//
// El problema: en `agy --print` no hay nadie que pueda contestar un permiso, y
// Antigravity «soft-deniega» la primera herramienta MCP que no esté aprobada
// —`permission check failed for mcp "scadlab/printer_info": user denied
// permission`— y **termina el turno ahí** (en su log: `Print mode: soft-denying
// tool confirmation "CallMcpTool"`). Por eso el chat acababa «sin responder nada».
//
// Lo que se midió con `agy` 1.4.x, y por qué es esta solución y no otra:
//
//   - Los hooks (`<workspace>/.agents/hooks.json`) cargan —su log dice `loaded 1
//     named hooks`— pero NO sirven: la confirmación de permiso MCP ocurre antes de
//     ellos y el hook ni se ejecuta.
//   - `<workspace>/.agents/settings.json` con `permissions.allow` no se lee (el log
//     sigue en `permissions=<nil>`), y un proyecto propio en
//     `~/.gemini/config/projects/*.json` con `agentPermissions` o `permissionGrants`
//     da «Unknown field» / «no grants».
//   - Lo que SÍ funciona es lo que el propio agy pide en su error («Add an
//     allow-rule under permissions.allow in settings.json (e.g. mcp(<target>))»): en
//     `~/.gemini/antigravity-cli/settings.json`, `"permissions": {"allow":
//     ["mcp(scadlab/list_checkpoints)"]}`. Formato exacto: `mcp(<servidor>/<herramienta>)`.
//
// ScadLab gestiona ahí, y solo ahí, UNA regla por herramienta de su propio catálogo
// (las listas de modes.go: lectura y escritura). Nunca otra cosa: ni `command(...)`,
// ni `mcp(*)`, ni `mcp(scadlab/*)`; una lista explícita es lo que se puede auditar.
// El resto del archivo —`trustedWorkspaces`, las reglas del usuario, el orden de las
// claves— no se toca. El modo «Solo consulta» no puede viajar en una regla global:
// lo hace cumplir el servidor `--mcp` (backend/turnmark y backend/mcp).

// agyRuleRe reconoce una regla que ScadLab gestiona: exactamente
// `mcp(scadlab/<nombre>)`. Cualquier otra —un comodín del usuario, otro servidor—
// no es nuestra y nunca se toca.
var agyRuleRe = regexp.MustCompile(`^mcp\(` + regexp.QuoteMeta(mcpServerName) + `/[A-Za-z0-9_]+\)$`)

// AgyRule es la regla de una herramienta de ScadLab.
func AgyRule(tool string) string { return "mcp(" + mcpServerName + "/" + tool + ")" }

// IsAgyRule dice si es una regla `mcp(scadlab/<herramienta>)` (de las que gestiona
// ScadLab).
func IsAgyRule(rule string) bool { return agyRuleRe.MatchString(rule) }

// AgyRules son las reglas del catálogo de ScadLab, en el orden de modes.go
// (lectura y luego escritura): la lista explícita que se aprueba.
func AgyRules() []string {
	tools := append(append([]string(nil), readTools...), writeTools...)
	out := make([]string, len(tools))
	for i, t := range tools {
		out[i] = AgyRule(t)
	}
	return out
}

// AgySettingsPath es el settings.json de la CLI de Antigravity ("" si no se
// conoce el home).
func AgySettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
}

// ErrAgySettingsUnreadable: el settings.json de Antigravity existe pero no es un
// objeto JSON con la forma que se espera (o `permissions`/`allow` tienen otro
// tipo). ScadLab no lo pisa —es del usuario y de Antigravity— y el turno sigue sin
// los permisos.
var ErrAgySettingsUnreadable = i18n.New(i18n.Msg{
	ES: "el settings.json de Antigravity no es un objeto JSON válido (ScadLab no lo pisa: arréglalo o bórralo)",
	EN: "Antigravity's settings.json is not a valid JSON object (ScadLab will not overwrite it: fix or delete it)",
})

// AgyPermReport es lo que pasó con el archivo.
type AgyPermReport struct {
	Path    string   `json:"path"`
	Existed bool     `json:"existed"` // el archivo existía antes
	Changed bool     `json:"changed"` // se reescribió
	Added   []string `json:"added"`   // reglas `mcp(scadlab/…)` agregadas
	Removed []string `json:"removed"` // reglas `mcp(scadlab/…)` quitadas (sobrantes, o todas al quitar)
	// Approved: herramientas del catálogo con su regla en el archivo, después de la
	// operación. Total: las del catálogo.
	Approved int    `json:"approved"`
	Total    int    `json:"total"`
	Backup   string `json:"backup"` // la copia previa del archivo ("" si no hubo qué copiar)
}

// AgyPermInfo es el estado del archivo sin tocarlo.
type AgyPermInfo struct {
	Path     string `json:"path"`
	Exists   bool   `json:"exists"`
	Approved int    `json:"approved"` // herramientas del catálogo con regla
	Total    int    `json:"total"`
	Stale    int    `json:"stale"` // reglas mcp(scadlab/…) de herramientas que ya no existen
}

// agyBackupsKept es cuántas copias del settings.json se conservan.
const agyBackupsKept = 10

// afterAgyPlan es un punto de enganche de las pruebas: corre entre que se leyó el
// archivo y se comprueba que no cambió, justo donde Antigravity podría escribirlo.
var afterAgyPlan = func() {}

// agyPermMu serializa los cambios al archivo dentro del proceso.
var agyPermMu sync.Mutex

// permPlan es el settings.json leído, listo para editar solo `permissions.allow`.
type permPlan struct {
	raw     []byte // el archivo tal cual estaba (sin BOM)
	bom     bool
	existed bool
	top     orderedObj
	permsAt int // posición de "permissions" en top, o -1
	perms   orderedObj
	allowAt int // posición de "allow" en perms, o -1
	allow   []json.RawMessage
}

func loadPermPlan(path string) (permPlan, error) {
	p := permPlan{permsAt: -1, allowAt: -1}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	p.existed = true
	if bytes.HasPrefix(raw, []byte("\xef\xbb\xbf")) {
		p.bom, raw = true, bytes.TrimPrefix(raw, []byte("\xef\xbb\xbf"))
	}
	p.raw = raw
	if len(bytes.TrimSpace(raw)) == 0 {
		return p, nil // un archivo vacío es un objeto vacío
	}
	if p.top, err = parseObject(raw); err != nil {
		return p, ErrAgySettingsUnreadable
	}
	// Un `null` cuenta como ausente, pero conserva su posición: al escribir se
	// reemplaza en su lugar en vez de dejar la clave repetida.
	if p.permsAt = p.top.index("permissions"); p.permsAt >= 0 && !isNull(p.top[p.permsAt].val) {
		if p.perms, err = parseObject(p.top[p.permsAt].val); err != nil {
			return p, ErrAgySettingsUnreadable
		}
		if p.allowAt = p.perms.index("allow"); p.allowAt >= 0 && !isNull(p.perms[p.allowAt].val) {
			if err := json.Unmarshal(p.perms[p.allowAt].val, &p.allow); err != nil {
				return p, ErrAgySettingsUnreadable
			}
		}
	}
	return p, nil
}

// ruleOf devuelve la regla de un elemento de `allow` si es una de ScadLab.
func ruleOf(item json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(item, &s) != nil || !IsAgyRule(s) {
		return "", false
	}
	return s, true
}

// apply calcula el `allow` nuevo. Con removeAll quita todas las reglas de ScadLab; si
// no, deja exactamente las de want: conserva las que ya estaban donde estaban,
// quita las `mcp(scadlab/…)` de herramientas que ya no existen o repetidas y agrega
// al final las que faltan. Lo que no es una regla de ScadLab pasa tal cual.
func (p permPlan) apply(want []string, removeAll bool) (allow []json.RawMessage, added, removed []string, changed bool) {
	wantSet := map[string]bool{}
	for _, r := range want {
		wantSet[r] = true
	}
	seen := map[string]bool{}
	for _, item := range p.allow {
		rule, ours := ruleOf(item)
		switch {
		case !ours:
			allow = append(allow, item)
		case removeAll || !wantSet[rule]:
			removed = append(removed, rule)
			changed = true
		case seen[rule]:
			changed = true // un duplicado nuestro: se descarta sin avisar
		default:
			seen[rule] = true
			allow = append(allow, item)
		}
	}
	if !removeAll {
		for _, r := range want {
			if !seen[r] {
				seen[r] = true
				raw, _ := marshalNoEscape(r)
				allow = append(allow, raw)
				added = append(added, r)
				changed = true
			}
		}
	}
	return allow, added, removed, changed
}

// render arma el archivo con el `allow` dado, tocando solo esa clave.
func (p permPlan) render(allow []json.RawMessage) ([]byte, error) {
	top := append(orderedObj(nil), p.top...)
	perms := append(orderedObj(nil), p.perms...)
	allowRaw, err := json.Marshal(allow)
	if err != nil {
		return nil, err
	}
	if allow == nil {
		allowRaw = []byte("[]")
	}
	switch {
	case len(allow) > 0 && p.allowAt >= 0:
		perms[p.allowAt].val = allowRaw
	case len(allow) > 0:
		perms = append(perms, member{key: "allow", val: allowRaw})
	case p.allowAt >= 0:
		perms = perms.without(p.allowAt) // sin reglas: no queda una clave vacía
	}
	if len(perms) == 0 {
		if p.permsAt >= 0 {
			top = top.without(p.permsAt)
		}
	} else {
		val, err := perms.marshalCompact()
		if err != nil {
			return nil, err
		}
		if p.permsAt >= 0 {
			top[p.permsAt].val = val
		} else {
			top = append(top, member{key: "permissions", val: val})
		}
	}
	out, err := top.marshal()
	if err != nil {
		return nil, err
	}
	if p.bom {
		out = append([]byte("\xef\xbb\xbf"), out...)
	}
	return out, nil
}

// InspectAgyPermissions lee el settings.json sin tocarlo y cuenta cuántas
// herramientas del catálogo tienen su regla.
func InspectAgyPermissions(path string) (AgyPermInfo, error) {
	info := AgyPermInfo{Path: path, Total: len(AgyRules())}
	p, err := loadPermPlan(path)
	if err != nil {
		return info, err
	}
	info.Exists = p.existed
	want := map[string]bool{}
	for _, r := range AgyRules() {
		want[r] = true
	}
	have := map[string]bool{}
	for _, item := range p.allow {
		if rule, ok := ruleOf(item); ok {
			if want[rule] {
				have[rule] = true
			} else {
				info.Stale++
			}
		}
	}
	info.Approved = len(have)
	return info, nil
}

// SyncAgyPermissions deja en `permissions.allow` del settings.json de Antigravity
// exactamente las reglas del catálogo actual de ScadLab: agrega las que faltan y
// quita las `mcp(scadlab/…)` de herramientas que ya no existen. El resto del archivo
// no se toca. backupDir recibe una copia del archivo antes de cualquier cambio.
func SyncAgyPermissions(path, backupDir string) (AgyPermReport, error) {
	return editAgyPermissions(path, backupDir, AgyRules(), false)
}

// RemoveAgyPermissions quita TODAS las reglas `mcp(scadlab/…)` y nada más.
func RemoveAgyPermissions(path, backupDir string) (AgyPermReport, error) {
	return editAgyPermissions(path, backupDir, AgyRules(), true)
}

func editAgyPermissions(path, backupDir string, want []string, removeAll bool) (AgyPermReport, error) {
	rep := AgyPermReport{Path: path, Total: len(want), Added: []string{}, Removed: []string{}}
	if path == "" {
		return rep, i18n.New(i18n.Msg{ES: "no se pudo ubicar el settings.json de Antigravity", EN: "could not locate Antigravity's settings.json"})
	}
	agyPermMu.Lock()
	defer agyPermMu.Unlock()

	// Se resuelve el enlace simbólico: renombrar sobre un enlace lo reemplazaría
	// por un archivo.
	target := path
	if real, err := filepath.EvalSymlinks(path); err == nil {
		target = real
	}

	// Si Antigravity escribe el archivo entre que se lee y se reemplaza (guarda sus
	// carpetas de confianza ahí), se vuelve a empezar en vez de perder su cambio.
	for attempt := 0; attempt < 4; attempt++ {
		p, err := loadPermPlan(target)
		if err != nil {
			return rep, err
		}
		rep.Existed = p.existed
		allow, added, removed, changed := p.apply(want, removeAll)
		rep.Added, rep.Removed = nonNil(added), nonNil(removed)
		rep.Approved = countApproved(allow, want)
		if !changed {
			return rep, nil
		}
		out, err := p.render(allow)
		if err != nil {
			return rep, err
		}
		if p.existed {
			if rep.Backup, err = backupAgySettings(target, p, backupDir); err != nil {
				return rep, err
			}
		}
		afterAgyPlan()
		if now, err := os.ReadFile(target); p.existed && (err != nil || !bytes.Equal(trimBOM(now), p.raw)) {
			continue // cambió mientras tanto
		}
		if err := writeKeepingMode(target, out); err != nil {
			return rep, err
		}
		rep.Changed = true
		return rep, nil
	}
	return rep, i18n.New(i18n.Msg{
		ES: "el settings.json de Antigravity cambió mientras se escribía; vuelve a intentarlo",
		EN: "Antigravity's settings.json changed while it was being written; try again",
	})
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func trimBOM(b []byte) []byte { return bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")) }

func countApproved(allow []json.RawMessage, want []string) int {
	wantSet := map[string]bool{}
	for _, r := range want {
		wantSet[r] = true
	}
	have := map[string]bool{}
	for _, item := range allow {
		if rule, ok := ruleOf(item); ok && wantSet[rule] {
			have[rule] = true
		}
	}
	return len(have)
}

// backupAgySettings copia el archivo tal cual estaba (con su BOM) a backupDir y deja
// solo las últimas copias. Sin carpeta de copias no se escribe nada: tocar un
// archivo ajeno sin red de seguridad no es una opción.
func backupAgySettings(path string, p permPlan, backupDir string) (string, error) {
	if backupDir == "" {
		return "", i18n.New(i18n.Msg{ES: "no hay carpeta para la copia del settings.json de Antigravity", EN: "there is no folder for the copy of Antigravity's settings.json"})
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", err
	}
	body := p.raw
	if p.bom {
		body = append([]byte("\xef\xbb\xbf"), body...)
	}
	name := "settings-" + time.Now().UTC().Format("20060102-150405.000") + ".json"
	dst := filepath.Join(backupDir, name)
	if err := os.WriteFile(dst, body, 0o600); err != nil {
		return "", err
	}
	pruneAgyBackups(backupDir)
	return dst, nil
}

func pruneAgyBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "settings-") && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // la fecha va en el nombre: el orden alfabético es el cronológico
	for len(names) > agyBackupsKept {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// writeKeepingMode reemplaza el archivo de forma atómica conservando su modo (el de
// Antigravity es 0600); si no existía, lo crea (y su carpeta) con 0600.
func writeKeepingMode(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings.json.tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
