# S12.b — CLIs como proveedores del chat y pase de UX

Sigue a [S12](s12-hermes-proveedores.md). Notas vigentes: [claude/19-hermes.md](../../claude/19-hermes.md).

## Por qué

El usuario probó S12. En su chat de mini-tools ve **todos sus CLIs instalados**
(Claude Code, Codex, Antigravity, OpenCode) mezclados con sus servidores, en un
selector propio con estado y latencia, y modelos por proveedor («Por defecto»
con su explicación). En ScadLab ve un `<select>` nativo con solo «Ollama».
Además pide un pase de UX del panel de chat entero: igual o mejor que
mini-tools, y usable en paneles angostos del dock.

## Decisiones

1. **Los CLIs son proveedores del chat**, portados de mini-tools sin
   reinventarlos: corren en modo no interactivo con streaming NDJSON, en la
   carpeta del workspace, **con el MCP de ScadLab conectado** (los mismos
   argumentos que ya arma `app_terminal.go`, `mcpLaunchArgs`/`agentLaunchArgs`,
   sobre `mcpconf`). Así el CLI usa `render`, `lint`, `measure`, `preview`… y sus
   pasos se muestran con las tarjetas de Hermes (`chat:step`).
2. **Ids**: un CLI es `cli:<id>` (`cli:claude`, `cli:codex`, `cli:antigravity`,
   `cli:opencode`); un servidor sigue con su id de `model_servers`.
   `chats.provider_id` admite los dos. La conversación del CLI se continúa con
   su id de sesión (`--resume`, `resume <id>`…), guardado en `chats`
   (migración aditiva **v8**: `chats.cli_session TEXT NOT NULL DEFAULT ''`).
3. **Modos**: «Solo consulta» (por defecto, como mini-tools) = el CLI puede leer
   y usar las herramientas de lectura; «Agente» = puede crear checkpoints y usar
   todas las herramientas de ScadLab. Los comandos de shell **no** se permiten en
   ninguno (no se porta la aprobación interactiva, `agentapprove`). Los flags
   por modo y por CLI salen de mini-tools (`PermissionFor` y el argv de cada
   adaptador). En Hermes (servidores) los dos modos son el interruptor
   «Agente» de S12.
4. **Importar desde mini-tools**: solo cuando el usuario lo pide y confirma.
   Abre la base de mini-tools **en solo lectura** (`file:…?mode=ro&immutable=1`)
   y copia nombre, tipo, URL y modelo por defecto de `model_servers`. **Nunca**
   lee `encrypted_token`, `encrypted_headers` ni nada cifrado; nunca abre
   `salt.bin` ni el llavero de mini-tools. Las claves se vuelven a escribir en
   ScadLab. Ruta: `<config del SO>/mini-tools/vault.db`, con la variable
   `SCADLAB_MINITOOLS_DIR` que la reemplaza en pruebas. **Ni en desarrollo ni en
   pruebas se lee el directorio real de mini-tools**: siempre una copia falsa.
5. **Los CLIs se autodetectan, no se configuran** (aclaración del usuario):
   aparecen solos en el selector si están instalados —`PATH`, rutas típicas y el
   `PATH` del shell de login en macOS, como mini-tools— y **no** crean filas en
   `model_servers` ni piden nada en Configuración. Un CLI no instalado se trata
   como lo haga mini-tools (no aparecer o aparecer deshabilitado con «no
   instalado»). La detección se refresca sola: caché corta (≈30 s) y al abrir
   el selector, así un CLI recién instalado aparece sin reiniciar.
   Configuración → Proveedores es **solo** para proveedores por API/servidor;
   puede mostrar, de solo lectura, los CLIs detectados y su versión
   (`ListDetectedCLIs()`, con `--version` en paralelo y cacheado).
6. **Último proveedor y modelo** por proyecto (y global), como en S12
   (`SetLastModel`/`GetLastModel` aceptan ids `cli:*`).

## Origen en mini-tools → destino en ScadLab

| mini-tools | ScadLab | Cómo |
|---|---|---|
| `backend/agents/registry.go` (catálogo, `resolveBin`, `extraLookupDirs`, `Env`, `Launcher`) | `backend/detect` (ya existe) + `backend/clichat/registry.go` | reutilizar `backend/detect` para «instalado»; portar lo que falte (Launcher en Windows, alt bins `gemini`) |
| `backend/agentchat/adapters.go` (Claude, Antigravity, Codex: línea NDJSON → `[]Event`), `opencode.go`, `rawtext.go`, `types.go` | `backend/clichat/adapters.go`, `opencode.go` | portar tal cual; `Event` → `chat:delta` / `chat:step` / `chat:usage` |
| `backend/agentchat/session.go` (argv por CLI y modo, proceso, stream, cancelación, `--resume`) | `backend/clichat/session.go` | portar; cwd = workspace, + flags MCP de ScadLab |
| `backend/agentchat/codetools.go` (`PermissionFor`, modos) | `backend/clichat/modes.go` | portar solo `ask` y `edit`; sin aprobación |
| `backend/agentmodels/models.go` (modelos de cada CLI, caché) | `backend/clichat/models.go` | portar tal cual |
| `app_agent.go` (`SendAgentChat`, `ListAgents`, `AgentModelCatalog`) | `app_chat.go`, `app_chat_cli.go` | adaptar a los bindings del chat de S12 |
| `app_modelserver_health.go` (latencia cacheada 30 s) | `app_servers.go` | ya portado en S12: exponerlo como `ProviderHealth` |
| `frontend/src/components/agent/AgentChat.tsx` (selector de proveedor propio, píldora de latencia, selector de modelo con «Por defecto», barra superior, modo), `HealthIndicators.tsx`, `agentIcon.ts`, `lib/modelServerHealth.ts` | `frontend/src/components/chat/ProviderPicker.tsx`, `ModelPicker.tsx`, `ChatHeader.tsx` | portar la UX; el combobox de S12 conserva búsqueda e insignias |

## Contrato (T5 lo implementa, T6 lo consume)

```go
type ChatProvider struct {
    ID        string `json:"id"`        // "cli:claude" | id de model_servers
    Kind      string `json:"kind"`      // cli | ollama | openai | anthropic
    Name      string `json:"name"`      // "Claude Code", "MacOS · Ollama"
    Vendor    string `json:"vendor"`    // "Anthropic", "OpenAI", "Google", "SST", "" (servidor)
    Installed bool   `json:"installed"` // CLI encontrado; servidor = true
    Path      string `json:"path"`
    Remote    bool   `json:"remote"`    // servidor fuera de loopback
    Enabled   bool   `json:"enabled"`
}
ListChatProviders() ([]ChatProvider, error)          // CLIs instalados + servidores habilitados; liviano (sin --version)
ProviderHealth(id string) (ProviderHealthInfo, error) // {ok, latencyMs, error}; servidores: sondeo cacheado 30 s; CLI: ok si está instalado, latencyMs 0
ChatModelCatalog(id string, refresh bool) (ModelCatalog, error)
type ModelCatalog struct { DefaultHint string `json:"defaultHint"` /* «El que tenga configurado el CLI» | «El primero que devuelva el servidor» */; Models []ModelInfo `json:"models"` }
// ModelInfo de S12 gana `label` y `description` (los CLIs listan «Gemini 4 Argon (High)»…)
NewChat(workspaceId, providerId, model)     // acepta cli:*
SendChatMessage(..., opts SendOptions)      // con cli:*: corre el CLI; opts.Agent = modo Agente, false = Solo consulta
ImportMiniToolsServers(dryRun bool) (MiniToolsImport, error) // {found bool, path string, servers []{name, kind, baseUrl, defaultModel, exists bool}, imported int}
```

Eventos: los de S12 (`chat:delta`, `chat:step`, `chat:usage`, `chat:done`,
`chat:error`). Un CLI que no informa una herramienta no tiene tarjeta.

## Tareas

**T5 — Backend de CLIs del chat e importación · `fullstack-dev`** (primero).
Alcance: `backend/clichat/**` (nuevo), `backend/detect/**` si hace falta,
`backend/store/{migrations,chat}.go` (v8), `app_chat*.go`, `app_servers.go`,
`app_terminal.go` solo para exponer el armado de flags MCP sin duplicarlo,
`frontend/wailsjs/**`, tests. **No** toca `frontend/src`.
Criterio: tests con **CLIs falsos** (scripts en un `PATH` temporal que imitan el
NDJSON de cada uno, incluidas herramientas, uso y sesión): turno con streaming,
paso de herramienta, continuar con la sesión, cancelar, modo solo consulta vs
agente (flags), CLI ausente; importación con una base falsa de mini-tools (sin
leer columnas cifradas: el test lo verifica); migración v8. Verificación Go
completa. Ninguna sesión real de un CLI.

**T6 — Selector de proveedor y modelo + pase de UX del chat · `fullstack-dev`** (después de T5).
Alcance: `frontend/src/components/chat/**`, `frontend/src/lib/chat*.ts`,
`frontend/src/components/settings/providers/**` (botón «Importar servidores de
mini-tools» con confirmación), i18n, `uishot.tsx`, `help-shots.sh`,
`uishot-fit.sh`, `index.html`, `es/index.html`, `CHANGELOG.md`, `claude/19-hermes.md`.
- Selector de proveedor propio (no `<select>`): íconos, CLIs y servidores
  mezclados (CLIs primero, como mini-tools), estado (punto verde/rojo), check,
  y «Agregar proveedor de IA…» que abre Configuración → Proveedores. Píldora
  de latencia. Selector de modelo con «Por defecto» + `defaultHint`, búsqueda e
  insignias de visión/herramientas de S12.
- Barra superior en **una línea compacta**: título editable, proveedor,
  latencia, modelo, workspace (chip), modo «Solo consulta»/«Agente» con su
  explicación. En angosto se pliega en un menú «…», nunca se desborda.
- Estado vacío útil con sugerencias de OpenSCAD (clic = rellena el composer).
- Composer: adjuntos a la vista, atajos visibles, ⌘/Ctrl+Enter, Detener,
  contador de contexto (tokens estimados del borrador + historial vs. ventana
  del modelo si se conoce).
- Tarjetas de pasos más legibles: la de render/preview con miniatura y errores
  con su línea; clic en un checkpoint lo abre en el editor/visor
  (`onCheckpointCreated` / selección existente).
- Markdown cuidado; bloques `.scad` con «Guardar como checkpoint» y «Abrir en el
  editor».
- Lista de conversaciones con búsqueda.
- Funciona de 320 a 1200 px de ancho de panel: vistas del banco a varios
  anchos en `uishot-fit.sh`.
- Capturas **antes** (las de S12, ya en `docs/screenshots`) y **después**,
  miradas: selector con CLIs y servidores, modelos de un CLI, panel angosto,
  estado vacío.

## Integración (architect)
Verificación completa de S12 + prueba de humo con `wails dev`, CLI falso en
`PATH`, servidor simulado y una base falsa de mini-tools.
