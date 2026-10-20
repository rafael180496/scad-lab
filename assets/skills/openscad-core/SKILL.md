---
name: openscad-core
description: Reglas base para escribir o modificar cualquier archivo .scad en un workspace de ScadLab (convención de checkpoints inmutables, color obligatorio, parámetros de Customizer, CSG limpio, rendimiento y validación). Úsala SIEMPRE antes de crear un checkpoint nuevo o de diagnosticar un error o warning de OpenSCAD.
---

# OpenSCAD — reglas de modelado en ScadLab

Esta skill es la base de todas las demás. Si una regla de otra skill parece
contradecir esta, gana esta (es el contrato con la app).

## 1. Checkpoints inmutables (LO MÁS IMPORTANTE)

ScadLab reconstruye el historial y las ramas del modelo leyendo los archivos
`.scad` del workspace. Por eso:

- **Cada versión del modelo es un archivo `.scad` NUEVO.** Nunca sobrescribas,
  edites en el lugar, renombres ni borres un `.scad` existente — aunque tenga un
  error. El error se corrige en un checkpoint nuevo.
- **Nombre**: `NNN-slug.scad`. `NNN` = número de 3 dígitos, **siguiente al mayor
  que exista en el workspace** (aunque ramifiques desde uno viejo). `slug` en
  kebab-case ASCII, corto y en inglés: `001-base-box.scad`,
  `002-rounded-corners.scad`, `003-add-hinge.scad`.
- **Primera línea obligatoria** (exactamente este formato, comillas dobles):

  ```openscad
  // @checkpoint parent=002-base-box.scad desc="Agrega bisagra lateral"
  ```

  En el primer checkpoint, `parent=` va vacío:

  ```openscad
  // @checkpoint parent= desc="Caja base 60x40x25"
  ```

- **Ramificar** = crear un archivo nuevo cuyo `parent` es un checkpoint anterior
  (no el último). Ej.: existen `001`…`005`; el usuario quiere probar otra idea a
  partir de `003` → crea `006-alt-snap-lid.scad` con `parent=003-….scad`.
- **Cada checkpoint es autocontenido**: copia el código completo del padre y
  modifícalo. No hagas `include`/`use` de otro checkpoint (la app renderiza cada
  archivo por separado y un cambio de dependencia rompería la inmutabilidad).
  Las librerías externas van en `lib/` (ver skill `openscad-libraries`).
- `desc` describe **qué cambia respecto del padre**, en español, en una frase.
  No uses comillas dobles dentro de `desc`.

## 2. Estructura de un archivo

```openscad
// @checkpoint parent=001-base-box.scad desc="Redondea las esquinas verticales"
// Proyecto: caja para sensores — ScadLab

/* [Dimensiones] */
width  = 60;    // [20:1:180]
depth  = 40;    // [20:1:180]
height = 25;    // [5:0.5:150]
wall   = 2;     // [1.2:0.2:5]

/* [Acabado] */
corner_radius = 4;   // [0:0.5:15]
body_color = "SteelBlue"; // [SteelBlue, Orange, White, Black]

/* [Hidden] */
eps = 0.01;                        // solape anti-coplanar
$fa = $preview ? 12 : 4;           // calidad de curvas
$fs = $preview ? 2  : 0.4;

// ---------- Ensamblado (piezas de primer nivel, cada una con color) ----------
color(body_color) body();

// ---------- Módulos ----------
module body() {
    difference() {
        rounded_box([width, depth, height], corner_radius);
        translate([wall, wall, wall])
            rounded_box([width - 2*wall, depth - 2*wall, height], max(corner_radius - wall, 0.01));
    }
}

module rounded_box(size, r) {
    hull()
        for (x = [r, size.x - r], y = [r, size.y - r])
            translate([x, y, 0]) cylinder(r = r, h = size.z);
}
```

Orden fijo: metadatos → parámetros (con secciones) → `[Hidden]` (constantes,
`$fa/$fs`, epsilon) → **ensamblado** → módulos → funciones.

## 3. Reglas obligatorias

- **Color obligatorio**: toda pieza de primer nivel va dentro de
  `color("nombre")` o `color("#RRGGBB")`. La app colorea la vista y exporta 3MF
  por color. Dos piezas del mismo color = mismo filamento. Ver
  `openscad-3mf-multicolor`.
- **Milímetros** siempre. Nada de pulgadas sin convertir (`25.4`).
- **Parámetros arriba** con comentario de Customizer:
  - `// [min:max]` o `// [min:step:max]` → slider.
  - `// [a, b, c]` → desplegable; `// [10:Chico, 20:Grande]` → valor:etiqueta.
  - `/* [Sección] */` agrupa; `/* [Hidden] */` oculta lo que sigue.
  - Solo aparecen en el Customizer las variables con **valor literal** asignadas
    antes del primer `module`/`function`. Lo derivado (`inner = width - 2*wall;`)
    va en `[Hidden]`.
- **Ubicación**: el modelo apoya en `z = 0` (cara de impresión sobre la placa) y
  centrado aproximadamente en el origen en XY.
- **Imprimible**: si el workspace tiene `printer.json`, respeta su volumen y su
  máximo de colores (`colors`). La skill de marca de esa impresora
  (`bambu-lab-printers`, `prusa-printers`, `creality-printers`, `elegoo-printers`,
  `anycubic-printers`, `snapmaker-printers` o `ultimaker-printers`) trae su
  `assert_fits_plate()`, sus reglas FDM y qué formato exportar.

## 4. Resolución de curvas: `$fn`, `$fa`, `$fs`

| Variable | Significado | Preview | Render final |
|---|---|---|---|
| `$fa` | ángulo máximo por segmento (°) | 12 | 3–5 |
| `$fs` | largo mínimo de segmento (mm) | 2 | 0.3–0.5 |
| `$fn` | segmentos fijos (anula `$fa/$fs`) | 0 | 0 salvo casos puntuales |

- Usa `$fa/$fs` globales con `$preview` (ver plantilla de arriba). Así los
  círculos chicos no tienen 100 lados y los grandes quedan lisos.
- `$fn` solo local y con motivo: hexágonos (`$fn=6` para una tuerca), agujeros
  que deben coincidir exactamente entre dos piezas.
- `$fn = 200` global es el error de rendimiento más común.

## 5. CSG limpio

- **Caras coplanares**: en `difference()`, la herramienta de corte debe
  sobresalir `eps = 0.01` (o más) por ambos lados. Si no, aparecen "pieles"
  parpadeantes en preview y caras basura en el STL.

  ```openscad
  difference() {
      cube([20, 20, 5]);
      translate([10, 10, -eps]) cylinder(d = 3.4, h = 5 + 2*eps);
  }
  ```

- En `union()` de piezas que se tocan, solápalas `eps` en lugar de dejarlas cara
  a cara (evita "Object may not be a valid 2-manifold").
- **`hull()`**: barato y muy útil para redondeos (4 cilindros/esferas) y
  transiciones. Prefiérelo a `minkowski()`.
- **`minkowski()`**: costoso (crece con el producto de vértices). Úsalo solo con
  un objeto simple y una esfera de `$fn` bajo, o redondea con `hull()` /
  `offset()` en 2D antes de extruir.
- **2D primero**: perfiles con `offset(r=…)`/`offset(delta=…, chamfer=true)` y
  luego `linear_extrude()` o `rotate_extrude()`. Es más rápido y limpio que CSG 3D.
- `render()` alrededor de un subárbol complejo que se repite lo cachea en preview.

## 6. Lenguaje: lo esencial

- Variables son **inmutables por ámbito**: la última asignación en un ámbito gana
  para todo el ámbito (warning "was assigned on line N but was overwritten").
  No "reasignes" en bucles; usa `let`, funciones o list comprehensions.
- Módulos con `children()` para operadores propios:

  ```openscad
  module mirror_copy(v = [1, 0, 0]) { children(); mirror(v) children(); }
  ```

- Funciones puras + `let` + list comprehensions para geometría calculada
  (`polygon(points=[for (i=[0:n-1]) …])`).
- `assert(cond, "mensaje")` para validar parámetros al inicio de un módulo.
- Texto: `text(t, size, font="Liberation Sans:style=Bold", halign="center",
  valign="center")` dentro de `linear_extrude`. Liberation Sans viene con
  OpenSCAD; otra fuente se carga con `use <fonts/Archivo.ttf>`.

Referencia rápida de sintaxis, primitivas y patrones:
[references/cheatsheet.md](references/cheatsheet.md).

## 7. Validar SIEMPRE cada checkpoint

1. Renderiza con la herramienta MCP `render` del servidor `scadlab` (y mira el resultado con `preview`); si el MCP no está, usa `openscad -o exports/check.stl <archivo>`.
2. Si no, por línea de comandos:

   ```bash
   openscad -o /tmp/out.stl 004-snap-lid.scad 2>&1 | tee /tmp/render.log
   # OpenSCAD 2025+/snapshots: backend Manifold, 10–100× más rápido
   openscad --backend=manifold -o /tmp/out.stl 004-snap-lid.scad
   ```

   (`--backend=manifold` en snapshots recientes; en snapshots de 2023–2024 era
   `--enable=manifold` — **verificar** con `openscad --help`.)
3. Revisa **todo** `WARNING:` y `ERROR:`. Un render sin errores pero con
   warnings no está terminado.
4. Si la app dejó `RENDER_ERRORS.md` en el workspace, léelo y corrige en un
   **checkpoint nuevo** (parent = el que falló).
5. Para probar variantes de parámetros sin crear archivos:
   `openscad -D 'width=80' -o /tmp/out.stl archivo.scad`.

## 8. Errores y warnings frecuentes

| Mensaje | Causa típica | Arreglo |
|---|---|---|
| `Ignoring unknown variable 'x'` | typo o variable definida dentro de otro ámbito | revisar nombre/ámbito |
| `Ignoring unknown module 'm'` | módulo no definido o `use` faltante | definirlo o `use <lib/...>` |
| `undefined operation (number + undefined)` | variable `undef` en una cuenta | definirla / `assert` |
| `was assigned on line N but was overwritten` | reasignación en el mismo ámbito | renombrar o usar `let` |
| `Object may not be a valid 2-manifold` | caras coplanares, piezas que solo se tocan en una arista | solape `eps`, separar o unir con volumen |
| `Normalized tree is growing past N elements` | `for` enorme dentro de `difference`/`intersection` | unir antes (`union()` explícito) o simplificar |
| `Unable to convert ... to a number` | parámetro con tipo incorrecto | revisar llamada |
| `Assertion '...' failed` | un `assert` propio | el mensaje dice qué parámetro corregir |
| `Current top level object is empty` | todo restado / `difference` sin primer hijo | revisar tamaños y el primer hijo |
| preview OK, render lento horas | `minkowski`, `$fn` alto, muchos booleanos | Manifold, `hull`, 2D + extrude |

Lee el **número de línea** del mensaje y revisa también la línea anterior (los
`;` faltantes se reportan en la siguiente).

## 9. Rendimiento

- Usa el backend **Manifold** (OpenSCAD 2025+/snapshots). CGAL es lento con
  muchos booleanos.
- `$fa/$fs` con `$preview` (sección 4).
- Patrones repetidos (rejillas de ventilación, textos): constrúyelos en 2D y
  extruye una sola vez.
- Evita `minkowski` sobre geometría con agujeros; aplícalo antes de restar.
- Una pieza grande con mil agujeros: `difference() { base(); union() { for … } }`.

## 10. Checklist antes de entregar un checkpoint

- [ ] Archivo nuevo `NNN-slug.scad`, número siguiente al mayor, nada sobrescrito.
- [ ] Línea 1 `// @checkpoint parent=… desc="…"` correcta.
- [ ] Parámetros arriba con Customizer; derivados en `[Hidden]`.
- [ ] Toda pieza de primer nivel en `color()`.
- [ ] `eps` en todos los cortes; sin caras coplanares.
- [ ] Apoya en z=0, cabe en la placa (`printer.json`).
- [ ] Renderizado sin `ERROR` ni `WARNING`.
- [ ] Reglas FDM revisadas (skill de marca de la impresora y `references/fdm-design-rules.md`).
