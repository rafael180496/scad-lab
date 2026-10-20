# OpenSCAD, impresoras y skills de modelado

## OpenSCAD

- El nativo es externo. `detect.OpenSCAD` lo busca en la ruta configurada, el
  OpenSCAD descargado por la app (`<datos>/tools/openscad/`, ver
  [18-descarga-openscad.md](18-descarga-openscad.md)), el PATH y las rutas de
  instalación de cada SO.
- Sin nativo, el render cae al **motor integrado** (OpenSCAD 2025.12.30 en
  WebAssembly sobre wazero): mismos argumentos, mismo parser, misma caché;
  `Result.Engine = "wasm"`. Ver [17-motor-wasm.md](17-motor-wasm.md).
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

## Malla en el visor (S2)

Spec: [.claude/specs/s2-visor-3d.md](../.claude/specs/s2-visor-3d.md).

- **URL**: `GET /mesh/<clave>.stl`, con `<clave>` = nombre base de
  `render.Result.MeshPath` (64 hex). El frontend la arma con
  `'/mesh/' + basename(meshPath)` (separador `/` o `\`) y la pide con `fetch`.
  Por un binding, el `[]byte` cruzaría como base64 dentro de JSON.
- **`backend/meshserve`**: `Middleware(cacheDir)` del AssetServer, enganchado en
  `main.go` con `<datos>/cache/meshes` (`appdata.Dir()`; si falla, sin
  middleware y la app abre igual). Solo acepta `^[0-9a-f]{64}\.stl$`
  (`meshserve.ValidName`): cualquier otro nombre bajo `/mesh/` —`..`, `%2e%2e`,
  mayúsculas, `.json`, temporales `-NNN.stl`— es 404 y nunca llega a una ruta;
  métodos distintos de GET/HEAD, 405. Responde `Content-Type: model/stl` y
  `Cache-Control: no-store` (el visor tiene su caché LRU por clave). Lo que no
  empieza con `/mesh/` pasa a `next`.
- **Middleware y no `Handler`**: en `wails dev` Wails reemplaza el `Handler` por
  el proxy a Vite y manda **todo GET** al proxy (Vite contesta `index.html` a lo
  que no conoce); el `Handler` solo vería los no-GET. El `Middleware` envuelve al
  proxy en dev y a los assets embebidos en producción (verificado en Wails
  2.15, `assethandler_external.go`).
- **`backend/mesh`**: `ParseSTL(io.Reader)` / `InfoFromFile(path)` →
  `mesh.Info{Triangles, Min, Max, Size, VolumeMM3, AreaMM2, Watertight}`. Binario
  si el tamaño es exactamente `84 + 50·n` (n = contador del encabezado), no por
  el «solid» inicial, que muchos binarios también traen; si no, ASCII estricto
  (sin `endsolid`, con un vértice mal formado o con bytes NUL → error). Un
  lector posicionable se lee en streaming (STL de 18 MB / 360k triángulos ≈
  80 ms); uno sin `Seek` se lee entero. Coordenadas NaN/Inf → `ErrNonFinite`.
- **Volumen**: suma de tetraedros con signo contra el origen
  (`v0·(v1×v2)/6`), en valor absoluto (una malla con las normales invertidas da
  el mismo volumen). Solo tiene sentido si la malla es cerrada.
- **Watertight**: se sueldan los vértices por **posición exacta** (mismo
  float), cada triángulo aporta sus 3 aristas como par de ids ordenado, y la
  malla es cerrada si cada arista aparece **exactamente 2 veces** (se ordena un
  `[]uint64` en vez de un mapa de conteos: 8 bytes por arista). No mira la
  orientación. Una malla vacía no es watertight.
- **Binding** `GetMeshInfo(meshPath)` (`app_mesh.go`): exige
  `filepath.Dir(meshPath) == <datos>/cache/meshes` y nombre con forma de clave;
  cachea por clave en memoria (mutex, tope 4096 entradas). Vive a nivel de
  paquete: la malla de una clave no cambia nunca.
- **Tests**: `backend/mesh` (cubo binario/ASCII, abierto, truncado, NaN, y
  mallas reales de OpenSCAD contra la fórmula), `backend/meshserve`
  (`httptest`, escapes, STL real servido byte a byte) y
  `TestRenderedMeshInfoAndServe` (render real → `GetMeshInfo` → middleware). Los
  que usan OpenSCAD se saltan si no está instalado.

## Contrato de checkpoints

`NNN-slug.scad`, primera línea `// @checkpoint parent=<archivo> desc="<texto>"`.
Lo escriben los agentes (reglas en `assets/workspace/RULES.md.tmpl`), lo lee
`workspace.Scan`. Cambiar el formato es cambiar las dos puntas y las skills.

## Impresoras (S14: siete marcas)

El catálogo está **partido por marca**: cada skill de marca trae su
`assets/skills/<marca>-printers/references/printers.json` (esquema v2) y
`backend/printers` junta todos los que encuentra en el binario
(`skills/*/references/printers.json`). Es la única fuente: la lee la app (`printers.All`,
`Brands`, `ByID`) y la IA dentro de su skill. Las skills del usuario no cambian el catálogo.

| Marca | Skill | Impresoras | Multicolor |
|---|---|---|---|
| Bambu Lab | `bambu-lab-printers` | A1 mini, A1, A2L, P1P, P1S, P2S, X1C, X1E, X2D, H2D, H2S, H2C (12) | AMS |
| Prusa | `prusa-printers` | MK4S, CORE One+, CORE One L+, MINI+, XL (5) | MMU3, toolchanger XL / INDX |
| Creality | `creality-printers` | Ender-3 V3 / SE / KE, K1 / K1C / K1 Max, K2 / K2 Pro / K2 Plus, Hi (10) | CFS-C, CFS |
| Elegoo | `elegoo-printers` | Neptune 4 / Pro / Max, Centauri Carbon 1 y 2 (5) | CANVAS |
| Anycubic | `anycubic-printers` | Kobra 2 Pro / Plus / Max, Kobra 3, S1, S1 Max, X (7) | ACE Pro / ACE 2 Pro |
| Snapmaker | `snapmaker-printers` | U1, J1s, Artisan (3) | toolchanger, IDEX, doble extrusión |
| UltiMaker | `ultimaker-printers` | S3, S5, S7, S8, Factor 4 (5) | doble extrusión (2,85 mm) |

- **Una entrada por modelo comercial.** Lo que varía con los accesorios (AMS, CFS, ACE, los cabezales del XL) es la cantidad
  de **colores**: `multicolor_slots` es el máximo de la máquina y `default_colors` la configuración con que nace un workspace.
  La impresora **efectiva** de un workspace (`wsconfig.EffectivePrinter`) trae sus colores en `MulticolorSlots` y el tope en
  `MaxColors`. Detalle del contrato y de la configuración por workspace: [29-contrato-workspaces.md](29-contrato-workspaces.md).
- **Cada cifra tiene su ficha oficial** en `sources`; lo que no se pudo confirmar va en `verify` (nombre del campo, validado por
  `TestVerifyNamesAreSchemaFields`) y un número desconocido es `0`. Campos extra (`build_volume_dual_nozzle`,
  `build_volume_with_indx`, `build_volume_copy_mode`…) los ignora Go y los lee la IA. Dos casos a no olvidar: la **Kobra 2** da
  su volumen como alto × ancho × fondo (aquí va como X×Y×Z: la Max es 420×420×500) y las **UltiMaker** usan filamento de 2,85 mm.
- **La skill de la marca** (`SKILL.md`) trae la tabla de perfiles (espejo del JSON), las reglas FDM de esa marca (superficie de la
  cama, primera capa, voladizos, holguras, materiales), su multicolor (cómo modelar por color y cómo llega al slicer), qué formato
  exportar y `references/plate_check.scad` con su `assert_fits_plate()`. Los tests exigen que la tabla de cada skill y su
  `plate_check.scad` coincidan con el JSON (`TestBrandSkillsDocumentTheirPrinters`, `TestPlateCheckMirrorsTheCatalog`).
  Las reglas FDM genéricas y sus snippets están en `openscad-core/references/fdm-design-rules.md`.
- La impresora del workspace se escribe en `printer.json` y en las reglas (`RULES.md.tmpl`: marca, modelo, volumen, sistema
  multicolor, colores del workspace y máximo, boquilla, filamento, slicer y skill de la marca).
- **Agregar una marca**: carpeta `assets/skills/<marca>-printers/` con `SKILL.md` (frontmatter `name` = carpeta), `references/printers.json`
  (bloque `brand` {id, name, skill} y las impresoras; ids con el prefijo de la marca) y `references/plate_check.scad`; sumar la
  marca a `brandOrder` de `backend/printers` y, si tiene slicer propio, a `backend/slicers`. No hace falta tocar nada más de Go:
  el selector, las reglas y las herramientas MCP la ven sola. **Agregar un modelo**: una entrada más en el JSON de su marca, una
  fila en la tabla de su `SKILL.md` y en `plate_check.scad`.

## Skills de modelado (`assets/skills/`)

| Skill | Para qué |
|---|---|
| `openscad-core` | reglas de modelado paramétrico y del workspace, reglas FDM genéricas (`references/fdm-design-rules.md`) |
| `<marca>-printers` (7) | perfiles de la marca, reglas FDM propias, multicolor, formato a exportar, chequeo de placa (arriba) |
| `openscad-3mf-multicolor` | multicolor de cualquier sistema (AMS, MMU3, CFS, ACE, toolchanger, IDEX, doble extrusión) y qué 3MF lee cada slicer ([11-exportacion.md](11-exportacion.md)) |
| `openscad-templates` | plantillas paramétricas listas |
| `openscad-libraries` | BOSL2, MCAD, NopSCADlib |
| `openscad-stl-editing` | editar un STL existente sin tocarlo (S13): `import()` en un checkpoint nuevo, recetas de agujeros, cortes, texto, escala, partir y espejar; límites de las mallas ([23-edicion-stl.md](23-edicion-stl.md)) |
| `openscad-from-image` | modelar a partir de imágenes (S15): foto de una pieza, logo o placa plana, relieve o litofanía, boceto acotado, varias vistas; la escala (una medida del usuario o un objeto conocido: `references/standard-sizes.md`, cada cifra con su fuente o «por confirmar»), el flujo crear → render → `compare_with_image` → corregir, qué se puede y qué no se puede prometer; 3 plantillas renderizadas con OpenSCAD real (`bracket-from-photo`, `plate-from-svg`, `lithophane-surface`). Usa las herramientas `image_info`, `compare_with_image`, `trace_image_outline` e `image_to_heightmap` ([25](25-herramientas-ia.md#imágenes-s15)) |
| `_skill-template` | base para crear skills propias (no se instala) |

Agregar una skill incluida: carpeta nueva en `assets/skills/<nombre>/` con
`SKILL.md` (frontmatter `name` = carpeta, `description` que diga cuándo usarla).
Se embebe sola en el binario. Una skill del usuario se crea desde Configuración
→ Skills y vive en `<datos>/skills/` (o la carpeta que elijas ahí); desde la misma pantalla se edita, renombra y elimina ([20-skills-propias.md](20-skills-propias.md)).

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
  más `Default`) y color por triángulo. ScadLab lo reparte en una parte por color y escribe el 3MF
  que lee cada slicer (S9, S14: [11-exportacion.md](11-exportacion.md)).
- Las 10 plantillas de `openscad-templates` renderizan con Manifold sin errores
  ni warnings (vistas previas revisadas a ojo).
- Test: `TestRenderWithRealOpenSCAD` (`backend/render/real_test.go`) corre el
  motor contra el binario instalado; se salta si no hay OpenSCAD.

## Renders simultáneos (S8)

Con varios proyectos abiertos cada uno puede lanzar su render (auto-render,
agentes de fondo). El motor deja correr como mucho **2 OpenSCAD a la vez** en
todo el proceso (`maxConcurrentRenders`, canal `engine.slots`): Manifold ya usa
todos los núcleos en un solo render, y más en paralelo solo reparten la CPU. Los
que esperan turno siguen siendo cancelables y respetan su plazo. Test:
`TestConcurrentRendersAreCapped`. El servidor MCP (`ScadLab --mcp`) es otro
proceso con su propio tope.
