# Base local `scadlab.db`

SQLite (`modernc.org/sqlite`, WAL, `foreign_keys`, `busy_timeout`, una sola
conexión de escritura). Código en `backend/store/`.

| Tabla | Qué guarda |
|---|---|
| `schema_migrations` | versión aplicada |
| `settings` | clave/valor JSON; cada campo de `store.Settings` es una clave (campo nuevo = sin migración) |
| `workspaces` | carpetas registradas, impresora, último abierto, `last_checkpoint` (migración 4: el checkpoint elegido al cerrar); `skills` y `print_settings` (migración 10: skills activas y ajustes de impresión propios del workspace, con el backfill de skills y de colores) |
| `checkpoints` | índice de los `.scad` (se reconstruye del disco) + rótulo/plegado/sesión |
| `skills` | registro incluidas/propias y habilitado (la carpeta de las propias es `settings.skillsDir`, sin migración: ver [20-skills-propias.md](20-skills-propias.md)) |
| `ai_providers` | claude, opencode, codex, antigravity (`agy`), ollama, openai-compatible, anthropic. Desde la migración 5 el chat no usa las tres últimas (usa `model_servers`); siguen ahí sin tocar |
| `model_servers` | servidores de modelos del chat (migración 5): `kind` ollama/openai/anthropic, URL, `api_key_env`, `encrypted_token`+`token_nonce` (AES-256-GCM con la llave derivada de la contraseña maestra, que nunca está en la base; ver [21-llavero.md](21-llavero.md)), modelo por defecto, ventana de Ollama |
| `vault_meta` | una fila (migración 7): `verifier`+`verifier_nonce` (un texto conocido cifrado con la llave derivada: si abre, la contraseña es la buena), `remember` (la llave está en el llavero del SO), `created_at`. El salt vive en `<datos>/vault.salt`, no acá |
| `chats`, `chat_messages` | conversaciones del chat (migración 2); `chat_messages.parts` (JSON de adjuntos o pasos), `input_tokens`, `output_tokens` y `cost_usd` desde la migración 6; `chats.cli_session` desde la 8 (el id de conversación del CLI con que sigue un chat `cli:*`) |
| `materials` | densidad y costo/kg (PLA, PETG, ABS, ASA, TPU, PLA-CF) |

## Migraciones — reglas duras

- Solo aditivas: `CREATE TABLE IF NOT EXISTS`, `ALTER TABLE … ADD COLUMN … DEFAULT`.
  Nunca `DROP` ni reescribir filas: la base vive en máquinas reales.
- Una migración publicada **no se edita**; un cambio es una versión nueva al
  final de `migrations`. (La versión 1 se puede ajustar solo mientras no haya
  salido la primera versión publicada.)
- Cada una corre una vez en su propia transacción.
- Test: `TestMigrationsAreIdempotent` (abre dos veces la misma base); la migración 4 se prueba sobre una base de la v3 con filas (`migration4_test.go`) y la 6 sobre una de la v5 con chat y mensajes (`migration6_test.go`).

## Migraciones de S12

| Versión | Qué agrega |
|---|---|
| 5 | `model_servers` (servidores de modelos) y `workspaces.last_model` (JSON `{serverId, model}` por proyecto; el global es el ajuste `lastModel`). Copia las filas de chat de `ai_providers` (`ollama`, `openai`, `anthropic`) **con el mismo id**: `chats.provider_id` de los chats viejos sigue resolviendo |
| 6 | `chat_messages.parts TEXT NOT NULL DEFAULT ''`, `input_tokens`, `output_tokens` (INTEGER) y `cost_usd` (REAL), todas con default: los mensajes anteriores quedan sin partes ni tokens. `role` sigue siendo `user\|assistant` (el CHECK de la migración 2 no se puede cambiar de forma aditiva): los pasos de herramienta viajan dentro de `parts` |

`parts` es un JSON opaco para `backend/store`; lo escribe y lee `backend/hermes` (`Parts`: `attachments`, `steps`, `model`, `stepLimit`).
Las imágenes de un paso se guardan reducidas (JPEG ≤ 640 px) y los adjuntos solo como ficha y miniatura, nunca los bytes.

## Migración de S12.b

| Versión | Qué agrega |
|---|---|
| 8 | `chats.cli_session TEXT NOT NULL DEFAULT ''`: el id de conversación que devolvió el CLI (`--resume`, `resume <id>`…) de un chat con `provider_id = 'cli:*'`; el historial lo guarda el CLI. `SetChatModel` la olvida si cambia el proveedor. Prueba: `migration8_test.go` (base de la v7 con chats y mensajes) |

## Migración de S12.c

| Versión | Qué agrega |
|---|---|
| 9 | `chats.mode TEXT NOT NULL DEFAULT ''`: el modo de escritura **de esa conversación**. `''` = «Agente» (el predeterminado, también de toda fila anterior y de toda conversación nueva); `'ask'` = «Solo consulta», elegido a mano. No hay otro valor: `store.ChatModeValue` normaliza lo que llega de la interfaz (`agent` → `''`, `ask` → `ask`) y rechaza el resto. Reemplaza a las preferencias globales por tipo de proveedor que guardaba `localStorage`. Prueba: `migration9_test.go` (base de la v8 con chats de un servidor y de un CLI, mensajes y una sesión de CLI: suben a la 9 en Agente, sin perder nada, y la apertura repetida no hace nada) |

## Migración de S14

| Versión | Qué agrega |
|---|---|
| 10 | `workspaces.skills TEXT NOT NULL DEFAULT ''` y `workspaces.print_settings TEXT NOT NULL DEFAULT ''`. **`skills`** es un JSON `["nombre", …]` con las skills activas **de ese workspace**; `''` = «sin definir» (se resuelve por defecto en tiempo de ejecución: `store.Workspace.Skills` es `nil`) y `[]` = «ninguna». **`print_settings`** es el JSON de `store.WorkspacePrint` (`colors`, `material`, `infillPercent`, `wallLoops`, `lineWidthMm`, `filamentDiameterMm`, todo opcional); `''` = nada fijado. **Backfill solo de la columna nueva**: cada workspace existente queda con la lista de skills que estaban encendidas en `skills` en ese momento (`json_group_array` ordenada por nombre), es decir lo que ya tenía instalado, y cambiar Configuración → Skills después no lo altera. Si la tabla `skills` no tiene ninguna fila (una base que nunca la pobló) no hay nada que congelar y `skills` queda `''`; si tiene filas y todas están apagadas queda `[]`. No se reescribe ninguna otra columna. Prueba: `migration10_test.go` (base de la v9 con tres workspaces, checkpoints, un chat y un registro de skills con encendidas, una apagada y una propia: backfill correcto, nada de lo anterior cambia, sin filas en `skills` deja `''`, todas apagadas deja `[]`, la segunda apertura no hace nada) |

**Backfill de colores (misma migración 10).** Antes de S14 la cantidad de colores de un workspace era el `multicolor_slots` de su impresora; desde S14 el valor de fábrica es
`default_colors` del catálogo nuevo (4 en una Bambu de la serie A, 1 en una Prusa MK4S…). Para que un workspace existente **no pierda colores** (una P1S o una X1C guardada pasaría de 16 a 4),
la migración hace `UPDATE workspaces SET print_settings = CASE printer_id … END` con `{"colors": N}`, donde N es el `multicolor_slots` del catálogo anterior: `bambu-a1-mini` 4, `bambu-a1` 4,
`bambu-p1p` 16, `bambu-p1s` 16, `bambu-p2s` 16, `bambu-x1c` 16, `bambu-x1e` 16, `bambu-h2d` 24, `bambu-h2s` 24. Solo toca filas con `print_settings = ''` y esos nueve ids: un id desconocido
no se toca (queda en el valor de fábrica de su impresora) y los workspaces nuevos siguen naciendo con `default_colors`. Prueba: `migration10_test.go` (una P1S queda con 16, una H2D con 24, un id
desconocido sin cambios, y una fila que ya tenía ajustes los conserva).

Una columna con basura (JSON que no es una lista de textos, p. ej. una base editada a mano) se lee como «sin definir»: no impide abrir el
workspace. `Store.SetWorkspaceConfig(id, printerId, skills, print)` guarda impresora, skills y ajustes en una sola sentencia;
`InsertWorkspace` también guarda `skills` y `print_settings`. Los ajustes de impresión propios de un workspace **no** tocan la tabla
`settings`: lo que no fija lo hereda (ver [29](29-contrato-workspaces.md#configuración-por-workspace--app_wsconfiggo-s14)).

`settings.slicerPaths` (mapa id → ruta del slicer) es una clave más de la tabla key/value: sin migración.
