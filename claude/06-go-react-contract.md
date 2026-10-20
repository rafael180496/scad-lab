# Contrato Go ↔ React (bindings `App`)

Fuente: `app*.go`. Tipos TS generados en `frontend/wailsjs/go/models.ts` con
`wails generate module`. Al cambiar una firma, actualizar la tabla de su área.

Este archivo es el **índice del contrato**: los bindings generales y los eventos viven aquí; los de cada área, en su
propio segmento (para no pasar de ~200 líneas por archivo):

| Área | Segmento |
|---|---|
| Workspaces, configuración por workspace (S14) y proyectos abiertos (`app_workspaces.go`, `app_wsconfig.go`, `app_projects.go`) | [29-contrato-workspaces.md](29-contrato-workspaces.md) |
| Checkpoints avanzados: papelera, padre activo, sesiones (`app_checkpoints.go`) | [30-contrato-checkpoints.md](30-contrato-checkpoints.md) |
| Render, visor 3D, editor, paneles acoplables, apariencia y edición de STL (`app_render.go`, `app_viewer.go`, `app_editor.go`, `app_dock.go`, `app_appearance.go`, `app_meshedit.go`) | [31-contrato-render-visor-editor.md](31-contrato-render-visor-editor.md) |
| Exportación y «Abrir en <slicer>», descarga de OpenSCAD y skills (`app_export.go`, `app_openscad.go`, `app_skills.go`) | [32-contrato-exportacion-skills.md](32-contrato-exportacion-skills.md) |
| Servidores de modelos, llavero y chat con Hermes / CLIs (`app_servers.go`, `app_vault.go`, `app_chat*.go`) | [33-contrato-servidores-chat.md](33-contrato-servidores-chat.md) |
| Medición e impresión (`app_parts.go`) | [14-medicion.md](14-medicion.md#bindings-contrato-go--react) |
| Terminal de agentes, chat y servidor MCP (`app_terminal.go`, `app_chat.go`, `app_mcp.go`) | [09-agents-ai.md](09-agents-ai.md#bindings-contrato-go--react) |
| Impresoras, colores, skills y slicers por workspace: decisiones y migración | [27-impresoras-workspace.md](27-impresoras-workspace.md) |

## General — `app.go`, `app_settings.go`

| Binding | Devuelve | Nota |
|---|---|---|
| `GetBootstrap()` | `Bootstrap{version, platform, dataDir, schemaVersion, settings, initError}` | nunca falla: `initError` dice si la base no abrió |
| `SetLanguage(lang)` | — | `"en"`/`"es"`, persiste |
| `GetSettings()` / `SaveSettings(s)` | `store.Settings` | relleno 0–100; `renderTimeoutSec` 10–3600; `autoRender` default `true`; `swatches` (6 `#rrggbb`) solo se escribe si es válido, si no se conserva el guardado |
| `EffectiveWorkspacesRoot()` | `string` | configurada o `~/Documents/ScadLab` |
| `ChooseDirectory(title, start)` / `ChooseFile(title)` | `string` | `""` si se cancela |
| `DetectTools()` | `[]detect.Tool{id, found, path, version, source}` | openscad + cada CLI; en paralelo. `source` (solo OpenSCAD): `custom` (ruta fijada), `managed` (descargado por la app) o `system`; `""` si no se encontró |
| `ListPrinters()` | `[]printers.Printer` | el catálogo v2 (todas las marcas, agrupadas por marca en el orden de la app); ver «Configuración por workspace». `multicolor_slots` es el **máximo** de la máquina y `max_colors` lo repite |
| `ListMaterials()` / `SaveMaterial(m)` | `store.Material` | |
| `ListProviders()` / `SaveProvider(p)` | `store.AIProvider{id, kind, name, command, config, enabled}` | `kind`: cli, ollama, openai, anthropic. Desde S12 el chat **no** lee los de tipo servidor: usa `model_servers` (sección siguiente); quedan para los CLIs |

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
| `mcp:antigravity` | `{added, permissions, settingsPath, error}` | `EnsureAntigravityMCP` (S12.c, T14): ScadLab se registró en la configuración global de Antigravity (`added`) y/o agregó `permissions` reglas `mcp(scadlab/…)` al `settings.json` de Antigravity (`settingsPath`) — toast; o no pudo (`error` con el motivo y qué hacer) |
| `mcp:antigravity-denied` | `{tool, settingsPath, managed}` | Antigravity bloqueó una herramienta de ScadLab por permisos (fin de turno): aviso con «Reparar permisos de Antigravity» (`managed` = false si el usuario las quitó) |
| `chat:error` | `{chatId, message, messageId}` | error del proveedor; si había texto o pasos, se guardaron como respuesta parcial |

Watcher (`backend/watch`): raíz del workspace abierto, no recursivo, solo
`.scad`. Si aparece un `.scad` nuevo y `autoRender` está activo, renderiza el
de número más alto.
