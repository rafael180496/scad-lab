//go:build !windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scadlab/backend/appdata"
	"scadlab/backend/checkpoint"
	"scadlab/backend/clichat"
	"scadlab/backend/detect"
	"scadlab/backend/hermes"
	"scadlab/backend/store"
)

// "La IA edita el archivo que nombras" (S16) con cada proveedor. El usuario tiene
// 001-base.scad y 002-x.scad, el segundo seleccionado, y pide «modifica 002-x.scad para…»:
//
//   - el prompt que recibe el CLI nombra la carpeta ABSOLUTA, el archivo seleccionado y
//     la regla de update_checkpoint (el bug original: el modelo inventó una ruta para
//     view_file, falló, creyó que los checkpoints eran inmutables y creó un hijo);
//   - los argumentos/entorno del turno permiten update_checkpoint en «Agente» y no en
//     «Solo consulta»;
//   - el CLI de mentira llama update_checkpoint contra el `ScadLab --mcp` REAL (este mismo
//     binario de pruebas relanzado como servidor): 002-x.scad cambia EN EL LUGAR, queda 1
//     versión en su historial, no aparece 003-*.scad, la app emite checkpoint:updated y el
//     paso nombra 002-x.scad;
//   - con «hazme una variante» se llama create_checkpoint y nace 003-*.scad.
//
// NINGUNA prueba abre un CLI real, un slicer ni toca ~/.gemini.

// fakeMCPCLIScript es un CLI de mentira para Claude, Codex y OpenCode: registra cómo lo
// llamaron, ejecuta contra el servidor MCP real las llamadas que la prueba dejó en
// mcpcalls.<cli> (una por línea: herramienta TAB argumentos JSON) y después emite el stream
// NDJSON de stream.<cli>, que cuenta lo mismo que hizo (el CLI real hace las dos cosas a
// la vez: el modelo llama la herramienta y el CLI imprime el evento).
const fakeMCPCLIScript = `#!/bin/sh
n=$(basename "$0")
d="$FAKE_DIR"
if [ "$1" = "--version" ]; then echo "$n 9.8.7 (de mentira)"; exit 0; fi
{ printf '<<CALL>>\0'; for a in "$@"; do printf '%s\0' "$a"; done; } >> "$d/argv.$n"
pwd > "$d/cwd.$n"
printf '%s' "$OPENCODE_CONFIG_CONTENT" > "$d/opencode-config.$n"
if [ "$1" = "session" ] && [ -f "$d/export.$n" ]; then cat "$d/export.$n"; exit 0; fi
bin=$(cat "$d/scadlab.path")
INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}'
NOTIF='{"jsonrpc":"2.0","method":"notifications/initialized"}'
i=0
if [ -f "$d/mcpcalls.$n" ]; then
  while IFS="$(printf '\t')" read -r tool args; do
    i=$((i+1))
    CALL="{\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"$tool\",\"arguments\":$args}}"
    printf '%s\n%s\n%s\n' "$INIT" "$NOTIF" "$CALL" | "$bin" --mcp 2>>"$d/mcp.stderr" > "$d/resp.$n.$i"
  done < "$d/mcpcalls.$n"
fi
[ -f "$d/stream.$n" ] && cat "$d/stream.$n"
exit 0
`

const (
	updOrig   = "// @checkpoint parent=001-base.scad desc=\"caja con tapa\"\ncolor(\"red\") cube([20, 20, 20]);\n"
	updTaller = "color(\"red\") cube([20, 20, 30]);\n"
)

// updRig es un workspace con 001-base.scad y 002-x.scad (el seleccionado) y un `ScadLab`
// de mentira que relanza este binario de pruebas como servidor `--mcp`.
type updRig struct {
	t     *testing.T
	a     *App
	wsID  int64
	dir   string
	ev    *hermesEvents
	f     *appFakeCLIs
	named *namedEvents
}

func newUpdRig(t *testing.T) *updRig {
	t.Helper()
	// Sin OpenSCAD nativo ni extras: el render del servidor sale del motor integrado, el
	// mismo que usan las demás pruebas del servidor.
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	a, wsID, dir, ev, f := cliApp(t)
	t.Setenv(appdata.OverrideEnv, a.dataDir) // el servidor real lee la base de la prueba, no la del usuario
	if err := os.WriteFile(filepath.Join(dir, "002-x.scad"), []byte(updOrig), 0o644); err != nil {
		t.Fatal(err)
	}
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
	return &updRig{t: t, a: a, wsID: wsID, dir: dir, ev: ev, f: f, named: captureNamedEvents(a)}
}

// installMCP deja el CLI de mentira `bin` con sus llamadas MCP reales y su stream.
func (r *updRig) installMCP(bin, stream string, calls ...mcpCall) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.f.dir, bin), []byte(fakeMCPCLIScript), 0o755); err != nil {
		r.t.Fatal(err)
	}
	var lines []string
	for _, c := range calls {
		raw, err := json.Marshal(c.Args)
		if err != nil {
			r.t.Fatal(err)
		}
		lines = append(lines, c.Tool+"\t"+string(raw))
	}
	r.f.put("mcpcalls."+bin, strings.Join(lines, "\n")+"\n")
	r.f.put("stream."+bin, stream)
	r.f.put("export.opencode", "{}")
	clichat.Default.Reset()
}

type mcpCall struct {
	Tool string
	Args map[string]any
}

func updateCall() mcpCall {
	return mcpCall{"update_checkpoint", map[string]any{"file": "002-x.scad", "source": updTaller, "note": "más alto"}}
}

func variantCall() mcpCall {
	return mcpCall{"create_checkpoint", map[string]any{"label": "variante alta", "description": "Variante más alta", "parent": "002-x.scad", "source": updTaller}}
}

// send manda un mensaje con 002-x.scad seleccionado, espera el final del turno y devuelve el chat.
func (r *updRig) send(provider string, agent bool, text string) int64 {
	r.t.Helper()
	c, err := r.a.NewChat(r.wsID, provider, "")
	if err != nil {
		r.t.Fatal(err)
	}
	if _, err := r.a.SendChatMessage(c.ID, text, "002-x.scad", nil, SendOptions{Agent: boolp(agent)}); err != nil {
		r.t.Fatal(err)
	}
	r.ev.wait(r.t)
	return c.ID
}

func (r *updRig) scads() []string {
	files, _ := filepath.Glob(filepath.Join(r.dir, "*.scad"))
	for i := range files {
		files[i] = filepath.Base(files[i])
	}
	return files
}

// promptOf es el prompt de ScadLab que recibió el CLI: el argumento que empieza con [ScadLab].
func promptOf(t *testing.T, args []string) string {
	t.Helper()
	for _, a := range args {
		if strings.HasPrefix(a, "[ScadLab]") {
			return a
		}
	}
	t.Fatalf("ningún argumento es el prompt de ScadLab: %q", args)
	return ""
}

// requireEditRule comprueba lo que el prompt de «Agente» tiene que decirle al modelo para no
// repetir el bug: dónde está el proyecto, cuál es el archivo seleccionado y cómo leer y editar.
func (r *updRig) requireEditRule(prompt string) {
	r.t.Helper()
	for _, want := range []string{
		"The project is at " + r.dir, // la ruta ABSOLUTA: view_file de Antigravity pide AbsolutePath
		"it is your current directory",
		"Checkpoint selected in the app", // «este archivo» / «el actual»
		"002-x.scad",
		"`read_checkpoint`",
		"`update_checkpoint`",
		"Never write .scad files with your file tools or the shell",
	} {
		if !strings.Contains(prompt, want) {
			r.t.Errorf("al prompt le falta %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "never edit an existing one") {
		r.t.Errorf("el prompt ya no dice que los checkpoints son inmutables:\n%s", prompt)
	}
}

// requireEditedInPlace comprueba el resultado de «modifica 002-x.scad»: cambió en el
// lugar, con su cabecera, una versión en el historial, ningún .scad nuevo, el evento y el
// paso que nombra el archivo.
func (r *updRig) requireEditedInPlace() {
	r.t.Helper()
	got := readFile(r.t, filepath.Join(r.dir, "002-x.scad"))
	if !strings.Contains(got, "cube([20, 20, 30])") || !strings.HasPrefix(got, "// @checkpoint parent=001-base.scad") {
		r.t.Errorf("002-x.scad cambió en el lugar y conserva su cabecera:\n%s", got)
	}
	hist, err := checkpoint.History(r.dir, "002-x.scad")
	if err != nil || len(hist) != 1 || hist[0].Reason != checkpoint.ReasonAI || hist[0].Note != "más alto" {
		r.t.Fatalf("historial: %+v %v", hist, err)
	}
	if old, _ := checkpoint.ReadVersion(r.dir, "002-x.scad", hist[0].ID); old != updOrig {
		r.t.Errorf("la versión guardada es la de antes, entera:\n%s", old)
	}
	if !checkpoint.EditedByAI(r.dir, "002-x.scad") {
		r.t.Error("el árbol marca el archivo como editado por la IA")
	}
	if got := strings.Join(r.scads(), ","); got != "001-base.scad,002-x.scad" {
		r.t.Errorf("no aparece un .scad nuevo: %s", got)
	}
	updated := r.named.updatedFiles()
	if len(updated) == 0 || updated[0] != "002-x.scad" {
		r.t.Errorf("checkpoint:updated con 002-x.scad: %v", updated)
	}
	var step *hermes.Step
	for _, e := range stepsOf(r.ev) {
		if e.Step.Tool == "update_checkpoint" && e.Step.Status == hermes.StepOK {
			s := e.Step
			step = &s
		}
		if e.Step.Tool == autoStepTool {
			r.t.Errorf("la red de seguridad no guarda otro checkpoint: %+v", e.Step)
		}
	}
	if step == nil || step.Args["file"] != "002-x.scad" {
		r.t.Errorf("el paso update_checkpoint nombra 002-x.scad: %+v", stepsOf(r.ev))
	}
}

// requireVariant comprueba el resultado de «hazme una variante»: nace 003-*.scad hijo de
// 002 y 002 queda intacto (sin historial, sin evento de actualización).
func (r *updRig) requireVariant() {
	r.t.Helper()
	files := r.scads()
	if len(files) != 3 || files[2] != "003-variante-alta.scad" {
		r.t.Fatalf("la variante es un checkpoint nuevo: %v", files)
	}
	if got := readFile(r.t, filepath.Join(r.dir, "003-variante-alta.scad")); !strings.HasPrefix(got, "// @checkpoint parent=002-x.scad") {
		r.t.Errorf("la variante cuelga de 002-x.scad:\n%s", got)
	}
	if got := readFile(r.t, filepath.Join(r.dir, "002-x.scad")); got != updOrig {
		r.t.Errorf("002-x.scad no se toca:\n%s", got)
	}
	if hist, _ := checkpoint.History(r.dir, "002-x.scad"); len(hist) != 0 {
		r.t.Errorf("002-x.scad no tiene historial: %+v", hist)
	}
	if u := r.named.updatedFiles(); len(u) != 0 {
		r.t.Errorf("crear un checkpoint no es una actualización en el lugar: %v", u)
	}
}

// --- Streams NDJSON de cada CLI (lo que imprimiría al llamar la herramienta) -------------

func ndjson(lines ...map[string]any) string {
	var b strings.Builder
	for _, l := range lines {
		raw, _ := json.Marshal(l)
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

func toolResult(file string) string {
	raw, _ := json.Marshal(map[string]any{"file": file, "updated": true})
	return string(raw)
}

func claudeStream(tool string, args map[string]any, resultFile, final string) string {
	const sid = "7b3c1f20-5a4e-4d11-9c02-1e8d6f4a2b33"
	return ndjson(
		map[string]any{"type": "system", "subtype": "init", "session_id": sid, "tools": []string{"mcp__scadlab__" + tool},
			"mcp_servers": []map[string]any{{"name": "scadlab", "status": "connected"}}, "model": "claude-opus-5", "permissionMode": "acceptEdits"},
		map[string]any{"type": "assistant", "message": map[string]any{"model": "claude-opus-5", "id": "msg_1", "type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "tool_use", "id": "toolu_01", "name": "mcp__scadlab__" + tool, "input": args}}}},
		map[string]any{"type": "user", "session_id": sid, "message": map[string]any{"role": "user",
			"content": []any{map[string]any{"type": "tool_result", "tool_use_id": "toolu_01", "is_error": false,
				"content": []any{map[string]any{"type": "text", "text": toolResult(resultFile)}}}}}},
		map[string]any{"type": "assistant", "message": map[string]any{"model": "claude-opus-5", "id": "msg_2", "type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "text", "text": final}}}},
		map[string]any{"type": "result", "subtype": "success", "is_error": false, "duration_ms": 10, "num_turns": 2, "result": final, "stop_reason": "end_turn",
			"session_id": sid, "total_cost_usd": 0.01, "usage": map[string]any{"input_tokens": 3, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 0, "output_tokens": 30}},
	)
}

func codexStream(tool string, args map[string]any, resultFile, final string) string {
	item := func(status string, result any) map[string]any {
		return map[string]any{"type": "item." + map[string]string{"in_progress": "started", "completed": "completed"}[status],
			"item": map[string]any{"id": "item_1", "type": "mcp_tool_call", "server": "scadlab", "tool": tool, "arguments": args, "result": result, "error": nil, "status": status}}
	}
	return ndjson(
		map[string]any{"type": "thread.started", "thread_id": "01a00217-12d1-7492-b8df-b6a2c1f0bc86"},
		map[string]any{"type": "turn.started"},
		item("in_progress", nil),
		item("completed", map[string]any{"content": []any{map[string]any{"type": "text", "text": toolResult(resultFile)}}}),
		map[string]any{"type": "item.completed", "item": map[string]any{"id": "item_2", "type": "agent_message", "text": final}},
		map[string]any{"type": "turn.completed", "usage": map[string]any{"input_tokens": 100, "cached_input_tokens": 0, "output_tokens": 10, "reasoning_output_tokens": 0}},
	)
}

func opencodeStream(tool string, args map[string]any, resultFile, final string) string {
	const sid = "ses_ebf3d5642ffesbIf6ikLOXZQDR"
	return ndjson(
		map[string]any{"type": "step_start", "timestamp": 1, "sessionID": sid, "part": map[string]any{"id": "prt_1", "sessionID": sid, "type": "step-start"}},
		map[string]any{"type": "tool_use", "timestamp": 2, "sessionID": sid, "part": map[string]any{"partID": "prt_2", "sessionID": sid, "type": "tool", "id": "call-1", "tool": "scadlab_" + tool,
			"state": map[string]any{"status": "completed", "input": args, "output": toolResult(resultFile), "title": "scadlab_" + tool}}},
		map[string]any{"type": "step_finish", "timestamp": 3, "sessionID": sid, "part": map[string]any{"id": "prt_3", "sessionID": sid, "type": "step-finish", "reason": "tool-calls", "cost": 0,
			"tokens": map[string]any{"input": 10, "output": 5, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}}}},
		map[string]any{"type": "text", "timestamp": 4, "sessionID": sid, "part": map[string]any{"id": "prt_4", "sessionID": sid, "type": "text", "text": final}},
	)
}

type cliCase struct {
	id, bin string
	stream  func(tool string, args map[string]any, resultFile, final string) string
}

var shellCLIs = []cliCase{
	{"cli:claude", "claude", claudeStream},
	{"cli:codex", "codex", codexStream},
	{"cli:opencode", "opencode", opencodeStream},
}

// (El texto final trae el diseño en un bloque ```openscad, como suele hacer un modelo: la red de
// seguridad NO debe guardarlo como un checkpoint nuevo, porque update_checkpoint ya entregó.)
//
// «modifica 002-x.scad para…» con Claude Code, Codex y OpenCode: el prompt, el permiso y
// la edición real contra el servidor MCP.
func TestCLIsEditTheNamedFileInPlace(t *testing.T) {
	for _, tc := range shellCLIs {
		t.Run(tc.bin, func(t *testing.T) {
			r := newUpdRig(t)
			call := updateCall()
			r.installMCP(tc.bin, tc.stream("update_checkpoint", call.Args, "002-x.scad", "Listo: 002-x.scad ahora mide 30 mm de alto.\n\n```openscad\n"+updTaller+"```"), call)

			chatID := r.send(tc.id, true, "modifica 002-x.scad para que sea más alto")

			// OpenCode además se llama una vez más para exportar la sesión (tokens y modelo).
			calls := r.f.calls(tc.bin)
			if len(calls) == 0 {
				t.Fatal("el CLI no se llamó")
			}
			r.requireEditRule(promptOf(t, calls[0]))
			r.requireEditedInPlace()
			// El mensaje del asistente trae el paso guardado (el historial del chat lo enlaza).
			view, err := r.a.GetChat(chatID)
			if err != nil || len(view.Messages) != 2 {
				t.Fatalf("GetChat: %+v %v", view, err)
			}
			var found bool
			for _, s := range hermes.ParseParts(view.Messages[1].Parts).Steps {
				found = found || (s.Tool == "update_checkpoint" && s.Args["file"] == "002-x.scad")
			}
			if !found {
				t.Errorf("el paso guardado nombra 002-x.scad: %+v", view.Messages[1].Parts)
			}
		})
	}
}

// «hazme una variante»: create_checkpoint, y 002-x.scad queda como estaba.
func TestCLIsCreateAVariantWhenAskedForOne(t *testing.T) {
	for _, tc := range shellCLIs {
		t.Run(tc.bin, func(t *testing.T) {
			r := newUpdRig(t)
			call := variantCall()
			r.installMCP(tc.bin, tc.stream("create_checkpoint", call.Args, "003-variante-alta.scad", "Listo: 003-variante-alta.scad."), call)

			r.send(tc.id, true, "hazme una variante más alta de 002-x.scad")

			r.requireVariant()
			// El prompt sigue dando la misma regla: lo que decide es el pedido, no el prompt.
			r.requireEditRule(promptOf(t, r.f.calls(tc.bin)[0]))
		})
	}
}

// Los argumentos y el entorno del turno permiten update_checkpoint en «Agente» y lo
// quitan en «Solo consulta» (allowedTools de Claude, enabled_tools de Codex, permisos de
// OpenCode).
func TestUpdateCheckpointIsAllowedOnlyInAgentMode(t *testing.T) {
	r := newUpdRig(t)
	for _, tc := range shellCLIs {
		r.installMCP(tc.bin, "", mcpCall{"list_checkpoints", map[string]any{}})
	}
	r.send("cli:claude", true, "modifica 002-x.scad")
	r.send("cli:claude", false, "¿qué hace 002-x.scad?")
	r.send("cli:codex", true, "modifica 002-x.scad")
	r.send("cli:codex", false, "¿qué hace 002-x.scad?")
	r.send("cli:opencode", true, "modifica 002-x.scad")
	r.send("cli:opencode", false, "¿qué hace 002-x.scad?")

	claude := r.f.calls("claude")
	if got := argValue(claude[0], "--allowedTools"); !strings.Contains(got, "mcp__scadlab__update_checkpoint") {
		t.Errorf("Claude en Agente: %q", got)
	}
	if got := argValue(claude[1], "--allowedTools"); strings.Contains(got, "update_checkpoint") || !strings.Contains(got, "mcp__scadlab__read_checkpoint") {
		t.Errorf("Claude en Solo consulta no edita pero sí lee: %q", got)
	}

	enabled := func(args []string) string {
		for _, a := range args {
			if strings.HasPrefix(a, "mcp_servers.scadlab.enabled_tools=") {
				return a
			}
		}
		return ""
	}
	codex := r.f.calls("codex")
	if got := enabled(codex[0]); !strings.Contains(got, `"update_checkpoint"`) {
		t.Errorf("Codex en Agente: %q", got)
	}
	if got := enabled(codex[1]); got == "" || strings.Contains(got, "update_checkpoint") {
		t.Errorf("Codex en Solo consulta: %q", got)
	}

	ocAgent := openCodePermissions(t, r, "cli:opencode", true)
	if ocAgent["scadlab_update_checkpoint"] != "allow" || ocAgent["scadlab_read_checkpoint"] != "allow" || ocAgent["bash"] != "deny" {
		t.Errorf("OpenCode en Agente: %v", ocAgent)
	}
	ocAsk := openCodePermissions(t, r, "cli:opencode", false)
	if ocAsk["scadlab_update_checkpoint"] != "deny" || ocAsk["scadlab_read_checkpoint"] != "allow" {
		t.Errorf("OpenCode en Solo consulta: %v", ocAsk)
	}

	// «Solo consulta» ni siquiera le da la regla de editar en el prompt.
	if p := promptOf(t, claude[1]); strings.Contains(p, "edit it with `update_checkpoint`") || !strings.Contains(p, "Checkpoint selected in the app") || !strings.Contains(p, "`read_checkpoint`") {
		t.Errorf("prompt de consulta:\n%s", p)
	}
}

// openCodePermissions manda un turno y devuelve los permisos que OpenCode recibió por entorno.
func openCodePermissions(t *testing.T, r *updRig, provider string, agent bool) map[string]string {
	t.Helper()
	r.send(provider, agent, "revisa 002-x.scad")
	var cfg struct {
		Permission map[string]string `json:"permission"`
	}
	if err := json.Unmarshal([]byte(r.f.read("opencode-config.opencode")), &cfg); err != nil {
		t.Fatalf("OPENCODE_CONFIG_CONTENT: %v", err)
	}
	return cfg.Permission
}

// --- Antigravity -------------------------------------------------------------------

// agyUpdateRig es el rig de Antigravity con 002-x.scad y la llamada de la herramienta que
// el `agy` de mentira hace contra el servidor real.
func agyUpdateRig(t *testing.T, call mcpCall) *agyRig {
	t.Helper()
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	r := newAgyRig(t, []string{call.Tool}, "Listo")
	if err := os.WriteFile(filepath.Join(r.dir, "002-x.scad"), []byte(updOrig), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(call.Args)
	r.f.put("args."+call.Tool, string(raw))
	// El resultado que Antigravity muestra en el paso, con el archivo (JSON ya escapado).
	r.f.put("out."+call.Tool, `{\"file\":\"002-x.scad\",\"updated\":true}`)
	return r
}

func agySend(t *testing.T, r *agyRig, agent bool, text string) (store.ChatMessage, hermes.Parts) {
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
	if _, err := r.a.SendChatMessage(c.ID, text, "002-x.scad", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	r.ev.wait(t)
	view, err := r.a.GetChat(c.ID)
	if err != nil || len(view.Messages) != 2 {
		t.Fatalf("GetChat: %+v %v", view, err)
	}
	return view.Messages[1], hermes.ParseParts(view.Messages[1].Parts)
}

func TestAntigravityEditsTheNamedFileInPlace(t *testing.T) {
	call := updateCall()
	r := agyUpdateRig(t, call)
	named := captureNamedEvents(r.a)

	_, parts := agySend(t, r, true, "es en el archivo 002-x.scad que quería los cambios: hazlo más alto")

	if got := strings.Join(toolsOf(parts), " "); got != "update_checkpoint:ok" {
		t.Fatalf("pasos: %s", got)
	}
	if parts.Steps[0].Args["file"] != "002-x.scad" {
		t.Errorf("el paso nombra el archivo: %+v", parts.Steps[0].Args)
	}
	got := readFile(t, filepath.Join(r.dir, "002-x.scad"))
	if !strings.Contains(got, "cube([20, 20, 30])") || !strings.HasPrefix(got, "// @checkpoint parent=001-base.scad") {
		t.Errorf("002-x.scad cambió en el lugar:\n%s", got)
	}
	hist, err := checkpoint.History(r.dir, "002-x.scad")
	if err != nil || len(hist) != 1 || hist[0].Reason != checkpoint.ReasonAI {
		t.Errorf("historial: %+v %v", hist, err)
	}
	if files, _ := filepath.Glob(filepath.Join(r.dir, "003-*.scad")); len(files) != 0 {
		t.Errorf("no aparece un hijo: %v", files)
	}
	if u := named.updatedFiles(); len(u) == 0 || u[0] != "002-x.scad" {
		t.Errorf("checkpoint:updated: %v", u)
	}
	// El prompt: la ruta absoluta (para que view_file no inviente una) y la regla.
	prompt := promptOf(t, r.f.calls("agy")[0])
	for _, want := range []string{"The project is at " + r.dir, "Checkpoint selected in the app", "002-x.scad", "`update_checkpoint`", "`read_checkpoint`"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("al prompt le falta %q:\n%s", want, prompt)
		}
	}
	// Las reglas de permiso incluyen update_checkpoint (el agy de mentira solo corre las
	// herramientas con regla): sincronizadas solas, sin que nadie las agregue.
	if !strings.Contains(r.settingsText(), `"mcp(scadlab/update_checkpoint)"`) {
		t.Errorf("falta la regla de update_checkpoint:\n%s", r.settingsText())
	}
	found := false
	for _, rule := range clichat.AgyRules() {
		found = found || rule == "mcp(scadlab/update_checkpoint)"
	}
	if !found {
		t.Error("AgyRules incluye update_checkpoint")
	}
}

func TestAntigravityCreatesAVariantWhenAskedForOne(t *testing.T) {
	r := agyUpdateRig(t, variantCall())
	r.f.put("out.create_checkpoint", `{\"file\":\"003-variante-alta.scad\"}`)

	_, parts := agySend(t, r, true, "hazme una variante más alta")

	if got := strings.Join(toolsOf(parts), " "); got != "create_checkpoint:ok" {
		t.Fatalf("pasos: %s", got)
	}
	if files, _ := filepath.Glob(filepath.Join(r.dir, "*.scad")); len(files) != 3 {
		t.Errorf("nace un checkpoint nuevo: %v", files)
	}
	if got := readFile(t, filepath.Join(r.dir, "002-x.scad")); got != updOrig {
		t.Errorf("002-x.scad intacto:\n%s", got)
	}
	if hist, _ := checkpoint.History(r.dir, "002-x.scad"); len(hist) != 0 {
		t.Errorf("sin historial: %+v", hist)
	}
}

// En «Solo consulta» el SERVIDOR rechaza update_checkpoint (la regla de Antigravity es
// global y la aprueba): el archivo no cambia y no se guarda ninguna versión.
func TestAntigravityAskModeRejectsUpdateCheckpoint(t *testing.T) {
	r := agyUpdateRig(t, updateCall())

	_, parts := agySend(t, r, false, "cambia 002-x.scad para que sea más alto")

	if got := strings.Join(toolsOf(parts), " "); got != "update_checkpoint:error" {
		t.Fatalf("pasos: %s", got)
	}
	resp := r.f.read("resp.1.update_checkpoint")
	if !strings.Contains(resp, `"isError":true`) || !strings.Contains(resp, "Query only") {
		t.Errorf("el servidor rechaza con el motivo:\n%s", resp)
	}
	if got := readFile(t, filepath.Join(r.dir, "002-x.scad")); got != updOrig {
		t.Errorf("002-x.scad no cambia:\n%s", got)
	}
	if hist, _ := checkpoint.History(r.dir, "002-x.scad"); len(hist) != 0 {
		t.Errorf("sin versiones: %+v", hist)
	}
}
