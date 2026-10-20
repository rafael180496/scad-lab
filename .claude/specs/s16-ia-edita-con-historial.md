# S16 — La IA edita el archivo que nombras (con historial y deshacer)

Fase del roadmap: [claude/01-roadmap.md](../../claude/01-roadmap.md). Notas que cambia: regla 3 de
[technical.md](../rules/technical.md), `claude/15`, `claude/13`, `claude/25`, `claude/26` (mención), segmento nuevo
`claude/34-historial-checkpoints.md`, contrato en `claude/30`.

## Bug y pedido

En el chat, Antigravity en «Agente», el usuario: «es en el archivo 002-rpi5-snap-case.scad que quería los cambios».
La IA (1) llamó `read_file` con una ruta inexistente, (2) contestó que los checkpoints son inmutables y (3) creó
`004-rpi5-snap-case-vents.scad` como hijo. El usuario lo considera un error. **Decisión del usuario: «Editar el archivo
con respaldo».** Vale para todos los proveedores: Claude Code, Codex, Antigravity, OpenCode y Hermes (Ollama/API).

## Diagnóstico del `read_file` fallido

El turno de un CLI corre con `cwd` = carpeta del workspace, pero el prompt solo decía «esta carpeta» y nunca la ruta
absoluta. Las herramientas de archivo de Antigravity (`view_file`, ver `testdata/antigravity-denied.jsonl`) piden
**`AbsolutePath`**: el modelo inventó la ruta (no había forma de saberla) y falló. No es un problema de nuestro MCP. Arreglo
doble: (a) el prompt de cada turno nombra la carpeta absoluta del workspace; (b) las reglas y prompts dicen que un `.scad`
se lee con **`read_checkpoint`** (nombre del archivo, sin ruta) y se cambia con **`update_checkpoint`**, nunca con las
herramientas de archivo del CLI. No se pudo leer el historial real del usuario para confirmarlo: queda en la prueba manual.

## Decisiones

1. **Regla nueva** (una sola frase, la misma en todas partes): «Cuando el usuario nombra un archivo existente o se refiere
   al abierto/seleccionado (“este”, “el actual”) y pide cambios, **edítalo con `update_checkpoint`**. Crea un checkpoint
   nuevo solo si pide algo nuevo, una variante u “otra versión”. Ante la duda, edita el que nombró.» Desaparece de
   todos lados el «nunca modifiques un checkpoint existente» dirigido a la IA. Corregir un render que falla **en el
   archivo que la IA acaba de crear en este turno** también es `update_checkpoint` (el intento roto queda en el historial).
2. **Herramienta MCP `update_checkpoint(file, source, note?)`** — escritura, solo «Agente». Valida nombre (`ValidName`), que
   exista en la raíz, que no sea enlace simbólico, que no esté solo en la papelera (si no existe pero hay un
   `<stamp>-<file>` en la papelera el error lo dice y sugiere `restore_checkpoint`). Conserva la cabecera
   `// @checkpoint parent=… desc=…` (si `source` trae su propia línea `@checkpoint` se descarta); `note` reemplaza `desc`.
   Si el contenido es idéntico no escribe (`unchanged: true`). Orden: **respaldo en el historial → escritura atómica**
   (temporal en la carpeta + rename, conserva permisos). Después **lint + render** y devuelve
   `{file, updated:true, unchanged, version:{id,…}, parent, description, lint:[…], render:{ok, …}}`; si el render falla,
   el mismo formato de diagnósticos que `failedRender` con el consejo «corrígelo con update_checkpoint sobre este
   archivo». No toca el padre activo. Hermes la recibe gratis (misma caja `mcp.Toolbox`).
3. **Historial por archivo** en `<ws>/.scadlab/history/<archivo.scad>/<id>.scad` + `<id>.json`
   (`{"savedAt","reason","tool","note","size"}`), donde `<id>` = `AAAAMMDD-HHMMSS` hora local (+`-2`, `-3`… si choca,
   creado con `O_EXCL`). Guarda **la versión anterior** al cambio. `reason`: `ai` (lo escribió `update_checkpoint`) o
   `restore` (lo guardó un «Restaurar»/«Deshacer»). **Tope configurable** `Settings.HistoryMaxVersions`
   (`json:"historyMaxVersions"`, 0 = 20, rango 1–200): al pasar el tope se eliminan las versiones más viejas de ese
   archivo. `.scadlab/history` no se sigue si es enlace (como `.scadlab/trash`). `workspace.Scan` solo mira la raíz, así
   que el historial ya queda fuera del árbol: una prueba lo fija.
4. **«Editado por la IA»** = la versión más nueva del historial tiene `reason: "ai"`. «Deshacer cambio de la IA» = restaurar
   esa versión; restaurar guarda primero la actual con `reason: "restore"`, así que el indicador se apaga solo y el
   deshacer se puede deshacer. Restaurar conserva el `parent=` **actual** de la cabecera (metadato del árbol) y toma
   cuerpo y `desc` de la versión.
5. **Papelera**: `Trash`/`TrashBranch` mueven el historial a `.scadlab/trash/<stamp>-<archivo>.history/` (mismo stamp);
   `Restore` lo devuelve a `.scadlab/history/<nombre con que volvió>/` (si ya hay carpeta, mueve versión por versión sin
   pisar); `PurgeTrashed` borra también esa carpeta; `EmptyTrash` ya vacía todo (verificar que borra carpetas sin
   seguir enlaces). `ListTrash` sigue ignorándolas.
6. **Regla 3 reescrita**: los checkpoints ya no son «inmutables» para todo el mundo; las excepciones pasan a ser (a)
   `SetParent`, (b) eliminación permanente de la papelera (solo el usuario), (c) **reescritura en el lugar con respaldo
   previo en el historial**: `update_checkpoint` (IA, solo «Agente») y «Restaurar versión»/«Deshacer» (usuario); y (d) la
   poda del historial por el tope, que borra **respaldos**, nunca un checkpoint. Ninguna herramienta borra.
7. **Refresco**: `update_checkpoint` es `EffectCheckpoint` en `effects.go` (el chat enlaza y selecciona el archivo, la red
   de seguridad sabe que hubo entrega) y además `mcp.UpdatesInPlace(tool) bool` (conjunto `{update_checkpoint}`). La app
   emite **`checkpoint:updated {workspaceId, file}`** tras un paso bueno de esa herramienta **y** cuando el watcher ve
   escrito un `.scad` que ya existía (cubre el subproceso `--mcp` de los CLIs y cualquier escritura externa). El frontend,
   si el archivo es el elegido, relee el código (si no hay cambios sin guardar), re-renderiza (la caché es por contenido:
   sale sola) y refresca el visor y la etiqueta «editado por la IA».
8. **Permisos**: `update_checkpoint` entra en `writeTools` (`clichat/modes.go`) → Claude `--allowedTools` en «Agente»,
   Codex `enabled_tools`, OpenCode (lo que use `modes.go`), y las reglas `mcp(scadlab/update_checkpoint)` de Antigravity
   se agregan solas en la próxima sincronización (`AgyRules` sale de las dos listas). «Solo consulta» lo bloquea el
   `turnguard` (por `EffectOf != None`). Pruebas que lo fijan.
9. **Contexto del archivo abierto**: el prompt de cada turno CLI nombra la carpeta absoluta del workspace y «Checkpoint
   seleccionado en la app (es “este archivo”/“el actual”): X», y recuerda en una línea: leer con `read_checkpoint`,
   cambiar ese archivo con `update_checkpoint`. El prompt de Hermes (`assets/prompts/agent.*.md` + `prompts.Context`) dice
   lo mismo con `CurrentFile`.
10. **Editor manual**: sigue en solo lectura. Opción anotada (no en esta fase): «Editar en el lugar» del editor usaría el
    mismo `checkpoint.Update` con `reason: "user"` y el mismo historial.
11. **Escrituras directas con las herramientas de archivo del CLI** (Edit/Write de Claude, apply_patch de Codex,
    accept-edits de agy, build de OpenCode) **no** dejan respaldo: las reglas lo prohíben para `.scad`. Pendiente
    anotado: reglas de denegación por patrón (`Edit(*.scad)`) o respaldo desde el watcher con el último contenido
    conocido.

## Contrato

### Go — `backend/checkpoint/history.go` (openscad-dev, T1)

```go
const (
    ReasonAI      = "ai"
    ReasonRestore = "restore"
    DefaultHistoryMax = 20
)
type Version struct {
    ID      string    `json:"id"`      // "20261019-221500" o "20261019-221500-2"
    SavedAt time.Time `json:"savedAt"`
    Reason  string    `json:"reason"`  // ReasonAI | ReasonRestore
    Tool    string    `json:"tool"`    // "update_checkpoint" | ""
    Note    string    `json:"note"`
    Size    int64     `json:"size"`
}
type UpdateResult struct {
    Unchanged   bool
    Version     Version // respaldo guardado (cero si Unchanged)
    Description string  // desc final de la cabecera
    Parent      string
}
func Update(ws, file, source, note string, maxVersions int) (UpdateResult, error) // IA: ReasonAI, Tool "update_checkpoint"
func History(ws, file string) ([]Version, error)            // más nueva primero; sin carpeta → nil, nil
func ReadVersion(ws, file, id string) (string, error)
func RestoreVersion(ws, file, id string, maxVersions int) (Version, error) // guarda la actual (ReasonRestore) y escribe la versión
func EditedByAI(ws, file string) bool
func ValidVersionID(id string) error
```

Más los cambios en `Trash`/`TrashBranch`/`Restore`/`PurgeTrashed`/`EmptyTrash` (decisión 5). `maxVersions <= 0` =
`DefaultHistoryMax`.

### Go — `backend/store/settings.go` (openscad-dev, T1)

`HistoryMaxVersions int \`json:"historyMaxVersions"\`` (0 = 20; validación 1–200 donde se validan los demás).

### MCP (openscad-dev, T1)

`update_checkpoint` con `inputSchema`: `file` (requerido), `source` (requerido, ≤ 2 MB), `note` (opcional).
`annotations("write")` como las demás de escritura. `effects.go`: `"update_checkpoint": EffectCheckpoint`,
`func UpdatesInPlace(tool string) bool`. `clichat/modes.go`: en `writeTools`.

### Wails (fullstack-dev, T2) — `app_history.go`

```go
type CheckpointHistory struct {
    File       string               `json:"file"`
    Versions   []checkpoint.Version `json:"versions"`
    EditedByAI bool                 `json:"editedByAI"`
    Max        int                  `json:"max"`
}
func (a *App) ListCheckpointHistory(workspaceID int64, file string) (CheckpointHistory, error)
func (a *App) ReadCheckpointVersion(workspaceID int64, file, versionID string) (string, error)
func (a *App) RestoreCheckpointVersion(workspaceID int64, file, versionID string) (checkpoint.Version, error)
```

TS (generado en `frontend/wailsjs/`): `ListCheckpointHistory(id: number, file: string): Promise<main.CheckpointHistory>`,
`ReadCheckpointVersion(id, file, versionID): Promise<string>`, `RestoreCheckpointVersion(id, file, versionID):
Promise<checkpoint.Version>`. El nodo del árbol lleva `editedByAI: boolean` (campo nuevo en el struct que ya devuelve la
lista de checkpoints). `RestoreCheckpointVersion` emite `checkpoint:updated` y `checkpoints:changed`.

Evento nuevo: **`checkpoint:updated`** `{workspaceId: number, file: string}`.

Sin migración de `scadlab.db` (la configuración es JSON en settings).

## Tareas

| # | Tarea | Dueño | Alcance | Estado |
|---|---|---|---|---|
| T1 | Núcleo: historial en `backend/checkpoint`, papelera, `update_checkpoint` (MCP + Hermes), clasificación (`effects.go`, `modes.go`), textos de la regla en servidor MCP, descripciones de herramientas, `failedRender`, `render/errors_md.go`, `RULES.md.tmpl`, `assets/prompts/*`, `backend/prompts`, starter, skills, `Settings.HistoryMaxVersions`; docs `technical.md` regla 3, `claude/15`, `claude/13`, `claude/25`, `claude/08` si aplica. Tests Go. | openscad-dev | `backend/checkpoint/**`, `backend/mcp/**`, `backend/clichat/modes.go` (+ su test), `backend/clichat/agyperms_test.go`, `backend/render/errors_md.go`, `backend/prompts/**`, `backend/store/settings.go`, `backend/workspace/**`, `assets/**`, `.claude/rules/technical.md`, `claude/{08,13,15,25}.md` | ✅ |
| T2 | App e interfaz: bindings de historial, `checkpoint:updated` (paso de chat + watcher), `editedByAI` en el árbol, prompt CLI (ruta absoluta, archivo seleccionado, leer/editar), UI (indicador, menú Historial, «Deshacer cambio de la IA», «Ver diferencias» con `DiffViewer`, restaurar versión), ajuste del tope en Configuración, `CREATING_TOOLS`, i18n es/en (tú), pruebas de proveedores con CLIs falsos y Hermes simulado, uishot + help-shots (es/en) + `index.html`/`es/index.html`, CHANGELOG, `claude/34` nuevo + fila en `CLAUDE.md`, `claude/30`, `claude/26` (una línea). | fullstack-dev | `app*.go` (salvo nada de T1), `backend/watch/**`, `frontend/**`, `scripts/help-shots.sh`, `index.html`, `es/index.html`, `docs/screenshots/**`, `CHANGELOG.md`, `CLAUDE.md`, `claude/{26,30,34}.md` | ✅ |

T2 depende del API de T1 (`checkpoint.Update/History/ReadVersion/RestoreVersion/EditedByAI`): T1 lo entrega primero;
T2 empieza por el frontend y el prompt y conecta los bindings cuando `history.go` compile.

## Criterio de terminado

- «modifica 002-x.scad para…» → `update_checkpoint` sobre `002-x.scad`; el archivo cambia en el lugar, el historial tiene
  la versión anterior, ningún `.scad` nuevo. «hazme una variante» → `create_checkpoint`. Probado con CLIs falsos
  (Claude, Codex, Antigravity, OpenCode) contra el `--mcp` real donde la infraestructura de pruebas lo permite, y con Hermes
  y su servidor simulado. El prompt de cada uno lleva la regla y el archivo seleccionado.
- `update_checkpoint` rechazado en «Solo consulta» (CLI vía turnguard, Hermes sin la herramienta); aprobado en «Agente»
  para los cuatro CLIs; las reglas de agy lo incluyen tras sincronizar.
- Historial: guardar, tope, restaurar (que guarda la actual), deshacer, papelera ida y vuelta, purga, `EmptyTrash`, fuera
  del escaneo, nombres hostiles y enlaces.
- Verificación: `gofmt -l`, `go build ./...`, `go vet ./...`, `go test -race -timeout 20m ./...`, build de Windows,
  `pnpm build`, `pnpm test`, `./scripts/i18n-check.sh`, `uishot-fit.sh`, `uishot-persist.sh`, `wails build`.
- Capturas nuevas (historial y diff) en es y en, miradas.
