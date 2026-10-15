import type * as Monaco from 'monaco-editor/esm/vs/editor/editor.api.js'
import type {scad} from '../../../wailsjs/go/models'
import {GetScadCatalog} from '../../../wailsjs/go/main/App'
import {getLanguage, t} from '../../i18n'
import {LANGUAGE_ID} from './scadLanguage'

// Autocompletado, hover y ayuda de firma a partir del catálogo del lenguaje
// (assets/scad/catalog.json, embebido en Go; binding GetScadCatalog). Se pide
// una sola vez por sesión. Los textos salen en el idioma activo al momento de
// mostrarlos, así que cambiar de idioma no obliga a recargar nada.

type Monacoish = typeof Monaco

let entries = new Map<string, scad.CatalogEntry>()
let loading: Promise<void> | null = null

export function loadCatalog(onLoaded: (modules: string[], functions: string[]) => void): Promise<void> {
    if (!loading) {
        loading = GetScadCatalog()
            .then((c) => {
                entries = new Map((c?.entries ?? []).map((e) => [e.name, e]))
                const of = (kind: string) => [...entries.values()].filter((e) => e.kind === kind).map((e) => e.name)
                onLoaded(of('module'), of('function'))
            })
            .catch(() => {
                // Sin catálogo el editor funciona igual, solo sin ayuda.
                loading = null
            })
    }
    return loading
}

function text(l: scad.LocalizedText | undefined): string {
    if (!l) return ''
    return getLanguage() === 'es' ? l.es || l.en : l.en || l.es
}

// paramSpans ubica cada parámetro dentro de la firma («cylinder(h, r1, r2…)»)
// para que Monaco resalte el activo. Con offsets y no con el nombre: «r»
// aparecería primero dentro de «cylinder».
function paramSpans(signature: string): {name: string; span: [number, number]}[] {
    const open = signature.indexOf('(')
    const close = signature.lastIndexOf(')')
    if (open < 0 || close <= open + 1) return []
    const out: {name: string; span: [number, number]}[] = []
    let depth = 0
    let start = open + 1
    for (let i = open + 1; i <= close; i++) {
        const ch = signature[i]
        if (ch === '(' || ch === '[') depth++
        else if ((ch === ')' || ch === ']') && i < close) depth--
        if ((ch === ',' && depth === 0) || i === close) {
            const raw = signature.slice(start, i)
            const lead = raw.length - raw.trimStart().length
            const piece = raw.trim()
            if (piece && piece !== '…' && piece !== '...') {
                const s = start + lead
                out.push({name: piece.split('=')[0].trim(), span: [s, s + piece.length]})
            }
            start = i + 1
        }
    }
    return out
}

function docMarkdown(e: scad.CatalogEntry): string {
    const d = t().editor
    const parts = [text(e.doc)]
    if (e.params?.length) {
        parts.push(`**${d.params}**\n\n` + e.params.map((p) => `- \`${p.name}\` — ${text(p.doc)}`).join('\n'))
    }
    if (e.since) parts.push(`_${d.since.replace('{v}', e.since)}_`)
    return parts.filter(Boolean).join('\n\n')
}

const KEYWORD_SNIPPETS: Record<string, string> = {
    for: 'for (${1:i} = [${2:0} : ${3:1} : ${4:10}]) {\n\t$0\n}',
    intersection_for: 'intersection_for (${1:i} = [${2:0} : ${3:3}]) {\n\t$0\n}',
    if: 'if (${1:condition}) {\n\t$0\n}',
    module: 'module ${1:name}(${2}) {\n\t$0\n}',
    function: 'function ${1:name}(${2}) = ${0};',
    let: 'let (${1:x} = ${2:0}) $0',
    include: 'include <${1:file.scad}>',
    use: 'use <${1:file.scad}>',
    assert: 'assert(${1:condition}, "${2:message}");',
    echo: 'echo(${1});',
}

export function registerCatalogProviders(monaco: Monacoish, onLoaded: (modules: string[], functions: string[]) => void) {
    void loadCatalog(onLoaded)
    const {languages} = monaco
    const Kind = languages.CompletionItemKind
    const Rule = languages.CompletionItemInsertTextRule

    languages.registerCompletionItemProvider(LANGUAGE_ID, {
        triggerCharacters: ['$'],
        provideCompletionItems(model, position) {
            const word = model.getWordUntilPosition(position)
            // `$fn`: la palabra de Monaco no incluye el `$` si ya se escribió.
            const line = model.getLineContent(position.lineNumber)
            const dollar = line[word.startColumn - 2] === '$' ? 1 : 0
            const range = new monaco.Range(position.lineNumber, word.startColumn - dollar, position.lineNumber, word.endColumn)
            const suggestions: Monaco.languages.CompletionItem[] = []
            for (const e of entries.values()) {
                const doc = {value: docMarkdown(e)}
                const base = {label: e.name, detail: e.signature, documentation: doc, range}
                if (e.kind === 'keyword') {
                    const snippet = KEYWORD_SNIPPETS[e.name]
                    suggestions.push({
                        ...base,
                        kind: Kind.Keyword,
                        insertText: snippet ?? e.name,
                        insertTextRules: snippet ? Rule.InsertAsSnippet : undefined,
                    })
                } else if (e.kind === 'special') {
                    suggestions.push({...base, kind: e.name.startsWith('$') ? Kind.Variable : Kind.Constant, insertText: e.name})
                } else {
                    // Módulo o función: se abre el paréntesis con el primer
                    // parámetro como marcador y la ayuda de firma muestra el resto.
                    const first = paramSpans(e.signature)[0]?.name
                    const insert = first ? `${e.name}(\${1:${first}})$0` : `${e.name}()$0`
                    suggestions.push({
                        ...base,
                        kind: e.kind === 'module' ? Kind.Module : Kind.Function,
                        insertText: insert,
                        insertTextRules: Rule.InsertAsSnippet,
                        command: {id: 'editor.action.triggerParameterHints', title: ''},
                    })
                }
            }
            return {suggestions}
        },
    })

    languages.registerHoverProvider(LANGUAGE_ID, {
        provideHover(model, position) {
            const word = model.getWordAtPosition(position)
            if (!word) return null
            const line = model.getLineContent(position.lineNumber)
            const name = (line[word.startColumn - 2] === '$' ? '$' : '') + word.word.replace(/^\$/, '')
            const e = entries.get(name) ?? entries.get(word.word)
            if (!e) return null
            return {
                range: new monaco.Range(position.lineNumber, word.startColumn, position.lineNumber, word.endColumn),
                contents: [{value: '```openscad\n' + e.signature + '\n```'}, {value: docMarkdown(e)}],
            }
        },
    })

    languages.registerSignatureHelpProvider(LANGUAGE_ID, {
        signatureHelpTriggerCharacters: ['(', ','],
        signatureHelpRetriggerCharacters: [','],
        provideSignatureHelp(model, position) {
            const from = Math.max(1, position.lineNumber - 30)
            const before = model.getValueInRange(new monaco.Range(from, 1, position.lineNumber, position.column))
            const call = enclosingCall(before)
            if (!call) return null
            const e = entries.get(call.name)
            if (!e || (e.kind !== 'module' && e.kind !== 'function')) return null
            const spans = paramSpans(e.signature)
            const docs = new Map((e.params ?? []).map((p) => [p.name, text(p.doc)]))
            let active = call.index
            // Un argumento con nombre (`r = 3`) marca ese parámetro, no la posición.
            if (call.named) {
                const i = spans.findIndex((s) => s.name === call.named)
                if (i >= 0) active = i
            }
            return {
                value: {
                    signatures: [
                        {
                            label: e.signature,
                            documentation: {value: text(e.doc)},
                            parameters: spans.map((s) => ({label: s.span, documentation: docs.get(s.name) ?? ''})),
                        },
                    ],
                    activeSignature: 0,
                    activeParameter: Math.min(active, Math.max(0, spans.length - 1)),
                },
                dispose() {},
            }
        },
    })
}

// enclosingCall busca, hacia atrás desde el cursor, el paréntesis abierto de
// la llamada en curso: su nombre, en qué argumento está el cursor y, si ese
// argumento ya tiene `nombre =`, cuál es. Los strings se saltean a grandes
// rasgos (una coma dentro de "a, b" no cuenta).
function enclosingCall(src: string): {name: string; index: number; named: string} | null {
    let depth = 0
    let commas = 0
    let inString = false
    let argStart = src.length
    for (let i = src.length - 1; i >= 0; i--) {
        const ch = src[i]
        if (ch === '"' && src[i - 1] !== '\\') inString = !inString
        if (inString) continue
        if (ch === ')' || ch === ']' || ch === '}') depth++
        else if (ch === '[' || ch === '{') {
            if (depth === 0) return null
            depth--
        } else if (ch === '(') {
            if (depth > 0) {
                depth--
                continue
            }
            const m = /([A-Za-z_]\w*)\s*$/.exec(src.slice(0, i))
            if (!m) return null
            const named = /^\s*([A-Za-z_]\w*)\s*=(?!=)/.exec(commas === 0 ? src.slice(i + 1) : src.slice(argStart))?.[1] ?? ''
            return {name: m[1], index: commas, named}
        } else if (ch === ',' && depth === 0) {
            if (commas === 0) argStart = i + 1
            commas++
        } else if (ch === ';' && depth === 0) {
            return null
        }
    }
    return null
}
