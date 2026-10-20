# Historial de checkpoints: la IA edita el archivo que nombras (S16)

Plan: [.claude/specs/s16-ia-edita-con-historial.md](../.claude/specs/s16-ia-edita-con-historial.md). Lógica de archivos
(escritura atómica, tope, papelera, purga): [15-checkpoints.md](15-checkpoints.md). Herramienta MCP
`update_checkpoint`: [13-mcp.md](13-mcp.md) y [25-herramientas-ia.md](25-herramientas-ia.md). Contrato de bindings y
evento: [30-contrato-checkpoints.md](30-contrato-checkpoints.md).

## El problema

En el chat, con Antigravity en «Agente», el usuario dijo «es en el archivo `002-rpi5-snap-case.scad` que quería los
cambios». La IA (1) llamó `read_file` con una ruta que no existía, (2) contestó que los checkpoints son inmutables y (3)
creó `004-rpi5-snap-case-vents.scad` como hijo. Decisión del usuario: **«Editar el archivo con respaldo»**, para todos los
proveedores (Claude Code, Codex, Antigravity, OpenCode y Hermes).

### Diagnóstico del `read_file` fallido

El turno de un CLI corre con `cwd` = carpeta del workspace, pero el prompt solo decía «esta carpeta» y **nunca la ruta
absoluta**. Las herramientas de archivo de Antigravity (`view_file`, ver `backend/clichat/testdata/antigravity-denied.jsonl`)
piden `AbsolutePath`: el modelo no podía saberla, la inventó y falló. No era un problema de nuestro servidor MCP. No se pudo
leer el historial real del usuario para confirmarlo: queda en la prueba manual del plan (16.4). Arreglo doble:

1. `cliPrompt` (`app_chat_cli.go`) nombra la carpeta con `absDir(workspace.Path)`: «El proyecto está en <ruta>: es tu
   directorio actual».
2. El prompt, las reglas del workspace y los de Hermes dicen que un `.scad` se **lee con `read_checkpoint`** (por nombre, sin
   ruta) y se **cambia con `update_checkpoint`**, nunca con las herramientas de archivo ni la shell del CLI.

## La regla (una frase, la misma en todas partes)

Cuando el usuario **nombra un archivo existente** o se refiere al **seleccionado** («este», «el actual») y pide cambios, la IA
lo **edita con `update_checkpoint`**; crea un checkpoint nuevo solo si pide algo nuevo, una variante u «otra versión»; ante la
duda, edita el que nombró. Corregir un render que falla en el archivo que acaba de crear también es `update_checkpoint`. El
«nunca modifiques un checkpoint existente» dirigido a la IA ya no existe en ningún prompt.

El prompt de cada turno CLI (`cliPrompt`, textos `msgCLIPrompt*`) lleva: la ruta absoluta, «Checkpoint seleccionado en la app
(es “este archivo” / “el actual”): X» (más su código, hasta `cliSourceBytes`) y, en «Agente» con el MCP conectado,
`msgCLIPromptEdit` (leer con `read_checkpoint`, editar con `update_checkpoint`). Hermes recibe lo mismo desde
`assets/prompts` + `prompts.Context.CurrentFile` (que `chatPromptContext` sigue pasando).

## Historial en disco

```
<ws>/.scadlab/history/<archivo.scad>/<id>.scad    la versión anterior, entera
<ws>/.scadlab/history/<archivo.scad>/<id>.json    {savedAt, reason, tool, note, size}
```

`<id>` = `AAAAMMDD-HHMMSS` hora local (`-2`, `-3`… si choca). `reason`: `ai` (lo guardó `update_checkpoint`) o `restore`
(lo guardó un «Restaurar» o «Deshacer»). Orden: **respaldo → escritura atómica**. Tope: `Settings.HistoryMaxVersions`
(`historyMaxVersions`, 1–200, 20 por defecto), se poda lo más viejo **de ese archivo**. El historial queda fuera del árbol:
`workspace.Scan` solo mira la raíz. La papelera lo lleva a `.scadlab/trash/<sello>-<archivo>.history/` y lo devuelve al
restaurar; `EmptyTrash` y `PurgeTrashed` lo borran. «Editado por la IA» = la versión **más nueva** tiene `reason: "ai"`:
restaurar guarda la actual como `restore`, así que el indicador se apaga solo y el deshacer se puede deshacer. Restaurar
conserva el `parent=` **actual** del archivo (metadato del árbol).

## Bindings y evento (`app_history.go`)

| Binding | Devuelve |
|---|---|
| `ListCheckpointHistory(workspaceID, file)` | `CheckpointHistory{file, versions[] (más nueva primero, nunca null), editedByAI, max}` |
| `ReadCheckpointVersion(workspaceID, file, versionID)` | el código de esa versión |
| `RestoreCheckpointVersion(workspaceID, file, versionID)` | `checkpoint.Version` (la que se guardó con lo que había); emite `checkpoint:updated` y `checkpoints:changed` |

Los tres validan `checkpoint.ValidName` y `checkpoint.ValidVersionID`: nada con rutas. `store.Checkpoint.EditedByAI` no está
en la base: lo calcula `rescan` (`checkpoint.EditedByAI`, una lectura de directorio por archivo) en cada escaneo.

**`checkpoint:updated {workspaceId, file}`** se emite por tres caminos (la interfaz tolera repetidos: releer es idempotente):

1. tras un paso bueno de una herramienta con `mcp.UpdatesInPlace(tool)` (`refreshTreeAfterStep` → `refreshAfterUpdate`, en el
   chat de Hermes y de los CLIs; el archivo sale de `args.file` o del `file` del resultado);
2. por el watcher (`backend/watch`, `StartChanges`): compara tamaño y fecha de cada `.scad` de la raíz entre dos escaneos y
   reporta `Updated` (existía y cambió). Por **firma y no por tipo de evento**: una escritura atómica llega como Write, Create
   o Rename según el sistema. Cubre el subproceso `--mcp` de un CLI y cualquier editor externo. `onWorkspaceChange` lo emite;
3. `RestoreCheckpointVersion`.

La app **no renderiza** un archivo reescrito: lo hace la interfaz si es el elegido (la caché es por contenido y salta la
cabecera, así que repetirlo es gratis) y así no se renderiza dos veces.

## Interfaz

- **Árbol**: `EditedByAIBadge` (robot) en el nodo; menú del nodo con «Deshacer cambio de la IA» y «Ver diferencias» (solo si
  `editedByAI`) e «Historial» (siempre), todo en `checkpointActions.tsx`.
- **Cabeceras** del editor y del visor del elegido: `HistoryControls` (pastilla «Editado por la IA», deshacer, diferencias,
  historial).
- **Diálogos** `HistoryHost` (`components/workspace/HistoryDialogs.tsx`, montado en App; estado en `lib/historyUI.ts`):
  la lista de versiones y la comparación con `DiffViewer` (versión vs. actual, rótulos con fecha, motivo y nota). Confirmaciones
  y avisos en `historyActions.ts` (`confirmDialog`, `toast`).
- **`checkpoint:updated` en App** (`lib/history.ts` `planUpdate`): si es el elegido y el editor no tiene cambios sin guardar,
  relee, renderiza (si el código cambió y no hay un render en curso) y refresca; con cambios sin guardar, aviso con «Recargar»
  (que pasa por `confirmLeave`); si no es el elegido, `useRenders.forget` olvida su render viejo.
- **Configuración → General → «Versiones guardadas por archivo»** (`historyMaxVersions`).
- Banco: vistas `history-menu`, `history-dialog` y `history-diff` (`uishot.tsx`).

## Lo que queda fuera

- **Escrituras directas** con las herramientas de archivo del CLI (`Edit`/`Write` de Claude, `apply_patch` de Codex,
  accept-edits de `agy`, build de OpenCode) **no dejan respaldo**: las reglas lo prohíben para `.scad`. Pendiente anotado:
  reglas de denegación por patrón (`Edit(*.scad)`) o respaldo desde el watcher con el último contenido conocido.
- **Editor manual**: sigue en solo lectura; guardar crea un checkpoint hijo. Opción anotada: «Editar en el lugar» usaría el
  mismo `checkpoint.Update` con `reason: "user"` y este historial.
- Prueba con el `agy` real y los demás CLIs: pendiente del usuario (16.4).

## Pruebas

`app_history_test.go` (bindings, tope, nombres hostiles, papelera, watcher), `app_chat_update_test.go` (Claude, Codex,
OpenCode y Antigravity falsos contra el `--mcp` real: prompt, permisos, edición en el lugar, variante, «Solo consulta»),
`app_hermes_update_test.go` (servidor simulado y caja real), `backend/watch/watch_test.go` (`Updated`) y
`frontend/scripts/history.test.mjs` (qué se ofrece, qué hace la interfaz ante el evento, textos es/en).
