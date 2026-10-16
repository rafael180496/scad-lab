# Checkpoints: papelera, restaurar y padre activo

Operaciones sobre los checkpoints del workspace (S3). Código en
`backend/checkpoint` (la misma lógica la usan los bindings de la app y el
servidor MCP). Spec: [.claude/specs/s3-checkpoints-avanzados.md](../.claude/specs/s3-checkpoints-avanzados.md).
Formato del archivo y creación: [claude/10-editor.md](10-editor.md).

## Nunca se borra

Regla 3 de [technical.md](../.claude/rules/technical.md): ningún código de
ScadLab borra un `.scad`. «Borrar» = **mover** a
`<ws>/.scadlab/trash/<AAAAMMDD-HHMMSS>-<archivo>` (hora local). No hay «vaciar
papelera» en la app; el usuario puede borrar la carpeta a mano.

| Función | Qué hace |
|---|---|
| `Trash(ws, file)` | manda un checkpoint; devuelve su nombre en la papelera. Los hijos **no** se mueven: quedan huérfanos (su `parent=` sigue apuntando al archivo) hasta que se restaure |
| `TrashBranch(ws, file)` | el archivo y todos sus descendientes con la misma marca de fecha; devuelve los nombres (el de `file` primero, luego por número). Mueve las hojas primero y `file` al final: si falla a medias queda un árbol conectado, y se devuelve lo movido más el error |
| `Descendants(ws, file)` | hijos, nietos… (sin `file`), por número; `file` no tiene que existir; tolera ciclos |
| `ListTrash(ws)` | `[]TrashedItem{Name, Original, Parent, Description, TrashedAt}`, lo más reciente primero. Ignora lo que no tiene el formato de papelera |
| `Restore(ws, trashedName)` | vuelve a su nombre original si está libre; si no, al siguiente `NNN` libre con el mismo slug |

`Restore` **mueve sin reescribir**: el encabezado `@checkpoint` queda intacto.
Consecuencia: si cambió de número, los hijos que decían `parent=003-x.scad` no se
reenganchan (también son inmutables) y la rama restaurada aparece como raíz. Con
el nombre original libre —el caso normal— el vínculo se recompone solo, y
restaurar una rama completa devuelve el árbol tal cual.

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
`list_checkpoints` informa `activeParent` (`""` si no hay). El MCP no expone
papelera ni restaurar: son decisiones del usuario, desde la app. Las reglas del
workspace (`assets/workspace/RULES.md.tmpl`) se lo explican al agente.

## Pruebas

`backend/checkpoint/{active,trash}_test.go` (tabla con `t.TempDir()`: nombre libre
y ocupado, ramas con nietos, activo que avanza / se mueve / queda colgado, nombres
hostiles, enlaces simbólicos) y `TestActiveParentInMCP`. La hora del borrado se
fija con la variable `now` del paquete.

## Reglas del workspace: bloque marcado

`workspace.WriteRules` (al crear, importar, abrir y cambiar de impresora) ya no
reescribe `CLAUDE.md`/`AGENTS.md` enteros: fusiona el bloque entre
`<!-- scadlab:rules:begin … -->` y `<!-- scadlab:rules:end -->`
(`workspace.mergeRules`). Archivo sin marcas con el título «— workspace de
ScadLab» = generado por una versión vieja → copia a `.scadlab/backup/` y
reemplazo. Sin marcas y sin ese título = archivo del usuario → bloque arriba,
contenido intacto debajo. Idempotente; escritura atómica.
