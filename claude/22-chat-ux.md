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
| `ProviderPicker.tsx` · `ModelPicker.tsx` · `ModeControl.tsx` | los tres selectores propios (sobre `Popover.tsx`) |
| `ConversationsMenu.tsx` · `useChatSearch.ts` | lista de conversaciones con búsqueda |
| `Composer.tsx` · `ContextMeter.tsx` | caja de mensaje, atajos, contador de contexto |
| `EmptyChat.tsx` | estado vacío con sugerencias |
| `ToolCallCard.tsx` · `CodeBlock.tsx` · `Markdown.tsx` | pasos, bloques de código, Markdown |
| `useChatProviders.ts` · `lib/chatHealth.ts` · `lib/chatTier.ts` | proveedores, salud, escalones de ancho |
| `lib/chatContext.ts` · `lib/chatSearch.ts` · `lib/chatProviders.ts` | lo puro (probado con `pnpm test`) |

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

## Modos

`ModeControl` es «Solo consulta» (`agent=false`) / «Agente» (`agent=true`) para los dos tipos, con la
explicación de cada uno según el tipo (`t.chat.mode.cli|server`). Se recuerda **por tipo**
(`rememberedAgent(cli)`): con un CLI arranca en «Solo consulta» y con un servidor en «Agente». El tope de
pasos solo aplica a Hermes (servidor + Agente). Si el modelo no llama herramientas, «Agente» se
deshabilita y dice por qué.

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
  la conversación, contra `ModelInfo.contextTokens` si se conoce (ámbar al 70 %, rojo al 90 %; sin ventana,
  solo el número). Es una estimación y el `title` lo dice: no cuenta el prompt de sistema ni los adjuntos.
- **Estado vacío** (`EmptyChat`): proveedor, modelo y modo activos y cuatro sugerencias que **rellenan** la
  caja (no envían). Con menos de 380 px de alto se aprieta (sin ícono ni texto largo): el panel suele ser bajo
  y la lista de mensajes no se pega al fondo cuando está vacía, para no cortar el título.

## Pasos, código y Markdown

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

Un CLI nuevo en `backend/clichat` suma su ícono en `providerIcons.ts`. Un modo o un texto de modo nuevo, en
`t.chat.mode` (los dos tipos). Una herramienta nueva de ScadLab, su etiqueta (`steps.tools`) y su ícono
(`ICONS` de `ToolCallCard.tsx`). Un ancho nuevo en la barra se prueba con `uishot-fit.sh`, no a ojo.
