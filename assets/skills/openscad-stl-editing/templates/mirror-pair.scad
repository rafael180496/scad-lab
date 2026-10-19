// @checkpoint parent= desc="Par izquierda y derecha: el modelo y su espejo"
// Receta de la skill openscad-stl-editing: una pieza simétrica de la original
// (el espejo de una pieza no es la misma pieza, no se obtiene girándola).

/* [Modelo] */
model_file = "refs/part.stl";
model_min = [0, 0, 0];
model_max = [40, 30, 20];
model_color = "#9ca3af";

/* [Par] */
gap = 10;     // separación entre las dos piezas, en X
// Eje del espejo: x = izquierda/derecha, y = delante/detrás
mirror_axis = "x";   // [x, y]

/* [Hidden] */
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

module model() import(model_file, convexity = 10);

// La original, donde está
color(model_color) model();

// El espejo: mirror() lo da vuelta respecto del plano que pasa por el origen y
// la traslación lo deja a un costado de la original, separado por `gap`.
color(model_color) {
    if (mirror_axis == "x")
        translate([2 * model_max.x + gap, 0, 0]) mirror([1, 0, 0]) model();
    else
        translate([0, 2 * model_max.y + gap, 0]) mirror([0, 1, 0]) model();
}
