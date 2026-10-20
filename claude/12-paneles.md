# Paneles acoplables (estilo editor)

Spec: [.claude/specs/s4b-paneles-acoplables.md](../.claude/specs/s4b-paneles-acoplables.md).
Se separó de [claude/04-frontend.md](04-frontend.md) por tamaño (regla de
segmentación de `CLAUDE.md`).


Plan: [.claude/specs/s4b-paneles-acoplables.md](../.claude/specs/s4b-paneles-acoplables.md).
`dockview-react` 8.x (MIT) reemplaza a `Workbench` (que se borró). Es un chunk
propio (`DockWorkbench`, ~420 kB, ~99 kB gzip + ~51 kB de CSS): `App` lo carga con
`React.lazy`. El chunk principal pasó de 426 a 448 kB.

**Idea central: el contenido vive fuera de dockview.** dockview solo reparte
huecos (`Slot` en `DockParts.tsx`); el contenido de cada panel lo dibuja
`PanelHost.tsx` con un portal en un elemento persistente por panel
(`hosts.ts`), que el `Slot` cuelga de su grupo y, al irse, **aparca** (`display:none`
en `<body>`) con todo adentro. Por eso mover, dividir, flotar y **ocultar** (la ×)
un panel no desmonta nada: el visor conserva su contexto WebGL y su cámara, Monaco
su modelo y su deshacer, y xterm su pantalla (el proceso del agente es de Go y
sigue vivo). Un panel se monta la primera vez que se muestra. Lo que usa un
`<dialog>` y debe funcionar con el panel oculto (el de guardar de `EditorPane`)
va en un portal al `body`, no dentro del panel.

| Archivo | Qué hace |
|---|---|
| `panels.ts` | catálogo: ids, ícono, atajo (⌘⇧N), mínimos, título |
| `dock.ts` | controlador (módulo, no hook): `dockState` (presentes, visibles, flotantes, minimizados, zen, diseños guardados), mostrar/ocultar/enfocar, flotar/acoplar, maximizar, **minimizar**, **mover a…**, diseños, modo enfoque, `equalizeAtSash`, guardado con espera |
| `layouts.ts` | presets (`buildPreset`), sobre JSON `{v, dock, min?}` y su validación (`parseEnvelope`, `parseMinimized`) |
| `DockWorkbench.tsx` | el `<DockviewReact>`, atajos (captura en `window`), doble clic en divisores |
| `DockParts.tsx` | `Slot`, `DockTab` (pestaña y su menú contextual), `GroupActions` (minimizar, maximizar y ⋯), `EmptyDock` (sin paneles), `panelMenuEntries` (el menú, el mismo en la pestaña y en ⋯) |
| `ViewMenu.tsx` | menú Ver de la cabecera + diálogos «Guardar diseño como…» y «Diseños guardados» |
| `PanelHost.tsx` | contenido de cada panel; lleva las terminales (`useTerminals`) y los problemas |
| `dock.css` | tema `.dockview-theme-scadlab` con los tokens; pestañas, divisores, flotantes |

**Cómo agregar un panel nuevo.** (1) su id en `PANEL_IDS` y su entrada en
`PANEL_ICON`, `PANEL_SHORTCUT` (el siguiente número libre), `PANEL_MIN`,
`panelTitle` y `panelHint` (`panels.ts`); (2) su contenido en el `switch` de
`PanelHost.tsx`; (3) los textos en `i18n` (`es.ts` y `en.ts`); (4) dónde aparece
en cada preset de `layouts.ts` y, si no, en `placement` de `dock.ts`; (5) el
estado que deba verse en la pestaña (punto, contador) en `panelStatus` y
`DockTab`. Un id nuevo vuelve **inválidos** los diseños guardados que traigan
uno que el código no conoce (caen al predeterminado), pero un diseño viejo sin
el panel nuevo sigue cargando: el panel nuevo aparece al mostrarlo desde Ver.

**Persistencia.** `api.toJSON()` envuelto en `{v: LAYOUT_VERSION, dock}` se
guarda con `SaveDockLayout` (espera de 600 ms, y al salir con `pagehide`); los
diseños propios, con `SaveDockLayouts`. `parseEnvelope` rechaza JSON roto,
otra versión, paneles desconocidos o un `contentComponent` distinto de `slot`; si
`fromJSON` falla o devuelve algo raro, se arma el predeterminado. Cambiar la
forma de lo guardado = subir `LAYOUT_VERSION`. En modo enfoque no se guarda.

**Minimizar un grupo** (botón **–** a la izquierda de maximizar, ⌘/Ctrl+Shift+M sobre
el grupo con el foco, Ver → Minimizar panel). Achica sin cerrar ni desmontar nada
(mismo contrato que ocultar: el contenido vive fuera de dockview). Según el lado:
grupo **apilado** (padre `VERTICAL`) o **flotante** → queda solo su barra de pestañas
(`data-min="bar"`, alto = barra × escala); grupo **lado a lado** (padre `HORIZONTAL`)
→ franja de 36 px × escala con los íconos en vertical (`data-min="strip"`, CSS en
`dock.css`; el punto de estado queda como marca en la esquina del ícono). Cómo se
logra: restricciones **del grupo** con mínimo = máximo en la dimensión que se achica
(`desiredConstraints` / `syncConstraints`), más un `setSize`; los vecinos absorben el
resto. Ese mínimo especial vale solo mientras está minimizado: al restaurar el grupo
recupera los mínimos de su panel activo (`PANEL_MIN` × escala) y vuelve a `prev`, el
tamaño guardado al minimizar. **Ojo, dockview 8.5:** `panel.api.setConstraints` no
tiene a nadie escuchando (el grupo lee `activePanel.minimumWidth`, fijado al crear el
panel); lo único efectivo en runtime son las restricciones explícitas del grupo
(`group.api.setConstraints`), que mandan sobre las del panel y no se pueden «borrar»,
solo reescribir: por eso `syncConstraints` las fija SIEMPRE en todos los grupos (también
sin minimizar) y se repasa en cada cambio de layout, de pestaña activa y de escala.
`reconcileMinimized` olvida los grupos que desaparecieron o se quedaron solos, sigue a
un grupo que cambió de orientación y repasa las marcas. No se puede minimizar un grupo
sin vecino que ocupe su espacio (`canMinimize`: el botón sale deshabilitado, con su
motivo en el `title`). Restaurar: clic en cualquier pestaña, en el **+**, doble clic en
el vacío de la barra, o mostrar/enfocar el panel (Ver, atajos ⌘⇧N). Maximizar, el modo
enfoque y mover un panel a un grupo minimizado lo restauran antes. Se **guarda** en el
sobre (`min: [{panel, dim, prev}]`, anotado por un panel del grupo porque el id del
grupo lo asigna dockview al cargar); un diseño sin `min` carga igual (sin subir
`LAYOUT_VERSION`).

**«Mover a…»** (menú ⋯ y clic derecho en la pestaña; el menú contextual es propio, ya no
el de dockview, para tener submenús: `Menu` admite `submenu`, que se abre dentro del
mismo menú con una fila «Atrás»). Destinos que no dependen de arrastrar —el arrastre en
la webview no se puede probar solo, esta es la vía garantizada—: Izquierda / Derecha /
Arriba / Abajo **respecto del área completa** (`movePanelTo`: si un grupo ya ocupa ese
borde —hijo directo de la raíz en el eje que toca— el panel entra a él como pestaña; si
no, `addGroup({direction})` sin referencia crea uno que abarca todo el borde), Centro
(pestaña del grupo acoplado más grande que no sea el propio ni uno minimizado), «Junto
a…» (`movePanelBeside`: pestaña del grupo de otro panel a la vista) y Flotar/Acoplar.
Un destino que no cambiaría nada sale deshabilitado (`moveDisabled`). Ojo:
`panel.api.moveTo({position})` **sin** `group` mueve al centro del propio grupo (no
hace nada): para un grupo nuevo hay que crearlo con `addGroup` y moverlo ahí.

**Tamaños.** Mínimos por panel (`PANEL_MIN`, × escala de la interfaz) como
restricciones de dockview; dockview no corrige un grupo que nace por debajo de su
mínimo al soltar una pestaña en un borde, así que `enforceMinimums` fuerza una
pasada de `layout`. `proportionalLayout` reparte en proporción al achicar la
ventana. Divisores: línea de 1 px y zona de agarre de 6 px (`::before` del sash);
los tiradores de los flotantes se hacen de 8 px por **fuera** del borde porque el
contenido flotante se dibuja en una capa encima (sin `overflow: clip` en el
contenedor, que los recortaría). Doble clic en un divisor: `equalizeAtSash`
simula el arrastre con los mismos eventos de puntero.

**Arrastre con eventos de puntero (`dndStrategy: 'pointer'`), no HTML5.**
`main.go` activa `DragAndDrop{EnableFileDrop, DisableWebViewDrop}`, y en macOS
`WailsWebView.m` responde a **cualquier** arrastre con tipos de pasteboard sin
llamar al `performDragOperation` del WKWebView: un arrastre HTML5 dentro de la
página nunca dispararía `drop`. Los eventos de puntero no pasan por ahí. Efecto
conocido de dockview en este modo: el grupo flotante se mueve con un arrastre
normal de su barra, pero si el **primer** movimiento del ratón ya supera el
umbral (unos 4 px) arranca un arrastre de reacoplado en vez de mover. Con un ratón
real no pasa. **Sin probar en la ventana real de Wails** (WKWebView/WebView2):
verificado en Chrome headless con el banco y CDP (soltar en borde de grupo, en el centro y en la barra de otro grupo). Soltar en el borde de un grupo divide **ese** grupo; el borde de toda el área (zona `dndEdges` de dockview) no se alcanza con este arrastre, por eso «Mover a…» → Izquierda/Derecha/Arriba/Abajo es la vía para llevar un panel al borde completo. El indicador de destino es un relleno de acento al 24 % con borde sólido de 2 px (`--dv-drag-over-*` en `dock.css`), visible también sobre el visor naranja.

**Atajos** (listener en captura, antes que Monaco y xterm): ⌘/Ctrl+Shift+M = minimizar/restaurar el grupo con el foco (Monaco solo usa Ctrl+Shift+M en macOS —con Control, no ⌘— y Ctrl+M en Windows, sin Shift; en la terminal Ctrl+M ya es Enter), ⌘/Ctrl+Shift+1…7
(en macOS también ⌘⌥1…7, porque el sistema se queda con ⌘⇧3/4/5), ⌘/Ctrl+Shift+0
= modo enfoque (se descartó ⌘K Z: ⌘K es el prefijo de acordes de Monaco). Esc sale
del modo enfoque salvo con el foco en `.monaco-editor`, `.xterm`, un `dialog` o un
menú. **Menú nativo «Ver» de macOS: no se agregó** (`appMenu` queda con
Aplicación y Edición): Windows no tiene menú nativo, el menú Ver de la cabecera es
el mismo en las dos plataformas y evita emitir eventos Go → UI para cada acción.

**Banco de capturas.** `dock-default`, `dock-custom`, `dock-view-menu`,
`dock-layouts`, `dock-hidden`, `dock-150`, `dock-minimized` (código a franja y agentes a barra) y
`print-panel` (opción `dock: 'print'`: el grupo de abajo al 46 %), `dock-move-menu` (menú ⋯ del código con «Mover a…» abierto, con clics de verdad vía
`dockShot.menu`) (opciones `panel`, `dock`, `dockMenu` y `viewMenu`
de `ShotOptions`; `window.__dock` expone `shotTools` para pruebas con CDP, y
`?dockraw=` inyecta un diseño guardado). `dock: 'tall'` agranda el grupo de abajo
para las capturas de terminal y chat.

**Panel «Impresión» (`print`, S7).** Séptimo panel: ícono `Weight`, atajo ⌘/Ctrl+Shift+7
(`PANEL_SHORTCUT.print = 7`; el regex de `DockWorkbench` pasó de `[0-6]` a `[0-7]`), mínimo 300×120.
Contenido: `components/print/PrintPanel.tsx`. En los presets entra como **pestaña junto a
Problemas** (`tabs('agent-1', [..., 'problems', 'print'])` y, en «Modelado», junto al código); en
`dock.ts` está en `BOTTOM`, así que reaparece debajo del visor. Un diseño guardado antes de S7 sigue
cargando sin el panel (aparece desde Ver); «Restablecer diseño» lo trae. Los datos no viven en el
panel sino en `lib/printSession.ts` (store de módulo + `usePrintSession`, montado UNA vez en
`PanelHost`): lo comparten el visor, la tabla, la barra de estado (aviso de placa) y la tarjeta de
la parte. El modelo por partes solo se pide mientras se ve el visor **o** este panel
(`visible.viewport || visible.print`); `GetModelParts` lanza un render 3MF y no debe correr con
ninguno a la vista.

**Estilo de pestañas.** Ver `dock.css`: ícono + título en `text-ui-13` normal, la
activa con el fondo del contenido y la × siempre visible, las inactivas con el
fondo de la barra (`--c-bg`) y la × al pasar el ratón, el punto de estado que
vuelve a ser × con el ratón (`data-dot`), barra de 35 px × `--ui-font-scale`, y
línea de acento arriba en la pestaña activa del grupo con el foco.

## Pestañas de proyecto (S8)

Spec: [.claude/specs/s8-pestanas-proyecto.md](../.claude/specs/s8-pestanas-proyecto.md). Una ventana, hasta 4
workspaces abiertos (`MAX_PROJECTS` = `store.MaxOpenProjects`). El **dock es uno solo** y su contenido sigue al
proyecto activo; no hay un diseño por proyecto. Backend: [29-contrato-workspaces.md](29-contrato-workspaces.md#proyectos-abiertos--app_projectsgo-s8).

| Archivo | Qué hace |
|---|---|
| `components/layout/ProjectTabs.tsx` | la barra (cabecera, a la derecha del logo): pestañas, «+» y menús; arrastre con eventos de puntero |
| `App.tsx` | `order` + `activeId` + `projects: Record<id, Project>` (`view`, `selected`, `source`, `editor`); `activate`, `openTab`, `closeTabs`, `reorder`, atajos, arranque |
| `hooks/useRenders.ts` | resultados y renders en curso de **todos** los abiertos (`byWs`); los eventos `render:*` llevan `workspaceId` |
| `hooks/useProjectActivity.ts` | indicadores de fondo: eventos locales + `ProjectActivity()` (foto del backend para lo que aún no mandó eventos) |
| `hooks/useTerminals.ts` | ahora lo llama `App` (no `PanelHost`): `all` (todas las terminales por pestaña «Agente») y `alive` (por proyecto) |
| `lib/projectsApi.ts` | `MAX_PROJECTS` y los alias de los bindings de proyectos |

**Qué conserva cada proyecto al cambiar de pestaña.** Checkpoint elegido y su código (`Project`); lo que el editor
tiene y no vive en otro lado se anota **al irse** (`snapshotActive` → `Project.editor: EditorSnap`: código sin
guardar, si editaba, comparación con el padre) y `EditorPane` lo aplica una vez al volver (`restore` /
`onRestored`; `docKey` lleva el `workspaceId`, así que volver siempre es un documento nuevo). El snapshot es del
dueño del editor (`editorOwner`, el último commit), no del activo: con dos cambios en el mismo tick el segundo no
anota el estado del primero en el proyecto equivocado. La **parte seleccionada** se anota por contexto
(`rememberPartSelection` en `printSession`, se recupera una vez); el **chat elegido** por workspace
(`chosenChat` en `ChatPanel`); resultados de render y terminales viven por workspace desde antes. Lo que **no** se
conserva: el historial de deshacer de Monaco (un solo editor, se recarga el texto) ni la cámara del visor.

**Terminales.** Cada xterm de cada proyecto sigue montado (oculto con `hidden`) para conservar pantalla y
desplazamiento; solo cambia cuál se ve (`AgentTerminalTab`, `allTerms`). Al cerrar un proyecto,
`projectClosed(id)` (`sessionBridge`) hace que `useTerminals`, `ChatPanel` y `printSession` suelten su estado.

**Indicadores** (por pestaña, `lib/ProjectTab`): render en curso (spinner), último render del checkpoint más nuevo
fallido (triángulo rojo; igual que `ProjectActivity.renderFailed`), agentes vivos (ícono verde, y el número si
son 2) y cambios sin guardar (punto naranja). Con poco ancho (`useNarrow`, 150 px por pestaña) se omite el ícono
de carpeta y queda el indicador más urgente.

**Arranque y eco.** El backend no abre nada solo: `ListOpenProjects` + `OpenProject` por cada pestaña (el activo
al final). La primera vez tras actualizar, sin pestañas guardadas, se abre el último workspace usado
(`localStorage` `scadlab.projects.seen`); después una lista vacía se respeta. `projects:changed` es también el eco
de lo que hace la propia UI: se ignora mientras hay una operación en vuelo (`opsInFlight`) y solo se adopta un
proyecto cerrado por el backend y un orden distinto; cuál está al frente lo decide la UI.

**Atajos** (captura en `window`, `App.tsx`): ⌃Tab / ⌃⇧Tab (Control también en macOS), ⌘/Ctrl+1…4, ⌘/Ctrl+W. No
chocan con el dock (⌘⇧N / ⌘⌥N), el visor (1–7 sin modificador), el zoom (⌘/Ctrl + − 0), ⌘B/⌘S/⌘E, Monaco ni xterm
(que no usan estas combinaciones). Excepción: en **Windows** con el foco en `.xterm`, **Ctrl+W** es de la shell
(borrar palabra), así que ahí cierra **Ctrl+Shift+W**. Con un `dialog[open]` no actúan. Ctrl+Tab en WebView2 y
WKWebView **no está probado en las ventanas reales** (sí con eventos sintéticos en el banco).

**Banco.** `projects-tabs` (3 proyectos: el 2 con render en curso y cambios sin guardar, el 3 con agente vivo y
último render fallido), `projects-plus-menu` y `projects-narrow` (se captura a 1024 px). `window.__proj` expone
`editorSession`, `editorCommands` y `selectPart` para las pruebas con CDP. `uishot-fit.sh` comprueba también que el
contenido de la cabecera no sea más ancho que ella (`headerSw`/`headerCw`).
