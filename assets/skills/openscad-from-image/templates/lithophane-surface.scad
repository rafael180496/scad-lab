// @checkpoint parent= desc="Litofanía o relieve con surface() a partir de un mapa de alturas"
// Receta de la skill openscad-from-image: usa el .dat que deja image_to_heightmap
// en refs/. Copia aquí lo que devolvió la herramienta (`columns`, `rows`, `sizeMm`).
// Litofanía: pide el mapa con invert=true (oscuro = grueso), imprímela de pie y
// ponle una luz detrás. Relieve: sin invert (claro = alto).

/* [Mapa de alturas] */
map_file = "refs/photo-heightmap.dat";
// Columnas y filas del mapa (`columns` y `rows` del resultado)
cols = 200;     // [2:1:500]
rows = 150;     // [2:1:500]
// Tamaño de la pieza en mm (`sizeMm.width` y `sizeMm.height`)
width = 80;     // [10:1:300]
depth = 60;     // [10:1:300]

/* [Espesores] */
base = 0.6;     // [0.4:0.1:3] espesor donde el mapa vale 0 (lo más fino de una litofanía)
relief = 2.4;   // [0.4:0.2:20] cuánto sube el mapa sobre la base (valor 100)

/* [Marco y posición] */
frame = true;
frame_w = 3;    // [0:0.5:15]
// Una litofanía se imprime de pie, con la cara hacia el frente
stand_up = true;
piece_color = "white";

/* [Hidden] */
eps = 0.01;

sx = width / (cols - 1);
sy = depth / (rows - 1);
sz = relief / 100;

// surface() baja un punto por debajo del mínimo del mapa: se recorta a z >= 0.
module relief_body() {
    intersection() {
        union() {
            translate([0, 0, base]) scale([sx, sy, sz]) surface(file = map_file, center = true);
            translate([-width / 2, -depth / 2, 0]) cube([width, depth, base + eps]);
        }
        translate([-width / 2, -depth / 2, 0]) cube([width, depth, base + relief + 1]);
    }
}

module piece() {
    relief_body();
    if (frame && frame_w > 0)
        difference() {
            translate([-width / 2 - frame_w, -depth / 2 - frame_w, 0])
                cube([width + 2 * frame_w, depth + 2 * frame_w, base + relief]);
            translate([-width / 2, -depth / 2, -1])
                cube([width, depth, base + relief + 2]);
        }
}

color(piece_color)
    if (stand_up)
        // El eje Y del mapa pasa a ser el alto; apoyada en z = 0.
        translate([0, 0, depth / 2 + (frame ? frame_w : 0)]) rotate([90, 0, 0]) piece();
    else
        piece();
