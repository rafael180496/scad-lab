// @checkpoint parent= desc="Clip para cables a presión, para atornillar o pegar (plantilla)"
// Plantilla ScadLab: clip en C para uno o varios cables, sobre una base plana.
// Se imprime ACOSTADO (el perfil en C apoyado en la placa): las capas siguen
// el anillo y el clip flexiona sin partirse.

/* [Cable] */
cable_d = 6;         // [2:0.5:20]
cable_count = 1;     // [1:1:6]
// Apertura de la C respecto del diámetro del cable (< 1 para que retenga)
opening_ratio = 0.7; // [0.4:0.05:0.95]
// Juego radial entre el cable y el anillo
cable_clear = 0.3;   // [0:0.1:1]

/* [Clip] */
ring_t = 1.8;        // [1:0.2:4]
width = 10;          // [4:1:30]
base_t = 3;          // [1.6:0.2:6]

/* [Montaje] */
mount = "screw";     // [screw, adhesive]
screw_d = 3.5;       // [2:0.1:6]
head_d = 7;          // [4:0.5:12]

/* [Acabado] */
foot_inset = 0.3;    // [0:0.1:1]

/* [Colores] */
part_color = "#ECEFF1";

/* [Hidden] */
PLATE = [180, 180, 180];   // A1 mini; ajustar según printer.json
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.3;

r_in = cable_d / 2 + cable_clear;
r_out = r_in + ring_t;
gap = cable_d * opening_ratio;
pitch = 2 * r_out - ring_t;                            // anillos vecinos comparten pared
rings_len = (cable_count - 1) * pitch + 2 * r_out;
ear = mount == "screw" ? head_d + 3 : 2;
base_len = rings_len + 2 * ear;
base_y = -r_out;                                       // cara inferior de la base

assert(mount != "screw" || width >= head_d + 1, "width debe ser mayor que la cabeza del tornillo");
assert(gap < cable_d, "la apertura debe ser menor que el cable para retenerlo");
assert(base_len <= PLATE.x - 4 && width <= PLATE.z, str("No cabe en la placa ", PLATE));

// ---------- Ensamblado ----------
color(part_color) clip();

// ---------- Módulos ----------
function ring_x(i) = -rings_len / 2 + r_out + i * pitch;

module profile_2d() {
    difference() {
        union() {
            for (i = [0 : cable_count - 1]) translate([ring_x(i), 0]) circle(r = r_out);
            translate([-base_len / 2, base_y]) square([base_len, base_t]);
        }
        for (i = [0 : cable_count - 1]) {
            translate([ring_x(i), 0]) circle(r = r_in);
            translate([ring_x(i) - gap / 2, 0]) square([gap, r_out + 1]);   // apertura
        }
    }
}

module foot_compensated_extrude(h, c) {
    if (c > 0) linear_extrude(height = c + eps) offset(delta = -c) children();
    translate([0, 0, c]) linear_extrude(height = h - c) children();
}

module teardrop_2d(r) {
    union() {
        circle(r = r);
        rotate(45) square(r);
    }
}

// Agujero horizontal a lo largo de Y con la punta hacia +Z
module teardrop_hole(d, length) {
    rotate([90, 0, 0]) linear_extrude(height = length, center = true) teardrop_2d(d / 2);
}

module clip() {
    difference() {
        foot_compensated_extrude(width, foot_inset) profile_2d();
        if (mount == "screw")
            for (s = [-1, 1])
                translate([s * (base_len / 2 - ear / 2), base_y + base_t / 2, width / 2])
                    teardrop_hole(screw_d, base_t + 2);
    }
}
