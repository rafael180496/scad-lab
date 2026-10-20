import {useState, type ReactNode} from 'react'
import {
    AlignLeft,
    Archive,
    ArchiveRestore,
    ArrowUpRight,
    BookOpen,
    Check,
    ChevronDown,
    CircleAlert,
    Columns2,
    Download,
    Eye,
    FilePenLine,
    FilePlus2,
    FileImage,
    FileText,
    FileUp,
    FolderOpen,
    GitBranch,
    GitFork,
    Hammer,
    LayoutTemplate,
    Layers,
    Library,
    ListTree,
    Loader2,
    Mountain,
    Move3d,
    Blocks,
    PackageOpen,
    Pencil,
    Printer,
    Rotate3d,
    Ruler,
    Save,
    ScanSearch,
    ShieldCheck,
    SlidersHorizontal,
    Spline,
    TextSearch,
    Trash2,
    TriangleAlert,
    Weight,
    Wrench,
    type LucideIcon,
} from 'lucide-react'
import {fmt, locale, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import type {ChatStep} from '../../lib/chatTypes'
import {createdCheckpoint, stepResult} from '../../lib/chatSteps'
import {compactTokens, formatDuration} from '../../lib/chatContext'
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
    outline_checkpoint: ListTree,
    find_in_checkpoint: TextSearch,
    edit_checkpoint: FilePenLine,
    update_checkpoint: FilePenLine,
    extract_module: Blocks,
    create_checkpoint: FilePlus2,
    render: Hammer,
    lint: ShieldCheck,
    format: AlignLeft,
    measure: Ruler,
    preview: Eye,
    printer_info: Printer,
    list_printers: Printer,
    set_workspace_printer: Printer,
    export: PackageOpen,
    list_templates: LayoutTemplate,
    get_template: LayoutTemplate,
    list_skills: BookOpen,
    read_skill: BookOpen,
    inspect_reference: ScanSearch,
    import_mesh: FileUp,
    set_active_parent: GitFork,
    rename_checkpoint: Pencil,
    trash_checkpoint: Trash2,
    list_trash: Archive,
    restore_checkpoint: ArchiveRestore,
    list_refs: FolderOpen,
    get_parameters: SlidersHorizontal,
    set_parameters: SlidersHorizontal,
    create_from_template: LayoutTemplate,
    list_libraries: Library,
    install_library: Download,
    edit_mesh: Move3d,
    estimate_print: Weight,
    list_materials: Layers,
    suggest_orientation: Rotate3d,
    autosave: Save,
    image_info: FileImage,
    compare_with_image: Columns2,
    trace_image_outline: Spline,
    image_to_heightmap: Mountain,
}

// Las que escriben algo en el proyecto (un checkpoint nuevo, un cambio del árbol, una
// librería, un export): van en el color de acento, para que de un vistazo la secuencia
// distinga lo que miró de lo que cambió. Es la misma división «Solo consulta» / «Agente»
// de backend/clichat/modes.go.
const WRITES = new Set([
    'create_checkpoint',
    'edit_checkpoint',
    'update_checkpoint',
    'extract_module',
    'import_mesh',
    'set_parameters',
    'create_from_template',
    'edit_mesh',
    'set_active_parent',
    'rename_checkpoint',
    'trash_checkpoint',
    'restore_checkpoint',
    'install_library',
    'export',
    'set_workspace_printer',
    'autosave',
    'trace_image_outline',
    'image_to_heightmap',
])

// Claves de los argumentos que mejor resumen la llamada, en orden de preferencia.
// `file` no está: el checkpoint va aparte, como enlace.
const SUMMARY_KEYS = ['label', 'name', 'view', 'format', 'template', 'id', 'trashed_name']

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

// ── Herramientas de imagen (S15) ────────────────────────────────────────────
// compare_with_image y trace_image_outline devuelven JSON con sus medidas y una imagen. La tarjeta lee esos campos
// con tolerancia —varios nombres posibles, nada obligatorio—: lo que falte simplemente no se muestra como pastilla,
// y el JSON crudo sigue a un clic en el detalle.
type Json = Record<string, unknown>

function pick(r: Json | null, ...paths: string[]): unknown {
    for (const p of paths) {
        let cur: unknown = r
        for (const k of p.split('.')) cur = cur && typeof cur === 'object' ? (cur as Json)[k] : undefined
        if (cur !== undefined && cur !== null) return cur
    }
    return undefined
}

const asNum = (v: unknown): number | undefined => (typeof v === 'number' && Number.isFinite(v) ? v : undefined)

// Los archivos que dejó el trazado: una ruta, una lista de rutas o una lista de objetos con su ruta.
function filesOf(r: Json | null): string[] {
    const v = pick(r, 'files', 'svgFiles', 'svgs', 'svg', 'file')
    const list = Array.isArray(v) ? v : typeof v === 'string' ? [v] : []
    return list
        .map((x) => (typeof x === 'string' ? x : x && typeof x === 'object' ? String((x as Json).file ?? (x as Json).path ?? '') : ''))
        .filter((x) => x !== '')
}

// Ancho y alto de una caja en mm: [w, h], [x, y, w, h] o {w, h} / {width, height}.
function boxOf(r: Json | null): [number, number] | null {
    const v = pick(r, 'sizeMm', 'bboxMm', 'bbox', 'size')
    if (Array.isArray(v)) {
        const n = v.map(asNum)
        if (n.length >= 4 && n[2] !== undefined && n[3] !== undefined) return [n[2], n[3]]
        if (n.length >= 2 && n[0] !== undefined && n[1] !== undefined) return [n[0], n[1]]
    } else if (v && typeof v === 'object') {
        const o = v as Json
        const w = asNum(o.w ?? o.width ?? o.x)
        const h = asNum(o.h ?? o.height ?? o.y)
        if (w !== undefined && h !== undefined) return [w, h]
    }
    return null
}

// La comparación con la foto: la vista elegida, las dos proporciones ancho/alto y el IoU de las siluetas.
function CompareFacts({r}: {r: Json}) {
    const t = useT()
    const c = t.chat.steps.image
    const view = pick(r, 'view', 'viewUsed')
    const iou = asNum(pick(r, 'silhouetteIoU', 'silhouetteIou', 'iou'))
    const model = asNum(pick(r, 'modelAspect', 'aspect.model', 'modelRatio', 'proportions.model'))
    const photo = asNum(pick(r, 'photoAspect', 'aspect.photo', 'photoRatio', 'proportions.photo'))
    const nf = (n: number) => n.toLocaleString(locale(), {maximumFractionDigits: 2})
    const viewName = typeof view === 'string' ? ((t.viewer.viewNames as Record<string, string>)[view] ?? view) : ''
    return (
        <>
            {viewName ? <Chip title={c.viewHint}>{fmt(c.view, {view: viewName})}</Chip> : null}
            {model !== undefined || photo !== undefined ? <span className="text-ui-11 text-faint">{c.aspectLabel}</span> : null}
            {model !== undefined ? <Chip title={c.aspectModelHint}>{fmt(c.aspectModel, {n: nf(model)})}</Chip> : null}
            {photo !== undefined ? <Chip title={c.aspectPhotoHint}>{fmt(c.aspectPhoto, {n: nf(photo)})}</Chip> : null}
            {iou !== undefined ? (
                <Chip tone={iou >= 0.85 ? 'ok' : iou < 0.6 ? 'warn' : undefined} title={c.iouHint}>
                    {fmt(c.iou, {pct: Math.round(iou * 100)})}
                </Chip>
            ) : null}
        </>
    )
}

// El trazado: los contornos, el área, la caja en mm, las capas y los archivos SVG que quedaron en refs/.
function TraceFacts({r}: {r: Json}) {
    const t = useT()
    const c = t.chat.steps.image
    const contoursRaw = pick(r, 'contours', 'contourCount')
    const contours = Array.isArray(contoursRaw) ? contoursRaw.length : asNum(contoursRaw)
    const area = asNum(pick(r, 'areaMm2', 'area_mm2', 'area'))
    const box = boxOf(r)
    const layers = asNum(pick(r, 'layers', 'layerCount'))
    const nf = (n: number) => n.toLocaleString(locale(), {maximumFractionDigits: 1})
    return (
        <>
            {box ? <Chip title={c.sizeHint}>{fmt(c.size, {w: nf(box[0]), h: nf(box[1])})}</Chip> : null}
            {area !== undefined ? <Chip title={c.areaHint}>{fmt(c.area, {n: nf(area)})}</Chip> : null}
            {contours !== undefined ? <Chip title={c.contoursHint}>{fmt(c.contours, {n: contours})}</Chip> : null}
            {layers !== undefined && layers > 1 ? <Chip title={c.layersHint}>{fmt(c.layers, {n: layers})}</Chip> : null}
        </>
    )
}

// El checkpoint al que se refiere el paso: el que creó (su resultado lo dice) o el
// del argumento `file`. Solo si parece un archivo de checkpoint.
function fileOf(step: ChatStep): string {
    // Mandado a la papelera ya no está en el árbol: un enlace llevaría a un archivo que no existe.
    if (step.tool === 'trash_checkpoint') return ''
    // Restaurar: el archivo que volvió (puede llevar otro número que el de la papelera).
    const back = step.tool === 'restore_checkpoint' && step.status === 'ok' ? stepResult(step.text)?.file : undefined
    const f = createdCheckpoint(step) || back || step.args.file
    return typeof f === 'string' && /\.scad$/i.test(f.trim()) ? f.trim() : ''
}

const dim = (v: Vec | number[] | undefined): number[] | null => {
    if (!v) return null
    const a = Array.isArray(v) ? v : [v.x, v.y, v.z]
    return a.length === 3 && a.every((n) => typeof n === 'number') ? (a as number[]) : null
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
            {entries.map(([k, v]) => {
                // Un argumento que no es texto (las `edits` de edit_checkpoint, una lista de parámetros) se sangra si es largo.
                const text = typeof v === 'string' ? v : (JSON.stringify(v) ?? '')
                const long = isLong(text)
                return (
                    <div key={k} className="min-w-0">
                        <dt className="font-mono text-ui-10 tracking-wider text-faint uppercase">{k}</dt>
                        <dd className="min-w-0">
                            {long ? (
                                <Pre>{typeof v === 'string' ? v : JSON.stringify(v, null, 2)}</Pre>
                            ) : (
                                <span className="selectable font-mono text-ui-11 break-words text-fg">{text}</span>
                            )}
                        </dd>
                    </div>
                )
            })}
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

// Las medidas de un paso, en el detalle: cuánto tardó, el contexto de la vuelta que lo pidió, lo que
// escribió el modelo en esa vuelta y lo que la herramienta le devolvió. Solo lo que se informó (S17).
function StepStats({step}: {step: ChatStep}) {
    const c = useT().chat.steps
    const loc = locale()
    const n = (v: number) => v.toLocaleString(loc)
    const rows: [string, string][] = []
    if (step.durationMs > 0) rows.push([c.statTime, formatDuration(step.durationMs, loc)])
    if (step.roundInput > 0) rows.push([c.statContext, fmt(c.statTokens, {n: n(step.roundInput)})])
    if (step.roundOutput > 0) rows.push([c.statOutput, fmt(c.statTokens, {n: n(step.roundOutput)})])
    if (step.resultTokens > 0) rows.push([c.statResult, fmt(c.statResultValue, {tokens: n(step.resultTokens), chars: n(step.resultChars)})])
    if (rows.length === 0) return null
    return (
        <div data-testid="step-stats">
            <p className="mb-0.5 font-mono text-ui-10 tracking-wider text-faint uppercase">{c.stats}</p>
            <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-ui-11">
                {rows.map(([k, v]) => (
                    <div key={k} className="contents">
                        <dt className="text-faint">{k}</dt>
                        <dd className="font-mono text-fg tabular-nums">{v}</dd>
                    </div>
                ))}
            </dl>
        </div>
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
    const loc = locale()
    const dur = formatDuration(step.durationMs, loc)
    // S17: el contexto de la vuelta que pidió el paso y lo que el paso devolvió al modelo. 0 = no se informó.
    const ctx = step.roundInput > 0 ? compactTokens(step.roundInput, loc) : ''
    const added = step.resultTokens > 0 ? compactTokens(step.resultTokens, loc) : ''
    const status = c.status[step.status]
    const result = parseResult(step.text)
    const diags = diagnosticsOf(result)
    const file = fileOf(step)
    const failed = step.status === 'error'
    // Un error sin diagnósticos (otro origen, o un CLI) se lee tal cual, recortado.
    const plainError = failed && diags.length === 0 ? (result?.message ?? prettyResult(step.text)).trim() : ''
    const shownDiags = diags.slice(0, MAX_INLINE_DIAGS)
    const hasFacts = step.status === 'ok' && !!result?.mesh
    // Las herramientas de imagen (S15): la comparación con la foto muestra su imagen GRANDE (es lo que se vino a ver) con
    // sus medidas; el trazado, la silueta con los SVG creados. image_info e image_to_heightmap son tarjetas normales.
    const isCompare = step.tool === 'compare_with_image'
    const isTrace = step.tool === 'trace_image_outline'
    const imageFacts = step.status === 'ok' && (isCompare || isTrace) && !!result
    const traced = isTrace && step.status === 'ok' ? filesOf(result as Json | null) : []
    const body = !!file || hasFacts || imageFacts || traced.length > 0 || shownDiags.length > 0 || !!plainError || step.images.length > 0
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
                {dur ? <span title={`${c.statTime}: ${dur}`} className="hidden shrink-0 font-mono text-ui-10-5 text-faint tabular-nums @min-[18rem]:inline">{dur}</span> : null}
                {ctx ? (
                    <span data-testid="step-ctx" title={fmt(c.ctxHint, {n: step.roundInput.toLocaleString(loc)})} className="hidden shrink-0 font-mono text-ui-10-5 text-faint tabular-nums @min-[24rem]:inline">
                        {fmt(c.ctx, {n: ctx})}
                    </span>
                ) : null}
                {added ? (
                    <span
                        data-testid="step-added"
                        title={fmt(c.addedHint, {tokens: step.resultTokens.toLocaleString(loc), chars: step.resultChars.toLocaleString(loc)})}
                        className="hidden shrink-0 font-mono text-ui-10-5 text-faint tabular-nums @min-[24rem]:inline"
                    >
                        {fmt(c.added, {n: added})}
                    </span>
                ) : null}
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

                    {imageFacts || traced.length > 0 ? (
                        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                            {imageFacts && isCompare ? <CompareFacts r={result as Json} /> : null}
                            {imageFacts && isTrace ? <TraceFacts r={result as Json} /> : null}
                            {traced.map((f) => (
                                <span key={f} data-testid="step-svg" title={c.image.filesHint} className="max-w-full truncate font-mono text-ui-11-5 text-accent">
                                    {f}
                                </span>
                            ))}
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
                                    data-testid={isCompare ? 'step-compare-image' : isTrace ? 'step-trace-image' : undefined}
                                    title={c.zoomImage}
                                    onClick={() => setZoom(src)}
                                    className={cn(
                                        'no-drag max-w-full overflow-hidden rounded border border-line bg-surface-2 transition-colors hover:border-accent',
                                        isCompare && 'w-full',
                                    )}
                                >
                                    <img
                                        src={src}
                                        alt={fmt(c.imageAlt, {tool: label})}
                                        draggable={false}
                                        className={cn('block max-w-full object-contain', isCompare ? 'mx-auto h-auto max-h-72 w-auto' : isTrace ? 'h-36 w-auto' : 'h-24 w-auto')}
                                    />
                                </button>
                            ))}
                            {isCompare && step.status === 'ok' ? <p className="w-full text-ui-10-5 text-faint">{c.image.sideBySide}</p> : null}
                        </div>
                    ) : null}
                </div>
            ) : null}

            {open ? (
                <div className="flex flex-col gap-2 border-t border-line px-2.5 py-2">
                    <ArgsView args={step.args} />
                    <StepStats step={step} />
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
