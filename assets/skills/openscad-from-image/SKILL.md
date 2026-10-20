---
name: openscad-from-image
description: Modelar en OpenSCAD a partir de una imagen que el usuario adjunta (archivos en refs/images/), sea la foto de una pieza que quiere copiar, un logo o una placa plana que quiere en 3D, un relieve o litofanía, un boceto acotado a mano o varias fotos (frente, lado, arriba). Úsala cuando haya imágenes en refs/ o el usuario diga «hazlo como en la foto», «copia esta pieza», «convierte este logo en 3D», «hazme una litofanía» o «sigue este dibujo». Incluye cómo sacar la escala de la foto, el flujo crear, render, comparar con la imagen y corregir, y qué se puede y qué no se puede prometer.
---

# Modelar a partir de una imagen

Requisito previo: `openscad-core` (cuándo editar un checkpoint y cuándo crear uno, `color()` obligatorio,
parámetros arriba). Si lo que el usuario adjuntó es un **STL, 3MF u OBJ**, no es
esta skill: usa `inspect_reference` y `openscad-stl-editing`.

## 1. Qué se puede y qué no (dilo al usuario)

Una foto **no trae medidas**: son píxeles. Y una foto es una sola vista con
perspectiva. Por eso:

- La **escala** sale de una medida que da el usuario o de un objeto conocido que
  aparece en la foto (moneda, tarjeta, hoja). Sin ninguna de las dos, propón un
  tamaño típico y **di que es un supuesto**.
- Con una foto con lente y perspectiva, las medidas sacadas de ella tienen un
  error de **5–15 %**; no sirve para tolerancias de encaje. Lo que encaja con
  otra pieza se mide con calibre: pídelo.
- **Lo que no se ve no se sabe**: el grosor, el fondo de una caja, el lado
  oculto. Dedúcelo de la función de la pieza, ponlo como parámetro y dilo.
- Lo **orgánico** (figuras, animales, caras) se aproxima con formas simples
  (`hull()` de esferas, `scale()`); no promete parecido escultórico. Dilo antes
  de empezar, no al final.
- Una línea de 1–2 píxeles de ancho no sobrevive al trazado: pide una imagen más
  grande o con más contraste.

## 2. Herramientas

| Herramienta | Para qué | Necesita visión |
|---|---|---|
| `list_refs` | encuentra las imágenes (`kind: "image"`, en `refs/images/`) | no |
| `image_info` | tamaño, proporción, colores, caja del objeto; con `region` + `enhance` amplía y realza un detalle | para mirarla |
| `compare_with_image` | foto y modelo lado a lado (o superpuestos), vista elegida, proporciones, `silhouetteIoU` | para mirarla; el IoU y las proporciones sirven sin visión |
| `trace_image_outline` | contorno plano en mm → `refs/x.svg` para `import()` | **no** |
| `image_to_heightmap` | relieve → `refs/x.dat` para `surface()` | no |

Las imágenes se leen **solo** de `refs/` y `refs/images/`. Si el modelo elegido
**no ve imágenes**, no puedes juzgar la forma a ojo: apóyate en `image_info`
(proporciones, colores, caja), en `trace_image_outline` (el contorno se obtiene
sin mirar) y en el `silhouetteIoU` de `compare_with_image`, y dile al usuario que
revise el visor y la foto con sus ojos.

## 3. Flujo

1. **Mirar.** `list_refs` y `image_info(image)`. Con visión, mira la imagen
   devuelta; para un detalle (una etiqueta, una cota, un agujero) repite con
   `region=[x,y,ancho,alto]` (fracciones 0–1). Anota la **proporción** del objeto
   (`foreground.aspect`) y su caja.
2. **Entender.** En una frase: qué es, para qué sirve, qué partes tiene (cuerpo,
   vaciados, agujeros, nervios, redondeos) y qué simetrías. Si hay dos lecturas
   posibles, elige la más probable, pásala a parámetro y dilo.
3. **Escala** (sección 4). Decide cuánto mide algo en mm **antes** de escribir
   código.
4. **Descomponer** en primitivas y CSG: `cube`, `cylinder`, `sphere`, `hull`,
   `linear_extrude`, `rotate_extrude`, `difference`. Lo incierto (grosores,
   diámetros de agujeros, holguras) va como **parámetro del Customizer** con un
   valor razonable.
5. **Crear** con `create_checkpoint` (flujo normal de `openscad-core`), `render`
   y `preview`.
6. **Comparar**: `compare_with_image(image, file)` con `view=auto`: elige la vista
   cuya silueta mejor coincide con la foto y dice cuál fue. Las siguientes veces
   fija esa **misma vista** (`view=front`…) para comparar peras con peras.
7. **Leer el resultado** y corregir en un checkpoint **nuevo**:

   | `silhouetteIoU` | Qué hacer |
   |---|---|
   | ≥ 0,9 | la forma coincide: revisa detalles (agujeros, redondeos) |
   | 0,75–0,9 | parecido: busca qué parte sobra o falta en la imagen lado a lado |
   | < 0,75 | forma o proporciones distintas: revisa el cuerpo base |

   `proportions.differencePercent` positivo = el modelo es más ancho (o menos
   alto) que la foto. Corrige **una cosa por checkpoint** y no pases de **tres
   rondas** sin decirle al usuario qué falta.
8. **Cerrar**: resumen corto con el checkpoint, las medidas finales y una lista
   de **supuestos** («grosor 3 mm, no se ve en la foto») y de lo que conviene que
   confirme con un calibre.

## 4. Sacar la escala

En orden de preferencia:

1. **Una medida del usuario** («el ancho es 80 mm»). Es la mejor: úsala.
2. **Un objeto conocido en la foto**, en el mismo plano que la pieza y con la
   cámara de frente: moneda, tarjeta ID-1, hoja A4/Carta, pila, tornillo. Las
   medidas, con su fuente, están en
   [references/standard-sizes.md](references/standard-sizes.md). Mide en la foto
   el objeto (`image_info` con `region` sobre él, o su caja) y la pieza y haz la
   regla de tres: `mm_pieza = px_pieza × mm_objeto / px_objeto`. Si el objeto
   está más cerca o más lejos que la pieza, el error crece: dilo.
3. **Un tamaño típico** de lo que sea («un llavero mide ~ 40 mm»). Es un supuesto:
   decláralo y deja el tamaño como parámetro.

Para `trace_image_outline`: `width_mm` / `height_mm` son del **contorno
trazado**, no de la foto; `mm_per_px` es de la foto (milímetros por píxel del
archivo original) y sale de la regla de tres de arriba.

## 5. Casos

### Pieza mecánica (soporte, clip, adaptador, tapa)
Cuerpo con primitivas, `difference()` para los vaciados y agujeros. Tornillería,
tuercas y holguras: [references/standard-sizes.md](references/standard-sizes.md)
y la skill de tu impresora. Receta con la escala por una moneda:
[templates/bracket-from-photo.scad](templates/bracket-from-photo.scad).

### Carcasa o caja
Parte de lo que **contiene** (una placa, una pila, un conector): sus medidas
mandan, la foto da la forma. Paredes ≥ 1,6 mm, holgura al contenido 0,3–0,5 mm
por lado, tapa y agujeros de cable como parámetros. Si se parece a una plantilla
(`list_templates`), arranca de ella.

### Soporte o sostén
Mide **lo que sostiene** (diámetro de un tubo, ancho de un teléfono) y dimensiona
el soporte a partir de eso, no de la foto.

### Placa o logo plano
`trace_image_outline(image, width_mm=…)` y en el checkpoint
`linear_extrude(h) import("refs/x.svg");`. El SVG llega con la esquina inferior
izquierda de la forma en el origen y Y hacia arriba. Un logo de varios colores:
`layers=N` da un SVG por color; cada uno va en su `color()` y sobre la misma
altura de base (en
[templates/plate-from-svg.scad](templates/plate-from-svg.scad) está armado). Si
el contorno sale **al revés** (traza el fondo), usa `invert=true`; si sale
recargado o con huecos de más, sube `min_area_mm2` o ajusta `threshold`.

### Relieve o litofanía
`image_to_heightmap(image, width_mm, max_height_mm, invert?)` y el snippet con
`surface()`. Un **relieve** usa claro = alto; una **litofanía** usa `invert=true`
(oscuro = grueso, la luz pasa por lo fino). Espesores típicos de una litofanía en
PLA blanco: base 0,6–0,8 mm y relieve hasta 2,4–3 mm (por confirmar con una
prueba). Se imprime **de pie**, con la cara hacia el frente. Receta:
[templates/lithophane-surface.scad](templates/lithophane-surface.scad).

### Orgánico
Aproxima con `hull()` de esferas y `scale()`; si el usuario quiere el parecido
real de una escultura, dile que esta vía da una **versión simplificada**. Para
copiar una malla que ya existe (STL), `openscad-stl-editing`.

## 6. Varias fotos (frente, lado, arriba)

Cada foto es **una vista**: el frente fija ancho (X) y alto (Z), el lado fija la
profundidad (Y) y el alto, y la de arriba fija ancho y profundidad. Usa las tres
para **reconciliar** medidas (el alto del frente y del lado deben coincidir) y
luego compara cada una con su vista: `compare_with_image(image=frente, view=front)`,
`view=right` (la foto del lado derecho) y `view=top`. Pide al usuario fotos
perpendiculares, con la pieza sobre un fondo liso y un objeto de escala **en el
mismo plano**. Con una sola foto, la profundidad es un supuesto.

## 7. Bocetos acotados a mano

Las **cotas mandan, no el trazo**: no trates de calcar el dibujo. Léelas con
`image_info` (`region` + `enhance` sobre cada cota), pásalas a parámetros y
confirma la unidad (¿mm o cm?). Un boceto de planta y alzado se trata como dos
fotos (sección 6). Una cota que no se lea **se le pregunta al usuario**; no se
adivina.

## 8. Errores comunes

| Síntoma | Causa | Arreglo |
|---|---|---|
| `trace_image_outline` traza el fondo o un marco | la forma es lo que difiere del borde de la foto | `invert=true`, o recorta la foto |
| el contorno sale con motas o huecos de más | ruido de la foto | sube `min_area_mm2`; prueba `threshold` |
| IoU bajo con `view=auto` en todas las vistas | fondo no uniforme, sombras, foto muy en perspectiva | pide una foto sobre fondo liso y de frente; compara a ojo |
| el SVG mide otra cosa que la foto | `width_mm` es del contorno, no de la foto | recalcula con la caja del objeto |
| `import("refs/x.svg")` no encuentra el archivo | la ruta es relativa al `.scad` (raíz del workspace) | deja `refs/…` tal cual |
| la litofanía sale plana | la imagen usa pocos grises | `stretch=true`; más `max_height_mm` |
| «demasiados píxeles» o «demasiado grande» | límites: 20 MB, 40 MP | pide una versión reducida (se trabaja a ≤ 2048 px) |

## 9. Archivos

- [references/standard-sizes.md](references/standard-sizes.md): monedas, tarjeta,
  hojas, pilas, tornillos y conectores, con su fuente.
- [references/photo-tips.md](references/photo-tips.md): cómo pedir fotos que se
  puedan usar.
- [templates/bracket-from-photo.scad](templates/bracket-from-photo.scad): pieza
  por primitivas con la escala sacada de una moneda.
- [templates/plate-from-svg.scad](templates/plate-from-svg.scad): placa con un
  logo trazado, con una o varias capas de color.
- [templates/lithophane-surface.scad](templates/lithophane-surface.scad):
  litofanía o relieve con `surface()`.
