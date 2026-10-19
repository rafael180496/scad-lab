// @checkpoint parent= desc="Costilla triangular de refuerzo sobre la cara de arriba"
// Receta de la skill openscad-stl-editing: un refuerzo con hull() que se funde con
// el modelo. hull() de una tira baja y de un pilar alto da una rampa triangular.

/* [Modelo] */
model_file = "refs/part.stl";
model_min = [0, 0, 0];
model_max = [40, 30, 20];
model_color = "#9ca3af";

/* [Costilla] */
rib_x0 = 5;       // dónde empieza la rampa, en X
rib_x1 = 35;      // dónde termina (aquí está lo más alto)
rib_y = 15;       // centro, en Y
rib_t = 3;        // espesor (2-3 perímetros: 1.6-2.4 mm mínimo estructural)
rib_h = 12;       // altura sobre la cara de arriba
// Cuánto se hunde dentro del modelo para fundirse con él
overlap = 1;      // [0.5:0.1:3]

/* [Hidden] */
top_z = model_max.z;
eps = 0.01;

module model() import(model_file, convexity = 10);

module rib() {
    hull() {
        // Pie: una tira casi plana que se apoya en la cara
        translate([rib_x0, rib_y - rib_t / 2, top_z - overlap])
            cube([rib_x1 - rib_x0, rib_t, overlap + eps]);
        // Cabeza: un pilar al final de la tira
        translate([rib_x1 - rib_t, rib_y - rib_t / 2, top_z - overlap])
            cube([rib_t, rib_t, overlap + rib_h]);
    }
}

// Misma pieza, mismo color: una sola unión
color(model_color)
    union() {
        model();
        rib();
    }
