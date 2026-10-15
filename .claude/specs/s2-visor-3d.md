# S2 — Visor 3D

Fase del [roadmap](../../claude/01-roadmap.md). Objetivo: ver la malla del
checkpoint elegido en un visor 3D de calidad (PBR, reflejos), navegarlo con
ratón, táctil y teclado, cambiar de modo y de cámara, colorearlo, capturarlo y
abrir una segunda vista. Volver a un checkpoint ya visto es instantáneo.

## Decisiones de arquitectura (architect)

1. **La malla viaja por HTTP, no por un binding.** Un binding con `[]byte`
   cruza como base64 dentro de JSON: un STL de 15 MB serían 20 MB de texto y
   un `JSON.parse` que congela la UI. Go sirve la caché de mallas con un
   **middleware** del AssetServer en `GET /mesh/<clave>.stl`, y el frontend la
   pide con `fetch` → `ArrayBuffer` → `STLLoader`.
   Middleware y no `Handler`: en `wails dev` las rutas desconocidas las
   responde Vite con `index.html` y el `Handler` nunca se llamaría.
2. **Iluminación sin red.** Nada de `<Environment preset>` de drei (descarga
   HDR de un CDN; ver `.claude/rules/technical.md` regla 6). Mapa de entorno con
   `RoomEnvironment` + `PMREMGenerator` de three, generado en el momento.
3. **Colores**: en S2 el STL es monocromo y se pinta con una de las 6 muestras.
   Los colores reales de cada `color()` llegan en S7 (3MF por pieza).
4. **Dimensiones**: las calcula Go a partir del STL (`backend/mesh`), no el
   frontend: las va a reutilizar S6 (MCP `measure`), S7 (peso/costo) y el aviso
   de placa. El visor las muestra.

## Reparto (en paralelo, sin archivos compartidos)

| Tarea | Dueño | Alcance |
|---|---|---|
| 2.A malla por HTTP + análisis de malla | openscad-dev | `backend/mesh/**`, `backend/meshserve/**`, `main.go` (solo `AssetServer.Middleware`), `app_mesh.go`, `claude/08-openscad.md` |
| 2.B visor 3D completo | fullstack-dev | `frontend/**` (salvo lo generado por `wails generate module`, que regenera al final), `backend/store/settings.go`, `app_viewer.go`, `claude/04-frontend.md`, `claude/06-go-react-contract.md`, `claude/01-roadmap.md`, `CHANGELOG.md`, `index.html`, `scripts/help-shots.sh`, `docs/screenshots/**` |

`app.go`, `app_render.go`, `app_workspaces.go` y `backend/render/**` no se
tocan en esta fase.

## Contrato

### URL de malla (2.A → 2.B)

`GET /mesh/<clave>.stl`, donde `<clave>` es el nombre base de
`render.Result.MeshPath` (64 hex). El middleware:
- valida `^[0-9a-f]{64}\.stl$` (cualquier otra cosa → 404; nunca resuelve rutas
  del usuario ni `..`);
- sirve desde `<datos>/cache/meshes/` con `Content-Type: model/stl` y
  `Cache-Control: no-store` (el frontend tiene su propia caché);
- deja pasar todo lo que no empiece con `/mesh/` (`next.ServeHTTP`).

Constructor: `meshserve.Middleware(cacheDir string) assetserver.Middleware`.
`main.go` lo engancha con el `cacheDir` de `appdata` (`<datos>/cache/meshes`),
el mismo que usa `app.go`.

En TS (2.B): `meshUrl(meshPath) = '/mesh/' + basename(meshPath)` (separador `/`
o `\` — Windows).

### Análisis de malla (2.A)

```go
package mesh
type Info struct {
    Triangles  int        `json:"triangles"`
    Min        [3]float64 `json:"min"`
    Max        [3]float64 `json:"max"`
    Size       [3]float64 `json:"size"`       // Max-Min, mm
    VolumeMM3  float64    `json:"volumeMm3"`  // tetraedros con signo; |v|
    AreaMM2    float64    `json:"areaMm2"`
    Watertight bool       `json:"watertight"` // cada arista compartida por exactamente 2 caras
}
func ParseSTL(r io.Reader) (Info, error)   // binario y ASCII
func InfoFromFile(path string) (Info, error)
```

Binding (`app_mesh.go`): `GetMeshInfo(meshPath string) (mesh.Info, error)` —
valida que `meshPath` esté dentro de `<datos>/cache/meshes` y tenga la forma de
clave; cachea por clave en memoria (la malla de una clave no cambia nunca).

### Visor (2.B)

- `three` + `@react-three/fiber` + `@react-three/drei` (solo helpers que no
  bajen nada de la red: OrbitControls, Bounds/útiles de cámara, Grid, etc.).
- Carga: `fetch(meshUrl)` → `STLLoader.parse` → `BufferGeometry` con
  `computeVertexNormals`; **caché LRU en memoria** por clave (máx. ~300 MB de
  geometría o 24 mallas); cambiar de checkpoint a uno ya visto no vuelve a
  pedir nada. Mientras carga, la malla anterior sigue a la vista (sin parpadeo).
- Material PBR (`MeshPhysicalMaterial`, clearcoat suave) + entorno
  `RoomEnvironment`; sombras de contacto sobre la cama.
- **Cama de impresión** a escala de la impresora del workspace (volumen de
  `ListPrinters` por `workspace.printerId`): placa con cuadrícula de 10 mm y la
  caja del volumen en líneas tenues. La pieza se apoya en Z=0 (OpenSCAD es Z
  arriba: la escena usa Z arriba, `camera.up = (0,0,1)`).
- Controles: órbita/pan/zoom con ratón y táctil (OrbitControls); teclado con
  el visor enfocado: flechas = orbitar, Shift+flechas = pan, `+`/`-` = zoom,
  `1`–`7` = vistas, `F` = encuadrar, `W` = wireframe, `E` = aristas, `O` =
  ortográfica/perspectiva. Los atajos se muestran en el `title` de cada botón.
- Barra de herramientas del visor: 7 vistas (Frente, Atrás, Izquierda,
  Derecha, Arriba, Abajo, Iso) que encuadran la pieza, wireframe, aristas
  (`EdgesGeometry` 30°), orto/persp, encuadrar, 6 muestras de color, captura,
  vista dividida.
- **6 muestras personalizables**: setting `swatches []string` (6 hex, default
  `#ff7a1a #e7e9ee #2b2f38 #3ccf7a #5aa2ff #d32f2f`), clic = aplica, clic
  derecho o doble clic = cambiar color (input `type=color`); se guardan.
- **Captura PNG**: binding `SaveScreenshot(dataURL, suggestedName string)
  (string, error)` en `app_viewer.go` — diálogo nativo de guardar, escribe el
  PNG y devuelve la ruta ("" si se cancela). El canvas con
  `preserveDrawingBuffer` solo al capturar (o render a un target) para no
  pagarlo siempre.
- **Vista dividida**: segundo canvas con cámara y controles independientes,
  misma geometría (compartida, no clonada); botón para cerrarla.
- **Dimensiones** en una esquina: tamaño X×Y×Z, volumen en cm³, triángulos y
  `watertight` (de `GetMeshInfo`).
- Estados: sin render → el panel actual (`RenderPanel`) queda como barra de
  estado arriba del visor; error → diagnósticos superpuestos como hoy; OK → la
  malla.
- Textos en `es.ts`/`en.ts`; `title` en todo botón.
- **Capturas** (regla de `conventions.md`): vistas nuevas en el banco
  (`frontend/src/uishot.tsx`) con una malla STL de ejemplo servida por Vite
  (genérala una vez con OpenSCAD desde una plantilla y guárdala en
  `frontend/public/uishot/` — solo para el banco). Filas nuevas en
  `scripts/help-shots.sh`, regenerar, **mirarlas**, y reemplazar los `shot
  pending` de los temas del visor en `index.html` (quitar «En desarrollo» de lo
  hecho). WebGL en Chrome headless: usar `--use-angle=swiftshader` /
  `--enable-unsafe-swiftshader` en `uishot.sh` si sale en negro.

## Criterio de terminado

`go build/vet/test ./...` (con `-race`), `GOOS=windows go build ./...`,
`cd frontend && pnpm build`, `node frontend/scripts/i18n-check.mjs`, capturas
revisadas, `wails build` de macOS. Bundle del frontend: three se carga con
`lazy()` para no engordar el arranque.
