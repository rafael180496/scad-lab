// @checkpoint parent= desc="Maceta de prueba: cónica hueca con drenajes y relieve curvado en otro color"
// Fixture del diagnóstico del 3MF que Bambu Studio rellenaba (S17, tarea 17.2). Es una maceta
// reducida con la misma estructura que la del usuario, sin fuentes (las letras son polígonos) para
// que dé lo mismo en cualquier máquina:
//   - color() verde: difference() de un cuerpo cónico con una cavidad cónica que sobrepasa la boca
//     y drenajes cilíndricos que atraviesan el fondo.
//   - color() naranja: intersection() de un cascarón cónico con letras y un marco extruidos en
//     plano, que quedan curvados contra la pared. El cascarón entra 0.2 mm en la pared (se solapa
//     con el verde, como el original) y sale 0.8 mm hacia afuera.
d_sup = 60;
d_inf = 44;
alto = 50;
pared = 2.4;
fondo = 3;
relieve = 0.8;
penetra = 0.2;

function radio(z) = d_inf / 2 + (d_sup - d_inf) / 2 * (z / alto);

module maceta() {
    difference() {
        cylinder(r1 = d_inf / 2, r2 = d_sup / 2, h = alto, $fn = 64);
        translate([0, 0, fondo])
            cylinder(r1 = radio(fondo) - pared, r2 = d_sup / 2 - pared, h = alto - fondo + 2, $fn = 64);
        translate([0, 0, -1]) cylinder(d = 8, h = fondo + 2, $fn = 24);
        for (i = [0 : 5])
            rotate([0, 0, i * 60 + 30])
                translate([d_inf / 2 * 0.55, 0, -1]) cylinder(d = 4, h = fondo + 2, $fn = 20);
    }
}

module cascaron(e_in, e_out) {
    difference() {
        cylinder(r1 = d_inf / 2 + e_out, r2 = d_sup / 2 + e_out, h = alto, $fn = 64);
        translate([0, 0, -1])
            cylinder(r1 = d_inf / 2 - e_in, r2 = d_sup / 2 - e_in, h = alto + 2, $fn = 64);
    }
}

// Letras «P» y «L» hechas con rectángulos y un círculo: 2D plano, sin fuentes.
module letra_p() {
    translate([-5, -6]) square([3, 12]);
    translate([-5, 1]) difference() {
        hull() { square([3, 5]); translate([5, 2.5]) circle(r = 2.5, $fn = 24); }
        translate([3, 1.2]) hull() { square([0.1, 2.6]); translate([2, 1.3]) circle(r = 1.3, $fn = 24); }
    }
}
module letra_l() {
    translate([-5, -6]) square([3, 12]);
    translate([-5, -6]) square([9, 3]);
}
module marco() {
    difference() {
        offset(r = 3, $fn = 24) square([24, 16], center = true);
        offset(r = 1.8, $fn = 24) square([24, 16], center = true);
    }
}

module relieve_curvado() {
    z = 24;
    r = radio(z);
    ang = atan2((d_sup - d_inf) / 2, alto);
    intersection() {
        cascaron(penetra, relieve);
        union() {
            for (k = [[-1, 0], [1, 1]])
                rotate([0, 0, k[0] * 9])
                    translate([0, -r, z]) rotate([90 + ang, 0, 0])
                        linear_extrude(height = 6, center = true)
                            if (k[1] == 0) letra_p(); else letra_l();
            translate([0, -r, z]) rotate([90 + ang, 0, 0])
                linear_extrude(height = 6, center = true) marco();
        }
    }
}

// Misma estructura que el original: los dos color() cuelgan de un `if` del nivel superior.
// `solo` ("verde" / "naranja") deja un solo color: así se obtiene, con OpenSCAD, el STL de
// cada color por separado para compararlo con la parte del 3MF.
solo = "";
if (relieve > 0) {
    if (solo == "" || solo == "verde")
        color("#3A7D44") maceta();
    if (solo == "" || solo == "naranja")
        color("#E76F51") relieve_curvado();
}
