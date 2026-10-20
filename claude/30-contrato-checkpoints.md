# Contrato Go ↔ React: checkpoints avanzados

Parte de [06-go-react-contract.md](06-go-react-contract.md) (índice del contrato). Fuente: `app_checkpoints.go`, `app_chat.go`.

## Checkpoints avanzados — `app_checkpoints.go`, `app_chat.go` (S3)

Las operaciones sobre archivos viven en `backend/checkpoint` (ver [claude/15](15-checkpoints.md));
estos bindings resuelven el workspace, validan el nombre (`checkpoint.ValidName`, un `.scad`
suelto de la raíz), mantienen el índice de la base y emiten `checkpoints:changed`.

| Binding | Devuelve | Nota |
|---|---|---|
| `TrashCheckpoint(id, file, branch)` | `[]string` (nombres en la papelera) | con `branch` también todos los descendientes (el de `file` primero); sin `branch`, los hijos directos pasan a colgar del padre (o quedan raíz). Guarda antes rótulo, plegado y vínculo en `checkpoint_trash` |
| `ListTrash(id)` | `[]checkpoint.TrashedItem{name, original, parent, description, trashedAt (s)}` | lo más reciente primero |
| `RestoreCheckpoint(id, trashedName)` | `string` (nombre con el que volvió) | su nombre si está libre; si no, el siguiente `NNN` con el mismo slug. Devuelve rótulo, plegado y vínculo, y vuelve a colgar a los hijos que el borrado había re-colgado |
| `DeleteTrashed(id, trashedName)` | — | **elimina para siempre** un elemento de la papelera, su registro de re-colgado y lo guardado en `checkpoint_trash`. Solo la interfaz (confirmación `danger`); sin herramienta MCP |
| `EmptyTrash(id)` | `int` (checkpoints eliminados) | **vacía** `.scadlab/trash` entera (también lo que no sea checkpoint); deja `.scadlab/no-starter` para que no reaparezca el modelo de inicio |
| `MakeCheckpointRoot(id, file)` | — | «Hacer principal»: `parent=` vacío en la cabecera de `file` (única reescritura de un `.scad`; ver [15](15-checkpoints.md)) |
| `SetActiveParent(id, file)` / `GetActiveParent(id)` | — / `string` | `""` limpia / «sin padre activo explícito» (rige el más nuevo) |
| `RenameCheckpoint(id, file, label)` | — | rótulo (`label`): máx. 80 letras, sin saltos; `""` lo quita. El archivo no cambia de nombre |
| `ReadCheckpointPair(id, file)` | `CheckpointPair{file, parent, parentSource, source}` | para el diff; error claro si es una raíz o el padre ya no está (¿papelera?) |
| `ResumeCheckpointSession(id, file)` | `CheckpointResume{kind, terminalId, agentId, chatId}` | `kind: "terminal"` → abrió el CLI con `StartAgentTerminal` en modo `resume` (la UI reengancha `terminalId`); `"chat"` → la UI abre `chatId`. Falla sin vínculo, o con un agente sin id de sesión (Antigravity) |
| `RevealCheckpoint(id, file)` | — | Finder / Explorador, sobre `RevealPath` |
| `SaveChatCheckpoint(workspaceId, chatId, parent, label, desc, source)` | `string` | como `SaveAsCheckpoint`, y guarda `chat_id` si el chat es del workspace |
| `ChatCheckpointParent(workspaceId, currentFile)` | `string` | el archivo abierto; si no, el último checkpoint; **`""` en un workspace sin checkpoints** (S12.c: ya no es un error, el bloque será la raíz). `SaveAsCheckpoint` / `SaveChatCheckpoint` con padre `""` usan `checkpoint.CreateRoot` (falla si ya hay checkpoints: un padre olvidado) |
| `CreateStarterCheckpoint(id)` | `string` | «Crear un modelo de inicio» (visor y Código, S12.c): escribe `001-<nombre>.scad` aunque la papelera tenga algo; `""` si el workspace ya tiene un `.scad` |
| `EnsureAntigravityMCP()` | `{added, state, permissionsAdded, permissionsError}` | registra `scadlab` en la configuración global de Antigravity si falta (ver [13-mcp.md](13-mcp.md)) y sincroniza sus permisos (`mcp(scadlab/<herramienta>)` en `permissions.allow` de su `settings.json`, ver [26](26-antigravity-permisos.md)); lo llama el chat al mostrar `cli:antigravity`, y el backend al enviar un turno y al abrir su terminal |
| `AntigravityPermissionsStatus()` | `{state, path, approved, total, stale, managed, detail}` | solo lectura: cuántas herramientas de ScadLab tienen su regla (`ok`/`partial`/`none`/`unreadable`/`unavailable`/`not-installed`) |
| `RemoveAntigravityPermissions()` / `RepairAntigravityPermissions()` | mismo estado | «Quitar» (solo las `mcp(scadlab/…)`; deja de agregarlas solo) / «Volver a agregar» y «Reparar permisos de Antigravity» (las repone y restablece la sincronización) |

`WorkspaceView` ganó `activeParent` (`""` = ninguno explícito): lo llevan `OpenWorkspace`,
`RescanWorkspace` y el evento `checkpoints:changed`. Cambiar el activo emite ese mismo evento
(el archivo `.scadlab/active-parent` no es un `.scad` y el watcher no lo ve).
`store.Checkpoint` ganó `agentId` y `chatId` (`sessionId` ya existía): el vínculo con quien lo creó.

**Vínculo con la sesión.** El watcher, al ver un `.scad` nuevo, avanza el padre activo
(`checkpoint.Advance`) y, si hay una terminal de agente abierta en el workspace, guarda el agente y la
sesión más reciente de ese agente para la carpeta (`agentsessions.List`, en otra goroutine). No se
vincula lo que creó la propia app (`SaveAsCheckpoint`, `SaveChatCheckpoint`) ni lo que acaba de volver
de la papelera. La migración 3 de `scadlab.db` agrega `checkpoints.agent_id`, `checkpoints.chat_id` y la
tabla `checkpoint_trash`; `SyncCheckpoints` no toca esas columnas.
