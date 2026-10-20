---
name: openscad-3mf-multicolor
description: Diseño de modelos multicolor desde OpenSCAD para cualquier sistema multicolor (AMS de Bambu, MMU3 y toolchanger de Prusa, CFS de Creality, CANVAS de Elegoo, ACE de Anycubic, toolchanger o IDEX de Snapmaker, doble extrusión de UltiMaker) y qué formato exportar para cada slicer (3MF, 3MF para PrusaSlicer, STL por color). Úsala cuando el usuario pida más de un color, texto o logo en otro color, export 3MF, o pregunte cómo asignar filamentos o extrusores en su slicer.
---

# Multicolor y 3MF para cualquier slicer

Requisito previo: skill `openscad-core` (checkpoints, `color()` obligatorio). La
skill de marca de la impresora (`bambu-lab-printers`, `prusa-printers`, …) dice cómo
es su sistema multicolor y qué slicer usa.

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

- **Límite de colores** = `colors` de `printer.json`: los colores que el usuario
  eligió para **este workspace** (entre 1 y el tope de la máquina, `max_colors`).
  Una impresora sin sistema multicolor es de 1 color; la misma máquina puede ser
  de 1 o de 4 según tenga o no el accesorio. `printer_info` (MCP) devuelve
  `colors` y `maxColors`; para cambiarlos, «Configuración del workspace» o la
  herramienta `set_workspace_printer`. Si el diseño pide más colores de los que
  hay, avisa y propone agrupar o partir en impresiones.

### Sistemas multicolor (`multicolor_kind`)

| Kind | Sistemas | Cómo cambia de color | Qué cambia al diseñar |
|---|---|---|---|
| `ams` | Bambu AMS / AMS lite / AMS 2 Pro / AMS HT | Un solo extrusor; cada cambio purga | Pocos cambios; relieves finos en pocas capas. AMS lite = 4 por unidad |
| `mmu` | Prusa MMU3 | Un solo extrusor; cada cambio purga en la torre de limpieza | Reserva sitio para la torre; máx. 5 |
| `cfs` | Creality CFS / CFS-C, Elegoo CANVAS | Un solo extrusor; cada cambio purga | Como el AMS; el CFS-C de las K1 es de 4 y sin varias unidades |
| `ace` | Anycubic ACE Pro / ACE 2 Pro | Un solo extrusor; cada cambio purga | Como el AMS; 4 por unidad |
| `toolchanger` | Prusa XL / INDX, Snapmaker U1 | Un cabezal por filamento; se cambia de herramienta | Purga mucho menor; cada cambio de herramienta igual cuesta tiempo |
| `idex` | Snapmaker J1s | Dos cabezales independientes | Máx. 2 colores; los modos Copy / Mirror no sirven para dos colores |
| `dual` | UltiMaker (print cores AA/BB), Snapmaker Artisan | Dos extrusores | Máx. 2 colores; el soporte soluble usa el segundo |
| `none` | impresoras de un solo filamento | — | 1 color; el multicolor se hace por cambio de filamento en una capa |

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
| Cambio de color por capa | geometría de un solo color; el cambio se pone en el slicer sobre el deslizador de capas (todos lo traen) | carteles: base de un color y texto superior de otro, **sin sistema multicolor** (también en una impresora de 1 color) |
| Piezas separadas | cada color es un cuerpo con volumen propio | ensamblajes, bicolor funcional |

**Relieve para cambio por capa**: si el texto sobresale exactamente N capas por
encima de una cara plana y no hay otra geometría a esa altura, basta un único
cambio de filamento en la capa donde empieza el texto (en el deslizador de capas
del slicer, «cambiar filamento / color» en esa capa). Haz que `plate_h` y el
relieve sean múltiplos de la altura de capa prevista.

**Desperdicio de purga**: en un sistema de un solo extrusor (AMS, MMU3, CFS, ACE)
cada cambio de color purga filamento (torre de purga). Concentra los colores en
pocas capas (relieves finos arriba) en vez de repartirlos en toda la altura. Un
toolchanger o una IDEX casi no purgan, pero cambiar de herramienta también cuesta
tiempo.

## 4. Qué formato exportar según el slicer

La herramienta MCP `export` (y los botones de exportar de la app) escribe en
`exports/`; ya conoce la versión de OpenSCAD instalada. `printer_info` devuelve el
slicer recomendado de la impresora (`slicer`) y los otros posibles (`slicers`).

| Formato | Qué es | Para qué slicer |
|---|---|---|
| **`3mf`** | Un objeto con una parte por color; cada parte con su nombre y su extrusor en `Metadata/model_settings.config` | **Bambu Studio**, **OrcaSlicer**, **Creality Print**, **Anycubic Slicer Next** (todos leen ese archivo). **ElegooSlicer** y **Snapmaker Orca**, forks de Orca: por herencia, sin probar. |
| **`3mf-prusa`** | **Una sola malla** con un volumen por color y su extrusor en `Metadata/Slic3r_PE_model.config` | **PrusaSlicer** (no lee `model_settings.config`: con el `3mf` normal las partes llegan como objetos separados y sin extrusor) |
| **`stl-per-color`** | Un STL por color en un zip; todos comparten el origen y quedan alineados | **Cura** (UltiMaker), **Luban** (Snapmaker J1s y Artisan) y cualquier slicer que no lea los 3MF anteriores |
| `stl` | La malla entera, sin color | Una sola pieza o un solo color |

Cómo asignar los extrusores en cada uno:

- **Bambu Studio / Orca / Creality Print / Anycubic / Elegoo / Snapmaker Orca**: el
  `3mf` abre **un objeto con partes**; cada parte ya trae su extrusor (1..N). Los
  colores de los filamentos los pone el sistema multicolor del usuario.
- **PrusaSlicer**: el `3mf-prusa` abre **un objeto con partes**, cada una con su
  extrusor. Con `stl-per-color`: abre todos los STL juntos y responde **Sí** a
  «¿cargar como un solo objeto con varias partes?»; luego asigna el extrusor de
  cada parte en la lista de objetos.
- **Cura**: usa `stl-per-color`: carga los STL y elige el extrusor de cada modelo
  en la barra lateral. El 3MF de ScadLab no le dice a Cura qué extrusor lleva cada
  parte. Una UltiMaker admite como máximo 2 colores.

Ningún formato lleva el color como filamento: los colores concretos los elige el
usuario en su slicer. `displaycolor` solo sirve para verlos en el visor de ScadLab.

### Exportar a mano con OpenSCAD

**Opción A — 3MF con colores de OpenSCAD (2025 / snapshots)**: los snapshots
(2024+) y la versión 2025 exportan 3MF conservando `color()`, especialmente con el
backend Manifold; la 2021.01 estable **no** guarda colores en 3MF. Sale **un solo
objeto con color por triángulo** (`basematerials`), que un slicer puede abrir pero
sin partes ni extrusores: ScadLab lo reparte por color (`export`) para dárselos.

**Opción B — un objeto por color** (cualquier versión de OpenSCAD):

```bash
openscad -D 'PART="base"' -o /tmp/plate-base.stl 002-text-inlay.scad
openscad -D 'PART="text"' -o /tmp/plate-text.stl 002-text-inlay.scad
```

Todas las piezas comparten el mismo origen de coordenadas, así que quedan
alineadas sin moverlas. Se cargan juntas en el slicer como un objeto de varias
partes (ver arriba). El formato interno de los 3MF está en
[references/3mf-format.md](references/3mf-format.md).

## 5. Reglas rápidas

- [ ] Un `color()` de primer nivel por color, con color en parámetro.
- [ ] Sin solapes entre colores (restar donde haga falta).
- [ ] Parámetro `PART` para exportar cada color por separado.
- [ ] Colores totales ≤ `colors` del workspace (`printer.json`).
- [ ] Formato de exportación según el slicer de la impresora (tabla de la sección 4).
- [ ] Relieves/inlays en múltiplos de la altura de capa; texto ≥ 4–6 mm, trazo ≥ 0.8.
- [ ] Detalles de color de al menos 1–2 anchos de línea (≥ 0.8 mm): más finos
      no se imprimen.
