# ScadLab — release Windows

Artefacto de distribución generado con `./scripts/package-windows.sh` (o
`./scripts/package-all.sh`). El workflow `.github/workflows/release.yml` **no
recompila**: publica exactamente el archivo versionado en esta carpeta y
comprueba que su SHA-256 aparezca en este README.

## Versión actual

_Todavía no hay una versión publicada._ Al preparar la primera, seguir
[.claude/specs/releases.md](../../.claude/specs/releases.md) y completar aquí:
versión, archivo, tamaño, SHA-256, arquitectura verificada con `file`,
compatibilidad real del sistema operativo, estado de firma
(sin Authenticode: SmartScreen → Más información → Ejecutar de todas formas) y si se probó en una Windows real (en qué versiones),
pasos de instalación y la sección «Regenerar este artefacto».

## Requisitos de ejecución

- OpenSCAD instalado (2021.01 o superior; se recomienda un snapshot 2025 para
  exportar 3MF con colores y el backend Manifold). ScadLab lo detecta en
  Configuración → OpenSCAD, o se le indica la ruta a mano.
- Opcional: Claude Code, opencode, Codex, Antigravity u Ollama para el diseño
  asistido por IA.
