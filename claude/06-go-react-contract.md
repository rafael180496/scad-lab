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

## Eventos (Go → React)

Suscripción con `onEvent()` de `frontend/src/lib/events.ts`.

| Evento | Payload | Quién lo emite |
|---|---|---|
| `render:started` | `{workspaceId, fileName}` | `RenderCheckpoint` (también el auto-render del watcher) |
| `render:finished` | `{workspaceId, result}` | `RenderCheckpoint`, siempre, aunque falle el motor (un cancelado llega con `status: cancelled`) |
| `checkpoints:changed` | `WorkspaceView` | watcher: algún `.scad` de la raíz cambió (debounce 300 ms); también al cambiar el padre activo, la papelera, un rótulo o un vínculo con la sesión |
| `terminal:data` | `{id, data (base64), seq}` | PTY de una terminal de agente (S5) |
| `terminal:exit` | `{id, code}` | el proceso de la terminal terminó |
| `chat:delta` | `{chatId, text, seq}` | turno de chat en curso (deltas agrupados cada 40 ms) |
| `chat:done` | `{chatId, messageId, cancelled}` | fin del turno (también al cancelar con texto parcial) |
| `mcp:changed` | `{enabled}` | se apagó/encendió el servidor MCP o se resincronizó un workspace (`SetMCPEnabled`, `SyncWorkspaceMCP`) |
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

## Bindings documentados en su segmento

Para que este archivo no crezca sin tope, algunas áreas tienen su tabla de
bindings junto al resto de su documentación:

| Área | Segmento |
|---|---|
| Medición e impresión (`app_parts.go`) | [claude/14-medicion.md](14-medicion.md#bindings-contrato-go--react) |
| Terminal de agentes, chat y servidor MCP (`app_terminal.go`, `app_chat.go`, `app_mcp.go`) | [claude/09-agents-ai.md](09-agents-ai.md#bindings-contrato-go--react) |
