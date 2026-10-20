# ScadLab — release Windows

Artefacto de distribución generado con `./scripts/package-windows.sh` (o
`./scripts/package-all.sh`). El workflow `.github/workflows/release.yml` **no
recompila**: publica exactamente el archivo versionado en esta carpeta y
comprueba que su SHA-256 aparezca en este README.

## Versión actual

| Campo | Valor |
|---|---|
| Versión | **0.2.2** |
| Archivo | `ScadLab-v0.2.2-windows-amd64.exe` (portable) |
| Tamaño | 52 028 928 bytes (49,6 MB) |
| SHA-256 | `705ffccf23ecb0db0cb2ef63a983de757315d0b82d1b882fa0c71a49d4251262` |
| Arquitectura | x86-64 (amd64) |
| Descarga | https://github.com/rafael180496/scad-lab/releases/download/v0.2.2/ScadLab-v0.2.2-windows-amd64.exe |

### Compatibilidad verificada

`file` sobre el binario:

```
ScadLab-v0.2.2-windows-amd64.exe: PE32+ executable (GUI) x86-64, for MS Windows
```

Compilado sin CGO (`CGO_ENABLED=0`, SQLite puro Go). Windows 10/11 de 64 bits con el
runtime **WebView2** (viene con Windows 11 y con las versiones recientes de Windows 10;
si falta, el sistema lo pide).

### Verificación en Windows

**Verificado por el usuario en Windows 10 y 11 en la v0.2.0; la 0.2.2 no se probó todavía
en Windows.** El `.exe` se cross-compila desde macOS (sin Windows de por medio) y solo se
confirmó que compila limpio. Quedan sin confirmar en esta versión, corriéndola en una
Windows real: el runtime WebView2, el escalado DPI y los diálogos nativos. La verificación
de la v0.2.0 no se extiende a esta versión.

### Firma

**Sin firma Authenticode.** SmartScreen mostrará «Windows protegió su PC»: pulsa
**Más información** → **Ejecutar de todas formas**.

## Instalación

No hay instalador: el `.exe` es portable.

1. Descarga `ScadLab-v0.2.2-windows-amd64.exe` desde el Release.
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
cat VERSION                          # 0.2.2
./scripts/package-all.sh             # .dmg + .exe en build/bin/ (cross-compila, sin Windows)
cp build/bin/ScadLab-v0.2.2-windows-amd64.exe releases/windows/
shasum -a 256 releases/windows/ScadLab-v0.2.2-windows-amd64.exe
```

Una recompilación no da un binario idéntico bit a bit: si se regenera, hay que
actualizar el SHA-256 de arriba.
