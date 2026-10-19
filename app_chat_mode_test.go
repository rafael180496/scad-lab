package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"scadlab/backend/agentsessions"
	"scadlab/backend/appdata"
	"scadlab/backend/store"
)

// boolp es el override explícito de SendOptions.Agent en las pruebas.
func boolp(b bool) *bool { return &b }

// --- El modo es de cada conversación (S12.c, T12) ------------------------------------

// Una conversación nueva nace en «Agente» (modo vacío) con cualquier proveedor.
// SetChatMode guarda «Solo consulta» SOLO en esa conversación, GetChat y ListChats
// lo traen, se puede volver, y un modo inventado o un chat inexistente fallan sin
// tocar nada.
func TestSetChatModeBindingIsPerConversation(t *testing.T) {
	a, wsID, _, _ := hermesApp(t, newSim())
	c1, err := a.NewChat(wsID, "ollama", capsAgent)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := a.NewChat(wsID, "ollama", capsAgent)
	if err != nil {
		t.Fatal(err)
	}
	if c1.Mode != "" || c2.Mode != "" || !c1.Agent() || !c2.Agent() {
		t.Fatalf("una conversación nueva es Agente: %+v %+v", c1, c2)
	}

	if err := a.SetChatMode(c1.ID, "ask"); err != nil {
		t.Fatal(err)
	}
	v1, _ := a.GetChat(c1.ID)
	v2, _ := a.GetChat(c2.ID)
	if v1.Chat.Mode != store.ChatModeAsk || v1.Chat.Agent() || !v2.Chat.Agent() {
		t.Errorf("elegir consulta en una no cambia la otra: %+v / %+v", v1.Chat, v2.Chat)
	}
	list, _ := a.ListChats(wsID)
	got := map[int64]string{}
	for _, c := range list {
		got[c.ID] = c.Mode
	}
	if got[c1.ID] != "ask" || got[c2.ID] != "" {
		t.Errorf("ListChats trae el modo de cada una: %v", got)
	}
	// Elegir consulta no deja un predeterminado: la conversación que se abre después es Agente.
	if c3, _ := a.NewChat(wsID, "ollama", capsAgent); !c3.Agent() || c3.Mode != "" {
		t.Errorf("la conversación siguiente sigue naciendo en Agente: %+v", c3)
	}

	if err := a.SetChatMode(c1.ID, "agent"); err != nil {
		t.Fatal(err)
	}
	if v, _ := a.GetChat(c1.ID); !v.Chat.Agent() || v.Chat.Mode != "" {
		t.Errorf("de vuelta a Agente: %+v", v.Chat)
	}
	_ = a.SetChatMode(c1.ID, "ask")
	if err := a.SetChatMode(c1.ID, "plan"); err == nil {
		t.Error("un modo desconocido debía fallar")
	}
	if v, _ := a.GetChat(c1.ID); v.Chat.Mode != "ask" {
		t.Errorf("un modo inválido no cambia lo guardado: %+v", v.Chat)
	}
	if err := a.SetChatMode(9999, "ask"); err == nil {
		t.Error("un chat inexistente debía fallar")
	}
}

// Hermes: sin opciones, SendChatMessage usa el modo de la conversación. Una
// conversación nueva viaja con las herramientas (el interruptor del modo Agente);
// en «Solo consulta» van sin ellas; otra conversación sigue con herramientas; y el
// override explícito del envío manda sobre el modo guardado sin cambiarlo.
func TestHermesSendUsesTheConversationMode(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "uno"}, {text: "dos"}, {text: "tres"}, {text: "cuatro"}, {text: "cinco"}}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	c1, _ := a.NewChat(wsID, "ollama", capsAgent)
	c2, _ := a.NewChat(wsID, "ollama", capsAgent)
	send := func(id int64, text string, opts SendOptions) {
		t.Helper()
		if _, err := a.SendChatMessage(id, text, "", nil, opts); err != nil {
			t.Fatal(err)
		}
		ev.wait(t)
	}

	send(c1.ID, "una caja", SendOptions{}) // conversación nueva: Agente
	if !sim.hasTools(0) || !strings.Contains(sim.text(0), "Hermes") {
		t.Errorf("una conversación nueva viaja con herramientas: tools=%v", sim.hasTools(0))
	}

	if err := a.SetChatMode(c1.ID, "ask"); err != nil {
		t.Fatal(err)
	}
	send(c1.ID, "explícame esto", SendOptions{})
	if sim.hasTools(1) || strings.Contains(sim.text(1), "Hermes") {
		t.Errorf("en «Solo consulta» no se declaran herramientas: tools=%v", sim.hasTools(1))
	}

	send(c2.ID, "otra caja", SendOptions{}) // la otra conversación no se enteró
	if !sim.hasTools(2) {
		t.Error("elegir consulta en una conversación no apaga las herramientas de otra")
	}

	// Override del envío: manda ese turno, no cambia lo guardado.
	send(c1.ID, "ahora sí crea", SendOptions{Agent: boolp(true)})
	if !sim.hasTools(3) {
		t.Error("el override Agent=true enciende las herramientas de ese turno")
	}
	send(c2.ID, "solo explica", SendOptions{Agent: boolp(false)})
	if sim.hasTools(4) {
		t.Error("el override Agent=false las apaga en ese turno")
	}
	if v, _ := a.GetChat(c1.ID); v.Chat.Mode != "ask" {
		t.Errorf("un override no cambia el modo guardado de c1: %+v", v.Chat)
	}
	if v, _ := a.GetChat(c2.ID); v.Chat.Mode != "" {
		t.Errorf("un override no cambia el modo guardado de c2: %+v", v.Chat)
	}
}

// La terminal de agentes no arranca ningún CLI restringido: el argv que arma
// ScadLab (MCP, carpetas, sesión) no lleva modo plan, sandbox de solo lectura,
// listas de herramientas ni nada que lo ponga en consulta, y tampoco ningún flag
// que salte permisos (ahí la persona aprueba en su terminal).
func TestAgentTerminalArgsAreNeverReadOnlyNorBypassPermissions(t *testing.T) {
	f := newProjFixture(t, 2)
	f.openAll(t, 0, 1)
	me := f.ws[0]
	// Con y sin el MCP encendido, y con cada modo de arranque de cada CLI.
	for _, mcpOn := range []bool{true, false} {
		if err := f.a.store.SetSetting("mcpEnabled", mcpOn); err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{agentsessions.Claude, agentsessions.Codex, agentsessions.OpenCode, agentsessions.Antigravity} {
			for _, mode := range []string{agentsessions.ModeNew, agentsessions.ModeContinue} {
				sess, err := agentsessions.Args(id, mode, "")
				if err != nil {
					t.Fatal(err)
				}
				args := f.a.agentLaunchArgs(id, me, sess)
				for _, bad := range []string{
					"--permission-mode", "--allowedTools", "--allowed-tools", "--disallowedTools", "--disallowed-tools", // Claude
					"--sandbox", "read-only", "enabled_tools", "approval_policy", // Codex
					"--mode", "plan", "--agent", "--ask", "--read-only", // Antigravity y OpenCode
					"--dangerously-skip-permissions", "--dangerously-bypass-approvals-and-sandbox", "--yolo", "bypassPermissions", "--auto", // nunca
				} {
					for _, a := range args {
						if a == bad || strings.Contains(a, bad+"=") || strings.Contains(a, "sandbox_mode") {
							t.Errorf("%s (%s, mcp=%v) arranca con %q: %q", id, mode, mcpOn, bad, args)
						}
					}
				}
			}
		}
	}
}

// El servidor `--mcp` no tiene un modo de solo lectura: sin ningún flag extra
// declara las herramientas de escritura (crear checkpoints, importar mallas,
// exportar) junto con las de lectura, y su único flag es --workspace.
func TestMCPServerIsWritableByDefault(t *testing.T) {
	t.Setenv(appdata.OverrideEnv, t.TempDir())
	ws := t.TempDir()
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")
	var out, errOut bytes.Buffer
	if code := runMCP([]string{"--workspace", ws}, in, &out, &errOut); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	var tools []struct {
		Name        string         `json:"name"`
		Annotations map[string]any `json:"annotations"`
	}
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var m struct {
			ID     int `json:"id"`
			Result struct {
				Tools []struct {
					Name        string         `json:"name"`
					Annotations map[string]any `json:"annotations"`
				} `json:"tools"`
			} `json:"result"`
		}
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		if m.ID == 2 {
			tools = m.Result.Tools
		}
	}
	have := map[string]bool{}
	writers := 0
	for _, tl := range tools {
		have[tl.Name] = true
		if ro, _ := tl.Annotations["readOnlyHint"].(bool); !ro {
			writers++
		}
	}
	for _, want := range []string{"create_checkpoint", "import_mesh", "export", "render", "preview"} {
		if !have[want] {
			t.Errorf("--mcp sin flags debe declarar %q (declaradas: %d)", want, len(tools))
		}
	}
	if writers == 0 {
		t.Error("ninguna herramienta de escritura: el servidor arrancó en solo lectura")
	}
	// Un flag de solo lectura no existe: el único es --workspace.
	if code := runMCP([]string{"--read-only"}, strings.NewReader(""), &out, &errOut); code != 2 {
		t.Errorf("--read-only no es un flag del servidor: exit %d", code)
	}
}
