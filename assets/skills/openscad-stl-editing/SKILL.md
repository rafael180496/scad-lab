---
name: openscad-stl-editing
description: Modificar un STL (o 3MF/OBJ) que el usuario ya tiene sin tocarlo, importándolo con import() en un checkpoint nuevo y aplicándole operaciones (agujeros pasantes, avellanados M3, recortes con un plano, refuerzos con hull, texto grabado o en relieve, escalar a una medida exacta, partir para la placa, espejar). Úsala cuando el usuario adjunte o importe un modelo para cambiarlo, perforarlo, cortarlo, agrandarlo o personalizarlo, y no cuando solo quiera copiar su forma con primitivas.
---

# Editar un STL existente

Requisito previo: `openscad-core` (checkpoints inmutables, `color()` obligatorio,
`eps` en los cortes). Si el usuario solo quiere **copiar la forma** de una
referencia, ese es otro caso: reconstrúyela con primitivas a partir de
`inspect_reference`. Aquí se **modifica la pieza real**.

## 1. Principio: nada es destructivo

- El STL vive **intacto** en `refs/`. Nunca lo escribas, lo renombres ni lo
  borres, y tampoco sobrescribas un `.scad`.
- Editar = un **checkpoint nuevo** (hijo del anterior) que hace
  `import("refs/pieza.stl")` y le aplica las operaciones con CSG. Deshacer es
  volver al checkpoint padre.
- Un cambio por checkpoint cuando se pueda (un agujero, un corte): si algo sale
  mal se ve cuál fue.

## 2. Flujo

1. **Punto de partida.** Si el usuario importó el modelo desde la app ya hay un
   checkpoint base (`list_checkpoints`). Si el archivo solo está en `refs/`,
   `import_mesh(file, center_on_bed?)` crea ese checkpoint base (convierte un 3MF
   u OBJ a STL y avisa de mallas abiertas, enormes o que no caben en la placa).
2. **Medir antes de cortar.** `inspect_reference` (medidas del STL original,
   ¿malla cerrada?, imagen) y `measure` del checkpoint base (`minMm`/`maxMm`
   **con la pose que ya tiene**). Las coordenadas de agujeros y cortes salen de
   ahí, no a ojo; mira `preview` desde dos o tres vistas (`top`, `front`, `right`)
   para ubicar la cara donde va cada cosa.
3. **Crear el hijo** con `create_checkpoint` (parent = el base), partiendo de una
   receta de `templates/`. Pon `model_min`/`model_max` con lo medido.
4. **Validar**: `render`, `preview`, `measure` (¿cabe en la placa?). Una malla
   puede renderizar sin error y tener el agujero del lado equivocado.

## 3. Esqueleto

```openscad
// @checkpoint parent=001-soporte.scad desc="Agujero pasante M3 en la base"
model_file = "refs/soporte.stl";
eps = 0.01;

module model() import(model_file, convexity = 10);

color("#9ca3af")
    difference() {
        model();
        translate([10, 10, -1]) cylinder(d = 3.4, h = 22, $fn = 32);   // sobra 1 mm por cada lado
    }
```

- La ruta de `import()` es relativa al `.scad` (los checkpoints están en la raíz
  del workspace) y va con `/`, también en Windows.
- Cada checkpoint es autocontenido: repite el `import()`; no hagas
  `include` de otro checkpoint.
- El STL no trae unidades: OpenSCAD lo lee en **mm**.
- `color()` por fuera de `difference()`/`intersection()`: el `color()` de más
  afuera manda sobre los de adentro, y la cara nueva que deja un cortador sale
  con el color del **cortador** (sin color: el amarillo-verde `#9DCB51` de
  OpenSCAD, que el 3MF multicolor cuenta como un filamento más). Por eso las
  recetas envuelven el `difference()` entero en `color(...)`.
- **Un modelo multicolor se pierde al importar**: un STL tiene una sola malla.
  Si el 3MF original tenía colores, recolorea por pieza con tus propios cuerpos
  (ver `openscad-3mf-multicolor`).

## 4. Pose del modelo: orden de las transformaciones

Cuando muevas, rotes, escales o espejes el modelo, escríbelo siempre en este
orden (es el mismo que usa el editor de ScadLab, y así el resultado es el que
se espera):

```openscad
translate([tx, ty, tz])       // 4. mover
  rotate([rx, ry, rz])        // 3. rotar: primero X, luego Y, luego Z (ejes fijos del mundo)
    scale([sx, sy, sz])       // 2. escalar
      mirror([1, 0, 0])       // 1. espejar (solo los ejes que se invierten)
        import(model_file);
```

`rotate([x, y, z])` gira X, **luego** Y, **luego** Z sobre los ejes fijos
(equivale a la matriz Rz·Ry·Rx y, en three.js, a un Euler de orden `'ZYX'`).
Con dos ejes distintos de cero el orden importa: `rotate([90, 0, 90])` y
`rotate([90, 0, 0]) rotate([0, 0, 90])` no son lo mismo. `scale()` y `rotate()`
actúan respecto del **origen**, no del centro del modelo: si el modelo no está
en el origen, se aleja al escalar.

## 5. Recetas (copia el archivo, ajusta los parámetros de arriba)

| Receta | Archivo | Notas |
|---|---|---|
| Agujero pasante | `templates/through-hole.scad` | Ø del tornillo + 0,2 por lado (M3 → Ø 3,4); 1 mm de sobra arriba y abajo |
| Avellanado / caja M3 | `templates/countersink-m3.scad` | cono de 90° (avellanado) o cilindro (caja de la cabeza); entra por la cara de arriba |
| Recorte con un plano | `templates/plane-cut.scad`, `plane-cut-oblique.scad` | `intersection()` con un cubo enorme; perpendicular a X, Y o Z, o con punto y normal |
| Refuerzo con `hull()` | `templates/hull-rib.scad` | costilla triangular que se hunde 1 mm en el modelo para fundirse |
| Texto grabado o en relieve | `templates/text-on-face.scad` | grabado = se resta; relieve = pieza aparte, otro color; letras ≥ 4–6 mm |
| Escalar a una medida | `templates/scale-to-size.scad` | factor = medida final / medida actual (de `measure`); vuelve a apoyar la esquina en 0 |
| Partir para la placa | `templates/split-for-bed.scad` | dos mitades con la cara de corte sobre la placa, lado a lado |
| Espejar (par izq./der.) | `templates/mirror-pair.scad` | `mirror()` + traslación; el espejo no es la misma pieza girada |

Todas pasan el linter de ScadLab y renderizan con Manifold. Se leen con
`read_skill` (`file` = `templates/<receta>.scad`).

### Corte con un plano oblicuo

`plane-cut.scad` cubre los planos alineados con un eje; para uno cualquiera (punto
y normal) usa `templates/plane-cut-oblique.scad`: calcula los ángulos que llevan Z
a la normal (`rx = -asin(n.y)`, `ry = atan2(n.x, n.z)`) y rota con ellos un cubo
enorme. El editor de ScadLab escribe el suyo con `multmatrix`; si el checkpoint
viene de él (tiene `module semiespacio`), copia ese módulo.

## 6. Checkpoints del editor de ScadLab

Un checkpoint que creó el editor visual tiene `module modelo()` (el `import()` con
su pose) y, si hay agujeros, cortes o piezas, `module huecos()`, `module
semiespacio()` y `module pieza()`. Puedes continuar desde él copiando todo el código.
Si añades algo, **borra la línea `// @scadlab-mesh`** (marca un import base sin
nada más; con tu cambio el editor ya no puede tratarlo como tal).

## 7. Límites y rendimiento

Detalle y cifras: [references/limits.md](references/limits.md). Lo esencial:

- **Malla no cerrada**: `import()` la carga, pero **cualquier** `difference()`,
  `union()` o `intersection()` falla («mesh is not closed», el modelo queda
  vacío), con Manifold y con CGAL. Hay que repararla antes (laminador o
  herramienta de malla); `inspect_reference` y `measure` dicen si es cerrada.
- **`import()` de 3MF** depende de la versión de OpenSCAD y pierde los colores:
  usa un STL (`import_mesh` convierte). Un OBJ solo entra en versiones recientes.
- **`convexity = 10`** en cada `import()`: solo afecta a la vista previa de mallas
  cóncavas (si no, huecos que se ven rellenos); el render final no la usa.
- **Evita `minkowski()` y `offset()` sobre una malla importada**: sobre un sólido
  cóncavo y detallado `minkowski()` puede tardar minutos u horas, y `offset()` es
  2D (haría falta `projection()`). Para redondear, mejor `hull()` de formas
  simples o no redondear.
- **Muchos triángulos** (cientos de miles) hacen lentos los booleanos; con CGAL
  son inviables. Recorta un trozo pequeño para probar y simplifica la malla antes.
- Las caras coplanares (un cortador que termina justo en la cara del modelo)
  dejan una piel o fallan: pasa **1 mm** de más por cada lado.

## 8. Errores comunes

| Síntoma | Causa | Arreglo |
|---|---|---|
| `Input mesh is not closed!` / «Current top level object is empty» | malla abierta + una operación booleana | reparar la malla; sin booleanos solo se puede mover/escalar |
| El agujero no atraviesa | el cilindro termina en la cara del modelo | empezar 1 mm antes y terminar 1 mm después |
| El agujero está corrido | coordenadas del STL original, pero el checkpoint base lo movió | usar `measure` del checkpoint base (ya con su pose) |
| Un color extra `#9DCB51` en el 3MF | cortador sin `color()` heredado | envolver el `difference()` en `color()` |
| Pieza lejos de la placa tras escalar | `scale()` actúa desde el origen | trasladar `-min * k` (ver `scale-to-size.scad`) |
| El espejo se ve igual | el plano del espejo está lejos del modelo | `mirror()` es respecto del origen: trasladar después |
