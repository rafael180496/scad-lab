# Contrato Go ↔ React (bindings `App`)

Fuente: `app*.go`. Tipos TS generados en `frontend/wailsjs/go/models.ts` con
`wails generate module`. Al cambiar una firma, actualizar esta tabla.

## General — `app.go`, `app_settings.go`

| Binding | Devuelve | Nota |
|---|---|---|
| `GetBootstrap()` | `Bootstrap{version, platform, dataDir, schemaVersion, settings, initError}` | nunca falla: `initError` dice si la base no abrió |
| `SetLanguage(lang)` | — | `"en"`/`"es"`, persiste |
| `GetSettings()` / `SaveSettings(s)` | `store.Settings` | relleno 0–100; `renderTimeoutSec` 10–3600; `autoRender` default `true` |
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

## Eventos (Go → React)

Suscripción con `onEvent()` de `frontend/src/lib/events.ts`.

| Evento | Payload | Quién lo emite |
|---|---|---|
| `render:started` | `{workspaceId, fileName}` | `RenderCheckpoint` (también el auto-render del watcher) |
| `render:finished` | `{workspaceId, result}` | `RenderCheckpoint`, siempre, aunque falle el motor (un cancelado llega con `status: cancelled`) |
| `checkpoints:changed` | `WorkspaceView` | watcher: algún `.scad` de la raíz cambió (debounce 300 ms) |

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
