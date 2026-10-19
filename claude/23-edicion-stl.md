# Editar un STL (S13): núcleo, contrato y límites

Spec: [.claude/specs/s13-edicion-stl.md](../.claude/specs/s13-edicion-stl.md). Este segmento cubre el **núcleo Go**
(13.1): importar, congelar una malla, generar el código de una edición, `import_mesh` por MCP y la skill. La interfaz
(13.2: importar con arrastrar, modo «Editar» del visor, «Modificar con IA») está en [24-editor-3d.md](24-editor-3d.md).

## Principio: nada es destructivo

Editar un STL **no lo toca**. Vive intacto en `refs/` y cada edición es un **checkpoint nuevo** que hace
`import("refs/x.stl")` y le aplica las operaciones. Queda en el árbol con su padre, se deshace volviendo al padre, se
renderiza con el nativo o el integrado y se exporta a STL/3MF como cualquier checkpoint (regla 3 de `technical.md`).

```
backend/meshedit    Scene → código (Generate), import base (BaseOf), importar (ImportRef), avisos
backend/refmodel    ConvertToSTL (3MF/OBJ → STL), FreezeMesh (malla del render → refs/), WriteBinarySTL
backend/checkpoint  CreateDetached: raíz nueva en un árbol que ya existe (el import)
backend/mcp         import_mesh
app_meshedit.go     AnalyzeModelFile, ImportModel, PrepareMeshEdit, PreviewMeshEdit, ApplyMeshEdit, ChooseModelFile
assets/skills/openscad-stl-editing   recetas (8 + 1) y límites para la IA
```

## Contrato `Scene` (TS ↔ Go)

`Transform{translate, rotate, scale, mirror}`, `Primitive{id, shape, mode, size, text, center, color, transform}`,
`Cut{point, normal, keep, split, colors}` y `Scene{ref, model, color, parts, cut, ops}`: tal como en la spec. Diferencias:

- `Generate(s, meshedit.Header{Parent, Desc}, lang)`: `checkpoint.Header` es una función, no un tipo; el contenido de
  la cabecera viaja en `meshedit.Header`. `lang == "es"` → comentarios en español; cualquier otro, inglés.
- `Scene.Bounds` (`json:"-"`): la caja del STL original. `Generate` la necesita **solo con corte** (dimensiona el plano
  y separa las dos mitades); no viaja del frontend: `PreviewMeshEdit`/`ApplyMeshEdit` la leen del STL
  (`mesh.InfoFromFile`). Sin ella, un corte es un error.
- Valida (errores `i18n.Msg`): `ref` = `refs/<archivo>.stl` (sin `..`, subcarpetas, barras, comillas ni controles);
  colores `#rrggbb` (vacío = el del modelo); tamaños > 0 y finitos (|v| ≤ 100 000); escala ≥ 0 (0 = 1; para voltear,
  `mirror`); normal no nula; `keep` ∈ above/below/both; `split` ∈ objects/colors (vacío = objects); ≤ 200 primitivas;
  el texto se escapa para un literal de OpenSCAD.
- `Keep: "above"` conserva el lado **hacia el que apunta la normal**.

### Convención de rotación (contrato con el frontend)

OpenSCAD aplica `translate(t) rotate([rx,ry,rz]) scale(s) mirror(m) objeto` (primero se espeja, al final se mueve). En
three.js: `position = t`, `rotation` con orden de Euler **`'ZYX'`**, `scale = s` con el signo cambiado en los ejes
espejados. `rotate([x,y,z])` gira X, luego Y, luego Z sobre ejes **fijos** (matriz `Rz·Ry·Rx`) = Euler `'ZYX'` de three
(el `'XYZ'` por defecto de three da otra pose con dos ejes). Ángulos en grados en el contrato. `Transform.Point(p)`
implementa la cuenta; `TestRotationIsEulerZYX` fija la convención con `rotate([90,0,90])` (caja 10×20×30 → 30×10×20),
donde `'XYZ'` daría otra, y la compara con el render nativo y el WebAssembly.

## Código generado

```openscad
// @checkpoint parent=001-soporte.scad desc="Agujero y corte"
// Modelo basado en refs/soporte.stl (el STL original no se modifica: esta edición es solo código).
// Operaciones:
//   1. Mover al centro de la placa
//   2. Agujero pasante Ø3,4
//   3. Cortar a 12 mm de altura

module modelo() {
    // Mover -20 mm en X y -15 mm en Y
    translate([-20, -15, 0])
        import("refs/soporte.stl", convexity = 10);
}
module huecos() {
    // Agujero 1: cilindro Ø 3.4 × 30 mm de alto
    translate([0, 0, 10])
        cylinder(d = 3.4, h = 30, center = true, $fn = 32);
}
module semiespacio(side = 1) {            // un cubo enorme con una cara sobre el plano
    multmatrix([[1, 0, 0, 0], [0, 1, 0, 0], [0, 0, 1, 12], [0, 0, 0, 1]])   // (una fila por línea en el archivo)
        translate([-40, -40, side > 0 ? 0 : -80]) cube([80, 80, 80]);
}
module pieza(lado = 0) {
    color("#3366cc") difference() {       // cada cuerpo (modelo y cada unión) se resta lo suyo
        modelo();
        huecos();
        if (lado != 0) semiespacio(lado);
    }
}
pieza(1);                                  // se queda abajo; "ambos" = pieza(-1) y pieza(1)
```

- **Cortar «ambos lados»**: `objects` → `pieza(-1)` en su sitio y `translate([ancho + 10, 0, 0]) pieza(1)`; `colors` →
  `pieza(-1, "#c1")` y `pieza(1, "#c2")` en su sitio (la unión de las dos mitades es la pieza entera, con dos colores).
  Las primitivas de unión que ya traen color lo conservan.
- Forma de las primitivas: `cube(size)`, `cylinder(d, h)`, `sphere(d)`, `linear_extrude(h) text(…)`; `$fn` por diámetro
  (32/48/64/96, múltiplo de 4: el bbox del cilindro es el diámetro exacto).
- Sin primitivas ni corte: solo `color(c) modelo();` (el **import base**).

### Por qué cada cuerpo se resta lo suyo (y no `difference() { union() {…} agujeros }`)

Verificado con OpenSCAD 2026.10.11 y el motor integrado: (1) el `color()` más **externo** manda sobre los de adentro
(`color("red") union() { color("green") a(); b(); }` sale todo rojo); (2) las caras nuevas que deja un cortador salen con
**su** color, y un cortador sin color las deja en `#9DCB51`, un color de más que el 3MF multicolor contaría como otro
filamento. Con la resta dentro del `color()` de cada cuerpo, las paredes de un agujero y la cara de un corte salen del
color del cuerpo que cortan. La geometría es la misma: `(A ∪ B) − H = (A − H) ∪ (B − H)`.

## Import base y su marca

`Generate` de una escena **sin primitivas ni corte** escribe, tras la cabecera, `// @scadlab-mesh {"ref":…,"model":{…},
"color":…}`. `meshedit.BaseOf(código)` lo reconoce **solo si regenerar el código desde esa marca da el mismo código**
(comparado sin comentarios, líneas vacías ni sangría): un agente que copie la marca en otro archivo, o una versión
anterior del generador, no engaña al editor; cae al camino de congelar la malla. Una edición que solo mueve el modelo
vuelve a ser un import base, así que se sigue editando desde el STL original con su pose acumulada.

## Importar

`meshedit.ImportRef(ws, file, opts)` (lo usan `ImportModel` y `import_mesh`) sobre un archivo que **ya está en `refs/`**
(`refmodel.ResolveRef`: un nombre suelto, formato conocido, nada fuera de `refs/`):

1. Analiza (`refmodel.Analyze`). 2. 3MF/OBJ → STL en `refs/<nombre>.stl` (`-2`, `-3`… si existe; el original queda;
un 3MF se aplana en una malla y pierde los colores). 3. Checkpoint **raíz** (`checkpoint.CreateDetached`: `parent=`
vacío aunque el workspace ya tenga otros; no mueve el padre activo; `O_EXCL`, `NNN-<slug>`), con `color()` y, si
`CenterOnBed`, `translate([-centroX, -centroY, -minZ])` (redondeado a 6 decimales). 4. Avisos en el idioma de la
interfaz: malla **no cerrada** (importarla sola funciona, pero cualquier booleano falla con «mesh is not closed» en
Manifold **y** en CGAL: medido), más de 500 000 triángulos, 3MF con varios colores, y si no cabe en la placa de la
impresora del workspace (`parts.Fit`).

`ImportModel` copia primero con `refmodel.CopyToRefs` (sin pisar); si el modelo no sirve, quita su copia. La
descripción del checkpoint usa el nombre original del archivo (`Importa Mi Pieza.stl`).

## Congelar una malla (`PrepareMeshEdit`)

- **Import base** → `EditBase{FromImport: true, Ref: STL original, Model: su pose, Color}`.
- **Cualquier otro checkpoint** → su malla renderizada (último render OK de la sesión si sigue en la caché; si no,
  `RenderCheckpoint`) se copia a `refs/<checkpoint-sin-.scad>.stl` (`refmodel.FreezeMesh`): se **reutiliza** si ya
  existe con el mismo SHA-256, y con otro contenido va a `-2`, `-3`… Nunca pisa. `Model` = identidad (escala 1).
  Un checkpoint que no renderiza no se edita: el error trae el motivo y no queda nada en `refs/`.
- **`MeshPath` es siempre la malla CRUDA de `Ref`** (sin `Model`), servible por `GET /mesh/<clave>.stl`: en el caso
  congelado es el STL del render (idéntico byte a byte); en el import base, una copia **binaria** (el visor no lee
  ASCII) en `<datos>/cache/meshes/<sha256 del STL>.stl`, creada atómicamente y que la poda de la caché trata como una
  entrada más (se recrea al abrir el editor). El frontend aplica `Model` encima, no lo tiene "horneado".
- `EditBase.Color` se agregó al contrato: el color del import base ("" = el de fábrica `#9ca3af`).

## Bindings (`app_meshedit.go`)

Ver la tabla en [06-go-react-contract.md](06-go-react-contract.md). Tipos con etiquetas JSON en camelCase
(`centerOnBed`, `meshPath`, `fromImport`…); `refmodel.Report` ahora también (`volumeMm3`, `watertight`, `parts`…).
`ChooseModelFile()` (diálogo nativo con filtro STL/3MF/OBJ) se agregó porque el diálogo solo se abre desde Go.
`ApplyMeshEdit` usa `checkpoint.Create` como `SaveAsCheckpoint` (padre activo que avanza, `markOwn`: no se vincula a
una sesión) y no renderiza: lo hace el watcher. `PreviewMeshEdit` no escribe nada. Sin etiqueta, el título es
«Edición de STL».

## MCP: `import_mesh` y `edit_mesh`

Ver [13-mcp.md](13-mcp.md) y [25-herramientas-ia.md](25-herramientas-ia.md). Para Hermes, los CLIs (clasificadas como herramientas de
**escritura**: solo en «Agente») y `--mcp`. Misma puerta de rutas que `inspect_reference`.

- **`import_mesh(file, center_on_bed?)`**: por defecto **no** centra (conserva la posición del archivo).
- **`edit_mesh(file?, operations)`** (S12.c, T11): lo que hace el modo «Editar» del visor, en una llamada —`move rotate scale
  resize mirror orient center to_bed color add hole cut`— y un checkpoint **hijo** (`Generate` de la escena, el mismo código que
  `ApplyMeshEdit`). Elige la base con `meshedit.ResolveBase`, **la misma decisión** (import base o malla congelada en `refs/`)
  que `PrepareMeshEdit` de la app, que solo le pone el render de la sesión y la caché de mallas del visor: hay una sola
  definición de «desde qué se edita este checkpoint». `suggest_orientation` devuelve `operations` listas para `edit_mesh`.

## Skill `openscad-stl-editing`

`assets/skills/openscad-stl-editing/`: `SKILL.md` (principio, flujo «medir antes de cortar», orden de las
transformaciones, tabla de recetas, límites), `references/limits.md` (mallas abiertas, `import()` por formato,
`convexity`, rendimiento con cifras, colores) y 9 recetas en `templates/` (agujero pasante, avellanado/caja M3, recorte
con plano y plano oblicuo, costilla con `hull`, texto grabado/relieve, escalar a una medida, partir para la placa,
espejar). Se embebe y se sincroniza como las demás; `TestSTLEditingSkillIsValidAndSynced` valida el frontmatter, los
enlaces y la sincronización, y `TestSTLEditingRecipesRender` **renderiza cada receta** (nativo y WebAssembly).

## Límites conocidos

- Los colores de un checkpoint multicolor (o de un 3MF) se pierden en la base: es una sola malla.
- Las caras que crea un agujero o un corte llevan el color del cuerpo que cortan; el corte de una primitiva de otro color
  se ve en el color de esa primitiva.
- «Partir en dos objetos» solo separa en X: no pone cada mitad con su cara de corte sobre la placa (la receta
  `split-for-bed.scad` sí, para un plano horizontal).
- Mallas no cerradas: se pueden mover/escalar/espejar, no perforar ni cortar.
- El texto usa la fuente por defecto de OpenSCAD (Liberation Sans en el integrado); su ancho para la caja del corte es
  una cota holgada.

## Pruebas

`backend/meshedit`: cada operación **renderizada** con el OpenSCAD nativo y con el motor integrado (cerrada, caja,
volumen, partes por color con `parts.Load3MF`, linter sin avisos); rotación ZYX; escenas inválidas y hostiles; `BaseOf`
manipulado; importar STL/3MF de dos colores/OBJ y avisos; la skill. `backend/refmodel` (`ConvertToSTL`, `FreezeMesh`,
`WriteBinarySTL`), `backend/checkpoint` (`CreateDetached`), `backend/mcp` (`import_mesh` y rutas hostiles) y
`app_meshedit_test.go` (importar, preparar, previsualizar y aplicar de punta a punta con los dos motores).
