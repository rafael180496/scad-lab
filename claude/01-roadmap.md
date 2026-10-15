# Roadmap por fases

Cada fase se planifica con el agente `architect` (plan detallado en
`.claude/specs/<fase>-<slug>.md`), se reparte entre `fullstack-dev` (FS) y
`openscad-dev` (OS) y se cierra solo con la verificación de
[CLAUDE.md](../CLAUDE.md) en verde. Estado: ✅ hecho · 🟡 en curso · ⬜ pendiente.

Orden pensado para que cada fase sea usable sola: primero renderizar, después
ver, después editar, después la IA que itera sobre todo eso.

## S0 — Fundación ✅

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 0.1 | Scaffold Wails v2 + React 19 + TS + Vite 7 + Tailwind v4, pnpm | FS | ✅ |
| 0.2 | `appdata` multiplataforma (`SCADLAB_DATA_DIR` para tests) | FS | ✅ |
| 0.3 | SQLite `scadlab.db` con migraciones: settings, workspaces, checkpoints, skills, proveedores IA, materiales | FS | ✅ |
| 0.4 | i18n inglés/español (frontend tipado + `i18n.Msg` en Go, test de completitud) | FS | ✅ |
| 0.5 | Detección de OpenSCAD y CLIs (claude, opencode, codex, antigravity/`agy`) fuera del PATH | FS | ✅ |
| 0.6 | Workspaces: crear/importar/quitar, `CLAUDE.md`+`AGENTS.md`+`printer.json`, lectura de `@checkpoint` | FS | ✅ |
| 0.7 | Skills de modelado embebidas + plantilla `_skill-template` + sync a `.claude/.agents/.opencode/.agent` | OS | ✅ |
| 0.8 | Catálogo de impresoras Bambu (`printers.json`, compartido app/IA) | OS | ✅ |
| 0.9 | Shell de UI: cabecera, workspaces, árbol de checkpoints, paneles, Configuración | FS | ✅ |
| 0.10 | Icono, scripts de build/versión/empaquetado, `release.yml`, `index.html` de ayuda | FS | ✅ |
| 0.11 | Skills del repo (`npx skills add …`) y agentes `architect`/`fullstack-dev`/`openscad-dev` | — | ✅ |
| 0.12 | Probar el `.exe` en una Windows real | usuario | ⬜ |
| 0.13 | OpenSCAD instalado (snapshot) y las 10 plantillas renderizadas sin errores | architect | ✅ |

## S1 — Motor de render OpenSCAD ✅

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 1.1 | `backend/render`: correr OpenSCAD (`os/exec` con contexto, `--backend=manifold` según config, `hideWindow`) a STL binario; cola por workspace, cancelación | OS |✅ |
| 1.2 | Caché de mallas por hash del `.scad` (+ includes) en `<datos>/cache/` | OS |✅ |
| 1.3 | Parser de `ERROR:`/`WARNING:` → `{line, col, severity, message}`; `RENDER_ERRORS.md` en el workspace si falla (se borra al pasar) | OS |✅ |
| 1.4 | Watcher del workspace (fsnotify): `.scad` nuevo → rescan + render del último; eventos Wails `render:*`, `checkpoints:changed` | FS | ✅ |
| 1.5 | Estado de render en la UI (spinner por checkpoint, error en la barra) | FS | ✅ |
| 1.6 | Validación con OpenSCAD real (2026.10.11): mensajes, `assert`/2D, test `TestRenderWithRealOpenSCAD`; `RENDER_ERRORS.md` solo sigue al checkpoint más nuevo | architect | ✅ |
| 1.7 | Banco de capturas (`frontend/src/uishot.tsx`, `scripts/uishot.sh`, `scripts/help-shots.sh`) y capturas reales en `index.html` | architect | ✅ |

## S2 — Visor 3D ⬜

| # | Tarea | Dueño |
|---|---|---|
| 2.1 | React Three Fiber + carga STL desde binding (bytes, no archivo), PBR con environment map | FS |
| 2.2 | Órbita/pan/zoom con ratón, táctil y teclado; 7 vistas (Front/Back/Left/Right/Top/Bottom/Iso) | FS |
| 2.3 | Wireframe, aristas, ortográfica/perspectiva, cama de impresión a escala de la impresora | FS |
| 2.4 | 6 muestras de color personalizables (guardadas en settings) | FS |
| 2.5 | Captura PNG; vista dividida con segunda cámara independiente | FS |
| 2.6 | Caché en memoria (LRU) de mallas por checkpoint: clic = carga instantánea | FS |

## S3 — Checkpoints avanzados ⬜

| # | Tarea | Dueño |
|---|---|---|
| 3.1 | Menú contextual: renombrar (rótulo), plegar, ver código, retomar sesión | FS |
| 3.2 | «Borrar» = mover a `.scadlab/trash/` (nunca borrar un checkpoint del disco) | FS |
| 3.3 | Ramificar: fijar el «padre activo» (`.scadlab/active-parent`) que leen las reglas del agente | FS+OS |
| 3.4 | Vínculo checkpoint ↔ sesión del agente que lo creó | FS |

## S4 — Editor de código ⬜

| # | Tarea | Dueño |
|---|---|---|
| 4.1 | Monaco (empaquetado local, sin CDN) con gramática Monarch de OpenSCAD y tema propio | FS |
| 4.2 | Buscar/reemplazar, solo lectura por defecto, modo edición | FS |
| 4.3 | «Guardar» crea un checkpoint hijo (`NextFileName` + encabezado), nunca sobrescribe | FS+OS |
| 4.4 | Marcadores de error de S1 (subrayado rojo en la línea) | FS |

## S5 — Agentes de IA en la app ⬜

| # | Tarea | Dueño |
|---|---|---|
| 5.1 | PTY en Go (ConPTY en Windows) + xterm.js; lanzar el CLI elegido en el workspace | FS |
| 5.2 | Hasta 2 terminales; elegir agente por terminal | FS |
| 5.3 | Sesiones: listar/retomar/nueva (Claude `--resume`, equivalentes de cada CLI) | FS |
| 5.4 | Auto-iteración: render fallido → `RENDER_ERRORS.md` + aviso escrito en la terminal del agente | FS+OS |
| 5.5 | Chat con Ollama / API OpenAI-compatible / Anthropic para ideas y prompts (sin terminal) | FS |

## S6 — Servidor MCP de OpenSCAD ⬜

| # | Tarea | Dueño |
|---|---|---|
| 6.1 | `ScadLab --mcp` (stdio): `render`, `validate`, `export`, `list_checkpoints`, `create_checkpoint`, `measure` | OS |
| 6.2 | Autoconfiguración por workspace: `.mcp.json` (Claude), `opencode.json`, `.codex/config.toml`, config de Antigravity | FS |
| 6.3 | Reglas del workspace actualizadas para usar el MCP | OS |

## S7 — Medición e impresión ⬜

| # | Tarea | Dueño |
|---|---|---|
| 7.1 | Render por color → piezas seleccionables en el visor | OS |
| 7.2 | Clic en pieza: dimensiones, volumen, peso (densidad), costo (relleno, material, costo/kg) | OS+FS |
| 7.3 | Aviso si el modelo no cabe en la placa de la impresora del workspace | OS |

## S8 — Varios proyectos ⬜

| # | Tarea | Dueño |
|---|---|---|
| 8.1 | Hasta 4 workspaces abiertos (Wails v2 es de una ventana: pestañas de proyecto, o instancias con candado — decide `architect`) | FS |
| 8.2 | El agente ve todos los workspaces abiertos (`--add-dir` / equivalentes) | FS |

## S9 — Exportación ⬜

| # | Tarea | Dueño |
|---|---|---|
| 9.1 | Botones STL / 3MF / PNG de la cabecera | FS |
| 9.2 | 3MF multicolor generado en Go: un objeto por color con `basematerials`, abre en Bambu Studio con partes asignables al AMS | OS |
| 9.3 | Panel de costo de impresión | FS |

## S10 — Pulido y versión ⬜

| # | Tarea | Dueño |
|---|---|---|
| 10.1 | Chequeo i18n estricto (sin texto suelto en JSX ni en Go) | FS |
| 10.2 | Ayuda en inglés (`index.html`) + español (`es/index.html`), como mini-tools; capturas | FS |
| 10.3 | Prueba en Windows real; primera versión con `releases.md` | FS |
