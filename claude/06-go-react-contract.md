# Contrato Go ↔ React (bindings `App`)

Fuente: `app*.go`. Tipos TS generados en `frontend/wailsjs/go/models.ts` con
`wails generate module`. Al cambiar una firma, actualizar esta tabla.

## General — `app.go`, `app_settings.go`

| Binding | Devuelve | Nota |
|---|---|---|
| `GetBootstrap()` | `Bootstrap{version, platform, dataDir, schemaVersion, settings, initError}` | nunca falla: `initError` dice si la base no abrió |
| `SetLanguage(lang)` | — | `"en"`/`"es"`, persiste |
| `GetSettings()` / `SaveSettings(s)` | `store.Settings` | relleno 0–100; `renderTimeoutSec` 10–3600; `autoRender` default `true`; `swatches` (6 `#rrggbb`) solo se escribe si es válido, si no se conserva el guardado |
| `EffectiveWorkspacesRoot()` | `string` | configurada o `~/Documents/ScadLab` |
| `ChooseDirectory(title, start)` / `ChooseFile(title)` | `string` | `""` si se cancela |
| `DetectTools()` | `[]detect.Tool{id, found, path, version}` | openscad + cada CLI; en paralelo |
| `ListPrinters()` | `[]printers.Printer` | de `printers.json` |
| `ListMaterials()` / `SaveMaterial(m)` | `store.Material` | |
| `ListProviders()` / `SaveProvider(p)` | `store.AIProvider{id, kind, name, command, config, enabled}` | `kind`: cli, ollama, openai, anthropic |
| `OllamaModels(baseUrl)` | `[]string` | prueba de conexión + modelos |

## Workspaces — `app_workspaces.go`

| Binding | Devuelve | Nota |
|---|---|---|
| `ListWorkspaces()` | `[]store.Workspace` | recientes primero |
| `CreateWorkspace(name, printerId)` | `store.Workspace` | carpeta + reglas + skills |
| `ImportWorkspace(dir, printerId)` | `store.Workspace` | reglas solo si faltan |
| `UpdateWorkspace(w)` | — | si cambia la impresora reescribe reglas |
| `RemoveWorkspace(id)` | — | no borra la carpeta |
| `OpenWorkspace(id)` / `RescanWorkspace(id)` | `WorkspaceView{workspace, checkpoints, missing}` | `OpenWorkspace` además vigila la carpeta (detiene el watcher anterior) |
| `ReadCheckpoint(id, file)` | `string` | valida que sea un `.scad` del workspace |
| `SetCheckpointUI(id, file, label, collapsed)` | — | |
| `RevealWorkspace(id)` | — | Finder/Explorador |

## Render — `app_render.go` (S1)

| Binding | Devuelve | Nota |
|---|---|---|
| `RenderCheckpoint(id, fileName)` | `render.Result{fileName, status, meshPath, cacheHit, durationMs, diagnostics, log, backend, version}` | `status`: ok, error, cancelled, missing. `error` escribe `RENDER_ERRORS.md`, `ok` lo borra. Un fallo de OpenSCAD no es error del binding |
| `CancelRender(id)` | — | cancela el render en curso del workspace |
| `GetRenderState(id)` | `map[fileName]render.Result` | últimos resultados en memoria (pinta el árbol al reabrir) |

`render.Diagnostic{severity, line, col, file, message}`: `severity` error |
warning, `line`/`col` 1-based (0 = sin ubicación). La ruta de OpenSCAD se
resuelve con `detect.OpenSCAD` y se cachea por ruta configurada.

## Visor 3D — `app_viewer.go`, `app_mesh.go` (S2)

| Binding | Devuelve | Nota |
|---|---|---|
| `GetMeshInfo(meshPath)` | `mesh.Info{triangles, min, max, size, volumeMm3, areaMm2, watertight}` | solo rutas de `<datos>/cache/meshes` con forma de clave; cacheado por clave |
| `SaveSwatches(colors)` | — | exactamente 6 `#rrggbb` (se guardan en minúscula); aparte de `SaveSettings` para no reescribir la configuración desde el visor |
| `SaveScreenshot(dataURL, suggestedName)` | `string` | diálogo nativo (filtro PNG); acepta solo `data:image/png;base64,` con firma PNG; `""` si se cancela; el nombre se limpia y lleva `.png` |

La malla no viaja por binding: `GET /mesh/<64 hex>.stl` la sirve un middleware
del AssetServer (`backend/meshserve`) desde `<datos>/cache/meshes/`, con
`Cache-Control: no-store`. El frontend la pide con `fetch` (ver
`claude/04-frontend.md`, «Visor 3D»).

## Editor — `app_editor.go`, `app_layout.go` (S4)

| Binding | Devuelve | Nota |
|---|---|---|
| `LintCheckpointSource(id, fileName, source)` | `[]scad.Diagnostic{severity, line, col, endLine, endCol, code, source, message}` | linter sobre el código sin guardar; `endLine/endCol` 0 = toda la línea; el editor lo llama con debounce de 400 ms |
| `FormatScad(source)` | `string` | sangría de `settings.editorIndent`; falla solo con un string o comentario sin cerrar |
| `SaveAsCheckpoint(id, parentFile, label, description, source)` | `string` (archivo creado) | checkpoint hijo nuevo (`O_EXCL`), nunca sobrescribe; no renderiza (lo hace el watcher) |
| `GetScadCatalog()` | `scad.Catalog{version, entries}` | autocompletado y hover; `doc` en `{es, en}` |
| `SaveLayout(layout)` | `store.Layout` (acotado) | tamaños de paneles; aparte de `SaveSettings`, como `SaveSwatches` |

Settings del editor (en `store.Settings`): `editorIndent` (2 \| 4, default 4),
`editorFontSize` (10–24, default 13), `editorWordWrap`, `editorMinimap`
(default `false`) y `layout{sidebarWidth, sidebarCollapsed, codeWidth,
bottomHeight, dimsCollapsed}`. Fuera de rango se normalizan al leer; un
`SaveSettings` con `layout` en cero no pisa el guardado.

## Terminal de agentes — `app_terminal.go` (S5)

| Binding | Devuelve | Nota |
|---|---|---|
| `ListAgentChoices()` | `[]AgentChoice{id, name, found, path, enabled, canList}` | liviano: solo resuelve el ejecutable (no corre `--version`) |
| `StartAgentTerminal(workspaceId, agentId, mode, sessionId, cols, rows)` | `string` (termId) | `mode`: `new` \| `resume` \| `continue`; máx. 2 terminales de agente; argv sin shell, `sessionId` validado |
| `WriteTerminal(termId, data)` / `ResizeTerminal(termId, cols, rows)` / `CloseTerminal(termId)` | — | cerrar mata el grupo de procesos |
| `ListTerminals()` | `[]TerminalInfo{id, kind, agentId, workspaceId, cols, rows, startedAt}` | para reenganchar al recargar la UI |
| `ReadTerminalBuffer(termId)` | `TerminalSnapshot{data (base64), seq}` | pantalla reciente para redibujar |
| `ListAgentSessions(workspaceId, agentId)` | `[]agentsessions.Session{id, title, updatedAt}` | filtradas por la carpeta del workspace |
| `SetAutoIterate(enabled)` | — | setting `autoIterate` (auto-iteración, ver `onRenderFinished`) |

## Chat con modelos — `app_chat.go` (S5)

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

## Eventos (Go → React)

Suscripción con `onEvent()` de `frontend/src/lib/events.ts`.

| Evento | Payload | Quién lo emite |
|---|---|---|
| `render:started` | `{workspaceId, fileName}` | `RenderCheckpoint` (también el auto-render del watcher) |
| `render:finished` | `{workspaceId, result}` | `RenderCheckpoint`, siempre, aunque falle el motor (un cancelado llega con `status: cancelled`) |
| `checkpoints:changed` | `WorkspaceView` | watcher: algún `.scad` de la raíz cambió (debounce 300 ms) |
| `terminal:data` | `{id, data (base64), seq}` | PTY de una terminal de agente (S5) |
| `terminal:exit` | `{id, code}` | el proceso de la terminal terminó |
| `chat:delta` | `{chatId, text, seq}` | turno de chat en curso (deltas agrupados cada 40 ms) |
| `chat:done` | `{chatId, messageId, cancelled}` | fin del turno (también al cancelar con texto parcial) |
| `chat:error` | `{chatId, message, messageId}` | error del proveedor; si había texto, se guardó como respuesta parcial |

Watcher (`backend/watch`): raíz del workspace abierto, no recursivo, solo
`.scad`. Si aparece un `.scad` nuevo y `autoRender` está activo, renderiza el
de número más alto.

## Skills — `app_skills.go`

| Binding | Devuelve |
|---|---|
| `ListSkills()` | `[]SkillView{name, description, source, dir, enabled}` |
| `SetSkillEnabled(name, enabled)` | — |
| `CreateSkill(name)` | carpeta creada (kebab-case) |
| `OpenSkillsFolder()` | — |
| `SyncWorkspaceSkills(id)` | — |
