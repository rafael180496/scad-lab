package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/zalando/go-keyring"

	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/store"
	"scadlab/backend/vaultgate"
)

// Una clave inventada y reconocible: si aparece donde no debe, el test la ve.
const secretKey = "sk-prueba-NO-ES-REAL-0123456789"

// testMasterPassword es la contraseña maestra inventada de las pruebas.
const testMasterPassword = "contraseña-maestra-de-prueba"

// serverApp es una App de prueba con el llavero simulado y la contraseña maestra
// ya creada (con «Recordar»): ninguna prueba toca el llavero real de quien la
// corre, y guardar una clave de API funciona sin pasos previos.
func serverApp(t *testing.T) *App {
	t.Helper()
	keyring.MockInit()
	a := newTestApp(t)
	if _, err := a.CreateMasterPassword(testMasterPassword, true); err != nil {
		t.Fatal(err)
	}
	return a
}

// fakeOpenAI contesta /models y /chat/completions (SSE) y recuerda la cabecera
// Authorization del último pedido.
type fakeOpenAI struct {
	mu       sync.Mutex
	auth     []string
	models   atomic.Int32
	reject   bool // responde 401
	chunks   []string
	lastBody map[string]any
}

func (f *fakeOpenAI) server(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		reject := f.reject
		f.mu.Unlock()
		if reject {
			w.WriteHeader(401)
			_, _ = io.WriteString(w, `{"error":{"message":"invalid api key"}}`)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			f.models.Add(1)
			_, _ = io.WriteString(w, `{"data":[{"id":"zeta-model"},{"id":"Alfa-model"}]}`)
		case "/v1/chat/completions":
			raw, _ := io.ReadAll(r.Body)
			f.mu.Lock()
			_ = json.Unmarshal(raw, &f.lastBody)
			chunks := f.chunks
			f.mu.Unlock()
			for _, c := range chunks {
				b, _ := json.Marshal(c)
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s}}]}\n\n", b)
				w.(http.Flusher).Flush()
			}
			fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4}}\n\ndata: [DONE]\n\n")
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (f *fakeOpenAI) lastAuth() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.auth) == 0 {
		return ""
	}
	return f.auth[len(f.auth)-1]
}

func openAIServer(url string) store.ModelServer {
	return store.ModelServer{Kind: store.ServerOpenAI, Preset: "openrouter", Name: "Mi nube", BaseURL: url + "/v1", Enabled: true}
}

// Crear, editar, probar y borrar un servidor, con la clave guardada cifrada:
// la clave nunca está en el JSON que va al frontend, ni en el archivo .db, ni en
// un error, y aun así el servidor la recibe (descifrada) al probar y al chatear.
func TestServerLifecycleKeepsTheKeyPrivate(t *testing.T) {
	a := serverApp(t)
	f := &fakeOpenAI{chunks: []string{"Hola ", "mundo"}}
	srv := f.server(t)

	saved, err := a.SaveModelServer(openAIServer(srv.URL), secretKey, false)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.HasToken || !strings.HasPrefix(saved.ID, "srv-") {
		t.Fatalf("alta: %+v", saved)
	}

	// 1. El JSON del listado, el que cruza hacia el frontend.
	list, err := a.ListModelServers()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(list)
	if bytes.Contains(raw, []byte(secretKey)) || bytes.Contains(raw, []byte("sk-prueba")) {
		t.Fatalf("la clave está en el JSON de ListModelServers: %s", raw)
	}
	one, _ := json.Marshal(saved)
	if bytes.Contains(one, []byte("sk-prueba")) || !bytes.Contains(one, []byte(`"hasToken":true`)) {
		t.Fatalf("SaveModelServer: %s", one)
	}

	// 2. El archivo .db (y su WAL): cifrada, la clave no está en claro.
	for _, p := range []string{"scadlab.db", "scadlab.db-wal"} {
		if raw, err := os.ReadFile(filepath.Join(a.dataDir, p)); err == nil && bytes.Contains(raw, []byte("sk-prueba")) {
			t.Errorf("la clave está en claro en %s", p)
		}
	}
	if st, err := a.VaultStatus(); err != nil || !st.Initialized || !st.Unlocked || !st.KeyringAvailable {
		t.Errorf("con el llavero simulado: %+v, %v", st, err)
	}

	// 3. Probar usa la clave guardada (token vacío = la del servidor).
	pr, err := a.TestModelServer(saved, "")
	if err != nil || !pr.OK || pr.Models != 2 || len(pr.ModelIDs) != 2 {
		t.Fatalf("probar: %+v, %v", pr, err)
	}
	if got := f.lastAuth(); got != "Bearer "+secretKey {
		t.Errorf("el servidor no recibió la clave guardada: %q", got)
	}
	// Con el formulario sin guardar (id vacío) y otra clave, gana la escrita.
	draft := openAIServer(srv.URL)
	if pr, _ = a.TestModelServer(draft, "otra-clave"); !pr.OK || f.lastAuth() != "Bearer otra-clave" {
		t.Errorf("clave del formulario: %+v / %q", pr, f.lastAuth())
	}

	// 4. Un 401 se informa en el resultado (no como error de la llamada) y sin la clave.
	f.mu.Lock()
	f.reject = true
	f.mu.Unlock()
	pr, err = a.TestModelServer(saved, "")
	if err != nil || pr.OK || pr.Status != 401 || strings.Contains(pr.Message, "sk-prueba") || pr.Message == "" {
		t.Fatalf("401: %+v, %v", pr, err)
	}
	f.mu.Lock()
	f.reject = false
	f.mu.Unlock()

	// 5. Chatear: la clave guardada viaja al servidor durante el turno.
	a2 := captureChatEvents(a)
	wsID := mustWorkspace(t, a)
	c, err := a.NewChat(wsID, saved.ID, "Alfa-model")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	d := waitDone(t, a2)
	if d.MessageID == 0 || a2.text(c.ID) != "Hola mundo" {
		t.Fatalf("chat: %+v %q", d, a2.text(c.ID))
	}
	if f.lastAuth() != "Bearer "+secretKey {
		t.Errorf("el chat no usó la clave guardada: %q", f.lastAuth())
	}

	// 6. Editar sin clave la conserva; con clearToken la borra.
	saved.Name = "Mi nube 2"
	up, err := a.SaveModelServer(saved, "", false)
	if err != nil || !up.HasToken || up.Name != "Mi nube 2" {
		t.Fatalf("editar: %+v, %v", up, err)
	}
	if up, err = a.SaveModelServer(up, "", true); err != nil || up.HasToken {
		t.Fatalf("borrar la clave: %+v, %v", up, err)
	}
	if pr, _ = a.TestModelServer(up, ""); !pr.OK || f.lastAuth() != "" {
		t.Errorf("sin clave no debe mandarse Authorization: %+v / %q", pr, f.lastAuth())
	}

	// 7. Borrar: el chat sigue legible y el servidor ya no existe.
	if err := a.DeleteModelServer(saved.ID); err != nil {
		t.Fatal(err)
	}
	if v, err := a.GetChat(c.ID); err != nil || len(v.Messages) != 2 {
		t.Errorf("el chat debe seguir legible: %+v, %v", v, err)
	}
	if _, err := a.SendChatMessage(c.ID, "otra", "", nil, SendOptions{}); err == nil {
		t.Error("chatear con un servidor borrado debe fallar con un mensaje claro")
	}
	if list, _ := a.ListModelServers(); len(list) != 3 {
		t.Errorf("deberían quedar los 3 del seed: %+v", list)
	}
}

func mustWorkspace(t *testing.T, a *App) int64 {
	t.Helper()
	w, err := a.CreateWorkspace("Chat", "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	return w.ID
}

// Sin contraseña maestra guardar una clave falla con el error de código
// vault-uninitialized (la interfaz abre «Crear contraseña maestra» y reintenta),
// sin guardar nada ni repetir la clave; la variable de entorno funciona igual que
// siempre, sin contraseña ni llavero. Con el llavero denegado la contraseña se
// crea igual y todo sigue andando en esta sesión.
func TestServerKeyWithoutMasterPasswordAndWithDeniedKeychain(t *testing.T) {
	keyring.MockInitWithError(errors.New("sin servicio de secretos"))
	a := newTestApp(t)
	f := &fakeOpenAI{}
	srv := f.server(t)

	_, err := a.SaveModelServer(openAIServer(srv.URL), secretKey, false)
	if err == nil || i18n.Code(err) != vaultgate.CodeUninitialized {
		t.Fatalf("guardar una clave sin contraseña maestra: %v", err)
	}
	if strings.Contains(err.Error(), "sk-prueba") {
		t.Errorf("el error repite la clave: %v", err)
	}
	if list, _ := a.ListModelServers(); len(list) != 3 {
		t.Errorf("no debe quedar nada guardado: %+v", list)
	}

	// Sin clave guardada, con variable de entorno: se guarda y se usa, sin contraseña.
	t.Setenv("SCADLAB_TEST_SERVER_KEY", "  env-key-123 ")
	s := openAIServer(srv.URL)
	s.APIKeyEnv = "SCADLAB_TEST_SERVER_KEY"
	saved, err := a.SaveModelServer(s, "", false)
	if err != nil || saved.HasToken || saved.APIKeyEnv != "SCADLAB_TEST_SERVER_KEY" {
		t.Fatalf("con variable de entorno: %+v, %v", saved, err)
	}
	if pr, _ := a.TestModelServer(saved, ""); !pr.OK || f.lastAuth() != "Bearer env-key-123" {
		t.Errorf("la variable de entorno no se usó: %+v / %q", pr, f.lastAuth())
	}

	// Con el llavero denegado, crear la contraseña funciona y avisa que no recordó.
	res, err := a.CreateMasterPassword(testMasterPassword, true)
	if err != nil || !res.RememberFailed || res.Detail == "" {
		t.Fatalf("CreateMasterPassword: %+v, %v", res, err)
	}
	st, _ := a.VaultStatus()
	if !st.Initialized || !st.Unlocked || st.Remembered || st.KeyringAvailable {
		t.Fatalf("estado: %+v", st)
	}
	keyed := openAIServer(srv.URL)
	keyed.Name = "Con clave"
	saved, err = a.SaveModelServer(keyed, secretKey, false)
	if err != nil || !saved.HasToken {
		t.Fatalf("con contraseña maestra: %+v, %v", saved, err)
	}
	if pr, _ := a.TestModelServer(saved, ""); !pr.OK || f.lastAuth() != "Bearer "+secretKey {
		t.Errorf("la clave guardada no se usó: %+v / %q", pr, f.lastAuth())
	}
}

// La clave guardada gana a la variable de entorno.
func TestStoredKeyBeatsEnvVar(t *testing.T) {
	a := serverApp(t)
	f := &fakeOpenAI{}
	srv := f.server(t)
	t.Setenv("SCADLAB_TEST_SERVER_KEY", "env-key")
	s := openAIServer(srv.URL)
	s.APIKeyEnv = "SCADLAB_TEST_SERVER_KEY"
	saved, err := a.SaveModelServer(s, secretKey, false)
	if err != nil {
		t.Fatal(err)
	}
	if pr, _ := a.TestModelServer(saved, ""); !pr.OK || f.lastAuth() != "Bearer "+secretKey {
		t.Errorf("debía ganar la guardada: %q", f.lastAuth())
	}
}

func TestSaveModelServerValidation(t *testing.T) {
	a := serverApp(t)
	cases := map[string]store.ModelServer{
		"sin nombre":       {Kind: store.ServerOllama, BaseURL: "http://localhost:11434"},
		"tipo desconocido": {Kind: "gopher", Name: "x", BaseURL: "http://x"},
		"URL sin esquema":  {Kind: store.ServerOpenAI, Name: "x", BaseURL: "localhost:1234"},
		"URL no http":      {Kind: store.ServerOpenAI, Name: "x", BaseURL: "file:///etc/passwd"},
		"variable mala":    {Kind: store.ServerOpenAI, Name: "x", BaseURL: "http://x/v1", APIKeyEnv: "A;rm -rf"},
	}
	for name, s := range cases {
		if _, err := a.SaveModelServer(s, "", false); err == nil {
			t.Errorf("%s: debería rechazarse", name)
		}
	}
	// Anthropic sin URL guarda la oficial; con /v1 final se lo saca.
	an, err := a.SaveModelServer(store.ModelServer{Kind: store.ServerAnthropic, Name: "Claude", Enabled: true}, "", false)
	if err != nil || an.BaseURL != "https://api.anthropic.com" {
		t.Errorf("anthropic: %+v, %v", an, err)
	}
	an, err = a.SaveModelServer(store.ModelServer{Kind: store.ServerAnthropic, Name: "Claude proxy", BaseURL: "https://proxy.local/v1/"}, "", false)
	if err != nil || an.BaseURL != "https://proxy.local" {
		t.Errorf("anthropic con /v1: %+v, %v", an, err)
	}
	// Una URL inválida se informa en la prueba, no como error de la llamada.
	pr, err := a.TestModelServer(store.ModelServer{Kind: store.ServerOpenAI, Name: "x", BaseURL: "nope"}, "")
	if err != nil || pr.OK || pr.Message == "" {
		t.Errorf("prueba con URL mala: %+v, %v", pr, err)
	}
	// Un servidor caído tampoco es un error de la llamada.
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	pr, err = a.TestModelServer(store.ModelServer{Kind: store.ServerOllama, Name: "x", BaseURL: url}, "")
	if err != nil || pr.OK || pr.Message == "" {
		t.Errorf("prueba contra un servidor caído: %+v, %v", pr, err)
	}
}

// ListServerModels: capacidades reales de Ollama (/api/show), lista alfabética,
// caché de 5 minutos con refresco y el cambio de configuración la descarta.
func TestListServerModelsCapabilitiesCacheAndRefresh(t *testing.T) {
	a := serverApp(t)
	var tags, shows atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"version":"0.5.1"}`) })
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		tags.Add(1)
		fmt.Fprint(w, `{"models":[{"name":"qwen3-vl:8b"},{"name":"llama3.2:3b"}]}`)
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		shows.Add(1)
		var req struct{ Model string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model == "qwen3-vl:8b" {
			fmt.Fprint(w, `{"capabilities":["completion","vision","tools"],"model_info":{"qwen3vl.context_length":262144}}`)
			return
		}
		fmt.Fprint(w, `{"capabilities":["completion"],"model_info":{"llama.context_length":131072}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	o, _ := a.store.GetModelServer("ollama")
	o.BaseURL, o.Enabled = srv.URL, true
	o, err := a.SaveModelServer(o, "", false)
	if err != nil {
		t.Fatal(err)
	}

	list, err := a.ListServerModels("ollama", false)
	if err != nil || len(list) != 2 {
		t.Fatalf("%+v, %v", list, err)
	}
	if list[0].ID != "llama3.2:3b" || list[0].Vision || !list[0].VisionKnown || list[0].Tools || list[0].ContextTokens != 131072 {
		t.Errorf("llama: %+v", list[0])
	}
	if list[1].ID != "qwen3-vl:8b" || !list[1].Vision || !list[1].Tools || !list[1].ToolsKnown || list[1].ContextTokens != 262144 {
		t.Errorf("qwen: %+v", list[1])
	}
	if _, err := a.ListServerModels("ollama", false); err != nil || tags.Load() != 1 {
		t.Errorf("la segunda lista debía salir de la caché: %d consultas, %v", tags.Load(), err)
	}
	if m, ok := a.serverModelInfo("ollama", "qwen3-vl:8b"); !ok || !m.Vision {
		t.Errorf("serverModelInfo: %+v %v", m, ok)
	}
	if _, err := a.ListServerModels("ollama", true); err != nil || tags.Load() != 2 {
		t.Errorf("refresh debía volver a consultar: %d", tags.Load())
	}
	// Editar el servidor (otra clave de contexto, p. ej.) descarta la lista.
	o.ContextTokens = 8192
	if _, err := a.SaveModelServer(o, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.ListServerModels("ollama", false); err != nil || tags.Load() != 3 {
		t.Errorf("tras editar debía volver a consultar: %d", tags.Load())
	}
	// Servidor inexistente o deshabilitado.
	if _, err := a.ListServerModels("nope", false); err == nil {
		t.Error("servidor inexistente")
	}
	o.Enabled = false
	_, _ = a.SaveModelServer(o, "", false)
	if _, err := a.ListServerModels("ollama", false); err == nil {
		t.Error("servidor deshabilitado")
	}
}

// El chequeo previo: un servidor caído detiene el turno ANTES de guardar el
// mensaje, con el nombre y el host; un chequeo bueno vale 30 s.
func TestChatPrecheckStopsTheTurnWhenServerIsDown(t *testing.T) {
	a := serverApp(t)
	f := &fakeOpenAI{chunks: []string{"ok"}}
	srv := f.server(t)
	saved, err := a.SaveModelServer(openAIServer(srv.URL), "", false)
	if err != nil {
		t.Fatal(err)
	}
	ev := captureChatEvents(a)
	wsID := mustWorkspace(t, a)
	c, _ := a.NewChat(wsID, saved.ID, "m")

	// Con el servidor arriba: un turno, y el siguiente reutiliza el chequeo.
	if _, err := a.SendChatMessage(c.ID, "uno", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	waitDone(t, ev)
	if _, err := a.SendChatMessage(c.ID, "dos", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	waitDone(t, ev)
	if n := f.models.Load(); n != 1 {
		t.Errorf("el chequeo previo debía hacerse una sola vez en 30 s: %d", n)
	}

	// Se cae el servidor y se invalida el chequeo (como pasa al editarlo).
	srv.Close()
	a.serverSvc().health.invalidate(saved.ID)
	_, err = a.SendChatMessage(c.ID, "tres", "", nil, SendOptions{})
	if err == nil || !strings.Contains(err.Error(), "Mi nube") {
		t.Fatalf("debía fallar nombrando el servidor: %v", err)
	}
	if v, _ := a.GetChat(c.ID); len(v.Messages) != 4 {
		t.Errorf("el mensaje de un turno que ni empezó no se guarda: %d mensajes", len(v.Messages))
	}
}

// Un 401 en pleno turno agrega de dónde sale la clave, sin mostrarla.
func TestChatAuthErrorNamesWhereTheKeyComesFrom(t *testing.T) {
	a := serverApp(t)
	f := &fakeOpenAI{chunks: []string{"Hola"}}
	srv := f.server(t)
	saved, _ := a.SaveModelServer(openAIServer(srv.URL), secretKey, false)
	ev := captureChatEvents(a)
	c, _ := a.NewChat(mustWorkspace(t, a), saved.ID, "m")
	// El chequeo previo pasa (hay modelos) y el 401 llega en el chat.
	if _, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	waitDone(t, ev)
	f.mu.Lock()
	f.reject = true
	f.mu.Unlock()
	if _, err := a.SendChatMessage(c.ID, "otra", "", nil, SendOptions{}); err != nil {
		t.Fatal(err) // el chequeo sigue vigente (30 s): el 401 llega durante el turno
	}
	select {
	case e := <-ev.errs:
		if !strings.Contains(e.Message, "401") || strings.Contains(e.Message, "sk-prueba") || !strings.Contains(e.Message, "Mi nube") {
			t.Errorf("error del turno: %q", e.Message)
		}
	case <-ev.done:
		t.Fatal("debía fallar")
	}
}

func TestLastModelBindings(t *testing.T) {
	a := serverApp(t)
	wsID := mustWorkspace(t, a)
	if lm, err := a.GetLastModel(wsID); err != nil || lm != (LastModel{}) {
		t.Fatalf("vacío: %+v, %v", lm, err)
	}
	if err := a.SetLastModel(wsID, "ollama", " qwen3:8b "); err != nil {
		t.Fatal(err)
	}
	if lm, _ := a.GetLastModel(wsID); lm != (LastModel{ServerID: "ollama", Model: "qwen3:8b"}) {
		t.Errorf("proyecto: %+v", lm)
	}
	other := mustWorkspaceNamed(t, a, "Otro")
	if lm, _ := a.GetLastModel(other); lm.ServerID != "ollama" {
		t.Errorf("el otro proyecto debía caer al global: %+v", lm)
	}
	if err := a.SetLastModel(wsID, "nope", "m"); err == nil {
		t.Error("un servidor inexistente no se recuerda")
	}
	if err := a.SetLastModel(wsID, "", "m"); err == nil {
		t.Error("sin servidor")
	}
	// Un servidor borrado vale como «ninguna elección».
	saved, _ := a.SaveModelServer(store.ModelServer{Kind: store.ServerOllama, Name: "Otro Ollama", BaseURL: "http://x:1", Enabled: true}, "", false)
	_ = a.SetLastModel(wsID, saved.ID, "m")
	_ = a.DeleteModelServer(saved.ID)
	if lm, err := a.GetLastModel(wsID); err != nil || lm != (LastModel{}) {
		t.Errorf("servidor borrado: %+v, %v", lm, err)
	}
}

func mustWorkspaceNamed(t *testing.T, a *App, name string) int64 {
	t.Helper()
	w, err := a.CreateWorkspace(name, "bambu-a1-mini")
	if err != nil {
		t.Fatal(err)
	}
	return w.ID
}

// Anthropic con una clave guardada: la clave viaja como x-api-key y el chat de
// texto funciona de punta a punta con el stream del SDK.
func TestChatWithAnthropicServer(t *testing.T) {
	a := serverApp(t)
	var mu sync.Mutex
	var keys []string
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get("x-api-key"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"id":"claude-sonnet-5-5","type":"model","display_name":"S","created_at":"2026-08-01T00:00:00Z"}],"has_more":false}`)
	})
	mux.HandleFunc("/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys = append(keys, r.Header.Get("x-api-key"))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		ev := func(name, data string) { fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, data) }
		ev("message_start", `{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[],"stop_reason":null,"usage":{"input_tokens":20,"output_tokens":1}}}`)
		ev("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		ev("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hola desde Claude"}}`)
		ev("content_block_stop", `{"type":"content_block_stop","index":0}`)
		ev("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`)
		ev("message_stop", `{"type":"message_stop"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	an, _ := a.store.GetModelServer("anthropic")
	an.BaseURL, an.Enabled, an.APIKeyEnv, an.DefaultModel = srv.URL, true, "", "claude-sonnet-5-5"
	an, err := a.SaveModelServer(an, secretKey, false)
	if err != nil {
		t.Fatal(err)
	}
	ev := captureChatEvents(a)
	c, err := a.NewChat(mustWorkspace(t, a), an.ID, "")
	if err != nil || c.Model != "claude-sonnet-5-5" {
		t.Fatalf("NewChat: %+v, %v", c, err)
	}
	if _, err := a.SendChatMessage(c.ID, "hola", "", nil, SendOptions{}); err != nil {
		t.Fatal(err)
	}
	waitDone(t, ev)
	if got := ev.text(c.ID); got != "Hola desde Claude" {
		t.Errorf("texto: %q", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(keys) == 0 {
		t.Fatal("el servidor no recibió nada")
	}
	for _, k := range keys {
		if k != secretKey {
			t.Errorf("x-api-key = %q", k)
		}
	}
}

// El cliente de un turno (con el chequeo previo) conserva las capacidades del
// cliente de abajo: un Ollama sin visión sigue diciendo que no ve imágenes.
func TestTurnClientKeepsAttachCaps(t *testing.T) {
	a := serverApp(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"version":"0.5.1"}`) })
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"capabilities":["completion","tools"]}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	o, _ := a.store.GetModelServer("ollama")
	o.BaseURL, o.Enabled = srv.URL, true
	o, _ = a.SaveModelServer(o, "", false)
	client, err := a.llmClientForTurn(o)
	if err != nil {
		t.Fatal(err)
	}
	caps := llm.AttachCapsOf(context.Background(), client, "llama3.2:3b")
	if caps.Images || !caps.Known {
		t.Errorf("el cliente envuelto perdió las capacidades de Ollama: %+v", caps)
	}
}
