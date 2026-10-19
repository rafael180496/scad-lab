// @checkpoint parent= desc="Agujero pasante vertical en un STL importado"
// Receta de la skill openscad-stl-editing: un agujero de lado a lado.
// Parte de un checkpoint que ya importa el STL; las medidas del modelo salen de
// `measure` (minMm / maxMm) o de `inspect_reference`.

/* [Modelo] */
model_file = "refs/part.stl";
// Caja del modelo YA con su pose en este checkpoint, en mm
model_min = [0, 0, 0];
model_max = [40, 30, 20];
model_color = "#9ca3af";

/* [Agujero] */
hole_x = 10;      // [-200:0.5:200]
hole_y = 10;      // [-200:0.5:200]
// Diámetro del tornillo (M3 = 3). A presión se suma la holgura por lado.
screw_d = 3;      // [1.5:0.1:20]
// Holgura por lado: 0.2 da un M3 pasante de Ø 3.4
clearance = 0.2;  // [0:0.05:0.6]

/* [Hidden] */
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

module model() import(model_file, convexity = 10);

color(model_color)
    difference() {
        model();
        // 1 mm de sobra arriba y abajo: sin caras coplanares con el modelo
        translate([hole_x, hole_y, model_min.z - 1])
            cylinder(d = screw_d + 2 * clearance, h = model_max.z - model_min.z + 2);
    }
