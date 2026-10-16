# S11 — Motor integrado (OpenSCAD WASM) y descarga de OpenSCAD

Pedido del usuario: «el visor 3D debe seguir funcionando aunque no tenga
OpenSCAD instalado», opción **WASM + descarga**. Ampliación: sin OpenSCAD
también deben funcionar **exportar STL y 3MF** (color por parte, compatible con
Bambu Studio) y la **estimación de peso y costo**.

## Decisiones

### 1. El WASM corre en Go (wazero), no en un Web Worker

Spike del architect (2026-10-16) con la build oficial de openscad.org
`OpenSCAD-2025.12.30.wasm30277-WebAssembly-web.zip`:

- `openscad.wasm` 10,5 MB (3,2 MB con gzip -9), Manifold incluido (backend por
  defecto), Lib3MF incluido (3MF con color), fontconfig con `<dir>/fonts</dir>`.
- Sin pthreads (no hay `SharedArrayBuffer` ni `Worker` en el glue), pero usa
  instrucciones atómicas: wazero necesita `experimental.CoreFeaturesThreads`.
- 153 imports minificados (`a.a`…`a.Sb`) que el glue JS mapea a ~60 funciones
  reales de emscripten (syscalls de FS, `__cxa_*` de excepciones C++ por JS,
  `__emscripten_throw_longjmp`, `invoke_*`, reloj, `environ_*`, `fd_*`) más
  ~90 `invoke_*`. La tabla de mapeo se saca del `var wasmImports={…}` del
  `openscad.js` de **esa** versión y queda fija en el código (la versión está
  fijada, así que la tabla no cambia).
- wazero v1.12 (Go puro, sin CGO) compila el módulo en **2,3 s** la primera
  vez y **81 ms** con `NewCompilationCacheWithDir`.

Por qué Go y no un Worker del frontend: un solo motor sirve al visor, a la
exportación STL/3MF (que corre en Go), a la estimación (partes por color vía
3MF + `threemf.Split`), al linter y al **servidor MCP, que corre sin ventana**.
Con un Worker, la exportación tendría que ida-y-vuelta por el frontend y el MCP
quedaría sin motor. Además reutiliza todo lo que ya hay: mismos argumentos de
CLI, mismo parser de `ERROR:`/`WARNING:` (diagnósticos con línea → marcadores
de Monaco), misma caché por contenido (la versión `2025.12.30.wasm30277` entra
en la clave, así que una malla del WASM y una del nativo no se mezclan),
cancelación por contexto (`WithCloseOnContextDone`). Costo: hay que escribir el
host de emscripten en Go (FS con montajes, excepciones, longjmp).

### 2. MCP

Usa el mismo respaldo: `render`, `measure`, `lint`, `export` funcionan con el
WASM. `preview` (PNG) no puede: el PNG de OpenSCAD necesita OpenGL offscreen.
Con WASM, `preview` rasteriza la malla en Go (software, z-buffer, sombreado
plano, color por parte, mismas vistas y tamaño). El texto de la respuesta dice
que es el motor integrado.

### 3. PNG en la app

El botón PNG de la cabecera ya captura el visor three.js: funciona igual sin
OpenSCAD. No se toca.

### 4. Versión fijada

**Snapshot 2025.12.30** para todo (WASM integrado y descarga), única fecha de
2025 con dmg, zip de Windows y WebAssembly publicados:

| Archivo | SHA-256 (de openscad.org, `.sha256`) |
|---|---|
| `OpenSCAD-2025.12.30.wasm30277-WebAssembly-web.zip` | `8b3b20dfd91d2e949d9e8dea7f8aca9917fc415a9d9d8190f706e19721b23f33` |
| `openscad.wasm` (dentro del zip) | `1db0bee42c5cf7e4a72208f7d85fc53c12be0980fd98b9f1dbd5e9dcdc732f72` |
| `OpenSCAD-2025.12.30.dmg` (62 687 889 B) | `fbc17e223979884fc17a4bc19b76a50c8797646cbc2f3083cd407389d90a1060` |
| `OpenSCAD-2025.12.30-x86-64.zip` (41 511 427 B) | `f04d315b1f1fbe20b363383f45ea8ed65713fe9913b4368d90a94175b71423cb` |

URL base: `https://files.openscad.org/snapshots/`.

### 5. Licencia

OpenSCAD es GPL (v2 o posterior; con CGAL, el binario combinado es GPLv3+).
ScadLab es MIT. El `.wasm` se distribuye **sin modificar**, como programa
aparte (recibe argv y archivos, igual que el ejecutable nativo por `exec`):
agregado, no obra derivada. `THIRD_PARTY_NOTICES.md` en la raíz nombra la
versión, el SHA-256, el origen, la licencia y dónde está el código fuente
correspondiente (tag/commit del snapshot en github.com/openscad/openscad), y lo
mismo para las fuentes Liberation (SIL OFL 1.1) y wazero (Apache-2.0). El
OpenSCAD descargado no lo distribuye ScadLab: lo baja el usuario de openscad.org.

### 6. Reglas técnicas afectadas

Regla 7 de `technical.md` («OpenSCAD es externo») se amplía: el nativo sigue
siendo externo y preferido; el WASM integrado es el respaldo. Binario: +3,2 MB
de wasm gzip + fuentes + wazero; sigue lejos de 100 MB.

## Tareas

| # | Tarea | Dueño | Paralelo |
|---|---|---|---|
| 11.1 | `backend/scadwasm`: runner wazero + host emscripten + fuentes + licencia | openscad-dev | A |
| 11.2 | Integración del respaldo: `render`, `scad` (lint), `mcp` (+ rasterizador del preview), `app_render/app_editor/app_parts/app_export.go` | openscad-dev | A (tras 11.1) |
| 11.3 | Descarga de OpenSCAD: `backend/scaddownload`, `detect` (gestionado primero), bindings, evento | fullstack-dev | B |
| 11.4 | UI: Configuración → OpenSCAD, aviso del panel de render, barra de estado, i18n, ayuda, capturas, CHANGELOG | fullstack-dev | B (tras 11.3) |
| 11.5 | Integración, medidas, prueba de humo con OpenSCAD ausente | architect | final |

### Alcance (archivos) — sin solape

- **openscad-dev**: `backend/scadwasm/**` (nuevo), `assets/engines/**` (nuevo:
  wasm gz + fuentes), `backend/render/**` (sin cambiar lo que ya fijó el
  architect en `types.go`), `backend/scad/lint.go`, `backend/mcp/**`,
  `backend/raster/**` (nuevo, si separa el rasterizador), `app_render.go`,
  `app_editor.go`, `app_parts.go`, `app_export.go`, `main_mcp.go`, `go.mod`,
  `go.sum`, `THIRD_PARTY_NOTICES.md`, `claude/17-motor-wasm.md`.
- **fullstack-dev**: `backend/scaddownload/**` (nuevo), `backend/detect/**`,
  `app_openscad.go` (nuevo), `app_settings.go`, `frontend/**` (incluye
  `wailsjs/`, `i18n/`, `uishot.tsx`), `scripts/help-shots.sh`, `index.html`,
  `es/index.html`, `docs/screenshots/**`, `CHANGELOG.md`,
  `claude/18-descarga-openscad.md`, `claude/06-go-react-contract.md`.
- **architect**: `claude/01-roadmap.md`, `CLAUDE.md`, `.claude/rules/technical.md`,
  `claude/08-openscad.md`.

## Contratos

### Ya fijado (architect)

- `render.Result.Engine string` (`json:"engine"`): `render.EngineNative` |
  `render.EngineWASM`; `""` si no corrió (missing).
- `func (a *App) resetOpenSCADCache()` en `app_render.go`.

### `backend/scadwasm` (11.1)

```go
const Version = "2025.12.30.wasm30277" // lo que imprime --version
type Mount struct{ Host, Virtual string; ReadOnly bool }
type RunSpec struct {
    Args   []string          // argv sin argv[0], ya con rutas virtuales
    Cwd    string            // virtual
    Mounts []Mount
    Env    map[string]string // p. ej. OPENSCADPATH con rutas virtuales
}
type Runner struct{ /* runtime + módulo compilado, perezoso, seguro concurrente */ }
func New(cacheDir string) *Runner          // cacheDir = <datos>/cache/wasm (caché de compilación)
func (r *Runner) Available() bool          // el wasm embebido está y verifica su SHA-256
func (r *Runner) Run(ctx context.Context, spec RunSpec) (output string, exitCode int, err error)
```

- `output` = stdout+stderr intercalados, con las rutas virtuales de los
  montajes reescritas a las del host (los diagnósticos quedan iguales que con
  el nativo). `err` solo para fallos del motor (trap, sin memoria), no para un
  `exit 1` de OpenSCAD.
- `/fonts` siempre montado con las fuentes embebidas (Liberation Sans), para
  que `text()` funcione.
- Montajes fuera de la lista = `ENOENT`; `ReadOnly` = `EROFS` al escribir. Un
  `..` no sale del montaje.
- Instancia nueva por `Run` (memoria independiente): seguro en paralelo; el
  tope de 2 simultáneos de `render` sigue aplicando.

### `backend/render` (11.2)

- `Options.WASM *scadwasm.Runner` (o una interfaz equivalente). Si
  `OpenSCADPath == ""` y `WASM != nil && WASM.Available()`: render con WASM,
  `Result.Engine = EngineWASM`, `Version = scadwasm.Version`,
  `Backend = "manifold"`. Si ambos faltan: `StatusMissing` como hoy.
- Mismo para `RenderArtifact` (3MF con `-O export-3mf/color-mode=model`).
- `Result.Engine = EngineNative` en todo render nativo (también en hits de
  caché; un `.json` viejo sin `engine` se lee como nativo).
- Montajes: workspace → `/work` (cwd, solo lectura), carpeta temporal de
  salida → `/out`, `lib/` del workspace y cada directorio de OPENSCADPATH que
  use hoy el motor/linter → `/lib/N`, `OPENSCADPATH=/work:/work/lib:/lib/0:…`.
- Lint (`backend/scad`): misma regla para el chequeo de sintaxis `-o x.ast`.
- MCP: `env.openscad` devuelve también el runner; las herramientas que hoy
  dicen «OpenSCAD no está instalado» funcionan con WASM salvo `preview`, que
  usa el rasterizador. El texto de `render`/`preview` indica el motor.

### `backend/scaddownload` y bindings (11.3)

```go
// backend/scaddownload
type Asset struct{ OS, Arch, URL, SHA256, Kind string; Size int64 } // Kind: "dmg" | "zip"
func ForPlatform(goos, goarch string) (Asset, bool) // darwin/arm64, darwin/amd64, windows/amd64
func InstallDir(dataDir string) string             // <datos>/tools/openscad/2025.12.30
func InstalledBinary(dataDir string) string         // ruta del ejecutable o ""
type Progress struct{ Phase string; Received, Total int64 } // download|verify|extract|done
func Install(ctx context.Context, dataDir string, a Asset, onProgress func(Progress)) (string, error)
func Remove(dataDir string) error
```

```go
// app_openscad.go
type OpenSCADDownload struct {
    Version       string `json:"version"`
    SizeBytes     int64  `json:"sizeBytes"`
    Supported     bool   `json:"supported"`
    Installed     bool   `json:"installed"`
    InstalledPath string `json:"installedPath"`
    Running       bool   `json:"running"`
    WasmVersion   string `json:"wasmVersion"` // scadwasm.Version (para la UI)
}
func (a *App) GetOpenSCADDownload() (OpenSCADDownload, error)
func (a *App) DownloadOpenSCAD() (OpenSCADDownload, error) // bloquea hasta terminar; error i18n
func (a *App) CancelOpenSCADDownload()
func (a *App) RemoveDownloadedOpenSCAD() (OpenSCADDownload, error)
```

Evento `openscad:download` (Go → React):
`{phase: "download"|"verify"|"extract"|"done"|"error"|"cancelled", received, total, message}`.

- Detección (`detect.OpenSCAD`): ruta fijada por el usuario → OpenSCAD
  gestionado (`<datos>/tools/openscad/…`) → PATH → rutas de instalación.
  `detect.Tool` gana `Source string` (`"custom"|"managed"|"system"`).
- `SCADLAB_NO_SYSTEM_OPENSCAD=1` (solo pruebas) hace que `detect` ignore el
  PATH y las rutas del sistema: así se simula «sin OpenSCAD» sin desinstalar.
- macOS: `hdiutil attach -nobrowse -readonly -noautoopen -mountpoint <tmp>`,
  copiar `OpenSCAD.app` con `ditto`, `hdiutil detach`. El cliente HTTP de Go no
  pone `com.apple.quarantine`; igual se quita con `xattr -dr` si estuviera.
  Se valida corriendo `--version` antes de dar por instalado.
- Windows: `archive/zip` a la carpeta, `openscad.exe` buscado dentro.
- Escritura a temporal + renombre atómico de la carpeta final: una descarga
  cancelada o fallida no deja nada a medias.
- Errores claros es/en: sin red, HTTP ≠ 200, checksum distinto, sin espacio
  (comprobar espacio libre antes y `ENOSPC`/`ERROR_DISK_FULL`), plataforma no
  soportada. Sin `golang.org/x/sys` (no tocar `go.mod`): `syscall` de la stdlib.

## UI (11.4)

- Render con `engine === "wasm"`: aviso **informativo** (no de error) en el
  panel de render: «Renderizado con el motor integrado (OpenSCAD 2025.12.30,
  WebAssembly) porque no se encontró OpenSCAD instalado. Descarga OpenSCAD
  para renders más rápidos.» + botón «Descargar OpenSCAD» con progreso y
  Cancelar. Barra de estado: insignia «WASM 2025.12.30» con `title` del motivo.
- Configuración → OpenSCAD: estado (nativo / descargado / motor integrado),
  botón Descargar con barra de progreso, Cancelar, Quitar la descarga, enlace a
  la licencia del motor integrado.
- Al terminar la descarga se vuelve a renderizar el checkpoint activo.
- Capturas nuevas (banco, datos inventados): `render-wasm`,
  `settings-openscad-download`. Tema de ayuda en `index.html` y
  `es/index.html`. Entrada en `CHANGELOG.md`.

## Criterio de terminado

- Con `SCADLAB_NO_SYSTEM_OPENSCAD=1` y un `SCADLAB_DATA_DIR` temporal: el visor
  muestra la malla con el motor WASM, un error de sintaxis llega con su línea,
  la exportación STL (malla cerrada) y 3MF (una parte por color) funcionan, la
  estimación de peso y costo da valores, el MCP renderiza y exporta.
- Tiempos WASM vs nativo de las 10 plantillas y tamaño del binario medidos.
- Verificación completa de la fase (ver el pedido): `gofmt -l .`, build, vet,
  `go test -race ./...`, cross-compilación a Windows, `pnpm build`,
  `i18n-check.sh`, `uishot-fit.sh`, `uishot-persist.sh`, `wails build`.
