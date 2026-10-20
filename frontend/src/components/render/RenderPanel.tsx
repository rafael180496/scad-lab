import {lazy, Suspense, useEffect, useRef, useState} from 'react'
import {
    ChevronDown,
    ChevronRight,
    CircleAlert,
    CircleCheck,
    CircleDashed,
    Clock,
    Cpu,
    Database,
    Loader2,
    Play,
    Settings,
    Square,
    TriangleAlert,
} from 'lucide-react'
import type {render, store} from '../../../wailsjs/go/models'
import {Button} from '../ui/Button'
import {toast} from '../ui/Toast'
import {DownloadControls} from './DownloadControls'
import {WasmNotice} from './WasmNotice'
import {StarterAction} from '../workspace/StarterAction'
import {HistoryControls} from '../workspace/HistoryControls'
import {useNarrow} from '../../hooks/useNarrow'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {renderTone, seconds, warnings, type RenderTone} from '../../lib/render'
import {goToLine} from '../../lib/editorSession'
import {errorText} from '../../lib/errors'
import {endSession, prepareSession, useSession} from '../../lib/meshEditSession'
import type {MeshEditShot} from '../meshedit/MeshEditor'
import type {BedVolume} from '../viewer/ViewPane'
import type {ViewerShot} from '../viewer/Viewer'

// three + R3F van en su propio chunk: la app abre sin cargarlos.
const Viewer = lazy(() => import('../viewer/Viewer'))
// El modo «Editar» (S13) trae el gizmo, la escena de edición y sus paneles: otro chunk, que solo se pide al entrar.
const MeshEditor = lazy(() => import('../meshedit/MeshEditor'))

const toneText: Record<RenderTone, string> = {
    running: 'text-accent',
    idle: 'text-faint',
    ok: 'text-ok',
    warn: 'text-warn',
    error: 'text-danger',
    missing: 'text-warn',
}

// Ancho de la barra de estado (a interfaz 100 %) por debajo del cual se omiten
// los datos del render para que el estado y el botón Renderizar respiren.
const STATUS_META_MIN_W = 500

function ToneIcon({tone}: {tone: RenderTone}) {
    const cls = cn('icon-ui-14 shrink-0', toneText[tone])
    switch (tone) {
        case 'running':
            return <Loader2 className={cn(cls, 'animate-spin')} />
        case 'ok':
            return <CircleCheck className={cls} />
        case 'warn':
        case 'missing':
            return <TriangleAlert className={cls} />
        case 'error':
            return <CircleAlert className={cls} />
        default:
            return <CircleDashed className={cls} />
    }
}

function DiagnosticList({diags, showErrorsFile}: {diags: render.Diagnostic[]; showErrorsFile: boolean}) {
    const t = useT()
    return (
        <>
            <ul className="selectable divide-y divide-line">
                {diags.map((d, i) => {
                    // Clic = ir a la línea en el editor (solo si es del propio
                    // checkpoint: un error dentro de un include no está ahí).
                    const go = d.line > 0 && !d.file
                    return (
                        <li key={i}>
                            <button
                                type="button"
                                disabled={!go}
                                title={go ? fmt(t.render.goToLine, {n: d.line}) : d.file || t.render.noLine}
                                onClick={() => goToLine(d.line, d.col)}
                                className="flex w-full items-start gap-2 px-3 py-2 text-left text-ui-12 transition-colors duration-100 enabled:hover:bg-surface-2 disabled:cursor-default"
                            >
                                {d.severity === 'error' ? (
                                    <CircleAlert className="mt-px icon-ui-14 shrink-0 text-danger" aria-label={t.render.severity.error} />
                                ) : (
                                    <TriangleAlert className="mt-px icon-ui-14 shrink-0 text-warn" aria-label={t.render.severity.warning} />
                                )}
                                <span
                                    className={cn(
                                        'w-[74px] shrink-0 font-mono text-ui-11',
                                        d.severity === 'error' ? 'text-danger' : 'text-warn',
                                        go && 'underline decoration-dotted underline-offset-2',
                                    )}
                                >
                                    {d.line > 0 ? fmt(t.render.line, {n: d.line}) : t.render.noLine}
                                </span>
                                <span className="min-w-0 flex-1 leading-snug break-words text-fg">
                                    {d.file ? <span className="font-mono text-muted">{d.file}: </span> : null}
                                    {d.message}
                                </span>
                            </button>
                        </li>
                    )
                })}
            </ul>
            {showErrorsFile ? <p className="border-t border-line px-3 py-2 text-ui-11-5 leading-relaxed text-faint">{t.render.errorsFile}</p> : null}
        </>
    )
}

// WarningsOverlay: con la pieza a la vista, las advertencias de un render OK
// quedan plegadas en una esquina del visor en vez de taparlo.
function WarningsOverlay({diags}: {diags: render.Diagnostic[]}) {
    const t = useT()
    const [open, setOpen] = useState(false)
    return (
        <div className="overflow-hidden rounded-lg border border-warn/50 bg-surface/95">
            <button
                type="button"
                title={open ? t.viewer.hideWarnings : t.viewer.showWarnings}
                aria-expanded={open}
                onPointerDown={(e) => e.stopPropagation()}
                onClick={() => setOpen((v) => !v)}
                className="flex h-7 items-center gap-1.5 px-2.5 text-ui-11-5 font-medium text-warn"
            >
                <TriangleAlert className="icon-ui-14" />
                {fmt(t.viewer.warnings, {n: diags.length})}
                {open ? <ChevronDown className="icon-ui-12" /> : <ChevronRight className="icon-ui-12" />}
            </button>
            {open ? (
                <div className="max-h-[220px] overflow-auto border-t border-line" onPointerDown={(e) => e.stopPropagation()}>
                    <DiagnosticList diags={diags} showErrorsFile={false} />
                </div>
            ) : null}
        </div>
    )
}

function Chip({icon, text, title}: {icon: React.ReactNode; text: string; title: string}) {
    return (
        <span className="flex shrink-0 items-center gap-1 font-mono text-ui-10-5 whitespace-nowrap text-muted" title={title}>
            <span className="text-faint">{icon}</span>
            {text}
        </span>
    )
}

// RenderPanel es el contenido del panel del visor: barra de estado del render
// arriba y, debajo, la pieza en el visor 3D si el render salió bien, o los
// diagnósticos si falló.
export function RenderPanel({
    workspaceId,
    file,
    result,
    running,
    bed,
    swatches,
    onSwatchesSaved,
    viewerShot,
    dimsCollapsed,
    onDimsCollapsed,
    viewerSaved,
    onViewerSaved,
    onRender,
    onCancel,
    onOpenSettings,
    editShot,
    onEditApplied,
    emptyWorkspace = false,
    editedByAI = false,
    onStarter = () => {},
}: {
    workspaceId: number | null
    file: string | null
    result: render.Result | undefined
    running: boolean
    bed: BedVolume
    swatches: string[]
    onSwatchesSaved: (colors: string[]) => void
    viewerShot?: ViewerShot
    dimsCollapsed: boolean
    onDimsCollapsed: (collapsed: boolean) => void
    /** Estado del visor guardado (settings.viewer) y aviso de que se guardó uno nuevo. */
    viewerSaved?: store.ViewerState
    onViewerSaved?: (v: store.ViewerState) => void
    onRender: () => void
    onCancel: () => void
    onOpenSettings: () => void
    /** Banco de capturas: abre el modo «Editar» con unas operaciones ya hechas. */
    editShot?: MeshEditShot
    /** Se aplicó una edición: el checkpoint nuevo que dejó Go (hay que elegirlo). */
    onEditApplied: (file: string) => void
    /** El proyecto no tiene ningún checkpoint: se ofrece «Crear un modelo de inicio». */
    emptyWorkspace?: boolean
    /** La última versión del checkpoint la dejó la IA (S16): indicador, «Deshacer cambio de la IA» y «Ver diferencias». */
    editedByAI?: boolean
    onStarter?: (file: string) => void
}) {
    const t = useT()
    const [logOpen, setLogOpen] = useState(false)
    // Panel angosto: los datos del render (duración, motor, versión) se omiten.
    const [narrow, bar] = useNarrow(STATUS_META_MIN_W)
    const tone = renderTone(result, running)

    // Malla a la vista: la del resultado OK o, mientras se renderiza, la que
    // ya se estaba viendo (sin parpadeo). Un error la oculta: mostrar la pieza
    // anterior junto a los diagnósticos haría creer que es la rota.
    const okMesh = result?.status === 'ok' && result.meshPath ? result.meshPath : null
    const [lastMesh, setLastMesh] = useState<string | null>(null)
    useEffect(() => {
        if (okMesh) setLastMesh(okMesh)
    }, [okMesh])
    const meshPath = okMesh ?? (running ? lastMesh : null)
    // Una vez montado, el visor no se desmonta: conserva la cámara al pasar
    // por un checkpoint roto y volver.
    const [viewerMounted, setViewerMounted] = useState(false)
    if (meshPath && !viewerMounted) setViewerMounted(true)

    // Modo «Editar» (S13). La sesión es del proyecto y de un checkpoint: si el elegido ya no es ese (lo cambió
    // algo que no pasó por la confirmación del árbol, p. ej. un checkpoint que salió a la papelera), se cierra.
    const session = useSession(workspaceId)
    const editing = !!session && session.file === file
    const [preparing, setPreparing] = useState(false)
    useEffect(() => {
        if (workspaceId != null && session && session.file !== file) endSession(workspaceId)
    }, [workspaceId, session, file])
    const startEdit = async () => {
        if (workspaceId == null || !file || preparing || editing) return
        setPreparing(true)
        try {
            await prepareSession(workspaceId, file)
        } catch (e) {
            toast.error(fmt(t.meshEdit.prepareError, {msg: errorText(e)}))
        } finally {
            setPreparing(false)
        }
    }
    // Banco de capturas: entra solo al modo «Editar» cuando el checkpoint ya tiene su malla.
    const shotStarted = useRef(false)
    const startEditRef = useRef(startEdit)
    startEditRef.current = startEdit
    useEffect(() => {
        if (!editShot || shotStarted.current || !okMesh || !viewerMounted) return
        shotStarted.current = true
        void startEditRef.current()
    }, [editShot, okMesh, viewerMounted])

    if (!file) {
        return (
            <div className="bed-grid flex h-full items-center justify-center">
                <div className="flex flex-col items-center">
                    <p className="mx-auto max-w-[420px] px-6 text-center text-ui-12-5 leading-relaxed text-muted">{t.render.noFile}</p>
                    {emptyWorkspace ? <StarterAction workspaceId={workspaceId} onCreated={onStarter} /> : null}
                </div>
            </div>
        )
    }

    const label: Record<RenderTone, string> = {
        running: t.render.rendering,
        idle: result?.status === 'cancelled' ? t.render.cancelled : t.render.idle,
        ok: t.render.ok,
        warn: fmt(t.render.okWarnings, {n: result ? warnings(result).length : 0}),
        error: t.render.failed,
        missing: t.render.statusMissing,
    }
    const diags = result?.diagnostics ?? []
    const showMeta = !narrow && !running && result && (result.status === 'ok' || result.status === 'error')
    // Renderizado con el motor integrado (sin OpenSCAD instalado): aviso informativo.
    const showWasm = !running && result?.engine === 'wasm' && (result.status === 'ok' || result.status === 'error')
    // Con la pieza a la vista, las advertencias de un render OK van plegadas encima.
    const showWarnings = !!okMesh && diags.length > 0 && !running

    return (
        <div className="flex h-full flex-col">
            <div ref={bar} className="flex h-9 shrink-0 items-center gap-3 overflow-clip border-b border-line bg-surface px-3">
                <span className={cn('flex min-w-0 shrink-0 items-center gap-1.5 text-ui-12 font-medium whitespace-nowrap', toneText[tone])}>
                    <ToneIcon tone={tone} />
                    <span className="truncate">{label[tone]}</span>
                </span>
                {showMeta ? (
                    // Los datos que no caben pasan a una segunda fila recortada: se omiten
                    // enteros en vez de quedar cortados a la mitad.
                    <div className="flex h-9 min-w-0 flex-1 flex-wrap content-start items-center gap-x-3 overflow-clip [&>*]:h-9 [&>*]:items-center">
                        <Chip icon={<Clock className="icon-ui-12" />} text={fmt(t.render.seconds, {s: seconds(result.durationMs)})} title={t.render.durationHint} />
                        {result.cacheHit ? <Chip icon={<Database className="icon-ui-12" />} text={t.render.fromCache} title={t.render.fromCacheHint} /> : null}
                        {result.backend ? <Chip icon={<Cpu className="icon-ui-12" />} text={fmt(t.render.backend, {b: result.backend})} title={t.render.backendHint} /> : null}
                        {result.version ? (
                            <span className="flex shrink-0 font-mono text-ui-10-5 whitespace-nowrap text-faint">{fmt(t.render.version, {v: result.version})}</span>
                        ) : null}
                    </div>
                ) : null}
                {editedByAI && workspaceId != null ? (
                    <div className="ml-auto flex shrink-0 items-center gap-1">
                        <HistoryControls workspaceId={workspaceId} file={file} editedByAI compact={narrow} />
                    </div>
                ) : null}
                <div className={cn('shrink-0', editedByAI ? '' : 'ml-auto')}>
                    {running ? (
                        <Button variant="outline" title={t.render.cancelHint} onClick={onCancel}>
                            <Square className="icon-ui-12" />
                            {t.render.cancel}
                        </Button>
                    ) : (
                        <Button variant="primary" title={result ? t.render.rerenderHint : t.render.renderHint} onClick={onRender}>
                            <Play className="icon-ui-12" />
                            {t.render.render}
                        </Button>
                    )}
                </div>
            </div>

            {showWasm && result ? <WasmNotice version={result.version} /> : null}

            {viewerMounted ? (
                <Suspense
                    fallback={
                        <div className="bed-grid flex min-h-0 flex-1 items-center justify-center">
                            <Loader2 className="icon-ui-16 animate-spin text-accent" />
                        </div>
                    }
                >
                    <div className={meshPath && !editing ? 'min-h-0 flex-1' : 'hidden'}>
                        <Viewer
                            meshPath={meshPath}
                            fileName={file}
                            bed={bed}
                            swatches={swatches}
                            onSwatchesSaved={onSwatchesSaved}
                            initial={viewerShot}
                            dimsCollapsed={dimsCollapsed}
                            onDimsCollapsed={onDimsCollapsed}
                            saved={viewerSaved}
                            onViewerSaved={onViewerSaved}
                            overlay={showWarnings ? <WarningsOverlay diags={diags} /> : null}
                            onEdit={okMesh && !running && workspaceId != null ? () => void startEdit() : undefined}
                            editBusy={preparing}
                        />
                    </div>
                </Suspense>
            ) : null}

            {editing && workspaceId != null ? (
                <Suspense
                    fallback={
                        <div className="bed-grid flex min-h-0 flex-1 items-center justify-center">
                            <Loader2 className="icon-ui-16 animate-spin text-accent" />
                        </div>
                    }
                >
                    <div className="min-h-0 flex-1">
                        <MeshEditor workspaceId={workspaceId} bed={bed} swatches={swatches} shot={editShot} onApplied={onEditApplied} />
                    </div>
                </Suspense>
            ) : null}

            {meshPath || editing ? null : (
                <div className="bed-grid flex min-h-0 flex-1 flex-col overflow-auto">
                    {tone === 'missing' ? (
                        <div className="m-4 flex flex-col items-start gap-3 rounded-lg border border-warn/50 bg-surface p-4">
                            <p className="text-ui-12-5 leading-relaxed text-fg">{t.render.missing}</p>
                            <div className="flex flex-wrap items-center gap-2">
                                <DownloadControls />
                                <Button title={t.render.openSettingsHint} onClick={onOpenSettings}>
                                    <Settings className="icon-ui-14" />
                                    {t.render.openSettings}
                                </Button>
                            </div>
                        </div>
                    ) : null}

                    {diags.length > 0 && !running ? (
                        <div className="m-4 mb-0 flex flex-col overflow-hidden rounded-lg border border-line bg-surface">
                            <div className="border-b border-line px-3 py-1.5 font-mono text-ui-10-5 tracking-[0.12em] text-muted uppercase">
                                {t.render.diagnostics}
                            </div>
                            <DiagnosticList diags={diags} showErrorsFile={result?.status === 'error'} />
                        </div>
                    ) : null}

                    {tone !== 'missing' && result?.status !== 'error' ? (
                        <div className="flex flex-1 items-center justify-center px-6 py-6">
                            <p className="max-w-[420px] text-center text-ui-12 leading-relaxed text-muted">
                                {running ? t.viewer.waiting : t.viewer.idle}
                            </p>
                        </div>
                    ) : null}
                </div>
            )}

            {result && !running && result.status !== 'missing' ? (
                <div className="shrink-0 border-t border-line bg-surface">
                    <button
                        type="button"
                        title={logOpen ? t.render.hideLog : t.render.showLog}
                        aria-expanded={logOpen}
                        onClick={() => setLogOpen((v) => !v)}
                        className="flex h-7 w-full items-center gap-1.5 px-3 font-mono text-ui-10-5 tracking-[0.12em] text-muted uppercase transition-colors duration-100 hover:text-fg"
                    >
                        {logOpen ? <ChevronDown className="icon-ui-12" /> : <ChevronRight className="icon-ui-12" />}
                        {t.render.log}
                    </button>
                    {logOpen ? (
                        <pre className="selectable max-h-[160px] overflow-auto border-t border-line bg-surface-2 px-3 py-2 font-mono text-ui-11 leading-[1.5] whitespace-pre-wrap text-muted">
                            {result.log || t.render.emptyLog}
                        </pre>
                    ) : null}
                </div>
            ) : null}
        </div>
    )
}
