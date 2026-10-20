# Impresoras multimarca y configuración por workspace (S14)

Spec: [.claude/specs/s14-impresoras-multimarca.md](../.claude/specs/s14-impresoras-multimarca.md). Antes de S14 ScadLab era «para
Bambu Lab»: un catálogo único de nueve Bambu, una skill y un solo slicer. Ahora hay **siete marcas** (Bambu Lab, Prusa, Creality,
Elegoo, Anycubic, Snapmaker, UltiMaker) y cada workspace guarda lo suyo: impresora, colores, skills y ajustes de impresión.
Contratos y firmas: [29-contrato-workspaces.md](29-contrato-workspaces.md#configuración-por-workspace--app_wsconfiggo-s14).

## Catálogo (una fuente para la app y la IA)

`backend/printers` junta, desde el binario, el `references/printers.json` (esquema v2) de cada skill de marca
(`assets/skills/<marca>-printers/`). La app (`ListPrinters`, `ListPrinterBrands`) y la IA (`printer_info`, `list_printers`) leen **el mismo**
catálogo; las skills de usuario no lo cambian. Reglas del esquema:

- **Una entrada por modelo comercial.** Lo que varía entre configuraciones de una misma máquina (una XL de 1 a 5 cabezales, AMS/CFS/ACE
  opcional) es la cantidad de **colores** del workspace: `multicolor_slots` (y `max_colors`) es el máximo y `default_colors` con lo que nace.
- **Cada dato sale de la ficha oficial** y su URL va en `sources`. Lo que no se pudo confirmar va en `verify` y la interfaz lo marca «por
  confirmar» (⚠ en el selector, aviso en la ficha): nunca se adivina. Un número desconocido es `0`.
- El orden de las marcas es fijo (el de `ListPrinterBrands`) y el de las impresoras dentro de cada una, el de `ListPrinters`: la interfaz **no
  reordena**. `printers.ByID` cae en la A1 mini si un id ya no existe; los bindings que reciben un id explícito lo rechazan.

## Qué rige en un workspace

`backend/wsconfig` es la única definición (la app y el servidor MCP preguntan ahí):

| Dato | Orden de herencia |
|---|---|
| Colores | ajuste del workspace (`print.colors`) > `default_colors` de la impresora; 1..`max_colors`, fuera de rango es **error**, no recorte |
| Diámetro del filamento | ajuste del workspace > el de la impresora (UltiMaker: 2,85) > Configuración → Impresión |
| Material, relleno, perímetros, ancho de línea | ajuste del workspace > Configuración → Impresión |
| Skills | lista del workspace > (encendidas en Configuración que **no** son de marca) + la skill de la marca de su impresora |

`GetWorkspaceConfig` devuelve la impresora **efectiva** (`multicolor_slots` = colores del workspace, `max_colors` = tope de la máquina) y los ajustes
**efectivos** (`effective`); el panel Impresión, la estimación, el aviso de colores de más, `measure`, las reglas (`CLAUDE.md`, `AGENTS.md`,
`printer.json`) y el prompt de Hermes usan esos, nunca los globales. El `printer.json` suma `brand`, `model`, `colors`, `max_colors`,
`multicolor_system`, `nozzle_mm`, `filament_diameter_mm` y `slicer`.

Los **colores viajan solo en `Print.Colors`** (`WorkspaceConfigInput`/`NewWorkspaceInput` no tienen un `Colors` aparte: estaba duplicado y
dos campos para el mismo dato invitan a que discrepen). `WorkspaceConfigView.Colors` es el valor efectivo.

## Skills por workspace

- Configuración → Skills pasa a ser **el conjunto por defecto de los workspaces nuevos**; `SetSkillEnabled` ya no toca los existentes.
- Las **skills de marca** (`printers.BrandSkills()`) no se heredan del global: se eligen por la impresora. Por eso un workspace Prusa no lleva la guía
  de Bambu aunque esté encendida.
- Lo elegido se guarda como **lista explícita** (`workspaces.skills`); `nil` = «sin definir» (se resuelve por defecto al leer), `[]` = ninguna.
- Quitar una skill retira su copia de las **cuatro** carpetas de agente solo si nadie la editó (hash de `.scadlab-managed`, criterio de S10.6); una copia
  editada queda y se avisa (`kept`). Ver [20-skills-propias.md](20-skills-propias.md).

## Migración 10 y el backfill de colores

Detalle de columnas en [07-database.md](07-database.md#migración-de-s14). Dos backfills, los dos **solo sobre columnas nuevas**:

1. `skills`: cada workspace existente queda con las skills que estaban encendidas en ese momento (lo que ya tenía instalado).
2. `print_settings`: antes de S14 los colores de un workspace eran el `multicolor_slots` de su impresora; ahora el valor de fábrica es
   `default_colors` (una Bambu de la serie A: 4; una Prusa MK4S: 1). Sin congelar el número anterior, una **P1S o X1C guardada pasaría de 16 a 4 colores**
   en silencio. Por eso la migración escribe `{"colors": N}` con el `multicolor_slots` que tenía cada impresora en el catálogo anterior
   (A1 mini/A1: 4; P1P/P1S/P2S/X1C/X1E: 16; H2D/H2S: 24), solo donde `print_settings = ''` y para esos nueve ids; un id desconocido no se toca. Los workspaces
   **nuevos** siguen naciendo con `default_colors`.

## Slicers

`backend/slicers` es el registro de los ocho (`bambu-studio`, `orcaslicer`, `prusaslicer`, `creality-print`, `elegoo-slicer`, `anycubic-slicer-next`,
`snapmaker-orca`, `ultimaker-cura`): candidatos por sistema (nombres de `.app` en macOS; rutas relativas a `Program Files` en Windows, con comodín para
carpetas con versión) y **una ruta configurable por slicer** (`settings.slicerPaths`, sin migración). Las rutas salen del código o del instalador de cada
fabricante; las dos que **no se pudieron verificar** (la de Windows de Bambu Studio y todo Anycubic Slicer Next) están marcadas «sin verificar» en la tabla y
las cubre la ruta configurable. Detalle del lanzamiento (`open -a`, sin la CLI del slicer, `launchApp` inyectable) y de cómo editar la tabla:
[11-exportacion.md](11-exportacion.md#abrir-en-slicer-s9b-s14).

- **«Abrir en &lt;slicer&gt;»** usa el **primer slicer instalado** de `printer.slicers` (el recomendado va primero). Sin ninguno, el botón se ve **apagado**
  y su `title` nombra el recomendado y dice dónde indicar la ruta (Configuración → Impresión → Slicers). Está en el diálogo de exportar y en el aviso tras guardar
  (`ToastAction.disabled`).
- **Formato que arranca elegido en el diálogo de exportar**: `3mf-prusa` si el slicer recomendado es PrusaSlicer (no lee el `model_settings.config` del 3MF de
  Bambu/Orca), `stl-per-color` si es Cura (no asigna extrusores desde un 3MF de partes) y `3mf` en los demás. `3mf-prusa` lo escribe `backend/threemf`
  (14.2) y lo acepta `ExportCheckpoint`.
- El campo viejo `bambuStudioPath` sigue valiendo en Go (`Settings.SlicerPath`: `slicerPaths["bambu-studio"]` gana). La interfaz lo **migra**: al abrir
  Configuración lo copia a `slicerPaths['bambu-studio']` y lo vacía en el borrador; al guardar queda solo la clave nueva.
- **Ningún test ni script abre un slicer** (regla 9 de `technical.md`): el lanzador se inyecta y el banco no lanza nada.

## Interfaz

| Pieza | Dónde |
|---|---|
| `PrinterPicker` | `components/printer/PrinterPicker.tsx`: botón + lista con búsqueda agrupada por marca (`groupByBrand`), ícono genérico lucide `Printer` (sin logos), modelo, volumen, colores máximos con su sistema y ⚠ si `verify` no está vacío. Flechas, Inicio/Fin, Enter y Esc |
| `NewWorkspaceDialog` | nombre, impresora + colores (oculto si el máximo es 1), skills (`SkillsChecklist`), ubicación; crea con `CreateWorkspaceWith` |
| `WorkspaceConfigDialog` | tres secciones (Impresora con su ficha, Skills, Impresión con el valor heredado y «Usar el de Configuración»); guarda con `SaveWorkspaceConfig`; el toast nombra las skills `kept` |
| Barra lateral / barra de estado | engranaje junto al workspace al frente; «Impresora · N colores» (clic abre la configuración) |
| Configuración → Impresión | texto «valores por defecto de los workspaces nuevos», `PrinterPicker` de la impresora por defecto, `SlicerSettings` (los 8: detectado o ruta fijada, «Elegir…», «Usar la detección») |
| `hooks/useWorkspaceConfig.ts` | lee `GetWorkspaceConfig` del proyecto al frente y escucha `workspace:config` (guardado del diálogo y `set_workspace_printer` de la IA); `App` sincroniza el workspace de la lista y del proyecto abierto |
| `lib/printerCatalog.ts` / `lib/printerInfo.ts` | cálculos puros (agrupar, acotar colores, elegir slicer, `swapBrandSkill`; probados con `pnpm test`) y los textos que arman volumen y colores |

Decisiones que no son de estilo:

- **Las skills tocadas no se recalculan.** En «Nuevo workspace», mientras nadie toque una casilla, cambiar de impresora recalcula las skills por defecto
  (`DefaultWorkspaceSkills`); una vez tocadas solo se **cambia la skill de marca** (`swapBrandSkill`). En «Configuración del workspace» siempre se cambia solo la de
  marca. El `from` se lee antes de `setSkills(fn)`: la función corre después y la ref ya valdría la marca nueva (bug real al probarlo).
- **El `Popover` se cuelga del `<dialog>`** cuando el ancla está dentro de uno: lo que cuelga del `<body>` queda detrás de un modal (`showModal`). El selector no va dentro de un
  `<label>` (un clic en la lista volvería a pulsar el botón) y enfoca su buscador un cuadro después de abrir: el `Popover` nace oculto hasta medirse y un `autoFocus` de React corre antes.
- **Cambiar de impresora vuelve los colores al valor de fábrica** de la nueva (otra puede tener otro máximo).
- **`SkillsChecklist` ordena** la skill de la impresora primero («Tu impresora»), después las comunes y al final las de otras marcas («Marca»), cada grupo alfabético.
- **Un valor de impresión fuera de rango apaga Guardar** y el campo dice el rango (los mismos que Go); Go los valida de nuevo.
- Las capturas de «Abrir en» y del selector son del **banco** con datos inventados y cifras reales de las fichas (MK4S 250×210×220, CORE One+, K1 Max, Kobra 3, U1, S7).

## Banco, capturas y pruebas

Vistas de `uishot.tsx`: `workspace-new-printer` (Prusa MK4S, 4 colores y sus skills), `printer-picker` (lista abierta), `workspace-config` (+ `-skills`, `-print`
para revisar las otras secciones). Se actualizaron `new-workspace`, `settings-printing`, `settings-skills`, `export-3mf` y `export-3mf-warning`. Pruebas:
`backend/store/migration10_test.go` (backfill de skills y de colores, idempotencia), `backend/slicers` (candidatos por slicer y SO), `app_wsconfig_test.go`,
`frontend/scripts/printer-catalog.test.mjs`, `./scripts/uishot-fit.sh` y `./scripts/uishot-persist.sh`.

## Regla de sincronización

Una marca nueva se agrega en cinco lugares del mismo cambio: su `printers.json` y su skill (`assets/skills/`), el orden de marcas de `backend/printers`, su slicer en
`backend/slicers` (si es nuevo) y el banco (`BRANDS`/`SHOT_PRINTERS` de `uishot.tsx`). Una herramienta nueva de la IA que cambie la configuración declara el efecto
`mcp.EffectConfig` y su etiqueta en `chat.steps.tools` (es/en). Un ajuste de impresión nuevo toca `store.WorkspacePrint`, `wsconfig` (efectivo y validación),
`EffectivePrint` y la sección Impresión del diálogo.
