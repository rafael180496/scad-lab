# Qué recuerda la app al cerrar (y dónde se crea un workspace)

Regla: **todo lo visual que el usuario cambió vuelve como lo dejó**, salvo el modo
enfoque (a propósito). Cada cosa se guarda por un binding liviano y aparte de
`SaveSettings` —el diálogo de Configuración guarda una copia vieja de los ajustes y
pisaría lo que la ventana cambió después—, y `Store.SaveSettings` **no escribe** esas
claves (`delete(fields, …)`).

| Qué | Dónde vive | Binding (con espera) |
|---|---|---|
| Proyectos abiertos, orden y activo | settings `openProjects`, `activeProject` | `OpenProject`, `CloseProject`, `ReorderProjects`, `SetActiveProject` |
| **Checkpoint elegido de cada proyecto** | `workspaces.last_checkpoint` (**migración 4**) | `SetLastCheckpoint(id, file)` — 400 ms y al salir |
| Diseño de paneles (incluye pestaña activa de cada grupo, maximizado, flotantes, ocultos y `min`) | settings `dockLayout` | `SaveDockLayout` — 600 ms y al salir |
| Tamaños, barra lateral oculta, barra de estado oculta, medidas plegadas | settings `layout` | `SaveLayout` |
| Escala, letras | settings `uiScale`, `*FontSize` | `SaveAppearance` |
| **Estado del visor 3D** | settings `viewer` (`store.ViewerState`) | `SaveViewer` — 600 ms y al salir |
| Muestras de color | settings `swatches` | `SaveSwatches` |
| Carpeta predeterminada de workspaces | settings `workspacesRoot` | `SetWorkspacesRoot(dir)` (absoluta; `""` = `~/Documents/ScadLab`) |
| Carpeta de skills propias | settings `skillsDir` | `ChangeSkillsFolder(dir, move)` (absoluta; `""` = `<datos>/skills`) — [19](20-skills-propias.md) |

`ViewerState` = `{wireframe, edges, ortho[2], split, colorIndex, modelColors}`; la cámara
**no** se guarda (cada pieza se encuadra sola). `modelColors` arranca en `true`: un JSON
parcial no lo apaga (se decodifica sobre `DefaultSettings`). El visor lee `settings.viewer`
**una vez**, al montar (`Viewer.saved`), y guarda con espera comparando contra lo montado:
abrirlo no escribe nada.

**El checkpoint elegido** lo lleva `App.tsx` (`savedLast`/`pendingLast`): al abrir un
proyecto (`loadProject`) gana `shot.select` > `workspace.lastCheckpoint` (si el archivo
existe) > el más nuevo. Un nombre guardado que ya no existe se ignora, sin limpiar nada.

**Modo enfoque**: `persistNow` no corre mientras dura, y `enterZen` cancela el guardado
pendiente; por eso `enterZen` guarda **en ese momento** el diseño de antes (`zenSaved`):
sin eso, un cambio hecho justo antes de entrar se perdía al cerrar en modo enfoque.

## Dónde se crea un workspace

`CreateWorkspaceIn(name, printerId, parentDir)` (`CreateWorkspace` es su alias con
`parentDir = ""`): `parentDir` vacío = `EffectiveWorkspacesRoot()`; tiene que ser
**absoluta** (`workspace.ErrParentNotAbs`; «~» no se expande), se crea si no existe y un
destino `<padre>/<slug>` que existe y no está vacío es un error que **nombra la carpeta**
(es/en). `PreviewWorkspacePath(name, parentDir)` devuelve esa misma ruta (`workspace.TargetDir`,
el mismo `Slug`) o `""` si el nombre no da slug o el padre no es absoluto: el diálogo
«Nuevo workspace» la muestra como «Se creará en» y distingue los dos casos de `""`
preguntando por un nombre válido. La casilla «Usar esta carpeta como predeterminada»
llama `SetWorkspacesRoot` **después** de crear, y `App` actualiza `boot.settings.workspacesRoot`
para que Configuración no la pise.

## Pruebas

- Go: `backend/store/migration4_test.go` (base de la v3 con filas → v4 sin perder nada,
  idempotente), `app_workspaces_test.go` (padre nuevo, relativo, destino no vacío, acentos
  y espacios, vista previa = lo creado), `backend/workspace/workspace_test.go`.
- **«Cerrar y abrir» del frontend**: `./scripts/uishot-persist.sh`. El banco guarda en
  `localStorage` lo que Go guardaría (`?persist=reset|1` en `uishot.tsx`), un script CDP cambia
  lo visual con interacciones reales, **recarga la página** (arranque en frío) y comprueba que
  vuelve todo; incluye el modo enfoque que no debe persistir.
- Humo real: abrir la app con `SCADLAB_DATA_DIR` temporal, cerrarla, abrirla y mirar con
  `sqlite3 $SCADLAB_DATA_DIR/scadlab.db 'select key,value from settings'`.
