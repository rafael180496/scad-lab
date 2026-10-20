//go:build !windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"scadlab/backend/appdata"
	"scadlab/backend/clichat"
	"scadlab/backend/hermes"
	"scadlab/backend/store"
	"scadlab/backend/turnmark"
)

// Pruebas de la aprobación de las herramientas de ScadLab en Antigravity (S12.c,
// T14). NINGUNA lanza el `agy` real (gastaría cuota) ni toca ~/.gemini: el
// settings.json es un archivo temporal y un `agy` de mentira imita lo que se midió
// con el verdadero (1.4.x):
//
//   - en `--print`, una herramienta MCP sin regla `mcp(<servidor>/<herramienta>)` en
//     `permissions.allow` del settings.json se rechaza («permission check failed…»)
//     y el turno TERMINA ahí, sin texto;
//   - con la regla, la herramienta corre. Aquí corre contra el `ScadLab --mcp` REAL
//     (este mismo binario de pruebas, relanzado como servidor), que es quien hace
//     cumplir «Solo consulta» con la marca del turno.

const fakeAgyTurnScript = `#!/bin/sh
d="$FAKE_DIR"
if [ "$1" = "--version" ]; then echo "agy 1.4.2 (de mentira)"; exit 0; fi
{ printf '<<CALL>>\0'; for a in "$@"; do printf '%s\0' "$a"; done; } >> "$d/argv.agy"
env | grep '^SCADLAB_WORKSPACE=' > "$d/env.agy"
pwd > "$d/cwd.agy"
[ -f ./.scadlab/turn-mode ] && cp ./.scadlab/turn-mode "$d/mark.seen"
[ -f ./.agents/hooks.json ] && cp ./.agents/hooks.json "$d/hooks.present.json"
settings=$(cat "$d/settings.path")
[ -f "$settings" ] && cp "$settings" "$d/settings.seen"

emit() { printf '%s\n' "$1"; }
emit '{"event":"init","conversation_id":"aaaaaaaa-0000-4000-8000-000000000001","init":{"cwd":"x","tools":["call_mcp_tool"],"model":"gemini-de-mentira","permission_mode":"accept-edits"}}'
# Si ScadLab le dice que sus herramientas no están, contesta con el bloque (lo que haría el modelo).
case "$2" in
  *"NOT connected"*) cat "$d/block.agy"; exit 0 ;;
esac
bin=$(cat "$d/scadlab.path")
# Con el registro global (sin --workspace) el servidor no sabe en qué carpeta está: el
# directorio actual, o SCADLAB_WORKSPACE si Antigravity le pasa el entorno.
[ -f "$d/cwd.elsewhere" ] && cd "$d"
INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}'
NOTIF='{"jsonrpc":"2.0","method":"notifications/initialized"}'
n=0
while read -r tool; do
  n=$((n+1))
  emit "{\"event\":\"step_update\",\"step_update\":{\"step_index\":$n,\"state\":\"ACTIVE\",\"step_type\":\"call_mcp_tool\",\"tool_name\":\"call_mcp_tool\",\"tool_input\":{\"ServerName\":\"scadlab\",\"ToolName\":\"$tool\",\"Arguments\":{}}}}"
  # La regla de permiso: sin ella, el rechazo de verdad y el turno termina.
  if ! { [ -f "$settings" ] && grep -F "\"mcp(scadlab/$tool)\"" "$settings" >/dev/null 2>&1; }; then
    emit "{\"event\":\"step_update\",\"step_update\":{\"step_index\":$n,\"state\":\"ERROR\",\"step_type\":\"call_mcp_tool\",\"tool_name\":\"call_mcp_tool\",\"tool_info\":\"permission check failed for mcp \\\"scadlab/$tool\\\": user denied permission for mcp(scadlab/$tool)\\nDo not attempt to circumvent this denial.\"}}"
    emit '{"event":"result","result":{"conversation_id":"aaaaaaaa-0000-4000-8000-000000000001","status":"SUCCESS","response":"","usage":{"input_tokens":100,"output_tokens":10,"total_tokens":110}}}'
    exit 0
  fi
  args='{}'
  if [ "$tool" = "create_checkpoint" ]; then args='{"label":"caja","source":"color(\"red\") cube(10);"}'; fi
  CALL="{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"$tool\",\"arguments\":$args}}"
  resp=$(printf '%s\n%s\n%s\n' "$INIT" "$NOTIF" "$CALL" | "$bin" --mcp 2>"$d/mcp.stderr")
  printf '%s\n' "$resp" > "$d/resp.$n.$tool"
  case "$resp" in
    *'"isError":true'*)
      emit "{\"event\":\"step_update\",\"step_update\":{\"step_index\":$n,\"state\":\"ERROR\",\"step_type\":\"call_mcp_tool\",\"tool_name\":\"call_mcp_tool\",\"tool_info\":\"el servidor de ScadLab rechazó la herramienta\"}}"
      ;;
    *)
      emit "{\"event\":\"step_update\",\"step_update\":{\"step_index\":$n,\"state\":\"DONE\",\"step_type\":\"call_mcp_tool\",\"tool_name\":\"call_mcp_tool\",\"output\":\"{\\\"ok\\\":true}\"}}"
      ;;
  esac
done < "$d/calls.agy"

text=$(cat "$d/text.agy")
emit "{\"event\":\"step_update\",\"step_update\":{\"step_index\":99,\"state\":\"DONE\",\"step_type\":\"agent_response\",\"text_delta\":\"$text\"}}"
emit "{\"event\":\"result\",\"result\":{\"conversation_id\":\"aaaaaaaa-0000-4000-8000-000000000001\",\"status\":\"SUCCESS\",\"response\":\"$text\",\"usage\":{\"input_tokens\":100,\"output_tokens\":10,\"total_tokens\":110}}}"
exit 0
`

// agyRig es un workspace con el `agy` de mentira listo para un turno.
type agyRig struct {
	a        *App
	wsID     int64
	dir      string
	ev       *hermesEvents
	f        *appFakeCLIs
	settings string // el settings.json de mentira de Antigravity
	mu       sync.Mutex
	mcpEv    []AntigravityMCPEvent
	deniedEv []AntigravityDeniedEvent
}

// newAgyRig arma: el agy de mentira, un «ScadLab» de mentira que relanza este
// binario de pruebas como servidor `--mcp`, un settings.json temporal y el registro
// de Antigravity ya hecho (el MCP global de ScadLab apunta a ese ejecutable). calls
// son las herramientas que el agy de mentira va a llamar y text, lo que contesta.
func newAgyRig(t *testing.T, calls []string, text string) *agyRig {
	t.Helper()
	a, wsID, dir, ev, f := cliApp(t)
	t.Setenv(appdata.OverrideEnv, a.dataDir) // el servidor real lee la base de la prueba, no la del usuario
	f.install("agy", "")
	if err := os.WriteFile(filepath.Join(f.dir, "agy"), []byte(fakeAgyTurnScript), 0o755); err != nil {
		t.Fatal(err)
	}
	f.put("calls.agy", strings.Join(calls, "\n")+"\n")
	f.put("text.agy", text)

	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fakeScadlab := filepath.Join(f.dir, "ScadLab")
	wrapper := "#!/bin/sh\nSCADLAB_TEST_AS_MCP=1 exec '" + self + "' \"$@\"\n"
	if err := os.WriteFile(fakeScadlab, []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	f.put("scadlab.path", fakeScadlab)
	prev := mcpBinary
	mcpBinary = func() (string, error) { return fakeScadlab, nil }
	t.Cleanup(func() { mcpBinary = prev })

	fa := withFakeAgy(t, "/usr/local/bin/agy")
	fa.servers["scadlab"] = fakeScadlab + " --mcp"

	r := &agyRig{a: a, wsID: wsID, dir: dir, ev: ev, f: f, settings: filepath.Join(t.TempDir(), "antigravity-cli", "settings.json")}
	f.put("settings.path", r.settings)
	prevSettings := agySettingsPath
	agySettingsPath = func() string { return r.settings }
	t.Cleanup(func() { agySettingsPath = prevSettings })

	// Además de los eventos del chat, se anotan los de Antigravity.
	forward := a.emitter
	a.emitter = func(name string, payload any) {
		r.mu.Lock()
		switch p := payload.(type) {
		case AntigravityMCPEvent:
			r.mcpEv = append(r.mcpEv, p)
		case AntigravityDeniedEvent:
			r.deniedEv = append(r.deniedEv, p)
		}
		r.mu.Unlock()
		forward(name, payload)
	}
	return r
}

func (r *agyRig) writeSettings(body string) {
	r.f.t.Helper()
	if err := os.MkdirAll(filepath.Dir(r.settings), 0o755); err != nil {
		r.f.t.Fatal(err)
	}
	if err := os.WriteFile(r.settings, []byte(body), 0o600); err != nil {
		r.f.t.Fatal(err)
	}
}

func (r *agyRig) settingsText() string {
	raw, _ := os.ReadFile(r.settings)
	return string(raw)
}

func (r *agyRig) ruleCount() int { return strings.Count(r.settingsText(), `"mcp(scadlab/`) }

func (r *agyRig) events() ([]AntigravityMCPEvent, []AntigravityDeniedEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]AntigravityMCPEvent(nil), r.mcpEv...), append([]AntigravityDeniedEvent(nil), r.deniedEv...)
}

// send manda un mensaje al chat de Antigravity y devuelve el mensaje guardado del
// asistente y sus pasos.
func (r *agyRig) send(t *testing.T, agent bool, text string) (store.ChatMessage, hermes.Parts, store.Chat) {
	t.Helper()
	c, err := r.a.NewChat(r.wsID, "cli:antigravity", "")
	if err != nil {
		t.Fatal(err)
	}
	if !agent {
		if err := r.a.SetChatMode(c.ID, "ask"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.a.SendChatMessage(c.ID, text, "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	r.ev.wait(t)
	view, err := r.a.GetChat(c.ID)
	if err != nil || len(view.Messages) != 2 {
		t.Fatalf("GetChat: %+v %v", view, err)
	}
	m := view.Messages[1]
	return m, hermes.ParseParts(m.Parts), view.Chat
}

func (r *agyRig) hooksFile() string { return filepath.Join(r.dir, ".agents", "hooks.json") }
func (r *agyRig) markFile() string  { return turnmark.Path(r.dir) }

func toolsOf(p hermes.Parts) []string {
	var out []string
	for _, s := range p.Steps {
		out = append(out, s.Tool+":"+string(s.Status))
	}
	return out
}

// El caso del bug: en «Agente», la primera vez, ScadLab aprueba SUS herramientas en
// el settings.json (sin tocar lo que había), avisa qué agregó y dónde, y el turno
// crea el checkpoint de verdad contra el servidor real.
func TestAntigravityChatApprovesScadlabToolsThroughTheSettingsFile(t *testing.T) {
	r := newAgyRig(t, []string{"printer_info", "list_checkpoints", "create_checkpoint", "list_materials"}, "Listo: 002-caja.scad")
	r.writeSettings(`{"trustedWorkspaces": ["/Users/yo/otra"]}`)

	m, parts, _ := r.send(t, true, "crea una caja con tapa a presión para un Raspberry Pi 5")

	if got := strings.Join(toolsOf(parts), " "); got != "printer_info:ok list_checkpoints:ok create_checkpoint:ok list_materials:ok" {
		t.Errorf("pasos (con su nombre real, no call_mcp_tool): %s", got)
	}
	if m.Content != "Listo: 002-caja.scad" {
		t.Errorf("respuesta: %q", m.Content)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "002-caja.scad")); err != nil {
		files, _ := filepath.Glob(filepath.Join(r.dir, "*.scad"))
		t.Errorf("el servidor real creó el checkpoint: %v (%v)", err, files)
	}

	// Lo que ve Antigravity: una regla por herramienta del catálogo, nada más, y lo que ya había.
	seen := r.f.read("settings.seen")
	if !strings.Contains(seen, `"trustedWorkspaces"`) || !strings.Contains(seen, `"/Users/yo/otra"`) {
		t.Errorf("trustedWorkspaces intacto:\n%s", seen)
	}
	if n := strings.Count(seen, `"mcp(scadlab/`); n != len(clichat.AgyRules()) {
		t.Errorf("reglas durante el turno: %d, quería %d", n, len(clichat.AgyRules()))
	}
	for _, line := range strings.Split(seen, "\n") {
		trimmed := strings.Trim(strings.TrimSpace(line), `",`)
		if strings.Contains(trimmed, "(") && !strings.HasPrefix(trimmed, "mcp(scadlab/") {
			t.Errorf("ScadLab agregó una regla que no es suya: %q", trimmed)
		}
	}
	if strings.Contains(seen, "*") || strings.Contains(seen, "command(") {
		t.Errorf("sin comodines ni command(...):\n%s", seen)
	}

	// El aviso dice qué se agregó y dónde.
	mcpEv, _ := r.events()
	var perm *AntigravityMCPEvent
	for i := range mcpEv {
		if mcpEv[i].Permissions > 0 {
			perm = &mcpEv[i]
		}
	}
	if perm == nil || perm.Permissions != len(clichat.AgyRules()) || perm.SettingsPath != r.settings || perm.Error != "" {
		t.Errorf("evento mcp:antigravity: %+v", mcpEv)
	}
	// Copia previa en la carpeta de datos.
	if files, _ := filepath.Glob(filepath.Join(r.a.dataDir, "backups", "antigravity", "settings-*.json")); len(files) != 1 {
		t.Errorf("copia previa del settings.json: %v", files)
	}
	// El modo viaja a la marca, no a un hook: en Agente no hay marca ni hooks.json.
	if r.f.read("mark.seen") != "" {
		t.Error("en Agente no hay marca")
	}
	if _, err := os.Stat(r.hooksFile()); err == nil {
		t.Error("ya no se escribe ningún hooks.json")
	}
	// El servidor supo en qué carpeta estaba, y de dónde lo supo.
	if got := strings.TrimSpace(r.f.read("env.agy")); got != "SCADLAB_WORKSPACE="+r.dir {
		t.Errorf("entorno del turno: %q", got)
	}

	// Lo que NO se relaja: ni los flags que saltean permisos, ni la shell.
	args := r.f.calls("agy")[0]
	for _, a := range args {
		if strings.Contains(a, "dangerously") || a == "auto" {
			t.Errorf("flag que relaja permisos: %v", args)
		}
	}
	if !argHas(args, "--mode", "accept-edits") {
		t.Errorf("sigue en accept-edits: %v", args)
	}
}

// Las reglas ya puestas no se vuelven a escribir ni a anunciar; las que sobran de
// una versión vieja se quitan; lo del usuario se queda.
func TestAntigravityChatKeepsThePermissionsInSyncOnEveryTurn(t *testing.T) {
	r := newAgyRig(t, []string{"list_checkpoints"}, "ok")
	r.writeSettings(`{"permissions": {"allow": ["command(git status)", "mcp(scadlab/herramienta_que_ya_no_existe)", "mcp(scadlab/list_checkpoints)"], "deny": ["command(rm *)"]}}`)

	r.send(t, true, "uno")
	text := r.settingsText()
	if strings.Contains(text, "herramienta_que_ya_no_existe") {
		t.Errorf("la regla sobrante se quita:\n%s", text)
	}
	if !strings.Contains(text, `"command(git status)"`) || !strings.Contains(text, `"command(rm *)"`) {
		t.Errorf("lo del usuario se queda:\n%s", text)
	}
	if r.ruleCount() != len(clichat.AgyRules()) {
		t.Errorf("reglas: %d", r.ruleCount())
	}
	mcpEv, _ := r.events()
	before := len(mcpEv)

	// Segundo turno: ya está todo; nada que reescribir ni que anunciar.
	stat1, _ := os.Stat(r.settings)
	r.send(t, true, "dos")
	stat2, _ := os.Stat(r.settings)
	if !stat1.ModTime().Equal(stat2.ModTime()) {
		t.Error("el archivo no se reescribe si no hay nada que cambiar")
	}
	if mcpEv, _ = r.events(); len(mcpEv) != before {
		t.Errorf("sin aviso la segunda vez: %+v", mcpEv[before:])
	}
}

// «Solo consulta»: la lectura pasa y la escritura la rechaza el SERVIDOR de ScadLab
// (la regla de Antigravity es global y las aprueba todas), con el motivo; el turno
// sigue y entrega su respuesta. La marca vive lo que dura el turno.
func TestAntigravityChatAskModeIsEnforcedByTheMCPServer(t *testing.T) {
	r := newAgyRig(t, []string{"list_checkpoints", "create_checkpoint"}, "Aquí tienes el bloque")

	m, parts, _ := r.send(t, false, "¿cabe en la placa?")

	if got := strings.Join(toolsOf(parts), " "); got != "list_checkpoints:ok create_checkpoint:error" {
		t.Errorf("pasos: %s", got)
	}
	if m.Content != "Aquí tienes el bloque" {
		t.Errorf("el turno sigue tras el rechazo: %q", m.Content)
	}
	if files, _ := filepath.Glob(filepath.Join(r.dir, "002-*.scad")); len(files) != 0 {
		t.Errorf("en consulta no se crea el checkpoint: %v", files)
	}
	resp := r.f.read("resp.2.create_checkpoint")
	if !strings.Contains(resp, `"isError":true`) || !strings.Contains(resp, "Query only") || !strings.Contains(resp, "create_checkpoint") {
		t.Errorf("el servidor rechaza con el motivo:\n%s", resp)
	}
	if strings.Contains(r.f.read("resp.1.list_checkpoints"), `"isError":true`) {
		t.Errorf("la lectura pasa:\n%s", r.f.read("resp.1.list_checkpoints"))
	}
	// La marca existía mientras corría el turno y ya no está.
	if seen := r.f.read("mark.seen"); !strings.Contains(seen, `"mode": "ask"`) {
		t.Errorf("marca durante el turno:\n%s", seen)
	}
	if _, err := os.Stat(r.markFile()); err == nil {
		t.Error("la marca se retira al terminar el turno")
	}
	// Las reglas globales aprueban TAMBIÉN la escritura: es el servidor quien la frena.
	if !strings.Contains(r.settingsText(), `"mcp(scadlab/create_checkpoint)"`) {
		t.Error("la regla de create_checkpoint está (global)")
	}

	// Después, en Agente en el mismo workspace, se escribe: sin marca vieja que moleste.
	r.f.put("calls.agy", "create_checkpoint\n")
	r.send(t, true, "ahora sí")
	if files, _ := filepath.Glob(filepath.Join(r.dir, "002-*.scad")); len(files) != 1 {
		t.Errorf("en Agente sí se crea: %v", files)
	}
}

// El registro global de Antigravity no lleva carpeta: si su directorio actual no es
// el del proyecto, SCADLAB_WORKSPACE (puesta por el chat en el turno) la da, y el
// servidor lo dice en su stderr.
func TestAntigravityServerFindsTheWorkspaceFromTheEnvironment(t *testing.T) {
	r := newAgyRig(t, []string{"create_checkpoint"}, "ok")
	r.f.put("cwd.elsewhere", "")

	r.send(t, true, "crea una caja")

	if files, _ := filepath.Glob(filepath.Join(r.dir, "002-*.scad")); len(files) != 1 {
		t.Errorf("el checkpoint se creó en el workspace aunque el cwd era otro: %v", files)
	}
	if got := r.f.read("mcp.stderr"); !strings.Contains(got, "workspace "+r.dir+" (SCADLAB_WORKSPACE)") {
		t.Errorf("stderr del servidor: %q", got)
	}
}

// Si el usuario quitó los permisos desde Configuración, ScadLab NO los vuelve a poner
// solo: el turno se bloquea con un mensaje accionable (herramienta, ruta, qué hacer),
// un aviso con «Reparar», y repararlos lo arregla.
func TestAntigravityChatADeniedToolShowsAnActionableMessageAndRepairFixesIt(t *testing.T) {
	r := newAgyRig(t, []string{"printer_info", "create_checkpoint"}, "Listo")
	r.writeSettings(`{"trustedWorkspaces": ["/a"], "permissions": {"allow": ["command(ls)"]}}`)
	if _, err := r.a.RepairAntigravityPermissions(); err != nil {
		t.Fatal(err)
	}
	st, err := r.a.RemoveAntigravityPermissions()
	if err != nil || st.Approved != 0 || st.Managed || st.State != "none" {
		t.Fatalf("quitar: %+v %v", st, err)
	}
	if !strings.Contains(r.settingsText(), `"command(ls)"`) || r.ruleCount() != 0 {
		t.Fatalf("quitar solo toca las mcp(scadlab/…):\n%s", r.settingsText())
	}

	m, parts, _ := r.send(t, true, "crea una caja")

	for _, want := range []string{"Antigravity blocked ScadLab's “printer_info” tool", "non-interactive mode", r.settings, "Repair Antigravity permissions", "You removed those permissions", "permission check failed for mcp"} {
		if !strings.Contains(m.Content, want) {
			t.Errorf("falta %q en:\n%s", want, m.Content)
		}
	}
	if strings.Contains(m.Content, "failed: tool_info") || strings.Count(m.Content, "permission check failed for mcp") != 1 {
		t.Errorf("el rechazo se explica una sola vez:\n%s", m.Content)
	}
	if got := strings.Join(toolsOf(parts), " "); got != "printer_info:error" {
		t.Errorf("el turno terminó en la primera herramienta rechazada: %s", got)
	}
	if r.ruleCount() != 0 {
		t.Error("el turno no repone lo que el usuario quitó")
	}
	if _, denied := r.events(); len(denied) != 1 || denied[0].Tool != "printer_info" || denied[0].SettingsPath != r.settings || denied[0].Managed {
		t.Errorf("aviso con «Reparar»: %+v", denied)
	}

	// «Reparar permisos de Antigravity».
	st, err = r.a.RepairAntigravityPermissions()
	if err != nil || st.State != "ok" || st.Approved != st.Total || !st.Managed {
		t.Fatalf("reparar: %+v %v", st, err)
	}
	m, parts, _ = r.send(t, true, "vuelve a intentarlo")
	if m.Content != "Listo" || !strings.HasPrefix(strings.Join(toolsOf(parts), " "), "printer_info:ok") {
		t.Errorf("reparado: %q %v", m.Content, toolsOf(parts))
	}
}

// Un settings.json que no es JSON no se pisa: el turno explica por qué no hay
// permisos, le dice al modelo que entregue el bloque, y la red de seguridad lo guarda.
func TestAntigravityChatUnreadableSettingsAreExplainedAndTheBlockIsStillSaved(t *testing.T) {
	r := newAgyRig(t, []string{"printer_info"}, "x")
	broken := `{"trustedWorkspaces": [no es json`
	r.writeSettings(broken)
	block := "Here is the box:\n\n```openscad\n// @checkpoint parent= desc=\"Box\"\ncube([20, 20, 10]);\n```\n"
	line, _ := json.Marshal(map[string]any{"event": "step_update", "step_update": map[string]any{"step_index": 5, "state": "DONE", "step_type": "agent_response", "text_delta": block}})
	end, _ := json.Marshal(map[string]any{"event": "result", "result": map[string]any{"conversation_id": "aaaaaaaa-0000-4000-8000-000000000001", "status": "SUCCESS", "response": block}})
	r.f.put("block.agy", string(line)+"\n"+string(end)+"\n")

	m, parts, _ := r.send(t, true, "crea una caja")

	if prompt := r.f.calls("agy")[0][1]; !strings.Contains(prompt, "NOT connected") {
		t.Errorf("el prompt dice que no use las herramientas de ScadLab:\n%s", prompt)
	}
	for _, want := range []string{"could not be approved", "not a valid JSON object", r.settings, "Repair Antigravity permissions"} {
		if !strings.Contains(m.Content, want) {
			t.Errorf("falta %q en:\n%s", want, m.Content)
		}
	}
	if got := strings.Join(toolsOf(parts), " "); got != "autosave:ok render:ok" {
		t.Errorf("la red de seguridad guardó y renderizó el bloque: %s", got)
	}
	if r.settingsText() != broken {
		t.Errorf("el archivo del usuario no se toca: %q", r.settingsText())
	}
	if _, denied := r.events(); len(denied) != 0 {
		t.Errorf("no hubo rechazo de herramienta: no hay aviso de reparar: %+v", denied)
	}
	if mcpEv, _ := r.events(); !hasErrorEvent(mcpEv, "not a valid JSON object") {
		t.Errorf("aviso de error con el motivo: %+v", mcpEv)
	}
}

func hasErrorEvent(evs []AntigravityMCPEvent, sub string) bool {
	for _, e := range evs {
		if strings.Contains(e.Error, sub) {
			return true
		}
	}
	return false
}

// En consulta, si el permiso falta, la explicación también llega (antes solo la daba
// «Agente»).
func TestAntigravityChatAskModeAlsoExplainsADeniedTool(t *testing.T) {
	r := newAgyRig(t, []string{"list_checkpoints"}, "x")
	r.writeSettings(`{"permissions": {"allow": []}}`)
	if _, err := r.a.RemoveAntigravityPermissions(); err != nil {
		t.Fatal(err)
	}
	m, _, _ := r.send(t, false, "¿qué hay?")
	if !strings.Contains(m.Content, "Antigravity blocked ScadLab's “list_checkpoints” tool") {
		t.Errorf("la explicación llega también en consulta:\n%s", m.Content)
	}
}

// Si no se puede escribir la marca de «Solo consulta», el turno NO corre: sin ella el
// servidor dejaría escribir y las reglas globales aprueban la escritura.
func TestAntigravityChatAskModeDoesNotRunWithoutTheMark(t *testing.T) {
	r := newAgyRig(t, []string{"list_checkpoints"}, "x")
	if err := os.WriteFile(filepath.Join(r.dir, ".scadlab"), []byte("soy un archivo"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := r.a.NewChat(r.wsID, "cli:antigravity", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.a.SetChatMode(c.ID, "ask"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-r.ev.errs:
		if !strings.Contains(e.Message, "could not be marked as “Query only”") {
			t.Errorf("mensaje: %q", e.Message)
		}
	case d := <-r.ev.done:
		t.Fatalf("el turno no debía completarse: %+v", d)
	}
	if len(r.f.calls("agy")) != 0 {
		t.Error("el agy de mentira no debió ni lanzarse")
	}
}

// Con el servidor MCP apagado no se toca el settings.json de Antigravity.
func TestAgyPermissionsAreOnlyForAntigravityWithTheMCPOn(t *testing.T) {
	r := newAgyRig(t, []string{"printer_info"}, "ok")
	if err := r.a.SetMCPEnabled(false); err != nil {
		t.Fatal(err)
	}
	r.send(t, true, "crea una caja")
	if _, err := os.Stat(r.settings); err == nil {
		t.Errorf("con el MCP apagado no se escribe nada:\n%s", r.settingsText())
	}
}

// Los enlaces de Configuración: estado, quitar (solo lo nuestro; no se repone solo) y
// volver a agregar.
func TestAntigravityPermissionsSettingsLifecycle(t *testing.T) {
	r := newAgyRig(t, nil, "")
	a := r.a

	st, _ := a.AntigravityPermissionsStatus()
	if st.State != "none" || st.Approved != 0 || st.Total != len(clichat.AgyRules()) || !st.Managed || st.Path != r.settings {
		t.Fatalf("sin archivo: %+v", st)
	}
	r.writeSettings(`{"permissions": {"allow": ["command(ls)", "mcp(scadlab/render)"]}}`)
	if st, _ = a.AntigravityPermissionsStatus(); st.State != "partial" || st.Approved != 1 {
		t.Fatalf("parcial: %+v", st)
	}

	st, err := a.RepairAntigravityPermissions()
	if err != nil || st.State != "ok" || st.Approved != st.Total {
		t.Fatalf("volver a agregar: %+v %v", st, err)
	}
	st, err = a.RemoveAntigravityPermissions()
	if err != nil || st.State != "none" || st.Managed {
		t.Fatalf("quitar: %+v %v", st, err)
	}
	if !strings.Contains(r.settingsText(), `"command(ls)"`) {
		t.Errorf("lo del usuario se queda:\n%s", r.settingsText())
	}
	// Elegir Antigravity de nuevo no repone lo que se quitó a propósito.
	res, err := a.EnsureAntigravityMCP()
	if err != nil || res.PermissionsAdded != 0 || r.ruleCount() != 0 {
		t.Errorf("no se repone solo: %+v %v\n%s", res, err, r.settingsText())
	}

	// Sin agy no hay nada que mostrar.
	prev := agyPath
	agyPath = func() string { return "" }
	t.Cleanup(func() { agyPath = prev })
	if st, _ = a.AntigravityPermissionsStatus(); st.State != "not-installed" {
		t.Errorf("sin agy: %+v", st)
	}
}

func TestAntigravityPermissionsStatusReportsAnUnreadableFile(t *testing.T) {
	r := newAgyRig(t, nil, "")
	r.writeSettings(`[no es objeto`)
	st, _ := r.a.AntigravityPermissionsStatus()
	if st.State != "unreadable" || !strings.Contains(st.Detail, "not a valid JSON object") {
		t.Errorf("%+v", st)
	}
	if _, err := r.a.RepairAntigravityPermissions(); err == nil || !strings.Contains(err.Error(), r.settings) {
		t.Errorf("reparar avisa y nombra el archivo: %v", err)
	}
	if r.settingsText() != `[no es objeto` {
		t.Error("no se pisa")
	}
}

// Un hooks.json heredado (de la versión que usaba hooks) con la entrada de ScadLab se
// limpia al abrir el workspace sin tocar lo del usuario; si era solo de ScadLab, se
// borra el archivo.
func TestOpeningAWorkspaceRemovesTheLegacyHookEntry(t *testing.T) {
	r := newAgyRig(t, nil, "")
	if err := os.MkdirAll(filepath.Dir(r.hooksFile()), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"mi-hook": {"Stop": []}, "scadlab-managed": {"PreToolUse": [{"matcher": "call_mcp_tool"}]}}`
	if err := os.WriteFile(r.hooksFile(), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.a.SyncWorkspaceMCP(r.wsID); err != nil {
		t.Fatal(err)
	}
	left := readFile(t, r.hooksFile())
	if strings.Contains(left, "scadlab-managed") || !strings.Contains(left, `"mi-hook"`) {
		t.Errorf("queda solo lo del usuario:\n%s", left)
	}

	if err := os.WriteFile(r.hooksFile(), []byte(`{"scadlab-managed": {}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.a.SyncWorkspaceMCP(r.wsID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.hooksFile()); err == nil {
		t.Error("era solo de ScadLab: se borra")
	}
}
