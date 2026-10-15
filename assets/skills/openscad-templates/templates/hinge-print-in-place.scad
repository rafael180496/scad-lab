// @checkpoint parent= desc="Bisagra print-in-place con conos de 45 grados (plantilla)"
// Plantilla ScadLab: bisagra que se imprime ya armada, sin soportes.
// Cada nudillo tiene un cono macho de 45° en su extremo +X que entra en un
// alojamiento cónico del nudillo siguiente: no hay pasador que tenga que
// puentear en el aire. Las dos hojas apoyan planas en la placa.

/* [Hojas] */
hinge_len = 50;      // [20:1:170]
leaf_w = 25;         // [8:1:80]
leaf_t = 2.4;        // [1.2:0.2:6]

/* [Nudillos] */
// Número impar de nudillos
knuckles = 5;        // [3:2:11]
knuckle_d = 8;       // [5:0.5:16]
// Holgura entre piezas móviles (0.3-0.5; >= 0.4 en A1 mini)
clearance = 0.4;     // [0.2:0.05:0.8]
// Pared que queda alrededor del alojamiento cónico
socket_wall = 1.6;   // [1:0.2:3]

/* [Tornillos] */
holes_per_leaf = 2;  // [0:1:4]
hole_d = 3.4;        // [2:0.1:6]
csink_d = 6.5;       // [0:0.5:12]

/* [Colores] */
// Mismo color en ambas hojas evita cambios de filamento en cada capa
leaf_a_color = "#4A90D9";
leaf_b_color = "#4A90D9";

/* [Hidden] */
PLATE = [180, 180, 180];   // A1 mini; ajustar según printer.json
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 1 : 0.3;

R = knuckle_d / 2;
rc = R - socket_wall;                                  // radio de la base del cono
d_ax = clearance * sqrt(2);                            // corrimiento axial = holgura normal en 45°
seg = (hinge_len - (knuckles - 1) * clearance) / knuckles;
leaf_edge = R + clearance;                             // distancia del eje al borde de la hoja

assert(knuckles % 2 == 1 && knuckles >= 3, "knuckles debe ser impar y >= 3");
assert(rc >= 1, "knuckle_d muy chico para socket_wall");
assert(seg > rc + d_ax + 1, "nudillos demasiado cortos: bajar knuckles o subir hinge_len");
assert(hinge_len <= PLATE.x - 4 && 2 * (leaf_edge + leaf_w) <= PLATE.y - 4, str("No cabe en la placa ", PLATE));

// ---------- Ensamblado ----------
color(leaf_a_color) leaf(0);   // hoja A: nudillos pares, hacia -Y
color(leaf_b_color) leaf(1);   // hoja B: nudillos impares, hacia +Y

// ---------- Módulos ----------
function seg_x0(i) = i * (seg + clearance);

// Cono macho a lo largo de +X, base de radio rc en x = 0
module male_cone() {
    rotate([0, 90, 0]) cylinder(r1 = rc, r2 = 0, h = rc);
}

// Alojamiento: el mismo cono corrido d_ax en +X y prolongado hacia atrás
module socket_cone() {
    translate([d_ax - 1, 0, 0])
        rotate([0, 90, 0]) cylinder(r1 = rc + 1, r2 = 0, h = rc + 1);
}

module knuckle(i, side) {
    x0 = seg_x0(i);
    difference() {
        hull() {
            translate([x0, 0, R]) rotate([0, 90, 0]) cylinder(r = R, h = seg);
            // nervio que une el nudillo con su hoja
            translate([x0, side * (leaf_edge + 1) - (side > 0 ? 1 : 0), 0])
                cube([seg, 1, leaf_t]);
        }
        if (i > 0) translate([x0 - clearance, 0, R]) socket_cone();
    }
    if (i < knuckles - 1)
        translate([x0 + seg - eps, 0, R]) male_cone();
}

module leaf_plate(side) {
    y0 = side > 0 ? leaf_edge : -(leaf_edge + leaf_w);
    difference() {
        translate([0, y0, 0]) cube([hinge_len, leaf_w, leaf_t]);
        if (holes_per_leaf > 0)
            for (k = [0 : holes_per_leaf - 1]) {
                hx = hinge_len * (k + 0.5) / holes_per_leaf;
                hy = y0 + leaf_w / 2 + (side > 0 ? 2 : -2);
                translate([hx, hy, -eps]) cylinder(d = hole_d, h = leaf_t + 2 * eps);
                if (csink_d > hole_d)
                    translate([hx, hy, leaf_t - (csink_d - hole_d) / 2])
                        cylinder(d1 = hole_d, d2 = csink_d, h = (csink_d - hole_d) / 2 + eps);
            }
    }
}

// parity 0 = hoja A (lado -Y), 1 = hoja B (lado +Y)
module leaf(parity) {
    side = parity == 0 ? -1 : 1;
    leaf_plate(side);
    for (i = [0 : knuckles - 1])
        if (i % 2 == parity) knuckle(i, side);
}
