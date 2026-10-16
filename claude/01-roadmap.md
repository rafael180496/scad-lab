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

## S2 — Visor 3D ✅ (falta 2.9, prueba visual del usuario)

Plan: [.claude/specs/s2-visor-3d.md](../.claude/specs/s2-visor-3d.md).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 2.1 | React Three Fiber + carga STL por HTTP (`/mesh/<clave>.stl`, no binding), PBR con `RoomEnvironment` (sin red) | FS | ✅ |
| 2.2 | Órbita/pan/zoom con ratón, táctil y teclado; 7 vistas (Front/Back/Left/Right/Top/Bottom/Iso) | FS | ✅ |
| 2.3 | Wireframe, aristas, ortográfica/perspectiva, cama de impresión a escala de la impresora | FS | ✅ |
| 2.4 | 6 muestras de color personalizables (guardadas en settings) | FS | ✅ |
| 2.5 | Captura PNG; vista dividida con segunda cámara independiente | FS | ✅ |
| 2.6 | Caché en memoria (LRU) de mallas por checkpoint: clic = carga instantánea | FS | ✅ |
| 2.7 | Panel de medidas con `GetMeshInfo`; capturas del visor en la ayuda | FS | ✅ |
| 2.8 | `wails build` de macOS y arranque real (base + caché creadas, sin errores) | architect | ✅ |
| 2.9 | Mirar el visor en la ventana real (WebGL de WKWebView/WebView2) y el diálogo de captura PNG — no se puede automatizar sin permiso de grabación de pantalla | usuario | ⬜ |
| 2.A | Mallas por HTTP (`backend/meshserve`, middleware) + análisis STL (`backend/mesh`, `GetMeshInfo`) con tests contra OpenSCAD real | OS | ✅ |

## S3 — Checkpoints avanzados ✅ (falta 3.8, prueba del usuario en la ventana real)

Plan: [.claude/specs/s3-checkpoints-avanzados.md](../.claude/specs/s3-checkpoints-avanzados.md). Bloque 3.A (operaciones en Go: papelera, padre activo, MCP y reglas) → [15-checkpoints.md](15-checkpoints.md); 3.B (bindings, vínculo con la sesión y UI) ✅.

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 3.1 | Menú contextual del nodo (clic derecho y «…»): ver código, comparar, ramificar, renombrar (rótulo en línea), retomar sesión, plegar, copiar nombre, mostrar en el Finder, papelera; F2 y Supr con el árbol enfocado | FS | ✅ |
| 3.2 | «Borrar» = mover a `.scadlab/trash/` (nunca borrar un checkpoint del disco): este solo o con su rama, confirmación con el número de descendientes; diálogo de la papelera con Restaurar (rótulo y vínculo se conservan: tabla `checkpoint_trash`) | OS+FS | ✅ |
| 3.3 | Ramificar: «padre activo» (`.scadlab/active-parent`) que leen las reglas del agente y el MCP; avanza solo (Create, MCP y watcher); indicador en el árbol | OS+FS | ✅ |
| 3.4 | Vínculo checkpoint ↔ sesión del agente (watcher + `agentsessions`) o chat (`SaveChatCheckpoint`); migración 3 aditiva (`agent_id`, `chat_id`); «Retomar sesión» | FS | ✅ |
| 3.5 | Comparar con el padre: `DiffEditor` de Monaco (local) dentro del panel Código | FS | ✅ |
| 3.6 | Banco y capturas: `tree-menu`, `tree-active-branch`, `trash`, `diff-parent`; ayuda y recetas sin «En desarrollo» | FS | ✅ |
| 3.7 | Pruebas Go: papelera/restauración con rótulo, padre activo y watcher, vínculo con sesión inyectada, nombres hostiles, migración 3 sobre una base de la 2; clic derecho y teclado por CDP en el banco | FS | ✅ |
| 3.8 | Probar en la ventana real (WKWebView/WebView2): clic derecho, «Retomar sesión» con un CLI real y el DiffEditor — no se puede automatizar aquí | usuario | ⬜ |

## S4 — Editor de código ✅ (falta 4.10, prueba del usuario en la ventana real)

Plan: [.claude/specs/s4-editor-openscad.md](../.claude/specs/s4-editor-openscad.md). Tanda 4.A (núcleo Go: lexer, linter OpenSCAD + SL001–SL009, formateador, catálogo, guardar como checkpoint) ✅; 4.B (Monaco + UX) ✅.

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 4.1 | Monaco (empaquetado local, sin CDN) con gramática Monarch de OpenSCAD y tema propio | FS | ✅ |
| 4.2 | Buscar/reemplazar, solo lectura por defecto, modo edición | FS | ✅ |
| 4.3 | «Guardar» crea un checkpoint hijo (`NextFileName` + encabezado), nunca sobrescribe | FS+OS | ✅ |
| 4.4 | Marcadores de error de S1 (subrayado en la línea) y del linter, clic a la línea | FS | ✅ |
| 4.5 | Linter en vivo (debounce 400 ms), formatear, autocompletado/hover/firma del catálogo | FS | ✅ |
| 4.6 | Paneles redimensionables y persistidos, barra lateral plegable (⌘B/Ctrl+B) | FS | ✅ |
| 4.7 | Barra de estado y panel Problemas; medidas del visor plegables | FS | ✅ |
| 4.8 | Settings del editor (sangría, letra, ajuste, minimapa); capturas y ayuda | FS | ✅ |
| 4.9 | Menú nativo Edición en macOS (`appMenu` en `main.go`): sin él WKWebView no entrega ⌘C/⌘V/⌘Z al editor | architect | ✅ |
| 4.10 | Probar el editor en la ventana real (WKWebView/WebView2): workers de Monaco, copiar/pegar, ⌥⌘F | usuario | ⬜ |
| 4.11 | **Paneles acoplables (S4b)**: visor, código, agentes, chat y problemas con pestañas estilo VS Code (dockview), dividir, flotar, ocultar sin destruir, menú Ver, diseños (de fábrica y propios), modo enfoque, divisores con mínimos; reemplaza a `Workbench`/`Splitter` de S4. Plan: [.claude/specs/s4b-paneles-acoplables.md](../.claude/specs/s4b-paneles-acoplables.md). Pendiente del usuario: arrastrar pestañas en la ventana real de Wails (ver `claude/04-frontend.md`) | FS | ✅ |

## S5 — Agentes de IA en la app ✅ (falta 5.8, prueba del usuario en la ventana real)

Plan: [.claude/specs/s5-agentes-ia.md](../.claude/specs/s5-agentes-ia.md).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 5.1 | PTY en Go (`backend/pty`, go-pty, ConPTY en Windows, mata el grupo de procesos al cerrar) + xterm.js; el CLI corre en el workspace | FS | ✅ |
| 5.2 | Hasta 2 terminales de agente; elegir agente por terminal; reenganche al recargar (`ReadTerminalBuffer`) | FS | ✅ |
| 5.3 | Sesiones por workspace (`backend/agentsessions`): Claude, Codex, opencode verificados contra archivos reales; Antigravity solo nueva/continuar | FS | ✅ |
| 5.4 | Auto-iteración en Go (`onRenderFinished`, `prompts.Nudge`, una vez por fallo, tope de 5 seguidos) | FS+OS | ✅ |
| 5.5 | Chat con Ollama / OpenAI-compatible / Anthropic (`backend/llm`, `backend/chat`, migración 2) con guardar bloque como checkpoint | FS | ✅ |
| 5.6 | Prompts (`backend/prompts`): sistema del chat ES/EN, nudge, extracción de bloques | OS | ✅ |
| 5.7 | Español neutro (tú) en toda la interfaz y mensajes; regla en `conventions.md` | architect | ✅ |
| 5.8 | Probar en la ventana real: terminal (PTY con un CLI real, copiar/pegar) y chat contra un servidor real (Ollama no está instalado en esta Mac) | usuario | ⬜ |

## S6 — Servidor MCP de OpenSCAD ✅

Plan: [.claude/specs/s6-servidor-mcp.md](../.claude/specs/s6-servidor-mcp.md). Detalle y verificación por CLI: [claude/13-mcp.md](13-mcp.md).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 6.1 | `ScadLab --mcp --workspace <dir>` (stdio, sin SDK) con 12 herramientas: `list_checkpoints`, `read_checkpoint`, `create_checkpoint`, `render`, `lint`, `format`, `measure`, `preview` (PNG), `printer_info`, `export`, `list_templates`/`get_template`; lógica de checkpoints extraída a `backend/checkpoint` | OS | ✅ |
| 6.2 | Autoconfiguración **solo de proyecto** (`backend/mcpconf`): `.mcp.json` (Claude), `opencode.json`, `.gemini/settings.json` (Gemini CLI anterior); fusiona solo `scadlab`, no toca un JSON roto; sincroniza al crear/abrir/importar | FS | ✅ |
| 6.3 | Interfaz: Configuración → IA (interruptor, comando, estado por archivo, sincronizar), indicador «MCP ✓», tema `ia-mcp` | FS | ✅ |
| 6.4 | Reglas del workspace (`RULES.md.tmpl`) y skills usan las herramientas MCP, con respaldo a la CLI | OS | ✅ |
| 6.5 | Prueba real con los CLIs (`mcp list`, sin sesiones): Codex ✅ habilitado; opencode ✅ conectado (en repo git); Claude ⏸ pendiente de aprobación → se agregó `--mcp-config` al lanzarlo desde ScadLab | architect | ✅ |
| 6.6 | Antigravity (`agy`): solo config global → botón explícito «Agregar a Antigravity» con confirmación (`agy mcp add -- scadlab <bin> --mcp`), probado con HOME temporal | FS | ✅ |
| 6.7 | Probar con una sesión real de cada agente que use `render`/`preview` (gasta cuota: lo hace el usuario) | usuario | ⬜ |

## S7 — Medición e impresión ✅

Plan: [.claude/specs/s7-medicion-impresion.md](../.claude/specs/s7-medicion-impresion.md). Bloque 7.A (Go) ✅ → [14-medicion.md](14-medicion.md); 7.B (interfaz) ✅.

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 7.1 | Render por color → partes seleccionables en el visor (colores del modelo / muestra, clic, selección con el resto atenuado, Esc, vista dividida) | OS+FS | ✅ |
| 7.2 | Tarjeta de la parte: tamaño, volumen, peso, filamento y costo estimados, material por parte (relleno, perímetros, ancho de línea, diámetro y costo/kg) | OS+FS | ✅ |
| 7.3 | Aviso si el modelo no cabe en la placa: contorno de la cama en rojo, texto con los mm que sobran por eje y aviso en la barra de estado | OS+FS | ✅ |
| 7.4 | Panel acoplable «Impresión» (`print`, ⌘⇧7) y settings nuevos (perímetros, ancho de línea, diámetro, moneda) | FS | ✅ |

## S8 — Varios proyectos 🟡 (8.B interfaz ✅; falta la prueba manual en la ventana real de Wails)

Plan: [.claude/specs/s8-pestanas-proyecto.md](../.claude/specs/s8-pestanas-proyecto.md). Decisión: **pestañas de proyecto en una sola ventana** (Wails v2 es de una ventana). Bloque 8.A (backend, `app_projects.go`) → [06-go-react-contract.md](06-go-react-contract.md); 8.B (pestañas y estado por proyecto) → [12-paneles.md](12-paneles.md#pestañas-de-proyecto-s8).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 8.1 | Hasta 4 workspaces abiertos como pestañas de proyecto: barra en la cabecera, indicadores de fondo, reordenar, cerrar con confirmación, atajos ⌃Tab / ⌘1…4 / ⌘W, reapertura al arrancar | FS | ✅ UI |
| 8.2 | El agente ve todos los workspaces abiertos (`--add-dir` en Claude, Codex y Antigravity, en orden con `--mcp-config`; opencode no tiene) y límite de 2 agentes por proyecto | FS | ✅ |
| 8.5 | Tope global de 2 OpenSCAD simultáneos (`render.maxConcurrentRenders`) para que 4 proyectos con auto-render no saturen la CPU | architect | ✅ |
| 8.3 | Estado por proyecto en la UI (checkpoint, código sin guardar, parte, diff, chat) y terminales de fondo vivas | FS | ✅ |
| 8.4 | Prueba en la ventana real de Wails: Ctrl+Tab en WKWebView/WebView2, arrastre de pestañas | usuario | ⬜ |

## S9 — Exportación ✅ (falta 9.6, prueba manual del usuario en Bambu Studio)

Plan: [.claude/specs/s9-exportacion.md](../.claude/specs/s9-exportacion.md). Tanda 9.A (núcleo Go) ✅; 9.B (interfaz) ✅. Detalle en [11-exportacion.md](11-exportacion.md).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 9.1 | Botones STL / 3MF / PNG de la cabecera (activos con render OK, `title` con el motivo si no), diálogo de 3MF con la vista previa de partes y extrusores, avisos, atajos ⌘/Ctrl+E y ⌘/Ctrl+⇧+E | FS | ✅ |
| 9.2 | 3MF multicolor generado en Go: un objeto con una parte por color y su extrusor, STL por color (.zip) | OS | ✅ |
| 9.3 | «Abrir en Bambu Studio» (la app, nunca su CLI): `DetectSlicers`, `OpenInSlicer`, `RevealPath`, setting `bambuStudioPath`, aviso con «Mostrar en el Finder» | FS | ✅ |
| 9.4 | Capturas `export-3mf` y `export-3mf-warning`; ayuda de exportar y multicolor sin «En desarrollo» | FS | ✅ |
| 9.5 | Panel de costo de impresión (hecho en S7: panel «Impresión», `print`) | FS | ✅ |
| 9.6 | Abrir un 3MF exportado en Bambu Studio y comprobar una parte por color con su extrusor, asignar AMS y laminar (no se automatiza: regla 9 de `technical.md`) | usuario | ⬜ |

## S10 — Pulido ✅ · versión ⬜ (cuando el usuario la pida)

Plan: [.claude/specs/s10-pulido.md](../.claude/specs/s10-pulido.md). **No incluye
empaquetar**: eso se hace solo cuando el usuario dice «preparar la versión»
(regla 6 de `CLAUDE.md`).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 10.1 | i18n estricto (Go + frontend, sin línea base, `./scripts/i18n-check.sh`), 19 claves sin uso borradas, voseo → tú, código muerto | FS | ✅ |
| 10.2 | Ayuda bilingüe como mini-tools: `index.html` (inglés) + `es/index.html` (español), README al día | FS | ✅ |
| 10.3 | Workflow del tag (`release.yml`): validado (YAML, notas con `changelog-section.sh` sobre una sección simulada) | architect | ✅ |
| 10.4 | Probar el `.exe` en una Windows real | usuario | ⬜ |
| 10.5 | Primera versión (v0.1.0) con `releases.md` — **solo cuando el usuario lo pida** | — | ⬜ |

## S11 — Motor integrado (OpenSCAD WASM) y descarga de OpenSCAD ✅ (falta 11.6, prueba del usuario en Windows)

Plan: [.claude/specs/s11-motor-wasm-descarga.md](../.claude/specs/s11-motor-wasm-descarga.md). Motor → [17-motor-wasm.md](17-motor-wasm.md); descarga → [18-descarga-openscad.md](18-descarga-openscad.md).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 11.1 | `backend/scadwasm`: OpenSCAD 2025.12.30 oficial en WebAssembly ejecutado en Go (wazero, sin CGO), host emscripten con montajes, fuentes Liberation, `THIRD_PARTY_NOTICES.md` | OS | ✅ |
| 11.2 | Respaldo WASM en render, exportación STL/3MF, partes/estimación, linter y MCP (preview rasterizado en Go) | OS | ✅ |
| 11.3 | «Descargar OpenSCAD» a `<datos>/tools/openscad/` con SHA-256 fijado, progreso y cancelación; detección gestionado → PATH | FS | ✅ |
| 11.4 | UI: aviso del motor integrado en el panel de render y la barra de estado, Configuración → OpenSCAD, ayuda, capturas, CHANGELOG | FS | ✅ |
| 11.5 | Integración, medidas y prueba de humo con OpenSCAD ausente (`SCADLAB_NO_SYSTEM_OPENSCAD=1`): visor con malla WASM, error con línea, STL cerrado, 3MF con una parte por color y su extrusor (igual que el nativo), estimación, MCP render/export/preview | architect | ✅ |
| 11.6 | Probar en una Windows real: descarga del zip, `openscad.exe` detectado, render WASM sin OpenSCAD | usuario | ⬜ |
