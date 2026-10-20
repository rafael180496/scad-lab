// @checkpoint parent= desc="Placa con un logo trazado en relieve (una capa por color)"
// Receta de la skill openscad-from-image: usa el SVG que deja trace_image_outline
// en refs/. El SVG llega en mm, con la esquina inferior izquierda de la forma en
// el origen. Con layers=N hay un SVG por color: ponlos todos en `logo_files` y
// sus colores en `logo_colors`, en el mismo orden (cuenta también el color de la
// placa: no superes los colores del workspace).

/* [Logo trazado] */
// Un SVG por capa de color, en el orden en que los devolvió la herramienta
logo_files = ["refs/logo-outline.svg"];
logo_colors = ["#1f2937"];
// Ancho y alto del contorno trazado, en mm (`sizeMm` del resultado)
logo_w = 60;   // [5:0.5:300]
logo_h = 40;   // [5:0.5:300]

/* [Placa] */
margin = 4;          // [0:0.5:30] borde libre alrededor del logo
plate_t = 2;         // [0.8:0.2:8] grosor de la placa
relief = 1.2;        // [0.4:0.2:6] cuánto sobresale el logo
corner_r = 3;        // [0:0.5:15] radio de las esquinas
plate_color = "white";

/* [Hidden] */
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

module rounded_rect(w, h, r) {
    if (r <= 0) square([w, h], center = true);
    else offset(r = r) square([w - 2 * r, h - 2 * r], center = true);
}

// La placa, centrada en XY y apoyada en z = 0.
color(plate_color)
    linear_extrude(plate_t) rounded_rect(logo_w + 2 * margin, logo_h + 2 * margin, corner_r);

// El logo, centrado sobre la placa; cada capa en su color.
for (i = [0 : len(logo_files) - 1])
    color(logo_colors[i])
        translate([-logo_w / 2, -logo_h / 2, plate_t - eps])
            linear_extrude(relief + eps) import(logo_files[i]);
