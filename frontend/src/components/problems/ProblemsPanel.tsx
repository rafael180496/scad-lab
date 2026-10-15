import {CircleAlert, CircleCheck, Info, TriangleAlert} from 'lucide-react'
import {goToLine} from '../../lib/editorSession'
import {navigable, type Problem, type Severity} from '../../lib/problems'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'

const tone: Record<Severity, string> = {error: 'text-danger', warning: 'text-warn', info: 'text-info'}

export function SeverityIcon({severity, className}: {severity: Severity; className?: string}) {
    const t = useT()
    const cls = cn('icon-ui-14 shrink-0', tone[severity], className)
    const label = t.problems.severity[severity]
    if (severity === 'error') return <CircleAlert className={cls} aria-label={label} />
    if (severity === 'warning') return <TriangleAlert className={cls} aria-label={label} />
    return <Info className={cls} aria-label={label} />
}

// ProblemsPanel lista lo que encontraron el linter y el último render en el
// checkpoint abierto, errores primero y por línea. Clic = ir a esa línea.
export function ProblemsPanel({file, problems}: {file: string | null; problems: Problem[]}) {
    const t = useT()
    if (!file) {
        return <p className="px-4 py-3 text-ui-12 text-faint">{t.problems.noFile}</p>
    }
    if (problems.length === 0) {
        return (
            <p className="flex items-center gap-2 px-4 py-3 text-ui-12 text-muted">
                <CircleCheck className="icon-ui-14 text-ok" />
                {fmt(t.problems.empty, {file})}
            </p>
        )
    }
    return (
        <ul className="h-full overflow-auto py-1" role="list">
            {problems.map((p, i) => {
                const go = navigable(p)
                const where = p.line > 0 ? fmt(t.problems.position, {line: p.line, col: Math.max(1, p.col)}) : t.problems.noLine
                return (
                    <li key={i}>
                        <button
                            type="button"
                            disabled={!go}
                            title={go ? fmt(t.problems.goTo, {line: p.line}) : p.file ? fmt(t.problems.inFile, {file: p.file}) : t.problems.noLine}
                            onClick={() => goToLine(p.line, p.col)}
                            className="flex w-full items-start gap-2 px-3 py-1 text-left text-ui-12 transition-colors duration-100 enabled:hover:bg-surface-2 disabled:cursor-default"
                        >
                            <SeverityIcon severity={p.severity} className="mt-px" />
                            <span className="selectable min-w-0 flex-1 leading-snug break-words text-fg">
                                {p.file ? <span className="font-mono text-muted">{p.file}: </span> : null}
                                {p.message}
                            </span>
                            {p.code ? (
                                <span className="shrink-0 rounded border border-line px-1 font-mono text-ui-10 text-muted" title={t.problems.codeHint}>
                                    {p.code}
                                </span>
                            ) : null}
                            <span
                                className="w-[54px] shrink-0 text-right font-mono text-ui-10 tracking-[0.08em] text-faint uppercase"
                                title={p.origin === 'render' ? t.problems.renderHint : t.problems.lintHint}
                            >
                                {p.origin === 'render' ? t.problems.render : t.problems.lint}
                            </span>
                            <span className="w-[86px] shrink-0 text-right font-mono text-ui-11 text-muted">{where}</span>
                        </button>
                    </li>
                )
            })}
        </ul>
    )
}
