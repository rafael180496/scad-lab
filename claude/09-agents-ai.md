# Agentes de IA

Mismo catálogo que mini-tools.

| id | Tipo | Comando | Notas |
|---|---|---|---|
| `claude` | CLI | `claude` | lee `CLAUDE.md`, `.claude/skills`, `.mcp.json` |
| `opencode` | CLI | `opencode` | lee `AGENTS.md`, `.opencode/skills` (y `.claude/skills`) |
| `codex` | CLI | `codex` | lee `AGENTS.md`, `.agents/skills` |
| `antigravity` | CLI | `agy` | antes Gemini CLI: se detecta también `gemini`; `.agent/skills` |
| `ollama` | servidor | — | `baseUrl`, `model`; `OllamaModels` prueba y lista |
| `openai` | servidor | — | cualquier API compatible (LM Studio, vLLM, OpenRouter); `apiKeyEnv` |
| `anthropic` | servidor | — | API de Anthropic; `apiKeyEnv` |

- Las API keys **no** se guardan: `config.apiKeyEnv` nombra la variable.
- Cada workspace lleva `CLAUDE.md` y `AGENTS.md` con las mismas reglas
  (generadas de `assets/workspace/RULES.md.tmpl`), entre ellas: nunca
  sobrescribir checkpoints, color obligatorio, validar, respetar la placa y
  **no hacer commits**.
## En la app (S5)

- **Terminal** (`backend/pty`, `app_terminal.go`): el CLI corre en la carpeta
  del workspace con `argv` (nunca vía shell), PATH extendido con
  `backend/detect` y `TERM=xterm-256color`. Máximo 2 terminales de agente.
  Cerrar mata el grupo de procesos (Setsid / `taskkill /T`) para no dejar
  servidores MCP huérfanos.
- **Sesiones** (`backend/agentsessions`), verificadas contra archivos reales:

  | CLI | Dónde | Retomar / última |
  |---|---|---|
  | Claude | `~/.claude/projects/<cwd con [^A-Za-z0-9]→->/<uuid>.jsonl` (se verifica además el `cwd` guardado) | `--resume <id>` / `-c` |
  | Codex | `~/.codex/sessions/AAAA/MM/DD/rollout-…-<uuid>.jsonl` (`session_meta` con `cwd`; se omiten las de `source: exec`) | `resume <id>` / `resume --last` |
  | opencode | `opencode session list --format json` (filtrado por `directory`) | `--session <id>` / `-c` |
  | Antigravity | sin listado verificable | — / `--continue` |

- **Auto-iteración**: `a.onRenderFinished` (hook interno de `app_render.go`);
  solo el checkpoint más nuevo con error; `prompts.Nudge` + Enter aparte (los
  CLIs con Ink/Bubble Tea toman un `\r` pegado como salto de línea); una vez por
  fallo, tope de 5 seguidos.
- **Chat** (`backend/llm`, `backend/chat`, `app_chat.go`): Ollama (NDJSON,
  `num_ctx` 16384), OpenAI-compatible (SSE) y Anthropic (SDK oficial). Prompt
  de sistema de `backend/prompts` (`assets/prompts/system.{es,en}.md`).
- S6 agrega el servidor MCP `ScadLab --mcp` y su configuración automática por
  CLI.
