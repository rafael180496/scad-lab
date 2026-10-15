# Base local `scadlab.db`

SQLite (`modernc.org/sqlite`, WAL, `foreign_keys`, `busy_timeout`, una sola
conexión de escritura). Código en `backend/store/`.

| Tabla | Qué guarda |
|---|---|
| `schema_migrations` | versión aplicada |
| `settings` | clave/valor JSON; cada campo de `store.Settings` es una clave (campo nuevo = sin migración) |
| `workspaces` | carpetas registradas, impresora, último abierto |
| `checkpoints` | índice de los `.scad` (se reconstruye del disco) + rótulo/plegado/sesión |
| `skills` | registro incluidas/propias y habilitado |
| `ai_providers` | claude, opencode, codex, antigravity (`agy`), ollama, openai-compatible, anthropic |
| `materials` | densidad y costo/kg (PLA, PETG, ABS, ASA, TPU, PLA-CF) |

## Migraciones — reglas duras

- Solo aditivas: `CREATE TABLE IF NOT EXISTS`, `ALTER TABLE … ADD COLUMN … DEFAULT`.
  Nunca `DROP` ni reescribir filas: la base vive en máquinas reales.
- Una migración publicada **no se edita**; un cambio es una versión nueva al
  final de `migrations`. (La versión 1 se puede ajustar solo mientras no haya
  salido la primera versión publicada.)
- Cada una corre una vez en su propia transacción.
- Test: `TestMigrationsAreIdempotent` (abre dos veces la misma base).
