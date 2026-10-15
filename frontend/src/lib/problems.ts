import type {render, scad} from '../../wailsjs/go/models'

// Problemas del checkpoint abierto: lo que dice el linter (backend/scad,
// mientras se lee o se edita) más los diagnósticos del último render. Es lo
// que muestran los marcadores del editor, el panel Problemas y la barra de
// estado.
export type Severity = 'error' | 'warning' | 'info'

export type Problem = {
    severity: Severity
    line: number // 1-based; 0 = sin ubicación
    col: number
    endLine: number // 0 = hasta el final de la línea
    endCol: number
    code: string // "syntax", "SL003"… ("" en los del render)
    origin: 'lint' | 'render'
    message: string
    file: string // "" = el checkpoint; si no, el include donde ocurrió
}

const rank: Record<Severity, number> = {error: 0, warning: 1, info: 2}

function severity(s: string): Severity {
    return s === 'error' || s === 'warning' ? s : 'info'
}

export function fromLint(d: scad.Diagnostic): Problem {
    return {
        severity: severity(d.severity),
        line: d.line,
        col: d.col,
        endLine: d.endLine,
        endCol: d.endCol,
        code: d.code,
        origin: 'lint',
        message: sentence(d.message),
        file: '',
    }
}

export function fromRender(d: render.Diagnostic): Problem {
    return {
        severity: severity(d.severity),
        line: d.line,
        col: d.col,
        endLine: 0,
        endCol: 0,
        code: '',
        origin: 'render',
        message: sentence(d.message),
        file: d.file ?? '',
    }
}

// mergeProblems junta las dos fuentes. Un error de sintaxis lo informan las
// dos (el linter corre el mismo parser de OpenSCAD): si el linter ya marcó esa
// línea con la misma severidad, el del render sobra.
export function mergeProblems(lint: scad.Diagnostic[], renderDiags: render.Diagnostic[] | undefined): Problem[] {
    const out = lint.map(fromLint)
    const seen = new Set(out.filter((p) => p.code === 'syntax').map((p) => `${p.line}:${p.severity}`))
    for (const d of renderDiags ?? []) {
        const p = fromRender(d)
        if (!p.file && seen.has(`${p.line}:${p.severity}`)) continue
        out.push(p)
    }
    return sortProblems(out)
}

export function sortProblems(list: Problem[]): Problem[] {
    return [...list].sort((a, b) => rank[a.severity] - rank[b.severity] || a.line - b.line || a.col - b.col)
}

// navigable: el problema está en el checkpoint abierto y tiene línea.
export function navigable(p: Problem): boolean {
    return p.line > 0 && !p.file
}

export function counts(list: Problem[]): Record<Severity, number> {
    const c = {error: 0, warning: 0, info: 0}
    for (const p of list) c[p.severity]++
    return c
}

// sentence pone en mayúscula la primera letra: los mensajes del linter vienen
// de Go, donde un error empieza en minúscula por convención, y en una lista
// para el usuario eso se lee como un texto cortado.
function sentence(msg: string): string {
    return msg ? msg.charAt(0).toLocaleUpperCase() + msg.slice(1) : msg
}
