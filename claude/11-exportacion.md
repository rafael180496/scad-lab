# Exportación (S9, S14): STL, 3MF multicolor por slicer, STL por color

Spec: [.claude/specs/s9-exportacion.md](../.claude/specs/s9-exportacion.md).
Código: `backend/threemf` (leer/repartir/escribir 3MF), `backend/exporter`
(preparar y escribir), `app_export.go` (bindings y diálogo de guardar).

## Flujo

```
STL            → la malla del render (caché) se copia tal cual
3MF / 3mf-prusa / por color → OpenSCAD -o x.3mf (con colores) → threemf.Read
                  → Split por color (basematerials) → Write (Bambu/Orca) / WritePrusa / STL zip
```

- OpenSCAD (2025+) exporta el 3MF como **un solo objeto** con color por
  triángulo (`basematerials`). `threemf.Split` lo reparte en **una parte por
  color**, con los vértices compactados por parte.
- Un modelo sin `color()` sale como una sola parte.
- **Cada slicer lee un 3MF distinto** (S14, de leer el código de cada uno; **ninguno se abrió**, regla 9):

| Formato | Estructura | Slicers |
|---|---|---|
| `3mf` | `threemf.Write`: un objeto contenedor con `<components>` (una malla por color, `pid`/`pindex`) y `Metadata/model_settings.config` con **nombre y extrusor** (1..N) de cada parte | **Bambu Studio**, **OrcaSlicer**, **Creality Print**, **Anycubic Slicer Next** (todos con el lector `bbs_3mf.cpp` y el mismo `model_settings.config`). **ElegooSlicer** y **Snapmaker Orca**, forks de Orca: por herencia, sin leer su código. |
| `3mf-prusa` | `threemf.WritePrusa`: **un** objeto con **una** malla (vértices y triángulos de todas las partes, uno tras otro) y `Metadata/Slic3r_PE_model.config` con un `<volume firstid lastid>` por color (rango contiguo de triángulos), su `name` y su `extruder` (1-based). Sin `<components>`, `<basematerials>` ni `slic3rpe:Version3mf` | **PrusaSlicer**. No lee `model_settings.config`, y un objeto con `<components>` lo parte en objetos separados sin extrusor. |
| `stl-per-color` | zip con un STL por color (mismo origen) | **Cura**, **Luban** (Snapmaker J1s/Artisan) y cualquier otro |
| `stl` | la malla del render | una sola pieza |

- **`application` no es «BambuStudio-…»** a propósito: con ese prefijo Bambu
  Studio trata el archivo como proyecto propio, exige un
  `project_settings.config` completo y con uno parcial se cae. Como archivo de
  otro fabricante carga geometría + partes + extrusores; los colores del
  filamento los pone el sistema multicolor del usuario. (En Creality Print y
  Anycubic el XML de `model_settings.config` inválido es error fatal.)
- **Cura** asigna el extrusor por objeto con `extruder_nr` (0-based). `threemf.Write`
  sabe escribirlo en cada parte, dentro de un `<metadatagroup>` **antes del `<mesh>`**
  (`WriteOptions.CuraExtruders`), pero **`exporter` lo deja apagado** (decisión de S14):
  el `3mf` normal es el que el usuario abre en Bambu Studio y ese bloque no se probó en
  ningún slicer (lo de Cura —libSavitar `SceneNode`, `ThreeMFReader.py`— solo se leyó del
  código). Para Cura el formato recomendado es **`stl-per-color`**; encender
  `extruder_nr` (o un `3mf-cura` aparte) queda como opción futura, después de probarlo a
  mano en Cura y en Bambu Studio.
- `3mf-prusa` no usa `<components>` porque PrusaSlicer los instancia como objetos
  aparte; con una sola malla y rangos `firstid/lastid` son **volúmenes de un objeto**.
  El nombre sugerido lleva `-prusa` (`mi-placa-001-x-prusa.3mf`) para no pisar al
  `3mf` en `exports/`. Detalle y XML de ejemplo:
  `assets/skills/openscad-3mf-multicolor/references/3mf-format.md`.

## Avisos (no errores)

- Más colores que `multicolor_slots` de la impresora **efectiva** del workspace (los
  colores que el workspace eligió, no el tope de la máquina).
- Colores casi iguales (distancia RGB < ~2 % del rango): probablemente el
  usuario quiso uno solo.

## Bindings

| Binding | Devuelve |
|---|---|
| `PreviewExportParts(id, fileName)` | `export.Preview{parts, warnings}` sin escribir nada |
| `ExportCheckpoint(id, fileName, format)` | `export.Result{path, parts, warnings}`; `path` vacío si se cancela el diálogo. `format`: `stl` \| `3mf` \| **`3mf-prusa`** \| `stl-per-color` (`exporter.IsFileFormat`). El valor `3mf-prusa` es contrato con la interfaz y la herramienta `export` del MCP |

## Interfaz (S9.B)

- **Cabecera**: botones STL / 3MF / PNG (`components/layout/Header.tsx`). Activos
  con un render correcto del checkpoint elegido; si no, quedan en
  `aria-disabled` (no `disabled`, que no deja pasar el cursor y el `title` no se
  vería) con un `title` que dice por qué (`export.why.*`). Atajos ⌘/Ctrl+E (3MF) y
  ⌘/Ctrl+⇧+E (STL) en captura sobre `window` (`hooks/useExport.ts`): Monaco usa ⌘E
  para «usar la selección para buscar»; sin posibilidad de exportar el atajo
  avisa con un toast y con un diálogo abierto no actúa.
- **STL** va directo al diálogo de guardar. **PNG** llama a la captura del visor a
  través de `lib/viewerBridge.ts` (el visor es `lazy`: registra su captura al
  montarse).
- **Diálogo de 3MF** (`components/export/ExportDialog.tsx`): pide
  `PreviewExportParts`, muestra una fila por parte (muestra de color, nombre,
  triángulos, extrusor `E1..N`), los avisos del backend y el formato (3MF Bambu o
  STL por color en .zip). Botones **Exportar** y, si Bambu Studio está detectado y
  el formato es 3MF, **Exportar y abrir en Bambu Studio**. Si el diálogo nativo
  de guardar se cancela, el de exportación sigue abierto.
- **Tras guardar** (`lib/saved.ts`): toast con la ruta y las acciones **Mostrar
  en el Finder/Explorador** (`RevealPath`) y **Abrir en Bambu Studio**
  (`OpenInSlicer`, solo `.3mf`/`.stl`). El `Toast` admite acciones
  (`toast.withActions`) y en ese caso dura 15 s y se cierra con una X.
- **Banco de capturas**: vistas `export-3mf` (dos colores, sin avisos) y
  `export-3mf-warning` (cinco colores en una A1 mini: aviso de colores de más),
  con `DetectSlicers` encontrado y `OpenInSlicer`/`RevealPath` sin efecto.

## Abrir en <slicer> (S9.B, S14)

`DetectSlicers` / `OpenInSlicer` / `RevealPath` (`app_export.go`) y el registro de slicers de
`backend/slicers`. Se abre la **aplicación** con el archivo, desacoplada, sin shell y sin ninguna
opción de la CLI del slicer (`--slice`, `--export-3mf`… son las que hacen caer a Bambu Studio y abren
el diálogo de «se cerró inesperadamente»):

| Sistema | Comando |
|---|---|
| macOS | `open -a <ruta .app> <archivo>` (si la ruta configurada apunta dentro del `.app`, se lleva al propio `.app`) |
| Windows | `<ruta .exe> <archivo>` con `Start()` y `hideWindow` (`app_launch_windows.go`) |

**Registro** (`slicers.Registry`, ids en el campo `slicers` de cada impresora del catálogo; el primero es el
recomendado y «Abrir en …» usa el primero instalado): `bambu-studio`, `orcaslicer`, `prusaslicer`,
`creality-print`, `elegoo-slicer`, `anycubic-slicer-next`, `snapmaker-orca`, `ultimaker-cura`. Cada entrada
trae los nombres de paquete `.app` de macOS (se buscan en `/Applications` y `~/Applications`) y las rutas de
Windows relativas a `%ProgramFiles%`, `%ProgramFiles(x86)%` y `%LocalAppData%\Programs`, con `/` como
separador. Una carpeta con versión en el nombre lleva comodín (`UltiMaker Cura */UltiMaker-Cura.exe`) y se
elige la **última en orden natural** (`5.10` después de `5.9`). **La tabla es la parte que se corrige**: si un
fabricante cambia el nombre de su instalación se agrega una línea a su entrada y se dejan las viejas; el
comentario del paquete explica cómo.

**Ruta**: `settings.slicerPaths[id]` (Configuración → Impresión; para Bambu Studio vale además
`settings.bambuStudioPath`, la clave de antes de S14, y `slicerPaths["bambu-studio"]` gana si están las dos). Si la
ruta fijada no existe se ignora y se autodetecta, pero `DetectSlicers().configured` la sigue informando para que la
pantalla muestre lo que escribió el usuario. `DetectSlicers` devuelve **todos** los slicers, instalados o no.

El lanzador es la variable de paquete `launchApp`: en producción hace `exec.Command(...).Start()`; los tests la
reemplazan por un registrador y solo comprueban los argumentos y las validaciones (archivo inexistente, extensión no
permitida, ruta relativa, slicer desconocido o no instalado — todos nombran al slicer). El entorno de búsqueda
(`slicerEnv`) también es una variable: los tests lo apuntan a carpetas temporales para no depender de qué tenga
instalado la máquina. `slicers.LaunchCommand` y `revealLaunch` reciben el SO como parámetro, así se prueban los dos
sin ejecutar nada. **Ninguna prueba abre un slicer** (regla 9 de `technical.md`).

Los avisos de exportación («más colores que tu impresora…») se miden contra los **colores del workspace**
(`wsconfig.EffectivePrinter`), no contra el máximo de la máquina.

**Interfaz (S14.3).** «Abrir en &lt;slicer&gt;» (el aviso tras guardar y «Exportar y abrir en &lt;slicer&gt;» del diálogo) usa el **primer slicer instalado**
de `printer.slicers` de la impresora del workspace (`installedSlicer` en `lib/printerCatalog.ts`; el recomendado va primero). Sin ninguno instalado el botón se ve **apagado**
(`ToastAction.disabled` / `disabled` del `Button`) y su `title` dice cuál es el recomendado y que la ruta se indica en Configuración → Impresión → Slicers; sin
ningún slicer que nombrar (la impresora no los declara) no se ofrece nada. `useExport` re-detecta (`DetectSlicers`) cuando cambia `slicerPaths` o `bambuStudioPath`.
El diálogo ofrece tres formatos —«3MF para Bambu y Orca» (`3mf`), «3MF para PrusaSlicer» (`3mf-prusa`) y «Un STL por color» (`stl-per-color`)— y **arranca con el de la
impresora**: `3mf-prusa` si su slicer recomendado es PrusaSlicer, `stl-per-color` si es Cura y `3mf` en los demás. La columna de extrusores se muestra en los dos 3MF.
Detalle de la parte de la interfaz: [27-impresoras-workspace.md](27-impresoras-workspace.md#slicers).

## Pruebas

Automáticas (`go test`): 3MF real de OpenSCAD con `nameplate-two-color.scad` →
2 partes con sus colores y volumen total igual al del STL; sin color → 1 parte;
avisos de colores de más; XML y content types válidos. Del 3MF de PrusaSlicer
(`TestNameplatePrusa3MF`, `TestWritePrusa*`): zip y XML válidos, una sola malla,
volúmenes con rangos contiguos que cubren todos los triángulos, extrusores 1-based,
sin `model_settings.config`, volumen total igual al del STL. Del `extruder_nr` de
Cura (`TestWriteCuraExtruderNr`): con la opción encendida cada parte lo lleva antes del
`<mesh>` y `model_settings.config` no cambia; `TestNameplateNormal3MFHasNoCuraExtruders`
comprueba que el `3mf` exportado **no** lo lleva. `TestExportPrusa3MFOverMCP` y
`TestExportCheckpointPrusa3MF` cubren el MCP y el binding.

**Bambu Studio no se prueba de forma automática** (regla 9 de
`.claude/rules/technical.md`: su CLI se cae y le abre diálogos al usuario).
Prueba manual del usuario (con los botones de S9.B; «Exportar y abrir en Bambu Studio» lo abre en un clic):

1. Exportar a 3MF un checkpoint con dos o más `color()` (p. ej. la plantilla
   `nameplate-two-color.scad`).
2. Abrirlo en Bambu Studio: tiene que verse **un objeto con una parte por
   color**, cada parte con su extrusor (1, 2, …) y su nombre de color.
3. Asignar los filamentos del AMS y laminar: sin errores de geometría.
4. Un modelo sin color: un objeto de una sola parte.

**Prueba manual del usuario de los demás formatos** (tampoco se automatiza; abrir el archivo
en cada slicer es 14.5):

- `3mf-prusa` en PrusaSlicer: **un objeto con una parte por color**, cada una con su extrusor;
  laminar sin errores de geometría.
- `3mf` en Orca, Creality Print, Anycubic Slicer Next, ElegooSlicer y Snapmaker Orca: un objeto con
  sus partes y extrusores, como en Bambu Studio.
- `3mf` en Cura: ¿cada parte llega con su extrusor (`extruder_nr`)? Si no, `stl-per-color`.
