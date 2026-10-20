// Contador de contexto del chat: cuántos tokens lleva la conversación y qué
// parte de la ventana del modelo ocupa. Es una ESTIMACIÓN (4 caracteres ≈ 1
// token, la regla de bolsillo de los tokenizadores BPE en inglés y español): no
// hay tokenizador en el cliente, y el número sirve para ver que la conversación
// se acerca al límite, no para facturar. El uso real de cada turno lo informa el
// proveedor y se muestra al pie de la respuesta.

import type {ChatUsage} from './chatTypes'

export const CHARS_PER_TOKEN = 4

/** Tokens estimados de un texto. Contar caracteres Unicode, no unidades UTF-16: un emoji pesa como uno. */
export function estimateTokens(text: string): number {
    if (!text) return 0
    let chars = 0
    for (const _ of text) chars++
    return Math.ceil(chars / CHARS_PER_TOKEN)
}

export type ContextUsage = {
    /** Tokens estimados del borrador más la conversación (más `overhead`, si se conoce). */
    used: number
    /** S17: lo fijo de cada vuelta que ya se sabe —prompt de sistema y herramientas declaradas—; 0 = no se sabe todavía. */
    overhead: number
    /** Ventana del modelo en tokens; 0 = no se sabe. */
    limit: number
    /** used / limit en [0, ∞); 0 si no hay ventana conocida. */
    ratio: number
    level: 'ok' | 'warn' | 'full'
}

/** Umbrales del aviso: ámbar desde el 70 %, rojo desde el 90 %. */
export const CONTEXT_WARN = 0.7
export const CONTEXT_FULL = 0.9

export function contextUsage(historyTokens: number, draft: string, limit: number, overhead = 0): ContextUsage {
    const used = overhead + historyTokens + estimateTokens(draft)
    const ratio = limit > 0 ? used / limit : 0
    const level = ratio >= CONTEXT_FULL ? 'full' : ratio >= CONTEXT_WARN ? 'warn' : 'ok'
    return {used, overhead, limit, ratio, level}
}

/** Lo mínimo del evento `chat:context` que decide los avisos (ver ChatContextInfo). */
export type WindowInfo = {numCtx: number; promptTokens: number; toolsTokens: number; profile: string}

/**
 * La ventana que se usa para medir el chat: la que informó el servidor en el último turno (`numCtx`, la
 * efectiva: lo que se le pide a Ollama) y, si todavía no corrió ninguno, la del modelo.
 */
export function windowLimit(info: WindowInfo | null, modelContext: number): number {
    return info && info.numCtx > 0 ? info.numCtx : modelContext
}

/** Lo fijo de cada vuelta (prompt de sistema + herramientas) según el último turno; 0 si no se sabe. */
export function fixedOverhead(info: WindowInfo | null): number {
    return info ? info.promptTokens + info.toolsTokens : 0
}

/**
 * El aviso que corresponde sobre la caja de mensaje (S17), o null:
 *
 *  - `summary`: la conversación ocupa el 70 % de la ventana o más → ofrecer «Nueva conversación con resumen».
 *    Pesa más que el otro: ya no hay sitio para seguir en esta.
 *  - `over`: el prompt y las herramientas solos pasan del 60 % de la ventana → decirlo, con el atajo a las
 *    herramientas esenciales si todavía no se usan.
 *
 * Solo con un servidor (un CLI no informa la ventana) y con ventana conocida.
 */
export type ContextAdvice =
    | {kind: 'summary'; pct: number}
    | {kind: 'over'; pct: number; used: number; limit: number; canNarrow: boolean}

export const OVER_BUDGET = 0.6

export function contextAdvice(info: WindowInfo | null, usage: ContextUsage, server: boolean, hasMessages: boolean): ContextAdvice | null {
    if (!server || usage.limit <= 0) return null
    if (hasMessages && usage.ratio >= CONTEXT_WARN) return {kind: 'summary', pct: Math.round(usage.ratio * 100)}
    const fixed = fixedOverhead(info)
    if (info && fixed > OVER_BUDGET * usage.limit) {
        return {kind: 'over', pct: Math.round((fixed / usage.limit) * 100), used: fixed, limit: usage.limit, canNarrow: info.profile !== 'essential'}
    }
    return null
}

/** 1234 → «1,2k», 1 500 000 → «1,5M». Mismo estilo que las cifras de tokens del chat. */
export function compactTokens(n: number, locale: string): string {
    if (n < 1000) return String(n)
    if (n < 1_000_000) return `${(n / 1000).toLocaleString(locale, {maximumFractionDigits: n < 10_000 ? 1 : 0})}k`
    return `${(n / 1_000_000).toLocaleString(locale, {maximumFractionDigits: 1})}M`
}

// El pie de un turno (S17): «Contexto máx. 12k · ↑ 48k en 5 vueltas · ↓ 2k · modelo 41 s · herramientas 6 s».
//
// Por qué no es solo «↑ 48k»: cada vuelta del modelo reenvía el contexto entero, así que ↑ es la SUMA de la
// entrada de todas las vueltas. Un turno de 5 vueltas con 10k de contexto suma ~50k, y leído como si fuera
// el contexto parece que el modelo ya se pasó de su ventana. El contexto de verdad es la vuelta con más
// entrada (`peakInput`), y es lo que se compara con la ventana del modelo. Lo que el proveedor no informó
// (0) no se muestra: un número inventado es peor que ninguno.

/** Lo que el pie necesita del diccionario (`t.chat.usage`). */
export type UsageDict = {
    context: string
    contextEstimated: string
    input: string
    inputRounds: string
    output: string
    model: string
    tools: string
}

const fill = (tpl: string, vars: Record<string, string | number>): string => tpl.replace(/\{(\w+)\}/g, (_, k: string) => String(vars[k] ?? `{${k}}`))

/** 850 → «850 ms», 41 200 → «41 s», 5 400 → «5,4 s», 125 000 → «2 min 5 s». */
export function formatDuration(ms: number, locale: string): string {
    if (!(ms > 0)) return ''
    if (ms < 1000) return `${Math.round(ms)} ms`
    if (ms < 60_000) return `${(ms / 1000).toLocaleString(locale, {maximumFractionDigits: ms < 10_000 ? 1 : 0})} s`
    const total = Math.round(ms / 1000)
    return `${Math.floor(total / 60)} min ${total % 60} s`
}

/** Las piezas del pie, en orden y solo las que se informaron. */
export function usagePieces(u: ChatUsage, d: UsageDict, locale: string): string[] {
    const out: string[] = []
    if (u.peakInput > 0) out.push(fill(u.inputEstimated ? d.contextEstimated : d.context, {n: compactTokens(u.peakInput, locale)}))
    if (u.inputTokens > 0) {
        out.push(fill(u.rounds > 1 ? d.inputRounds : d.input, {input: compactTokens(u.inputTokens, locale), rounds: u.rounds}))
    }
    if (u.outputTokens > 0) out.push(fill(d.output, {output: compactTokens(u.outputTokens, locale)}))
    if (u.modelMs > 0) out.push(fill(d.model, {t: formatDuration(u.modelMs, locale)}))
    if (u.toolMs > 0) out.push(fill(d.tools, {t: formatDuration(u.toolMs, locale)}))
    return out
}

/** ¿El turno informó algo que valga un pie? */
export function hasUsage(u: ChatUsage | null | undefined): u is ChatUsage {
    return !!u && (u.inputTokens > 0 || u.outputTokens > 0 || u.peakInput > 0 || u.modelMs > 0)
}
