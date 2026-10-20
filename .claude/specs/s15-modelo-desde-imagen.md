# S15 — Crear modelos a partir de imágenes

Fase del roadmap: [claude/01-roadmap.md](../../claude/01-roadmap.md). Va **después de cerrar S14** y no se mezcla
con ella. Notas vigentes al cerrar: `claude/28-modelo-desde-imagen.md` (nuevo), `claude/13`, `claude/25`, `claude/19`.

## Pedido (textual)

«también necesito que, en base a imágenes que te adjunte, puedas generar los modelos; crea skill, tools, etc.
para esto»

## Qué ya existe

Adjuntos de imagen del chat (S12, `app_chat_attach.go`: staging en `<datos>/chat-attachments/`, al enviar se
mueven a `<datos>/chat-attachments/<chatId>/`), imágenes de vuelta al modelo (`preview`, pasos con `Images`),
`backend/refmodel` (refs/ de STL/3MF/OBJ, `ResolveRef`), `backend/raster` (rasterizador de mallas en Go), los CLIs
ya reciben las rutas de las imágenes (`cliTurn`: `Images`, `ReadDirs`) y las capacidades de visión por modelo
(`llm.ModelInfo.Vision/VisionKnown`, `modelCaps`).

## Decisiones

1. **Método en una skill incluida `openscad-from-image`** (estructura de `_skill-template`): identificar objeto y
   función → escala (una medida del usuario o un objeto conocido de la foto: tabla de monedas, tarjeta ID-1
   85,60 × 53,98 mm, A4, pilas, tornillos… con fuente) → descomponer en primitivas/CSG → parámetros del Customizer
   para lo incierto → crear → render → **`compare_with_image` desde la misma vista** → corregir. Casos: pieza
   mecánica, carcasa, soporte, placa/logo plano (`trace_image_outline`), relieve/litofanía (`image_to_heightmap`),
   orgánico (aproximar y decir el límite). Varias fotos (frente/lado/arriba) y bocetos acotados a mano.
2. **Procesamiento en Go puro** en un paquete nuevo `backend/imagetools`: PNG, JPEG y GIF de la biblioteca
   estándar; WebP con `golang.org/x/image/webp` (Go puro). Límites duros: archivo ≤ 20 MB, ≤ 40 MP decodificados
   (se comprueba con `image.DecodeConfig` **antes** de decodificar), se trabaja reducido a ≤ 2048 px de lado,
   contexto con plazo (10 s por operación). Nada de CGO.
3. **Las imágenes del chat viven en el workspace**: al enviar, cada imagen se copia a
   `<ws>/refs/images/<slug>-<hash8>.<ext>` (deduplicada por hash; nunca pisa). La ficha del adjunto guarda esa ruta
   relativa (`refPath`). Hermes recibe en el texto del turno la lista de rutas; los CLIs reciben esas rutas (dentro
   del workspace, sin `ReadDirs` extra) y el prompt las nombra. `list_refs` las lista con `kind: "image"`. Así no se
   pierden entre turnos ni entre proveedores.
4. **Herramientas** (catálogo único de `mcp.Toolbox`/`--mcp`; clasificar en `clichat/modes.go` y `effects.go`):

| Herramienta | Modo | Argumentos | Devuelve |
|---|---|---|---|
| `image_info` | L | `image` (ruta en refs/), `region?` `[x,y,w,h]` en fracciones 0–1, `enhance?` | ancho/alto px, proporción, formato, colores dominantes, caja del primer plano (fracciones); imagen: la región ampliada y realzada (contraste) o la imagen reducida |
| `compare_with_image` | L | `image`, `file?` (def. el más nuevo), `view?` (`auto` def. o una de las 8 vistas o `rotate`), `mode?` `side` (def.) \| `overlay` | imagen PNG lado a lado (o la silueta del modelo superpuesta, alineada por caja), `view` elegida, proporción ancho/alto del modelo en esa vista y del primer plano de la foto, `silhouetteIoU` (0–1) |
| `trace_image_outline` | E (escribe en refs/) | `image`, `width_mm` \| `height_mm` \| `mm_per_px`, `threshold?` (def. Otsu), `invert?`, `layers?` (1 def.; 2–6 = separa por color, un SVG por color), `simplify_mm?` (def. 0,1), `min_area_mm2?`, `name?` | `refs/<name>.svg` (uno por capa), contornos con agujeros (relleno evenodd) en mm, Y hacia arriba; área, caja en mm, nº de contornos; snippet `linear_extrude(h) import("refs/x.svg");` y, si es chico, `polygon(points, paths)` |
| `image_to_heightmap` | E | `image`, `width_mm`, `height_mm?` (def. por proporción), `max_height_mm`, `base_mm?` (def. 0,6), `invert?` (litofanía: oscuro = grueso), `resolution?` (def. 200, máx. 500 columnas) | `refs/<name>.dat` para `surface()` (valores 0–100) y snippet `scale([…]) surface(file="refs/x.dat", center=true);` + base |

   Cómo elige `auto`: la vista de las 8 cuya silueta renderizada da mayor IoU con el primer plano de la foto tras
   normalizar ambas cajas. El primer plano se separa por el color del borde de la foto (fondo) con umbral.
   Trazado: escala de grises → umbral (Otsu) → apertura/cierre morfológico → descarte de motas (`min_area`) →
   marching squares con agujeros → Douglas-Peucker → px a mm. Capas por color: k-medias en RGB (semilla fija,
   determinista) excluyendo el fondo.
5. **UX**: botón **«Crear desde una imagen»** en el estado vacío del chat y en el del workspace (junto a
   `StarterAction`): diálogo con una o varias fotos (elegir, pegar, arrastrar), **medida de referencia** opcional
   («el ancho es 80 mm», «hay una moneda de 25 centavos») y **qué es el objeto**; arma un mensaje estructurado,
   pone la conversación en Agente y lo envía. La tarjeta de paso de `compare_with_image` muestra la imagen grande
   con las proporciones y el IoU; `trace_image_outline` muestra la silueta trazada. Si el modelo elegido **no tiene
   visión** y hay imágenes: aviso con **«Cambiar a un modelo con visión»** (abre el selector filtrado) y **«Usar
   trazado de contorno»** (envía igual con la instrucción de usar `trace_image_outline`, que no necesita visión).
6. **Prompts y reglas**: el prompt de Hermes y el de agente (`assets/prompts`) y `RULES.md.tmpl` nombran la skill
   `openscad-from-image` y el flujo cuando el turno trae imágenes (`prompts.Context.Images []string`, rutas
   relativas al workspace).

## Tareas

| # | Tarea | Dueño | Alcance |
|---|---|---|---|
| 15.1 | `backend/imagetools` (decodificar con límites, info/recorte/realce, primer plano, trazado y SVG, capas por color, heightmap, composición lado a lado/superpuesta) + las 4 herramientas en `backend/mcp` + `list_refs` con imágenes + `refmodel` (resolver imágenes en `refs/` y `refs/images/`) + `modes.go`/`effects.go` (puntual) + skill `openscad-from-image` (con `references/` de medidas estándar y ejemplos `.scad` renderizados) + `prompts.Context.Images` y plantillas de prompts + `RULES.md.tmpl` + `claude/13`, `claude/25`; tests sintéticos (círculo, rectángulo con agujero, logo de dos colores: área y medidas en mm; heightmap; composición) | openscad-dev | `backend/imagetools/`, `backend/mcp/` (salvo `env.go`), `backend/refmodel/`, `backend/clichat/modes.go` (solo listas), `backend/prompts/`, `assets/` |
| 15.2 | Imágenes a `refs/images/` al enviar (`app_chat_attach.go`, ficha `refPath` en `hermes`), rutas en el turno de Hermes y en los CLIs (`app_chat_cli.go`, `cliTurn`), `chatPromptContext` llena `Images`, diálogo «Crear desde una imagen» y sus entradas, tarjetas de comparación y trazado, aviso sin visión con sus dos acciones, i18n, banco + capturas es/en (`chat-from-image`, `chat-compare`, `chat-trace`), ayuda `index.html`/`es/index.html`, CHANGELOG, `claude/28` + fila en `CLAUDE.md`, `claude/19`; test de la ruta de imágenes en los CLIs | fullstack-dev | `app_chat*.go`, `backend/hermes/`, `frontend/src/`, `scripts/help-shots.sh`, `docs/screenshots/`, `index.html`, `es/index.html`, `CHANGELOG.md`, `claude/19`, `claude/28-*`, `CLAUDE.md` |
| 15.3 | Integración, verificación y humo: servidor simulado que adjunta una foto sintética de una pieza → checkpoint renderizado + paso `compare_with_image` | architect | — |
| 15.4 | Probar con un modelo real con visión y fotos reales | usuario | — |

15.1 ∥ 15.2. Contrato entre los dos: los nombres, argumentos y resultados de la tabla de arriba y
`prompts.Context.Images` (lo agrega 15.1; 15.2 lo llena — si al terminar 15.2 el campo todavía no existe, lo
conecta el arquitecto).

## Criterio de terminado

Mismo que S14: `gofmt`, build, vet, `go test -race`, build de Windows, `pnpm build` y `pnpm test`, i18n-check,
`uishot-fit.sh`, `uishot-persist.sh`, `wails build`; capturas miradas; nada de commits; `SCADLAB_DATA_DIR`
temporal; ningún slicer abierto.
