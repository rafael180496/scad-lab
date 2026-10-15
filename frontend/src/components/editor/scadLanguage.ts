import type {languages} from 'monaco-editor/esm/vs/editor/editor.api.js'

// Lenguaje OpenSCAD para Monaco: configuración (comentarios, llaves,
// autocierre, sangría) y gramática Monarch.
//
// La gramática distingue dos estados: `root` es el principio de una sentencia
// y `expr` el resto. Hace falta porque `%`, `*` y `!` son modificadores
// (fondo, deshabilitar, mostrar solo esto) al principio de una sentencia y
// operadores dentro de una expresión: `* cube(5);` no multiplica nada, pero
// `2 * sin(a)` sí. `#` (resaltar) es siempre modificador.

export const LANGUAGE_ID = 'openscad'

export const KEYWORDS = [
    'module',
    'function',
    'if',
    'else',
    'for',
    'intersection_for',
    'let',
    'each',
    'assert',
    'echo',
    'include',
    'use',
]
export const CONSTANTS = ['true', 'false', 'undef', 'PI']

export const languageConfiguration: languages.LanguageConfiguration = {
    comments: {lineComment: '//', blockComment: ['/*', '*/']},
    brackets: [
        ['{', '}'],
        ['[', ']'],
        ['(', ')'],
    ],
    autoClosingPairs: [
        {open: '{', close: '}'},
        {open: '[', close: ']'},
        {open: '(', close: ')'},
        {open: '"', close: '"', notIn: ['string', 'comment']},
        {open: '/*', close: ' */', notIn: ['string']},
    ],
    surroundingPairs: [
        {open: '{', close: '}'},
        {open: '[', close: ']'},
        {open: '(', close: ')'},
        {open: '"', close: '"'},
    ],
    indentationRules: {
        increaseIndentPattern: /^.*(\{[^}"']*|\([^)"']*|\[[^\]"']*)$/,
        decreaseIndentPattern: /^\s*[}\])].*$/,
    },
    folding: {markers: {start: /^\s*\/\*\s*\[/, end: /^\s*\/\*\s*\[/}},
    wordPattern: /(-?\d*\.\d\w*)|(\$?[A-Za-z_]\w*)/,
}

// monarch arma la gramática con los built-ins del catálogo
// (assets/scad/catalog.json, vía GetScadCatalog). Hasta que llega, módulos y
// funciones integrados se ven como identificadores comunes.
export function monarch(builtinModules: string[], builtinFunctions: string[]): languages.IMonarchLanguage {
    return {
        defaultToken: '',
        tokenPostfix: '.scad',
        keywords: KEYWORDS,
        constants: CONSTANTS,
        builtinModules,
        builtinFunctions,
        symbols: /[=><!~?:&|+\-*/^%]+/,
        escapes: /\\(?:[nrt"\\]|x[0-9A-Fa-f]{2}|u[0-9A-Fa-f]{4}|U[0-9A-Fa-f]{6})/,
        brackets: [
            {open: '{', close: '}', token: 'delimiter.curly'},
            {open: '[', close: ']', token: 'delimiter.square'},
            {open: '(', close: ')', token: 'delimiter.parenthesis'},
        ],
        tokenizer: {
            // Principio de sentencia: acá los modificadores son modificadores.
            root: [
                {include: '@whitespace'},
                [/[#%*!]/, 'keyword.modifier'],
                [/(?=\S)/, {token: '', switchTo: '@expr'}],
            ],
            expr: [
                {include: '@whitespace'},
                [/[{}]/, {token: '@brackets', switchTo: '@root'}],
                [/;/, {token: 'delimiter', switchTo: '@root'}],
                // `include <MCAD/gears.scad>`: el <…> es una ruta, no dos comparaciones.
                [/(include|use)(\s*)(<[^>\n]*>?)/, ['keyword.directive', '', 'string.path']],
                // Nombre de un módulo o función que se define.
                [/(module|function)(\s+)([A-Za-z_]\w*)/, ['keyword', '', 'entity.name']],
                [/#/, 'keyword.modifier'],
                [/\$[A-Za-z_]\w*/, 'variable.special'],
                [
                    /[A-Za-z_]\w*/,
                    {
                        cases: {
                            '@keywords': 'keyword',
                            '@constants': 'constant',
                            '@builtinModules': 'predefined.module',
                            '@builtinFunctions': 'predefined.function',
                            '@default': 'identifier',
                        },
                    },
                ],
                [/0[xX][0-9a-fA-F]+/, 'number.hex'],
                [/(\d+\.?\d*|\.\d+)([eE][-+]?\d+)?/, 'number'],
                [/"/, 'string', '@string'],
                [/[()[\]]/, '@brackets'],
                [/@symbols/, 'operator'],
                [/[,.]/, 'delimiter'],
            ],
            whitespace: [
                [/[ \t\r\n]+/, ''],
                // Encabezado del checkpoint: `// @checkpoint parent=… desc="…"`.
                [/(\/\/\s*)([@]checkpoint\b)(.*)$/, ['comment.checkpoint', 'annotation.checkpoint', 'comment.checkpoint']],
                // Customizer: `/* [Sección] */` y el rango al final de la línea `// [min:step:max]`.
                [/\/\*\s*\[[^\]\n]*\]\s*\*\//, 'annotation.section'],
                [/\/\/\s*\[[^\]\n]*\]\s*$/, 'annotation.range'],
                [/\/\/.*$/, 'comment'],
                [/\/\*/, 'comment', '@blockComment'],
            ],
            blockComment: [
                [/[^*]+/, 'comment'],
                [/\*\//, 'comment', '@pop'],
                [/\*/, 'comment'],
            ],
            string: [
                [/[^\\"]+/, 'string'],
                [/@escapes/, 'string.escape'],
                [/\\./, 'string.escape.invalid'],
                [/"/, 'string', '@pop'],
            ],
        },
    }
}
