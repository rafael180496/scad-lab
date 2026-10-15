// @checkpoint parent= desc="Escuadra de pared en L con refuerzo y agujeros avellanados (plantilla)"
// Plantilla ScadLab: escuadra en L para estantes o soportes de pared.
// Se imprime ACOSTADA (el perfil en L apoyado en la placa): así las capas
// siguen la L y la esquina no se parte. Los agujeros quedan horizontales y
// por eso tienen forma de lágrima.

/* [Dimensiones] */
arm_len = 60;        // [20:1:170]
plate_h = 60;        // [20:1:170]
width = 20;          // [8:1:60]
thickness = 5;       // [2:0.5:12]
// Cateto del refuerzo triangular (0 = sin refuerzo)
gusset = 20;         // [0:1:80]

/* [Tornillos] */
screw_d = 4.5;       // [2.5:0.1:8]
head_d = 8.5;        // [5:0.5:16]
countersink = true;
wall_holes = 2;      // [0:1:4]
arm_holes = 1;       // [0:1:4]

/* [Acabado] */
// Recorte de la primera capa contra la pata de elefante
foot_inset = 0.4;    // [0:0.1:1]

/* [Colores] */
part_color = "#607D8B";

/* [Hidden] */
PLATE = [180, 180, 180];   // A1 mini; ajustar según printer.json
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

cs_h = (head_d - screw_d) / 2;                // avellanado a 90°
wall_lo = thickness + gusset + head_d / 2 + 1;
wall_hi = plate_h - head_d / 2 - 2;
arm_lo = thickness + gusset + head_d / 2 + 1;
arm_hi = arm_len - head_d / 2 - 2;

assert(width >= head_d + 2, "width debe dejar al menos 1 mm a cada lado de la cabeza");
assert(wall_holes == 0 || wall_hi >= wall_lo, "plate_h muy corto para los agujeros de pared con este gusset");
assert(arm_holes == 0 || arm_hi >= arm_lo, "arm_len muy corto para los agujeros del brazo con este gusset");
assert(!countersink || cs_h < thickness - 0.8, "thickness muy fino para avellanar");
assert(arm_len <= PLATE.x - 4 && plate_h <= PLATE.y - 4 && width <= PLATE.z, str("No cabe en la placa ", PLATE));

// ---------- Ensamblado ----------
color(part_color) bracket();

// ---------- Módulos ----------
function spread(lo, hi, n) = n <= 1 ? [(lo + hi) / 2] : [for (i = [0 : n - 1]) lo + i * (hi - lo) / (n - 1)];

module profile_2d() {
    union() {
        square([arm_len, thickness]);                 // brazo (eje X)
        square([thickness, plate_h]);                 // placa de pared (eje Y)
        if (gusset > 0)
            translate([thickness - eps, thickness - eps])
                polygon([[0, 0], [gusset, 0], [0, gusset]]);
    }
}

// Extruye el perfil con la primera capa recortada (compensa la pata de elefante)
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

module bracket() {
    difference() {
        foot_compensated_extrude(width, foot_inset) profile_2d();
        // agujeros de la placa de pared (eje X), cabeza del lado interno
        if (wall_holes > 0)
            for (y = spread(wall_lo, wall_hi, wall_holes)) {
                translate([thickness / 2, y, width / 2])
                    rotate([0, 0, 90]) teardrop_hole(screw_d, thickness + 2);
                if (countersink)
                    translate([thickness - cs_h, y, width / 2])
                        rotate([0, 90, 0]) cylinder(h = cs_h + eps, d1 = screw_d, d2 = head_d + 2 * eps);
            }
        // agujeros del brazo (eje Y), cabeza del lado interno
        if (arm_holes > 0)
            for (x = spread(arm_lo, arm_hi, arm_holes)) {
                translate([x, thickness / 2, width / 2])
                    teardrop_hole(screw_d, thickness + 2);
                if (countersink)
                    translate([x, thickness - cs_h, width / 2])
                        rotate([-90, 0, 0]) cylinder(h = cs_h + eps, d1 = screw_d, d2 = head_d + 2 * eps);
            }
    }
}
