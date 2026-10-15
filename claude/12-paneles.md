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
| `dock.ts` | controlador (módulo, no hook): `dockState` (presentes, visibles, flotantes, zen, diseños guardados), mostrar/ocultar/enfocar, flotar/acoplar, maximizar, diseños, modo enfoque, `equalizeAtSash`, guardado con espera |
| `layouts.ts` | presets (`buildPreset`), sobre JSON `{v, dock}` y su validación (`parseEnvelope`) |
| `DockWorkbench.tsx` | el `<DockviewReact>`, atajos (captura en `window`), doble clic en divisores |
| `DockParts.tsx` | `Slot`, `DockTab` (pestaña), `GroupActions` (maximizar y ⋯), `EmptyDock` (sin paneles), `tabActions` |
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
verificado en Chrome headless con el banco y CDP.

**Atajos** (listener en captura, antes que Monaco y xterm): ⌘/Ctrl+Shift+1…6
(en macOS también ⌘⌥1…6, porque el sistema se queda con ⌘⇧3/4/5), ⌘/Ctrl+Shift+0
= modo enfoque (se descartó ⌘K Z: ⌘K es el prefijo de acordes de Monaco). Esc sale
del modo enfoque salvo con el foco en `.monaco-editor`, `.xterm`, un `dialog` o un
menú. **Menú nativo «Ver» de macOS: no se agregó** (`appMenu` queda con
Aplicación y Edición): Windows no tiene menú nativo, el menú Ver de la cabecera es
el mismo en las dos plataformas y evita emitir eventos Go → UI para cada acción.

**Banco de capturas.** `dock-default`, `dock-custom`, `dock-view-menu`,
`dock-layouts`, `dock-hidden` y `dock-150` (opciones `panel`, `dock` y `viewMenu`
de `ShotOptions`; `window.__dock` expone `shotTools` para pruebas con CDP, y
`?dockraw=` inyecta un diseño guardado). `dock: 'tall'` agranda el grupo de abajo
para las capturas de terminal y chat.

**Estilo de pestañas.** Ver `dock.css`: ícono + título en `text-ui-13` normal, la
activa con el fondo del contenido y la × siempre visible, las inactivas con el
fondo de la barra (`--c-bg`) y la × al pasar el ratón, el punto de estado que
vuelve a ser × con el ratón (`data-dot`), barra de 35 px × `--ui-font-scale`, y
línea de acento arriba en la pestaña activa del grupo con el foco.
