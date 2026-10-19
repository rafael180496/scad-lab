# S12 — Hermes: chat agéntico con proveedores de IA

Fase del roadmap: [claude/01-roadmap.md](../../claude/01-roadmap.md#s12--hermes-chat-agéntico-con-proveedores-de-ia-).
Notas vigentes al cerrar: [claude/19-hermes.md](../../claude/19-hermes.md).

## Por qué

El chat de S5 es demasiado pobre: un select con un proveedor, el modelo como
texto libre, sin servidores, sin claves, sin adjuntos y sin herramientas. El
usuario pide que funcione **como el de mini-tools** (mismo autor, misma pila) y
que sea un **agente propio** («Hermes») que use las herramientas de ScadLab para
diseñar, renderizar, mirar y corregir el modelo solo.

Regla de la fase: **lo genérico se porta de mini-tools, no se reinventa**; lo
nuevo es solo lo específico de OpenSCAD.

## Decisiones

1. **Protocolos = los de mini-tools**: `ollama` (NDJSON), `openai` (SSE; cubre
   OpenAI, OpenRouter, Google Gemini por su endpoint compatible
   `https://generativelanguage.googleapis.com/v1beta/openai`, Groq, DeepSeek,
   Mistral, Together, xAI, LM Studio, llama.cpp, vLLM) y `anthropic` (SDK
   oficial, ya está en `go.mod`). Gemini y OpenRouter son **atajos** (presets)
   del tipo `openai`, igual que en mini-tools: un protocolo más sería código
   nuevo sin beneficio.
2. **Servidores de modelos** (varios por tipo): tabla nueva `model_servers`
   (portada de mini-tools, migración aditiva). La migración copia las filas de
   chat de `ai_providers` (`ollama`, `openai`, `anthropic`) **con el mismo id**,
   para que `chats.provider_id` de los chats viejos siga resolviendo; esas filas
   de `ai_providers` no se borran ni se tocan (quedan sin uso para el chat; los
   CLIs siguen en `ai_providers`).
3. **Claves**: AES-256-GCM (`backend/crypto`, portado tal cual) con una **clave
   de datos aleatoria de 32 bytes guardada en el llavero del SO** con
   `github.com/zalando/go-keyring` (lo mismo que usa mini-tools para recordar su
   clave; Go puro: `security` en macOS, wincred en Windows, sin CGO). Servicio
   `ScadLab`, cuenta `model-servers-key`. ScadLab no tiene contraseña maestra
   (mini-tools sí, `vaultgate`): esa parte **no** se porta. La base guarda solo
   `encrypted_token` + `token_nonce`; la clave nunca viaja al frontend (solo
   `hasToken`), nunca se escribe en un log ni en un error. Si el llavero no está
   disponible, guardar una clave falla con un error claro y se ofrece la
   alternativa que ya existía: **variable de entorno** (`apiKeyEnv`), que se
   conserva. Tests con `keyring.MockInit()`. Esto **enmienda la regla 5 de
   `.claude/rules/technical.md`** por pedido explícito del usuario («claves
   guardadas de forma segura, igual que en mini-tools»): nada de secretos en
   texto plano; cifrados con clave en el llavero, o por variable de entorno.
4. **Hermes** = el bucle de herramientas de mini-tools (`agentchat/llmrunner.go`
   + `app_llmtools.go`) con las herramientas de ScadLab **en proceso**: el
   paquete `backend/mcp` expone una caja de herramientas (`mcp.Toolbox`) que
   reutiliza el mismo `env` del servidor MCP, con el `store`, el motor de render,
   el WASM y la caché de partes **de la app inyectados** (no abre una segunda
   base ni un segundo motor). El servidor `--mcp` sigue igual.
5. **Mejoras OpenSCAD** (lo único nuevo):
   - prompt de sistema de Hermes con el bucle crear → render → preview →
     measure → corregir en un checkpoint nuevo; la impresora, los checkpoints, el
     código abierto y la **lista de skills** (nombre + descripción) del
     workspace; las skills se leen bajo demanda con `read_skill`;
   - el resultado de `preview` vuelve al modelo **como imagen** (si el modelo
     tiene visión; si no, solo el texto y se avisa);
   - diagnósticos de render con línea y la línea de código ofensiva (ya los da
     la herramienta `render`);
   - `create_checkpoint` escribe la cabecera `@checkpoint`; el modo de respaldo
     (modelo sin tool calling) conserva «Guardar como checkpoint» del bloque;
   - referencias STL/3MF/OBJ analizadas en Go (bbox, volumen, triángulos,
     estanqueidad, partes y colores del 3MF) con vista previa rasterizada
     (`backend/raster`) y copia a `<ws>/refs/` para `import()`;
   - elegir modelo: la lista marca los que tienen **visión** y **herramientas**
     (capacidades de `attachcaps` de mini-tools) y avisa si hay imagen o
     preview y el modelo no ve.
6. **Workspace nuevo con `.scad` por defecto**: `001-<slug>.scad` válido, con
   cabecera, `$fn`, parámetros de Customizer, `color()` y una pieza simple que
   renderiza. También al importar una carpeta **sin ningún `.scad`**; si ya hay
   alguno, no se toca. Nunca sobrescribe (`O_EXCL`).
7. **Terminal**: el `^[[?1;2c` es la respuesta de xterm a la consulta Device
   Attributes (`ESC [ c`) que el CLI hizo al arrancar: al **reenganchar**,
   `ReadTerminalBuffer` vuelve a escribir esa consulta en xterm y xterm vuelve a
   contestarla por `onData` → `WriteTerminal` → el agente, que ya pasó su
   arranque y la muestra como texto. Arreglo: no reenviar al PTY las respuestas
   que xterm genera **mientras reproduce** la captura o eventos ya vistos
   (bandera durante `x.write(snapshot, cb)`), y filtrar en ese tramo las
   respuestas de consulta (`ESC [ ? … c`, `ESC [ > … c`, `ESC [ n ; m R`,
   `ESC ] 10/11 ; … ST`). Una sola vista por terminal contesta.

## Origen en mini-tools → destino en ScadLab

| mini-tools | ScadLab | Cómo |
|---|---|---|
| `backend/llm/llm.go` (Message con `Images`, `ToolCalls`, `ToolDef`, `Usage`, `Delta`, `ChatResult`, `StatusError`, `IsToolsUnsupported`) | `backend/llm/llm.go` | portar y reemplazar el reducido de S5 (sin `PDFs`) |
| `backend/llm/conn.go`, `ollama.go`, `openai.go`, `anthropic.go` | mismos archivos | portar (tools, imágenes, uso, pensamiento descartado como hoy) |
| `backend/llm/attachcaps.go` (visión/herramientas por modelo, OpenRouter modalities) | `backend/llm/attachcaps.go` | portar |
| `backend/llm/ollama_admin.go` | — | **no** (administrar Ollama no es de esta fase) |
| `backend/crypto/aesgcm.go` | `backend/crypto/aesgcm.go` | portar tal cual (`argon2.go` no) |
| `app_modelservers.go` `llmClientForTurn` + `modelHealthCache` (sondeo de 4 s cacheado 30 s antes del turno) | `app_servers.go` | portar |
| `backend/vaultgate/remember.go` (go-keyring) | `backend/secrets` | adaptar: clave de datos aleatoria en el llavero, sin contraseña maestra |
| `backend/vault/model_servers_repo.go` + migraciones de `model_servers` | `backend/store/servers.go` + migración nueva | adaptar a `store` (sqlite modernc, una migración aditiva) |
| `app_modelservers.go`, `app_modelserver_health.go`, `app_modelserver_usage.go` | `app_servers.go` | adaptar bindings (lista, guardar, borrar, probar, modelos, uso) |
| `backend/agentchat/llmrunner.go`, `app_llmtools.go` | `backend/hermes` + `app_chat.go` | adaptar: bucle, tope de pasos, cancelación, eventos de paso; herramientas = `mcp.Toolbox` |
| `backend/agentchat/attachments.go`, `attach.go`, `app_attachments.go` (`SaveChatFile`, tipo por contenido, imagen 5 MB, máx. 10, `PrepareAttachments`, `forgetAttachments`) | `backend/hermes/attachments.go`, `app_chat_attach.go` | portar; STL/3MF/OBJ (64 MB) → `backend/refmodel` (nuevo). Sin PDF ni texto pegado (fuera de alcance) |
| `backend/agentchat/record.go` (`TurnRecorder`, `SeedHistory`), `types.go` (`Event`, `Usage`) | `backend/hermes/record.go`, `types.go` | portar: retomar tras reiniciar = sembrar historial desde la base |
| `backend/agentchat/conversations.go`, `history.go`, repos `agent_chats*`/`agent_chat_messages` | `backend/store/chat.go` + migración | adaptar sobre `chats`/`chat_messages` (columnas nuevas) |
| `frontend/src/components/ModelServersSettings.tsx`, `ModelServerForm.tsx`, `ModelServerDialog.tsx`, `ModelServerProbeResult.tsx`, `DefaultModelField.tsx`, `lib/modelServerPresets.ts`, `lib/modelServers.ts`, `lib/modelServerHealth.ts` | `frontend/src/components/settings/providers/*`, `frontend/src/lib/modelServers*.ts` | portar con lucide en vez de Material Symbols y tokens `text-ui-N` |
| `components/agent/AgentChat.tsx` (selector de servidor/modelo, composer) | `components/chat/*` | adaptar sobre el panel de S5 |
| `components/agent/ToolCallCard.tsx`, `AttachmentChips.tsx`, `useChatAttachments.ts`, `lib/chatAttachments.ts`, `ChatCodeBlock.tsx`, `lib/chatRename.ts`, `AgentChatHistory.tsx`, `AgentUsagePanel.tsx` | `components/chat/*` | portar |

Costo: mini-tools no lo calcula para servidores HTTP. ScadLab lo estima solo
cuando el proveedor publica precios (OpenRouter, `pricing` de `/models`);
si no, muestra solo tokens. El selector de modelos de mini-tools no tiene
búsqueda: en ScadLab es un combobox con filtro (nuevo, genérico pero pedido).

Lo que **no** se porta: vault con contraseña maestra, notas/brain, aprobación de
herramientas peligrosas (`agentapprove`: las herramientas de ScadLab no borran
nada y escriben solo checkpoints nuevos o `exports/`), administración de Ollama,
límites de cuota de CLIs.

## Tareas y reparto

### Tanda 1 (en paralelo)

**T1 — 12.1 Proveedores y servidores · `fullstack-dev`**

Alcance (exclusivo en la tanda): `backend/llm/**`, `backend/crypto/**`,
`backend/secrets/**`, `backend/imageopt/**`, `backend/store/migrations.go`,
`backend/store/servers*.go`, `backend/store/store_test.go`/`*_test.go` nuevos,
`app_servers.go` (+test), `app_chat.go` (solo para usar servidores en vez de
`ai_providers`), `app.go` si hace falta, `go.mod`/`go.sum`, `frontend/wailsjs/**`,
`frontend/src/i18n/{es,en}.ts`, `frontend/src/components/settings/SettingsDialog.tsx`
(**solo** agregar la pestaña `providers` y quitar el bloque de servidores de chat
de la pestaña `ai`; la pestaña `skills` es de otra persona: no se toca),
`frontend/src/components/settings/providers/**` (nuevo), `frontend/src/lib/modelServers*.ts`,
`frontend/src/components/chat/{ChatHeader,useChatProviders}.tsx|ts` (selector),
`frontend/src/uishot.tsx` y `scripts/help-shots.sh` (vista `settings-providers` y
`chat` con el selector nuevo).

Contrato:

```go
// backend/store/servers.go
type ModelServer struct {
    ID          string         `json:"id"`        // "ollama", "openai", "anthropic" (migrados) o "srv-<hex>"
    Kind        string         `json:"kind"`      // ollama | openai | anthropic
    Preset      string         `json:"preset"`    // clave del atajo de la UI ("gemini", "openrouter"…), informativo
    Name        string         `json:"name"`
    BaseURL     string         `json:"baseUrl"`
    APIKeyEnv   string         `json:"apiKeyEnv"` // alternativa a la clave guardada
    HasToken    bool           `json:"hasToken"`  // nunca la clave
    DefaultModel string        `json:"defaultModel"`
    ContextTokens int          `json:"contextTokens"` // num_ctx de Ollama (0 = 16384)
    Enabled     bool           `json:"enabled"`
    SortOrder   int            `json:"sortOrder"`
    UpdatedAt   int64          `json:"updatedAt"`
}
```

```go
// app_servers.go
ListModelServers() ([]store.ModelServer, error)
SaveModelServer(s store.ModelServer, token string, clearToken bool) (store.ModelServer, error) // token "" = conservar
DeleteModelServer(id string) error               // borra la clave cifrada; los chats quedan legibles
TestModelServer(s store.ModelServer, token string) (ServerProbe, error) // sin guardar; token "" = el guardado si s.ID existe
ListServerModels(serverID string, refresh bool) ([]ModelInfo, error)  // caché en memoria 5 min por servidor
SetLastModel(workspaceID int64, serverID, model string) error         // recuerda por proyecto y global
GetLastModel(workspaceID int64) (LastModel, error)                     // {serverId, model}; cae al global
KeyringStatus() KeyringStatus                                          // {available bool, detail string}

type ServerProbe struct { OK bool; Status int; Message string; Models int; LatencyMs int64 }
type ModelInfo struct { ID string; Vision bool; Tools bool; ContextTokens int; PromptPricePerM, CompletionPricePerM float64 } // precios 0 = desconocido (OpenRouter los da)
type LastModel struct { ServerID string; Model string }
```

Migración (la siguiente libre; **volver a leer `migrations.go` justo antes de
editar**, otra tarea en paralelo podría haber agregado una):
`CREATE TABLE IF NOT EXISTS model_servers (…, encrypted_token BLOB, token_nonce BLOB, …)`
+ `INSERT OR IGNORE INTO model_servers … SELECT … FROM ai_providers WHERE kind IN ('ollama','openai','anthropic')`
(la URL, el modelo y `apiKeyEnv` salen del JSON `config` con `json_extract`).
Último modelo: settings `lastModel` (global) y columna `workspaces.last_model`
(TEXT JSON, default `''`) en la misma migración.

`NewChat(workspaceId, providerId, model)` y `ListProviderModels` siguen
existiendo: `providerId` pasa a ser un id de `model_servers`. `ListProviderModels`
queda como alias que devuelve los ids.

Criterio de terminado: crear, editar, probar y borrar servidores de los 3 tipos
contra servidores HTTP simulados en tests (`httptest`); la clave nunca aparece en
el JSON que va al frontend, ni en la base en claro (test que busca el token en el
archivo `.db`), ni en un error; modelos reales listados con búsqueda y refresco,
con marcas de visión y herramientas; el chat sigue funcionando (texto) con el
servidor elegido; Configuración → Proveedores de IA con atajos (Ollama local y
remoto, LM Studio, llama.cpp, vLLM, OpenAI-compatible, OpenRouter, OpenAI,
Google Gemini, Anthropic, Groq, DeepSeek, Mistral, Together, xAI) y «Probar
conexión»; aviso si el llavero no está disponible. Verificación completa de
`CLAUDE.md`. Entregables laterales: i18n es/en, captura `settings-providers`.

**T2 — 12.2 + 12.3 Caja de herramientas, referencias y `.scad` por defecto · `openscad-dev`**

Alcance (exclusivo en la tanda): `backend/mcp/**`, `backend/refmodel/**` (nuevo),
`backend/workspace/**`, `backend/prompts/**`, `assets/prompts/**`,
`assets/workspace/**`, `app_workspaces.go`, `app_workspaces_test.go`,
`backend/mesh/**` si hace falta un lector de OBJ. **No** toca `frontend/`, i18n
del frontend, `migrations.go`, `app_chat.go` ni `wailsjs`.

Contrato:

```go
// backend/mcp/toolbox.go — las mismas herramientas del servidor, en proceso
type ToolboxOptions struct {
    WorkspaceDir string
    Store  *store.Store      // de la app; nil = abrir como hoy
    Engine *render.Engine    // idem
    WASM   *scadwasm.Runner  // idem
    Parts  *parts.Store      // idem
    Log    io.Writer
    InApp  bool              // suma list_skills y read_skill (y inspect_reference)
}
type ToolSpec struct { Name, Description string; InputSchema map[string]any }
type ToolImage struct { MIME string; Data []byte }
type ToolOutput struct { Text string; Images []ToolImage; IsError bool }
func NewToolbox(opts ToolboxOptions) (*Toolbox, error)
func (t *Toolbox) Tools() []ToolSpec
func (t *Toolbox) Call(ctx context.Context, name string, args map[string]any) (ToolOutput, error)
func (t *Toolbox) Close() error // no cierra lo inyectado
```

- `list_skills`: nombre y descripción del frontmatter de cada
  `<ws>/.claude/skills/*/SKILL.md` (las incluidas y las propias ya sincronizadas
  ahí). `read_skill(name, file?)`: el `SKILL.md` o un archivo dentro de la skill,
  con la misma seguridad de rutas que `checkpointFile` (sin `..`, sin enlaces que
  salgan). Lectura sola: no depende del paquete `backend/skills` (lo está
  cambiando otra tarea).
- `inspect_reference(file)`: corre `refmodel.Analyze` sobre un archivo de
  `<ws>/refs/` y devuelve el informe + la vista previa.

```go
// backend/refmodel — referencias STL / 3MF / OBJ
const MaxBytes = 64 << 20
type Part struct { Name, Color string; Triangles int; Size [3]float64; VolumeMm3 float64 }
type Report struct {
    Format string; Triangles int; Min, Max, Size [3]float64
    VolumeMm3, AreaMm2 float64; Watertight bool; Parts []Part // 3MF: por color; STL/OBJ: una
}
func Analyze(path string) (Report, error)
func Preview(path string, view string, size int) ([]byte, error) // PNG, backend/raster; views como `preview`
func CopyToRefs(workspaceDir, src string) (rel string, err error) // refs/<nombre-seguro>.<ext>, nunca pisa (sufijo -2, -3…)
func (r Report) PromptText(rel, lang string) string // resumen para el modelo + cómo import("refs/x.stl")
```

```go
// backend/prompts
type AgentContext struct { prompts.Context; Skills []SkillInfo; Tools bool; Vision bool; MaxSteps int; References []string }
type SkillInfo struct { Name, Description string }
func AgentSystemPrompt(c AgentContext) string // es/en en assets/prompts/agent.{es,en}.md
```

Con `Tools=false` el prompt es el de S5 (modo de respaldo: bloques de código y
«Guardar como checkpoint»).

```go
// backend/workspace
func WriteStarter(dir, workspaceName, lang string) (fileName string, err error)
// solo si dir no tiene ningún .scad en la raíz; "" sin error si ya había
```

Plantilla `assets/workspace/starter.scad.tmpl`: primera línea
`// @checkpoint parent= desc="<nombre>"`, `$fn`, parámetros de Customizer con
rangos (`/* [Medidas] */ ancho = 40; // [10:1:200]`), un módulo, `color()` en
cada pieza de nivel superior; pasa el linter (SL001–SL009 sin hallazgos) y
renderiza con OpenSCAD real y con el WASM. `CreateWorkspaceIn` e
`ImportWorkspace` la llaman (error = log, no falla el alta).

Criterio de terminado: tests de la caja (cada herramienta en proceso, imagen de
`preview`, rutas hostiles en `read_skill`/`inspect_reference`), de `refmodel`
(STL ASCII y binario, OBJ con caras de 4 lados, 3MF de dos colores generado con
`threemf.Write`; tamaño máximo; `CopyToRefs` sin pisar), del prompt (es/en, con y
sin herramientas) y del `.scad` por defecto (crear, importar vacía, importar con
`.scad` intacta, render real si hay OpenSCAD). `TestServe*` del MCP sigue verde.
Verificación de `CLAUDE.md` (sin `pnpm build`: no toca el frontend).

### Tanda 2 (después de T1 y T2)

**T3 — 12.4 + 12.5 + 12.6 + 12.7 + 12.8 Hermes, adjuntos, conversaciones, terminal y documentación · `fullstack-dev`**

Alcance: `backend/hermes/**` (nuevo), `backend/chat/**`, `backend/store/chat*.go`,
`backend/store/migrations.go`, `app_chat*.go`, `app_terminal.go` si hiciera falta,
`frontend/src/components/chat/**`, `frontend/src/components/terminal/TerminalView.tsx`,
`frontend/wailsjs/**`, i18n, `uishot.tsx`, `help-shots.sh`, `index.html`,
`es/index.html`, `CHANGELOG.md`, `claude/19-hermes.md`, `CLAUDE.md` (fila del
índice), `claude/06-go-react-contract.md`, `claude/07-database.md`,
`claude/09-agents-ai.md`, `.claude/rules/technical.md` (regla 5).

Contrato (los nombres de eventos son contrato con `chatStreams.ts`):

```go
SendChatMessage(chatId int64, text, currentFile string, attachmentIds []string, opts SendOptions) (store.ChatMessage, error)
type SendOptions struct { Agent bool; MaxSteps int } // Agent=false = modo texto; MaxSteps 1–30, default 12
AddChatAttachment(workspaceId int64, path string) (Attachment, error)        // elegir archivo
AddChatAttachmentData(workspaceId int64, name, dataBase64 string) (Attachment, error) // pegar / arrastrar
RemoveChatAttachment(id string) error
ChooseChatAttachment(workspaceId int64) ([]Attachment, error)                // diálogo nativo
type Attachment struct { ID, Kind /* image | model */, Name, Mime string; Size int64; ThumbDataURL string; RefPath string; Summary string }
RenameChat(chatId int64, title string) error
SetChatModel(chatId int64, serverId, model string) error // cambiar de modelo a mitad
```

Eventos: `chat:delta` (igual), `chat:step` `{chatId, step:{id, index, tool, args, status: running|ok|error, text, images:[dataURL], durationMs}}`,
`chat:usage` `{chatId, inputTokens, outputTokens, costUsd}`, `chat:done`, `chat:error`.

Migración (siguiente libre): `chat_messages.parts TEXT DEFAULT ''` (JSON: adjuntos
del usuario; pasos de herramienta del asistente), `chat_messages.input_tokens`,
`output_tokens`, `cost_usd`; `chats.archived` no. Los adjuntos se copian a
`<datos>/chat-attachments/<chatId>/` (imágenes) y los modelos de referencia
además a `<ws>/refs/` (`refmodel.CopyToRefs`).

Criterio de terminado: un ciclo completo contra un servidor simulado
(`httptest`, OpenAI-compatible y Anthropic, con tool calls en stream): el modelo
llama `create_checkpoint` → `render` (error con línea) → `create_checkpoint` →
`render` ok → `preview` (la imagen vuelve en el siguiente request como
`image_url`/`image`) y termina; tope de pasos; cancelar a mitad de una
herramienta; modelo sin herramientas (`IsToolsUnsupported`) cae al modo texto con
aviso; imagen con un modelo sin visión avisa antes de mandar. Chat panel con
tarjetas plegables, chips con miniatura, lista de conversaciones con renombrar y
borrar, tokens y costo. Terminal sin `^[[?1;2c` al reenganchar (test del filtro).
Capturas `chat`, `chat-attachments`, `chat-agent-steps`, `settings-providers`
regeneradas en es/en y **miradas**; temas de ayuda; CHANGELOG; segmento
`claude/19-hermes.md` con su fila en `CLAUDE.md`.

### Tanda 3

**T4 — 12.10 Llavero propio de ScadLab · `fullstack-dev`** (pedido del usuario
después de T1). Reemplaza la clave de datos aleatoria de T1 por el esquema de
mini-tools: `backend/crypto/argon2.go` + `backend/vaultgate` (gate en memoria,
salt en archivo, verifier cifrado, `remember.go`) y su UI de contraseña maestra.

- Se pide **crear** la contraseña maestra (con confirmación e indicador de
  fuerza) solo la **primera vez** que se guarda una clave de API; «Recordar en
  este equipo» viene **marcado**: la llave derivada va al llavero del SO.
- Después todo es automático: la llave se lee **una vez, perezosa**, y queda en
  memoria hasta cerrar; ningún acceso al llavero por llamada.
- Sin recordar o sin llavero (o si macOS niega el acceso): se pide la contraseña
  una vez por sesión, solo cuando algo necesita descifrar; nunca en bucle.
- Cambiar contraseña (re-cifra), olvidar este equipo, restablecer (borra todas
  las claves, `confirmDialog({danger:true})`). Una contraseña olvidada no se
  recupera. La variable de entorno sigue como respaldo.
- Migra lo de T1: claves cifradas con «ScadLab / model-servers-key» se
  re-cifran al crear la contraseña y la entrada vieja se borra.
- macOS: app sin firmar = firma ad-hoc nueva por build → el Keychain puede volver
  a preguntar tras cada actualización; documentar cómo lo resuelve mini-tools.

### Integración (architect)

Revisión de los diffs contra el contrato, capturas miradas, verificación
completa (`gofmt -l .`, `go build`, `go vet`, `go test -race`, Windows
cross-compile, `pnpm build`, `i18n-check`, `uishot-fit`, `uishot-persist`,
`wails build`) y prueba de humo en la app con `SCADLAB_DATA_DIR` temporal y el
servidor simulado.

## Coordinación con la tarea de Skills (otra persona, en paralelo)

No se tocan `app_skills.go`, `backend/skills/**`, los componentes y el editor de
skills ni la pestaña `skills` de `SettingsDialog.tsx`. Hermes **lee** las skills
desde `<ws>/.claude/skills/` (lo que `skills.Sync` ya escribe), no desde el
paquete. En archivos compartidos (`es.ts`, `en.ts`, `uishot.tsx`,
`help-shots.sh`, `index.html`, `CHANGELOG.md`, `CLAUDE.md`, `migrations.go`) solo
ediciones puntuales con `Edit`, releyendo antes; nunca reescribir el archivo
entero.
