package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scadlab/backend/detect"
	"scadlab/backend/hermes"
	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/mcp"
	"scadlab/backend/store"
)

// Pruebas de Hermes de punta a punta dentro de la App: un servidor Ollama de
// mentira (el protocolo con más metadatos: capacidades por modelo), la caja de
// herramientas real (con el motor integrado, sin OpenSCAD instalado) o una de
// mentira donde se necesita control fino, y la base y el directorio de datos
// temporales de newTestApp. Nada toca la base real de la app ni abre un slicer.

// --- Servidor Ollama simulado ------------------------------------------------------

type simCall struct {
	Name string
	Args map[string]any
}

type simReply struct {
	text  string
	calls []simCall
	// hold mantiene la conexión abierta hasta que el cliente la corte.
	hold bool
	// errStatus/errBody contestan un error HTTP.
	errStatus int
	errBody   string
}

type ollamaSim struct {
	mu     sync.Mutex
	caps   map[string][]string // /api/show por modelo
	script []simReply
	bodies []map[string]any
	// rejectTools hace que un pedido con herramientas conteste 400 «does not
	// support tools», como Ollama con un modelo que no las tiene.
	rejectTools bool
	served      int

	// S17: ventana de contexto simulada. ctxLen es lo que /api/show dice del modelo
	// (0 = 32768); limit, si es mayor que 0, hace que un /api/chat que pese más de
	// esos tokens (estimados como caracteres/4) conteste 400 «context length exceeded»,
	// como un modelo chico de verdad. tokens y numCtx anotan, por pedido, lo que pesó y
	// el `options.num_ctx` que ScadLab pidió.
	ctxLen int
	limit  int
	tokens []int
	numCtx []int
	over   int
}

func (s *ollamaSim) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_, _ = io.WriteString(w, `{"version":"0.9.0"}`)
		case "/api/tags":
			var names []string
			s.mu.Lock()
			for m := range s.caps {
				names = append(names, `{"name":`+jsonString(m)+`}`)
			}
			s.mu.Unlock()
			_, _ = io.WriteString(w, `{"models":[`+strings.Join(names, ",")+`]}`)
		case "/api/show":
			var req struct{ Model string }
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &req)
			s.mu.Lock()
			caps := s.caps[req.Model]
			s.mu.Unlock()
			b, _ := json.Marshal(map[string]any{"capabilities": caps, "model_info": map[string]any{"llama.context_length": s.contextLength()}})
			_, _ = w.Write(b)
		case "/api/chat":
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			s.mu.Lock()
			s.bodies = append(s.bodies, body)
			weight := simTokens(body)
			s.tokens = append(s.tokens, weight)
			nc := 0
			if opts, ok := body["options"].(map[string]any); ok {
				if v, ok := opts["num_ctx"].(float64); ok {
					nc = int(v)
				}
			}
			s.numCtx = append(s.numCtx, nc)
			if s.limit > 0 && weight > s.limit {
				s.over++
				s.mu.Unlock()
				w.WriteHeader(400)
				_, _ = io.WriteString(w, fmt.Sprintf(`{"error":"context length exceeded: %d tokens > %d"}`, weight, s.limit))
				return
			}
			_, withTools := body["tools"]
			if withTools && s.rejectTools {
				// Un pedido rechazado no gasta una entrada del guion.
				s.mu.Unlock()
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":"registry.ollama.ai/library/tiny:1b does not support tools"}`)
				return
			}
			s.served++
			reply := simReply{text: "(sin guion)"}
			if s.served <= len(s.script) {
				reply = s.script[s.served-1]
			}
			s.mu.Unlock()
			if reply.errStatus != 0 {
				w.WriteHeader(reply.errStatus)
				_, _ = io.WriteString(w, reply.errBody)
				return
			}
			if reply.text != "" {
				half := len(reply.text) / 2
				for _, part := range []string{reply.text[:half], reply.text[half:]} {
					if part != "" {
						_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":`+jsonString(part)+`},"done":false}`+"\n")
						w.(http.Flusher).Flush()
					}
				}
			}
			for _, c := range reply.calls {
				args, _ := json.Marshal(c.Args)
				_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":`+jsonString(c.Name)+`,"arguments":`+string(args)+`}}]},"done":false}`+"\n")
				w.(http.Flusher).Flush()
			}
			if reply.hold {
				<-r.Context().Done()
				return
			}
			_, _ = io.WriteString(w, `{"message":{"role":"assistant","content":""},"done":true,"prompt_eval_count":100,"eval_count":20}`+"\n")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (s *ollamaSim) contextLength() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctxLen > 0 {
		return s.ctxLen
	}
	return 32768
}

// simTokens estima los tokens de un /api/chat como caracteres/4 de lo que un modelo
// recibe de verdad: el texto de los mensajes, los argumentos de las llamadas que
// pidió y las definiciones de herramientas. Es la misma regla que usa ScadLab
// (hermes.EstimateTokens), a propósito: la prueba mide si el turno cabe, no si el
// tokenizador de algún modelo coincide.
func simTokens(body map[string]any) int {
	chars := 0
	for _, m := range body["messages"].([]any) {
		mm := m.(map[string]any)
		c, _ := mm["content"].(string)
		chars += len([]rune(c)) + 4
		if tc, ok := mm["tool_calls"]; ok {
			raw, _ := json.Marshal(tc)
			chars += len([]rune(string(raw)))
		}
	}
	if tools, ok := body["tools"]; ok {
		raw, _ := json.Marshal(tools)
		chars += len([]rune(string(raw)))
	}
	return (chars + 3) / 4
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (s *ollamaSim) requests() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func (s *ollamaSim) body(i int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bodies[i]
}

// messages devuelve los mensajes del pedido i.
func (s *ollamaSim) messages(i int) []map[string]any {
	var out []map[string]any
	for _, m := range s.body(i)["messages"].([]any) {
		out = append(out, m.(map[string]any))
	}
	return out
}

func (s *ollamaSim) hasTools(i int) bool {
	_, has := s.body(i)["tools"]
	return has
}

// imagesIn cuenta las imágenes (base64) de todos los mensajes del pedido i.
func (s *ollamaSim) imagesIn(i int) int {
	n := 0
	for _, m := range s.messages(i) {
		if imgs, ok := m["images"].([]any); ok {
			n += len(imgs)
		}
	}
	return n
}

func (s *ollamaSim) text(i int) string {
	var b strings.Builder
	for _, m := range s.messages(i) {
		c, _ := m["content"].(string)
		b.WriteString(m["role"].(string) + ": " + c + "\n")
	}
	return b.String()
}

const (
	capsAgent = "agent-model"   // completion + tools + vision
	capsBlind = "blind-model"   // completion + tools, sin visión
	capsPlain = "plain-model"   // completion, sin herramientas ni visión
	capsMute  = "unknown-model" // /api/show vacío: no se sabe nada
)

func newSim() *ollamaSim {
	return &ollamaSim{caps: map[string][]string{
		capsAgent: {"completion", "tools", "vision"},
		capsBlind: {"completion", "tools"},
		capsPlain: {"completion"},
		capsMute:  nil,
	}}
}

// --- Eventos -----------------------------------------------------------------------

type hermesEvents struct {
	mu     sync.Mutex
	text   map[int64]string
	steps  []ChatStepEvent
	usage  []ChatUsageEvent
	ctx    []ChatContextEvent
	done   chan ChatDoneEvent
	errs   chan ChatErrorEvent
	nsteps int
}

func captureHermesEvents(a *App) *hermesEvents {
	ev := &hermesEvents{text: map[int64]string{}, done: make(chan ChatDoneEvent, 8), errs: make(chan ChatErrorEvent, 8)}
	a.emitter = func(_ string, payload any) {
		ev.mu.Lock()
		defer ev.mu.Unlock()
		switch p := payload.(type) {
		case ChatDeltaEvent:
			ev.text[p.ChatID] += p.Text
		case ChatStepEvent:
			ev.steps = append(ev.steps, p)
		case ChatUsageEvent:
			ev.usage = append(ev.usage, p)
		case ChatContextEvent:
			ev.ctx = append(ev.ctx, p)
		case ChatDoneEvent:
			ev.done <- p
		case ChatErrorEvent:
			ev.errs <- p
		}
	}
	return ev
}

func (e *hermesEvents) wait(t *testing.T) ChatDoneEvent {
	t.Helper()
	select {
	case d := <-e.done:
		return d
	case er := <-e.errs:
		t.Fatalf("llegó chat:error: %+v", er)
	case <-time.After(60 * time.Second):
		t.Fatal("el turno nunca terminó")
	}
	return ChatDoneEvent{}
}

// hermesApp deja una App con un workspace (con un checkpoint) y el servidor
// «ollama» apuntando al simulador.
func hermesApp(t *testing.T, sim *ollamaSim) (*App, int64, string, *hermesEvents) {
	t.Helper()
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang("en") })
	a, wsID, dir := editorWorkspace(t)
	srv := sim.server(t)
	p, err := a.store.GetModelServer("ollama")
	if err != nil {
		t.Fatal(err)
	}
	p.BaseURL, p.DefaultModel, p.Enabled = srv.URL, capsAgent, true
	if _, err := a.SaveModelServer(p, "", false); err != nil {
		t.Fatal(err)
	}
	return a, wsID, dir, captureHermesEvents(a)
}

// listModels llena la lista guardada del servidor (lo que hace el selector del
// chat al abrirse), para que las capacidades salgan de ahí.
func listModels(t *testing.T, a *App) {
	t.Helper()
	if _, err := a.ListServerModels("ollama", true); err != nil {
		t.Fatal(err)
	}
}

// --- El ciclo completo con la caja real ------------------------------------------

const (
	srcBroken = "color(\"red\") cube([40, 40, 20);\n"
	srcFixed  = "$fn = 32;\ncolor(\"red\") cube([40, 40, 20]);\n"
)

// Crear → render con error de línea → crear → render ok → preview → texto final,
// con la caja de herramientas REAL (el motor integrado renderiza), los eventos
// de paso y de uso, y lo guardado en la base.
func TestHermesAgentCycleWithRealToolbox(t *testing.T) {
	t.Setenv(detect.NoSystemOpenSCADEnv, "1")
	sim := newSim()
	sim.script = []simReply{
		{text: "Voy a crear la caja.", calls: []simCall{{"create_checkpoint", map[string]any{"label": "caja", "description": "Caja", "source": srcBroken}}}},
		{calls: []simCall{{"render", map[string]any{"file": "002-caja.scad"}}}},
		{calls: []simCall{{"create_checkpoint", map[string]any{"parent": "002-caja.scad", "label": "caja v2", "source": srcFixed}}}},
		{calls: []simCall{{"render", map[string]any{"file": "003-caja-v2.scad"}}}},
		{calls: []simCall{{"preview", map[string]any{"file": "003-caja-v2.scad"}}}},
		{text: "Listo: la caja mide 40 × 40 × 20 mm."},
	}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	c, err := a.NewChat(wsID, "ollama", capsAgent)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendChatMessage(c.ID, "haz una caja de 40 mm", "001-base.scad", nil, SendOptions{Agent: boolp(true), MaxSteps: 8}); err != nil {
		t.Fatal(err)
	}
	d := ev.wait(t)
	if d.Cancelled || d.MessageID == 0 {
		t.Fatalf("done: %+v", d)
	}

	// Los checkpoints existen en el disco: el modelo trabajó de verdad.
	for _, f := range []string{"002-caja.scad", "003-caja-v2.scad"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("falta %s: %v", f, err)
		}
	}
	if sim.requests() != 6 {
		t.Fatalf("pedidos: %d", sim.requests())
	}
	// Con herramientas declaradas y el prompt del agente (con el código abierto).
	if !sim.hasTools(0) {
		t.Error("el modelo debía recibir las herramientas")
	}
	if sys := sim.text(0); !strings.Contains(sys, "Hermes") || !strings.Contains(sys, "001-base.scad") {
		t.Errorf("prompt de sistema: %.300q", sys)
	}
	// La imagen del preview vuelve en el pedido siguiente (y no antes).
	if sim.imagesIn(4) != 0 || sim.imagesIn(5) != 1 {
		t.Errorf("imágenes por pedido: antes %d, después %d", sim.imagesIn(4), sim.imagesIn(5))
	}
	// El render con error trajo la línea al modelo.
	if tx := sim.text(2); !strings.Contains(tx, "error:") || !strings.Contains(tx, "line") {
		t.Errorf("el error del render no llegó al modelo: %.400q", tx)
	}

	// Eventos de paso: 5 pasos, cada uno running + final.
	ev.mu.Lock()
	steps, usage := append([]ChatStepEvent(nil), ev.steps...), append([]ChatUsageEvent(nil), ev.usage...)
	ev.mu.Unlock()
	if len(steps) != 10 {
		t.Fatalf("eventos de paso: %d", len(steps))
	}
	final := map[string]hermes.Step{}
	for _, e := range steps {
		if e.ChatID != c.ID {
			t.Errorf("chat del evento: %d", e.ChatID)
		}
		final[e.Step.ID] = e.Step
	}
	var tools []string
	var errors int
	for _, e := range steps {
		if e.Step.Status != hermes.StepRunning {
			tools = append(tools, e.Step.Tool)
			if e.Step.Status == hermes.StepError {
				errors++
			}
		}
	}
	if strings.Join(tools, ",") != "create_checkpoint,render,create_checkpoint,render,preview" || errors != 1 {
		t.Errorf("pasos: %v (con error: %d)", tools, errors)
	}
	if len(usage) != 6 || usage[5].InputTokens != 600 || usage[5].OutputTokens != 120 || usage[5].CostUSD != 0 {
		t.Errorf("uso: %+v", usage)
	}

	// Lo guardado: el mensaje del asistente trae los pasos (con la imagen
	// reducida a JPEG), los tokens y el modelo.
	v, err := a.GetChat(c.ID)
	if err != nil || len(v.Messages) != 2 || v.Running {
		t.Fatalf("GetChat: %+v, %v", v, err)
	}
	m := v.Messages[1]
	if m.ID != d.MessageID || m.InputTokens != 600 || m.OutputTokens != 120 || m.CostUSD != 0 {
		t.Errorf("mensaje: %+v", m)
	}
	if want := "Voy a crear la caja.\n\nListo: la caja mide 40 × 40 × 20 mm."; m.Content != want {
		t.Errorf("texto guardado: %q", m.Content)
	}
	parts := hermes.ParseParts(m.Parts)
	if len(parts.Steps) != 5 || parts.Model == "" {
		t.Fatalf("partes: %d pasos", len(parts.Steps))
	}
	if parts.Steps[1].Status != hermes.StepError || parts.Steps[4].Status != hermes.StepOK {
		t.Errorf("estados guardados: %+v", parts.Steps)
	}
	if len(parts.Steps[4].Images) != 1 || !strings.HasPrefix(parts.Steps[4].Images[0], "data:image/jpeg;base64,") {
		t.Errorf("la imagen del preview debía guardarse reducida: %.40s", fmt.Sprint(parts.Steps[4].Images))
	}
	if parts.Steps[0].TextAt != len("Voy a crear la caja.") {
		t.Errorf("textAt: %d", parts.Steps[0].TextAt)
	}
	// Las imágenes temporales de la herramienta no quedan en el disco.
	if left, _ := os.ReadDir(a.chatDir(c.ID)); len(left) != 0 {
		t.Errorf("quedaron archivos del turno: %v", left)
	}
}

// Sin «Agente» es el chat de texto de S5: sin herramientas y con el prompt de
// respaldo.
func TestHermesTextModeSendsNoTools(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "```openscad\ncube(10);\n```"}}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "un cubo", "", nil, SendOptions{Agent: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if sim.hasTools(0) || strings.Contains(sim.text(0), "Hermes") {
		t.Errorf("el modo texto no declara herramientas: tools=%v", sim.hasTools(0))
	}
	if v, _ := a.GetChat(c.ID); len(v.Messages) != 2 || hermes.ParseParts(v.Messages[1].Parts).Steps != nil {
		t.Errorf("mensajes: %+v", v.Messages)
	}
}

// Un modelo cuyas capacidades dicen que no llama herramientas: «Agente» pedido,
// modo texto con aviso (sin gastar un pedido en fallar).
func TestHermesKnownNoToolsDowngradesWithNotice(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Aquí tienes el código."}}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsPlain)
	if _, err := a.SendChatMessage(c.ID, "un cubo", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if sim.requests() != 1 || sim.hasTools(0) {
		t.Fatalf("pedidos %d, herramientas %v", sim.requests(), sim.hasTools(0))
	}
	v, _ := a.GetChat(c.ID)
	if !strings.Contains(v.Messages[1].Content, "does not support tools") || !strings.Contains(v.Messages[1].Content, "Aquí tienes") {
		t.Errorf("respuesta: %q", v.Messages[1].Content)
	}
}

// Un modelo que no declara sus capacidades pero rechaza las herramientas cae al
// modo texto con aviso; el servidor se recuerda y el turno siguiente ni lo intenta.
func TestHermesToolsRejectedFallsBackAndIsRemembered(t *testing.T) {
	sim := newSim()
	sim.rejectTools = true
	sim.script = []simReply{{text: "Va en texto."}, {text: "Otra vez en texto."}}
	a, wsID, _, ev := hermesApp(t, sim)
	c, _ := a.NewChat(wsID, "ollama", capsMute)
	send := func(text string) {
		t.Helper()
		if _, err := a.SendChatMessage(c.ID, text, "", nil, SendOptions{Agent: boolp(true)}); err != nil {
			t.Fatal(err)
		}
		ev.wait(t)
	}
	send("uno")
	// Primer pedido con herramientas (rechazado) y reintento sin ellas.
	if sim.requests() != 2 || !sim.hasTools(0) || sim.hasTools(1) {
		t.Fatalf("pedidos %d: herramientas %v/%v", sim.requests(), sim.hasTools(0), sim.hasTools(1))
	}
	v, _ := a.GetChat(c.ID)
	if !strings.Contains(v.Messages[1].Content, "does not support tools") || !strings.Contains(v.Messages[1].Content, "Va en texto.") {
		t.Errorf("respuesta: %q", v.Messages[1].Content)
	}
	send("dos")
	// El segundo turno no vuelve a intentarlo: un solo pedido más, sin herramientas.
	if sim.requests() != 3 || sim.hasTools(2) {
		t.Errorf("pedidos %d, herramientas en el 3º: %v", sim.requests(), sim.hasTools(2))
	}
}

// Cancelar a mitad de una herramienta: el paso en curso aparece en GetChat y, al
// cancelar, el turno guarda lo hecho y avisa que se canceló.
func TestHermesCancelMidToolKeepsTheStep(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Renderizando.", calls: []simCall{{"render", map[string]any{"file": "001-base.scad"}}}}}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	started := make(chan struct{})
	prev := newChatToolset
	newChatToolset = func(*App, store.Workspace) (hermes.Toolset, error) {
		return blockingTools{started: started}, nil
	}
	t.Cleanup(func() { newChatToolset = prev })
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "renderiza", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("la herramienta no arrancó")
	}
	// Mientras corre: GetChat devuelve el paso en curso y el texto parcial.
	v, _ := a.GetChat(c.ID)
	if !v.Running || len(v.Steps) != 1 || v.Steps[0].Status != hermes.StepRunning || v.Steps[0].Tool != "render" {
		t.Fatalf("GetChat en curso: running=%v pasos=%+v", v.Running, v.Steps)
	}
	a.CancelChat(c.ID)
	d := ev.wait(t)
	if !d.Cancelled || d.MessageID == 0 {
		t.Fatalf("done: %+v", d)
	}
	v, _ = a.GetChat(c.ID)
	if v.Running || len(v.Steps) != 0 || len(v.Messages) != 2 {
		t.Fatalf("tras cancelar: %+v", v)
	}
	parts := hermes.ParseParts(v.Messages[1].Parts)
	if v.Messages[1].Content != "Renderizando." || len(parts.Steps) != 1 || parts.Steps[0].Status != hermes.StepError {
		t.Errorf("lo hecho se conserva: %q %+v", v.Messages[1].Content, parts.Steps)
	}
	if sim.requests() != 1 {
		t.Errorf("no se debe volver al modelo: %d pedidos", sim.requests())
	}
}

// blockingTools tiene una herramienta, `render`, que no termina hasta que la cancelan.
type blockingTools struct{ started chan struct{} }

func (b blockingTools) Tools() []mcp.ToolSpec {
	return []mcp.ToolSpec{{Name: "render", Description: "r", InputSchema: map[string]any{"type": "object"}}}
}

func (b blockingTools) Call(ctx context.Context, _ string, _ map[string]any) (mcp.ToolOutput, error) {
	close(b.started)
	<-ctx.Done()
	return mcp.ToolOutput{}, ctx.Err()
}

// El tope de pasos que pide la interfaz llega al bucle (y se acota a 1–30).
func TestHermesStepLimitFromOptions(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{
		{calls: []simCall{{"list_checkpoints", map[string]any{}}}},
		{calls: []simCall{{"list_checkpoints", map[string]any{}}}},
		{text: "No llegué más lejos."},
	}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "mira todo", "", nil, SendOptions{Agent: boolp(true), MaxSteps: 1}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	v, _ := a.GetChat(c.ID)
	parts := hermes.ParseParts(v.Messages[1].Parts)
	if len(parts.Steps) != 1 || !parts.StepLimit {
		t.Fatalf("pasos %d, tope %v", len(parts.Steps), parts.StepLimit)
	}
	if !strings.Contains(v.Messages[1].Content, "limit of 1 steps") || !strings.Contains(v.Messages[1].Content, "No llegué más lejos.") {
		t.Errorf("texto: %q", v.Messages[1].Content)
	}
	// El prompt anunció el tope pedido.
	if sys := sim.text(0); !strings.Contains(sys, "at most **1 steps**") && !strings.Contains(sys, "1 steps") {
		t.Errorf("el prompt no anunció el tope: %.200q", sys)
	}
}

// --- Retomar tras reabrir -----------------------------------------------------------

// Tras cerrar y volver a abrir la app (otra App sobre la misma base) la
// conversación sigue: el historial guardado —con el informe del modelo de
// referencia adjunto— viaja al modelo.
func TestHermesSeedHistoryAfterRestart(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Medí tu soporte: 30 mm."}, {text: "Sigo con el soporte."}}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)

	att, err := a.AddChatAttachmentData(wsID, "mi soporte.stl", b64(asciiSTL()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendChatMessage(c.ID, "mide esto", "", []string{att.ID}, SendOptions{Agent: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)

	// «Reinicio»: otra App con la misma base y los mismos datos, sin nada en memoria.
	b := &App{ctx: context.Background(), store: a.store, dataDir: a.dataDir, engine: a.engine}
	t.Cleanup(func() { chatServices.Delete(b) })
	captureHermesEvents(b)
	ev2 := captureHermesEvents(b)
	if _, err := b.SendChatMessage(c.ID, "ahora hazle un agujero", "", nil, SendOptions{Agent: boolp(false)}); err != nil {
		t.Fatal(err)
	}
	ev2.wait(t)
	tx := sim.text(1)
	for _, want := range []string{"user: mide esto", "refs/mi-soporte.stl", "assistant: Medí tu soporte: 30 mm.", "user: ahora hazle un agujero"} {
		if !strings.Contains(tx, want) {
			t.Errorf("falta %q en el historial reenviado:\n%s", want, tx)
		}
	}
	if sim.imagesIn(1) != 0 {
		t.Error("el historial no reenvía imágenes")
	}
	// La referencia sigue en refs/ aunque el chat se haya reabierto.
	if _, err := os.Stat(filepath.Join(dir, "refs", "mi-soporte.stl")); err != nil {
		t.Errorf("refs/: %v", err)
	}
}

// --- Adjuntos -----------------------------------------------------------------------

func b64(raw []byte) string { return base64.StdEncoding.EncodeToString(raw) }

// asciiSTL es un cubo de 30 mm como STL ASCII (12 triángulos).
func asciiSTL() []byte {
	var b strings.Builder
	b.WriteString("solid cubo\n")
	v := [8][3]float64{{0, 0, 0}, {30, 0, 0}, {30, 30, 0}, {0, 30, 0}, {0, 0, 30}, {30, 0, 30}, {30, 30, 30}, {0, 30, 30}}
	tris := [][3]int{{0, 2, 1}, {0, 3, 2}, {4, 5, 6}, {4, 6, 7}, {0, 1, 5}, {0, 5, 4}, {1, 2, 6}, {1, 6, 5}, {2, 3, 7}, {2, 7, 6}, {3, 0, 4}, {3, 4, 7}}
	for _, t := range tris {
		b.WriteString("facet normal 0 0 0\n outer loop\n")
		for _, i := range t {
			fmt.Fprintf(&b, "  vertex %g %g %g\n", v[i][0], v[i][1], v[i][2])
		}
		b.WriteString(" endloop\nendfacet\n")
	}
	b.WriteString("endsolid cubo\n")
	return []byte(b.String())
}

func pngB64(t *testing.T) string {
	t.Helper()
	// Un PNG 1×1 válido.
	raw, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	return b64(raw)
}

// Adjuntar un STL lo copia a refs/, lo analiza (el informe y la miniatura salen
// ya en la ficha), y al enviar el informe entra al mensaje que lee el modelo.
func TestAttachSTLCopiesToRefsAndSummaryEntersThePrompt(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Entendido."}}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)

	att, err := a.AddChatAttachmentData(wsID, "mi soporte.stl", b64(asciiSTL()))
	if err != nil {
		t.Fatal(err)
	}
	if att.Kind != hermes.KindModel || att.Name != "mi soporte.stl" || att.RefPath != "refs/mi-soporte.stl" || att.ID == "" {
		t.Fatalf("ficha: %+v", att)
	}
	if !strings.Contains(att.Summary, "30.00") || !strings.Contains(att.Summary, "refs/mi-soporte.stl") || !strings.Contains(att.Summary, "12") {
		t.Errorf("resumen: %s", att.Summary)
	}
	if !strings.HasPrefix(att.ThumbDataURL, "data:image/jpeg;base64,") {
		t.Errorf("miniatura: %.40s", att.ThumbDataURL)
	}
	if _, err := os.Stat(filepath.Join(dir, "refs", "mi-soporte.stl")); err != nil {
		t.Fatalf("la referencia debía copiarse a refs/: %v", err)
	}

	// Adjuntar otra con el mismo nombre no pisa la primera (-2).
	again, err := a.AddChatAttachmentData(wsID, "mi soporte.stl", b64(asciiSTL()))
	if err != nil || again.RefPath != "refs/mi-soporte-2.stl" {
		t.Fatalf("segundo adjunto: %+v, %v", again, err)
	}
	if err := a.RemoveChatAttachment(again.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "refs", "mi-soporte-2.stl")); !os.IsNotExist(err) {
		t.Error("quitar un adjunto sin enviar borra su copia")
	}
	if _, err := os.Stat(filepath.Join(dir, "refs", "mi-soporte.stl")); err != nil {
		t.Error("…pero no la de otro")
	}

	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	um, err := a.SendChatMessage(c.ID, "haz un estante para esto", "", []string{att.ID}, SendOptions{Agent: boolp(false)})
	if err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if um.Content != "haz un estante para esto" {
		t.Errorf("lo que se guarda como texto es lo que escribió la persona: %q", um.Content)
	}
	// El modelo recibe el texto, el informe y (ve) la vista previa del modelo.
	last := sim.messages(0)
	user := last[len(last)-1]
	content := user["content"].(string)
	if !strings.Contains(content, "haz un estante para esto") || !strings.Contains(content, "Reference: refs/mi-soporte.stl") || !strings.Contains(content, "import(") {
		t.Errorf("mensaje al modelo: %.500q", content)
	}
	if sim.imagesIn(0) != 1 {
		t.Errorf("con visión viaja la vista previa del modelo: %d imágenes", sim.imagesIn(0))
	}
	// El prompt de sistema lista la referencia.
	// (modo texto: el prompt de S5 no la nombra; en modo agente sí — ver abajo)

	// Lo guardado en el mensaje del usuario: la ficha sin bytes.
	v, _ := a.GetChat(c.ID)
	p := hermes.ParseParts(v.Messages[0].Parts)
	if len(p.Attachments) != 1 || p.Attachments[0].RefPath != "refs/mi-soporte.stl" || p.Attachments[0].Thumb == "" || p.Attachments[0].Summary == "" {
		t.Errorf("partes del usuario: %+v", p.Attachments)
	}
	// Enviado, el adjunto deja de estar pendiente: borrarlo ya no toca refs/.
	_ = a.RemoveChatAttachment(att.ID)
	if _, err := os.Stat(filepath.Join(dir, "refs", "mi-soporte.stl")); err != nil {
		t.Error("una referencia enviada se queda en refs/")
	}
}

// En modo agente el prompt de sistema lista las referencias adjuntas.
func TestAgentPromptListsAttachedReferences(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Listo."}}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	att, err := a.AddChatAttachmentData(wsID, "pieza.stl", b64(asciiSTL()))
	if err != nil {
		t.Fatal(err)
	}
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "cópiala", "", []string{att.ID}, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if sys := sim.text(0); !strings.Contains(sys, "`refs/pieza.stl`") {
		t.Errorf("el prompt de sistema no lista la referencia: %.300q", sys)
	}
}

// Una imagen con un modelo que informó que no ve se rechaza antes de mandar nada:
// no se guarda el mensaje, no se arma el turno y el adjunto queda para elegir otro modelo.
func TestImageWithBlindModelIsRejectedBeforeSending(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "No debería llegar."}}
	a, wsID, _, _ := hermesApp(t, sim)
	listModels(t, a)
	att, err := a.AddChatAttachmentData(wsID, "foto.png", pngB64(t))
	if err != nil {
		t.Fatal(err)
	}
	if att.Kind != hermes.KindImage || att.ThumbDataURL == "" || att.RefPath != "" {
		t.Fatalf("ficha: %+v", att)
	}
	c, _ := a.NewChat(wsID, "ollama", capsBlind)
	_, err = a.SendChatMessage(c.ID, "copia esta foto", "", []string{att.ID}, SendOptions{Agent: boolp(true)})
	if err == nil || !strings.Contains(err.Error(), "cannot see images") || !strings.Contains(err.Error(), "foto.png") {
		t.Fatalf("debía rechazarse: %v", err)
	}
	if sim.requests() != 0 {
		t.Error("no debía llegar ningún pedido al modelo")
	}
	if v, _ := a.GetChat(c.ID); len(v.Messages) != 0 || v.Running {
		t.Errorf("no se guarda nada: %+v", v)
	}
	// El adjunto sigue pendiente: con un modelo que ve, se envía.
	if err := a.SetChatModel(c.ID, "ollama", capsAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendChatMessage(c.ID, "copia esta foto", "", []string{att.ID}, SendOptions{Agent: boolp(false)}); err != nil {
		t.Fatalf("con visión debía enviarse: %v", err)
	}
}

// Con un modelo del que no se sabe si ve, la imagen se manda igual (un «no sé» no bloquea).
func TestImageWithUnknownVisionIsSentAnyway(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "La veo."}}
	a, wsID, dir, ev := hermesApp(t, sim)
	listModels(t, a)
	att, _ := a.AddChatAttachmentData(wsID, "foto.png", pngB64(t))
	c, _ := a.NewChat(wsID, "ollama", capsMute)
	if _, err := a.SendChatMessage(c.ID, "mírala", "", []string{att.ID}, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if sim.imagesIn(0) != 1 {
		t.Errorf("imágenes: %d", sim.imagesIn(0))
	}
	// La imagen pasó a la carpeta del chat y salió de la de preparación.
	if entries, _ := os.ReadDir(a.chatDir(c.ID)); len(entries) != 1 {
		t.Errorf("carpeta del chat: %v", entries)
	}
	if entries, _ := os.ReadDir(a.stagingDir()); len(entries) != 0 {
		t.Errorf("preparación: %v", entries)
	}
	_ = dir
	// Borrar el chat borra sus imágenes.
	if err := a.DeleteChat(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a.chatDir(c.ID)); !os.IsNotExist(err) {
		t.Error("la carpeta del chat borrado debía irse")
	}
}

// Sin visión, un modelo 3D adjunto sí se manda: solo el informe, sin la vista previa.
func TestModelAttachmentWithBlindModelSendsOnlyTheReport(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Ok."}}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	att, err := a.AddChatAttachmentData(wsID, "pieza.stl", b64(asciiSTL()))
	if err != nil {
		t.Fatal(err)
	}
	c, _ := a.NewChat(wsID, "ollama", capsBlind)
	if _, err := a.SendChatMessage(c.ID, "", "", []string{att.ID}, SendOptions{}); err != nil {
		t.Fatalf("sin texto y con adjunto debe poder enviarse: %v", err)
	}
	ev.wait(t)
	if sim.imagesIn(0) != 0 {
		t.Error("un modelo ciego no recibe la vista previa")
	}
	user := sim.messages(0)
	if c := user[len(user)-1]["content"].(string); !strings.Contains(c, "Take a look at the attached files.") || !strings.Contains(c, "refs/pieza.stl") {
		t.Errorf("mensaje: %.300q", c)
	}
	if v, _ := a.GetChat(c.ID); v.Chat.Title != "pieza.stl" {
		t.Errorf("sin texto, el título sale del adjunto: %q", v.Chat.Title)
	}
}

// Los adjuntos que no sirven se rechazan con un motivo y sin dejar nada.
func TestAttachmentValidation(t *testing.T) {
	sim := newSim()
	a, wsID, dir, _ := hermesApp(t, sim)
	cases := []struct {
		name, data, want string
	}{
		{"nota.pdf", b64([]byte("%PDF-1.4 hola")), "cannot be attached"},
		{"nota.txt", b64([]byte("texto suelto")), "cannot be attached"},
		{"vacio.stl", "", "empty"},
		{"roto.stl", b64([]byte("esto no es un STL")), "could not read"},
		{"falso.3mf", b64([]byte("no es un zip")), "not a valid 3MF"},
		// Más de 5 MB ya no es «demasiado grande» (se reduce al adjuntar); más de
		// 20 MB, sí. Y una imagen que no se puede leer no se adjunta.
		{"enorme.png", b64(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, hermes.MaxImageInputBytes+10)...)), "maximum size"},
		{"rota.png", b64(append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, hermes.MaxImageBytes+10)...)), "could not prepare the image"},
		{"mal.png", "@@@no es base64", "could not be decoded"},
	}
	for _, c := range cases {
		_, err := a.AddChatAttachmentData(wsID, c.name, c.data)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v (esperaba %q)", c.name, err, c.want)
		}
	}
	// Un STL roto no deja su copia en refs/.
	if entries, _ := os.ReadDir(filepath.Join(dir, "refs")); len(entries) != 0 {
		t.Errorf("refs/ debía quedar vacío: %v", entries)
	}
	if _, err := a.AddChatAttachmentData(99999, "foto.png", pngB64(t)); err == nil {
		t.Error("workspace inexistente")
	}
	// Un adjunto que no existe o es de otro proyecto no se envía.
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "hola", "", []string{"att-inventado"}, SendOptions{}); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("adjunto desconocido: %v", err)
	}
	other, err := a.CreateWorkspace("Otro", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	att, _ := a.AddChatAttachmentData(other.ID, "foto.png", pngB64(t))
	if _, err := a.SendChatMessage(c.ID, "hola", "", []string{att.ID}, SendOptions{}); err == nil || !strings.Contains(err.Error(), "another project") {
		t.Errorf("adjunto de otro proyecto: %v", err)
	}
	// Más de 10 adjuntos.
	many := make([]string, hermes.MaxAttachments+1)
	for i := range many {
		many[i] = fmt.Sprintf("att-%d", i)
	}
	if _, err := a.SendChatMessage(c.ID, "hola", "", many, SendOptions{}); err == nil || !strings.Contains(err.Error(), "up to 10") {
		t.Errorf("demasiados adjuntos: %v", err)
	}
}

// AddChatAttachment (por ruta, la del diálogo del sistema) clasifica por contenido.
func TestAddChatAttachmentByPath(t *testing.T) {
	sim := newSim()
	a, wsID, _, _ := hermesApp(t, sim)
	src := t.TempDir()
	stl := filepath.Join(src, "base.stl")
	if err := os.WriteFile(stl, asciiSTL(), 0o644); err != nil {
		t.Fatal(err)
	}
	att, err := a.AddChatAttachment(wsID, stl)
	if err != nil || att.Kind != hermes.KindModel || att.RefPath != "refs/base.stl" {
		t.Fatalf("stl: %+v, %v", att, err)
	}
	// El original no se toca.
	if _, err := os.Stat(stl); err != nil {
		t.Error("el archivo original debía quedarse")
	}
	// Un PNG con extensión de modelo es una imagen: el contenido manda.
	raw, _ := base64.StdEncoding.DecodeString(pngB64(t))
	disguised := filepath.Join(src, "foto.stl")
	_ = os.WriteFile(disguised, raw, 0o644)
	img, err := a.AddChatAttachment(wsID, disguised)
	if err != nil || img.Kind != hermes.KindImage {
		t.Fatalf("png disfrazado: %+v, %v", img, err)
	}
	if _, err := a.AddChatAttachment(wsID, filepath.Join(src, "no-existe.stl")); err == nil {
		t.Error("archivo inexistente")
	}
	if _, err := a.AddChatAttachment(wsID, src); err == nil {
		t.Error("una carpeta no se adjunta")
	}
	empty := filepath.Join(src, "vacio.png")
	_ = os.WriteFile(empty, nil, 0o644)
	if _, err := a.AddChatAttachment(wsID, empty); err == nil {
		t.Error("archivo vacío")
	}
}

// Al cerrar la app se borran las copias de refs/ de lo que quedó sin enviar.
func TestPendingAttachmentsAreDiscardedOnShutdown(t *testing.T) {
	sim := newSim()
	a, wsID, dir, _ := hermesApp(t, sim)
	if _, err := a.AddChatAttachmentData(wsID, "pieza.stl", b64(asciiSTL())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "refs", "pieza.stl")); err != nil {
		t.Fatal(err)
	}
	a.shutdown(context.Background())
	if _, err := os.Stat(filepath.Join(dir, "refs", "pieza.stl")); !os.IsNotExist(err) {
		t.Error("lo pendiente no se queda en refs/ al cerrar")
	}
}

// --- Conversaciones --------------------------------------------------------------

func TestRenameChatAndSetChatModel(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "Va."}}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	// Un segundo servidor, para cambiar de uno a otro a mitad de camino.
	sim2 := newSim()
	sim2.script = []simReply{{text: "Soy el segundo."}}
	srv2 := sim2.server(t)
	saved, err := a.SaveModelServer(store.ModelServer{Kind: store.ServerOllama, Name: "Taller", BaseURL: srv2.URL, Enabled: true, DefaultModel: capsBlind}, "", false)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)

	if err := a.RenameChat(c.ID, "   Mi   caja \n de Raspberry  "); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.store.GetChat(c.ID); got.Title != "Mi caja de Raspberry" {
		t.Errorf("título: %q", got.Title)
	}
	long := strings.Repeat("ñ", 300)
	if err := a.RenameChat(c.ID, long); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.store.GetChat(c.ID); len([]rune(got.Title)) != 120 {
		t.Errorf("máx. 120 caracteres: %d", len([]rune(got.Title)))
	}
	if err := a.RenameChat(c.ID, " \n\t "); err == nil {
		t.Error("un nombre vacío se rechaza")
	}
	if got, _ := a.store.GetChat(c.ID); len([]rune(got.Title)) != 120 {
		t.Error("el rechazo no cambia el título")
	}
	if err := a.RenameChat(99999, "x"); err == nil {
		t.Error("chat inexistente")
	}

	// Cambiar de modelo a mitad: el historial se conserva y el turno siguiente va al otro servidor.
	if err := a.SetChatModel(c.ID, saved.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.store.GetChat(c.ID); got.ProviderID != saved.ID || got.Model != capsBlind {
		t.Errorf("chat: %+v", got)
	}
	if lm, _ := a.GetLastModel(wsID); lm.ServerID != saved.ID || lm.Model != capsBlind {
		t.Errorf("último modelo: %+v", lm)
	}
	if _, err := a.SendChatMessage(c.ID, "sigue", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	if sim2.requests() != 1 || !strings.Contains(sim2.text(0), "user: hola") || !strings.Contains(sim2.text(0), "assistant: Va.") {
		t.Errorf("el segundo servidor debía recibir el historial: %d pedidos\n%s", sim2.requests(), sim2.text(0))
	}
	if err := a.SetChatModel(c.ID, "nope", "x"); err == nil {
		t.Error("servidor inexistente")
	}
	if err := a.SetChatModel(99999, saved.ID, "x"); err == nil {
		t.Error("chat inexistente")
	}
	if err := a.SetChatModel(c.ID, "ollama", " "); err != nil {
		t.Fatalf("modelo vacío usa el predeterminado del servidor: %v", err)
	}
	if got, _ := a.store.GetChat(c.ID); got.Model != capsAgent {
		t.Errorf("predeterminado: %q", got.Model)
	}
}

// El costo se calcula solo con precios conocidos (OpenRouter los publica; un
// Ollama local, no): sin precios queda en 0, que la interfaz lee como «no informado».
func TestUsageCostUsesKnownPricesOnly(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{{text: "uno"}}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	m, _ := a.store.ListChatMessages(c.ID)
	if m[1].InputTokens != 100 || m[1].OutputTokens != 20 || m[1].CostUSD != 0 {
		t.Errorf("uso de un servidor sin precios: %+v", m[1])
	}
	// Con precios publicados (se siembra la lista como si la hubiera dado OpenRouter).
	svc := a.serverSvc()
	srv, _ := a.store.GetModelServer("ollama")
	svc.mu.Lock()
	svc.models["ollama"] = modelsEntry{list: []ModelInfo{{ID: capsAgent, Tools: true, ToolsKnown: true, Vision: true, VisionKnown: true, PromptPricePerM: 3, CompletionPricePerM: 15}}, stamp: srv.UpdatedAt, at: time.Now()}
	svc.mu.Unlock()
	sim.mu.Lock()
	sim.script = append(sim.script, simReply{text: "dos"})
	sim.mu.Unlock()
	if _, err := a.SendChatMessage(c.ID, "otra", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	ev.wait(t)
	m, _ = a.store.ListChatMessages(c.ID)
	want := 100*3.0/1e6 + 20*15.0/1e6
	if got := m[3].CostUSD; got < want-1e-12 || got > want+1e-12 {
		t.Errorf("costo %v, esperaba %v", got, want)
	}
	ev.mu.Lock()
	last := ev.usage[len(ev.usage)-1]
	ev.mu.Unlock()
	if last.CostUSD < want-1e-12 || last.CostUSD > want+1e-12 {
		t.Errorf("chat:usage costo %v", last.CostUSD)
	}
}

// Un fallo del proveedor a mitad de un turno con pasos guarda lo hecho.
func TestHermesProviderErrorKeepsStepsAsPartial(t *testing.T) {
	sim := newSim()
	sim.script = []simReply{
		{text: "Miro el proyecto.", calls: []simCall{{"list_checkpoints", map[string]any{}}}},
		{errStatus: 500, errBody: `{"error":"se cayó el modelo"}`},
	}
	a, wsID, _, ev := hermesApp(t, sim)
	listModels(t, a)
	c, _ := a.NewChat(wsID, "ollama", capsAgent)
	if _, err := a.SendChatMessage(c.ID, "mira", "", nil, SendOptions{Agent: boolp(true)}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-ev.errs:
		if e.MessageID == 0 || !strings.Contains(e.Message, "se cayó el modelo") {
			t.Errorf("error: %+v", e)
		}
	case <-ev.done:
		t.Fatal("debía fallar")
	case <-time.After(30 * time.Second):
		t.Fatal("sin evento")
	}
	v, _ := a.GetChat(c.ID)
	if len(v.Messages) != 2 || len(hermes.ParseParts(v.Messages[1].Parts).Steps) != 1 || v.Messages[1].Content != "Miro el proyecto." {
		t.Errorf("parcial: %+v", v.Messages)
	}
}

var _ llm.Client // el simulador se prueba contra el cliente real de backend/llm

// «Modificar con IA» adjunta un STL que ya vive en refs/ (PrepareMeshEdit): se usa
// tal cual, sin una copia -2, y quitar el chip no lo borra (el chat no lo creó).
func TestAttachModelAlreadyInRefsIsReusedAndNotDeleted(t *testing.T) {
	sim := newSim()
	a, wsID, dir, _ := hermesApp(t, sim)
	if err := os.MkdirAll(filepath.Join(dir, "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	ref := filepath.Join(dir, "refs", "base.stl")
	if err := os.WriteFile(ref, []byte(asciiSTL()), 0o644); err != nil {
		t.Fatal(err)
	}
	att, err := a.AddChatAttachment(wsID, ref)
	if err != nil {
		t.Fatal(err)
	}
	if att.RefPath != "refs/base.stl" {
		t.Fatalf("debía reutilizar refs/base.stl, no copiarlo: %q", att.RefPath)
	}
	if _, err := os.Stat(filepath.Join(dir, "refs", "base-2.stl")); !os.IsNotExist(err) {
		t.Error("no debía quedar una copia -2")
	}
	if err := a.RemoveChatAttachment(att.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ref); err != nil {
		t.Errorf("quitar el chip no debe borrar un archivo que ya estaba en refs/: %v", err)
	}
}
