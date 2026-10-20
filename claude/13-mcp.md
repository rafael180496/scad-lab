# Servidor MCP de ScadLab (S6)

Spec: [.claude/specs/s6-servidor-mcp.md](../.claude/specs/s6-servidor-mcp.md).
Código: `backend/mcp` (servidor y herramientas), `main_mcp.go` (el modo `--mcp`),
`backend/checkpoint` (crear checkpoints, compartido con el editor). La
configuración de cada agente (`.mcp.json`…) la escribe `backend/mcpconf`
(bloque 6.B): ver [claude/09](09-agents-ai.md).

## Qué es

`ScadLab --mcp --workspace <dir>` es un servidor MCP **autónomo** por stdio:
JSON-RPC 2.0, un mensaje por línea, sin abrir la ventana. Lo lanza el agente
(Claude Code, opencode, Codex, Antigravity), no la app. No hay puente a la
ventana: ScadLab no tiene secretos que proteger, así que el proceso lee lo que
necesita de `scadlab.db` (**solo lectura** de settings, materiales y del
workspace registrado) y trabaja sobre los archivos de la carpeta. Funciona con la
app cerrada.

```
main.go: os.Args[1] == "--mcp"  →  runMCP (main_mcp.go)  →  mcp.Serve(ctx, stdin, stdout, opts)
                                   (corre ANTES de NewApp/wails.Run: nada de GUI)
```

- `mcp.ServerName = "scadlab"` y `mcp.Args(dir) = ["--mcp","--workspace",dir]`: la
  línea de comandos la define este paquete y la reutiliza `mcpconf`.
- **stdout solo lleva JSON-RPC.** Los logs van a stderr; OpenSCAD corre siempre
  con su salida capturada. Un test (`helpers_test.go`) exige que cada línea de
  stdout sea JSON-RPC 2.0 parseable.
- Sin SDK de MCP: `rpc.go` es un puerto del de mini-tools. `initialize` negocia
  versión (cliente ≥ `2025-06-18` → `2025-06-18`; si no `2024-11-05`), más
  `tools/list`, `tools/call`, `ping`; las notificaciones no llevan respuesta salvo
  `notifications/cancelled`, que cancela la petición en curso.
- Las llamadas a herramientas corren en su propia goroutine: un render de minutos
  no bloquea `ping` ni la cancelación (se mata el OpenSCAD). Al llegar a EOF se
  **espera** a las que están en curso: `printf '…' | ScadLab --mcp` tiene que
  poder contestar.
- Un nombre de herramienta desconocido o `arguments` que no es un objeto son
  errores de protocolo (-32602). Todo lo que falla **dentro** de una herramienta
  vuelve como `isError: true` con el mensaje (`i18n.Msg`, español con tú / inglés
  según `settings.language`, que se relee en cada llamada).
- Descripciones de herramientas y claves del JSON en inglés (los modelos las leen
  mejor); los avisos y errores salen en el idioma de la app.

## Herramientas

El catálogo completo —cada herramienta, qué hace, si es de lectura o de escritura y en qué superficie está— vive en
[25-herramientas-ia.md](25-herramientas-ia.md). Resumen: workspace y árbol (`list_checkpoints`, `read_checkpoint`,
`create_checkpoint`, `set_active_parent`, `rename_checkpoint`, `trash_checkpoint`, `list_trash`, `restore_checkpoint`,
`list_refs`), imágenes (`image_info`, `compare_with_image`, `trace_image_outline`, `image_to_heightmap`; S15), modelado (`render`, `lint`, `format`, `preview`, `measure`, `get_parameters`, `set_parameters`,
`list_templates`, `get_template`, `create_from_template`, `list_libraries`, `install_library`, `list_skills`, `read_skill`),
STL/3MF/OBJ (`inspect_reference`, `import_mesh`, `edit_mesh`) e impresión (`printer_info`, `list_printers`,
`set_workspace_printer`, `estimate_print`,
`list_materials`, `suggest_orientation`, `export`). El servidor `--mcp` y la caja en proceso de Hermes ofrecen **el mismo
catálogo**.

Detalles que no son obvios:

- **Herramientas de imagen** (S15). `image_info` y `compare_with_image` son de **lectura**; `trace_image_outline` e
  `image_to_heightmap` son de **escritura** (efecto `EffectFiles`: dejan un `.svg` o un `.dat` en `refs/`, nunca pisan). La
  imagen se resuelve con `refmodel.ResolveImage` (solo `refs/` y `refs/images/`, mismo confinamiento que `ResolveRef`) y se
  decodifica con los límites de `backend/imagetools` (20 MB, 40 MP, 2048 px, 10 s). Los resultados llevan la imagen primero y
  un único texto JSON después (el que lee la tarjeta del chat). `list_refs` suma las imágenes con `kind: "image"`. Detalle en
  [25](25-herramientas-ia.md#imágenes-s15).
- **`import_mesh`** (S13) es de **escritura** (crea un checkpoint; en el chat con un CLI solo está en «Agente»). La ruta pasa por
  `refmodel.ResolveRef`, la misma puerta de `inspect_reference`: un solo nombre de `refs/`, formato conocido, un enlace
  simbólico solo si su destino sigue dentro de `refs/`. Avisa (`warnings`) de una malla abierta —importarla sola funciona,
  pero cualquier booleano falla con «mesh is not closed»—, de más de 500 000 triángulos, de un 3MF con colores (se aplana) y
  de un modelo que no cabe en la placa. No es idempotente: llamarlo dos veces crea dos checkpoints raíz.
- **`render` con modelo roto es `isError: true`**, aunque el JSON es el mismo que
  el de un render bueno (con `status`, `diagnostics`, `message`): la herramienta
  no logró su propósito (una malla) y el agente tiene que corregir. `lint`, en
  cambio, devuelve diagnósticos como resultado normal: encontrarlos es su trabajo.
- **`RENDER_ERRORS.md`** sigue la regla de `app_render.go`: solo el checkpoint
  **más nuevo** lo escribe (error) o lo borra (OK); renderizar uno viejo, un
  cancelado o sin OpenSCAD no lo toca. Lo aplica `env.render`, que usan `render`
  y `measure`.
- **Estado de render en `list_checkpoints`**: lo renderizado por este proceso, y
  si no, lo que diga `RENDER_ERRORS.md`. La app y el servidor no comparten
  memoria; `unknown` es una respuesta honesta.
- **Una cola de render por checkpoint** (`mcp:<ws>:<file>`): dos renders de
  archivos distintos en paralelo no se cancelan entre sí (el motor cancela por
  clave); repetir el mismo reemplaza al anterior. `export` usa otra clave.
- **`preview`** corre `openscad -o x.png --render --colorscheme=Tomorrow
  --imgsize=N,N --viewall --autocenter --camera=0,0,0,rx,ry,rz,500` con
  `--backend=manifold` salvo que la configuración diga `cgal` (si OpenSCAD no lo
  conoce, reintenta sin él). `size` se acota a 256–1024 (default 640). Vistas
  (verificadas con una pieza asimétrica): `iso` y `iso-back` en perspectiva;
  `front` (desde −Y), `back`, `right` (desde **+X**), `left`, `top`, `bottom`
  ortogonales. Ojo: «Right» en la GUI de OpenSCAD (`90,0,270`) mira desde −X, la
  convención contraria; aquí `right` es la de los planos. `rotate: [rx, ry, rz]` (grados enteros) da un ángulo libre, con `ortho`
  opcional; el nativo y el rasterizador lo reciben igual. El PNG va a un temporal
  en `<ws>/.scadlab/preview/` (ignorado por git, el watcher no es recursivo) y se
  borra siempre.
- **`measure`**: la malla, las **partes por color** y una **estimación de
  impresión** por parte y total (`backend/estimate`: cáscara de perímetros ×
  ancho de línea + relleno, con la densidad y el costo del material por defecto;
  `estimatedWeightG` es ese total, ya no la pieza sólida al 100 %). Los
  `assumptions` van en el resultado. Avisa si la malla no es cerrada (volumen no
  fiable) o si el modelo no apoya en z=0. «Cabe» es `parts.Fit`, el mismo
  cálculo de placa que el visor (0,05 mm de tolerancia). Detalle y fórmula en
  [claude/14](14-medicion.md).
- **Sin OpenSCAD instalado** las herramientas siguen funcionando con el **motor
  integrado** (OpenSCAD en WebAssembly, [claude/17](17-motor-wasm.md)): `render`,
  `measure`, `lint` y `export` lo usan sin cambios para el agente (`render` agrega
  `engine: "wasm"` y un `engineNote`; `lint`, un `note`; `printer_info`, `openscad.engine`).
  Un solo `scadwasm.Runner` por proceso (`env.wasm`), con la caché de compilación de
  `<datos>/cache/wasm` compartida con la app. `preview` no puede pedirle el PNG a
  OpenSCAD (necesita OpenGL): renderiza con el integrado —misma caché que `render`,
  incluido `RENDER_ERRORS.md`— y dibuja la malla con `backend/raster` (z-buffer por
  software, sombreado plano, **un color por parte** salido de la caché de partes,
  las mismas ocho vistas y el mismo tamaño que el nativo). Un modelo roto da el
  mismo error con su línea que el preview nativo.
- **Impresora del workspace**: la del workspace registrado en la app (por ruta), ya con sus colores y ajustes
  (`wsconfig.EffectivePrinter`/`EffectiveSettings`); si la carpeta no está registrada, `printer.json`; si no, la de
  Configuración. `printer_info` la describe entera (marca, modelo, sistema multicolor, `colors`/`maxColors`, filamento, slicer,
  skill de la marca, fuentes); `list_printers` lista el catálogo de las siete marcas; `set_workspace_printer` cambia la impresora
  y los colores de un workspace **registrado** con `wsconfig.Apply` (escribe en `scadlab.db` y reescribe reglas y skills; en una
  carpeta no registrada falla con un mensaje claro). Detalle: [25](25-herramientas-ia.md).

## Seguridad

- **Toda ruta es un nombre de archivo suelto** (`checkpoint.ValidName`: sin
  carpetas, termina en `.scad`), y si en disco es un enlace simbólico su destino
  tiene que seguir dentro del workspace (`env.checkpointFile`). Los argumentos
  `file` de **todas** las herramientas pasan por ahí; un test los prueba con
  `../`, rutas absolutas, subcarpetas, NUL y un enlace a un archivo externo.
- `exports/` se comprueba igual: si es un enlace hacia afuera, no se escribe.
- Los checkpoints son inmutables: `create_checkpoint` usa `O_EXCL` y avanza el
  número si el nombre se ocupó entre medio; ninguna herramienta edita o borra un
  `.scad` (`trash_checkpoint` lo **mueve** a `.scadlab/trash/` y `restore_checkpoint` lo trae de vuelta, sin pisar).
  `rename_checkpoint`, `trash_checkpoint` y `set_active_parent` exigen `file` (vacío no es «el más nuevo»).
- `refs/` y `lib/` se comprueban como `exports/` (una carpeta que sale del workspace no se usa); `install_library` solo baja
  lo del catálogo fijo, con SHA-256 y sin `..` ni enlaces ([25](25-herramientas-ia.md)).
- Nada se escribe fuera de `<ws>` salvo la caché de mallas y de exportaciones de
  `<datos>/cache/` (la misma de la app). `get_template` busca el nombre en la
  lista embebida, no arma una ruta con lo recibido.
- La base se abre **solo si ya existe** (`scadlab.db`): lanzar un agente no crea
  datos de la app. Sin base se trabaja con los defaults.
- Sin puerto TCP: solo los descriptores del proceso que lo lanzó.

## Windows

El `.exe` de ScadLab es de subsistema gráfico (sin consola), pero un proceso hijo
**hereda los pipes** que le abre quien lo lanza (Claude Code, opencode… lo hacen
con `CreateProcess` y handles heredables), así que stdin/stdout funcionan; es lo
mismo que hace mini-tools con su propio `--mcp`. OpenSCAD (de consola) se lanza
con `hideWindow` para que no parpadee una consola por cada render. No se probó
con un agente real en Windows desde macOS: compila (`GOOS=windows go build ./...`)
y la lógica es la misma; si un cliente no entregara los handles, `runMCP` termina
con código 1 y el error en stderr.

## Pruebas

- `backend/mcp` — end-to-end por `io.Pipe` con OpenSCAD real (`t.Skip` si no
  está) y `SCADLAB_DATA_DIR` temporal: `initialize` (versiones), `tools/list`
  (todas con schema), cada `tools/call` (render OK y con error con línea,
  `create_checkpoint` nunca pisa, `preview` devuelve PNG válido, `export` escribe
  en `exports/`, rutas fuera del workspace → `isError`), cancelación y stdout
  limpio.
- `mcp_engine_test.go` — el servidor **sin OpenSCAD** (`SCADLAB_NO_SYSTEM_OPENSCAD=1`):
  render, measure (partes por color), lint, export (STL, 3MF con 2 partes, STL por
  color) y `preview` rasterizado, con `-race`.
- `main_mcp_test.go` — `runMCP` en proceso (códigos de salida, stderr) y, con
  `SCADLAB_MCP_SMOKE=1`, el binario real compilado y lanzado con `--mcp`.
- `backend/checkpoint` — crear, carrera con el agente, raíz, encabezado.
- `turnguard_test.go` + `backend/turnmark` — «Solo consulta» en Antigravity: con la marca `.scadlab/turn-mode` viva, el servidor rechaza cada herramienta de
  `WriteTools()` y deja pasar la lectura; sin marca escribe; la caja de Hermes no la lee ([26](26-antigravity-permisos.md)).
- Herramientas de T11 ([25](25-herramientas-ia.md)): `tools_workspace_test.go` (árbol, padre activo, rótulos, papelera y
  restaurar con rutas hostiles, `list_refs`, y que `readTools`/`writeTools` cubran el catálogo), `tools_params_test.go`
  (`set_parameters` y `create_from_template` renderizan, nativo y WASM), `libraries_test.go` (zip falso: SHA-256, zip slip, enlaces,
  red caída; `SCADLAB_REAL_DOWNLOAD=1` baja las tres de verdad), `tools_mesh_test.go` (`edit_mesh` = `meshedit.Generate`,
  `suggest_orientation`, `estimate_print`, `preview` con `rotate`, `export` png, todo por `--mcp`) y `backend/scad/params_test.go`.
- **Prueba con un CLI real** (`claude mcp list` en un workspace temporal) la hace
  el architect; los tests del repo no lanzan agentes ni slicers (regla 9).

## Agregar una herramienta

1. Entrada nueva en `env.tools()` o en el grupo de su área (`tools_workspace.go`, `tools_params.go`, `libraries.go`,
   `tools_mesh.go`, `tools_print.go`): `Name`, `Description` en inglés, `InputSchema` con `obj(...)`, `Annotations`
   (`annReadOnly`/`annWrite`) y el `handler`.
2. El handler resuelve cualquier archivo con `e.checkpointFile` y devuelve
   `jsonResult(...)`/`textResult(...)`; un error del dominio es un `error` (sale
   como `isError`). Mensajes con `i18n.Msg`.
3. Clasificarla en `readTools`/`writeTools` de `backend/clichat/modes.go`, agregarla a `wantTools` en `mcp_test.go` y
   probarla (caso feliz, error, ruta inválida, en proceso y por `--mcp`), a la tabla de
   [25](25-herramientas-ia.md) y a la de `assets/workspace/RULES.md.tmpl`.

## Verificado con los CLIs reales (architect, S6)

`mcp list` de cada CLI (no abre sesiones ni gasta cuota), con la app compilada
y un workspace temporal:

| CLI | Resultado | Nota |
|---|---|---|
| Claude Code 2.1 | `scadlab … ⏸ Pending approval` | el `.mcp.json` de proyecto se aprueba en el primer `claude` interactivo; la aprobación vive en `~/.claude.json` (global: no se toca). `.claude/settings{,.local}.json` con `enabledMcpjsonServers`/`enableAllProjectMcpServers` **no** la saltan. Por eso, al lanzar Claude desde la terminal de ScadLab se agrega `--mcp-config '<json>'` (`mcpconf.ClaudeArgs`), que no necesita aprobación. `--mcp-config` es variádico: lo que sigue en el argv tiene que ser un flag. |
| Codex | `scadlab … enabled` | con los flags `-c` de `mcpconf.CodexArgs`; rutas con espacios OK |
| opencode 2.0 | `✓ scadlab connected` **solo en un repo git** | fuera de git `opencode mcp list` dice «No MCP servers configured», pero `opencode debug config` muestra la entrada cargada (`opencode.json` del workspace). No se inicializa git en los workspaces por esto (decisión del usuario). |
| Antigravity 1.3.6 (`agy`) | **`.gemini/settings.json` de proyecto NO se lee** | con ese archivo, `agy mcp list` dice «No MCP servers configured». Era del **Gemini CLI anterior** (rótulo «Gemini CLI (anterior)» en `mcpconf` y la UI; se sigue escribiendo para quien usa `gemini`). `agy` solo tiene configuración **global** y no tiene flags al arrancar para pasarle un servidor. Ver «Antigravity» abajo. |

## Antigravity: configuración global, registrada al elegirlo

Código: `backend/mcpconf/antigravity.go` (argv, parseo de `agy mcp list`, estados),
`app_mcp.go` (`AntigravityMCPStatus`, `AddAntigravityMCP`, `RemoveAntigravityMCP`,
variables de paquete `agyExec` y `agyPath` inyectables en tests) y
`AntigravityRow` en `McpSettings.tsx`.

- **Qué hace.** `agy mcp add -- scadlab <ScadLab> --mcp` / `agy mcp remove scadlab` /
  `agy mcp list`, con argv (sin shell), `hideWindow` en Windows, plazo de 15 s
  (`agyTimeout`) y `agy` resuelto con `detect.Find("agy")` (no `gemini`). `add` es un
  upsert: sirve para corregir una ruta vieja. Nunca se edita el archivo de agy a mano.
- **Consentimiento (los botones).** «Agregar a Antigravity» / «Quitar de Antigravity»
  piden `confirmDialog` diciendo que se modifica la configuración global (`~/.gemini`),
  la de todos los proyectos. El interruptor del MCP y «Volver a sincronizar» **no** la
  tocan.
- **Registro automático (S12.c).** Sin su servidor, `agy` no tiene herramientas de ScadLab:
  intenta la shell (denegada) y el turno del chat no deja nada. `App.EnsureAntigravityMCP`
  (`app_mcp.go`) corre `agy mcp list` y, **solo si `scadlab` falta** (`AgyMissing`), llama a
  `AddAntigravityMCP` — mismo argv, sin `--workspace`—. Una entrada que ya existe (otra
  ruta, desactivada) es del usuario y no se toca; con el MCP apagado en Configuración no hace
  nada; sin `agy` tampoco. Lo disparan: la UI al mostrar `cli:antigravity` (`ChatPanel`),
  `prepareCLIMCP` al empezar un turno y `StartAgentTerminal` antes de lanzarlo. Emite
  `mcp:antigravity {added, permissions, settingsPath, error}` (toast en `App.tsx`); un `agy` que no lo acepta devuelve
  `msgAgyEnsureFailed` con la salida de `agy`, y el turno lo repite en el prompt (el CLI debe
  entregar un bloque) y, si no dejó nada, en su respuesta. `agyMu` serializa los add/remove y
  `agyEnsureMu` el registro (dos clics no avisan dos veces). **Las pruebas nunca lanzan el `agy`
  real**: `TestMain` deja `agyExec` en un doble que solo contesta `mcp list` y rechaza lo demás.
- **Permiso por herramienta (T14).** Registrar el servidor no basta: en `agy --print` cada herramienta MCP sin aprobar se rechaza y
  el turno termina. ScadLab agrega al `settings.json` global de Antigravity una regla `mcp(scadlab/<herramienta>)` por cada herramienta
  del catálogo (junto a este registro y en cada turno; Quitar/Volver a agregar en Configuración), y el servidor `--mcp` hace cumplir «Solo
  consulta» con la marca `.scadlab/turn-mode`: [26-antigravity-permisos.md](26-antigravity-permisos.md). Los hooks no sirven (medido).
- **Entrada sin `--workspace`.** `scadlab` → `<bin> --mcp`: la carpeta es, por orden, `--workspace`, la variable
  `SCADLAB_WORKSPACE` (el chat la pone en cada turno de `agy`) y el cwd (`mcp.Options.WorkspaceDir` vacío → `os.Getwd()`, ver
  `main_mcp.go`; la primera línea de stderr dice cuál valió), y `agy` lo lanza en la carpeta del proyecto, así que una entrada sirve a todos los workspaces.
  **Riesgo no verificado con una sesión real** (abrirla gasta cuota): si `agy` no
  lanzara el MCP con cwd = proyecto, el servidor trabajaría en otra carpeta y la
  herramienta lo diría al usarla. La ayuda lo documenta.
- **Estado** (`mcpconf.AgyStateOf` sobre `ParseAgyList`): `not-installed`, `missing`,
  `ok` (comando == `<bin> --mcp`), `other` (otro comando, p. ej. la app se movió),
  `disabled`, `error`. Quitar sin entrada no llama a `agy mcp remove` (saldría con
  error «not found»).
- **Formato real verificado con `HOME` temporal** (agy 1.3.6 respeta `HOME`):
  la configuración vive en `~/.gemini/config/mcp_config.json` (no `~/.gemini/mcp_config.json`),
  `list` imprime `NAME  TYPE  STATUS  COMMAND/URL` (STATUS `enabled|disabled`; la columna
  de comando es «ejecutable args…» separado por espacios, con la ruta sin comillas),
  «No MCP servers configured.» si no hay, `remove` de un nombre inexistente sale con
  código 1 («not found»), y `add` escribe `{"command","args","disabled":false}`.
  Los flags de `add` van antes del nombre; `--` antes del comando si empieza con `-`.
- **Pruebas.** `app_mcp_test.go` (`fakeAgy` con la salida real, argv exacto de add,
  remove y list, ciclo de estados, otros servidores intactos, fallos) y
  `mcpconf/antigravity_test.go`. Los tests nunca lanzan el `agy` real.

## La misma caja, en proceso (Hermes, S12)

`mcp.NewToolbox(mcp.ToolboxOptions{WorkspaceDir, Store, Engine, WASM, Parts})` expone **las mismas herramientas** sin lanzar un
subproceso `--mcp`: el chat de la app (Hermes, [19-hermes.md](19-hermes.md)) las llama como funciones (`Tools()`, `Call(ctx, name, args)` →
`ToolOutput{Text, Images, IsError}`), con la base, el motor de render, el WebAssembly y la caché de partes **de la app** inyectados (no abre
una segunda base ni un segundo motor; `Close()` no cierra lo inyectado). Desde T11 el catálogo es idéntico al del servidor:
`list_skills`, `read_skill` e `inspect_reference` también están en `--mcp` (`InApp` se conserva sin efecto). Una herramienta nueva se
agrega una sola vez y llega a los dos.
