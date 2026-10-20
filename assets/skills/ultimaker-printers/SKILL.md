---
name: ultimaker-printers
description: Perfiles de impresoras UltiMaker (S3, S5, S7, S8, Factor 4 / Factor 4 Plus) y reglas de diseño FDM para que un .scad se imprima bien en ellas, con filamento de 2,85 mm, doble extrusión (print cores AA/BB, soporte soluble) y exportación para UltiMaker Cura. Úsala cuando printer.json diga una impresora ultimaker-*, al dimensionar el modelo, al definir holguras o voladizos, o al preparar un archivo de dos colores o con soporte soluble.
---

# Impresoras UltiMaker y diseño para FDM

Requisito previo: `openscad-core` (checkpoints, `color()` obligatorio). Las reglas
FDM generales y sus snippets están en `openscad-core/references/fdm-design-rules.md`;
aquí va lo propio de UltiMaker.

## 1. Elegir la impresora

1. Si el workspace tiene `printer.json`, su `printer` (ej. `ultimaker-s5`) es la
   impresora. Cópiala a una constante en `[Hidden]`: `PRINTER = "ultimaker-s5";`.
2. Sin `printer.json`, asume **`ultimaker-s3`** (230×190×200, la más chica).
3. Datos legibles por máquina: [references/printers.json](references/printers.json)
   (la app también lo lee). Los campos de `verify` no están confirmados; las URLs
   de la ficha oficial están en `sources`.

| id | Impresora | Volumen X×Y×Z (mm) | Colores (inicial / máx.) | Sistema | Cámara | A confirmar |
|---|---|---|---|---|---|---|
| `ultimaker-s3` | S3 | 230×190×200 | 2 / 2 | Doble extrusión (print cores AA/BB) | cerrada | `max_bed_temp_c`, `enclosed` |
| `ultimaker-s5` | S5 | 330×240×300 | 2 / 2 | Doble extrusión (print cores AA/BB) | cerrada | `max_bed_temp_c`, `enclosed` |
| `ultimaker-s7` | S7 | 330×240×300 | 2 / 2 | Doble extrusión (print cores AA/BB) | cerrada | `max_bed_temp_c` |
| `ultimaker-s8` | S8 | 330×240×300 | 2 / 2 | Doble extrusión (print cores AA/BB) | cerrada | `max_nozzle_temp_c`, `max_bed_temp_c`, `bed_surface`, `enclosed` |
| `ultimaker-factor-4` | Factor 4 | 330×240×300 | 2 / 2 | Doble extrusión (print cores AA/BB) | cerrada, calefaccionada | `max_bed_temp_c`, `bed_surface`, `heated_chamber` |

- **Filamento de 2,85 mm** en todas (no 1,75). ScadLab ya lo usa en la estimación de
  peso y metros de filamento de este workspace (`filament_diameter_mm` de
  `printer.json`); si compartes un modelo o usas otras tablas, no mezcles diámetros.
- **Colores del workspace**: las UltiMaker son de **doble extrusión**: `colors` de
  `printer.json` es como máximo 2, y nacen con 2. No hay forma de pasar de 2 con un
  solo trabajo: un diseño de tres colores se parte en dos impresiones o se agrupa.
  El usuario cambia los colores en «Configuración del workspace» o tú con
  `set_workspace_printer`.
- **Cámara**: S3 y S5 parcialmente cerradas (cámara pasiva); S7 totalmente cerrada
  con puerta de vidrio; Factor 4 con cámara aislada y control térmico (`heated_chamber`
  está en `verify`: confírmalo antes de prometer ABS/ASA de gran tamaño).
- **Print cores** intercambiables (0,25 / 0,4 / 0,6 / 0,8 mm): la tabla supone el AA
  de 0,4 mm.
- **Margen**: deja 2–5 mm libres respecto del volumen nominal; con doble extrusión
  la torre de purga (*prime tower*) de Cura ocupa un rincón de la placa.

## 2. Validar que cabe en la placa

Pega [references/plate_check.scad](references/plate_check.scad) al final del
checkpoint y llámalo con el tamaño total ya orientado:

```openscad
PRINTER = "ultimaker-s5";
assert_fits_plate([width, depth, height], PRINTER);   // falla el render si no cabe
color("Orange") body();
```

Con las herramientas de ScadLab: `measure` dice si cabe y `suggest_orientation`
prueba las seis caras. En las S3 (230×190) la placa es bastante más chica que en las
demás (330×240): revisa el X antes de proponer una pieza ancha.

## 3. Reglas de diseño FDM en UltiMaker (boquilla 0,4)

| Tema | Regla |
|---|---|
| Superficie de la cama | S3 y S5: cristal calefactado; S7 y S8: acero flexible. En cristal suele usarse adhesivo (barra de pegamento o láminas de adherencia) y la cara inferior sale **lisa y brillante**; la lámina flexible da un acabado distinto. Confirma el adhesivo del usuario. |
| Primera capa | Se aplasta: chaflán de 0,4–0,6 mm a 45° contra la pata de elefante; esquinas largas con radio ≥ 2 mm contra el warping. En piezas grandes de ABS/ASA usa *brim* generoso. |
| Voladizos | ≤ 45° desde la vertical sin soporte. En doble extrusión puedes mandar el soporte al segundo núcleo (BB) con un material soluble: ahí el voladizo deja de ser un problema de diseño. |
| Puentes | Cómodo hasta ~20 mm entre apoyos en PLA; más, arco o soporte. |
| Ancho de línea | Perfil de 0,4: ~0,4 mm de línea. Pared mínima 2 perímetros ≈ 0,8–0,9 mm; estructural 1,6–2,4 mm. Detalle mínimo 0,4–0,5 mm. |
| Holguras (por lado) | A presión 0,1–0,2 mm; deslizante 0,3–0,4; impresas ya montadas 0,3–0,5. Con soporte soluble pueden bajar (0,2–0,3) porque no queda marca de soporte en el hueco: confirma con una pieza de prueba. |
| Dos materiales | Las dos boquillas no están a la misma altura: el *core* inactivo se sube y la pieza no debe rozarlo. Piezas de dos materiales que se tocan en una cara plana tienen buena adherencia solo si los materiales son compatibles (PLA con PLA). |
| Materiales | PLA, Tough PLA, PETG, nylon, ABS, TPU según el *core* y la máquina; soportes solubles (PVA) y *breakaway* en el *core* BB. |
| Agujeros | Verticales: suma 0,2 mm al Ø (M3 pasante Ø 3,4). Horizontales Ø > 5 mm: lágrima con la punta arriba. |

## 4. Multicolor: doble extrusión en Cura

- Un trabajo imprime con **dos extrusores** (0 y 1 en Cura; 1 y 2 en las
  herramientas de ScadLab). El primero es el *core* AA; el segundo suele ser el BB.
- Una pieza de primer nivel por color, **sin solapes**, y como máximo 2 colores.
  Para color + soporte soluble, el soporte es la segunda "pieza" y no se modela:
  lo genera Cura.
- Cada cambio de núcleo implica retraer el que se aparta; la torre de purga limpia
  la boquilla que entra. Menos cambios de capa = menos tiempo y desperdicio.
- Los colores concretos los pone el usuario en Cura (material de cada extrusor).

## 5. Qué formato exportar

| Para | Formato | Nota |
|---|---|---|
| **UltiMaker Cura** (recomendado) | **`stl-per-color`** | Un STL por color en un zip; todos comparten el origen, así que quedan alineados. En Cura, carga todos y elige a qué extrusor va cada modelo con el selector de extrusor de la barra lateral. Si Cura los separa al cargarlos, desactiva «Asegurar que los modelos se mantengan separados» en Preferencias o usa «Combinar modelos» y comprueba que las partes quedaron alineadas (el comportamiento varía entre versiones de Cura). Es el camino más seguro, aunque tampoco se probó con la aplicación. |
| UltiMaker Cura (alternativa) | `3mf` | Carga la geometría con una parte por color, pero **sin** extrusor asignado: elígelo a mano en Cura para cada parte. |
| Una sola pieza | `stl` | Sin color. |

No uses `3mf-prusa` aquí: es para PrusaSlicer.

## 6. Checklist de imprimibilidad

- [ ] Cabe en `PRINTER` con margen (`assert_fits_plate`); en S3, X ≤ 230 mm.
- [ ] Apoya en z = 0 sobre su cara más ancha; chaflán anti pata de elefante.
- [ ] Sin voladizos > 45° ni puentes > ~20 mm (o con soporte, soluble si es doble extrusión).
- [ ] Paredes ≥ 0,8–0,9 mm (estructurales ≥ 1,6); holguras como parámetro.
- [ ] Un `color()` por extrusor, **como máximo 2 colores**, sin solapes.
