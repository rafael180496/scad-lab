// plate_check.scad — validar que el modelo cabe en la placa de una Prusa.
// Snippet para PEGAR al final de un checkpoint (no hacer `use` desde un
// checkpoint: los checkpoints son autocontenidos). La tabla es espejo de
// references/printers.json; si cambia una, cambiar la otra.
//
// Uso:
//   PRINTER = "prusa-mini";              // el `printer` de printer.json del workspace
//   assert_fits_plate([width, depth, height], PRINTER);   // llamada suelta, sin hijos
//   color("Orange") body();                                // el color sigue en primer nivel
//
// OpenSCAD no puede medir el bounding box de la geometría: pasa el tamaño
// total del modelo YA ORIENTADO como se va a imprimir (lo que calcules a partir
// de tus parámetros, incluidos bordes, orejas y piezas impresas juntas).

// [id, [x, y, z]]  — volumen nominal en mm
PRUSA_PRINTERS = [
    ["prusa-mk4s",        [250, 210, 220]],
    ["prusa-core-one",    [250, 220, 270]],   // con INDX: 248 x 205 x 270
    ["prusa-core-one-l",  [300, 300, 330]],
    ["prusa-mini",        [180, 180, 180]],
    ["prusa-xl",          [360, 360, 360]]
];

function printer_build_volume(id) =
    let(m = [for (p = PRUSA_PRINTERS) if (p[0] == id) p[1]])
    len(m) > 0 ? m[0] : undef;

// true si [x, y, z] + margen cabe, opcionalmente girando 90° en Z
function fits_plate(size, vol, margin = 2, allow_rotate_z = true) =
    size.z <= vol.z && (
        (size.x + 2 * margin <= vol.x && size.y + 2 * margin <= vol.y) ||
        (allow_rotate_z && size.y + 2 * margin <= vol.x && size.x + 2 * margin <= vol.y)
    );

// Valida; se usa como llamada suelta (preferido, deja el color() en primer
// nivel) o envolviendo hijos. En preview dibuja la placa como fantasma (%).
module assert_fits_plate(size, printer = "prusa-mini", margin = 2, allow_rotate_z = true, show_plate = true) {
    vol = printer_build_volume(printer);
    assert(!is_undef(vol), str("Impresora desconocida: '", printer, "'. Ver references/printers.json"));
    assert(fits_plate(size, vol, margin, allow_rotate_z),
        str("El modelo ", size, " mm (+", margin, " mm de margen) no cabe en ", printer,
            " ", vol, " mm. Reducir, partir en piezas o reorientar."));
    if ($preview && show_plate)
        %translate([-vol.x / 2, -vol.y / 2, -0.6]) cube([vol.x, vol.y, 0.5]);
    children();
}

// Ejemplo (borrar al pegar):
// PRINTER = "prusa-mini";
// assert_fits_plate([120, 80, 40], PRINTER);
// color("Orange") cube([120, 80, 40]);
