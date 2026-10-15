---
name: openscad-3mf-multicolor
description: Diseño de modelos multicolor para Bambu Studio y AMS desde OpenSCAD (una pieza por color, export 3MF con colores, o un objeto por color ensamblado en un 3MF multi-objeto). Úsala cuando el usuario pida más de un color, texto o logo en otro color, export 3MF, o pregunte cómo asignar filamentos del AMS.
---

# Multicolor y 3MF para Bambu Studio / AMS

Requisito previo: skill `openscad-core` (checkpoints, `color()` obligatorio).

## 1. Principio: una pieza = un color = un filamento

- Cada región de color es un **sólido separado**, de primer nivel, envuelto en
  su propio `color()`. La app agrupa por color: mismo string de color = mismo
  filamento.
- **Las piezas de colores distintos NO se solapan.** Si el texto va incrustado
  en la base, réstalo de la base. Un solape hace que el slicer decida quién gana
  (o falle) y el resultado no es predecible.
- Usa nombres o hex estables y declarados como parámetros:

  ```openscad
  /* [Colores] */
  base_color = "#FFFFFF";   // filamento 1
  text_color = "#D32F2F";   // filamento 2
  ```

- **Límite de colores** = slots del AMS de la impresora (`bambu-lab-printers`):
  AMS lite (A1, A1 mini) = **4**. Con 1 AMS en P1/X1 = 4, más con AMS encadenados.
  Si el diseño pide más colores que slots, avisa y propone agrupar.

## 2. Patrón de archivo multicolor

```openscad
// @checkpoint parent=001-plate.scad desc="Texto en rojo incrustado al ras"
/* [Dimensiones] */
plate_w = 90;   // [30:1:170]
plate_d = 30;   // [15:1:100]
plate_h = 3;    // [1.2:0.2:10]
label   = "ScadLab";
text_size = 10; // [4:0.5:30]
inlay_depth = 0.6;   // [0.2:0.2:2]   múltiplo de la capa

/* [Colores] */
base_color = "#FFFFFF";
text_color = "#D32F2F";

/* [Exportar] */
PART = "all";   // [all, base, text]

/* [Hidden] */
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

if (PART == "all" || PART == "base") color(base_color) base();
if (PART == "all" || PART == "text") color(text_color) text_part();

module text_2d() text(label, size = text_size, font = "Liberation Sans:style=Bold",
                      halign = "center", valign = "center");

module base() {
    difference() {
        translate([-plate_w / 2, -plate_d / 2, 0]) cube([plate_w, plate_d, plate_h]);
        translate([0, 0, plate_h - inlay_depth]) linear_extrude(inlay_depth + eps) text_2d();
    }
}

module text_part() {
    translate([0, 0, plate_h - inlay_depth]) linear_extrude(inlay_depth) text_2d();
}
```

- `if (...) color(...)` mantiene el `color()` en primer nivel y permite exportar
  cada color suelto con `-D 'PART="text"'`.
- Incrustado al ras (inlay) = superficie superior lisa. En relieve = el texto se
  apoya **encima** de la base (`translate([0, 0, plate_h])`), sin restar.

## 3. Variantes de diseño multicolor

| Técnica | Cómo | Cuándo |
|---|---|---|
| Inlay al ras | restar el texto de la base y rellenar con la pieza de color | mejor acabado, cambios de color en pocas capas |
| Relieve | texto encima de la base, 1–2 capas (0.2–0.4) o más | rápido, mínimo desperdicio de purga |
| Cambio de color por capa | geometría de un solo color; el cambio se pone en Bambu Studio sobre el deslizador de capas | carteles: base de un color y texto superior de otro, sin AMS por región |
| Piezas separadas | cada color es un cuerpo con volumen propio | ensamblajes, bicolor funcional |

**Relieve para cambio por capa**: si el texto sobresale exactamente N capas por
encima de una cara plana y no hay otra geometría a esa altura, basta un único
cambio de filamento en la capa donde empieza el texto (botón derecho en el
deslizador de capas → cambiar filamento). Haz que `plate_h` y el relieve sean
múltiplos de la altura de capa prevista.

**Desperdicio de purga**: cada cambio de color por capa purga filamento (torre
de purga). Concentra los colores en pocas capas (relieves finos arriba) en vez
de repartirlos en toda la altura.

## 4. Exportar a 3MF

### Opción A — export 3MF con colores de OpenSCAD (2025 / snapshots)

Los snapshots de desarrollo de OpenSCAD (2024+) y la versión 2025 exportan 3MF
conservando `color()`, especialmente con el backend Manifold. La 2021.01
estable **no** guarda colores en 3MF. Según lo conocido (todo «verificar» con
`openscad --help` y el diálogo de export de 3MF de la versión instalada):

- El diálogo de export 3MF ofrece un **modo de color** (usar colores del
  modelo / un solo color / ninguno), unidad y si se agregan metadatos.
- Por CLI esas opciones se pasan con `-O export-3mf/<opción>=<valor>`
  «verificar nombres exactos».
- Con CGAL los colores pueden perderse tras operaciones booleanas; usa
  `--backend=manifold` «verificar».
- Bambu Studio puede abrir ese 3MF como **un solo objeto con color por
  triángulo/material**; al importarlo suele ofrecer mapear colores a filamentos
  «verificar comportamiento por versión».

Si la app tiene MCP de OpenSCAD con export 3MF, úsalo: ya sabe la versión
instalada.

### Opción B — robusta: un objeto por color + 3MF multi-objeto

Funciona con cualquier versión de OpenSCAD:

```bash
openscad -D 'PART="base"' -o /tmp/plate-base.stl 002-text-inlay.scad
openscad -D 'PART="text"' -o /tmp/plate-text.stl 002-text-inlay.scad
```

Luego:

- **La app** (o el agente) ensambla los STL en un 3MF con un objeto padre de
  varias partes, cada parte con su material/color. Formato en
  [references/3mf-format.md](references/3mf-format.md).
- **A mano en Bambu Studio**: importar los STL juntos → responder **Sí** a
  "¿cargar como un solo objeto con varias partes?" → en la lista de objetos,
  asignar a cada parte su filamento del AMS.

Todas las piezas comparten el mismo origen de coordenadas, así que quedan
alineadas sin moverlas.

## 5. Reglas rápidas

- [ ] Un `color()` de primer nivel por color, con color en parámetro.
- [ ] Sin solapes entre colores (restar donde haga falta).
- [ ] Parámetro `PART` para exportar cada color por separado.
- [ ] Colores totales ≤ slots del AMS de la impresora.
- [ ] Relieves/inlays en múltiplos de la altura de capa; texto ≥ 4–6 mm, trazo ≥ 0.8.
- [ ] Detalles de color de al menos 1–2 anchos de línea (≥ 0.8 mm): más finos
      no se imprimen.
