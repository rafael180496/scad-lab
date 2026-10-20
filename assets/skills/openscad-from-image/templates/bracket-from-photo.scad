// @checkpoint parent= desc="Soporte en L cuya escala sale de una moneda de la foto"
// Receta de la skill openscad-from-image: una pieza armada con primitivas a
// partir de medidas LEÍDAS EN PÍXELES de una foto. La escala sale de un objeto
// de medida conocida (una moneda) que está en el mismo plano que la pieza.
// Lo que la foto no muestra (el ancho del soporte, el grosor) es un supuesto y
// queda como parámetro: cámbialo con las medidas reales.

/* [Escala de la foto] */
// Diámetro real del objeto de escala, en mm (aquí una moneda de 25 centavos de USD: 24.26)
ref_mm = 24.26;
// Lo que mide ese objeto en la foto, en píxeles (image_info con `region` sobre la moneda)
ref_px = 150;

/* [Medidas leídas en la foto, en píxeles] */
base_len_px = 480;    // largo de la base
base_thick_px = 36;   // grosor de la base
wall_height_px = 360; // alto de la pared vertical
wall_thick_px = 36;   // grosor de la pared
gusset_px = 150;      // lado del refuerzo triangular (0 = sin refuerzo)

/* [Supuestos: no se ven en la foto] */
// Ancho del soporte (la profundidad que la foto no muestra)
bracket_w = 25;       // [10:1:80]
// Tornillo de las dos perforaciones de la base (M4 → 4.5 pasante, serie media ISO 273)
hole_d = 4.5;         // [2.2:0.1:9]
// Holgura que se suma al agujero para FDM
clearance = 0.2;      // [0:0.05:0.6]
bracket_color = "#9ca3af";

/* [Hidden] */
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

// De píxeles a milímetros con la regla de tres de la moneda.
mm_per_px = ref_mm / ref_px;
function mm(px) = px * mm_per_px;

base_len = mm(base_len_px);
base_t = mm(base_thick_px);
wall_h = mm(wall_height_px);
wall_t = mm(wall_thick_px);
gusset = mm(gusset_px);

module profile() {
    // Perfil en L en el plano XZ: base a lo largo de X y pared en x = 0.
    union() {
        square([base_len, base_t]);
        square([wall_t, wall_h]);
        if (gusset > 0)
            polygon([[wall_t - eps, base_t - eps], [wall_t - eps, base_t + gusset], [wall_t + gusset, base_t - eps]]);
    }
}

module bracket() {
    difference() {
        // Extruye el perfil a lo ancho (Y) con la cara frontal en y = 0.
        rotate([90, 0, 0]) translate([0, 0, -bracket_w]) linear_extrude(bracket_w) profile();
        // Dos agujeros pasantes en la base, lejos de la pared.
        for (k = [0.45, 0.8])
            translate([base_len * k, bracket_w / 2, -eps])
                cylinder(d = hole_d + 2 * clearance, h = base_t + 2 * eps);
    }
}

color(bracket_color) bracket();
