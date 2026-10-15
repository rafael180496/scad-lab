# OpenSCAD, impresoras y skills de modelado

## OpenSCAD

- Externo, no se empaqueta. `detect.OpenSCAD` lo busca en la ruta configurada,
  el PATH y las rutas de instalación de cada SO.
- Render (S1): `openscad -o out.stl [--backend=manifold] archivo.scad`. Manifold
  requiere OpenSCAD 2025+; con 2021.01 usar `cgal` (Configuración → OpenSCAD).
- Colores: cada pieza de primer nivel con `color()` (regla del workspace); el
  3MF multicolor (S9) depende de eso.

## Motor de render (`backend/render`, S1)

Contrato en `backend/render/types.go` (fijo) y
[.claude/specs/s1-motor-render.md](../.claude/specs/s1-motor-render.md).

- **Invocación**: `openscad -o <cache>/<k>-NNN.stl --export-format binstl
  [--backend=manifold] NNN-slug.scad`, `cwd` = workspace, `exec.CommandContext`
  sin shell, `hideWindow` en Windows, plazo `TimeoutSeconds` (300 por defecto).
  El temporal termina en `.stl` para que una versión sin `--export-format`
  deduzca el formato; se renombra a la caché solo si el render salió bien.
- **Degradación**: si la salida dice `unknown option` / `unrecognised option`
  con `backend` o `export-format`, se reintenta sin esa opción y se recuerda
  por binario+versión. Manifold rechazado → `Backend: "cgal"`. `Backend: ""`
  se trata como manifold.
- **Versión**: `openscad --version` (stderr), cacheada por ruta + mtime +
  tamaño del binario (una actualización en el mismo lugar no deja la vieja).
- **Caché** (`<datos>/cache/meshes`): clave = sha256(contenido del `.scad` +
  cada `include<>`/`use<>` resuelto recursivamente —relativo al archivo, al
  workspace y a `lib/`, ignorando los comentados— + backend + versión).
  `<clave>.stl` + `<clave>.json` (el `Result`, warnings incluidos). Solo se
  cachea `StatusOK`. Tope 1 GB, poda por fecha de acceso (se toca el mtime en
  cada hit) y temporales huérfanos de más de 2 h.
- **Cola**: un render nuevo con la misma `WorkspaceKey` cancela el anterior,
  que vuelve con `StatusCancelled`. Plazo vencido ≠ cancelado: es
  `StatusError` con un diagnóstico que sugiere simplificar o subir el límite.
- **Estado**: `StatusMissing` si `OpenSCADPath == ""` (no ejecuta nada).
  `StatusError` si hay un `ERROR:`, si sale con código ≠ 0, si el modelo está
  vacío (`top level object is empty`, `No top level geometry`) o si no escribió
  la malla (modelo 2D). `Log` = últimos 64 KB de stdout+stderr.
- **Parser** (`parse.go`): `ERROR:` → error; `WARNING:` y `DEPRECATED:` →
  warning; `TRACE:`/`ECHO:` no son diagnósticos. Ubicación en orden:
  `in file "x", line N[, column C]`, `in file x, line N`, `at line N`,
  `, line N`. El mensaje queda sin la ubicación. `File` = "" si es el propio
  checkpoint, relativo al workspace si es un include. Un formato nuevo de
  OpenSCAD se agrega como caso en `parse_test.go`.
- **`RENDER_ERRORS.md`**: errores con línea y ±3 líneas de código, warnings,
  final del log si ningún error trae ubicación, y la instrucción de corregir en
  un checkpoint NUEVO con `parent=<archivo>`. Textos en el idioma activo;
  escritura atómica. Lo escribe/borra `app_render.go` según el estado.
- **Tests**: openscad falso (script sh en `t.TempDir()`), se saltean en
  Windows. Validado sin OpenSCAD real instalado.

## Contrato de checkpoints

`NNN-slug.scad`, primera línea `// @checkpoint parent=<archivo> desc="<texto>"`.
Lo escriben los agentes (reglas en `assets/workspace/RULES.md.tmpl`), lo lee
`workspace.Scan`. Cambiar el formato es cambiar las dos puntas y las skills.

## Impresoras

`assets/skills/bambu-lab-printers/references/printers.json` es la única fuente:
la lee la app (`backend/printers`) y la IA (skill). Las cifras sin confirmar van
en `verify` de cada impresora. La impresora del workspace se escribe en
`printer.json` y en las reglas (volumen de impresión, colores del AMS).

## Skills de modelado (`assets/skills/`)

| Skill | Para qué |
|---|---|
| `openscad-core` | reglas de modelado paramétrico y del workspace |
| `bambu-lab-printers` | perfiles, reglas FDM, chequeo de placa |
| `openscad-3mf-multicolor` | multicolor AMS, formato 3MF |
| `openscad-templates` | plantillas paramétricas listas |
| `openscad-libraries` | BOSL2, MCAD, NopSCADlib |
| `_skill-template` | base para crear skills propias (no se instala) |

Agregar una skill incluida: carpeta nueva en `assets/skills/<nombre>/` con
`SKILL.md` (frontmatter `name` = carpeta, `description` que diga cuándo usarla).
Se embebe sola en el binario. Una skill del usuario se crea desde Configuración
→ Skills y vive en `<datos>/skills/`.

## Verificado con OpenSCAD real (snapshot 2026.10.11, macOS)

- Aceptados: `--backend=manifold`, `--export-format binstl`, `--render`,
  `--imgsize`, `--colorscheme`. `openscad --version` → `OpenSCAD version 2026.10.11` (stderr).
- Mensajes: `ERROR: Parser error: syntax error in file x.scad, line N`,
  `ERROR: Assertion '…' failed: "…" in file x.scad, line N`,
  `ERROR: Recursion detected calling module 'a' in file …, line N` seguido de
  `TRACE: called by …`, `WARNING: Ignoring unknown variable "foo" in file …, line N`,
  `WARNING: Can't find include file 'x.scad'. in file …, line N`. Ruta sin comillas
  y relativa; sin columna.
- Modelo vacío: `Current top level object is empty.` (código 1). 2D:
  `Current top level object is not a 3D object.` (código 1). Un `assert()` que
  falla imprime **además** el aviso de vacío: el motor solo lo agrega si no hubo
  un `ERROR` antes.
- Un include faltante es WARNING, no ERROR: el render sigue.
- **3MF con color**: `-o x.3mf` exporta por defecto con colores
  (`-O export-3mf/color-mode=model`; también `none` y `selected-only`). Sale **un
  solo objeto** con `<basematerials>` (un `<base displaycolor>` por cada `color()`
  más `Default`) y color por triángulo. Para el AMS de Bambu (S9) falta decidir si
  basta eso o se arma un objeto por color.
- Las 10 plantillas de `openscad-templates` renderizan con Manifold sin errores
  ni warnings (vistas previas revisadas a ojo).
- Test: `TestRenderWithRealOpenSCAD` (`backend/render/real_test.go`) corre el
  motor contra el binario instalado; se salta si no hay OpenSCAD.
