# Overview

## Qué es

ScadLab es un «taller» de modelado paramétrico: el usuario describe una pieza,
un agente de IA escribe OpenSCAD dentro de un **workspace**, ScadLab renderiza,
muestra el árbol de versiones (**checkpoints**), mide y exporta a STL/3MF listo
para Bambu Studio. Inspirado en ClawSCAD, con soporte para varios agentes y
foco en impresoras Bambu Lab.

## Visión y principios (palabras del usuario)

> «La idea de este proyecto es hacer modelos sin saber modelaje 3D, al 100 % con
> ayuda de la IA, así que la IA tiene que tener los tools necesarios para hacer
> la creación y todo lo del proyecto: scad, stl, etc.»

Consecuencias que rigen cualquier fase:

1. **La IA puede hacer de punta a punta todo lo que el usuario hace con la UI**,
   con cualquier proveedor (Hermes por tool calling; los CLIs por el MCP de
   ScadLab). Cada funcionalidad nueva de la UI que crea o cambia algo lleva su
   herramienta MCP (tabla en [13-mcp.md](13-mcp.md)). Única excepción a
   propósito: «Abrir en Bambu Studio», que solo dispara el usuario.
2. **El agente crea, renderiza, mira, mide y corrige**; nunca responde solo con
   código cuando puede crearlo: el usuario no sabe modelar.
3. **Escritura por defecto**: el modo «Agente» es el predeterminado en todos
   lados; «Solo consulta» se elige a mano y vale solo para esa conversación.
4. **Nada es destructivo**: borrar es mover a la papelera; un STL importado queda
   intacto en `refs/`; y un checkpoint solo se reescribe (la IA con
   `update_checkpoint`, el usuario con «Restaurar versión») guardando antes la
   versión anterior en su historial ([claude/15](15-checkpoints.md)).
5. Un pedido en lenguaje natural en un workspace vacío termina en un modelo
   renderizado, medido y exportable sin que el usuario toque código.

## Stack

| Capa | Tecnología |
|---|---|
| Desktop | Wails v2 (WebKit en macOS, WebView2 en Windows) |
| Backend | Go 1.27, sin CGO |
| Base local | SQLite (`modernc.org/sqlite`) — `scadlab.db` |
| Frontend | React 19 + TypeScript estricto + Vite 7 + Tailwind v4 |
| Visor 3D (S2) | three.js / React Three Fiber |
| Editor (S4) | Monaco con gramática Monarch de OpenSCAD |
| Terminal (S5) | xterm.js + PTY en Go (`go-pty`, ConPTY en Windows) |
| Modelado | OpenSCAD externo (2021.01+, recomendado snapshot 2025 con Manifold) |

## Estructura

```
main.go, app*.go        bindings Wails (App), uno por área
backend/
  appdata/              rutas de datos por SO (SCADLAB_DATA_DIR para tests)
  i18n/                 Msg{ES,EN}
  store/                SQLite: migraciones + repositorios
  detect/               OpenSCAD y CLIs agénticos fuera del PATH
  workspace/            crear workspaces, leer checkpoints
  skills/               descubrir, crear desde plantilla, sincronizar a cada CLI
  printers/             catálogo (lee printers.json de la skill)
  osopen/               abrir en Finder/Explorador
assets/
  skills/               skills de modelado (embebidas, se copian a workspaces)
  workspace/            plantilla de CLAUDE.md/AGENTS.md del workspace
  brand/                icono (icon.svg es la fuente)
frontend/src/
  i18n/                 es.ts, en.ts (tipados)
  components/{layout,workspace,settings,ui}/
  lib/                  helpers sin estado
scripts/                build, empaquetado, versión, iconos
releases/<os>/          artefactos publicables + README con SHA-256
.github/workflows/      release.yml (publica el tag, no compila)
```

## Comandos

```bash
./scripts/install.sh          # deps (wails CLI, go mod, pnpm)
./scripts/start-dev.sh        # wails dev
go test ./...                 # tests backend
cd frontend && pnpm build     # tsc + vite
./scripts/build.sh            # wails build -clean
./scripts/make-icons.sh       # regenera iconos desde assets/brand/icon.svg
./scripts/uishot.sh main      # captura una vista del banco (datos inventados)
./scripts/help-shots.sh       # regenera todas las capturas de la ayuda (en + es)
./scripts/bump-version.sh patch && ./scripts/package-all.sh   # versión
```
