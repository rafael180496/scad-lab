# Motor integrado (OpenSCAD en WebAssembly, S11)

Spec: [.claude/specs/s11-motor-wasm-descarga.md](../.claude/specs/s11-motor-wasm-descarga.md).
Código: `backend/scadwasm` (el host), `backend/render` (`wasm.go` y los cambios en
`engine.go`/`artifact.go`), `backend/raster` (preview por software),
`assets/engines/` (el `.wasm` y las fuentes embebidos). Licencias y orígenes:
[THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).

## Qué es y cuándo se usa

El OpenSCAD oficial de openscad.org compilado a WebAssembly (**2025.12.30.wasm30277**,
Manifold + CGAL + lib3mf + fontconfig), ejecutado **dentro de Go con wazero** (Go
puro, sin CGO). Es el respaldo cuando `detect.OpenSCAD` no encuentra un nativo:
`render.Options.OpenSCADPath == ""` y `Options.WASM.Available()`. El nativo gana
siempre. Sirve al visor, a la exportación STL/3MF, a las partes por color y la
estimación, al linter y al servidor MCP. `Result.Engine` dice cuál corrió
(`native` | `wasm`; un `.json` de caché anterior sin el campo se lee como nativo).

```
render.Engine ──► runWASM / runArtifactWASM ──► scadwasm.Runner.Run(RunSpec)
                    montajes: /work (ws, RO)  /out (temporal)  /lib/N (RO)  /cache (memoria)  /fonts (embebido, RO)
```

## `scadwasm`

- **API**: `Version`, `Mount{Host, Virtual, ReadOnly}`, `RunSpec{Args, Cwd, Mounts, Env}`,
  `New(cacheDir)`, `Available()`, `Run(ctx, spec) (output, exitCode, err)`; más
  `Warmup()` (compilar en segundo plano), `Close()` y `ConvertOFFTo3MF`.
- **Un `Runner` por proceso**: compila el módulo la primera vez (2,5 s; 80 ms con
  la caché de wazero en `<datos>/cache/wasm`, **60 MB** en disco, regenerable) y
  cada `Run` es una instancia nueva con memoria propia, así que corre en paralelo.
  `Available()` descomprime el gzip embebido y **verifica el SHA-256**
  (`scadwasm.WasmSHA256`) antes de dar el motor por bueno.
- `err` es solo de fallo del motor (trap, cancelación, montaje inválido). Un
  `exit 1` de OpenSCAD es `exitCode`. `output` mezcla stdout+stderr en orden y
  trae las rutas virtuales reescritas a las del disco (`rewritePaths`), así
  `render.ParseOutput` y el linter ven lo mismo que con el nativo.

### El host de emscripten (lo que hace el `openscad.js` de JS, en Go)

El `.wasm` importa 153 funciones con nombres minificados (`a.a`…`a.Sb`).
`importmap.go` las traduce a las de emscripten (la tabla sale del
`wasmImports` del `openscad.js` de ESTA versión; `TestImportsAreCovered` falla si
una versión nueva trae una importación sin implementar).

- **Excepciones de C++ y `longjmp`** (modo JS de emscripten): `__cxa_throw`,
  `__resumeException`, `__emscripten_throw_longjmp` hacen `panic(throwSignal)`;
  cada `invoke_*` (~90, generados por firma) llama a la función de la tabla
  indirecta con `experimental/table.LookupFunction`, atrapa el throw, restaura la
  pila (`stackRestore`) y llama a `setThrew(1,0)`. El estado de la excepción
  (`exceptionLast`, pila de capturadas, `__cxa_find_matching_catch_N` con
  `__cxa_can_catch`) vive en `proc`. `exit()`/`abort()` son `panic` aparte que
  ningún `invoke_*` atrapa. Se decide con banderas de `proc` y no con
  `errors.As`: con miles de `invoke_*` anidados era cuadrático.
- **Funciones del host dentro de la tabla indirecta** (`exit`, `__cxa_throw`,
  `__cxa_rethrow`, `__cxa_end_catch`): wazero no sabe buscarlas con `LookupFunction`
  (nil pointer), así que `tableparse.go` lee la sección de elementos del `.wasm` y
  `tableLookup` las despacha a mano.
- **Sistema de archivos** (`vfs.go`): árbol virtual con raíz sintética y solo los
  montajes. Fuera de un montaje = `ENOENT`; escribir en uno `ReadOnly` o en la
  raíz = `EROFS`; las rutas se limpian en el espacio virtual, así `..` nunca sale
  de un montaje; los enlaces simbólicos del disco se siguen de forma transparente
  (`lstat` = `stat`) como con el nativo. Backends: directorio del disco
  (`hostBackend`, `filepath.Join` + `FromSlash`: rutas de Windows con letra de
  unidad), `fs.FS` (las fuentes) y memoria (`/cache`). Syscalls: `openat`, `stat*`,
  `getdents64` (dirent de 280 bytes como el glue), `mkdirat`, `renameat`,
  `unlinkat`, `fcntl64`, `ioctl` (stdout se hace pasar por TTY: sin eso musl lo
  deja en búfer completo y los avisos salen desordenados), `mmap` (copia a memoria
  alineada), `fd_*` de WASI.
- **Memoria**: `emscripten_resize_heap` replica el crecimiento del glue (+20 %).
  El módulo declara 256 páginas iniciales y 65 536 de máximo (4 GB).
- **Resto**: reloj (`clock_time_get`, `date_now`), zona horaria y `localtime`/`mktime`
  con `time.Local`, `random_get`, entorno (`environ_*`; `FONTCONFIG_FILE` apunta a
  `/fonts/fonts.conf`), `system()` = `ENOSYS`.
- **Fuentes**: `/fonts` siempre montado con Liberation Sans (4 variantes) y un
  `fonts.conf` propio; `/cache` en memoria para la caché de fontconfig. Sin esos
  dos, fontconfig imprime avisos en cada ejecución.

## Montajes y entorno de `render.WASMSpec`

Workspace → `/work` (cwd, solo lectura); carpeta temporal de salida → `/out`
(se crea dentro de la caché con prefijo `wasm-` y se borra siempre; `prune` limpia
las que dejó un proceso muerto); las carpetas de `OPENSCADPATH` del proceso y la
biblioteca de usuario de OpenSCAD (`~/Documents/OpenSCAD/libraries`, o
`~/.local/share/OpenSCAD/libraries`) que existan → `/lib/N` (máx. 8, solo lectura);
`OPENSCADPATH=/work:/work/lib:/lib/0:…`. La versión del wasm entra en la clave de
la caché de mallas como cualquier versión: una malla del integrado y una del
nativo no se mezclan. Con `Backend == "cgal"` se pasa `--backend=cgal` (sin
flag, este OpenSCAD usa Manifold).

## 3MF y color por parte: el 3MF nativo del wasm no funciona

El `lib3mf` del `.wasm` registra el callback de escritura con firma `(i32,i32,i32)→i32`
y `lib3mf` lo llama con `(i64,i64,i32)→()`: **cualquier** `-o x.3mf` (con o sin
color) muere con `indirect call type mismatch` en el `call_indirect` del propio
módulo. Es un defecto del binario oficial; ningún host lo arregla (la
especificación de WebAssembly exige el trap en ese `call_indirect`).

Lo que sí sale bien, con el color de cada cara, es **OFF**. `RenderArtifact` con el
integrado pide `-o /out/model.off` y `scadwasm.ConvertOFFTo3MF` lo convierte a un
3MF del estilo de OpenSCAD (un objeto, `<basematerials>` con un «Color N» por
`color()` en orden de aparición, material por triángulo) que `threemf.ReadFile` y
`threemf.Split` leen igual que el nativo. Las caras no triangulares (los
primitivos de OpenSCAD son cuadriláteros) se cortan por orejas sobre su plano
(sirve también para polígonos cóncavos de `polyhedron()`).
Límite: con algún `color()`, el OFF escribe las caras sin color con el amarillo
`#F9D72C` explícito; se lee como «sin color» (parte `Default`), así que un
`color("#F9D72C")` deliberado se confunde con el defecto.

## Cancelación y rendimiento

`WithCloseOnContextDone(true)`: cancelar el contexto corta la instancia aunque
esté en un cálculo (un `minkowski` de minutos no hace ninguna llamada al host
donde mirar una bandera: se probó, y se descartó la cancelación por sondeo).
Costo: wazero lanza una goroutine por **cada** llamada al módulo y cada `invoke_*`
hace dos (`stackSave` y la función), así que las cargas con muchas excepciones o
llamadas con limpieza (el evaluador del lenguaje) salen ~4-5× más lentas que sin
la opción. Las llamadas del host al módulo usan `proc.nctx` (contexto sin
cancelación; la vigila la llamada de `main`) y un pool de funciones de la tabla.

## Medidas (macOS arm64, Manifold, STL; nativo = OpenSCAD 2026.10.11)

Mediana de 5 renders sin caché de mallas por plantilla (ms):

| Plantilla | Nativo | WASM |
|---|---:|---:|
| box-with-lid | 42 | 89 |
| cable-clip | 46 | 108 |
| electronics-enclosure | 53 | 327 |
| gridfinity-bin | 53 | 441 |
| hinge-print-in-place | 59 | 526 |
| knob | 50 | 335 |
| nameplate-two-color | 58 | 428 |
| phone-stand | 42 | 55 |
| spur-gear | 49 | 269 |
| wall-bracket | 50 | 203 |

En frío (primer render tras arrancar el proceso, `box-with-lid`): nativo 89 ms;
WASM **2 542 ms** con la caché de compilación vacía y **263 ms** con ella. La
app precompila en segundo plano al abrir un proyecto
(`warmWASM`, desde `GetRenderState`), así que el primer render no paga la
compilación. Binario: **+10,5 MB** en macOS arm64 (28,7 → 39,3 MB) y +10,8 MB en
Windows amd64 (29,5 → 40,3 MB): 3,2 MB de wasm gzip + 1,7 MB de fuentes + wazero.

## Límites conocidos

- **Pila de 64 KB** en el `.wasm` oficial: una recursión profunda (≈1000 niveles
  de función o módulo) la desborda y corrompe datos estáticos (`bad_cast`,
  «out of bounds»). Se informa como error con esa pista; con un OpenSCAD nativo
  no pasa.
- Sin hilos (un núcleo por render) y sin PNG nativo (OpenGL): el `preview` del MCP
  rasteriza la malla en Go (`backend/raster`).
- 3MF vía OFF (arriba); AMF/OFF/OBJ sí funcionan, 3MF nativo no.
- Un enlace simbólico dentro del workspace que apunte afuera se sigue, igual que
  con el nativo (la protección es el montaje, no los enlaces).
- Probado en macOS; Windows compila (`GOOS=windows`) y las rutas de disco usan
  `filepath`, pero no se ejecutó allí.

## Actualizar la versión del wasm

Cambiar `openscad.wasm.gz`, `Version`, `WasmSHA256`, `THIRD_PARTY_NOTICES.md` y
regenerar `importmap.go` desde el `wasmImports` de su `openscad.js`; revisar los
nombres de los exports (`exp*` en `host.go`) y correr `go test ./backend/scadwasm`
(`TestImportsAreCovered`, `TestHostSlotsFinds…`).

## Pruebas

`backend/scadwasm` (cubo → STL cerrado, error de sintaxis con línea, include
desde `/lib/0`, `text()`, dos `color()` → dos partes, cancelación y plazo,
montaje de solo lectura, `..` no escapa, paralelo con `-race`, VFS, OFF,
triangulación); `backend/render/wasm_test.go`; `backend/scad/lint_engine_test.go`;
`backend/mcp/mcp_engine_test.go` y `app_engine_test.go` (sin OpenSCAD:
`SCADLAB_NO_SYSTEM_OPENSCAD=1`); `backend/raster` (las vistas ponen cada marca
del lado de la convención del preview nativo).
