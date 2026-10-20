---
# name: kebab-case, IGUAL al nombre de la carpeta (ej. carpeta "openscad-threads" -> name: openscad-threads).
# Solo minúsculas, números y guiones.
name: skill-template
# description: una o dos frases que digan CUÁNDO usar la skill. Es lo único que el
# agente lee antes de decidir cargarla: nombra los pedidos, palabras o situaciones
# que la disparan. Ej.: "Diseño de roscas imprimibles en OpenSCAD. Úsala cuando el
# usuario pida tornillos, tuercas o tapas roscadas."
description: Plantilla para crear skills nuevas de ScadLab; no es una skill operativa. No la uses para modelar: cópiala y renómbrala según el README de esta carpeta.
---

<!--
  Plantilla de skill de ScadLab. Al copiarla:
  1. Renombra la carpeta y el campo `name` (kebab-case, idénticos).
  2. Reescribe `description` (cuándo se usa, en español).
  3. Completa las secciones de abajo y borra estos comentarios.
  4. Mantén este archivo por debajo de ~250 líneas: lo extenso va en
     references/*.md y el código listo para copiar en templates/*.scad.
  Texto en español; identificadores y código OpenSCAD en inglés.
-->

# <Título de la skill>

<!-- Una o dos frases: qué resuelve esta skill y de qué otras depende
     (casi siempre de `openscad-core`, que define el contrato con la app). -->

## Cuándo usar

<!-- Lista concreta de pedidos o situaciones. Ej.:
- El usuario pide una pieza con rosca.
- Un render falla con "Ignoring unknown module 'threaded_rod'".
-->

## Reglas

<!-- Reglas obligatorias, en forma de lista o tabla. Recordar siempre las del
     contrato de la app:
- Cambios a un archivo que el usuario nombró: `update_checkpoint` (con respaldo); un checkpoint nuevo `NNN-slug.scad` solo para algo nuevo o una variante.
- Línea 1: `// @checkpoint parent=<padre.scad> desc="<qué cambia>"`.
- Toda pieza de primer nivel dentro de `color()`.
- Parámetros arriba con comentarios de Customizer; milímetros.
- Validar el render y corregir con `update_checkpoint` sobre el mismo archivo.
-->

## Flujo

<!-- Pasos numerados que sigue el agente. Ej.:
1. Leer printer.json (si existe) para el volumen de impresión.
2. Partir de templates/<plantilla>.scad copiándola como `NNN-slug.scad`.
3. Ajustar parámetros, renderizar, revisar warnings.
-->

## Ejemplos

<!-- Fragmentos cortos de OpenSCAD válidos. Lo largo va en templates/.

```openscad
// @checkpoint parent= desc="Ejemplo"
/* [Dimensiones] */
size = 20; // [5:1:100]

/* [Hidden] */
eps = 0.01;

color("Orange") cube(size);
```

Referencias: [references/<tema>.md](references/<tema>.md)
-->

## Errores comunes

<!-- Tabla mensaje -> causa -> arreglo. Ej.:

| Mensaje | Causa | Arreglo |
|---|---|---|
| `Ignoring unknown module 'x'` | falta include | agregar `include <lib/...>` |
-->
