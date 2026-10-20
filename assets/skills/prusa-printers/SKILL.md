---
name: prusa-printers
description: Perfiles de impresoras Prusa Research (MK4S, CORE One+, CORE One L+, MINI+, XL) y reglas de diseño FDM para que un .scad se imprima bien en ellas, con multicolor MMU3 o toolchanger XL y exportación para PrusaSlicer (3mf-prusa). Úsala cuando printer.json diga una impresora prusa-*, al dimensionar el modelo, al definir holguras o voladizos, o al preparar un archivo multicolor para PrusaSlicer.
---

# Impresoras Prusa Research y diseño para FDM

Requisito previo: `openscad-core` (checkpoints, `color()` obligatorio). Las reglas
FDM generales y sus snippets (lágrima, chaflán, insertos, texto) están en
`openscad-core/references/fdm-design-rules.md`; aquí va lo propio de Prusa.

## 1. Elegir la impresora

1. Si el workspace tiene `printer.json`, su `printer` (ej. `prusa-mk4s`) es la
   impresora. Cópiala a una constante en `[Hidden]`: `PRINTER = "prusa-mk4s";`.
2. Sin `printer.json`, asume **`prusa-mini`** (la placa más chica de la marca).
3. Datos legibles por máquina: [references/printers.json](references/printers.json)
   (la app también lo lee). Los campos de `verify` no están confirmados; las URLs
   de la ficha oficial están en `sources`.

| id | Impresora | Volumen X×Y×Z (mm) | Colores (inicial / máx.) | Sistema | Cámara | A confirmar |
|---|---|---|---|---|---|---|
| `prusa-mk4s` | MK4S | 250×210×220 | 1 / 5 | Prusa MMU3 (opcional) | abierta | — |
| `prusa-core-one` | CORE One / CORE One+ | 250×220×270 | 1 / 5 | MMU3 (5) o INDX (4 a 8 herramientas) | cerrada, calefaccionada | — |
| `prusa-core-one-l` | CORE One L+ | 300×300×330 | 1 / 5 | MMU3 o INDX (4 a 8 herramientas) | cerrada, calefaccionada | `bed_surface`, `filament_diameter_mm` |
| `prusa-mini` | MINI+ | 180×180×180 | 1 | Ninguno | abierta | `multicolor_slots` |
| `prusa-xl` | XL / XL+ | 360×360×360 | 1 / 5 | Toolchanger XL (1 a 5 cabezales) | abierta | `max_bed_temp_c` |

- **Colores del workspace**: el máximo de colores que puedes usar es `colors` de
  `printer.json` (lo que el usuario eligió para este workspace), no el tope de la
  máquina (`max_colors`). Una MK4S sin MMU3 es de **1 color**. Para otra
  impresora o más colores, el usuario lo cambia en «Configuración del workspace»
  o tú con `set_workspace_printer`.
- **MK4S**: 250×210×220 es la cifra de la ficha oficial (X×Y×Z). Cama de 120 °C,
  boquilla hasta 290 °C, abierta (el cerramiento es opcional).
- **CORE One+ / CORE One L+**: CoreXY cerradas con cámara activa (55 y 60 °C):
  aptas para ASA/ABS. El kit **INDX** (toolchanger de 4 a 8 herramientas) reduce
  el volumen (CORE One+: 248×205×270, campo `build_volume_with_indx`).
- **MINI+**: extrusor Bowden, sin multicolor declarado: evita los flexibles muy
  blandos y espera algo de hilos en PETG.
- **XL**: de 1 a 5 cabezales; `prusa-xl` nace con 1 color y el usuario sube hasta 5
  según los cabezales que tenga.
- **Margen**: deja 2–5 mm libres respecto del volumen nominal.

## 2. Validar que cabe en la placa

Pega [references/plate_check.scad](references/plate_check.scad) al final del
checkpoint y llámalo con el tamaño total ya orientado:

```openscad
PRINTER = "prusa-mk4s";
assert_fits_plate([width, depth, height], PRINTER);   // falla el render si no cabe
color("Orange") body();
```

Con las herramientas de ScadLab: `measure` dice si cabe (`fitsBuildVolume`) y
`suggest_orientation` prueba las seis caras hacia abajo. Para partir una pieza
que no cabe, uniones con pasadores Ø 4–5 mm con 0,2 de holgura o cola de milano.

## 3. Reglas de diseño FDM en Prusa (boquilla 0,4)

| Tema | Regla |
|---|---|
| Superficie de la cama | La cara inferior copia la lámina: **lisa** (Smooth PEI) = brillante; **texturizada o satinada** = mate con grano. Elige la lámina según el acabado que quieras abajo; una cara que debe quedar lisa va contra la lámina lisa. |
| Primera capa | Se aplasta: sin detalles de menos de 0,4 mm en la cara inferior; chaflán de 0,4–0,6 mm a 45° contra la pata de elefante. Esquinas largas con radio ≥ 2 mm contra el warping. |
| Voladizos | ≤ 45° desde la vertical sin soporte; más, chaflán, otra orientación o soporte (PrusaSlicer puede generarlos o dibujarlos a mano en la pieza). |
| Puentes | Cómodo hasta ~20 mm entre apoyos en PLA; más largo, arco o soporte. |
| Ancho de línea | Los perfiles de PrusaSlicer usan ~0,45 mm para perímetros con boquilla de 0,4 «verificar en el perfil instalado». Pared mínima: 2 perímetros ≈ 0,9 mm; estructural 1,6–2,4 mm. |
| Holguras (por lado) | A presión 0,1–0,2 mm; deslizante 0,3–0,4; impresas ya montadas 0,3–0,5. Las máquinas de extrusor directo y *input shaper* (MK4S, CORE One) son consistentes: confirma con una pieza de prueba antes de afinar por debajo de 0,15. |
| Altura de capa | Perfiles de 0,20 mm (QUALITY / SPEED) por defecto; 0,15 y 0,10 para detalle. Relieves y alturas críticas en **múltiplos** de la capa prevista. |
| Agujeros | Verticales: suma 0,2 mm al Ø (M3 pasante Ø 3,4). Horizontales Ø > 5 mm: lágrima con la punta arriba. |
| Materiales | MK4S abierta: PLA, PETG, TPU. ASA/ABS/PC piden la cámara de la CORE One o un cerramiento. |
| Texto | Altura ≥ 4–6 mm en negrita, trazo ≥ 0,8 mm. |

## 4. Multicolor en Prusa

El sistema decide cómo diseñar. En todos: **una pieza de primer nivel por color**,
sin solapes entre colores (resta el hueco de una antes de rellenarlo con la otra) y
no más piezas distintas que `colors`.

| Sistema | Cómo funciona | Qué cambia al modelar |
|---|---|---|
| **MMU3** (MK4S, CORE One) | Un solo cabezal, hasta 5 filamentos. Cada cambio pasa el filamento por la misma boquilla y **purga en la torre de limpieza** (*wipe tower*). | Deja sitio en la placa para la torre. Menos cambios = menos tiempo y desperdicio: prefiere relieves finos en pocas capas a colores alternados capa por capa. |
| **Toolchanger XL** | Hasta 5 cabezales, cada uno con su filamento; se cambia de herramienta, no de filamento. | Purga bastante menos que el MMU3; el cambio de herramienta igual cuesta tiempo en cada capa donde hay varios colores. |
| **INDX** (CORE One+ / L+) | Toolchanger de 4 a 8 herramientas (kit aparte, volumen menor). | Igual que el XL; usa el volumen reducido (`build_volume_with_indx`). |

En PrusaSlicer los filamentos son **extrusores**: la parte con `extruder 2` se
imprime con el filamento 2 del perfil. Los colores concretos los elige el usuario
en su perfil, no el archivo.

## 5. Qué formato exportar

La herramienta MCP `export` (y el botón de la app) escribe en `exports/`:

| Para | Formato | Nota |
|---|---|---|
| **PrusaSlicer** (recomendado) | **`3mf-prusa`** | Una sola malla con un volumen por color y su extrusor (`Metadata/Slic3r_PE_model.config`). Se abre como **un objeto con varias partes**, cada una ya con su extrusor. |
| OrcaSlicer | `3mf` | Lee las partes y extrusores de `model_settings.config`. |
| Cualquiera | `stl-per-color` | Un STL por color en un zip. En PrusaSlicer, abre todos juntos y responde **Sí** a «¿cargar como un solo objeto con varias partes?»; después asigna el extrusor de cada parte en la lista de objetos. |
| Una sola pieza | `stl` | Sin color. |

- **No uses `3mf` en PrusaSlicer**: no lee `model_settings.config`, así que las
  partes llegan como objetos separados sin extrusor.
- `3mf-prusa` se escribió leyendo el código de PrusaSlicer 2.9 y todavía no se
  probó con la aplicación real. Si no ves las partes, usa `stl-per-color`.

## 6. Checklist de imprimibilidad

- [ ] Cabe en `PRINTER` con margen (`assert_fits_plate`); con MMU3, sitio para la torre.
- [ ] Apoya en z = 0 sobre su cara más ancha; chaflán anti pata de elefante.
- [ ] Sin voladizos > 45° ni puentes > ~20 mm (o justificados con soporte).
- [ ] Paredes ≥ 0,9 mm (estructurales ≥ 1,6); holguras como parámetro.
- [ ] Un `color()` por filamento y como máximo `colors` colores; sin solapes.
- [ ] Exportación `3mf-prusa` si el slicer es PrusaSlicer.
