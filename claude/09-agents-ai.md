# Agentes de IA

Mismo catálogo que mini-tools.

| id | Tipo | Comando | Notas |
|---|---|---|---|
| `claude` | CLI | `claude` | lee `CLAUDE.md`, `.claude/skills`, `.mcp.json` (servidor MCP de ScadLab) |
| `opencode` | CLI | `opencode` | lee `AGENTS.md`, `.opencode/skills` (y `.claude/skills`) |
| `codex` | CLI | `codex` | lee `AGENTS.md`, `.agents/skills` |
| `antigravity` | CLI | `agy` | antes Gemini CLI: se detecta también `gemini`; `.agent/skills` |
| `ollama` | servidor | — | `baseUrl`, `model`; `OllamaModels` prueba y lista |
| `openai` | servidor | — | cualquier API compatible (LM Studio, vLLM, OpenRouter); `apiKeyEnv` |
| `anthropic` | servidor | — | API de Anthropic; `apiKeyEnv` |

- Las API keys **no** se guardan: `config.apiKeyEnv` nombra la variable.
- Cada workspace lleva `CLAUDE.md` y `AGENTS.md` con las mismas reglas
  (generadas de `assets/workspace/RULES.md.tmpl`), entre ellas: nunca
  sobrescribir checkpoints, color obligatorio, validar, respetar la placa y
  **no hacer commits**.
## En la app (S5)

- **Terminal** (`backend/pty`, `app_terminal.go`): el CLI corre en la carpeta
  del workspace con `argv` (nunca vía shell), PATH extendido con
  `backend/detect` y `TERM=xterm-256color`. Máximo 2 terminales de agente **por proyecto** (S8; antes
  eran 2 en total).
  Cerrar mata el grupo de procesos (Setsid / `taskkill /T`) para no dejar
  servidores MCP huérfanos.
- **Sesiones** (`backend/agentsessions`), verificadas contra archivos reales:

  | CLI | Dónde | Retomar / última |
  |---|---|---|
  | Claude | `~/.claude/projects/<cwd con [^A-Za-z0-9]→->/<uuid>.jsonl` (se verifica además el `cwd` guardado) | `--resume <id>` / `-c` |
  | Codex | `~/.codex/sessions/AAAA/MM/DD/rollout-…-<uuid>.jsonl` (`session_meta` con `cwd`; se omiten las de `source: exec`) | `resume <id>` / `resume --last` |
  | opencode | `opencode session list --format json` (filtrado por `directory`) | `--session <id>` / `-c` |
  | Antigravity | sin listado verificable | — / `--continue` |

- **Auto-iteración**: `a.onRenderFinished` (hook interno de `app_render.go`);
  solo el checkpoint más nuevo con error; `prompts.Nudge` + Enter aparte (los
  CLIs con Ink/Bubble Tea toman un `\r` pegado como salto de línea); una vez por
  fallo, tope de 5 seguidos.
- **Chat** (`backend/llm`, `backend/chat`, `app_chat.go`): Ollama (NDJSON,
  `num_ctx` 16384), OpenAI-compatible (SSE) y Anthropic (SDK oficial). Prompt
  de sistema de `backend/prompts` (`assets/prompts/system.{es,en}.md`).
- **Servidor MCP** (S6): `ScadLab --mcp --workspace <dir>` (bloque 6.A,
  `backend/mcp`) le da al agente herramientas para renderizar, validar, medir y
  ver sus modelos. ScadLab lo deja configurado solo (bloque 6.B,
  `backend/mcpconf`, `app_mcp.go`), **solo en la configuración de proyecto** —
  nunca la global del usuario—:

  | CLI | Dónde | Entrada |
  |---|---|---|
  | Claude Code | `<ws>/.mcp.json` | `mcpServers.scadlab` |
  | opencode | `<ws>/opencode.json` (o `opencode.jsonc` sin comentarios) | `mcp.scadlab` = `{"type":"local","command":[bin,…],"enabled":true}` |
  | Gemini CLI (anterior) | `<ws>/.gemini/settings.json` | `mcpServers.scadlab` (Antigravity/`agy` **no** lo lee: configuración global por botón, ver [claude/13](13-mcp.md)) |
  | Codex | no lee config de proyecto | flags `-c mcp_servers.scadlab.command="<bin>"` y `-c mcp_servers.scadlab.args=["--mcp","--workspace","<dir>"]` al lanzarlo desde la terminal de ScadLab (valor = TOML; `codex -c key=value`, verificado con `codex mcp list`) |

  Reglas de `mcpconf.Sync`: fusiona **solo la clave `scadlab`** (el resto del
  archivo —orden, sangría, saltos de línea, otras claves— queda byte a byte; los
  campos que el usuario agregó dentro de la entrada se conservan), crea el
  archivo si no existe, la quita si el servidor está apagado (y borra el archivo
  si solo existía por ella), es idempotente (no reescribe si no cambió, así que
  mover la app solo reescribe la ruta) y **no toca un JSON que no se pueda
  leer** ni un `opencode.jsonc` con comentarios: los reporta como
  `skipped-invalid`. Se sincroniza al crear/importar/abrir un workspace y al
  cambiar el interruptor `mcpEnabled` (Configuración → IA, default encendido).
  La ruta del binario es `os.Executable()` resuelto con `EvalSymlinks`. Interfaz:
  `McpSettings.tsx` (bloque de Configuración → IA) y el indicador «MCP ✓» de
  `AgentTerminalTab.tsx`. Contrato: `mcpconf.ServerName` y `mcpconf.Args(dir)` son los de
  `backend/mcp` (6.A): una sola definición de la línea de comandos.

## Bindings (contrato Go ↔ React)

### Terminal de agentes — `app_terminal.go` (S5)

| Binding | Devuelve | Nota |
|---|---|---|
| `ListAgentChoices()` | `[]AgentChoice{id, name, found, path, enabled, canList}` | liviano: solo resuelve el ejecutable (no corre `--version`) |
| `StartAgentTerminal(workspaceId, agentId, mode, sessionId, cols, rows)` | `string` (termId) | `mode`: `new` \| `resume` \| `continue`; máx. 2 terminales de agente por proyecto; argv sin shell, `sessionId` validado |
| `WriteTerminal(termId, data)` / `ResizeTerminal(termId, cols, rows)` / `CloseTerminal(termId)` | — | cerrar mata el grupo de procesos |
| `ListTerminals()` | `[]TerminalInfo{id, kind, agentId, workspaceId, cols, rows, startedAt}` | para reenganchar al recargar la UI |
| `ReadTerminalBuffer(termId)` | `TerminalSnapshot{data (base64), seq}` | pantalla reciente para redibujar |
| `ListAgentSessions(workspaceId, agentId)` | `[]agentsessions.Session{id, title, updatedAt}` | filtradas por la carpeta del workspace |
| `SetAutoIterate(enabled)` | — | setting `autoIterate` (auto-iteración, ver `onRenderFinished`) |

### Chat con modelos — `app_chat.go` (S5)

| Binding | Devuelve | Nota |
|---|---|---|
| `ListChats(workspaceId)` / `GetChat(chatId)` | `[]store.Chat` / `ChatView{chat, messages, running, partial, partialSeq}` | `GetChat` permite retomar un turno en curso |
| `NewChat(workspaceId, providerId, model)` | `store.Chat` | proveedor y modelo quedan fijos en el chat |
| `SendChatMessage(chatId, text, currentFile)` | `store.ChatMessage` (el del usuario) | respuesta por eventos `chat:*`; prompt de sistema = `prompts.SystemPrompt` |
| `CancelChat(chatId)` / `DeleteChat(chatId)` | — | cancelar conserva el parcial |
| `ListProviderModels(providerId)` | `[]string` | Ollama `/api/tags`, OpenAI `/v1/models`, Anthropic lista fija |
| `ChatCheckpointParent(workspaceId, currentFile)` | `string` | padre para «Guardar como checkpoint»: el abierto o el último |

Persistencia: migración 2 (`chats`, `chat_messages`). Las API keys se leen de
la variable que nombra `config.apiKeyEnv` (si no está en el proceso, se pide una
vez a la shell de login en macOS); nunca se guardan ni viajan al frontend.

### Servidor MCP — `app_mcp.go` (S6, bloque 6.B)

| Binding | Devuelve | Nota |
|---|---|---|
| `GetMCPStatus(workspaceId)` | `MCPStatus{enabled, binary, binaryError, workspaceDir, command, files, codexFlags, openscad, openscadPath}` | no escribe nada; `workspaceId` 0 = solo el estado general (sin `files`). `files` = `mcpconf.FileReport{agent, name, path, status, detail}` con `status` ∈ `ok` \| `missing` \| `outdated` \| `stale` \| `off` \| `skipped-invalid` |
| `SyncWorkspaceMCP(workspaceId)` | `mcpconf.Report{files}` | `status` de cada archivo: `created` \| `updated` \| `unchanged` \| `removed` \| `skipped-invalid`; emite `mcp:changed` |
| `SetMCPEnabled(enabled)` | — | guarda `mcpEnabled` y sincroniza **todos** los workspaces registrados (los que tienen carpeta); emite `mcp:changed` |

Setting nuevo `mcpEnabled` (default `true`, sin migración). **`SaveSettings` nunca lo
escribe** (como `dockLayout`): el diálogo de Configuración guarda una copia vieja y
apagaría o encendería el servidor sin reescribir los archivos; solo cambia por
`SetMCPEnabled`. El ejecutable es `os.Executable()` con `filepath.EvalSymlinks`
(variable de paquete `mcpBinary`, que los tests reemplazan); el comando es
`<ejecutable> --mcp --workspace <carpeta absoluta>` (`mcpconf.Args` = `mcp.Args` del
bloque 6.A). `CreateWorkspace`, `ImportWorkspace` y
`OpenWorkspace` sincronizan sin fallar (un error solo se registra); Codex recibe
los flags `-c mcp_servers.scadlab.command=…` y `-c mcp_servers.scadlab.args=[…]`
**antes** del subcomando, armados por `mcpLaunchArgs` en `StartAgentTerminal`.
Detalle del paquete: `backend/mcpconf` (fusión quirúrgica de JSON en `jsonedit.go`).

### Varios proyectos abiertos — `app_projects.go` (S8, bloque 8.A)

Hasta `store.MaxOpenProjects` (4) workspaces como pestañas de una ventana. Lo abierto,
su orden y el activo viven en settings (`openProjects []int64`, `activeProject int64`);
**`SaveSettings` no los escribe** (como `dockLayout`) y solo cambian por estos bindings
(`Store.SetOpenProjects` los guarda en una transacción). Al arrancar no se abre nada
solo: el frontend llama `ListOpenProjects` y reabre cada uno con `OpenProject`.

| Binding | Devuelve | Nota |
|---|---|---|
| `ListOpenProjects()` | `OpenProjectsView{projects: [{workspace, missing}], activeId}` | solo lee; `missing` = la carpeta no existe; un workspace borrado del registro se omite |
| `OpenProject(id)` | `WorkspaceView` | lo agrega (si no estaba) y lo activa; con 4 abiertos y uno nuevo, error claro (ES/EN). Abrir uno abierto solo lo activa y relee. `OpenWorkspace(id)` es su alias |
| `CloseProject(id)` | — | detiene su watcher, cancela su render y **cierra sus terminales de agente**; el activo pasa al vecino. Idempotente |
| `SetActiveProject(id)` | — | solo persiste; error si no está abierto (0 = ninguno) |
| `ReorderProjects(ids)` | — | tolerante: ignora ids ajenos/repetidos y deja al final los no nombrados |
| `ProjectActivity()` | `[]ProjectActivityEntry{id, rendering, latestFile, lastRenderStatus, renderFailed, agentsAlive}` | en el orden de las pestañas; `lastRenderStatus` es el del checkpoint **más nuevo** (`""` = aún no renderizado) |

Evento `projects:changed` (payload `OpenProjectsView`) tras cada cambio. `checkpoints:changed`,
`render:*` y `terminal:*` ya llevan `workspaceId`, y llegan también de los de fondo.

- **Un watcher por proyecto** (`App.watchers`, `map[int64]*watch.Watcher`): los de fondo
  siguen avisando, vinculando la sesión del agente y auto-renderizando. `RemoveWorkspace`
  cierra el proyecto antes de quitarlo.
- **`--add-dir`** (`agentLaunchArgs`): al lanzar un agente se le pasan las carpetas de los
  **otros** proyectos abiertos que existan, en este orden: MCP, `--add-dir`, modo de arranque.
  Claude y Antigravity: `--add-dir <dir>` repetido; Codex: igual, antes del subcomando
  (`resume`); **opencode no tiene la opción** (verificado): solo ve su carpeta. Claude lleva
  `--mcp-config` y `--add-dir`, ambos variádicos: cada grupo termina en otro flag o en nada.
  Las carpetas se **fijan al lanzar**: abrir otro proyecto después no cambia a un agente ya
  abierto («Nueva» lo relanza). Las reglas del workspace dicen que se puede leer lo de los
  otros proyectos pero solo se escribe en el propio.
