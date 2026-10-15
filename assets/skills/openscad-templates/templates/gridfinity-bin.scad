// @checkpoint parent= desc="Contenedor Gridfinity paramétrico con labio de apilado (plantilla)"
// Plantilla ScadLab: contenedor (bin) compatible con Gridfinity.
// Medidas del estándar: celda de 42 mm, unidad de altura de 7 mm, contenedor
// 0.5 mm más chico que la grilla (41.5 por celda), base de 4.75 mm de alto con
// perfil 0.8 (45°) + 1.8 (vertical) + 2.15 (45°). El labio de apilado (0.7 +
// 1.8 + 1.9 = 4.4 mm) va por encima de la altura nominal — «verificar» contra
// la especificación si se necesita compatibilidad exacta con otro generador.
// A1 mini (180 mm): hasta 4 x 4 celdas.

/* [Tamaño] */
grid_x = 2;          // [1:1:6]
grid_y = 1;          // [1:1:6]
// Altura en unidades de 7 mm (sin el labio)
height_units = 3;    // [2:1:20]

/* [Interior] */
wall = 1.2;          // [0.8:0.2:3]
floor_t = 1.2;       // [0.8:0.2:4]
dividers_x = 0;      // [0:1:8]
dividers_y = 0;      // [0:1:8]
divider_t = 1.2;     // [0.8:0.2:3]

/* [Opciones] */
stacking_lip = true;
// Alojamientos para imanes 6 x 2 mm en cada pie
magnets = false;
magnet_d = 6.5;      // [5:0.1:8]
magnet_h = 2.4;      // [1:0.1:4]

/* [Colores] */
bin_color = "#43A047";

/* [Hidden] */
PLATE = [180, 180, 180];   // A1 mini; ajustar según printer.json
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 1 : 0.3;

GRID = 42;                 // paso de la grilla
UNIT_H = 7;                // unidad de altura
GAP = 0.5;                 // el contenedor es 0.5 mm más chico que la grilla
OUTER_R = 3.75;            // radio exterior de las esquinas
BASE_H = 4.75;             // alto del perfil de la base
LIP_H = 4.4;               // alto del labio de apilado
MAGNET_OFFSET = 13;        // centro del imán desde el centro de la celda (42/2 - 8)

outer = [grid_x * GRID - GAP, grid_y * GRID - GAP];
bin_h = height_units * UNIT_H;
total_h = bin_h + (stacking_lip ? LIP_H : 0);
floor_z = BASE_H + floor_t;
cav = [outer.x - 2 * wall, outer.y - 2 * wall];
cav_r = max(OUTER_R - wall, 0.5);
lip_in = 2.6;              // el labio entra 0.7 + 1.9 desde la pared exterior

assert(floor_z < bin_h - 2, "height_units demasiado bajo para el piso");
assert(outer.x <= PLATE.x - 4 && outer.y <= PLATE.y - 4 && total_h <= PLATE.z,
       str("No cabe en la placa ", PLATE, ": ", [outer.x, outer.y, total_h]));

// ---------- Ensamblado ----------
color(bin_color) bin();

// ---------- Módulos ----------
module rrect_c(size, r) {
    offset(r = r) square([size.x - 2 * r, size.y - 2 * r], center = true);
}

module slab(size, r, z) {
    translate([0, 0, z]) linear_extrude(height = eps) rrect_c(size, r);
}

// Pie de una celda: 0.8 a 45°, 1.8 vertical, 2.15 a 45°
module foot() {
    w = GRID - GAP;
    hull() {
        slab([w - 2 * 2.95, w - 2 * 2.95], 0.8, 0);
        slab([w - 2 * 2.15, w - 2 * 2.15], 1.6, 0.8);
        slab([w - 2 * 2.15, w - 2 * 2.15], 1.6, 2.6);
        slab([w, w], OUTER_R, BASE_H - eps);
    }
}

function cell_center(i, j) = [(i - (grid_x - 1) / 2) * GRID, (j - (grid_y - 1) / 2) * GRID];

module bin() {
    difference() {
        union() {
            for (i = [0 : grid_x - 1], j = [0 : grid_y - 1])
                translate(cell_center(i, j)) foot();
            translate([0, 0, BASE_H - eps])
                linear_extrude(height = total_h - BASE_H + eps) rrect_c(outer, OUTER_R);
        }
        cavity();
        if (magnets)
            for (i = [0 : grid_x - 1], j = [0 : grid_y - 1], sx = [-1, 1], sy = [-1, 1])
                translate(cell_center(i, j))
                    translate([sx * MAGNET_OFFSET, sy * MAGNET_OFFSET, -eps])
                        cylinder(d = magnet_d, h = magnet_h + eps);
    }
    dividers();
}

module cavity() {
    if (stacking_lip) {
        z_lip = bin_h;
        neck = [outer.x - 2 * lip_in, outer.y - 2 * lip_in];    // parte más angosta del labio
        z_neck = z_lip - (lip_in - wall);                       // chaflán de 45° que sostiene el labio
        // interior recto
        translate([0, 0, floor_z]) linear_extrude(height = z_neck - floor_z + eps) rrect_c(cav, cav_r);
        // transición a 45° hasta el cuello del labio
        hull() {
            slab(cav, cav_r, z_neck);
            slab(neck, max(OUTER_R - lip_in, 0.5), z_lip);
        }
        // labio: 1.9 a 45°, 1.8 vertical, 0.7 a 45°
        hull() {
            slab(neck, max(OUTER_R - lip_in, 0.5), z_lip);
            slab([outer.x - 2 * 0.7, outer.y - 2 * 0.7], OUTER_R - 0.7, z_lip + 1.9);
        }
        hull() {
            slab([outer.x - 2 * 0.7, outer.y - 2 * 0.7], OUTER_R - 0.7, z_lip + 1.9);
            slab([outer.x - 2 * 0.7, outer.y - 2 * 0.7], OUTER_R - 0.7, z_lip + 3.7);
        }
        hull() {
            slab([outer.x - 2 * 0.7, outer.y - 2 * 0.7], OUTER_R - 0.7, z_lip + 3.7);
            slab([outer.x + 2 * eps, outer.y + 2 * eps], OUTER_R, total_h + eps);
        }
    } else {
        translate([0, 0, floor_z]) linear_extrude(height = bin_h) rrect_c(cav, cav_r);
    }
}

module dividers() {
    top = bin_h - 1;                                            // un poco por debajo del labio
    h = top - floor_z + eps;
    if (dividers_x > 0)
        for (k = [1 : dividers_x])
            translate([-cav.x / 2 + k * cav.x / (dividers_x + 1) - divider_t / 2, -cav.y / 2 - eps, floor_z - eps])
                cube([divider_t, cav.y + 2 * eps, h]);
    if (dividers_y > 0)
        for (k = [1 : dividers_y])
            translate([-cav.x / 2 - eps, -cav.y / 2 + k * cav.y / (dividers_y + 1) - divider_t / 2, floor_z - eps])
                cube([cav.x + 2 * eps, divider_t, h]);
}
