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
- S5 agrega la terminal (PTY + xterm) y las sesiones; S6 el servidor MCP
  `ScadLab --mcp` y su configuración automática por CLI.
