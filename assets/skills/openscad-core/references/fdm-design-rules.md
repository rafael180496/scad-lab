# Reglas de diseño FDM — detalle y snippets

Complemento de [../SKILL.md](../SKILL.md) y de la skill de marca de la impresora
(`bambu-lab-printers`, `prusa-printers`, `creality-printers`, `elegoo-printers`,
`anycubic-printers`, `snapmaker-printers`, `ultimaker-printers`), que dicen lo
propio de cada máquina. Valores para boquilla 0.4 mm, PLA/PETG, perfiles por
defecto de cualquier slicer; una marca puede afinarlos. Todos los snippets son
OpenSCAD puro y se pegan dentro del checkpoint (los checkpoints son
autocontenidos).

## Voladizos y chaflanes

Un voladizo de hasta 45° se imprime sin soporte porque cada capa apoya al menos
la mitad sobre la anterior. Para techos internos o salientes, cambia un escalón
recto por un chaflán de 45°:

```openscad
// Saliente horizontal con chaflán de 45° por debajo (sin soportes)
module chamfered_ledge(length, depth, thickness) {
    rotate([90, 0, 0])
    linear_extrude(height = length, center = true)
        polygon([[0, 0], [depth, depth], [depth, depth + thickness], [0, depth + thickness]]);
}
```

Esferas y cúpulas: el tramo inferior (< 45°) necesita soporte o un corte plano.

## Pata de elefante

Las primeras capas se aplastan y sobresalen ~0.2–0.4 mm. En piezas que encajan,
chaflana el borde inferior:

```openscad
// Prisma con chaflán inferior de 45° en el contorno (perfil 2D cualquiera)
module foot_chamfered_extrude(h, c = 0.5) {
    hull() {
        linear_extrude(height = eps) offset(delta = -c) children();
        translate([0, 0, c]) linear_extrude(height = h - c) children();
    }
}
// Nota: hull() convierte el perfil en convexo; para perfiles cóncavos usa
// una unión de rodajas con offset, o resta un chaflán por cada arista.
```

## Agujeros horizontales en lágrima

Un agujero redondo horizontal tiene un "techo" plano que se cae. La lágrima
reemplaza la parte de arriba por dos caras a 45°:

```openscad
// Perfil 2D: círculo con punta hacia +Y
module teardrop_2d(r) {
    union() {
        circle(r = r);
        rotate(45) square(r);       // tangente al círculo a 45°
    }
}

// Agujero horizontal a lo largo de Y, punta hacia +Z. Para orientarlo en X:
// rotate([0, 0, 90]) teardrop_hole(...)
module teardrop_hole(d, length) {
    rotate([90, 0, 0])
        linear_extrude(height = length, center = true)
            teardrop_2d(d / 2);
}
```

Para tornillos ≤ M4 en horizontal suele bastar un círculo con +0.2 mm.

## Agujeros verticales y tolerancias

- Los círculos se aproximan con polígonos **inscritos**: el agujero real queda
  más chico. Con `$fs` bajo el efecto es menor; aun así suma 0.2 mm al Ø.
- Ejes que deben girar: Ø eje + 0.3–0.4.
- Ejes a presión: Ø eje + 0.05–0.15, con ranura de expansión si es largo.

## Encajes a presión (snap-fit)

- Gancho en voladizo: largo ≥ 5× espesor; espesor 1.2–2 mm; diente 0.6–1 mm.
- Orientar el voladizo en XY (paralelo a las capas): en Z se parte.
- Labio de tapa a presión: 0.15–0.2 mm de interferencia total por lado.

## Paredes y espesores

| Uso | Espesor |
|---|---|
| Decorativa / tapa liviana | 0.9–1.2 |
| Caja general | 1.6–2.0 |
| Pieza estructural, ganchos | 2.4–4.0 |
| Piso / techo | ≥ 4 capas (0.8 con capa 0.2) |

Una pared entre 1 y 2 anchos de línea (0.45–0.85) se imprime mal o queda hueca.

## Insertos térmicos M3 (boss)

```openscad
insert_d     = 4.0;   // Ø agujero según ficha del inserto («verificar» marca)
insert_depth = 6.7;   // largo del inserto + 1 mm
boss_d       = 8.0;

module insert_boss(h) {
    difference() {
        cylinder(d = boss_d, h = h);
        translate([0, 0, h - insert_depth]) cylinder(d = insert_d, h = insert_depth + eps);
        translate([0, 0, h - 0.5]) cylinder(d1 = insert_d, d2 = insert_d + 1 + eps, h = 0.5 + eps); // chaflán de entrada
    }
}
```

Alternativas sin inserto: agujero Ø 2.5–2.7 para tornillo M3 autorroscante
(plásticos), o tuerca M3 embutida (hexágono 5.5 entre caras → alojamiento
`cylinder(d = 6.0 / cos(30), h = 2.6, $fn = 6)` aprox.).

## Texto

```openscad
module label(t, size = 6, h = 0.6) {
    linear_extrude(height = h)
        text(t, size = size, font = "Liberation Sans:style=Bold",
             halign = "center", valign = "center");
}
```

- `size` ≥ 6 mm para lectura cómoda; 4 mm mínimo con bold.
- `h` múltiplo de la altura de capa (0.6 = 3 capas de 0.2).
- Grabado: restar `label()` con `h + eps` desde `z = top - depth`.

## Puentes

- Hasta ~20 mm: sin soporte en PLA bien refrigerado.
- Arcos/catenarias en lugar de techos planos cuando el diseño lo permite.
- Un agujero en un techo (por ej. alojamiento de cabeza de tornillo invertido):
  usa "capas de sacrificio" de 1 capa sobre el agujero, que se perforan después.

## Orientación y resistencia

- Las capas se separan con cargas en Z: un gancho en "L" se imprime acostado.
- Clips y bisagras con la flexión en el plano XY.
- Un eje largo y fino se imprime acostado (mejor redondez se consigue de pie,
  pero se quiebra fácil).

## Print-in-place

- Holgura 0.3–0.5 mm entre piezas móviles, en XY **y** en Z.
- Las holguras en Z dependen de la capa: con 0.2 usa 0.4 (2 capas) como mínimo.
- Evita grandes superficies paralelas a poca distancia: se sueldan.
- Pasadores cónicos (punta 45°) en bisagras para imprimir sin soporte.
