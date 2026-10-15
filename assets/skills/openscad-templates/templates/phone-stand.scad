// @checkpoint parent= desc="Soporte de escritorio para teléfono con ranura de cable (plantilla)"
// Plantilla ScadLab: soporte inclinado para teléfono o tablet chica.
// Se imprime ACOSTADO (el perfil apoyado en la placa, ancho en Z): todas las
// piezas del perfil quedan en el plano de las capas y no hay voladizos.

/* [Teléfono] */
// Espesor del teléfono con funda
phone_t = 12;        // [6:0.5:20]
// Ángulo del respaldo respecto de la horizontal
angle = 65;          // [45:1:80]
seat_clear = 1;      // [0:0.5:4]

/* [Soporte] */
width = 70;          // [30:1:170]
thickness = 5;       // [3:0.5:10]
lip_h = 12;          // [5:1:30]
back_len = 100;      // [40:1:170]
base_depth = 90;     // [40:1:170]
// Ancho de la ranura para el cable de carga (0 = sin ranura)
cable_slot = 12;     // [0:1:25]

/* [Acabado] */
foot_inset = 0.4;    // [0:0.1:1]

/* [Colores] */
part_color = "#8D6E63";

/* [Hidden] */
PLATE = [180, 180, 180];   // A1 mini; ajustar según printer.json
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

seat_w = phone_t / sin(angle) + seat_clear;      // ancho horizontal del asiento
x_back = thickness + seat_w;                     // pie de la cara frontal del respaldo
top_x = x_back + back_len * cos(angle) + thickness * sin(angle);
top_y = back_len * sin(angle);
brace_at = 0.6;                                  // fracción del respaldo donde apoya el puntal

assert(top_x <= base_depth, "base_depth debe cubrir la proyección del respaldo (si no, vuelca)");
assert(cable_slot == 0 || cable_slot < width - 2 * thickness, "cable_slot más ancho que el soporte");
assert(base_depth <= PLATE.x - 4 && top_y <= PLATE.y - 4 && width <= PLATE.z, str("No cabe en la placa ", PLATE));

// ---------- Ensamblado ----------
color(part_color) stand();

// ---------- Módulos ----------
module profile_2d() {
    intersection() {
        union() {
            square([base_depth, thickness]);                         // base
            square([thickness, lip_h]);                              // labio frontal
            // respaldo: la cara frontal pasa por (x_back, 0); el espesor queda detrás
            translate([x_back, 0]) rotate(angle) translate([0, -thickness]) square([back_len, thickness]);
            // puntal desde el respaldo al extremo de la base
            hull() {
                // punto en el eje del respaldo (medio espesor detrás de la cara frontal)
                translate([x_back + brace_at * back_len * cos(angle) + thickness / 2 * sin(angle),
                           brace_at * back_len * sin(angle) - thickness / 2 * cos(angle)])
                    circle(d = thickness);
                translate([base_depth - thickness, 0]) square([thickness, thickness]);
            }
        }
        translate([-1, 0]) square([base_depth + 2, top_y + thickness + 1]);   // recorta lo que cae bajo y = 0
    }
}

module foot_compensated_extrude(h, c) {
    if (c > 0) linear_extrude(height = c + eps) offset(delta = -c) children();
    translate([0, 0, c]) linear_extrude(height = h - c) children();
}

module stand() {
    difference() {
        foot_compensated_extrude(width, foot_inset) profile_2d();
        // ranura vertical bajo el asiento para el conector de carga
        if (cable_slot > 0)
            translate([thickness + seat_w / 2 - cable_slot / 2, -1, width / 2 - cable_slot / 2])
                cube([cable_slot, thickness + 2, cable_slot]);
    }
}
