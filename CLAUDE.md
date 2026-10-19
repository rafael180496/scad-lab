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
   `cd frontend && pnpm build && pnpm test`, `./scripts/i18n-check.sh` (sin línea base: todo
   hallazgo se corrige).
5. **CodeGraph**: si existe `.codegraph/`, consultarlo antes de grep y correr
   `codegraph sync` tras cada bloque de ediciones. Nunca `init`/`index` sin pedido.
6. **Preparar la versión es un trigger explícito del usuario** («preparar la
   versión», «empaquetar», «versión oficial»): entonces se sigue
   [.claude/specs/releases.md](.claude/specs/releases.md) completo —bump,
   `./scripts/package-all.sh` (`.dmg` + `.exe`), copia a `releases/<so>/`,
   SHA-256 en cada README, CHANGELOG `[Unreleased]` → versión, `index.html` y
   `es/index.html` al día—, igual que mini-tools. **Nunca antes de que lo pida.**
   Publicar = el usuario commitea y empuja el tag `vX.Y.Z`; el workflow
   `.github/workflows/release.yml` crea el Release con esos artefactos (no
   compila).
7. **Documentación segmentada**: este archivo es solo un índice. Cada tema vive
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
| [claude/13-mcp.md](claude/13-mcp.md) | Servidor MCP (`ScadLab --mcp --workspace`): herramientas `render`/`preview`/`measure`…, seguridad de rutas, pruebas, cómo agregar una herramienta |
| [claude/14-medicion.md](claude/14-medicion.md) | Medición e impresión (S7): partes por color y su caché (`/parts/`), estimación de peso, filamento y costo (fórmula), cálculo de placa, `GetModelParts`/`EstimatePrint` |
| [claude/15-checkpoints.md](claude/15-checkpoints.md) | Checkpoints (S3): papelera (nunca se borra), restaurar, descendientes, padre activo que avanza solo (`.scadlab/active-parent`), seguridad de rutas |
| [claude/16-persistencia.md](claude/16-persistencia.md) | Qué recuerda la app al cerrar (checkpoint elegido, visor, paneles, barras; el modo enfoque no), bindings livianos, dónde se crea un workspace (`CreateWorkspaceIn`), prueba `uishot-persist.sh` |
| [claude/17-motor-wasm.md](claude/17-motor-wasm.md) | Motor integrado (S11): OpenSCAD oficial en WebAssembly ejecutado en Go con wazero cuando no hay OpenSCAD nativo; host emscripten, montajes, fuentes, licencia, medidas |
| [claude/18-descarga-openscad.md](claude/18-descarga-openscad.md) | «Descargar OpenSCAD» (S11): snapshot fijado con SHA-256 en `<datos>/tools/openscad/`, cuarentena de macOS, orden de detección, `SCADLAB_NO_SYSTEM_OPENSCAD` para pruebas |
| [claude/19-hermes.md](claude/19-hermes.md) | Hermes (S12): chat agéntico con herramientas de ScadLab en proceso, bucle y tope de pasos, adjuntos (imágenes y modelos STL/3MF/OBJ), conversaciones, tokens y costo, proveedores, arreglo de la terminal |
| [claude/20-skills-propias.md](claude/20-skills-propias.md) | Skills propias (Configuración → Skills): editor de archivos con Monaco y validación del frontmatter, papelera, renombrar, carpeta elegible (mover o solo apuntar), botón de carpeta |
| [claude/21-llavero.md](claude/21-llavero.md) | Llavero propio (S12, 12.10): contraseña maestra (argon2id) que se crea al guardar la primera clave de API, «Recordar en este equipo» en el llavero del SO, una sola lectura perezosa por sesión, desbloqueo con reintento, cambiar/olvidar/restablecer, migración de T1, qué se averiguó del Keychain de macOS |
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
