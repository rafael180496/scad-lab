# Medición e impresión (S7): partes del modelo y estimación

Spec: [.claude/specs/s7-medicion-impresion.md](../.claude/specs/s7-medicion-impresion.md).
Código Go (bloque 7.A): `backend/parts` (partes por color y su caché),
`backend/estimate` (peso, filamento, costo), `backend/meshserve` (ruta
`/parts/`), `app_parts.go` (bindings) y `backend/mcp` (`measure`). La interfaz
(visor por partes, panel «Impresión», settings nuevos) es el bloque 7.B.

## Qué es una «parte»

Una parte = **un color** del modelo (`color()` en el `.scad`). Es lo que ya
separa la exportación a Bambu ([claude/11](11-exportacion.md)): OpenSCAD escribe
un 3MF con color por triángulo y `threemf.Split` lo reparte. Ese render 3MF y
el reparto viven ahora en `parts.Load3MF` (con `parts.RenderFailure`), y
`exporter.Prepare` los usa: exportar y mirar las partes **no pueden dar
distinto**.

```
render normal OK (STL, clave K) ──► parts.Store.Ensure
                                      ├─ RenderArtifact .3mf (cola propia «:parts»)
                                      ├─ threemf.Split → una malla por color
                                      └─ <datos>/cache/parts/K.parts/{00.stl,01.stl,…,manifest.json}
```

- **Clave K** = la del render normal (nombre de su malla en
  `<datos>/cache/meshes`, 64 hex). Es por contenido + backend + versión, así que
  la caché de partes nunca queda vieja y se comparte entre la app y el servidor
  MCP (misma carpeta).
- **Una sola parte** si el modelo no tiene `color()` o tiene uno solo: su STL es
  una copia del render normal. `uncolored: true` avisa que el color es el
  amarillo por defecto de OpenSCAD (`#F9D72C`) y no una elección del usuario:
  el visor puede preferir una muestra.
- **Si falla solo el 3MF** (el STL ya había salido bien, p. ej. un OpenSCAD sin
  exportación 3MF) se degrada a una sola parte, sin error. Un OpenSCAD ausente o
  un render cancelado sí son error.
- Tope de 100 partes (`NN` tiene dos dígitos); más colores son un error que
  pide unificar.

### Caché y concurrencia

- Se genera en una carpeta temporal `K.parts.tmp-*` y se **renombra** al final;
  el `manifest.json` se escribe último. Una carpeta sin manifest, con otra
  versión o con una parte faltante es un miss y se regenera.
- **Una sola generación por clave** dentro del proceso (`singleflight` propio).
  Si cancelan al que genera, los que esperaban reintentan en vez de heredar la
  cancelación. Entre procesos (app + MCP) gana el primer `rename`; el otro usa
  lo que quedó si es válido.
- Tope de 512 MB, poda por fecha de acceso (`load` toca el manifest) y
  temporales de más de 2 h. Carpeta propia y no `cache/meshes`: la poda del
  motor solo conoce `<clave>.stl` + `<clave>.json`.

## HTTP: `GET /parts/<clave>/NN.stl`

`meshserve.MiddlewareWithParts(meshDir, partsDir)` (en `main.go`; `Middleware`
sigue sirviendo solo `/mesh/`). Mismas reglas que `/mesh/`
([claude/08](08-openscad.md)): lo que sigue a `/parts/` tiene que cumplir
**exactamente** `^[0-9a-f]{64}/[0-9]{2}\.stl$` (`meshserve.ValidPartPath`).
`..`, escapes codificados, mayúsculas, subcarpetas, `manifest.json`, tres
dígitos, barra final o doble → 404 (nunca caen al `next`, que en `wails dev`
devolvería `index.html`); otros métodos que GET/HEAD → 405;
`Content-Type: model/stl` y `Cache-Control: no-store`. Con `partsDir` vacío
`/parts/` pasa al siguiente handler.

## Bindings (`app_parts.go`)

Los dos exigen un **render correcto** del checkpoint en la sesión
(`a.results`, lo que deja `RenderCheckpoint`); si no, error claro «renderízalo
antes de medirlo». Nombres de parámetro = contrato con 7.B.

| Binding | Devuelve |
|---|---|
| `GetModelParts(workspaceId, fileName)` | `parts.Model{key, parts[{index,name,color,uncolored,url,info}], total, plate{x,y,z}, fits, overflow{x,y,z}}` |
| `EstimatePrint(workspaceId, fileName, opts)` | `estimate.Result{parts[{index,materialId,volumeMm3,effectiveMm3,massG,lengthM,cost}], total{massG,lengthM,cost}, currency, assumptions[]}` |

- `total` es el `mesh.Info` de la malla completa (bbox global).
- **Placa**: `parts.Fit(size, volumen)` compara el bbox global con el volumen de
  la impresora **del workspace**, con 0,05 mm de tolerancia; `overflow` son los
  mm que sobran por eje (0 si cabe). Es el único cálculo de placa: `measure` lo
  usa también (`overflowMm` solo trae los ejes que se pasan).
- `GetModelParts` corre un render 3MF la primera vez por clave; `EstimatePrint`
  es rápido (lee el manifest) y, si no está, lo genera primero.
- La cola del render 3MF es `ws:<id>:parts`: no cancela el render del visor ni
  la exportación (`ws:<id>:export`), ni al revés.

## La fórmula (`backend/estimate`)

Un modelo FDM simple y explicable, **siempre una estimación**:

```
pared    = perímetros × anchoDeLínea              (def. 2 × 0,42 = 0,84 mm)
shell    = min(volumen, área × pared)
interior = volumen − shell
efectivo = shell + interior × relleno%
masa_g   = efectivo_mm3 / 1000 × densidad_g_cm3
largo_m  = efectivo_mm3 / (π × (diámetro/2)²) / 1000   (def. 1,75 mm)
costo    = masa_g / 1000 × costo_por_kg
```

Caso de mano (cubo de 20 mm: V = 8000, A = 2400): shell = 2016; al 15 %
efectivo = 2016 + 5984 × 0,15 = 2913,6 mm³; PLA 1,24 → 3,61 g; 1,21 m. El
`min()` cubre la pieza delgada: si la pared ocupa más que el volumen sale
maciza y no se cuenta material de más. Relleno 0 = solo cáscara; 100 = macizo.

- **No cuenta** soportes, balsa/borde, purga del AMS ni la forma del patrón de
  relleno; las tapas se tratan como pared. Los `assumptions` lo dicen en el
  idioma activo (español con tú / inglés) y avisan si alguna parte no es una
  malla cerrada (su volumen no es fiable). El número exacto lo da el laminador.
- **Materiales**: tabla `materials` (densidad y costo por kg). Un material
  desconocido o sin densidad es **error con su nombre**, no un peso con otra
  densidad. Cada parte puede tener el suyo (`partMaterials`: índice → id).
- `Estimate` es pura (sin disco ni base). Los ceros de perímetros, ancho de
  línea, diámetro y moneda se leen como el default.
- **Settings**: `estimate.OptionsFromSettings` lee `infillPercent`,
  `defaultMaterial`, `wallLoops` (2), `lineWidthMm` (0,42),
  `filamentDiameterMm` (1,75) y `currency` ("USD") **por su nombre JSON** y con
  defaults si no existen (como `FormatScad` con `editorIndent`): no depende de
  cuándo 7.B los agrega a `store.Settings`.
- **`opts` vacíos** (`estimate.Merge`): lo que falta se completa con los
  ajustes. El relleno es el caso delicado, porque 0 % es válido y también es el
  valor de un campo ausente: se toma tal cual si el llamador especificó
  cualquiera de perímetros / ancho de línea / diámetro (el panel Impresión manda
  siempre el conjunto completo); si no especificó ninguno, un 0 vale «sin dato»
  y se usa el relleno de Configuración.

## MCP `measure`

Devuelve lo de siempre (`mesh`, `printer`, `fitsBuildVolume`, `overflowMm`,
`warnings`, `material`) más `parts` (una por color: nombre, color, tamaño,
volumen, área, triángulos, cerrada) y `estimate` (parámetros usados, partes,
total, moneda y `assumptions`). `estimatedWeightG` ahora es el peso **estimado**
con relleno y perímetros reales, ya no el de la pieza sólida al 100 %
(`weightNote` desapareció: los supuestos van en `estimate.assumptions`). Usa la
misma caché de partes que la app; si no puede separar las partes, estima el
modelo entero como una parte y lo avisa en `warnings`. Siempre con el material
por defecto (el MCP no asigna uno por parte).

## Pruebas

- `backend/estimate`: números de mano del spec, relleno 0/100, pieza delgada,
  varias partes con materiales distintos, material desconocido, settings sin los
  campos nuevos, `Merge`, supuestos en los dos idiomas.
- `backend/parts`: OpenSCAD real sobre `nameplate-two-color.scad` (2 partes con
  los colores de la plantilla, volúmenes que suman el total, cada parte servida
  por el middleware byte a byte), modelo sin color y de un solo color, una sola
  generación concurrente; con motor falso: single-flight, reintento tras
  cancelar, degradación, caché rota, `Fit`.
- `backend/meshserve` (`parts_test.go`): rutas maliciosas, métodos, carpetas.
- `app_parts_test.go`: los bindings de punta a punta (placa que no cabe,
  render faltante, materiales por parte, relleno 0).
- Se saltan sin OpenSCAD. Nada de esto abre Bambu Studio.

## Bindings (contrato Go ↔ React)

| Binding | Devuelve | Nota |
|---|---|---|
| `GetModelParts(workspaceId, fileName)` | `parts.Model{key, parts[{index, name, color, uncolored, url, info}], total, plate{x,y,z}, fits, overflow{x,y,z}}` | exige un render OK del checkpoint en la sesión; la primera vez por clave corre OpenSCAD a 3MF con color (cola propia `:parts`); un modelo sin `color()` es **una** parte amarilla con `uncolored: true` |
| `EstimatePrint(workspaceId, fileName, opts)` | `estimate.Result{parts[{index, materialId, volumeMm3, effectiveMm3, massG, lengthM, cost}], total{massG, lengthM, cost}, currency, assumptions[]}` | rápido (usa el manifest de partes en caché). `opts` = `{infillPercent, wallLoops, lineWidthMm, filamentDiameterMm, defaultMaterialId, partMaterials (índice→id), currency}`: el frontend manda **siempre el conjunto completo** (con perímetros/ancho/diámetro presentes, un relleno 0 se respeta como 0 %) |

Las partes no viajan por binding: `GET /parts/<clave>/NN.stl` (middleware de `meshserve`, igual que
`/mesh/`; la clave es la del render normal). El frontend las baja con `fetch` en `lib/mesh.ts`
(`loadParts`, misma caché LRU que las mallas, una entrada por modelo) y las apoya **todas con una
sola traslación** (centro XY y mínimo Z de la unión), para que queden donde las dejó OpenSCAD.
Los bindings se llaman por `lib/printApi.ts`; los tipos (`ModelParts`, `EstimateResult`…) salen de
`wailsjs/go/models` y se re-exportan en `lib/printTypes.ts`. Detalle del lado Go:
[14-medicion.md](14-medicion.md).

Settings nuevos (en `store.Settings`, una clave por campo, sin migración): `wallLoops` (1–10, default
2), `lineWidthMm` (0,2–1,5, default 0,42), `filamentDiameterMm` (1,0–3,5, default 1,75) y
`currency` (3 letras ISO en mayúsculas, default `"USD"`). Fuera de rango **no es error**:
`Settings.normalizePrint` deja el valor de fábrica (misma política que la apariencia), tanto en
`GetSettings` como en `SaveSettings`. `infillPercent` y `defaultMaterial` ya existían.
