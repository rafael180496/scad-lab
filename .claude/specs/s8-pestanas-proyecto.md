# S8 — Varios proyectos: pestañas de proyecto

Fase del [roadmap](../../claude/01-roadmap.md). Decisión del usuario:
**pestañas de proyecto en una sola ventana** (no ventanas separadas). Hasta 4
proyectos (workspaces) abiertos a la vez; los de fondo siguen vivos.

## Decisiones (architect)

- **Una ventana, hasta 4 proyectos abiertos.** Barra de pestañas de proyecto
  arriba (en la cabecera, a la derecha del logo), estilo navegador: nombre,
  indicadores de actividad, ×, «+» para abrir otro (elige de la lista de
  workspaces o crea uno). Reordenables arrastrando (con «Mover a la izquierda/
  derecha» en el menú contextual como vía garantizada, igual que el dock).
- **Los de fondo siguen vivos**: su watcher, el auto-render, sus terminales de
  agente y la auto-iteración siguen funcionando. Cambiar de pestaña es
  instantáneo y conserva el estado de cada proyecto (checkpoint elegido, código
  sin guardar, selección de parte, diff abierto, chat elegido).
- **El dock (paneles) es uno solo** y su contenido sigue al proyecto activo
  (no hay un diseño por proyecto). Las terminales de agente pertenecen a su
  proyecto: los paneles Agente 1/2 muestran las del proyecto activo; el límite
  pasa a **2 terminales de agente por proyecto** (8 en total como máximo).
- **Cada agente ve los otros proyectos abiertos**: al lanzar un agente desde
  ScadLab se agregan las carpetas de los otros proyectos abiertos (solo
  lectura en las reglas: «puedes leer otros proyectos abiertos para reutilizar
  piezas, pero solo escribes en el tuyo»):
  - Claude Code: `--add-dir <dir>` (variádico: cada uno seguido de otro flag).
  - Codex: `--add-dir <dir>` (repetible).
  - Antigravity: `--add-dir <dir>` (repetible).
  - opencode: no tiene opción (verificado); solo ve el suyo. Documentarlo.
  Las carpetas se fijan al lanzar el agente: abrir otro proyecto después no
  cambia un agente ya abierto (documentarlo; «Nueva» lo relanza).
- **Persistencia**: settings `openProjects []int64` (orden de las pestañas) y
  `activeProject int64`; al arrancar se reabren (los que ya no existan se
  omiten). Migrar el comportamiento actual (un workspace abierto) = una pestaña.
- Cerrar una pestaña con **cambios sin guardar** o **agentes vivos** pide
  confirmación (`confirmDialog`) que dice qué se pierde (los agentes se
  cierran). Cerrar el último deja la pantalla vacía de hoy.
- Atajos: ⌃Tab / ⌃⇧Tab (siguiente/anterior), ⌘/Ctrl+1…4 (ir a la pestaña),
  ⌘/Ctrl+W cerrar la pestaña de proyecto activa (con la confirmación). Verificar
  que no chocan con Monaco, xterm, el visor (1–7 sin modificador) ni el dock
  (⌘⇧1…7).

## Reparto (dos devs en Sonnet, en paralelo)

| Bloque | Dueño | Alcance |
|---|---|---|
| 8.A backend multi-proyecto | fullstack-dev A | `app_projects.go` (nuevo), `app_workspaces.go` (watchers por proyecto en vez de uno), `app.go` (solo los campos del mapa de watchers, sin tocar el resto), `app_terminal.go` (límite por proyecto, `--add-dir`), `backend/store/settings.go` (`openProjects`, `activeProject`), `assets/workspace/RULES.md.tmpl` (párrafo de otros proyectos), tests |
| 8.B pestañas y estado por proyecto (UI) | fullstack-dev B | `frontend/**`, `index.html`, `CHANGELOG.md`, `scripts/help-shots.sh`, `scripts/uishot-fit.sh`, `docs/screenshots/**`, `claude/04-frontend.md` o `claude/12-paneles.md`, `claude/06-*.md`, `claude/01-roadmap.md` |

### Contrato 8.A → 8.B

| Binding | Devuelve | Nota |
|---|---|---|
| `ListOpenProjects()` | `{projects: [{workspace, missing}], activeId}` | en el orden de las pestañas |
| `OpenProject(id)` | `WorkspaceView` | lo agrega (si no estaba) y lo activa; error claro si ya hay 4 |
| `CloseProject(id)` | — | detiene su watcher y cierra sus terminales de agente |
| `SetActiveProject(id)` | — | solo persiste cuál está al frente |
| `ReorderProjects(ids []int64)` | — | |
| `ProjectActivity()` | `[{id, rendering, lastRenderStatus, agentsAlive, ...}]` | para los indicadores de las pestañas de fondo |

`OpenWorkspace(id)` sigue existiendo (compatibilidad) y equivale a
`OpenProject`. Eventos existentes (`render:*`, `checkpoints:changed`,
`terminal:*`, `chat:*`) ya llevan `workspaceId`; agregar
`projects:changed` (lista/orden/activo) y que `checkpoints:changed` llegue de
los proyectos de fondo también.

## Pruebas

- 8.A: abrir 4, rechazar el 5.º, cerrar detiene watcher y terminales, watcher
  de fondo dispara auto-render y vincula sesión, persistencia y reapertura,
  `--add-dir` en el argv de cada CLI (y orden correcto con `--mcp-config`
  variádico), límite de 2 agentes por proyecto.
- 8.B: banco con 3 proyectos (uno con render en curso, otro con agente vivo y
  error de render), cambiar de pestaña conserva el estado (CDP), cerrar con
  cambios sin guardar pide confirmación, `uishot-fit.sh` OK (la barra de
  pestañas no desborda la cabecera en 1024 px: pestañas que se acortan).
- Nadie ejecuta Bambu Studio ni abre sesiones de agentes.
