// @checkpoint parent= desc="Perilla estriada para eje D de 6 mm (plantilla knob)"
// Plantilla ScadLab: perilla para potenciómetro/encoder con eje D de 6 mm.
// Se imprime BOCA ABAJO: la cara frontal (con la marca) apoya en la placa y el
// agujero del eje queda abierto hacia arriba, sin voladizos.

/* [Perilla] */
knob_d = 30;         // [10:1:80]
knob_h = 16;         // [6:0.5:40]
flutes = 18;         // [0:1:48]
flute_d = 3;         // [1:0.5:8]
edge_chamfer = 1;    // [0:0.2:3]
// Profundidad de la marca indicadora en la cara frontal (0 = sin marca)
pointer_depth = 0.6; // [0:0.2:2]
pointer_w = 1.6;     // [0.8:0.2:4]

/* [Eje D] */
shaft_d = 6;         // [3:0.1:10]
// Medida a través del plano (eje D estándar de 6 mm: 4.5)
shaft_flat = 4.5;    // [2:0.1:10]
// Holgura sobre el eje (0.1-0.2 a presión)
shaft_clear = 0.15;  // [0:0.05:0.5]
shaft_depth = 12;    // [4:0.5:35]
// Agujero radial para prisionero M3 autorroscante (Ø 2.8) contra el plano
set_screw = false;

/* [Colores] */
knob_color = "#212121";

/* [Hidden] */
PLATE = [180, 180, 180];   // A1 mini; ajustar según printer.json
eps = 0.01;
$fa = $preview ? 12 : 3;
$fs = $preview ? 1 : 0.3;

hole_d = shaft_d + shaft_clear;
hole_flat = shaft_flat + shaft_clear;

assert(shaft_depth <= knob_h - 1.2, "shaft_depth debe dejar al menos 1.2 mm de frente");
assert(shaft_flat < shaft_d, "shaft_flat debe ser menor que shaft_d");
assert(knob_d >= hole_d + 6, "knob_d muy chico para el eje");
assert(knob_d <= PLATE.x - 4 && knob_h <= PLATE.z, str("No cabe en la placa ", PLATE));

// ---------- Ensamblado ----------
color(knob_color) knob();

// ---------- Módulos ----------
// Perfil D: círculo recortado por un plano del lado +Y
module d_profile(d, flat) {
    intersection() {
        circle(d = d);
        translate([-d / 2, -d / 2]) square([d, flat]);
    }
}

module teardrop_2d(r) {
    union() {
        circle(r = r);
        rotate(45) square(r);
    }
}

module teardrop_hole(d, length) {
    rotate([90, 0, 0]) linear_extrude(height = length, center = true) teardrop_2d(d / 2);
}

module knob_body() {
    c = edge_chamfer;
    hull() {
        cylinder(d1 = knob_d - 2 * c, d2 = knob_d, h = c + eps);                 // chaflán frontal (sobre la placa)
        translate([0, 0, c]) cylinder(d = knob_d, h = knob_h - 2 * c);
        translate([0, 0, knob_h - c - eps]) cylinder(d1 = knob_d, d2 = knob_d - 2 * c, h = c + eps);
    }
}

module knob() {
    difference() {
        knob_body();
        // estrías de agarre
        if (flutes > 0)
            for (i = [0 : flutes - 1])
                rotate([0, 0, i * 360 / flutes])
                    translate([knob_d / 2 + flute_d / 6, 0, -eps])
                        cylinder(d = flute_d, h = knob_h + 2 * eps);
        // agujero del eje (abierto hacia arriba)
        translate([0, 0, knob_h - shaft_depth])
            linear_extrude(height = shaft_depth + eps) d_profile(hole_d, hole_flat);
        // marca indicadora en la cara frontal, alineada con el plano del eje (+Y)
        if (pointer_depth > 0)
            translate([-pointer_w / 2, hole_d / 2 + 1, -eps])
                cube([pointer_w, knob_d / 2 - hole_d / 2 - 1 - edge_chamfer - 1, pointer_depth + eps]);
        // prisionero radial que apunta al plano
        if (set_screw)
            translate([0, knob_d / 4, knob_h - shaft_depth / 2])
                teardrop_hole(2.8, knob_d / 2 + 2);
    }
}
