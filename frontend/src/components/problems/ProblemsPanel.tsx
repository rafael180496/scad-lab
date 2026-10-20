import {CircleAlert, CircleCheck, Info, Network, TriangleAlert} from 'lucide-react'
import {MakeCheckpointRoot} from '../../../wailsjs/go/main/App'
import {goToLine} from '../../lib/editorSession'
import {errorText} from '../../lib/errors'
import {navigable, type Problem, type Severity} from '../../lib/problems'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {Button} from '../ui/Button'
import {toast} from '../ui/Toast'

// SL002: el `parent=` apunta a un checkpoint que no está (en la papelera, borrado a
// mano o él mismo). El arreglo de un clic es dejarlo como principal.
const PARENT_MISSING = 'SL002'

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
export function ProblemsPanel({workspaceId, file, problems}: {workspaceId: number | null; file: string | null; problems: Problem[]}) {
    const t = useT()
    const makeRoot = async () => {
        if (workspaceId == null || !file) return
        try {
            await MakeCheckpointRoot(workspaceId, file)
            toast.ok(fmt(t.problems.madeRoot, {file}))
        } catch (e) {
            toast.error(errorText(e))
        }
    }
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
                const fixable = p.origin === 'lint' && p.code === PARENT_MISSING && workspaceId != null
                return (
                    <li key={i} className="flex items-start hover:bg-surface-2">
                        <button
                            type="button"
                            disabled={!go}
                            title={go ? fmt(t.problems.goTo, {line: p.line}) : p.file ? fmt(t.problems.inFile, {file: p.file}) : t.problems.noLine}
                            onClick={() => goToLine(p.line, p.col)}
                            className="flex min-w-0 flex-1 items-start gap-2 px-3 py-1 text-left text-ui-12 transition-colors duration-100 enabled:hover:bg-surface-2 disabled:cursor-default"
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
                        {fixable ? (
                            <Button className="mx-2 my-0.5 h-6 shrink-0 px-2 text-ui-11-5" title={t.problems.makeRootHint} onClick={() => void makeRoot()}>
                                <Network className="icon-ui-12" />
                                {t.problems.makeRoot}
                            </Button>
                        ) : null}
                    </li>
                )
            })}
        </ul>
    )
}
