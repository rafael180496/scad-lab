# Contrato Go ↔ React: workspaces, configuración y proyectos abiertos

Parte de [06-go-react-contract.md](06-go-react-contract.md) (índice del contrato). Fuente: `app_workspaces.go`,
`app_wsconfig.go`, `app_projects.go`. Tipos TS en `frontend/wailsjs/go/models.ts` (`wails generate module`); al cambiar una firma,
actualizar esta tabla. Visión de conjunto de la fase S14: [27-impresoras-workspace.md](27-impresoras-workspace.md).

## Workspaces — `app_workspaces.go`

| Binding | Devuelve | Nota |
|---|---|---|
| `ListWorkspaces()` | `[]store.Workspace` | recientes primero |
| `CreateWorkspace(name, printerId)` | `store.Workspace` | alias de `CreateWorkspaceIn(…, "")` y este de `CreateWorkspaceWith` sin colores ni skills |
| `CreateWorkspaceIn(name, printerId, parentDir)` / `PreviewWorkspacePath(name, parentDir)` | `store.Workspace` / `string` | ubicación elegible, ruta final exacta; ver [16](16-persistencia.md). También `SetLastCheckpoint`, `SaveViewer`, `SetWorkspacesRoot` |
| `ImportWorkspace(dir, printerId)` | `store.Workspace` | reglas solo si faltan; guarda las skills por defecto de la impresora como lista explícita |
| `UpdateWorkspace(w)` | — | nombre, impresora y descripción; si cambia la impresora o el nombre reescribe reglas con la impresora **efectiva**. Colores, skills y ajustes de impresión **no** se guardan acá: `SaveWorkspaceConfig` |
| `RemoveWorkspace(id)` | — | no borra la carpeta |
| `OpenWorkspace(id)` / `RescanWorkspace(id)` | `WorkspaceView{workspace, checkpoints, missing}` | `OpenWorkspace` además vigila la carpeta (detiene el watcher anterior) |
| `ReadCheckpoint(id, file)` | `string` | valida que sea un `.scad` del workspace |
| `SetCheckpointUI(id, file, label, collapsed)` | — | |
| `RevealWorkspace(id)` | — | Finder/Explorador |

## Configuración por workspace — `app_wsconfig.go` (S14)

Cada workspace guarda su **impresora y colores**, sus **skills activas** y sus **ajustes de impresión**; lo que no fija lo hereda
(Configuración → Impresión, la impresora). La lógica y la herencia viven en `backend/wsconfig`, la misma definición que lee el
servidor MCP. Notas: [07-database.md](07-database.md) (migración 10) y [27-impresoras-workspace.md](27-impresoras-workspace.md).

| Binding | Devuelve | Nota |
|---|---|---|
| `GetWorkspaceConfig(id)` | `WorkspaceConfigView` | la configuración resuelta |
| `SaveWorkspaceConfig(id, in)` | `WorkspaceConfigResult{config, removed[], kept[], syncError}` | valida (todo o nada), guarda, reescribe `CLAUDE.md`/`AGENTS.md`/`printer.json` con la impresora efectiva y sincroniza **las skills de ese workspace**; emite `workspace:config`. Una skill que sale de la lista se retira solo si su copia no fue editada (si no, va en `kept`). `syncError` = reglas o skills que no se pudieron escribir, con la configuración ya guardada |
| `DefaultWorkspaceSkills(printerId)` | `[]string` | las encendidas en Configuración → Skills **que no son de marca** + la skill de la marca de esa impresora; `""` = la impresora por defecto. Lo que «Nuevo workspace» muestra marcado |
| `CreateWorkspaceWith(in)` | `store.Workspace` | crea con impresora, colores, skills y ajustes; instala **solo** esas skills en las 4 carpetas de agente. `Skills` nil = las de por defecto; se guardan siempre como lista explícita |
| `ListPrinterBrands()` | `[]printers.Brand{id, name, skill}` | en el orden fijo de la app: Bambu Lab, Prusa, Creality, Elegoo, Anycubic, Snapmaker, Ultimaker |

```go
type EffectivePrint struct{ Material string; InfillPercent float64; WallLoops int; LineWidthMm, FilamentDiameterMm float64 }
type WorkspaceConfigView struct {
    Workspace store.Workspace; Printer printers.Printer // efectiva: multicolor_slots = los colores del workspace
    Colors int; Skills, DefaultSkills []string          // Skills = las que tiene activas (su lista o, sin lista, las de por defecto)
    Print store.WorkspacePrint                          // solo lo que el workspace fijó
    Effective EffectivePrint                            // lo que rige (fijado o heredado)
}
type WorkspaceConfigInput struct{ PrinterID string; Skills []string; Print store.WorkspacePrint }
type NewWorkspaceInput struct{ Name, ParentDir, PrinterID string; Skills []string; Print store.WorkspacePrint }
type WorkspaceConfigResult struct{ Config WorkspaceConfigView; Removed, Kept []string; SyncError string }
```

JSON en camelCase (`printerId`, `parentDir`, `defaultSkills`, `syncError`…). Reglas de los contratos:

- Los **colores** viajan solo en `Print.Colors` (las dos entradas no tienen un `Colors` aparte, para no repetir el dato): 0 = los
  `default_colors` de la impresora. Rango: 1..`max_colors`; fuera de rango es **error** (es/en), no un valor recortado.
  `WorkspaceConfigView.Colors` es el valor **efectivo** (lo que rige), no lo fijado.
- `PrinterID` vacío en `SaveWorkspaceConfig` conserva la actual; en `CreateWorkspaceWith` usa la de Configuración. Un id que no
  está en el catálogo es error (no cae a la A1 mini en silencio).
- `Skills` nil = las de por defecto para esa impresora; `[]` = ninguna. Un nombre que no existe es error.
- Rangos de impresión = los de `Settings.normalizePrint` (relleno 0–100, perímetros 1–10, ancho de línea 0,2–1,5, filamento
  1,0–3,5) pero aquí fuera de rango **no** se normaliza: falla con el rango en el mensaje. El material tiene que existir.
- **Filamento**: ajuste del workspace > el de la impresora (`filament_diameter_mm`; Ultimaker usa 2,85) > el global.
- `store.Workspace` gana `skills` (`null` = sin definir) y `print` (`WorkspacePrint`, todo opcional). Cambiar Configuración →
  Skills (`SetSkillEnabled`) **ya no toca workspaces existentes**: es el conjunto por defecto de los nuevos.

**Evento `workspace:config`** (payload `WorkspaceConfigView`): lo emite `SaveWorkspaceConfig` y `refreshTreeAfterStep` tras un
paso ok de una herramienta con efecto `mcp.EffectConfig` (`set_workspace_printer`, que declara `openscad-dev`).

`printers.Printer` (v2) suma `model`, `brand_id`, `brand_name`, `skill`, `nozzle_count`, `filament_diameter_mm`,
`multicolor_kind`, `max_colors`, `default_colors`, `bed_surface`, `max_nozzle_temp_c`, `max_bed_temp_c`, `slicers`, `sources`.
Los bindings que miden o estiman (`GetModelParts`, `EstimatePrint`, `ExportCheckpoint`, `PrepareMeshEdit`/`ImportModel`) y el
contexto del chat usan la impresora y los ajustes **efectivos** del workspace.

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
