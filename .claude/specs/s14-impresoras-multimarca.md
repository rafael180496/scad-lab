# S14 — Impresoras multimarca y configuración por workspace

Fase del roadmap: [claude/01-roadmap.md](../../claude/01-roadmap.md). Notas vigentes al cerrar:
`claude/27-impresoras-workspace.md` (nuevo) y los segmentos 06, 07, 08, 11, 13, 25.

## Pedido (textual)

«Necesito que agregues más impresoras de las marcas Creality, Prusa, Elegoo, Anycubic, Snapmaker y
Ultimaker, para aumentar la cantidad de skills y configuración de impresión. Esto se debe poder
configurar en el proyecto/workspace en el que estoy trabajando, para así seleccionar, cuando creo un
workspace nuevo, qué skills activas e impresoras trabajarás. Así la aplicación tiene más extensión de
mercado.»

## Decisiones

1. **El catálogo sigue siendo una sola fuente para la app y la IA**, pero partido por marca: cada skill
   de marca trae su `references/printers.json` (esquema v2, abajo). `backend/printers` los junta todos
   desde el binario (`skills/*/references/printers.json`). Las skills de usuario **no** cambian el catálogo.
2. **Cada dato se verifica con la ficha oficial** y la URL queda en `sources` de la impresora. Lo que no
   se pudo confirmar va en `verify` (nunca adivinado). La investigación la hizo un agente con acceso web;
   el resultado lo transcribe `openscad-dev`.
3. **Una entrada por modelo comercial.** Lo que cambia de una configuración a otra (XL de 1 a 5 cabezales,
   AMS/CFS/ACE opcional) es la cantidad de **colores** del workspace: `multicolor_slots` es el máximo de
   esa máquina y `default_colors` la configuración con que nace un workspace. El usuario lo ajusta
   (1..máximo) al crear y en «Configuración del workspace».
4. **Configuración por workspace** (migración aditiva): impresora (ya existía), colores, **skills activas**
   y **ajustes de impresión** (material, relleno, perímetros, ancho de línea, diámetro de filamento). Lo
   que el workspace no fija lo hereda de Configuración → Impresión, que pasa a ser «valores por defecto de
   los workspaces nuevos». El **diámetro de filamento** es una propiedad de la máquina: override del
   workspace > el de la impresora (Ultimaker usa 2,85) > el global.
5. **Skills por workspace.** Las de Configuración → Skills encendidas pasan a ser el conjunto por defecto
   de un workspace nuevo, **excepto las skills de marca**, que se eligen por la impresora: por defecto =
   encendidas globalmente que no son de marca + la skill de la marca elegida. Lo elegido se guarda como
   lista explícita en el workspace: cambiar el global después no altera los existentes.
6. **Migración**: los workspaces existentes quedan con su impresora y la lista de skills encendidas
   globalmente en ese momento (backfill de la columna nueva en la misma migración; no se reescribe ningún
   dato anterior). `''` en la columna = «sin definir» → se resuelve por defecto en tiempo de ejecución.
7. **Quitar una skill del workspace** retira su copia gestionada **solo si nadie la editó** (hash de
   `.scadlab-managed`, el criterio de S10.6). Una copia editada a mano queda y se avisa (`kept`).
8. **Slicers**: «Abrir en Bambu Studio» pasa a «Abrir en <slicer>» según la impresora del workspace
   (`slicers`: el primero es el recomendado; se usa el primero instalado). Registro de slicers en Go con
   candidatos por SO (comodines para carpetas con versión) y **ruta configurable por slicer**
   (`settings.slicerPaths`; `bambuStudioPath` sigue valiendo para Bambu Studio). Regla 9: **nunca se lanza
   un slicer** en tests ni en humo; el lanzador se inyecta.
9. **3MF**: el 3MF de S9 (un objeto, una parte por color, extrusor en `Metadata/model_settings.config`) se
   revisa para Orca y PrusaSlicer; si PrusaSlicer necesita `Metadata/Slic3r_PE_model.config` para las
   partes y extrusores, se agrega. Cura no asigna extrusores desde un 3MF de partes: para Ultimaker el
   formato recomendado es **STL por color**. Las skills lo dicen por marca.
10. **IA**: `printer_info` devuelve marca, modelo, sistema multicolor, colores del workspace (y máximo),
    filamento y slicer. Nuevas: `list_printers` (L) y `set_workspace_printer` (E, solo Agente, efecto
    `EffectConfig`). Reglas del workspace, prompt de Hermes y `measure`/estimación usan la impresora
    **efectiva del workspace**.
11. **UI**: selector de impresora con búsqueda, agrupado por marca, ícono genérico (lucide `Printer`, sin
    logos), volumen y colores a la vista. Barra de estado con la impresora del workspace (clic = abre su
    configuración).

## Contrato

### Catálogo — esquema v2 (`assets/skills/<marca>-printers/references/printers.json`)

```json
{
  "schema_version": 2, "units": "mm",
  "brand": {"id": "prusa", "name": "Prusa Research", "skill": "prusa-printers"},
  "notes": "…",
  "printers": [{
    "id": "prusa-mk4s", "name": "Prusa MK4S", "model": "MK4S",
    "build_volume": {"x": 250, "y": 210, "z": 220},
    "nozzle_mm": 0.4, "nozzle_count": 1, "filament_diameter_mm": 1.75,
    "multicolor_kind": "mmu", "multicolor_system": "Prusa MMU3",
    "multicolor_slots": 5, "default_colors": 1,
    "enclosed": false, "heated_chamber": false,
    "bed_surface": "…", "max_nozzle_temp_c": 290, "max_bed_temp_c": 120,
    "slicers": ["prusaslicer", "orcaslicer"],
    "notes": "…", "sources": ["https://…"], "verify": []
  }]
}
```

`multicolor_kind`: `ams | mmu | toolchanger | cfs | ace | idex | dual | none`. Sin multicolor:
`multicolor_slots = 1`. Un número desconocido = `0` y su campo en `verify`. Campos extra (p. ej.
`build_volume_dual_nozzle` de la H2D) los ignora Go y los lee la IA.

Marcas e ids de skill: `bambu-lab-printers` (existente), `prusa-printers`, `creality-printers`,
`elegoo-printers`, `anycubic-printers`, `snapmaker-printers`, `ultimaker-printers`. Orden de marcas en
la app: Bambu Lab, Prusa, Creality, Elegoo, Anycubic, Snapmaker, Ultimaker.

Ids de slicer: `bambu-studio`, `orcaslicer`, `prusaslicer`, `creality-print`, `elegoo-slicer`,
`anycubic-slicer-next`, `snapmaker-orca`, `ultimaker-cura`.

### Go — `backend/printers`

```go
type Brand struct{ ID, Name, Skill string }
type Printer struct {
    ID, Name, Model string
    BrandID   string `json:"brand_id"`   // del bloque brand del archivo
    BrandName string `json:"brand_name"`
    Skill     string `json:"skill"`
    BuildVolume Volume `json:"build_volume"`
    NozzleMM float64 `json:"nozzle_mm"`; NozzleCount int `json:"nozzle_count"`
    FilamentDiameterMM float64 `json:"filament_diameter_mm"`
    MulticolorKind string `json:"multicolor_kind"`; MulticolorSys string `json:"multicolor_system"`
    MulticolorSlots int `json:"multicolor_slots"` // en una impresora EFECTIVA: los colores del workspace
    MaxColors int `json:"max_colors"`             // = multicolor_slots del catálogo (lo llena el cargador)
    DefaultColors int `json:"default_colors"`
    Enclosed, HeatedChamber bool
    BedSurface string `json:"bed_surface"`; MaxNozzleTempC, MaxBedTempC int
    Slicers []string; Notes string; Sources []string; Verify []string
}
func All() ([]Printer, error); func Brands() ([]Brand, error)
func ByID(id string) (Printer, bool)         // fallback: bambu-a1-mini
func BrandSkills() map[string]bool
func (p Printer) Colors(n int) int           // n<=0 → DefaultColors (o MaxColors, o 1); recorta a 1..MaxColors
```

### Go — `backend/slicers` (registro) y `app_export.go`

```go
type Def struct{ ID, Name string; Mac []string /* rutas o comodines */; Win []string /* relativas a %ProgramFiles%, comodines */ }
var Registry []Def
// App
type Slicer struct{ ID, Name, Path string; Found bool; Configured string }
func (a *App) DetectSlicers() []Slicer               // todos los del registro
func (a *App) OpenInSlicer(slicerID, filePath string) error // cualquier id del registro
```
Settings: `SlicerPaths map[string]string json:"slicerPaths"` (sin migración). Para `bambu-studio`,
`slicerPaths["bambu-studio"]` y si no `bambuStudioPath`.

### Base — migración (la siguiente libre de `migrations.go`; hoy sería la 10)

```sql
ALTER TABLE workspaces ADD COLUMN skills TEXT NOT NULL DEFAULT '';          -- JSON []string; '' = sin definir
ALTER TABLE workspaces ADD COLUMN print_settings TEXT NOT NULL DEFAULT '';  -- JSON WorkspacePrint
UPDATE workspaces SET skills = (SELECT json_group_array(name) FROM (SELECT name FROM skills WHERE enabled = 1 ORDER BY name))
 WHERE EXISTS (SELECT 1 FROM skills);  -- backfill SOLO de la columna nueva
```

```go
type WorkspacePrint struct {
    Colors int `json:"colors,omitempty"`              // 0 = default_colors de la impresora
    Material string `json:"material,omitempty"`        // "" = global
    InfillPercent *float64 `json:"infillPercent,omitempty"`
    WallLoops *int `json:"wallLoops,omitempty"`
    LineWidthMm *float64 `json:"lineWidthMm,omitempty"`
    FilamentDiameterMm *float64 `json:"filamentDiameterMm,omitempty"`
}
// store.Workspace suma: Skills []string `json:"skills"` (nil = sin definir), Print WorkspacePrint `json:"print"`
func (s *Store) SetWorkspaceConfig(id int64, printerID string, skills []string, p WorkspacePrint) error
```

### Go — `backend/wsconfig` (una sola definición para la app y el MCP)

```go
func EffectivePrinter(w store.Workspace) printers.Printer   // MulticolorSlots = p.Colors(w.Print.Colors)
func EffectiveSettings(s store.Settings, w store.Workspace, p printers.Printer) store.Settings
func DefaultSkills(all []skills.Skill, globalEnabled map[string]bool, p printers.Printer) []string
func EffectiveSkills(w store.Workspace, all []skills.Skill, globalEnabled map[string]bool, p printers.Printer) []string
type ApplyResult struct{ Removed, Kept []string; SyncError string }
func Apply(st *store.Store, userSkillsDir string, w store.Workspace, printerID string, skillNames []string, p store.WorkspacePrint) (store.Workspace, ApplyResult, error)
// valida (impresora existe, skills existen, rangos de impresión = error claro, colores 1..máx),
// guarda, reescribe CLAUDE.md/AGENTS.md/printer.json y sincroniza las skills de ESE workspace.
```

`workspace.WriteRules(dir, name string, p printers.Printer)` recibe la impresora **efectiva**. Variables
nuevas de `RULES.md.tmpl`: `.Brand`, `.PrinterModel`, `.MulticolorSystem`, `.ColorSlots` (efectivos),
`.MaxColors`, `.NozzleMM`, `.FilamentDiameter`, `.Slicer` (nombre del recomendado), `.BrandSkill`.
`printer.json` suma `brand`, `model`, `colors`, `max_colors`, `multicolor_system`, `nozzle_mm`,
`filament_diameter_mm`, `slicer`.

`prompts.Context` suma `Brand`, `MulticolorSystem`, `SlicerName`, `NozzleMM`, `FilamentDiameterMm`.

`skills.Sync` devuelve `(SyncResult{Removed, Kept []string}, error)`: lo apagado se retira solo si la
copia coincide con su hash.

### Bindings (`app_wsconfig.go`)

```go
type EffectivePrint struct{ Material string; InfillPercent float64; WallLoops int; LineWidthMm, FilamentDiameterMm float64 }
type WorkspaceConfigView struct {
    Workspace store.Workspace; Printer printers.Printer /* efectiva */; Colors int
    Skills, DefaultSkills []string; Print store.WorkspacePrint; Effective EffectivePrint
}
type WorkspaceConfigInput struct{ PrinterID string; Colors int; Skills []string; Print store.WorkspacePrint }
type WorkspaceConfigResult struct{ Config WorkspaceConfigView; Removed, Kept []string; SyncError string }
type NewWorkspaceInput struct{ Name, ParentDir, PrinterID string; Colors int; Skills []string /* nil = por defecto */; Print store.WorkspacePrint }

func (a *App) GetWorkspaceConfig(id int64) (WorkspaceConfigView, error)
func (a *App) SaveWorkspaceConfig(id int64, in WorkspaceConfigInput) (WorkspaceConfigResult, error)
func (a *App) DefaultWorkspaceSkills(printerID string) ([]string, error)
func (a *App) CreateWorkspaceWith(in NewWorkspaceInput) (store.Workspace, error)
func (a *App) ListPrinterBrands() ([]printers.Brand, error)
```
`CreateWorkspaceIn`/`CreateWorkspace`/`ImportWorkspace` siguen; guardan la lista por defecto explícita.
Evento Wails `workspace:config` con `WorkspaceConfigView` tras guardar (binding) o tras un paso ok de
`set_workspace_printer` (efecto `mcp.EffectConfig`, manejado en `refreshTreeAfterStep`).

### MCP

- `printer_info`: suma `brand`, `model`, `multicolorKind`, `colors` (del workspace), `maxColors`,
  `filamentDiameterMm`, `bedSurface`, `maxNozzleTempC`, `maxBedTempC`, `slicer` (`{id,name}` del
  recomendado), `slicers`, `brandSkill`, `sources`.
- `list_printers` (L): `brand?`, `query?` → id, marca, modelo, volumen, colores máx., sistema, slicer.
- `set_workspace_printer` (E): `printer_id`, `colors?`, `swap_brand_skill` (def. true: si cambia la marca,
  sale la skill de la marca anterior y entra la nueva) → `wsconfig.Apply`. Efecto `EffectConfig`.

## Tareas

| # | Tarea | Dueño | Alcance |
|---|---|---|---|
| 14.0 | Investigación del catálogo con la web (fichas oficiales, URLs, apps de slicer en macOS/Windows, qué lee cada slicer de un 3MF) → `scratchpad` | agente de investigación | solo lectura |
| 14.1 | Núcleo Go: esquema v2 en `backend/printers` (+ subir el JSON de Bambu a v2 sin cambiar medidas), `backend/slicers`, migración, `store`, `backend/wsconfig`, `skills.Sync` por workspace, `WriteRules` con la impresora efectiva, bindings, `app_parts`/`app_export`/`app_chat`/`app_meshedit`/`mcp/env.go` con la config efectiva, `EffectConfig` + `refreshTreeAfterStep`, `wails generate module`, tests, `claude/06`, `claude/07`, sección de slicers de `claude/11` | fullstack-dev | `backend/{printers,slicers,store,wsconfig,skills,workspace}`, `app_*.go` (no `app_chat_cli*`), `backend/mcp/{env,effects}.go`, `backend/prompts/prompts.go` (solo el struct `Context`), `frontend/wailsjs/` |
| 14.2 | Catálogo verificado (7 JSON), 6 skills de marca + `bambu-lab-printers` al día, `openscad-3mf-multicolor` genérica, revisión 3MF Orca/PrusaSlicer (`threemf`), `RULES.md.tmpl`, prompts de Hermes, `printer_info`/`list_printers`/`set_workspace_printer`, `clichat/modes.go` (puntual), `assets/skills/README.md`, `claude/08`, `claude/13`, `claude/25`, parte 3MF de `claude/11` | openscad-dev | `assets/skills/`, `assets/workspace/RULES.md.tmpl`, `assets/prompts/`, `backend/prompts/`, `backend/threemf/`, `backend/exporter/`, `backend/mcp/` (salvo env/effects), `backend/clichat/modes.go` (solo las listas) |
| 14.3 | UI: selector de impresoras, «Nuevo workspace» con marca/modelo/colores/skills, «Configuración del workspace» (impresora, skills, impresión), Configuración → Impresión (por defecto + rutas de slicers) y → Skills (texto), «Abrir en <slicer>», barra de estado, panel Impresión con valores del workspace, evento `workspace:config`, i18n, banco + capturas es/en, `index.html` y `es/index.html` (portada multimarca), README, CHANGELOG, `claude/27` + fila en `CLAUDE.md` | fullstack-dev | `frontend/src/`, `scripts/help-shots.sh`, `docs/screenshots/`, `index.html`, `es/index.html`, `README.md`, `CHANGELOG.md`, `claude/27-*`, `CLAUDE.md` |
| 14.4 | Integración, verificación y humo (MK4S) | architect | — |
| 14.5 | Abrir un 3MF/STL exportado en cada slicer instalado y probar «Abrir en <slicer>» | usuario | — |

Orden: 14.0 ∥ 14.1 → 14.2 ∥ 14.3 → 14.4.

Archivos compartidos con otros devs en curso (Antigravity en `backend/clichat`, `claude/19`, `main.go`;
papelera en `backend/checkpoint`): **ediciones puntuales** en `es.ts`/`en.ts`, `uishot.tsx`, `CHANGELOG.md`,
`migrations.go`, `help-shots.sh`, `index.html`, `clichat/modes.go`; nunca revertir ni reescribir cambios
ajenos, nunca `git checkout`/`stash`/`restore`.

## Criterio de terminado

- Go: `gofmt -l .` vacío, `go build ./...`, `go vet ./...`, `go test -race ./...`,
  `GOOS=windows GOARCH=amd64 go build ./...`. Frontend: `pnpm build`, `pnpm test`.
  `./scripts/i18n-check.sh`, `./scripts/uishot-fit.sh`, `./scripts/uishot-persist.sh`, `wails build`.
- Humo (`SCADLAB_DATA_DIR` temporal): workspace Prusa MK4S con sus skills → solo esas instaladas en las
  4 carpetas; `measure` con placa 250×210×220; el system prompt de Hermes nombra la impresora. Ningún
  slicer abierto.
