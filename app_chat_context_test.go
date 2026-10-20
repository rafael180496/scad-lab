package main

import (
	"strings"
	"testing"

	"scadlab/backend/store"
)

// Pruebas de la ventana de contexto, el perfil de herramientas por conversación y
// «Nueva conversación con resumen» (S17).

func TestEffectiveWindow(t *testing.T) {
	ollama := func(ctx int) store.ModelServer {
		return store.ModelServer{Kind: store.ServerOllama, ContextTokens: ctx}
	}
	cases := []struct {
		name  string
		srv   store.ModelServer
		model int
		want  int
		fixed bool
	}{
		{"el servidor la fija", ollama(12000), 131072, 12000, true},
		{"el modelo trae más de 32k: se acota", ollama(0), 131072, 32768, false},
		{"el modelo trae 16k", ollama(0), 16384, 16384, false},
		{"mínimo 8 192", ollama(0), 2048, 8192, false},
		{"el modelo no la dice", ollama(0), 0, chatDefaultNumCtx, false},
		{"OpenAI sin dato: no se sabe", store.ModelServer{Kind: store.ServerOpenAI}, 0, 0, false},
		{"OpenAI con la del proveedor", store.ModelServer{Kind: store.ServerOpenAI}, 128000, 128000, false},
		{"OpenAI con la fijada a mano", store.ModelServer{Kind: store.ServerOpenAI, ContextTokens: 8000}, 128000, 8000, true},
		{"Anthropic", store.ModelServer{Kind: store.ServerAnthropic}, 200000, 200000, false},
	}
	for _, c := range cases {
		got := effectiveWindow(c.srv, modelCaps{ContextTokens: c.model})
		if got.Tokens != c.want || got.Fixed != c.fixed {
			t.Errorf("%s: %+v, esperaba %d (fijada: %v)", c.name, got, c.want, c.fixed)
		}
	}
}

func TestResolveToolProfile(t *testing.T) {
	cases := []struct {
		tools  string
		window int
		want   string
		auto   bool
	}{
		{"", 8192, "essential", true},
		{"", 32767, "essential", true},
		{"", 32768, "full", true},
		{"", 0, "full", true}, // no se sabe: el catálogo completo
		{"", 200000, "full", true},
		{"essential", 200000, "essential", false},
		{"full", 8192, "full", false},
	}
	for _, c := range cases {
		if got, auto := resolveToolProfile(c.tools, c.window); got != c.want || auto != c.auto {
			t.Errorf("resolveToolProfile(%q, %d) = %q, %v; esperaba %q, %v", c.tools, c.window, got, auto, c.want, c.auto)
		}
	}
}

func TestSetChatToolProfileIsPerConversation(t *testing.T) {
	a, wsID, _, _ := hermesApp(t, newSim())
	c1, _ := a.NewChat(wsID, "ollama", capsAgent)
	c2, _ := a.NewChat(wsID, "ollama", capsAgent)
	if c1.Tools != "" {
		t.Fatalf("una conversación nueva nace en Auto: %+v", c1)
	}
	if err := a.SetChatToolProfile(c1.ID, "essential"); err != nil {
		t.Fatal(err)
	}
	list, _ := a.ListChats(wsID)
	got := map[int64]string{}
	for _, c := range list {
		got[c.ID] = c.Tools
	}
	if got[c1.ID] != "essential" || got[c2.ID] != "" {
		t.Errorf("perfiles: %v", got)
	}
	if err := a.SetChatToolProfile(c1.ID, "auto"); err != nil {
		t.Fatal(err)
	}
	if c, _ := a.store.GetChat(c1.ID); c.Tools != "" {
		t.Errorf("auto vuelve a vacío: %+v", c)
	}
	if err := a.SetChatToolProfile(c1.ID, "todas"); err == nil {
		t.Error("un perfil desconocido se rechaza")
	}
	if err := a.SetChatToolProfile(9999, "full"); err == nil {
		t.Error("un chat inexistente falla")
	}
}

// El primer turno anuncia la ventana, el peso del prompt y las herramientas y el perfil; GetChat lo recuerda.
func TestChatContextEventIsAnnouncedAndRemembered(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Hola."}}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	ev.mu.Lock()
	ctx := append([]ChatContextEvent(nil), ev.ctx...)
	ev.mu.Unlock()
	if len(ctx) != 1 {
		t.Fatalf("eventos de contexto: %+v", ctx)
	}
	e := ctx[0]
	if e.ChatID != c.ID || e.NumCtx != 32768 || e.PromptTokens <= 0 || e.ToolsTokens <= 0 || e.Profile != "full" || !e.Auto || e.OverBudget {
		t.Errorf("evento: %+v", e)
	}
	if sim.numCtx[0] != 32768 {
		t.Errorf("num_ctx pedido: %d", sim.numCtx[0])
	}
	v, _ := a.GetChat(c.ID)
	if v.Context == nil || *v.Context != e {
		t.Errorf("GetChat recuerda el contexto: %+v", v.Context)
	}
	// Modo texto: sin herramientas no hay perfil.
	sim2 := newSim()
	sim2.script = []simReply{{text: "```openscad\ncube(1);\n```"}}
	a2, ws2, _, ev2 := hermesApp(t, sim2)
	listModels(t, a2)
	c2, _ := a2.NewChat(ws2, "ollama", capsAgent)
	if _, err := a2.SendChatMessage(c2.ID, "un cubo", "", nil, SendOptions{Agent: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	ev2.wait(t)
	ev2.mu.Lock()
	text := append([]ChatContextEvent(nil), ev2.ctx...)
	ev2.mu.Unlock()
	if len(text) != 1 || text[0].Profile != "" || text[0].ToolsTokens != 0 {
		t.Errorf("en modo texto no hay herramientas ni perfil: %+v", text)
	}
}

func seedConversation(t *testing.T, a *App, chatID int64) {
	t.Helper()
	for _, m := range []struct{ role, text string }{
		{"user", "Hazme una maceta cónica de 90 mm con drenaje."},
		{"assistant", "Listo, creé 002-maceta.scad con 5 agujeros de drenaje.\n\n```openscad\ncube(1);\n```"},
		{"user", "Hazla 10 mm más alta."},
		{"assistant", "Listo: ahora mide 90 mm de alto."},
	} {
		if _, err := a.store.AddChatMessage(chatID, m.role, m.text); err != nil {
			t.Fatal(err)
		}
	}
}

// «Nueva conversación con resumen»: el modelo escribe el resumen y la conversación nueva (mismo proyecto, servidor,
// modelo, modo y perfil) empieza con él; la original queda intacta.
func TestNewChatWithSummaryUsesTheModelSummary(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Maceta cónica de 90 mm con 5 drenajes; archivo 002-maceta.scad; falta el plato."}}
	a, wsID, _, _ := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	seedConversation(t, a, c.ID)
	if err := a.store.SetChatTitle(c.ID, "Una maceta"); err != nil {
		t.Fatal(err)
	}
	_ = a.store.SetChatMode(c.ID, "ask")
	_ = a.SetChatToolProfile(c.ID, "essential")

	nc, err := a.NewChatWithSummary(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if nc.ID == c.ID || nc.WorkspaceID != wsID || nc.ProviderID != c.ProviderID || nc.Model != c.Model || nc.Mode != "ask" || nc.Tools != "essential" {
		t.Errorf("la conversación nueva hereda lo de la anterior: %+v", nc)
	}
	if nc.Title != "Una maceta (continued)" {
		t.Errorf("título: %q", nc.Title)
	}
	msgs, _ := a.store.ListChatMessages(nc.ID)
	if len(msgs) != 1 || msgs[0].Role != "user" || !strings.Contains(msgs[0].Content, "Maceta cónica de 90 mm con 5 drenajes") || !strings.Contains(msgs[0].Content, "“Una maceta”") {
		t.Fatalf("la conversación nueva empieza con el resumen: %+v", msgs)
	}
	// El pedido al modelo: la conversación y la instrucción de resumir, sin herramientas.
	if sim.requests() != 1 || sim.hasTools(0) {
		t.Fatalf("un solo pedido sin herramientas: %d (tools: %v)", sim.requests(), sim.hasTools(0))
	}
	req := sim.text(0)
	if !strings.Contains(req, "Hazme una maceta cónica") || !strings.Contains(req, "Summarize this conversation") {
		t.Errorf("el pedido: %.600s", req)
	}
	if old, _ := a.store.ListChatMessages(c.ID); len(old) != 4 {
		t.Errorf("la conversación original queda intacta: %d mensajes", len(old))
	}

	// La nueva se puede seguir: su historial arranca con el resumen (rol usuario) y el mensaje nuevo se funde con él.
	hist := seedFrom(msgs, "ollama", 8192)
	if len(hist) != 1 || !strings.Contains(hist[0].Content, "Maceta cónica") {
		t.Errorf("historial sembrado: %+v", hist)
	}
}

// Si el servidor no responde o no devuelve nada, el resumen es el extractivo (sin el modelo).
func TestNewChatWithSummaryFallsBackToTheExtractiveOne(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{errStatus: 500, errBody: `{"error":"boom"}`}}
	a, wsID, _, _ := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	seedConversation(t, a, c.ID)

	nc, err := a.NewChatWithSummary(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := a.store.ListChatMessages(nc.ID)
	if len(msgs) != 1 {
		t.Fatalf("mensajes: %+v", msgs)
	}
	body := msgs[0].Content
	for _, want := range []string{"Automatic summary", "Asked: Hazme una maceta cónica", "Answered: Listo, creé 002-maceta.scad con 5 agujeros de drenaje.", "Answered: Listo: ahora mide 90 mm de alto."} {
		if !strings.Contains(body, want) {
			t.Errorf("el resumen extractivo no dice %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "cube(1)") {
		t.Error("el código de los bloques no entra al resumen")
	}
}

func TestNewChatWithSummaryRejectsWhatCannotBeSummarized(t *testing.T) {
	a, wsID, _, _ := hermesApp(t, newSim())
	empty, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.NewChatWithSummary(empty.ID); err == nil || !strings.Contains(err.Error(), "no messages") {
		t.Errorf("sin mensajes no hay qué resumir: %v", err)
	}
	cli, err := a.store.CreateChat(wsID, "cli:claude", "")
	if err != nil {
		t.Fatal(err)
	}
	seedConversation(t, a, cli.ID)
	if _, err := a.NewChatWithSummary(cli.ID); err == nil || !strings.Contains(err.Error(), "model server") {
		t.Errorf("un CLI guarda su propia sesión: %v", err)
	}
	if _, err := a.NewChatWithSummary(9999); err == nil {
		t.Error("un chat inexistente falla")
	}
}
