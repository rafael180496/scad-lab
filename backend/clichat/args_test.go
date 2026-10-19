package clichat

import (
	"slices"
	"strings"
	"testing"

	"scadlab/backend/i18n"
)

var mcpClaude = []string{"--mcp-config", `{"mcpServers":{"scadlab":{"command":"/app/ScadLab","args":["--mcp","--workspace","/work/caja"]}}}`}
var mcpCodex = []string{
	"-c", `mcp_servers.scadlab.command="/app/ScadLab"`,
	"-c", `mcp_servers.scadlab.args=["--mcp","--workspace","/work/caja"]`,
}

func baseTurn(cli string, mode Mode) Turn {
	t := Turn{CLI: cli, Exe: "/bin/" + cli, Cwd: "/work/caja", Prompt: "haz una caja", Mode: mode, Lang: "es", MCPEnabled: true}
	switch cli {
	case "claude":
		t.MCP = mcpClaude
	case "codex":
		t.MCP = mcpCodex
	}
	return t
}

func mustArgs(t *testing.T, tr Turn) []string {
	t.Helper()
	args, err := BuildArgs(tr)
	if err != nil {
		t.Fatalf("BuildArgs(%s, %s): %v", tr.CLI, tr.Mode, err)
	}
	return args
}

// «Solo consulta» y «Agente» difieren exactamente en lo que dice la spec, para
// cada CLI. Ninguno de los dos permite comandos de shell.
func TestBuildArgsAskVersusEdit(t *testing.T) {
	t.Run("claude", func(t *testing.T) {
		ask, edit := mustArgs(t, baseTurn("claude", ModeAsk)), mustArgs(t, baseTurn("claude", ModeEdit))
		if hasArg(ask, "--permission-mode") || !has(edit, "--permission-mode", "acceptEdits") {
			t.Errorf("solo «Agente» acepta ediciones:\nask=%v\nedit=%v", ask, edit)
		}
		// La shell se niega siempre; las herramientas que editan, en «Solo consulta».
		for _, a := range [][]string{ask, edit} {
			if !strings.Contains(valueOf(a, "--disallowedTools"), "Bash") {
				t.Errorf("la shell se deniega siempre: %v", a)
			}
		}
		if !strings.Contains(valueOf(ask, "--disallowedTools"), "Edit") || strings.Contains(valueOf(edit, "--disallowedTools"), "Edit") {
			t.Errorf("Edit se niega solo en «Solo consulta»: %q / %q", valueOf(ask, "--disallowedTools"), valueOf(edit, "--disallowedTools"))
		}
		askTools, editTools := valueOf(ask, "--allowedTools"), valueOf(edit, "--allowedTools")
		for _, tool := range []string{"render", "preview", "measure", "lint", "read_checkpoint", "list_checkpoints"} {
			if !strings.Contains(askTools, "mcp__scadlab__"+tool) {
				t.Errorf("«Solo consulta» debe poder usar %s: %q", tool, askTools)
			}
		}
		if strings.Contains(askTools, "create_checkpoint") || strings.Contains(askTools, "export") {
			t.Errorf("«Solo consulta» no escribe: %q", askTools)
		}
		if !strings.Contains(editTools, "mcp__scadlab__create_checkpoint") || !strings.Contains(editTools, "mcp__scadlab__export") {
			t.Errorf("«Agente» usa todas: %q", editTools)
		}
	})

	t.Run("codex", func(t *testing.T) {
		ask, edit := mustArgs(t, baseTurn("codex", ModeAsk)), mustArgs(t, baseTurn("codex", ModeEdit))
		if !has(ask, "--sandbox", "read-only") || !has(edit, "--sandbox", "workspace-write") {
			t.Errorf("sandbox:\nask=%v\nedit=%v", ask, edit)
		}
		enabled := ""
		for i, a := range ask {
			if a == "-c" && strings.HasPrefix(ask[i+1], "mcp_servers.scadlab.enabled_tools=") {
				enabled = ask[i+1]
			}
		}
		if !strings.Contains(enabled, `"render"`) || strings.Contains(enabled, "create_checkpoint") {
			t.Errorf("«Solo consulta» limita las herramientas MCP a las de lectura: %q", enabled)
		}
		for _, a := range edit {
			if strings.Contains(a, "enabled_tools") {
				t.Errorf("«Agente» no limita las herramientas: %v", edit)
			}
		}
	})

	t.Run("antigravity", func(t *testing.T) {
		ask, edit := mustArgs(t, baseTurn("antigravity", ModeAsk)), mustArgs(t, baseTurn("antigravity", ModeEdit))
		if hasArg(ask, "--mode") || !has(edit, "--mode", "accept-edits") {
			t.Errorf("ask=%v\nedit=%v", ask, edit)
		}
	})

	t.Run("opencode", func(t *testing.T) {
		ask, edit := mustArgs(t, baseTurn("opencode", ModeAsk)), mustArgs(t, baseTurn("opencode", ModeEdit))
		// Se nombra SIEMPRE el agente: una sesión retomada conserva el último.
		if !has(ask, "--agent", "plan") || !has(edit, "--agent", "build") {
			t.Errorf("ask=%v\nedit=%v", ask, edit)
		}
		askEnv, editEnv := opencodeConfig(ModeAsk, true), opencodeConfig(ModeEdit, true)
		for _, cfg := range []string{askEnv, editEnv} {
			if !strings.Contains(cfg, `"bash":"deny"`) {
				t.Errorf("la shell se deniega siempre: %s", cfg)
			}
		}
		if !strings.Contains(askEnv, `"scadlab_create_checkpoint":"deny"`) || !strings.Contains(askEnv, `"scadlab_export":"deny"`) {
			t.Errorf("«Solo consulta» niega las herramientas de ScadLab que escriben: %s", askEnv)
		}
		if strings.Contains(editEnv, "scadlab_") {
			t.Errorf("«Agente» no niega nada de ScadLab: %s", editEnv)
		}
		if strings.Contains(opencodeConfig(ModeAsk, false), "scadlab_") {
			t.Error("sin MCP no hay nada que negar")
		}
	})
}

// Los flags MCP y de carpetas llegan al CLI tal como los arma la app, en un
// orden que no deja a un flag variádico comerse el prompt.
func TestMCPAndAddDirFlagsArePassedInOrder(t *testing.T) {
	tr := baseTurn("claude", ModeEdit)
	tr.AddDirs = []string{"--add-dir", "/work/otro"}
	tr.ReadDirs = []string{"/data/chat-attachments/7"}
	args := mustArgs(t, tr)

	if !has(args, mcpClaude...) {
		t.Fatalf("falta --mcp-config: %v", args)
	}
	if !has(args, "--add-dir", "/work/otro") || !has(args, "--add-dir", "/data/chat-attachments/7") {
		t.Errorf("faltan las carpetas: %v", args)
	}
	if args[0] != "-p" || args[1] != "haz una caja" {
		t.Errorf("el prompt va antes de todo flag variádico: %v", args[:3])
	}
	// Después de cada grupo variádico viene otro flag, nunca un valor suelto.
	for i, a := range args {
		if a == "--mcp-config" || a == "--add-dir" || a == "--allowedTools" || a == "--disallowedTools" {
			if i+1 >= len(args) {
				t.Errorf("%s sin valor", a)
			}
		}
	}
	idxMCP := slices.Index(args, "--mcp-config")
	if next := args[idxMCP+2]; !strings.HasPrefix(next, "-") {
		t.Errorf("tras el valor de --mcp-config tiene que venir un flag: %q", next)
	}
	if last := args[len(args)-2]; last != "--allowedTools" {
		t.Errorf("--allowedTools cierra la lista: %v", args[len(args)-3:])
	}

	cx := baseTurn("codex", ModeEdit)
	cx.AddDirs = []string{"--add-dir", "/work/otro"}
	cargs := mustArgs(t, cx)
	if !has(cargs, mcpCodex...) || !has(cargs, "--add-dir", "/work/otro") {
		t.Errorf("codex: %v", cargs)
	}
	// Los flags de exec van antes de --json y antes del subcomando resume.
	if slices.Index(cargs, "-c") > slices.Index(cargs, "--json") || cargs[0] != "exec" {
		t.Errorf("orden en Codex: %v", cargs)
	}
}

func TestMCPOffAddsNoToolRules(t *testing.T) {
	tr := baseTurn("claude", ModeAsk)
	tr.MCP, tr.MCPEnabled = nil, false
	args := mustArgs(t, tr)
	if hasArg(args, "--mcp-config") || hasArg(args, "--allowedTools") {
		t.Errorf("con el servidor MCP apagado no hay reglas de herramientas: %v", args)
	}
	cx := baseTurn("codex", ModeAsk)
	cx.MCP, cx.MCPEnabled = nil, false
	for _, a := range mustArgs(t, cx) {
		if strings.Contains(a, "enabled_tools") {
			t.Error("codex: sin MCP no hay lista de herramientas")
		}
	}
}

func TestResumeAndModelArgs(t *testing.T) {
	const sid = "01a00217-12d1-7492-b8df-b6a2c1f0bc86"
	for cli, want := range map[string][]string{
		"claude":      {"--resume", sid},
		"codex":       {"resume", sid},
		"antigravity": {"--conversation", sid},
		"opencode":    {"--session", sid},
	} {
		tr := baseTurn(cli, ModeAsk)
		tr.Session, tr.Model = sid, "un-modelo"
		args := mustArgs(t, tr)
		if !has(args, want...) {
			t.Errorf("%s: falta %v en %v", cli, want, args)
		}
		if !has(args, "--model", "un-modelo") {
			t.Errorf("%s: falta --model: %v", cli, args)
		}
		// Sin sesión ni modelo no hay nada de eso.
		fresh := mustArgs(t, baseTurn(cli, ModeAsk))
		if has(fresh, want[0]) || hasArg(fresh, "--model") {
			t.Errorf("%s: conversación nueva con restos: %v", cli, fresh)
		}
	}
	// En Codex, `resume` es un subcomando: va después de los flags de exec.
	tr := baseTurn("codex", ModeEdit)
	tr.Session = sid
	args := mustArgs(t, tr)
	if slices.Index(args, "resume") < slices.Index(args, "--sandbox") {
		t.Errorf("--sandbox no existe en `exec resume`: tiene que ir antes: %v", args)
	}
}

func TestImagesAreNamedOrAttachedPerCLI(t *testing.T) {
	i18n.SetLang("es")
	t.Cleanup(func() { i18n.SetLang("en") })
	imgs := []string{"/data/chat-attachments/7/a.png", "/data/chat-attachments/7/b.png"}
	for _, cli := range []string{"claude", "antigravity", "opencode"} {
		tr := baseTurn(cli, ModeAsk)
		tr.Images = imgs
		args := mustArgs(t, tr)
		joined := strings.Join(args, "\n")
		if !strings.Contains(joined, imgs[0]) || !strings.Contains(joined, imgs[1]) || !strings.Contains(joined, "Imágenes adjuntas") {
			t.Errorf("%s: las imágenes van nombradas en el prompt: %v", cli, args)
		}
	}
	tr := baseTurn("codex", ModeAsk)
	tr.Images = imgs
	args := mustArgs(t, tr)
	if !has(args, "-i", imgs[0]) || !has(args, "-i", imgs[1]) {
		t.Errorf("codex recibe las imágenes con -i: %v", args)
	}
	// `-i` es variádico: el `--` evita que se coma el prompt.
	if args[len(args)-2] != "--" || args[len(args)-1] != "haz una caja" {
		t.Errorf("el prompt cierra el argv tras --: %v", args)
	}
	if strings.Contains(args[len(args)-1], "Imágenes adjuntas") {
		t.Error("codex no necesita las rutas en el prompt")
	}
}

func TestPromptViaStdinLeavesNoPromptInArgv(t *testing.T) {
	for _, cli := range []string{"claude", "codex", "opencode"} {
		tr := baseTurn(cli, ModeAsk)
		tr.StdinPrompt = true
		for _, a := range mustArgs(t, tr) {
			if strings.Contains(a, "haz una caja") {
				t.Errorf("%s: el prompt no puede ir por argv con un shim: %q", cli, a)
			}
		}
	}
	// Antigravity no lee stdin: su prompt va por argv pero limpio.
	if got := shimSafe("a&b |c\n\"d\" %PATH%"); strings.ContainsAny(got, "&|\"%\n") {
		t.Errorf("shimSafe: %q", got)
	}
}

// Ninguna combinación de CLI y modo pasa una bandera que saltee los permisos.
func TestNoDangerousFlagsEver(t *testing.T) {
	banned := []string{"--dangerously-skip-permissions", "--allow-dangerously-skip-permissions", "bypassPermissions", "--auto",
		"--dangerously-bypass-approvals-and-sandbox", "danger-full-access", "--yolo", "--dangerously-bypass-hook-trust"}
	for _, cli := range []string{"claude", "codex", "antigravity", "opencode"} {
		for _, mode := range []Mode{ModeAsk, ModeEdit} {
			tr := baseTurn(cli, mode)
			tr.Session, tr.Model, tr.Images = "abc-123", "m", []string{"/x.png"}
			for _, a := range mustArgs(t, tr) {
				for _, b := range banned {
					if a == b {
						t.Errorf("%s/%s pasa %s", cli, mode, b)
					}
				}
			}
		}
	}
}

// El modelo y el id de sesión acaban como argumentos del CLI: no pueden leerse
// como banderas ni llevar metacaracteres.
func TestModelAndSessionAreValidated(t *testing.T) {
	for _, m := range []string{"--auto", "-m", "a b", "x;rm", "$(x)", "a\nb", strings.Repeat("a", 300)} {
		if ValidModel(m) {
			t.Errorf("modelo %q no debería valer", m)
		}
		tr := baseTurn("claude", ModeAsk)
		tr.Model = m
		if _, err := BuildArgs(tr); err == nil {
			t.Errorf("BuildArgs aceptó el modelo %q", m)
		}
	}
	for _, m := range []string{"", "opus", "claude-opus-5[1m]", "anthropic/claude-sonnet-5#high", "gemini-4-argon-high", "gpt-5.2-codex"} {
		if !ValidModel(m) {
			t.Errorf("modelo %q debería valer", m)
		}
	}
	for _, s := range []string{"--auto", "a b", "x;y", "../x"} {
		tr := baseTurn("codex", ModeAsk)
		tr.Session = s
		if _, err := BuildArgs(tr); err == nil {
			t.Errorf("BuildArgs aceptó la sesión %q", s)
		}
	}
}

func TestUnknownCLIAndMissingExe(t *testing.T) {
	if _, err := BuildArgs(Turn{CLI: "otro", Prompt: "x"}); err == nil {
		t.Error("un CLI fuera del catálogo no tiene argv")
	}
	if _, err := Command(Turn{CLI: "claude", Prompt: "x"}); err == nil {
		t.Error("sin ejecutable no hay comando")
	}
	if ModeFor(false) != ModeAsk || ModeFor(true) != ModeEdit {
		t.Error("el interruptor «Agente» elige el modo")
	}
}

func TestReplyLanguageUsesTu(t *testing.T) {
	if es := replyLanguage("es"); !strings.Contains(es, "Responde") || strings.Contains(es, "Respondé") {
		t.Errorf("%q", es)
	}
	args := mustArgs(t, baseTurn("claude", ModeAsk))
	if !strings.Contains(valueOf(args, "--append-system-prompt"), "español") {
		t.Errorf("Claude Code contesta en el idioma de la interfaz: %v", args)
	}
}
