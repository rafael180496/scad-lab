// Lo puro de la búsqueda en las conversaciones (sin React ni Wails, para poder probarlo con node).

const SNIPPET = 44

/** Fragmento del texto alrededor de la primera coincidencia, en una sola línea; '' si no hay. */
export function snippetAround(text: string, query: string): string {
    const q = query.trim()
    const i = q ? text.toLowerCase().indexOf(q.toLowerCase()) : -1
    if (i < 0) return ''
    const from = Math.max(0, i - SNIPPET)
    const to = Math.min(text.length, i + q.length + SNIPPET)
    return `${from > 0 ? '…' : ''}${text.slice(from, to).replace(/\s+/g, ' ').trim()}${to < text.length ? '…' : ''}`
}
