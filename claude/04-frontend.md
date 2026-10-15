# Frontend — reglas React/TS

Skills: `vercel-react-best-practices`, `react`, `tailwind-design-system`,
`frontend-design`, `ui-ux-pro-max`.

- React 19, TypeScript estricto, Vite 7, Tailwind v4 CSS-first (sin
  `tailwind.config.js`; tokens en `src/index.css` con `@theme`). **pnpm**, nunca
  npm ni yarn.
- **Solo tokens del tema**: `bg-bg`, `bg-surface`, `bg-surface-2`, `bg-raised`,
  `border-line`, `border-line-strong`, `text-fg`, `text-muted`, `text-faint`,
  `accent`, `accent-soft`, `ok`, `warn`, `danger`, `info`. Un color suelto rompe
  el modo claro. Oscuro por defecto (`.dark` en `<html>`).
- Estética: taller — grafito, naranja filamento, rótulos en monoespaciada
  (`font-mono uppercase tracking`), radios de 6–10 px, transiciones ≤150 ms y
  respeto a `prefers-reduced-motion`.
- **Todo control interactivo lleva `title`** (el `Button` lo exige por tipo).
- Nunca `confirm()`/`alert()`: `confirmDialog()` y `toast` de `components/ui`.
- Errores del backend con `errorText(e)` (quita el código invisible).
- Sin fetch: todo pasa por los bindings de `wailsjs/`. Única excepción: la
  malla del visor (`GET /mesh/<clave>.stl`, ver abajo).
- Componentes: `components/ui` (primitivas), `layout`, `workspace`, `settings`,
  `render`, `viewer`, `editor`, `problems`, `dock`;
  helpers sin estado en `lib/`.
- macOS: cabecera con `--wails-draggable` y 82 px a la izquierda para los
  semáforos (`TitleBarHiddenInset`); Windows usa el marco nativo.

## Visor 3D (S2) — `components/viewer/`

- **Entra con `React.lazy`** (`RenderPanel`): three + R3F + drei van en su
  propio chunk (~970 kB, 265 kB gzip) y la app abre sin cargarlos. Una vez
  montado, el visor no se desmonta (queda oculto con un render fallido) para
  conservar la cámara.
- **Z arriba**, como OpenSCAD: `camera.up = (0,0,1)` en las dos cámaras
  (`camera.ts`), vistas definidas como dirección desde la que se mira. La pieza
  se centra en XY sobre la placa y se apoya en Z=0 al cargarla (`lib/mesh.ts`).
- **Malla por HTTP, no por binding**: `meshUrl(meshPath)` = `/mesh/` + nombre
  base (acepta `\` de Windows) → `fetch` → `STLLoader.parse` → normales con
  pliegue a 30° (planas por encima de 400 k triángulos). Caché **LRU** en
  memoria por clave (24 mallas o 300 MB) y deduplicación de pedidos en vuelo:
  volver a un checkpoint visto no pide nada. Mientras carga, la malla anterior
  sigue a la vista.
- **Sin red en runtime**: nada de `<Environment preset>` de drei (baja HDR de un
  CDN). El entorno sale de `RoomEnvironment` + `PMREMGenerator`. De drei solo
  `OrbitControls` y `ContactShadows`.
- La cama, la cuadrícula y la caja de volumen van en la **capa 1**: la ven las
  cámaras del visor y no la de `ContactShadows`. Desde abajo la placa se oculta.
- Colores de la escena desde los tokens del tema (`hooks/useThemeColors.ts`,
  lee las variables CSS y se actualiza al cambiar claro/oscuro). Las muestras
  son colores del usuario (`settings.swatches`), igual que los de un proyecto.
- `frameloop="demand"`: se dibuja solo al mover la cámara o cambiar algo. La
  captura PNG renderiza un cuadro y lee el canvas en el mismo tick (sin
  `preserveDrawingBuffer`), y lo compone sobre el color de superficie.
- Vista dividida = dos `<Canvas>` con cámara y controles propios y la **misma**
  `BufferGeometry` (no se clona). La barra y el teclado actúan sobre el panel
  activo.
- Banco de capturas: la malla de ejemplo es `public/uishot/enclosure.stl`
  (plantilla `electronics-enclosure.scad`); el banco redirige `/mesh/*` a ella.
  El build borra `dist/uishot/` (plugin en `vite.config.ts`): no va al binario.
  WebGL en Chrome headless corre en SwiftShader (`scripts/uishot.sh`).

## Editor y ventana (S4)

- Editor: ver [10-editor.md](10-editor.md#frontend-tanda-4b--componentseditor).
  Chunk propio (~3,4 MB, ~890 kB gzip), lazy como el visor.
- **Layout** (`hooks/useLayout.ts`): solo la barra lateral y la barra de
  estado (`Splitter` entre barra lateral y paneles, y entre workspaces y
  checkpoints; `statusBarHidden`). Se previsualiza en vivo y se guarda al soltar
  con `SaveLayout`. Todo lo demás —visor, código, agentes, chat, problemas— son
  **paneles acoplables**: ver «Paneles acoplables (S4b)». ⌘B/Ctrl+B pliega la
  barra lateral (listener en captura).
- Atajos en los `title`: `withKeys(texto, 'mod+s')` de `lib/platform.ts` (⌘ en
  macOS, Ctrl en Windows, según `boot.platform`).
- Estado que cambia seguido y leen pocos (cursor, lint): `lib/store.ts`
  (`createStore` + `useSyncExternalStore`), no estado de `App`.
- Banco de capturas: vistas `editor`, `editor-edit`, `editor-search`,
  `problems`, `save-checkpoint` (opción `editor` de `ShotOptions`); el banco
  importa el `catalog.json` real (`server.fs.allow: ['..']`, solo dev) y su
  sincronización WebGL salta los canvas de Monaco (un `getContext('webgl')` deja
  sin contexto 2D a la regla de Monaco).

## Tamaños y escala (S10)

**Regla: nunca `text-[Npx]` ni un ícono con tamaño fijo.** Todo tamaño de texto
o de ícono pasa por una utilidad escalada (`grep -rn "text-\[[0-9]*px\]" frontend/src`
tiene que dar vacío). Se definen en `src/index.css`, no como clase arbitraria:

- **Texto**: `text-ui-N` (`N` = 10, 10-5, 11, 11-5, 12, 12-5, 13, 13-5, 14, 15; el
  `-5` es «punto cinco»), que vale `N px × --ui-font-scale`. El `body` también
  escala (`13px × --ui-font-scale`), así que lo que hereda crece igual. Un valor
  nuevo se agrega en `index.css`.
- **Ícono** (lucide) **y la caja que lo contiene**: `icon-ui-N` (`N` = 12, 14, 16,
  20, 24, 28, 36) = ancho y alto `N px × --ui-font-scale`. Se elige `icon-ui-N`
  y no `size-[1em]` porque los íconos van sueltos, sin texto de referencia, y un
  `em` heredaría el tamaño del contenedor equivocado. Un punto de 6 px o un
  círculo decorativo **no** son íconos: siguen con `size-1.5`.
- **Letra del chat**: `text-chat` / `text-chat-sm` (`--chat-font-size`; los
  encabezados del markdown van en `em` relativos a ella). La del editor y la de
  la terminal son opciones de Monaco/xterm, no CSS.

**Qué escala cada preferencia** (`lib/appearance.ts`, `hooks/useAppearance.ts`):

| Preferencia | Escala | Mecanismo |
|---|---|---|
| `uiScale` 90/100/115/130/150 % | texto e íconos de la interfaz, **no** los espacios | variable `--ui-font-scale` en `<html>` |
| `editorFontSize`, `terminalFontSize`, `chatFontSize` (9–28 px) | solo su área, independiente de `uiScale` | Monaco `fontSize`, xterm `fontSize`, `--chat-font-size` |

Se **aplican en vivo y se guardan al instante** (`SaveAppearance`, 300 ms de
espera), sin pasar por «Guardar» de Configuración; `applyAppearance` escribe las
variables y la store `appearance` (que leen xterm y los cálculos de «¿cabe?»).
Atajos ⌘/Ctrl `+` `-` `0` en captura sobre `window`: con el foco dentro de un
elemento con `data-font-area` (`editor`, `terminal`, `chat`) cambian la letra de
esa área; fuera, recorren los presets de la interfaz. Un nuevo área con letra
propia = su `data-font-area` + su campo en `Appearance` (Go y TS) + su fila en
`AppearanceSettings.tsx`.

**El documento nunca se desplaza** (bug del documento «corrido» en WebKit): en
`index.css`, `html, body, #root { overflow: clip }` (`hidden` solo como
respaldo). `hidden` bloquea la rueda pero no el desplazamiento programático
(`focus()` de xterm/Monaco/`<dialog>`, `scrollIntoView`); `clip` no admite
ninguno. Los contenedores de la estructura usan `overflow-clip`, no
`overflow-hidden`, y todo hijo flex/grid que pueda empujar el tamaño lleva
`min-h-0`/`min-w-0`. `lib/scrollGuard.ts` (instalado en `main.tsx`) devuelve el
documento a 0,0 si algo lo mueve y reajusta los elementos marcados `data-lock`.
Verificación: `./scripts/uishot-fit.sh [vista ancho alto]` (con
`UISHOT_SCALE=150` para otra escala) abre el banco con `poke=1`, que intenta
desplazar la página, y falla si `scrollWidth/Height` superan la ventana o la
cabecera / barra de estado no quedan en su sitio.

**Degradar sin cortar controles.** Toda barra con rótulos usa `hooks/useNarrow.ts`
(`const [compact, ref] = useNarrow(limit)`): mide **su propio elemento** (no la
ventana) y compara con `limit × escala`, así que con la interfaz agrandada pasa
a solo ícono antes. En modo compacto el rótulo se omite y el `title` queda como
nombre; si ni así cabe, la barra envuelve (`flex-wrap`) o recorta datos
secundarios enteros, nunca a medias. Ya lo usan la cabecera, el menú Ver, la
barra de la terminal, el editor y la barra de estado del render. (Las pestañas
de los paneles no lo necesitan: no se encogen, la barra se desplaza.)

**Tamaño de cada sección.** La barra lateral y el reparto workspaces ↔
checkpoints se arrastran y se guardan en `layout` (`hooks/useLayout.ts`); los
límites se calculan con la ventana actual (`sidebarMaxFor(innerWidth)`) y el
tamaño guardado solo se **recorta al mostrarlo**, así que al volver a agrandar la
ventana reaparece. Los de los paneles los lleva dockview (ver abajo).
«Restablecer el diseño» de Configuración llama a `resetLayout` (barra lateral,
barras visibles) **y** a `resetLayout` del dock (paneles al predeterminado).

## Paneles acoplables

Ver [claude/12-paneles.md](12-paneles.md).
