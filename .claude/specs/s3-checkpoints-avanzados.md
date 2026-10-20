# S3 — Checkpoints avanzados

Fase del [roadmap](../../claude/01-roadmap.md): menú contextual del árbol,
papelera, ramificar con «padre activo», vínculo checkpoint ↔ sesión del agente,
y (agregado por el architect) comparar con el padre.

## Decisiones (architect)

- **Nunca se borra un checkpoint** (regla 3 de `technical.md`). «Borrar» mueve a
  `<ws>/.scadlab/trash/<AAAAMMDD-HHMMSS>-<archivo>`; se puede **restaurar**. No
  hay «vaciar papelera» en la app (el usuario puede borrar la carpeta a mano). [Superado: ver claude/15, «Eliminación permanente».]
- **Renombrar = rótulo** (columna `label` que ya existe): el archivo no cambia
  de nombre (los hijos lo referencian en `parent=`).
- **Padre activo** = `<ws>/.scadlab/active-parent` (una línea: el nombre del
  checkpoint). «Ramificar desde aquí» lo fija. **Avanza solo**: cuando se crea
  un checkpoint cuyo `parent` es el activo (por la app, el chat, el MCP o un
  agente que escribe el archivo directo), el nuevo pasa a ser el activo. Sin
  archivo = la rama del más nuevo (comportamiento de hoy). Si el activo se manda
  a la papelera, se limpia.
- Las **reglas del workspace** (`RULES.md.tmpl`) y la herramienta MCP
  `create_checkpoint` usan el padre activo como `parent` por defecto;
  `list_checkpoints` lo informa.
- **Vínculo con la sesión**: cuando aparece un checkpoint nuevo y hay una
  terminal de agente abierta en ese workspace, se guarda qué agente y qué
  sesión lo crearon (la sesión se resuelve con `backend/agentsessions`: la más
  reciente de ese agente para la carpeta). Los creados desde el chat guardan el
  `chatId`. Migración **3** aditiva: `checkpoints.agent_id TEXT DEFAULT ''`,
  `checkpoints.chat_id INTEGER DEFAULT 0` (`session_id` ya existe).

## Reparto (dos devs en Sonnet, en paralelo)

| Bloque | Dueño | Alcance |
|---|---|---|
| 3.A operaciones en Go | openscad-dev | `backend/checkpoint/**`, `backend/mcp/**` (default de `parent` y `list_checkpoints`), `assets/workspace/RULES.md.tmpl`, `claude/10-editor.md` o segmento nuevo `claude/15-checkpoints.md` + fila en `CLAUDE.md` |
| 3.B bindings, sesión y UI | fullstack-dev | `app_checkpoints.go` (nuevo), `app_workspaces.go` (watcher: avanzar el activo y vincular sesión), `app_editor.go`/`app_chat.go` (solo para guardar `chat_id` y avanzar), `backend/store/**` (migración 3 + repos), `frontend/**`, `index.html`, `CHANGELOG.md`, `scripts/help-shots.sh`, `docs/screenshots/**`, `claude/06-*.md`, `claude/01-roadmap.md` |

### Contrato 3.A (paquete `backend/checkpoint`)

```go
type TrashedItem struct { Name, Original, Parent, Description string; TrashedAt int64 }
func Trash(wsDir, file string) (trashedName string, err error)      // mueve a .scadlab/trash/
func TrashBranch(wsDir, file string) ([]string, error)               // el archivo y todos sus descendientes
func Descendants(wsDir, file string) ([]string, error)
func ListTrash(wsDir string) ([]TrashedItem, error)
func Restore(wsDir, trashedName string) (restoredFile string, err error) // a su nombre original si está libre; si no, siguiente NNN con el mismo slug (encabezado intacto)
func ActiveParent(wsDir string) string                              // "" si no hay o apunta a un archivo inexistente
func SetActiveParent(wsDir, file string) error                       // "" lo limpia; valida que exista
func Advance(wsDir, parent, newFile string) error                    // si activo == parent → activo = newFile
```
- `Create`/`CreateRoot` llaman a `Advance` al terminar. `Trash` limpia el
  activo si era ese archivo (o lo mueve al padre del borrado: decidir y
  documentar).
- Todo con `ValidName`, rutas dentro del workspace, sin seguir enlaces
  simbólicos hacia afuera, escrituras atómicas (`.scadlab/active-parent` con
  temporal + rename).

### 3.B — bindings y UI

- `app_checkpoints.go`: `TrashCheckpoint(id, file, branch bool)`,
  `ListTrash(id)`, `RestoreCheckpoint(id, trashedName)`,
  `SetActiveParent(id, file)`, `GetActiveParent(id)`, `RenameCheckpoint(id,
  file, label)` (usa `SetCheckpointUI`), `ResumeCheckpointSession(id, file)`
  (abre la terminal de agente en modo `resume` con la sesión guardada; si es de
  chat, abre el chat), `ReadCheckpointPair(id, file)` → `{parent, parentSource,
  source}` para el diff. Eventos: `checkpoints:changed` ya existe; emitir
  también al cambiar el activo.
- **Menú contextual** del nodo (clic derecho y botón «…» al hover), con
  `components/ui/Menu.tsx`: Ver código, Comparar con el padre, Ramificar desde
  aquí (marca activo), Renombrar (rótulo, en línea), Retomar sesión (si hay
  vínculo: «Retomar en Claude Code» / «Abrir el chat»), Plegar/Desplegar rama,
  Copiar nombre, Mostrar en el Finder, Mover a la papelera (este / con su rama,
  con `confirmDialog` que cuenta cuántos descendientes).
- Árbol: el **padre activo** con un indicador claro (p. ej. ícono de rama +
  acento) y `title`; tooltip con descripción, agente/sesión que lo creó y fecha.
- **Papelera**: entrada en la cabecera del árbol (ícono con contador) → diálogo
  con lo borrado (nombre, descripción, fecha) y **Restaurar**.
- **Comparar con el padre**: `DiffEditor` de Monaco (local, sin CDN) dentro del
  panel Código (modo diff con botón para volver), resaltado OpenSCAD.
- Atajos: F2 renombrar, Supr/⌫ papelera (con confirmación) con el árbol
  enfocado; sin pisar Monaco/xterm.
- Capturas: `tree-menu` (menú abierto), `tree-active-branch` (rama activa
  marcada), `trash` (diálogo), `diff-parent` (diff con el padre). Ayuda: temas
  `checkpoints` y `checkpoints-arbol` reescritos sin «En desarrollo»; receta
  «Ramificar para probar dos variantes» actualizada.

## Pruebas

- 3.A: tabla de casos con `t.TempDir()`: trash/restore (nombre libre y
  ocupado), rama completa, activo que avanza, activo borrado, nombres hostiles,
  enlaces simbólicos; MCP `create_checkpoint` sin `parent` usa el activo.
- 3.B: migración 3 idempotente; vínculo de sesión con un `agentsessions`
  inyectado; banco + `uishot-fit.sh`; clic derecho por CDP en el banco.
- Nadie ejecuta Bambu Studio ni abre sesiones de agentes.
