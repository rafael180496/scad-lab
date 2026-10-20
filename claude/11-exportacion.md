# Exportación (S9, S14): STL, 3MF multicolor por slicer, STL por color

Spec: [.claude/specs/s9-exportacion.md](../.claude/specs/s9-exportacion.md).
Código: `backend/threemf` (leer/repartir/escribir 3MF), `backend/exporter`
(preparar y escribir), `app_export.go` (bindings y diálogo de guardar).

## Flujo

```
STL            → la malla del render (caché) se copia tal cual
3MF / 3mf-prusa / por color → OpenSCAD -o x.3mf (con colores) → threemf.Read
                  → Split por color (basematerials) → ¿partes cerradas? (si no: otro render con
                  --enable=lazy-union, ver «Partes cerradas») → Write (Bambu/Orca) / WritePrusa / STL zip
```

- OpenSCAD (2025+) exporta el 3MF como **un solo objeto** con color por
  triángulo (`basematerials`). `threemf.Split` lo reparte en **una parte por
  color**, con los vértices compactados por parte.
- Un modelo sin `color()` sale como una sola parte.
- **Cada slicer lee un 3MF distinto** (S14, de leer el código de cada uno; **ninguno se abrió**, regla 9):

| Formato | Estructura | Slicers |
|---|---|---|
| `3mf` | `threemf.Write`: un objeto contenedor con `<components>` (una malla por color), **un `<m:colorgroup>` con un solo color por parte** (`pid`/`pindex="0"`) y `Metadata/model_settings.config` con **nombre y extrusor** (1..N) de cada parte | **Bambu Studio**, **OrcaSlicer**, **Creality Print**, **Anycubic Slicer Next** (todos con el lector `bbs_3mf.cpp` y el mismo `model_settings.config`). **ElegooSlicer** y **Snapmaker Orca**, forks de Orca: por herencia, sin leer su código. |
| `3mf-prusa` | `threemf.WritePrusa`: **un** objeto con **una** malla (vértices y triángulos de todas las partes, uno tras otro) y `Metadata/Slic3r_PE_model.config` con un `<volume firstid lastid>` por color (rango contiguo de triángulos), su `name` y su `extruder` (1-based). Sin `<components>`, `<basematerials>` ni `slic3rpe:Version3mf` | **PrusaSlicer**. No lee `model_settings.config`, y un objeto con `<components>` lo parte en objetos separados sin extrusor. |
| `stl-per-color` | zip con un STL por color (mismo origen) | **Cura**, **Luban** (Snapmaker J1s/Artisan) y cualquier otro |
| `stl` | la malla del render | una sola pieza |

- **`application` no es «BambuStudio-…» ni «PrusaSlicer»** a propósito: con el primero
  Bambu Studio trata el archivo como proyecto propio (descarta el color por triángulo y
  exige un `project_settings.config` completo; con uno parcial se cae); con el segundo lo
  manda por el lector de Prusa. Como archivo de otro fabricante carga geometría + partes +
  extrusores, y los **colores del modelo** los lee de `<m:colorgroup>` (sección siguiente).
  (En Creality Print y Anycubic el XML de `model_settings.config` inválido es error fatal.)

## Bambu Studio 02.08.02.61: el diálogo y el multicolor que no llegaba

Leído del código (tag `v02.08.02.61`, la versión instalada; el `02.08.04.61` es igual en lo
que importa). Lo que vio el usuario, en dos partes que no se deben confundir:

1. **«El archivo 3mf tiene una configuración no válida, carga solo los datos de geometría»**
   (`Plater.cpp:8449`) sale con **cualquier** 3MF que no traiga el `project_settings.config`
   de Bambu: `check_project_config` (`Plater.cpp:320`) devuelve `false` si no hay
   `nozzle_diameter`, y el archivo vacío cae ahí. Es un defecto de 02.08.02.60/.61 (en
   02.08.03.65 el vacío pasa y el aviso es una notificación, no un diálogo). **No se
   puede evitar sin un `project_settings.config` completo** (el parcial rompe la CLI) y
   **no hace daño**: `load_config=false` es justo lo que se quiere de un archivo ajeno.
2. **El multicolor perdido** era otra cosa: el 3MF llevaba el color en `<basematerials>` y
   Bambu **solo lee `<m:colorgroup>`** (`COLOR_GROUP_TAG = "m:colorgroup"`, `bbs_3mf.cpp:198`,
   con el prefijo literal: expat sin espacios de nombres). Sin grupos de color no se abre
   la ventana «Standard 3mf Import color» (`Plater.cpp:8655`), y el extrusor 2 de la
   parte queda en un proyecto de 1 filamento: el bloque que agrega filamentos
   (`Plater.cpp:8418`) corre **antes** de `load_config=false`, y `GLCanvas3D::reload_scene`
   (`:3581`) → `ModelVolume::update_extruder_count` borra el `extruder` mayor que los
   filamentos. Resultado: todo en el filamento 1.

Con `<m:colorgroup>` (`bbs_3mf.cpp:3961`, el color de un triángulo sin `pid` propio es el del
objeto: `:4078`) Bambu abre la ventana «Standard 3mf Import color», que por defecto hace
**«Append»**: agrega los colores del modelo como filamentos nuevos (`deal_add_btn`,
`send_new_filament_to_ui`); **«Color match»** los empareja con los filamentos que ya hay.
Al aceptar asigna el extrusor de cada parte (`ObjColorPanel::apply_volume_filament_ids`,
`ObjColorDialog.cpp:647`). Un grupo por parte
con un solo color: Orca guarda un solo color por grupo (`m_group_id_to_color[id] = color`).
Si el usuario cancela el cuadro, o usa una versión sin él (< 02.08.00), el extrusor del
`model_settings.config` solo sobrevive si el proyecto ya tiene tantos filamentos.
Las pruebas lo comprueban con `backend/threemf/bbscheck` (port del lector de Bambu).

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

## Partes cerradas: el hueco relleno y los soportes de más (S17, 17.2)

**Síntoma** (maceta cónica verde con texto y marco en relieve naranja, curvados contra la
pared): en Bambu Studio el 3MF salía con **el hueco de la maceta relleno y muchos soportes**.

**Causa** (medida, no supuesta). OpenSCAD **funde en un solo sólido todo lo del nivel
superior** antes de exportar y marca cada cara con su color. La cara por la que el relieve se
apoya en la pared **desaparece** (queda dentro del sólido), así que al repartir esa malla por
color (`Split`) **cada parte es una cáscara abierta**, con un hueco con la forma del
contacto. Bambu Studio lamina cada parte por separado: el contorno de una cáscara abierta no
se cierra y el relleno/soportes salen mal. Con `threemf.Inspect`:

| 3MF de la maceta de prueba (`macetatest`) | Triáng. | Aristas abiertas | Volumen con signo | Volumen del STL de ese color |
|---|---|---|---|---|
| Por defecto, verde | 1 227 | **109** | 21 951 mm³ | 23 132 mm³ |
| Por defecto, naranja | 439 | **109** | 1 314 mm³ | 164 mm³ |
| `--enable=lazy-union`, verde | 1 112 | **0** | 23 132 mm³ | 23 132 mm³ |
| `--enable=lazy-union`, naranja | 540 | **0** | 163,7 mm³ | 164 mm³ |

La maceta real del usuario (3 910 triáng.): **226** aristas abiertas por parte → **0**. La
plantilla `nameplate-two-color` (texto sobre la placa): **1 769** → **0**. Es decir, **todo
modelo de dos colores que se tocan** (casi todos los útiles) salía así. El motor integrado
(WASM, OFF → `ConvertOFFTo3MF`) da los mismos números que el OpenSCAD nativo.

**Descartado con datos** (por las tres rutas: OpenSCAD nativo, ScadLab y WASM):
volumen con signo **> 0** en todas las partes (el *winding* es correcto al partir y al
escribir; antes del arreglo el único volumen sin sentido era el de las cáscaras abiertas);
ninguna arista dirigida repetida, ningún triángulo degenerado ni repetido, **0 triángulos
compartidos** entre partes y vértices independientes por parte; la parte naranja es solo el
relieve (163,7 mm³, **ningún vértice dentro de la cavidad**, ocupa 0,2 mm de pared y 0,8 mm
hacia afuera); el `<item>` solo traslada (determinante +1, sin espejado), los componentes no
llevan transformación (**no hay doble traslación**: la malla conserva las coordenadas del
`.scad` y la caja del 3MF coincide con la del STL de cada color) y cada parte se declara
`normal_part` (ni *modifier* ni *negative*).

**Arreglo.** `parts.Load3MF` (lo usan la exportación y el visor) reparte el 3MF normal; si
con **más de un color** alguna parte queda abierta, **renderiza otra vez con
`--enable=lazy-union`** (`render.Options.LazyUnion`), que entrega cada `color()` del nivel
superior como su propio sólido cerrado, y usa ese reparto si deja menos bordes abiertos.
El 3MF de lazy-union tiene su propia clave de la caché de exportaciones y el motor integrado
recibe el mismo flag. Un modelo de un solo color, o cuyos colores no se tocan, **no paga el
segundo render** ni cambia. `manifestVersion` de la caché de partes pasó a 2.

Consecuencias que conviene saber:

- Las partes **ya no son un reparto exacto del sólido fundido**: si un color entra en otro
  (el relieve de la maceta entra 0,2 mm en la pared) **se solapan** y suman algo más que el
  STL (23 295,7 contra 23 264,8 mm³). Es lo que diseñó el usuario y lo que un slicer espera de
  partes de un objeto. El peso por parte del panel Impresión pasa de ser basura (cáscaras
  abiertas) a ser el de cada sólido.
- **Solo se separan los `color()` que cuelgan directamente del nivel superior** (también a
  través de `if`, `for` o un `color(c) { … }` con varios hijos). Siguen fundidos —y la
  exportación **avisa** «la parte … no es un sólido cerrado» con el consejo— los que están
  dentro de `union()`, `intersection()`, `render()`, de un `translate()/rotate()` que agrupa
  colores o **de un módulo que los contiene** (`module ensamble() { color(a) …; color(b) … }
  ensamble();`). Medido con OpenSCAD 2026.10.11. La regla de la skill ya era «un `color()` de
  primer nivel por color»; si el modelo agrupa por módulos, el `color()` va **fuera** del
  módulo (`color(a) base(); color(b) texto();`).
- Un solo sólido pintado por caras (`difference() { color(a) …; color(b) … }`) no tiene cara
  interior que separar: el aviso aplica igual.
- Versiones de OpenSCAD sin `--enable` rechazan el flag y se reintenta sin él (se recuerda);
  las que lo conocen pero no la función solo avisan y lo ignoran: el modelo se exporta como
  antes, con el aviso de partes abiertas.

**Cómo se prueba** (sin abrir ningún slicer, regla 9): `threemf.Inspect`/`InspectAll`
(volumen con signo, aristas dirigidas sin opuesta o repetidas, degenerados, duplicados,
caja, triángulos compartidos) y el modelo de prueba `backend/threemf/macetatest`
(`maceta.scad` + los dos 3MF de OpenSCAD; `AssertSolids` compara cada parte con el STL de
ese color). Cubren: los 3MF de OpenSCAD tal cual (`TestNativeOpenSCADMacetaRoutes`,
`TestMacetaUnionSplitLeavesOpenShells`), la ruta de ScadLab (`TestPotPartsAreClosedSolids`
→ Bambu, Prusa y STL por color, más `TestLoad3MF*`, `TestPotPartsAreWatertight` y la caché
de partes), el motor integrado (`TestMacetaOFFTo3MF`, `TestMacetaWASMEndToEnd`) y el flag
del motor (`TestArtifactLazyUnion*`). Sin el arreglo fallan con las cifras de arriba.

## Avisos (no errores)

- Más colores que `multicolor_slots` de la impresora **efectiva** del workspace (los
  colores que el workspace eligió, no el tope de la máquina).
- Colores casi iguales (distancia RGB < ~2 % del rango): probablemente el
  usuario quiso uno solo.
- **Una parte que no es un sólido cerrado** (aristas abiertas): OpenSCAD fundió su color con
  otro y el slicer la laminaría mal. Nombra la parte y pide poner cada `color()` en el nivel
  superior. Una parte con volumen negativo (caras al revés) también avisa.

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

Automáticas (`go test`): `bambu_test.go` y `TestBambuStudioReadsTwoColorModels` releen el
`3mf` con `bbscheck` (port de `bbs_3mf.cpp`): partes, extrusores, `<m:colorgroup>` válidos,
cuadro de colores, y que el formato viejo (`<basematerials>`) no daría colores; con OpenSCAD
real, la plantilla `nameplate-two-color` y una maceta con texto en relieve de otro color.
3MF real de OpenSCAD con `nameplate-two-color.scad` →
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
   `nameplate-two-color.scad` o una maceta con texto en relieve de otro color).
2. Abrirlo en Bambu Studio. En 02.08.02.x sale el aviso «configuración no válida»
   (es el defecto de esa versión, ver arriba): aceptarlo. Después tiene que abrirse la ventana
   **«Standard 3mf Import color»** con los colores del modelo (por defecto «Append»: los agrega
   como filamentos nuevos; «Color match» los empareja con los del AMS): **OK**.
3. Tiene que verse **un objeto con una parte por color**, cada parte con su extrusor (1, 2, …)
   y el color de su filamento.
4. Asignar los filamentos del AMS y laminar: sin errores de geometría.
5. Un modelo sin color: un objeto de una sola parte.

**Prueba manual del usuario de los demás formatos** (tampoco se automatiza; abrir el archivo
en cada slicer es 14.5):

- `3mf-prusa` en PrusaSlicer: **un objeto con una parte por color**, cada una con su extrusor;
  laminar sin errores de geometría.
- `3mf` en Orca, Creality Print, Anycubic Slicer Next, ElegooSlicer y Snapmaker Orca: un objeto con
  sus partes y extrusores, como en Bambu Studio.
- `3mf` en Cura: ¿cada parte llega con su extrusor (`extruder_nr`)? Si no, `stl-per-color`.
