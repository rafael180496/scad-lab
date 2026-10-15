// @checkpoint parent= desc="Caja con tapa a presión o deslizante (plantilla box-with-lid)"
// Plantilla ScadLab: caja rectangular con esquinas redondeadas y tapa.
// Caja y tapa se imprimen una al lado de la otra, cada una apoyada en su cara
// plana. Medidas internas: lo que tiene que entrar dentro de la caja.

/* [Interior] */
inner_w = 60;        // [10:1:160]
inner_d = 40;        // [10:1:160]
inner_h = 30;        // [5:1:160]

/* [Paredes] */
wall = 2.4;          // [1.2:0.2:5]
floor_t = 1.6;       // [0.8:0.2:5]
corner_r = 2;        // [0:0.5:10]
foot_chamfer = 0.4;  // [0:0.1:1]

/* [Tapa] */
lid_type = "snap";   // [snap, slide]
lid_t = 2;           // [1.2:0.2:4]
// Holgura por lado: snap 0.1-0.2, slide 0.3-0.4
clearance = 0.2;     // [0:0.05:0.8]
lip_h = 4;           // [2:0.5:10]
lip_w = 1.6;         // [0.8:0.2:3]

/* [Colores] */
box_color = "#3A7BD5";
lid_color = "#F5A623";

/* [Exportar] */
PART = "all";        // [all, box, lid]
spacing = 8;         // [3:1:30]

/* [Hidden] */
PLATE = [180, 180, 180];   // volumen de la impresora (A1 mini); ajustar según printer.json
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

outer_w = inner_w + 2 * wall;
outer_d = inner_d + 2 * wall;
groove = wall / 2;                     // profundidad de la ranura de la tapa deslizante
slot_h = lid_t + 2 * clearance;        // alto de la ranura
rim_t = 1.2;                           // borde que queda sobre la ranura
box_h = lid_type == "slide" ? floor_t + inner_h + slot_h + rim_t : floor_t + inner_h;
inner_r = max(corner_r - wall, 0);

assert(wall >= 1.2, "wall debe ser >= 1.2 mm (2-3 perímetros)");
assert(lid_type != "slide" || wall >= 2, "la tapa deslizante necesita wall >= 2 mm");
assert(corner_r < min(outer_w, outer_d) / 2, "corner_r demasiado grande");
assert(outer_w <= PLATE.x - 4 && 2 * outer_d + spacing <= PLATE.y - 4 && box_h <= PLATE.z,
       str("No cabe en la placa ", PLATE, ": ", [outer_w, 2 * outer_d + spacing, box_h]));

// ---------- Ensamblado ----------
if (PART == "all" || PART == "box")
    color(box_color) box();
if (PART == "all" || PART == "lid")
    color(lid_color) translate([0, outer_d + spacing, 0]) lid();

// ---------- Módulos ----------
module rrect(size, r) {
    if (r <= 0) square(size);
    else translate([r, r]) offset(r = r) square([size.x - 2 * r, size.y - 2 * r]);
}

// Prisma de base redondeada con chaflán inferior anti pata de elefante
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

module box() {
    difference() {
        chamfered_block([outer_w, outer_d, box_h], corner_r, foot_chamfer);
        translate([wall, wall, floor_t])
            linear_extrude(height = box_h) rrect([inner_w, inner_d], inner_r);
        if (lid_type == "slide") slide_cuts();
    }
}

// Ranura en las paredes largas, abierta por el extremo +X
module slide_cuts() {
    z0 = floor_t + inner_h;
    translate([wall, wall - groove, z0])
        cube([outer_w - wall + eps, inner_d + 2 * groove, slot_h]);
    // abrir la pared del extremo +X por encima de la ranura
    translate([outer_w - wall - eps, wall, z0])
        cube([wall + 2 * eps, inner_d, box_h - z0 + eps]);
}

module lid() {
    if (lid_type == "slide") slide_lid();
    else snap_lid();
}

module slide_lid() {
    lw = inner_d + 2 * groove - 2 * clearance;
    ll = outer_w - wall - clearance;
    notch_r = 3;
    translate([0, (outer_d - lw) / 2, 0])
        difference() {
            cube([ll, lw, lid_t]);
            // muesca para el dedo, la mitad del espesor de la tapa
            translate([ll - 8, lw / 2, lid_t + notch_r - lid_t / 2])
                scale([1, 2.5, 1]) sphere(r = notch_r);
        }
}

// Tapa plana con labio que entra en la caja (imprimir así: labio hacia arriba)
module snap_lid() {
    lip_o = [inner_w - 2 * clearance, inner_d - 2 * clearance];
    lip_r = max(inner_r - clearance, 0);
    chamfered_block([outer_w, outer_d, lid_t], corner_r, foot_chamfer);
    translate([wall + clearance, wall + clearance, lid_t - eps])
        linear_extrude(height = lip_h + eps)
            difference() {
                rrect(lip_o, lip_r);
                translate([lip_w, lip_w])
                    rrect([lip_o.x - 2 * lip_w, lip_o.y - 2 * lip_w], max(lip_r - lip_w, 0));
            }
}
