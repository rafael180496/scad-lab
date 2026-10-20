# Antigravity: aprobar las herramientas de ScadLab en `--print` (S12.c, T14)

Sigue a [19-hermes.md](19-hermes.md) (CLIs como proveedores del chat) y a [13-mcp.md](13-mcp.md) (el servidor y su registro global en Antigravity).

Bug real: con Antigravity en «Agente», el MCP ya registrado (`agy mcp list` lo muestra `enabled`), el chat terminaba con «Antigravity
terminó el turno sin responder nada» y la tarjeta `call_mcp_tool` con `permission check failed for mcp "scadlab/printer_info": user denied
permission`. **En `--print` Antigravity soft-deniega la primera herramienta MCP sin aprobar y TERMINA el turno** (`Print mode:
soft-denying tool confirmation "CallMcpTool"` y, 20 ms después, `CLI store manager shutting down`): el modelo ni llega a ver el rechazo, así
que no es el adaptador el que pierde el texto, sino que Antigravity no dice nada. `--mode accept-edits` aprueba ediciones de archivos, no MCP;
`--mode auto` aprobaría también comandos de shell y `--dangerously-skip-permissions` todo: ninguno se usa.

## Lo que se midió con el `agy` real (1.4.x) y por qué NO son los hooks

La primera versión (T14) usaba el mecanismo que Antigravity documenta, los hooks: `<workspace>/.agents/hooks.json` con un `PreToolUse` sobre
`call_mcp_tool`. **No funciona**, y se retiró:

- Con un `hooks.json` cargado (el log dice `loaded 1 named hooks`), en `agy -p` la herramienta MCP igual se rechaza con `Print mode:
  soft-denying tool confirmation "CallMcpTool"`. El hook ni se ejecuta (uno de prueba que registraba su stdin nunca escribió nada): **la
  confirmación de permiso de un MCP ocurre antes de los hooks**.
- Tampoco funcionan `<workspace>/.agents/settings.json` con `permissions.allow` (el log sigue en `permissions=<nil>`) ni un proyecto propio en
  `~/.gemini/config/projects/*.json` con `agentPermissions` («Unknown field») o `permissionGrants` («no grants»).
- **Lo que SÍ funciona** es lo que el propio `agy` pide en su error («Add an allow-rule under permissions.allow in settings.json (e.g.
  mcp(<target>))»): en `~/.gemini/antigravity-cli/settings.json`, `"permissions": {"allow": ["mcp(scadlab/list_checkpoints)"]}`. Formato
  exacto `mcp(<servidor>/<herramienta>)`. Con eso `agy -p` llamó a la herramienta y respondió. El log lo confirma al arrancar:
  `CLI settings initialized: permissions=&{Allow:[mcp(scadlab/list_checkpoints)] …}`.

El archivo es **global** (todos los proyectos). El usuario autorizó que ScadLab gestione ahí los permisos de **sus** herramientas.

## Cómo queda

**1. Las reglas** (`backend/clichat/agyperms.go`, `agyperms` sobre `settings.json`; `jsonobj.go`):

- Una regla `mcp(scadlab/<herramienta>)` por **cada** herramienta del catálogo —las de `readTools` y las de `writeTools` de `modes.go`, una sola
  definición, así que una herramienta nueva queda aprobada sin más; así `update_checkpoint` (S16, [34](34-historial-checkpoints.md)) entra en las reglas con la próxima sincronización—. Lista **explícita**: nunca `mcp(*)`, `mcp(scadlab/*)` ni `command(...)`.
  `IsAgyRule` reconoce solo `mcp(scadlab/[A-Za-z0-9_]+)`: cualquier otra regla —un comodín del usuario, otro servidor— no es nuestra y no se toca.
- **Combina sin tocar el resto**: el archivo se lee como objeto con el orden de sus claves y el texto de cada valor intactos (`jsonobj.go`),
  y solo cambia `permissions.allow`: conserva las reglas del usuario donde estaban, deja las nuestras que ya estaban, quita las
  `mcp(scadlab/…)` de herramientas que ya no existen o repetidas y agrega al final las que faltan. `deny`, `ask`, `trustedWorkspaces` y las
  demás claves no se tocan; un archivo sin cambios que hacer **no se reescribe**. Al quitar, si `allow` o `permissions` quedan vacíos se
  borran (el archivo vuelve a lo que tenía). BOM y modo (`0600`) se conservan; un enlace simbólico se escribe por su destino.
- **Escritura atómica con copia previa**: temporal + `rename`; antes, copia del archivo tal cual en `<datos>/backups/antigravity/settings-<fecha>.json`
  (las últimas 10; sin carpeta de copias no se escribe). Si Antigravity reescribe el archivo entre la lectura y el reemplazo (guarda
  ahí sus carpetas de confianza), se vuelve a leer y se rehace: no se pierde su cambio.
- **Un archivo que no es JSON** (o `permissions`/`allow` de otro tipo, o con comentarios) **no se pisa**: `ErrAgySettingsUnreadable`, un aviso con la
  ruta y el turno sigue sin permisos.

**2. Cuándo se sincroniza** (`app_agyperms.go`, `app_mcp.go`): `EnsureAntigravityMCP` registra el servidor (`agy mcp add`, solo si falta) y
**después sincroniza los permisos**, en cada llamada: la UI al elegir `cli:antigravity`, `prepareCLIMCP` al empezar cada turno y
`StartAgentTerminal`. La primera vez agrega las reglas junto al `agy mcp add` y emite **un** `mcp:antigravity {added, permissions,
settingsPath, error}` (toast es/en: qué se agregó y dónde); después solo avisa si agregó algo nuevo o falló. Agregar las herramientas nuevas y
quitar las `mcp(scadlab/…)` que ya no existen son parte de la misma sincronización. Con el MCP apagado en Configuración no se toca nada; sin
`agy`, tampoco. `agySettingsPath` (variable) es el único camino al archivo.

**3. Configuración → Proveedores de IA → CLIs detectados** (`AgyPermissions.tsx`): bajo Antigravity, «Permisos de ScadLab: N herramientas
aprobadas» con **Quitar** (`RemoveAntigravityPermissions`: borra solo las `mcp(scadlab/…)`, con confirmación) y **Volver a agregar**
(`RepairAntigravityPermissions`). **Quitar es una decisión que se respeta**: deja `<datos>/antigravity-permissions-off` y ni los turnos ni el
registro vuelven a agregarlas solas hasta «Volver a agregar» o «Reparar»; el mensaje de un bloqueo dice entonces que las quitaste.
`AntigravityPermissionsStatus` (solo lectura) trae `state` (`ok`/`partial`/`none`/`unreadable`/`unavailable`/`not-installed`), `approved`,
`total`, `stale`, `managed`, `path`.

**4. «Solo consulta»** (`backend/turnmark`, `backend/mcp/turnguard.go`): la regla global aprueba también las herramientas de escritura, así que
el modo lo hace cumplir **el servidor `--mcp`**. Durante un turno de Antigravity en «Solo consulta», `runCLITurn` escribe `<workspace>/.scadlab/turn-mode`
(`{"turns":[{"id","mode":"ask","pid","expires"}]}`) y la quita al terminar (`defer`, idempotente). El servidor, **antes de cada herramienta con
efecto** (`EffectOf != EffectNone`, la definición de `effects.go` que una prueba obliga a coincidir con `WriteTools()`), lee la marca y, si hay un
turno de consulta vigente, contesta `isError` con «Este turno es «Solo consulta» y la herramienta «X» de ScadLab modifica el proyecto: no está
permitida. Entrega el diseño en UN bloque…». Detalles que importan:
  - **Vencimiento y latido**: cada entrada vence a los 90 s y la app la renueva cada ~22 s mientras el turno corre; si la app muere, la marca deja
    de valer sola (nunca queda huérfana). Una marca ilegible cuenta como consulta mientras es reciente (ante la duda no se escribe) y se ignora si
    es más vieja que el vencimiento.
  - **Dos turnos a la vez** (dos chats del mismo workspace, o dos ScadLab): las entradas conviven (el registro en memoria reescribe el archivo
    entero y conserva las vigentes de otros procesos) y **gana el más restrictivo**: mientras haya un turno de consulta vivo no se escribe, también para
    otro cliente MCP del mismo workspace (el servidor no sabe a qué turno pertenece cada llamada).
  - En «Agente» no hay marca. Si la marca **no se puede escribir**, el turno de consulta **no corre** («No se pudo marcar el turno…»): sin ella el
    servidor dejaría escribir.
  - Hermes (la caja en proceso) no lee la marca: decide su modo por su cuenta.

**5. El workspace del servidor** (`main_mcp.go`): el registro global de Antigravity es `<ScadLab> --mcp` **sin `--workspace`** (no puede llevar una carpeta), y
de dónde hereda el servidor su directorio actual depende de Antigravity (el log muestra que `agy` lanza un servidor de lenguaje que a su vez lanza
los MCP: **no verificado** que su cwd sea el proyecto). Por eso la prioridad es `--workspace` → **`SCADLAB_WORKSPACE`** → directorio actual, y el chat
pone `SCADLAB_WORKSPACE=<carpeta del workspace>` en el entorno de cada turno de `agy` (`cliTurn`). Si Antigravity no pasa el entorno al MCP y
tampoco su cwd es el proyecto, el servidor trabajaría en otra carpeta —y la marca de «Solo consulta» no la vería—; en su primera línea de stderr
dice qué carpeta usó y de dónde (`scadlab --mcp: workspace <dir> (SCADLAB_WORKSPACE|cwd|--workspace)`): es lo primero que se mira. Se prueba con un
`agy` de mentira que arranca el servidor real con cwd distinto.

**6. Si igual se bloquea** (`agyDenial`, `agyDeniedNote`, `cliTurnText`, `emitAgyDenied`): el mensaje del chat nombra la herramienta real, la ruta del
`settings.json` (`permissions.allow`), el detalle del rechazo y «Reparar permisos de Antigravity»; en consulta también (antes solo en «Agente»). Si el turno ya
sabía por qué (el archivo ilegible…), ese motivo **es** la explicación y el prompt le dice al modelo que entregue el bloque (que la red de seguridad guarda y
renderiza). La app emite `mcp:antigravity-denied {tool, settingsPath, managed}` y `App.tsx` muestra el aviso con el botón **Reparar permisos de
Antigravity** (`RepairAntigravityPermissions`, que vuelve a sincronizar). `unwrapAgyMCP` (adaptador) vuelve la tarjeta `call_mcp_tool` la herramienta que es.

**7. Limpieza del mecanismo anterior**: `--agy-hook`, `.agents/hooks.json`, `SCADLAB_AGY_MODE` y `SCADLAB_AGY_HOOK_LOG` ya no existen. Al abrir, crear o
importar un workspace (`syncMCP`) se quita la entrada `scadlab-managed` de un `.agents/hooks.json` heredado sin tocar las del usuario (y se borra el
archivo —y `.agents/` vacía— si era solo nuestra); un archivo que no es JSON no se toca. La línea `.agents/hooks.json` ya no está en el `.gitignore`
de un workspace nuevo.

**8. La confianza de carpeta** (`agytrust.go`) era la condición para que Antigravity cargara el hook; con permisos globales **ya no es un requisito** y el
turno no la comprueba. Como no se pudo probar qué hace `agy` en una carpeta sin confianza (las pruebas del usuario fueron en una de `trustedWorkspaces`),
queda **solo como pista**: si un turno termina bloqueado y la carpeta no figura en `trustedWorkspaces`, el mensaje agrega que puede ser la causa
(«Yes, I trust this folder»). Nunca bloquea un turno ni escribe en el archivo.

## Pruebas

Todas con `HOME`/`SCADLAB_DATA_DIR`/settings en directorios temporales: **ninguna toca el `~/.gemini` real ni lanza el `agy` real**.

- `backend/clichat/agyperms_test.go`: el catálogo exacto sin comodines; vacío, con `trustedWorkspaces`, con reglas propias, con reglas viejas o repetidas;
  orden de claves y números intactos; idempotencia (no reescribe); copia previa byte a byte y poda; JSON inválido (8 formas) sin tocar el archivo;
  sin carpeta de copias; enlace simbólico; BOM; el cambio concurrente de Antigravity; quitar y limpiar lo vacío; y una propiedad: sobre un corpus,
  **nunca cambia nada que no sea `mcp(scadlab/…)`**. `agytrust_test.go`, `agymcp_test.go` (adaptador), limpieza del `hooks.json` heredado.
- `backend/turnmark/turnmark_test.go` (`-race`): escribir y retirar, vencida, latido, ilegible, dos turnos, 16 en paralelo, entradas de otro proceso, fallo al escribir.
- `backend/mcp/turnguard_test.go`: el servidor rechaza **cada** herramienta de `WriteTools()` en consulta y deja pasar la lectura; sin marca o con `agent` escribe;
  la caja de Hermes la ignora.
- `app_chat_agy_test.go`: un `agy` de mentira que lee `permissions.allow` de un settings temporal, rechaza como el real lo que no tiene regla y ejecuta lo demás
  contra **el `--mcp` real** (este binario relanzado): aprobación y creación de un checkpoint de verdad, lo que Antigravity ve en el archivo, el aviso, la
  sincronización en cada turno, «Solo consulta» frenada por el servidor, el workspace por `SCADLAB_WORKSPACE`, el bloqueo con mensaje y «Reparar», el archivo ilegible,
  el turno que no corre sin marca, el MCP apagado, el estado/quitar/reponer y la limpieza del hook heredado.

## Límites sin verificar con una sesión real

El usuario lo prueba con el `agy` real (abrirla gasta cuota): que **el archivo se relea en cada `agy -p`** (cada turno es un proceso nuevo y el log lo
muestra al arrancar, así que sí), que un `agy` con **otra versión** acepte el mismo formato, el cwd/entorno del servidor MCP lanzado por Antigravity
(punto 5), qué hace `agy` en una carpeta sin confianza, y Windows (`settings.json` en `%USERPROFILE%\.gemini\antigravity-cli`; compila, no se probó).
Cómo diagnosticar: el log de `agy` (`~/.gemini/antigravity-cli/log/cli-*.log`) dice `CLI settings initialized: permissions=&{Allow:[…]}` con las reglas
que cargó, y `Print mode: soft-denying tool confirmation "CallMcpTool"` si igual rechazó una herramienta.

## S17: la herramienta real, la shell y el turno sin respuesta (17.3, G)

Tres cosas que salieron de leer una sesión real de `agy` 1.4.x (`~/.gemini/antigravity-cli/brain/<id>/.system_generated/logs/transcript_full.jsonl` y `log/cli-*.log`, solo lectura).

**1. La tarjeta decía `call_mcp_tool`.** En 1.4.x los argumentos de la llamada viajan en `step_update.tool_info.parameters` (`ServerName`, `ToolName`, `Arguments`) —como objeto o como un JSON dentro de un texto— y ya no en `tool_input`; `unwrapAgyMCP` no los veía. `agyToolInfoInput` (`adapters.go`) los saca de ahí y `tool_info.parameters` desemboca en el mismo `unwrapAgyMCP`: la tarjeta es `edit_checkpoint`, `render`… y `mcp.CreatesCheckpoint` la reconoce. Si el resultado de un paso viene dentro de `tool_info` como objeto (`output`, `result`, `error`, `message`) también se lee. En un paso que **falló**, `tool_info` es un texto con el motivo y sigue cayendo en el detalle del paso. Fixture saneado: `backend/clichat/testdata/antigravity-mcp-1.4.jsonl` (sin rutas ni datos de nadie; el contorno `tool_info.parameters` es el que describió el usuario y el contenido —`ServerName`/`ToolName`/`Arguments`— el que muestra el transcript real). **El JSON exacto del stream de 1.4.x no se pudo ver en esta máquina** (el transcript guarda las llamadas como `args`, no el NDJSON de `--output-format stream-json`): ver «Para probar» abajo.

**2. La shell: ya está bloqueada, y por eso el turno terminaba.** Lo medido (`cli-20261020_*.log` y el transcript):

```
Print mode: soft-denying tool confirmation "RunCommand" at step 163
step 74  ERROR  permission check failed for unsandboxed "openscad -v": user denied permission to run command: …
                Do not attempt to circumvent this denial by rephrasing the command…
```

En `agy --print` **todo lo que pide confirmación se rechaza** (igual que el MCP sin regla, [arriba](#lo-que-se-midió-con-el-agy-real-14x-y-por-qué-no-son-los-hooks)) y el turno termina ahí: `run_command` nunca llegó a ejecutarse —la tarjeta «Agente» que se veía era el intento rechazado—, y el «terminó el turno sin responder nada» era ese rechazo, no una respuesta perdida. Lo que decidí, y por qué:

- **No se pasa `--sandbox`.** `agy --help`: «Run in a sandbox with terminal restrictions enabled». La guía local de Antigravity (`builtin/skills/antigravity_guide/references/app.md`) lo describe como una política de ejecución —`proceed-in-sandbox`—: los comandos **corren dentro de un sandbox** sin pedir permiso. Es lo contrario de bloquear: habilitaría la shell. Tampoco `--mode auto` (aprueba comandos) ni `--dangerously-skip-permissions`. `TestAntigravityNeverEnablesTheShell` (`args_test.go`) fija el argv: sin `--sandbox`, `--mode` solo `accept-edits` en «Agente» y nada en «Solo consulta».
- **No se escribe ningún `deny` global.** La única regla de shell que documenta el propio `agy` es `command(...)` en `permissions.deny` del `settings.json` global, que afectaría también a la terminal interactiva del usuario; y como `--print` ya la rechaza, no aporta. Sigue valiendo lo de [arriba](#cómo-queda): ScadLab solo mantiene las reglas `mcp(scadlab/…)`.
- **Los hooks no sirven.** `PreToolUse` con `matcher: run_command` y `decision: deny` es lo que documenta `builtin/skills/agy-customizations/docs/hooks.md`, pero T14 midió que la confirmación de permiso va **antes** de los hooks (para MCP) y aquí la confirmación de `RunCommand` es la misma; y exigiría escribir un `hooks.json` y un script en el proyecto del usuario.
- **Se le pide al modelo que no lo intente** (`msgCLIPromptAgyShell`, en el prompt del turno de Antigravity): «no uses `run_command` ni la terminal… responde siempre con un texto breve» — un modelo que no intenta la shell no termina el turno.
- **Se explica cuando lo intenta igual** (`agyShellDenial`, `msgCLIAgyShell`): el chat nombra el comando que Antigravity se negó a ejecutar, dice que no se ejecutó nada y qué hacer (pedir que use `edit_checkpoint`/`set_parameters`/`render`, o cambiar de proveedor). Solo con el texto de rechazo real de Antigravity (`permission check failed` / `user denied permission to run command`), no con cualquier fallo de la shell. La nota no repite el rechazo crudo.

**3. El turno sin texto cuenta qué hizo.** `cliTurnText` (CLIs) y `runChatTurn` (servidores) ya no dejan solo «terminó sin responder nada»: `turnRecap` (`app_chat_recap.go`) agrega «Esto es lo que hizo antes de terminar»: las herramientas en orden (`outline_checkpoint` → `edit_checkpoint` → `render ×2`, con ✗ en las que fallaron), los archivos que quedaron creados o cambiados (o «no quedó ningún checkpoint») y el resultado de la última herramienta resumido a sus campos útiles (`file`, `status`, `updated`, `watertight`…). El resultado que ya explica otra nota —un rechazo de permisos, la shell— no se repite.

### Para probar con el `agy` real (no se pudo aquí)

Ninguna abre un prompt en esta sesión de desarrollo: son comandos para que los corras tú, en una carpeta cualquiera.

```sh
# 1. ¿La shell se rechaza en --print? Esperado: un paso run_command con state ERROR y «permission check failed for unsandboxed …».
agy --print "ejecuta echo hola con run_command" --output-format stream-json --mode accept-edits | tee /tmp/agy-shell.jsonl
grep -c 'soft-denying tool confirmation "RunCommand"' ~/.gemini/antigravity-cli/log/cli-*.log   # sube en 1 por intento

# 2. ¿--sandbox ejecuta el comando en vez de rechazarlo? (por eso ScadLab NO lo pasa). Esperado: el paso termina en DONE con «hola».
agy --sandbox --print "ejecuta echo hola con run_command" --output-format stream-json --mode accept-edits

# 3. El NDJSON exacto de una llamada MCP en 1.4.x (donde está tool_info.parameters):
agy --print "llama a la herramienta printer_info del servidor scadlab" --output-format stream-json | head -40 > /tmp/agy-mcp.jsonl
#   compara el step_update ACTIVE de call_mcp_tool con backend/clichat/testdata/antigravity-mcp-1.4.jsonl;
#   si el contorno es otro, el cambio va en agyToolInfoInput (adapters.go) y el fixture.

# 4. En el chat de ScadLab con Antigravity en «Agente»: pide «mide la maceta con python»: la tarjeta run_command sale roja y el
#    mensaje nombra el comando bloqueado y dice «Nothing was run / No se ejecutó nada».
```
