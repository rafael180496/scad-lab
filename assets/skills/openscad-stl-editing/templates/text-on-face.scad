// @checkpoint parent= desc="Texto grabado o en relieve sobre la cara de arriba"
// Receta de la skill openscad-stl-editing. Grabado: se resta del modelo.
// En relieve: una pieza aparte de otro color apoyada en la cara (un filamento más).

/* [Modelo] */
model_file = "refs/part.stl";
model_min = [0, 0, 0];
model_max = [40, 30, 20];
model_color = "#9ca3af";

/* [Texto] */
label = "ScadLab";
mode = "engrave";       // [engrave, emboss]
text_size = 6;          // [4:0.5:30]   alto de las letras; mínimo legible 4-6 mm
// Profundidad del grabado, o altura del relieve (múltiplo de la capa)
depth = 0.6;            // [0.2:0.2:3]
text_x = 20;            // [-200:0.5:200]
text_y = 15;            // [-200:0.5:200]
text_color = "#d32f2f"; // solo en relieve

/* [Hidden] */
top_z = model_max.z;
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.4;

module model() import(model_file, convexity = 10);

module label_2d()
    text(label, size = text_size, font = "Liberation Sans:style=Bold", halign = "center", valign = "center");

color(model_color)
    difference() {
        model();
        if (mode == "engrave")
            translate([text_x, text_y, top_z - depth])
                linear_extrude(depth + 1) label_2d();
    }

// En relieve: sobre la cara, hundido eps para que se funda con el modelo
if (mode == "emboss")
    color(text_color)
        translate([text_x, text_y, top_z - eps])
            linear_extrude(depth + eps) label_2d();
