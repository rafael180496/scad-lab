---
name: snapmaker-printers
description: Perfiles de impresoras Snapmaker (U1, J1s / J1, Artisan) y reglas de diseño FDM para que un .scad se imprima bien en ellas, con multicolor de toolchanger (U1), IDEX (J1s) o doble extrusión (Artisan) y exportación para Snapmaker Orca o Luban. Úsala cuando printer.json diga una impresora snapmaker-*, al dimensionar el modelo (el volumen cambia según el modo o el cabezal), al definir holguras o voladizos, o al preparar un archivo multicolor.
---

# Impresoras Snapmaker y diseño para FDM

Requisito previo: `openscad-core` (checkpoints, `color()` obligatorio). Las reglas
FDM generales y sus snippets están en `openscad-core/references/fdm-design-rules.md`;
aquí va lo propio de Snapmaker.

## 1. Elegir la impresora

1. Si el workspace tiene `printer.json`, su `printer` (ej. `snapmaker-u1`) es la
   impresora. Cópiala a una constante en `[Hidden]`: `PRINTER = "snapmaker-u1";`.
2. Sin `printer.json`, asume **`snapmaker-j1s`** (300×200×200, el menor volumen de
   la marca; ojo: su ancho en Y es de 200 mm).
3. Datos legibles por máquina: [references/printers.json](references/printers.json)
   (la app también lo lee). Los campos de `verify` no están confirmados; las URLs
   de la ficha oficial están en `sources`.

| id | Impresora | Volumen X×Y×Z (mm) | Colores (inicial / máx.) | Sistema | Cámara | A confirmar |
|---|---|---|---|---|---|---|
| `snapmaker-u1` | U1 | 270×270×270 | 4 / 4 | SnapSwap (4 cabezales) | abierta | — |
| `snapmaker-j1s` | J1s / J1 | 300×200×200 | 2 / 2 | IDEX (2 cabezales independientes) | cerrada | `slicers`, `enclosed` |
| `snapmaker-artisan` | Artisan | 350×400×400 | 2 / 2 | Doble extrusión (2 cabezales) | cerrada | `slicers`, `filament_diameter_mm` |

- **Colores del workspace**: el máximo de colores que puedes usar es `colors` de
  `printer.json`, no el tope de la máquina (`max_colors`). La U1 nace con 4 colores;
  la J1s y la Artisan con 2 (sus dos cabezales). El usuario los cambia en
  «Configuración del workspace» o tú con `set_workspace_printer`.
- **U1**: toolchanger de 4 cabezales independientes (SnapSwap), cambio en ~5 s. Marco
  abierto; la tapa superior opcional la cierra. Cama 100 °C, boquilla 300 °C.
- **J1s (y J1)**: IDEX, dos cabezales independientes, cerrada. El volumen cambia con
  el modo: **Default / Backup 300×200×200**, **Copy 160×200×200**, **Mirror 150×200×200**
  (campos `build_volume_copy_mode` / `build_volume_mirror_mode`). Diseña para el
  modo en que se va a imprimir.
- **Artisan**: 3 en 1 (FDM, láser, CNC) con cerramiento. Área FDM: **350×400×400**
  con los dos cabezales; 375×400×400 solo con el izquierdo y 400×400×400 solo con
  el derecho (`build_volume_left_nozzle` / `build_volume_right_nozzle`). La cama
  llega a 110 °C en la zona interior (260×260) y a 80 °C fuera de ella.
- **Margen**: deja 2–5 mm libres respecto del volumen nominal.

## 2. Validar que cabe en la placa

Pega [references/plate_check.scad](references/plate_check.scad) al final del
checkpoint y llámalo con el tamaño total ya orientado (la tabla trae el volumen
principal; los otros modos están en los comentarios):

```openscad
PRINTER = "snapmaker-u1";
assert_fits_plate([width, depth, height], PRINTER);   // falla el render si no cabe
color("Orange") body();
```

Con las herramientas de ScadLab: `measure` dice si cabe (con el volumen principal)
y `suggest_orientation` prueba las seis caras. Para partir una pieza que no cabe,
pasadores Ø 4–5 mm con 0,2 mm de holgura o cola de milano.

## 3. Reglas de diseño FDM en Snapmaker (boquilla 0,4)

| Tema | Regla |
|---|---|
| Superficie de la cama | U1: lámina flexible de acero con superficie PEI. J1s: placa de vidrio con PEI. Artisan: lámina PEI de doble textura. La cara inferior sale con el acabado de la superficie. |
| Primera capa | Se aplasta: chaflán de 0,4–0,6 mm a 45° contra la pata de elefante; esquinas largas con radio ≥ 2 mm contra el warping. |
| Voladizos | ≤ 45° desde la vertical sin soporte. |
| Puentes | Cómodo hasta ~20 mm entre apoyos en PLA; más, arco o soporte. |
| Ancho de línea | Perfil de 0,4: ~0,42 mm de línea. Pared mínima 2 perímetros ≈ 0,9 mm; estructural 1,6–2,4 mm. Detalle mínimo 0,4–0,5 mm. |
| Holguras (por lado) | A presión 0,1–0,2 mm; deslizante 0,3–0,4; impresas ya montadas 0,3–0,5. Con dos cabezales, el alineado entre ellos puede dejar una diferencia de décimas de milímetro entre dos colores: no confíes en un ajuste de menos de 0,2 mm **entre piezas de cabezales distintos** sin probar. |
| Materiales | J1s y Artisan cerradas: PLA, PETG, ABS, ASA; la U1 es abierta (tapa opcional). |
| Agujeros | Verticales: suma 0,2 mm al Ø (M3 pasante Ø 3,4). Horizontales Ø > 5 mm: lágrima con la punta arriba. |

## 4. Multicolor en Snapmaker

| Sistema | Cómo funciona | Qué cambia al modelar |
|---|---|---|
| **U1 — toolchanger** | 4 cabezales precalentados, cada uno con su filamento; se cambia de herramienta, no se pasa un filamento por la misma boquilla. | Como no pasa un filamento por la misma boquilla, la purga por cambio es mucho menor que en un AMS (el perfil del slicer puede igual poner una torre de limpieza): un diseño con varios cambios cuesta menos. Hasta 4 colores. |
| **J1s — IDEX** | 2 cabezales independientes, uno por color. | Máximo 2 colores. Los modos **Copy** y **Mirror** imprimen dos copias de la misma pieza a la vez (una por cabezal): no sirven para un modelo de dos colores y el volumen es menor. |
| **Artisan — doble extrusión** | 2 cabezales; el área depende de cuál se use. | Máximo 2 colores; con los dos cabezales el área es 350×400×400. |

Una pieza de primer nivel por color, **sin solapes**; como máximo `colors` colores
(`printer.json`). Los colores concretos los pone el usuario en su perfil.

## 5. Qué formato exportar

| Para | Formato | Nota |
|---|---|---|
| **Snapmaker Orca** (U1) | **`3mf`** | Snapmaker Orca es un derivado de OrcaSlicer: lee las partes y los extrusores de `Metadata/model_settings.config`. |
| OrcaSlicer | `3mf` | Igual. |
| **Luban** (J1s, Artisan) | `stl-per-color` | Luban es el software oficial de J1s y Artisan y no figura entre los slicers de ScadLab; carga un STL por color y asigna cada uno a su cabezal (no verificado con la aplicación). |
| Una sola pieza | `stl` | Sin color. |

`3mf` en Snapmaker Orca se infiere de que es un fork de Orca; no se probó con la
aplicación real. Para la J1s y la Artisan el catálogo marca `slicers` en `verify`.

## 6. Checklist de imprimibilidad

- [ ] Cabe en `PRINTER` con margen (`assert_fits_plate`), con el volumen del modo que se use.
- [ ] Apoya en z = 0 sobre su cara más ancha; chaflán anti pata de elefante.
- [ ] Sin voladizos > 45° ni puentes > ~20 mm (o justificados con soporte).
- [ ] Paredes ≥ 0,9 mm (estructurales ≥ 1,6); holguras como parámetro.
- [ ] Un `color()` por filamento, como máximo `colors` colores, sin solapes.
