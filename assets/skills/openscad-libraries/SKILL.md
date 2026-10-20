---
name: openscad-libraries
description: Cuándo y cómo usar librerías externas de OpenSCAD (BOSL2, MCAD, NopSCADlib) dentro de un workspace de ScadLab, instalándolas en lib/ e incluyéndolas con include/use. Úsala cuando el modelo necesite roscas, redondeos complejos, attachments, engranajes no rectos o modelos de tornillería/componentes, o al ver errores de include.
---

# Librerías de OpenSCAD en ScadLab

## 1. Primero: ¿hace falta una librería?

**Por defecto, código propio sin dependencias.** Un checkpoint sin librerías se
renderiza en cualquier máquina, es más rápido y nunca cambia por una
actualización ajena. Las plantillas de `openscad-templates` y los snippets de
`openscad-core/references/fdm-design-rules.md` cubren cajas, redondeos con
`hull()`/`offset()`, lágrimas, insertos y engranajes rectos.

| Necesidad | Solución |
|---|---|
| Caja redondeada, cilindros, agujeros, chaflanes simples | código propio |
| Engranaje recto | `openscad-templates/templates/spur-gear.scad` |
| **Rosca imprimible** (tornillo/tuerca/tapa roscada) | BOSL2 `threading.scad` |
| Redondear **aristas arbitrarias** de un cubo/cilindro | BOSL2 `cuboid(rounding=...)`, `cyl(rounding=...)` |
| Posicionar piezas relativas a caras (attachments) | BOSL2 `attach()`, `position()`, `diff()` |
| Engranajes helicoidales, cónicos, cremalleras, sinfín | BOSL2 `gears.scad` |
| Barridos, `skin`, curvas Bézier, `path_sweep` | BOSL2 |
| Tornillería métrica con cabezas y alojamientos | BOSL2 `screws.scad` |
| Modelos de componentes reales (PCBs, ventiladores, displays) para chequear interferencias | NopSCADlib |
| Algo puntual de MCAD | preferir BOSL2 o código propio |

## 2. Instalar en `lib/` del workspace

**Con las herramientas de ScadLab (MCP) —lo normal, porque no tienes shell—:**
`list_libraries` dice qué hay (BOSL2, MCAD y NopSCADlib, con versión, licencia y
la línea `include` exacta) y qué está instalado; `install_library(name)` la
descarga a `lib/<nombre>/` (necesita internet: avisa al usuario). Baja un
commit fijo y comprueba su SHA-256 antes de extraer, es todo o nada, anota la
instalación en `lib/LIBRARIES.txt` y **nunca reemplaza** una librería ya
instalada. Si no hay internet, dilo en vez de inventar el código de la librería.

Si trabajas con shell (git), el equivalente manual es:

Las librerías viven **dentro del workspace**, en `lib/`, nunca en una ruta
global: así el workspace es portable y la app puede renderizarlo igual en otra
máquina.

```bash
mkdir -p lib
# BOSL2 — fijar un commit o tag concreto (BOSL2 sigue en beta y cambia su API)
git clone https://github.com/BelfrySCAD/BOSL2.git lib/BOSL2
git -C lib/BOSL2 checkout <commit-o-tag>
# NopSCADlib
git clone --depth 1 https://github.com/nophead/NopSCADlib.git lib/NopSCADlib
# MCAD (también viene empaquetada con muchas instalaciones de OpenSCAD)
git clone --depth 1 https://github.com/openscad/MCAD.git lib/MCAD
```

- Si no hay red o `git`, avisa al usuario en vez de inventar el código de la
  librería.
- Anota en `lib/LIBRARIES.txt` el nombre, URL y commit de cada librería.
- **Sin actualizar en el lugar**: los checkpoints existentes hacen `include` de la
  librería sin versión, así que una librería ya usada **no se actualiza en el
  lugar**: cambiaría el resultado de modelos que ya estaban bien. Para una versión
  nueva, clónala en otra carpeta (`lib/BOSL2-2026-10`) y úsala desde los
  checkpoints que la necesiten (`install_library` nunca reemplaza una instalada).
- No borres `.git` dentro de `lib/` salvo que el usuario lo pida.

## 3. `include` vs `use`

| Sentencia | Qué importa | Ejecuta geometría del archivo |
|---|---|---|
| `include <f.scad>` | módulos, funciones **y variables/constantes** | sí (todo lo de primer nivel) |
| `use <f.scad>` | solo módulos y funciones | no |

- **BOSL2 exige `include`**: `include <lib/BOSL2/std.scad>` (define constantes
  como `TOP`, `BOTTOM`, `CENTER`). Con `use` fallan sus constantes.
- **MCAD**: normalmente `use <lib/MCAD/archivo.scad>`; algunos archivos
  (`nuts_and_bolts.scad`, `units.scad`) necesitan `include` por sus constantes.
- **NopSCADlib**: `include <lib/NopSCADlib/lib.scad>` (o el archivo de vitamina
  concreto) «verificar ruta exacta según versión».

Rutas: OpenSCAD busca primero relativo al archivo que incluye, así que desde un
checkpoint en la raíz del workspace `include <lib/BOSL2/std.scad>` funciona sin
configurar nada. Alternativa: `OPENSCADPATH=./lib openscad ...` y entonces
`include <BOSL2/std.scad>`. Elige una forma y úsala en todo el workspace;
recomendado: la ruta relativa `lib/...` (no depende de variables de entorno).

## 4. BOSL2 — lo esencial

```openscad
include <lib/BOSL2/std.scad>
include <lib/BOSL2/threading.scad>

/* [Hidden] */
$fa = $preview ? 12 : 3;
$fs = $preview ? 1 : 0.3;
$slop = 0.15;   // holgura global de BOSL2 para roscas/encajes; calibrar por impresora

color("SteelBlue")
    diff()
        cuboid([60, 40, 20], rounding = 3, except = BOTTOM, anchor = BOTTOM)
            tag("remove") position(TOP) cyl(d = 10, h = 30);   // agujero ciego de 15 mm

color("Orange")
    right(50) threaded_rod(d = 12, l = 20, pitch = 1.75, anchor = BOTTOM);
```

Conceptos:

- **Anchors**: `anchor = BOTTOM` apoya el objeto sobre z=0 (útil para cumplir
  "el modelo apoya en z=0"). Direcciones: `TOP`, `BOTTOM`, `LEFT`, `RIGHT`,
  `FRONT`, `BACK`, combinables (`TOP + RIGHT`).
- **Attachments**: `attach(TOP) hijo()` pega el hijo a la cara superior del
  padre; `position(TOP)` lo ubica sin reorientar.
- **`diff()` + `tag("remove")`**: restas declarativas dentro de un árbol de
  attachments (reemplaza `difference()` cuando hay attachments).
- **Redondeos/chaflanes**: `cuboid(size, rounding=r, edges=..., except=...)`,
  `cuboid(size, chamfer=c)`, `cyl(h, d, rounding1=, rounding2=)`. `except =
  BOTTOM` deja la base viva (mejor primera capa; añade tú un chaflán de 0.4 si
  hace falta contra la pata de elefante).
- **Roscas**: `threaded_rod(d, l, pitch)`, `threaded_nut(nutwidth, id, h,
  pitch)`. Imprimibles bien desde M8 con capa ≤ 0.16; `$slop` 0.1–0.2.
- **Tornillería**: `screw("M3", length = 10)`, `screw_hole("M3", length = 10,
  head = "socket", counterbore = 3)`, `nut_trap_inline(...)` «verificar firmas en
  la versión instalada».
- **Engranajes**: `spur_gear(mod = 1.5, teeth = 20, thickness = 6)`,
  `rack(...)`, `bevel_gear(...)`; mismo `mod` y `pressure_angle` para engranar.
- **Colores**: envuelve el resultado de primer nivel en `color()` como siempre.
  BOSL2 también tiene `recolor()`; la regla de `openscad-core` sigue valiendo.

Documentación: wiki de BOSL2 en GitHub (`BelfrySCAD/BOSL2/wiki`). Ante una
duda de firma, **lee el archivo fuente en `lib/BOSL2/`** (cada módulo trae su
bloque de documentación) en lugar de suponer parámetros.

## 5. MCAD

Librería antigua, poco mantenida, incluida en muchas instalaciones de OpenSCAD.

```openscad
use <lib/MCAD/boxes.scad>
color("Gray") roundedBox(size = [40, 30, 10], radius = 3, sidesonly = true);
```

Útil para cosas puntuales (`roundedBox`, `nuts_and_bolts.scad`). Para
engranajes, `MCAD/involute_gears.scad` funciona pero BOSL2 o la plantilla
propia son mejores. No la mezcles con BOSL2 en el mismo archivo si no hace
falta (nombres que chocan).

## 6. NopSCADlib

Biblioteca de "vitaminas" (piezas compradas: tornillos, tuercas, PCBs,
ventiladores, displays, conectores) y utilidades de Nophead.

```openscad
include <lib/NopSCADlib/lib.scad>
// ejemplo: tornillo M3 cabeza allen de 10 mm, solo para chequear interferencias
%screw(M3_cap_screw, 10);
```

- Úsala para **verificar** que un componente real entra (con `%`, no se
  exporta), no como geometría a imprimir.
- Es grande y lenta de cargar; inclúyela solo en checkpoints que la necesiten.
- Nombres de constantes y módulos «verificar» en `lib/NopSCADlib/vitamins/`.

## 7. Errores típicos

| Mensaje | Causa | Arreglo |
|---|---|---|
| `Can't open include file 'lib/BOSL2/std.scad'` | librería no instalada o ruta mal | clonar en `lib/`, revisar mayúsculas |
| `Ignoring unknown variable 'TOP'` | BOSL2 con `use` | cambiar a `include` |
| `Ignoring unknown module 'threaded_rod'` | falta `include <lib/BOSL2/threading.scad>` | agregar el include |
| Render muy lento con BOSL2 | roscas/redondeos con `$fn` alto | `$fa/$fs` con `$preview`, backend Manifold |
| Rosca que no enrosca | holgura | subir `$slop` 0.05 por vez, capa ≤ 0.16 |
