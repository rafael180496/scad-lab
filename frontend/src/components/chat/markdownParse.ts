// Parser de markdown mínimo para las respuestas del chat. Propio y no una
// librería a propósito: una respuesta de modelo es entrada no confiable y acá
// nunca se arma HTML (el renderer produce elementos de React, que escapan
// todo), no hay dependencia que auditar ni peso que sumar al binario, y el
// subconjunto que los modelos usan para esto cabe en pocas líneas.
//
// Soporta: bloques de código con cerco (``` y ~~~), títulos, listas (con
// sangría y casillas [ ] / [x]), citas, tablas (con alineación de columna),
// separadores y párrafos; y en línea: `código`, **negrita**, *cursiva*,
// ~~tachado~~, [enlaces](https://…) y direcciones http(s) sueltas. Lo demás se
// muestra como texto, que es lo seguro.

export type Block =
    | {type: 'code'; raw: string; lang: string; code: string; closed: boolean}
    | {type: 'heading'; raw: string; level: number; text: string}
    | {type: 'paragraph'; raw: string; text: string}
    | {type: 'list'; raw: string; items: ListItem[]}
    | {type: 'quote'; raw: string; text: string}
    | {type: 'table'; raw: string; head: string[]; rows: string[][]; align: Align[]}
    | {type: 'hr'; raw: string}

export type ListItem = {depth: number; marker: string; text: string; task?: 'open' | 'done'}
export type Align = 'left' | 'center' | 'right' | null

const FENCE = /^ {0,3}(`{3,}|~{3,})\s*([^\s`]*)[^`]*$/
const HEADING = /^ {0,3}(#{1,6})\s+(.*?)\s*#*\s*$/
const HR = /^ {0,3}([-*_])(\s*\1){2,}\s*$/
const LIST_ITEM = /^(\s*)([-*+]|\d{1,3}[.)])\s+(.*)$/
const QUOTE = /^ {0,3}>\s?(.*)$/
const TABLE_SEP = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$/

// La alineación de cada columna sale de la fila de guiones: «:--» izquierda, «:-:» centro, «--:» derecha.
const alignOf = (sep: string): Align[] =>
    sep
        .trim()
        .replace(/^\||\|$/g, '')
        .split('|')
        .map((c) => {
            const t = c.trim()
            return t.startsWith(':') && t.endsWith(':') ? 'center' : t.endsWith(':') ? 'right' : t.startsWith(':') ? 'left' : null
        })

const cells = (line: string) =>
    line
        .trim()
        .replace(/^\||\|$/g, '')
        .split('|')
        .map((c) => c.trim())

function startsBlock(line: string, next: string | undefined): boolean {
    return (
        FENCE.test(line) ||
        HEADING.test(line) ||
        HR.test(line) ||
        LIST_ITEM.test(line) ||
        QUOTE.test(line) ||
        (line.includes('|') && next !== undefined && TABLE_SEP.test(next))
    )
}

export function parseMarkdown(src: string): Block[] {
    const lines = src.replace(/\r\n?/g, '\n').split('\n')
    const out: Block[] = []
    let i = 0
    while (i < lines.length) {
        const line = lines[i]
        if (line.trim() === '') {
            i++
            continue
        }

        const fence = line.match(FENCE)
        if (fence) {
            const mark = fence[1]
            const body: string[] = []
            const start = i
            i++
            let closed = false
            while (i < lines.length) {
                const l = lines[i]
                // Cierra con el mismo carácter y al menos el mismo largo.
                if (new RegExp(`^ {0,3}\\${mark[0]}{${mark.length},}\\s*$`).test(l)) {
                    closed = true
                    i++
                    break
                }
                body.push(l)
                i++
            }
            out.push({type: 'code', raw: lines.slice(start, i).join('\n'), lang: fence[2].toLowerCase(), code: body.join('\n'), closed})
            continue
        }

        const h = line.match(HEADING)
        if (h) {
            out.push({type: 'heading', raw: line, level: h[1].length, text: h[2]})
            i++
            continue
        }
        if (HR.test(line)) {
            out.push({type: 'hr', raw: line})
            i++
            continue
        }

        if (line.includes('|') && i + 1 < lines.length && TABLE_SEP.test(lines[i + 1])) {
            const start = i
            const head = cells(line)
            const align = alignOf(lines[i + 1])
            i += 2
            const rows: string[][] = []
            while (i < lines.length && lines[i].trim() !== '' && lines[i].includes('|')) {
                rows.push(cells(lines[i]))
                i++
            }
            out.push({type: 'table', raw: lines.slice(start, i).join('\n'), head, rows, align})
            continue
        }

        if (QUOTE.test(line)) {
            const start = i
            const body: string[] = []
            while (i < lines.length && QUOTE.test(lines[i])) {
                body.push((lines[i].match(QUOTE) as RegExpMatchArray)[1])
                i++
            }
            out.push({type: 'quote', raw: lines.slice(start, i).join('\n'), text: body.join('\n')})
            continue
        }

        if (LIST_ITEM.test(line)) {
            const start = i
            const items: ListItem[] = []
            while (i < lines.length) {
                const m = lines[i].match(LIST_ITEM)
                if (m) {
                    const task = m[3].match(/^\[( |x|X)\]\s+(.*)$/)
                    items.push({
                        depth: Math.min(3, Math.floor(m[1].replace(/\t/g, '    ').length / 2)),
                        marker: /\d/.test(m[2]) ? m[2] : '•',
                        text: task ? task[2] : m[3],
                        ...(task ? {task: task[1] === ' ' ? ('open' as const) : ('done' as const)} : {}),
                    })
                    i++
                } else if (lines[i].trim() !== '' && /^\s+\S/.test(lines[i]) && items.length > 0) {
                    items[items.length - 1].text += '\n' + lines[i].trim() // continuación del ítem
                    i++
                } else break
            }
            out.push({type: 'list', raw: lines.slice(start, i).join('\n'), items})
            continue
        }

        const start = i
        const body: string[] = []
        while (i < lines.length && lines[i].trim() !== '' && (i === start || !startsBlock(lines[i], lines[i + 1]))) {
            body.push(lines[i])
            i++
        }
        out.push({type: 'paragraph', raw: lines.slice(start, i).join('\n'), text: body.join('\n')})
    }
    return out
}

// ── En línea ────────────────────────────────────────────────────────────────

export type Inline =
    | {type: 'text'; text: string}
    | {type: 'code'; text: string}
    | {type: 'strong'; children: Inline[]}
    | {type: 'em'; children: Inline[]}
    | {type: 'del'; children: Inline[]}
    | {type: 'link'; href: string; children: Inline[]}
    | {type: 'br'}

// Orden de alternativas = prioridad: el código gana sobre el resto (dentro de
// `…` nada se interpreta).
// Los dos últimos grupos son el tachado y la dirección suelta (sin el punto, la coma o el
// paréntesis con que suele cerrar una frase).
const INLINE_SRC = /(`+)([\s\S]*?[^`])\1(?!`)|\*\*([^\n]+?)\*\*|\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)|(?<![\w*])\*([^\s*][^*\n]*?)\*(?![\w*])|(?<![\w_])_([^\s_][^_\n]*?)_(?![\w_])|~~([^\n]+?)~~|(https?:\/\/[^\s<>]*[^\s<>.,;:!?)\]'"])/

export function parseInline(src: string): Inline[] {
    const out: Inline[] = []
    const pushText = (s: string) => {
        if (!s) return
        const parts = s.split('\n')
        parts.forEach((p, i) => {
            if (i > 0) out.push({type: 'br'})
            if (p) out.push({type: 'text', text: p})
        })
    }
    let last = 0
    // Una regex nueva por llamada: es global (lleva lastIndex) y esta función se
    // llama a sí misma para negrita/cursiva/enlaces; compartirla la reiniciaría
    // a mitad del recorrido del nivel de afuera.
    const re = new RegExp(INLINE_SRC.source, 'g')
    let m: RegExpExecArray | null
    while ((m = re.exec(src))) {
        pushText(src.slice(last, m.index))
        if (m[2] !== undefined) out.push({type: 'code', text: m[2].trim()})
        else if (m[3] !== undefined) out.push({type: 'strong', children: parseInline(m[3])})
        else if (m[4] !== undefined) out.push({type: 'link', href: m[5], children: /https?:\/\//.test(m[4]) ? [{type: 'text', text: m[4]}] : parseInline(m[4])})
        else if (m[8] !== undefined) out.push({type: 'del', children: parseInline(m[8])})
        else if (m[9] !== undefined) out.push({type: 'link', href: m[9], children: [{type: 'text', text: m[9]}]})
        else out.push({type: 'em', children: parseInline(m[6] ?? m[7])})
        last = m.index + m[0].length
    }
    pushText(src.slice(last))
    return out
}
