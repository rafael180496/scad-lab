#!/usr/bin/env node
// Chequeo estricto de i18n del frontend (ver claude/05-i18n.md). Sin línea
// base: cualquier hallazgo falla; se corrige en src/i18n/es.ts y en.ts.
//
// Recorre el AST de TypeScript —no regex— de cada .ts/.tsx y busca:
//
//  1. Textos a mano que deberían venir del diccionario:
//     - texto entre etiquetas JSX;
//     - atributos de texto (title, placeholder, aria-label, alt, label, hint…),
//       con literales, ternarios, concatenaciones y plantillas (`Abrir ${x}`);
//     - propiedades de objeto de texto (label, title, message, …): ítems de menú;
//     - argumentos de lo que termina en pantalla: toast.*(…), confirmDialog(…),
//       setError(…), new Error(…) (se muestra con String(e));
//     - valores por defecto de props de texto (`confirmLabel = 'Guardar'`);
//     - en lib/, hooks/ y los .ts de components/: cualquier literal que sea
//       prosa (dos palabras o más), porque ahí viven los formateadores y las
//       etiquetas que llegan a la UI sin pasar por un componente.
//  2. Claves de es.ts / en.ts que ningún código usa (--unused las lista con su
//     línea). Las que se arman dinámicamente (un objeto entero pasado por prop,
//     indexado con una variable…) se conservan: --dynamic las lista.
//
// Una línea con `i18n-ignore` queda fuera (para lo que de verdad no es texto
// de interfaz: ids técnicos, datos de ejemplo). Úsalo con motivo.
//
// Uso: node scripts/i18n-check.mjs [--list archivo] [--unused] [--dynamic]
import fs from 'node:fs'
import path from 'node:path'
import ts from 'typescript'

const root = path.resolve(import.meta.dirname, '../src')
const TEXT_ATTRS = new Set(['title', 'placeholder', 'aria-label', 'label', 'alt', 'description', 'confirmLabel', 'hint', 'emptyText', 'tooltip'])
const TEXT_PROPS = new Set(['label', 'hint', 'title', 'description', 'placeholder', 'text', 'message', 'tooltip'])
const TEXTY_NAME = /(label|title|hint|placeholder|description|text|message|tooltip)$/i
// Archivos que no son interfaz: los diccionarios, los bindings generados y el
// banco de capturas (sus datos de prueba son contenido, no interfaz).
const SKIP = [/^i18n\//, /^uishot\.tsx$/, /\.d\.ts$/, /\.test\.tsx?$/]
// Donde viven los formateadores: ahí se revisa también la prosa suelta.
const LOGIC_DIRS = /^(lib|hooks)\//

// Nombres propios y marcas que se escriben igual en los dos idiomas.
const SAME_IN_ALL = new Set([
    'ScadLab', 'OpenSCAD', 'openscad', 'Bambu Studio', 'Bambu Lab', 'Claude Code', 'Claude', 'opencode', 'Codex', 'Antigravity',
    'Ollama', 'OpenAI', 'Anthropic', 'Gemini', 'MCP', 'AMS', 'STL', '3MF', 'PNG', 'SVG', 'Markdown', 'macOS', 'Windows', 'Linux',
])
// Unidades que acompañan a un número dentro de una plantilla (`${n} mm`).
const UNITS = /\b(mm|cm|kg|g|px|ms|s|min|h|fps|MB|KB|GB|x|deg)\b/g

const isText = (s) => {
    // Unidades, siglas (HTTP, SVG) y marcas no cuentan como texto a traducir.
    const v = s
        .replace(UNITS, ' ')
        .replace(/\b[A-Z0-9]{2,6}\b/g, ' ')
        .replace(new RegExp(`\\b(${[...SAME_IN_ALL].join('|')})\\b`, 'g'), ' ')
        .replace(/\s+/g, ' ')
        .trim()
    if (!/\p{L}{2,}/u.test(v)) return false
    if (SAME_IN_ALL.has(v)) return false
    if (/^[A-Z0-9_./:+-]{2,6}$/.test(v)) return false
    return true
}
// Prosa: dos palabras de letras separadas por espacio/puntuación, sin pinta de código.
const isProse = (s) => {
    const v = s.trim()
    if (!/\p{L}{2,}[,.:;!?]*\s+\p{L}{2,}/u.test(v)) return false
    if (/[{}<>=\\/"']|=>|\w\(\)/.test(v)) return false
    return !SAME_IN_ALL.has(v)
}

// Frase de interfaz: mayúscula inicial seguida de más palabras o de puntuación
// final, o cualquier letra acentuada / signo ¿¡ (en código, solo el texto lo lleva).
const looksLikeUiText = (s) => /[áéíóúñÁÉÍÓÚÑ¿¡]/.test(s) || /^\p{Lu}\p{Ll}{2,}(\s+\p{L}|[.!?:…])/u.test(s.trim())

function walkFiles(dir, out = []) {
    for (const e of fs.readdirSync(dir, {withFileTypes: true})) {
        const p = path.join(dir, e.name)
        if (e.isDirectory()) walkFiles(p, out)
        else if (/\.(tsx|ts)$/.test(e.name)) out.push(p)
    }
    return out
}

function parse(file) {
    const src = fs.readFileSync(file, 'utf8')
    const sf = ts.createSourceFile(file, src, ts.ScriptTarget.Latest, true, file.endsWith('x') ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
    return {src, sf, lines: src.split('\n')}
}

// ---------------------------------------------------------------- textos ---

function scan(file, rel) {
    const {sf, lines} = parse(file)
    const hits = []
    const logic = LOGIC_DIRS.test(rel) || (rel.endsWith('.ts') && rel.startsWith('components/'))
    const add = (node, text) => {
        const {line} = sf.getLineAndCharacterOfPosition(node.getStart())
        if (lines[line].includes('i18n-ignore') || (line > 0 && lines[line - 1].includes('i18n-ignore'))) return
        hits.push({line: line + 1, text: text.replace(/\s+/g, ' ').trim().slice(0, 80)})
    }
    // Recorre los literales que PUEDEN terminar en pantalla: el valor mismo, las
    // ramas de un ternario, una concatenación, el lado derecho de `a && 'x'` y
    // los dos de `a || b` / `a ?? b`. Una comparación es código, no texto.
    const strings = (node, cb) => {
        if (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) cb(node, node.text)
        else if (ts.isTemplateExpression(node)) cb(node, node.head.text + ' ' + node.templateSpans.map((s) => s.literal.text).join(' '))
        else if (ts.isConditionalExpression(node)) {
            strings(node.whenTrue, cb)
            strings(node.whenFalse, cb)
        } else if (ts.isBinaryExpression(node)) {
            const op = node.operatorToken.kind
            const K = ts.SyntaxKind
            if (op === K.PlusToken || op === K.BarBarToken || op === K.QuestionQuestionToken) {
                strings(node.left, cb)
                strings(node.right, cb)
            } else if (op === K.AmpersandAmpersandToken) strings(node.right, cb)
        } else if (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) || ts.isNonNullExpression(node)) strings(node.expression, cb)
    }
    const report = (node) => strings(node, (n, t) => isText(t) && add(n, t))

    const visit = (node) => {
        if (ts.isJsxText(node)) {
            if (isText(node.text)) add(node, node.text)
        } else if (ts.isJsxExpression(node) && node.expression && (ts.isJsxElement(node.parent) || ts.isJsxFragment(node.parent))) {
            report(node.expression)
        } else if (ts.isJsxAttribute(node)) {
            const name = node.name.getText(sf)
            if ((TEXT_ATTRS.has(name) || TEXTY_NAME.test(name)) && node.initializer) {
                report(ts.isJsxExpression(node.initializer) ? node.initializer.expression ?? node.initializer : node.initializer)
            }
        } else if (ts.isPropertyAssignment(node)) {
            const name = node.name.getText(sf).replace(/['"]/g, '')
            if (TEXT_PROPS.has(name) || TEXTY_NAME.test(name)) report(node.initializer)
        } else if (ts.isCallExpression(node) || ts.isNewExpression(node)) {
            const callee = node.expression.getText(sf)
            // Lo que termina en pantalla: avisos, diálogos de confirmación,
            // estados de error y los Error que la UI muestra con String(e).
            if (
                callee === 'confirmDialog' ||
                /^set(Error|Notice|Message|Status|Info|Warning|Hint)$/.test(callee) ||
                (ts.isNewExpression(node) && callee === 'Error')
            ) {
                for (const a of node.arguments ?? []) report(a)
            } else if (/^toast(\.\w+)?$/.test(callee)) {
                // Solo el mensaje: el segundo argumento es el id para reemplazar un aviso.
                if (node.arguments?.[0]) report(node.arguments[0])
            }
        } else if ((ts.isBindingElement(node) || ts.isParameter(node)) && node.initializer && ts.isIdentifier(node.name)) {
            if (TEXTY_NAME.test(node.name.text)) report(node.initializer)
        } else if (!logic && (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && isLogicText(node) && looksLikeUiText(node.text)) {
            // Red de seguridad en componentes: un literal suelto con pinta de frase
            // (`const label = ok ? 'Guardado.' : '¿Seguro?'`) que no pasó por ningún
            // atributo ni llamada de las reglas de arriba.
            add(node, node.text)
        } else if (logic && (ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node) || ts.isTemplateExpression(node))) {
            if (isLogicText(node)) {
                const text = ts.isTemplateExpression(node) ? node.head.text + ' ' + node.templateSpans.map((s) => s.literal.text).join(' ') : node.text
                if (isProse(text)) add(node, text)
            }
        }
        ts.forEachChild(node, visit)
    }
    visit(sf)
    // Un literal alcanzado por dos reglas (p. ej. un `title:` dentro de lib/)
    // se cuenta una vez.
    const seen = new Set()
    return hits.filter((h) => (seen.has(h.line + h.text) ? false : seen.add(h.line + h.text)))
}

// Un literal de lib/hooks que no es texto de pantalla: import/export, tipos
// literales, claves, comparaciones, logs, regex, selectores y mensajes de
// consola. Lo que queda (return, asignaciones, argumentos) puede llegar a la UI.
function isLogicText(node) {
    const p = node.parent
    if (!p) return false
    if (ts.isImportDeclaration(p) || ts.isExportDeclaration(p) || ts.isLiteralTypeNode(p) || ts.isCaseClause(p) || ts.isExternalModuleReference(p)) return false
    if (ts.isPropertyAssignment(p) && p.name === node) return false
    // Valores técnicos de configuración (Monaco, CSS): no son texto de interfaz.
    if (ts.isPropertyAssignment(p) && /^(fontStyle|fontFamily|token|foreground|background|className|style|cursor|transition)$/.test(p.name.getText())) return false
    if (ts.isElementAccessExpression(p) || ts.isComputedPropertyName(p)) return false
    if (ts.isBinaryExpression(p)) {
        const K = ts.SyntaxKind
        if ([K.EqualsEqualsEqualsToken, K.ExclamationEqualsEqualsToken, K.EqualsEqualsToken, K.ExclamationEqualsToken].includes(p.operatorToken.kind)) return false
    }
    if (ts.isCallExpression(p) && p.arguments.includes(node)) {
        const callee = p.expression.getText()
        if (/^console\./.test(callee) || /(addEventListener|removeEventListener|querySelector|querySelectorAll|getItem|setItem|removeItem|postMessage|setAttribute|getAttribute|createElement|matchMedia|dispatchEvent|test|match|startsWith|endsWith|includes|split|replace|replaceAll|join|indexOf|require|import|emit|on|off|once|listen|invoke|define|register|setLanguage|setTheme|localStorage)$/.test(callee)) return false
    }
    return true
}

// --------------------------------------------------------- claves sin uso ---

// Aplana un diccionario: devuelve [{path:['a','b'], line}] de cada hoja string.
function dictLeaves(file) {
    const {sf} = parse(file)
    const leaves = []
    const walk = (obj, prefix) => {
        for (const prop of obj.properties) {
            if (!ts.isPropertyAssignment(prop)) continue
            const name = prop.name.getText(sf).replace(/^['"]|['"]$/g, '')
            const init = unwrap(prop.initializer)
            const p = [...prefix, name]
            if (ts.isObjectLiteralExpression(init)) walk(init, p)
            else {
                const {line} = sf.getLineAndCharacterOfPosition(prop.getStart())
                leaves.push({path: p, line: line + 1})
            }
        }
    }
    const unwrap = (n) => (ts.isAsExpression(n) || ts.isSatisfiesExpression?.(n) || ts.isParenthesizedExpression(n) ? unwrap(n.expression) : n)
    sf.forEachChild((n) => {
        if (ts.isVariableStatement(n)) {
            for (const d of n.declarationList.declarations) if (d.initializer && ts.isObjectLiteralExpression(unwrap(d.initializer))) walk(unwrap(d.initializer), [])
        }
    })
    return leaves
}

// Reúne, de todo el código fuera de i18n/, las cadenas de acceso `a.b.c`
// (con alias de una variable local, desestructurado y `t()`/`useT()` como raíz)
// y los objetos del diccionario que se usan enteros o con índice dinámico.
function collectUsage(files) {
    const chains = [] // arreglos de nombres
    const whole = [] // cadenas que terminan en un uso "entero" (prop, spread, índice dinámico…)
    for (const f of files) {
        const {sf} = parse(f)
        const alias = new Map() // nombre local -> cadena
        // Primera pasada: alias `const x = t.a.b` / `const {c} = t.a.b`.
        // Devuelve las cadenas posibles de una expresión: un mismo nombre local
        // (`t`) puede declararse en varias funciones del archivo, con valores
        // distintos, y sin análisis de alcance se prueban todos.
        const chainOf = (node) => {
            const out = []
            let cur = node
            for (;;) {
                if (ts.isParenthesizedExpression(cur) || ts.isNonNullExpression(cur) || ts.isAsExpression(cur)) cur = cur.expression
                else if (ts.isPropertyAccessExpression(cur)) {
                    out.unshift(cur.name.text)
                    cur = cur.expression
                } else if (ts.isElementAccessExpression(cur)) {
                    const a = cur.argumentExpression
                    if (a && (ts.isStringLiteral(a) || ts.isNoSubstitutionTemplateLiteral(a))) out.unshift(a.text)
                    else out.unshift('*') // índice dinámico
                    cur = cur.expression
                } else if (ts.isCallExpression(cur) && ts.isIdentifier(cur.expression) && ['t', 'tNow', 'useT'].includes(cur.expression.text)) {
                    return [['@', ...out]] // '@' marca la raíz: el diccionario
                } else if (ts.isIdentifier(cur)) {
                    const a = alias.get(cur.text)
                    if (a) return a.map((x) => [...x, ...out])
                    // `t` suelto es el diccionario (parámetro `t: Dict`); cualquier otro nombre no.
                    return cur.text === 't' ? [['@', ...out]] : []
                } else return null
            }
        }
        const addAlias = (name, cs) => {
            const list = alias.get(name) ?? []
            for (const c of cs) if (!list.some((x) => x.join('.') === c.join('.'))) list.push(c)
            alias.set(name, list)
        }
        const decls = []
        const collectDecls = (n) => {
            if (ts.isVariableDeclaration(n) && n.initializer) decls.push(n)
            ts.forEachChild(n, collectDecls)
        }
        collectDecls(sf)
        // `const {t: tt} = props`: `t` es el diccionario aunque se renombre.
        const collectRenamed = (n) => {
            if (ts.isBindingElement(n) && n.propertyName?.getText(sf) === 't' && ts.isIdentifier(n.name)) addAlias(n.name.text, [['@']])
            ts.forEachChild(n, collectRenamed)
        }
        collectRenamed(sf)
        for (let pass = 0; pass < 3; pass++) {
            for (const d of decls) {
                const cs = chainOf(d.initializer)
                if (!cs) continue
                if (ts.isIdentifier(d.name)) addAlias(d.name.text, cs)
                else if (ts.isObjectBindingPattern(d.name)) {
                    for (const el of d.name.elements) {
                        const key = (el.propertyName ?? el.name).getText(sf)
                        for (const c of cs) chains.push([...c, key])
                    }
                }
            }
        }
        const visit = (n) => {
            if (ts.isPropertyAccessExpression(n) || ts.isElementAccessExpression(n)) {
                const parent = n.parent
                const isInner =
                    (ts.isPropertyAccessExpression(parent) && parent.expression === n) || (ts.isElementAccessExpression(parent) && parent.expression === n)
                if (!isInner) {
                    for (const c of chainOf(n) ?? []) {
                        chains.push(c)
                        // Usado entero: no es operando de otra cadena ni se llama a un método.
                        const isAliasInit = ts.isVariableDeclaration(parent) && parent.initializer === n // `const c = t.a`: los usos de `c` cuentan aparte
                        if (!isAliasInit && (!ts.isCallExpression(parent) || parent.expression !== n)) whole.push(c)
                    }
                }
            } else if (ts.isIdentifier(n) && alias.has(n.text)) {
                const p = n.parent
                const inChain = (ts.isPropertyAccessExpression(p) || ts.isElementAccessExpression(p)) && p.expression === n
                const isDecl = ts.isVariableDeclaration(p) && p.name === n
                if (!inChain && !isDecl) whole.push(...alias.get(n.text)) // se pasa/devuelve el objeto entero
            } else if (ts.isSpreadAssignment(n) || ts.isSpreadElement(n)) {
                whole.push(...(chainOf(n.expression) ?? []))
            }
            ts.forEachChild(n, visit)
        }
        visit(sf)
    }
    return {chains, whole}
}

// Voseo: el diccionario en español va con tú («elige», no «elegí»).
const VOSEO = /(?<![\p{L}\p{N}_])(?:vos|sos|acá|tenés|podés|querés|sabés|hacés|ponés|decís|venís|elegís|usás|necesitás|preferís|estás|andás|elegí|creá|pedí|abrí|guardá|hacé|agregá|borrá|seleccioná|escribí|cambiá|revisá|probá|instalá|configurá|definí|reiniciá|preferí|reformulá|respondé|corregí|decí|mirá|usá|tené|poné|volvé|esperá|intentá|cerrá|activá|desactivá|confirmá|cancelá|verificá|ejecutá|descargá|importá|exportá|fijate|asegurate|dejá|pasá|corré|buscá|copiá|pegá|marcá|ajustá|completá|indicá|especificá|escogé|seguí|empezá|comenzá|iniciá|actualizá|restaurá|recuperá|duplicá|renombrá|ordená|filtrá|compará|chequeá|aceptá|presioná|apretá|tocá|arrastrá|soltá|movés|mové|sacá|salí|entrá|ingresá|rellená|leé|contame|avisame|pedime|decime)(?![\p{L}\p{N}_])/giu

const contains = (chain, path) => {
    for (let i = 0; i + path.length <= chain.length; i++) {
        if (chain[i] !== path[0]) continue // el comodín solo vale tras una clave literal
        let ok = true
        for (let j = 0; j < path.length; j++) {
            if (chain[i + j] !== path[j] && chain[i + j] !== '*') {
                ok = false
                break
            }
        }
        if (ok) return true
    }
    return false
}
// ¿La cadena (con raíz '@') es exactamente un prefijo propio de `path`? Es un
// objeto del diccionario usado entero: pasado por prop, devuelto, esparcido…
const isPrefixOf = (chain, path) => {
    const c = chain.slice(1)
    return chain[0] === '@' && c.length >= 1 && c.length < path.length && c.every((k, i) => k === path[i])
}
// ¿Hay un índice dinámico (`t.a.b[x]`) justo bajo un prefijo de `path`?
const dynamicIndexUnder = (chain, path) => {
    const i = chain.indexOf('*')
    return chain[0] === '@' && i > 0 && chain.slice(1, i).every((k, j) => k === path[j]) && i - 1 < path.length
}

function unusedKeys(files, dictFile) {
    const leaves = dictLeaves(dictFile)
    const {chains, whole} = collectUsage(files)
    const unused = []
    const dynamic = []
    for (const lf of leaves) {
        if (chains.some((c) => !c.includes('*') && contains(c, lf.path))) continue
        // El objeto que lo contiene se usa entero o con índice dinámico: puede
        // estar usándose sin que se vea la clave. Se conserva y se documenta.
        const dyn = whole.some((c) => isPrefixOf(c, lf.path)) || chains.some((c) => dynamicIndexUnder(c, lf.path))
        ;(dyn ? dynamic : unused).push(lf)
    }
    return {unused, dynamic}
}

// ------------------------------------------------------------------ main ---

const args = process.argv.slice(2)
const all = walkFiles(root)
const files = all
    .map((f) => path.relative(root, f))
    .filter((f) => !SKIP.some((re) => re.test(f)))
    .sort()

if (args[0] === '--list') {
    for (const h of scan(path.join(root, args[1]), args[1])) console.log(`${args[1]}:${h.line}  ${h.text}`)
    process.exit(0)
}

const codeFiles = all.filter((f) => !/\/i18n\//.test(f) && !/\.d\.ts$/.test(f))
const es = path.join(root, 'i18n/es.ts')

if (args[0] === '--unused' || args[0] === '--dynamic') {
    const {unused, dynamic} = unusedKeys(codeFiles, es)
    const list = args[0] === '--unused' ? unused : dynamic
    for (const l of list) console.log(`${l.path.join('.')}  (es.ts:${l.line})`)
    process.exit(0)
}

let problems = 0
const byFile = {}
for (const f of files) {
    const h = scan(path.join(root, f), f)
    if (h.length) byFile[f] = h
}
for (const [f, h] of Object.entries(byFile)) {
    for (const x of h) console.error(`${f}:${x.line}: texto sin i18n: ${JSON.stringify(x.text)}`)
    problems += h.length
}
if (problems) console.error(`\n${problems} texto(s) sin pasar por el diccionario (src/i18n/es.ts y en.ts).`)

const {sf: esSf} = parse(es)
const voseo = []
const findVoseo = (n) => {
    if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) {
        for (const m of n.text.matchAll(VOSEO)) voseo.push({line: esSf.getLineAndCharacterOfPosition(n.getStart()).line + 1, word: m[0]})
    }
    ts.forEachChild(n, findVoseo)
}
findVoseo(esSf)
for (const v of voseo) console.error(`i18n/es.ts:${v.line}: voseo «${v.word}» (usa tú)`)
problems += voseo.length

const {unused, dynamic} = unusedKeys(codeFiles, es)
for (const u of unused) console.error(`i18n/es.ts:${u.line}: clave sin uso: ${u.path.join('.')} (bórrala de es.ts y en.ts)`)
if (unused.length) console.error(`\n${unused.length} clave(s) sin uso en el diccionario.`)

if (problems || unused.length) process.exit(1)
console.log(`i18n (frontend) OK — ${files.length} archivos revisados, ${dynamic.length} clave(s) dinámicas conservadas (--dynamic)`)
