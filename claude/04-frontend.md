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
  `render`, `viewer`, `editor`, `problems`;
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
- **Layout** (`hooks/useLayout.ts`): `Splitter` (divisor con arrastre por
  pointer capture, doble clic = tamaño de fábrica, flechas con foco) entre barra
  lateral, visor, código y panel inferior. Se previsualiza en vivo y se guarda al
  soltar con `SaveLayout`. Los tamaños se acotan al tamaño real (visor mínimo
  360×200). ⌘B/Ctrl+B pliega la barra lateral (listener en captura).
- Atajos en los `title`: `withKeys(texto, 'mod+s')` de `lib/platform.ts` (⌘ en
  macOS, Ctrl en Windows, según `boot.platform`).
- Estado que cambia seguido y leen pocos (cursor, lint): `lib/store.ts`
  (`createStore` + `useSyncExternalStore`), no estado de `App`.
- Banco de capturas: vistas `editor`, `editor-edit`, `editor-search`,
  `problems`, `save-checkpoint` (opción `editor` de `ShotOptions`); el banco
  importa el `catalog.json` real (`server.fs.allow: ['..']`, solo dev) y su
  sincronización WebGL salta los canvas de Monaco (un `getContext('webgl')` deja
  sin contexto 2D a la regla de Monaco).
