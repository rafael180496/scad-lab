// Tipos del chat de Hermes que viajan por EVENTOS de Wails (chat:step,
// chat:usage) o dentro del JSON `parts` de un mensaje guardado. Se escriben a
// mano y no salen de wailsjs/go/models por lo mismo que los eventos de
// render: llegan como JSON plano, no como instancias de clase. Espejan
// backend/hermes (Step, Usage, Parts, AttachmentMeta).

export type StepStatus = 'running' | 'ok' | 'error'

export type ChatStep = {
    id: string
    /** Número del paso en el turno, desde 1. */
    index: number
    tool: string
    args: Record<string, unknown>
    status: StepStatus
    /** Resultado de la herramienta (o su error), ya recortado. */
    text: string
    /** Data URL: PNG en vivo; JPEG reducido en un mensaje guardado. */
    images: string[]
    durationMs: number
    /** Texto visible (unidades UTF-16) que llevaba el turno cuando arrancó el paso. */
    textAt: number
    /** S17: tokens de la vuelta del modelo que pidió este paso (0 = el proveedor no los informó). RoundInput es el contexto de esa vuelta. */
    roundInput: number
    roundOutput: number
    /** S17: lo que de este paso vuelve al modelo (caracteres y tokens estimados). */
    resultChars: number
    resultTokens: number
}

/**
 * Uso de un turno (S17). `inputTokens` es la SUMA de todas las vueltas (cada una reenvía el contexto
 * entero), no el contexto: ese es `peakInput`, la vuelta con más entrada. Un campo en 0 es «no se
 * informó» y no se muestra.
 */
export type ChatUsage = {
    inputTokens: number
    outputTokens: number
    costUsd: number
    modelMs: number
    toolMs: number
    rounds: number
    peakInput: number
    lastInput: number
    /** peakInput/lastInput son una estimación (el servidor no informó la entrada). */
    inputEstimated: boolean
}

/** Normaliza el uso que llega de Go (un evento, GetChat o los `parts` guardados): lo que falte es 0. */
export function toUsage(u: Partial<ChatUsage> | null | undefined): ChatUsage {
    return {
        inputTokens: u?.inputTokens ?? 0,
        outputTokens: u?.outputTokens ?? 0,
        costUsd: u?.costUsd ?? 0,
        modelMs: u?.modelMs ?? 0,
        toolMs: u?.toolMs ?? 0,
        rounds: u?.rounds ?? 0,
        peakInput: u?.peakInput ?? 0,
        lastInput: u?.lastInput ?? 0,
        inputEstimated: u?.inputEstimated ?? false,
    }
}

/**
 * Lo que dice la ventana de un turno con un servidor (evento `chat:context`, S17): su tamaño efectivo,
 * lo que pesan solos el prompt de sistema y las herramientas declaradas (viajan en CADA vuelta), el
 * perfil de herramientas y si pasa del 60 % de la ventana. `profile` vacío = sin herramientas.
 */
export type ChatContextInfo = {
    chatId: number
    numCtx: number
    promptTokens: number
    toolsTokens: number
    profile: '' | 'essential' | 'full'
    auto: boolean
    overBudget: boolean
}

/** Lo que se guarda de un adjunto de un mensaje: nunca los bytes. */
export type AttachmentMeta = {
    kind: 'image' | 'model' | string
    name: string
    mime?: string
    size: number
    refPath?: string
    summary?: string
    thumb?: string
}

export type ChatParts = {
    attachments?: AttachmentMeta[]
    steps?: ChatStep[]
    model?: string
    stepLimit?: boolean
    /** S17: tiempo y contexto del turno (el pie de la respuesta). Un mensaje anterior no lo trae. */
    usage?: ChatUsage
}

/** Lee los `parts` de un mensaje; vacío o corrupto = sin partes. */
export function parseParts(raw: string | undefined): ChatParts {
    if (!raw || !raw.trim()) return {}
    try {
        const v = JSON.parse(raw) as unknown
        if (!v || typeof v !== 'object' || Array.isArray(v)) return {}
        // Lo guardado pasa por la misma normalización que los eventos: un slice
        // nil de Go llega como null (images: null) y un paso sin normalizar
        // tumbaba la conversación entera al reabrirla.
        const p = v as ChatParts
        return {
            ...p,
            attachments: Array.isArray(p.attachments) ? p.attachments : [],
            steps: Array.isArray(p.steps) ? p.steps.map((s) => toStep(s as Parameters<typeof toStep>[0])) : [],
            usage: p.usage ? toUsage(p.usage) : undefined,
        }
    } catch {
        return {}
    }
}

/** Normaliza un paso que viene de Go (un binding de GetChat o un evento). */
export function toStep(s: {
    id: string
    index: number
    tool: string
    args?: Record<string, unknown> | null
    status: string
    text?: string
    images?: string[] | null
    durationMs?: number
    textAt?: number
    roundInput?: number
    roundOutput?: number
    resultChars?: number
    resultTokens?: number
}): ChatStep {
    return {
        id: s.id,
        index: s.index,
        tool: s.tool,
        args: s.args ?? {},
        status: s.status === 'ok' || s.status === 'error' ? s.status : 'running',
        text: s.text ?? '',
        images: s.images ?? [],
        durationMs: s.durationMs ?? 0,
        textAt: s.textAt ?? 0,
        roundInput: s.roundInput ?? 0,
        roundOutput: s.roundOutput ?? 0,
        resultChars: s.resultChars ?? 0,
        resultTokens: s.resultTokens ?? 0,
    }
}

/**
 * Parte el texto de un turno en tramos y pasos, en el orden en que ocurrieron:
 * cada paso va donde estaba el texto cuando arrancó (`textAt`). Dos pasos sin
 * texto entre medio quedan juntos. Pasos desordenados o con un `textAt` fuera de
 * rango se acotan: nunca se pierde texto ni se parte una pareja sustituta.
 */
export type TurnItem = {type: 'text'; text: string} | {type: 'steps'; steps: ChatStep[]}

export function interleave(text: string, steps: ChatStep[]): TurnItem[] {
    const items: TurnItem[] = []
    let pos = 0
    let group: ChatStep[] | null = null
    const sorted = [...steps].sort((a, b) => a.index - b.index)
    for (const s of sorted) {
        let at = Math.max(pos, Math.min(s.textAt, text.length))
        // No cortar entre las dos mitades de un emoji.
        const c = text.charCodeAt(at - 1)
        if (at > pos && at < text.length && c >= 0xd800 && c <= 0xdbff) at++
        if (at > pos) {
            items.push({type: 'text', text: text.slice(pos, at)})
            pos = at
            group = null
        }
        if (!group) {
            group = []
            items.push({type: 'steps', steps: group})
        }
        group.push(s)
    }
    if (pos < text.length) items.push({type: 'text', text: text.slice(pos)})
    return items
}
