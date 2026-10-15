# Exportación (S9): STL, 3MF multicolor para Bambu, STL por color

Spec: [.claude/specs/s9-exportacion.md](../.claude/specs/s9-exportacion.md).
Código: `backend/threemf` (leer/repartir/escribir 3MF), `backend/exporter`
(preparar y escribir), `app_export.go` (bindings y diálogo de guardar).

## Flujo

```
STL           → la malla del render (caché) se copia tal cual
3MF / por color → OpenSCAD -o x.3mf (con colores) → threemf.Read
                  → Split por color (basematerials) → Write (Bambu) / STL zip
```

- OpenSCAD (2025+) exporta el 3MF como **un solo objeto** con color por
  triángulo (`basematerials`). `threemf.Split` lo reparte en **una parte por
  color**, con los vértices compactados por parte.
- `threemf.Write` arma un 3MF core válido (`[Content_Types].xml`,
  `_rels/.rels`, `3D/3dmodel.model` en mm) con un objeto contenedor y sus
  `<components>`, más `Metadata/model_settings.config` que da a cada parte su
  **nombre (el color) y su extrusor** (1..N en orden de aparición).
- **`application` no es «BambuStudio-…»** a propósito: con ese prefijo Bambu
  Studio trata el archivo como proyecto propio, exige un
  `project_settings.config` completo y con uno parcial se cae. Como archivo de
  otro fabricante carga geometría + partes + extrusores; los colores del
  filamento los pone el AMS del usuario.
- Un modelo sin `color()` sale como una sola parte.

## Avisos (no errores)

- Más colores que `multicolor_slots` de la impresora del workspace.
- Colores casi iguales (distancia RGB < ~2 % del rango): probablemente el
  usuario quiso uno solo.

## Bindings

| Binding | Devuelve |
|---|---|
| `PreviewExportParts(id, fileName)` | `export.Preview{parts, warnings}` sin escribir nada |
| `ExportCheckpoint(id, fileName, format)` | `export.Result{path, parts, warnings}`; `path` vacío si se cancela el diálogo. `format`: `stl` \| `3mf` \| `stl-per-color` |

## Interfaz (S9.B)

- **Cabecera**: botones STL / 3MF / PNG (`components/layout/Header.tsx`). Activos
  con un render correcto del checkpoint elegido; si no, quedan en
  `aria-disabled` (no `disabled`, que no deja pasar el cursor y el `title` no se
  vería) con un `title` que dice por qué (`export.why.*`). Atajos ⌘/Ctrl+E (3MF) y
  ⌘/Ctrl+⇧+E (STL) en captura sobre `window` (`hooks/useExport.ts`): Monaco usa ⌘E
  para «usar la selección para buscar»; sin posibilidad de exportar el atajo
  avisa con un toast y con un diálogo abierto no actúa.
- **STL** va directo al diálogo de guardar. **PNG** llama a la captura del visor a
  través de `lib/viewerBridge.ts` (el visor es `lazy`: registra su captura al
  montarse).
- **Diálogo de 3MF** (`components/export/ExportDialog.tsx`): pide
  `PreviewExportParts`, muestra una fila por parte (muestra de color, nombre,
  triángulos, extrusor `E1..N`), los avisos del backend y el formato (3MF Bambu o
  STL por color en .zip). Botones **Exportar** y, si Bambu Studio está detectado y
  el formato es 3MF, **Exportar y abrir en Bambu Studio**. Si el diálogo nativo
  de guardar se cancela, el de exportación sigue abierto.
- **Tras guardar** (`lib/saved.ts`): toast con la ruta y las acciones **Mostrar
  en el Finder/Explorador** (`RevealPath`) y **Abrir en Bambu Studio**
  (`OpenInSlicer`, solo `.3mf`/`.stl`). El `Toast` admite acciones
  (`toast.withActions`) y en ese caso dura 15 s y se cierra con una X.
- **Banco de capturas**: vistas `export-3mf` (dos colores, sin avisos) y
  `export-3mf-warning` (cinco colores en una A1 mini: aviso de colores de más),
  con `DetectSlicers` encontrado y `OpenInSlicer`/`RevealPath` sin efecto.

## Abrir en Bambu Studio

`DetectSlicers` / `OpenInSlicer` / `RevealPath` (`app_export.go`). Se abre la
**aplicación** con el archivo, desacoplada, sin shell y sin flags de la CLI de
Bambu (`--slice`, `--export-3mf`… son los que la hacen caerse y abrir el diálogo
de «se cerró inesperadamente»):

| Sistema | Comando |
|---|---|
| macOS | `open -a <ruta .app> <archivo>` (si la ruta configurada apunta dentro del `.app`, se lleva al propio `.app`) |
| Windows | `<ruta .exe> <archivo>` con `Start()` y `hideWindow` (`app_launch_windows.go`) |

Ruta: `settings.bambuStudioPath` (Configuración → Impresión → «Ruta de Bambu
Studio»; vacío = autodetectar) y, si no existe, las rutas habituales. El
lanzador es la variable de paquete `launchApp`: en producción hace
`exec.Command(...).Start()`; los tests la reemplazan por un registrador y solo
comprueban los argumentos y las validaciones (archivo inexistente, extensión no
permitida, ruta relativa, slicer no encontrado). `slicerLaunch` y `revealLaunch`
reciben el SO como parámetro, así se prueban los dos sin ejecutar nada.

## Pruebas

Automáticas (`go test`): 3MF real de OpenSCAD con `nameplate-two-color.scad` →
2 partes con sus colores y volumen total igual al del STL; sin color → 1 parte;
avisos de colores de más; XML y content types válidos.

**Bambu Studio no se prueba de forma automática** (regla 9 de
`.claude/rules/technical.md`: su CLI se cae y le abre diálogos al usuario).
Prueba manual del usuario (con los botones de S9.B; «Exportar y abrir en Bambu Studio» lo abre en un clic):

1. Exportar a 3MF un checkpoint con dos o más `color()` (p. ej. la plantilla
   `nameplate-two-color.scad`).
2. Abrirlo en Bambu Studio: tiene que verse **un objeto con una parte por
   color**, cada parte con su extrusor (1, 2, …) y su nombre de color.
3. Asignar los filamentos del AMS y laminar: sin errores de geometría.
4. Un modelo sin color: un objeto de una sola parte.
