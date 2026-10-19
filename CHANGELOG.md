# Changelog

Todos los cambios visibles de ScadLab. Formato [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/),
versionado [SemVer](https://semver.org/lang/es/); la versión vigente está en `VERSION`.
Todo cambio se acumula primero en `[Unreleased]` y se vuelca a su versión al
empaquetarla (ver `.claude/specs/releases.md`).

## [Unreleased]

### Agregado

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
