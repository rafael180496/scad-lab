# OpenSCAD — cheatsheet

Referencia rápida para la skill `openscad-core`. Todo en milímetros.

## Primitivas 3D

```openscad
cube([x, y, z], center = false);
cube(10, center = true);
sphere(r = 5);                 // o d = 10
cylinder(h = 10, r = 3);       // o d = 6
cylinder(h = 10, r1 = 5, r2 = 2);   // cono / tronco (d1, d2)
polyhedron(points = [...], faces = [[...], ...]);   // caras en sentido horario vistas desde fuera
```

## Primitivas 2D

```openscad
square([x, y], center = false);
circle(r = 5);                 // o d
polygon(points = [[0,0], [10,0], [5,8]]);
polygon(points = pts, paths = [outer_idx, hole_idx]);
text("Hola", size = 8, font = "Liberation Sans:style=Bold",
     halign = "center", valign = "center", spacing = 1);
import("perfil.svg");          // 2D desde SVG/DXF
```

`size` de `text()` ≈ altura de las mayúsculas en mm (aprox.; la altura real
depende de la fuente).

## Transformaciones

```openscad
translate([x, y, z]) ...
rotate([ax, ay, az]) ...       // grados, orden X luego Y luego Z
rotate(a = 30, v = [0, 0, 1]) ...
scale([sx, sy, sz]) ...
resize([x, y, z], auto = true) ...
mirror([1, 0, 0]) ...          // plano normal al vector
multmatrix(m) ...
color("Tomato") ...            // nombre CSS
color("#FF8800") ...           // hex (también "#RGB", "#RRGGBBAA")
color([1, 0.5, 0], alpha = 1) ...
offset(r = 2) ...              // 2D, redondea
offset(delta = 2, chamfer = true) ...   // 2D, esquinas vivas/chaflán
```

## Booleanos y combinadores

```openscad
union() { a(); b(); }
difference() { base(); cut1(); cut2(); }   // el 1.er hijo menos el resto
intersection() { a(); b(); }
hull() { ... }                 // envolvente convexa (barata)
minkowski() { a(); sphere(1); }   // suma de Minkowski (cara)
render(convexity = 4) ...      // fuerza/cachea CGAL en preview
projection(cut = false) ...    // 3D -> 2D
```

## Extrusiones

```openscad
linear_extrude(height = 5, center = false, twist = 0, slices = 20, scale = 1)
    square(10);
rotate_extrude(angle = 360, convexity = 4)
    translate([10, 0]) circle(r = 2);   // perfil en X>0 del plano XY -> toro
```

El perfil de `rotate_extrude` debe estar entero en X ≥ 0.

## Control de flujo

```openscad
for (i = [0 : n - 1]) translate([i * pitch, 0, 0]) child();
for (i = [0 : 2 : 10]) ...        // inicio : paso : fin (fin incluido)
for (p = [[0,0], [10,5]]) translate(p) ...
for (x = [0, 10], y = [0, 10]) ... // producto cartesiano
intersection_for (i = [0:2]) rotate([0, 0, i * 60]) cube([20, 5, 5], center = true);
if (with_lid) lid(); else ...;
x = cond ? a : b;
```

## Funciones, let, comprehensions

```openscad
function clamp(v, lo, hi) = min(max(v, lo), hi);
function circle_pts(r, n) = [for (i = [0 : n - 1]) [r * cos(360 * i / n), r * sin(360 * i / n)]];
function sum(v, i = 0) = i >= len(v) ? 0 : v[i] + sum(v, i + 1);   // recursión

evens = [for (i = [0 : 20]) if (i % 2 == 0) i];
flat  = [for (row = m) each row];
pts   = [for (i = [0 : n]) let(a = i * 360 / n) [cos(a), sin(a)]];
module m() let(r = d / 2) cylinder(r = r, h = 2);    // let en módulo (2019.05+)

f = function(x) x * 2;          // literal de función (2021.01+)
echo(f(3));
```

Funciones útiles: `len`, `concat`, `lookup`, `search`, `max`, `min`, `norm`,
`cross`, `abs`, `ceil`, `floor`, `round`, `sqrt`, `pow`, `sin/cos/tan` (grados),
`atan2(y, x)`, `str`, `chr`, `ord`, `is_undef`, `is_num`, `is_list`,
`is_string`, `is_function`.

## Módulos y children

```openscad
module grid(nx, ny, pitch) {
    for (i = [0 : nx - 1], j = [0 : ny - 1])
        translate([i * pitch, j * pitch, 0]) children();
}
grid(3, 2, 10) cylinder(d = 3, h = 2);

module each_child_spaced(d) {
    for (i = [0 : $children - 1]) translate([i * d, 0, 0]) children(i);
}
```

Variables especiales: `$children`, `$preview`, `$t` (animación), `$fn`, `$fa`,
`$fs`, `$vpr/$vpt/$vpd` (cámara). Las variables `$x` son de ámbito dinámico: se
heredan hacia los módulos llamados.

## Modificadores de depuración (quitar antes del checkpoint final)

| Prefijo | Efecto |
|---|---|
| `*` | desactiva el subárbol |
| `!` | renderiza solo este subárbol |
| `#` | resalta (se dibuja, aunque sea un corte) |
| `%` | transparente, no forma parte del render |

## Diagnóstico

```openscad
echo("ancho interno", width - 2 * wall);
assert(wall >= 0.8, str("wall=", wall, " es menor a 2 perímetros de 0.4"));
```

## CLI

```bash
openscad -o out.stl modelo.scad                    # STL
openscad -o out.3mf modelo.scad                    # 3MF (colores: ver skill 3mf)
openscad -o out.png --imgsize=800,600 modelo.scad  # captura
openscad -D 'width=80' -D 'label="ABC"' -o out.stl modelo.scad
openscad -p params.json -P preset1 -o out.stl modelo.scad   # set del Customizer
openscad --backend=manifold -o out.stl modelo.scad # 2025+/snapshots (verificar flag)
openscad --hardwarnings -o out.stl modelo.scad     # falla en el primer warning
```

Strings dentro de `-D` llevan comillas dobles escapadas por la shell.

## Valores típicos

| Uso | Valor |
|---|---|
| epsilon de corte | `0.01` |
| agujero pasante M3 | Ø 3.2–3.4 |
| agujero M3 roscado directo en plástico | Ø 2.5–2.8 |
| cabeza M3 allen (DIN 912) | Ø 5.5 → alojamiento Ø 6.0–6.2 |
| tuerca M3 entre caras | 5.5 → alojamiento 5.8–6.0 (`$fn = 6`, `d = ef / cos(30)`) |
| inserto térmico M3 | ver skill `bambu-lab-printers` |
