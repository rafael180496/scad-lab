---
name: elegoo-printers
description: Perfiles de impresoras Elegoo (Neptune 4, Neptune 4 Pro, Neptune 4 Max, Centauri Carbon, Centauri Carbon 2) y reglas de diseño FDM para que un .scad se imprima bien en ellas, con multicolor CANVAS y exportación para ElegooSlicer. Úsala cuando printer.json diga una impresora elegoo-*, al dimensionar el modelo, al definir holguras o voladizos, o al preparar un archivo multicolor.
---

# Impresoras Elegoo y diseño para FDM

Requisito previo: `openscad-core` (checkpoints, `color()` obligatorio). Las reglas
FDM generales y sus snippets están en `openscad-core/references/fdm-design-rules.md`;
aquí va lo propio de Elegoo.

## 1. Elegir la impresora

1. Si el workspace tiene `printer.json`, su `printer` (ej. `elegoo-centauri-carbon`)
   es la impresora. Cópiala a una constante en `[Hidden]`:
   `PRINTER = "elegoo-centauri-carbon";`.
2. Sin `printer.json`, asume **`elegoo-neptune-4`** (225×225×265, el menor volumen
   de la marca).
3. Datos legibles por máquina: [references/printers.json](references/printers.json)
   (la app también lo lee). Los campos de `verify` no están confirmados; las URLs
   de la ficha oficial están en `sources`.

| id | Impresora | Volumen X×Y×Z (mm) | Colores (inicial / máx.) | Sistema | Cámara | A confirmar |
|---|---|---|---|---|---|---|
| `elegoo-neptune-4` | Neptune 4 | 225×225×265 | 1 | Ninguno | abierta | `filament_diameter_mm`, `slicers` |
| `elegoo-neptune-4-pro` | Neptune 4 Pro | 225×225×265 | 1 | Ninguno | abierta | `filament_diameter_mm`, `slicers` |
| `elegoo-neptune-4-max` | Neptune 4 Max | 420×420×480 | 1 | Ninguno | abierta | `filament_diameter_mm`, `slicers` |
| `elegoo-centauri-carbon` | Centauri Carbon | 256×256×256 | 1 / 4 | CANVAS (4 colores, accesorio) | cerrada | `multicolor_kind`, `multicolor_slots` |
| `elegoo-centauri-carbon-2` | Centauri Carbon 2 | 256×256×256 | 4 / 4 | CANVAS (4 colores; el Combo lo incluye) | cerrada | `multicolor_kind` |

- **Colores del workspace**: el máximo de colores que puedes usar es `colors` de
  `printer.json`, no el tope de la máquina (`max_colors`). La Centauri Carbon
  nace con **1 color** (CANVAS es un accesorio); la Centauri Carbon 2 con 4. El
  usuario cambia los colores en «Configuración del workspace» o tú con
  `set_workspace_printer`.
- **Neptune 4 / 4 Pro / 4 Max**: Klipper, cartesianas de cama móvil, marco abierto,
  extrusor directo, boquilla hasta 300 °C. La **Neptune 4 Max** (420×420×480) tiene
  la cama más fría (85 °C) y sin cerramiento: ABS y ASA son difíciles de sacar sin que se levanten; úsala con PLA y PETG.
- **Centauri Carbon / Centauri Carbon 2**: CoreXY cerradas de 256×256×256. La CC
  llega a 320 °C y la CC2 a 350 °C; ninguna tiene cámara calefaccionada (rejilla
  inteligente en la CC2).
- **Margen**: deja 2–5 mm libres respecto del volumen nominal.

## 2. Validar que cabe en la placa

Pega [references/plate_check.scad](references/plate_check.scad) al final del
checkpoint y llámalo con el tamaño total ya orientado:

```openscad
PRINTER = "elegoo-centauri-carbon";
assert_fits_plate([width, depth, height], PRINTER);   // falla el render si no cabe
color("Orange") body();
```

Con las herramientas de ScadLab: `measure` dice si cabe y `suggest_orientation`
prueba las seis caras. Para partir una pieza que no cabe, pasadores Ø 4–5 mm con
0,2 mm de holgura o cola de milano.

## 3. Reglas de diseño FDM en Elegoo (boquilla 0,4)

| Tema | Regla |
|---|---|
| Superficie de la cama | Lámina de acero con PEI (la de la Centauri es de doble cara: texturizada y lisa para PLA). La cara inferior sale con el grano de la lámina que uses. |
| Primera capa | Se aplasta: chaflán de 0,4–0,6 mm a 45° contra la pata de elefante; esquinas largas con radio ≥ 2 mm. En la Neptune 4 Max, que es enorme y de cama móvil, una base muy grande y delgada se levanta por las esquinas. |
| Voladizos | ≤ 45° desde la vertical sin soporte. Las Neptune 4 y la Centauri imprimen rápido: deja margen en voladizos límite. |
| Puentes | Cómodo hasta ~20 mm entre apoyos en PLA; más, arco o soporte. |
| Ancho de línea | Perfil de 0,4: ~0,42 mm de línea. Pared mínima 2 perímetros ≈ 0,9 mm; estructural 1,6–2,4 mm. Detalle mínimo 0,4–0,5 mm. |
| Holguras (por lado) | A presión 0,1–0,2 mm; deslizante 0,3–0,4; impresas ya montadas 0,3–0,5. Confirma con una pieza de prueba antes de afinar. |
| Cama móvil (Neptune 4) | Piezas altas y delgadas oscilan: lado largo en Y, base más ancha u orejas (*mouse ears*). |
| Materiales | Neptune 4: PLA, PETG, TPU. Centauri (cerradas): también ABS y ASA; la CC admite boquilla de latón o de acero endurecido. |
| Agujeros | Verticales: suma 0,2 mm al Ø (M3 pasante Ø 3,4). Horizontales Ø > 5 mm: lágrima con la punta arriba. |
| Texto | Altura ≥ 4–6 mm en negrita, trazo ≥ 0,8 mm. |

## 4. Multicolor con CANVAS

- **CANVAS** es el alimentador de 4 colores de Elegoo (en la Centauri Carbon es un
  accesorio; el Combo de la CC2 lo incluye). El catálogo lo registra como
  `multicolor_kind: "cfs"` porque el esquema no tiene un valor propio: se
  comporta como el CFS de Creality o el AMS de Bambu.
- Una pieza de primer nivel por color, **sin solapes**; un cambio de color
  purga filamento, así que concentra los colores en pocas capas (texto o logo en
  relieve fino) en vez de repartir cambios en toda la altura.
- Máximo de colores: `colors` de `printer.json`. Las Neptune 4 no tienen multicolor.
- En ElegooSlicer los filamentos son **extrusores 1..N**; los colores concretos los
  pone el usuario.

## 5. Qué formato exportar

ElegooSlicer es un derivado de OrcaSlicer: lee las partes y los extrusores de
`Metadata/model_settings.config` de nuestro 3MF.

| Para | Formato | Nota |
|---|---|---|
| **ElegooSlicer** (recomendado) | **`3mf`** | Un objeto con una parte por color y su extrusor. |
| OrcaSlicer | `3mf` | Igual. |
| Cura (alternativa) | `stl-per-color` | Un STL por color; asigna el extrusor a mano. |
| Una sola pieza | `stl` | Sin color. |

`3mf` en ElegooSlicer se infiere de que es un fork de Orca; no se probó con la
aplicación real. Si no aparecen las partes, usa `stl-per-color`.

## 6. Checklist de imprimibilidad

- [ ] Cabe en `PRINTER` con margen (`assert_fits_plate`).
- [ ] Apoya en z = 0 sobre su cara más ancha; chaflán anti pata de elefante.
- [ ] Sin voladizos > 45° ni puentes > ~20 mm (o justificados con soporte).
- [ ] Paredes ≥ 0,9 mm (estructurales ≥ 1,6); holguras como parámetro.
- [ ] Un `color()` por filamento, como máximo `colors` colores, sin solapes.
