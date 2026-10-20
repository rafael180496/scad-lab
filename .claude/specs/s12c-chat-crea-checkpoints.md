# S12.c — El chat siempre termina en un `.scad` renderizado

Sigue a [S12](s12-hermes-proveedores.md) y [S12.b](s12b-cli-chat-ux.md).

## Problema (prueba real del usuario)

Workspace sin checkpoints, «Una caja con tapa a presión para un Raspberry Pi 5»,
modo «Solo consulta» (el predeterminado de los CLIs):

| Proveedor | Qué pasó | Causa |
|---|---|---|
| Antigravity | una tarjeta `run_command` ERROR «Sin resultado», sin texto | `agy mcp list` = «No MCP servers configured»: Antigravity **solo** lee MCP global (no hay por proyecto ni por flag; `agy --help`) y en S6 quedó como botón opcional → sin herramientas de ScadLab intenta la shell, que está denegada; el adaptador no muestra el detalle del error |
| Codex, Claude | diseño completo en texto, «no creé el checkpoint: Solo consulta» | modo por defecto «Solo consulta» + un prompt que dice «no crees checkpoints» |
| Ollama (Hermes) | solo texto | modelo sin tool calling → modo de respaldo que solo muestra el bloque |
| «Guardar como checkpoint» | «no hay checkpoint que sirva de padre» | `ChatCheckpointParent` exige padre; `SaveAsCheckpoint` no usa `checkpoint.CreateRoot` |
| Visor/Código vacíos | sin `001` | `WriteStarter` corre solo al crear/importar; el workspace es anterior a S12 o quedó vacío |

## Visión del usuario (rige esta fase)

> «La idea de este proyecto es hacer modelos sin saber modelaje 3D, al 100 % con
> ayuda de la IA, así que la IA tiene que tener los tools necesarios para hacer
> la creación y todo lo del proyecto: scad, stl, etc.» — y: «por defecto debe ser
> escritura, ya que el chiste de esto es modificar y crear modelos».

Principios en [claude/00-overview.md](../../claude/00-overview.md#visión-y-principios-palabras-del-usuario).

## Objetivo

Para alguien que no sabe OpenSCAD, pedir una pieza **termina siempre** con un
`.scad` creado, renderizado y visible en el visor, con cualquier proveedor.

## Decisiones

1. **Modo por defecto «Agente»** para toda conversación nueva y todo proveedor.
   «Solo consulta» es explícito y se recuerda solo si el usuario lo eligió
   (la preferencia guardada de S12.b que arrancaba en consulta para CLIs se
   ignora una vez: se migra al nuevo default en el frontend).
2. **Primer checkpoint sin padre**: `ChatCheckpointParent` devuelve `""` en un
   workspace vacío y `SaveAsCheckpoint`/`SaveChatCheckpoint` con padre `""`
   usan `checkpoint.CreateRoot` (`001-<slug>.scad`, `parent=` vacío).
3. **Workspace vacío al abrir**: `OpenProject` escribe el starter
   (`WriteStarter`) si la raíz no tiene ningún `.scad` **y** la papelera
   (`.scadlab/trash/`) está vacía. Justificación: el usuario pidió que sea
   automático; nunca pisa nada (`O_EXCL`), y la condición de la papelera evita
   resucitar el starter en un workspace que el usuario vació a propósito (ahí
   se muestra en el visor y en Código un botón «Crear un modelo de inicio»).
   Las reglas gestionadas (`CLAUDE.md`/`AGENTS.md`) se re-sincronizan al abrir
   si su bloque cambió.
4. **Red de seguridad común (CLIs y Hermes)**: si un turno en modo «Agente»
   termina **sin** haber creado ningún checkpoint y su texto trae un bloque
   OpenSCAD completo, ScadLab lo guarda como checkpoint (raíz si el workspace
   está vacío; si no, hijo del padre activo), lo renderiza y lo muestra como un
   paso más («Guardado automáticamente»). En Hermes (servidores), si el render
   falla y quedan pasos, los errores con línea vuelven al modelo para otra
   vuelta (bucle de respaldo sin tool calling).
5. **CLIs con las herramientas de escritura habilitadas de verdad en «Agente»**:
   comprobar argv por CLI (Claude `--allowedTools` con `create_checkpoint`,
   Codex `enabled_tools`, OpenCode `permission`, Antigravity `--mode`). El
   prompt de turno ya no dice «no crees»; en «Agente» ordena el flujo
   `create_checkpoint` → `render` → `preview` → corregir en uno nuevo, y en un
   workspace vacío **crear el primero sin preguntar**.
6. **Antigravity**: al elegirlo en el chat (o abrir su terminal), si `agy mcp
   list` no tiene `scadlab`, ScadLab lo registra solo con
   `agy mcp add -- scadlab <bin> --mcp` (sin `--workspace`: el servidor usa la
   carpeta actual, que es la del workspace) y avisa con un toast que se agregó
   a su configuración global. Un fallo de herramienta muestra el detalle real
   (el adaptador lee el error del evento) y un turno sin texto ni pasos útiles
   termina con un mensaje explicativo, nunca vacío.
7. **Ollama con tools**: verificar que con modelos que lo soportan (`/api/show`
   capabilities `tools`: qwen2.5-coder, qwen3…) se mandan las herramientas y el
   prompt de sistema; sin tools, el respaldo del punto 4.
8. **Solo consulta**: un bloque `.scad` en la respuesta muestra «Crear
   checkpoint y renderizar»; si el texto del usuario pide crear algo, aviso con
   «Cambiar a Agente» de un clic.
9. Al crearse un checkpoint desde el chat, el visor y Código lo seleccionan y
   renderizan solos y la tarjeta enlaza a él (ya existe `onCheckpointCreated`).

## Tareas (en paralelo, sin archivos compartidos)

**T9 — Código · `fullstack-dev`**: todo `app*.go`, `backend/clichat/**`,
`backend/hermes/**`, `backend/checkpoint/**`, `backend/workspace/**` (solo el
uso de `WriteStarter` y la re-sincronización), `frontend/src/**`, i18n,
`wailsjs`, `uishot.tsx`, `help-shots.sh`, `index.html`, `es/index.html`,
`CHANGELOG.md`, `claude/19-hermes.md`, `claude/22-chat-ux.md`, `claude/01-roadmap.md`.
El texto del prompt de turno de los CLIs vive en `app_chat_cli.go` (`cliPrompt`):
es de T9.

**T10 — Prompts y reglas · `openscad-dev`**: solo archivos de texto embebidos,
sin cambiar firmas Go: `assets/prompts/agent.{es,en}.md` (Hermes: en un
workspace vacío crear el primero de inmediato; nunca terminar sin
checkpoint; corregir con la línea del error), `assets/prompts/system.{es,en}.md`
(respaldo sin herramientas: **un** bloque OpenSCAD completo con la cabecera,
listo para guardar), `assets/workspace/RULES.md.tmpl` (flujo obligatorio con
las herramientas MCP para todos los CLIs), `backend/mcp/mcp.go`
(`serverInstructions`) y los tests de `backend/prompts` que dependan del texto.

## Tanda 2 de S12.c

**T11 — Auditoría de herramientas MCP · `openscad-dev`**: la IA tiene que poder
hacer todo lo que hace la UI. Lista pedida (workspace y archivos, modelado,
STL/3MF/OBJ con las operaciones del editor de S13, impresión, contexto), cada
herramienta clasificada lectura («Solo consulta») / escritura («Agente»),
tabla en `claude/13-mcp.md`. «Abrir en Bambu Studio» **no** es herramienta.

**T12 — Escritura por defecto en todos lados · `fullstack-dev`**: el modo es
**por conversación** (columna `chats.mode`, migración aditiva: `''` = Agente,
`ask` = elegido a mano); conversaciones existentes sin elección explícita pasan
a Agente; elegir «Solo consulta» no cambia el predeterminado global; la terminal
de agentes y `--mcp` no arrancan en solo lectura.

## Criterio de terminado
CLIs falsos que reproducen los casos reales (solo texto con bloque; `run_command`
fallido; uso de `create_checkpoint`), Ollama simulado sin y con tools, tests de
checkpoint raíz y starter al abrir; prueba de humo en la app con un workspace
vacío: tras un mensaje queda un `.scad` renderizado con Claude (falso), Codex
(falso), Antigravity (falso) y Hermes (sin y con tools).

## Tanda 3 de S12.c

**T14 — Antigravity aprueba las herramientas de ScadLab en `--print` · `fullstack-dev`**: en el chat con
Antigravity en «Agente» el MCP ya estaba registrado pero `agy --print` rechazaba la primera herramienta MCP
sin aprobar y **terminaba el turno** («terminó el turno sin responder nada»). Aprobar solo las herramientas
de ScadLab, sin `--dangerously-skip-permissions` ni relajar la shell. Primer intento: hook `PreToolUse` en
`<workspace>/.agents/hooks.json` (el documentado). **Medido con el `agy` real: no sirve**, la confirmación de permiso de
un MCP ocurre antes de los hooks. Lo que funciona —y el propio `agy` pide— es una regla `mcp(scadlab/<herramienta>)` en
`permissions.allow` de `~/.gemini/antigravity-cli/settings.json` (el usuario autorizó que ScadLab gestione ahí los
permisos de sus herramientas): una regla por herramienta del catálogo, sincronizada al registrar el MCP y en cada turno,
con Quitar/Volver a agregar en Configuración; «Solo consulta» lo hace cumplir el servidor `--mcp` con una marca en el
workspace. Mensaje accionable si igual se bloquea y pruebas con un `agy` falso. Detalle y resultado:
[claude/26-antigravity-permisos.md](../../claude/26-antigravity-permisos.md).
