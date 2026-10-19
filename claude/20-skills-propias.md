# Skills propias: editor, papelera, renombrar y carpeta

Configuración → Skills (`SkillsSettings.tsx`, `SkillEditor.tsx`; Go en `app_skills.go`,
`app_skills_files.go` y `backend/skills/`). Las **incluidas** se ven (`Ver`, solo lectura)
y se apagan; las **propias** se editan, renombran y eliminan. Qué skills hay y cómo se
instalan: [02-architecture.md](02-architecture.md#skills).

## Dónde viven

`settings.skillsDir` (vacío = `<datos>/skills`). Es una clave más de la tabla key/value:
**sin migración**. Solo cambia por `ChangeSkillsFolder`, y `Store.SaveSettings` **no la
escribe** (`delete(fields, "skillsDir")`): el diálogo de Configuración guarda una copia vieja.
Una ruta relativa guardada a mano se ignora.

`ChangeSkillsFolder(dir, move)` (`""` = la predeterminada): `skills.CheckFolder` exige ruta
absoluta, no raíz del disco, distinta de la actual y **no dentro de ella** (mover metería las
skills dentro de sí mismas). Con `move`, `MoveAll` lleva cada carpeta con `SKILL.md`; si el
destino ya tiene una con ese nombre **no la pisa** (queda en `skipped`); si falla a medias,
lo movido vuelve y la carpeta no cambia. Sin `move` solo se apunta: las de la anterior dejan de
verse y **las copias ya instaladas en los workspaces no se tocan**.

Una carpeta que no existe (disco externo desmontado) no es una carpeta vacía:
`refreshSkillRegistry` no olvida las filas `user` mientras no esté, para no perder cuáles
estaban apagadas.

## El botón de carpeta no hacía nada

`OpenSkillsFolder` pasaba `<datos>/skills` a `osopen.Open`, que lanzaba `open` y no miraba
cómo terminaba. La carpeta no existe hasta crear la primera skill; `open` salía con 1 y el
error nunca llegaba a un aviso. Ahora: `OpenSkillsFolder` hace `MkdirAll` antes, y `osopen.Open`
comprueba que la ruta exista y en macOS espera a `open` (8 s) y devuelve su salida; Windows
(`explorer.exe` devuelve 1 aunque abra bien) y Linux solo informan si el proceso no arranca.
Los tests inyectan `openFolder` (variable de `app_skills.go`): **nada abre el Finder**.

## Eliminar: papelera, no borrado

`DeleteSkill` mueve la carpeta a `<datos>/trash/skills/<AAAAMMDD-HHMMSS>-<nombre>` (mismo
sello que la papelera de checkpoints, que sigue en `.scadlab/trash` del workspace). Un
archivo suelto va a `<datos>/trash/skill-files/`. No hay botón de vaciar: recuperar es mover
de vuelta a mano. `skills.MovePath` renombra y, si no se puede (otro disco), copia y borra el
origen; nunca pisa, y un enlace simbólico impide mover entre discos.

**Copias en los workspaces**: `Sync` escribe en `.scadlab-managed` el origen y el
`sha256` del contenido instalado. Al borrar o renombrar, `PlanInstalled` revisa los cuatro
`AgentDirs` de **todos** los workspaces y solo retira una copia con marca `user` cuyo contenido
sigue igual a ese hash («nadie la tocó»); una marca vieja sin hash se compara con la skill
original. Lo editado a mano va en `kept` y se avisa; lo que no tiene marca no es de ScadLab y
no se mira. Comparar con el hash y no con la skill actual evita llamar «editada» a una copia
que solo quedó vieja porque el workspace estaba cerrado al guardar.

## Renombrar

`RenameUser`: carpeta, `name:` del `SKILL.md` (`setFrontmatterName`, solo dentro del
frontmatter) y copias instaladas. Valida `CheckName` (kebab-case, sin nombres reservados de
Windows) y rechaza el nombre de otra skill propia **o incluida** (una propia con el nombre de
una incluida la reemplaza: en un renombrado sería un accidente). Si reescribir el `name:` falla,
deshace el renombrado de la carpeta. Conserva encendida/apagada.

## Editor

Bindings: `ListSkillFiles`, `ReadSkillFile`, `SaveSkillFile`, `CreateSkillFile`,
`RenameSkillFile`, `DeleteSkillFile`, `ValidateSkillMarkdown`. **Toda ruta que llega es relativa
a la carpeta de la skill** y `skills.confine` la acota: rechaza vacía, absoluta, con unidad,
`\`, `..`, segmentos vacíos, caracteres que Windows no admite y `.scadlab-managed`, y además
resuelve enlaces simbólicos (de la ruta o de su ancestro existente) para que ninguno saque el
archivo de la carpeta. Se edita solo texto UTF-8 de hasta 1 MiB (el resto sale con
`text: false`); guardar es atómico (temporal + renombrado) y **no crea** archivos; `SKILL.md`
no se renombra ni se borra; las incluidas devuelven error de solo lectura al escribir.

El frontmatter lo valida **Go** (`skills.ValidateSkillMD`, códigos `SK001`–`SK009`) y el editor
solo marca lo que devuelve, con 300 ms de espera: una sola implementación, ya en el idioma
activo. Errores (`name`/`description` faltan o vacíos, `name` no kebab-case o distinto de la
carpeta, sin bloque `---`) **impiden guardar el `SKILL.md`** (`SaveSkillFile` devuelve
`saved: false` con los diagnósticos); los avisos no (descripción de ejemplo, > 1024 caracteres,
`: ` sin comillas). `name` ≠ carpeta es el caso que el CLI ignora sin decir nada.

Al guardar, crear, renombrar o borrar un archivo: `refreshSkillRegistry` (si fue el `SKILL.md`,
la descripción nueva se ve en la lista) y `syncOpenWorkspaces(nombre)`: el mismo `Sync` de
siempre pero **solo de esa skill** y solo en los proyectos abiertos (`settings.openProjects`); los
demás se sincronizan al abrirse. Ojo: `Sync` reescribe la copia aunque alguien la haya editado
a mano en el workspace (ya lo hacía al abrir).

`CreateSkill` crea desde la plantilla con el `name:` puesto y
`skills.DescriptionPlaceholder` como descripción (no la de la plantilla), **no abre el Finder** y
la interfaz abre el editor. No instala nada hasta el primer guardado.

## Frontend

- `SkillCodeEditor` (chunk de Monaco, lazy; `lazy.ts`) usa **un modelo por archivo** (cada uno
  conserva su deshacer), Markdown (`languages/definitions/markdown/register.js`, local) u
  OpenSCAD según la extensión. Markdown y texto envuelven línea siempre; OpenSCAD usa el ajuste.
  `fixedOverflowWidgets: false`: dentro de un `<dialog>` los widgets sacados al `<body>`
  quedarían detrás.
- ⌘S / Ctrl+S: listener en captura en `SkillEditor`; `EditorPane` ignora el atajo si hay un
  `dialog[open]` (si no, abriría «guardar checkpoint» detrás).
- `Dialog` acepta `onCancel`: con él, Escape no cierra solo y pregunta por los cambios sin
  guardar. `confirmDialog(..., {danger: true})` pinta el botón de confirmar (`Button`
  `variant="destructive"`).
- Banco de capturas: `lib/skillsShot.ts` (`skillsShot.mode`) deja la pestaña en el estado a
  fotografiar; vistas `skills-editor`, `skills-editor-errors`, `skills-view`, `skills-delete`,
  `skills-folder` y `settings-skills`.

## Regla de sincronización

Un código de diagnóstico nuevo en `ValidateSkillMD` no necesita cambios en TS (el mensaje
viaja hecho); un campo nuevo de `SkillsFolder`/`DeleteSkillResult`… sí: regenerar con
`wails generate module`.
