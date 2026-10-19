// @checkpoint parent= desc="Recorte con un plano perpendicular a un eje"
// Receta de la skill openscad-stl-editing: se queda con un lado de un plano
// x = c, y = c o z = c. Para un plano oblicuo, rota el semiespacio (ver la skill).

/* [Modelo] */
model_file = "refs/part.stl";
model_color = "#9ca3af";

/* [Corte] */
cut_axis = "z";   // [x, y, z]
cut_at = 12;      // [-200:0.5:200]
// low = se queda el lado de los valores menores; high = el de los mayores
keep = "high";    // [low, high]

/* [Hidden] */
big = 1000;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

module model() import(model_file, convexity = 10);

// Un cubo enorme con una cara sobre el plano. side = 1 cubre el lado de los
// valores mayores del eje; side = -1, el de los menores.
module half_space(side) {
    at = cut_axis == "x" ? [cut_at, 0, 0] : cut_axis == "y" ? [0, cut_at, 0] : [0, 0, cut_at];
    turn = cut_axis == "x" ? [0, 90, 0] : cut_axis == "y" ? [-90, 0, 0] : [0, 0, 0];
    translate(at) rotate(turn)
        translate([-big / 2, -big / 2, side > 0 ? 0 : -big]) cube([big, big, big]);
}

// El color() va por fuera: la cara nueva que deja el corte toma el color de
// afuera (un cortador sin color la deja en el amarillo-verde por defecto, un
// color de más para el 3MF multicolor).
color(model_color)
    intersection() {
        model();
        half_space(keep == "high" ? 1 : -1);
    }
