import {useState, type ReactNode} from 'react'
import {
    AlignLeft,
    ArrowUpRight,
    BookOpen,
    Check,
    ChevronDown,
    CircleAlert,
    Eye,
    FilePlus2,
    FileText,
    FileUp,
    GitBranch,
    Hammer,
    LayoutTemplate,
    Loader2,
    PackageOpen,
    Printer,
    Ruler,
    ScanSearch,
    ShieldCheck,
    TriangleAlert,
    Wrench,
    type LucideIcon,
} from 'lucide-react'
import {fmt, locale, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import type {ChatStep} from '../../lib/chatTypes'
import {Dialog} from '../ui/Dialog'

// Tarjeta de un paso: qué herramienta llamó el modelo (o el CLI), con qué
// argumentos y qué devolvió. Es la MISMA tarjeta para Hermes y para los CLIs: un CLI
// con el MCP de ScadLab conectado llega por el mismo evento (chat:step).
//
// Plegada es una línea (herramienta, resumen, estado y duración) y, debajo, lo que
// hace falta leer sin abrir nada: el checkpoint al que se refiere (un enlace que lo
// abre en el árbol, el editor y el visor), los datos de la malla de un render o una
// medición, los errores con su línea y el código ofensivo, y la imagen del `preview`
// en miniatura (se amplía con un clic). Desplegada, además, los argumentos y la
// respuesta cruda.
//
// Es el ToolCallCard de mini-tools con lucide en vez de Material Symbols, la imagen
// de la herramienta y el resumen de OpenSCAD, que allá no existen.

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
    import_mesh: FileUp,
}

// Las que escriben algo en el proyecto (un checkpoint nuevo, un export): van en el
// color de acento, para que de un vistazo la secuencia distinga lo que miró de
// lo que creó.
const WRITES = new Set(['create_checkpoint', 'import_mesh', 'export'])

// Claves de los argumentos que mejor resumen la llamada, en orden de preferencia.
// `file` no está: el checkpoint va aparte, como enlace.
const SUMMARY_KEYS = ['label', 'name', 'view', 'format', 'template', 'id']

function summaryOf(step: ChatStep): string {
    for (const k of SUMMARY_KEYS) {
        const v = step.args[k]
        if (typeof v === 'string' && v.trim()) return v.trim()
    }
    // Una herramienta sin ninguna de esas claves (la de un CLI, por ejemplo) se resume con su primer texto corto.
    for (const [k, v] of Object.entries(step.args)) {
        if (k !== 'file' && typeof v === 'string' && v.trim() && v.length <= 60 && !v.includes('\n')) return v.trim()
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

type Diagnostic = {severity?: string; line?: number; col?: number; message?: string; code?: string; file?: string}
type Vec = {x?: number; y?: number; z?: number}
type Mesh = {sizeMm?: Vec | number[]; size?: Vec | number[]; volumeCm3?: number; triangles?: number; watertight?: boolean}
type ToolJSON = {
    file?: string
    diagnostics?: Diagnostic[]
    message?: string
    mesh?: Mesh
    fitsBuildVolume?: boolean
    printer?: {name?: string}
    estimatedWeightG?: number
    warnings?: string[]
}

// El resultado de una herramienta de ScadLab, que es JSON; null si no lo es (la
// herramienta de un CLI, un error de otro origen).
function parseResult(text: string): ToolJSON | null {
    const t = text.trim().replace(/^error:\s*/, '')
    if (!t.startsWith('{')) return null
    try {
        const v = JSON.parse(t) as unknown
        return v && typeof v === 'object' && !Array.isArray(v) ? (v as ToolJSON) : null
    } catch {
        return null
    }
}

// Los diagnósticos de un render que falló o de un lint: la línea y el código
// ofensivo son lo primero que hay que leer. Vacío si el resultado no trae ninguno.
function diagnosticsOf(r: ToolJSON | null): Diagnostic[] {
    if (!r || !Array.isArray(r.diagnostics)) return []
    return r.diagnostics.filter((d) => d && typeof d === 'object')
}

const MAX_INLINE_DIAGS = 3

// El checkpoint al que se refiere el paso: el que creó (su resultado lo dice) o el
// del argumento `file`. Solo si parece un archivo de checkpoint.
function fileOf(step: ChatStep, r: ToolJSON | null): string {
    const fromResult = (step.tool === 'create_checkpoint' || step.tool === 'import_mesh') && step.status === 'ok' ? r?.file : ''
    const f = fromResult || step.args.file
    return typeof f === 'string' && /\.scad$/i.test(f.trim()) ? f.trim() : ''
}

const dim = (v: Vec | number[] | undefined): number[] | null => {
    if (!v) return null
    const a = Array.isArray(v) ? v : [v.x, v.y, v.z]
    return a.length === 3 && a.every((n) => typeof n === 'number') ? (a as number[]) : null
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

function Chip({children, tone, title}: {children: ReactNode; tone?: 'ok' | 'warn'; title?: string}) {
    return (
        <span
            title={title}
            className={cn(
                'inline-flex items-center gap-1 rounded-full border px-1.5 py-px font-mono text-ui-10-5',
                tone === 'ok' ? 'border-ok/40 text-ok' : tone === 'warn' ? 'border-warn/50 text-warn' : 'border-line text-muted',
            )}
        >
            {children}
        </span>
    )
}

// Los datos de la malla de un render o una medición, en una línea de pastillas.
function MeshFacts({r, tool}: {r: ToolJSON; tool: string}) {
    const t = useT()
    const c = t.chat.steps
    const m = r.mesh
    if (!m) return null
    const size = dim(m.sizeMm ?? m.size)
    const nf = (n: number) => n.toLocaleString(locale(), {maximumFractionDigits: 1})
    return (
        <>
            {size ? <Chip title={c.facts.sizeHint}>{`${nf(size[0])} × ${nf(size[1])} × ${nf(size[2])} mm`}</Chip> : null}
            {typeof m.triangles === 'number' ? <Chip title={c.facts.trianglesHint}>{fmt(c.facts.triangles, {n: m.triangles.toLocaleString(locale())})}</Chip> : null}
            {tool === 'measure' && typeof m.volumeCm3 === 'number' ? <Chip title={c.facts.volumeHint}>{`${nf(m.volumeCm3)} cm³`}</Chip> : null}
            {tool === 'measure' && typeof r.estimatedWeightG === 'number' ? <Chip title={c.facts.weightHint}>{`≈ ${nf(r.estimatedWeightG)} g`}</Chip> : null}
            {typeof m.watertight === 'boolean' ? (
                <Chip tone={m.watertight ? 'ok' : 'warn'} title={m.watertight ? c.facts.watertightHint : c.facts.openHint}>
                    {m.watertight ? c.facts.watertight : c.facts.open}
                </Chip>
            ) : null}
            {r.fitsBuildVolume === false ? (
                <Chip tone="warn" title={c.facts.noFitHint}>
                    {c.facts.noFit}
                </Chip>
            ) : r.fitsBuildVolume === true ? (
                <Chip tone="ok" title={r.printer?.name ? fmt(c.facts.fitsHint, {printer: r.printer.name}) : c.facts.fits}>
                    {c.facts.fits}
                </Chip>
            ) : null}
        </>
    )
}

// Un diagnóstico con su línea y el código que marca: el error de un render, sin abrir nada.
function DiagnosticRow({d}: {d: Diagnostic}) {
    const c = useT().chat.steps
    const warn = d.severity === 'warning'
    return (
        <li className={cn('rounded border px-2 py-1', warn ? 'border-warn/30 bg-warn/10' : 'border-danger/30 bg-danger/10')}>
            <p className="flex flex-wrap items-baseline gap-x-2 text-ui-11-5">
                {d.line ? <span className={cn('font-mono font-medium', warn ? 'text-warn' : 'text-danger')}>{fmt(c.line, {n: d.line})}</span> : null}
                <span className="selectable min-w-0 break-words text-fg">{d.message}</span>
            </p>
            {d.code ? <p className="selectable mt-0.5 truncate font-mono text-ui-11 text-muted">{d.code}</p> : null}
        </li>
    )
}

export function ToolCallCard({step, onOpenCheckpoint}: {step: ChatStep; onOpenCheckpoint?: (file: string) => void}) {
    const t = useT()
    const c = t.chat.steps
    // El detalle (argumentos y respuesta cruda) arranca plegado: lo que hay que leer de un paso —el
    // error, los datos, la miniatura— ya está a la vista debajo de la línea.
    const [open, setOpen] = useState(false)
    const [zoom, setZoom] = useState<string | null>(null)
    const Icon = ICONS[step.tool] ?? Wrench
    const known = (c.tools as Record<string, string>)[step.tool]
    const label = known ?? step.tool
    const summary = summaryOf(step)
    const dur = formatDuration(step.durationMs)
    const status = c.status[step.status]
    const result = parseResult(step.text)
    const diags = diagnosticsOf(result)
    const file = fileOf(step, result)
    const failed = step.status === 'error'
    // Un error sin diagnósticos (otro origen, o un CLI) se lee tal cual, recortado.
    const plainError = failed && diags.length === 0 ? (result?.message ?? prettyResult(step.text)).trim() : ''
    const shownDiags = diags.slice(0, MAX_INLINE_DIAGS)
    const hasFacts = step.status === 'ok' && !!result?.mesh
    const body = !!file || hasFacts || shownDiags.length > 0 || !!plainError || step.images.length > 0
    return (
        <div
            data-step={step.status}
            data-tool={step.tool}
            className={cn('@container my-1.5 overflow-hidden rounded-lg border bg-surface', failed ? 'border-danger/50' : 'border-line')}
        >
            <button
                type="button"
                title={fmt(c.cardHint, {n: step.index, tool: label, status})}
                aria-expanded={open}
                onClick={() => setOpen(!open)}
                className="no-drag flex w-full items-center gap-2 px-2.5 py-1.5 text-left transition-colors hover:bg-surface-2"
            >
                <span className="font-mono text-ui-10 text-faint tabular-nums">{step.index}</span>
                <Icon className={cn('icon-ui-14 shrink-0', WRITES.has(step.tool) ? 'text-accent' : 'text-muted')} />
                <span className="min-w-0 shrink-0 truncate text-ui-12 font-medium text-fg">{label}</span>
                {summary ? <span className="min-w-0 flex-1 truncate font-mono text-ui-11 text-muted">{summary}</span> : <span className="flex-1" />}
                {dur ? <span className="hidden shrink-0 font-mono text-ui-10-5 text-faint tabular-nums @min-[18rem]:inline">{dur}</span> : null}
                <span
                    title={status}
                    className={cn('flex shrink-0 items-center gap-1 text-ui-11', step.status === 'ok' && 'text-ok', failed && 'text-danger', step.status === 'running' && 'text-accent')}
                >
                    {step.status === 'running' ? <Loader2 className="icon-ui-14 animate-spin" /> : null}
                    {step.status === 'ok' ? <Check className="icon-ui-14" /> : null}
                    {failed ? <CircleAlert className="icon-ui-14" /> : null}
                    <span className="sr-only">{status}</span>
                </span>
                <ChevronDown className={cn('icon-ui-14 shrink-0 text-faint transition-transform duration-150', open && 'rotate-180')} />
            </button>

            {body ? (
                <div className="flex flex-col gap-1.5 border-t border-line px-2.5 py-1.5">
                    {file || hasFacts ? (
                        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                            {file ? (
                                onOpenCheckpoint ? (
                                    <button
                                        type="button"
                                        data-testid="step-file"
                                        title={fmt(c.openFileHint, {file})}
                                        onClick={() => onOpenCheckpoint(file)}
                                        className="no-drag inline-flex max-w-full items-center gap-1 rounded text-accent underline decoration-accent/30 underline-offset-2 transition-colors hover:decoration-accent"
                                    >
                                        <span className="truncate font-mono text-ui-11-5">{file}</span>
                                        <ArrowUpRight className="icon-ui-12 shrink-0" />
                                    </button>
                                ) : (
                                    <span className="truncate font-mono text-ui-11-5 text-muted">{file}</span>
                                )
                            ) : null}
                            {hasFacts && result ? <MeshFacts r={result} tool={step.tool} /> : null}
                        </div>
                    ) : null}

                    {shownDiags.length > 0 ? (
                        <ul className="flex flex-col gap-1">
                            {shownDiags.map((d, i) => (
                                <DiagnosticRow key={i} d={d} />
                            ))}
                            {diags.length > shownDiags.length ? (
                                <li>
                                    <button type="button" title={c.moreDiagsHint} onClick={() => setOpen(true)} className="no-drag text-ui-11 text-faint underline decoration-line-strong underline-offset-2 hover:text-fg">
                                        {fmt(c.moreDiags, {n: diags.length - shownDiags.length})}
                                    </button>
                                </li>
                            ) : null}
                        </ul>
                    ) : null}
                    {shownDiags.length > 0 && result?.message ? <p className="selectable text-ui-11-5 text-muted">{result.message}</p> : null}
                    {plainError ? (
                        <p className="selectable flex items-start gap-1.5 text-ui-11-5 break-words text-danger">
                            <TriangleAlert className="mt-0.5 icon-ui-12 shrink-0" />
                            <span className="line-clamp-4 min-w-0">{plainError}</span>
                        </p>
                    ) : null}
                    {hasFacts && result?.warnings?.length ? <p className="selectable text-ui-11 text-warn">{result.warnings[0]}</p> : null}

                    {step.images.length > 0 ? (
                        <div className="flex flex-wrap gap-1.5">
                            {step.images.map((src, i) => (
                                <button
                                    key={i}
                                    type="button"
                                    title={c.zoomImage}
                                    onClick={() => setZoom(src)}
                                    className="no-drag max-w-full overflow-hidden rounded border border-line bg-surface-2 transition-colors hover:border-accent"
                                >
                                    <img src={src} alt={fmt(c.imageAlt, {tool: label})} draggable={false} className="block h-24 max-w-full w-auto object-contain" />
                                </button>
                            ))}
                        </div>
                    ) : null}
                </div>
            ) : null}

            {open ? (
                <div className="flex flex-col gap-2 border-t border-line px-2.5 py-2">
                    <ArgsView args={step.args} />
                    <div>
                        <p className="mb-0.5 font-mono text-ui-10 tracking-wider text-faint uppercase">{failed ? c.error : c.result}</p>
                        {step.text.trim() ? (
                            <Pre className={failed ? 'border-danger/40' : undefined}>{prettyResult(step.text)}</Pre>
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
