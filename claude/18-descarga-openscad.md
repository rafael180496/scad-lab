# Descarga de OpenSCAD (S11)

Plan: [.claude/specs/s11-motor-wasm-descarga.md](../.claude/specs/s11-motor-wasm-descarga.md).
Complementa al motor integrado ([17-motor-wasm.md](17-motor-wasm.md)): el integrado
mantiene la app usable sin OpenSCAD; esto le da al usuario, con un clic, el nativo
(más rápido) sin salir de ScadLab. Bindings y evento: [32](32-contrato-exportacion-skills.md#descarga-de-openscad--app_openscadgo-s11).

## Qué se descarga

**Snapshot 2025.12.30**, fijado en el código (`backend/scaddownload/scaddownload.go`) con
el SHA-256 que publica openscad.org. Es la misma versión que el motor integrado.

| Plataforma | Archivo | Tamaño | Tipo |
|---|---|---|---|
| macOS arm64 **y** amd64 | `OpenSCAD-2025.12.30.dmg` | 62 687 889 B | `dmg` |
| Windows amd64 | `OpenSCAD-2025.12.30-x86-64.zip` | 41 511 427 B | `zip` |

Base: `https://files.openscad.org/snapshots/`. Otra plataforma (Linux, Windows arm64):
`ForPlatform` devuelve `false` y la UI no ofrece el botón (`supported: false`).

- **El dmg es universal.** Comprobado con `lipo -info` sobre el binario real:
  `x86_64 arm64`. Por eso hay una sola fila de hash para las dos arquitecturas de macOS.
- **Instalado ocupa** ~146 MB en macOS y ~120 MB en Windows. Antes de bajar se exige
  `Size × 5` de espacio libre (`spaceFactor`): archivo + contenido extraído + margen.
- Subir de versión = cambiar `Version`, `Size`, la URL y el SHA-256 de la tabla **y**
  `scadwasm.Version` si se quiere seguir alineados. El SHA-256 sale del `.sha256` que
  publica openscad.org junto a cada archivo, no de uno calculado sobre lo que se baja.

## Dónde queda

`<datos>/tools/openscad/2025.12.30/` (`scaddownload.InstallDir`). `InstalledBinary` es el
ejecutable, o `""` si no hay instalación completa:

| SO | Ejecutable |
|---|---|
| macOS | `OpenSCAD.app/Contents/MacOS/OpenSCAD` |
| Windows | `openscad.exe`, buscado hasta dos niveles (el zip trae una carpeta raíz `OpenSCAD-2025.12.30-x86-64/`) |

En Windows se usa `openscad.exe` y no `openscad.com` (el lanzador de consola que trae el
zip): mismo ejecutable que el instalado en Archivos de programa, y si el `.com` es un proceso
aparte que lanza al `.exe`, cancelar un render mataría solo al lanzador. **Sin verificar en una
Windows real** (ver abajo).

## Orden de detección

`detect.OpenSCAD(ctx, override)`:

```
ruta fijada por el usuario (Configuración)   → Tool.Source = "custom"
→ OpenSCAD descargado (<datos>/tools/…)      → "managed"
→ PATH y rutas de instalación del SO         → "system"
→ nada                                       → Found=false (el motor integrado toma el relevo)
```

El descargado va **antes** que el del sistema porque es la versión que ScadLab verificó;
quien quiere otro lo fija en Configuración. `detect` calcula `<datos>` sin crearlo
(misma regla que `appdata.Dir`, incluida `SCADLAB_DATA_DIR`): detectar no deja carpetas.

`SCADLAB_NO_SYSTEM_OPENSCAD=1` (**solo pruebas**) hace que `detect` ignore el PATH y las
rutas del sistema; la ruta fijada y el descargado siguen valiendo. Con un
`SCADLAB_DATA_DIR` temporal simula «sin OpenSCAD» sin desinstalar el del usuario.

## Cómo se instala (`scaddownload.Install`)

Todo ocurre en una carpeta temporal `.tmp-XXXX` **hermana** de la final (mismo volumen →
renombre atómico). Una descarga cancelada o fallida no deja nada, ni siquiera la carpeta
`tools/openscad` vacía.

1. **Espacio libre** (`freeBytes`: `Statfs` en unix, `GetDiskFreeSpaceExW` por
   `syscall.NewLazyDLL` en Windows; sin `golang.org/x/sys`). Falla antes de pedir nada.
2. **Descarga** con un `http.Client` sin plazo total —60 MB por una conexión lenta es legítimo—
   pero con corte por **inactividad**: 60 s sin un byte = «se detuvo» (`stallTimeout`). El
   SHA-256 se calcula mientras se escribe.
3. **Verificación**: hash distinto → `ErrChecksum`, no se instala nada.
4. **Extracción**:
   - macOS: `hdiutil attach -nobrowse -readonly -noautoopen -mountpoint <tmp>` →
     `ditto OpenSCAD.app` → `hdiutil detach` (con `-force` si falla; corre aunque se haya
     cancelado el contexto, para no dejar un volumen montado).
   - Windows: `archive/zip`, rechazando rutas fuera del destino (zip slip), enlaces y más de
     3 GB descomprimidos.
5. **Cuarentena (macOS)**: el cliente HTTP de Go **no** pone `com.apple.quarantine` (la
   ponen los navegadores), así que Gatekeeper no interviene. Igual se corre
   `xattr -dr com.apple.quarantine` por si el dmg la trajera.
6. **Validación**: se corre `openscad --version` sobre lo extraído y debe decir `2025.12.30`
   (salida vacía con código 0 se acepta: un ejecutable gráfico de Windows puede no escribir
   a una tubería).
7. **Renombre atómico** a `InstallDir`. Una instalación previa se aparta primero y vuelve si
   el renombre falla.

`Progress{Phase, Received, Total}` va **acotado a ~10 avisos por segundo** (`throttle`); los
cambios de fase siempre pasan. La extracción de un dmg no informa avance (`Total` = 0): la UI
muestra una barra indeterminada.

## Errores (es/en)

Cada uno con su identidad para `errors.Is`: `ErrNetwork` (sin conexión o corte),
`ErrHTTP` (≠ 200), `ErrChecksum`, `ErrNoSpace` (chequeo previo, `ENOSPC`/`EDQUOT`/
`ERROR_DISK_FULL`), `ErrUnsupported`. Cancelar devuelve `context.Canceled`; el binding lo
convierte en un error con código `download-cancelled` que la UI no muestra como fallo.

## Bindings y estado

`app_openscad.go`. Una sola descarga a la vez (el estado vive en un registro por `App`, como
el chat). Al terminar o quitar llama a `resetOpenSCADCache()` (`app_render.go`): el próximo
render vuelve a detectar y encuentra el descargado. Cerrar la app cancela la descarga en curso.
`RemoveDownloadedOpenSCAD` borra todo `tools/openscad/` y se rechaza con una descarga corriendo.

## Interfaz

- **Estado compartido**: `hooks/useOpenSCADDownload.ts` es un store de módulo (`createStore`)
  alimentado por el evento `openscad:download` y por los bindings; el aviso del panel de render y
  Configuración muestran la **misma** descarga. `App` lo engancha con
  `useOpenSCADDownloadSync()` (sin leer el estado: no redibuja la ventana en cada aviso).
- **Re-render al terminar**: el store tiene un `epoch` que sube al instalar o quitar; `App`
  reacciona volviendo a renderizar el checkpoint activo (la clave de caché lleva motor y versión:
  las mallas del WASM y del nativo no se mezclan) y repitiendo la detección de la barra de estado.
- `components/render/DownloadControls.tsx` (botón → progreso + Cancelar), `WasmNotice.tsx` (aviso
  del panel cuando `result.engine === 'wasm'`), insignia «WASM 2025.12.30» en `StatusBar`, y
  `components/settings/OpenSCADSettings.tsx` (motor en uso, descargar, quitar con
  `confirmDialog`, aviso de licencia). El aviso es **informativo**, no un error.
- Banco de capturas: vistas `render-wasm` y `settings-openscad-download`.

## Pruebas

- `backend/scaddownload`: `httptest` con un zip falso y su SHA-256 inyectado (la tabla real queda
  fija; `Install` recibe un `Asset` de prueba), checksum malo, HTTP 404, sin red, cancelación a
  mitad, corte por inactividad, sin espacio, zip slip, sin ejecutable, validación fallida,
  reinstalar y que una reinstalación fallida conserve la anterior, y **nada de basura tras fallar**.
  `TestInstallDMG` (solo macOS) crea un dmg de verdad con `hdiutil`.
- `TestRealInstall` (`SCADLAB_REAL_DOWNLOAD=1`, ~60 MB): baja el dmg real a una carpeta temporal,
  verifica `--version`, renderiza un cubo a STL y lo borra todo. Es la prueba de punta a punta.
- `backend/detect`: descargado vs PATH vs ruta fijada vs `SCADLAB_NO_SYSTEM_OPENSCAD`, y que
  detectar no crea carpetas.
- `app_openscad_test.go`: flujo, una sola descarga a la vez, cancelar, error y plataforma sin soporte.

## Pendiente de probar a mano

- **Windows real**: el zip se listó (`openscad.exe` dentro de `OpenSCAD-2025.12.30-x86-64/`) y el
  código cross-compila, pero la extracción y `openscad.exe --version` por tubería no se ejecutaron
  en Windows.
- Un Mac que haya **descargado el dmg con un navegador** (con cuarentena) no es el caso: lo baja
  ScadLab. Si Gatekeeper llegara a objetar el binario, `xattr -dr com.apple.quarantine` ya se corre.
