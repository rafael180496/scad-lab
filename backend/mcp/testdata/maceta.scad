// @checkpoint parent= desc="Maceta cónica con drenajes y relieve de texto curvado"
// =============================================================================
// Maceta cónica personalizable
// -----------------------------------------------------------------------------
// Cuerpo cónico hueco con cavidad para la tierra, agujeros de drenaje en el
// fondo, reborde superior y base achaflanada (contra la pata de elefante).
// Sobre la pared frontal lleva un texto y un marco en relieve que siguen la
// curvatura del cono: se obtienen intersectando el texto extruido con un
// cascarón cónico, en otro color (otro filamento en el 3MF).
//
// Piezas (cada una en su color):
//   1. maceta  -> color_maceta
//   2. relieve -> color_relieve (texto + marco)
//   3. plato   -> color_plato   (opcional, desactivado por defecto)
// =============================================================================

/* [Maceta] */
// Diámetro exterior de la base, en mm
diametro_base = 70; // [30:1:150]
// Diámetro exterior de la boca, en mm
diametro_boca = 95; // [40:1:180]
// Altura total, en mm
altura = 85; // [30:1:160]
// Grosor de la pared, en mm
grosor_pared = 2.4; // [1.2:0.1:6]
// Grosor del fondo, en mm
grosor_fondo = 3; // [1.5:0.1:8]
// Chaflán de la base contra la pata de elefante, en mm
chaflan_base = 0.6; // [0:0.1:2]

/* [Reborde] */
// Mostrar un reborde en la boca
reborde_activo = true;
// Altura del reborde, en mm
altura_reborde = 4; // [1:0.5:12]
// Cuánto sobresale el reborde hacia afuera, en mm
saliente_reborde = 1.6; // [0.4:0.1:5]

/* [Drenaje] */
// Cantidad de agujeros de drenaje alrededor del centro
cantidad_drenajes = 5; // [0:1:12]
// Diámetro de cada agujero, en mm
diametro_drenaje = 6; // [2:0.5:14]
// Distancia del centro a cada agujero, en mm
radio_drenajes = 16; // [5:1:40]
// Agujero central
drenaje_central = true;

/* [Costillas] */
// Costillas verticales decorativas en la pared exterior
costillas_activas = false;
// Cantidad de costillas
cantidad_costillas = 24; // [6:1:60]
// Ancho de cada costilla, en mm
ancho_costilla = 1.4; // [0.8:0.1:4]
// Cuánto sobresale la costilla, en mm
saliente_costilla = 0.8; // [0.4:0.1:3]

/* [Patas] */
// Patas de apoyo bajo la base (elevan la maceta para que drene)
patas_activas = false;
// Cantidad de patas
cantidad_patas = 4; // [3:1:8]
// Altura de cada pata, en mm
alto_pata = 6; // [2:0.5:20]
// Diámetro de cada pata, en mm
diametro_pata = 9; // [4:0.5:20]
// Distancia del centro a cada pata, en mm
radio_patas = 24; // [10:1:60]

/* [Asas] */
// Dos asas laterales para levantar la maceta
asas_activas = false;
// Altura del centro de las asas, en mm
z_asas = 55; // [20:1:140]
// Cuánto salen del cuerpo, en mm
salida_asa = 14; // [6:1:40]
// Grosor del tubo del asa, en mm
grosor_asa = 5; // [3:0.5:12]
// Alto del hueco del asa, en mm
hueco_asa = 16; // [8:1:40]

/* [Textura] */
// Rombos hundidos en la pared (textura decorativa)
textura_activa = false;
// Filas de rombos
filas_textura = 5; // [1:1:12]
// Rombos por fila
columnas_textura = 16; // [6:1:40]
// Tamaño de cada rombo, en mm
tamano_rombo = 5; // [2:0.5:12]
// Profundidad de la textura, en mm
profundidad_textura = 0.6; // [0.2:0.1:1.2]
// Altura de la primera fila, en mm
z_textura = 14; // [6:1:60]

/* [Base] */
// Grabar un texto bajo la base
grabado_base_activo = false;
// Texto del grabado
texto_base = "2026";
// Tamaño de las letras del grabado, en mm
tamano_grabado = 8; // [4:0.5:20]
// Profundidad del grabado, en mm
profundidad_grabado = 0.6; // [0.2:0.1:1.5]

/* [Relieve] */
// Texto en relieve
texto = "Rosa";
// Altura de las letras, en mm
tamano_texto = 11; // [4:0.5:30]
// Cuánto sobresale el relieve de la pared, en mm
altura_relieve = 1.2; // [0.6:0.1:4]
// Altura (desde la base) del centro del texto, en mm
z_texto = 42; // [10:1:140]
// Fuente del texto
fuente = "Liberation Sans:style=Bold";
// Espaciado entre letras
espaciado_texto = 1; // [0.8:0.05:1.5]
// Rodear el texto con un marco
marco_activo = true;
// Ancho del marco (a lo largo de la circunferencia), en mm
ancho_marco = 44; // [20:1:100]
// Alto del marco, en mm
alto_marco = 20; // [10:1:60]
// Grosor del borde del marco, en mm
borde_marco = 1.6; // [0.8:0.1:4]

/* [Plato] */
// Generar el plato inferior (otra pieza)
plato_activo = false;
// Holgura entre la maceta y el plato, en mm
holgura_plato = 0.4; // [0.2:0.05:1]
// Altura del plato, en mm
altura_plato = 12; // [6:1:30]

/* [Colores] */
color_maceta = "seagreen";
color_relieve = "orange";
color_plato = "white";

/* [Hidden] */
eps = 0.01;
$fa = $preview ? 12 : 4;
$fs = $preview ? 2 : 0.5;

// =============================================================================
// FUNCIONES
// =============================================================================

// Radio exterior del cuerpo a la altura z (interpolación lineal base -> boca).
function radio_ext(z) = diametro_base / 2 + (diametro_boca - diametro_base) / 2 * (z / altura);

// Radio interior del cuerpo a la altura z.
function radio_int(z) = radio_ext(z) - grosor_pared;

// Radio mayor del cuerpo (la boca), útil para dimensionar cajas auxiliares.
function radio_max() = max(diametro_base, diametro_boca) / 2 + saliente_reborde + 2;

// Ángulo, en grados, que ocupa en la circunferencia un arco de largo `arco` a
// la altura z. Sirve para recortar el marco y el texto al frente de la pared.
function angulo_arco(arco, z) = arco / radio_ext(z) * 180 / PI;

// Posición angular i-ésima de n elementos repartidos en 360°.
function angulo_en(i, n) = 360 / n * i;

// Cuántas letras tiene el texto (para estimar el ancho que ocupa).
function letras() = len(texto);

// Ancho aproximado del texto, en mm, con la fuente por defecto.
function ancho_texto() = letras() * tamano_texto * 0.62 * espaciado_texto;

// =============================================================================
// PERFILES
// =============================================================================

// Perfil 2D (mitad derecha) del cuerpo macizo: de la base al borde, con el
// chaflán inferior. Se gira con rotate_extrude.
module perfil_cuerpo() {
    r0 = diametro_base / 2;
    r1 = diametro_boca / 2;
    polygon([
        [0, 0],
        [r0 - chaflan_base, 0],
        [r0, chaflan_base],
        [r1, altura],
        [0, altura]
    ]);
}

// Perfil 2D (mitad derecha) de la cavidad interior para la tierra.
module perfil_cavidad() {
    polygon([
        [0, grosor_fondo],
        [radio_int(grosor_fondo), grosor_fondo],
        [radio_int(altura), altura],
        [radio_int(altura), altura + 1],
        [0, altura + 1]
    ]);
}

// Perfil 2D del reborde: un trapecio que sobresale hacia afuera en la boca.
module perfil_reborde() {
    r1 = diametro_boca / 2;
    z0 = altura - altura_reborde;
    polygon([
        [r1 - grosor_pared, z0],
        [r1 + saliente_reborde, z0 + saliente_reborde * 0.8],
        [r1 + saliente_reborde, altura],
        [r1 - grosor_pared, altura]
    ]);
}

// =============================================================================
// CUERPO
// =============================================================================

// Sólido exterior de la maceta, sin vaciar.
module cuerpo_macizo() {
    rotate_extrude() perfil_cuerpo();
}

// Hueco interior: la tierra va aquí.
module cavidad_interior() {
    rotate_extrude() perfil_cavidad();
}

// Anillo del reborde superior (opcional).
module reborde() {
    if (reborde_activo) {
        rotate_extrude() perfil_reborde();
    }
}

// Un agujero de drenaje vertical que atraviesa el fondo.
module agujero_drenaje() {
    translate([0, 0, -eps])
        cylinder(d = diametro_drenaje, h = grosor_fondo + 2 * eps);
}

// Todos los agujeros de drenaje: el central y los de la corona.
module drenajes() {
    if (drenaje_central) {
        agujero_drenaje();
    }
    if (cantidad_drenajes > 0) {
        for (i = [0 : cantidad_drenajes - 1]) {
            rotate([0, 0, angulo_en(i, cantidad_drenajes)])
                translate([radio_drenajes, 0, 0])
                    agujero_drenaje();
        }
    }
}

// Una costilla vertical: un prisma delgado pegado a la pared cónica.
module costilla() {
    r0 = diametro_base / 2;
    r1 = diametro_boca / 2;
    hull() {
        translate([r0 - 0.5, -ancho_costilla / 2, chaflan_base + 1])
            cube([saliente_costilla + 0.5, ancho_costilla, eps]);
        translate([r1 - 0.5, -ancho_costilla / 2, altura - altura_reborde - 1])
            cube([saliente_costilla + 0.5, ancho_costilla, eps]);
    }
}

// Corona de costillas decorativas (opcional).
module costillas() {
    if (costillas_activas) {
        for (i = [0 : cantidad_costillas - 1]) {
            rotate([0, 0, angulo_en(i, cantidad_costillas)])
                costilla();
        }
    }
}

// Los módulos de la maceta se definen más abajo (patas, asas, textura, grabado,
// refuerzo); este módulo los junta.

// La maceta completa: cuerpo menos cavidad y drenajes, más reborde, costillas,
// patas, asas y refuerzo; menos la textura y el grabado de la base.
module maceta() {
    difference() {
        union() {
            cuerpo_macizo();
            reborde();
            costillas();
            patas();
            asas();
            anillo_refuerzo();
        }
        // Cavidad para la tierra.
        cavidad_interior();
        // Agujeros de drenaje en el fondo.
        drenajes();
        // Textura de rombos hundidos en la pared.
        textura();
        // Texto grabado bajo la base.
        grabado_base();
    }
}

// =============================================================================
// PATAS
// =============================================================================

// Una pata cilíndrica con la punta achaflanada.
module pata() {
    cylinder(d = diametro_pata, h = alto_pata - 0.6);
    translate([0, 0, alto_pata - 0.6])
        cylinder(d1 = diametro_pata, d2 = diametro_pata - 1.2, h = 0.6);
}

// Las patas repartidas en una corona bajo la base.
module patas() {
    if (patas_activas) {
        for (i = [0 : cantidad_patas - 1]) {
            rotate([0, 0, angulo_en(i, cantidad_patas) + 45])
                translate([radio_patas, 0, -alto_pata + eps])
                    pata();
        }
    }
}

// =============================================================================
// ASAS
// =============================================================================

// Un asa: un arco de tubo que sale de la pared a la altura z_asas.
// Se construye con hull() de dos cilindros por tramo para que sea imprimible
// sin soportes (los tramos inclinados no pasan de 45°).
module asa() {
    r = radio_ext(z_asas);
    z = z_asas;
    mitad = hueco_asa / 2;
    // Tramo superior, inclinado hacia afuera.
    hull() {
        translate([r - 1, 0, z + mitad])
            rotate([90, 0, 0]) cylinder(d = grosor_asa, h = grosor_asa, center = true);
        translate([r + salida_asa, 0, z + mitad - 3])
            rotate([90, 0, 0]) cylinder(d = grosor_asa, h = grosor_asa, center = true);
    }
    // Tramo vertical exterior.
    hull() {
        translate([r + salida_asa, 0, z + mitad - 3])
            rotate([90, 0, 0]) cylinder(d = grosor_asa, h = grosor_asa, center = true);
        translate([r + salida_asa, 0, z - mitad + 3])
            rotate([90, 0, 0]) cylinder(d = grosor_asa, h = grosor_asa, center = true);
    }
    // Tramo inferior, inclinado hacia la pared.
    hull() {
        translate([r + salida_asa, 0, z - mitad + 3])
            rotate([90, 0, 0]) cylinder(d = grosor_asa, h = grosor_asa, center = true);
        translate([r - 1, 0, z - mitad])
            rotate([90, 0, 0]) cylinder(d = grosor_asa, h = grosor_asa, center = true);
    }
}

// Las dos asas, una a cada lado (eje X).
module asas() {
    if (asas_activas) {
        asa();
        rotate([0, 0, 180]) asa();
    }
}

// =============================================================================
// TEXTURA DE ROMBOS
// =============================================================================

// Un rombo hundido: un cubo girado 45° y orientado hacia el centro.
module rombo() {
    rotate([0, 45, 0])
        cube([tamano_rombo, profundidad_textura * 4, tamano_rombo], center = true);
}

// La textura completa: filas de rombos alrededor de la pared, alternadas.
// Se resta del cuerpo, así que no agrega material.
module textura() {
    if (textura_activa) {
        paso_z = tamano_rombo * 1.5;
        for (fila = [0 : filas_textura - 1]) {
            z = z_textura + fila * paso_z;
            if (z + tamano_rombo < altura - altura_reborde - 2) {
                desfase = (fila % 2) * (360 / columnas_textura / 2);
                for (c = [0 : columnas_textura - 1]) {
                    rotate([0, 0, angulo_en(c, columnas_textura) + desfase])
                        translate([radio_ext(z) + profundidad_textura * 1.5, 0, z])
                            rombo();
                }
            }
        }
    }
}

// =============================================================================
// GRABADO BAJO LA BASE
// =============================================================================

// Texto hundido bajo la base, para recordar el año o el nombre.
module grabado_base() {
    if (grabado_base_activo) {
        translate([0, 0, -eps])
            mirror([1, 0, 0])
                linear_extrude(height = profundidad_grabado + eps)
                    text(texto_base,
                         size = tamano_grabado,
                         font = fuente,
                         halign = "center",
                         valign = "center");
    }
}

// =============================================================================
// REFUERZO DE LA BOCA
// =============================================================================

// Anillo interior que refuerza la boca cuando la pared es muy fina. Solo se
// agrega si el grosor de pared es menor que 1.8 mm.
module anillo_refuerzo() {
    if (grosor_pared < 1.8) {
        z0 = altura - altura_reborde - 6;
        translate([0, 0, z0])
            difference() {
                cylinder(r1 = radio_ext(z0) - 0.2, r2 = radio_ext(altura - altura_reborde) - 0.2, h = 6);
                translate([0, 0, -eps])
                    cylinder(r1 = radio_int(z0) - 1.2, r2 = radio_int(altura - altura_reborde) - 1.2, h = 6 + 2 * eps);
            }
    }
}

// =============================================================================
// RELIEVE (TEXTO Y MARCO) SOBRE LA PARED CÓNICA
// =============================================================================

// Cascarón cónico que envuelve la pared exterior: su cara interior coincide con
// la pared y su cara exterior queda `altura_relieve` por fuera. Intersectar algo
// plano con este cascarón lo "dobla" siguiendo el cono.
//   z0, z1: altura donde empieza y termina el cascarón.
module cascaron_relieve(z0, z1) {
    h = z1 - z0;
    difference() {
        translate([0, 0, z0])
            cylinder(r1 = radio_ext(z0) + altura_relieve,
                     r2 = radio_ext(z1) + altura_relieve,
                     h = h);
        translate([0, 0, z0 - eps])
            cylinder(r1 = radio_ext(z0),
                     r2 = radio_ext(z1),
                     h = h + 2 * eps);
    }
}

// Texto 2D centrado.
module texto_2d() {
    text(texto,
         size = tamano_texto,
         font = fuente,
         halign = "center",
         valign = "center",
         spacing = espaciado_texto);
}

// Texto extruido en dirección -Y (hacia el frente de la maceta): el eje del
// texto queda en el plano XZ y la extrusión cruza toda la pared.
module texto_plano() {
    rotate([90, 0, 0])
        linear_extrude(height = radio_max())
            texto_2d();
}

// El texto en relieve, curvado: texto plano ∩ cascarón cónico.
module relieve_texto() {
    zc = z_texto;
    margen = tamano_texto * 1.2;
    intersection() {
        translate([0, 0, zc])
            texto_plano();
        cascaron_relieve(max(zc - margen, chaflan_base), min(zc + margen, altura - altura_reborde));
    }
}

// Rectángulo 2D hueco que hace de marco.
module marco_2d() {
    difference() {
        square([ancho_marco, alto_marco], center = true);
        square([ancho_marco - 2 * borde_marco, alto_marco - 2 * borde_marco], center = true);
    }
}

// Marco plano extruido hacia el frente.
module marco_plano() {
    rotate([90, 0, 0])
        linear_extrude(height = radio_max())
            marco_2d();
}

// El marco en relieve, curvado igual que el texto.
module relieve_marco() {
    if (marco_activo) {
        zc = z_texto;
        intersection() {
            translate([0, 0, zc])
                marco_plano();
            cascaron_relieve(max(zc - alto_marco / 2 - 1, chaflan_base), min(zc + alto_marco / 2 + 1, altura - altura_reborde));
        }
    }
}

// Texto y marco juntos: una sola pieza de color.
module relieve() {
    relieve_texto();
    relieve_marco();
}

// =============================================================================
// PLATO INFERIOR (OPCIONAL)
// =============================================================================

// Plato cónico que recoge el agua: se imprime aparte, al lado de la maceta.
module plato() {
    if (plato_activo) {
        r_in = diametro_base / 2 + holgura_plato;
        r_out = r_in + grosor_pared + 4;
        translate([diametro_boca + 10, 0, 0])
            difference() {
                cylinder(r1 = r_out - 2, r2 = r_out, h = altura_plato);
                translate([0, 0, grosor_fondo])
                    cylinder(r1 = r_in, r2 = r_in + 4, h = altura_plato);
            }
    }
}

// =============================================================================
// DEPURACIÓN
// =============================================================================

// Silueta de cotas para revisar el modelo en la vista previa: no se imprime
// (el modificador * la desactiva) y no entra en el 3MF.
module guia_dimensiones() {
    // Línea vertical a lo alto del cuerpo.
    translate([radio_max() + 4, 0, 0])
        cylinder(d = 0.6, h = altura);
    // Marca de la altura del texto.
    translate([radio_max() + 4, 0, z_texto])
        sphere(d = 2);
}
*guia_dimensiones();

// =============================================================================
// NOTAS DE USO
// =============================================================================
//
// Medidas habituales (diámetro de boca x altura):
//   - Suculentas:   70 x 55   (drenaje central de 6 mm)
//   - Hierbas:      95 x 85   (5 drenajes de 6 mm en corona)
//   - Helechos:    130 x 110  (usar pared de 3 mm y 6 drenajes)
//
// Para un texto largo (más de 8 letras) conviene subir `ancho_marco` y bajar
// `tamano_texto`; el marco se curva igual que el texto, así que un marco muy
// ancho se "enrolla" más de lo que parece en la vista plana. Por encima de
// 90° de arco el relieve deja de verse de frente.
//
// Impresión:
//   - Base de la maceta contra la placa, sin soportes (la pared sube a menos de
//     45° respecto de la vertical con los valores por defecto).
//   - El relieve es otra pieza de color: en Bambu Studio se imprime con otro
//     filamento (AMS) y queda pegado a la pared, sin voladizos.
//   - Si el texto no se ve, sube `altura_relieve` a 1.6 o más.
//
// Cambios frecuentes y dónde hacerlos:
//   - Tamaño de la maceta        -> sección [Maceta].
//   - Cantidad de agujeros       -> sección [Drenaje].
//   - Texto y marco              -> sección [Relieve].
//   - Colores de cada pieza      -> sección [Colores].
//   - Forma del perfil exterior  -> módulo perfil_cuerpo().
//   - Forma de la cavidad        -> módulo perfil_cavidad().
//
// Historial de decisiones:
//   - El cuerpo se hace con rotate_extrude de un polígono (en vez de un
//     cylinder cónico) para poder achaflanar la base sin hull().
//   - El relieve es la intersección del texto plano con un cascarón cónico: así
//     la altura es uniforme sobre la pared curva y no hay que calcular rotaciones
//     por letra.
//   - Las piezas de distinto color no se solapan: el cascarón empieza justo en la
//     pared exterior y la maceta no lo contiene.
//   - eps = 0.01 en todos los cortes para evitar caras coplanares.

// =============================================================================
// ENSAMBLE
// =============================================================================

// Cada pieza de primer nivel va en su propio color() para el 3MF multicolor.
color(color_maceta) maceta();
color(color_relieve) relieve();
color(color_plato) plato();
