# Base local `scadlab.db`

SQLite (`modernc.org/sqlite`, WAL, `foreign_keys`, `busy_timeout`, una sola
conexión de escritura). Código en `backend/store/`.

| Tabla | Qué guarda |
|---|---|
| `schema_migrations` | versión aplicada |
| `settings` | clave/valor JSON; cada campo de `store.Settings` es una clave (campo nuevo = sin migración) |
| `workspaces` | carpetas registradas, impresora, último abierto, `last_checkpoint` (migración 4: el checkpoint elegido al cerrar) |
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
