# Crear un modelo desde una imagen (S15)

El usuario adjunta fotos y la IA modela a partir de ellas. Spec: [.claude/specs/s15-modelo-desde-imagen.md](../.claude/specs/s15-modelo-desde-imagen.md).
Este segmento cubre **el camino de las fotos y la interfaz** (15.2); las herramientas y el procesamiento de imagen
están en [13-mcp.md](13-mcp.md) y [25-herramientas-ia.md](25-herramientas-ia.md) (`backend/imagetools`, 15.1) y el método en la
skill `openscad-from-image` (`assets/skills/`). Adjuntos en general: [19-hermes.md](19-hermes.md#adjuntos-backendhermesattachmentsgo-app_chat_attachgo).

## El camino de una foto

```
elegir / pegar / arrastrar → AddChatAttachment*  → <datos>/chat-attachments/staging/<id>.<ext>   (chip pendiente, en memoria)
enviar (SendChatMessage)   → hermes.PrepareIn
                               1. copia a <ws>/refs/images/<slug>-<hash8>.<ext>      (CopyImageToRefs)
                               2. mueve la de staging a <datos>/chat-attachments/<chatId>/        (miniatura e historial)
                               3. arma el texto: lo escrito + informes de modelos 3D + línea con las rutas
```

- **`refs/images/` no se versiona de otra forma que `refs/`**: el `.gitignore` del workspace no ignora `refs/` (las referencias
  3D tampoco), así que las fotos quedan en el repo del usuario si lo usa. Es lo que ya pasaba con las referencias.
- **Nombre y deduplicación** (`backend/hermes/imagerefs.go`): `<slug del nombre>-<8 hex del SHA-256>.<ext>` con `workspace.Slug` (sin acentos,
  `[a-z0-9-]`, ≤ 40). La misma foto = el mismo archivo (se reutiliza); dos fotos distintas con el mismo nombre no se pisan. Si un archivo con ese
  nombre existe con **otro** contenido (colisión de 8 dígitos), se alarga el hash a 16 y a 64: nunca se sobrescribe.
- **Escritura atómica**: un temporal en la misma carpeta (`.tmp-*`) y `os.Link` al nombre final, que falla si ya existe; en un sistema de
  archivos sin enlaces duros se comprueba y se renombra. El temporal se borra siempre. Probado con 8 goroutines escribiendo la misma foto.
- **`refs/` como enlace hacia afuera**: `imagesDir` resuelve `refs/` **antes** de crear nada debajo y se niega si sale del workspace.
- **Dos pasadas en `PrepareIn`**: primero copia **todas** las fotos a `refs/images/` (repetible sin consecuencias) y recién después mueve las de
  staging. Si una foto ya no está, ninguna se movió y el envío se puede reintentar con las demás intactas.
- **La ficha** (`hermes.AttachmentMeta`, los `parts` del mensaje) lleva `refPath` también en las fotos (antes solo los modelos). El chip pendiente
  **no** lo tiene: la copia se hace al enviar, no al adjuntar. `referencePaths` sigue filtrando por `Kind == model`.
- **Quitar un chip antes de enviar** no toca `refs/images/` (todavía no existe). **Borrar el chat** tampoco: las fotos son del proyecto.

## Qué recibe el modelo

| Proveedor | Cómo recibe las fotos |
|---|---|
| Hermes (servidor) | la imagen sigue viajando como imagen desde `<datos>/chat-attachments/<chat>/` (si el modelo ve) y el **texto** del turno cierra con `[Fotos de este mensaje, en el proyecto (pasa estas rutas a las herramientas de imagen): refs/images/a.png, …]` (`hermes.ImagePathsNote`) |
| CLI | `cliImages` (`app_chat_cli.go`): las rutas **absolutas** de `refs/images/` van en `clichat.Turn.Images` (Codex `-i`; los demás las nombran en el prompt) y la misma línea relativa va en el texto. Están dentro del `cwd`: **no hace falta `--add-dir`**. La vista previa de un modelo 3D adjunto sigue en la carpeta del chat y por eso su carpeta sigue como `ReadDirs` (`readDirsFor` omite lo que está dentro del `cwd`) |
| Turnos siguientes | `hermes.AttachmentNote` (lo que reemplaza a los adjuntos del historial) escribe `nombre.png (refs/images/…)`: el modelo puede volver a usar una foto de un mensaje anterior |

`chatPromptContext(w, currentFile, images...)` recibe las rutas del turno (`chatJob.imageRefs`) y las pone en `prompts.Context.Images` (15.1): el prompt de
Hermes y el de un CLI las nombran y mandan a la skill `openscad-from-image`. Solo las del mensaje que se envía; las anteriores quedan en el historial
(`AttachmentNote`).

## «Usar trazado de contorno» (modelo sin visión)

`SendOptions.ImagesAsFiles` (bool, `imagesAsFiles` en JSON): el envío **no** pasa por `hermes.CheckVision` aunque el modelo haya informado que no ve, y
`PrepareOptions.ImagesAsFiles` deja las fotos **fuera de `Prepared.Images`** (solo quedan como archivo en `refs/images/` y en la línea de rutas). La interfaz
le añade al texto la instrucción de usar `trace_image_outline` (`chat.noVision.traceInstruction`). Solo lo mira un servidor de modelos: un CLI recibe
siempre las rutas y decide él.

## Interfaz (`components/chat/`)

- **`FromImageDialog`** (`components/chat/FromImageDialog.tsx`, texto en `t.fromImage`): reutiliza `useChatAttachments({imagesOnly: true})`
  (diálogo del sistema solo con fotos —`ChooseChatImages`—, un modelo 3D pegado se rechaza). Las fotos se **suben a Go al elegirlas**; Cancelar las suelta
  (el desmontaje borra los pendientes) y Crear las **entrega al chat sin borrarlas** (`consume` antes de desmontar).
- **Puente** (`lib/sessionBridge.ts`: `requestFromImage` / `onFromImage` / `takePendingFromImage`): el diálogo vive también en el estado vacío del workspace
  (`StarterAction`, en el visor y en Código), donde el chat ni está montado; el panel entra con `lazy()`, así que el pedido queda pendiente hasta que se monta.
- **`ChatPanel`** recibe el pedido: conversación nueva en **Agente** (`setChatId(null)`, `setDraftAgent(true)`), el texto en la caja, las fotos como chips (`addReady`)
  y un **envío automático** (efecto sin dependencias que espera proveedor, modelo y visión conocidos; se rinde a los 5 s y deja todo en la caja). Con un modelo que
  **informó que no ve** no envía solo: queda todo en la caja y aparece el aviso.
- **`lib/fromImage.ts`** arma el mensaje: intro con la skill `openscad-from-image`, «Objeto» y «Medida de referencia» (o la indicación de identificar el objeto /
  estimar la escala y decir qué se supuso). Sin `fmt` de `../i18n` a propósito: lo prueba `scripts/chat-ux.test.mjs` con Node.
- **`NoVisionNotice`** (sobre el compositor, cuando `attach.blockedCount > 0`): «Cambiar a un modelo con visión» (sube `visionSignal`: `ModelPicker` se abre solo con
  `visionOnly`) y «Usar trazado de contorno» (`doSend(true)`). `ModelPicker` ganó el botón **«Solo con visión»** (filtro a `m.vision`; sin «Por defecto» mientras filtra).
- **`EmptyChat`** (`onFromImage`) y **`StarterAction`** (dos botones) abren el diálogo.

## Tarjetas de paso (`ToolCallCard.tsx`)

Íconos y etiquetas de las cuatro herramientas en `ICONS` y `steps.tools` (`image_info`, `compare_with_image`, `trace_image_outline`, `image_to_heightmap`); las dos
últimas están en `WRITES` (escriben en `refs/`). La tarjeta lee el JSON del resultado **con tolerancia** (varios nombres, nada obligatorio; lo que falte no se pinta y el JSON
crudo sigue en el detalle):

| Herramienta | Campos que lee | Qué muestra |
|---|---|---|
| `compare_with_image` | `view`, `modelAspect` (o `aspect.model`), `photoAspect` (o `aspect.photo`), `silhouetteIoU` | la imagen **grande** (`step.images[0]`, ancho completo, ampliable), pastillas de vista, ancho/alto del modelo y de la foto, e IoU (verde ≥ 0,85; ámbar < 0,6) |
| `trace_image_outline` | `files` (o `svg`/`file`), `contours`, `areaMm2`, `sizeMm` (o `bboxMm`: `[w,h]`, `[x,y,w,h]` u objeto), `layers` | la silueta (`step.images[0]`, 144 px) y los SVG creados en `refs/` |
| `image_info`, `image_to_heightmap` | — | tarjeta normal; si `image_info` devuelve imagen, la miniatura de siempre |

**Contrato con 15.1**: la silueta del trazado llega como imagen del paso (como `preview`); sin imagen, la tarjeta muestra solo las pastillas y los archivos.

## Banco y capturas

Vistas `chat-from-image` (el diálogo con dos fotos, la medida y qué es), `chat-compare`, `chat-trace` y `chat-no-vision` en `frontend/src/uishot.tsx`; las imágenes
sintéticas (una escuadra en L con una moneda, su comparación, un logo y su trazado) son PNG de `frontend/public/uishot/` (generados una vez con un programa de Go
con solo la biblioteca estándar; **nunca fotos reales**), que el build borra. `lib/fromImageShot.ts` deja el diálogo abierto y relleno sin simular clics. Filas en
`scripts/help-shots.sh` y las cuatro vistas en `scripts/uishot-fit.sh`.

## Límites conocidos

- Una foto del chat se acepta hasta **20 MB** (el tope de `imagetools`) y **se reduce al adjuntar** (`hermes.ReduceImage`): al modelo va una copia de lado ≤ 2048 px y
  ≤ 5 MB (el tope de la API de Anthropic; PNG si tiene transparencia y cabe, si no JPEG), y a `refs/images/` va el **original**, deduplicado, que es el que leen las herramientas.
- `refs/images/` crece sin tope: no hay purga automática (son archivos del usuario).
