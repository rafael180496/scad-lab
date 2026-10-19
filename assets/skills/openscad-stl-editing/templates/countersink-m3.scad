// @checkpoint parent= desc="Agujero M3 con avellanado o con caja para la cabeza"
// Receta de la skill openscad-stl-editing: tornillo M3 que entra por la cara de
// ARRIBA, con la cabeza hundida (cónica) o en una caja cilíndrica.

/* [Modelo] */
model_file = "refs/part.stl";
model_min = [0, 0, 0];
model_max = [40, 30, 20];
model_color = "#9ca3af";

/* [Tornillo] */
screw_x = 20;       // [-200:0.5:200]
screw_y = 15;       // [-200:0.5:200]
screw_d = 3;        // [2:0.5:8]
clearance = 0.2;    // [0:0.05:0.6]
// Diámetro de la cabeza con holgura (M3 avellanado: 6 + 0.6)
head_d = 6.6;       // [4:0.1:14]
head_style = "countersunk";   // [countersunk, counterbore]
// Solo counterbore: profundidad de la caja (M3 cilíndrica: 3 + 0.4)
head_depth = 3.4;   // [1:0.1:10]

/* [Hidden] */
eps = 0.01;
top_z = model_max.z;
shaft_d = screw_d + 2 * clearance;
// Cono de 90°: el radio crece lo mismo que baja
cone_h = (head_d - shaft_d) / 2;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

module model() import(model_file, convexity = 10);

color(model_color)
    difference() {
        model();
        // Caña, de lado a lado
        translate([screw_x, screw_y, model_min.z - 1])
            cylinder(d = shaft_d, h = model_max.z - model_min.z + 2);
        if (head_style == "countersunk")
            // El cono sigue 1 mm por encima de la cara para no dejar una piel coplanar
            translate([screw_x, screw_y, top_z - cone_h])
                cylinder(d1 = shaft_d, d2 = head_d + 2, h = cone_h + 1);
        else
            translate([screw_x, screw_y, top_z - head_depth])
                cylinder(d = head_d, h = head_depth + 1);
    }
