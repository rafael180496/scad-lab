# Hermes: chat agéntico con proveedores de IA (S12)

El chat del panel inferior ya no es solo texto: con **Agente** encendido, el modelo
usa las herramientas de ScadLab (crear checkpoints, renderizar, mirar, medir…) en un
bucle hasta resolver el pedido. Spec: [.claude/specs/s12-hermes-proveedores.md](../.claude/specs/s12-hermes-proveedores.md).
Regla de la fase: **lo genérico se porta de mini-tools, no se reinventa**; lo nuevo
es solo lo específico de OpenSCAD.

## Capas

```
backend/llm        clientes ollama / openai / anthropic (portado de mini-tools)
backend/vaultgate  llavero propio: contraseña maestra (argon2id) + AES-256-GCM (backend/crypto); ver 21-llavero.md
backend/store      model_servers (v5), chat_messages.parts/tokens/costo (v6)
backend/mcp        Toolbox: las herramientas del servidor --mcp, EN PROCESO
backend/refmodel   análisis, vista previa y copia a refs/ de STL / 3MF / OBJ
backend/hermes     el bucle, los adjuntos y el registro de turnos (esta fase)
backend/clichat    los CLIs (Claude Code, Codex, Antigravity, OpenCode) como proveedores del chat (S12.b)
backend/chat       Manager/Turn: un turno por chat, cancelación, trozos de texto
app_servers.go     bindings de servidores y modelos (+ chequeo previo cacheado)
app_chat.go        SendChatMessage y el turno; app_chat_attach.go: adjuntos
```

## El bucle (`backend/hermes/run.go`)

Es el `llmrunner.go` de mini-tools con las herramientas de ScadLab:

- **Un error de herramienta vuelve al modelo como `error: …`**, nunca corta el turno:
  herramienta inexistente, argumentos que no son JSON (`_raw`), pánico, render con
  error de línea. El resultado se recorta para el modelo y a 4000 caracteres para la tarjeta. **Desde S17 los topes son
  proporcionales a la ventana de contexto** (`Config.NumCtx`; ver «Modelos chicos» abajo): con la ventana desconocida rigen los de siempre
  —32 000 caracteres por resultado y 160 000 de mensajes, con los resultados viejos reducidos a una nota, `compactToolResults`—.
- **Imagen de `preview` / `inspect_reference`**: se escribe a
  `<datos>/chat-attachments/<chatId>/step-*.png` (`llm.Message.Images` son **rutas**) y
  vuelve como un mensaje `user` justo después de los resultados —OpenAI y Ollama no
  aceptan imágenes en un mensaje de herramienta; Anthropic los funde—. Solo si el modelo
  ve (o no se sabe); si no, el resultado lleva una nota. Los archivos se borran al terminar.
- **Tope de pasos** (1–30, default 12; cada llamada cuenta uno): las llamadas que ya no
  caben se contestan con un error sin ejecutarse, el modelo resume en **una** última
  vuelta (con las herramientas declaradas: Anthropic lo exige si hay `tool_use`) y el
  turno cierra con un aviso. `Result.StepLimit` lo recuerda en `parts`.
- **Sin herramientas**: `llm.IsToolsUnsupported` → modo texto con el prompt de S5 y un
  aviso (una vez); el par servidor|modelo se recuerda en memoria
  (`chatService.noTools`) y el turno siguiente ni lo intenta. Si las capacidades del
  modelo ya dicen que no (`toolsKnown && !tools`), no se gasta un pedido en fallar.
- **Cancelar a mitad de una herramienta**: el `ctx` llega a `Toolbox.Call`; el paso queda
  `error` con «cancelado», se conserva el texto y los pasos, y no es un error del turno.
- **Reintento** de un fallo de conexión transitorio (una vez, ~1 s, solo si no llegó nada
  del stream). El pensamiento (`Delta.Thinking`) no se muestra; `Message.Raw` (bloques
  firmados de Anthropic) se reenvía tal cual dentro del turno abierto.
- **`Step.TextAt`**: el texto visible (unidades UTF-16, el `length` de JS) que llevaba el
  turno al arrancar el paso. El frontend parte el texto ahí (`interleave` en
  `lib/chatTypes.ts`) y las tarjetas quedan donde ocurrieron. El texto pendiente se vacía
  antes de anunciar un paso (`Hooks.Flush`) para que no llegue después de él.

## Contexto y memoria

No hay historial en memoria que sobreviva entre turnos: **cada turno arma el contexto de
`chat_messages`** (`hermes.SeedHistory`). Retomar tras reiniciar y seguir en la misma
sesión son el mismo camino. Solo van usuario y asistente; los pasos no se reenvían (el
resultado queda en el texto final y en los checkpoints, que lista el prompt de sistema),
las imágenes tampoco (una línea con los nombres) y de un modelo de referencia viaja su
informe, que dice dónde quedó en `refs/`. Los mensajes seguidos del mismo rol se funden.

**Entre turnos (S17)**: lo que no cabe del historial ya no se pierde: `SeedHistory` lo reduce a un **resumen extractivo** (qué se pidió, qué se
contestó y qué archivos se tocaron, sin llamar al modelo) que encabeza el primer mensaje conservado, y los últimos mensajes viajan enteros. El
presupuesto del historial es proporcional a la ventana (`chat.HistoryBudgetFor`: un tercio de ella en caracteres, 4 000–60 000 en Ollama).

El prompt de sistema es `prompts.AgentSystemPrompt`: con herramientas, el de Hermes
(bucle, tope, skills del workspace —nombre y descripción, se leen con `read_skill`—,
referencias adjuntas, si el modelo ve); sin ellas, el de S5.

## Modelos chicos y tiempos (S17)

Un Ollama de 8k–32k no entra con el catálogo completo, un prompt largo y el código de una maceta: medido, **17 900–28 500 tokens por vuelta** contra 8 192
de ventana (con las esenciales: 1 900–3 500). Spec: [.claude/specs/s17-edicion-compacta-modelos-chicos.md](../.claude/specs/s17-edicion-compacta-modelos-chicos.md); todo el detalle,
las medidas y el cálculo de ↑/↓ están en **[35-edicion-compacta.md](35-edicion-compacta.md)**. En corto:

- **Ventana efectiva** (`effectiveWindow`, `app_chat_context.go`): la del servidor si está fijada; si no, `min(ventana del modelo, 32 768)` con mínimo
  8 192 (`/api/show`), y si el modelo no la dice, `chatDefaultNumCtx` (16 384). Va al pedido de Ollama como `options.num_ctx` por petición
  (`llm.ChatRequest.NumCtx`, no por cliente: el cliente se arma antes de saber el modelo).
- **Perfil de herramientas** por conversación (`chats.tools`: Auto / Esenciales / Todas): esenciales = `Toolbox.ToolsFor(ProfileEssential)` con el prompt compacto.
  `Config.ToolSpecs` declara menos, pero `Call` sigue aceptando todo el catálogo.
- **Dentro del turno** (`backend/hermes/run.go`): presupuesto y tope por resultado proporcionales a `NumCtx`; una lectura repetida que no cambió deja la vieja
  como `[archivo X leído, versión Y…]` (`dedupeRead`); al apretar, resultados viejos a nota y luego los textos largos de las llamadas viejas.
- **«Nueva conversación con resumen»** (`NewChatWithSummary`) al pasar el 70 % de la ventana.
- **Tiempo y tokens**: `Step.roundInput/roundOutput/resultChars/resultTokens` y `Usage.modelMs/toolMs/rounds/peakInput/lastInput`, que alimentan las tarjetas y el pie del turno.
- **Un turno sin texto** cuenta qué hizo (`turnRecap`), también el de un CLI.

## Adjuntos (`backend/hermes/attachments.go`, `app_chat_attach.go`)

| Tipo | Límite | Qué pasa |
|---|---|---|
| Imagen PNG/JPG/GIF/WebP | 20 MB (se reduce a ≤ 2048 px y ≤ 5 MB para el modelo; el original va a `refs/images/`) | se guarda en `<datos>/chat-attachments/staging/`; al enviar se **copia a `<ws>/refs/images/<slug>-<hash8>.<ext>`** (S15: deduplicada por contenido, sin pisar, escritura atómica), pasa a `<chatId>/` y viaja como imagen; el texto del turno cierra con una línea con la ruta relativa para las herramientas de imagen |
| Modelo STL / 3MF / OBJ | 64 MB | se copia a `<ws>/refs/` (`refmodel.CopyToRefs`), se analiza y su informe (`PromptText`) entra al mensaje; la vista previa viaja como imagen solo si el modelo ve |

- Máximo **10** por mensaje. El tipo se decide **por el contenido** (un PNG llamado
  `.stl` es una imagen); no hay PDF ni texto pegado.
- Los pendientes viven en un registro en memoria (`hermes.Registry`) con vencimiento de
  24 h. Quitar un chip borra lo que se creó (la copia en `refs/` solo si la hizo ese
  adjunto); al cerrar la app se descartan los pendientes.
- Imagen con un modelo que **informó** que no ve → error **antes** de guardar nada
  (`hermes.CheckVision`). Un servidor que no lo informa (`visionKnown=false`) nunca bloquea.
- El historial guarda ficha, miniatura de 96 px y informe, nunca los bytes. La ficha de una **imagen** lleva `refPath` (`refs/images/…`) desde S15 y
  `hermes.AttachmentNote` la nombra en el historial que se reenvía, para que un turno posterior pueda volver a usar la foto.
- **Fotos en el workspace (S15)**: `hermes.PrepareIn` copia primero todas las fotos a `refs/images/` y después mueve las de staging (si una ya no está, ninguna se
  movió). Con `SendOptions.ImagesAsFiles` las fotos **no** viajan como imagen ni pasan por `CheckVision` («Usar trazado de contorno»). Detalle y decisiones:
  [28-modelo-desde-imagen.md](28-modelo-desde-imagen.md).

## Persistencia (migración v6)

`chat_messages` gana `parts` (JSON), `input_tokens`, `output_tokens`, `cost_usd`.
`role` sigue siendo `user|assistant`: los pasos viajan en `parts.steps` (el CHECK no se
puede ampliar de forma aditiva). Las imágenes de un paso se guardan reducidas a JPEG de
≤ 640 px; en vivo viajan como PNG completo. El costo solo se calcula con precios
publicados (OpenRouter, `serverModelInfo`); **0 = «no informado»**. Ver
[07-database.md](07-database.md).

## Eventos y resumir un turno

`chat:delta`, `chat:step`, `chat:usage`, `chat:done`, `chat:error`: payloads en
[06-go-react-contract.md](06-go-react-contract.md). `GetChat` devuelve también los
`steps` y el `usage` del turno en curso: cerrar y volver a abrir el panel a mitad de un
turno retoma por `seq` sin duplicar texto, y un paso ya terminado nunca vuelve a «en
curso» (`upsertStep` en `chatStreams.ts`).

## Frontend (`components/chat/`)

`ChatPanel` orquesta; `Messages` pinta el texto con las `ToolCallCard` intercaladas y los
tokens/costo al pie; `useChatAttachments` y `AttachmentChips` llevan los adjuntos. La barra
superior, los selectores de proveedor y modelo, la caja de mensaje, el estado vacío, las
tarjetas, la búsqueda y el ancho del panel (S12.b) están en [22-chat-ux.md](22-chat-ux.md).
Borrar una conversación pasa por `confirmDialog({danger:true})`. El modo Agente se apaga y
lo explica si `toolsKnown && !tools`; Enviar se deshabilita solo con `visionKnown && !vision`
y una imagen.

## CLIs como proveedores (S12.b)

Spec: [.claude/specs/s12b-cli-chat-ux.md](../.claude/specs/s12b-cli-chat-ux.md). `backend/clichat` es el `agentchat` +
`agents` + `agentmodels` de mini-tools portado; `app_chat_cli.go` lo conecta con el chat. Un CLI es el proveedor `cli:<id>`
(`claude`, `codex`, `antigravity`, `opencode`); **no** hay fila en `model_servers` ni nada que configurar: Configuración →
Proveedores solo los muestra, de solo lectura, en «CLIs detectados» (`ListDetectedCLIs`).

- **Autodetección** (`registry.go`): `backend/detect` (PATH + rutas típicas + `.opencode/bin`…) y, como último recurso en
  macOS/Linux, el PATH de la shell de login (`detect.FindLogin`, `-ilc`, una vez cada 10 min, solo si lo barato falló). Caché de
  30 s; `RefreshChatProviders` la salta. `agy` y, si queda, `gemini` son Antigravity. **Un CLI no instalado no aparece** en
  `ListChatProviders` (mini-tools filtra por `available` en su selector del chat); `ListDetectedCLIs` sí lo trae con
  `installed:false` para Configuración. En pruebas, `SCADLAB_NO_EXTRA_DIRS=1` limita `Find` al PATH.
- **Modos** (`modes.go`): «Solo consulta» = el `ask` de mini-tools, «Agente» = su `edit`. En ninguno corre la shell ni se
  pasan `--dangerously-*`/`--auto`/`bypassPermissions`. Las herramientas de ScadLab se clasifican en lectura (`list_checkpoints`,
  `read_checkpoint`, `lint`, `format`, `render`, `measure`, `preview`, `printer_info`, `list_templates`, `get_template`…) y escritura
  (`create_checkpoint`, `import_mesh`, `edit_mesh`, `export`… solo en «Agente»); la lista completa está en
  [25-herramientas-ia.md](25-herramientas-ia.md) y una prueba las cruza con el catálogo del servidor MCP.
- **Flags** (`session.go`, `BuildArgs`; el prompt es lo primero salvo en Codex/OpenCode, que lo llevan tras `--`):

| CLI | Base | «Solo consulta» / «Agente» | Sesión |
|---|---|---|---|
| Claude Code | `-p <prompt> --output-format stream-json --verbose --append-system-prompt <idioma>` + `--mcp-config` + `--add-dir`* | sin `--permission-mode` / `--permission-mode acceptEdits`; `--disallowedTools Bash,PowerShell` (+ `Edit,Write,MultiEdit,NotebookEdit` en consulta); `--allowedTools mcp__scadlab__<lectura>` / `+ create_checkpoint,export` | `--resume <id>` |
| Codex | `exec` + `-c mcp_servers.scadlab.*` + `--add-dir`* + `--json --skip-git-repo-check` | `--sandbox read-only` y `-c mcp_servers.scadlab.enabled_tools=[lectura]` / `--sandbox workspace-write` y `enabled_tools=[todas]`; imágenes `-i`, prompt tras `--` | `resume <id>` (antes de las banderas de `exec`: `resume` no repite `--sandbox`) |
| Antigravity | `--print <prompt> --output-format stream-json` + `--add-dir`* | sin `--mode` / `--mode accept-edits` | `--conversation <id>` |
| OpenCode | `run --format json --standalone` + `--model` + `-- <prompt>` | `--agent plan` y `OPENCODE_CONFIG_CONTENT={"permission":{"bash":"deny","scadlab_<lectura>":"allow","scadlab_<escritura>":"deny"}}` / `--agent build` (`bash` denegado y todas las `scadlab_*` en `allow`) | `--session <id>` |

  `*` = los flags que ya arma la terminal (`mcpLaunchArgs`, `addDirArgs` de `app_terminal.go`/`app_mcp.go`): una sola
  definición. OpenCode lee el MCP de `<ws>/opencode.json`; Antigravity solo del `agy mcp add` global. Las imágenes adjuntas se
  nombran en el prompt (Codex: `-i`) por su copia en `<ws>/refs/images/` (S15; dentro del `cwd`, sin permiso extra); solo la vista previa de un modelo 3D
  sigue en `<datos>/chat-attachments/<chat>/`, cuya carpeta va como `--add-dir` a Claude/Antigravity para que puedan leerla. `--model` y la sesión se validan (no pueden leerse como bandera).
- **Eventos** (`adapters.go`, `opencode.go`): línea NDJSON → `Event` (`text`, `tool`, `toolresult`, `done`, `error`); lo que no se
  entiende se descarta. La herramienta llega como paso de Hermes (`chat:step`): el prefijo del servidor se quita
  (`mcp__scadlab__render`/`scadlab_render` → `render`); el resultado (y la imagen de `preview`, Claude) cierra el paso; un paso
  que el CLI no cierra queda `ok` al terminar el turno, o `error`/«cancelado» si se cortó. OpenCode no cierra su stream: el uso
  sale de `opencode session export`. El texto de Antigravity y Claude se recompone con `utf8Stitcher` (un carácter partido entre
  dos trozos no se rompe). Uso y costo (solo Claude Code y OpenCode lo informan) se guardan igual que en Hermes.
- **Sesión** (`chats.cli_session`, migración 8): se guarda apenas el CLI la informa (también si se cancela). Cambiar de proveedor
  la olvida (`store.SetChatModel`); si no hay sesión y el chat ya tenía mensajes, el primer prompt lleva el hilo (≤ 12 000 caracteres,
  `SeedHistory`). Una sesión que el CLI ya no tiene falla antes de decir nada → se olvida y se reintenta **una vez** sin ella.
- **Cancelar**: `Setpgid` + `kill(-pgid)` (macOS) o `taskkill /T /F` (Windows); el nieto (servidor MCP) muere con el CLI.
- **Windows**: `.cmd`/`.bat` por `cmd.exe /c`, `.ps1` por PowerShell. Con un shim el prompt va por **stdin** (Claude, Codex,
  OpenCode) porque cmd.exe reinterpreta comillas, `%`, `&` y corta en el salto de línea; Antigravity no lee stdin y su prompt se
  limpia (`shimSafe`). **Sin verificar en un Windows real**; el cross-compile y el argv sí se prueban.
- **Prompt** (`cliPrompt`): contexto de ScadLab y modo, el checkpoint abierto (≤ 16 KB; si no, `read_checkpoint`), el hilo si
  aplica y el mensaje con el informe de las referencias STL. Las reglas ya van en `CLAUDE.md`/`AGENTS.md`.
- **Modelos** (`models.go`): alias de Claude Code, `~/.codex/models_cache.json` (sin `visibility:"hide"`), `agy models`, catálogo
  de OpenCode filtrado a proveedores con credenciales (solo los nombres de `auth.json`); caché 10 min. «Por defecto» no es parte
  de la lista: la pone la UI con `defaultHint`.
- **Importar de mini-tools** (`app_servers_minitools.go`): `<config del SO>/mini-tools/vault.db` (o `SCADLAB_MINITOOLS_DIR`),
  `file:…?mode=ro&immutable=1`; solo `name`, `kind`, `base_url`, `default_model`, `context_tokens` si existe; nunca columnas
  cifradas, `salt.bin` ni el llavero. Mismo nombre o URL = `exists`; las filas nuevas nacen **sin clave**.
- **Pruebas**: CLIs falsos (scripts `sh` en un PATH temporal con el NDJSON de `backend/clichat/testdata`); ninguna abre un CLI real.
  **Sin verificar con sesiones reales**: el JSON de `mcp_tool_call` de Codex, `enabled_tools` de Codex y los permisos
  `scadlab_*` de OpenCode se basan en el formato documentado/inspeccionado, no en una corrida.

## Red de seguridad: el chat termina siempre en un `.scad` (S12.c)

Spec: [.claude/specs/s12c-chat-crea-checkpoints.md](../.claude/specs/s12c-chat-crea-checkpoints.md). Un turno en **«Agente»** que
termina **sin** haber creado un checkpoint y cuyo texto trae un bloque OpenSCAD completo se guarda solo (`app_chat_autosave.go`):

- **Qué guarda.** `designBlock` (sobre `prompts.ScadBlocks`) toma el último bloque cerrado que no sea un fragmento al lado de uno
  más del doble de largo. Padre: el de un intento anterior del turno; si no, el archivo abierto, el padre activo, el más nuevo;
  `""` (workspace vacío) = **raíz**. Pasa por `SaveChatCheckpoint` (cabecera con `desc=` del bloque, vínculo con el chat) y se
  renderiza con la caja `newChatToolset` (`render`, la misma que usa el modelo: malla, diagnósticos con línea,
  `RENDER_ERRORS.md`). Salen **dos pasos** ya terminados: `autosave` («Guardado automáticamente», su `file` es el enlace de la
  tarjeta) y `render`. Un bloque idéntico a uno ya intentado no se vuelve a guardar.
- **Hermes** (`Config.AutoSave`, `runner.autoSave`): corre al final de la vuelta que no pide herramientas, con o sin tool calling
  (un modelo con herramientas que contesta con el código también cae ahí). Si el modelo ya creó un checkpoint (`createdCheckpoint`:
  una herramienta con `mcp.CreatesCheckpoint` —`create_checkpoint`, `import_mesh`, `set_parameters`, `create_from_template`,
  `edit_mesh`— en `ok`) no hace nada. **Bucle de respaldo**: si el render falló, `AutoSaveOutcome.Feedback`
  (el error con su línea) vuelve al modelo como mensaje de usuario (`msgAutoFix`) y se da otra vuelta, mientras quepan sus dos
  pasos en `MaxSteps`; la corrección cuelga del que falló. El aviso de «sin herramientas» ahora dice que ScadLab guardará el bloque.
- **CLIs** (`cliSafetyNet`, en `runCLITurn` tras `rec.finish`): **un solo intento**, sin segunda vuelta (el CLI tiene su sesión). No
  actúa si hay un paso `mcp.CreatesCheckpoint(tool)` o `autosave` en `ok`, ni si apareció un `.scad` que no estaba al empezar el
  turno (Claude con `Write`, Codex editando): `workspaceScadSet` lo compara. Los pasos van por `chat:step` con `workspaceId`.
- **Turno vacío**: `cliTurnText` reemplaza una respuesta sin texto ni checkpoint por una explicación (qué herramienta falló y con qué
  detalle, y por qué no se creó nada); con `mcpNote` (el MCP apagado, Antigravity sin registrar o desactivado) la repite también
  tras un texto sin checkpoint.
- **Antigravity** (`antigravityAdapter`, ahora con estado por turno): una herramienta que falla lleva el **detalle real** del paso
  (`error`, `output`… y, si el campo es nuevo, lo que traiga) en vez de «Sin resultado», y un `result.response` sin trozos previos
  sale como texto. `agyEvents` y los tests con `antigravity-error.jsonl` / `antigravity-response-only.jsonl` documentan la forma;
  el NDJSON real de un error **no está documentado** y se lee por nombres probables.
- **Modo y permisos** (`backend/clichat`): «Agente» por defecto, y **por conversación** (ver la sección de T12 abajo). En «Agente», Claude Code ya traía
  `--allowedTools mcp__scadlab__<todas>`; **Codex** pasa `mcp_servers.scadlab.enabled_tools=[todas]` y **OpenCode**
  `permission.scadlab_<herramienta>="allow"` (en consulta, `deny` a las de escritura); **Antigravity** aprueba las de ScadLab con reglas
  `mcp(scadlab/…)` en su `settings.json` global (sección «Antigravity: aprobar las herramientas…» abajo: `--mode accept-edits` solo cubre ediciones de
  archivos, no las herramientas MCP) y el modo lo hace cumplir el servidor `--mcp` y `--dangerously-skip-permissions` no se pasa nunca. La shell sigue denegada en todos.
  El prompt de turno (`cliPrompt`) ordena en «Agente» *crear → renderizar → mirar → corregir en uno nuevo* y, sin checkpoints,
  crear el primero sin preguntar; en «Solo consulta» pide **un** bloque completo con la cabecera.
- **Sin verificar con sesiones reales**: que Codex no pide aprobación por herramienta MCP en `exec` (se prueba con un CLI falso, argv y
  NDJSON, no con una corrida). Lo de Antigravity —que `accept-edits` **no** aprueba las MCP— sí se midió: ver la sección siguiente.

## Antigravity: aprobar las herramientas de ScadLab en `--print` (S12.c, T14)

Detalle completo en [26-antigravity-permisos.md](26-antigravity-permisos.md). En `agy --print` Antigravity rechaza la primera herramienta
MCP sin aprobar y **termina el turno** (el chat decía «terminó el turno sin responder nada»); `--mode accept-edits` solo cubre ediciones, y los
hooks **no sirven** (medido: el permiso se pide antes de ejecutarlos). ScadLab agrega al `settings.json` global de Antigravity
(`~/.gemini/antigravity-cli/`) una regla `mcp(scadlab/<herramienta>)` en `permissions.allow` **por cada herramienta de su catálogo** (lectura y
escritura; lista explícita, sin comodines), combinada sin tocar el resto del archivo, con copia previa y sincronizada al registrar el MCP y en
cada turno. Como la regla es global, **«Solo consulta» lo hace cumplir el servidor `--mcp`** con una marca en el workspace durante el turno
(`.scadlab/turn-mode`). Nunca `--dangerously-skip-permissions`, la shell sigue denegada.

## Modo por conversación y escritura por defecto (S12.c, T12)

Visión del usuario: «por defecto debe ser escritura, ya que el chiste de esto es modificar y crear modelos». T9 dejó el modo en
Agente pero lo recordaba en `localStorage` **por tipo de proveedor**: elegir «Solo consulta» en una conversación lo volvía el modo de
todas las de ese tipo. Ahora es de cada conversación.

- **Dónde vive.** `chats.mode` (migración 9, ver [07-database.md](07-database.md)): `''` = «Agente» (el predeterminado, de toda fila
  anterior y de toda conversación nueva) y `'ask'` = «Solo consulta», elegido a mano **para esa conversación**. `store.Chat.Mode`
  viaja en `ListChats`, `GetChat` y `NewChat`; `Chat.Agent()` es `Mode != "ask"`.
- **`SetChatMode(chatId, mode)`** (`"agent"` | `"ask"`): guarda el modo de esa conversación y de ninguna otra; no deja un predeterminado
  ni toca `updated_at`. Un turno en curso termina con el modo con que empezó.
- **`SendChatMessage` usa el modo de la conversación.** `SendOptions.Agent` pasó de `bool` a `*bool` y es un **override explícito solo de
  ese turno**: `nil` (lo normal) = el modo guardado; `true`/`false` lo fuerza sin cambiar lo guardado (`SendOptions.agentFor`). Se
  conserva porque hay un caso real: un modelo que informó no llamar herramientas no puede correr en «Agente» aunque la conversación lo
  esté, y la interfaz (que en ese caso deshabilita el interruptor y ofrece «Crear checkpoint y renderizar») manda `agent:false` solo
  para ese turno. Un `bool` sin más habría convertido el «no mandé nada» de los clientes viejos en «Solo consulta».
- **Hermes (servidores).** El modo Agente es el interruptor de herramientas: una conversación nueva viaja con las herramientas
  declaradas y el prompt de Hermes; en consulta, sin ellas y con el prompt de texto de S5. Prueba: `TestHermesSendUsesTheConversationMode`
  (pedido 0 con `tools`, el de la conversación en consulta sin ellas, la otra conversación con ellas, y los dos overrides).
- **CLIs.** El argv sale de `clichat.ModeFor(agent)` con el modo de la conversación. Prueba: `TestCLISendUsesTheConversationMode` (CLI
  falso: `--permission-mode acceptEdits` y `create_checkpoint` en `--allowedTools` vs. `--disallowedTools Edit…` y solo lectura).
- **Frontend** (`ChatPanel.tsx`, `lib/chatMode.ts`): el interruptor lee `current.mode` del chat abierto; sin chat abierto es un borrador
  (`draftAgent`) que **siempre arranca en Agente** y se reinicia con «Chat nuevo», con «Modificar con IA» y al cambiar de proyecto. Si
  en el panel vacío se elige consulta, se guarda en la conversación recién creada (`NewChat` → `SetChatMode`) antes de enviar. Cambiar
  el interruptor en un chat abierto se refleja ya en la lista y llama a `SetChatMode` (un envío inmediato espera ese guardado; si falla,
  se relee la lista). El aviso «Cambiar a Agente» pasa por el mismo camino, así que queda guardado en esa conversación.
- **Migración de `localStorage`.** `main.tsx` llama una vez a `clearLegacyChatMode()`, que borra `scadlab.chat.agent`,
  `scadlab.chat.agent.cli` (S12/S12.b) y `scadlab.chat.mode.cli|server` (T9). **No se migran a ninguna conversación**: las de S12.b no
  se distinguían de un default y las de T9 eran por tipo, no por conversación; migrarlas habría dejado en consulta chats que nadie eligió
  así. Toda conversación existente con la columna vacía queda en Agente. Prueba: `cd frontend && pnpm test` (`chat-ux.test.mjs`).

### La terminal de agentes y `--mcp` no arrancan en solo lectura (T12)

**Terminal** (`app_terminal.go`, `agentLaunchArgs`). Revisado: el argv que arma ScadLab son solo tres grupos —el MCP (`--mcp-config` en
Claude, `-c mcp_servers.scadlab.*` en Codex), `--add-dir` con los otros proyectos abiertos y los de la sesión (`--resume`, `-c`,
`resume --last`…)—. **Ningún CLI arranca restringido por un flag de ScadLab, y no se agregó ninguno**; en particular no hay
`--dangerously-skip-permissions`, `--dangerously-bypass-approvals-and-sandbox` ni equivalentes: en la terminal la persona aprueba en su
propia terminal. Cada CLI arranca en su modo habitual:

| CLI | Qué le pasa ScadLab | Cómo queda |
|---|---|---|
| Claude Code | `--mcp-config <json>` + `--add-dir` + sesión; sin `--permission-mode`, `--allowedTools` ni `--disallowedTools` | modo de permisos habitual de Claude Code: puede editar y crear, pidiendo aprobación en la terminal (incluida cada herramienta de ScadLab la primera vez) |
| Codex | `-c mcp_servers.scadlab.command/args` + `--add-dir` + sesión; sin `--sandbox` ni `enabled_tools` | sandbox y aprobaciones de su propia configuración; ve **todas** las herramientas de ScadLab (`enabled_tools` solo lo pone el chat) |
| Antigravity | `--add-dir` + sesión; sin `--mode`; el MCP está en su configuración global (`agy mcp add`, ScadLab lo registra al abrirlo) | modo habitual de `agy`, no `plan` |
| OpenCode | solo la sesión; el MCP va en `<workspace>/opencode.json` | agente por defecto (`build`), no `plan` |

La prueba `TestAgentTerminalArgsAreNeverReadOnlyNorBypassPermissions` recorre los cuatro CLIs, con el MCP encendido y apagado y en los
modos nuevo/continuar, y falla si aparece cualquiera de esos flags. Los modos de «Solo consulta» y «Agente» de arriba son **solo del
chat**: la terminal no tiene modo.

**`--mcp`** (`main_mcp.go`, `backend/mcp`). Su único flag es `--workspace`; no hay `--read-only`, ni variable de entorno, ni setting que
oculte herramientas. Sin nada más declara las de escritura (`create_checkpoint`, `import_mesh`, `export`…) junto con las de lectura:
`TestMCPServerIsWritableByDefault` lo comprueba por `tools/list` y que `--read-only` sea un flag desconocido. «Solo consulta» no es un
modo del servidor: lo implementa el chat al armar la lista de herramientas permitidas de cada CLI (`backend/clichat`).

## Terminal (12.7)

El `^[[?1;2c` de Antigravity era la respuesta de xterm a la consulta DA (`ESC [ c`) que el
CLI hizo al arrancar: al reenganchar, `ReadTerminalBuffer` reescribía la consulta y xterm
la contestaba otra vez. `TerminalView` cuenta las escrituras de **reproducción** (la
captura y los eventos previos a `goLive`) y, mientras alguna siga sin procesar (se baja en
el callback de `x.write`), descarta de `onData` las respuestas de consulta
(`components/terminal/queryReplies.ts`: DA1/DA2, CPR, DSR, OSC 10/11/12, DECRPM,
XTVERSION). Prueba: `cd frontend && pnpm test`.
**Compromiso**: las consultas que el agente hizo antes de que la vista se enganchara por
primera vez tampoco se contestan; los CLIs las resuelven por tiempo de espera.

## Lo que no se portó de mini-tools

Notas, aprobación de herramientas peligrosas
(las de ScadLab solo escriben checkpoints nuevos y `exports/`), administración de Ollama,
cuotas de CLIs, PDF y texto pegado como adjuntos.

## Regla de sincronización

Una herramienta nueva en `backend/mcp` llega sola a Hermes; solo su etiqueta y su ícono
(`steps.tools` en `es.ts`/`en.ts`, `ICONS` de `ToolCallCard.tsx`) se agregan a mano — sin
ellos se muestra el nombre crudo. Si es esencial para un modelo chico, además entra en `mcp.EssentialTools` con su descripción corta. Un campo nuevo de `hermes.Step`/`Usage` se agrega en
`types.go`, `lib/chatTypes.ts` y `chatStreams.ts` en el mismo cambio.
