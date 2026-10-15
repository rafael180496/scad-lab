# S9 — Exportación (STL, 3MF multicolor para Bambu, PNG)

Fase del [roadmap](../../claude/01-roadmap.md). Dos tandas: 9.A núcleo Go
(openscad-dev) ya; 9.B botones de la cabecera y diálogo (fullstack) cuando
cierre la tarea de UX de tamaños, que está tocando el frontend.

## Hechos verificados (architect, OpenSCAD 2026.10.11)

- `openscad -o x.3mf` exporta con colores por defecto
  (`-O export-3mf/color-mode=model`): **un solo objeto** con `<basematerials>`
  (un `<base displaycolor>` por cada `color()` + `Default`) y color por
  triángulo (`pid`/`p1`).
- Bambu Studio está instalado (`/Applications/BambuStudio.app/Contents/MacOS/BambuStudio`,
  con CLI: `--help`). Úsalo para validar que el 3MF generado abre y cuántos
  objetos/partes/extrusores ve (p. ej. `--info`, `--slice 0` en un directorio
  temporal, o lo que su `--help` ofrezca; verificalo).

## 9.A — núcleo Go (openscad-dev)

Alcance: `backend/export/**`, `backend/threemf/**`, `app_export.go`,
`claude/11-exportacion.md` (segmento nuevo) + su fila en el índice de
`CLAUDE.md`. No tocar frontend, `backend/render/types.go`, `app.go`.

- **STL**: copia la malla cacheada del render (si no hay render OK, renderiza
  antes con `a.engine`/`RenderCheckpoint`).
- **3MF multicolor para Bambu** (`backend/threemf`):
  1. renderizar el checkpoint a 3MF con OpenSCAD (un render aparte, con su
     propia caché por clave + formato; reutilizá el patrón de `backend/render`
     sin cambiar su contrato: si necesitás exportar `ParseOutput` u otra cosa,
     archivo nuevo en `render/`);
  2. leer el 3MF (zip + XML), agrupar triángulos por color (`basematerials`),
     y escribir un 3MF **estilo Bambu**: un objeto con una **parte por color**
     (`<components>`), vértices compactados por parte, `<basematerials>` con
     los colores, y `Metadata/model_settings.config` que asigna a cada parte un
     extrusor (1..N en orden de aparición) y el nombre del color; más
     `Metadata/project_settings.config` mínimo si Bambu lo necesita para
     mostrar los colores del filamento (verificalo con el CLI de Bambu y con la
     skill `assets/skills/openscad-3mf-multicolor/references/3mf-format.md`).
  3. Si hay más colores que `multicolor_slots` de la impresora del workspace,
     devolver un aviso (no error) en el resultado.
  4. Un modelo sin `color()` = una sola parte.
- **STL por color** (alternativa para otros slicers): un ZIP o una carpeta con
  `<base>-<color>.stl` por parte.
- Bindings (`app_export.go`): `ExportCheckpoint(id, fileName, format)
  (ExportResult, error)` con `format` = `stl` | `3mf` | `stl-per-color`; abre el
  diálogo nativo de guardar (`runtime.SaveFileDialog`, nombre sugerido
  `<workspace>-<NNN-slug>.<ext>`) y devuelve `{path, parts:[{color, triangles}],
  warnings:[...]}`; `""` en `path` si se cancela. Y `PreviewExportParts(id,
  fileName)` que devuelve las partes/colores sin guardar (para que la UI muestre
  qué va a salir antes de elegir dónde).
- Tests: 3MF real de OpenSCAD (está instalado) con la plantilla
  `nameplate-two-color.scad` → 2 partes con los colores correctos y volumen
  total igual al del STL; un modelo sin color → 1 parte; validación del XML de
  salida contra la estructura del 3MF core (paths, content types, rels); y una
  **prueba con Bambu Studio** (skip si no está instalado) que confirme que lo
  abre y ve N partes/extrusores. Todo en `t.TempDir()`.

## 9.B — frontend (después de la tarea de tamaños/UX)

Alcance: `frontend/**`, `CHANGELOG.md`, `index.html` (tema
`imprimir-exportar` y `imprimir-multicolor`, receta multicolor),
`scripts/help-shots.sh`, `docs/screenshots/**`, `claude/06-go-react-contract.md`,
`claude/01-roadmap.md`. Regenerar `frontend/wailsjs` (aparecen
`ExportCheckpoint`, `PreviewExportParts` y los tipos `export.*`).

- Botones **STL / 3MF / PNG** de la cabecera habilitados cuando hay un
  checkpoint con render OK (deshabilitados con `title` que diga por qué si no).
- **3MF**: abre un diálogo de exportación con la vista previa de
  `PreviewExportParts`: una fila por parte con su muestra de color, nombre y
  triángulos, el extrusor que le tocó (1..N), los avisos (colores de más para
  la impresora del workspace, colores casi iguales) y la opción de formato
  (3MF Bambu o STL por color, en un .zip). Botón Exportar → `ExportCheckpoint`
  → diálogo nativo de guardar → toast con la ruta y botón «Mostrar en el
  Finder/Explorador» (binding `RevealPath` si hace falta: crearlo en
  `app_export.go` es aceptable, con validación de que la ruta exista).
- **STL**: directo al diálogo de guardar, sin vista previa.
- **PNG**: la captura del visor (`SaveScreenshot`, ya existe).
- Atajos: Cmd/Ctrl+E exportar 3MF, Cmd/Ctrl+Shift+E STL.
- Escalable con `--ui-font-scale` (regla nueva de `claude/04-frontend.md`).
- Capturas: vistas `export-3mf` (dos colores, sin avisos) y
  `export-3mf-warning` (cinco colores con una A1 mini: aviso). Ayuda: quitar
  «En desarrollo» de exportar y multicolor, explicar la prueba en Bambu Studio
  (abrir el 3MF, ver una parte por color con su extrusor, asignar AMS).
- **Abrir en Bambu Studio** (pedido del usuario): después de exportar un 3MF,
  el toast y el diálogo ofrecen «Abrir en Bambu Studio», y el diálogo de
  exportación tiene la opción «Exportar y abrir en Bambu Studio». Solo se
  ejecuta **por un clic del usuario**, nunca solo.
  - Backend (`app_export.go`): `DetectSlicers() []Slicer{id, name, path,
    found}` (Bambu Studio: macOS `/Applications/BambuStudio.app` y
    `~/Applications/BambuStudio.app`; Windows `%ProgramFiles%\Bambu
    Studio\bambu-studio.exe`; ruta configurable en Configuración →
    Impresión, setting `bambuStudioPath`, vacío = autodetectar) y
    `OpenInSlicer(slicerId, filePath) error`: valida que el archivo exista y sea
    `.3mf`/`.stl`, y lo abre **con la app**, desacoplado, sin esperar:
    macOS `open -a <ruta .app> <archivo>`; Windows `exec.Command(exe, archivo)`
    con `Start()` y `hideWindow` del lanzador, sin shell. Nunca la CLI de
    Bambu (`--slice`, `--export-3mf`…): esa es la que se cae.
  - Si Bambu Studio no está, el botón no aparece (o aparece deshabilitado con
    `title` que lo explique y un enlace a Configuración → Impresión).
- **Pruebas**: ninguna prueba ejecuta Bambu Studio ni `open`. El lanzador es
  una función inyectable (`a.launch` o una variable de paquete) y los tests
  solo verifican los argumentos que se le pasarían y las validaciones (archivo
  inexistente, extensión no permitida, slicer no encontrado). Regla 9 de
  `technical.md`.
