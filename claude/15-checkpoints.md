# Checkpoints: papelera, restaurar y padre activo

Operaciones sobre los checkpoints del workspace (S3). Código en
`backend/checkpoint` (la misma lógica la usan los bindings de la app y el
servidor MCP). Spec: [.claude/specs/s3-checkpoints-avanzados.md](../.claude/specs/s3-checkpoints-avanzados.md).
Formato del archivo y creación: [claude/10-editor.md](10-editor.md).

## Nunca se borra

Regla 3 de [technical.md](../.claude/rules/technical.md): ningún código de
ScadLab borra un `.scad` por su cuenta. «Borrar» = **mover** a
`<ws>/.scadlab/trash/<AAAAMMDD-HHMMSS>-<archivo>` (hora local). Eliminar de verdad
existe, pero **solo como acción explícita del usuario** en la papelera de la app (ver
«Eliminación permanente»); ningún agente puede.

| Función | Qué hace |
|---|---|
| `Trash(ws, file)` / `TrashRelinked` | manda un checkpoint; devuelve su nombre en la papelera (y, `TrashRelinked`, los hijos que re-colgó). Los hijos directos **no** se mueven: pasan a colgar de su padre (o quedan raíz); ver «Re-colgado» |
| `TrashBranch(ws, file)` | el archivo y todos sus descendientes con la misma marca de fecha; devuelve los nombres (el de `file` primero, luego por número). Mueve las hojas primero y `file` al final: si falla a medias queda un árbol conectado, y se devuelve lo movido más el error |
| `Descendants(ws, file)` | hijos, nietos… (sin `file`), por número; `file` no tiene que existir; tolera ciclos |
| `ListTrash(ws)` | `[]TrashedItem{Name, Original, Parent, Description, TrashedAt}`, lo más reciente primero. Ignora lo que no tiene el formato de papelera |
| `Restore(ws, trashedName)` / `RestoreRelinked` | vuelve a su nombre original si está libre; si no, al siguiente `NNN` libre con el mismo slug. Deshace el re-colgado |
| `SetParent(ws, file, parent)` | reescribe solo el `parent=` de la cabecera (`""` = raíz); ver «Re-colgado» |
| `PurgeTrashed` / `EmptyTrash` / `StarterDeclined` | eliminación permanente; ver «Eliminación permanente» |

`Restore` **mueve sin reescribir** el archivo restaurado: su propia cabecera queda intacta. Con el nombre original libre
—el caso normal— vuelve con el mismo nombre; si cambió de número, los hijos que había re-colgado (ver abajo) se
re-cuelgan del nombre nuevo.

## Re-colgado de los hijos (`reparent.go`, `relink.go`)

`Trash` (suelto, no `TrashBranch`) deja a cada **hijo directo** del borrado colgando de **su padre** (el abuelo), o como
**raíz** (`parent=` vacío) si era raíz o si el abuelo ya no está: el árbol nunca queda con un `parent=` a un archivo ausente
y el usuario no ve el ⚠ ni el SL002 por algo que hizo a propósito. Los nietos no se tocan. Con `TrashBranch` se va todo el
subárbol y no hay nada que re-colgar.

- **`SetParent(ws, file, parent)` es la única excepción a la inmutabilidad** (regla 3): es metadato del árbol, no el
  modelo. Cambia **solo los bytes del valor de `parent=`** en la primera línea (misma regex que `workspace.Scan`, así lo
  escrito y lo leído coinciden): descripción, espacios, BOM, `\r\n` y el resto del archivo quedan byte a byte. Escritura
  atómica (temporal en la carpeta + rename) **conservando permisos y fecha de modificación** —cambiar de rama no es
  «haberlo editado»: el árbol muestra la fecha de creación—. No reescribe enlaces simbólicos (el rename los reemplazaría) ni
  archivos sin cabecera; si ya tenía ese padre no escribe. El padre nuevo no tiene que existir, sí ser un nombre válido sin
  espacios (`parent=` termina en el primero).
- **Registro para deshacer**: `<papelera>/<AAAAMMDD-HHMMSS>-<archivo>.scad.relink.json` = `{"parent": <de quién cuelgan>,
  "children": [...]}`. Termina en `.json`, así que `ListTrash`/`Scan` no lo ven. `Restore` lo consume: cada hijo que
  **siga existiendo y siga colgando de `parent`** vuelve a colgar del restaurado **con el nombre con que volvió**; uno que el
  usuario movió a otra rama no se toca, y no se re-cuelga si cerraría un ciclo. Un fallo al re-colgar no revierte el borrado
  ni la restauración: se devuelve el error junto con lo hecho (`TrashRelinked`/`RestoreRelinked` devuelven también la lista).
- **El render no se invalida**: `render.contentHash` ignora la línea `// @checkpoint …` (es un comentario; re-colgar la
  reescribe, y con ella en la clave cada borrado costaría re-renderizar —minutos en un modelo pesado— a todos los hijos).
  Efecto secundario: dos checkpoints con el mismo código comparten entrada de caché, y **la primera vez tras actualizar** se
  recalculan los renders ya guardados (la clave cambió una vez).
- **Workspaces con el problema ya hecho** (hijos con padre en la papelera, de antes): SL002 dice «parent=X está en la
  papelera: restáuralo o haz principal» (`scad.analysis.inTrash`), y **«Hacer principal»** (`MakeCheckpointRoot` →
  `SetParent(…, "")`) está en el aviso del panel Problemas y en el menú del nodo con ⚠. Al cambiar el `parent=` del
  checkpoint elegido, `App.tsx` relee su código (si no hay cambios sin guardar) para que el linter lo vea.

## Eliminación permanente (`purge.go`)

Pedida por el usuario: poder liberar la papelera. **Solo la dispara la interfaz** (`DeleteTrashed(id, name)`,
`EmptyTrash(id)`): **no hay herramienta MCP** equivalente y un test (`TestNoToolDeletesForGood`) falla si aparece una
herramienta o un parámetro que sugiera borrar. Mismo criterio que «Abrir en Bambu Studio»: lo irreversible es del usuario.

- `PurgeTrashed`: valida el nombre (`ValidName` + formato de papelera), solo toca un archivo suelto **dentro de**
  `.scadlab/trash`; un enlace se quita a él, no a su destino; una carpeta no se elimina por esta vía. Borra también su
  registro `.relink.json` y, desde la app, lo guardado en `checkpoint_trash` (`Store.ForgetCheckpointMeta`).
- `EmptyTrash`: vacía **todo** lo que haya en `.scadlab/trash` (checkpoints, registros y cualquier otra cosa: la confirmación
  lo dice) sin seguir enlaces; la carpeta queda. Devuelve cuántos checkpoints eran. `metaDir` rechaza una papelera que sea un
  enlace: nada fuera del workspace se toca. El botón está deshabilitado con la lista vacía.
- **Starter**: la regla de S12.c escribe el modelo de inicio al abrir un workspace sin `.scad` **y con la papelera vacía**,
  y vaciarla deja justo ese estado. Para que no reaparezca en un workspace que el usuario dejó vacío adrede, cuando una
  eliminación deja la papelera vacía se escribe `.scadlab/no-starter` (`StarterDeclined`) y `ensureStarter` lo respeta; el botón
  «Crear un modelo de inicio» sigue funcionando. Se eligió una marca y no «tratar la papelera vacía como llena» porque dura
  entre aperturas y no depende de que la papelera siga existiendo.
- La papelera de **skills** (`<datos>/trash/skills`) es otra y **no tiene** «Vaciar»: no hay un panel que la liste, así que
  una confirmación no podría nombrar qué se elimina. Pendiente si hace falta.

### Mover sin pisar

`moveNoReplace` falla con `fs.ErrExist` en vez de reemplazar: `os.Rename` pisa el
destino en macOS y Linux, y aquí pisar es perder un checkpoint. Usa `os.Link` (atómico,
nunca reemplaza) y quita el nombre de origen —el contenido sigue vivo bajo el
nuevo—; donde no hay enlaces duros, o si el origen es un enlace simbólico, cae a
comprobar y renombrar. Dos borrados del mismo nombre en el mismo segundo no se
pisan: la marca de fecha corre de a un segundo (hasta 100).

## Padre activo

`<ws>/.scadlab/active-parent`: una línea con el nombre de un checkpoint. Es **el
padre del próximo checkpoint**. Sin archivo rige lo de siempre: la rama del más
nuevo.

| Función | Qué hace |
|---|---|
| `ActiveParent(ws)` | el activo, o `""` si no hay, está mal formado, es muy grande, es un enlace o apunta a un archivo que ya no existe |
| `SetActiveParent(ws, file)` | lo fija (valida nombre y que exista); `""` lo limpia |
| `Advance(ws, parent, newFile)` | si el activo es `parent`, pasa a `newFile`; si es otro o no hay, no hace nada |

- **Avanza solo.** `Create` llama a `Advance`; el watcher de la app hace lo mismo
  cuando un agente escribe el archivo directo. `Advance` es idempotente, así que
  repetirlo no cambia nada. Un hijo de *otro* checkpoint no mueve el activo
  (crear una variante lateral no saca al usuario de su rama). `CreateRoot` no lo
  fija: sin activo ya rige «el más nuevo».
- **Un activo colgado es «ninguno», no un error.** Es una preferencia: un archivo
  que el usuario movió a mano no debe impedir crear checkpoints. Por la misma
  razón, `Create` ignora un fallo al guardar el activo (el checkpoint ya existe).
- **Si el activo se manda a la papelera pasa a su padre** (si existe): quien
  trabajaba en esa rama sigue en ella, un paso atrás, en vez de saltar al más
  nuevo, que puede ser de otra rama. Sin padre se limpia. En `TrashBranch`, si
  el activo estaba dentro de la rama, pasa al padre de la raíz de la rama.
  Restaurar **no** toca el activo.
- Escritura atómica (temporal en `.scadlab/` + rename). Un candado de proceso
  serializa Set/Advance/Trash; entre la app y el MCP no hace falta (idempotente).

## Seguridad de rutas

- Todo nombre pasa por `ValidName`: archivo suelto `*.scad`, sin carpetas ni `..`
  ni rutas absolutas, sin caracteres de control (NUL incluido), no `.scad` a secas.
- `.scadlab` y `.scadlab/trash` no se siguen si son enlaces simbólicos (ni puntos
  de unión de Windows): error y no se mueve nada. `active-parent` enlazado se lee
  como «sin activo». Se crean si faltan.

## Herramientas MCP

[claude/13-mcp.md](13-mcp.md): `create_checkpoint` sin `parent` usa
`ActiveParent` y, si no hay, el más nuevo; devuelve `activeParent` tras crear.
`list_checkpoints` informa `activeParent` (`""` si no hay). Desde S12.c (T11) el MCP
**sí** expone el árbol completo en «Agente»: `set_active_parent`, `rename_checkpoint` (el rótulo, no el archivo),
`trash_checkpoint` (`branch=true` con su rama), `list_trash` y `restore_checkpoint`, con la misma lógica de
`backend/checkpoint` que los bindings. «Borrar» sigue siendo mandar a `.scadlab/trash/`: ninguna herramienta borra. Como
esas cuatro no crean un `.scad`, la app reescanea y emite `checkpoints:changed` tras un paso bueno
([25-herramientas-ia.md](25-herramientas-ia.md#para-la-interfaz)); `trash_checkpoint` informa `relinked` (los hijos re-colgados) y `restore_checkpoint` también; este además marca el archivo como
restaurado (`markRestored`) para que el watcher no lo trate como trabajo nuevo. Las reglas del workspace
(`assets/workspace/RULES.md.tmpl`) se lo explican al agente.

## Pruebas

`backend/checkpoint/{active,trash}_test.go` (tabla con `t.TempDir()`: nombre libre
y ocupado, ramas con nietos, activo que avanza / se mueve / queda colgado, nombres
hostiles, enlaces simbólicos) y `TestActiveParentInMCP`. La hora del borrado se
fija con la variable `now` del paquete.

## Raíz sin padre y modelo de inicio al abrir (S12.c)

- `ChatCheckpointParent` devuelve `""` en un workspace sin ningún checkpoint y
  `SaveAsCheckpoint` / `SaveChatCheckpoint` con padre `""` usan `checkpoint.CreateRoot`
  (`001-<slug>.scad`, `parent=` vacío; no fija el padre activo). Con checkpoints ya en el
  árbol, un padre vacío sigue siendo un error: es casi siempre un padre olvidado.
- `openWorkspaceView` (`OpenProject`) llama `ensureStarter` **antes** del reescaneo: si la
  raíz no tiene ningún `.scad` **y** `checkpoint.TrashEmpty` (papelera sin nada) →
  `workspace.WriteStarter` (`O_EXCL`, nunca pisa). Con la papelera con algo, hubo trabajo y se
  lo mandó a la basura: no se resucita, y el visor y Código ofrecen «Crear un modelo de
  inicio» (`CreateStarterCheckpoint`, que ignora la papelera porque el usuario lo pide).
  Las reglas (`WriteRules`) ya se re-sincronizaban al abrir; hay una prueba que lo fija.

## Reglas del workspace: bloque marcado

`workspace.WriteRules` (al crear, importar, abrir y cambiar de impresora) ya no
reescribe `CLAUDE.md`/`AGENTS.md` enteros: fusiona el bloque entre
`<!-- scadlab:rules:begin … -->` y `<!-- scadlab:rules:end -->`
(`workspace.mergeRules`). Archivo sin marcas con el título «— workspace de
ScadLab» = generado por una versión vieja → copia a `.scadlab/backup/` y
reemplazo. Sin marcas y sin ese título = archivo del usuario → bloque arriba,
contenido intacto debajo. Idempotente; escritura atómica.
