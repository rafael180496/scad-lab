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
| `DetectTools()` | `[]detect.Tool{id, found, path, version, source}` | openscad + cada CLI; en paralelo. `source` (solo OpenSCAD): `custom` (ruta fijada), `managed` (descargado por la app) o `system`; `""` si no se encontró |
| `ListPrinters()` | `[]printers.Printer` | de `printers.json` |
| `ListMaterials()` / `SaveMaterial(m)` | `store.Material` | |
| `ListProviders()` / `SaveProvider(p)` | `store.AIProvider{id, kind, name, command, config, enabled}` | `kind`: cli, ollama, openai, anthropic. Desde S12 el chat **no** lee los de tipo servidor: usa `model_servers` (sección siguiente); quedan para los CLIs |

## Workspaces — `app_workspaces.go`

| Binding | Devuelve | Nota |
|---|---|---|
| `ListWorkspaces()` | `[]store.Workspace` | recientes primero |
| `CreateWorkspace(name, printerId)` | `store.Workspace` | alias de `CreateWorkspaceIn(…, "")` |
| `CreateWorkspaceIn(name, printerId, parentDir)` / `PreviewWorkspacePath(name, parentDir)` | `store.Workspace` / `string` | ubicación elegible, ruta final exacta; ver [16](16-persistencia.md). También `SetLastCheckpoint`, `SaveViewer`, `SetWorkspacesRoot` |
| `ImportWorkspace(dir, printerId)` | `store.Workspace` | reglas solo si faltan |
| `UpdateWorkspace(w)` | — | si cambia la impresora reescribe reglas |
| `RemoveWorkspace(id)` | — | no borra la carpeta |
| `OpenWorkspace(id)` / `RescanWorkspace(id)` | `WorkspaceView{workspace, checkpoints, missing}` | `OpenWorkspace` además vigila la carpeta (detiene el watcher anterior) |
| `ReadCheckpoint(id, file)` | `string` | valida que sea un `.scad` del workspace |
| `SetCheckpointUI(id, file, label, collapsed)` | — | |
| `RevealWorkspace(id)` | — | Finder/Explorador |

## Proyectos abiertos — `app_projects.go` (S8)

Hasta 4 workspaces como pestañas de una sola ventana (`store.MaxOpenProjects`); orden y activo en settings
(`openProjects`, `activeProject`). **Al arrancar el backend no abre nada**: la UI llama `ListOpenProjects` y reabre cada
pestaña con `OpenProject`. Uso en la UI: [12-paneles.md](12-paneles.md#pestañas-de-proyecto-s8).

| Binding | Devuelve | Nota |
|---|---|---|
| `ListOpenProjects()` | `OpenProjectsView{projects: [{workspace, missing}], activeId}` | en el orden de las pestañas; `activeId` 0 = ninguno |
| `OpenProject(id)` | `WorkspaceView` | lo agrega (si no estaba), lo activa y vigila su carpeta sin soltar la de los demás; error claro con 4 abiertos. Si ya estaba abierto solo lo activa y lo relee |
| `CloseProject(id)` | — | detiene su watcher, cancela su render y cierra sus terminales; si era el activo pasa al vecino. No es error cerrar uno que no está abierto |
| `SetActiveProject(id)` | — | solo persiste cuál está al frente |
| `ReorderProjects(ids)` | — | tolerante: ignora ids no abiertos o repetidos y deja al final los que no nombra |
| `ProjectActivity()` | `[ProjectActivityEntry{id, rendering, latestFile, lastRenderStatus, renderFailed, agentsAlive}]` | barato; `renderFailed` = falló el último render del checkpoint más nuevo (en esta sesión) |

Evento `projects:changed` (payload `OpenProjectsView`) tras abrir, cerrar, reordenar o activar: también es el **eco** de
lo que hace la propia UI, que lo ignora mientras tiene una operación en vuelo. `render:*`, `terminal:*`, `chat:*` y
`checkpoints:changed` llevan `workspaceId` y llegan **también de los proyectos de fondo**. `OpenWorkspace` sigue existiendo
(compatibilidad); la UI usa `OpenProject`.

## Checkpoints avanzados — `app_checkpoints.go`, `app_chat.go` (S3)

Las operaciones sobre archivos viven en `backend/checkpoint` (ver [claude/15](15-checkpoints.md));
estos bindings resuelven el workspace, validan el nombre (`checkpoint.ValidName`, un `.scad`
suelto de la raíz), mantienen el índice de la base y emiten `checkpoints:changed`.

| Binding | Devuelve | Nota |
|---|---|---|
| `TrashCheckpoint(id, file, branch)` | `[]string` (nombres en la papelera) | con `branch` también todos los descendientes (el de `file` primero). Guarda antes rótulo, plegado y vínculo en `checkpoint_trash` |
| `ListTrash(id)` | `[]checkpoint.TrashedItem{name, original, parent, description, trashedAt (s)}` | lo más reciente primero |
| `RestoreCheckpoint(id, trashedName)` | `string` (nombre con el que volvió) | su nombre si está libre; si no, el siguiente `NNN` con el mismo slug. Devuelve rótulo, plegado y vínculo |
| `SetActiveParent(id, file)` / `GetActiveParent(id)` | — / `string` | `""` limpia / «sin padre activo explícito» (rige el más nuevo) |
| `RenameCheckpoint(id, file, label)` | — | rótulo (`label`): máx. 80 letras, sin saltos; `""` lo quita. El archivo no cambia de nombre |
| `ReadCheckpointPair(id, file)` | `CheckpointPair{file, parent, parentSource, source}` | para el diff; error claro si es una raíz o el padre ya no está (¿papelera?) |
| `ResumeCheckpointSession(id, file)` | `CheckpointResume{kind, terminalId, agentId, chatId}` | `kind: "terminal"` → abrió el CLI con `StartAgentTerminal` en modo `resume` (la UI reengancha `terminalId`); `"chat"` → la UI abre `chatId`. Falla sin vínculo, o con un agente sin id de sesión (Antigravity) |
| `RevealCheckpoint(id, file)` | — | Finder / Explorador, sobre `RevealPath` |
| `SaveChatCheckpoint(workspaceId, chatId, parent, label, desc, source)` | `string` | como `SaveAsCheckpoint`, y guarda `chat_id` si el chat es del workspace |
| `ChatCheckpointParent(workspaceId, currentFile)` | `string` | el archivo abierto; si no, el último checkpoint; **`""` en un workspace sin checkpoints** (S12.c: ya no es un error, el bloque será la raíz). `SaveAsCheckpoint` / `SaveChatCheckpoint` con padre `""` usan `checkpoint.CreateRoot` (falla si ya hay checkpoints: un padre olvidado) |
| `CreateStarterCheckpoint(id)` | `string` | «Crear un modelo de inicio» (visor y Código, S12.c): escribe `001-<nombre>.scad` aunque la papelera tenga algo; `""` si el workspace ya tiene un `.scad` |
| `EnsureAntigravityMCP()` | `{added, state}` | registra `scadlab` en la configuración global de Antigravity si falta (ver [13-mcp.md](13-mcp.md)); lo llama el chat al mostrar `cli:antigravity`, y el backend al enviar un turno y al abrir su terminal |

`WorkspaceView` ganó `activeParent` (`""` = ninguno explícito): lo llevan `OpenWorkspace`,
`RescanWorkspace` y el evento `checkpoints:changed`. Cambiar el activo emite ese mismo evento
(el archivo `.scadlab/active-parent` no es un `.scad` y el watcher no lo ve).
`store.Checkpoint` ganó `agentId` y `chatId` (`sessionId` ya existía): el vínculo con quien lo creó.

**Vínculo con la sesión.** El watcher, al ver un `.scad` nuevo, avanza el padre activo
(`checkpoint.Advance`) y, si hay una terminal de agente abierta en el workspace, guarda el agente y la
sesión más reciente de ese agente para la carpeta (`agentsessions.List`, en otra goroutine). No se
vincula lo que creó la propia app (`SaveAsCheckpoint`, `SaveChatCheckpoint`) ni lo que acaba de volver
de la papelera. La migración 3 de `scadlab.db` agrega `checkpoints.agent_id`, `checkpoints.chat_id` y la
tabla `checkpoint_trash`; `SyncCheckpoints` no toca esas columnas.

## Render — `app_render.go` (S1)

| Binding | Devuelve | Nota |
|---|---|---|
| `RenderCheckpoint(id, fileName)` | `render.Result{fileName, status, meshPath, cacheHit, durationMs, diagnostics, log, backend, version, engine}` | `status`: ok, error, cancelled, missing. `error` escribe `RENDER_ERRORS.md`, `ok` lo borra. Un fallo de OpenSCAD no es error del binding |
| `CancelRender(id)` | — | cancela el render en curso del workspace |
| `GetRenderState(id)` | `map[fileName]render.Result` | últimos resultados en memoria (pinta el árbol al reabrir) |

`engine` (S11): `native` (OpenSCAD instalado, descargado o de ruta fijada), `wasm` (motor integrado, ver
[17-motor-wasm.md](17-motor-wasm.md)) o `""` si no corrió nada (`missing`). Un `.json` de caché sin `engine` se lee como `native`.

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
| `SaveLayout(layout)` | `store.Layout` (acotado) | barra lateral y barra de estado (ver abajo); aparte de `SaveSettings`, como `SaveSwatches` |

Settings del editor (en `store.Settings`): `editorIndent` (2 \| 4, default 4),
`editorFontSize` (9–28, default 13), `editorWordWrap`, `editorMinimap`
(default `false`) y `layout{sidebarWidth, sidebarCollapsed, codeWidth,
bottomHeight, dimsCollapsed, statusBarHidden, workspacesHeight}`. Fuera de rango
se normalizan al leer; un `SaveSettings` con `layout` en cero no pisa el
guardado. Desde S4b `codeWidth` y `bottomHeight` están **obsoletos** (el código
y el panel inferior son paneles acoplables): se conservan en la base y en el
struct, sin uso. `statusBarHidden` es nuevo (menú Ver); la barra lateral sigue
usando `sidebarCollapsed`.

## Paneles acoplables — `app_dock.go` (S4b)

| Binding | Devuelve | Nota |
|---|---|---|
| `SaveDockLayout(layout)` | — | `layout` = el sobre JSON que arma el frontend (`{v, dock}`, sobre dockview). `""` borra el guardado (vuelve al predeterminado); un texto que no sea un objeto JSON o pase de 256 KB se rechaza sin tocar lo guardado. Se llama con espera de 600 ms al mover/dividir/ocultar y al salir |
| `SaveDockLayouts(list)` | `[]store.DockLayoutEntry{name, json}` | lista **completa** de diseños propios; devuelve lo guardado: nombres recortados (≤ 40 letras), sin repetidos (sin distinguir mayúsculas; gana el último), JSON válido; rechaza más de 30 o uno inválido |

Settings nuevos (en `store.Settings`, una clave por campo, sin migración):
`dockLayout` (texto) y `dockLayouts` (`[{name, json}]`). Son **opacos para Go**:
solo se acota el tamaño y se valida que sea un objeto JSON; quien los entiende y
valida (versión `v`, ids de panel conocidos, `contentComponent: "slot"`) es el
frontend, que ante cualquier duda abre con el diseño predeterminado.
`SaveSettings` **nunca** escribe estas dos claves (el diálogo de Configuración
tiene una copia vieja): solo cambian por estos bindings.

## Apariencia — `app_appearance.go` (S10)

| Binding | Devuelve | Nota |
|---|---|---|
| `SaveAppearance(appearance)` | `store.Appearance` (normalizado) | `{uiScale, editorFontSize, terminalFontSize, chatFontSize}`; aparte de `SaveSettings` (como `SaveLayout`) porque Configuración → Apariencia aplica y guarda **al instante**, con el diálogo abierto, y no debe reescribir la configuración entera |

Settings nuevos (en `store.Settings`, una clave `settings` por campo, sin
migración): `uiScale` (uno de `UIScalePresets` = 90, 100, 115, 130, 150; default
100), `terminalFontSize` y `chatFontSize` (9–28, default 13) y `editorFontSize`
(que pasó de 10–24 a 9–28). Un valor inválido no es un error: `Normalize` deja
la escala en 100 % y las letras fuera de 9–28 en 13 (misma política que
`editorIndent`), tanto en `GetSettings` como en `SaveSettings` y `SaveAppearance`.
`Layout.workspacesHeight` (56–640, default 150) es el alto de la lista de
workspaces dentro de la barra lateral. El `SaveSettings` de Configuración manda
la apariencia vigente fusionada con su borrador para no pisarla con la de cuando
se abrió el diálogo.

## Exportación — `app_export.go` (S9)

| Binding | Devuelve | Nota |
|---|---|---|
| `PreviewExportParts(id, fileName)` | `exporter.Preview{parts, warnings}` | sin escribir nada; la primera vez renderiza el checkpoint a 3MF con OpenSCAD (queda en caché) |
| `ExportCheckpoint(id, fileName, format)` | `exporter.Result{path, format, parts, warnings}` | `format`: `stl` \| `3mf` \| `stl-per-color` (.zip); diálogo nativo de guardar; `path` vacío si se cancela |
| `DetectSlicers()` | `[]Slicer{id, name, path, found}` | hoy solo `bambu-studio`; gana `settings.bambuStudioPath` si existe, si no autodetecta (macOS `/Applications` y `~/Applications`; Windows `%ProgramFiles%\Bambu Studio`) |
| `OpenInSlicer(slicerId, filePath)` | — | valida ruta absoluta, archivo existente y extensión `.3mf`/`.stl`, y que el slicer esté; abre **la app** desacoplada (macOS `open -a <app> <archivo>`; Windows `exec(<exe>, <archivo>)`). Nunca la CLI de Bambu; solo por un clic del usuario |
| `RevealPath(path)` | — | muestra el archivo en el Finder/Explorador (`open -R` / `explorer /select,`); la ruta tiene que existir |

`exporter.PartInfo{name, color, triangles, extruder, uncolored}`. Setting nuevo
`bambuStudioPath` (vacío = autodetectar, sin migración). Los lanzamientos pasan
por la variable de paquete `launchApp`, que los tests reemplazan por un
registrador: ninguna prueba abre una aplicación (regla 9 de `technical.md`).

## Descarga de OpenSCAD — `app_openscad.go` (S11)

Detalle y decisiones: [claude/18](18-descarga-openscad.md).

| Binding | Devuelve | Nota |
|---|---|---|
| `GetOpenSCADDownload()` | `OpenSCADDownload{version, sizeBytes, supported, installed, installedPath, running, wasmVersion}` | `supported` = hay descarga para este SO/arquitectura (darwin arm64/amd64, windows amd64); `wasmVersion` = versión del motor integrado |
| `DownloadOpenSCAD()` | `OpenSCADDownload` | **bloquea** hasta terminar y emite `openscad:download`; una sola a la vez (si hay otra, error). Cancelada: error con código `download-cancelled` (la UI no lo muestra como fallo). Al terminar bien olvida la ruta de OpenSCAD cacheada |
| `CancelOpenSCADDownload()` | — | corta la descarga en curso; sin descarga no hace nada |
| `RemoveDownloadedOpenSCAD()` | `OpenSCADDownload` | borra `<datos>/tools/openscad/`; rechazado con una descarga en curso; no toca el OpenSCAD del usuario |

## Eventos (Go → React)

Suscripción con `onEvent()` de `frontend/src/lib/events.ts`.

| Evento | Payload | Quién lo emite |
|---|---|---|
| `render:started` | `{workspaceId, fileName}` | `RenderCheckpoint` (también el auto-render del watcher) |
| `render:finished` | `{workspaceId, result}` | `RenderCheckpoint`, siempre, aunque falle el motor (un cancelado llega con `status: cancelled`) |
| `openscad:download` | `{phase, received, total, message}` | `DownloadOpenSCAD`: `phase` = `download` \| `verify` \| `extract` \| `done` \| `error` \| `cancelled`; el progreso va acotado a ~10 por segundo; `message` solo con `error`; en `extract` de un dmg `total` es 0 (no hay avance) |
| `checkpoints:changed` | `WorkspaceView` | watcher: algún `.scad` de la raíz cambió (debounce 300 ms); también al cambiar el padre activo, la papelera, un rótulo o un vínculo con la sesión |
| `terminal:data` | `{id, data (base64), seq}` | PTY de una terminal de agente (S5) |
| `terminal:exit` | `{id, code}` | el proceso de la terminal terminó |
| `chat:delta` | `{chatId, text, seq}` | turno de chat en curso (deltas agrupados cada 40 ms) |
| `chat:step` | `{chatId, workspaceId, step{id, index, tool, args, status, text, images[], durationMs, textAt}}` | Hermes y los CLIs: un paso de herramienta, dos veces con el mismo `id` (`status` `running` al arrancar; `ok` o `error` al terminar). `images` son data URL (PNG en vivo); `textAt` = texto visible previo en unidades UTF-16. `workspaceId` (S12.c) deja que la UI abra el checkpoint que un paso acaba de crear (`create_checkpoint`, `import_mesh` o `autosave`) sin saber de qué chat vino. Los pasos `autosave` y `render` de la red de seguridad llegan una sola vez, ya terminados |
| `chat:usage` | `{chatId, inputTokens, outputTokens, costUsd}` | uso acumulado del turno tras cada vuelta; `costUsd` 0 = «no informado» (solo hay costo con precios publicados) |
| `chat:done` | `{chatId, messageId, cancelled}` | fin del turno (también al cancelar con texto parcial o pasos) |
| `mcp:changed` | `{enabled}` | se apagó/encendió el servidor MCP o se resincronizó un workspace (`SetMCPEnabled`, `SyncWorkspaceMCP`) |
| `mcp:antigravity` | `{added, error}` | `EnsureAntigravityMCP` (S12.c): ScadLab se registró en la configuración global de Antigravity (`added`, toast) o no pudo (`error` con el motivo y qué hacer) |
| `chat:error` | `{chatId, message, messageId}` | error del proveedor; si había texto o pasos, se guardaron como respuesta parcial |

Watcher (`backend/watch`): raíz del workspace abierto, no recursivo, solo
`.scad`. Si aparece un `.scad` nuevo y `autoRender` está activo, renderiza el
de número más alto.

## Skills — `app_skills.go`

| Binding | Devuelve |
|---|---|
| `ListSkills()` | `[]SkillView{name, description, source, dir, enabled}` |
| `SetSkillEnabled(name, enabled)` | — |
| `CreateSkill(name)` | carpeta creada (kebab-case); desde la plantilla, con `name` y descripción pendiente; **no abre nada** |
| `GetSkillsFolder()` | `SkillsFolder{path, default, custom, exists, count}` |
| `OpenSkillsFolder()` | — (crea la carpeta si falta; error claro si el sistema no puede abrirla) |
| `ChangeSkillsFolder(dir, move)` | `SkillsFolderChange{path, moved[], skipped[], syncError}`; `dir` vacío = predeterminada |
| `DeleteSkill(name)` | `DeleteSkillResult{trash, removed, kept[], syncError}` — a la papelera de la app; solo propias |
| `RenameSkill(old, new)` | `RenameSkillResult{name, removed, kept[], syncError}` — solo propias |
| `SyncWorkspaceSkills(id)` | — |
| `ListSkillFiles(name)` | `[]skills.File{path, size, text}` (SKILL.md primero) |
| `ReadSkillFile(name, rel)` | texto; las incluidas también |
| `SaveSkillFile(name, rel, content)` | `SaveSkillFileResult{saved, diagnostics[], syncError}`; `saved:false` si el SKILL.md tiene errores |
| `CreateSkillFile(name, rel)` / `RenameSkillFile(name, from, to)` / `DeleteSkillFile(name, rel)` | — / syncError / ruta en la papelera |
| `ValidateSkillMarkdown(name, text)` | `[]skills.Diagnostic{line, severity, code, message}` |

Detalle (papelera, confinamiento de rutas, copias en workspaces): [20-skills-propias.md](20-skills-propias.md).

## Servidores de modelos — `app_servers.go` (S12, 12.1)

Ollama, compatibles con OpenAI (OpenRouter, Gemini, LM Studio, vLLM…) y Anthropic, varios por tipo, en la tabla
`model_servers` ([07-database.md](07-database.md)). La clave de API se cifra con AES-256-GCM con la llave derivada de la contraseña
maestra (`backend/vaultgate`, ver la sección siguiente); **nunca** cruza hacia el frontend (`hasToken`) ni aparece en un log o error.
Detalle: [19-hermes.md](19-hermes.md).

| Binding | Devuelve | Nota |
|---|---|---|
| `ListModelServers()` | `[]store.ModelServer{id, kind, preset, name, baseUrl, apiKeyEnv, hasToken, defaultModel, contextTokens, enabled, sortOrder, updatedAt}` | sin claves |
| `SaveModelServer(s, token, clearToken)` | `store.ModelServer` | id vacío = alta; `token` vacío conserva la guardada. Con `token` y sin contraseña maestra falla con el error de código `vault-uninitialized`; con la app bloqueada, `vault-locked` (alternativa sin contraseña: `apiKeyEnv`) |
| `DeleteModelServer(id)` | — | borra también la clave cifrada; los chats quedan legibles |
| `TestModelServer(s, token)` | `ServerProbe{ok, status, message, models, modelIds[], latencyMs}` | sin guardar; un servidor caído **no** es error del binding (sí lo son `vault-locked` / `vault-uninitialized` si la clave guardada no se puede abrir) |
| `ListServerModels(serverId, refresh)` | `[]ModelInfo{id, label, description, vision, visionKnown, tools, toolsKnown, contextTokens, promptPricePerM, completionPricePerM}` | caché de 5 min por servidor; `…Known=false` = «no se sabe» (no bloquea, solo avisa) |
| `ListProviderModels(providerId)` | `[]string` | alias de S5: los ids de `ListServerModels` (o de los modelos del CLI si es `cli:*`) |
| `SetLastModel(workspaceId, serverId, model)` / `GetLastModel(workspaceId)` | — / `LastModel{serverId, model}` | por proyecto y global; acepta `cli:*`; cae al global y a «ninguno» si el servidor ya no existe o el CLI ya no está instalado |

### Llavero propio — `app_vault.go` (S12, 12.10)

Detalle y flujos: [21-llavero.md](21-llavero.md). La contraseña solo entra por `CreateMasterPassword`, `UnlockVault` y
`ChangeMasterPassword`; ni ella ni la llave salen nunca de Go. Reemplaza a `KeyringStatus()` de T1 (ya no existe).

| Binding | Devuelve | Nota |
|---|---|---|
| `VaultStatus()` | `VaultStatus{initialized, unlocked, remembered, keyringAvailable, keyringDetail}` | **no** lee la llave recordada (con `remembered` puede haber `unlocked=false` hasta el primer uso); sondea el llavero una vez por sesión |
| `CreateMasterPassword(password, remember)` | `VaultResult{rememberFailed, detail, lostKeys}` | mínimo 8 caracteres; solo si no hay una (`ErrAlreadyInitialized`); re-cifra las claves de T1 y borra su entrada; `remember` guarda la **llave derivada**. Que el llavero no deje no es error: `rememberFailed=true` |
| `UnlockVault(password, remember)` | `VaultResult` | error de código `vault-wrong-password`; `remember` enciende o apaga la entrada del llavero |
| `ChangeMasterPassword(oldPassword, newPassword)` | `VaultResult` | re-cifra todas las claves en una transacción; `vault-wrong-password` si la vieja no es |
| `ForgetThisDevice()` | — | «Olvidar este equipo»: borra la entrada del llavero; la sesión actual sigue desbloqueada |
| `SetRememberDevice(remember)` | — | encender necesita la app desbloqueada (`vault-locked`); apagar = `ForgetThisDevice` |
| `ResetVault()` | `number` | cuántas claves se borraron; borra claves, verificador, `vault.salt` y entradas del llavero |

Códigos de error (`errorCode(e)` en el frontend, `lib/vault.ts`): `vault-uninitialized` (abrir «Crear contraseña maestra»),
`vault-locked` (abrir «Desbloquear») y `vault-wrong-password`. Los devuelven `SaveModelServer` (con token),
`TestModelServer`, `ListServerModels` y `SendChatMessage` (esta última **antes** de guardar el mensaje);
`withVault()` abre el diálogo y reintenta la llamada una sola vez.

## Chat con Hermes — `app_chat.go`, `app_chat_attach.go` (S12, 12.4–12.6)

Tabla completa del chat (con `NewChat`, `ListChats`…) en [09-agents-ai.md](09-agents-ai.md#chat-con-modelos--app_chatgo-s5-s12).

| Binding | Devuelve | Nota |
|---|---|---|
| `SendChatMessage(chatId, text, currentFile, attachmentIds, opts)` | `store.ChatMessage` (el del usuario) | `opts` = `SendOptions{agent?, maxSteps}` (`maxSteps` 1–30, 0 = 12). **El modo sale de la conversación** (`chat.mode`, ver `SetChatMode`); `agent` es un override opcional (`*bool`) **solo de ese turno** —`false` = modo texto de S5, `true` = herramientas— y no cambia lo guardado; la interfaz no lo manda salvo cuando el modelo no llama herramientas. Texto vacío solo con adjuntos. Falla **antes** de guardar nada si el servidor no responde o una imagen va a un modelo que informó no ver |
| `AddChatAttachment(workspaceId, path)` | `hermes.Attachment{id, kind, name, mime, size, thumbDataUrl, refPath, summary}` | `kind` `image` (≤ 5 MB) o `model` (STL/3MF/OBJ ≤ 64 MB, copiado a `refs/` y analizado); por contenido |
| `AddChatAttachmentData(workspaceId, name, dataBase64)` | `hermes.Attachment` | pegar / arrastrar; acepta data URL; `name` solo aporta nombre y extensión |
| `ChooseChatAttachment(workspaceId)` | `[]hermes.Attachment` | diálogo nativo; si uno falla no queda ninguno de la tanda y el error nombra el archivo |
| `RemoveChatAttachment(id)` | — | borra lo que se creó (la copia en `refs/` solo si la hizo el adjunto); no es error quitar uno que ya no está |
| `RenameChat(chatId, title)` | — | recorta y colapsa espacios, máx. 120 letras, no vacío |
| `SetChatMode(chatId, mode)` | — | `mode`: `"agent"` (también `""`) o `"ask"`; se guarda en `chats.mode` y vale **solo para esa conversación** (no cambia otra ni deja un predeterminado: toda conversación nueva nace en Agente). Un modo desconocido o un chat inexistente fallan sin tocar nada. Un turno en curso termina con el modo con que empezó |
| `SetChatModel(chatId, serverId, model)` | — | cambia servidor y modelo a mitad de camino (modelo vacío = el del servidor); también es el último elegido del proyecto |
| `GetChat(chatId)` | `ChatView{chat, messages, running, partial, partialSeq, steps[], usage}` | `steps` y `usage` son los del turno en curso |

`store.Chat` ganó `mode` (S12.c: `""` = Agente, `"ask"` = Solo consulta; viaja en `ListChats`, `GetChat` y `NewChat`). `store.ChatMessage` ganó `parts` (JSON: adjuntos del usuario o pasos del asistente), `inputTokens`, `outputTokens` y `costUsd`.

## CLIs como proveedores del chat — `app_chat_cli.go`, `app_servers_minitools.go` (S12.b, T5)

Los CLIs (Claude Code, Codex, Antigravity, OpenCode) se **autodetectan** y son un proveedor más del chat; no hay filas en `model_servers`.
Detalle, flags por modo y decisiones: [19-hermes.md](19-hermes.md#clis-como-proveedores-s12b).

```go
type ChatProvider struct { ID, Kind, Name, Vendor string; Installed bool; Path string; Remote, Enabled bool } // json camelCase
// ID "cli:<id>" | id de model_servers; Kind cli|ollama|openai|anthropic; Vendor "" en un servidor
type DetectedCLI struct { ID, Name, Vendor string; Installed bool; Path, Version string }             // ID corto: "claude"
type ProviderHealthInfo struct { OK bool; LatencyMs int64; Error string }                              // ok, latencyMs, error
type ModelCatalog struct { DefaultHint string; Models []ModelInfo }                                    // defaultHint, models
type MiniToolsServer struct { Name, Kind, BaseURL, DefaultModel string; ContextTokens int; Exists bool; Error string }
type MiniToolsImport struct { Found bool; Path string; Servers []MiniToolsServer; Imported int }

ListChatProviders() ([]ChatProvider, error)             // CLIs instalados (primero) + servidores habilitados; sin --version; detección con caché de ~30 s
RefreshChatProviders() ([]ChatProvider, error)          // lo mismo volviendo a detectar (al abrir el selector); extra del contrato
ListDetectedCLIs() ([]DetectedCLI, error)               // los cuatro, instalados o no, con --version en paralelo y cacheado; solo lectura
ProviderHealth(id string) (ProviderHealthInfo, error)   // servidor: sondeo de S12 cacheado 30 s con su latencia; CLI: ok si está instalado, latencia 0
ChatModelCatalog(id string, refresh bool) (ModelCatalog, error) // CLI: modelos del propio CLI (sin «Por defecto»: la pone la UI con defaultHint)
ImportMiniToolsServers(dryRun bool) (MiniToolsImport, error)    // dryRun=true no escribe; false crea los que no existen, sin clave
```

`NewChat`, `SendChatMessage`, `SetChatModel`, `SetLastModel`/`GetLastModel` y `ListProviderModels` aceptan ids `cli:*`. Con un CLI,
el modo es el de la conversación (`chat.mode`; `SendOptions.Agent`, si viene, lo fuerza en ese turno) y `maxSteps` se ignora; los eventos son los de S12 (`chat:delta`,
`chat:step`, `chat:usage`, `chat:done`, `chat:error`). `store.Chat` gana `cliSession` (el id de conversación del CLI).
Un proveedor caído o un CLI ausente **no** son error de `ProviderHealth`: viajan con `ok=false` y el motivo en `error`.

## Editar un STL — `app_meshedit.go` (S13)

Notas y contrato de `Scene`: [23-edicion-stl.md](23-edicion-stl.md). Los tipos de `backend/meshedit` (`Scene`,
`Transform`, `Primitive`, `Cut`) y `refmodel.Report` llevan etiquetas JSON en camelCase.

| Binding | Devuelve | Nota |
|---|---|---|
| `ChooseModelFile()` | `string` ("" si se cancela) | diálogo nativo con filtro STL/3MF/OBJ (extra del contrato: el diálogo solo se abre desde Go) |
| `AnalyzeModelFile(path)` | `refmodel.Report{format, triangles, min, max, size, volumeMm3, areaMm2, watertight, parts[{name, color, triangles, size, volumeMm3}]}` | mide antes de importar; no copia nada |
| `ImportModel(workspaceId, path, {centerOnBed, label})` | `ImportResult{file, ref, report, warnings[]}` | copia a `refs/` (sin pisar), convierte 3MF/OBJ a STL (el original queda), crea el checkpoint base **raíz** (`O_EXCL`); avisos: malla abierta, > 500 000 triángulos, 3MF multicolor, no cabe en la placa |
| `PrepareMeshEdit(workspaceId, file)` | `EditBase{ref, parent, meshPath, report, fromImport, model, color}` | import base → su STL y su pose; si no, **congela** la malla renderizada en `refs/<checkpoint>.stl` (reutiliza por hash). `meshPath` = malla CRUDA de `ref`, servible por `/mesh/` |
| `PreviewMeshEdit(workspaceId, scene)` | `string` (código) | no escribe nada |
| `ApplyMeshEdit(workspaceId, parent, label, scene)` | `string` (checkpoint creado) | hijo de `parent` (`O_EXCL`, padre activo que avanza, no se vincula a una sesión); no renderiza: lo hace el watcher |

## Bindings documentados en su segmento

Para que este archivo no crezca sin tope, algunas áreas tienen su tabla de
bindings junto al resto de su documentación:

| Área | Segmento |
|---|---|
| Medición e impresión (`app_parts.go`) | [claude/14-medicion.md](14-medicion.md#bindings-contrato-go--react) |
| Terminal de agentes, chat y servidor MCP (`app_terminal.go`, `app_chat.go`, `app_mcp.go`) | [claude/09-agents-ai.md](09-agents-ai.md#bindings-contrato-go--react) |
