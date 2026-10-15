# ScadLab

App de escritorio para **diseñar modelos 3D en OpenSCAD con ayuda de agentes de
IA** (Claude Code, opencode, Codex, Antigravity, Ollama) y dejarlos listos para
imprimir en Bambu Lab (A1, A1 mini, P1S, X1C, H2D…). Go + Wails v2 + React 19 +
TypeScript + Tailwind v4 + SQLite. macOS y Windows. Interfaz en inglés y español.

> Idioma de trabajo: **español** para specs, planes, changelog y docs.
> Identificadores de código en inglés.

## Reglas de ejecución (prioritarias)

1. **Solo el usuario hace commits.** Nunca `git add`, `commit`, `push`, tags ni
   PRs — ni Claude ni ningún sub-agente, aunque se haya pedido en otra
   conversación. Los cambios quedan en el árbol de trabajo. Ver
   [.claude/rules/conventions.md](.claude/rules/conventions.md).
2. **Por fases.** Todo trabajo pertenece a una fase de
   [claude/01-roadmap.md](claude/01-roadmap.md) y se descompone en una lista
   ordenada de tareas antes de tocar código; cada tarea se marca al terminarla.
3. **Agentes del repo** ([.claude/agents/](.claude/agents/)): `architect`
   (**Opus, esfuerzo medio**) planifica y reparte cada fase; los devs van en
   **Sonnet, esfuerzo alto**: `fullstack-dev` implementa Go/React y
   `openscad-dev` geometría, CLI de OpenSCAD, 3MF y skills de modelado. Al
   lanzar un dev como agente genérico, pasar `model: sonnet` (si no, hereda el
   modelo de la sesión).
4. **Verificar antes de cerrar**: `go build ./... && go vet ./... && go test ./...`,
   `cd frontend && pnpm build`, `node frontend/scripts/i18n-check.mjs`.
5. **CodeGraph**: si existe `.codegraph/`, consultarlo antes de grep y correr
   `codegraph sync` tras cada bloque de ediciones. Nunca `init`/`index` sin pedido.
6. **Documentación segmentada**: este archivo es solo un índice. Cada tema vive
   en `claude/NN-topic.md` (máx. ~200 líneas); un tema nuevo es un segmento nuevo.

## Índice

| Segmento | Contenido |
|---|---|
| [claude/00-overview.md](claude/00-overview.md) | Qué es, stack, estructura de carpetas, comandos |
| [claude/01-roadmap.md](claude/01-roadmap.md) | **Fases S0–S10**, tareas, dueño y estado |
| [claude/02-architecture.md](claude/02-architecture.md) | Capas, flujo de datos, workspace y checkpoints |
| [claude/03-backend.md](claude/03-backend.md) | Reglas Go: sin CGO, multiplataforma, errores, procesos |
| [claude/04-frontend.md](claude/04-frontend.md) | Reglas React/TS/Tailwind, design system, componentes |
| [claude/05-i18n.md](claude/05-i18n.md) | Inglés y español en frontend y backend, chequeo |
| [claude/06-go-react-contract.md](claude/06-go-react-contract.md) | Bindings `App` vigentes |
| [claude/07-database.md](claude/07-database.md) | `scadlab.db`: tablas, migraciones aditivas |
| [claude/08-openscad.md](claude/08-openscad.md) | OpenSCAD, skills de modelado, impresoras Bambu, 3MF |
| [claude/09-agents-ai.md](claude/09-agents-ai.md) | CLIs agénticos, Ollama/OpenAI/Anthropic, MCP, workspaces |
| [claude/10-editor.md](claude/10-editor.md) | Editor OpenSCAD: lexer, linter (SL001–SL009), formateador, catálogo, guardar como checkpoint |
| [claude/11-exportacion.md](claude/11-exportacion.md) | Exportación: STL, 3MF multicolor para Bambu (una parte por color con extrusor), STL por color; prueba manual en Bambu Studio |
| [claude/12-paneles.md](claude/12-paneles.md) | Paneles acoplables (dockview): pestañas estilo VS Code, ocultar, dividir, flotar, diseños, cómo agregar un panel |
| [.claude/rules/technical.md](.claude/rules/technical.md) | Restricciones técnicas duras |
| [.claude/rules/conventions.md](.claude/rules/conventions.md) | Commits, changelog, `index.html`, estilo |
| [.claude/specs/releases.md](.claude/specs/releases.md) | Proceso de versión (trigger «preparar la versión») |
| [index.html](index.html) | Ayuda pública (GitHub Pages) — se actualiza con cada funcionalidad, **con su captura** (`./scripts/help-shots.sh`) |

## Skills

- Del repo (para desarrollar ScadLab, en `.claude/skills/` y `.agents/skills/`,
  instaladas con `npx skills add`; ver `skills-lock.json`):
  `vercel-react-best-practices`, `react`, `tailwind-design-system`,
  `golang-code-style`, `golang-design-patterns`, `golang-performance`,
  `golang-concurrency`, `frontend-design`, `ui-ux-pro-max`.
- De modelado (para la IA que diseña dentro de cada workspace, embebidas en el
  binario): [assets/skills/](assets/skills/README.md).
