package main

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"scadlab/backend/chat"
	"scadlab/backend/clichat"
	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/mcp"
	"scadlab/backend/printers"
	"scadlab/backend/prompts"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// Chat de diseño con modelos (S5, bloque 5.B) y Hermes (S12): conversación con
// Ollama, un servidor compatible con OpenAI o la API de Anthropic. Con «Agente»
// encendido el modelo usa las herramientas de ScadLab (crear checkpoints,
// renderizar, mirar, medir…) en un bucle (backend/hermes); apagado, es el chat
// de texto de S5. La orquestación de los turnos (cancelación, agrupado de
// trozos) vive en backend/chat, los clientes en backend/llm y el bucle en
// backend/hermes; aquí se resuelve el workspace, el servidor de modelos
// (app_servers.go), los adjuntos (app_chat_attach.go) y la persistencia, y se
// emiten los eventos.

// Eventos Wails del chat (Go → React). Los nombres son contrato con
// frontend/src/components/chat/chatStreams.ts.
const (
	evChatDelta = "chat:delta"
	evChatStep  = "chat:step"
	evChatUsage = "chat:usage"
	evChatDone  = "chat:done"
	evChatError = "chat:error"
)

// ChatDeltaEvent es un trozo de la respuesta. Seq cuenta los trozos del turno
// desde 1: una pantalla que se abre a mitad de respuesta recibe de GetChat el
// texto hasta cierto Seq y descarta los eventos que ya incluye.
type ChatDeltaEvent struct {
	ChatID int64  `json:"chatId"`
	Text   string `json:"text"`
	Seq    int    `json:"seq"`
}

// ChatStepEvent anuncia un paso de herramienta: una vez al arrancar (status
// running) y otra al terminar (ok o error) con el mismo Step.ID.
type ChatStepEvent struct {
	ChatID int64 `json:"chatId"`
	// WorkspaceID es el proyecto del chat: la interfaz abre en el visor y en
	// Código el checkpoint que un paso acaba de crear (S12.c) sin tener que saber
	// de qué chat vino.
	WorkspaceID int64       `json:"workspaceId"`
	Step        hermes.Step `json:"step"`
}

// ChatUsageEvent es el uso acumulado del turno hasta ahora. CostUSD es 0 cuando
// el proveedor no publica precios («no informado»).
type ChatUsageEvent struct {
	ChatID       int64   `json:"chatId"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	CostUSD      float64 `json:"costUsd"`
}

// ChatDoneEvent cierra un turno. MessageID es la respuesta guardada (0 si no
// hubo texto ni pasos). Cancelled dice que el usuario cortó: lo escrito hasta
// ahí se conserva igual.
type ChatDoneEvent struct {
	ChatID    int64 `json:"chatId"`
	MessageID int64 `json:"messageId"`
	Cancelled bool  `json:"cancelled"`
}

// ChatErrorEvent es un fallo durante el turno. Si ya había texto o pasos, se
// guarda como respuesta parcial y viaja su MessageID.
type ChatErrorEvent struct {
	ChatID    int64  `json:"chatId"`
	Message   string `json:"message"`
	MessageID int64  `json:"messageId"`
}

// ChatView es un chat con su conversación. Running, Partial, Steps y Usage
// permiten retomar una respuesta que sigue en curso (la pestaña se cerró y se
// volvió a abrir).
type ChatView struct {
	Chat       store.Chat          `json:"chat"`
	Messages   []store.ChatMessage `json:"messages"`
	Running    bool                `json:"running"`
	Partial    string              `json:"partial"`
	PartialSeq int                 `json:"partialSeq"`
	// Steps y Usage son los del turno en curso (vacíos si no hay).
	Steps []hermes.Step `json:"steps"`
	Usage hermes.Usage  `json:"usage"`
}

// SendOptions son las opciones de un envío. MaxSteps es el tope de llamadas a
// herramientas del turno (1–30; 0 = 12).
//
// Agent es un override explícito SOLO de este turno y casi nunca viene: nil (lo
// normal) usa el modo de la conversación (chats.mode, ver SetChatMode), que es
// «Agente» salvo que la persona haya elegido «Solo consulta» para ESA conversación.
// La interfaz solo lo manda cuando el modelo no llama herramientas y por eso el
// turno no puede ser «Agente» aunque la conversación lo sea; no cambia lo guardado.
// false = modo texto de S5 (bloques de código y «Guardar como checkpoint»);
// true = Hermes con herramientas, o el CLI con las de escritura.
type SendOptions struct {
	Agent    *bool `json:"agent,omitempty"`
	MaxSteps int   `json:"maxSteps"`
}

// agentFor resuelve si el turno de la conversación c corre en «Agente»: el
// override del envío si lo hay; si no, el modo guardado de la conversación.
func (o SendOptions) agentFor(c store.Chat) bool {
	if o.Agent != nil {
		return *o.Agent
	}
	return c.Agent()
}

// maxChatTextBytes acota un mensaje: pegar un archivo entero por error no debe
// mandar medio megabyte a un modelo local ni a un proveedor que cobra por token.
const maxChatTextBytes = 100 * 1024

// chatDefaultNumCtx es la ventana que se le pide a Ollama: su default (2k–4k)
// recorta el prompt SIN avisar, y el de sistema más el código abierto ya
// pasan de eso. Se puede cambiar con `numCtx` en la config del proveedor.
const chatDefaultNumCtx = 16384

// El estado del chat vive acá y no en App (app.go no se toca): un mapa por App,
// creado la primera vez que se usa y cerrado con onShutdown.
type chatService struct {
	mgr *chat.Manager
	// attachments son los adjuntos elegidos y todavía no enviados.
	attachments *hermes.Registry
	// pruned dice si ya se barrió la carpeta de preparación en esta sesión.
	pruneOnce sync.Once

	mu sync.Mutex
	// live es el estado de los turnos en curso (pasos y uso), para retomarlos.
	live map[int64]*liveTurn
	// noTools recuerda los modelos que ya rechazaron las herramientas (servidor|
	// modelo): no se vuelve a intentar en cada turno para fallar otra vez.
	noTools map[string]bool
}

// liveTurn es lo que GetChat necesita de un turno en curso además de su texto.
type liveTurn struct {
	steps []hermes.Step
	usage hermes.Usage
}

var chatServices sync.Map // *App → *chatService

func (a *App) chatSvc() *chatService {
	if v, ok := chatServices.Load(a); ok {
		return v.(*chatService)
	}
	s := &chatService{mgr: chat.NewManager(), attachments: hermes.NewRegistry(), live: map[int64]*liveTurn{}, noTools: map[string]bool{}}
	actual, loaded := chatServices.LoadOrStore(a, s)
	if !loaded {
		a.onShutdown(func() {
			// Cancela y espera: un turno que guarda su parcial después de cerrada
			// la base se perdería con un error.
			s.mgr.CancelAll(3 * time.Second)
			s.discardAllAttachments()
			chatServices.Delete(a)
		})
	}
	return actual.(*chatService)
}

func (s *chatService) beginLive(chatID int64) {
	s.mu.Lock()
	s.live[chatID] = &liveTurn{}
	s.mu.Unlock()
}

func (s *chatService) endLive(chatID int64) {
	s.mu.Lock()
	delete(s.live, chatID)
	s.mu.Unlock()
}

// upsertStep guarda el paso por su ID (un paso se anuncia al arrancar y de nuevo
// al terminar).
func (s *chatService) upsertStep(chatID int64, st hermes.Step) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.live[chatID]
	if l == nil {
		return
	}
	for i := range l.steps {
		if l.steps[i].ID == st.ID {
			l.steps[i] = st
			return
		}
	}
	l.steps = append(l.steps, st)
}

func (s *chatService) setUsage(chatID int64, u hermes.Usage) {
	s.mu.Lock()
	if l := s.live[chatID]; l != nil {
		l.usage = u
	}
	s.mu.Unlock()
}

func (s *chatService) liveSnapshot(chatID int64) ([]hermes.Step, hermes.Usage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.live[chatID]
	if l == nil {
		return []hermes.Step{}, hermes.Usage{}
	}
	return append([]hermes.Step{}, l.steps...), l.usage
}

func noToolsKey(serverID, model string) string { return serverID + "|" + model }

func (s *chatService) knownNoTools(serverID, model string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.noTools[noToolsKey(serverID, model)]
}

func (s *chatService) markNoTools(serverID, model string) {
	s.mu.Lock()
	s.noTools[noToolsKey(serverID, model)] = true
	s.mu.Unlock()
}

// chatErrorText agrega a un fallo de autenticación de dónde salió la clave: «401»
// a secas no dice qué revisar.
func chatErrorText(s store.ModelServer, err error) string {
	msg := err.Error()
	var se *llm.StatusError
	if !errors.As(err, &se) || (se.Status != 401 && se.Status != 403) {
		return msg
	}
	switch {
	case s.HasToken:
		msg += i18n.T(i18n.Msg{ES: " (usa la clave guardada del servidor «%s»; revísala en Configuración → Proveedores de IA)", EN: " (uses the saved key of the server “%s”; check it in Settings → AI providers)"}, s.Name)
	case s.APIKeyEnv != "":
		msg += i18n.T(i18n.Msg{ES: " (clave leída de la variable %s)", EN: " (key read from the %s variable)"}, s.APIKeyEnv)
	}
	return msg
}

// ListChats devuelve los chats del workspace, el más reciente primero.
func (a *App) ListChats(workspaceID int64) ([]store.Chat, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListChats(workspaceID)
}

// GetChat devuelve un chat con su conversación y, si está respondiendo, lo
// escrito hasta ahora.
func (a *App) GetChat(chatID int64) (ChatView, error) {
	if err := a.ready(); err != nil {
		return ChatView{}, err
	}
	c, err := a.store.GetChat(chatID)
	if err != nil {
		return ChatView{}, err
	}
	msgs, err := a.store.ListChatMessages(chatID)
	if err != nil {
		return ChatView{}, err
	}
	v := ChatView{Chat: c, Messages: msgs}
	v.Running, v.Partial, v.PartialSeq = a.chatSvc().mgr.Snapshot(chatID)
	v.Steps, v.Usage = a.chatSvc().liveSnapshot(chatID)
	return v, nil
}

// NewChat abre un chat vacío en el workspace con el servidor de modelos
// (providerID es un id de model_servers) o el CLI (`cli:<id>`, S12.b) y el
// modelo dados. Un modelo vacío toma el predeterminado del servidor; con un CLI,
// el que el CLI tenga configurado.
func (a *App) NewChat(workspaceID int64, providerID, model string) (store.Chat, error) {
	if err := a.ready(); err != nil {
		return store.Chat{}, err
	}
	if _, err := a.store.GetWorkspace(workspaceID); err != nil {
		return store.Chat{}, err
	}
	if clichat.IsProviderID(providerID) {
		// Un CLI: el modelo vacío es válido (usa el que el CLI tenga configurado).
		model = strings.TrimSpace(model)
		if _, err := a.resolveCLI(providerID, model); err != nil {
			return store.Chat{}, err
		}
		return a.store.CreateChat(workspaceID, providerID, model)
	}
	p, err := a.chatServer(providerID)
	if err != nil {
		return store.Chat{}, err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = p.DefaultModel
	}
	if model == "" {
		return store.Chat{}, i18n.New(i18n.Msg{ES: "elige un modelo para el chat", EN: "pick a model for the chat"})
	}
	return a.store.CreateChat(workspaceID, p.ID, model)
}

// SendChatMessage guarda el mensaje del usuario y lanza la respuesta, que llega
// por los eventos chat:delta / chat:step / chat:usage / chat:done / chat:error.
// currentFile es el checkpoint abierto en el editor ("" si ninguno): su código
// entra al prompt de sistema. attachmentIDs son los adjuntos elegidos (imágenes
// y modelos de referencia, ver app_chat_attach.go); con ellos el texto puede ir
// vacío. El modo (herramientas o solo consulta) es el de la conversación, salvo
// que opts.Agent lo fuerce para este turno. Devuelve el mensaje guardado.
//
// Todo lo que puede fallar antes de empezar (el servidor no responde, un adjunto
// que el modelo no admite, el chat ocupado) falla ANTES de guardar nada: no
// queda un mensaje del usuario huérfano.
func (a *App) SendChatMessage(chatID int64, text, currentFile string, attachmentIDs []string, opts SendOptions) (store.ChatMessage, error) {
	if err := a.ready(); err != nil {
		return store.ChatMessage{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" && len(attachmentIDs) == 0 {
		return store.ChatMessage{}, i18n.New(i18n.Msg{ES: "escribe un mensaje", EN: "type a message"})
	}
	if len(text) > maxChatTextBytes {
		return store.ChatMessage{}, i18n.Errorf(i18n.Msg{ES: "el mensaje es demasiado largo (máximo %d KB)", EN: "the message is too long (maximum %d KB)"}, maxChatTextBytes/1024)
	}
	c, err := a.store.GetChat(chatID)
	if err != nil {
		return store.ChatMessage{}, err
	}
	if clichat.IsProviderID(c.ProviderID) {
		return a.sendCLIMessage(c, text, currentFile, attachmentIDs, opts)
	}
	p, err := a.chatServer(c.ProviderID)
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
	// Con el chequeo previo del servidor: si no responde, el turno ni empieza
	// y no se guarda el mensaje.
	client, err := a.llmClientForTurn(p)
	if err != nil {
		return store.ChatMessage{}, err
	}
	agent := opts.agentFor(c)
	caps := a.modelCapabilities(p, client, c.Model, agent || len(pending) > 0)
	if err := hermes.CheckVision(pending, caps.Vision, caps.VisionKnown, p.Name+" · "+c.Model); err != nil {
		return store.ChatMessage{}, err
	}

	ot, err := a.openChatTurn(c, text, pending, attachmentIDs, caps.assumeVision())
	if err != nil {
		return store.ChatMessage{}, err
	}
	um, turn, stored, prepared := ot.user, ot.turn, ot.stored, ot.prepared

	job := chatJob{
		turn: turn, chat: c, server: p, client: client, workspace: w, caps: caps,
		currentFile: currentFile, user: hermes.UserMessage(prepared), agent: agent,
		maxSteps: hermes.ClampSteps(opts.MaxSteps), history: seedFrom(stored, p.Kind), refs: referencePaths(stored, prepared.Metas),
	}
	a.chatSvc().beginLive(chatID)
	go a.runChatTurn(job)
	return um, nil
}

// openedTurn es un turno ya reservado con el mensaje del usuario guardado: lo
// que SendChatMessage y sendCLIMessage tienen en común.
type openedTurn struct {
	turn     *chat.Turn
	stored   []store.ChatMessage // el historial ANTES de este mensaje
	prepared hermes.Prepared
	user     store.ChatMessage
}

// openChatTurn reserva el turno del chat, prepara los adjuntos, guarda el
// mensaje del usuario y le pone título al chat si es el primero. vision dice si
// la vista previa de un modelo de referencia viaja como imagen. Si algo falla
// libera el turno y no queda nada a medias.
func (a *App) openChatTurn(c store.Chat, text string, pending []*hermes.Pending, attachmentIDs []string, vision bool) (openedTurn, error) {
	var ot openedTurn
	turn, err := a.chatSvc().mgr.Begin(a.appCtx(), c.ID)
	if err != nil {
		return ot, err
	}
	// El historial se lee DESPUÉS de reservar el turno: si la respuesta anterior
	// terminó justo ahora, ya guardó su mensaje y tiene que entrar.
	stored, err := a.store.ListChatMessages(c.ID)
	if err != nil {
		turn.End()
		return ot, err
	}
	prepared, err := hermes.Prepare(text, pending, vision, a.chatDir(c.ID))
	if err != nil {
		turn.End()
		return ot, err
	}
	um, err := a.store.AddChatMessageFull(c.ID, store.NewChatMessage{
		Role: "user", Content: text, Parts: hermes.EncodeParts(hermes.Parts{Attachments: prepared.Metas}),
	})
	if err != nil {
		turn.End()
		return ot, err
	}
	a.chatSvc().releaseAttachments(attachmentIDs)
	if len(stored) == 0 {
		title := text
		if title == "" && len(prepared.Metas) > 0 {
			title = prepared.Metas[0].Name
		}
		if err := a.store.SetChatTitle(c.ID, chat.Title(title)); err != nil {
			log.Printf("scadlab: título del chat %d: %v", c.ID, err)
		}
	}
	return openedTurn{turn: turn, stored: stored, prepared: prepared, user: um}, nil
}

// seedFrom arma el historial del modelo con las filas guardadas.
func seedFrom(stored []store.ChatMessage, kind string) []llm.Message {
	rows := make([]hermes.StoredMessage, len(stored))
	for i, m := range stored {
		rows[i] = hermes.StoredMessage{Role: m.Role, Content: m.Content, Parts: m.Parts}
	}
	return hermes.SeedHistory(rows, chat.HistoryBudget(kind))
}

// referencePaths son las referencias 3D que el chat tiene adjuntas (las de los
// mensajes anteriores y las de este), para el prompt de sistema.
func referencePaths(stored []store.ChatMessage, current []hermes.AttachmentMeta) []string {
	seen := map[string]bool{}
	var out []string
	add := func(metas []hermes.AttachmentMeta) {
		for _, m := range metas {
			if m.Kind == hermes.KindModel && m.RefPath != "" && !seen[m.RefPath] {
				seen[m.RefPath] = true
				out = append(out, m.RefPath)
			}
		}
	}
	for _, m := range stored {
		add(hermes.ParseParts(m.Parts).Attachments)
	}
	add(current)
	return out
}

// appCtx es el contexto de la app (Background en los tests que no lo traen).
func (a *App) appCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// chatJob es todo lo que un turno necesita una vez validado el envío.
type chatJob struct {
	turn        *chat.Turn
	chat        store.Chat
	server      store.ModelServer
	client      llm.Client
	workspace   store.Workspace
	caps        modelCaps
	currentFile string
	user        llm.Message
	agent       bool
	maxSteps    int
	history     []llm.Message
	refs        []string
	// cli es lo propio de un turno con un CLI agéntico (S12.b); nil en un turno
	// con un servidor de modelos.
	cli *cliJob
}

// runChatTurn corre la respuesta, la guarda y avisa a la pantalla. Un
// cancelado conserva lo escrito y los pasos; un fallo con texto o pasos previos
// también (es lo que el usuario vio y puede querer copiar).
func (a *App) runChatTurn(j chatJob) {
	defer j.turn.End()
	c, p := j.chat, j.server
	svc := a.chatSvc()
	defer svc.endLive(c.ID)
	if j.cli != nil {
		a.runCLITurn(j)
		return
	}

	cfg := a.withAutoSave(j, a.hermesConfig(j))
	if cl, ok := cfg.Tools.(io.Closer); ok {
		// La caja se cierra al terminar el turno; lo inyectado (base, motor,
		// WebAssembly, caché de partes) es de la app y no se cierra.
		defer cl.Close()
	}
	stream := j.turn.Stream(func(seq int, text string) {
		a.emit(evChatDelta, ChatDeltaEvent{ChatID: c.ID, Text: text, Seq: seq})
	})
	res, err := hermes.Run(j.turn.Context(), cfg, hermes.Hooks{
		Text:  stream.Add,
		Flush: stream.Flush,
		Step: func(st hermes.Step) {
			svc.upsertStep(c.ID, st)
			a.emit(evChatStep, ChatStepEvent{ChatID: c.ID, WorkspaceID: c.WorkspaceID, Step: st})
			a.refreshTreeAfterStep(c.WorkspaceID, st)
		},
		Usage: func(u hermes.Usage) {
			svc.setUsage(c.ID, u)
			a.emit(evChatUsage, ChatUsageEvent{ChatID: c.ID, InputTokens: u.Input, OutputTokens: u.Output, CostUSD: u.CostUSD})
		},
	})
	stream.Close()
	if res.ToolsUnsupported {
		svc.markNoTools(p.ID, c.Model)
	}

	errText := ""
	if err != nil {
		errText = chatErrorText(p, err)
	}
	a.concludeChatTurn(c, res, err, errText)
}

// concludeChatTurn guarda la respuesta del turno (con sus pasos, tokens y modelo)
// y avisa a la pantalla: chat:done, o chat:error con errText si falló. Lo comparten
// los turnos con un servidor y con un CLI.
func (a *App) concludeChatTurn(c store.Chat, res hermes.Result, err error, errText string) {
	var messageID int64
	if strings.TrimSpace(res.Text) != "" || len(res.Steps) > 0 {
		m, serr := a.store.AddChatMessageFull(c.ID, store.NewChatMessage{
			Role: "assistant", Content: res.Text,
			Parts:       hermes.EncodeParts(hermes.Parts{Steps: hermes.StepsForStorage(res.Steps), Model: res.Model, StepLimit: res.StepLimit}),
			InputTokens: res.Usage.Input, OutputTokens: res.Usage.Output, CostUSD: res.Usage.CostUSD,
		})
		switch {
		case serr == nil:
			messageID = m.ID
		case errors.Is(serr, store.ErrNotFound) || a.chatGone(c.ID):
			// Borraron el chat mientras respondía: no hay dónde guardarlo.
		default:
			log.Printf("scadlab: guardar la respuesta del chat %d: %v", c.ID, serr)
		}
	}
	if err != nil {
		a.emit(evChatError, ChatErrorEvent{ChatID: c.ID, Message: errText, MessageID: messageID})
		return
	}
	a.emit(evChatDone, ChatDoneEvent{ChatID: c.ID, MessageID: messageID, Cancelled: res.Cancelled})
}

// hermesConfig arma la configuración del bucle: los dos prompts de sistema (con
// y sin herramientas), la caja de herramientas del workspace si el modelo las
// puede usar, y el historial.
func (a *App) hermesConfig(j chatJob) hermes.Config {
	c, p, w := j.chat, j.server, j.workspace
	pctx := chatPromptContext(w, j.currentFile)
	vision := j.caps.assumeVision()
	base := prompts.AgentContext{Context: pctx, Vision: vision, MaxSteps: j.maxSteps, References: j.refs}
	toolsCtx := base
	toolsCtx.Tools = true
	toolsCtx.Skills = mcp.ListWorkspaceSkills(w.Path)
	textCtx := base // Tools=false: el prompt de S5

	cfg := hermes.Config{
		Client: j.client, Model: c.Model,
		SystemTools: prompts.AgentSystemPrompt(toolsCtx),
		SystemText:  prompts.AgentSystemPrompt(textCtx),
		History:     j.history,
		User:        j.user,
		Vision:      vision,
		ImageDir:    a.chatDir(c.ID),
		MaxSteps:    j.maxSteps,
		Price:       hermes.Price{PromptPerM: j.caps.PromptPricePerM, CompletionPerM: j.caps.CompletionPricePerM},
		NoTools:     true,
	}
	if !j.agent {
		return cfg
	}
	switch {
	case j.caps.ToolsKnown && !j.caps.Tools, a.chatSvc().knownNoTools(p.ID, c.Model):
		// El modelo ya mostró que no las soporta: modo texto, con el aviso.
		cfg.NoToolsNotice = true
		return cfg
	}
	tools, err := newChatToolset(a, w)
	if err != nil {
		log.Printf("scadlab: caja de herramientas del chat %d: %v", c.ID, err)
		cfg.NoToolsNotice = true
		return cfg
	}
	cfg.Tools, cfg.NoTools = tools, false
	return cfg
}

// withAutoSave le pone a cfg la red de seguridad (ver app_chat_autosave.go): solo
// en modo «Agente». Va también cuando el modelo NO tiene herramientas —es el caso
// para el que existe— y cuando las tiene pero contestó con el código en vez de
// llamarlas.
func (a *App) withAutoSave(j chatJob, cfg hermes.Config) hermes.Config {
	if !j.agent {
		return cfg
	}
	saver := a.newAutoSaver(j.workspace, j.chat.ID, j.currentFile)
	cfg.AutoSave = saver.run
	return cfg
}

// newChatToolset abre la caja de herramientas del workspace para un turno, con la
// base, el motor de render, el WebAssembly y la caché de partes DE LA APP
// inyectados: Hermes no abre una segunda base ni un segundo motor. Es una
// variable para que las pruebas pongan una caja de mentira.
var newChatToolset = func(a *App, w store.Workspace) (hermes.Toolset, error) {
	return mcp.NewToolbox(mcp.ToolboxOptions{
		WorkspaceDir: w.Path, Store: a.store, Engine: a.engine, WASM: a.wasm(), Parts: partsStoreFor(a.dataDir), InApp: true,
	})
}

func (a *App) chatGone(chatID int64) bool {
	_, err := a.store.GetChat(chatID)
	return errors.Is(err, store.ErrNotFound)
}

// CancelChat corta la respuesta en curso del chat. Lo ya escrito se conserva.
func (a *App) CancelChat(chatID int64) {
	a.chatSvc().mgr.Cancel(chatID)
}

// DeleteChat borra el chat y su conversación. Si estaba respondiendo, la corta.
func (a *App) DeleteChat(chatID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	a.chatSvc().mgr.Cancel(chatID)
	if err := a.store.DeleteChat(chatID); err != nil {
		return err
	}
	// Las imágenes que el chat mandó ya no sirven a nadie.
	if dir := a.chatDir(chatID); dir != "" {
		_ = os.RemoveAll(dir)
	}
	return nil
}

// RenameChat cambia el nombre de una conversación. El texto se recorta y los
// espacios se colapsan; no puede quedar vacío ni pasar de 120 letras.
func (a *App) RenameChat(chatID int64, title string) error {
	if err := a.ready(); err != nil {
		return err
	}
	title = chat.CleanTitle(title)
	if title == "" {
		return i18n.New(i18n.Msg{ES: "el nombre de la conversación no puede estar vacío", EN: "the conversation name cannot be empty"})
	}
	return a.store.RenameChat(chatID, title)
}

// SetChatMode fija el modo de ESA conversación: "agent" (el predeterminado: el
// asistente crea y cambia modelos) o "ask" («Solo consulta»: solo lee y explica).
// Se guarda en chats.mode y vale para todos sus turnos siguientes; no cambia el
// modo de ninguna otra conversación ni deja un nuevo predeterminado. Un turno en
// curso termina con el modo con que empezó.
func (a *App) SetChatMode(chatID int64, mode string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetChatMode(chatID, mode)
}

// SetChatModel cambia el servidor y el modelo con que sigue una conversación (a
// mitad de camino): el historial se conserva y el próximo turno ya usa el nuevo.
// Un turno en curso termina con el modelo anterior.
func (a *App) SetChatModel(chatID int64, serverID, model string) error {
	if err := a.ready(); err != nil {
		return err
	}
	c, err := a.store.GetChat(chatID)
	if err != nil {
		return err
	}
	serverID, model = strings.TrimSpace(serverID), strings.TrimSpace(model)
	var providerID string
	if clichat.IsProviderID(serverID) {
		// Un CLI: el modelo vacío es válido. Al cambiar de proveedor el store
		// olvida la sesión del CLI anterior.
		if _, err := a.resolveCLI(serverID, model); err != nil {
			return err
		}
		providerID = serverID
	} else {
		srv, err := a.chatServer(serverID)
		if err != nil {
			return err
		}
		if model == "" {
			model = srv.DefaultModel
		}
		if model == "" {
			return i18n.New(i18n.Msg{ES: "elige un modelo para el chat", EN: "pick a model for the chat"})
		}
		providerID = srv.ID
	}
	if err := a.store.SetChatModel(chatID, providerID, model); err != nil {
		return err
	}
	// La elección también es la última del proyecto (lo que arranca el próximo
	// chat nuevo). No es crítico.
	if err := a.store.SetLastModel(c.WorkspaceID, store.LastModel{ServerID: providerID, Model: model}); err != nil {
		log.Printf("scadlab: recordar el modelo del chat %d: %v", chatID, err)
	}
	return nil
}

// SaveChatCheckpoint es SaveAsCheckpoint desde un bloque de código del chat: además
// de crear el checkpoint guarda de qué chat salió (chat_id), para que el árbol
// pueda ofrecer «Abrir el chat» (S3). Un chat que no es del workspace se ignora
// —el checkpoint se crea igual—: el vínculo es una ayuda, no un requisito.
func (a *App) SaveChatCheckpoint(workspaceID, chatID int64, parentFile, label, description, source string) (string, error) {
	name, err := a.SaveAsCheckpoint(workspaceID, parentFile, label, description, source)
	if err != nil || chatID <= 0 {
		return name, err
	}
	if c, cerr := a.store.GetChat(chatID); cerr != nil || c.WorkspaceID != workspaceID {
		return name, nil
	}
	if lerr := a.store.SetCheckpointChat(workspaceID, name, chatID); lerr != nil {
		log.Printf("scadlab: vincular %s con el chat %d: %v", name, chatID, lerr)
	}
	return name, nil
}

// ChatCheckpointParent resuelve el padre con el que se guarda un bloque del
// chat como checkpoint: el archivo abierto; si no hay (o ya no existe), el
// último checkpoint; si el workspace no tiene ninguno devuelve "" —no es un
// error—: el bloque será el primero, la raíz del árbol (S12.c).
func (a *App) ChatCheckpointParent(workspaceID int64, currentFile string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	w, err := a.store.GetWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	return chatParent(w.Path, currentFile), nil
}

// chatParent es el padre con el que se guarda un bloque del chat; "" en un
// workspace sin ningún checkpoint (checkpoint.CreateRoot lo crea sin padre).
func chatParent(dir, currentFile string) string {
	if currentFile != "" && validCheckpointName(currentFile) == nil {
		if st, err := os.Stat(filepath.Join(dir, currentFile)); err == nil && !st.IsDir() {
			return currentFile
		}
	}
	return latestCheckpoint(dir)
}

// chatPromptContext reúne lo que el prompt de sistema sabe del workspace: la
// impresora (volumen y filamentos del AMS), los checkpoints existentes y el
// código del archivo abierto, leído del DISCO —no del editor— con el mismo
// control de nombre que ReadCheckpoint, para que la interfaz no pueda hacer
// leer un archivo fuera del workspace.
func chatPromptContext(w store.Workspace, currentFile string) prompts.Context {
	ctx := prompts.Context{Lang: i18n.Lang(), WorkspaceName: w.Name}
	if pr, ok := printers.ByID(w.PrinterID); ok {
		ctx.PrinterName = pr.Name
		ctx.BuildVolume = [3]float64{pr.BuildVolume.X, pr.BuildVolume.Y, pr.BuildVolume.Z}
		ctx.ColorSlots = pr.MulticolorSlots
	}
	if files, err := workspace.Scan(w.Path); err == nil {
		for _, f := range files {
			ctx.Checkpoints = append(ctx.Checkpoints, f.FileName)
		}
	}
	if currentFile != "" && validCheckpointName(currentFile) == nil {
		if f, err := os.Open(filepath.Join(w.Path, currentFile)); err == nil {
			raw, _ := io.ReadAll(io.LimitReader(f, 1<<20))
			f.Close()
			ctx.CurrentFile = currentFile
			ctx.CurrentSource = string(raw)
		}
	}
	return ctx
}
