# Límites de editar mallas importadas

Medido con OpenSCAD 2026.10.11 (macOS, Manifold y CGAL) y con el motor integrado de
ScadLab (OpenSCAD 2025.12.30 en WebAssembly). Cifras de referencia, no garantías.

## Mallas no cerradas

Una malla es cerrada cuando cada arista la comparten exactamente dos caras. Si
tiene huecos (una tapa que falta, aristas sueltas):

| Operación | Resultado |
|---|---|
| `import("x.stl");` sola, o con `translate`/`rotate`/`scale`/`mirror` | funciona; se ve bien |
| con `difference()`, `union()` o `intersection()`, backend **Manifold** | `WARNING: PolySet -> Manifold conversion failed: NotManifold`, `ERROR: [manifold] Input mesh is not closed!` y `Current top level object is empty.` |
| lo mismo con **CGAL** | `ERROR: The given mesh is not closed! Unable to convert to CGALNefGeometry.` y el modelo vacío |

Es decir: no se puede perforar ni cortar una malla abierta; solo moverla. Hay que
**repararla antes** (reparación del laminador, una herramienta de malla). El volumen
y el peso estimado de una malla abierta tampoco son fiables. `inspect_reference`
dice si es cerrada, y ScadLab avisa al importarla.

## import() según el formato

- **STL** (binario o ASCII): lo lee toda versión de OpenSCAD. Es el formato con el
  que se edita; por eso `import_mesh` convierte los demás a STL en `refs/`.
- **3MF**: `import()` no existe en todas las versiones (depende de cómo se compiló
  OpenSCAD: compruébalo con tu versión) y, donde existe, pierde los colores y reúne
  las partes. Un 3MF multicolor se aplana a una
  sola malla al convertirlo.
- **OBJ**: solo versiones recientes; sin color. Las caras de más de tres vértices
  se triangulan en abanico (correcto para polígonos convexos, que es lo normal).
- Sin unidades: un STL es adimensional y OpenSCAD lo lee en mm. Un modelo de 0,04
  de ancho probablemente está en metros; uno de 1,6 en pulgadas. Se corrige con
  `scale()` (receta `scale-to-size.scad`).

## convexity

`import("x.stl", convexity = 10)`: ayuda a la vista previa interactiva de OpenSCAD a
dibujar mallas cóncavas (sin ella, caras interiores que se ven rellenas). No cambia
el resultado del render final (F6 / `openscad -o`), pero no cuesta nada ponerla
siempre. 10 alcanza para casi todo; sube si la malla tiene muchas capas de pared
por rayo (un panal).

## Rendimiento

Una malla de ~20 000 triángulos (una esfera de `$fn = 200`), con un agujero:

| Backend | `difference()` con un cilindro |
|---|---|
| Manifold (nativo) | 0,4 s |
| CGAL (nativo) | 4,0 s (10×) |

Sube casi linealmente con los triángulos en Manifold; en CGAL, mucho peor. El motor
integrado (WebAssembly) es unas 2–10× más lento que el nativo. Orientación:

- hasta ~100 000 triángulos: cómodo con Manifold;
- 100 000–500 000: segundos a decenas de segundos por render;
- más de 500 000 (ScadLab avisa al importar): simplifica antes; un STL de 64 MB es
  más de un millón de triángulos y es el máximo que acepta la app.

### minkowski() y offset()

- `minkowski()` sobre un sólido **cóncavo** descompone la malla en partes convexas:
  una malla de 13 000 triángulos con una esfera de 8 lados no terminó en 90 s. Sobre
  un sólido convexo (una bola, un cubo) sí es rápido (1 s con 20 000 triángulos).
- `offset()` es **2D**: sobre una malla haría falta `projection(cut = true)`, que
  es lento y solo da una rebanada. No lo uses para engrosar o redondear un STL.
- Alternativas: `hull()` de formas simples para redondear un borde que tú modelas;
  para engrosar una pared, `scale()` sobre un tramo aislado; o aceptar el borde.

## Colores al editar

Verificado con Manifold (nativo y WebAssembly):

- el `color()` más externo manda sobre los de adentro: `color("red") union() {
  color("green") a(); b(); }` sale todo rojo;
- las caras nuevas que deja un cortador salen con el color **del cortador**: un
  agujero sin color deja las paredes en `#9DCB51`, un color de más;
- por eso: cada cuerpo con su propio `color()` por fuera de su `difference()`, o un
  solo `color()` por fuera de todo si la pieza es de un color. Las paredes del
  agujero y la cara del corte salen del color del cuerpo que cortan.

## Orden de las transformaciones y ángulos

El orden es `translate → rotate → scale → mirror → objeto` (el objeto se espeja
primero). `rotate([x, y, z])` gira X, luego Y, luego Z sobre ejes fijos: matriz
`Rz·Ry·Rx`. Ejemplo para una caja de 10 × 20 × 30: con `rotate([90, 0, 90])` queda de
30 en X, 10 en Y y 20 en Z (X'=z, Y'=x, Z'=y); con el orden contrario, de otro
tamaño y en otro cuadrante.

## Apoyar en la placa

Tras mover o rotar, el modelo puede quedar bajo z = 0 o flotando. `measure` da
`minMm`; para apoyarlo, traslada en Z por `-min.z`. Para una cara concreta, rota
hasta que su normal apunte a −Z y vuelve a apoyar.
