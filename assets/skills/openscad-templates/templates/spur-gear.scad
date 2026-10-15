// @checkpoint parent= desc="Engranaje recto de involuta, sin librerías (plantilla spur-gear)"
// Plantilla ScadLab: engranaje recto con perfil de involuta calculado aquí.
// No modela el socavado de pie: usar 17+ dientes con ángulo de presión 20°.
// Dos engranajes engranan si comparten módulo y ángulo de presión; distancia
// entre centros = gear_module * (z1 + z2) / 2.

/* [Engranaje] */
gear_module = 1.5;   // [0.5:0.25:5]
teeth = 20;          // [8:1:120]
thickness = 6;       // [2:0.5:30]
pressure_angle = 20; // [14.5, 20, 25]
// Juego entre dientes (reduce el espesor del diente en la circunferencia primitiva)
backlash = 0.15;     // [0:0.05:0.5]

/* [Eje y cubo] */
bore_d = 5.2;        // [0:0.1:30]
hub_d = 12;          // [0:0.5:40]
hub_h = 4;           // [0:0.5:20]

/* [Vista previa] */
// Muestra un engranaje compañero engranado (solo para comprobar)
show_mate = false;
mate_teeth = 12;     // [8:1:120]

/* [Colores] */
gear_color = "#FF7043";
mate_color = "#78909C";

/* [Hidden] */
PLATE = [180, 180, 180];   // A1 mini; ajustar según printer.json
eps = 0.01;
$fa = $preview ? 12 : 3;
$fs = $preview ? 1 : 0.25;

function pitch_r(z) = gear_module * z / 2;
function outer_r(z) = pitch_r(z) + gear_module;

assert(teeth >= 8, "teeth >= 8");
assert(hub_d == 0 || hub_d > bore_d + 2, "hub_d debe superar bore_d + 2");
assert(2 * outer_r(teeth) <= min(PLATE.x, PLATE.y) - 4, str("No cabe en la placa ", PLATE));

// ---------- Ensamblado ----------
color(gear_color) spur_gear(teeth, thickness, bore_d, hub_d, hub_h);
if (show_mate && $preview)
    color(mate_color)
        translate([gear_module * (teeth + mate_teeth) / 2, 0, 0])
            rotate([0, 0, 180 + 180 / mate_teeth])
                spur_gear(mate_teeth, thickness, 0, 0, 0);

// ---------- Involuta ----------
// t: parámetro de la involuta (radianes). Ángulos de OpenSCAD en grados.
function inv_t(r, rb) = sqrt(max((r / rb) * (r / rb) - 1, 0));
function inv_polar_deg(t) = t * 180 / PI - atan(t);          // ángulo polar del punto
function polar(r, a) = [r * cos(a), r * sin(a)];

function flank_points(rb, r0, r1, rot, steps) =
    let(t0 = inv_t(r0, rb), t1 = inv_t(r1, rb))
    [for (i = [0 : steps])
        let(t = t0 + (t1 - t0) * i / steps)
        polar(rb * sqrt(1 + t * t), rot + inv_polar_deg(t))];

// Diente centrado en +X
module tooth_2d(z) {
    rp = pitch_r(z);
    rb = rp * cos(pressure_angle);
    ra = outer_r(z);
    rf = rp - 1.25 * gear_module;
    half_deg = 90 / z - (backlash / 2) / rp * 180 / PI;          // medio espesor angular en rp
    inv_p = tan(pressure_angle) * 180 / PI - pressure_angle;      // involuta en rp (grados)
    rot_low = -(half_deg + inv_p);
    r_start = max(rb, rf);
    lower = flank_points(rb, r_start, ra, rot_low, 10);
    upper = [for (i = [len(lower) - 1 : -1 : 0]) [lower[i].x, -lower[i].y]];
    a0 = atan2(lower[0].y, lower[0].x);
    r_root = max(rf - 0.5, 0.1);
    polygon(concat([polar(r_root, a0)], lower, upper, [polar(r_root, -a0)]));
}

module gear_2d(z) {
    rf = pitch_r(z) - 1.25 * gear_module;
    union() {
        circle(r = rf);
        for (i = [0 : z - 1]) rotate(i * 360 / z) tooth_2d(z);
    }
}

module spur_gear(z, h, bore, hub, hub_height) {
    difference() {
        union() {
            linear_extrude(height = h) gear_2d(z);
            if (hub > 0 && hub_height > 0)
                translate([0, 0, h - eps]) cylinder(d = hub, h = hub_height + eps);
        }
        if (bore > 0)
            translate([0, 0, -eps]) cylinder(d = bore, h = h + hub_height + 2 * eps);
    }
}
