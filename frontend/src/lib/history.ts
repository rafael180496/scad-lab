// Lógica pura del historial de un checkpoint (S16): qué se muestra, cómo se rotula una versión y qué
// hace la interfaz cuando el archivo que mira cambió en el disco. Sin React ni bindings: la prueba
// `pnpm test` la importa directo (scripts/history.test.mjs).

/** Lo que de una versión del historial (checkpoint.Version) usa la interfaz. `savedAt` llega como texto RFC 3339. */
export type HistoryVersion = {
    id: string
    savedAt: unknown
    reason: string
    tool: string
    note: string
    size: number
}

export type ReasonKind = 'ai' | 'restore' | 'other'

export function reasonKind(reason: string): ReasonKind {
    return reason === 'ai' ? 'ai' : reason === 'restore' ? 'restore' : 'other'
}

/** Fecha de una versión; null si el backend mandó algo que no se entiende. */
export function versionDate(v: Pick<HistoryVersion, 'savedAt'>): Date | null {
    const raw = v.savedAt
    const d = raw instanceof Date ? raw : new Date(typeof raw === 'string' || typeof raw === 'number' ? raw : NaN)
    return Number.isNaN(d.getTime()) ? null : d
}

/** «hace 3 minutos» / «3 minutes ago». Más de una semana: la fecha. */
export function relativeTime(when: Date, now: Date, locale: string): string {
    const sec = Math.round((when.getTime() - now.getTime()) / 1000)
    const abs = Math.abs(sec)
    const rtf = new Intl.RelativeTimeFormat(locale, {numeric: 'auto'})
    if (abs < 45) return rtf.format(0, 'second')
    if (abs < 45 * 60) return rtf.format(Math.round(sec / 60), 'minute')
    if (abs < 22 * 3600) return rtf.format(Math.round(sec / 3600), 'hour')
    if (abs < 7 * 86400) return rtf.format(Math.round(sec / 86400), 'day')
    return absoluteTime(when, locale)
}

/** Fecha y hora cortas en el idioma activo. */
export function absoluteTime(when: Date, locale: string): string {
    return new Intl.DateTimeFormat(locale, {dateStyle: 'medium', timeStyle: 'short'}).format(when)
}

/**
 * La versión que «Deshacer cambio de la IA» restaura: la más nueva, y solo si la guardó la IA. Después de
 * restaurar, la más nueva es la «restore» (la que se acaba de dejar), así que no hay nada más que deshacer
 * como cambio de la IA: restaurarla de nuevo es «rehacer» y se hace desde el historial.
 */
export function undoVersion<V extends Pick<HistoryVersion, 'reason'>>(versions: readonly V[] | null | undefined): V | null {
    const newest = versions?.[0]
    return newest && reasonKind(newest.reason) === 'ai' ? newest : null
}

/** La versión con la que «Ver diferencias» compara el archivo actual: la más nueva guardada. */
export function previousVersion<V>(versions: readonly V[] | null | undefined): V | null {
    return versions && versions.length > 0 ? versions[0] : null
}

/** Qué hace la interfaz cuando `checkpoint:updated` dice que `file` cambió en el disco. */
export type UpdatePlan =
    /** No es el archivo que se ve: solo se olvida su render (se vuelve a pedir al elegirlo). */
    | 'invalidate'
    /** Es el que se ve y el editor no tiene cambios: releer el código y volver a renderizar. */
    | 'reload'
    /** Es el que se ve pero el editor tiene cambios sin guardar: aviso no bloqueante, el borrador manda. */
    | 'warn'

export function planUpdate(args: {selected: string | null; file: string; dirty: boolean}): UpdatePlan {
    if (args.selected !== args.file) return 'invalidate'
    return args.dirty ? 'warn' : 'reload'
}

/** Rótulo de una versión anterior para el visor de diferencias: «cuándo · motivo · nota». */
export function versionLabel(parts: {when: string; reason: string; note: string}): string {
    return [parts.when, parts.reason, parts.note].filter((p) => p.trim() !== '').join(' · ')
}
