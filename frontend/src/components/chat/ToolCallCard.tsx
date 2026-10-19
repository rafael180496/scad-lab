import {useState, type ReactNode} from 'react'
import {
    AlignLeft,
    BookOpen,
    Check,
    ChevronDown,
    CircleAlert,
    Eye,
    FilePlus2,
    FileText,
    GitBranch,
    Hammer,
    LayoutTemplate,
    Loader2,
    PackageOpen,
    Printer,
    Ruler,
    ScanSearch,
    ShieldCheck,
    Wrench,
    type LucideIcon,
} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import type {ChatStep} from '../../lib/chatTypes'
import {Dialog} from '../ui/Dialog'

// Tarjeta de un paso de Hermes: qué herramienta llamó, con qué argumentos y qué
// devolvió. Plegada muestra una línea (herramienta, resumen, estado y duración);
// desplegada, los argumentos y el resultado. Si la herramienta devolvió una
// imagen (el `preview`), se ve siempre en miniatura y se amplía con un clic.
//
// Es el ToolCallCard de mini-tools con lucide en vez de Material Symbols y con
// la imagen de la herramienta, que allá no existe.

const ICONS: Record<string, LucideIcon> = {
    list_checkpoints: GitBranch,
    read_checkpoint: FileText,
    create_checkpoint: FilePlus2,
    render: Hammer,
    lint: ShieldCheck,
    format: AlignLeft,
    measure: Ruler,
    preview: Eye,
    printer_info: Printer,
    export: PackageOpen,
    list_templates: LayoutTemplate,
    get_template: LayoutTemplate,
    list_skills: BookOpen,
    read_skill: BookOpen,
    inspect_reference: ScanSearch,
}

// Las que escriben algo en el proyecto (un checkpoint nuevo, un export): van en el
// color de acento, para que de un vistazo la secuencia distinga lo que miró de
// lo que creó.
const WRITES = new Set(['create_checkpoint', 'export'])

// Claves de los argumentos que mejor resumen la llamada, en orden de preferencia.
const SUMMARY_KEYS = ['label', 'file', 'name', 'view', 'format', 'template', 'id']

function summaryOf(step: ChatStep): string {
    for (const k of SUMMARY_KEYS) {
        const v = step.args[k]
        if (typeof v === 'string' && v.trim()) return v.trim()
    }
    return ''
}

const isLong = (v: unknown) => typeof v === 'string' && (v.length > 80 || v.includes('\n'))

// El resultado listo para leer: sin el «error: » con que Go marca un fallo (el
// rótulo de la tarjeta ya lo dice) y con el JSON sangrado, que es como lo
// devuelven casi todas las herramientas.
function prettyResult(text: string): string {
    const t = text.trim().replace(/^error:\s*/, '')
    if (t.startsWith('{') || t.startsWith('[')) {
        try {
            const out = JSON.stringify(JSON.parse(t), null, 2)
            if (out.length <= 6000) return out
        } catch {
            // no era JSON: se muestra tal cual
        }
    }
    return t
}

type Diagnostic = {severity?: string; line?: number; col?: number; message?: string; code?: string}

// Los diagnósticos de un render que falló: `render` devuelve un JSON con la línea y
// el código ofensivo, y es lo primero que hay que leer. Null si el resultado no
// trae ninguno (otra herramienta, o un error que no es de OpenSCAD).
function parseDiagnostics(text: string): {diags: Diagnostic[]; message: string} | null {
    try {
        const v = JSON.parse(text.trim().replace(/^error:\s*/, '')) as {diagnostics?: unknown; message?: unknown}
        if (!Array.isArray(v.diagnostics) || v.diagnostics.length === 0) return null
        const diags = (v.diagnostics as Diagnostic[]).filter((d) => d && typeof d === 'object').slice(0, 6)
        return diags.length ? {diags, message: typeof v.message === 'string' ? v.message : ''} : null
    } catch {
        return null
    }
}

function formatDuration(ms: number): string {
    if (ms <= 0) return ''
    return ms < 1000 ? `${Math.round(ms)} ms` : `${(ms / 1000).toFixed(1)} s`
}

function Pre({children, className}: {children: ReactNode; className?: string}) {
    return (
        <pre
            className={cn(
                'selectable max-h-40 overflow-auto rounded border border-line bg-bg px-2 py-1.5 font-mono text-ui-11 leading-relaxed whitespace-pre-wrap break-words text-fg',
                className,
            )}
        >
            {children}
        </pre>
    )
}

function ArgsView({args}: {args: Record<string, unknown>}) {
    const entries = Object.entries(args)
    if (entries.length === 0) return null
    return (
        <dl className="flex flex-col gap-1.5">
            {entries.map(([k, v]) => (
                <div key={k} className="min-w-0">
                    <dt className="font-mono text-ui-10 tracking-wider text-faint uppercase">{k}</dt>
                    <dd className="min-w-0">
                        {isLong(v) ? (
                            <Pre>{String(v)}</Pre>
                        ) : (
                            <span className="selectable font-mono text-ui-11 break-words text-fg">{typeof v === 'string' ? v : JSON.stringify(v)}</span>
                        )}
                    </dd>
                </div>
            ))}
        </dl>
    )
}

export function ToolCallCard({step}: {step: ChatStep}) {
    const t = useT()
    const c = t.chat.steps
    // Un paso fallido arranca abierto (el error es lo que hay que leer), pero el estado final llega
    // DESPUÉS de que la tarjeta se montó «en curso»: se decide al mostrarla, no al montarla.
    const [userOpen, setUserOpen] = useState<boolean | null>(null)
    const open = userOpen ?? step.status === 'error'
    const [zoom, setZoom] = useState<string | null>(null)
    const [raw, setRaw] = useState(false)
    const Icon = ICONS[step.tool] ?? Wrench
    const known = (c.tools as Record<string, string>)[step.tool]
    const label = known ?? step.tool
    const summary = summaryOf(step)
    const dur = formatDuration(step.durationMs)
    const diag = step.status === 'error' ? parseDiagnostics(step.text) : null
    const status = c.status[step.status]
    return (
        <div
            data-step={step.status}
            data-tool={step.tool}
            className={cn('my-1.5 overflow-hidden rounded-lg border bg-surface', step.status === 'error' ? 'border-danger/50' : 'border-line')}
        >
            <button
                type="button"
                title={fmt(c.cardHint, {n: step.index, tool: label, status})}
                aria-expanded={open}
                onClick={() => setUserOpen(!open)}
                className="no-drag flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-surface-2"
            >
                <span className="font-mono text-ui-10 text-faint tabular-nums">{step.index}</span>
                <Icon className={cn('icon-ui-14 shrink-0', WRITES.has(step.tool) ? 'text-accent' : 'text-muted')} />
                <span className="shrink-0 text-ui-12 font-medium text-fg">{label}</span>
                {summary ? <span className="min-w-0 flex-1 truncate font-mono text-ui-11 text-muted">{summary}</span> : <span className="flex-1" />}
                {dur ? <span className="shrink-0 font-mono text-ui-10-5 text-faint tabular-nums">{dur}</span> : null}
                <span
                    title={status}
                    className={cn(
                        'flex shrink-0 items-center gap-1 text-ui-11',
                        step.status === 'ok' && 'text-ok',
                        step.status === 'error' && 'text-danger',
                        step.status === 'running' && 'text-accent',
                    )}
                >
                    {step.status === 'running' ? <Loader2 className="icon-ui-14 animate-spin" /> : null}
                    {step.status === 'ok' ? <Check className="icon-ui-14" /> : null}
                    {step.status === 'error' ? <CircleAlert className="icon-ui-14" /> : null}
                    <span className="sr-only">{status}</span>
                </span>
                <ChevronDown className={cn('icon-ui-14 shrink-0 text-faint transition-transform duration-150', open && 'rotate-180')} />
            </button>

            {step.images.length > 0 ? (
                <div className="flex flex-wrap gap-1.5 border-t border-line px-2.5 py-1.5">
                    {step.images.map((src, i) => (
                        <button
                            key={i}
                            type="button"
                            title={c.zoomImage}
                            onClick={() => setZoom(src)}
                            className="no-drag overflow-hidden rounded border border-line bg-surface-2 transition-colors hover:border-accent"
                        >
                            <img src={src} alt={fmt(c.imageAlt, {tool: label})} draggable={false} className="block h-24 w-auto" />
                        </button>
                    ))}
                </div>
            ) : null}

            {open ? (
                <div className="flex flex-col gap-2 border-t border-line px-2.5 py-2">
                    <ArgsView args={step.args} />
                    <div>
                        <p className="mb-0.5 font-mono text-ui-10 tracking-wider text-faint uppercase">{step.status === 'error' ? c.error : c.result}</p>
                        {diag ? (
                            <>
                                <ul className="flex flex-col gap-1">
                                    {diag.diags.map((d, i) => (
                                        <li key={i} className="rounded border border-danger/30 bg-danger/10 px-2 py-1">
                                            <p className="flex flex-wrap items-baseline gap-x-2 text-ui-11-5">
                                                {d.line ? <span className="font-mono font-medium text-danger">{fmt(c.line, {n: d.line})}</span> : null}
                                                <span className="selectable text-fg">{d.message}</span>
                                            </p>
                                            {d.code ? <p className="selectable mt-0.5 truncate font-mono text-ui-11 text-muted">{d.code}</p> : null}
                                        </li>
                                    ))}
                                </ul>
                                {diag.message ? <p className="selectable mt-1 text-ui-11-5 text-muted">{diag.message}</p> : null}
                                <button
                                    type="button"
                                    title={raw ? c.hideRawHint : c.showRawHint}
                                    onClick={() => setRaw((r) => !r)}
                                    className="no-drag mt-1 text-ui-11 text-faint underline decoration-line-strong underline-offset-2 hover:text-fg"
                                >
                                    {raw ? c.hideRaw : c.showRaw}
                                </button>
                                {raw ? <Pre className="mt-1 border-danger/40">{prettyResult(step.text)}</Pre> : null}
                            </>
                        ) : step.text.trim() ? (
                            <Pre className={step.status === 'error' ? 'border-danger/40' : undefined}>{prettyResult(step.text)}</Pre>
                        ) : (
                            <p className="text-ui-11-5 text-faint">{step.status === 'running' ? c.waiting : c.noResult}</p>
                        )}
                    </div>
                </div>
            ) : null}

            <Dialog open={zoom != null} onClose={() => setZoom(null)} title={label} closeLabel={c.closeImage} className="w-[760px]">
                {zoom ? (
                    <div className="flex items-center justify-center bg-surface-2 p-2">
                        <img src={zoom} alt={fmt(c.imageAlt, {tool: label})} className="max-h-[70vh] w-auto max-w-full" />
                    </div>
                ) : null}
            </Dialog>
        </div>
    )
}
