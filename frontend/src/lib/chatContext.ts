// Contador de contexto del chat: cuántos tokens lleva la conversación y qué
// parte de la ventana del modelo ocupa. Es una ESTIMACIÓN (4 caracteres ≈ 1
// token, la regla de bolsillo de los tokenizadores BPE en inglés y español): no
// hay tokenizador en el cliente, y el número sirve para ver que la conversación
// se acerca al límite, no para facturar. El uso real de cada turno lo informa el
// proveedor y se muestra al pie de la respuesta.

export const CHARS_PER_TOKEN = 4

/** Tokens estimados de un texto. Contar caracteres Unicode, no unidades UTF-16: un emoji pesa como uno. */
export function estimateTokens(text: string): number {
    if (!text) return 0
    let chars = 0
    for (const _ of text) chars++
    return Math.ceil(chars / CHARS_PER_TOKEN)
}

export type ContextUsage = {
    /** Tokens estimados del borrador más la conversación. */
    used: number
    /** Ventana del modelo en tokens; 0 = no se sabe. */
    limit: number
    /** used / limit en [0, ∞); 0 si no hay ventana conocida. */
    ratio: number
    level: 'ok' | 'warn' | 'full'
}

/** Umbrales del aviso: ámbar desde el 70 %, rojo desde el 90 %. */
export const CONTEXT_WARN = 0.7
export const CONTEXT_FULL = 0.9

export function contextUsage(historyTokens: number, draft: string, limit: number): ContextUsage {
    const used = historyTokens + estimateTokens(draft)
    const ratio = limit > 0 ? used / limit : 0
    const level = ratio >= CONTEXT_FULL ? 'full' : ratio >= CONTEXT_WARN ? 'warn' : 'ok'
    return {used, limit, ratio, level}
}

/** 1234 → «1,2k», 1 500 000 → «1,5M». Mismo estilo que las cifras de tokens del chat. */
export function compactTokens(n: number, locale: string): string {
    if (n < 1000) return String(n)
    if (n < 1_000_000) return `${(n / 1000).toLocaleString(locale, {maximumFractionDigits: n < 10_000 ? 1 : 0})}k`
    return `${(n / 1_000_000).toLocaleString(locale, {maximumFractionDigits: 1})}M`
}
