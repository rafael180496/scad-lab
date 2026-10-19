package main

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"scadlab/backend/chat"
	"scadlab/backend/clichat"
	"scadlab/backend/i18n"
	"scadlab/backend/llm"
	"scadlab/backend/store"
)

// Servidores de modelos (S12, 12.1): Ollama, cualquier servidor compatible con
// la API de OpenAI (OpenRouter, Google Gemini, LM Studio, vLLM, Groq…) y la API
// de Anthropic, varios de cada tipo. Es el app_modelservers.go de mini-tools,
// adaptado: con el llavero propio de ScadLab (contraseña maestra que se crea al
// guardar la primera clave, ver app_vault.go) y sin cabeceras propias ni TLS
// inseguro.
//
// **La clave de API nunca cruza hacia el frontend**: store.ModelServer solo
// trae HasToken. Llega por SaveModelServer/TestModelServer (token), se cifra en
// el acto y de ahí en adelante solo sale de Go hacia el cliente HTTP del
// servidor. Tampoco se escribe en un log ni en un error: los clientes de
// backend/llm la reciben en Options y no la repiten en ningún mensaje.
//
// El estado (llavero, listas de modelos, chequeo previo) vive acá y no en App:
// app.go no se toca (mismo criterio que chatServices en app_chat.go), un mapa
// por App creado la primera vez que se usa.

// ServerProbe es el resultado de probar una conexión. Un servidor caído NO es
// un error de la llamada: es justo la respuesta que se vino a buscar, y viaja
// con OK=false y el motivo en Message.
type ServerProbe struct {
	OK bool `json:"ok"`
	// Status es el código HTTP cuando el servidor contestó con uno que no es 2xx
	// (401 = clave mala o ausente); 0 si no hubo respuesta HTTP.
	Status int `json:"status"`
	// Message es el motivo del fallo; con OK, la versión del servidor si la dice
	// (Ollama) o vacío.
	Message string `json:"message"`
	Models  int    `json:"models"`
	// ModelIDs son los ids que devolvió el servidor (hasta 500), para elegir el
	// modelo por defecto de la lista en el formulario sin guardar antes.
	ModelIDs  []string `json:"modelIds"`
	LatencyMs int64    `json:"latencyMs"`
}

// maxProbeModelIDs acota la lista de ids que vuelve con una prueba.
const maxProbeModelIDs = 500

// ModelInfo es un modelo de la lista de un servidor con lo que el proveedor
// sabe de él. VisionKnown/ToolsKnown distinguen «no» de «no se sabe»: un
// servidor compatible con OpenAI genérico no informa ninguna de las dos, y ahí
// un «no» nunca bloquea, solo se avisa. Los precios (dólares por millón de
// tokens) solo los publica OpenRouter; 0 = desconocido.
type ModelInfo struct {
	ID string `json:"id"`
	// Label y Description los llenan los CLIs («Gemini 4 Argon (High)» + para qué
	// sirve); un servidor no los trae y la interfaz muestra el id.
	Label               string  `json:"label"`
	Description         string  `json:"description"`
	Vision              bool    `json:"vision"`
	VisionKnown         bool    `json:"visionKnown"`
	Tools               bool    `json:"tools"`
	ToolsKnown          bool    `json:"toolsKnown"`
	ContextTokens       int     `json:"contextTokens"`
	PromptPricePerM     float64 `json:"promptPricePerM"`
	CompletionPricePerM float64 `json:"completionPricePerM"`
}

// LastModel es el último servidor y modelo elegidos en el chat.
type LastModel struct {
	ServerID string `json:"serverId"`
	Model    string `json:"model"`
}

const (
	// probeTimeout acota la prueba entera. Un remoto lento tiene que contestar
	// «no responde» en vez de dejar el botón girando.
	probeTimeout = 20 * time.Second
	// modelsTTL es cuánto vale la lista de modelos de un servidor. El botón de
	// refrescar la salta.
	modelsTTL = 5 * time.Minute
	// listModelsTimeout acota el listado con capacidades: un Ollama con muchos
	// modelos hace un /api/show por cada uno.
	listModelsTimeout = 45 * time.Second
)

type modelsEntry struct {
	list []ModelInfo
	// stamp es el updated_at del servidor al listar: si se editó (otra URL, otra
	// clave), la lista ya no corresponde.
	stamp int64
	at    time.Time
}

type serverService struct {
	mu     sync.Mutex
	models map[string]modelsEntry
	// ListServerModels de un mismo servidor no se repite en paralelo: dos
	// pantallas pidiendo la lista a la vez harían el doble de /api/show.
	listing map[string]*listCall

	health modelHealthCache
}

type listCall struct {
	done chan struct{}
	list []ModelInfo
	err  error
}

var serverServices sync.Map // *App → *serverService

func (a *App) serverSvc() *serverService {
	if v, ok := serverServices.Load(a); ok {
		return v.(*serverService)
	}
	s := &serverService{models: map[string]modelsEntry{}, listing: map[string]*listCall{}}
	actual, loaded := serverServices.LoadOrStore(a, s)
	if !loaded {
		a.onShutdown(func() { serverServices.Delete(a) })
	}
	return actual.(*serverService)
}

// invalidate olvida todo lo cacheado de un servidor (se guardó, se borró, o
// falló la conexión en pleno turno).
func (s *serverService) invalidate(id string) {
	s.mu.Lock()
	delete(s.models, id)
	s.mu.Unlock()
	s.health.invalidate(id)
}

// invalidateAll olvida todo lo cacheado de todos los servidores (cambió la llave
// o se borraron las claves).
func (s *serverService) invalidateAll() {
	s.mu.Lock()
	s.models = map[string]modelsEntry{}
	s.mu.Unlock()
	s.health.invalidateAll()
}

// ── Lista y alta ───────────────────────────────────────────────────────────

// ListModelServers devuelve los servidores guardados, sin sus claves.
func (a *App) ListModelServers() ([]store.ModelServer, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListModelServers()
}

// SaveModelServer crea (id vacío) o actualiza un servidor. token "" conserva la
// clave guardada; clearToken la borra; un token no vacío la reemplaza aunque
// venga clearToken (pedir las dos cosas a la vez no tiene otra lectura que
// «ponla»). La clave se cifra con la llave del llavero propio (contraseña
// maestra, ver app_vault.go): sin contraseña maestra esto falla con el error
// de código vault-uninitialized y sin guardar nada (la interfaz abre «Crear
// contraseña maestra» y reintenta); con la app bloqueada, vault-locked. La
// alternativa es la variable de entorno (apiKeyEnv).
func (a *App) SaveModelServer(s store.ModelServer, token string, clearToken bool) (store.ModelServer, error) {
	if err := a.ready(); err != nil {
		return store.ModelServer{}, err
	}
	s, err := normalizeServer(s)
	if err != nil {
		return store.ModelServer{}, err
	}
	var tok store.TokenWrite
	switch token = strings.TrimSpace(token); {
	case token != "":
		cipher, nonce, err := a.vaultSvc().Encrypt(token)
		if err != nil {
			return store.ModelServer{}, err
		}
		tok = store.TokenWrite{Set: true, Cipher: cipher, Nonce: nonce}
	case clearToken:
		tok.Clear = true
	}
	saved, err := a.store.SaveModelServer(s, tok)
	if err != nil {
		return store.ModelServer{}, err
	}
	a.serverSvc().invalidate(saved.ID)
	return saved, nil
}

// normalizeServer valida lo que viene de la interfaz y deja la URL limpia
// (Anthropic: la oficial si está vacía, sin /v1 final).
func normalizeServer(s store.ModelServer) (store.ModelServer, error) {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return s, store.ErrServerName
	}
	if !store.ValidServerKind(s.Kind) {
		return s, i18n.Errorf(i18n.Msg{ES: "tipo de servidor desconocido %q (usa ollama, openai o anthropic)", EN: "unknown server kind %q (use ollama, openai or anthropic)"}, s.Kind)
	}
	base, err := llm.NormalizeBaseURL(llm.NormalizeBaseURLFor(s.Kind, s.BaseURL))
	if err != nil {
		return s, err
	}
	s.BaseURL = base
	if env := strings.TrimSpace(s.APIKeyEnv); env != "" && !chat.ValidEnvName(env) {
		return s, i18n.Errorf(i18n.Msg{ES: "%q no es un nombre de variable de entorno válido", EN: "%q is not a valid environment variable name"}, env)
	}
	s.APIKeyEnv = strings.TrimSpace(s.APIKeyEnv)
	return s, nil
}

// DeleteModelServer borra un servidor y su clave cifrada. Los chats que lo
// usaron quedan legibles (no hay FK): solo no se puede seguir conversando con
// ellos hasta elegir otro servidor.
func (a *App) DeleteModelServer(id string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.DeleteModelServer(id); err != nil {
		return err
	}
	a.serverSvc().invalidate(id)
	return nil
}

// ── Probar ─────────────────────────────────────────────────────────────────

// TestModelServer prueba una conexión sin guardar: versión (Ollama) y modelos.
//
// token "" usa la clave guardada del servidor s.ID (si existe) para poder
// probar uno ya guardado sin volver a escribirla; sin clave guardada, la
// variable de entorno de s.APIKeyEnv. Para probar «sin clave» un servidor que
// la tiene, el formulario manda el servidor con el id vacío. Todos los fallos
// van en el resultado, nunca como error de la llamada — salvo que la clave
// guardada esté bloqueada (vault-locked / vault-uninitialized): eso sí es un
// error de la llamada, para que la interfaz pida la contraseña y reintente.
func (a *App) TestModelServer(s store.ModelServer, token string) (ServerProbe, error) {
	if err := a.ready(); err != nil {
		return ServerProbe{}, err
	}
	fail := func(err error) (ServerProbe, error) {
		if isVaultLockError(err) {
			return ServerProbe{}, err
		}
		p := ServerProbe{Message: err.Error(), ModelIDs: []string{}}
		var se *llm.StatusError
		if errors.As(err, &se) {
			p.Status = se.Status
		}
		return p, nil
	}
	s, err := normalizeServer(s)
	if err != nil {
		return fail(err)
	}
	token = strings.TrimSpace(token)
	if token == "" {
		if token, err = a.serverToken(s, s.ID); err != nil {
			return fail(err)
		}
	}
	client, err := newServerClient(s, token)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(a.appCtx(), probeTimeout)
	defer cancel()
	start := time.Now()
	version, err := client.Version(ctx)
	if err != nil {
		return fail(err)
	}
	models, err := client.Models(ctx)
	if err != nil {
		return fail(err)
	}
	ids := append([]string{}, models[:min(len(models), maxProbeModelIDs)]...)
	return ServerProbe{OK: true, Message: version, Models: len(models), ModelIDs: ids, LatencyMs: time.Since(start).Milliseconds()}, nil
}

// ── Clientes ───────────────────────────────────────────────────────────────

// serverToken resuelve la clave con la que hablar con el servidor s: la
// guardada (de storedID, que puede ser otro id o vacío) y, si no hay, la que
// nombra la variable de entorno de s. Un servidor local sin autenticación
// devuelve "" sin error. Anthropic sin clave es un error claro, antes de pedir
// nada (la API no contesta sin clave).
func (a *App) serverToken(s store.ModelServer, storedID string) (string, error) {
	if storedID != "" {
		cipher, nonce, err := a.store.ModelServerToken(storedID)
		if err != nil && !errors.Is(err, store.ErrServerNotFound) {
			return "", err
		}
		if len(cipher) > 0 {
			return a.vaultSvc().Decrypt(cipher, nonce)
		}
	}
	if env := s.APIKeyEnv; env != "" {
		if !chat.ValidEnvName(env) {
			return "", i18n.Errorf(i18n.Msg{ES: "%q no es un nombre de variable de entorno válido", EN: "%q is not a valid environment variable name"}, env)
		}
		if key, ok := chat.LookupAPIKey(env); ok {
			return key, nil
		}
		if s.Kind == store.ServerAnthropic {
			return "", i18n.Errorf(i18n.Msg{
				ES: "falta la clave de API: define la variable de entorno %s con tu clave y reinicia ScadLab, o guárdala en Configuración → Proveedores de IA",
				EN: "the API key is missing: set the environment variable %s to your key and restart ScadLab, or save it in Settings → AI providers",
			}, env)
		}
		// Un servidor compatible con OpenAI puede no pedir clave (LM Studio y vLLM
		// locales): se sigue sin ella y, si la exige, contesta 401 con su motivo.
		return "", nil
	}
	if s.Kind == store.ServerAnthropic {
		return "", i18n.New(i18n.Msg{
			ES: "falta la clave de API: guárdala en Configuración → Proveedores de IA o indica la variable de entorno que la tiene",
			EN: "the API key is missing: save it in Settings → AI providers or set the environment variable that holds it",
		})
	}
	return "", nil
}

// newServerClient arma el cliente HTTP. Ollama pide siempre una ventana de
// contexto: su default (2k–4k) recorta el prompt SIN avisar, y el de sistema
// más el código abierto ya pasan de eso; el servidor puede traer la suya.
func newServerClient(s store.ModelServer, token string) (llm.Client, error) {
	opt := llm.Options{Token: token}
	if s.Kind == store.ServerOllama {
		opt.ContextTokens = chatDefaultNumCtx
		if s.ContextTokens > 0 {
			opt.ContextTokens = s.ContextTokens
		}
	}
	return llm.New(s.Kind, s.BaseURL, opt)
}

// chatServer lee un servidor y verifica que se pueda chatear con él.
func (a *App) chatServer(id string) (store.ModelServer, error) {
	s, err := a.store.GetModelServer(id)
	if errors.Is(err, store.ErrServerNotFound) {
		return s, i18n.New(i18n.Msg{ES: "el servidor de modelos no existe (¿se borró? Elige otro en el chat)", EN: "the model server does not exist (was it deleted? Pick another in the chat)"})
	}
	if err != nil {
		return s, err
	}
	if !s.Enabled {
		return s, i18n.Errorf(i18n.Msg{ES: "el servidor %s está deshabilitado (Configuración → Proveedores de IA)", EN: "the server %s is disabled (Settings → AI providers)"}, s.Name)
	}
	return s, nil
}

// serverClient es el cliente SIN chequeo previo: para listar modelos, que tiene
// su propio manejo de errores. Los caminos que arrancan un turno usan
// llmClientForTurn.
func (a *App) serverClient(s store.ModelServer) (llm.Client, error) {
	token, err := a.serverToken(s, s.ID)
	if err != nil {
		return nil, err
	}
	return newServerClient(s, token)
}

// ── Modelos ────────────────────────────────────────────────────────────────

// ListServerModels lista los modelos de un servidor con lo que el proveedor
// sabe de cada uno (visión, herramientas, ventana, precio). La lista se guarda
// 5 minutos por servidor; refresh la salta. Pedir lo mismo a la vez desde dos
// pantallas comparte una sola consulta.
func (a *App) ListServerModels(serverID string, refresh bool) ([]ModelInfo, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	srv, err := a.chatServer(serverID)
	if err != nil {
		return nil, err
	}
	svc := a.serverSvc()
	svc.mu.Lock()
	if e, ok := svc.models[srv.ID]; ok && !refresh && e.stamp == srv.UpdatedAt && time.Since(e.at) < modelsTTL {
		list := append([]ModelInfo(nil), e.list...)
		svc.mu.Unlock()
		return list, nil
	}
	if call, ok := svc.listing[srv.ID]; ok {
		svc.mu.Unlock()
		<-call.done
		return append([]ModelInfo(nil), call.list...), call.err
	}
	call := &listCall{done: make(chan struct{})}
	svc.listing[srv.ID] = call
	svc.mu.Unlock()

	call.list, call.err = a.fetchServerModels(srv)
	svc.mu.Lock()
	delete(svc.listing, srv.ID)
	if call.err == nil {
		svc.models[srv.ID] = modelsEntry{list: call.list, stamp: srv.UpdatedAt, at: time.Now()}
	}
	svc.mu.Unlock()
	close(call.done)
	return append([]ModelInfo(nil), call.list...), call.err
}

func (a *App) fetchServerModels(srv store.ModelServer) ([]ModelInfo, error) {
	client, err := a.serverClient(srv)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.appCtx(), listModelsTimeout)
	defer cancel()
	infos, err := llm.ListModelInfo(ctx, client)
	if err != nil {
		return nil, err
	}
	out := make([]ModelInfo, 0, len(infos))
	for _, m := range infos {
		out = append(out, ModelInfo{
			ID: m.ID, Vision: m.Vision, VisionKnown: m.VisionKnown, Tools: m.Tools, ToolsKnown: m.ToolsKnown,
			ContextTokens: m.ContextTokens, PromptPricePerM: m.PromptPricePerM, CompletionPricePerM: m.CompletionPricePerM,
		})
	}
	// Alfabética para que la búsqueda y el ojo encuentren: OpenRouter lista
	// cientos. La API de Anthropic ya viene con los más nuevos primero y ese
	// orden es el que importa.
	if srv.Kind != store.ServerAnthropic {
		sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].ID) < strings.ToLower(out[j].ID) })
	}
	return out, nil
}

// serverModelInfo busca un modelo en la lista guardada del servidor (sin salir
// a la red). false si no se listó todavía o el servidor no lo trae. Lo usa el
// chat para avisar, antes de mandar, que un modelo no ve imágenes.
func (a *App) serverModelInfo(serverID, model string) (ModelInfo, bool) {
	svc := a.serverSvc()
	svc.mu.Lock()
	defer svc.mu.Unlock()
	for _, m := range svc.models[serverID].list {
		if m.ID == model {
			return m, true
		}
	}
	return ModelInfo{}, false
}

// ListProviderModels es el alias de S5: los ids de los modelos de un servidor.
func (a *App) ListProviderModels(providerID string) ([]string, error) {
	if cliID, ok := clichat.ParseProviderID(providerID); ok {
		ids := []string{}
		for _, m := range clichat.Models(cliID, false) {
			ids = append(ids, m.ID)
		}
		return ids, nil
	}
	list, err := a.ListServerModels(providerID, false)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, m := range list {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// SetLastModel recuerda el servidor y modelo elegidos para el proyecto y como
// los últimos globales.
func (a *App) SetLastModel(workspaceID int64, serverID, model string) error {
	if err := a.ready(); err != nil {
		return err
	}
	serverID, model = strings.TrimSpace(serverID), strings.TrimSpace(model)
	if serverID == "" {
		return i18n.New(i18n.Msg{ES: "falta el servidor", EN: "the server is missing"})
	}
	if clichat.IsProviderID(serverID) {
		// Un CLI no es una fila de model_servers: basta con que exista en el
		// catálogo (recordar la elección no exige que esté instalado ahora).
		if _, ok := clichat.ParseProviderID(serverID); !ok {
			return i18n.Errorf(i18n.Msg{ES: "el CLI %q no es compatible con el chat", EN: "the CLI %q is not supported by the chat"}, serverID)
		}
		if !clichat.ValidModel(model) {
			return i18n.Errorf(i18n.Msg{ES: "el modelo %q no es un id válido", EN: "the model %q is not a valid id"}, model)
		}
	} else if _, err := a.store.GetModelServer(serverID); err != nil {
		return err
	}
	return a.store.SetLastModel(workspaceID, store.LastModel{ServerID: serverID, Model: model})
}

// GetLastModel devuelve la elección del proyecto; si no tiene, la global. Una
// elección que apunta a un servidor que ya no existe vale como «ninguna»: el
// chat cae a su valor por defecto en vez de quedar atado a un servidor borrado.
func (a *App) GetLastModel(workspaceID int64) (LastModel, error) {
	if err := a.ready(); err != nil {
		return LastModel{}, err
	}
	lm, err := a.store.GetLastModel(workspaceID)
	if err != nil || lm.ServerID == "" {
		return LastModel{}, err
	}
	if clichat.IsProviderID(lm.ServerID) {
		// Un CLI que ya no está instalado (o que dejó el catálogo) vale como
		// «ninguna»: el chat cae a su valor por defecto.
		id, ok := clichat.ParseProviderID(lm.ServerID)
		if !ok {
			return LastModel{}, nil
		}
		if d, found := clichat.Default.Get(id, false); !found || !d.Installed {
			return LastModel{}, nil
		}
	} else if _, err := a.store.GetModelServer(lm.ServerID); err != nil {
		return LastModel{}, nil
	}
	return LastModel{ServerID: lm.ServerID, Model: lm.Model}, nil
}

// ── Chequeo previo de un servidor ──────────────────────────────────────────
//
// Un servidor remoto se apaga, cambia de IP o queda detrás de una VPN caída, y
// sin chequeo lo que se ve es un turno que arranca, gira un buen rato y falla
// con un mensaje de red. Antes de CADA turno se hace una pregunta barata
// —Ollama: GET /api/version; los demás: la lista de modelos— con 4 s de plazo.
// Si falla, el turno ni empieza: no se guarda el mensaje del usuario, solo el
// error con el nombre del servidor, su host y la causa.
//
// Para no sumar una ida y vuelta a cada mensaje, un chequeo exitoso vale 30 s y
// se renueva solo con cada respuesta real del chat. Un fallo NO se cachea: el
// siguiente vuelve a preguntar, que es lo que se quiere cuando el usuario
// enciende el servidor y reintenta.

const (
	modelHealthTTL     = 30 * time.Second
	modelHealthTimeout = 4 * time.Second
)

// modelHealthCache recuerda cuándo fue el último chequeo exitoso por servidor.
// Su valor cero está listo para usar y es seguro entre goroutines.
type modelHealthCache struct {
	mu sync.Mutex
	ok map[string]time.Time
	// lat es la latencia (ms) del último chequeo que la midió: la píldora del
	// selector de proveedor la muestra mientras el chequeo vale.
	lat map[string]int64
	// gen se incrementa al invalidar. Un chequeo que arrancó antes no puede
	// guardar su resultado después: lo hizo con la configuración vieja (otra
	// URL, otra clave) y daría por sano lo que ya no es lo mismo.
	gen map[string]uint64
	now func() time.Time // nil = time.Now (los tests lo reemplazan)
}

func (c *modelHealthCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *modelHealthCache) fresh(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.ok[id]
	return ok && c.clock().Sub(t) < modelHealthTTL
}

func (c *modelHealthCache) begin(id string) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen[id]
}

// put guarda un chequeo exitoso, salvo que el servidor se haya invalidado
// desde begin.
func (c *modelHealthCache) put(id string, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen[id] != gen {
		return
	}
	if c.ok == nil {
		c.ok = map[string]time.Time{}
	}
	c.ok[id] = c.clock()
}

// putLatency recuerda lo que tardó el chequeo, con la misma guarda de put.
func (c *modelHealthCache) putLatency(id string, gen uint64, ms int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen[id] != gen {
		return
	}
	if c.lat == nil {
		c.lat = map[string]int64{}
	}
	c.lat[id] = ms
}

func (c *modelHealthCache) latency(id string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lat[id]
}

func (c *modelHealthCache) invalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen == nil {
		c.gen = map[string]uint64{}
	}
	for id := range c.ok {
		c.gen[id]++
	}
	c.ok = nil
	c.lat = nil
}

func (c *modelHealthCache) invalidate(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.ok, id)
	delete(c.lat, id)
	if c.gen == nil {
		c.gen = map[string]uint64{}
	}
	c.gen[id]++
}

// healthTrackedClient renueva el chequeo con cada respuesta real y lo invalida
// si la conexión se cae en pleno turno.
type healthTrackedClient struct {
	llm.Client
	id  string
	gen uint64
	svc *serverService
}

func (c *healthTrackedClient) Chat(ctx context.Context, req llm.ChatRequest, onDelta func(llm.Delta)) (llm.ChatResult, error) {
	res, err := c.Client.Chat(ctx, req, onDelta)
	switch {
	case err == nil:
		c.svc.health.put(c.id, c.gen) // renueva la vigencia
	case llm.IsRetryable(err):
		// El próximo turno tiene que volver a preguntar antes de confiar.
		c.svc.invalidate(c.id)
	}
	return res, err
}

// AttachCaps y ListModelInfo reenvían al cliente de abajo: AttachCapsOf y
// ListModelInfo buscan esas interfaces con una aserción de tipo, y sin esto un
// cliente envuelto parecería «un servidor que no sabe nada» (imágenes supuestas,
// capacidades desconocidas) aunque Ollama, OpenRouter o Anthropic sí las informen.
func (c *healthTrackedClient) AttachCaps(ctx context.Context, model string) llm.AttachCaps {
	return llm.AttachCapsOf(ctx, c.Client, model)
}

func (c *healthTrackedClient) ListModelInfo(ctx context.Context) ([]llm.ModelInfo, error) {
	return llm.ListModelInfo(ctx, c.Client)
}

var (
	_ llm.AttachCapper = (*healthTrackedClient)(nil)
	_ llm.ModelLister  = (*healthTrackedClient)(nil)
)

// llmClientForTurn es serverClient con el chequeo previo: lo que usan los
// caminos que arrancan un turno. Si el servidor no responde devuelve el error y
// no hay cliente, así que nadie llega a registrar nada.
func (a *App) llmClientForTurn(s store.ModelServer) (llm.Client, error) {
	client, err := a.serverClient(s)
	if err != nil {
		return nil, err
	}
	svc := a.serverSvc()
	gen := svc.health.begin(s.ID)
	if !svc.health.fresh(s.ID) {
		ctx, cancel := context.WithTimeout(a.appCtx(), modelHealthTimeout)
		defer cancel()
		start := time.Now()
		if err := runProbe(ctx, s.Kind, client); err != nil {
			return nil, serverUnreachableError(s, err)
		}
		svc.health.put(s.ID, gen)
		svc.health.putLatency(s.ID, gen, time.Since(start).Milliseconds())
	}
	return &healthTrackedClient{Client: client, id: s.ID, gen: gen, svc: svc}, nil
}

// runProbe hace la pregunta barata: Ollama publica su versión; el protocolo de
// OpenAI y el de Anthropic no, y ahí la lista de modelos hace de chequeo.
func runProbe(ctx context.Context, kind string, client llm.Client) error {
	if kind == store.ServerOllama {
		_, err := client.Version(ctx)
		return err
	}
	_, err := client.Models(ctx)
	return err
}

// serverHost es el host:puerto de la URL, lo que se muestra en un error. No se
// muestra la URL completa: puede llevar una ruta o credenciales.
func serverHost(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// serverUnreachableError es el error del chequeo previo: dice qué servidor, en
// qué host y por qué, más qué hacer.
func serverUnreachableError(s store.ModelServer, cause error) error {
	hint := i18n.Msg{ES: "Revisa que esté encendido y accesible desde esta máquina.", EN: "Check that it is on and reachable from this machine."}
	var se *llm.StatusError
	if errors.As(cause, &se) && (se.Status == 401 || se.Status == 403) {
		hint = i18n.Msg{ES: "Revisa la clave de API del servidor.", EN: "Check the server's API key."}
	}
	return i18n.Errorf(i18n.Msg{
		ES: "No se pudo usar el servidor «%s» (%s): %w. %s",
		EN: "Could not use the server “%s” (%s): %w. %s",
	}, s.Name, serverHost(s.BaseURL), cause, i18n.T(hint))
}
