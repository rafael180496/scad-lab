# Changelog

Todos los cambios visibles de ScadLab. Formato [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/),
versionado [SemVer](https://semver.org/lang/es/); la versión vigente está en `VERSION`.
Todo cambio se acumula primero en `[Unreleased]` y se vuelca a su versión al
empaquetarla (ver `.claude/specs/releases.md`).

## [Unreleased]

## [0.2.2] - 2026-10-20

### Agregado

- **Un Ollama de poco contexto ya puede modificar un modelo.** Un modelo local de 8 000 o
  32 000 tokens no entraba con el catálogo completo de herramientas (más de 10 000 tokens
  solo en descripciones), unas instrucciones largas y el código de una maceta de 600
  líneas: medido con un Ollama simulado de 8k, **cada vuelta pesaba entre 17 900 y 28 500
  tokens** contra una ventana de 8 192; ahora, entre 1 900 y 3 500. El chat con un servidor
  de modelos se adapta a la ventana del modelo:
  - **Herramientas esenciales.** Un selector nuevo en la barra del chat —«Auto (esenciales)»,
    «Esenciales» o «Todas», por conversación— elige qué herramientas se le declaran al
    modelo. Las esenciales son las once que alcanzan para modificar un modelo (leer por
    partes, buscar, editar unas líneas, parámetros, render, vista y medición), con
    descripciones cortas y unas instrucciones compactas. «Auto» las usa cuando la ventana del
    modelo es menor de 32 768 tokens y el catálogo completo si es mayor o no se sabe.
  - **La ventana de Ollama sale del modelo.** Si no la fijas en Configuración, ScadLab le pide
    a Ollama la del modelo (con tope de 32 768 y mínimo de 8 192) en lugar de los 16 384
    fijos de antes; sin esto Ollama recorta el comienzo del prompt sin avisar.
  - **El turno no acumula contexto de más.** Lo que cada herramienta le devuelve al modelo
    tiene un tope proporcional a la ventana; una lectura repetida del mismo archivo que no
    cambió se reemplaza por una nota con su versión; los resultados viejos y el código que
    el modelo mandó en llamadas anteriores se reducen cuando aprieta el contexto. Entre
    turnos, lo viejo que ya no cabe se reduce a un resumen automático —qué se pidió, qué se
    contestó, qué archivos se tocaron— y los últimos mensajes viajan enteros.
  - **Avisos y «Nueva conversación con resumen».** El contador de contexto mide contra la
    ventana de verdad y suma el prompt y las herramientas de cada vuelta. Si estos solos pasan
    del 60 % de la ventana, el chat lo dice y ofrece usar las esenciales; al llegar al 70 % con
    la conversación, ofrece un botón que le pide al modelo un resumen de unos 300 tokens y
    abre una conversación nueva —con el mismo proveedor, modelo, modo y herramientas— que
    empieza con él (si el modelo falla, el resumen es uno automático). La anterior se conserva.
- **Tiempo y tokens de cada paso y de cada turno.** Cada tarjeta de herramienta muestra, además
  de su duración, `ctx 7,2k` (el contexto de la vuelta que la pidió) y `+880` (lo que le
  devolvió al modelo), y el pie de cada respuesta dice «Contexto máx. 12k · ↑ 48k en 5
  vueltas · ↓ 2k · modelo 41 s · herramientas 6 s». **El ↑ ya no se lee como el contexto**:
  es la suma de la entrada de todas las vueltas (cada una reenvía el contexto entero) y el
  contexto de verdad es el máximo; con un CLI, ↑ ahora cuenta también lo que leyó de la
  caché, y Claude Code (y Antigravity, si su paso lo informa) aportan el contexto máximo y las
  vueltas. Lo que el proveedor no informa no se muestra. Un turno que termina sin escribir nada
  —un modelo chico sin contexto, Antigravity al primer rechazo— cuenta qué herramientas ejecutó,
  qué archivos quedaron cambiados y qué devolvió la última, en lugar de «terminó sin responder».

- **La IA cambia unas líneas de un modelo sin regenerarlo entero.** Medido con Antigravity,
  cada cambio a una maceta de 600 líneas pasaba por reescribir el archivo completo y tardaba
  entre 1 min 42 s y 3 min 39 s, con 105 000 a 192 000 tokens de contexto por paso. Ahora la
  IA se orienta y edita por partes, con las herramientas nuevas `outline_checkpoint` (el mapa
  del archivo: parámetros por sección, módulos con sus líneas), `find_in_checkpoint` y
  `read_checkpoint` con `from`/`to`, y cambia solo lo que toca con `edit_checkpoint` (el
  texto exacto o un rango de líneas; si una edición falla o dos se pisan no se escribe nada, y
  el error le dice cuál es la línea más parecida). Una medida, un color o un texto del
  Customizer lo cambia `set_parameters`, que ahora edita el mismo archivo (con su respaldo en el
  historial) en vez de crear otro; «una versión más ancha» sigue siendo un hijo con
  `variant`. `update_checkpoint` queda para reescrituras grandes.
  - **Respuestas cortas.** `render`, `lint`, `measure` y las que editan devuelven un resumen
    (estado, caja, volumen, ¿cabe?, partes por color y hasta 5 errores con su línea) en vez
    de varios KB de JSON, `preview` manda una sola vista de 384 px salvo que pidas más, y
    ninguna herramienta pasa de 6 KB sin pedirlo (`verbose`). Una prueba corre todas las
    herramientas sobre una maceta de 596 líneas y falla si una se pasa.
  - **Modelos grandes en archivos de `lib/`.** `extract_module` mueve módulos a
    `lib/<nombre>.scad` y deja el `include`; renderiza antes y después y, si la caja o el
    volumen cambian, lo deshace y te lo dice. Los archivos de `lib/` se leen y se editan como un
    checkpoint, con su propio historial (las librerías instaladas, como BOSL2, siguen de solo
    lectura).
  - **Perfil esencial para modelos chicos.** Las once herramientas que alcanzan para modificar
    un modelo, con descripciones cortas, pesan unos 1 100 tokens contra unos 11 200 del
    catálogo completo, y hay instrucciones compactas (unos 700 tokens contra unos 6 200 del
    prompt de siempre con el código abierto).

### Corregido

- **El editor de código mostraba la versión vieja después de que la IA editara el archivo.**
  Cuando la IA cambiaba un `.scad` con `update_checkpoint` (o se restauraba una versión
  del historial), «Ver diferencias» y el visor ya tenían el código nuevo, pero el panel
  Código seguía mostrando el anterior: solo recargaba el texto al cambiar de archivo. Ahora
  se actualiza solo, sin perder el scroll ni el cursor, siempre que no estés editando a
  mano (con cambios sin guardar, tu borrador manda y aparece el aviso de siempre).
- **El 3MF multicolor llegaba a Bambu Studio sin colores ni extrusores.** Un modelo de dos
  colores (una maceta y su texto en relieve) se abría como una sola pieza en el filamento 1.
  La causa, leída del código de Bambu Studio 02.08.02.61: ScadLab escribía el color en
  `<basematerials>`, que Bambu Studio no lee (solo lee `<m:colorgroup>`), así que no había
  colores que emparejar con los filamentos y el extrusor 2 de la segunda parte se descartaba
  cuando el proyecto tenía un solo filamento. Ahora cada parte lleva su `<m:colorgroup>`: al
  abrir el archivo, Bambu Studio muestra su ventana de colores del 3MF («Standard 3mf Import
  color») con los colores del modelo —por defecto los agrega como filamentos nuevos («Append»);
  «Color match» los empareja con los del AMS— y asigna el extrusor de cada parte al aceptar. El aviso «El archivo 3mf tiene una configuración no válida, carga solo los datos de
  geometría» de la 02.08.02 es un defecto de esa versión con cualquier 3MF de otro fabricante
  (se corrige en la 02.08.03): el archivo carga igual, acéptalo. Orca, Creality Print y
  Anycubic siguen leyendo las partes y extrusores de `model_settings.config`.
- **Un modelo de varios colores que se tocan se abría en Bambu Studio con el hueco relleno y
  llenó de soportes.** Pasaba con una maceta y su texto y marco en relieve de otro color, y con
  la plantilla de placa de dos colores: cualquier color apoyado en otro. La causa, medida pieza
  por pieza: OpenSCAD funde todo lo del nivel superior en un solo sólido antes de exportar, y la
  cara por la que dos colores se tocaban desaparece, así que al repartir el modelo por color
  cada parte quedaba como una cáscara abierta (109 aristas sin cerrar en cada parte de la
  maceta de prueba, 226 en la tuya). Bambu Studio lamina cada parte por separado y una cáscara
  abierta se rellena mal. No era el orden de las caras, ni las coordenadas, ni el tipo de parte:
  esos salían bien. Ahora, si las partes salen abiertas, ScadLab renderiza otra vez con
  `--enable=lazy-union` y cada color llega como un sólido cerrado, con el volumen y la caja de
  ese color solo; el motor integrado lo hace igual y la vista de partes del visor también. Si
  el color está dentro de un `union()` o de un módulo que contiene varios `color()`, no hay
  forma de separarlo sin perder esa cara: la exportación lo avisa («no es un sólido cerrado») y
  las skills piden poner cada `color()` en el nivel superior del archivo. Si ya exportaste un
  modelo así, vuelve a exportarlo.
- **En Antigravity las tarjetas decían `call_mcp_tool` y la shell terminaba el turno sin
  explicación.** Antigravity 1.4.x manda el nombre de la herramienta de ScadLab dentro de
  `tool_info.parameters`, no donde ScadLab la buscaba: ahora cada tarjeta es la herramienta
  que es (`render`, `edit_checkpoint`…) y ScadLab reconoce un checkpoint creado. Y cuando el
  modelo intenta ejecutar un comando de shell (`run_command`), Antigravity —que en modo no
  interactivo lo rechaza— termina el turno: el chat ahora le pide al modelo que no lo intente
  y, si lo intenta, nombra el comando bloqueado y dice que no se ejecutó nada. No se usa
  `--sandbox`, que ejecutaría los comandos en vez de bloquearlos, ni reglas globales de shell.

## [0.2.1] - 2026-10-20

### Agregado

- **La IA edita el archivo que le nombras, con historial y deshacer.** Hasta ahora, si le
  decías a la IA «es en el archivo `002-rpi5-snap-case.scad` que quería los cambios», no lo
  tocaba: contestaba que los checkpoints son inmutables y creaba uno hijo
  (`004-rpi5-snap-case-vents.scad`), con lo que el árbol se llenaba de copias de algo que
  querías cambiar en el sitio. Ahora, cuando **nombras un archivo que existe** o te refieres al
  que tienes **seleccionado** («este», «el actual») y pides cambios, la IA **edita ese archivo**
  con una herramienta nueva, `update_checkpoint`; solo crea un checkpoint nuevo si pides algo
  nuevo, una variante u «otra versión». Vale para todos los proveedores: Claude Code, Codex,
  Antigravity, opencode y el chat con Ollama o una API (Hermes), y solo en modo Agente.
  - **Antes de escribir se guarda la versión anterior.** Cada archivo editado tiene su historial
    en `.scadlab/history/<archivo>/`, con la fecha, el motivo (IA o restaurado) y la nota que la
    IA dejó sobre el cambio. Se conservan hasta **20 versiones por archivo** (se ajusta de 1 a 200
    en Configuración → General → «Versiones guardadas por archivo»); al pasar el tope se descartan
    las más viejas de ese archivo, nunca un checkpoint. La cabecera `// @checkpoint` del archivo
    se conserva, y la papelera se lleva el historial con el archivo y lo devuelve al restaurarlo.
  - **«Editado por la IA».** Un archivo cuyo último cambio fue de la IA lleva un ícono en el árbol
    y una pastilla en las cabeceras del visor y del código, con tres acciones (también en el menú
    del nodo): **Deshacer cambio de la IA** (vuelve a la versión anterior, con confirmación; lo que
    había se guarda, así que deshacer también se puede deshacer), **Ver diferencias** (la versión
    anterior y la actual lado a lado, con su fecha y la nota) e **Historial** (todas las versiones,
    con «Ver diferencias» y «Restaurar esta versión» en cada una).
  - **El visor y el código se enteran solos.** Cuando la IA —o el agente de la terminal, o una
    restauración— cambia el archivo que tienes abierto, el código se recarga y se renderiza de
    nuevo sin que toques nada. Si tienes cambios sin guardar en el editor no se pisan: aparece un
    aviso con un botón «Recargar» que pregunta antes de descartarlos.
  - **Lo que no cubre.** Si un agente escribe un `.scad` con sus propias herramientas de archivo o
    con la shell (`Edit`, `Write`, `apply_patch`…), no queda respaldo: las reglas del workspace lo
    prohíben, pero ScadLab no puede impedirlo. El editor de código sigue en solo lectura;
    guardar desde él sigue creando un checkpoint nuevo.

### Corregido

- **Los CLIs ya no inventan la ruta del archivo.** El turno de un CLI se ejecuta en la carpeta del
  proyecto, pero el prompt solo decía «esta carpeta»: las herramientas de archivo de Antigravity
  piden una ruta **absoluta** y el modelo no tenía cómo saberla, así que la inventaba y la lectura
  fallaba con «no existe» (de ahí, con toda probabilidad, la respuesta sobre los checkpoints
  inmutables). Ahora cada turno dice la ruta completa del proyecto,
  cuál es el checkpoint seleccionado en la app («es “este archivo”») y que un `.scad` se lee con
  `read_checkpoint` y se cambia con `update_checkpoint`, nunca con las herramientas de archivo ni la
  shell del CLI.

- **Los enlaces a la ayuda y a las descargas daban 404.** Apuntaban a
  `rafael180496/ScadLab`, pero el repositorio se llama `scad-lab`: la ayuda está en
  https://rafael180496.github.io/scad-lab/ y las descargas en sus Releases. Corregidos
  en el README, la ayuda (en y es), los README de `releases/` y las notas que genera
  el workflow de release.

## [0.2.0] - 2026-10-19

### Agregado

- **Crear un modelo a partir de una imagen.** Dale a ScadLab una o varias fotos de un
  objeto —o un boceto con cotas— y la IA lo modela en OpenSCAD: averigua qué es y a qué
  escala, lo arma con formas simples, lo renderiza y **lo compara con tu foto** hasta que
  coincide. Sigue la skill incluida `openscad-from-image` y funciona con cualquier
  proveedor del chat, Hermes o un CLI.
  - **«Crear desde una imagen»**, en el chat vacío y, en un proyecto sin ningún modelo, junto a
    «Crear un modelo de inicio». Abre un diálogo donde eliges las fotos (con el selector,
    pegándolas o arrastrándolas; hasta 10), escribes una **medida de referencia** opcional
    («el ancho es 80 mm», «hay una moneda de 25 centavos junto a la pieza») y **qué es** el
    objeto. Al confirmar abre una conversación nueva en modo Agente con las fotos adjuntas y
    un mensaje ya armado, y lo envía.
  - **Las fotos viven en el proyecto.** Al enviar, cada foto se copia a
    `refs/images/<nombre>-<código>.<ext>`: la misma foto enviada dos veces es un solo
    archivo y nunca se pisa nada. Ahí la leen las herramientas de imagen, y el modelo recibe la
    ruta al final del mensaje (los CLIs las abren desde su carpeta de trabajo, sin permisos
    extra); no se pierden entre mensajes ni al cambiar de proveedor.
  - **Fotos grandes, sin rechazos.** Una foto del teléfono (más de 5 MB o de 2048 px) ya no se
    rechaza: se acepta hasta 20 MB y al modelo le llega una copia reducida, mientras que
    `refs/images/` guarda el original entero para que el trazado y la comparación trabajen con
    todo el detalle.
  - **Cuatro herramientas nuevas** para la IA (también en el servidor MCP): `image_info` (lee
    la foto, y amplía y realza una región), `compare_with_image` (la foto y el modelo lado a lado
    desde la misma vista, con las proporciones y el solapamiento de siluetas),
    `trace_image_outline` (la silueta a un SVG en milímetros, para placas y logos) e
    `image_to_heightmap` (relieves y litofanías). Sus pasos en el chat son tarjetas: la
    comparación muestra su imagen en grande con la vista, las dos proporciones y el IoU; el
    trazado, la silueta y los archivos que creó.
  - **Si el modelo no ve imágenes**, en vez de un «Enviar» apagado aparece un aviso con dos
    salidas: **«Cambiar a un modelo con visión»**, que abre la lista de modelos ya filtrada a
    los que ven (el filtro «Solo con visión» también está a mano en el selector), o **«Usar
    trazado de contorno»**, que envía igual sin mostrarle la foto al modelo y le pide usar
    `trace_image_outline`, que no necesita visión (sirve para siluetas y logos).

- **Impresoras de siete marcas y configuración propia de cada workspace.** ScadLab ya no es
  solo para Bambu Lab: el catálogo trae **Bambu Lab, Prusa, Creality, Elegoo, Anycubic,
  Snapmaker y UltiMaker**, con los datos de la ficha oficial de cada modelo (volumen,
  boquilla, filamento, temperaturas máximas, cámara, superficie de la cama y sistema
  multicolor: AMS, MMU3, CFS, ACE, cabezales independientes…). Lo que no se pudo confirmar
  no se inventa: la impresora lleva la marca «por confirmar» y su ficha dice qué falta.
  - **Elegir la impresora** con un selector con búsqueda, agrupado por marca, que muestra
    el volumen X×Y×Z y los colores máximos de cada una. Está en **Nuevo workspace**, en la
    nueva **Configuración del workspace** y en Configuración → Impresión (la impresora de
    los workspaces nuevos); se maneja con flechas, Enter y Esc.
  - **Cada workspace tiene lo suyo.** «Nuevo workspace» pide además cuántos **colores o
    extrusores** usa a la vez (de 1 al máximo de la máquina; no aparece si no es multicolor) y
    qué **skills** instalar: vienen marcadas las encendidas en Configuración → Skills más
    **la skill de la marca de tu impresora** (una skill por marca: `prusa-printers`,
    `creality-printers`, `elegoo-printers`, `anycubic-printers`, `snapmaker-printers`,
    `ultimaker-printers` y la de Bambu de siempre), y puedes cambiar lo que quieras.
  - **Configuración del workspace**, con el engranaje junto al workspace en la barra lateral
    o con un clic en la impresora de la barra de estado (que ahora dice «Impresora · N
    colores»): la impresora y su ficha, las skills (con «Restablecer por defecto»; quitar una
    retira su copia solo si nadie la editó, y si la editaste se queda y te avisa cuál) y los
    ajustes de impresión —material, relleno, perímetros, ancho de línea y filamento—, cada uno
    con el valor que hereda de Configuración → Impresión y «Usar el de Configuración» para
    volver a él. El filamento es propiedad de la máquina (una UltiMaker usa 2,85 mm). Guardar
    reescribe las reglas para la IA (`CLAUDE.md`, `AGENTS.md`, `printer.json`) y sincroniza las
    skills de ese workspace; el panel **Impresión** parte de lo que rige en él, no de lo global.
    La IA también puede ver y cambiar la impresora desde el chat y la pantalla se entera sola.
  - **Configuración → Impresión** pasa a ser el valor por defecto de los workspaces nuevos, y
    **Configuración → Skills** el conjunto con que nacen (las de marca se eligen por la
    impresora). Cambiar cualquiera de las dos **no toca los workspaces que ya existen**: los
    tuyos conservan su impresora y su cantidad de colores (una P1S o una X1C guardada sigue
    con 16, no baja a 4).
  - **Abrir en tu slicer.** «Abrir en Bambu Studio» pasa a **«Abrir en &lt;slicer&gt;»**: el primero
    instalado de los slicers de la impresora del workspace (PrusaSlicer, OrcaSlicer, Creality
    Print, ElegooSlicer, Anycubic Slicer Next, Snapmaker Orca, UltiMaker Cura o Bambu Studio), en
    el diálogo de exportar y en el aviso tras guardar. Sin ninguno instalado el botón se ve
    apagado y dice cuál falta. Configuración → Impresión → **Slicers** lista los ocho con lo
    detectado y deja fijar la ruta de cada uno («Elegir…» y «Usar la detección»); la ruta de
    Bambu Studio que tenías pasa sola a su fila. ScadLab abre la aplicación con el archivo y nunca
    el slicer por su línea de comandos.
  - **3MF para PrusaSlicer.** PrusaSlicer no lee los extrusores del 3MF de Bambu y Orca, así que
    el diálogo de exportar suma el formato **«3MF para PrusaSlicer»**, que arranca elegido si el
    slicer de tu impresora es PrusaSlicer. Con UltiMaker Cura, que no asigna extrusores desde un
    3MF de partes, el formato que arranca elegido es **un STL por color**.
- **Eliminar para siempre y vaciar la papelera.** La ventana de la papelera de
  checkpoints tiene ahora una **✕ en cada fila** (elimina ese checkpoint) y el botón
  **Vaciar papelera** (elimina todo lo que haya en `.scadlab/trash/` del workspace, también
  lo que no sea un checkpoint). Las dos piden confirmación con el archivo o la cantidad y
  avisan que **no se puede deshacer**; el registro de re-colgado de lo eliminado se va con
  ello. Es lo único de ScadLab que borra de verdad, y es solo tuyo: **ninguna herramienta
  de la IA** puede eliminar para siempre ni vaciar la papelera (siguen pudiendo mandar a
  la papelera y restaurar). Vaciar la papelera de un workspace que se quedó sin modelos
  **no hace reaparecer el modelo de inicio** al reabrirlo —lo dejaste vacío a propósito—;
  el botón «Crear un modelo de inicio» sigue ahí.
- **«Hacer principal»** para un checkpoint cuyo padre ya no está (el ⚠ del árbol): clic
  derecho en el checkpoint, o el botón del aviso del panel Problemas, que además dice
  **«parent=… está en la papelera»** cuando es el caso, para que elijas entre restaurarlo o
  dejar este como principal. Solo se reescribe la línea de cabecera `// @checkpoint`.
- **Pedir una pieza en el chat termina siempre en un `.scad` renderizado y a la vista.**
  La prueba real —«una caja con tapa a presión para un Raspberry Pi 5» en un workspace
  sin nada— dejaba un diseño en texto con Codex y Claude, una tarjeta de error sin
  explicación con Antigravity y un bloque suelto con un modelo de Ollama sin
  herramientas. Ahora, con cualquier proveedor:
  - **El chat arranca en «Agente».** Con CLIs y con servidores. «Solo consulta» es una
    elección tuya, **conversación por conversación** (ver el punto siguiente). Al CLI se le ordena el flujo *crear checkpoint →
    renderizar → mirar → corregir en uno nuevo*, y en un workspace sin checkpoints, **crear
    el primero sin preguntar**; en «Solo consulta» se le pide **un** bloque completo.
    Los permisos de cada CLI en «Agente» dejan las herramientas de escritura de ScadLab
    permitidas de forma explícita (Claude Code, Codex y OpenCode), y la shell sigue denegada.
  - **El primer checkpoint no necesita padre.** «Guardar como checkpoint», «Abrir en el
    editor» y el botón nuevo crean la **raíz** del árbol en un workspace vacío, en vez de
    decir «no hay checkpoint que sirva de padre».
  - **Un workspace nunca abre vacío.** Al abrir uno sin ningún `.scad` (uno anterior a
    esta versión, por ejemplo) se escribe el modelo de inicio. Si lo vaciaste tú —con
    la papelera con algo adentro— no vuelve solo: el visor y Código ofrecen **Crear un
    modelo de inicio**. Las reglas de `CLAUDE.md` y `AGENTS.md` se vuelven a sincronizar al
    abrir si cambiaron.
  - **Guardado automático.** Si en «Agente» el turno termina sin haber creado ningún
    checkpoint pero trae un bloque OpenSCAD completo (un modelo sin herramientas, un CLI al
    que no le llegó el servidor), ScadLab lo guarda por ti —raíz si el workspace está
    vacío, hijo del archivo abierto si no— y lo renderiza, y lo muestra como un paso:
    **«Guardado automáticamente»**, con el enlace al checkpoint. Con un servidor de modelos,
    si el render falla y quedan pasos, el modelo recibe los errores **con su línea** y
    corrige en otra vuelta. Si el proveedor ya creó el checkpoint, no se duplica.
  - **Antigravity se registra solo.** Antigravity solo lee MCP de su configuración global,
    y sin ScadLab ahí intentaba la shell (denegada) y no dejaba nada. Al elegirlo en el
    chat o abrir su terminal, si `agy mcp list` no lista `scadlab`, ScadLab lo agrega con
    `agy mcp add -- scadlab <ScadLab> --mcp` y te avisa. Solo agrega lo que falta; si
    `agy` no lo acepta, el error dice por qué y el chat explica que sin ese servidor no
    puede crear checkpoints. Un turno sin texto termina con una explicación en vez de un
    mensaje vacío.
  - **En «Solo consulta»,** los bloques de código traen un botón destacado **Crear
    checkpoint y renderizar**, y si lo que escribes pide crear o cambiar algo aparece un aviso
    con **Cambiar a Agente** de un clic.
  - **Lo que nace en el chat se abre solo.** Venga de una herramienta, del guardado
    automático o de un botón, el checkpoint se selecciona en el árbol, en Código y en el
    visor, y se renderiza aunque tengas apagado el auto-render. Si hay cambios sin guardar
    en el editor, no te cambia de archivo.

- **Agente por defecto en todos lados; «Solo consulta» se elige por conversación.** ScadLab
  existe para crear y modificar modelos, así que el modo con escritura ya no es una
  preferencia que se arrastra de un chat a otro: **cada conversación tiene el suyo**.
  - **Toda conversación nueva nace en «Agente»**, con cualquier proveedor. Si eliges
    «Solo consulta», vale **solo para esa conversación**: las demás siguen como estaban y la
    siguiente que abras vuelve a empezar en «Agente». Puedes cambiarlo cuando quieras en el
    selector de modo del chat, y el aviso «Cambiar a Agente» lo guarda en esa conversación.
  - **Las conversaciones que ya tenías quedan en «Agente».** Antes ScadLab recordaba un
    único modo por tipo de proveedor (CLIs por un lado, servidores por otro), y una elección
    —o un valor de una versión anterior que no se distinguía de un default— dejaba en
    consulta chats que nadie había puesto así. Esas preferencias viejas se descartan al
    abrir la app, sin tocar tus conversaciones ni tus mensajes.
  - **La terminal de agentes y el servidor MCP no arrancan en solo lectura.** Revisado: los
    CLIs que abres desde la terminal de ScadLab (Claude Code, Codex, Antigravity, OpenCode)
    reciben solo el servidor de ScadLab, las carpetas de tus otros proyectos y la sesión que
    retomas; ningún modo plan ni sandbox de lectura, y tampoco ningún flag que se salte los
    permisos —ahí apruebas tú, en tu terminal—. `ScadLab --mcp` declara las herramientas de
    escritura por defecto; no existe un modo de solo lectura.

- **La IA ahora puede hacer todo lo que haces tú con la interfaz.** ScadLab existe para
  hacer modelos sin saber modelado 3D, así que la IA tiene las mismas herramientas que tú,
  con cualquier proveedor (Hermes o un CLI). Se sumaron quince; tu chat las muestra con su
  tarjeta, y las que cambian algo van con el color de acento.
  - **Cambiar medidas sin reescribir el modelo.** *Leer parámetros* y *Cambiar parámetros*
    trabajan sobre las variables del Customizer: la IA lee el ancho, el alto o la tapa con
    su rango y sus opciones, y guarda un checkpoint **hijo** con los valores nuevos; el
    resto del archivo queda tal cual. Si pide algo fuera de rango, lo avisa.
  - **Crear piezas desde las plantillas.** *Crear desde plantilla* arranca de una de las
    plantillas de ScadLab (la perilla, la caja con tapa, el clip de cables…) con tus
    medidas ya puestas y la placa de tu impresora.
  - **Editar un STL.** *Editar el modelo 3D* hace lo que el modo Editar del visor: mover,
    rotar, escalar, espejar, centrar, apoyar en la placa, recolorear, agregar piezas,
    perforar y cortar con un plano. Parte de cualquier checkpoint y deja otro nuevo.
  - **Saber si cabe y cómo ponerla.** *Estimar la impresión* da el peso, los metros y el
    costo con el material y el relleno que prefieras, *Ver materiales* lista los que tienes
    y *Sugerir orientación* prueba las seis caras hacia abajo: cuál cabe en tu placa, cuál
    pide menos soporte y cuál queda más baja, con las operaciones listas para aplicarlas.
  - **Instalar BOSL2 y otras librerías.** *Ver librerías* dice cuáles tienes y la línea
    `include` exacta; *Instalar librería* baja BOSL2, MCAD o NopSCADlib a `lib/` de tu
    workspace, de una versión fija y comprobada con su huella SHA-256. Nunca reemplaza una
    librería que ya tienes.
  - **Ordenar el árbol, con papelera.** La IA puede *elegir el padre* («ramificar desde
    aquí»), *renombrar* un checkpoint (cambia el rótulo, no el archivo), *mandarlo a la
    papelera* —nunca borra— y *restaurarlo*; también *ver la papelera* y *ver las
    referencias* de `refs/`. Tras cualquiera de esas acciones el árbol, la papelera, los
    rótulos y el padre activo se actualizan al instante, sin esperar al vigilante de
    archivos.
  - **Cada checkpoint que nace del chat se abre solo.** Cambiar parámetros, crear desde
    una plantilla y editar un modelo dejan un checkpoint nuevo, y el chat lo trata como
    los demás: la tarjeta enlaza a él, se selecciona en el árbol, en Código y en el
    visor, y la red de seguridad del guardado automático no guarda un bloque de más.
  - En «Solo consulta» siguen disponibles todas las de lectura; las que cambian el
    proyecto, solo en «Agente».

- **Edita un STL, con el editor 3D o con la IA.** Importa un **STL, 3MF u OBJ** con el
  botón de la barra lateral (junto a los de checkpoints) o **arrastrando el archivo a la
  ventana**: un diálogo te muestra antes las medidas, los triángulos, si la malla es
  cerrada y los colores de un 3MF, te avisa de lo que pesa (una malla abierta, una pieza
  que no cabe en tu placa) y trae marcado **Centrar y apoyar en la placa**. El archivo no
  se toca nunca: se copia a `refs/` y cada cambio es **código OpenSCAD en un checkpoint
  nuevo** que lo importa, así que queda en el árbol con su padre, se deshace volviendo
  al anterior y se exporta a STL o 3MF como cualquier otro.
  - **Un modo «Editar» en el visor** (botón **Editar** de su barra), sobre cualquier
    checkpoint con render, sea una importación o un diseño tuyo. **Mover, rotar y
    escalar** con el gizmo (el cubo central escala por igual, las flechas por eje) o
    escribiendo los valores en mm y grados, con ajuste a pasos (1 mm, 15°, 0,1; los
    cambias en Propiedades) y las medidas de la caja en vivo; **espejar** en X, Y o Z,
    **centrar**, **apoyar en la placa** y **apoyar sobre una cara** (haces clic en la cara
    y el modelo gira hasta dejarla contra la placa). El gizmo gira y escala el modelo
    sobre el centro de su caja, no sobre el origen del archivo, que en un STL suele
    caer lejos de la pieza.
  - **Cortar con un plano** (lo mueves y lo inclinas con su gizmo): te quedas con lo de
    arriba, con lo de abajo o **partes la pieza en dos**, como dos objetos separados
    para imprimirlos aparte o como **dos colores** en su sitio para un 3MF multicolor.
    **Cubo, cilindro, esfera y texto** como **unión** o como **agujero** (el agujero se
    ve rojo y translúcido; el resultado real es el render al aplicar), cada uno con su
    gizmo, sus medidas y su color; también se recolorea el modelo.
  - **Una pila de operaciones** con etiquetas legibles (van como comentarios al código
    del checkpoint), con **deshacer y rehacer** (⌘Z y ⌘⇧Z; Ctrl en Windows) y vuelta a
    cualquier paso. **Ver código** muestra el OpenSCAD que se guardaría sin escribir
    nada, **Aplicar** crea el checkpoint nuevo y lo elige, y **Descartar** (o Esc) sale
    sin dejar nada, con confirmación si ya hiciste cambios. El modo Editar no se
    recuerda al cerrar la app, a propósito.
  - **Una malla abierta se avisa antes de aplicar:** se puede mover, rotar y escalar,
    pero cortarla o perforarla falla en OpenSCAD, y el editor lo dice en el visor y te
    pregunta antes de aplicar.
  - **Modificar con IA.** En el menú de un checkpoint y en la barra del modo Editar:
    abre el chat con el modelo ya adjunto y «Modifica este modelo: » escrito, para que
    termines la frase. Responde el proveedor que tengas elegido, Hermes o un CLI; en
    el chat, la herramienta nueva **Importar modelo** (`import_mesh`) tiene su tarjeta.
  - **En un panel angosto** la barra de herramientas se pliega en varias líneas, el
    panel lateral pasa a un cajón con pestañas bajo el visor y las medidas se reducen a
    la de lo seleccionado, sin controles cortados.
- **Tus CLIs de IA, en el mismo chat que tus servidores.** Claude Code, Codex,
  Antigravity y OpenCode aparecen solos en el selector de proveedor del chat si los
  tienes instalados: **no hay nada que configurar** (ScadLab los busca en el `PATH`, en
  las rutas habituales y, en macOS, en el `PATH` de tu shell de login). Al elegir uno,
  tus mensajes corren en ese CLI, en la carpeta del workspace y con las herramientas de
  ScadLab conectadas; sus pasos —render, vista previa, medición, checkpoints— llegan al
  chat con las mismas tarjetas que usa Hermes. La conversación se retoma con la sesión
  del propio CLI, y cada CLI trae su lista de modelos («Gemini 4 Argon (High)»…).
  - **Dos modos, y sin comandos de shell en ninguno.** **Solo consulta** deja que el CLI
    lea el proyecto y use las herramientas de lectura; **Agente** (el de arranque) le deja
    además crear checkpoints y exportar. Cada modo explica
    qué permite, en el selector y bajo la caja de mensaje.
  - **Cuando un CLI no está instalado, no aparece.** Y uno recién instalado se ve al
    abrir el selector, sin reiniciar. En Configuración → Proveedores de IA hay un
    bloque **CLIs detectados**, de solo lectura, con la versión de cada uno.
- **Un selector de proveedor y de modelo hechos para el chat.** El desplegable de
  servidores se reemplazó por uno propio: los CLIs detectados primero y después tus
  servidores, cada uno con su ícono, un **punto de estado** (verde responde, rojo no) y
  un check en el elegido; al final, **Agregar proveedor de IA…** te lleva a
  Configuración. Se maneja con el teclado (flechas, Enter y Esc). Junto a él, una
  **píldora de latencia** (`18 ms`, o **Sin respuesta**) que se refresca sola cada
  minuto, al cambiar de proveedor y con un clic. El selector de modelo suma la opción
  **Por defecto**, con la explicación de cuál se usa (el que tenga configurado el CLI, o
  el predeterminado del servidor), y conserva la búsqueda y las insignias de visión y
  herramientas. Tu último proveedor y modelo se recuerdan por proyecto y vuelven al
  abrirlo.
- **El panel del chat, repensado para un panel angosto.** La barra de arriba es de **una
  sola línea**: el título de la conversación (clic para renombrarlo), el proveedor, la
  latencia, el modelo, el workspace y el modo **Solo consulta / Agente**. Cuando el panel
  se achica, lo secundario —el workspace, la latencia, «Chat nuevo», el modo— se pliega en
  un menú **«…»**; nunca hay scroll horizontal ni un control cortado, desde un panel de
  320 px hasta uno de 1200, y los desplegables siempre quedan dentro de la ventana.
  - **Un chat nuevo ya no es una pantalla en blanco.** Muestra con qué proveedor, modelo
    y modo vas a hablar y cuatro pedidos de OpenSCAD para empezar (*una caja con tapa a
    presión para un Raspberry Pi 5*, *copia la pieza de esta foto*…); un clic rellena la
    caja de mensaje.
  - **La caja de mensaje dice lo que importa.** Los atajos a la vista (**Enter** envía,
    **Mayús+Enter** agrega una línea y **⌘/Ctrl+Enter** también envía), el modo con su
    explicación y un **contador de contexto**: los tokens estimados del borrador más la
    conversación, contra la ventana del modelo si el proveedor la informa, en ámbar al 70 %
    y en rojo al 90 %.
  - **Buscar en las conversaciones.** La lista de conversaciones tiene un buscador que
    mira el título **y lo que se dijo**, con la coincidencia resaltada.
- **Pasos más legibles y enlaces a tus checkpoints.** Cada tarjeta muestra, sin abrirla,
  el checkpoint al que se refiere —un enlace que lo selecciona en el árbol y lo abre en el
  editor y el visor—, los datos de la malla de un render o una medición (tamaño,
  triángulos, si es cerrada, si cabe en la placa) y, si falla, la línea y el código del
  error. Las respuestas con Markdown se leen mejor (tablas con su alineación, casillas
  `[x]`, tachado y direcciones sueltas que se abren en el navegador), y cada bloque de
  OpenSCAD trae **Abrir en el editor** además de **Guardar como checkpoint**: abrirlo lo
  guarda como un checkpoint nuevo, sin pedir nombre, y lo abre (los checkpoints no se
  editan, así que no hay un borrador suelto).
- **Importar tus servidores de mini-tools.** En Configuración → Proveedores de IA, el
  botón **Importar servidores de mini-tools** muestra primero la lista de lo que hay, te
  pide confirmación y recién entonces los copia (nombre, tipo, dirección y modelo por
  defecto). **Las claves no se copian**: están cifradas con la contraseña de mini-tools y
  ScadLab nunca las lee, así que cada servidor llega sin clave y la escribes de nuevo. Si
  mini-tools no está en tu equipo, lo dice.
- **Hermes: un chat que diseña por ti.** Con el interruptor **Agente** encendido, el
  modelo del chat deja de solo conversar: usa las herramientas de ScadLab para crear
  un checkpoint, renderizarlo, mirarlo, medirlo y, si el render falla, leer la línea
  del error y corregirlo en un checkpoint nuevo, hasta que la pieza queda como la
  pediste. Cada paso aparece como una tarjeta plegable, con la herramienta, sus
  argumentos y su resultado; si el render falla, la tarjeta se abre sola con la línea y
  el código que dio el error, y la imagen que el modelo mira con `preview` se ve en la
  conversación y se amplía con un clic. Tú pones el tope de pasos (de 1 a 30, 12 por
  defecto): al llegar, el modelo resume lo que logró y se detiene en vez de seguir sin
  fin. **Detener** muestra un reloj y corta incluso a mitad de una herramienta, sin
  perder lo que ya hizo. Apagado, el chat es el de siempre: texto y bloques de
  OpenSCAD con **Guardar como checkpoint**. Si el modelo no sabe usar herramientas,
  el chat sigue en texto y te lo dice, sin errores.
  - **El modelo ve lo que dibuja.** La imagen del `preview` vuelve al modelo como
    imagen, solo si el modelo ve; si no, recibe las medidas y se avisa.
  - **Cada respuesta dice cuánto costó.** Al pie quedan los tokens de entrada y salida
    y, con los proveedores que publican precios (OpenRouter), el costo en dólares; si
    el proveedor no lo informa, no se inventa un «$0».
- **Una contraseña maestra para tus claves de API, que se pide una sola vez.** La
  primera vez que guardas una clave de API, ScadLab te pide **crear una contraseña
  maestra** (la escribes dos veces y una barra te dice qué tan fuerte es). Con esa
  contraseña se cifran tus claves en este equipo; la contraseña en sí no se guarda en
  ningún lado. La casilla **Recordar en este equipo** viene marcada: ScadLab guarda la
  llave derivada —no la contraseña— en el llavero del sistema (Keychain en macOS,
  Administrador de credenciales en Windows) y desde entonces guardar otra clave, usar el
  chat o abrir la app **no vuelve a pedirte nada**. La llave se lee del llavero una sola
  vez por sesión, la primera vez que hace falta, y nunca en cada mensaje.
  - **Si no la recuerdas, o el llavero niega el acceso**, ScadLab te pide la contraseña
    **una vez por sesión** y solo cuando algo necesita una clave —enviar un mensaje a un
    servidor que la tiene, listar sus modelos o probar la conexión—; al escribirla, lo que
    ibas a hacer sigue solo. Si el llavero falla no se rompe nada ni se te pregunta en
    bucle, y las variables de entorno siguen sirviendo sin contraseña.
  - **Desde Configuración → Proveedores de IA → Llavero de claves** puedes cambiar la
    contraseña (todas las claves se vuelven a cifrar, todas o ninguna), **olvidar este
    equipo** o **restablecer** todo. Una contraseña olvidada **no se puede recuperar**: lo
    único que queda es restablecer, que borra las claves guardadas (los servidores quedan,
    sin clave), y la interfaz lo dice antes de que importe.
  - Las claves que guardaste con la versión anterior se re-cifran solas al crear la
    contraseña.
- **Adjuntos en el chat: imágenes y modelos 3D.** Con el clip, pegando o arrastrando
  sobre el panel puedes adjuntar fotos y capturas (PNG, JPG, GIF, WebP de hasta 5 MB) y
  modelos de referencia **STL, 3MF y OBJ** (hasta 64 MB), hasta 10 por mensaje. Un modelo
  se copia a la carpeta `refs/` del proyecto, ScadLab lo mide (tamaño, volumen,
  triángulos, si la malla es cerrada, las partes por color de un 3MF) y le da al modelo
  ese informe y una vista previa dibujada para que pueda copiarlo o encajar una pieza
  con él. Cada adjunto es un chip con su miniatura y un botón para quitarlo; si el
  modelo no ve imágenes, el chip lo avisa y **Enviar** espera a que lo cambies, pero un
  servidor que no informa si ve imágenes nunca te lo impide.
- **Conversaciones: renombrar, borrar y cambiar de modelo.** La lista de
  conversaciones del proyecto deja **renombrar en línea** (Enter guarda, Escape
  cancela) y **borrar** con confirmación. Al abrir la app retomas la última
  conversación, y puedes cambiar de servidor y de modelo a mitad de una
  conversación sin perder el historial.
- **Proveedores de IA con claves guardadas de forma segura.** Nueva pestaña
  **Configuración → Proveedores de IA** para agregar tantos servidores como quieras:
  Ollama (local o remoto), LM Studio, llama.cpp, vLLM, OpenAI, **OpenRouter**,
  **Google Gemini**, Anthropic, Groq, DeepSeek, Mistral, Together, xAI o cualquier
  servidor compatible con OpenAI, cada uno con un atajo que ya trae su dirección.
  **Probar conexión** dice si responde (y cuántos modelos tiene) antes de guardar. La
  clave de API se guarda **cifrada** con tu contraseña maestra (ver más arriba), nunca
  en texto plano ni en el archivo de la base; si prefieres no guardarla, puedes indicar
  la variable de entorno que la tiene. El selector de modelos del chat ahora **busca**, y marca cuáles
  ven imágenes y cuáles usan herramientas (con la ventana de contexto y el precio
  cuando el proveedor los publica). Los servidores que ya tenías de versiones
  anteriores se conservan, y tus chats viejos siguen abriendo.
- **Un workspace nuevo ya trae su primer `.scad`.** En vez de una carpeta vacía,
  crear un workspace —o importar una carpeta que no tiene ningún `.scad`— deja un
  `001-<nombre>.scad` válido, con su cabecera de checkpoint, parámetros del Customizer
  y una pieza con `color()` que renderiza, ya seleccionado. Si la carpeta tenía
  archivos `.scad`, no se toca nada.

- **Doble clic en la cabecera agranda la ventana.** En macOS la barra de título
  nativa está oculta y la cabecera de ScadLab hace de barra, pero el doble clic no
  hacía nada. Ahora agranda o restaura la ventana —o la minimiza, o no hace nada, si
  así lo elegiste en Ajustes del Sistema → Escritorio y Dock—, igual que en
  cualquier otra app. Sobre un botón o una pestaña de proyecto, el doble clic sigue
  siendo de ese control.
- **Editor de skills dentro de la app.** En Configuración → Skills, cada skill
  propia tiene un botón **Editar** que abre un editor con la lista de sus archivos
  (`SKILL.md`, `README.md`, `references/`, `templates/`…) y el mismo Monaco local del
  código, en Markdown u OpenSCAD según la extensión. Puedes crear, renombrar y borrar
  archivos sin salir de la carpeta de la skill, guardar con ⌘S / Ctrl+S y, al cerrar,
  se te avisa si quedan cambios sin guardar. Las skills incluidas se abren con **Ver**,
  en solo lectura.
  - **El frontmatter se valida mientras escribes.** `name` y `description` son
    obligatorios y `name` tiene que ser igual al nombre de la carpeta; los errores se
    subrayan en el editor y se listan debajo (un clic lleva a la línea), y mientras
    haya alguno no se guarda el `SKILL.md`, porque los agentes ignorarían una skill
    rota sin avisar. Una descripción que sigue siendo el texto de ejemplo, de más de
    1024 caracteres o con «: » sin comillas solo da un aviso.
  - **Guardar actualiza todo.** La lista muestra la descripción nueva y la skill se
    vuelve a copiar a los proyectos abiertos; a los demás, al abrirlos.
  - **Una skill nueva ya no hereda la descripción de la plantilla.** «Plantilla para
    crear skills nuevas…» quedaba como descripción real y no había cómo cambiarla
    desde la app. Ahora nace con su `name` puesto y una descripción pendiente (con un
    aviso hasta que la escribas), y **Nueva skill abre su editor** en vez de abrir el
    Finder.
- **Eliminar y renombrar skills propias.** Cada skill propia tiene su botón de
  eliminar, con una confirmación que nombra la skill y su carpeta. La carpeta no se
  borra: va a una papelera dentro de los datos de la app (`trash/skills`), de donde se
  recupera moviéndola de vuelta. También se quita de los workspaces donde estaba
  instalada, pero solo las copias que instaló ScadLab y nadie tocó: si editaste una a
  mano, se deja y se te dice cuáles fueron. Las incluidas no se eliminan, solo se
  apagan. **Renombrar** cambia la carpeta, el `name` del `SKILL.md` y las copias
  instaladas, y no deja repetir el nombre de otra skill (propia o incluida).
- **Elegir dónde viven tus skills propias.** Configuración → Skills muestra la
  **carpeta de skills propias** con **Cambiar…** (por ejemplo, una carpeta sincronizada
  en la nube o dentro de tu repositorio). Si ya tienes skills, eliges entre **mover** tus
  skills a la carpeta nueva —las que ya existan allí no se pisan— o **solo apuntar** a
  ella, sin tocar la anterior. La elección se recuerda, y **Predeterminada** vuelve a la
  carpeta de siempre.

### Corregido

- **Mandar un checkpoint a la papelera ya no deja huérfanos a sus hijos.** Antes, borrar
  `001-prueba.scad` dejaba a `002-…` colgando de un padre que ya no estaba: el árbol lo
  marcaba con ⚠ y el panel Problemas decía «parent=001-prueba.scad no existe», por algo
  que habías hecho a propósito. Ahora los **hijos directos pasan a colgar de su abuelo**, o
  quedan como **principales** si lo borrado era una raíz; la confirmación te lo dice
  («Sus 2 checkpoints hijos pasarán a colgar de …»). Solo cambia la línea
  `// @checkpoint parent=…` de cada hijo (el código, la descripción, los finales de línea y
  la fecha del archivo quedan como estaban; la escritura es atómica). **Restaurar lo
  deshace**: los hijos que sigan donde se los dejó vuelven a colgar del restaurado, con el
  nombre con que volvió. El padre activo pasa al padre del borrado, como antes. La herramienta
  `trash_checkpoint` de la IA hace lo mismo y lo informa. El render de esos hijos no se
  invalida (la caché de render ya no cuenta la línea de cabecera; la primera vez tras actualizar
  se vuelven a calcular los renders guardados).
- **Antigravity ya puede usar las herramientas de ScadLab desde el chat.**
  Pedir «una caja con tapa a presión para un Raspberry Pi 5» terminaba en «Antigravity
  terminó el turno sin responder nada»: aunque ScadLab ya estaba registrado en
  Antigravity, en modo no interactivo (`agy --print`) **nadie puede contestar un permiso**,
  así que Antigravity rechazaba la primera herramienta —`permission check failed for mcp
  "scadlab/printer_info"`— y **terminaba el turno ahí**, antes de que el modelo pudiera
  decir nada. Ahora, la primera vez que usas Antigravity en el chat (junto con el registro
  del servidor), ScadLab agrega al archivo de configuración de Antigravity,
  `~/.gemini/antigravity-cli/settings.json`, **una regla `mcp(scadlab/<herramienta>)` por
  cada herramienta de ScadLab** en `permissions.allow` —el formato exacto que el propio
  Antigravity pide—, y un aviso te dice qué agregó y dónde. Es una lista explícita de **sus
  propias herramientas**: nunca un comodín ni una regla de otro tipo. Lo demás del archivo
  (`trustedWorkspaces`, tus reglas, el orden) no se toca, antes se guarda una copia en la
  carpeta de datos de ScadLab y, si el archivo no es un JSON válido, no se pisa y el chat te
  avisa. En cada turno se mantiene al día: se agregan las herramientas nuevas y se quitan las
  que ya no existen. **«Solo consulta» sigue valiendo**: como las reglas son globales, el
  servidor de ScadLab rechaza las herramientas que modifican el proyecto mientras dura un
  turno de consulta (con el motivo, para que Antigravity entregue el diseño en un bloque).
  En Configuración → Proveedores de IA, Antigravity muestra «Permisos de ScadLab: N
  herramientas aprobadas» con **Quitar** (borra solo esas reglas y ScadLab no las repone
  sola) y **Volver a agregar**; y si Antigravity igual bloquea una herramienta, el mensaje
  del chat nombra la herramienta, el archivo y el paso a seguir, con un aviso que ofrece
  **Reparar permisos de Antigravity**. La shell sigue denegada y no se usa nunca
  `--dangerously-skip-permissions`. **Lo que tienes que saber:** las reglas son globales, así
  que también valen cuando abres Antigravity tú mismo: no te pedirá confirmación para las
  herramientas de ScadLab, tampoco las que escriben. (Una primera versión lo intentó con un
  *hook* en `.agents/hooks.json` del proyecto; con el `agy` real se comprobó que Antigravity
  pide el permiso antes de ejecutar los hooks, así que no servía: ese mecanismo se retiró, y
  si un turno interrumpido dejó la entrada en un proyecto, ScadLab la quita al abrirlo sin
  tocar tus otros hooks.) De paso, las tarjetas de Antigravity ya no dicen `call_mcp_tool`
  para todo: llevan el nombre de la herramienta de ScadLab que se llamó (`printer_info`,
  `create_checkpoint`…).
- **Antigravity mostraba «Sin resultado» cuando una herramienta fallaba.** La tarjeta del
  paso ahora trae el detalle real del error que informa Antigravity, y una respuesta que
  llega solo en el cierre del turno (sin los trozos en vivo) también se muestra.
- **«Guardar como checkpoint» fallaba en un workspace sin checkpoints** con «no hay
  checkpoint que sirva de padre»: ahora crea la raíz.
- **Un `^[[?1;2c` aparecía en la primera línea de Antigravity al volver a una terminal.**
  Al reenganchar una terminal, ScadLab vuelve a dibujar lo que ya había pasado, y la
  terminal volvía a contestar la pregunta «¿qué terminal eres?» que el agente hizo al
  arrancar. Esa respuesta llegaba cuando el agente ya estaba en marcha y la mostraba
  como si la hubieras escrito. Ahora, mientras se repone lo ya visto, esas respuestas
  automáticas (tipo de terminal, posición del cursor y colores) no se envían al agente;
  lo que tecleas pasa igual.
- **El botón de carpeta de Configuración → Skills no hacía nada.** Abría la carpeta de
  skills propias, pero esa carpeta no existe hasta que se crea la primera skill, y el
  sistema se negaba a abrir una ruta inexistente sin que el error llegara a ningún
  aviso. Ahora el botón **Abrir** crea la carpeta si falta y la muestra en el
  Finder o el Explorador, y si el sistema no puede abrirla lo dice con un aviso. Lo
  mismo vale para «Mostrar en el Finder» de un workspace.
- **«Elegir…» no abría el diálogo de carpetas si la carpeta todavía no existía.**
  En «Nuevo workspace», la ubicación predeterminada (`~/Documents/ScadLab`) se crea
  recién con el primer workspace, y el diálogo nativo se negaba a abrir con «default
  directory … does not exist», un aviso por cada clic. Ahora abre en la carpeta
  existente más cercana. Lo mismo valía para Configuración → carpeta de workspaces
  y para el diálogo de exportar.

## [0.1.0] - 2026-10-16

### Agregado

- **ScadLab funciona sin OpenSCAD instalado.** Antes, sin OpenSCAD el visor 3D se
  quedaba vacío y la app solo decía «no encontrado». Ahora trae un **motor
  integrado**: el OpenSCAD oficial 2025.12.30 compilado a WebAssembly, que corre
  dentro de ScadLab y toma el relevo solo cuando no hay uno instalado. Con él
  funcionan igual el **visor 3D**, los **errores con su línea** (marcados en el
  editor), la **exportación a STL y 3MF** (una parte por color, lista para Bambu
  Studio) y la **estimación de peso, filamento y costo**; lo único que cambia es la
  velocidad. El **servidor MCP** también lo usa: los agentes siguen pudiendo
  renderizar, validar, medir y exportar, y su vista previa PNG se dibuja sin
  OpenGL. Un OpenSCAD instalado sigue siendo el preferido: el integrado es el
  respaldo, no un reemplazo.
  - **Cuándo se nota.** El panel de render muestra una nota informativa (no un
    error): «Renderizado con el motor integrado (OpenSCAD 2025.12.30,
    WebAssembly) porque no se encontró OpenSCAD instalado», y la barra de estado
    una insignia **WASM 2025.12.30** que abre Configuración → OpenSCAD.
  - **Licencia.** OpenSCAD es GPL: el motor va **sin modificar** y como programa
    aparte (recibe argumentos y archivos, igual que el ejecutable). Su versión,
    SHA-256, licencia y código fuente están en `THIRD_PARTY_NOTICES.md`.
- **Botón «Descargar OpenSCAD».** En esa nota, en el aviso de «no se pudo
  renderizar» y en **Configuración → OpenSCAD**, un clic baja OpenSCAD 2025.12.30
  desde openscad.org (unos 60 MB en macOS, 40 MB en Windows) y lo instala en la
  carpeta de datos de ScadLab: no toca el resto del sistema ni pide permisos de
  administrador. Ves la fase y el avance con una barra y puedes **Cancelar**; una
  descarga cancelada, sin red o con el archivo dañado no deja nada a medias.
  - **Verificada.** El archivo se comprueba contra un SHA-256 fijado en ScadLab y,
    ya instalado, se corre `--version` antes de darlo por bueno. Antes de empezar se
    revisa que haya espacio en disco, y los errores (sin conexión, el servidor
    respondió otra cosa, checksum distinto, disco lleno) dicen qué pasó en tu idioma.
  - **Al terminar, se vuelve a renderizar** el checkpoint activo, ya con el
    OpenSCAD real. El descargado gana al que haya en tu `PATH`; la ruta que fijes a
    mano en Configuración gana a los dos.
  - **Quitar la descarga** (con confirmación) borra solo lo que bajó ScadLab, nunca
    un OpenSCAD que instalaste tú. Disponible para macOS (Apple Silicon e Intel) y
    Windows de 64 bits.
- **Elige en qué carpeta se crea un workspace.** El diálogo **Nuevo workspace**
  ahora tiene el campo **Ubicación** —la carpeta que contendrá al proyecto— con su
  botón **Elegir…**. Por defecto es la de Configuración → General (o
  `~/Documents/ScadLab`), y si no existe se crea. Debajo, **Se creará en** muestra
  la **ruta final exacta** (`<ubicación>/<nombre-en-minúsculas-y-guiones>`) y se
  actualiza mientras escribes: el mismo cálculo que hace ScadLab al crear, así lo
  que ves es lo que se crea, con acentos, espacios y símbolos incluidos. La casilla
  **Usar esta carpeta como predeterminada** deja esa ubicación para los próximos.
  - Una ubicación que no sea una ruta absoluta (relativa o con `~`) se rechaza con
    un aviso claro, y si el destino ya existe y tiene archivos, ScadLab no lo toca y
    te dice qué carpeta es, en español o en inglés.
- **ScadLab recuerda cómo lo dejaste.** Al cerrar y volver a abrir, cada proyecto
  reabre en **el checkpoint que tenías elegido** (antes saltaba siempre al más
  nuevo; si ese archivo ya no existe, se abre el más reciente), y el **visor 3D**
  conserva el alambre, las aristas, la proyección ortográfica o en perspectiva, la
  vista dividida, la muestra de color elegida y si la pieza se pinta con los colores
  del modelo. Se suma a lo que ya se guardaba (pestañas, diseño de paneles,
  tamaños, tema y letras), y el panel maximizado, los grupos minimizados y la
  pestaña activa de cada grupo también vuelven como estaban. El **modo enfoque no
  se recuerda**, a propósito: ScadLab siempre abre con tus paneles.

- **Varios proyectos a la vez: pestañas de proyecto.** La cabecera, a la derecha
  del logo, tiene ahora una pestaña por cada proyecto abierto (hasta **4**), con
  su **+** para abrir otro: elige entre tus workspaces o crea uno nuevo, y con 4
  abiertos el botón se apaga y su aviso te dice por qué. Es una sola ventana.
  - **Cambiar de pestaña es instantáneo y no pierde nada.** Cada proyecto
    conserva el checkpoint que tenías elegido, el código sin guardar (y si
    estabas editando), la comparación con el padre abierta, la parte seleccionada
    en el visor y el chat que mirabas. La barra lateral, el visor, el código,
    Problemas, Impresión y el chat siguen al proyecto de la pestaña activa.
  - **Los de fondo siguen trabajando.** Su carpeta se sigue vigilando, el
    auto-render responde a lo que escriba un agente y los agentes (y la
    auto-iteración) siguen corriendo; los paneles **Agente 1** y **Agente 2**
    muestran las terminales del proyecto activo, con un máximo de **2 por
    proyecto**, y las de los otros conservan su pantalla. Cada pestaña avisa de lo
    que pasa en ella aunque no la mires: un render en curso, que el último render
    **falló**, que hay un **agente** en marcha o que tiene **cambios sin guardar**.
  - **Reordenar y cerrar.** Arrastra una pestaña, o usa su menú (clic derecho):
    **Mover a la izquierda / derecha**, **Cerrar**, **Cerrar los demás** y
    **Mostrar en el Finder** (o el Explorador). Cerrar un proyecto con cambios sin
    guardar o agentes en marcha pide confirmación y dice qué se pierde; los agentes
    se detienen.
  - **Atajos:** ⌃Tab / ⌃⇧Tab (siguiente / anterior), ⌘/Ctrl+1…4 (ir a la pestaña)
    y ⌘/Ctrl+W (cerrar la activa). En Windows, con el foco en una terminal de
    agente, **Ctrl+W** es de la shell (borrar palabra): ahí se cierra con
    **Ctrl+Shift+W**.
  - **Cada agente ve los otros proyectos abiertos** (solo para leer y reutilizar
    piezas; escribe únicamente en el suyo). La lista se fija al abrir el agente:
    abrir otro proyecto después no la cambia, «Nueva» lo relanza. **opencode** no
    tiene una opción equivalente y solo ve su propio proyecto.
  - Al abrir ScadLab se **reabren las pestañas** que tenías y la que estaba al
    frente.
  - La lista de workspaces de la barra lateral sigue sirviendo para abrir: un clic
    abre el workspace como pestaña (o te lleva a la suya).

- **Checkpoints avanzados: menú, papelera, rama activa y comparar con el padre.**
  Haz clic derecho sobre un checkpoint del árbol (o pulsa «…» al pasar el ratón)
  y tienes **Ver código**, **Comparar con el padre**, **Ramificar desde aquí**,
  **Renombrar**, **Retomar sesión**, **Plegar o desplegar la rama**, **Copiar el
  nombre**, **Mostrar en el Finder** (o el Explorador) y **Mover a la papelera**.
  Con el foco en el árbol, **F2** renombra, **Supr** (⌫ en macOS) manda a la
  papelera y las flechas recorren y pliegan; esas teclas no hacen nada con el
  foco en el editor o en la terminal.
  - **Ramificar desde aquí** marca un **padre activo**: el checkpoint del que
    cuelga el próximo que cree el agente, el chat o el servidor MCP (y las
    reglas del workspace se lo dicen a la IA). Se ve en el árbol con un ícono de
    rama y la rama que lleva a él resaltada, y **avanza solo**: al crearse un
    hijo suyo pasa a ser el activo, así que sigues en la misma rama sin volver a
    marcarla. Para abrir una segunda variante, vuelve a ramificar desde el punto
    de partida.
  - **Papelera.** ScadLab nunca borra un checkpoint: «Mover a la papelera» lo
    lleva a `.scadlab/trash/` (solo ese, o con toda su rama; la confirmación dice
    cuántos descendientes se van) y el ícono de papelera de la cabecera del árbol
    cuenta lo que hay y lo restaura. Restaurar conserva el rótulo y el vínculo
    con la sesión, y no pisa nada: si el número está ocupado, toma el siguiente.
    Vaciarla es borrar esa carpeta a mano.
  - **Renombrar** cambia el nombre que se ve en el árbol, en la propia fila; el
    archivo no cambia de nombre porque sus hijos lo nombran.
  - **Comparar con el padre** convierte el panel Código en un visor de diferencias
    de solo lectura (lado a lado, o en una columna si el panel es angosto).
  - **Retomar sesión.** Cuando un agente de la terminal crea un checkpoint, ScadLab
    guarda qué agente y qué sesión fueron; los que guardas desde el chat recuerdan
    el chat. «Retomar en Claude Code» (o en el agente que sea) reabre esa
    conversación en la terminal y «Abrir el chat» abre el chat. Lo que guardas
    tú desde el editor no se le atribuye al agente.
  - El tooltip de cada checkpoint suma quién lo creó y cuándo. La base local pasa
    a la versión 3 (migración aditiva: dos columnas y una tabla; no toca tus
    checkpoints).

- **El visor ve el modelo por partes, con sus colores.** Un modelo con varios
  `color()` ya no se pinta entero con una sola muestra: cada parte sale con el
  color que le dio el modelo. El interruptor **Modelo / Muestra** de la barra del
  visor elige entre los colores del modelo y la muestra de siempre (elegir una
  muestra pasa solo a «Muestra»). **Haz clic en una parte** para seleccionarla:
  se resalta con su contorno y el resto se atenúa, y una tarjeta dice su color,
  tamaño, volumen, triángulos, si la malla es cerrada y cuánto pesa, cuántos
  metros de filamento lleva y cuánto cuesta. Con el selector de material de la
  tarjeta cada parte puede ir en un filamento distinto y la estimación cambia al
  instante. **Esc** o un clic en el vacío deselecciona; la vista dividida
  conserva la selección y un arrastre para orbitar nunca selecciona sin querer.
- **Aviso cuando la pieza no cabe en la placa.** Si el modelo sobrepasa el volumen
  de la impresora del workspace, el contorno de la cama se pinta en rojo y un
  aviso dice cuántos milímetros sobran en cada eje y qué probar (rotarla, partirla,
  otra impresora). El mismo aviso aparece en la barra de estado, y un clic en él
  abre el panel Impresión.
- **Panel «Impresión».** Un panel acoplable nuevo (menú Ver, ⌘⇧7 / Ctrl+Shift+7;
  en los diseños viene como pestaña junto a Problemas) con una tabla por parte
  —color, material, peso, metros de filamento y costo— y el total. Debajo van el
  relleno, los perímetros, el ancho de línea, el diámetro del filamento, el
  material por defecto y la moneda: cambiarlos recalcula en el acto, solo para ese
  cálculo. Un clic en una fila selecciona la parte en el visor y **Exportar 3MF**
  abre el mismo diálogo de siempre. Es una estimación (sin soportes ni purga del
  AMS): el número exacto lo da el laminador.
- **Configuración → Impresión** gana los datos de la estimación: perímetros, ancho
  de línea, diámetro del filamento y moneda (un valor fuera de rango vuelve al de
  fábrica en vez de fallar). Editar la densidad o el costo de un material
  actualiza la estimación al guardar.

- **Claude Code usa el servidor MCP sin pedir aprobación** cuando lo abres desde
  la terminal de ScadLab: la app le pasa el servidor con `--mcp-config`. Si lo
  abres por tu cuenta, Claude pregunta una sola vez si confías en el
  `.mcp.json` del workspace.

- **Agentes de IA dentro de ScadLab.** Hay ahora dos paneles de terminal de agente
  (Agente 1 y Agente 2) donde corre Claude Code, opencode,
  Codex o Antigravity directamente en la carpeta del workspace: ya no hace falta
  salir a otra ventana para pedirle un modelo. Cada terminal ofrece **Nueva**,
  **Continuar** la última conversación y **Sesiones**, que lista las
  conversaciones previas de ese agente en ese workspace para retomarlas
  (Antigravity solo permite nueva y continuar). Los agentes siguen vivos si se
  recarga la interfaz y se vuelve a ver su pantalla. Cada panel se puede maximizar.
- **Auto-iteración.** Si el render del checkpoint más nuevo falla, ScadLab le
  avisa al agente de la terminal para que lea `RENDER_ERRORS.md` y lo corrija en
  un checkpoint nuevo: una sola vez por fallo y con un tope de cinco avisos
  seguidos, para no gastar cuota en un bucle. Se apaga desde la barra de la
  terminal.
- **Chat de diseño con modelos.** La pestaña Chat conversa con Ollama, con
  cualquier servidor compatible con la API de OpenAI (LM Studio, vLLM,
  OpenRouter…) o con la API de Anthropic, para pensar ideas y pedir piezas sin
  salir de ScadLab. El modelo conoce tu workspace —impresora, volumen de
  impresión, colores del AMS, checkpoints y el código del archivo abierto— gracias
  a un prompt propio (español e inglés) que le enseña las reglas de checkpoints,
  color y diseño FDM de Bambu Lab. Responde en streaming; **Detener** corta y
  conserva lo escrito. Cada bloque de OpenSCAD trae **Guardar como checkpoint**
  (crea uno nuevo, hijo del archivo abierto) y **Copiar**. El historial queda por
  workspace. Las claves de API no se guardan: se leen de la variable de entorno
  que indiques en Configuración → IA, también si la definiste en `~/.zshrc`
  aunque abras la app desde el Finder.

- **Paneles acoplables, como en un editor.** El visor 3D, el código, las dos
  terminales de agente, el chat y los problemas son ahora **paneles** que
  reorganizas a tu gusto, con pestañas como las de un editor de código (ícono y
  nombre, la activa fundida con su contenido, un punto naranja en *Código* si hay
  cambios sin guardar y uno verde en *Agente* mientras el agente corre).
  **Arrastra una pestaña** para reordenarla, llevarla a otro grupo o soltarla en
  un borde y **dividir**; **doble clic** en una pestaña maximiza su grupo; el clic
  derecho ofrece ocultar, maximizar, **flotar** (el panel pasa a una ventana
  dentro de ScadLab, que se mueve y se redimensiona libremente sin salirse de la
  ventana), acoplar y mover. Todo borde entre paneles es un **divisor** que se
  arrastra en las dos direcciones (con doble clic reparte el espacio a partes
  iguales) y ningún panel baja de un mínimo útil: el visor de unos 200 × 150 px,
  el código y las terminales de unos 240 × 120. **Ocultar un panel no destruye
  nada**: el visor conserva su cámara, el editor su texto y su historial de
  deshacer, y el agente sigue corriendo con su pantalla intacta.
- **Menú Ver, diseños y modo enfoque.** El menú **Ver** de la cabecera tiene una
  casilla por panel (con su atajo ⌘⇧1…7, Ctrl+Shift+1…7 en Windows; en macOS
  ⌘⇧3/4/5 los reserva el sistema para capturas, así que también valen ⌘⌥3/4/5),
  la barra lateral y la barra de estado, que ahora se pueden ocultar, y los
  **diseños**: Predeterminado, Modelado, Código e IA y Pantalla dividida, más los
  tuyos con **Guardar diseño como…** y **Diseños guardados…** para aplicarlos o
  borrarlos. El diseño actual se guarda solo y se restaura al abrir; si el
  guardado estuviera dañado, ScadLab abre con el predeterminado. Si ocultas todos
  los paneles, una pantalla te deja volver a abrirlos o aplicar un diseño. El
  **modo enfoque** (⌘⇧0 / Ctrl+Shift+0) deja solo el panel activo sin barras, y
  Esc o el mismo atajo vuelve al diseño anterior. Los paneles flotan dentro de la
  app, no en ventanas del sistema: varias ventanas nativas es otra fase.
- **Minimizar un panel sin cerrarlo.** Cada grupo de paneles tiene ahora un botón
  **–** (Minimizar panel) a la izquierda de maximizar, y **⌘⇧M / Ctrl+Shift+M**
  hace lo mismo con el grupo activo. Si el grupo está apilado arriba o abajo de
  otro, queda **solo su barra de pestañas**; si está al lado de otro, queda una
  **franja angosta con los íconos de sus pestañas** en vertical (un punto verde
  o naranja sigue a la vista si hay un agente en marcha o cambios sin guardar).
  Los vecinos se reparten el espacio que queda. Un clic en cualquier pestaña, en
  el **+** (Restaurar panel) o un doble clic en la barra del grupo lo devuelve a
  su tamaño anterior. Nada se destruye: el visor conserva su cámara, el editor su
  texto y la terminal su agente. Un grupo flotante minimizado se reduce a su barra.
  Lo minimizado se guarda con el diseño, también en los diseños con nombre.
- **«Mover a…» en el menú de cada panel.** El botón **⋯** del grupo y el clic
  derecho en una pestaña tienen **Mover a…** con destinos que funcionan siempre,
  sin arrastrar: **Izquierda**, **Derecha**, **Arriba** y **Abajo** (del área de
  paneles completa: el panel va al grupo que ya ocupe ese borde o a uno nuevo),
  **Centro** (como pestaña del grupo acoplado más grande), **Junto a…** (como
  pestaña del grupo de otro panel que elijas) y **Flotar / Acoplar**. Sirve para
  el visor, el código, los dos agentes, el chat y los problemas; mover no
  destruye su contenido. El arrastre de pestañas sigue ahí para quien lo prefiera.
- **Tamaños a tu medida.** Configuración → General tiene ahora un bloque
  **Apariencia**: elige el **tamaño de la interfaz** (Compacto 90 %, Normal,
  Grande 115 %, Más grande 130 % o Máximo 150 %), que agranda o reduce el texto
  y los íconos de toda la app sin tocar los espacios, y la **letra propia del
  editor, de la terminal y del chat** (de 9 a 28 px, cada una independiente de
  la interfaz) con una vista previa en vivo. Todo se aplica y se guarda al
  instante, sin pulsar Guardar. Con el teclado, ⌘+ / ⌘− / ⌘0 (Ctrl en Windows)
  suben, bajan y restablecen la interfaz; con el foco dentro del editor, la
  terminal o el chat, esos mismos atajos cambian la letra de esa área. Los
  botones **Restablecer apariencia** y **Restablecer el diseño** vuelven todo a
  fábrica.
- **Cada sección, a tu tamaño.** Además del ancho de la barra lateral, ahora
  también se puede arrastrar el reparto de la barra lateral entre la lista de
  workspaces y el árbol de checkpoints. Los tamaños se ajustan a la ventana de
  cada momento: si la achicas, las secciones ceden en vez de salirse, y recuperan
  lo elegido al agrandarla.
- **Exportar a STL, 3MF y PNG.** Los tres botones de la cabecera ya funcionan y
  se activan cuando el checkpoint elegido tiene un render correcto; si no, siguen
  apagados y su descripción te dice por qué. **STL** guarda la malla directo,
  **PNG** la vista del visor tal como se ve, y **3MF** abre un diálogo que te
  muestra qué va a salir antes de elegir dónde guardar: **una parte por cada
  `color()` del modelo**, con su muestra de color, sus triángulos y el
  **extrusor** que Bambu Studio le asigna (E1, E2…). Avisa, sin impedir exportar,
  si el modelo usa más colores de los que admite la impresora del workspace (por
  ejemplo, cinco en una A1 mini, que admite cuatro), si hay geometría sin
  `color()` o si dos colores son casi iguales y gastarían dos filamentos. Además
  del 3MF para Bambu Studio puedes elegir **un STL por color** en un `.zip`,
  pensado para OrcaSlicer, PrusaSlicer o Cura. Atajos: ⌘E / Ctrl+E para el 3MF y
  ⇧⌘E / Ctrl+Shift+E para el STL.
- **Abrir en Bambu Studio.** Si ScadLab encuentra Bambu Studio, el diálogo de
  3MF ofrece **Exportar y abrir en Bambu Studio**, y tras cualquier exportación
  un aviso con la ruta del archivo te deja **Mostrar en el Finder** (o en el
  Explorador) y **Abrir en Bambu Studio**. Siempre es por un clic tuyo: ScadLab
  nunca lo abre solo, y lo abre como app con el archivo, no por línea de
  comandos. Si lo tienes en otra carpeta, indica la ruta en Configuración →
  Impresión → Ruta de Bambu Studio (vacío = autodetectar), donde además ves si
  lo encontró.
- **Servidor MCP de ScadLab, configurado solo en cada agente.** ScadLab puede
  ahora lanzarse como servidor MCP (`ScadLab --mcp --workspace <carpeta>`), y al
  crear, importar o abrir un workspace deja la entrada `scadlab` en el archivo de
  proyecto que lee cada CLI: `.mcp.json` para Claude Code, `opencode.json` para
  opencode y `.gemini/settings.json` para el Gemini CLI anterior. Codex, que no
  lee configuración de proyecto, recibe el servidor por flags `-c` cada vez que lo
  abres en la terminal de ScadLab. Solo se fusiona la clave `scadlab`: tus otros
  servidores, el orden y la sangría del archivo quedan como estaban, y un JSON
  roto (o un `opencode.jsonc` con comentarios) no se toca, solo se avisa. Si
  mueves o actualizas la app, la ruta se corrige al abrir el workspace.
  **Configuración → IA** tiene un bloque nuevo, «Servidor MCP de ScadLab», con el
  interruptor (apagarlo quita la entrada de todos los workspaces), el comando que
  usan los agentes para copiarlo, el estado de cada archivo del workspace abierto
  y **Volver a sincronizar**. En la barra de cada panel de agente, el indicador
  **MCP ✓** confirma que está configurado (pasa a **MCP !** si algo no quedó al
  día).
- **Antigravity se agrega al servidor MCP con un botón y tu permiso.** El
  archivo `.gemini/settings.json` es del Gemini CLI anterior: Antigravity
  (`agy`) no lo lee (comprobado: con ese archivo, `agy mcp list` sigue diciendo
  «No MCP servers configured»), y su única configuración de servidores es global,
  en `~/.gemini`. En Configuración → IA → Servidor MCP de ScadLab hay ahora una
  fila **Antigravity** con su estado (agy no instalado, no configurado,
  configurado, otra ruta, desactivado) y los botones **Agregar a Antigravity** y
  **Quitar de Antigravity**. Los dos piden confirmación y dicen con todas las
  letras que modifican la configuración global de Antigravity, la de todos tus
  proyectos; se hacen con `agy mcp add|remove`, nunca editando su archivo, y no
  tocan tus otros servidores. La entrada es una sola, `scadlab` →
  `<ScadLab> --mcp` sin `--workspace`: el servidor trabaja en la carpeta donde
  Antigravity lo lanza, así que sirve a todos tus workspaces. En la ayuda, el
  archivo `.gemini/settings.json` se llama ahora «Gemini CLI (anterior)».

### Cambiado

- **Como mucho dos renders de OpenSCAD a la vez.** Con varios proyectos abiertos
  y el auto-render, cada uno podía lanzar el suyo y la máquina entera se
  arrastraba con un modelo pesado. Ahora los demás esperan su turno (y se pueden
  cancelar mientras esperan).

- **Las reglas para los agentes se actualizan solas sin pisar lo tuyo.** En
  `CLAUDE.md` y `AGENTS.md` las reglas de ScadLab van en un bloque marcado que
  se renueva al abrir el workspace (así los workspaces viejos reciben las
  reglas nuevas del MCP, las ramas y la papelera); lo que escribas fuera del
  bloque se conserva. Un archivo creado por una versión anterior se respalda en
  `.scadlab/backup/` antes de reemplazarlo.

- **El menú de las pestañas de los paneles ahora es el de ScadLab**, el mismo que
  el botón **⋯** del grupo (con submenús y atajos), en lugar del menú genérico de
  la librería. «Mover a la derecha» y «Mover abajo» pasaron a **Mover a…**, que
  ahora cuenta desde el área de paneles completa y no desde el grupo.
- **«Restablecer el diseño» ahora también devuelve los paneles** al diseño
  predeterminado, y muestra la barra lateral y la de estado si estaban ocultas.
  El alto del panel inferior y el ancho del código que se guardaban antes ya no
  se usan (los paneles llevan su tamaño dentro del diseño), pero no se borran.
- **Español neutro en toda la interfaz.** Algunos textos usaban vos («Elegí»,
  «Creá», «podés») y otros tú; ahora la interfaz, los mensajes de error y
  `RENDER_ERRORS.md` usan tú, igual que la ayuda.

- **Apoya el proyecto.** ScadLab sigue siendo gratis y de código abierto; quien
  quiera apoyarlo tiene a un clic **Buy Me a Coffee** (aporte único) y
  **Patreon** (suscripción mensual): en el pie de Configuración, en un bloque de
  Configuración → Acerca de (junto con la licencia MIT), en la barra de la
  ayuda y en el README. El repositorio muestra además el botón «Sponsor» de
  GitHub.

- **Primera piedra de ScadLab (fase S0).** La app abre en macOS y Windows con su
  forma definitiva: cabecera con los botones de exportación, lista de
  workspaces, árbol de checkpoints, y los paneles del visor 3D, el código y el
  agente de IA (estos tres todavía muestran qué van a tener y en qué fase llegan).
- **Workspaces.** Crear un proyecto arma la carpeta en `~/Documents/ScadLab/`
  con `CLAUDE.md` y `AGENTS.md` —las reglas que siguen todos los agentes: nunca
  sobrescribir un checkpoint, poner color a cada pieza, validar cada render,
  respetar el volumen de la impresora elegida y no hacer commits— más
  `printer.json`. También se puede importar una carpeta existente.
- **Checkpoints.** Cada `.scad` del workspace es una versión inmutable; el
  encabezado `// @checkpoint parent=… desc="…"` arma el árbol con ramas, que se
  puede plegar, y al elegir uno se ve su código.
- **Skills de modelado para la IA**, instaladas en cada workspace para Claude
  Code, opencode, Codex y Antigravity a la vez: reglas de OpenSCAD, perfiles y
  reglas de diseño de impresoras Bambu Lab (A1, A1 mini, P1P, P1S, P2S, X1C,
  X1E, H2D, H2S), flujo multicolor y 3MF, diez plantillas paramétricas (caja con
  tapa, caja de electrónica, soporte de pared, clip, perilla, engranaje, bisagra
  print-in-place, soporte de teléfono, placa bicolor, bin Gridfinity) y
  librerías. Desde Configuración → Skills se pueden apagar o crear skills
  propias a partir de una plantilla.
- **Configuración**: idioma (inglés y español, cambia en caliente), tema
  oscuro/claro/sistema, carpeta de workspaces, ruta de OpenSCAD con detección
  automática y motor de render, agentes de IA detectados aunque no estén en el
  PATH de la app (Antigravity también si todavía se llama `gemini`), Ollama con
  prueba de conexión y lista de modelos, APIs compatibles con OpenAI y de
  Anthropic (la key se lee de una variable de entorno, nunca se guarda),
  impresora y material por defecto, relleno y costo por kilo de cada filamento.
- Todo se guarda en una base SQLite local (`scadlab.db`).
- **Renderizar con OpenSCAD (fase S1).** El panel del visor tiene un botón
  **Renderizar** (y **Cancelar** mientras corre) y una barra de estado con lo
  que tardó, si la malla salió de la caché y qué motor usó OpenSCAD. Si el
  modelo falla, se ve la lista de errores y advertencias con su línea, y la
  salida completa de OpenSCAD se puede desplegar. Si OpenSCAD no está
  instalado, el visor lo dice y lleva directo a Configuración → OpenSCAD.
- **El árbol muestra cómo quedó cada checkpoint**: un spinner mientras se
  renderiza y un punto verde (salió bien), ámbar (salió con advertencias) o
  rojo (falló); al pasar el ratón se lee el resumen, con el primer error y su
  línea.
- **La app se entera sola de los checkpoints nuevos.** Cuando un agente crea un
  `.scad` desde la terminal, el árbol se actualiza sin pulsar «volver a leer» y,
  con el auto-render activo, ScadLab lo renderiza. Si falla, deja
  `RENDER_ERRORS.md` en el workspace con el error y la línea para que el agente
  lo corrija en un checkpoint nuevo; cuando un render sale bien, el archivo se
  borra. El archivo sigue siempre al checkpoint más nuevo: recorrer el historial
  y renderizar uno viejo no lo toca, así el agente no recibe errores que ya
  corrigió. Elegir en el árbol un checkpoint que nunca se renderizó también lo
  renderiza.
- Los mensajes de OpenSCAD se probaron contra una instalación real: un
  `assert()` que falla muestra solo su error (antes sumaba un «modelo vacío»
  engañoso) y un modelo 2D sugiere extruirlo con `linear_extrude`.
- En Configuración → OpenSCAD se puede apagar el auto-render y fijar el tiempo
  máximo de un render (de 10 segundos a una hora; 5 minutos por defecto).
- **Visor 3D (fase S2).** Cuando un render sale bien, la pieza aparece en el
  panel del visor con materiales PBR y reflejos, apoyada sobre la placa de la
  impresora del workspace a escala real (cuadrícula de 10 mm y la caja del
  volumen de impresión). Se orbita, desplaza y hace zoom con ratón, trackpad,
  pantalla táctil o teclado (flechas, Shift+flechas, `+`/`-`). La barra del visor
  trae las siete vistas (Frente, Atrás, Izquierda, Derecha, Arriba, Abajo e Iso,
  también con `1`–`7`), wireframe para ver los triángulos, aristas marcadas,
  proyección ortográfica o en perspectiva y Encuadrar (`F`). Todo funciona sin
  internet: la iluminación se genera en la app, no se descarga.
- **Medidas de la pieza** en una esquina del visor: tamaño X × Y × Z, volumen en
  cm³, cantidad de triángulos y si la malla es cerrada (lista para laminar) o
  tiene agujeros.
- **Seis muestras de color** en la barra del visor, pensadas como los filamentos
  del AMS: un clic pinta la pieza, doble clic o clic derecho cambia el color, y
  la paleta se guarda.
- **Captura PNG** de la vista tal como se ve, con el diálogo nativo para elegir
  dónde guardarla, y **vista dividida**: una segunda cámara independiente sobre
  la misma pieza (por ejemplo Iso y Abajo a la vez), cada una con su proyección.
- **Volver a un checkpoint ya visto es instantáneo**: las mallas quedan en
  memoria y, al cambiar de checkpoint, la cámara se queda en el mismo ángulo
  para comparar versiones; mientras llega la nueva, la anterior sigue a la
  vista. Si el render falla, el visor se oculta y se ven los diagnósticos como
  antes; las advertencias de un render que salió bien quedan plegadas en una
  esquina.
- **Editor de código OpenSCAD (fase S4).** El panel de código es ahora un editor
  de verdad —Monaco, el de VS Code, incluido en la app, sin internet— con
  resaltado propio de OpenSCAD (también el encabezado `@checkpoint`, las
  secciones del Customizer y los rangos `// [min:paso:max]`) en el tema claro u
  oscuro de la app. Se abre en **solo lectura**, porque los checkpoints no se
  tocan; con **Editar** o un doble clic se pasa a edición y **Guardar**
  (⌘S/Ctrl+S) pide un rótulo y una descripción y crea un **checkpoint nuevo,
  hijo del que se estaba viendo**, que aparece en el árbol y se renderiza solo. Si
  se cambia de checkpoint o de workspace con cambios sin guardar, pregunta antes.
  Trae **buscar y reemplazar**, **formatear** (botón o ⇧⌥F/Shift+Alt+F: sangría,
  espacios y líneas en blanco, sin unir ni partir líneas ni tocar strings y
  comentarios, y se deshace de una vez) y **autocompletado** de todos los módulos,
  funciones y variables especiales de OpenSCAD con su firma y su explicación en
  el idioma de la app, más la ayuda de firma mientras se escriben los argumentos y al pasar el
  ratón. Mientras se escribe, un **linter** revisa el código con el parser del
  propio OpenSCAD y con nueve reglas pensadas para imprimir y para el historial
  (SL001–SL009: encabezado del checkpoint, padre inexistente, piezas sin
  `color()` que quedarían sin filamento en el 3MF, includes que no existen,
  checkpoints que incluyen a otros, `$fn` excesivos, `minkowski()` anidados,
  variables asignadas dos veces y parámetros del Customizer sin rango), y los
  subraya en el código. Los errores del último render también se marcan, y un
  clic en cualquier diagnóstico lleva a su línea. Sangría, tamaño de letra,
  ajuste de líneas y minimapa se eligen en Configuración → General.
- **Una ventana que se acomoda a cómo trabaja cada uno.** Los bordes entre la barra
  lateral y los paneles se **arrastran** y la disposición se recuerda; la barra lateral se
  **pliega** con el botón de la cabecera o ⌘B/Ctrl+B. Abajo hay una **barra de
  estado** con el workspace, la impresora, el modo del editor y si hay cambios
  sin guardar, la posición del cursor, el conteo de errores y advertencias y la
  versión de OpenSCAD; el conteo abre el nuevo panel **Problemas**, junto a
  Agente IA, que junta lo del linter y lo del render con clic a la línea. El
  panel de medidas del visor se **pliega** a una sola línea, para que en la vista
  dividida no tape la pieza, y cada botón recuerda su atajo (⌘ en macOS, Ctrl en
  Windows) al pasar el ratón.

### Corregido

- **Cerrar ScadLab en modo enfoque ya no pierde el diseño de paneles.** Al entrar
  en modo enfoque se cancelaba el guardado pendiente del diseño; si cambiabas de
  pestaña o de división y cerrabas la aplicación antes de salir del modo, al
  reabrir faltaba ese último cambio. Ahora el diseño de antes se guarda en el
  momento de entrar.
- **La barra lateral ya no se corre ni queda con los rótulos cortados**
  («ORKSPACES», «HECKPOINTS»). Las listas de la barra solo se desplazan en
  vertical: con una cadena larga de checkpoints el árbol era más ancho que la
  barra y la lista podía moverse de lado, dejando el contenido corrido unos
  píxeles. Ahora el desplazamiento horizontal está bloqueado también por código
  (al desplazarse, al enfocar un nodo y al cambiar de tamaño), y el árbol recorta
  la sangría de los niveles lejanos (con «…») para que el nombre del checkpoint
  siga a la vista.

- En un modelo de varios colores, cada pieza ya no se marca como «abierta» ni la
  estimación avisa que su volumen no es fiable: al separar por color, la cara
  donde dos colores se tocan queda abierta en cada pieza aunque el modelo entero
  sea estanco. El aviso solo aparece si el modelo completo está abierto.

- Con teclado en español, ⌘/Ctrl+Shift+0 (modo Enfoque) ya no cambia además el
  tamaño de la interfaz: Shift+0 escribe «=» en ese teclado y se tomaba también
  como «agrandar».

- **Los tamaños mínimos de los paneles ahora siguen al tamaño de la interfaz.**
  Se fijaban al crear el panel y, al cambiar el tamaño de la interfaz con la app
  abierta, no se recalculaban hasta reabrirla. También **Acoplar** funciona
  cuando todos los paneles están flotando (antes no hacía nada).
- Los diálogos «Nuevo workspace» y «Guardar checkpoint» abren con el cursor en
  el campo del nombre: antes el foco quedaba en el diálogo y había que hacer
  clic para empezar a escribir.

- **La ventana ya no queda «corrida».** En macOS, a veces la cabecera
  desaparecía, los semáforos de la ventana quedaban encima del contenido, la
  barra lateral se veía cortada por la izquierda y faltaba la barra de estado:
  todo el documento se había desplazado hacia arriba y a la izquierda (al enfocar
  la terminal, el editor o un diálogo) y no había forma de volver. Ahora el
  documento no puede desplazarse en ningún caso.
- **Nada se corta en ventanas angostas.** En el tamaño mínimo (1024 × 640) las
  pestañas de los paneles y las barras de la terminal, el chat, el editor y
  el visor se cortaban. Ahora los rótulos pasan a solo ícono (con su ayuda
  emergente) o la barra se reparte en dos filas, y lo mismo ocurre al agrandar la
  interfaz.
