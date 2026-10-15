# Editor de código OpenSCAD (S4): lexer, linter, formateador, catálogo

Spec: [.claude/specs/s4-editor-openscad.md](../.claude/specs/s4-editor-openscad.md).
El núcleo vive en Go (`backend/scad`) y no en TS porque lo usan también el MCP
de los agentes (S6: `lint`, `format`) y la auto-iteración (S5): una sola
implementación. El editor lo llama por binding con debounce.

## Lexer (`lexer.go`)

`Lex(src) ([]Token, error)` → `Token{Kind, Text, Line, Col}`, 1-based.

- **Columnas en unidades UTF-16**, las de Monaco: un acento suma 1 y un emoji 2.
- Clases: `ident`, `number`, `string`, `comment` (línea o bloque, con sus
  delimitadores), `op`, `punct`, `keyword`, `newline` y **`path`**: el `<…>` de
  `include`/`use` es un solo token. Leído como operadores serían divisiones y
  comparaciones, y el formateador les metería espacios.
- Como flex en OpenSCAD: entre número e identificador gana el más largo y, si
  empatan, el número (`3d` es identificador, `1e5` número). Hex `0x1F` y los
  operadores de bits de 2025 (`& | ~ << >>`) se reconocen.
- Un carácter desconocido es `punct` y el formateador respeta su espaciado. El
  único error es un string o un `/*` sin cerrar (`*LexError`, con línea).
- El BOM inicial se ignora; el `\r` de `\r\n` no forma parte de los comentarios.

## Linter (`lint.go`, `rules.go`, `structure.go`)

`Lint(ctx, src, LintOptions) []Diagnostic` nunca falla: lo que no se pudo
revisar no aporta diagnósticos. Ordenados por línea y columna.

**OpenSCAD real** (si hay `OpenSCADPath`): escribe el código a
`<ws>/.scadlab/lint/lint-*.scad` (el watcher no es recursivo: no lo ve), corre
`openscad -o <tmp>.ast <tmp>.scad` con `cwd` = workspace,
`OPENSCADPATH=<ws><sep><ws>/lib[<sep>el que hubiera]`, plazo de 5 s y
`hideWindow`, y borra los dos temporales siempre. Exportar a `.ast` **también
evalúa** el nivel superior (~50 ms con 2026.10.11): además de `Parser error`
salen módulos y variables desconocidos, includes faltantes y reasignaciones.
La salida se lee con `render.ParseOutput` (`backend/render/parse_export.go`):

- Mensaje sobre el temporal → misma línea en el archivo editado (es el mismo
  texto). En el mensaje el nombre del temporal se reemplaza por el del
  checkpoint.
- Mensaje sobre un include → en la línea del `include`/`use` que lo trae, con
  `archivo:línea:` al principio del texto.
- Todos van con `Code: "syntax"`, `Source: "openscad"`; sin columna → `Col 1`,
  `EndLine/EndCol 0` (toda la línea).
- **Sin duplicados**: `Can't find include file` / `Can't open library` se
  convierten en SL004 (error, con la ruta marcada) y `was assigned on line N but
  was overwritten` se descarta si SL008 ya marcó esa línea.
- Plazo vencido o binario que no arranca → solo reglas propias.

**Reglas propias** (`Source: "scadlab"`), sobre un esqueleto de sentencias
(`structure.go`): no es un parser de OpenSCAD; reconoce asignaciones,
`module`/`function`, `include`/`use`, bloques y cadenas de instanciación
(`[!#%*] cabeza(…) hijo`, con `if … else`), y salta las expresiones por
paréntesis balanceados. Lo que no entiende lo saltea hasta el próximo `;`/`}`:
el editor lo llama mientras se escribe. Con el archivo cortado (string sin
cerrar) solo corren SL001/SL002 y el error del lexer.

| Código | Sev. | Regla | Detalle que evita falsos positivos |
|---|---|---|---|
| SL001 | warning | encabezado `// @checkpoint parent=… desc="…"` en la línea 1 | solo si `FileName != ""`; `desc` admite `\"` |
| SL002 | warning | `parent=` inexistente (o el propio archivo) | contra `ExistingFiles`; si es nil, el disco |
| SL003 | warning | objeto de primer nivel sin `color()` | ver abajo |
| SL004 | error | `include`/`use` que no se encuentra | con OpenSCAD lo decide OpenSCAD (conoce MCAD y sus librerías); sin él: workspace, `lib/`, `OPENSCADPATH`, librerías del usuario |
| SL005 | warning | include de un checkpoint (`NNN-*.scad`) | por nombre de archivo |
| SL006 | info | `$fn` > 256 asignado en el nivel superior | cualquier número > 256 en la expresión (`$preview ? 32 : 512`); no mira módulos ni argumentos |
| SL007 | info | `minkowski()` dentro de `minkowski()`/`hull()` | sigue hijos, bloques, `if`/`else`; no entra a módulos llamados |
| SL008 | warning | variable asignada dos veces en el mismo ámbito | cada `{}` es su ámbito; `let`, parámetros y `function` no cuentan |
| SL009 | info | parámetro numérico del Customizer sin `// [..]` | solo si hay `/* [..] */`; hasta el primer `module`/`function`; salta `[Hidden]`, `$vars`, strings, booleanos y expresiones |

**SL003 en detalle.** Se recorre cada sentencia del nivel superior:
`color(…)` en cualquier eslabón de la cadena la da por coloreada; `if`/`else`,
`for`, `let`, `echo`, `assert`, `union`, `render` y las transformaciones son
transparentes (se mira el hijo; un bloque `{}` revisa cada hijo por separado);
cualquier otra cabeza (primitiva, `difference`, `hull`, extrusión, módulo) es
un objeto y basta con que algo debajo tenga color (`difference` toma el del
primer hijo: avisar por los que resta sería ruido). Un módulo **del propio
archivo** que aplica `color()` adentro —directo o llamando a otro que lo hace,
por punto fijo— cuenta como coloreado. `*` y `%` no salen en el render: no se
avisan. Comentarios y strings no son tokens de código, así que un `color(` o un
`include` dentro de ellos no cuenta.

Las 10 plantillas de `openscad-templates` no disparan ninguna regla
(`TestTemplatesAreClean`).

### Agregar una regla

1. Constante `CodeXxx = "SL0NN"` en `rules.go` (siguiente número libre).
2. `func ruleXxx(a *analysis)` que recorra `a.top` (esqueleto; `walk` visita
   hijos, `else` y bloques) o `a.toks` (tokens con comentarios), y reporte con
   `a.addTok(sev, code, token, i18n.Msg{ES, EN}, args…)`.
3. Sumarla a `ownRules`.
4. Tests en `rules_test.go`: casos positivos **y** falsos positivos típicos
   (comentarios, strings, módulos, ámbitos). Verificar que las plantillas siguen
   limpias.
5. Fila en esta tabla y en la del spec.

## Formateador (`format.go`)

`Format(src, FormatOptions{IndentSize})`; indentación fuera de 1–8 → 4. Falla
solo si el lexer falla. Conservador: **nunca une ni parte líneas** y no toca el
interior de strings ni comentarios.

- **Indentación** (en columnas): un cierre al principio de línea va a la altura
  de la línea que abrió; dentro de `(`/`[`, alineado con lo que sigue a la
  apertura si hay código en la misma línea (`f(a,` → la siguiente bajo la `a`),
  o un nivel más si la apertura termina la línea o es una comprensión
  (`[for …`); en un bloque, la sentencia arranca al nivel del bloque.
  Continuación de una sentencia con `=` en su nivel (asignación, `function`):
  bloque + 1, todas iguales. Continuación de una cadena de módulos (`if (…)` /
  `translate(…)` / `cube();` en líneas distintas): escalera, una más que la
  cabeza anterior. Un `{` que empieza línea va a la altura de su cabeza (Allman).
- **Espacios**: uno alrededor de `=` y de los operadores binarios (incluidos `?`,
  `:` y el `:` de los rangos); ninguno después de un unario (`-x`, `!a`, `~m`) ni
  de un modificador (`#`, `%`, `*`, `!` delante de un módulo, que se distinguen
  de `%`/`*` binarios por contexto); `if (`, `for (`; `f(`, `let(`, `a[0]`,
  `p.x` pegados; `, ` y `; `. Ambiguos (`) [` en una comprensión vs `f(x)[0]`)
  conservan si había espacio.
- **Comentarios**: el espacio antes de un comentario final se conserva (mantiene
  la alineación de `// [1:100]`); los de línea pierden los blancos finales. Un
  `/* … */` de varias líneas que empieza la línea se corre entero lo mismo que
  su primera línea; si correrlo a la izquierda le comería texto, sus líneas
  interiores quedan como estaban.
- Líneas en blanco: máximo una seguida, ninguna al principio ni al final;
  archivo terminado en un salto. `\r\n` si el original lo usaba.
- **Idempotente** y **no cambia los tokens** (`assertSameTokens` en cada caso de
  la tabla). `TestFormatTemplatesSameGeometry` renderiza cada plantilla con
  OpenSCAD real antes y después de formatear y compara volumen y triángulos con
  `mesh.InfoFromFile` (iguales en las 10).

## Catálogo (`catalog.go`, `assets/scad/catalog.json`)

Embebido (`all:scad` en `assets/assets.go`). `Catalog{version, entries}`,
`CatalogEntry{name, kind, signature, params[{name, doc}], doc, since?}` con
`doc` y la descripción de cada parámetro en `{es, en}` (el idioma lo elige el
frontend). `kind`: `module`, `function`, `keyword`, `special` (`$fn`… y `PI`).
Describe OpenSCAD 2021.01 (`version`); algo posterior lleva `since`.
`TestCatalog` valida que carga, que cada entrada tiene firma y doc en los dos
idiomas, que cada parámetro aparece en la firma y que están todas las palabras
clave del lexer. Afirmaciones verificadas con 2026.10.11 (`round(-2.5) = -3`,
`$parent_modules` solo dentro de un módulo…).

## Bindings (`app_editor.go`)

| Binding | Devuelve | Nota |
|---|---|---|
| `LintCheckpointSource(id, fileName, source)` | `[]scad.Diagnostic` | `fileName` "" = sin reglas de encabezado; ruta de OpenSCAD cacheada (`a.openscadPath`); `ExistingFiles` de `workspace.Scan` |
| `FormatScad(source)` | `string` | indentación de `settings.editorIndent` (2 o 4; si no existe o es otro valor, 4). Se lee por su nombre JSON para no depender de cuándo se agrega el campo |
| `SaveAsCheckpoint(id, parentFile, label, description, source)` | `string` (nombre creado) | ver abajo |
| `GetScadCatalog()` | `scad.Catalog` | |

`Diagnostic{severity, line, col, endLine, endCol, code, source, message}`;
`endLine/endCol` 0 = toda la línea; `message` en el idioma activo.

**Guardar nunca sobrescribe.** `SaveAsCheckpoint` valida `parentFile` con
`validCheckpointName` y que exista; calcula `NNN-slug` con
`workspace.NextFileName(label)` y crea con `O_CREATE|O_EXCL`: si el agente tomó
ese nombre entre medio, prueba el número siguiente (hasta 50). Reemplaza la
primera línea si es un `// @checkpoint …` o antepone el encabezado; `desc` es
`description` (o `label` si está vacía), en una línea y con `\` y `"`
escapados. Respeta `\r\n`. Si la escritura falla, borra el archivo a medias
(es suyo, no un checkpoint). No renderiza ni toca el rótulo del árbol: el
watcher sincroniza y renderiza el archivo nuevo.

## Workspace

`.scadlab/` va en el `.gitignore` de los workspaces nuevos (temporales del
linter y papelera).

## Frontend (tanda 4.B) — `components/editor/`

- **Monaco empaquetado local**, sin `@monaco-editor/react`: su `loader` lleva la
  URL de jsDelivr como literal por defecto (quedaría en `dist/`). `monaco.ts`
  importa el núcleo (`editor.api`) y solo las contribuciones que se usan
  (`features/*/register.js`) más `suggest/browser/suggestController.js` —el
  feature `suggest` solo trae las sugerencias en línea—. Worker base por
  `?worker`. Alias de Vite a la ruta **real** de `monaco-editor/esm` (pnpm la
  enlaza: con la del enlace habría dos copias de los registros) y
  `optimizeDeps.exclude` (el optimizador no maneja el `?worker`).
- **Chunk propio** con `React.lazy` (`lazy.ts`). Los textos de los widgets salen
  del catálogo de mensajes de Monaco (`nls/lang/es.js`, local) y se leen al
  evaluar los módulos: se carga antes que Monaco; cambiar de idioma con el editor
  cargado los deja en el anterior hasta reiniciar.
- `scadLanguage.ts`: Monarch con dos estados (`root` = principio de sentencia,
  donde `% * !` son modificadores; `expr` = expresión, donde son operadores) y
  built-ins del catálogo (se registra de nuevo cuando llega `GetScadCatalog`). En
  un regex de Monarch `@x` es un atributo: `[@]checkpoint`.
- Tema `scadlab` definido desde los tokens de `index.css` y redefinido al cambiar
  la clase de `<html>`. Colorizado de llaves apagado (paleta de VS Code).
- `renderValidationDecorations: 'on'`: por defecto Monaco **no pinta marcadores
  en solo lectura**, que es como se abre un checkpoint.
- Formatear = `DocumentFormattingEditProvider` → `FormatScad`; el worker reduce el
  reemplazo a ediciones mínimas (cursor y scroll quedan, un solo deshacer).
  Shift+Alt+F se sobrescribe para que en solo lectura pase a edición y formatee.
- Estado compartido en `lib/editorSession.ts` (store con suscripción, no estado
  de `App`: el cursor cambia con cada tecla y redibujaría el visor). `goToLine`
  lo usan el panel Problemas y los diagnósticos del render.
- Marcadores: linter + render (`lib/problems.ts`, sin duplicar el error de
  sintaxis que dan los dos); los del render solo mientras no hay cambios.
- Guardar: `SaveCheckpointDialog` → `SaveAsCheckpoint` → `App.select(nuevo)`; se
  marca el archivo en `autoTried` para que el auto-render lo haga solo el watcher.
- `EditorBoundary`: si Monaco no carga, el código se muestra como texto.
- **macOS**: la app no define menú nativo; sin un menú Edición WKWebView no
  entrega ⌘C/⌘V/⌘Z a la página (pendiente en `main.go`, fuera de esta tanda).
