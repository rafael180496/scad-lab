# S13 — Editar un STL: editor 3D manual y con IA

Fase del roadmap: [claude/01-roadmap.md](../../claude/01-roadmap.md). Notas vigentes al cerrar:
`claude/23-edicion-stl.md`.

## Pedido

«Agrega poder modificar un STL, con el editor 3D o por medio del generador de la IA.»

## Principio: nada es destructivo

Editar un STL **no lo toca**. El STL vive intacto en `refs/` y cada edición es
**código OpenSCAD en un checkpoint nuevo** que hace `import("refs/<archivo>.stl")`
y le aplica las operaciones. Así queda en el árbol con su padre, se deshace
volviendo al padre, se renderiza con el nativo o el WASM y se exporta a STL/3MF
como cualquier checkpoint (reglas 3 de `technical.md`).

## Decisiones

1. **Traer el modelo**: «Importar STL/3MF/OBJ…» (barra lateral) y arrastrar a la
   ventana. Go: `refmodel.CopyToRefs` + `Analyze`. Un 3MF u OBJ se **convierte a
   STL** en `refs/` (`threemf.WriteSTL` / lector OBJ de `refmodel`) porque
   `import()` de 3MF no está en todas las versiones de OpenSCAD; el original se
   conserva al lado. Se crea el checkpoint base (`NNN-<slug>.scad`, cabecera,
   `color()`) con la opción «centrar y apoyar en la placa» (marcada por
   defecto). Antes de importar, si la malla **no es cerrada** se avisa: Manifold
   o CGAL pueden fallar.
2. **Base de una edición = la malla renderizada del checkpoint**, congelada en
   `refs/<checkpoint-sin-.scad>.stl` (reutilizada si es idéntica por hash). Así se
   puede editar **cualquier** checkpoint con render OK —un import o un diseño—,
   el visor previsualiza con la malla que ya tiene (no hace falta otra ruta HTTP)
   y la edición es un hijo que importa esa malla. Límite: los colores de un
   checkpoint multicolor se pierden en la base (una sola malla); el editor
   permite recolorear por pieza. Si el checkpoint es un import base (solo un
   `import("refs/x.stl")` con su transformación), se usa ese `refs/x.stl` directo.
3. **La edición es una escena, no una lista de operaciones**: el frontend lleva la
   pila de operaciones (deshacer/rehacer en la sesión) y manda a Go el **estado
   final** (`EditScene`) más las etiquetas de las operaciones (van como
   comentarios). Go genera el código; la misma escena da el mismo código.
4. **Convención de transformaciones** (contrato TS ↔ Go): OpenSCAD aplica
   `translate(t) rotate([rx,ry,rz]) scale(s) mirror(...)`. En three.js eso es
   `position = t`, `rotation` con **orden de Euler `'ZYX'`** (= `rotate([x,y,z])`
   de OpenSCAD, que gira X, luego Y, luego Z), `scale = s` con el signo del
   espejo. Ángulos en grados en el contrato, radianes solo dentro de three.
   Coordenadas del mundo = las de OpenSCAD (Z arriba, como el visor).
   «Centrar», «apoyar en la placa» y «apoyar sobre una cara» los calcula el
   frontend con la malla y se traducen a `translate`/`rotate` de la escena.
5. **Corte con plano**: un medio espacio (cubo grande orientado con
   `multmatrix`) en `intersection`/`difference`. «Partir en dos» = dos piezas:
   como **dos objetos** separados en X (para imprimirlas planas) o **dos
   colores** en su sitio (3MF multicolor).
6. **Primitivas** (cubo, cilindro, esfera, texto) como **unión** o **agujero**
   (`difference`), cada una con su transformación y su color. Los agujeros se
   restan de todo; las uniones llevan su `color()`.
7. **Con IA**: «Modificar con IA» abre el chat (Hermes o el CLI elegido) con el
   STL ya adjunto (S12: `AddChatAttachment` → `refs/` + análisis + vista
   previa) y un mensaje sugerido. Skill incluida nueva `openscad-stl-editing`.
   Herramienta MCP nueva `import_mesh` (para Hermes, los CLIs y `--mcp`).

## Contrato

```go
// backend/meshedit
type Transform struct {
    Translate [3]float64 `json:"translate"`
    Rotate    [3]float64 `json:"rotate"` // grados, rotate([x,y,z]) de OpenSCAD
    Scale     [3]float64 `json:"scale"`  // 0 = 1
    Mirror    [3]bool    `json:"mirror"`
}
type Primitive struct {
    ID    string    `json:"id"`
    Shape string    `json:"shape"` // cube | cylinder | sphere | text
    Mode  string    `json:"mode"`  // union | hole
    Size  [3]float64 `json:"size"` // cubo x,y,z; cilindro [d, d, h]; esfera [d]; texto [tamaño, -, alto de extrusión]
    Text  string    `json:"text"`
    Center bool     `json:"center"`
    Color string    `json:"color"` // #rrggbb, "" = el del modelo
    Transform Transform `json:"transform"`
}
type Cut struct {
    Point  [3]float64 `json:"point"`
    Normal [3]float64 `json:"normal"`
    Keep   string     `json:"keep"`  // above | below | both
    Split  string     `json:"split"` // objects | colors (solo con both)
    Colors [2]string  `json:"colors"`
}
type Scene struct {
    Ref       string      `json:"ref"`   // refs/x.stl (relativa al workspace)
    Model     Transform   `json:"model"`
    Color     string      `json:"color"`
    Parts     []Primitive `json:"parts"`
    Cut       *Cut        `json:"cut"`
    Ops       []string    `json:"ops"`   // etiquetas legibles, en orden, para los comentarios
}
func Generate(s Scene, header checkpoint.Header, lang string) (string, error) // código legible y comentado; valida la escena
```

Bindings (`app_meshedit.go`):

```go
AnalyzeModelFile(path string) (refmodel.Report, error)                // antes de importar (avisos)
ImportModel(workspaceId int64, path string, opts ImportOptions) (ImportResult, error)
type ImportOptions struct { CenterOnBed bool; Label string }
type ImportResult struct { File, Ref string; Report refmodel.Report; Warnings []string }
PrepareMeshEdit(workspaceId int64, file string) (EditBase, error)     // congela la malla en refs/, devuelve la base
type EditBase struct { Ref, Parent, MeshPath string; Report refmodel.Report; FromImport bool; Model meshedit.Transform }
PreviewMeshEdit(workspaceId int64, scene meshedit.Scene) (string, error)   // el código, sin escribir nada
ApplyMeshEdit(workspaceId int64, parent string, label string, scene meshedit.Scene) (string, error) // checkpoint nuevo (O_EXCL); lo renderiza el watcher
```

MCP: `import_mesh(file, center_on_bed?)` sobre un archivo de `refs/` → checkpoint base.

## Tareas

**T7 — Núcleo Go: importar, congelar, generar código, MCP y skill · `openscad-dev`** (primero).
Alcance: `backend/meshedit/**` (nuevo), `backend/refmodel/**`, `backend/mcp/**`,
`backend/workspace/**` si hace falta, `app_meshedit.go` (+test), `frontend/wailsjs/**`
(solo `wails generate module`), `assets/skills/openscad-stl-editing/**` (siguiendo
`_skill-template`), `claude/13-mcp.md`, `claude/23-edicion-stl.md` (nuevo; la parte
de núcleo y contrato) y su fila en `CLAUDE.md`, `claude/06-go-react-contract.md`.
Criterio: tests de `Generate` para cada operación (transformación con espejo,
centrar/apoyar ya resuelto en la escena, corte arriba/abajo/ambos como objetos y
colores, primitivas unión/agujero de las 4 formas, color por pieza) con **render
real del resultado con OpenSCAD nativo y con el WASM** (cerrado, medidas
esperadas en el bbox); importar STL/3MF/OBJ (conversión, aviso de malla abierta);
congelar la malla (reutiliza por hash); `import_mesh` por MCP; la skill pasa la
validación de frontmatter de skills. Verificación Go completa.

**T8 — Interfaz: importar, modo Editar del visor y «Modificar con IA» · `fullstack-dev`** (después de T7).
Alcance: `frontend/src/**` (visor, barra lateral, chat solo para abrirlo con el
adjunto), i18n, `uishot.tsx`, `help-shots.sh`, `uishot-fit.sh`, `index.html`,
`es/index.html`, `CHANGELOG.md`, `claude/23-edicion-stl.md` (sección de interfaz),
`claude/12-paneles.md` si cambia algo del dock.
- Importar: botón en la barra lateral y arrastrar a la ventana (Wails
  `OnFileDrop`), diálogo con el análisis, avisos y «centrar y apoyar».
- Modo «Editar» del visor: `TransformControls` (mover, rotar, escalar uniforme o
  por eje), entradas numéricas en mm y grados, ajuste a pasos, medidas en vivo,
  espejar, centrar, apoyar en la placa, apoyar sobre una cara (clic), cortar con
  plano (gizmo del plano, lado o partir en dos como objetos/colores), primitivas
  como unión o agujero con su gizmo, color por pieza, pila de operaciones con
  deshacer/rehacer (⌘Z/⌘⇧Z), vista del código generado (`PreviewMeshEdit`),
  «Aplicar» (`ApplyMeshEdit`, selecciona el nuevo) y «Descartar».
  Previsualización inmediata en three.js (los agujeros se muestran como volumen
  translúcido rojo; el resultado booleano real es el render al aplicar).
- «Modificar con IA»: sobre un checkpoint/import, abre el chat con el STL
  adjunto y un texto sugerido.
- Capturas en es y en, miradas: modo Editar con el gizmo, corte, agujero,
  importar con aviso, «Modificar con IA».

## Integración (architect)
Verificación completa + prueba de humo con un STL de prueba: importar, editar
(mover, cortar, agujero), aplicar, render real, exportar 3MF, y «Modificar con
IA» con el servidor simulado.
