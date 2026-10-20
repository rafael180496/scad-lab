# UX del panel del chat (S12.b, 12.13)

El panel **Chat** del dock con CLIs y servidores como proveedores. Spec:
[.claude/specs/s12b-cli-chat-ux.md](../.claude/specs/s12b-cli-chat-ux.md); el backend de los
CLIs, en [19-hermes.md](19-hermes.md). La UX es la del chat de mini-tools
(`AgentChat`, `HealthIndicators`, `agentIcon`) con lucide y los tokens de ScadLab: no se
portó su código, se portó lo que hace.

## Mapa de archivos (`components/chat/` y `lib/`)

| Archivo | Qué hace |
|---|---|
| `ChatPanel.tsx` | orquesta: chat abierto, proveedor/modelo/modo, enviar, «Abrir en el editor» |
| `ChatHeader.tsx` | barra de **una línea**: título, proveedor, píldora, modelo, chip del workspace, modo, «…» |
| `ProviderPicker.tsx` · `ModelPicker.tsx` · `ModeControl.tsx` · `ToolsControl.tsx` | los cuatro selectores propios (sobre `Popover.tsx`); el último es el perfil de herramientas (S17) |
| `ConversationsMenu.tsx` · `useChatSearch.ts` | lista de conversaciones con búsqueda |
| `Composer.tsx` · `ContextMeter.tsx` · `ContextNotice.tsx` | caja de mensaje, atajos, contador de contexto y el aviso sobre la caja cuando el contexto se acaba (S17) |
| `EmptyChat.tsx` | estado vacío con sugerencias |
| `ToolCallCard.tsx` · `CodeBlock.tsx` · `Markdown.tsx` | pasos, bloques de código, Markdown |
| `useChatProviders.ts` · `lib/chatHealth.ts` · `lib/chatTier.ts` | proveedores, salud, escalones de ancho |
| `lib/chatContext.ts` · `lib/chatSearch.ts` · `lib/chatProviders.ts` | lo puro (probado con `pnpm test`); `chatContext.ts` también tiene el pie del turno (`usagePieces`, `formatDuration`) y los avisos (`contextAdvice`) |

## Proveedor, salud y modelo

- **`useChatProviders`** junta `ListChatProviders` (CLIs instalados + servidores habilitados) con
  `ListModelServers` (el modelo predeterminado y la dirección, que `ChatProvider` no trae). Abrir el
  selector llama `RefreshChatProviders`: un CLI recién instalado aparece sin reiniciar. Los CLIs van
  primero (`sortProviders`).
- **Salud** (`lib/chatHealth.ts`, el `modelServerHealth` de mini-tools): un almacén por proveedor, un
  sondeo para quien lo mire; 60 s mientras la ventana tiene foco y al cambiar de proveedor. Un CLI
  responde «instalado» (latencia 0), así que **la píldora solo se dibuja para servidores**: para un CLI
  ya lo dice el punto verde. Un servidor caído no es un error de la llamada: llega `ok:false` y sale el
  `HealthBanner` sobre la caja (no bloquea escribir).
- **Modelo `''` = «Por defecto».** Con un CLI es válido (usa el que el CLI tenga configurado; también se
  recuerda como último modelo). Con un servidor el backend lo resuelve con su modelo predeterminado; si
  no tiene, `ChatPanel.resolveModel` pone el primero de la lista —es lo que dice el `defaultHint`—.
  El catálogo sale de `ChatModelCatalog` (`useProviderModels`); las insignias de visión/herramientas
  solo se pintan en servidores (todos los modelos de un CLI usan herramientas: sería ruido).
- **Cambiar de proveedor en un chat abierto** se aplica ya (`SetChatModel`) si el nuevo tiene un modelo
  que se pueda resolver solo (un CLI, o un servidor con predeterminado o con modelo recordado); si no, el
  chat sigue como estaba hasta que se elige el modelo (`switching`).
- **Último proveedor y modelo** por proyecto: `GetLastModel`/`SetLastModel` (+ `localStorage` de respaldo).

## Perfil de herramientas (S17)

`ToolsControl` es el selector compacto de **qué herramientas de ScadLab declara el chat** al modelo: «Auto (esenciales)» /
«Auto (todas)» (el rótulo dice en qué se resolvió el último turno), «Esenciales» y «Todas». Es de **cada conversación**
(`chats.tools`, `Chat.tools`, `SetChatToolProfile`); sin chat abierto es un borrador (`draftTools`) que se guarda al crearla.
Solo con servidores **y** en «Agente» (sin herramientas no hay nada que declarar); un CLI no lo tiene. En la barra va
con rótulo en `wide`, solo ícono en `mid` y **en el menú «…»** en `narrow` y `tiny` (a 480–719 px la barra ya va justa: lo
encontró `uishot-fit.sh` con un 498 > 490 en el dock de 1024 px). Detalle de qué hace cada perfil:
[35-edicion-compacta.md](35-edicion-compacta.md).

## Modos

`ModeControl` es «Solo consulta» (`agent=false`) / «Agente» (`agent=true`) para los dos tipos, con la
explicación de cada uno según el tipo (`t.chat.mode.cli|server`). **El modo es de cada conversación** (T12): vive en `chats.mode`
(`Chat.mode`, `SetChatMode`) y **arranca en «Agente» con todo proveedor** (S12.c, «el chiste de esto es modificar y crear modelos»).
Elegir «Solo consulta» vale solo para esa conversación: no cambia otra ni lo que arranca el chat nuevo. Sin chat abierto, el
interruptor es un borrador (`draftAgent`, siempre «Agente» al abrir el panel, con «Chat nuevo» y al cambiar de proyecto) que se guarda
en la conversación al crearla. `lib/chatMode.ts` tiene lo puro (`isAgentMode`, `chatModeValue`) y `clearLegacyChatMode()`, que `main.tsx`
corre al arrancar para borrar las preferencias globales de antes (`scadlab.chat.agent`, `scadlab.chat.agent.cli`,
`scadlab.chat.mode.cli|server`): ninguna deja en consulta una conversación que nadie eligió así. Ver
[19-hermes.md](19-hermes.md#modo-por-conversación-y-escritura-por-defecto-s12c-t12). El tope de pasos solo aplica a Hermes
(servidor + Agente). Si el modelo no llama herramientas, «Agente» se deshabilita y dice por qué.

### «Solo consulta» y lo que nace en el chat (S12.c)

- **«Crear checkpoint y renderizar»** (`CodeBlock`, `actions.onCreate`): solo se pasa en «Solo consulta» (en «Agente» lo hace
  ScadLab con el guardado automático). Botón de acento que reemplaza a «Abrir en el editor»; «Guardar como checkpoint» pasa a
  secundario. Guarda sin diálogo (`SaveChatCheckpoint`, padre `""` = raíz) y llama a `onCheckpointCreated`.
- **Aviso «Cambiar a Agente»** (`SwitchAgentNotice`): sobre la caja, cuando `!effectiveAgent`, el modelo admite herramientas y
  `asksToCreate` (`lib/chatIntent.ts`, heurística de palabras en es/en; una pregunta —«¿cómo…?»— no cuenta) ve el borrador o,
  con la caja vacía, el último mensaje enviado. La X lo oculta para ese texto.
- **Un bloque ya guardado no se ofrece otra vez**: si el turno tiene un paso que creó un checkpoint (`createdCheckpoint`,
  `lib/chatSteps.ts`: `CREATING_TOOLS` = `create_checkpoint`, `import_mesh`, `set_parameters`, `create_from_template`, `edit_mesh` y
  `autosave`, en `ok`; es el espejo de `EffectCheckpoint` en `backend/mcp/effects.go` y `chat-ux.test.mjs` lo cruza con ese
  archivo. Para `set_parameters` y `edit_mesh` el argumento `file` es la **base**, no el checkpoint nuevo: solo cuenta el `file`
  del resultado), `Turn` (`Messages.tsx`) no pasa acciones a sus
  bloques de código.
- **Selección automática**: `chatStreams.ts` avisa (`onChatCheckpoint`) cada vez que un `chat:step` termina bien y creó un
  checkpoint; `App.tsx` lo abre en el visor y en Código (`adoptChatCheckpoint` = `adoptNewCheckpoint` + render aunque el
  auto-render esté apagado) si es el proyecto activo y no hay cambios sin guardar ni una edición de STL sucia. El botón y los
  pasos usan el mismo camino (`onCheckpointCreated` de `ChatPanel` → `PanelHost.onChatCheckpoint`).
- **Antigravity**: `ChatPanel` llama `EnsureAntigravityMCP` al mostrar `cli:antigravity`; el toast sale del evento
  `mcp:antigravity` en `App.tsx` (también cuando lo dispara el backend); si Antigravity bloquea una herramienta, el evento `mcp:antigravity-denied` abre un
  aviso con «Reparar permisos de Antigravity». Ver [13-mcp.md](13-mcp.md) y [26-antigravity-permisos.md](26-antigravity-permisos.md).
- **«Crear un modelo de inicio»** (`components/workspace/StarterAction.tsx`): en el visor y en Código cuando el proyecto no
  tiene ningún checkpoint (`emptyWorkspace`); `CreateStarterCheckpoint` lo escribe aunque la papelera tenga algo.

## Barra de una línea y escalones de ancho

El panel mide **su propio ancho** (`useChatTier`, `ResizeObserver`, dividido por la escala de la interfaz) y
la barra se pliega por escalones (`lib/chatTier.ts`):

| Escalón | Ancho | Qué cambia |
|---|---|---|
| wide | ≥ 900 | todo a la vista, con el chip del workspace |
| mid | ≥ 720 | el chip del workspace y «Chat nuevo» pasan al menú «…» |
| narrow | ≥ 480 | sin píldora (queda el punto) y el modo solo ícono |
| tiny | < 480 | proveedor y modelo solo ícono, el modo al menú, sin contador de conversaciones |

El título tiene un ancho mínimo y los selectores se acortan antes que él; los mínimos suman menos que el
límite de cada escalón (si cambias un ancho de `ChatHeader`, corre `uishot-fit.sh`: falla justo en el
límite de abajo). El menú «…» es el `Menu` compartido. Los desplegables van en `Popover.tsx` (portal,
posición fija, acotado a la ventana, se recoloca si el ancla se mueve): un `absolute` dentro del dock
quedaba cortado por su `overflow-clip`.

## Caja de mensaje

- **Enter envía, Mayús+Enter agrega una línea y ⌘/Ctrl+Enter también envía.** No se invirtió porque el chat
  de mini-tools (la referencia) y el de S12 ya eran así y un mensaje de diseño suele ser una frase.
  Los atajos se escriben bajo la caja (`↵` / `Enter`, según la plataforma).
- **Contador de contexto** (`lib/chatContext.ts`): `ceil(caracteres / 4)` del borrador más cada mensaje de
  la conversación, contra la ventana (ámbar al 70 %, rojo al 90 %; sin ventana, solo el número). Es una
  estimación y el `title` lo dice. **Desde S17** la ventana es la **efectiva** del último turno con un servidor
  (`chat:context.numCtx`: lo que se le pide a Ollama; antes del primer turno, la del modelo) y suma lo fijo de
  cada vuelta que ese evento informó —prompt de sistema y herramientas declaradas, `promptTokens + toolsTokens`—:
  con un modelo de 8k eso es lo que decide si hay sitio. Sigue sin contar los adjuntos.
- **Aviso sobre la caja** (`ContextNotice`, `contextAdvice`): con un servidor y la ventana conocida, un solo aviso a
  la vez. **La conversación ocupa el 70 % o más** → «Nueva conversación con resumen» (`NewChatWithSummary`: el modelo
  resume y se sigue en una conversación nueva; la actual se conserva) — pesa más porque ya no hay sitio. **El prompt
  y las herramientas solos pasan del 60 %** (`OverBudget`) → lo dice y, si no se usan ya, ofrece «Usar esenciales»
  (`SetChatToolProfile`). La X lo oculta para esa conversación y ese tipo de aviso.
- **Estado vacío** (`EmptyChat`): proveedor, modelo y modo activos y cuatro sugerencias que **rellenan** la
  caja (no envían). Con menos de 380 px de alto se aprieta (sin ícono ni texto largo): el panel suele ser bajo
  y la lista de mensajes no se pega al fondo cuando está vacía, para no cortar el título.

## Pasos, código y Markdown

- **Tiempo y tokens** (S17). La línea de cada tarjeta suma, a la duración, `ctx 7,2k` (el contexto de la vuelta que pidió el paso: lo
  que el modelo tenía delante) y `+880` (lo que la herramienta devolvió al modelo, estimado); los dos se esconden en una tarjeta
  angosta (`@min-[24rem]`) y están completos en el detalle desplegado (`StepStats`: tiempo, contexto, salida de esa vuelta y
  resultado en tokens y caracteres). Lo que el proveedor no informó (0) no se muestra. **El pie del turno** (`UsageLine` en `Messages`)
  dice «Contexto máx. 12k · ↑ 48k en 5 vueltas · ↓ 2k · modelo 41 s · herramientas 6 s»: ↑ es la **suma** de la entrada de todas las
  vueltas, no el contexto —cada vuelta reenvía todo—, y su `title` lo explica; con `≈` cuando el servidor no informó la entrada y es
  una estimación. Cómo se calcula con cada proveedor: [35-edicion-compacta.md](35-edicion-compacta.md).
- **`ToolCallCard`** muestra sin abrir: el checkpoint (un enlace → `onCheckpointCreated`, que lo selecciona
  en el árbol y el editor y el visor lo siguen), los datos de la malla de `render`/`measure` (lee el JSON
  real de `backend/mcp`: `mesh.sizeMm {x,y,z}`, `triangles`, `watertight`, `fitsBuildVolume`, `estimatedWeightG`),
  los diagnósticos con su línea y la miniatura del `preview`. El detalle (argumentos y respuesta cruda) arranca
  plegado. Un CLI llega por el mismo `chat:step`: misma tarjeta.
- **«Abrir en el editor»** guarda el bloque como checkpoint nuevo **sin diálogo** (nombre de su cabecera
  `desc=`), lo selecciona y muestra el panel Código. No hay un camino a un borrador suelto: el editor siempre
  muestra un checkpoint y los checkpoints son inmutables. «Guardar como checkpoint» sigue abriendo el diálogo.
- **Markdown** (`markdownParse.ts`, propio, nunca HTML): además de lo de S5, tablas con alineación
  (`:--`/`:-:`/`--:`, también con un solo guion), casillas `[x]`/`[ ]`, `~~tachado~~` y direcciones http(s)
  sueltas; un enlace nunca lleva otro adentro y solo http(s) abre el navegador del sistema.

## Búsqueda en las conversaciones

No hay un binding de búsqueda: `useChatSearch` filtra el título y, al escribir 2 letras o más, lee con
`GetChat` el contenido de las 60 conversaciones más recientes (3 a la vez, en memoria por `updatedAt`). Abrir la
lista sin buscar no cuesta nada. Si un día pesa, el camino es un `SearchChats` en Go (un `LIKE` sobre
`chat_messages`), no más caché aquí.

## Banco de capturas y pruebas

- Vistas de S12.c: `chat-agent-creates` (workspace con un solo `001`, Claude Code con `create_checkpoint → render → preview` y la
  pieza en el visor), `chat-autosave` (Ollama sin herramientas: el bloque y «Guardado automáticamente»), `chat-query-only`
  (Codex en consulta con el botón destacado y el aviso) y `workspace-starter` (el modelo de inicio ya en el visor, con
  `public/uishot/starter-{all,base,knob}.stl`, generados una vez con la geometría de `starter.scad.*.tmpl`). `chat-code` y `chat-query-only` ponen la
  conversación abierta en `mode: 'ask'` (`hermesChatMode`; el banco ya no toca `localStorage`) y el chat 1 de la lista de ejemplo también
  está en consulta, para que se vea que el modo es de cada conversación.
- Vistas de S17: `chat-turn-stats` (un Ollama de 30b: cuatro tarjetas con su tiempo y tokens y el pie del turno), `chat-tools` (un Ollama de 8k,
  con «Esenciales» elegido y el selector abierto) y `chat-context` (el mismo, al 75 % de la ventana, con el aviso «Nueva conversación con resumen»).
  `chat-sw<N>` es el chat solo (como `chat-w<N>`) pero con un servidor, para que `uishot-fit.sh` recorra el selector de herramientas y el aviso de 320 a 1200 px.
- Vistas nuevas en `uishot.tsx`: `chat-providers`, `chat-cli-models`, `chat-empty`, `chat-no-providers`,
  `chat-code`, `settings-import-minitools`, y el chat solo en un panel de N px: `chat-narrow` (360) y
  `chat-wN` (cualquier N). El ancho se fija en el contenedor: Chrome headless no baja de ~500 px de ventana.
- `UISHOT_CLICK='[data-testid="chat-more"]' ./scripts/uishot.sh chat-w480` hace clic antes de fotografiar.
- **`uishot.sh` fija el alto de la página** (`vh=<alto>`): Chrome headless da la página con 87 px menos de alto
  mientras corre y la agranda justo al fotografiar, y el dock se reacomoda en ese instante; un desplegable ya
  abierto (`position: fixed`, medido antes) quedaba unos píxeles más arriba que su botón. Las vistas con un
  desplegable abierto esperan además a que pase el reacomodo (5 s virtuales) y `help-shots.sh` les da 9 s de presupuesto.
- Un `import` estático de `ChatPanel` en `uishot.tsx` adelantaba su montaje y cambiaba cómo termina de repartirse el
  dock en las demás vistas: el banco lo carga con `lazy()`, como `PanelHost`.
- `uishot-fit.sh` prueba 320, 360, 479, 480, 720, 899, 900 y 1200: la barra, la lista de mensajes y el panel
  no pueden tener contenido más ancho que su caja, y con `poke=1` abre proveedor, modelo, modo, «…» y
  conversaciones: ningún desplegable puede salirse de la ventana.
- `cd frontend && pnpm test` incluye `scripts/chat-ux.test.mjs` (contexto, orden de proveedores, Markdown,
  búsqueda y escalones).

## Regla de sincronización

Un perfil de herramientas nuevo se suma en `mcp.ToolProfile`, en `store.ChatToolsValue`, en `TOOL_PROFILES` de `ToolsControl.tsx` y en `t.chat.tools`. Un campo nuevo del uso de un turno, en `hermes.Usage`, `ChatUsageEvent`, `lib/chatTypes.ts` y `usagePieces`. Un CLI nuevo en `backend/clichat` suma su ícono en `providerIcons.ts`. Un modo o un texto de modo nuevo, en
`t.chat.mode` (los dos tipos). Una herramienta nueva de ScadLab, su etiqueta (`steps.tools`) y su ícono
(`ICONS` de `ToolCallCard.tsx`). Un ancho nuevo en la barra se prueba con `uishot-fit.sh`, no a ojo.
