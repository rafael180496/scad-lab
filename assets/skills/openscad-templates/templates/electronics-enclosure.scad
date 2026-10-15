// @checkpoint parent= desc="Gabinete para PCB con standoffs para insertos M3, ventilación y recorte USB (plantilla)"
// Plantilla ScadLab: gabinete para una placa electrónica.
// La PCB se atornilla sobre 4 standoffs con insertos térmicos M3; la tapa entra
// a presión con un labio y lleva ranuras de ventilación.

/* [PCB] */
pcb_w = 70;          // [20:0.5:150]
pcb_d = 50;          // [20:0.5:150]
pcb_t = 1.6;         // [0.8:0.1:3]
// Distancia de cada agujero de la PCB a sus dos bordes
hole_inset = 3.5;    // [2:0.5:10]
// Espacio libre alrededor de la PCB
pcb_clear = 2;       // [1:0.5:10]
// Altura del componente más alto sobre la PCB
component_h = 18;    // [3:1:80]

/* [Standoffs e insertos M3] */
standoff_h = 6;      // [3:0.5:20]
standoff_d = 8;      // [6:0.5:12]
// Ø del agujero del inserto (verificar ficha del fabricante)
insert_d = 4.0;      // [3.5:0.1:5]
// Largo del inserto + 1 mm
insert_depth = 5;    // [3:0.5:10]

/* [Caja] */
wall = 2;            // [1.2:0.2:4]
floor_t = 2;         // [1.2:0.2:4]
corner_r = 3;        // [0:0.5:8]
foot_chamfer = 0.4;  // [0:0.1:1]

/* [Puerto USB] */
usb_cutout = true;
usb_side = "front";  // [front, back, left, right]
usb_w = 12;          // [4:0.5:30]
usb_h = 7;           // [3:0.5:20]
// Desplazamiento del centro del puerto respecto del centro de la PCB, a lo largo de la pared
usb_offset = 0;      // [-60:0.5:60]
// Altura del centro del conector sobre la cara superior de la PCB
usb_center_z = 1.6;  // [0:0.1:10]

/* [Tapa] */
lid_t = 2;           // [1.2:0.2:4]
clearance = 0.2;     // [0:0.05:0.6]
lip_h = 4;           // [2:0.5:8]
lip_w = 1.4;         // [0.8:0.2:3]
vents = true;
vent_w = 2;          // [1:0.5:4]

/* [Colores] */
case_color = "#2E2E2E";
lid_color = "#2E2E2E";

/* [Exportar] */
PART = "all";        // [all, case, lid]
spacing = 8;         // [3:1:30]

/* [Hidden] */
PLATE = [180, 180, 180];   // A1 mini; ajustar según printer.json
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

inner_w = pcb_w + 2 * pcb_clear;
inner_d = pcb_d + 2 * pcb_clear;
outer_w = inner_w + 2 * wall;
outer_d = inner_d + 2 * wall;
inner_r = max(corner_r - wall, 0);
pcb_z = floor_t + standoff_h;                     // cara inferior de la PCB
case_h = pcb_z + pcb_t + component_h;
pcb_origin = [wall + pcb_clear, wall + pcb_clear];
pcb_holes = [[hole_inset, hole_inset], [pcb_w - hole_inset, hole_inset],
             [hole_inset, pcb_d - hole_inset], [pcb_w - hole_inset, pcb_d - hole_inset]];

assert(insert_depth <= standoff_h + floor_t - 0.8, "insert_depth deja el piso demasiado fino");
assert(standoff_d >= insert_d + 2.4, "standoff_d muy chico para el inserto");
assert(usb_center_z - usb_h / 2 + pcb_z + pcb_t > floor_t, "el recorte USB corta el piso");
assert(outer_w <= PLATE.x - 4 && 2 * outer_d + spacing <= PLATE.y - 4 && case_h <= PLATE.z,
       str("No cabe en la placa ", PLATE));

// ---------- Ensamblado ----------
if (PART == "all" || PART == "case")
    color(case_color) enclosure();
if (PART == "all" || PART == "lid")
    color(lid_color) translate([0, outer_d + spacing, 0]) lid();

// ---------- Módulos ----------
module rrect(size, r) {
    if (r <= 0) square(size);
    else translate([r, r]) offset(r = r) square([size.x - 2 * r, size.y - 2 * r]);
}

module chamfered_block(size, r, c) {
    if (c <= 0)
        linear_extrude(height = size.z) rrect([size.x, size.y], r);
    else
        hull() {
            translate([c, c, 0])
                linear_extrude(height = eps) rrect([size.x - 2 * c, size.y - 2 * c], max(r - c, 0));
            translate([0, 0, c])
                linear_extrude(height = size.z - c) rrect([size.x, size.y], r);
        }
}

module enclosure() {
    difference() {
        chamfered_block([outer_w, outer_d, case_h], corner_r, foot_chamfer);
        translate([wall, wall, floor_t])
            linear_extrude(height = case_h) rrect([inner_w, inner_d], inner_r);
        if (usb_cutout) usb_cut();
    }
    for (h = pcb_holes)
        translate([pcb_origin.x + h.x, pcb_origin.y + h.y, 0]) standoff();
}

module standoff() {
    difference() {
        translate([0, 0, floor_t - eps]) cylinder(d = standoff_d, h = standoff_h + eps);
        translate([0, 0, pcb_z - insert_depth]) cylinder(d = insert_d, h = insert_depth + eps);
        // chaflán de entrada del inserto
        translate([0, 0, pcb_z - 0.5]) cylinder(d1 = insert_d, d2 = insert_d + 1, h = 0.5 + eps);
    }
}

// Recorte redondeado, centrado en el origen, que atraviesa una pared a lo largo de Y
module port_cutter(w, h, depth) {
    r = min(1, w / 2 - eps, h / 2 - eps);
    rotate([90, 0, 0])
        linear_extrude(height = depth, center = true)
            offset(r = r) square([w - 2 * r, h - 2 * r], center = true);
}

module usb_cut() {
    cz = pcb_z + pcb_t + usb_center_z;
    cx = pcb_origin.x + pcb_w / 2 + usb_offset;
    cy = pcb_origin.y + pcb_d / 2 + usb_offset;
    depth = wall + 1;
    if (usb_side == "front")      translate([cx, wall / 2, cz]) port_cutter(usb_w, usb_h, depth);
    else if (usb_side == "back")  translate([cx, outer_d - wall / 2, cz]) port_cutter(usb_w, usb_h, depth);
    else if (usb_side == "left")  translate([wall / 2, cy, cz]) rotate([0, 0, 90]) port_cutter(usb_w, usb_h, depth);
    else                          translate([outer_w - wall / 2, cy, cz]) rotate([0, 0, 90]) port_cutter(usb_w, usb_h, depth);
}

// Tapa plana con labio a presión (imprimir así: labio hacia arriba)
module lid() {
    lip_o = [inner_w - 2 * clearance, inner_d - 2 * clearance];
    lip_r = max(inner_r - clearance, 0);
    difference() {
        union() {
            chamfered_block([outer_w, outer_d, lid_t], corner_r, foot_chamfer);
            translate([wall + clearance, wall + clearance, lid_t - eps])
                linear_extrude(height = lip_h + eps)
                    difference() {
                        rrect(lip_o, lip_r);
                        translate([lip_w, lip_w])
                            rrect([lip_o.x - 2 * lip_w, lip_o.y - 2 * lip_w], max(lip_r - lip_w, 0));
                    }
        }
        if (vents)
            translate([outer_w / 2, outer_d / 2, -eps])
                linear_extrude(height = lid_t + 2 * eps) vent_pattern();
    }
}

// Ranuras paralelas a X, dentro del área que deja libre el labio
module vent_pattern() {
    span_x = inner_w - 2 * (clearance + lip_w) - 6;
    span_y = inner_d - 2 * (clearance + lip_w) - 6;
    pitch = 2 * vent_w;
    n = floor((span_y + vent_w) / pitch);
    if (n > 0 && span_x > vent_w)
        for (i = [0 : n - 1])
            translate([0, (i - (n - 1) / 2) * pitch])
                hull() {
                    translate([-(span_x - vent_w) / 2, 0]) circle(d = vent_w);
                    translate([(span_x - vent_w) / 2, 0]) circle(d = vent_w);
                }
}
