---
name: creality-printers
description: Perfiles de impresoras Creality (Ender-3 V3, V3 SE, V3 KE, K1, K1C, K1 Max, K2, K2 Pro, K2 Plus, Hi) y reglas de diseño FDM para que un .scad se imprima bien en ellas, con multicolor CFS y exportación para Creality Print. Úsala cuando printer.json diga una impresora creality-*, al dimensionar el modelo, al definir holguras o voladizos, o al preparar un archivo multicolor.
---

# Impresoras Creality y diseño para FDM

Requisito previo: `openscad-core` (checkpoints, `color()` obligatorio). Las reglas
FDM generales y sus snippets están en `openscad-core/references/fdm-design-rules.md`;
aquí va lo propio de Creality.

## 1. Elegir la impresora

1. Si el workspace tiene `printer.json`, su `printer` (ej. `creality-k1c`) es la
   impresora. Cópiala a una constante en `[Hidden]`: `PRINTER = "creality-k1c";`.
2. Sin `printer.json`, asume **`creality-ender-3-v3-ke`** (el menor volumen de la
   marca, 220×220×240: lo que cabe ahí cabe en todas).
3. Datos legibles por máquina: [references/printers.json](references/printers.json)
   (la app también lo lee). Los campos de `verify` no están confirmados; las URLs
   de la ficha oficial están en `sources`.

| id | Impresora | Volumen X×Y×Z (mm) | Colores (inicial / máx.) | Sistema | Cámara | A confirmar |
|---|---|---|---|---|---|---|
| `creality-ender-3-v3` | Ender-3 V3 | 220×220×250 | 1 | Ninguno | abierta | — |
| `creality-ender-3-v3-se` | Ender-3 V3 SE | 220×220×250 | 1 | Ninguno | abierta | `enclosed`, `filament_diameter_mm` |
| `creality-ender-3-v3-ke` | Ender-3 V3 KE | 220×220×240 | 1 | Ninguno | abierta | — |
| `creality-k1` | K1 | 220×220×250 | 1 / 4 | CFS-C (kit, 4 colores; 1 unidad) | cerrada | `default_colors` |
| `creality-k1c` | K1C | 220×220×250 | 1 / 4 | CFS-C (kit, 4 colores; 1 unidad) | cerrada | — |
| `creality-k1-max` | K1 Max | 300×300×300 | 1 / 4 | CFS-C (kit, 4 colores; 1 unidad) | cerrada | — |
| `creality-k2-plus` | K2 Plus | 350×350×350 | 4 / 16 | CFS (hasta 4 unidades = 16) | cerrada, calefaccionada | — |
| `creality-k2-pro` | K2 Pro | 300×300×300 | 4 / 16 | CFS (hasta 4 unidades = 16) | cerrada, calefaccionada | `bed_surface` |
| `creality-k2` | K2 | 260×260×260 | 4 / 16 | CFS (hasta 4 unidades = 16) | cerrada | `bed_surface`, `enclosed` |
| `creality-hi` | Hi / Hi Combo | 260×260×300 | 4 / 16 | CFS (el Combo trae 1; hasta 4 = 16) | abierta | — |

- **Colores del workspace**: el máximo de colores que puedes usar es `colors` de
  `printer.json`, no el tope de la máquina (`max_colors`). Una K1 sin CFS-C es de
  **1 color**; el usuario cambia los colores en «Configuración del workspace» o tú
  con `set_workspace_printer`.
- **Serie Ender-3 V3** (abiertas, extrusor directo): la SE es de cama móvil y 260 °C;
  la V3 y la KE llegan a 300 °C. Cama de 100–110 °C. Sin multicolor.
- **Serie K1** (K1, K1C, K1 Max): cerradas con refrigeración activa de la cámara,
  no calefaccionada. Multicolor solo con el kit **CFS-C** (4 ranuras, no admite
  varias unidades). La K1C trae boquilla de acero endurecido.
- **Serie K2** (K2, K2 Pro, K2 Plus): multicolor con **CFS** (hasta 4 unidades = 16
  colores; el Combo trae 1). La K2 Pro y la K2 Plus tienen cámara calefaccionada
  (hasta 60 °C); la K2 no.
- **Hi**: marco abierto, hasta 4 CFS (16 colores), el Combo trae 1.
- **Margen**: deja 2–5 mm libres respecto del volumen nominal.

## 2. Validar que cabe en la placa

Pega [references/plate_check.scad](references/plate_check.scad) al final del
checkpoint y llámalo con el tamaño total ya orientado:

```openscad
PRINTER = "creality-k1c";
assert_fits_plate([width, depth, height], PRINTER);   // falla el render si no cabe
color("Orange") body();
```

Con las herramientas de ScadLab: `measure` dice si cabe y `suggest_orientation`
prueba las seis caras. Para partir una pieza que no cabe, pasadores Ø 4–5 mm con
0,2 mm de holgura o cola de milano.

## 3. Reglas de diseño FDM en Creality (boquilla 0,4)

| Tema | Regla |
|---|---|
| Superficie de la cama | Lámina flexible de acero de resorte con PEI (o PC/epoxi en las Ender): la cara inferior sale con el grano de la lámina. La lámina se dobla para despegar: piezas con base muy grande y rígida piden más cuidado. |
| Primera capa | Se aplasta: chaflán de 0,4–0,6 mm a 45° contra la pata de elefante; esquinas largas con radio ≥ 2 mm contra el warping. |
| Voladizos | ≤ 45° desde la vertical sin soporte. Las K1/K2 imprimen rápido: un voladizo límite sale peor que en una máquina lenta; deja margen. |
| Puentes | Cómodo hasta ~20 mm entre apoyos en PLA; más, arco o soporte. |
| Ancho de línea | Perfil de 0,4: ~0,42 mm de línea. Pared mínima 2 perímetros ≈ 0,9 mm; estructural 1,6–2,4 mm. Detalle mínimo 0,4–0,5 mm. |
| Holguras (por lado) | A presión 0,1–0,2 mm; deslizante 0,3–0,4; impresas ya montadas 0,3–0,5. Las Ender de cama móvil (SE) pueden variar más entre ejes: confirma con una pieza de prueba. |
| Velocidad y rigidez | A alta velocidad aparecen *ringing* y vibraciones en esquinas y texto pequeño: usa texto en negrita ≥ 6 mm y trazo ≥ 0,8 mm. Piezas altas y delgadas oscilan en cama móvil (Ender-3 V3 SE). |
| Materiales | K1 / K1C / K1 Max cerradas: PLA, PETG, ABS, ASA (con PLA suele convenir abrir la tapa o ventilar la cámara). Serie Ender-3 V3 abierta: PLA, PETG, TPU. |
| Agujeros | Verticales: suma 0,2 mm al Ø (M3 pasante Ø 3,4). Horizontales Ø > 5 mm: lágrima con la punta arriba. |

## 4. Multicolor con CFS / CFS-C

- **CFS** (K2, Hi) y **CFS-C** (kit para K1): un alimentador de 4 bobinas por unidad
  que conecta a un solo extrusor. Cada cambio de color **purga** filamento: un
  modelo con muchos cambios gasta más en purga que en la pieza.
- Una pieza de primer nivel por color, **sin solapes**. Concentra los colores en
  pocas capas (texto o logo en relieve fino) en vez de repartir cambios en toda
  la altura.
- Máximo de colores: `colors` de `printer.json`. Con más de 4 hacen falta varios
  CFS (solo K2 y Hi; la K1 con CFS-C no admite varias unidades).
- En Creality Print los filamentos son **extrusores 1..N** que se asignan a las
  ranuras del CFS; los colores concretos los pone el usuario.

## 5. Qué formato exportar

Creality Print está basado en OrcaSlicer y **lee las partes y los extrusores** de
`Metadata/model_settings.config` de nuestro 3MF.

| Para | Formato | Nota |
|---|---|---|
| **Creality Print** (recomendado) | **`3mf`** | Un objeto con una parte por color y su extrusor. |
| OrcaSlicer | `3mf` | Igual. |
| Cualquiera | `stl-per-color` | Un STL por color en un zip; carga todos juntos como un solo objeto de varias partes y asigna el extrusor a mano. |
| Una sola pieza | `stl` | Sin color. |

`3mf` no está probado en la aplicación real de Creality Print (se comprobó leyendo
su código). Si no aparecen las partes, usa `stl-per-color`.

## 6. Checklist de imprimibilidad

- [ ] Cabe en `PRINTER` con margen (`assert_fits_plate`).
- [ ] Apoya en z = 0 sobre su cara más ancha; chaflán anti pata de elefante.
- [ ] Sin voladizos > 45° ni puentes > ~20 mm (o justificados con soporte).
- [ ] Paredes ≥ 0,9 mm (estructurales ≥ 1,6); holguras como parámetro.
- [ ] Un `color()` por filamento, como máximo `colors` colores, sin solapes.
