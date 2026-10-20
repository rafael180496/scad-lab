package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// chatEvents recoge los eventos que emite la App (en los tests no hay runtime
// de Wails: emitter es la función que se instala acá).
type chatEvents struct {
	mu     sync.Mutex
	deltas map[int64]string
	done   chan ChatDoneEvent
	errs   chan ChatErrorEvent
}

func captureChatEvents(a *App) *chatEvents {
	ev := &chatEvents{deltas: map[int64]string{}, done: make(chan ChatDoneEvent, 4), errs: make(chan ChatErrorEvent, 4)}
	a.emitter = func(name string, payload any) {
		switch p := payload.(type) {
		case ChatDeltaEvent:
			ev.mu.Lock()
			ev.deltas[p.ChatID] += p.Text
			ev.mu.Unlock()
		case ChatDoneEvent:
			ev.done <- p
		case ChatErrorEvent:
			ev.errs <- p
		}
	}
	return ev
}

func (e *chatEvents) text(id int64) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.deltas[id]
}

// fakeOllama contesta un stream NDJSON y guarda el cuerpo del último pedido.
type fakeOllama struct {
	mu     sync.Mutex
	body   map[string]any
	chunks []string
	status int
	hold   bool
}

func (f *fakeOllama) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			_, _ = io.WriteString(w, `{"version":"0.5.1"}`)
		case "/api/tags":
			_, _ = io.WriteString(w, `{"models":[{"name":"qwen3-coder:30b"},{"name":"llama3.2:3b"}]}`)
		case "/api/chat":
			raw, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			_ = json.Unmarshal(raw, &f.body)
			chunks, status, hold := f.chunks, f.status, f.hold
			f.mu.Unlock()
			if status != 0 {
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{"error":"model not found"}`)
				return
			}
			for _, c := range chunks {
				b, _ := json.Marshal(c)
				_, _ = io.WriteString(w, `{"message":{"content":`+string(b)+`},"done":false}`+"\n")
				w.(http.Flusher).Flush()
			}
			if hold {
				<-r.Context().Done()
				return
			}
			_, _ = io.WriteString(w, `{"message":{"content":""},"done":true}`+"\n")
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// chatApp deja un App con un workspace (con un checkpoint) y el servidor
// «ollama» (migrado de S5, mismo id) apuntando al servidor falso.
func chatApp(t *testing.T, f *fakeOllama) (*App, int64, string, *chatEvents) {
	t.Helper()
	a, wsID, dir := editorWorkspace(t)
	srv := f.server(t)
	p, err := a.store.GetModelServer("ollama")
	if err != nil {
		t.Fatal(err)
	}
	p.BaseURL, p.DefaultModel, p.Enabled = srv.URL, "qwen3-coder:30b", true
	if _, err := a.SaveModelServer(p, "", false); err != nil {
		t.Fatal(err)
	}
	return a, wsID, dir, captureChatEvents(a)
}

func waitDone(t *testing.T, ev *chatEvents) ChatDoneEvent {
	t.Helper()
	select {
	case d := <-ev.done:
		return d
	case e := <-ev.errs:
		t.Fatalf("llegó chat:error: %+v", e)
	case <-time.After(5 * time.Second):
		t.Fatal("la respuesta nunca terminó")
	}
	return ChatDoneEvent{}
}

func TestChatSendStreamsPersistsAndTitles(t *testing.T) {
	f := &fakeOllama{chunks: []string{"Claro: ", "```openscad\n", "cube(10);\n```"}}
	a, wsID, _, ev := chatApp(t, f)

	c, err := a.NewChat(wsID, "ollama", "")
	if err != nil || c.Model != "qwen3-coder:30b" {
		t.Fatalf("NewChat: %+v, %v", c, err)
	}
	// El perfil completo (S17): con la ventana sin informar el servidor falso no dice el contexto del modelo, así que «Auto»
	// elegiría las esenciales y su prompt compacto, que no trae el código. Este test mira el prompt de siempre.
	if err := a.SetChatToolProfile(c.ID, "full"); err != nil {
		t.Fatal(err)
	}
	um, err := a.SendChatMessage(c.ID, "  Haz un cubo de diez milímetros para mi pieza  ", "001-base.scad", nil, SendOptions{})
	if err != nil || um.Role != "user" || um.Content != "Haz un cubo de diez milímetros para mi pieza" {
		t.Fatalf("Send: %+v, %v", um, err)
	}
	d := waitDone(t, ev)
	if d.ChatID != c.ID || d.MessageID == 0 || d.Cancelled {
		t.Fatalf("done: %+v", d)
	}
	if got := ev.text(c.ID); got != "Claro: ```openscad\ncube(10);\n```" {
		t.Errorf("deltas: %q", got)
	}

	v, err := a.GetChat(c.ID)
	if err != nil || len(v.Messages) != 2 || v.Messages[1].ID != d.MessageID || v.Running {
		t.Fatalf("GetChat: %+v, %v", v, err)
	}
	if v.Messages[1].Content != "Claro: ```openscad\ncube(10);\n```" {
		t.Errorf("respuesta guardada: %q", v.Messages[1].Content)
	}
	if v.Chat.Title != "Haz un cubo de diez milímetros para mi…" {
		t.Errorf("título: %q", v.Chat.Title)
	}

	// El pedido al modelo: prompt de sistema con el código del archivo abierto,
	// y el contexto de ventana que pide ScadLab.
	f.mu.Lock()
	body := f.body
	f.mu.Unlock()
	msgs := body["messages"].([]any)
	sys := msgs[0].(map[string]any)
	if sys["role"] != "system" || !strings.Contains(sys["content"].(string), `color("red") cube(10);`) || !strings.Contains(sys["content"].(string), "001-base.scad") {
		t.Errorf("el prompt de sistema no trae el archivo abierto: %.300q", sys["content"])
	}
	if opts, _ := body["options"].(map[string]any); opts["num_ctx"] != float64(chatDefaultNumCtx) {
		t.Errorf("num_ctx: %v", body["options"])
	}

	// Segundo turno: el historial viaja y el título no cambia.
	f.mu.Lock()
	f.chunks = []string{"Listo."}
	f.mu.Unlock()
	if _, err := a.SendChatMessage(c.ID, "ahora un cilindro", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	waitDone(t, ev)
	f.mu.Lock()
	n := len(f.body["messages"].([]any))
	f.mu.Unlock()
	if n != 4 { // system + user + assistant + user
		t.Errorf("mensajes enviados: %d", n)
	}
	if v, _ := a.GetChat(c.ID); v.Chat.Title != "Haz un cubo de diez milímetros para mi…" || len(v.Messages) != 4 {
		t.Errorf("tras el segundo turno: %+v", v)
	}
}

func TestChatBusyAndCancelKeepsPartial(t *testing.T) {
	f := &fakeOllama{chunks: []string{"a medias"}, hold: true}
	a, wsID, _, ev := chatApp(t, f)
	c, _ := a.NewChat(wsID, "ollama", "m")
	if _, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	// Mientras responde: GetChat lo informa y un segundo envío se rechaza.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if v, _ := a.GetChat(c.ID); v.Running && v.Partial == "a medias" && v.PartialSeq > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("GetChat nunca mostró la respuesta en curso")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := a.SendChatMessage(c.ID, "otra", "", nil, SendOptions{}); err == nil {
		t.Error("un segundo envío con el chat ocupado debería fallar")
	}
	a.CancelChat(c.ID)
	d := waitDone(t, ev)
	if !d.Cancelled || d.MessageID == 0 {
		t.Fatalf("done: %+v", d)
	}
	v, _ := a.GetChat(c.ID)
	if v.Running || len(v.Messages) != 2 || v.Messages[1].Content != "a medias" {
		t.Errorf("tras cancelar: %+v", v)
	}
}

func TestChatServerErrorEmitsErrorAndKeepsUserMessage(t *testing.T) {
	f := &fakeOllama{status: 404}
	a, wsID, _, ev := chatApp(t, f)
	c, _ := a.NewChat(wsID, "ollama", "nope")
	if _, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-ev.errs:
		if e.ChatID != c.ID || !strings.Contains(e.Message, "model not found") || e.MessageID != 0 {
			t.Errorf("error: %+v", e)
		}
	case <-ev.done:
		t.Fatal("no debería terminar bien")
	case <-time.After(5 * time.Second):
		t.Fatal("sin evento")
	}
	if v, _ := a.GetChat(c.ID); len(v.Messages) != 1 || v.Messages[0].Role != "user" || v.Running {
		t.Errorf("el mensaje del usuario se conserva y el chat se libera: %+v", v)
	}
	// Y se puede reintentar: los dos mensajes de usuario se funden al enviar.
	f.mu.Lock()
	f.status, f.chunks = 0, []string{"ok"}
	f.mu.Unlock()
	if _, err := a.SendChatMessage(c.ID, "reintento", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	waitDone(t, ev)
	f.mu.Lock()
	msgs := f.body["messages"].([]any)
	f.mu.Unlock()
	if len(msgs) != 2 || !strings.Contains(msgs[1].(map[string]any)["content"].(string), "hola\n\nreintento") {
		t.Errorf("mensajes: %v", msgs)
	}
}

func TestChatValidation(t *testing.T) {
	f := &fakeOllama{}
	a, wsID, _, _ := chatApp(t, f)
	if _, err := a.NewChat(wsID, "claude", "x"); err == nil {
		t.Error("un CLI agéntico no es un servidor de modelos")
	}
	if _, err := a.NewChat(wsID, "anthropic", "x"); err == nil {
		t.Error("un servidor deshabilitado no sirve")
	}
	if _, err := a.NewChat(wsID, "nope", "x"); err == nil {
		t.Error("servidor inexistente")
	}
	if _, err := a.NewChat(99999, "ollama", "x"); err == nil {
		t.Error("workspace inexistente")
	}
	p, _ := a.store.GetModelServer("ollama")
	p.DefaultModel = ""
	_, _ = a.SaveModelServer(p, "", false)
	if _, err := a.NewChat(wsID, "ollama", " "); err == nil {
		t.Error("sin modelo en ningún lado debería pedirlo")
	}
	c, _ := a.NewChat(wsID, "ollama", "m")
	if _, err := a.SendChatMessage(c.ID, "   ", "", nil, SendOptions{}); err == nil {
		t.Error("mensaje vacío")
	}
	if _, err := a.SendChatMessage(c.ID, strings.Repeat("x", maxChatTextBytes+1), "", nil, SendOptions{}); err == nil {
		t.Error("mensaje enorme")
	}
	if _, err := a.SendChatMessage(99999, "hola", "", nil, SendOptions{}); err == nil {
		t.Error("chat inexistente")
	}
}

func TestChatAnthropicNeedsKeyBeforeAnyRequest(t *testing.T) {
	f := &fakeOllama{}
	a, wsID, _, _ := chatApp(t, f)
	p, _ := a.store.GetModelServer("anthropic")
	p.Enabled, p.DefaultModel, p.APIKeyEnv = true, "claude-sonnet-5-5", "SCADLAB_TEST_NO_SUCH_KEY"
	if _, err := a.SaveModelServer(p, "", false); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SHELL", "/nonexistent/shell") // que el respaldo de la shell de login no encuentre nada
	c, err := a.NewChat(wsID, "anthropic", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{})
	if err == nil || !strings.Contains(err.Error(), "SCADLAB_TEST_NO_SUCH_KEY") {
		t.Fatalf("debería pedir la variable por nombre: %v", err)
	}
	if v, _ := a.GetChat(c.ID); len(v.Messages) != 0 {
		t.Error("si falta la clave no se guarda nada")
	}
}

func TestChatDeleteCascades(t *testing.T) {
	f := &fakeOllama{chunks: []string{"x"}, hold: true}
	a, wsID, _, _ := chatApp(t, f)
	c, _ := a.NewChat(wsID, "ollama", "m")
	if _, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteChat(c.ID); err != nil {
		t.Fatal(err)
	}
	// El turno se corta y se libera: nada queda corriendo ni la base guarda una
	// respuesta de un chat borrado.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if running, _, _ := a.chatSvc().mgr.Snapshot(c.ID); !running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("el turno del chat borrado sigue corriendo")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := a.GetChat(c.ID); err == nil {
		t.Error("el chat debería haberse ido")
	}
	if list, _ := a.ListChats(wsID); len(list) != 0 {
		t.Errorf("lista: %+v", list)
	}
}

func TestListProviderModels(t *testing.T) {
	f := &fakeOllama{}
	a, _, _, _ := chatApp(t, f)
	got, err := a.ListProviderModels("ollama")
	// La lista sale ordenada para que la búsqueda y el ojo encuentren.
	if err != nil || strings.Join(got, ",") != "llama3.2:3b,qwen3-coder:30b" {
		t.Fatalf("ollama: %v, %v", got, err)
	}
	// Anthropic sin clave: el error nombra qué falta, antes de pedir nada.
	p, _ := a.store.GetModelServer("anthropic")
	p.Enabled, p.APIKeyEnv = true, ""
	_, _ = a.SaveModelServer(p, "", false)
	if _, err = a.ListProviderModels("anthropic"); err == nil || !strings.Contains(err.Error(), "clave") && !strings.Contains(err.Error(), "key") {
		t.Fatalf("anthropic sin clave: %v", err)
	}
	if _, err := a.ListProviderModels("claude"); err == nil {
		t.Error("un CLI no lista modelos")
	}
}

func TestChatCheckpointParent(t *testing.T) {
	a, wsID, dir := editorWorkspace(t)
	second := filepath.Join(dir, "002-otro.scad")
	if err := os.WriteFile(second, []byte("cube(1);"), 0o644); err != nil {
		t.Fatal(err)
	}
	// El archivo abierto manda.
	if got, err := a.ChatCheckpointParent(wsID, "001-base.scad"); err != nil || got != "001-base.scad" {
		t.Errorf("abierto: %q, %v", got, err)
	}
	// Sin archivo abierto, o con uno que ya no existe o con un nombre raro: el último.
	for _, cur := range []string{"", "999-borrado.scad", "../fuera.scad", "x.txt"} {
		if got, err := a.ChatCheckpointParent(wsID, cur); err != nil || got != "002-otro.scad" {
			t.Errorf("%q → %q, %v", cur, got, err)
		}
	}
	// Workspace vacío: no hay padre, y no es un error (S12.c): el bloque será la raíz.
	empty, err := a.CreateWorkspace("Vacío", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := a.ChatCheckpointParent(empty.ID, ""); err != nil || got != "" {
		t.Errorf("sin checkpoints: %q, %v", got, err)
	}
}

// Guardar un bloque del chat en un workspace vacío crea la raíz (sin padre) y
// enlaza el chat; desde ahí cuelga lo siguiente. Antes fallaba con «no hay
// checkpoint que sirva de padre».
func TestSaveChatCheckpointCreatesTheRootInAnEmptyWorkspace(t *testing.T) {
	a := newTestApp(t)
	w, err := a.CreateWorkspace("Vacío", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	c, err := a.store.CreateChat(w.ID, "ollama", "m")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := a.ChatCheckpointParent(w.ID, "")
	if err != nil || parent != "" {
		t.Fatalf("padre: %q %v", parent, err)
	}
	name, err := a.SaveChatCheckpoint(w.ID, c.ID, parent, "Caja con tapa", "Caja con tapa para una Raspberry", "color(\"red\") cube(10);\n")
	if err != nil || name != "001-caja-con-tapa.scad" {
		t.Fatalf("raíz: %q %v", name, err)
	}
	got := readFile(t, filepath.Join(w.Path, name))
	if !strings.HasPrefix(got, "// @checkpoint parent= desc=\"Caja con tapa para una Raspberry\"\n") {
		t.Errorf("la raíz lleva `parent=` vacío: %q", got)
	}
	// Lo siguiente cuelga de ella.
	parent, _ = a.ChatCheckpointParent(w.ID, "")
	if parent != name {
		t.Errorf("el padre pasa a ser la raíz: %q", parent)
	}
	next, err := a.SaveChatCheckpoint(w.ID, c.ID, parent, "Más alta", "", "color(\"red\") cube(12);\n")
	if err != nil || next != "002-mas-alta.scad" || !strings.Contains(readFile(t, filepath.Join(w.Path, next)), "parent=001-caja-con-tapa.scad") {
		t.Errorf("hijo: %q %v", next, err)
	}
	// Con checkpoints ya en el árbol, un padre vacío sigue siendo un olvido: no se crea una raíz de más.
	if _, err := a.SaveAsCheckpoint(w.ID, "", "otra", "", "cube(1);"); err == nil {
		t.Error("una segunda raíz por olvido del padre debe fallar")
	}
}

// El archivo abierto se lee del disco con el mismo control de nombre que
// ReadCheckpoint: la interfaz no puede hacer entrar al prompt un archivo de
// fuera del workspace.
func TestChatPromptContextRejectsPathsOutsideWorkspace(t *testing.T) {
	a, wsID, dir := editorWorkspace(t)
	outside := filepath.Join(filepath.Dir(dir), "secreto.scad")
	if err := os.WriteFile(outside, []byte("SECRETO"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, _ := a.store.GetWorkspace(wsID)
	for _, cur := range []string{"../secreto.scad", filepath.Join("..", "secreto.scad"), outside} {
		ctx := chatPromptContext(w, cur)
		if ctx.CurrentFile != "" || strings.Contains(ctx.CurrentSource, "SECRETO") {
			t.Errorf("%q se coló: %+v", cur, ctx)
		}
	}
	ctx := chatPromptContext(w, "001-base.scad")
	if ctx.CurrentFile != "001-base.scad" || !strings.Contains(ctx.CurrentSource, "cube(10)") || ctx.PrinterName == "" || ctx.ColorSlots == 0 {
		t.Errorf("contexto: %+v", ctx)
	}
	if len(ctx.Checkpoints) != 1 || ctx.WorkspaceName != "Editor" || ctx.BuildVolume[0] == 0 {
		t.Errorf("contexto: %+v", ctx)
	}
}

// Con la ventana chica (el servidor no informa el contexto del modelo: se le piden 16 384) «Auto» declara las herramientas
// esenciales y manda el prompt compacto (S17): nombra el archivo abierto pero no lo embebe —el modelo lo lee por partes—.
func TestChatAutoProfileUsesEssentialToolsAndTheCompactPromptInASmallWindow(t *testing.T) {
	f := &fakeOllama{chunks: []string{"Listo."}}
	a, wsID, _, ev := chatApp(t, f)
	c, err := a.NewChat(wsID, "ollama", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendChatMessage(c.ID, "cambia algo", "001-base.scad", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	waitDone(t, ev)
	f.mu.Lock()
	body := f.body
	f.mu.Unlock()
	sys := body["messages"].([]any)[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "compact mode") || !strings.Contains(sys, "001-base.scad") || strings.Contains(sys, `color("red") cube(10);`) {
		t.Errorf("prompt compacto esperado, con el archivo nombrado y sin su código: %.400q", sys)
	}
	var names []string
	for _, tl := range body["tools"].([]any) {
		fn, _ := tl.(map[string]any)["function"].(map[string]any)
		names = append(names, fn["name"].(string))
	}
	if len(names) != 11 || !hasName(names, "edit_checkpoint") || hasName(names, "extract_module") {
		t.Errorf("se esperaban las 11 herramientas esenciales: %v", names)
	}
}
