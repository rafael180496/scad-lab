# S6 — Servidor MCP de OpenSCAD

Fase del [roadmap](../../claude/01-roadmap.md). Objetivo: que los agentes
(Claude Code, opencode, Codex, Antigravity) **rendericen, validen, midan y vean**
sus modelos solos, con herramientas MCP de ScadLab, en vez de depender de la
auto-iteración o de invocar OpenSCAD a mano.

## Decisiones (architect)

- **Servidor autónomo**: `ScadLab --mcp --workspace <dir>` (stdio, JSON-RPC
  2.0, sin abrir la ventana). A diferencia de mini-tools no hay puente a la
  ventana: ScadLab no tiene secretos; el proceso MCP lee la configuración de
  `scadlab.db` (solo lectura de settings: ruta de OpenSCAD, motor, impresora del
  workspace, materiales), usa el mismo motor de render y caché, y trabaja sobre
  los archivos del workspace. Funciona con la app cerrada (agente lanzado desde
  una terminal externa).
- **Sin SDK externo**: puerto del `rpc.go` de mini-tools
  (`/Users/developer/Documents/project/github_project/golang/mini-tools/backend/mcpserver/rpc.go`),
  `initialize` negociando versión (`2025-06-18` si el cliente la pide, si no
  `2024-11-05`), `tools/list`, `tools/call`, `ping`, notificaciones ignoradas.
  **stdout solo lleva JSON-RPC**; logs a stderr.
- **Configuración solo de proyecto** (nunca la global del usuario):
  - Claude Code: `<ws>/.mcp.json` → `mcpServers.scadlab`.
  - opencode: `<ws>/opencode.json` → `mcp.scadlab` (`{"type":"local","command":[bin,"--mcp","--workspace",ws],"enabled":true}`).
  - Gemini CLI (anterior): `<ws>/.gemini/settings.json` → `mcpServers.scadlab`.
  - **Antigravity (`agy`)**: verificado después que **no** lee config de proyecto
    ni acepta flags al arrancar; solo global (`~/.gemini/config/mcp_config.json`).
    Se agrega con un botón explícito (`agy mcp add -- scadlab <bin> --mcp`, sin
    `--workspace`: usa el cwd) con confirmación del usuario. Ver
    `claude/13-mcp.md`.
  - Codex: no lee config de proyecto; cuando ScadLab lanza Codex en su
    terminal agrega `-c mcp_servers.scadlab.command="<bin>"` y
    `-c mcp_servers.scadlab.args=[...]` (verificado: `codex -c key=value`).
  Se **fusiona** solo la clave `scadlab`; el resto del archivo del usuario se
  conserva (si el JSON está roto, no se toca y se avisa). La ruta del binario es
  `os.Executable()`; se reescribe al abrir/crear el workspace si cambió (app
  movida, actualización).
- **Seguridad**: toda ruta se resuelve dentro del workspace; nombres de
  checkpoint con `validCheckpointName`; nunca se sobrescribe ni borra un
  checkpoint; nada fuera de `<ws>` salvo la caché de mallas.

## Herramientas (prefijo `scadlab_` no hace falta: el servidor ya se llama scadlab)

| Herramienta | Entrada | Salida |
|---|---|---|
| `list_checkpoints` | — | árbol: archivo, padre, descripción, último estado de render conocido |
| `read_checkpoint` | `file` | código |
| `create_checkpoint` | `parent`, `label`, `description`, `source` | archivo creado (`NNN-slug.scad`, encabezado correcto, `O_EXCL`); nunca sobrescribe |
| `render` | `file` (default: el más nuevo) | estado, diagnósticos con línea, duración, `mesh.Info` (tamaño, volumen, triángulos, watertight), si cabe en la placa; escribe/borra `RENDER_ERRORS.md` con la misma regla que la app (solo el más nuevo) |
| `lint` | `file` o `source` | diagnósticos del linter (OpenSCAD + SL001–SL009) |
| `format` | `source` | código formateado (no escribe archivos) |
| `measure` | `file` | `mesh.Info` + volumen de la impresora + cabe/no cabe + peso estimado con el material por defecto (densidad × volumen, aclarado como sólido al 100 %) |
| `preview` | `file`, `view` (`iso`\|`front`\|`top`\|`right`…), `size` | **imagen PNG** (contenido MCP `image`, base64) renderizada con OpenSCAD (`--render`, `--camera`/`--viewall`, colores del modelo) para que el agente vea la pieza |
| `printer_info` | — | impresora del workspace: volumen, boquilla, slots AMS, reglas clave |
| `export` | `file`, `format` (`stl`\|`3mf`\|`stl-per-color`) | ruta en `<ws>/exports/…` + partes/avisos (usa `backend/exporter`) |
| `list_templates` / `get_template` | `name` | plantillas de `openscad-templates` |

Descripciones de las herramientas en inglés (los modelos las leen mejor) y
cortas; mensajes de error con `i18n.Msg` (el idioma de la app).

## Reparto (dos devs en Sonnet, en paralelo)

| Bloque | Dueño | Alcance |
|---|---|---|
| 6.A servidor + herramientas | openscad-dev | `backend/mcp/**` (nuevo), `main.go` (solo el modo `--mcp` antes de `wails.Run`), `assets/workspace/RULES.md.tmpl` (las reglas pasan a usar las herramientas MCP con fallback a la CLI), `claude/13-mcp.md` (nuevo) + fila en `CLAUDE.md` |
| 6.B configuración por CLI + UI | fullstack-dev | `backend/mcpconf/**` (nuevo: leer/fusionar/escribir los tres JSON), `app_mcp.go` (nuevo), `app_workspaces.go` (sincronizar al crear/abrir/importar), `app_terminal.go` (flags `-c` de Codex), `backend/store/settings.go` (`mcpEnabled`, default true), `frontend/**`, `index.html`, `CHANGELOG.md`, `scripts/help-shots.sh`, `docs/screenshots/**`, `claude/06-*.md`, `claude/09-agents-ai.md`, `claude/01-roadmap.md` |

Contrato entre ambos: el comando es `<os.Executable()> --mcp --workspace <dir absoluto>`;
6.A expone `mcp.ServerName = "scadlab"` y `mcp.Args(workspaceDir) []string`
(`["--mcp","--workspace",dir]`) para que 6.B no lo duplique. Si 6.B lo necesita
antes, que lo declare igual en su paquete y el architect lo une.

## Pruebas

- 6.A: end-to-end por pipes (lanzar el binario de test o `mcp.Serve` con
  `io.Pipe`): `initialize` → `tools/list` → `tools/call` de cada herramienta en
  un workspace temporal con OpenSCAD real (`t.Skip` si no está): render OK y
  con error (diagnóstico con línea), create nunca pisa, preview devuelve PNG
  válido (cabecera `\x89PNG`), export escribe en `exports/`, ruta fuera del
  workspace → error. stdout limpio (todo parseable como JSON-RPC).
- 6.B: fusión de JSON (conserva otras claves, no toca un JSON roto, idempotente),
  rutas por SO, flags de Codex armados como `argv` (sin shell), sincronización
  al abrir el workspace.
- **Prueba real con un CLI**: el architect corre `claude mcp list` (o
  `claude --mcp-config … mcp list`) en un workspace temporal para ver el
  servidor «connected»; los devs **no** lanzan sesiones de agentes (gastan
  cuota) ni Bambu Studio.
