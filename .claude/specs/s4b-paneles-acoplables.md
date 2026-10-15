# Paneles acoplables (estilo editor)

Pedido del usuario antes de S6: «que se puedan ocultar, mover y separar en
pestañas, estilo editor, los paneles visor 3D, chat, agentes y código, para
tener múltiples ventanas/paneles». Reemplaza la grilla fija con divisores de
S4 (`Workbench`, `Splitter`, `useLayout`) por un sistema de paneles acoplables.

## Decisiones (architect)

- **Librería: `dockview-react`** (MIT, sin dependencias aparte de su núcleo,
  React 19): pestañas, grupos, divisiones por arrastre a los bordes, grupos
  flotantes, maximizar, serialización `toJSON`/`fromJSON`. Descartadas:
  rc-dock (lodash + rc-components), flexlayout-react (flotantes menos maduros).
- **Ventanas**: Wails v2 tiene una sola ventana nativa. Los «popout» de
  dockview usan `window.open`, que en Wails no crea una ventana nativa: **no se
  usan**. Las ventanas extra son **grupos flotantes dentro de la app**
  (se arrastran y redimensionan libremente). Ventanas nativas = Wails v3, fuera
  de alcance; documentarlo en la ayuda.
- **Nada se destruye al mover u ocultar**: visor (contexto WebGL), código
  (Monaco con su modelo y deshacer) y terminales (xterm + proceso) usan
  `renderer: 'always'` o el mecanismo equivalente de dockview para que el
  componente siga montado. **Ocultar un panel de agente NO mata al agente**:
  solo «Cerrar» en su barra lo termina (como hoy). Al volver a mostrarlo se ve
  la misma pantalla.
- La **barra lateral** (workspaces + checkpoints) queda fija y plegable como
  hoy (fuera de este cambio). Panel inferior y divisores de S4 desaparecen:
  todo lo que estaba en ellos pasa a paneles.

## Paneles

| id | Título | Notas |
|---|---|---|
| `viewport` | Visor 3D | incluye su barra de estado de render y diagnósticos; la «vista dividida» interna del visor se mantiene |
| `source` | Código | editor Monaco |
| `agent-1`, `agent-2` | Agente 1 / Agente 2 | terminales de agente |
| `chat` | Chat | |
| `problems` | Problemas | |

Cada panel aparece una sola vez (abrirlo de nuevo lo enfoca).

## Funciones

- Pestañas: arrastrar para reordenar; arrastrar a otro grupo; arrastrar a un
  borde para dividir (izquierda/derecha/arriba/abajo); doble clic en la pestaña
  = maximizar/restaurar el grupo; menú contextual de la pestaña: Cerrar,
  Cerrar los demás, Flotar, Acoplar (si está flotante), Mover a la derecha/abajo.
- **Ocultar/mostrar**: menú **Ver** en la cabecera (casillas por panel +
  diseños), también en el menú nativo de macOS (`appMenu` en `main.go`: agregar
  un menú «Ver» que emita eventos a la UI, o dejarlo solo en la cabecera si el
  menú nativo complica: decidir y documentar). Atajos: ⌘/Ctrl+Shift+1…6 =
  mostrar y enfocar Visor, Código, Agente 1, Agente 2, Chat, Problemas (si ya
  está enfocado, lo oculta). No pisar atajos de Monaco/xterm.
- **Flotar**: un panel o grupo pasa a ventana flotante dentro de la app.
- **Diseños**: presets «Predeterminado» (como hoy: visor + código a la
  derecha + agentes/chat/problemas abajo), «Modelado» (visor grande, código y
  agentes como pestañas a la derecha), «Código e IA» (código grande + agente
  al lado + visor chico), «Pantalla dividida» (visor y código mitad y mitad,
  agentes abajo); **Restablecer diseño**; y diseños propios: «Guardar diseño
  como…» con nombre, listarlos, aplicarlos y borrarlos.
- **Persistencia**: el diseño actual se guarda solo (debounce) y se restaura
  al abrir. Settings nuevos (sin migración, key/value): `dockLayout` (JSON de
  dockview) y `dockLayouts` (diseños guardados `{name, json}`), con un binding
  liviano propio (p. ej. `SaveDockLayout(json)`, `SaveDockLayouts(list)`).
  Validar al cargar: JSON roto, versión distinta o paneles desconocidos → caer
  al predeterminado (sin romper la app). Los settings `layout` de S4 que ya no
  se usen se dejan (no se borran: regla de datos) pero se documentan como
  obsoletos.
- **Estilo de pestañas = pestañas de editor estilo VS Code** (referencia que
  mandó el usuario: barra de pestañas con «🔶 ScadLab en Wails y React» y
  «🔶 Claude Code ×»):
  - cada pestaña: **ícono del panel + título en letra normal** (sans, tamaño
    `text-ui-13`, mayúsculas/minúsculas normales — **no** mono ni MAYÚSCULAS
    ni tracking ancho como los rótulos de sección actuales);
  - **pestaña activa**: fondo igual al del contenido del panel (se funde con él,
    sin línea entre ambos), borde fino a los lados y arriba, texto `text-fg`,
    y la **×** visible siempre;
  - **pestañas inactivas**: fondo de la barra (`bg`/`surface-2`, más oscuro),
    texto `text-muted`, la × aparece al pasar el ratón; hover aclara el fondo;
  - indicador de estado en lugar de la × cuando aplica (punto `●` = código con
    cambios sin guardar; punto verde = agente vivo), que vuelve a ser × al pasar
    el ratón (como VS Code);
  - barra de pestañas de alto ~35 px escalable, sin rellenos grandes; a la
    derecha del grupo, acciones del grupo (maximizar, más opciones);
  - ancho de pestaña según el título, con elipsis si no cabe; desplazamiento
    horizontal de la barra cuando hay muchas.
  Tema de dockview mapeado a los tokens (claro y oscuro), textos con
  `text-ui-N`/`icon-ui-N` (escala de interfaz), divisores de 1 px.
- Los atajos y comportamientos existentes siguen funcionando (escala ⌘±, F/W/E
  del visor, ⌘S/⌘F del editor, ⌘E export, guard de desplazamiento,
  `uishot-fit.sh`).

## Criterio de terminado

Verificación completa del repo + `./scripts/uishot-fit.sh` OK; capturas
nuevas (`dock-default`, `dock-custom` con un panel flotante y una división
distinta, `dock-view-menu` con el menú Ver abierto, `dock-layouts` con el
diálogo de diseños guardados) mirada una por una; ayuda: tema `ventana`
reescrito (paneles, pestañas, dividir, flotar, ocultar, diseños, atajos, la
limitación de ventanas nativas); `CHANGELOG.md`; `claude/04-frontend.md`
(sección «Paneles acoplables»: cómo agregar un panel nuevo); prueba de humo
abriendo la app real (permitido) — **nunca** disparar «Abrir en Bambu Studio».
