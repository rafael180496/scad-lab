# ScadLab — release macOS

Artefacto de distribución generado con `./scripts/package-macos.sh` (o
`./scripts/package-all.sh`). El workflow `.github/workflows/release.yml` **no
recompila**: publica exactamente el archivo versionado en esta carpeta y
comprueba que su SHA-256 aparezca en este README.

## Versión actual

| Campo | Valor |
|---|---|
| Versión | **0.2.0** |
| Archivo | `ScadLab-v0.2.0.dmg` |
| Tamaño | 19 313 894 bytes (18,4 MB) |
| SHA-256 | `5b943a17a7734716ff029cc5236610dcb7c13443d533c647188001a3a8f43655` |
| Arquitectura | arm64 (Apple Silicon) |
| Descarga | https://github.com/rafael180496/ScadLab/releases/download/v0.2.0/ScadLab-v0.2.0.dmg |

### Compatibilidad verificada

`file` sobre el binario de la `.app` (extraída del `.dmg`):

```
ScadLab.app/Contents/MacOS/ScadLab: Mach-O 64-bit executable arm64
```

- **Solo Apple Silicon (M1 y posteriores).** No hay build para Intel.
- **macOS 11 (Big Sur) o superior.** El binario es arm64 y su `LC_BUILD_VERSION`
  declara `minos 11.0`: Apple Silicon no existe antes de macOS 11.
- **Discrepancia con el `Info.plist`:** el que genera Wails declara
  `LSMinimumSystemVersion = 10.13.0`. Es una plantilla genérica heredada de cuando
  Wails también apuntaba a Intel; **no es la compatibilidad real**. En un Mac
  con macOS 10.13–10.15 el sistema dejaría abrir la app según el plist, pero no
  hay ningún procesador compatible con el binario.
- El binario se compiló y empaquetó en macOS (Apple Silicon); la versión de `minos` y la
  arquitectura se leyeron del propio ejecutable, no del `Info.plist`.

### Firma

**No está firmado ni notarizado** (sin Apple Developer ID; solo la firma ad-hoc
que hace Wails). Gatekeeper avisa «desarrollador no identificado» o que la app
«está dañada». Elige uno:

- clic derecho sobre la app → **Abrir** → confirma;
- `xattr -cr /Applications/ScadLab.app` (o `xattr -dr com.apple.quarantine …`);
- Ajustes del Sistema → Privacidad y Seguridad → **Abrir de todas formas**.

## Instalación

1. Descarga `ScadLab-v0.2.0.dmg` desde el Release.
2. Ábrelo y arrastra `ScadLab.app` a **Aplicaciones**.
3. La primera vez, **clic derecho → Abrir** (no doble clic) y confirma.
4. Si macOS dice que está dañada: `xattr -cr /Applications/ScadLab.app`.

## Requisitos de ejecución

- **OpenSCAD es opcional.** Sin él, ScadLab renderiza con su **motor integrado**
  (OpenSCAD 2025.12.30 en WebAssembly, más lento) y ofrece **Descargar OpenSCAD**
  (60 MB, verificado con SHA-256) en Configuración → OpenSCAD. Un OpenSCAD instalado
  (2021.01 o superior; se recomienda un snapshot 2025 para 3MF con colores y el
  backend Manifold) se usa con preferencia.
- Opcional: Claude Code, opencode, Codex, Antigravity u Ollama para el diseño
  asistido por IA.

## Regenerar este artefacto

```bash
cat VERSION                          # 0.2.0
./scripts/package-all.sh             # .dmg + .exe en build/bin/
cp build/bin/ScadLab-v0.2.0.dmg releases/macos/
shasum -a 256 releases/macos/ScadLab-v0.2.0.dmg
```

`package-macos.sh` solo corre en macOS (usa `hdiutil`). Una recompilación no da un
binario idéntico bit a bit: si se regenera, hay que actualizar el SHA-256 de arriba.
