# Contrato Go ↔ React: render, visor, editor, paneles y edición de STL

Parte de [06-go-react-contract.md](06-go-react-contract.md) (índice del contrato). Fuente: `app_render.go`, `app_viewer.go`,
`app_mesh.go`, `app_editor.go`, `app_layout.go`, `app_dock.go`, `app_appearance.go`, `app_meshedit.go`.

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
