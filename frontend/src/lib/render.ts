import type {render} from '../../wailsjs/go/models'
import {fmt, locale, type Dict} from '../i18n'

// Tono de un resultado de render: es lo que pinta el punto del árbol y la
// barra de estado. Un render OK con advertencias es ámbar, no verde: la pieza
// salió, pero algo merece una mirada (un include que no se encontró, una
// variable sin definir).
export type RenderTone = 'running' | 'idle' | 'ok' | 'warn' | 'error' | 'missing'

export function renderTone(r: render.Result | undefined, running: boolean): RenderTone {
    if (running) return 'running'
    if (!r || r.status === 'cancelled') return 'idle'
    if (r.status === 'missing') return 'missing'
    if (r.status === 'error') return 'error'
    return warnings(r).length > 0 ? 'warn' : 'ok'
}

export function errors(r: render.Result): render.Diagnostic[] {
    return (r.diagnostics ?? []).filter((d) => d.severity === 'error')
}

export function warnings(r: render.Result): render.Diagnostic[] {
    return (r.diagnostics ?? []).filter((d) => d.severity === 'warning')
}

// seconds formatea la duración con un decimal en el idioma activo.
export function seconds(ms: number): string {
    return (ms / 1000).toLocaleString(locale(), {minimumFractionDigits: 1, maximumFractionDigits: 1})
}

// summary es el texto corto del `title` de un checkpoint en el árbol.
export function summary(t: Dict, r: render.Result | undefined, running: boolean): string {
    const tone = renderTone(r, running)
    if (tone === 'running') return t.render.rendering
    if (!r || tone === 'idle') return t.render.treeIdle
    if (tone === 'missing') return t.render.treeMissing
    if (tone === 'error') {
        const first = errors(r)[0]
        if (!first) return t.render.failed
        return first.line > 0
            ? fmt(t.render.treeErrorLine, {line: first.line, msg: first.message})
            : fmt(t.render.treeError, {msg: first.message})
    }
    if (tone === 'warn') return fmt(t.render.treeWarn, {n: warnings(r).length})
    return fmt(t.render.treeOk, {s: seconds(r.durationMs)})
}
