# Changelog

Todos los cambios visibles de ScadLab. Formato [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/),
versionado [SemVer](https://semver.org/lang/es/); la versión vigente está en `VERSION`.
Todo cambio se acumula primero en `[Unreleased]` y se vuelca a su versión al
empaquetarla (ver `.claude/specs/releases.md`).

## [Unreleased]

### Agregado

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
  casilla por panel (con su atajo ⌘⇧1…6, Ctrl+Shift+1…6 en Windows; en macOS
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

### Cambiado

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
