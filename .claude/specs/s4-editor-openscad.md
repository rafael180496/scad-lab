# S4 — Editor de código OpenSCAD (formato, búsqueda, linter) + UX

Fase del [roadmap](../../claude/01-roadmap.md), adelantada a pedido del
usuario («mejorar la UX y el lector de código OpenSCAD: formato, búsqueda,
linter»). Se hace en dos tandas para no chocar con S2, que está tocando el
frontend:

| Tanda | Dueño | Cuándo | Alcance |
|---|---|---|---|
| 4.A núcleo Go: lexer, linter, formateador, catálogo, guardar checkpoint | openscad-dev | ya (en paralelo con S2) | `backend/scad/**`, `backend/render/` (solo un `ParseOutput` exportado en archivo nuevo `parse_export.go`), `app_editor.go`, `assets/scad/**`, `claude/10-editor.md` |
| 4.B editor Monaco + UX general | fullstack-dev | cuando S2 cierre | `frontend/**`, `backend/store/settings.go`, `CHANGELOG.md`, `index.html`, capturas |

## Decisiones (architect)

- **Linter y formateador en Go, no en TS.** Los van a usar también los
  agentes (MCP de S6: `lint`, `format`) y la auto-iteración; una sola
  implementación. El editor los llama por binding con debounce.
- **Sintaxis real con OpenSCAD**: `openscad -o <tmp>.ast <archivo>` parsea sin
  generar geometría (~50 ms, verificado con 2026.10.11) y da `ERROR: Parser
  error … line N`. El código del editor se escribe a un temporal en
  `<ws>/.scadlab/lint/` (subcarpeta: el watcher no es recursivo, así que no lo
  ve) y se borra siempre. Los `include <…>` del archivo se resuelven pasando al
  proceso `OPENSCADPATH=<ws><sep><ws>/lib` (separador de rutas del SO). Plazo
  de 5 s.
- **Formateador propio**, sobre un lexer de OpenSCAD: el AST de `-o x.ast` no
  sirve (descarta comentarios y reordena módulos). Conservador: reindenta por
  llaves/paréntesis/corchetes, normaliza espacios alrededor de `=`, `,`, `;` y
  operadores binarios, colapsa líneas en blanco múltiples (máx. 1), quita
  espacios finales, **nunca** une ni parte líneas de código ni toca el interior
  de strings y comentarios (salvo reindentar la primera línea de un comentario
  de línea). Idempotente: `fmt(fmt(x)) == fmt(x)`. Indentación configurable
  (2 o 4 espacios, default 4).
- **Guardar nunca sobrescribe.** El editor guarda creando un checkpoint hijo:
  `SaveAsCheckpoint(id, parentFile, label, description, source)` →
  `NNN-slug.scad` nuevo con el encabezado `@checkpoint` correcto (si el código
  ya trae uno, se reemplaza la primera línea). Regla dura de `technical.md`.

## Contrato 4.A (Go)

```go
package scad // backend/scad

type Token struct{ Kind TokenKind; Text string; Line, Col int } // Line/Col 1-based
func Lex(src string) ([]Token, error)   // ident, number, string, comment (línea/bloque), op, punct, keyword, newline

type Diagnostic struct {
    Severity string `json:"severity"` // "error" | "warning" | "info"
    Line, Col, EndLine, EndCol int   `json:"line" …` // tags: line, col, endLine, endCol (1-based; End* 0 = toda la línea)
    Code     string `json:"code"`     // "syntax" | "SL001"…
    Source   string `json:"source"`   // "openscad" | "scadlab"
    Message  string `json:"message"`  // idioma activo (i18n.Msg)
}

type LintOptions struct {
    WorkspaceDir string   // para resolver include/use y el parent
    FileName     string   // nombre del checkpoint (para SL001/SL002); "" = sin reglas de checkpoint
    OpenSCADPath string   // "" = sin chequeo de sintaxis de OpenSCAD (solo reglas propias)
    ExistingFiles []string // .scad del workspace (para SL002)
}
func Lint(ctx context.Context, src string, o LintOptions) []Diagnostic

type FormatOptions struct{ IndentSize int }
func Format(src string, o FormatOptions) (string, error) // error solo si el lexer falla (string sin cerrar…)
```

Reglas propias (cada una con test):

| Código | Severidad | Regla |
|---|---|---|
| SL001 | warning | falta o está mal formado el encabezado `// @checkpoint parent=… desc="…"` en la línea 1 |
| SL002 | warning | `parent=` apunta a un archivo que no existe en el workspace |
| SL003 | warning | objeto de primer nivel sin `color()` (llamada de geometría o de módulo en el nivel superior que no esté envuelta en `color(...)`) — el 3MF multicolor depende de esto |
| SL004 | error | `include <x>`/`use <x>` que no se encuentra (relativo al archivo, al workspace y a `lib/`) |
| SL005 | warning | un checkpoint incluye a otro checkpoint (`NNN-*.scad`): deben ser autocontenidos |
| SL006 | info | `$fn` mayor a 256 asignado en el nivel superior (render lento) |
| SL007 | info | `minkowski()` anidado dentro de otro `minkowski()`/`hull()` (render muy lento) |
| SL008 | warning | variable asignada dos veces en el mismo ámbito (OpenSCAD usa la última: suele ser un error) |
| SL009 | info | parámetro de Customizer en la sección de parámetros sin anotación de rango (`// [min:step:max]`) — solo si el archivo usa secciones `/* [..] */` |

Las de sintaxis de OpenSCAD (`syntax`, `Source: "openscad"`) se mapean desde
el `ParseOutput` de `render` (exportarlo en `backend/render/parse_export.go`
sin tocar `types.go`). Si el chequeo de OpenSCAD falla por sintaxis, se
devuelven igual las reglas propias que el lexer pueda evaluar.

### Catálogo del lenguaje (autocompletado y hover)

`assets/scad/catalog.json` (embebido; agregar `all:scad` al embed de
`assets/assets.go`): palabras clave, módulos y funciones built-in de OpenSCAD
(cube, sphere, cylinder, polyhedron, square, circle, polygon, text, import,
surface, translate, rotate, scale, resize, mirror, multmatrix, color, offset,
hull, minkowski, union, difference, intersection, linear_extrude,
rotate_extrude, projection, render, children, echo, assert, let, for,
intersection_for, if, each, function y módulos; funciones matemáticas, de
listas y strings; variables especiales `$fn $fa $fs $t $vpr $vpt $vpd $vpf
$children $preview`), cada uno con `name`, `kind` (module|function|keyword|
special), `signature` (`cylinder(h, r1, r2, center, r, d, d1, d2)`), `params`
con nombre y descripción, y `doc` en **es y en**. Binding:
`GetScadCatalog() (scad.Catalog, error)`.

### Bindings (`app_editor.go`)

| Binding | Devuelve |
|---|---|
| `LintCheckpointSource(id int64, fileName, source string)` | `[]scad.Diagnostic` (usa la ruta de OpenSCAD cacheada de `app_render.go`: `a.openscadPath(settings.OpenSCADPath)`) |
| `FormatScad(source string)` | `string` (indentación de settings `editorIndent`, default 4: si el campo no existe todavía, usar 4) |
| `SaveAsCheckpoint(id int64, parentFile, label, description, source string)` | `string` nombre del archivo creado |
| `GetScadCatalog()` | `scad.Catalog` |

`SaveAsCheckpoint`: valida `parentFile` con `validCheckpointName` y que exista;
crea el archivo con `O_EXCL` (si el nombre ya existe por una carrera con el
agente, prueba el siguiente número); escribe el encabezado; **no** dispara render
(lo hace el watcher). Test con `t.TempDir()`.

## Contrato 4.B (frontend) — se detalla al lanzar la tanda

Monaco empaquetado local (`monaco-editor` + `@monaco-editor/react` con
`loader.config({ monaco })`, workers por `?worker` de Vite; **sin CDN**),
gramática Monarch de OpenSCAD, tema claro/oscuro con los tokens del design
system, buscar (Cmd/Ctrl+F) y reemplazar (Cmd/Ctrl+H), solo lectura por defecto
con botón «Editar», marcadores del linter (debounce 400 ms) y de los
diagnósticos de render, formatear (Shift+Alt+F y botón), autocompletado y hover
del catálogo, guardar = checkpoint hijo (Cmd/Ctrl+S abre un mini diálogo de
rótulo/descripción). UX general: paneles redimensionables y persistidos, barra
lateral plegable, barra de estado inferior (cursor, conteo de problemas,
OpenSCAD, impresora), panel «Problemas» con clic a la línea.
