# S5 — Agentes de IA en la app

Fase del [roadmap](../../claude/01-roadmap.md). Objetivo: diseñar con IA sin
salir de ScadLab. Una **terminal integrada** corre el CLI agéntico elegido
(Claude Code, opencode, Codex, Antigravity) dentro del workspace; un **chat**
con modelos (Ollama, API compatible con OpenAI, Anthropic) sirve para pensar
ideas y generar código que se guarda como checkpoint; y la **auto-iteración**
cierra el ciclo: si el render del checkpoint más nuevo falla, el agente de la
terminal recibe el aviso para corregirlo.

## Reparto (tres devs en Sonnet, en paralelo)

| Bloque | Dueño | Alcance (único que puede tocar) |
|---|---|---|
| 5.A terminal + sesiones + auto-iteración | fullstack-dev A | `backend/pty/**`, `backend/agentsessions/**`, `app_terminal.go`, `frontend/src/components/terminal/**`, `frontend/src/components/layout/Workbench.tsx`, `frontend/src/hooks/useTerminals.ts`, el namespace `terminal` de los diccionarios |
| 5.B chat con modelos | fullstack-dev B | `backend/llm/**`, `backend/chat/**`, `backend/store/migrations.go` (+ repos nuevos en `backend/store/chat*.go`), `app_chat.go`, `frontend/src/components/chat/**` (reemplaza el stub `ChatPanel.tsx`), el namespace `chat` de los diccionarios |
| 5.C prompts y auto-iteración (contenido) | openscad-dev | `backend/prompts/**`, `assets/prompts/**`, `assets/assets.go` (solo agregar `all:prompts` al embed) |

Compartidos con cuidado:
- `frontend/src/i18n/es.ts` y `en.ts`: cada dev agrega **solo su namespace de
  primer nivel** (`terminal`, `chat`) y **relee el archivo justo antes de cada
  edición** (el otro dev puede haberlo cambiado).
- `go.mod`/`go.sum`: `go get` de lo propio y `go mod tidy` al final.
- `frontend/wailsjs`: cada uno regenera con `wails generate module` al final;
  el último en terminar deja el resultado completo.
- `app.go` **no se toca**: para cerrar recursos al salir, `a.onShutdown(func)`
  (ya existe). El estado de cada servicio vive en su `app_*.go` (inicializado
  con `sync.Once` la primera vez que se usa).
- `CHANGELOG.md`, `index.html`, `claude/06-go-react-contract.md`,
  `claude/01-roadmap.md`, `scripts/help-shots.sh` y las capturas los integra el
  **architect** al final con lo que reporte cada uno (incluyan en su respuesta
  el texto propuesto para el CHANGELOG y la ayuda, y las vistas del banco que
  agregaron).
- `frontend/src/uishot.tsx`: cada uno agrega sus vistas y mocks en un bloque
  propio, releyendo antes de editar.

## Contrato 5.C → 5.A/5.B (`backend/prompts`)

```go
package prompts
type Context struct {
    Lang         string // "es" | "en"
    WorkspaceName string
    PrinterName  string
    BuildVolume  [3]float64
    ColorSlots   int
    Checkpoints  []string // nombres existentes (para sugerir el siguiente)
    CurrentFile  string   // checkpoint seleccionado ("" si ninguno)
    CurrentSource string  // su código (recortado a ~12 KB)
}
// SystemPrompt: prompt de sistema del CHAT (modelos sin acceso a archivos ni
// skills): experto en OpenSCAD + reglas FDM de Bambu + contrato de checkpoints
// + colores + formato de respuesta (código en bloques ```openscad, un archivo
// completo por bloque, con la línea @checkpoint). Construido a partir de las
// skills incluidas (resumidas en assets/prompts/, no copiadas enteras).
func SystemPrompt(c Context) string
// Nudge: texto que se escribe en la terminal del agente cuando el render del
// checkpoint más nuevo falla (auto-iteración). Corto, una sola línea lógica,
// en el idioma activo, sin secuencias de control; termina SIN salto de línea
// (la terminal decide si envía Enter). Menciona RENDER_ERRORS.md y que la
// corrección va en un checkpoint NUEVO.
func Nudge(fileName string, errorCount int, lang string) string
// ScadBlocks extrae los bloques ```openscad / ```scad (y ``` sin lenguaje cuyo
// contenido parezca OpenSCAD) de una respuesta markdown.
type Block struct{ Code, Lang string; Start, End int }
func ScadBlocks(markdown string) []Block
```

## Bloque 5.A — terminal

- `backend/pty`: puerto de `mini-tools/backend/localterm` (`github.com/aymanbagabas/go-pty`,
  ConPTY en Windows): `Manager` con `Open(id, cwd, argv, env, cols, rows)`,
  `Write`, `Resize`, `Close`, `CloseAll`; salida por evento Wails
  `terminal:data` `{id, data(base64)}` y `terminal:exit` `{id, code}`.
  `env`: el del proceso + PATH extendido con los directorios de
  `backend/detect` (una app gráfica no hereda el PATH de la shell) +
  `TERM=xterm-256color`, `COLORTERM=truecolor`.
- `backend/agentsessions`: listar sesiones previas de cada CLI **para el
  workspace** (filtradas por cwd = carpeta del workspace):
  - Claude: `~/.claude/projects/<cwd con / y . → ->/*.jsonl` (id = nombre del
    archivo; título = primer mensaje de usuario; fecha = mtime). Retomar:
    `claude --resume <id>`; continuar la última: `claude -c`.
  - Codex: `~/.codex/sessions/AAAA/MM/DD/*.jsonl`, primera línea `session_meta`
    con `cwd`; retomar `codex resume <id>`, última `codex resume --last`.
  - opencode: `opencode session list --format json` con `cwd` = workspace (si
    el flag no existe en la versión instalada, parsear la tabla; verificar);
    retomar `opencode --session <id>`, última `opencode -c`.
  - Antigravity (`agy`): sin listado verificable → solo «nueva» y
    «continuar la última» (`agy --continue`).
  Verificá cada formato contra los archivos reales de esta Mac (solo lectura:
  **nunca** modifiques ni copies las sesiones del usuario a ningún lado; en los
  tests usá fixtures inventados).
- Bindings (`app_terminal.go`): `StartAgentTerminal(workspaceId, agentId,
  mode, sessionId, cols, rows) (termId string, err)` con `mode` =
  `new|resume|continue`; `StartShellTerminal(workspaceId, cols, rows)`;
  `WriteTerminal(termId, data)`; `ResizeTerminal(termId, cols, rows)`;
  `CloseTerminal(termId)`; `ListAgentSessions(workspaceId, agentId)
  ([]agentsessions.Session, error)`; `ListTerminals()` (las abiertas, para
  reenganchar al recargar). Máximo **2** terminales de agente abiertas a la vez
  (error claro si se pide una tercera). Todas se cierran con `onShutdown`.
- **Auto-iteración**: setting `autoIterate bool` (default true). Al recibir
  `render:finished` con `status:"error"` del **checkpoint más nuevo** del
  workspace (mismo criterio que `RENDER_ERRORS.md`: `latestCheckpoint` en
  `app_render.go`), si hay una terminal de agente abierta en ese workspace,
  escribir `prompts.Nudge(...)` + Enter. Una sola vez por (archivo, render):
  nunca en bucle si el agente vuelve a fallar con el mismo archivo. Hacelo en
  Go (suscripción interna), no en el frontend, para que funcione aunque el
  panel esté plegado. Usá el hook interno `a.onRenderFinished(func(wsID,
  res, latest))` (ya existe en `app_render.go`, que no se toca): `latest` ya
  dice si es el checkpoint más nuevo. Registralo una sola vez (`sync.Once`).
- Frontend (`components/terminal/`): xterm.js (`@xterm/xterm`,
  `@xterm/addon-fit`, `@xterm/addon-web-links`), tema con los tokens; panel
  inferior con pestañas **Agente 1 / Agente 2 / Chat / Problemas** (Problemas
  ya existe; Chat = lazy de `components/chat/ChatPanel.tsx`, props del stub);
  selector de agente (solo los detectados), botones Nueva / Continuar última /
  Sesiones (lista con título y fecha → retomar) / Cerrar, indicador de
  auto-iteración. El panel puede maximizarse (y volver).
- Seguridad: los argumentos se pasan como `argv`, nunca por una shell; el
  `sessionId` se valida (charset del CLI) antes de usarlo.

## Bloque 5.B — chat

- `backend/llm`: puerto **reducido** de `mini-tools/backend/llm` (solo chat de
  texto con streaming; sin adjuntos ni herramientas): Ollama (`/api/chat`),
  OpenAI-compatible (`/v1/chat/completions` con SSE), Anthropic (SDK oficial
  `github.com/anthropics/anthropic-sdk-go`, misma versión que mini-tools). La
  API key se lee de la variable de entorno que nombra `config.apiKeyEnv` del
  proveedor; nunca se guarda ni se loguea.
- Persistencia: migración **2** en `backend/store/migrations.go`: tablas
  `chats(id, workspace_id, provider_id, model, title, created_at, updated_at)`
  y `chat_messages(id, chat_id, role, content, created_at)` (aditiva; FK con
  `ON DELETE CASCADE`).
- Bindings (`app_chat.go`): `ListChats(workspaceId)`, `GetChat(chatId)`,
  `NewChat(workspaceId, providerId, model)`, `SendChatMessage(chatId, text)`
  (streaming por eventos `chat:delta` `{chatId, text}`, `chat:done` `{chatId,
  messageId}`, `chat:error`), `CancelChat(chatId)`, `DeleteChat(chatId)`,
  `ListProviderModels(providerId)` (Ollama: `/api/tags`; OpenAI-compatible:
  `/v1/models`; Anthropic: lista fija documentada). El prompt de sistema sale
  de `prompts.SystemPrompt` con el contexto del workspace y del checkpoint
  seleccionado (el frontend pasa `currentFile` en `SendChatMessage`… agregá ese
  parámetro: `SendChatMessage(chatId, text, currentFile)`).
- Frontend (`components/chat/ChatPanel.tsx` + lo que necesite): lista de
  chats del workspace, selector de proveedor y modelo (solo proveedores
  habilitados), conversación con markdown y streaming, botón Detener; en cada
  bloque de código OpenSCAD: **Guardar como checkpoint** (usa el binding
  existente `SaveAsCheckpoint`, con padre = checkpoint seleccionado y un mini
  diálogo de rótulo) y **Copiar**. Aviso claro si no hay ningún proveedor
  habilitado, con botón a Configuración → IA.

## Criterio de terminado (cada dev)

`gofmt -l .` vacío, `go build ./...`, `go vet ./...`, `go test -race ./...`,
`GOOS=windows GOARCH=amd64 go build ./...`, `cd frontend && pnpm build`,
`node frontend/scripts/i18n-check.mjs`. Vistas nuevas en el banco con datos
inventados y sus capturas generadas y revisadas (`./scripts/uishot.sh <vista>`),
listadas en la respuesta para que el architect las sume a `help-shots.sh` y a
la ayuda.
