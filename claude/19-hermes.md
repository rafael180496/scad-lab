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
backend/chat       Manager/Turn: un turno por chat, cancelación, trozos de texto
app_servers.go     bindings de servidores y modelos (+ chequeo previo cacheado)
app_chat.go        SendChatMessage y el turno; app_chat_attach.go: adjuntos
```

## El bucle (`backend/hermes/run.go`)

Es el `llmrunner.go` de mini-tools con las herramientas de ScadLab:

- **Un error de herramienta vuelve al modelo como `error: …`**, nunca corta el turno:
  herramienta inexistente, argumentos que no son JSON (`_raw`), pánico, render con
  error de línea. El resultado se recorta a 32 000 caracteres para el modelo y a 4000
  para la tarjeta; los resultados viejos se reducen a una nota al pasar de 160 000
  caracteres (`compactToolResults`; los 4 últimos quedan completos).
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

El prompt de sistema es `prompts.AgentSystemPrompt`: con herramientas, el de Hermes
(bucle, tope, skills del workspace —nombre y descripción, se leen con `read_skill`—,
referencias adjuntas, si el modelo ve); sin ellas, el de S5.

## Adjuntos (`backend/hermes/attachments.go`, `app_chat_attach.go`)

| Tipo | Límite | Qué pasa |
|---|---|---|
| Imagen PNG/JPG/GIF/WebP | 5 MB | se guarda en `<datos>/chat-attachments/staging/`; al enviar pasa a `<chatId>/` y viaja como imagen |
| Modelo STL / 3MF / OBJ | 64 MB | se copia a `<ws>/refs/` (`refmodel.CopyToRefs`), se analiza y su informe (`PromptText`) entra al mensaje; la vista previa viaja como imagen solo si el modelo ve |

- Máximo **10** por mensaje. El tipo se decide **por el contenido** (un PNG llamado
  `.stl` es una imagen); no hay PDF ni texto pegado.
- Los pendientes viven en un registro en memoria (`hermes.Registry`) con vencimiento de
  24 h. Quitar un chip borra lo que se creó (la copia en `refs/` solo si la hizo ese
  adjunto); al cerrar la app se descartan los pendientes.
- Imagen con un modelo que **informó** que no ve → error **antes** de guardar nada
  (`hermes.CheckVision`). Un servidor que no lo informa (`visionKnown=false`) nunca bloquea.
- El historial guarda ficha, miniatura de 96 px y informe, nunca los bytes.

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

`ChatPanel` orquesta; `Composer` (adjuntar con el clip, pegar o arrastrar sobre el panel,
interruptor **Agente** y tope de pasos, Detener con reloj), `Messages` (texto con
`ToolCallCard` intercaladas y tokens/costo al pie), `ChatHeader` (conversaciones con
renombrar en línea y borrar con `confirmDialog({danger:true})`; servidor y modelo también
en un chat abierto → `SetChatModel`), `useChatAttachments` y `AttachmentChips`.
El interruptor Agente se apaga y lo explica si `toolsKnown && !tools`; Enviar se
deshabilita solo con `visionKnown && !vision` y una imagen. Un paso fallido de `render`
muestra la línea y el código ofensivo en rojo (`parseDiagnostics`).

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
ellos se muestra el nombre crudo. Un campo nuevo de `hermes.Step`/`Usage` se agrega en
`types.go`, `lib/chatTypes.ts` y `chatStreams.ts` en el mismo cambio.
