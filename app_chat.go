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
	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/printers"
	"scadlab/backend/prompts"
	"scadlab/backend/store"
	"scadlab/backend/workspace"
)

// Chat de diseño con modelos (S5, bloque 5.B): conversación de texto con
// Ollama, un servidor compatible con OpenAI o la API de Anthropic. La
// orquestación (turnos, cancelación, agrupado de trozos) vive en
// backend/chat y los clientes en backend/llm; aquí se resuelve el workspace,
// el proveedor y la persistencia, y se emiten los eventos.

// Eventos Wails del chat (Go → React). Los nombres son contrato con
// frontend/src/components/chat/chatStreams.ts.
const (
	evChatDelta = "chat:delta"
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

// ChatDoneEvent cierra un turno. MessageID es la respuesta guardada (0 si no
// hubo texto). Cancelled dice que el usuario cortó: lo escrito hasta ahí se
// conserva igual.
type ChatDoneEvent struct {
	ChatID    int64 `json:"chatId"`
	MessageID int64 `json:"messageId"`
	Cancelled bool  `json:"cancelled"`
}

// ChatErrorEvent es un fallo durante el turno. Si ya había texto, se guarda
// como respuesta parcial y viaja su MessageID.
type ChatErrorEvent struct {
	ChatID    int64  `json:"chatId"`
	Message   string `json:"message"`
	MessageID int64  `json:"messageId"`
}

// ChatView es un chat con su conversación. Running y Partial permiten retomar
// una respuesta que sigue en curso (la pestaña se cerró y se volvió a abrir).
type ChatView struct {
	Chat       store.Chat          `json:"chat"`
	Messages   []store.ChatMessage `json:"messages"`
	Running    bool                `json:"running"`
	Partial    string              `json:"partial"`
	PartialSeq int                 `json:"partialSeq"`
}

// maxChatTextBytes acota un mensaje: pegar un archivo entero por error no debe
// mandar medio megabyte a un modelo local ni a un proveedor que cobra por token.
const maxChatTextBytes = 100 * 1024

// chatDefaultNumCtx es la ventana que se le pide a Ollama: su default (2k–4k)
// recorta el prompt SIN avisar, y el de sistema más el código abierto ya
// pasan de eso. Se puede cambiar con `numCtx` en la config del proveedor.
const chatDefaultNumCtx = 16384

// El estado del chat vive acá y no en App (app.go no se toca en S5): un mapa
// por App, creado la primera vez que se usa y cerrado con onShutdown.
type chatService struct{ mgr *chat.Manager }

var chatServices sync.Map // *App → *chatService

func (a *App) chatSvc() *chatService {
	if v, ok := chatServices.Load(a); ok {
		return v.(*chatService)
	}
	s := &chatService{mgr: chat.NewManager()}
	actual, loaded := chatServices.LoadOrStore(a, s)
	if !loaded {
		a.onShutdown(func() {
			// Cancela y espera: un turno que guarda su parcial después de cerrada
			// la base se perdería con un error.
			s.mgr.CancelAll(3 * time.Second)
			chatServices.Delete(a)
		})
	}
	return actual.(*chatService)
}

// isChatProvider dice si el proveedor sirve para el chat: los CLIs agénticos
// van por la terminal, no por acá.
func isChatProvider(p store.AIProvider) bool {
	return p.Kind == llm.KindOllama || p.Kind == llm.KindOpenAI || p.Kind == llm.KindAnthropic
}

func cfgString(p store.AIProvider, key string) string {
	s, _ := p.Config[key].(string)
	return strings.TrimSpace(s)
}

func cfgInt(p store.AIProvider, key string) int {
	switch n := p.Config[key].(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

// chatProvider lee un proveedor y verifica que se pueda chatear con él.
func (a *App) chatProvider(id string) (store.AIProvider, error) {
	p, err := a.store.GetProvider(id)
	if errors.Is(err, store.ErrNotFound) {
		return p, i18n.New(i18n.Msg{ES: "el proveedor de IA no existe", EN: "the AI provider does not exist"})
	}
	if err != nil {
		return p, err
	}
	if !isChatProvider(p) {
		return p, i18n.Errorf(i18n.Msg{ES: "%s corre en la terminal, no en el chat", EN: "%s runs in the terminal, not in the chat"}, p.Name)
	}
	if !p.Enabled {
		return p, i18n.Errorf(i18n.Msg{ES: "el proveedor %s está deshabilitado (Configuración → IA)", EN: "the provider %s is disabled (Settings → AI)"}, p.Name)
	}
	return p, nil
}

// chatClient arma el cliente del proveedor. La clave sale de la variable de
// entorno que nombra apiKeyEnv; nunca se guarda ni se loguea.
func chatClient(p store.AIProvider) (llm.Client, error) {
	opt := llm.Options{}
	if env := cfgString(p, "apiKeyEnv"); env != "" {
		if !chat.ValidEnvName(env) {
			return nil, i18n.Errorf(i18n.Msg{ES: "%q no es un nombre de variable de entorno válido (Configuración → IA)", EN: "%q is not a valid environment variable name (Settings → AI)"}, env)
		}
		if key, ok := chat.LookupAPIKey(env); ok {
			opt.Token = key
		} else if p.Kind == llm.KindAnthropic {
			// Anthropic no responde sin clave. Un servidor compatible con OpenAI
			// sí puede (LM Studio y vLLM locales), así que ahí se sigue sin
			// clave y, si el servidor la exige, contesta 401 con su motivo.
			return nil, i18n.Errorf(i18n.Msg{
				ES: "falta la clave de API: definí la variable de entorno %s con tu clave y reiniciá ScadLab",
				EN: "the API key is missing: set the environment variable %s to your key and restart ScadLab",
			}, env)
		}
	} else if p.Kind == llm.KindAnthropic {
		return nil, i18n.New(i18n.Msg{ES: "falta indicar la variable de entorno con la clave (Configuración → IA)", EN: "the environment variable that holds the key is not set (Settings → AI)"})
	}
	baseURL := cfgString(p, "baseUrl")
	if p.Kind == llm.KindOllama {
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		opt.NumCtx = chatDefaultNumCtx
		if n := cfgInt(p, "numCtx"); n > 0 {
			opt.NumCtx = n
		}
	}
	return llm.New(p.Kind, baseURL, opt)
}

// chatErrorText agrega a un fallo de autenticación el nombre de la variable
// que se leyó: «401» a secas no dice qué revisar.
func chatErrorText(p store.AIProvider, err error) string {
	msg := err.Error()
	var se *llm.StatusError
	if env := cfgString(p, "apiKeyEnv"); env != "" && errors.As(err, &se) && (se.Status == 401 || se.Status == 403) {
		msg += i18n.T(i18n.Msg{ES: " (clave leída de la variable %s)", EN: " (key read from the %s variable)"}, env)
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
	return v, nil
}

// NewChat abre un chat vacío en el workspace con el proveedor y modelo dados.
// Un modelo vacío toma el de la config del proveedor.
func (a *App) NewChat(workspaceID int64, providerID, model string) (store.Chat, error) {
	if err := a.ready(); err != nil {
		return store.Chat{}, err
	}
	if _, err := a.store.GetWorkspace(workspaceID); err != nil {
		return store.Chat{}, err
	}
	p, err := a.chatProvider(providerID)
	if err != nil {
		return store.Chat{}, err
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = cfgString(p, "model")
	}
	if model == "" {
		return store.Chat{}, i18n.New(i18n.Msg{ES: "elige un modelo para el chat", EN: "pick a model for the chat"})
	}
	return a.store.CreateChat(workspaceID, p.ID, model)
}

// SendChatMessage guarda el mensaje del usuario y lanza la respuesta, que llega
// por los eventos chat:delta / chat:done / chat:error. currentFile es el
// checkpoint abierto en el editor ("" si ninguno): su código entra al prompt
// de sistema. Devuelve el mensaje guardado.
func (a *App) SendChatMessage(chatID int64, text, currentFile string) (store.ChatMessage, error) {
	if err := a.ready(); err != nil {
		return store.ChatMessage{}, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return store.ChatMessage{}, i18n.New(i18n.Msg{ES: "escribe un mensaje", EN: "type a message"})
	}
	if len(text) > maxChatTextBytes {
		return store.ChatMessage{}, i18n.Errorf(i18n.Msg{ES: "el mensaje es demasiado largo (máximo %d KB)", EN: "the message is too long (maximum %d KB)"}, maxChatTextBytes/1024)
	}
	c, err := a.store.GetChat(chatID)
	if err != nil {
		return store.ChatMessage{}, err
	}
	p, err := a.chatProvider(c.ProviderID)
	if err != nil {
		return store.ChatMessage{}, err
	}
	w, err := a.store.GetWorkspace(c.WorkspaceID)
	if err != nil {
		return store.ChatMessage{}, err
	}
	client, err := chatClient(p)
	if err != nil {
		return store.ChatMessage{}, err
	}

	turn, err := a.chatSvc().mgr.Begin(a.appCtx(), chatID)
	if err != nil {
		return store.ChatMessage{}, err
	}
	// El historial se lee DESPUÉS de reservar el turno: si la respuesta anterior
	// terminó justo ahora, ya guardó su mensaje y tiene que entrar.
	history, err := a.store.ListChatMessages(chatID)
	if err != nil {
		turn.End()
		return store.ChatMessage{}, err
	}
	um, err := a.store.AddChatMessage(chatID, "user", text)
	if err != nil {
		turn.End()
		return store.ChatMessage{}, err
	}
	if len(history) == 0 {
		if err := a.store.SetChatTitle(chatID, chat.Title(text)); err != nil {
			log.Printf("scadlab: título del chat %d: %v", chatID, err)
		}
	}

	msgs := make([]llm.Message, 0, len(history)+1)
	for _, m := range history {
		msgs = append(msgs, llm.Message{Role: m.Role, Content: m.Content})
	}
	msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: text})
	req := llm.ChatRequest{
		Model:    c.Model,
		System:   prompts.SystemPrompt(chatPromptContext(w, currentFile)),
		Messages: chat.TrimHistory(chat.MergeRoles(msgs), chat.HistoryBudget(p.Kind)),
	}
	go a.runChatTurn(turn, c, p, client, req)
	return um, nil
}

// appCtx es el contexto de la app (Background en los tests que no lo traen).
func (a *App) appCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

// runChatTurn corre la respuesta, la guarda y avisa a la pantalla. Un
// cancelado conserva lo escrito; un fallo con texto previo también (es lo que
// el usuario vio y puede querer copiar).
func (a *App) runChatTurn(turn *chat.Turn, c store.Chat, p store.AIProvider, client llm.Client, req llm.ChatRequest) {
	defer turn.End()
	out, err := turn.Run(client, req, func(seq int, text string) {
		a.emit(evChatDelta, ChatDeltaEvent{ChatID: c.ID, Text: text, Seq: seq})
	})
	var messageID int64
	if strings.TrimSpace(out.Text) != "" {
		m, serr := a.store.AddChatMessage(c.ID, "assistant", out.Text)
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
		a.emit(evChatError, ChatErrorEvent{ChatID: c.ID, Message: chatErrorText(p, err), MessageID: messageID})
		return
	}
	a.emit(evChatDone, ChatDoneEvent{ChatID: c.ID, MessageID: messageID, Cancelled: out.Cancelled})
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
	return a.store.DeleteChat(chatID)
}

// ListProviderModels lista los modelos que ofrece un proveedor: Ollama
// (/api/tags) y los compatibles con OpenAI (/v1/models) por red; Anthropic, una
// lista fija (llm.AnthropicModels), para no pedir la clave solo para llenar un
// desplegable.
func (a *App) ListProviderModels(providerID string) ([]string, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	p, err := a.chatProvider(providerID)
	if err != nil {
		return nil, err
	}
	if p.Kind == llm.KindAnthropic {
		return append([]string(nil), llm.AnthropicModels...), nil
	}
	client, err := chatClient(p)
	if err != nil {
		return nil, err
	}
	return client.Models(a.appCtx())
}

// ChatCheckpointParent resuelve el padre con el que se guarda un bloque del
// chat como checkpoint: el archivo abierto; si no hay (o ya no existe), el
// último checkpoint; si el workspace no tiene ninguno, un error claro.
func (a *App) ChatCheckpointParent(workspaceID int64, currentFile string) (string, error) {
	if err := a.ready(); err != nil {
		return "", err
	}
	w, err := a.store.GetWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	return chatParent(w.Path, currentFile)
}

func chatParent(dir, currentFile string) (string, error) {
	if currentFile != "" && validCheckpointName(currentFile) == nil {
		if st, err := os.Stat(filepath.Join(dir, currentFile)); err == nil && !st.IsDir() {
			return currentFile, nil
		}
	}
	if last := latestCheckpoint(dir); last != "" {
		return last, nil
	}
	return "", i18n.New(i18n.Msg{
		ES: "este workspace todavía no tiene ningún checkpoint que sirva de padre: pídele a un agente el primero (o crea el .scad a mano) y vuelve a guardar",
		EN: "this workspace has no checkpoint yet to be the parent: ask an agent for the first one (or create the .scad by hand) and save again",
	})
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
