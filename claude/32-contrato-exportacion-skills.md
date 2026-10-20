# Contrato Go ↔ React: exportación, descarga de OpenSCAD y skills

Parte de [06-go-react-contract.md](06-go-react-contract.md) (índice del contrato). Fuente: `app_export.go`, `app_openscad.go`,
`app_skills.go`. Cómo se abre un archivo en cada slicer: [11-exportacion.md](11-exportacion.md#abrir-en-slicer-s9b-s14).

## Exportación — `app_export.go` (S9)

| Binding | Devuelve | Nota |
|---|---|---|
| `PreviewExportParts(id, fileName)` | `exporter.Preview{parts, warnings}` | sin escribir nada; la primera vez renderiza el checkpoint a 3MF con OpenSCAD (queda en caché) |
| `ExportCheckpoint(id, fileName, format)` | `exporter.Result{path, format, parts, warnings}` | `format`: `stl` \| `3mf` \| **`3mf-prusa`** (3MF para PrusaSlicer, S14) \| `stl-per-color` (.zip); diálogo nativo de guardar; `path` vacío si se cancela |
| `DetectSlicers()` | `[]Slicer{id, name, path, found, configured}` | **todos** los del registro (`backend/slicers`): `bambu-studio`, `orcaslicer`, `prusaslicer`, `creality-print`, `elegoo-slicer`, `anycubic-slicer-next`, `snapmaker-orca`, `ultimaker-cura`. Gana la ruta fijada (`settings.slicerPaths[id]`; para Bambu Studio también `bambuStudioPath`) si existe; si no, autodetecta. `configured` = lo que el usuario fijó (aunque no exista) |
| `OpenInSlicer(slicerId, filePath)` | — | cualquier id del registro; valida ruta absoluta, archivo existente y extensión `.3mf`/`.stl`, y que el slicer esté (los mensajes nombran al slicer); abre **la app** desacoplada (macOS `open -a <app> <archivo>`; Windows `exec(<exe>, <archivo>)`). Nunca la CLI de un slicer; solo por un clic del usuario |
| `RevealPath(path)` | — | muestra el archivo en el Finder/Explorador (`open -R` / `explorer /select,`); la ruta tiene que existir |

`exporter.PartInfo{name, color, triangles, extruder, uncolored}`. Settings
`bambuStudioPath` y, desde S14, `slicerPaths` (`{id: ruta}`; vacío = autodetectar,
sin migración). Los avisos de exportación se miden contra los colores del workspace. Los lanzamientos pasan
por la variable de paquete `launchApp`, que los tests reemplazan por un
registrador: ninguna prueba abre una aplicación (regla 9 de `technical.md`).

## Descarga de OpenSCAD — `app_openscad.go` (S11)

Detalle y decisiones: [claude/18](18-descarga-openscad.md).

| Binding | Devuelve | Nota |
|---|---|---|
| `GetOpenSCADDownload()` | `OpenSCADDownload{version, sizeBytes, supported, installed, installedPath, running, wasmVersion}` | `supported` = hay descarga para este SO/arquitectura (darwin arm64/amd64, windows amd64); `wasmVersion` = versión del motor integrado |
| `DownloadOpenSCAD()` | `OpenSCADDownload` | **bloquea** hasta terminar y emite `openscad:download`; una sola a la vez (si hay otra, error). Cancelada: error con código `download-cancelled` (la UI no lo muestra como fallo). Al terminar bien olvida la ruta de OpenSCAD cacheada |
| `CancelOpenSCADDownload()` | — | corta la descarga en curso; sin descarga no hace nada |
| `RemoveDownloadedOpenSCAD()` | `OpenSCADDownload` | borra `<datos>/tools/openscad/`; rechazado con una descarga en curso; no toca el OpenSCAD del usuario |

## Skills — `app_skills.go`

| Binding | Devuelve |
|---|---|
| `ListSkills()` | `[]SkillView{name, description, source, dir, enabled}` |
| `SetSkillEnabled(name, enabled)` | — (desde S14 es el conjunto **por defecto de los workspaces nuevos**; no toca los existentes) |
| `CreateSkill(name)` | carpeta creada (kebab-case); desde la plantilla, con `name` y descripción pendiente; **no abre nada** |
| `GetSkillsFolder()` | `SkillsFolder{path, default, custom, exists, count}` |
| `OpenSkillsFolder()` | — (crea la carpeta si falta; error claro si el sistema no puede abrirla) |
| `ChangeSkillsFolder(dir, move)` | `SkillsFolderChange{path, moved[], skipped[], syncError}`; `dir` vacío = predeterminada |
| `DeleteSkill(name)` | `DeleteSkillResult{trash, removed, kept[], syncError}` — a la papelera de la app; solo propias |
| `RenameSkill(old, new)` | `RenameSkillResult{name, removed, kept[], syncError}` — solo propias; renombra también la entrada en las listas de skills de los workspaces |
| `SyncWorkspaceSkills(id)` | — (instala las skills de **ese** workspace; retira las demás de ScadLab que nadie editó) |
| `ListSkillFiles(name)` | `[]skills.File{path, size, text}` (SKILL.md primero) |
| `ReadSkillFile(name, rel)` | texto; las incluidas también |
| `SaveSkillFile(name, rel, content)` | `SaveSkillFileResult{saved, diagnostics[], syncError}`; `saved:false` si el SKILL.md tiene errores |
| `CreateSkillFile(name, rel)` / `RenameSkillFile(name, from, to)` / `DeleteSkillFile(name, rel)` | — / syncError / ruta en la papelera |
| `ValidateSkillMarkdown(name, text)` | `[]skills.Diagnostic{line, severity, code, message}` |

Detalle (papelera, confinamiento de rutas, copias en workspaces): [20-skills-propias.md](20-skills-propias.md).
