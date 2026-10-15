# S1 — Motor de render OpenSCAD

Fase del [roadmap](../../claude/01-roadmap.md). Objetivo: cada checkpoint se
puede renderizar a STL con OpenSCAD, con caché, cancelación, diagnósticos con
línea, `RENDER_ERRORS.md` para los agentes, y la UI se entera sola cuando un
agente crea un `.scad` nuevo.

## Reparto (en paralelo, sin archivos compartidos)

| Tarea | Dueño | Alcance (único que puede tocar) |
|---|---|---|
| 1.1–1.3 motor, caché, parser, RENDER_ERRORS.md | openscad-dev | `backend/render/**` (excepto `types.go`), `claude/08-openscad.md` |
| 1.4–1.5 watcher, bindings, eventos, UI, settings | fullstack-dev | `backend/watch/**`, `backend/store/settings.go`, `app.go`, `app_render.go`, `app_workspaces.go`, `frontend/**`, `claude/06-go-react-contract.md`, `CHANGELOG.md`, `index.html`, `go.mod`/`go.sum` |

`backend/render/types.go` es el contrato y **no lo cambia nadie** sin pasar por
el architect. Hasta que openscad-dev termine, `engine_stub.go` hace compilar
al fullstack-dev; openscad-dev lo borra.

## Contrato

### Paquete `render` (types.go)

`NewEngine(cacheDir)`, `Engine.Render(ctx, Request, Options) (Result, error)`,
`Engine.Cancel(key)`, `Engine.Close()`, `WriteRenderErrors(dir, Result)`,
`ClearRenderErrors(dir)`. Tipos `Request`, `Options`, `Result`, `Diagnostic`,
constantes `Status*` y `Severity*`.

### Comportamiento del motor (openscad-dev)

- `openscad -o <tmp>.stl --export-format binstl [--backend=manifold] <archivo>`
  con `cwd` = WorkspaceDir, `exec.CommandContext`, `hideWindow` en Windows,
  plazo `TimeoutSeconds` (default 300). El STL se escribe en un temporal y se
  renombra a la caché solo si el render salió bien (nunca una malla a medias).
  Si la versión no acepta `--export-format binstl`, sin esa opción.
- **Manifold**: si OpenSCAD rechaza `--backend` (versiones viejas), reintentar
  sin él y devolver `Backend: "cgal"`. Recordar el resultado por ruta+versión
  para no reintentar cada vez.
- **Caché**: clave = sha256(contenido del `.scad` + contenido de cada
  `include <…>`/`use <…>` resuelto recursivamente relativo al archivo, al
  workspace y a `lib/` + backend + versión de OpenSCAD). Archivo
  `<cacheDir>/<clave>.stl` + `<clave>.json` con el `Result` (diagnósticos
  incluidos, así un hit devuelve también los warnings). `CacheHit: true`.
  Límite de tamaño de la caché: 1 GB, se poda por antigüedad de acceso.
- **Cola por WorkspaceKey**: un `Render` nuevo cancela el anterior de la misma
  clave; el cancelado devuelve `StatusCancelled`.
- **Parser** (`parse.go`, con tests de tabla): líneas `ERROR:` / `WARNING:` /
  `DEPRECATED:` (→ warning) con ubicación en los formatos que usan 2021.01 y
  2025 (`in file "x.scad", line 12`, `in file x.scad, line 12`, `, line 12`,
  `at line 12`). Un `ERROR` sin ubicación igual se reporta (línea 0). Modelo
  vacío (`Current top level object is empty` / `top-level object is empty`) →
  `StatusError` con diagnóstico explicativo.
- `OpenSCADPath == ""` → `StatusMissing` sin ejecutar nada.
- `RENDER_ERRORS.md`: título, archivo, fecha, lista de errores con línea, el
  tramo de código alrededor de cada línea (±3), los warnings, y la instrucción
  «corregir en un checkpoint NUEVO cuyo parent sea <archivo>». Textos en el
  idioma activo (`i18n.Msg`).
- Tests con un **openscad falso** (script sh generado en `t.TempDir()`, que
  escribe la salida según el contenido del `.scad`); `t.Skip` en Windows.

### Bindings nuevos (fullstack-dev, `app_render.go`)

| Binding | Devuelve | Nota |
|---|---|---|
| `RenderCheckpoint(id, fileName)` | `render.Result` | escribe/borra `RENDER_ERRORS.md` según el estado; emite eventos |
| `CancelRender(id)` | — | |
| `GetRenderState(id)` | `map[fileName]render.Result` | últimos resultados en memoria del workspace (para pintar el árbol al reabrir) |

Settings nuevos (sin migración, key/value): `autoRender bool` (default `true`),
`renderTimeoutSec int` (default 300). Validar 10–3600.

### Eventos Wails (Go → React)

| Evento | Payload |
|---|---|
| `render:started` | `{workspaceId, fileName}` |
| `render:finished` | `{workspaceId, result}` |
| `checkpoints:changed` | `WorkspaceView` |

### Watcher (`backend/watch`)

- `fsnotify` (Go puro) sobre la raíz del workspace abierto (no recursivo).
  Debounce 300 ms. Ignora todo lo que no sea `.scad`, y `RENDER_ERRORS.md`.
- Al cambiar: rescan → `checkpoints:changed`. Si apareció un `.scad` **nuevo**
  y `autoRender`, se renderiza ese (el más alto por número si llegaron varios).
- Un solo workspace vigilado en S1 (abrir otro detiene el anterior); S8 lo
  extiende. Se detiene en `shutdown`.

### UI

- Árbol: indicador por checkpoint (renderizando = spinner, ok = punto verde,
  warning = ámbar, error = rojo), `title` con el resumen.
- Panel del visor: botón «Renderizar» (y «Cancelar» mientras corre), barra de
  estado con duración, «desde caché», backend usado; si falla, lista de
  diagnósticos (línea + mensaje) y el log plegable. Si `StatusMissing`, aviso
  con enlace a Configuración → OpenSCAD. El visor 3D en sí es S2: acá se
  muestra el estado y el tamaño de la malla.
- Configuración → OpenSCAD: toggle «Renderizar automáticamente los checkpoints
  nuevos» y tiempo máximo.
- Textos en `es.ts`/`en.ts`. Entrada en `CHANGELOG.md` `[Unreleased]`, y en
  `index.html` quitar «En desarrollo» de lo que corresponde al render y
  documentar el estado/diagnósticos y `RENDER_ERRORS.md`.

## Criterio de terminado

`go build ./... && go vet ./... && go test ./...`, `cd frontend && pnpm build`,
`node frontend/scripts/i18n-check.mjs`, `GOOS=windows go build ./...`.
Si OpenSCAD está instalado, render real de `assets/skills/openscad-templates/templates/*.scad`.
