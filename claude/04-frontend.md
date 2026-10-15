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
- Sin fetch: todo pasa por los bindings de `wailsjs/`.
- Componentes: `components/ui` (primitivas), `layout`, `workspace`, `settings`;
  helpers sin estado en `lib/`.
- macOS: cabecera con `--wails-draggable` y 82 px a la izquierda para los
  semáforos (`TitleBarHiddenInset`); Windows usa el marco nativo.
