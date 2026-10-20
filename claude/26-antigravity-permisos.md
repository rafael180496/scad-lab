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
