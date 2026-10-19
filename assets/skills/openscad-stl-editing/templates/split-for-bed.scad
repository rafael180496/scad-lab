// @checkpoint parent= desc="Parte el modelo en dos mitades planas lado a lado para imprimirlas"
// Receta de la skill openscad-stl-editing: cortar con un plano horizontal y poner
// cada mitad con su cara de corte sobre la placa (sin soportes en la cara plana).

/* [Modelo] */
model_file = "refs/part.stl";
model_min = [0, 0, 0];
model_max = [40, 30, 20];
model_color = "#9ca3af";

/* [Corte] */
cut_z = 10;       // [-100:0.5:200]
gap = 10;         // separación entre las dos piezas, en X

/* [Hidden] */
big = 1000;
width = model_max.x - model_min.x;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

module model() import(model_file, convexity = 10);

module upper_half() intersection() {
    model();
    translate([-big / 2, -big / 2, cut_z]) cube([big, big, big]);
}

module lower_half() intersection() {
    model();
    translate([-big / 2, -big / 2, cut_z - big]) cube([big, big, big]);
}

// Mitad de arriba: su cara de corte ya es la de abajo; solo se baja hasta la placa.
color(model_color)
    translate([0, 0, -cut_z])
        upper_half();

// Mitad de abajo: su cara de corte es la de arriba, así que se da la vuelta
// (180° sobre X) y se vuelve a poner en su rango de Y y sobre la placa, a un costado.
color(model_color)
    translate([width + gap, model_min.y + model_max.y, cut_z])
        rotate([180, 0, 0])
            lower_half();
