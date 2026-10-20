# Edición compacta y modelos chicos (S17)

Spec: [.claude/specs/s17-edicion-compacta-modelos-chicos.md](../.claude/specs/s17-edicion-compacta-modelos-chicos.md). Este segmento cubre lo
de **17.3** (Hermes con modelos chicos, tiempo y tokens, Antigravity 1.4.x, interfaz); las herramientas nuevas de **17.1** están en
[25-herramientas-ia.md](25-herramientas-ia.md) y su servidor en [13-mcp.md](13-mcp.md).

## El problema, medido

Una sesión real de Antigravity (Gemini Flash) sobre una maceta de ~600 líneas: **3 min 39 s y 1 min 42 s por cambio** (el modelo regeneraba las 600 líneas con
`update_checkpoint`), **105k–192k tokens de contexto por paso**, tarjetas que decían `call_mcp_tool`, el chat mostrando «↑694k» por turno (la **suma** de todas las vueltas,
que se leía como si fuera el contexto) y turnos que terminaban sin texto. Y un Ollama chico de 8k ni siquiera puede empezar: el catálogo completo de herramientas son
~11 000 tokens y el prompt de Hermes ~6 300, antes de la conversación (tabla de medidas abajo).

## Las herramientas, en resumen

`edit_checkpoint` (reemplaza solo las líneas que cambian, todo o nada), `outline_checkpoint` (el mapa del archivo), `find_in_checkpoint` (búsqueda con líneas numeradas),
`read_checkpoint` con `from`/`to`, `set_parameters` en el lugar, `extract_module` y salidas compactas. Detalle, contratos y la tabla de bytes: [25](25-herramientas-ia.md).
Aquí importa que un modelo chico las **declare todas por separado** y que cada resultado sea corto: eso es lo que hace posible el perfil esencial.

## Perfiles de herramientas (`chats.tools`)

Cada conversación con un servidor tiene un perfil: `''` **Auto**, `essential`, `full` (migración 11, [07](07-database.md); binding `SetChatToolProfile`).

- **Esenciales** = `mcp.EssentialTools()`: `outline_checkpoint`, `find_in_checkpoint`, `read_checkpoint`, `edit_checkpoint`, `set_parameters`, `get_parameters`, `render`, `preview`,
  `measure`, `create_checkpoint`, `list_checkpoints`, con `Tool.Short`/`ShortSchema` (1–2 frases, esquema mínimo). `Toolbox.ToolsFor(ProfileEssential)`.
- **Auto** (`resolveToolProfile`, `app_chat_context.go`): esenciales si la ventana efectiva es **< 32 768** tokens; el catálogo completo si es mayor **o no se sabe** (un servidor
  en la nube que no la informa).
- Con esenciales el prompt de sistema es el **compacto** (`prompts.Context.Compact`, `agent-compact.{es,en}.md`: las mismas reglas, sin el código del checkpoint abierto ni skills
  ni referencias; el modelo lee por partes).
- `hermes.Config.ToolSpecs` es lo que se **declara**; `Tools()` sigue siendo todo el catálogo, así que una llamada a una herramienta no declarada se ejecuta igual (`Call` acepta
  cualquiera): declarar menos no es prohibir. `app_chat_profile.go` lo aplica (`toolSpecsFor`, `setCompactPrompt`); una caja que no sabe ofrecer perfiles declara todo.
- Un CLI no usa el perfil (trae su catálogo y el servidor `--mcp` ofrece todo).

## La ventana de contexto y `num_ctx`

`effectiveWindow(servidor, capacidades)`:

| Servidor | Ventana efectiva |
|---|---|
| Ollama con `ContextTokens` fijado | esa (`Fixed`) |
| Ollama sin fijar | `min(contexto del modelo según /api/show, 32 768)`, mínimo **8 192**; si el modelo no la dice, `chatDefaultNumCtx` (16 384) |
| otros (OpenAI, Anthropic) | la fijada, la que informe el proveedor, o **0 = no se sabe** (rigen los topes fijos) |

Va al pedido como `options.num_ctx` **por petición** (`llm.ChatRequest.NumCtx`): el cliente se arma antes de saber el modelo, y Ollama sin esto recorta el comienzo del prompt **sin
avisar**. Antes era un `chatDefaultNumCtx = 16384` fijo para todos los modelos.

**`chat:context {chatId, numCtx, promptTokens, toolsTokens, profile, auto, overBudget}`** se emite al empezar cada turno con un servidor (también en modo texto: perfil `''`,
`toolsTokens` 0) y `GetChat.context` lo recuerda en la sesión. `promptTokens`/`toolsTokens` salen de `hermes.Config.Footprint()` (caracteres ÷ 4, la misma regla que la interfaz).
`overBudget` = prompt + herramientas > **60 %** de la ventana. La interfaz lo muestra como aviso y suma esa parte fija al contador ([22](22-chat-ux.md)).

## Dentro del turno (`backend/hermes/run.go`)

- **Presupuesto proporcional** (`contextBudget`): de la ventana se descuentan las herramientas declaradas y una reserva para la respuesta (1/6, mínimo 1 024) y queda el presupuesto de
  caracteres de los mensajes (techo: los 160 000 de siempre); cada resultado puede ocupar hasta 1/4 de eso (entre 3 000 y 32 000 caracteres). Con la ventana en 0, los topes fijos de antes.
- **Lectura repetida** (`dedupeRead`): si una herramienta **que no escribe** devuelve exactamente lo mismo que la última vez con los mismos argumentos, la copia vieja queda como
  `[archivo X leído, versión Y: sin cambios; usa la lectura más reciente, más abajo]` (la versión es el hash corto del contenido: si el archivo cambió, el hash cambia y se conservan las dos).
- **Apretar** (`compactToolResults`, antes de cada vuelta, por pasadas hasta que entra): resultados viejos a una nota dejando los últimos 4, luego 2, luego 1; después los **textos
  largos de los argumentos de llamadas viejas** (el código de un `update_checkpoint` ya está en el archivo) a «[texto de N caracteres omitido…]». Los mensajes con `Raw` (bloques firmados de
  Anthropic) no se tocan.

## Entre turnos y «Nueva conversación con resumen»

- `SeedHistory(stored, maxChars)`: arma el historial desde las filas guardadas; lo que no cabe se reduce a un **resumen extractivo** (sin llamar al modelo): por cada par, la primera frase de lo pedido
  y de lo contestado (los bloques de código se omiten: están en el checkpoint) y los archivos que los pasos tocaron; de lo más reciente a lo más viejo, ≤ 1/4 del presupuesto (600–2 400
  caracteres), y encabeza el primer mensaje conservado. Los últimos mensajes viajan **enteros**. `chat.HistoryBudgetFor(kind, ventana)` = un tercio de la ventana en caracteres
  (4 000 mínimo; techo 60 000 en Ollama y 200 000 en la nube).
- `NewChatWithSummary(chatId)` (`app_chat_context.go`): el modelo de la conversación resume en **una vuelta sin herramientas** (≤ ~300 tokens; la conversación entra por `SeedHistory`, así que cabe);
  si el servidor falla o devuelve vacío, el extractivo (`hermes.ExtractiveSummary`). La conversación nueva hereda proyecto, servidor, modelo, **modo y perfil**, se titula «<título> (continuación)» y
  su primer mensaje (rol `user`) es el resumen: se ve y, al enviar, se funde con el mensaje nuevo. La original queda intacta. Solo servidores. La interfaz la ofrece al 70 % de la ventana.

## Tiempo y tokens: cómo se lee ↑ y ↓

`hermes.Step` suma `roundInput`/`roundOutput` (los tokens de la vuelta del modelo **que pidió** el paso: `roundInput` es el contexto en ese momento) y `resultChars`/`resultTokens` (lo que de
la herramienta vuelve al modelo, estimado); `hermes.Usage` suma `modelMs`, `toolMs`, `rounds`, `peakInput`, `lastInput` e `inputEstimated`. `Parts.usage` los guarda con el mensaje.

| Número | Qué es |
|---|---|
| **Contexto máx.** (`peakInput`) | la vuelta con **más entrada**: la ocupación real de la ventana. Es lo que se compara con el límite |
| **↑** (`inputTokens`) | la **suma** de la entrada de todas las vueltas: cada vuelta reenvía el contexto entero, así que 5 vueltas con 10k suman ~50k. **No es el contexto** (el chat lo leía así y mostraba ↑694k) |
| **↓** (`outputTokens`) | lo que escribió el modelo, sumado |
| modelo / herramientas | tiempo esperando al modelo (`chatRetry` de cada vuelta) y tiempo ejecutando herramientas (la suma de las duraciones de los pasos) |

Por proveedor: **Hermes** = la `usage` de cada respuesta (Ollama `prompt_eval_count`, OpenAI `prompt_tokens`, Anthropic con caché incluida); si el servidor no informa la entrada (0), `peakInput` es una
**estimación** (`inputEstimated`, la interfaz pone `≈`). Ojo con Ollama: con la caché del prompt reutilizada algunas versiones informan en `prompt_eval_count` solo lo evaluado de nuevo, no el contexto entero.
**Claude Code**: el `usage` de cada mensaje `assistant` es una vuelta (`KindRound`, deduplicada por `message.id`: un mismo mensaje llega partido en una línea por bloque) → contexto máximo y vueltas;
el `result` trae `duration_api_ms` (tiempo en el modelo) y `num_turns`. **Antigravity**: si el `step_update` trae `usage` es una vuelta (`input_tokens` + `cache_read_tokens`); si no, solo el total.
**Codex y OpenCode**: solo el total. Para los CLIs, `Input` normaliza los adaptadores —que informan la entrada **sin** la caché leída— sumándola (`cliRecorder.setUsage`): antes ↑ la dejaba fuera
(subestimaba), ahora es todo lo que el modelo recibió, como en Hermes. Para Antigravity la suposición es que `result.usage.input_tokens` y `cache_read_tokens` vienen separados, como en cada paso de su
transcript: no se pudo comprobar contra el stream real (ver [26](26-antigravity-permisos.md)).

## Antigravity 1.4.x

Detalle en [26](26-antigravity-permisos.md#s17-la-herramienta-real-la-shell-y-el-turno-sin-respuesta-173-g): `unwrapAgyMCP` lee `tool_info.parameters`; la shell **ya se rechaza** en `--print` (por eso el turno
terminaba) y no se pasa `--sandbox` (la ejecutaría) ni se escribe un `deny` global; el prompt le pide al modelo que no la use; y un turno sin texto cuenta qué hizo (`turnRecap`, también en los servidores).

## Medidas: un Ollama simulado de 8k

`TestSmallOllamaEditsAMacetaWithin8kTokens` y `TestSmallOllamaWithTheFullProfileDoesNotFit` (`app_hermes_small_test.go`): un servidor `httptest` que finge `/api/show` con 8 192 de contexto, cuenta los
tokens de cada `/api/chat` (caracteres ÷ 4 de mensajes, argumentos de llamadas y herramientas) y contesta 400 si pasan de 8 192; el modelo es un guion. Maceta de ~600 líneas (la de `backend/mcp/testdata/maceta.scad`, de 17.1: cono hueco con drenajes y relieve curvado; si falta, una propia de 52 módulos de detalle). Escenario 1: «hazla 10 mm más alta». Escenario 2: cambiar el diámetro de los drenajes.

| Escenario | Antes: tokens por vuelta (catálogo completo, prompt de siempre, el modelo lee y reescribe el archivo) | Después: tokens por vuelta (Auto → esenciales, prompt compacto, edición por líneas) |
|---|---|---|
| 1. «hazla 10 mm más alta» | 17 860 · 18 807 · 18 891 (`get_parameters`, `set_parameters` variante, respuesta) | **1 890 · 2 027** (`set_parameters`, respuesta) |
| 2. cambio de código | 17 874 · 23 059 · 28 536 (`read_checkpoint` entero, `update_checkpoint` con el archivo completo, respuesta) | **1 898 · 3 151 · 3 283 · 3 476** (`outline_checkpoint`, `find_in_checkpoint`, `edit_checkpoint`, respuesta) |
| Contra el tope de 8 192 | **las 6 vueltas pasan** | ninguna pasa (máx. 3 476) |

Lo fijo por vuelta: antes **prompt 6 303 + herramientas 11 123** tokens (42 herramientas); ahora **681 + 1 097** (11 herramientas): 17 426 tokens contra 1 778, es decir 213 % de la ventana contra 22 % (`overBudget` pasa de verdadero a falso). El «antes» reproduce el chat anterior: la prueba reemplaza `windowFor` por «ventana desconocida», con lo que rigen los topes de entonces (32 000 caracteres por resultado, 160 000 de mensajes) y el `num_ctx` de 16 384. Cada petición pide
`num_ctx = 8192` (la ventana del modelo, no 16 384). Los números salen de `t.Log` de las pruebas (`go test -run SmallOllama -v .`).

## Tamaño de las salidas (maceta de 596 líneas, 20 653 B)

`go test ./backend/mcp -run TestToolOutputBudgetTable -v`: ningún resultado de texto pasa de **6 KB** sin `verbose`/`full`
(exentas por diseño: `read_checkpoint` —12 KB sin rango, 400 líneas con rango—, `read_skill`, `get_template`, `format`).

| Herramienta | Antes (B) | Ahora (B) |
|---|---|---|
| `read_checkpoint` sin rango / `from=440 to=480` | 20 653 / — | 12 365 (tope) / 1 477 |
| `outline_checkpoint` / `find_in_checkpoint` (en vez de leer todo) | 20 653 | 4 924 / 695 |
| `get_parameters` · `render` · `measure` | 13 285 · 729 · 2 518 | 3 703 · 253 · 422 |
| `update_checkpoint` (resultado) · `list_printers` | 762 · 29 608 | 528 · 512 |
| `preview` (PNG en base64) | 39 972 (640 px) | 21 240 (una vista `iso`, 384 px) |
| Petición del modelo para el mismo cambio | `update_checkpoint` 21 378 | `edit_checkpoint` 80 · `set_parameters` 49 |

Catálogo completo: 42 herramientas ≈ 11 229 tokens; esencial: 11 ≈ 1 114. Prompt de Hermes con contexto típico ≈ 6 184
tokens; compacto ≈ 714 (`backend/prompts/compact_test.go` exige ≤ 1 200).

El 3MF de la maceta que Bambu Studio rellenaba (partes abiertas por la cara de contacto entre colores, arreglado con un
segundo render `--enable=lazy-union`) está en [11-exportacion.md](11-exportacion.md).

## Pruebas

`backend/hermes/s17_test.go` (presupuesto, deduplicación, apretar, uso por vuelta, estimación, `ToolSpecs`, resumen extractivo, `Parts.usage`), `backend/store/migration11_test.go`,
`backend/chat` (`HistoryBudgetFor`), `backend/clichat` (rondas de Claude y Antigravity, `tool_info`, argv sin shell), `app_chat_context_test.go` (ventana, perfil, evento, resumen del modelo y extractivo),
`app_chat_recap_test.go`, `app_hermes_small_test.go` (arriba) y, en el frontend, `chat-ux.test.mjs` (pie, avisos, `CREATING_TOOLS` contra `effects.go`).

## Sin verificar

Con un Ollama real: que `prompt_eval_count` informe la entrada completa en tu versión, y el tiempo real de una vuelta. Con el `agy` real: el contorno exacto de `tool_info` en el stream y qué es
`result.usage` — los comandos están en [26](26-antigravity-permisos.md#para-probar-con-el-agy-real-no-se-pudo-aquí). Windows: compila y los tests de argv pasan; no se probó una sesión.
