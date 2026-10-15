// @checkpoint parent= desc="Placa con nombre en dos colores para AMS (plantilla)"
// Plantilla ScadLab: placa base de un color con texto y borde de otro color.
// Modo "raised": texto en relieve (pocas capas: cambio de color barato).
// Modo "inlay": texto incrustado al ras (superficie lisa).
// Las dos piezas nunca se solapan; PART permite exportar cada color por separado.

/* [Texto] */
label = "ScadLab";
font = "Liberation Sans:style=Bold";
text_size = 12;      // [4:0.5:40]
// Relieve o incrustación; múltiplo de la altura de capa (0.2 -> 0.4, 0.6...)
text_h = 0.6;        // [0.2:0.2:3]
mode = "raised";     // [raised, inlay]

/* [Placa] */
plate_w = 110;       // [30:1:170]
plate_d = 32;        // [10:1:170]
plate_h = 3;         // [1.2:0.2:10]
corner_r = 4;        // [0:0.5:15]
// Ancho del borde decorativo del color del texto (0 = sin borde)
border_w = 1.6;      // [0:0.2:5]
border_inset = 2;    // [0.8:0.2:8]
// Agujeros para colgar en los extremos (0 = sin agujeros)
hole_d = 0;          // [0:0.5:8]
foot_chamfer = 0.4;  // [0:0.1:1]

/* [Colores] */
base_color = "#FFFFFF";
text_color = "#D32F2F";

/* [Exportar] */
PART = "all";        // [all, base, text]

/* [Hidden] */
PLATE = [180, 180, 180];   // A1 mini; ajustar según printer.json
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 1 : 0.25;

assert(text_size >= 4, "text_size >= 4 mm para que sea legible");
assert(mode != "inlay" || text_h <= plate_h - 0.8, "la incrustación deja el fondo demasiado fino");
assert(plate_w <= PLATE.x - 4 && plate_d <= PLATE.y - 4, str("No cabe en la placa ", PLATE));

// ---------- Ensamblado ----------
if (PART == "all" || PART == "base")
    color(base_color) base();
if (PART == "all" || PART == "text")
    color(text_color) decor();

// ---------- Módulos ----------
module rrect_c(size, r) {
    if (r <= 0) square(size, center = true);
    else offset(r = r) square([size.x - 2 * r, size.y - 2 * r], center = true);
}

module hole_positions() {
    for (s = [-1, 1]) translate([s * (plate_w / 2 - border_inset - border_w - hole_d), 0]) children();
}

// Todo lo que va en el color del texto, en 2D
module decor_2d() {
    difference() {
        union() {
            text(label, size = text_size, font = font, halign = "center", valign = "center");
            if (border_w > 0)
                difference() {
                    rrect_c([plate_w - 2 * border_inset, plate_d - 2 * border_inset],
                            max(corner_r - border_inset, 0));
                    rrect_c([plate_w - 2 * (border_inset + border_w), plate_d - 2 * (border_inset + border_w)],
                            max(corner_r - border_inset - border_w, 0));
                }
        }
        if (hole_d > 0) hole_positions() circle(d = hole_d + 2);
    }
}

module base() {
    c = foot_chamfer;
    difference() {
        hull() {
            linear_extrude(height = eps) rrect_c([plate_w - 2 * c, plate_d - 2 * c], max(corner_r - c, 0));
            translate([0, 0, c]) linear_extrude(height = plate_h - c) rrect_c([plate_w, plate_d], corner_r);
        }
        if (mode == "inlay")
            translate([0, 0, plate_h - text_h]) linear_extrude(height = text_h + eps) decor_2d();
        if (hole_d > 0)
            translate([0, 0, -eps]) linear_extrude(height = plate_h + 2 * eps) hole_positions() circle(d = hole_d);
    }
}

module decor() {
    z0 = mode == "inlay" ? plate_h - text_h : plate_h;
    translate([0, 0, z0]) linear_extrude(height = text_h) decor_2d();
}
