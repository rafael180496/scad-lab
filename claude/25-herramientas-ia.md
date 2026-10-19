# Herramientas de la IA (S12.c, T11)

Tabla completa de lo que la IA puede hacer en ScadLab, con qué modo la ve y en qué superficie. Principio
([00-overview.md](00-overview.md#visión-y-principios-palabras-del-usuario)): **la IA hace de punta a punta todo lo que
el usuario hace con la UI**, con cualquier proveedor. El servidor y su seguridad: [13-mcp.md](13-mcp.md).

- **Una sola caja.** Hermes llama `mcp.Toolbox` en proceso; los CLIs (Claude Code, opencode, Codex, Antigravity) llaman
  `ScadLab --mcp`. Las dos salen de `env.tools()`: el catálogo es **idéntico** (desde T11 `list_skills`, `read_skill` e
  `inspect_reference` ya no son solo de la app; `ToolboxOptions.InApp` quedó sin efecto).
- **Lectura / Escritura** = qué modo la habilita. **L** («Solo consulta» y «Agente») no toca el diseño: lee, o escribe solo
  caché (`render`, `measure`, `preview`, `estimate_print` y `suggest_orientation` escriben mallas en `<datos>/cache` y
  `RENDER_ERRORS.md`). **E** (solo «Agente») crea un checkpoint, cambia el árbol, escribe en `exports/` o `lib/` o deja una
  malla congelada en `refs/`. Las listas viven en `backend/clichat/modes.go` (`readTools`/`writeTools`: de ahí salen
  `--allowedTools` de Claude, `enabled_tools` de Codex y los permisos de OpenCode); `TestEveryToolIsClassifiedForTheModes`
  (en `backend/mcp`) exige que cubran el catálogo entero, sin solaparse, y la de la app (`TestModeToolListsMatchTheMCPCatalog`) lo repite.
- **Fuera del catálogo a propósito:** «Abrir en Bambu Studio». Lo dispara solo el usuario (regla 9 de `technical.md`).

## Workspace y archivos

| Herramienta | Qué hace | Modo | Hermes | `--mcp` |
|---|---|---|---|---|
| `list_checkpoints` | árbol: archivo, padre, descripción, **rótulo** del usuario, hijos, fecha y estado de render; `tree` lo dibuja como texto; `activeParent` | L | sí | sí |
| `read_checkpoint` | el código (`file`, def. el más nuevo) | L | sí | sí |
| `create_checkpoint` | `label`, `source`, `parent` (def. padre activo o el más nuevo), `description`; `root=true` abre una raíz nueva aunque ya haya otras. Nunca pisa | E | sí | sí |
| `set_active_parent` | el «Ramificar desde aquí» (`file`) o `clear=true`; avanza solo con cada hijo | E | sí | sí |
| `rename_checkpoint` | `file` + `label` (≤ 80): el rótulo del árbol, en `scadlab.db`. Ver decisiones | E | sí | sí |
| `trash_checkpoint` | `file` (+ `branch=true` con sus descendientes) a `.scadlab/trash/`. **Nunca borra** | E | sí | sí |
| `list_trash` / `restore_checkpoint` | la papelera / traer uno de vuelta (`trashed_name`); recupera su rótulo; nunca pisa | L / E | sí | sí |
| `list_refs` | los STL/3MF/OBJ de `refs/` (nombre, ruta, formato, bytes, fecha) | L | sí | sí |

## Modelado

| Herramienta | Qué hace | Modo | Hermes | `--mcp` |
|---|---|---|---|---|
| `render` | estado, errores con línea y código, tamaño, volumen, ¿cabe?, motor (`native`/`wasm`); `isError` si no renderiza | L | sí | sí |
| `lint` / `format` | diagnósticos OpenSCAD + SL001–SL009 / código formateado, sin guardar | L | sí | sí |
| `preview` | **PNG**: 8 vistas con nombre o ángulo libre (`rotate:[rx,ry,rz]`, `ortho`), `size` 256–1024 | L | sí | sí |
| `measure` | malla, ¿cabe en la placa?, partes por color y estimación con la configuración de la app | L | sí | sí |
| `get_parameters` | parámetros del Customizer: tipo, valor, grupo, descripción, rango `[min:paso:max]`, opciones | L | sí | sí |
| `set_parameters` | `values` {nombre→valor} en un checkpoint **hijo** (`file`, `label`, `description`); el resto del archivo, byte a byte. Avisa de fuera de rango/opciones | E | sí | sí |
| `list_templates` / `get_template` | las plantillas de `openscad-templates` / su código | L | sí | sí |
| `create_from_template` | `name` + `parameters`: checkpoint con los valores puestos y `PLATE` = placa de la impresora; raíz si el workspace está vacío, si no hijo del padre por defecto (`parent`, `root`) | E | sí | sí |
| `list_libraries` | BOSL2, MCAD, NopSCADlib: versión, licencia, ¿instalada?, línea `include` exacta; más lo otro que haya en `lib/` | L | sí | sí |
| `install_library` | `name`: **descarga** la librería a `lib/<name>/` (red). Ver decisiones | E | sí | sí |
| `list_skills` / `read_skill` | las skills de `.claude/skills/` / su `SKILL.md` o un archivo suyo | L | sí | sí |

## STL, 3MF y OBJ

| Herramienta | Qué hace | Modo | Hermes | `--mcp` |
|---|---|---|---|---|
| `inspect_reference` | medidas, volumen, partes por color de un modelo de `refs/` + una imagen | L | sí | sí |
| `import_mesh` | `file` de `refs/` → checkpoint **raíz** que lo importa (3MF/OBJ → STL; `center_on_bed`) | E | sí | sí |
| `edit_mesh` | `operations`: `move rotate scale resize mirror orient center to_bed color add hole cut` → checkpoint **hijo**, con `meshedit.Generate`. Ver decisiones | E | sí | sí |

## Impresión

| Herramienta | Qué hace | Modo | Hermes | `--mcp` |
|---|---|---|---|---|
| `printer_info` | impresora, placa, boquilla, slots del AMS, reglas FDM, material por defecto, motor detectado | L | sí | sí |
| `estimate_print` | peso, metros y costo con `material`, `infill_percent`, `wall_loops` a elección | L | sí | sí |
| `list_materials` | id, nombre, densidad, costo/kg y el material por defecto | L | sí | sí |
| `suggest_orientation` | las 6 caras hacia abajo: tamaño, ¿cabe?, altura, área en voladizo (>45°), contacto; `operations` listas para `edit_mesh`; si nada cabe, escala máxima y dónde cortar | L | sí | sí |
| `export` | `stl`, `3mf` (multicolor), `stl-per-color` o `png` (la imagen del preview; `view`/`rotate`/`size`) en `exports/` | E | sí | sí |

## Decisiones

- **Rótulo, no descripción.** El archivo es inmutable y sus hijos lo nombran en `parent=`, y la descripción vive en su cabecera
  (el escaneo la relee y pisaría cualquier cambio). Lo que se renombra es el **rótulo** (`checkpoints.label`), el mismo que
  cambia `RenameCheckpoint` en la UI. Como vive en la base de la app, solo funciona en un workspace **registrado**; si no, la
  herramienta lo dice. `file` es obligatorio en `rename`, `trash` y `set_active_parent` (en las demás un `file` vacío es «el
  más nuevo»: ahí un argumento olvidado no puede caer sobre un checkpoint que nadie nombró).
- **Librerías (`install_library`).** Escritura, con red: sin shell la skill `openscad-libraries` (`git clone`) era letra muerta.
  Como la descarga de OpenSCAD (S11): catálogo **fijo** en `libraryCatalog` — la IA elige un nombre, nunca una URL—, zip de un
  **commit** (no una rama) de `codeload.github.com`, **SHA-256** comprobado antes de extraer (BOSL2 v2.0.774, MCAD
  openscad-2019.05, NopSCADlib v21.43.1; tres descargas dieron los mismos bytes), todo o nada (se extrae a `lib/.install-*` y se
  renombra), sin `..`/rutas absolutas/enlaces, tope de archivos y de tamaño, y **nunca reemplaza** una librería instalada
  (los checkpoints son inmutables). Anota en `lib/LIBRARIES.txt`. Subir una versión = cambiar commit, URL, SHA y tamaño juntos.
  Un SHA que GitHub cambie al regenerar archivos falla **cerrado** («no coincide: no se instaló»). Prueba con red real:
  `SCADLAB_REAL_DOWNLOAD=1 go test ./backend/mcp -run TestRealInstallCatalog` (las tres bajan y renderizan).
- **`edit_mesh`.** Las operaciones de **pose** actúan sobre el centro de la caja del modelo (girar o agrandar no lo manda a
  otro lado); `move`/`center`/`to_bed` lo colocan. Las primitivas y el corte se colocan en el marco **final** (después de toda
  la pose, esté donde esté en la lista): `at_rel` es una fracción de la caja (`[0.5,0.5,1]` = centro de la cara de arriba), el
  corte sale de `axis`+`at`/`at_rel` o de `point`+`normal`. Un corte por llamada. La composición de giros es la de OpenSCAD
  (`TestOpsMatchTheRender`, nativo y WASM) y el código es el de la UI (`TestEditMeshMatchesTheEditor` compara contra
  `Generate` de la escena armada a mano). Sobre un checkpoint que no es un import congela su malla en `refs/` (como
  `PrepareMeshEdit`): una sola pieza de un solo color, y lo avisa; si la edición falla, esa copia se quita. Una malla no
  cerrada se mueve pero no se perfora ni se corta. Lógica en `meshedit.ResolveBase` + `meshedit.BuildScene`.
- **`suggest_orientation`.** Orden: cabe → menos voladizo → menos altura. El voladizo y el contacto salen de la malla
  renderizada (triángulos cuya normal mira hacia abajo más de 45° de la vertical, sin contar lo apoyado). No juzga
  resistencia ni dirección de capas.
- **Parámetros.** `scad.Parameters`/`SetParameters` leen y reemplazan solo el literal de las asignaciones del nivel superior
  anteriores al primer `module` (la regla de SL009). Hidden y expresiones no son editables.

## Para la interfaz (T13)

**Una sola definición de qué hace cada herramienta de escritura** en `backend/mcp/effects.go` (`Effect`):

| Efecto | Herramientas | Quién lo usa |
|---|---|---|
| `EffectCheckpoint` (deja un `.scad` nuevo; el resultado trae `file`) | `create_checkpoint`, `import_mesh`, `set_parameters`, `create_from_template`, `edit_mesh` | `mcp.CreatesCheckpoint`: la red de seguridad de Hermes (`runner.createdCheckpoint`) y de los CLIs (`cliSafetyNet`); espejo `CREATING_TOOLS` (`lib/chatSteps.ts`): enlace de la tarjeta y selección automática del checkpoint |
| `EffectTree` (cambia el árbol sin `.scad` nuevo) | `set_active_parent`, `rename_checkpoint`, `trash_checkpoint`, `restore_checkpoint` | `mcp.ChangesTree`: `refreshTreeAfterStep` (`app_chat_tree.go`) |
| `EffectFiles` (escribe fuera del árbol) | `export`, `install_library` | nada que refrescar |

`TestEffectsCoverTheWriteTools` exige que toda herramienta de `clichat.WriteTools()` tenga su entrada y que ninguna de
lectura la tenga: una herramienta nueva que cambie el proyecto **no compila a verde** sin declarar su efecto.
`TestDeclaredEffectsMatchWhatTheToolsDo` comprueba que las `EffectCheckpoint` devuelven de verdad un `.scad` nuevo en `file`, y
`chat-ux.test.mjs` lee `effects.go` y exige que `CREATING_TOOLS` diga lo mismo (más `autosave`).

**Refresco del árbol.** `set_active_parent`, `rename_checkpoint`, `trash_checkpoint` y `restore_checkpoint` no crean un
`.scad`: el padre activo y el rótulo no son un archivo, y el watcher solo ve aparecer o desaparecer `.scad` (tarde, tras
300 ms). Con Hermes corren en proceso y la app no se entera; con un CLI corren en el `ScadLab --mcp` de otro proceso.
Tras un paso **ok** de una de ellas —lo emite `runChatTurn` (Hermes) o `cliRecorder.emitStep` (CLIs)— la app llama a
`refreshTreeAfterStep`: `emitView` (rescan + `checkpoints:changed`), y el árbol, la papelera (que recuenta cuando cambia
`checkpoints`), el rótulo y el padre activo se actualizan al instante. `restore_checkpoint` antes marca el archivo con
`markRestored`, igual que `RestoreCheckpoint`: volver de la papelera no es trabajo nuevo (no avanza el padre activo ni se
renderiza solo). Es de mejor esfuerzo: con un CLI el paso puede llegar después del debounce del watcher.

Etiquetas e íconos de las tarjetas de paso: `ICONS`/`WRITES` en `ToolCallCard.tsx` y `steps.tools` de `es.ts`/`en.ts`.
Una tarjeta de `trash_checkpoint` no enlaza (el archivo ya no está en el árbol); la de `restore_checkpoint` enlaza al
nombre con que volvió.

## Agregar una herramienta

[13-mcp.md](13-mcp.md#agregar-una-herramienta), más: clasificarla en `readTools`/`writeTools` y, si escribe, declarar su efecto en `effects.go` (y en `CREATING_TOOLS` si crea checkpoints), sumarla a `wantTools`
(`mcp_test.go`), a la tabla de arriba y de `assets/workspace/RULES.md.tmpl`, y a las instrucciones de `agent.{es,en}.md` si
cambia el flujo.
