# ScadLab — release Windows

Artefacto de distribución generado con `./scripts/package-windows.sh` (o
`./scripts/package-all.sh`). El workflow `.github/workflows/release.yml` **no
recompila**: publica exactamente el archivo versionado en esta carpeta y
comprueba que su SHA-256 aparezca en este README.

## Versión actual

| Campo | Valor |
|---|---|
| Versión | **0.1.0** (la primera) |
| Archivo | `ScadLab-v0.1.0-windows-amd64.exe` (portable) |
| Tamaño | 48 761 344 bytes (46,5 MB) |
| SHA-256 | `0216d858ef5aacb20124fe2add54def5cf27f08b4f5a0da18a0ba7f7671fa87a` |
| Arquitectura | x86-64 (amd64) |
| Descarga | https://github.com/rafael180496/ScadLab/releases/download/v0.1.0/ScadLab-v0.1.0-windows-amd64.exe |

### Compatibilidad verificada

`file` sobre el binario:

```
ScadLab-v0.1.0-windows-amd64.exe: PE32+ executable (GUI) x86-64, for MS Windows
```

Compilado sin CGO (`CGO_ENABLED=0`, SQLite puro Go). Windows 10/11 de 64 bits con el
runtime **WebView2** (viene con Windows 11 y con las versiones recientes de Windows 10;
si falta, el sistema lo pide).

### ⚠ No verificado en Windows real

**Este `.exe` se cross-compiló desde macOS y nadie lo ha ejecutado todavía en una
Windows real.** «Compila limpio» solo prueba eso. Quedan **sin confirmar**:

- **WebView2:** que la ventana abra y que WebGL funcione en el visor 3D.
- **DPI y diálogos nativos:** escalado en pantallas de alta densidad y los
  selectores de carpeta y archivo.
- **La terminal (ConPTY):** que los agentes CLI corran dentro de la terminal
  integrada.
- **«Descargar OpenSCAD»:** la descarga, la verificación SHA-256 y la extracción del
  paquete de Windows (40 MB).
- **El motor integrado (WebAssembly)** y la **cancelación de renders**.

Si lo pruebas, anota aquí en qué versiones de Windows y qué se confirmó.

### Firma

**Sin firma Authenticode.** SmartScreen mostrará «Windows protegió su PC»: pulsa
**Más información** → **Ejecutar de todas formas**.

## Instalación

No hay instalador: el `.exe` es portable.

1. Descarga `ScadLab-v0.1.0-windows-amd64.exe` desde el Release.
2. Guárdalo donde quieras (por ejemplo `C:\Herramientas\ScadLab\`).
3. Haz doble clic. Si aparece SmartScreen: **Más información** → **Ejecutar de todas formas**.

Tus datos van en `%AppData%\ScadLab\` y tus modelos, en las carpetas de tus workspaces.

## Requisitos de ejecución

- **OpenSCAD es opcional.** Sin él, ScadLab renderiza con su **motor integrado**
  (OpenSCAD 2025.12.30 en WebAssembly, más lento) y ofrece **Descargar OpenSCAD**
  en Configuración → OpenSCAD. Un OpenSCAD instalado (2021.01 o superior; se
  recomienda un snapshot 2025 para 3MF con colores y el backend Manifold) se usa con
  preferencia.
- Opcional: Claude Code, opencode, Codex, Antigravity u Ollama para el diseño
  asistido por IA.

## Regenerar este artefacto

```bash
cat VERSION                          # 0.1.0
./scripts/package-all.sh             # .dmg + .exe en build/bin/ (cross-compila, sin Windows)
cp build/bin/ScadLab-v0.1.0-windows-amd64.exe releases/windows/
shasum -a 256 releases/windows/ScadLab-v0.1.0-windows-amd64.exe
```

Una recompilación no da un binario idéntico bit a bit: si se regenera, hay que
actualizar el SHA-256 de arriba.
