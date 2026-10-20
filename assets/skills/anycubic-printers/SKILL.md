---
name: anycubic-printers
description: Perfiles de impresoras Anycubic (Kobra 2 Pro/Plus/Max, Kobra 3, Kobra S1, Kobra S1 Max, Kobra X) y reglas de diseño FDM para que un .scad se imprima bien en ellas, con multicolor ACE Pro / ACE 2 Pro y exportación para Anycubic Slicer Next. Úsala cuando printer.json diga una impresora anycubic-*, al dimensionar el modelo (ojo con el orden de las medidas de la Kobra 2), al definir holguras o voladizos, o al preparar un archivo multicolor.
---

# Impresoras Anycubic y diseño para FDM

Requisito previo: `openscad-core` (checkpoints, `color()` obligatorio). Las reglas
FDM generales y sus snippets están en `openscad-core/references/fdm-design-rules.md`;
aquí va lo propio de Anycubic.

## 1. Elegir la impresora

1. Si el workspace tiene `printer.json`, su `printer` (ej. `anycubic-kobra-3`) es la
   impresora. Cópiala a una constante en `[Hidden]`: `PRINTER = "anycubic-kobra-3";`.
2. Sin `printer.json`, asume **`anycubic-kobra-2-pro`** (220×220×250, el menor
   volumen de la marca).
3. Datos legibles por máquina: [references/printers.json](references/printers.json)
   (la app también lo lee). Los campos de `verify` no están confirmados; las URLs
   de la tienda oficial están en `sources`.

| id | Impresora | Volumen X×Y×Z (mm) | Colores (inicial / máx.) | Sistema | Cámara | A confirmar |
|---|---|---|---|---|---|---|
| `anycubic-kobra-3` | Kobra 3 | 250×250×260 | 4 / 8 | ACE Pro (4; hasta 2 unidades = 8) | abierta | `slicers`, `filament_diameter_mm` |
| `anycubic-kobra-s1` | Kobra S1 | 250×250×250 | 4 / 8 | ACE Pro (4; hasta 2 unidades = 8) | cerrada | `slicers`, `filament_diameter_mm`, `heated_chamber` |
| `anycubic-kobra-s1-max` | Kobra S1 Max | 350×350×350 | 4 / 16 | ACE 2 Pro (4; hasta 4 unidades = 16) | cerrada, calefaccionada | `slicers`, `filament_diameter_mm` |
| `anycubic-kobra-x` | Kobra X | 260×260×260 | 4 / 19 | 4 nativos (ACE 2 Pro), hasta 19 | abierta | `slicers`, `enclosed`, `filament_diameter_mm`, `build_volume` |
| `anycubic-kobra-2-max` | Kobra 2 Max | 420×420×500 | 1 | Ninguno | abierta | `slicers`, `filament_diameter_mm` |
| `anycubic-kobra-2-plus` | Kobra 2 Plus | 320×320×400 | 1 | Ninguno | abierta | `slicers`, `filament_diameter_mm` |
| `anycubic-kobra-2-pro` | Kobra 2 Pro | 220×220×250 | 1 | Ninguno | abierta | `slicers`, `filament_diameter_mm` |

- **Kobra 2: ojo con el orden de las medidas.** La ficha de Anycubic da el volumen
  como *alto × ancho × fondo*: la Kobra 2 Max dice 500×420×420 y su volumen real
  es **X 420, Y 420, Z 500**. Aquí siempre va como X×Y×Z; si un modelo "alto" no
  cabe, comprueba que no se haya leído el orden de la ficha.
- **Colores del workspace**: el máximo de colores que puedes usar es `colors` de
  `printer.json`, no el tope de la máquina (`max_colors`). Las Kobra 3, S1, S1 Max
  y X nacen con 4 colores (el Combo con ACE); el usuario los cambia en
  «Configuración del workspace» o tú con `set_workspace_printer`. La Kobra 2 no
  tiene multicolor.
- **Kobra 3 / Kobra 2**: marco abierto, cama móvil. La Kobra 3 usa **ACE Pro**
  (4 colores; 2 ACE Pro = 8).
- **Kobra S1 / S1 Max**: CoreXY cerradas. La S1 llega a 320 °C y 120 °C de cama
  sin cámara calefaccionada; la S1 Max (350×350×350) tiene cámara calefaccionada
  hasta 65 °C y **ACE 2 Pro** (4 unidades = 16).
- **Kobra X**: multicolor nativo de 4 colores, ampliable a 19 con ACE 2 Pro. La
  ficha oficial se contradice con el volumen (`build_volume` en `verify`).
- **Margen**: deja 2–5 mm libres respecto del volumen nominal.

## 2. Validar que cabe en la placa

Pega [references/plate_check.scad](references/plate_check.scad) al final del
checkpoint y llámalo con el tamaño total ya orientado:

```openscad
PRINTER = "anycubic-kobra-3";
assert_fits_plate([width, depth, height], PRINTER);   // falla el render si no cabe
color("Orange") body();
```

Con las herramientas de ScadLab: `measure` dice si cabe y `suggest_orientation`
prueba las seis caras. Para partir una pieza que no cabe, pasadores Ø 4–5 mm con
0,2 mm de holgura o cola de milano.

## 3. Reglas de diseño FDM en Anycubic (boquilla 0,4)

| Tema | Regla |
|---|---|
| Superficie de la cama | Lámina de acero de resorte con PEI (magnética en la Kobra 2): la cara inferior sale con el grano de la lámina. |
| Primera capa | Se aplasta: chaflán de 0,4–0,6 mm a 45° contra la pata de elefante; esquinas largas con radio ≥ 2 mm contra el warping. |
| Camas grandes (Kobra 2 Max / Plus, S1 Max) | Una base muy grande y delgada se levanta por las esquinas; piezas altas y delgadas oscilan en cama móvil. Lado largo en Y, base más ancha u orejas (*mouse ears*). |
| Voladizos | ≤ 45° desde la vertical sin soporte. |
| Puentes | Cómodo hasta ~20 mm entre apoyos en PLA; más, arco o soporte. |
| Ancho de línea | Perfil de 0,4: ~0,42 mm de línea. Pared mínima 2 perímetros ≈ 0,9 mm; estructural 1,6–2,4 mm. Detalle mínimo 0,4–0,5 mm. |
| Holguras (por lado) | A presión 0,1–0,2 mm; deslizante 0,3–0,4; impresas ya montadas 0,3–0,5. Confirma con una pieza de prueba antes de afinar. |
| Materiales | Kobra 3 / Kobra 2 (abiertas): PLA, PETG, TPU. Kobra S1 / S1 Max / X (cerradas): también ABS y ASA; la S1 Max con cámara activa tolera mejor las piezas grandes en ABS. |
| Agujeros | Verticales: suma 0,2 mm al Ø (M3 pasante Ø 3,4). Horizontales Ø > 5 mm: lágrima con la punta arriba. |
| Texto | Altura ≥ 4–6 mm en negrita, trazo ≥ 0,8 mm. |

## 4. Multicolor con ACE

- **ACE Pro** (Kobra 3, S1): 4 colores por unidad, hasta 2 unidades = 8.
  **ACE 2 Pro** (S1 Max, X): 4 colores por unidad, hasta 4 (S1 Max) o 19 colores
  en total (X). Es un alimentador que conecta a un solo extrusor.
- Cada cambio de color **purga** filamento: concentra los colores en pocas capas
  (texto o logo en relieve fino) en vez de repartir cambios en toda la altura.
- Una pieza de primer nivel por color, **sin solapes**; como máximo `colors`
  colores (`printer.json`).
- En Anycubic Slicer Next los filamentos son **extrusores 1..N** que se asignan a
  las ranuras del ACE; los colores concretos los pone el usuario.

## 5. Qué formato exportar

Anycubic Slicer Next está basado en OrcaSlicer: lee las partes y los extrusores de
`Metadata/model_settings.config` de nuestro 3MF.

| Para | Formato | Nota |
|---|---|---|
| **Anycubic Slicer Next** (recomendado) | **`3mf`** | Un objeto con una parte por color y su extrusor. |
| OrcaSlicer | `3mf` | Igual. |
| Cualquiera | `stl-per-color` | Un STL por color en un zip; carga todos juntos como un solo objeto de varias partes y asigna el extrusor a mano. |
| Una sola pieza | `stl` | Sin color. |

La lectura del 3MF se comprobó en el código público de Anycubic Slicer Next, no en la
aplicación; sus rutas de instalación tampoco están verificadas (por eso `slicers`
va en `verify`). Si no aparecen las partes, usa `stl-per-color`.

## 6. Checklist de imprimibilidad

- [ ] Cabe en `PRINTER` con margen (`assert_fits_plate`); Kobra 2: Z es el eje largo.
- [ ] Apoya en z = 0 sobre su cara más ancha; chaflán anti pata de elefante.
- [ ] Sin voladizos > 45° ni puentes > ~20 mm (o justificados con soporte).
- [ ] Paredes ≥ 0,9 mm (estructurales ≥ 1,6); holguras como parámetro.
- [ ] Un `color()` por filamento, como máximo `colors` colores, sin solapes.
