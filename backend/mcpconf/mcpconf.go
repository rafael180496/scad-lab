// Package mcpconf deja configurado el servidor MCP de ScadLab en los CLIs de
// IA que corren dentro de un workspace (S6, bloque 6.B).
//
// Los archivos se escriben solo en la configuración **de proyecto** (nunca en
// la global del usuario):
//
//	Claude Code        <ws>/.mcp.json             mcpServers.scadlab
//	opencode           <ws>/opencode.json         mcp.scadlab
//	Gemini CLI (anterior) <ws>/.gemini/settings.json mcpServers.scadlab
//
// Codex no lee configuración de proyecto: cuando ScadLab lo lanza en su
// terminal se le pasa el servidor con flags `-c` (ver CodexArgs).
//
// Antigravity (`agy`) NO lee `.gemini/settings.json` (verificado con agy 1.3.6):
// solo tiene configuración global. Esa solo se toca con el consentimiento
// explícito del usuario, por `agy mcp add|remove` (ver antigravity.go).
//
// De cada archivo se fusiona **solo la clave scadlab**: el resto —claves,
// orden, sangría— queda byte a byte como estaba (ver jsonedit.go). Un JSON que
// no se puede leer no se toca: se informa y se sigue.
package mcpconf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"scadlab/backend/i18n"
	"scadlab/backend/mcp"
)

// ServerName y Args son el contrato con el servidor (backend/mcp, bloque 6.A):
// el comando es `<ScadLab> --mcp --workspace <dir>`. Una sola definición, la
// del propio servidor.
const ServerName = mcp.ServerName

// Args son los argumentos con los que ScadLab arranca como servidor MCP.
func Args(workspaceDir string) []string { return mcp.Args(workspaceDir) }

// Estados de un archivo tras Sync.
const (
	StatusCreated  = "created"
	StatusUpdated  = "updated"
	StatusRemoved  = "removed"
	StatusUnchange = "unchanged"
	StatusInvalid  = "skipped-invalid"
)

// Estados de un archivo según Inspect (no escribe nada).
const (
	StateOK       = "ok"       // la entrada está y es la vigente
	StateMissing  = "missing"  // falta la entrada (o el archivo)
	StateOutdated = "outdated" // está, pero con otro comando (app movida)
	StateStale    = "stale"    // el MCP está apagado y todavía queda la entrada
	StateOff      = "off"      // el MCP está apagado y no hay entrada
	StateInvalid  = "skipped-invalid"
)

// Agentes con archivo de proyecto. AgentGemini es el Gemini CLI anterior a
// Antigravity: su archivo de proyecto sigue sirviendo a quien todavía usa
// `gemini`.
const (
	AgentClaude   = "claude"
	AgentOpenCode = "opencode"
	AgentGemini   = "gemini"
)

// FileReport es lo que pasó (o pasaría) con un archivo.
type FileReport struct {
	Agent  string `json:"agent"`
	Name   string `json:"name"` // relativo al workspace, con «/»
	Path   string `json:"path"` // absoluto
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`

	added bool // «updated» que en realidad agrega la clave (Inspect lo ve como «falta»)
}

// Report es el resultado de Sync o de Inspect.
type Report struct {
	Files []FileReport `json:"files"`
}

// target describe un archivo de configuración y cómo se escribe la entrada.
type target struct {
	agent string
	rel   string // ruta relativa al workspace, con «/»
	top   string // objeto que agrupa los servidores
	seed  ordered
	entry func(bin, dir string) ordered
}

func claudeTarget() target {
	return target{
		agent: AgentClaude, rel: ".mcp.json", top: "mcpServers",
		entry: func(bin, dir string) ordered {
			return ordered{{"command", bin}, {"args", Args(dir)}}
		},
	}
}

func geminiTarget() target {
	return target{
		agent: AgentGemini, rel: ".gemini/settings.json", top: "mcpServers",
		entry: func(bin, dir string) ordered {
			return ordered{{"command", bin}, {"args", Args(dir)}}
		},
	}
}

// openCodeTarget elige el archivo de opencode: opencode.json si existe; si no,
// opencode.jsonc si existe (con comentarios no se puede editar sin perderlos y
// se informa); si no hay ninguno, se crea opencode.json.
func openCodeTarget(dir string) target {
	rel := "opencode.json"
	if !exists(filepath.Join(dir, "opencode.json")) && exists(filepath.Join(dir, "opencode.jsonc")) {
		rel = "opencode.jsonc"
	}
	return target{
		agent: AgentOpenCode, rel: rel, top: "mcp",
		seed: ordered{{"$schema", "https://opencode.ai/config.json"}},
		entry: func(bin, dir string) ordered {
			cmd := append([]string{bin}, Args(dir)...)
			return ordered{{"type", "local"}, {"command", cmd}, {"enabled", true}}
		},
	}
}

func targets(dir string) []target {
	return []target{claudeTarget(), openCodeTarget(dir), geminiTarget()}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Sync deja la entrada scadlab en los tres archivos de proyecto (enabled) o la
// quita (no enabled). Es idempotente: un archivo que ya está como corresponde
// no se reescribe. Un archivo que no se pudo tratar no detiene a los demás: su
// estado dice por qué.
func Sync(workspaceDir, binPath string, enabled bool) (Report, error) {
	return run(workspaceDir, binPath, enabled, true)
}

// Inspect dice en qué estado está cada archivo sin escribir nada.
func Inspect(workspaceDir, binPath string, enabled bool) (Report, error) {
	rep, err := run(workspaceDir, binPath, enabled, false)
	if err != nil {
		return rep, err
	}
	for i, f := range rep.Files {
		switch f.Status {
		case StatusUnchange:
			if enabled {
				rep.Files[i].Status = StateOK
			} else {
				rep.Files[i].Status = StateOff
			}
		case StatusCreated:
			rep.Files[i].Status = StateMissing
		case StatusUpdated:
			// «updated» con el MCP encendido: o la clave falta dentro de un
			// archivo que sí existe, o está con otro comando (app movida).
			rep.Files[i].Status = StateOutdated
			if f.added {
				rep.Files[i].Status = StateMissing
			}
		case StatusRemoved:
			rep.Files[i].Status = StateStale
		}
	}
	return rep, nil
}

func run(workspaceDir, binPath string, enabled, write bool) (Report, error) {
	if st, err := os.Stat(workspaceDir); err != nil || !st.IsDir() {
		return Report{}, i18n.Errorf(i18n.Msg{ES: "la carpeta del workspace no existe: %q", EN: "the workspace folder does not exist: %q"}, workspaceDir)
	}
	if enabled && strings.TrimSpace(binPath) == "" {
		return Report{}, i18n.New(i18n.Msg{ES: "no se conoce la ruta del ejecutable de ScadLab", EN: "the ScadLab executable path is unknown"})
	}
	rep := Report{Files: []FileReport{}}
	for _, t := range targets(workspaceDir) {
		rep.Files = append(rep.Files, t.apply(workspaceDir, binPath, enabled, write))
	}
	return rep, nil
}

func (t target) apply(dir, bin string, enabled, write bool) FileReport {
	path := filepath.Join(dir, filepath.FromSlash(t.rel))
	fr := FileReport{Agent: t.agent, Name: t.rel, Path: path}

	raw, err := os.ReadFile(path)
	missing := os.IsNotExist(err)
	if err != nil && !missing {
		fr.Status = StatusInvalid
		fr.Detail = i18n.T(i18n.Msg{ES: "no se pudo leer: %v", EN: "could not be read: %v"}, err)
		return fr
	}

	var entry ordered
	if enabled {
		entry = t.entry(bin, dir)
	}
	out, status, detail, added := t.edit(raw, missing, entry)
	fr.Status, fr.Detail, fr.added = status, detail, added
	if out == nil || !write {
		return fr
	}

	if len(out) == 0 { // quedó vacío: era solo nuestro
		if err := os.Remove(path); err != nil {
			fr.Status, fr.Detail = StatusInvalid, i18n.T(i18n.Msg{ES: "no se pudo borrar: %v", EN: "could not be deleted: %v"}, err)
			return fr
		}
		_ = os.Remove(filepath.Dir(path)) // .gemini vacío; falla sin daño si tiene más cosas
		return fr
	}
	if err := writeAtomic(path, out); err != nil {
		fr.Status, fr.Detail = StatusInvalid, i18n.T(i18n.Msg{ES: "no se pudo escribir: %v", EN: "could not be written: %v"}, err)
	}
	return fr
}

// edit calcula el contenido nuevo del archivo. out es nil si no hay nada que
// escribir y un slice vacío no nil si el archivo debe borrarse. added dice que
// el cambio fue agregar la clave (no corregir una que ya estaba).
func (t target) edit(raw []byte, missing bool, entry ordered) (out []byte, status, detail string, added bool) {
	var bom []byte
	data := raw
	if bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) {
		bom, data = []byte("\xef\xbb\xbf"), data[3:]
	}
	unchanged := func() ([]byte, string, string, bool) { return nil, StatusUnchange, "", false }
	invalid := func(msg string) ([]byte, string, string, bool) { return nil, StatusInvalid, msg, false }
	// with devuelve el texto nuevo (con su BOM, si lo tenía) como cambio.
	with := func(res []byte, st string, add bool) ([]byte, string, string, bool) {
		return append(append([]byte(nil), bom...), res...), st, "", add
	}

	if missing || len(bytes.TrimSpace(data)) == 0 {
		if entry == nil {
			return unchanged() // nada que quitar
		}
		fresh, err := t.fresh(entry)
		if err != nil {
			return invalid(err.Error())
		}
		if missing {
			return fresh, StatusCreated, "", true
		}
		return fresh, StatusUpdated, "", true
	}

	if !json.Valid(data) {
		return invalid(t.invalidDetail())
	}
	root, err := scanObject(data, skipWS(data, 0))
	if err != nil {
		return invalid(i18n.T(i18n.Msg{ES: "la raíz no es un objeto JSON", EN: "the root is not a JSON object"}))
	}
	st := detectStyle(data)
	wrapped := ordered{{ServerName, entry}}

	ti, hasTop := root.find(t.top)
	if !hasTop {
		if entry == nil {
			return unchanged()
		}
		res, err := insertMember(data, root, t.top, wrapped, st)
		if err != nil {
			return invalid(err.Error())
		}
		return with(res, StatusUpdated, true)
	}

	tm := root.members[ti]
	if bytes.Equal(bytes.TrimSpace(data[tm.valStart:tm.valEnd]), []byte("null")) {
		if entry == nil {
			return unchanged()
		}
		r, err := renderIn(wrapped, lineIndent(data, tm.start), st)
		if err != nil {
			return invalid(err.Error())
		}
		return with(splice(data, tm.valStart, tm.valEnd, r), StatusUpdated, true)
	}
	topObj, err := scanObject(data, tm.valStart)
	if err != nil {
		return invalid(i18n.T(i18n.Msg{ES: "%q no es un objeto", EN: "%q is not an object"}, t.top))
	}

	si, hasSrv := topObj.find(ServerName)
	if !hasSrv {
		if entry == nil {
			return unchanged()
		}
		res, err := insertMember(data, topObj, ServerName, entry, st)
		if err != nil {
			return invalid(err.Error())
		}
		return with(res, StatusUpdated, true)
	}

	sm := topObj.members[si]
	if entry == nil { // quitar
		if len(topObj.members) > 1 {
			return with(removeMember(data, topObj, si), StatusRemoved, false)
		}
		// Era el único servidor: se va el objeto entero y, si con él se va todo
		// lo que había (salvo lo que pusimos al crear el archivo), el archivo.
		if t.onlySeedBesides(root, ti) {
			return []byte{}, StatusRemoved, "", false
		}
		return with(removeMember(data, root, ti), StatusRemoved, false)
	}

	merged, changed, err := mergeOwned(data[sm.valStart:sm.valEnd], entry)
	if err != nil { // la entrada no es un objeto: se reemplaza entera
		merged, changed = entry, true
	}
	if !changed {
		return unchanged()
	}
	v, err := renderIn(merged, lineIndent(data, sm.start), st)
	if err != nil {
		return invalid(err.Error())
	}
	return with(splice(data, sm.valStart, sm.valEnd, v), StatusUpdated, false)
}

// onlySeedBesides dice si, además del miembro except, la raíz solo tiene las
// claves que ScadLab pone al crear el archivo (p. ej. $schema).
func (t target) onlySeedBesides(root object, except int) bool {
	for i, m := range root.members {
		if i == except {
			continue
		}
		seed := false
		for _, p := range t.seed {
			if p.Key == m.key {
				seed = true
			}
		}
		if !seed {
			return false
		}
	}
	return true
}

// fresh arma un archivo nuevo con la entrada (y las claves iniciales del
// agente, si las tiene).
func (t target) fresh(entry ordered) ([]byte, error) {
	root := append(ordered{}, t.seed...)
	root = append(root, pair{t.top, ordered{{ServerName, entry}}})
	b, err := render(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func (t target) invalidDetail() string {
	if strings.HasSuffix(t.rel, ".jsonc") {
		return i18n.T(i18n.Msg{
			ES: "tiene comentarios o comas finales y editarlo borraría los comentarios; agrega la entrada a mano",
			EN: "it has comments or trailing commas and editing it would drop the comments; add the entry by hand",
		})
	}
	return i18n.T(i18n.Msg{ES: "no es un JSON válido; no se toca", EN: "it is not valid JSON; leaving it untouched"})
}

// writeAtomic escribe por temporal + rename en el mismo directorio: un corte a
// mitad deja el archivo anterior, no uno truncado. Conserva los permisos del
// archivo si ya existía.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Si el archivo es un enlace simbólico se escribe su destino, no se
	// reemplaza el enlace.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".scadlab-mcp-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// CodexArgs son los flags `-c` con los que se lanza Codex para que vea el
// servidor: Codex no lee configuración de proyecto. Van como argv (nunca por
// shell) y el valor es TOML, así que las rutas se escapan como cadena TOML
// (una ruta de Windows lleva «\»).
func CodexArgs(binPath, workspaceDir string) []string {
	args := Args(workspaceDir)
	quoted := make([]string, len(args))
	for i, a := range args {
		quoted[i] = tomlString(a)
	}
	return []string{
		"-c", fmt.Sprintf("mcp_servers.%s.command=%s", ServerName, tomlString(binPath)),
		"-c", fmt.Sprintf("mcp_servers.%s.args=[%s]", ServerName, strings.Join(quoted, ",")),
	}
}

// ClaudeArgs son los flags para que una sesión de Claude Code abierta desde la
// terminal de ScadLab use el servidor sin pasar por la aprobación del
// .mcp.json del proyecto: un servidor dado explícitamente con --mcp-config no
// la necesita, y la aprobación se guarda en la configuración global del usuario
// (~/.claude.json), que ScadLab no toca. Verificado en Claude Code 2.1: el
// .mcp.json solo queda «Pending approval» hasta el primer `claude` interactivo.
//
// Va como una cadena JSON (el flag admite archivos o cadenas). --mcp-config es
// variádico: quien arme el argv tiene que poner a continuación un flag (que
// empieza con «-») o nada, nunca un argumento posicional.
func ClaudeArgs(binPath, workspaceDir string) []string {
	cfg := map[string]any{"mcpServers": map[string]any{
		ServerName: map[string]any{"command": binPath, "args": Args(workspaceDir)},
	}}
	raw, _ := json.Marshal(cfg) // un mapa de strings no puede fallar
	return []string{"--mcp-config", string(raw)}
}

// tomlString escribe s como cadena básica de TOML.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Command arma la línea de comando que usan los agentes, para mostrarla y
// copiarla: el ejecutable y sus argumentos, entre comillas los que lo piden.
func Command(binPath, workspaceDir string) string {
	parts := append([]string{binPath}, Args(workspaceDir)...)
	for i, p := range parts {
		if strings.ContainsAny(p, " \t\"'") {
			parts[i] = `"` + strings.ReplaceAll(p, `"`, `\"`) + `"`
		}
	}
	return strings.Join(parts, " ")
}
