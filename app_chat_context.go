package main

import (
	"context"
	"strings"
	"time"

	"scadlab/backend/chat"
	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/store"
)

// Ventana de contexto y perfil de herramientas (S17, 17.3). Un modelo chico de
// Ollama (8k–32k) no entra con el catálogo completo de herramientas, un prompt de
// sistema largo y el código de una maceta de 600 líneas: este archivo decide cuánto
// contexto tiene un chat, qué herramientas declara en consecuencia y avisa cuando
// el prompt y las herramientas solas ya se comen demasiado.

const (
	// minOllamaNumCtx y maxAutoNumCtx acotan el num_ctx que ScadLab elige solo
	// para Ollama: un modelo con 131 072 de ventana nativa reserva memoria de GPU
	// para toda ella, y 32 768 sobra para editar un modelo; por debajo de 8 192 el
	// prompt y las herramientas no caben.
	minOllamaNumCtx = 8192
	maxAutoNumCtx   = 32768
	// essentialBelow: bajo esta ventana efectiva, «Auto» declara solo las
	// herramientas esenciales y el prompt compacto.
	essentialBelow = 32768
	// overBudgetRatio: si el prompt y las herramientas pasan de esta fracción de la
	// ventana, el chat lo avisa (queda poco para trabajar).
	overBudgetRatio = 0.60
)

// ctxWindow es la ventana de contexto de un turno con un servidor.
type ctxWindow struct {
	// Tokens es la ventana efectiva; 0 = no se sabe (un servidor en la nube que no
	// la informa): rigen los topes fijos y «Auto» declara el catálogo completo.
	Tokens int
	// Fixed: el servidor la tiene fijada a mano (store.ModelServer.ContextTokens).
	Fixed bool
}

// effectiveWindow resuelve la ventana del turno.
//
//   - Ollama: la del servidor si está fijada; si no, min(ventana del modelo,
//     32 768) con mínimo 8 192; si el modelo no la dice, chatDefaultNumCtx. Es la
//     que se pide como `num_ctx`: si fuera menor que lo que el prompt necesita,
//     Ollama recortaría el comienzo del prompt SIN avisar.
//   - Los demás: la del servidor si está fijada (un LM Studio con la ventana
//     conocida), la que informe el proveedor o 0.
func effectiveWindow(srv store.ModelServer, caps modelCaps) ctxWindow {
	if srv.ContextTokens > 0 {
		return ctxWindow{Tokens: srv.ContextTokens, Fixed: true}
	}
	if srv.Kind != store.ServerOllama {
		return ctxWindow{Tokens: caps.ContextTokens}
	}
	if caps.ContextTokens <= 0 {
		return ctxWindow{Tokens: chatDefaultNumCtx}
	}
	n := caps.ContextTokens
	if n > maxAutoNumCtx {
		n = maxAutoNumCtx
	}
	if n < minOllamaNumCtx {
		n = minOllamaNumCtx
	}
	return ctxWindow{Tokens: n}
}

// windowFor es effectiveWindow como variable: la prueba «antes» del Ollama chico (app_hermes_small_test.go) la
// reemplaza por «ventana desconocida» para reproducir lo que hacía el chat antes de S17 (topes fijos, num_ctx de 16 384).
var windowFor = effectiveWindow

// Perfiles de herramientas que declara Hermes (los mismos nombres que guarda
// store.Chat.Tools y que mcp.ToolProfile).
const (
	profileEssential = store.ChatToolsEssential
	profileFull      = store.ChatToolsFull
)

// resolveToolProfile decide el perfil del turno: el que la persona fijó en la
// conversación, o —en «Auto» (chats.tools vacío)— esenciales cuando la ventana
// efectiva es menor de 32 768 y todas si es mayor o no se sabe. auto dice que lo
// decidió ScadLab.
func resolveToolProfile(chatTools string, window int) (profile string, auto bool) {
	switch chatTools {
	case store.ChatToolsEssential, store.ChatToolsFull:
		return chatTools, false
	}
	if window > 0 && window < essentialBelow {
		return profileEssential, true
	}
	return profileFull, true
}

// ChatContextEvent es lo que se sabe de la ventana de un turno con un servidor:
// cuánto pesan por sí solos el prompt de sistema y las herramientas declaradas (se
// reenvían en CADA vuelta) y si eso ya es demasiado. Perfil vacío = sin
// herramientas (modo texto).
type ChatContextEvent struct {
	ChatID       int64  `json:"chatId"`
	NumCtx       int    `json:"numCtx"`
	PromptTokens int    `json:"promptTokens"`
	ToolsTokens  int    `json:"toolsTokens"`
	Profile      string `json:"profile"`
	Auto         bool   `json:"auto"`
	OverBudget   bool   `json:"overBudget"`
}

func (s *chatService) setContextInfo(ev ChatContextEvent) {
	s.mu.Lock()
	s.ctxInfo[ev.ChatID] = ev
	s.mu.Unlock()
}

func (s *chatService) contextInfo(chatID int64) (ChatContextEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ev, ok := s.ctxInfo[chatID]
	return ev, ok
}

// contextEvent arma el chat:context de un turno a partir de su configuración.
func contextEvent(j chatJob, cfg hermes.Config) ChatContextEvent {
	prompt, tools := cfg.Footprint()
	ev := ChatContextEvent{
		ChatID: j.chat.ID, NumCtx: j.window.Tokens, PromptTokens: prompt, ToolsTokens: tools,
	}
	if tools > 0 {
		ev.Profile, ev.Auto = resolveToolProfile(j.chat.Tools, j.window.Tokens)
	}
	if j.window.Tokens > 0 {
		ev.OverBudget = float64(prompt+tools) > overBudgetRatio*float64(j.window.Tokens)
	}
	return ev
}

// announceContext emite chat:context y lo recuerda para GetChat.
func (a *App) announceContext(j chatJob, cfg hermes.Config) {
	ev := contextEvent(j, cfg)
	a.chatSvc().setContextInfo(ev)
	a.emit(evChatContext, ev)
}

// usageForParts devuelve el uso para guardarlo en los Parts del mensaje; nil si el
// turno no informó nada (un mensaje sin uso no lleva el campo).
func usageForParts(u hermes.Usage) *hermes.Usage {
	if u == (hermes.Usage{}) {
		return nil
	}
	return &u
}

// SetChatToolProfile fija qué herramientas de ScadLab declara Hermes en ESA
// conversación: "" o "auto" (esenciales si la ventana efectiva es menor de 32 768,
// todas si no), "essential" o "full". Solo vale para servidores de modelos: un CLI
// trae su propio catálogo. Un turno en curso termina con el perfil con que empezó.
func (a *App) SetChatToolProfile(chatID int64, profile string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.SetChatTools(chatID, profile)
}

// summaryMaxChars es el largo máximo del resumen que viaja a la conversación
// nueva (unos 300 tokens). El modelo recibe el pedido de no pasarse; lo que pase
// se recorta.
const summaryMaxChars = 1400

// summaryTimeout acota el pedido del resumen: es una sola vuelta, sin herramientas.
const summaryTimeout = 90 * time.Second

var (
	msgSummaryAsk = i18n.Msg{
		ES: "Resume esta conversación para continuarla en una conversación nueva. Máximo 300 tokens, sin saludos ni código completo. Conserva: qué modelo se está construyendo, las medidas y decisiones tomadas, los archivos .scad importantes (por su nombre) y lo que falta por hacer. Responde solo con el resumen.",
		EN: "Summarize this conversation so it can continue in a new conversation. At most 300 tokens, no greetings and no full code. Keep: what model is being built, the measurements and decisions made, the important .scad files (by name) and what is still to do. Reply with the summary only.",
	}
	msgSummarySystem = i18n.Msg{
		ES: "Eres un asistente que resume conversaciones de diseño 3D con OpenSCAD de forma fiel y breve.",
		EN: "You are an assistant that summarizes 3D design conversations about OpenSCAD faithfully and briefly.",
	}
	msgSeedHeader = i18n.Msg{
		ES: "Resumen de la conversación anterior «%s» (continúo desde aquí):",
		EN: "Summary of the previous conversation “%s” (continuing from here):",
	}
	msgContinuation       = i18n.Msg{ES: "%s (continuación)", EN: "%s (continued)"}
	msgSummaryOnlyServers = i18n.Msg{
		ES: "«Nueva conversación con resumen» es para chats con un servidor de modelos; un CLI guarda su propia sesión",
		EN: "“New conversation with summary” is for chats with a model server; a CLI keeps its own session",
	}
	msgSummaryEmpty = i18n.Msg{ES: "esta conversación no tiene mensajes que resumir", EN: "this conversation has no messages to summarize"}
)

// NewChatWithSummary abre una conversación nueva, en el mismo proyecto, con el
// mismo servidor, modelo, modo y perfil de herramientas, sembrada con un resumen de
// la actual (S17). Es lo que el chat ofrece al pasar el 70 % del contexto: en vez
// de arrastrar —y resumir de a poco— una conversación larga, se continúa desde
// una corta.
//
// El resumen lo escribe el modelo de la conversación en una sola vuelta, sin
// herramientas y con un máximo de ~300 tokens; si el servidor no responde o no
// devuelve nada, es el resumen extractivo (hermes.ExtractiveSummary), que no
// necesita al modelo. El resumen queda como primer mensaje (del usuario) de la
// conversación nueva y se ve en ella: así se sabe exactamente qué se llevó.
func (a *App) NewChatWithSummary(chatID int64) (store.Chat, error) {
	if err := a.ready(); err != nil {
		return store.Chat{}, err
	}
	c, err := a.store.GetChat(chatID)
	if err != nil {
		return store.Chat{}, err
	}
	if running, _, _ := a.chatSvc().mgr.Snapshot(chatID); running {
		return store.Chat{}, chat.ErrBusy
	}
	if strings.HasPrefix(c.ProviderID, "cli:") {
		return store.Chat{}, i18n.New(msgSummaryOnlyServers)
	}
	msgs, err := a.store.ListChatMessages(chatID)
	if err != nil {
		return store.Chat{}, err
	}
	rows := make([]hermes.StoredMessage, len(msgs))
	for i, m := range msgs {
		rows[i] = hermes.StoredMessage{Role: m.Role, Content: m.Content, Parts: m.Parts}
	}
	extractive := hermes.ExtractiveSummary(rows, summaryMaxChars)
	if extractive == "" {
		return store.Chat{}, i18n.New(msgSummaryEmpty)
	}

	summary := a.modelSummary(c, rows)
	if summary == "" {
		summary = extractive
	}
	title := c.Title
	if title == "" {
		title = "…"
	}
	seed := i18n.T(msgSeedHeader, title) + "\n\n" + summary

	nc, err := a.store.CreateChat(c.WorkspaceID, c.ProviderID, c.Model)
	if err != nil {
		return store.Chat{}, err
	}
	// Hereda el modo y el perfil: continuar no cambia cómo se trabaja. Una falla acá
	// no frena nada (la conversación ya existe): queda en los valores de fábrica.
	if err := a.store.SetChatMode(nc.ID, c.Mode); err != nil {
		return store.Chat{}, err
	}
	if err := a.store.SetChatTools(nc.ID, c.Tools); err != nil {
		return store.Chat{}, err
	}
	if t := chat.CleanTitle(i18n.T(msgContinuation, c.Title)); c.Title != "" && t != "" {
		if err := a.store.SetChatTitle(nc.ID, t); err != nil {
			return store.Chat{}, err
		}
	}
	if _, err := a.store.AddChatMessageFull(nc.ID, store.NewChatMessage{Role: "user", Content: seed}); err != nil {
		return store.Chat{}, err
	}
	return a.store.GetChat(nc.ID)
}

// modelSummary le pide al modelo de la conversación un resumen; "" si no se pudo
// (el servidor no responde, la clave falló, devolvió vacío): quien llama cae al
// extractivo.
func (a *App) modelSummary(c store.Chat, rows []hermes.StoredMessage) string {
	p, err := a.chatServer(c.ProviderID)
	if err != nil {
		return ""
	}
	client, err := a.llmClientForTurn(p)
	if err != nil {
		return ""
	}
	window := effectiveWindow(p, a.modelCapabilities(p, client, c.Model, false))
	// Lo que el modelo lee para resumir tiene que caber en su ventana: la mitad,
	// el resto es la instrucción y la respuesta. SeedHistory resume lo que no entra.
	budget := chat.HistoryBudgetFor(p.Kind, window.Tokens) * 3 / 2
	history := hermes.SeedHistory(rows, budget)
	req := llm.ChatRequest{Model: c.Model, NumCtx: window.Tokens}
	req.Messages = append(req.Messages, llm.Message{Role: llm.RoleSystem, Content: i18n.T(msgSummarySystem)})
	req.Messages = append(req.Messages, history...)
	if n := len(req.Messages); n > 1 && req.Messages[n-1].Role == llm.RoleUser {
		req.Messages[n-1].Content += "\n\n" + i18n.T(msgSummaryAsk)
	} else {
		req.Messages = append(req.Messages, llm.Message{Role: llm.RoleUser, Content: i18n.T(msgSummaryAsk)})
	}
	ctx, cancel := context.WithTimeout(a.appCtx(), summaryTimeout)
	defer cancel()
	res, err := client.Chat(ctx, req, nil)
	if err != nil {
		return ""
	}
	text := strings.TrimSpace(res.Message.Content)
	if r := []rune(text); len(r) > summaryMaxChars {
		text = strings.TrimSpace(string(r[:summaryMaxChars])) + "…"
	}
	return text
}
