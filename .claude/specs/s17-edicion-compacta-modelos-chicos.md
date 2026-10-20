# S17 — Edición sin reescribir, lectura por partes, salidas compactas y modelos chicos

Fase del roadmap: [claude/01-roadmap.md](../../claude/01-roadmap.md). Segmento nuevo `claude/35-edicion-compacta.md` (fila en
`CLAUDE.md`). Notas que cambia: `claude/13`, `19`, `22`, `25`, `26`, `34`, `11`; `technical.md` regla 3 (c) suma
`edit_checkpoint`, `set_parameters` en el lugar y `lib/*.scad`.

## Contexto medido (transcript real de Antigravity / Gemini 3.8 Flash)

- Cada cambio a una maceta de ~600 líneas pasaba por `update_checkpoint` (archivo completo): **3 min 39 s y 1 min 42 s por
  cambio**, el modelo regeneraba las 600 líneas.
- Contexto de **105k–192k tokens por paso** (fuentes completas y resultados de herramientas acumulados); el chat mostraba
  ↑694k / ↑833k por turno (suma de todas las vueltas, no el contexto).
- Antigravity guarda las salidas grandes en archivos y luego hace `read_file`: más vueltas.
- Tarjetas `call_mcp_tool`: en agy 1.4.x el nombre viene en `tool_info.parameters.ToolName`; `unwrapAgyMCP` no lo ve.
- agy ejecutó `run_command` (`python3 -c` sobre el 3MF) en «Agente», y después terminó el turno sin texto.
- El 3MF de la maceta (verde, `difference` con cavidad cónica y drenajes) + texto/marco naranja en relieve curvado
  (`intersection` con cascarón cónico) se abre en Bambu Studio con **el hueco relleno y muchos soportes**.

Transcripts reales (solo lectura, nunca copiar rutas ni datos del usuario a fixtures sin sanear):
`~/.gemini/antigravity-cli/brain/*/.system_generated/logs/transcript_full.jsonl` (la maceta: `54b43222-…`).

## Decisiones

### A. Editar sin regenerar el archivo
1. **`edit_checkpoint(file, edits[], note?)`** — escritura, solo «Agente», `EffectCheckpoint`, en el lugar.
   Cada edición es **una de dos formas**, y todas se resuelven contra el archivo **tal como está en disco antes del
   cambio** (las de línea usan los números que devolvieron `read`/`outline`/`find`):
   - `{old, new}`: reemplazo exacto; `old` debe aparecer **exactamente una vez**.
   - `{from_line, to_line, new}`: reemplaza el rango inclusivo (1-based); `to_line = from_line - 1` = insertar antes de
     `from_line` (insertar al final: `from_line = líneas+1`). Se adopta porque los modelos chicos copian mal el texto
     exacto pero sí leen bien los números que les dimos; el resultado devuelve las líneas reemplazadas para que el
     modelo vea qué tocó.
   - Ediciones que se solapan → error. **Todo o nada**: si una falla no se escribe nada.
   - Error útil: `old` no encontrado → «no aparece; la línea más parecida es la 123: `…`» (similitud por línea, recorte a
     120 caracteres); ambiguo → «aparece N veces, en las líneas 12, 40, 77: agrega contexto o usa from_line/to_line».
   - La línea 1 `// @checkpoint …` no se edita (error claro); el resto pasa por `checkpoint.Update` (respaldo → atómica,
     tope), con `tool: "edit_checkpoint"` en el `.json` del historial. Luego lint + render y **resultado compacto**.
2. **`set_parameters` edita en el lugar** (con historial, `checkpoint.Update`, `tool: "set_parameters"`) — coherente con
   S16. `variant: true` conserva el comportamiento viejo (hijo nuevo). Resultado en el lugar: `{file, updated:true, changes,
   warnings?, render}` compacto; variante: `{file, created:true, parent, …}`. Reglas: medidas, colores o textos del
   Customizer → `set_parameters`.
3. **`update_checkpoint`** queda para reescrituras grandes o reestructurar. **`mcp.UpdatesInPlace(tool, resultText)`**
   decide por el resultado (`"updated": true`) y no solo por el nombre: `update_checkpoint`, `edit_checkpoint`,
   `set_parameters` (sin `variant`), `extract_module`.

### B. Leer menos
- **`read_checkpoint(file, from?, to?, full?)`**: con rango → líneas numeradas `N| código` (tope 400 líneas por llamada);
  sin rango → el código crudo como hoy si mide ≤ 12 KB; si no, el comienzo crudo hasta 12 KB + una línea final
  `[truncado: N líneas, X KB; usa outline_checkpoint / find_in_checkpoint y read_checkpoint con from/to, o full=true]`.
  `full: true` = entero (para `update_checkpoint`).
- **`outline_checkpoint(file)`**: `{file, lines, bytes, params:[{section, items:[{name, line, value}]}], modules:[{name,
  line, end, args}], functions:[…], includes:[{kind, path, line}], parts:[{color, line}]}` (las llamadas `color()` del
  nivel superior). Tope ~4 KB (recorta valores largos).
- **`find_in_checkpoint(file, query, context?=2, regex?=false)`**: ventanas numeradas fusionadas, máx. 30 coincidencias,
  tope ~4 KB; sin coincidencias → las 3 líneas más parecidas.
- `file` admite también **`lib/<nombre>.scad`** (solo hijos directos de `lib/`; las subcarpetas son librerías instaladas,
  de solo lectura) en `read`, `outline`, `find` y `edit`. Rutas seguras (sin `..`, absolutas, unidades, enlaces que salen).
  Historial de un archivo de `lib/`: `.scadlab/history/lib/<nombre>.scad/`.

### C. Respuestas compactas
- `render`, `lint`, `measure`, `update/edit/set_parameters`: resumen corto por defecto (estado, partes `[{color, bbox}]`,
  bbox total, ¿cabe?, errores con línea —máx. 5—); detalle completo con `verbose: true` o si hay error.
- `preview`: por defecto **una** vista de 384 px (`iso`); `views: [...]` o `all: true` para más.
- **Tope duro**: ningún resultado de texto > **6 KB** sin `verbose`/`full`. Prueba de tabla que llama a cada herramienta
  sobre la maceta de prueba y mide bytes (las medidas van al informe y a `claude/35`).

### D. Modelos chicos (Hermes)
- **Perfil de herramientas** `mcp.ProfileEssential` / `ProfileFull`: esencial = `outline_checkpoint`, `find_in_checkpoint`,
  `read_checkpoint`, `edit_checkpoint`, `set_parameters`, `get_parameters`, `render`, `preview`, `measure`,
  `create_checkpoint`, `list_checkpoints`, con descripción de 1–2 frases y esquema mínimo (`Tool.Short`, `Tool.ShortSchema`).
  `(*Toolbox).ToolsFor(p)`; `Call` sigue aceptando cualquier herramienta del catálogo.
- **Automático**: contexto efectivo < 32 768 tokens → esencial. **Control manual** por conversación: `chats.tools`
  (migración aditiva) `''` = auto, `essential`, `full`; binding `SetChatToolProfile(chatId, profile string) error`; el chat
  muestra un selector compacto (solo con servidores, no con CLIs).
- **Prompt compacto** `assets/prompts/agent-compact.{es,en}.md` (`prompts.Context.Compact`): mismas reglas, ≤ 1 200 tokens.
  Medir el prompt actual y reportar.
- **Contexto en el turno** (`backend/hermes`): presupuesto proporcional a `num_ctx` (no 160 000 fijos); una lectura de un
  archivo ya leído y sin cambios se reemplaza por `[archivo X leído, versión Y]`; resultados viejos → nota.
- **Entre turnos**: `SeedHistory` resume extractivamente los turnos viejos que no caben (sin llamada extra al modelo) y
  conserva enteros los últimos. **«Nueva conversación con resumen»** al pasar el 70 % del contexto: una llamada al modelo
  pide un resumen (≤ 300 tokens; si falla, el extractivo) y abre un chat nuevo sembrado con él.
- **`num_ctx`**: el del servidor si está fijado; si no, `min(ContextTokens del modelo, 32768)`, mínimo 8 192. Aviso en el
  chat si prompt + herramientas superan el 60 % del contexto.
- **Prueba con Ollama simulado de 8k**: maceta de 600 líneas; (1) «hazla 10 mm más alta» → `set_parameters`; (2) un cambio
  de código → `outline` → `find` → `edit_checkpoint`. Ninguna petición supera 8 192 tokens estimados; se registran los
  tokens enviados por vuelta **antes** (perfil completo, prompt actual) y **después**.

### E. Modularidad
- **`extract_module(file, modules[], lib, mode?)`** (escritura, en el lugar): mueve los `module`/`function` nombrados a
  `lib/<lib>.scad` (nuevo; si existe y es nuestro, agrega; nunca pisa sin respaldo) y deja `include <lib/…>` (por defecto:
  ve las variables del archivo) o `use <…>` (`mode: "use"`). Renderiza antes y después: si cambian bbox o volumen
  (tolerancia 1e-3) **deshace** (restaura la versión del historial) y lo dice. Receta en la skill `openscad-core`.

### F. Tiempo y tokens visibles
- `Step` suma `roundInput`, `roundOutput` (vuelta del modelo que pidió la herramienta) y `resultChars`/`resultTokens`
  (estimado de lo que vuelve al modelo). `DurationMs` ya existe: se muestra.
- `Usage` suma `modelMs`, `toolMs`, `rounds`, `peakInput` (la vuelta con más entrada = ocupación real del contexto) y
  `lastInput`. Al pie del turno: «Contexto máx. 12k · ↑ 48k en 5 vueltas · ↓ 2k · modelo 41 s · herramientas 6 s», con
  `title` que explica que ↑ es la suma de las vueltas. Los CLIs llenan lo que su stream informe.

### G. Antigravity
1. `unwrapAgyMCP` con `tool_info.parameters.{ServerName,ToolName,Arguments}` + fixture saneado de agy 1.4.x.
2. **Shell bloqueada en el chat** sin tocar más del `settings.json` global que las reglas `mcp(scadlab/*)`: investigar
   `agy --help` (solo `--help` de sus subcomandos; ningún prompt) y `~/.gemini/antigravity-cli/builtin/skills/*/docs/`
   (`--sandbox`, modos distintos de `accept-edits`, un settings por invocación). `permissions.deny` global solo si no hay
   otra (afecta la terminal). Lo que no se pueda probar sin el `agy` real se deja como comando exacto en `claude/26`.
3. **Turno sin texto tras herramientas**: el chat muestra el resultado (resumido) de la última herramienta y qué quedó
   hecho, en vez de solo «terminó sin responder».

### H. 3MF que Bambu Studio rellena
Reconstruir la maceta (cónica hueca con `difference`, drenajes; texto/marco en relieve curvado con `intersection` de
cascarón cónico, otro `color()`), exportarla por la ruta nativa y la WASM (`ConvertOFFTo3MF`) y por `backend/threemf`;
por parte: volumen con signo (> 0), manifold, aristas consistentes (cada arista dirigida una vez y su opuesta una vez),
bbox igual al STL de OpenSCAD, transformaciones de `<item>`/`<component>`. Corregir la causa (winding, triángulos
compartidos, una parte que ocupa la cavidad de otra…) con pruebas que la fijen. **Nunca abrir Bambu Studio.**

## Tareas

| # | Tarea | Dueño | Alcance |
|---|---|---|---|
| 17.1 | Herramientas A+B+C+E, perfiles de herramientas, prompts/reglas/skills | openscad-dev | `backend/mcp/**`, `backend/scad/**`, `backend/checkpoint/**`, `backend/textedit/` (nuevo), `backend/prompts/**`, `backend/clichat/modes.go` (+`modes_test.go`), `assets/prompts/**`, `assets/workspace/**`, `assets/skills/openscad-core/**`, `claude/13`, `25`, `34`, `technical.md` |
| 17.2 | 3MF: diagnóstico y arreglo | openscad-dev | `backend/threemf/**`, `backend/parts/**`, `backend/exporter/**`, `backend/scadwasm/**` (solo la conversión), `backend/mesh/**`, skills `openscad-3mf-multicolor`/`bambu-lab-printers`, `claude/11` |
| 17.3 | Hermes D + F, Antigravity G, interfaz, migración, documentación y capturas | fullstack-dev | `backend/hermes/**`, `backend/llm/**`, `backend/chat/**`, `backend/store/**`, `backend/clichat/**` (salvo `modes.go`), `app*.go`, `frontend/**`, `scripts/**`, `docs/screenshots/**`, `index.html`, `es/index.html`, `claude/19`, `22`, `26`, `33`, `07`, `35` (nuevo), fila en `CLAUDE.md` |
| 17.4 | Integración, medidas, verificación completa, roadmap | architect | todo |

**Puntos de choque:** `CHANGELOG.md` lo editan los tres (releer justo antes de cada edición; cada uno su viñeta bajo
`[Unreleased]`). `frontend/src/i18n/{es,en}.ts` y `lib/chatSteps.ts` (`CREATING_TOOLS`) solo 17.3: las etiquetas de
`edit_checkpoint`, `outline_checkpoint`, `find_in_checkpoint`, `extract_module` las agrega 17.3. `effects.go` solo 17.1.
No revertir los cambios sin commitear del usuario (editor y `m:colorgroup`).

## Contrato entre 17.1 y 17.3 (Go)

```go
// backend/mcp
type ToolProfile string
const ( ProfileFull ToolProfile = "full"; ProfileEssential ToolProfile = "essential" )
func EssentialTools() []string
func (t *Toolbox) ToolsFor(p ToolProfile) []ToolSpec   // esencial: descripción y esquema cortos
func UpdatesInPlace(tool, resultText string) bool      // por el resultado ("updated": true)

// backend/prompts
type Context struct { …; Compact bool }                // agent-compact.{es,en}.md

// backend/hermes (17.3)
type Step struct { …; RoundInput, RoundOutput int64 `json:"roundInput","roundOutput"`; ResultChars int `json:"resultChars"`; ResultTokens int `json:"resultTokens"` }
type Usage struct { …; ModelMs, ToolMs int64; Rounds int; PeakInput, LastInput int64 }  // json camelCase

// app (17.3)
func (a *App) SetChatToolProfile(chatID, profile string) error   // "", "essential", "full"
func (a *App) NewChatWithSummary(chatID string) (store.Chat, error)
// store.Chat.Tools string `json:"tools"`; migración aditiva chats.tools TEXT NOT NULL DEFAULT ''
// evento chat:context {chatId, numCtx, promptTokens, toolsTokens, profile, auto, overBudget}
```

## Criterio de terminado

- Las pruebas nuevas fijan: ediciones todo-o-nada, errores con la línea más parecida, `set_parameters` sin hijo, `lib/`
  con historial y rutas hostiles rechazadas, topes de salida (tabla de bytes), perfil esencial, prompt compacto ≤ 1 200
  tokens, simulación Ollama 8k, desenvoltura agy 1.4.x, shell bloqueada, turno sin texto, 3MF (volumen con signo, manifold,
  aristas, bbox, transformaciones) nativo / WASM / Go.
- `gofmt`, `go build`, `go vet`, `go test -race -timeout 20m ./...`, build de Windows, `pnpm build`, `pnpm test`,
  `./scripts/i18n-check.sh`, `uishot-fit.sh`, `uishot-persist.sh`, `wails build`.
- Capturas nuevas/actualizadas y miradas: tarjetas con tiempo y tokens, pie del turno, selector de herramientas
  (esencial), aviso de contexto. Ayuda es/en, CHANGELOG, `claude/35` + 13, 19, 25, 26, 34, 11.
