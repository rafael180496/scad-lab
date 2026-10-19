package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"scadlab/backend/chat"
	"scadlab/backend/clichat"
	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
	"scadlab/backend/mcpconf"
	"scadlab/backend/store"
)

// CLIs como proveedores del chat (S12.b): Claude Code, Codex, Antigravity y
// OpenCode se usan como un proveedor más del chat de diseño. Corren sin
// interacción, con streaming, en la carpeta del workspace y con el MCP de
// ScadLab conectado; sus herramientas llegan como pasos de Hermes (chat:step).
// El motor es backend/clichat (portado de mini-tools); aquí se conecta con el
// chat: la lista de proveedores, el turno, los pasos y la persistencia.
//
// Los CLIs no se configuran: se autodetectan (PATH, rutas típicas y el PATH de la
// shell de login) y, si están instalados, aparecen solos. No crean filas en
// model_servers ni piden nada en Configuración.

// ChatProvider es un proveedor del selector del chat: un CLI instalado o un
// servidor de modelos habilitado.
type ChatProvider struct {
	// ID es `cli:<id>` (`cli:claude`, `cli:codex`, `cli:antigravity`,
	// `cli:opencode`) o el id de model_servers.
	ID string `json:"id"`
	// Kind es cli | ollama | openai | anthropic.
	Kind string `json:"kind"`
	// Name es «Claude Code» o el nombre que el usuario le puso al servidor.
	Name string `json:"name"`
	// Vendor es «Anthropic», «OpenAI», «Google», «SST»; vacío en un servidor.
	Vendor string `json:"vendor"`
	// Installed: el CLI se encontró en este equipo (en un servidor, siempre true).
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
	// Remote: el servidor está fuera de loopback (siempre false en un CLI).
	Remote  bool `json:"remote"`
	Enabled bool `json:"enabled"`
}

// DetectedCLI es un CLI del catálogo con su estado, instalado o no: lo que
// Configuración muestra, de solo lectura. ID es el id corto (`claude`), no el del
// proveedor del chat.
type DetectedCLI struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Vendor    string `json:"vendor"`
	Installed bool   `json:"installed"`
	Path      string `json:"path"`
	Version   string `json:"version"`
}

// ProviderHealthInfo es el estado de un proveedor: la píldora del selector. Un
// proveedor caído NO es un error de la llamada: es la respuesta que se vino a
// buscar, y viaja con ok=false y el motivo en error.
type ProviderHealthInfo struct {
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	Error     string `json:"error"`
}

// ModelCatalog son los modelos de un proveedor y la explicación de la opción
// «Por defecto» (que pone la interfaz: no es parte de Models).
type ModelCatalog struct {
	// DefaultHint dice qué modelo se usa si no se elige ninguno: «El que tenga
	// configurado el CLI» o «El primero que devuelva el servidor» (o el modelo
	// por defecto del servidor, si lo tiene).
	DefaultHint string      `json:"defaultHint"`
	Models      []ModelInfo `json:"models"`
}

// ListChatProviders devuelve los proveedores del selector del chat: primero los
// CLIs instalados (como en mini-tools) y después los servidores habilitados. Es
// liviano: no corre `--version`, y la detección de CLIs se recuerda ~30 s.
//
// Un CLI que no está instalado no aparece (mini-tools hace lo mismo en su
// selector del chat: lista solo los `available`); ListDetectedCLIs sí los trae,
// para que Configuración diga «no instalado».
func (a *App) ListChatProviders() ([]ChatProvider, error) { return a.chatProviders(false) }

// RefreshChatProviders es ListChatProviders volviendo a detectar los CLIs: lo
// que pide el selector al abrirse, para que un CLI recién instalado aparezca
// sin reiniciar la app.
func (a *App) RefreshChatProviders() ([]ChatProvider, error) { return a.chatProviders(true) }

func (a *App) chatProviders(refresh bool) ([]ChatProvider, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	out := []ChatProvider{}
	for _, c := range clichat.Default.List(refresh) {
		if !c.Installed {
			continue
		}
		out = append(out, ChatProvider{
			ID: clichat.ProviderID(c.ID), Kind: "cli", Name: c.Name, Vendor: c.Vendor,
			Installed: true, Path: c.Path, Enabled: true,
		})
	}
	servers, err := a.store.ListModelServers()
	if err != nil {
		return nil, err
	}
	for _, s := range servers {
		if !s.Enabled {
			continue
		}
		out = append(out, ChatProvider{
			ID: s.ID, Kind: s.Kind, Name: s.Name, Installed: true,
			Remote: isRemoteURL(s.BaseURL), Enabled: true,
		})
	}
	return out, nil
}

// isRemoteURL dice si la URL apunta fuera de esta máquina. Un servidor local
// (Ollama en localhost) no manda nada a la red; uno remoto sí: la interfaz lo
// marca para que se sepa adónde van el código y las imágenes.
func isRemoteURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback()
	}
	return true
}

// ListDetectedCLIs devuelve los CLIs del catálogo con su ruta y su versión, para
// Configuración (solo lectura). La versión sale de `<cli> --version`, pedida en
// paralelo y recordada por ruta: arrancar un CLI de node tarda ~1 s.
func (a *App) ListDetectedCLIs() ([]DetectedCLI, error) {
	ctx, cancel := context.WithTimeout(a.appCtx(), 10*time.Second)
	defer cancel()
	list := clichat.Default.List(false)
	versions := clichat.Default.Versions(ctx, list)
	out := make([]DetectedCLI, 0, len(list))
	for _, c := range list {
		out = append(out, DetectedCLI{ID: c.ID, Name: c.Name, Vendor: c.Vendor, Installed: c.Installed, Path: c.Path, Version: versions[c.ID]})
	}
	return out, nil
}

// ProviderHealth dice si el proveedor responde. Un servidor: el mismo sondeo
// barato de antes de cada turno (Ollama `/api/version`; los demás, la lista de
// modelos) con 4 s de plazo, recordado 30 s; un fallo no se recuerda. Un CLI: ok
// si está instalado, sin latencia (arrancarlo para medirla sería lanzar una
// sesión).
func (a *App) ProviderHealth(id string) (ProviderHealthInfo, error) {
	if cliID, ok := clichat.ParseProviderID(id); ok {
		d, found := clichat.Default.Get(cliID, false)
		if found && d.Installed {
			return ProviderHealthInfo{OK: true}, nil
		}
		name := cliID
		if found {
			name = d.Name
		}
		return ProviderHealthInfo{Error: i18n.T(msgCLINotInstalled, name)}, nil
	}
	if clichat.IsProviderID(id) {
		return ProviderHealthInfo{}, i18n.Errorf(i18n.Msg{ES: "el CLI %q no es compatible con el chat", EN: "the CLI %q is not supported by the chat"}, id)
	}
	if err := a.ready(); err != nil {
		return ProviderHealthInfo{}, err
	}
	s, err := a.store.GetModelServer(id)
	if err != nil {
		return ProviderHealthInfo{}, err
	}
	if !s.Enabled {
		return ProviderHealthInfo{Error: i18n.T(i18n.Msg{ES: "el servidor %s está deshabilitado (Configuración → Proveedores de IA)", EN: "the server %s is disabled (Settings → AI providers)"}, s.Name)}, nil
	}
	svc := a.serverSvc()
	gen := svc.health.begin(s.ID)
	if svc.health.fresh(s.ID) {
		return ProviderHealthInfo{OK: true, LatencyMs: svc.health.latency(s.ID)}, nil
	}
	client, err := a.serverClient(s)
	if err != nil {
		// Una clave bloqueada o ausente es un motivo, no un fallo de la llamada:
		// la píldora no debe abrir un diálogo de contraseña cada vez que se mira.
		return ProviderHealthInfo{Error: err.Error()}, nil
	}
	ctx, cancel := context.WithTimeout(a.appCtx(), modelHealthTimeout)
	defer cancel()
	start := time.Now()
	if err := runProbe(ctx, s.Kind, client); err != nil {
		return ProviderHealthInfo{Error: serverUnreachableError(s, err).Error()}, nil
	}
	ms := time.Since(start).Milliseconds()
	svc.health.put(s.ID, gen)
	svc.health.putLatency(s.ID, gen, ms)
	return ProviderHealthInfo{OK: true, LatencyMs: ms}, nil
}

// ChatModelCatalog devuelve los modelos de un proveedor para el selector. Un
// servidor: los de ListServerModels (con visión, herramientas y precios). Un CLI:
// los que el propio CLI lista (alias de Claude Code, la caché de Codex, `agy
// models`, el catálogo de OpenCode), con su nombre y su descripción. refresh se
// salta las cachés.
func (a *App) ChatModelCatalog(id string, refresh bool) (ModelCatalog, error) {
	if cliID, ok := clichat.ParseProviderID(id); ok {
		models := clichat.Models(cliID, refresh)
		out := make([]ModelInfo, 0, len(models))
		for _, m := range models {
			// Un CLI siempre tiene herramientas; si ve imágenes depende del
			// modelo y ningún CLI lo informa, así que no se afirma.
			out = append(out, ModelInfo{ID: m.ID, Label: m.Label, Description: m.Description, Tools: true, ToolsKnown: true})
		}
		return ModelCatalog{
			DefaultHint: i18n.T(i18n.Msg{ES: "El que tenga configurado el CLI", EN: "Whatever the CLI has configured"}),
			Models:      out,
		}, nil
	}
	if clichat.IsProviderID(id) {
		return ModelCatalog{}, i18n.Errorf(i18n.Msg{ES: "el CLI %q no es compatible con el chat", EN: "the CLI %q is not supported by the chat"}, id)
	}
	if err := a.ready(); err != nil {
		return ModelCatalog{}, err
	}
	srv, err := a.chatServer(id)
	if err != nil {
		return ModelCatalog{}, err
	}
	models, err := a.ListServerModels(id, refresh)
	if err != nil {
		return ModelCatalog{}, err
	}
	hint := i18n.T(i18n.Msg{ES: "El primero que devuelva el servidor", EN: "The first one the server returns"})
	if srv.DefaultModel != "" {
		hint = i18n.T(i18n.Msg{ES: "El predeterminado del servidor (%s)", EN: "The server's default (%s)"}, srv.DefaultModel)
	}
	return ModelCatalog{DefaultHint: hint, Models: models}, nil
}

var msgCLINotInstalled = i18n.Msg{
	ES: "%s no está instalado en este equipo",
	EN: "%s is not installed on this computer",
}

// resolveCLI valida un proveedor `cli:<id>` y un modelo para empezar o seguir un
// chat: el CLI es del catálogo, está instalado (se vuelve a detectar si no, por
// si se instaló hace un rato) y el modelo es un id válido. Un modelo vacío vale.
func (a *App) resolveCLI(providerID, model string) (clichat.Detected, error) {
	cliID, ok := clichat.ParseProviderID(providerID)
	if !ok {
		return clichat.Detected{}, i18n.Errorf(i18n.Msg{ES: "el CLI %q no es compatible con el chat", EN: "the CLI %q is not supported by the chat"}, providerID)
	}
	d, _ := clichat.Default.Get(cliID, false)
	if !d.Installed {
		d, _ = clichat.Default.Get(cliID, true)
	}
	if !d.Installed {
		return d, i18n.Errorf(i18n.Msg{
			ES: "%s no está instalado en este equipo; instálalo (o elige otro proveedor) y vuelve a intentarlo",
			EN: "%s is not installed on this computer; install it (or pick another provider) and try again",
		}, d.Name)
	}
	if !clichat.ValidModel(model) {
		return d, i18n.Errorf(i18n.Msg{ES: "el modelo %q no es un id válido", EN: "the model %q is not a valid id"}, model)
	}
	return d, nil
}

// ── El turno ───────────────────────────────────────────────────────────────

// cliJob es lo propio de un turno con un CLI.
type cliJob struct {
	det clichat.Detected
	// mode sale del interruptor «Agente»: false = «Solo consulta».
	mode clichat.Mode
	// userText es el mensaje del usuario con el informe de las referencias 3D;
	// images, las rutas de las imágenes adjuntas.
	userText string
	images   []string
	// stored es el historial anterior a este mensaje: solo se reenvía si el CLI no
	// tiene sesión que continuar (chat nuevo, o cambió de CLI a mitad).
	stored  []store.ChatMessage
	session string
	mcpOn   bool
	mcp     []string
	addDirs []string
	// mcpNote es por qué ScadLab no pudo dejar registrado su servidor MCP en el
	// CLI (hoy solo Antigravity, que lo lee de su configuración global). Vacío =
	// todo bien. Cambia el prompt y explica el turno si no deja checkpoints.
	mcpNote string
}

// cliHistoryChars acota lo que se resume de la conversación anterior cuando el
// CLI no tiene sesión que continuar: un CLI tiene su propia memoria y esto solo
// le da el hilo de un chat que empezó con otro proveedor.
const cliHistoryChars = 12_000

// cliSourceBytes acota el código del checkpoint abierto que viaja en el prompt: el
// CLI puede leer el archivo entero con sus herramientas si lo necesita.
const cliSourceBytes = 16 * 1024

// sendCLIMessage es SendChatMessage para un chat con un CLI. Mismo contrato: todo
// lo que puede fallar antes de empezar (el CLI no está instalado, un adjunto
// inválido, el chat ocupado) falla ANTES de guardar nada.
func (a *App) sendCLIMessage(c store.Chat, text, currentFile string, attachmentIDs []string, opts SendOptions) (store.ChatMessage, error) {
	det, err := a.resolveCLI(c.ProviderID, c.Model)
	if err != nil {
		return store.ChatMessage{}, err
	}
	w, err := a.store.GetWorkspace(c.WorkspaceID)
	if err != nil {
		return store.ChatMessage{}, err
	}
	pending, err := a.pendingAttachments(attachmentIDs, w.ID)
	if err != nil {
		return store.ChatMessage{}, err
	}
	settings, err := a.store.GetSettings()
	if err != nil {
		return store.ChatMessage{}, err
	}
	// Los CLIs ven imágenes (o lo dirán ellos): no hay chequeo previo de visión.
	ot, err := a.openChatTurn(c, text, pending, attachmentIDs, true)
	if err != nil {
		return store.ChatMessage{}, err
	}

	// Los flags del servidor MCP y de las carpetas de los otros proyectos son los
	// de la terminal (app_terminal.go, app_mcp.go): una sola definición de la
	// línea de comandos.
	agent := opts.agentFor(c)
	job := chatJob{
		turn: ot.turn, chat: c, workspace: w, currentFile: currentFile, agent: agent,
		cli: &cliJob{
			det: det, mode: clichat.ModeFor(agent),
			userText: ot.prepared.Text, images: ot.prepared.Images, stored: ot.stored, session: c.CLISession,
			mcpOn: settings.MCPEnabled, mcp: a.mcpLaunchArgs(det.ID, w.Path), addDirs: a.addDirArgs(det.ID, w.ID),
		},
	}
	a.chatSvc().beginLive(c.ID)
	go a.runChatTurn(job)
	return ot.user, nil
}

// cliPrompt arma el prompt del turno: lo que el CLI no lee solo. Las reglas del
// proyecto ya van en CLAUDE.md / AGENTS.md del workspace y el CLI las carga; acá
// va el contexto de ScadLab, el checkpoint que el usuario tiene abierto, el hilo de
// la conversación si el CLI no tiene sesión que continuar, y el mensaje. Siempre
// empieza con texto propio (nunca con un guion), así que no se lee como bandera.
func (a *App) cliPrompt(j chatJob, withHistory bool) string {
	var b strings.Builder
	b.WriteString(i18n.T(i18n.Msg{
		ES: "[ScadLab] Diseñas un modelo 3D en OpenSCAD en el proyecto «%s» (esta carpeta). Las reglas del proyecto están en CLAUDE.md y AGENTS.md. Usa las herramientas del servidor «scadlab» (render, preview, measure, lint…) para comprobar tu trabajo.",
		EN: "[ScadLab] You are designing a 3D model in OpenSCAD in the project “%s” (this folder). The project rules are in CLAUDE.md and AGENTS.md. Use the tools of the “scadlab” server (render, preview, measure, lint…) to check your work.",
	}, j.workspace.Name))
	pctx := chatPromptContext(j.workspace, j.currentFile)
	switch j.cli.mode {
	case clichat.ModeAsk:
		// Solo consulta: una respuesta que se pueda guardar con un clic.
		b.WriteString(" " + i18n.T(msgCLIPromptAsk))
	default:
		// Agente: el diseño lo crea el agente, de punta a punta.
		b.WriteString(" " + i18n.T(msgCLIPromptAgent))
		if len(pctx.Checkpoints) == 0 {
			b.WriteString(" " + i18n.T(msgCLIPromptEmpty))
		}
		if j.cli.mcpNote != "" {
			b.WriteString(" " + i18n.T(msgCLIPromptNoMCP))
		}
	}
	if pctx.CurrentFile != "" {
		b.WriteString("\n\n" + i18n.T(i18n.Msg{ES: "Checkpoint abierto en el editor: %s", EN: "Checkpoint open in the editor: %s"}, pctx.CurrentFile))
		src := pctx.CurrentSource
		cut := false
		if len(src) > cliSourceBytes {
			src, cut = strings.ToValidUTF8(src[:cliSourceBytes], ""), true
		}
		b.WriteString("\n```openscad\n" + strings.TrimRight(src, "\n") + "\n```")
		if cut {
			b.WriteString("\n" + i18n.T(i18n.Msg{ES: "(código recortado; léelo entero con read_checkpoint)", EN: "(code truncated; read it in full with read_checkpoint)"}))
		}
	}
	if withHistory {
		if hist := cliHistory(j.cli.stored); hist != "" {
			b.WriteString("\n\n" + i18n.T(i18n.Msg{ES: "[Conversación anterior de este chat]", EN: "[Earlier conversation in this chat]"}) + "\n" + hist)
		}
	}
	b.WriteString("\n\n" + j.cli.userText)
	return b.String()
}

// cliHistory resume la conversación anterior como texto. Reutiliza SeedHistory
// (adjuntos como una línea con sus nombres, mensajes seguidos del mismo rol
// fundidos, cola que entra en el presupuesto).
func cliHistory(stored []store.ChatMessage) string {
	rows := make([]hermes.StoredMessage, len(stored))
	for i, m := range stored {
		rows[i] = hermes.StoredMessage{Role: m.Role, Content: m.Content, Parts: m.Parts}
	}
	var b strings.Builder
	for _, m := range hermes.SeedHistory(rows, cliHistoryChars) {
		who := i18n.T(i18n.Msg{ES: "Usuario", EN: "User"})
		if m.Role == "assistant" {
			who = i18n.T(i18n.Msg{ES: "Asistente", EN: "Assistant"})
		}
		b.WriteString(who + ": " + strings.TrimSpace(m.Content) + "\n\n")
	}
	return strings.TrimSpace(b.String())
}

// Las instrucciones de modo del prompt de un CLI (S12.c). Van en el prompt y no
// solo en CLAUDE.md/AGENTS.md porque el prompt del turno es lo último que el
// modelo lee antes de actuar, y los dos modos piden cosas opuestas.
var (
	msgCLIPromptAsk = i18n.Msg{
		ES: "El modo de este turno es «Solo consulta»: puedes leer, razonar y comprobar (render, preview, measure, lint), pero no crees checkpoints ni exportes. Si hay que diseñar o cambiar algo, propón UN solo bloque ```openscad con el código COMPLETO y la cabecera `// @checkpoint parent=… desc=\"…\"`, listo para guardar: ScadLab le ofrece al usuario guardarlo y renderizarlo con un clic.",
		EN: "This turn is in “Query only” mode: you can read, reason and check (render, preview, measure, lint), but do not create checkpoints or export. If something has to be designed or changed, propose ONE ```openscad block with the COMPLETE code and the header `// @checkpoint parent=… desc=\"…\"`, ready to save: ScadLab offers the user to save and render it with one click.",
	}
	msgCLIPromptAgent = i18n.Msg{
		ES: "El modo de este turno es «Agente»: el diseño lo creas TÚ, no el usuario, y el turno no termina hasta que exista un checkpoint que renderice. Flujo obligatorio con las herramientas del servidor «scadlab»: 1) `create_checkpoint` con el código completo; 2) `render` para validarlo; 3) `preview` para mirarlo; 4) si falla o no se ve bien, corrige en un checkpoint NUEVO (`create_checkpoint` con `parent` = el que falló; nunca edites uno existente) y vuelve al paso 2. No pegues el código en la respuesta en lugar de crear el checkpoint, no pidas permiso para crearlo y no uses la shell.",
		EN: "This turn is in “Agent” mode: YOU create the design, not the user, and the turn does not end until a checkpoint that renders exists. Mandatory flow with the tools of the “scadlab” server: 1) `create_checkpoint` with the complete code; 2) `render` to validate it; 3) `preview` to look at it; 4) if it fails or does not look right, fix it in a NEW checkpoint (`create_checkpoint` with `parent` = the one that failed; never edit an existing one) and go back to step 2. Do not paste the code in the answer instead of creating the checkpoint, do not ask permission to create it, and do not use the shell.",
	}
	msgCLIPromptEmpty = i18n.Msg{
		ES: "El proyecto todavía no tiene checkpoints: crea el primero ya, sin preguntar nada (sin `parent`).",
		EN: "The project has no checkpoints yet: create the first one right away, without asking anything (no `parent`).",
	}
	msgCLIPromptNoMCP = i18n.Msg{
		ES: "Atención: el servidor «scadlab» NO está conectado en este turno, así que no puedes crear checkpoints. Entrega el diseño en UN bloque ```openscad con el código completo y la cabecera `// @checkpoint`: ScadLab lo guardará y lo renderizará por ti.",
		EN: "Warning: the “scadlab” server is NOT connected in this turn, so you cannot create checkpoints. Deliver the design in ONE ```openscad block with the complete code and the `// @checkpoint` header: ScadLab will save and render it for you.",
	}
)

// cliTurn arma el Turn de clichat para el prompt y la sesión dados.
func (a *App) cliTurn(j chatJob, session, prompt string) clichat.Turn {
	cwd := absDir(j.workspace.Path)
	var readDirs []string
	seen := map[string]bool{}
	for _, img := range j.cli.images {
		if d := filepath.Dir(img); !seen[d] {
			seen[d] = true
			readDirs = append(readDirs, d)
		}
	}
	return clichat.Turn{
		CLI: j.cli.det.ID, Exe: j.cli.det.Path, Cwd: cwd, Env: clichat.Env(j.cli.det.Path),
		Prompt: prompt, Mode: j.cli.mode, Model: j.chat.Model, Session: session,
		Images: j.cli.images, MCP: j.cli.mcp, AddDirs: j.cli.addDirs, ReadDirs: readDirs,
		MCPEnabled: j.cli.mcpOn, Lang: i18n.Lang(),
	}
}

// runCLITurn corre la respuesta de un CLI, la guarda y avisa a la pantalla. Un
// cancelado conserva lo escrito y los pasos; un fallo con texto o pasos previos
// también.
func (a *App) runCLITurn(j chatJob) {
	c, svc := j.chat, a.chatSvc()
	// Los .scad que ya existen: al terminar, uno nuevo que no vino de una
	// herramienta (el CLI lo escribió él) también cuenta como un checkpoint creado.
	before := workspaceScadSet(j.workspace.Path)
	j.cli.mcpNote = a.prepareCLIMCP(j)
	stream := j.turn.Stream(func(seq int, text string) {
		a.emit(evChatDelta, ChatDeltaEvent{ChatID: c.ID, Text: text, Seq: seq})
	})
	rec := newCLIRecorder(a, c, stream)

	run := func(session string) (clichat.Result, error) {
		return clichat.Run(j.turn.Context(), a.cliTurn(j, session, a.cliPrompt(j, session == "")), rec.handle)
	}
	res, err := run(j.cli.session)
	// Una sesión que el CLI ya no tiene (la borró, o se reinstaló) falla antes de
	// decir nada: se olvida y se reintenta una vez como conversación nueva, con el
	// hilo anterior en el prompt. Si ya contestó algo, no se repite.
	if err != nil && j.cli.session != "" && !res.Cancelled && !rec.hadOutput() {
		log.Printf("scadlab: chat %d: la sesión del CLI no sirvió (%v); se reintenta sin ella", c.ID, err)
		if serr := a.store.SetChatCLISession(c.ID, ""); serr != nil {
			log.Printf("scadlab: chat %d: olvidar la sesión del CLI: %v", c.ID, serr)
		}
		rec.reset()
		res, err = run("")
	}
	out := rec.finish(res, err)
	if err == nil && !out.Cancelled {
		// La red de seguridad (S12.c) y, si el turno no dejó nada, la explicación.
		made := a.cliSafetyNet(j.turn.Context(), j, before, &out)
		out.Text = a.cliTurnText(j, out, made)
	}
	svc.setUsage(c.ID, out.Usage)
	a.concludeChatTurn(c, out, err, cliErrorText(j.cli.det, err))
}

// prepareCLIMCP deja listo el servidor MCP de ScadLab para el CLI del turno y
// devuelve, si no quedó disponible, el motivo (vacío = todo bien). Antigravity
// lo lee de su configuración global: se registra ahí si falta (ver
// EnsureAntigravityMCP). Con el servidor apagado en Configuración → IA ningún CLI
// lo tiene.
func (a *App) prepareCLIMCP(j chatJob) string {
	if !j.cli.mcpOn {
		return i18n.T(msgCLIMCPOff)
	}
	if j.cli.det.ID != "antigravity" {
		return ""
	}
	st, err := a.EnsureAntigravityMCP()
	switch {
	case err != nil:
		return err.Error()
	case st.State == mcpconf.AgyDisabled:
		return i18n.T(msgCLIAgyDisabled)
	}
	return ""
}

var (
	msgCLIMCPOff = i18n.Msg{
		ES: "El servidor MCP de ScadLab está apagado (Configuración → IA): sin él, el CLI no puede crear ni renderizar checkpoints desde el chat.",
		EN: "ScadLab's MCP server is turned off (Settings → AI): without it, the CLI cannot create or render checkpoints from the chat.",
	}
	msgCLIAgyDisabled = i18n.Msg{
		ES: "Antigravity tiene el servidor «scadlab» desactivado (`agy mcp enable scadlab`): sin él no puede crear checkpoints desde el chat.",
		EN: "Antigravity has the “scadlab” server disabled (`agy mcp enable scadlab`): without it, it cannot create checkpoints from the chat.",
	}
	msgCLIEmptyTurn = i18n.Msg{
		ES: "%s terminó el turno sin responder nada.",
		EN: "%s finished the turn without saying anything.",
	}
	msgCLIEmptyToolFailed = i18n.Msg{
		ES: "Falló la herramienta «%s»: %s",
		EN: "The tool “%s” failed: %s",
	}
	msgCLIEmptyHint = i18n.Msg{
		ES: "No se creó ningún checkpoint. Vuelve a pedirlo, o elige otro proveedor en la barra del chat.",
		EN: "No checkpoint was created. Ask again, or pick another provider in the chat bar.",
	}
)

// cliTurnText devuelve el texto que se guarda del turno: el del CLI, y cuando no
// dejó nada que ver, una explicación en vez de una respuesta vacía. made dice que
// el turno dejó un checkpoint (por una herramienta, por la red de seguridad o
// porque el CLI escribió un .scad).
func (a *App) cliTurnText(j chatJob, res hermes.Result, made bool) string {
	text := res.Text
	if made {
		return text
	}
	if strings.TrimSpace(text) == "" {
		var b strings.Builder
		b.WriteString("_" + i18n.T(msgCLIEmptyTurn, j.cli.det.Name) + "_")
		shown := 0
		for _, st := range res.Steps {
			if st.Status != hermes.StepError || shown >= 3 {
				continue
			}
			shown++
			b.WriteString("\n\n" + i18n.T(msgCLIEmptyToolFailed, st.Tool, clipText(strings.TrimSpace(st.Text), 300)))
		}
		if j.agent {
			if j.cli.mcpNote != "" {
				b.WriteString("\n\n" + j.cli.mcpNote)
			} else {
				b.WriteString("\n\n" + i18n.T(msgCLIEmptyHint))
			}
		}
		return b.String()
	}
	if j.agent && j.cli.mcpNote != "" {
		return strings.TrimRight(text, "\n") + "\n\n_⚠ " + j.cli.mcpNote + "_"
	}
	return text
}

// cliErrorText antepone el nombre del CLI al fallo y, si parece de autenticación,
// dice qué hacer: cada CLI maneja su propio login y ScadLab no lo replica.
func cliErrorText(d clichat.Detected, err error) string {
	if err == nil {
		return ""
	}
	msg := d.Name + ": " + err.Error()
	low := strings.ToLower(err.Error())
	for _, k := range []string{"log in", "login", "logged in", "unauthorized", "401", "not authenticated", "api key"} {
		if strings.Contains(low, k) {
			msg += i18n.T(i18n.Msg{
				ES: " (cada CLI maneja su propia sesión: abre la terminal de ScadLab, ejecuta %s e inicia sesión)",
				EN: " (each CLI manages its own login: open ScadLab's terminal, run %s and sign in)",
			}, d.Bin)
			break
		}
	}
	return msg
}

// ── Del stream del CLI a los pasos de Hermes ───────────────────────────────

const (
	cliStepTextRunes = 4000
	cliStepArgRunes  = 6000
)

// cliRecorder traduce los eventos de un CLI a lo que ya entiende el chat: texto
// en trozos (chat:delta), pasos (chat:step) y uso (chat:usage). Es el equivalente,
// para un CLI, de los Hooks del bucle de Hermes. Lo llama una sola goroutine.
type cliRecorder struct {
	a      *App
	chat   store.Chat
	stream *chat.TextStream

	text    strings.Builder
	textLen int // unidades UTF-16, el `length` de JS (Step.TextAt)
	steps   []*hermes.Step
	ids     []string // el id del CLI de cada paso, en el mismo orden
	byID    map[string]*hermes.Step
	started map[string]time.Time
	auto    int
	session string
	usage   hermes.Usage
	sent    bool // ya se emitió chat:usage
}

func newCLIRecorder(a *App, c store.Chat, stream *chat.TextStream) *cliRecorder {
	r := &cliRecorder{a: a, chat: c, stream: stream}
	r.reset()
	r.session = c.CLISession
	return r
}

// reset descarta lo acumulado (el reintento sin sesión empieza de cero).
func (r *cliRecorder) reset() {
	r.text.Reset()
	r.textLen = 0
	r.steps, r.ids = nil, nil
	r.byID = map[string]*hermes.Step{}
	r.started = map[string]time.Time{}
	r.usage, r.sent = hermes.Usage{}, false
}

func (r *cliRecorder) hadOutput() bool { return r.textLen > 0 || len(r.steps) > 0 }

func (r *cliRecorder) emitStep(st *hermes.Step) {
	r.a.chatSvc().upsertStep(r.chat.ID, *st)
	r.a.emit(evChatStep, ChatStepEvent{ChatID: r.chat.ID, WorkspaceID: r.chat.WorkspaceID, Step: *st})
	r.a.refreshTreeAfterStep(r.chat.WorkspaceID, *st)
}

func (r *cliRecorder) handle(ev clichat.Event) {
	// La sesión se guarda apenas aparece, no al final: si el turno se corta a la
	// mitad, el siguiente igual continúa la conversación.
	if ev.ConversationID != "" && ev.ConversationID != r.session {
		r.session = ev.ConversationID
		if err := r.a.store.SetChatCLISession(r.chat.ID, ev.ConversationID); err != nil && !errors.Is(err, store.ErrNotFound) {
			log.Printf("scadlab: chat %d: guardar la sesión del CLI: %v", r.chat.ID, err)
		}
	}
	switch ev.Kind {
	case clichat.KindText:
		r.text.WriteString(ev.Text)
		r.textLen += utf16Len(ev.Text)
		r.stream.Add(ev.Text)

	case clichat.KindTool:
		t := ev.Tool
		if t == nil {
			return
		}
		id := t.ID
		if id == "" {
			r.auto++
			id = "auto-" + strconv.Itoa(r.auto)
		}
		if _, dup := r.byID[id]; dup {
			return // el CLI repite el arranque de un paso (Antigravity: ACTIVE)
		}
		args := t.Args
		if args == nil {
			args = map[string]any{}
			if t.Input != "" {
				args["input"] = t.Input
			}
		}
		st := &hermes.Step{
			ID: "st-" + randHex(6), Index: len(r.steps) + 1, Tool: t.Name, Args: capStepArgs(args),
			Status: hermes.StepRunning, Images: []string{}, TextAt: r.textLen,
		}
		r.steps = append(r.steps, st)
		r.ids = append(r.ids, id)
		r.byID[id] = st
		r.started[id] = time.Now()
		// El texto pendiente sale antes de anunciar el paso: que no llegue después.
		r.stream.Flush()
		r.emitStep(st)

	case clichat.KindToolResult:
		t := ev.Tool
		if t == nil {
			return
		}
		st, ok := r.byID[t.ID]
		if !ok || st.Status != hermes.StepRunning {
			return
		}
		r.close(t.ID, st, t.IsError, t.Result, t.Images)

	case clichat.KindDone:
		if ev.Usage != nil {
			r.setUsage(*ev.Usage)
		}
	}
}

// close termina un paso y lo anuncia.
func (r *cliRecorder) close(id string, st *hermes.Step, isErr bool, text string, images []string) {
	st.Status = hermes.StepOK
	if isErr {
		st.Status = hermes.StepError
	}
	st.Text = clipText(text, cliStepTextRunes)
	if len(images) > 0 {
		st.Images = images
	}
	st.DurationMs = time.Since(r.started[id]).Milliseconds()
	r.emitStep(st)
}

func (r *cliRecorder) setUsage(u clichat.Usage) {
	r.usage = hermes.Usage{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheWrite, Thinking: u.Thinking, CostUSD: u.CostUSD}
	r.a.chatSvc().setUsage(r.chat.ID, r.usage)
	r.a.emit(evChatUsage, ChatUsageEvent{ChatID: r.chat.ID, InputTokens: r.usage.Input, OutputTokens: r.usage.Output, CostUSD: r.usage.CostUSD})
	r.sent = true
}

// finish cierra el turno: los pasos que quedaron abiertos (un CLI que no informa
// el resultado de una herramienta) pasan a ok, o a error si el turno se cortó, y
// arma el resultado que se guarda.
func (r *cliRecorder) finish(res clichat.Result, err error) hermes.Result {
	r.stream.Close()
	interrupted := res.Cancelled || err != nil
	for i, st := range r.steps {
		if st.Status != hermes.StepRunning {
			continue
		}
		id := r.ids[i]
		if interrupted {
			note := ""
			if res.Cancelled {
				note = i18n.T(i18n.Msg{ES: "cancelado", EN: "cancelled"})
			}
			r.close(id, st, true, note, nil)
		} else {
			r.close(id, st, false, "", nil)
		}
	}
	if res.Usage != nil && !r.sent {
		r.setUsage(*res.Usage)
	}
	steps := make([]hermes.Step, len(r.steps))
	for i, st := range r.steps {
		steps[i] = *st
	}
	model := res.Model
	if model == "" {
		model = r.chat.Model
	}
	return hermes.Result{Text: r.text.String(), Steps: steps, Usage: r.usage, Model: model, Cancelled: res.Cancelled}
}

// capStepArgs recorta los textos muy largos de los argumentos de un paso: se
// muestran y se guardan, y el código de un `create_checkpoint` ya está en el
// archivo del checkpoint.
func capStepArgs(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if s, ok := v.(string); ok {
			v = clipText(s, cliStepArgRunes)
		}
		out[k] = v
	}
	return out
}

func clipText(s string, max int) string {
	if rs := []rune(s); len(rs) > max {
		return string(rs[:max]) + "…"
	}
	return s
}

// utf16Len cuenta las unidades UTF-16 de s (el `length` de un string de JS).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
