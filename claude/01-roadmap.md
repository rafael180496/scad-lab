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
| 10.6 | Skills propias (Configuración → Skills): botón de carpeta que sí abre, editor de archivos con validación del frontmatter, eliminar (papelera), renombrar y carpeta elegible — [20-skills-propias.md](20-skills-propias.md) | FS | ✅ |

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

## S12 — Hermes: chat agéntico con proveedores de IA ✅ (falta 12.11, prueba del usuario en la ventana real)

Plan: [.claude/specs/s12-hermes-proveedores.md](../.claude/specs/s12-hermes-proveedores.md). Capa de modelos portada de mini-tools (proveedores, servidores, claves, modelos, streaming, tool calling, conversaciones, adjuntos, tokens y costo) y mejorada para OpenSCAD (herramientas de render/lint/measure/preview en el bucle, vista previa de vuelta al modelo, STL/3MF de referencia, skills). Detalle: [claude/19-hermes.md](19-hermes.md).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 12.1 | Proveedores y servidores (Ollama ×N, OpenAI, Anthropic, Gemini, OpenRouter, compatibles OpenAI), claves cifradas (AES-256-GCM, con la llave de la contraseña maestra: ver 12.10), modelos reales con búsqueda, Configuración → Proveedores de IA con «Probar conexión»; selector de servidor y modelo en el chat | FS | ✅ |
| 12.2 | Caja de herramientas OpenSCAD en proceso (`mcp.Toolbox`), `list_skills`/`read_skill`, prompt de Hermes, análisis de referencias STL/3MF/OBJ (`backend/refmodel`) con vista previa rasterizada y copia a `refs/` | OS | ✅ |
| 12.3 | Workspace nuevo (y carpeta importada vacía) con `001-<slug>.scad` por defecto, válido, con cabecera, `$fn`, Customizer y `color()`; se selecciona y renderiza al abrir | OS | ✅ |
| 12.4 | Hermes: bucle agéntico con tool calling nativo, tarjetas de pasos plegables con imagen, detener, tope de pasos, modo de respaldo sin herramientas | FS | ✅ |
| 12.5 | Adjuntos (imágenes por pegar/arrastrar/elegir; STL/3MF/OBJ de referencia) con chips y miniatura, aviso sin visión | FS | ✅ |
| 12.6 | Conversaciones: renombrar, borrar, retomar, tokens y costo | FS | ✅ |
| 12.7 | Terminal: la respuesta DA (`^[[?1;2c`) ya no se cuela en el agente al reenganchar | FS | ✅ |
| 12.8 | Capturas (chat con proveedor y modelo, adjuntos, pasos, Configuración → Proveedores), ayuda es/en, CHANGELOG, `claude/19-hermes.md` | FS | ✅ |
| 12.9 | Integración, verificación y prueba de humo con servidor simulado (`wails dev` + CDP, `SCADLAB_FAKE_KEYRING=1`): starter renderizado, llavero, adjuntos, ciclo del agente, terminal sin `^[[?1;2c`; arreglado el chat en blanco al reabrir pasos guardados (`images: null`) | architect | ✅ |
| 12.10 | Llavero propio: contraseña maestra (argon2id) pedida solo al guardar la primera clave, «Recordar en este equipo» (marcado por defecto) en el llavero del SO, sin recordar = una vez por sesión; cambiar, olvidar este equipo, restablecer. Detalle: [21-llavero.md](21-llavero.md) | FS | ✅ |
| 12.11 | Probar en la ventana real con un proveedor real (clave propia) y el Keychain real de macOS: ¿pregunta «ScadLab quiere acceder…» tras cada build? Antigravity en la terminal sin `^[[?1;2c` | usuario | ⬜ |
| 12.12 | **S12.b** ([spec](../.claude/specs/s12b-cli-chat-ux.md)): CLIs instalados (Claude Code, Codex, Antigravity, OpenCode) como proveedores del chat con el MCP de ScadLab, modos solo consulta / agente, importar servidores de mini-tools — backend | FS | ✅ |
| 12.13 | S12.b: selector de proveedor y modelo propio (CLIs + servidores, estado, latencia, «Por defecto»), pase de UX del chat (barra compacta, estado vacío, composer, pasos, markdown, búsqueda), 320–1200 px. Detalle: [22-chat-ux.md](22-chat-ux.md) | FS | ✅ |
| 12.14 | S12.b: integración y prueba de humo (`wails dev`, PATH solo con CLIs falsos y `SCADLAB_NO_EXTRA_DIRS=1`): 4 CLIs detectados sin configurar, turno de Claude falso conectado al MCP real (`render` ok), sesión continuada con `--resume`, flags por modo, importación de una base falsa de mini-tools sin claves ni escritura | architect | ✅ |
| 12.15 | **S12.c** ([spec](../.claude/specs/s12c-chat-crea-checkpoints.md)): el chat siempre termina en un `.scad` renderizado — modo Agente por defecto, checkpoint raíz sin padre, starter al abrir un workspace vacío, red de seguridad que guarda el bloque, Antigravity con MCP global automático, respaldo de Hermes con bucle de errores | FS | ✅ |
| 12.16 | S12.c: prompts de Hermes y del respaldo, reglas del workspace y `serverInstructions` con el flujo obligatorio | OS | ✅ |
| 12.17 | S12.c: integración y prueba de humo (`wails dev`, PATH con CLIs falsos, servidores simulados): en un workspace vacío y con la conversación nueva en Agente, cada proveedor deja un `.scad` renderizado y cerrado — Claude (MCP real: crear → render → preview → measure → export 3MF, flujo «usuario sin conocimientos»), Codex y Antigravity solo texto (red de seguridad), Antigravity con `agy mcp add` automático, Hermes con tools y sin tools (respaldo); guardar sin padre crea la raíz; starter al abrir un workspace sin `.scad` y no con papelera llena | architect | ✅ |
| 12.18 | S12.c T11: auditoría del MCP — 31 herramientas con lectura y escritura (parámetros, plantillas, librerías, `edit_mesh`, estimación, orientación, árbol y papelera) | OS | ✅ |
| 12.19 | S12.c T12: modo Agente por defecto, por conversación (migración 9, `chats.mode`) | FS | ✅ |
| 12.20 | S12.c T13: las herramientas nuevas en la app y el chat — etiquetas e íconos, `mcp.CreatesCheckpoint`/`ChangesTree` (una sola definición), refresco del árbol tras set_active_parent/rename/trash/restore, `PrepareMeshEdit` sobre `meshedit.ResolveBase` | FS | ✅ |
| 12.21 | S12.c T14: Antigravity usa las herramientas de ScadLab en `agy --print` — reglas `mcp(scadlab/<herramienta>)` en `permissions.allow` del `settings.json` global de Antigravity (los hooks no sirven: medido), sincronizadas al registrar el MCP y en cada turno, Quitar/Volver a agregar en Configuración, «Solo consulta» hecho cumplir por el servidor `--mcp` con la marca `.scadlab/turn-mode`, mensaje accionable y «Reparar permisos de Antigravity» si igual se bloquea, `call_mcp_tool` desenvuelto. Detalle: [26-antigravity-permisos.md](26-antigravity-permisos.md). Falta probarlo con el `agy` real (lo hace el usuario) | FS | ✅ código · ⬜ sesión real |

## S13 — Editar un STL (editor 3D y con IA) ✅ (falta 13.4, prueba del usuario en la ventana real)

Plan: [.claude/specs/s13-edicion-stl.md](../.claude/specs/s13-edicion-stl.md). No destructivo: cada edición es un checkpoint nuevo que hace `import("refs/<archivo>.stl")` y le aplica las operaciones; el STL queda intacto en `refs/`.

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 13.1 | Núcleo: importar STL/3MF/OBJ (conversión a STL, aviso de malla abierta, checkpoint base centrado y apoyado), congelar la malla de un checkpoint en `refs/`, generador de código de la escena (`backend/meshedit`), `import_mesh` por MCP, skill `openscad-stl-editing`. Detalle: [23-edicion-stl.md](23-edicion-stl.md) | OS | ✅ |
| 13.2 | Interfaz: importar (barra lateral y arrastrar), modo «Editar» del visor (gizmo mover/rotar/escalar, espejar, centrar, apoyar, apoyar sobre una cara, cortar con plano, primitivas unión/agujero, color por pieza, deshacer/rehacer, aplicar/descartar), «Modificar con IA». Detalle: [24-editor-3d.md](24-editor-3d.md) | FS | ✅ |
| 13.3 | Integración, verificación y prueba de humo (`wails dev`): importar un STL (centrado y apoyado), editar con agujero y corte en dos colores, render cerrado, 3MF con 2 partes, STL original intacto, «Modificar con IA» con servidor simulado; adjuntar un STL que ya está en `refs/` lo reutiliza (sin copia `-2`) | architect | ✅ |
| 13.4 | Probar en la ventana real: arrastrar un archivo a la ventana (`OnFileDrop`), «Importar…» con el diálogo nativo y el gizmo con el ratón en WKWebView/WebView2 | usuario | ⬜ |

## S14 — Impresoras multimarca y configuración por workspace ✅ (falta 14.5, prueba del usuario en los slicers)

Plan: [.claude/specs/s14-impresoras-multimarca.md](../.claude/specs/s14-impresoras-multimarca.md). Detalle: [27-impresoras-workspace.md](27-impresoras-workspace.md).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 14.0 | Investigación con fichas oficiales: 47 impresoras de 7 marcas con `sources` y `verify`, apps de los slicers en macOS/Windows, qué lee cada slicer de un 3MF | investigación | ✅ |
| 14.1 | Núcleo Go: catálogo v2 por marca, `backend/slicers`, migración 10 (skills y ajustes por workspace, backfill de skills y de colores), `backend/wsconfig`, skills por workspace (retira solo copias sin editar), bindings `GetWorkspaceConfig`/`SaveWorkspaceConfig`/`CreateWorkspaceWith`, evento `workspace:config` | FS | ✅ |
| 14.2 | Catálogo verificado, skills de marca (Creality, Prusa, Elegoo, Anycubic, Snapmaker, UltiMaker) y Bambu al día, `openscad-3mf-multicolor` genérica, formato `3mf-prusa` (`Slic3r_PE_model.config`), `RULES.md.tmpl` y prompts con la impresora del workspace, `printer_info`/`list_printers`/`set_workspace_printer` | OS | ✅ |
| 14.3 | Interfaz: selector de impresoras, «Nuevo workspace» con impresora/colores/skills, «Configuración del workspace», slicers en Configuración → Impresión, «Abrir en <slicer>», barra de estado y panel Impresión con valores del workspace; capturas, ayuda, README, CHANGELOG; `claude/06` partido en 29–33 | FS | ✅ |
| 14.4 | Integración y verificación (`go test -race`, build de Windows, frontend, i18n, uishot-fit/persist, `wails build`, todas las capturas regeneradas) y humo con la Prusa MK4S (solo sus skills instaladas, prompt y reglas con la impresora, `measure` con placa 250×210×220). `extruder_nr` de Cura apagado en el 3MF normal | architect | ✅ |
| 14.5 | Abrir un 3MF/3MF para PrusaSlicer/STL por color en cada slicer instalado y probar «Abrir en <slicer>» (regla 9: no se automatiza) | usuario | ⬜ |

## S15 — Crear modelos a partir de imágenes ✅ (falta 15.4, prueba del usuario con un modelo real con visión)

Plan: [.claude/specs/s15-modelo-desde-imagen.md](../.claude/specs/s15-modelo-desde-imagen.md). Detalle: [28-modelo-desde-imagen.md](28-modelo-desde-imagen.md).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 15.1 | `backend/imagetools` (Go puro, límites de 20 MB / 40 MP / 10 s), herramientas `image_info`, `compare_with_image` (L), `trace_image_outline`, `image_to_heightmap` (E), `list_refs` con imágenes, skill `openscad-from-image` (método, casos, `standard-sizes.md`, plantillas renderizadas), prompts y reglas con las imágenes del turno | OS | ✅ |
| 15.2 | Fotos del chat a `refs/images/` (deduplicadas), rutas en el turno de Hermes y de los CLIs, «Crear desde una imagen», tarjetas de comparación y trazado, aviso sin visión con «Cambiar a un modelo con visión» / «Usar trazado de contorno»; capturas, ayuda, CHANGELOG | FS | ✅ |
| 15.3 | Integración: una foto grande se reduce al adjuntar (≤ 2048 px y ≤ 5 MB al modelo; el original a `refs/images/`; entrada hasta 20 MB), verificación completa y humo con servidor simulado (foto sintética de 3000 px → checkpoint renderizado → `compare_with_image` en vista frontal, IoU 1) | architect | ✅ |
| 15.4 | Probar con un modelo real con visión, fotos reales de una pieza y un logo, y con un CLI (Claude Code / Codex) leyendo `refs/images/` | usuario | ⬜ |

## S16 — La IA edita el archivo que nombras, con historial y deshacer ✅ (falta 16.4, prueba del usuario con el `agy` real)

Plan: [.claude/specs/s16-ia-edita-con-historial.md](../.claude/specs/s16-ia-edita-con-historial.md). Detalle: [34-historial-checkpoints.md](34-historial-checkpoints.md).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 16.1 | Historial por archivo en `backend/checkpoint` (respaldo antes de escribir, tope, restaurar, papelera y purga se lo llevan), herramienta `update_checkpoint` (MCP y Hermes, solo «Agente», lint + render), clasificación en `effects.go`/`modes.go`, regla nueva en servidor MCP, `RULES.md.tmpl`, prompts, skills, `RENDER_ERRORS.md`; regla 3 de `technical.md` | OS | ✅ |
| 16.2 | Bindings de historial, evento `checkpoint:updated` (chat y watcher), prompt CLI con ruta absoluta y archivo seleccionado, indicador «Editado por la IA», «Deshacer cambio de la IA», «Ver diferencias», historial, tope en Configuración; pruebas por proveedor con CLIs falsos y Hermes simulado; capturas, ayuda, CHANGELOG, `claude/34` | FS | ✅ |
| 16.3 | Integración y verificación completa | architect | ✅ |
| 16.4 | Probar con el `agy` real (y los demás CLIs) el pedido «modifica 002-….scad» y «hazme una variante» | usuario | ⬜ |

## S17 — Edición compacta, salidas cortas y modelos chicos ✅ (falta 17.5, prueba del usuario con el `agy` real, un Ollama real y Bambu Studio)

Plan: [.claude/specs/s17-edicion-compacta-modelos-chicos.md](../.claude/specs/s17-edicion-compacta-modelos-chicos.md). Detalle: [35-edicion-compacta.md](35-edicion-compacta.md); 3MF en [11-exportacion.md](11-exportacion.md).

| # | Tarea | Dueño | Estado |
|---|---|---|---|
| 17.1 | `edit_checkpoint` (`old/new` y por líneas, todo o nada, error con la línea más parecida), `set_parameters` en el lugar con historial (`variant` = hijo), `read_checkpoint` por rango, `outline_checkpoint`, `find_in_checkpoint`, `lib/*.scad` con historial, `extract_module` (deshace si cambia el render), salidas compactas con tope de 6 KB, perfil esencial, prompt compacto, reglas y skills | OS | ✅ |
| 17.2 | 3MF que Bambu Studio rellenaba: partes abiertas por la cara de contacto entre colores; segundo render `--enable=lazy-union` cuando una parte queda abierta, aviso en la exportación, pruebas nativo/WASM/Go | OS | ✅ |
| 17.3 | Hermes con ventana real (`num_ctx` del modelo), perfil por conversación (migración 11, `SetChatToolProfile`), contexto acotado por turno y entre turnos, «Nueva conversación con resumen», tiempo y tokens por paso y por turno, Antigravity 1.4.x (`tool_info`), shell (prompt y mensaje), turno sin texto con resumen; capturas, ayuda, CHANGELOG, `claude/35` | FS | ✅ |
| 17.4 | Integración y verificación completa; Ollama simulado de 8k: ninguna vuelta pasa de 3 476 tokens (antes 17 860–28 536) | architect | ✅ |
| 17.5 | Probar con el `agy` real (stream de `tool_info`, shell rechazada, `result.usage`), con un Ollama real de 8k y abrir el 3MF de la maceta en Bambu Studio (hueco vacío, sin soportes dentro) | usuario | ⬜ |
