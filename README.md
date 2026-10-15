<p align="center"><img src="assets/brand/icon-1024.png" width="128" alt="ScadLab"></p>

# ScadLab

**Diseño 3D con OpenSCAD e IA, listo para imprimir en Bambu Lab.**

ScadLab es una app de escritorio para macOS y Windows donde describís una pieza,
un agente de IA (Claude Code, opencode, Codex o Antigravity, u Ollama en local)
la escribe en OpenSCAD dentro de tu proyecto, y vos la ves, la medís, comparás
versiones y la exportás a STL o 3MF multicolor para tu A1, A1 mini, P1S, X1C o H2D.

📖 [Documentación](https://rafael180496.github.io/ScadLab/)

## Qué hace (y qué viene)

| | Estado |
|---|---|
| Workspaces con reglas para la IA y skills de modelado instaladas para todos los agentes | ✅ v0.1.0 |
| Detección de OpenSCAD y de los CLIs de IA, configuración de Ollama / APIs | ✅ v0.1.0 |
| Árbol de checkpoints inmutables con ramas | ✅ lectura · 🛠 menú contextual |
| Visor 3D PBR, vistas, wireframe, vista dividida, medición y costo | 🛠 en desarrollo |
| Editor Monaco con resaltado OpenSCAD y marcadores de error | 🛠 en desarrollo |
| Terminal integrada con el agente, servidor MCP de OpenSCAD, auto-iteración | 🛠 en desarrollo |
| Exportación STL / 3MF multicolor (AMS) / PNG | 🛠 en desarrollo |

Plan completo por fases: [claude/01-roadmap.md](claude/01-roadmap.md).

## Requisitos

- macOS 11+ (Apple Silicon) o Windows 10/11 (x64, WebView2).
- [OpenSCAD](https://openscad.org/downloads.html) — se recomienda un snapshot 2025
  (backend Manifold y 3MF con colores).
- Opcional: Claude Code, opencode, Codex, Antigravity (`agy`) u Ollama.

## Desarrollo

```bash
./scripts/install.sh      # Go 1.27, Wails CLI, pnpm
./scripts/start-dev.sh    # wails dev
go test ./...
cd frontend && pnpm build
```

Arquitectura, reglas y agentes de desarrollo: [CLAUDE.md](CLAUDE.md).

## Distribución / Empaquetado macOS

_Todavía no hay una versión publicada._ Ver [releases/macos](releases/macos/README.md).

## Distribución / Empaquetado Windows

_Todavía no hay una versión publicada._ Ver [releases/windows](releases/windows/README.md).
