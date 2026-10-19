// @checkpoint parent= desc="Escala el STL para que mida 60 mm de ancho"
// Receta de la skill openscad-stl-editing: llevar un modelo a una medida exacta.
// Primero mide el ancho actual (measure / inspect_reference) y escríbelo aquí.

/* [Modelo] */
model_file = "refs/part.stl";
// Caja del modelo SIN escalar (la del STL), en mm
model_min = [0, 0, 0];
model_max = [40, 30, 20];
model_color = "#9ca3af";

/* [Tamaño final] */
target_width = 60;    // [5:0.5:400]   medida final en X
// uniform = la misma escala en los tres ejes; x-only = solo se estira X
mode = "uniform";     // [uniform, x-only]

/* [Hidden] */
k = target_width / (model_max.x - model_min.x);
sc = mode == "uniform" ? [k, k, k] : [k, 1, 1];

module model() import(model_file, convexity = 10);

// scale() escala respecto del origen: se vuelve a apoyar la esquina mínima en 0
// para que el modelo no se aleje de la placa.
color(model_color)
    translate([-model_min.x * sc.x, -model_min.y * sc.y, -model_min.z * sc.z])
        scale(sc)
            model();

// Alternativa: resize([target_width, 0, 0], auto = true) model();
// deja el ancho exacto sin calcular k, pero hay que medir la malla en cada
// render y el resultado depende de su caja actual.
