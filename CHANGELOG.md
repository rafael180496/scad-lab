# Changelog

Todos los cambios visibles de ScadLab. Formato [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/),
versionado [SemVer](https://semver.org/lang/es/); la versión vigente está en `VERSION`.
Todo cambio se acumula primero en `[Unreleased]` y se vuelca a su versión al
empaquetarla (ver `.claude/specs/releases.md`).

## [Unreleased]

### Agregado

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
  instalado, el visor lo dice y lleva directo a Configuración → OpenSCAD. El
  visor 3D en sí llega en la fase S2: por ahora se muestra el estado del render.
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
