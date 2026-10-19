// @checkpoint parent= desc="Recorte con un plano oblicuo (punto y normal)"
// Receta de la skill openscad-stl-editing: el plano pasa por cut_point y su normal
// es cut_normal. Se queda el lado hacia el que apunta la normal.

/* [Modelo] */
model_file = "refs/part.stl";
model_color = "#9ca3af";

/* [Plano] */
cut_point = [20, 15, 10];
// No hace falta que esté normalizada (pero no puede ser [0, 0, 0])
cut_normal = [1, 0, 1];
// above = se queda el lado de la normal; below = el contrario
keep = "above";   // [above, below]

/* [Hidden] */
big = 1000;
n = cut_normal / norm(cut_normal);
// Ángulos que llevan el eje Z a la normal con rotate([rx, ry, 0]) (primero X, luego Y)
rx = -asin(n.y);
ry = atan2(n.x, n.z);
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

module model() import(model_file, convexity = 10);

// Un cubo enorme con su cara de arriba sobre el plano (side = -1: el lado contrario
// a la normal) o con su cara de abajo sobre el plano (side = 1: el lado de la normal).
module half_space(side) {
    translate(cut_point) rotate([rx, ry, 0])
        translate([-big / 2, -big / 2, side > 0 ? 0 : -big]) cube([big, big, big]);
}

// color() por fuera: la cara del corte hereda el color del modelo
color(model_color)
    intersection() {
        model();
        half_space(keep == "above" ? 1 : -1);
    }
