import {useMemo, useState, type RefObject} from 'react'
import {Canvas} from '@react-three/fiber'
import {Box3, NeutralToneMapping, Vector3, type BufferGeometry} from 'three'
import {Box as BoxIcon, CircleAlert, Loader2, Maximize, TriangleAlert} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {boundsOf, boxSize, tfMatrix} from '../../lib/meshEditMath'
import {docOf, needsClosedMesh, type Session} from '../../lib/meshEditSession'
import {num} from '../../lib/printFormat'
import type {ThemeColors} from '../../hooks/useThemeColors'
import {Row} from '../viewer/Dimensions'
import type {BedVolume, PaneHandle} from '../viewer/ViewPane'
import type {ViewName} from '../viewer/camera'
import {partWorldBox, type EditActions} from './actions'
import {EditScene} from './EditScene'
import {liveStore} from './live'

// El visor del modo «Editar»: el <Canvas> con la escena y, encima, lo que no es 3D: avisos, atajos de vista y las
// medidas en vivo de la selección.

const VIEW_BUTTONS: ViewName[] = ['iso', 'top', 'front', 'right']

function Chip({tone, icon, children, title}: {tone: 'warn' | 'info'; icon: React.ReactNode; children: React.ReactNode; title: string}) {
    return (
        <span
            title={title}
            className={cn(
                'pointer-events-auto flex max-w-full items-start gap-1.5 rounded-md border bg-surface/90 px-2 py-1 text-ui-11 leading-snug backdrop-blur-sm',
                tone === 'warn' ? 'border-warn/50 text-warn' : 'border-line text-muted',
            )}
        >
            <span className="mt-px shrink-0">{icon}</span>
            <span className="min-w-0">{children}</span>
        </span>
    )
}

function Measures({session, geometry, dense}: {session: Session; geometry: BufferGeometry | null; dense: boolean}) {
    const t = useT()
    const M = t.meshEdit
    const doc = docOf(session)
    const live = liveStore.use((s) => s.box)
    const modelBox = useMemo(() => (geometry ? boundsOf(geometry, tfMatrix(doc.model)) : new Box3()), [geometry, doc.model])
    const sel = session.sel
    const selBox = useMemo(() => {
        if (live) return new Box3(new Vector3(...live.min), new Vector3(...live.max))
        if (!sel) return null
        if (sel.kind === 'model') return modelBox
        if (sel.kind === 'part') {
            const p = doc.parts.find((x) => x.id === sel.id)
            return p ? partWorldBox(p) : null
        }
        return null
    }, [live, sel, doc.parts, modelBox])
    const total = useMemo(() => {
        const b = modelBox.clone()
        for (const p of doc.parts) if (p.mode === 'union') b.union(partWorldBox(p))
        return b
    }, [modelBox, doc.parts])
    const size = (b: Box3 | null) => (b && !b.isEmpty() ? boxSize(b).map((v) => num(v, 1)).join(' × ') : '—')
    const showTotal = (sel?.kind !== 'model' || doc.parts.some((p) => p.mode === 'union')) && !total.isEmpty()
    return (
        <div data-testid="measures" className="pointer-events-auto flex min-w-[150px] flex-col gap-1 rounded-lg border border-line bg-surface/90 px-3 py-2 backdrop-blur-sm">
            {dense ? null : <span className="font-mono text-ui-10 tracking-[0.12em] text-muted uppercase">{M.measure.title}</span>}
            {sel && sel.kind !== 'cut' ? (
                <>
                    <Row label={M.measure.selection} hint={M.measure.selectionHint}>
                        {fmt(t.viewer.mm, {v: size(selBox)})}
                    </Row>
                    {dense ? null : (
                        <Row label={M.measure.base} hint={M.measure.baseHint}>
                            {selBox && !selBox.isEmpty() ? fmt(t.viewer.mm, {v: num(selBox.min.z, 1)}) : '—'}
                        </Row>
                    )}
                </>
            ) : null}
            {showTotal && !dense ? (
                <Row label={M.measure.total} hint={M.measure.totalHint}>
                    {fmt(t.viewer.mm, {v: size(total)})}
                </Row>
            ) : null}
        </div>
    )
}

export function EditViewport({
    session,
    actions,
    geometry,
    loading,
    loadError,
    bed,
    theme,
    fitBox,
    handleRef,
    initialView = 'iso',
    dense = false,
}: {
    session: Session
    actions: EditActions
    geometry: BufferGeometry | null
    loading: boolean
    loadError: string
    bed: BedVolume
    theme: ThemeColors
    fitBox?: Box3
    handleRef: RefObject<PaneHandle | null>
    initialView?: ViewName
    /** Un panel angosto: las medidas se reducen a la de la selección. */
    dense?: boolean
}) {
    const t = useT()
    const M = t.meshEdit
    const [ortho, setOrtho] = useState(false)
    const doc = docOf(session)
    const booleans = needsClosedMesh(doc)
    return (
        <div data-meshedit-view className="relative min-h-0 min-w-0 flex-1 overflow-hidden bg-surface-2">
            <Canvas
                frameloop="demand"
                dpr={[1, 2]}
                gl={{antialias: true, alpha: true}}
                onCreated={({gl}) => {
                    gl.toneMapping = NeutralToneMapping
                    // Los planos de recorte de cada material (el corte se previsualiza sin tocar la malla).
                    gl.localClippingEnabled = true
                }}
            >
                <EditScene ws={session.workspaceId} geometry={geometry} bed={bed} theme={theme} actions={actions} ortho={ortho} initialView={initialView} handleRef={handleRef} fitBox={fitBox} />
            </Canvas>

            <div className="pointer-events-none absolute top-2 left-2 z-10 flex max-w-[calc(100%-13rem)] flex-col items-start gap-1.5">
                {!session.facts.watertight ? (
                    <Chip tone={booleans ? 'warn' : 'info'} icon={<TriangleAlert className="icon-ui-14" />} title={M.notice.openHint}>
                        {booleans ? M.notice.openBooleans : M.notice.open}
                    </Chip>
                ) : null}
                {session.tool === 'face' ? (
                    <Chip tone="info" icon={<CircleAlert className="icon-ui-14" />} title={M.notice.faceHint}>
                        {M.notice.face}
                    </Chip>
                ) : null}
            </div>

            <div className="pointer-events-none absolute top-2 right-2 z-10 flex flex-wrap justify-end gap-0.5">
                <div className="pointer-events-auto flex items-center gap-0.5 rounded-md border border-line bg-surface/90 p-0.5 backdrop-blur-sm">
                    {VIEW_BUTTONS.map((v, i) => (
                        <button
                            key={v}
                            type="button"
                            title={fmt(t.viewer.viewHint, {view: t.viewer.viewNames[v], key: v === 'iso' ? 7 : v === 'top' ? 5 : v === 'front' ? 1 : 4})}
                            data-testid={`view-${v}`}
                            onClick={() => handleRef.current?.setView(v)}
                            className="inline-flex h-5 shrink-0 items-center rounded px-1 font-mono text-ui-10-5 tracking-wide text-muted uppercase transition-colors duration-100 hover:bg-surface-2 hover:text-fg"
                        >
                            {t.viewer.views[v]}
                        </button>
                    ))}
                    <button
                        type="button"
                        title={t.viewer.fit}
                        onClick={() => handleRef.current?.fit()}
                        className="inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded text-muted transition-colors duration-100 hover:bg-surface-2 hover:text-fg"
                    >
                        <Maximize className="icon-ui-12" />
                    </button>
                    <button
                        type="button"
                        title={ortho ? t.viewer.toPersp : t.viewer.toOrtho}
                        aria-pressed={ortho}
                        onClick={() => setOrtho((o) => !o)}
                        className={cn('inline-flex h-5 shrink-0 items-center gap-1 rounded px-1 font-mono text-ui-10-5 transition-colors duration-100', ortho ? 'bg-accent-soft text-accent' : 'text-muted hover:bg-surface-2 hover:text-fg')}
                    >
                        <BoxIcon className="icon-ui-12" />
                        {ortho ? t.viewer.ortho : t.viewer.persp}
                    </button>
                </div>
            </div>

            {geometry ? (
                <div className="pointer-events-none absolute bottom-2 left-2 z-10 max-h-[calc(100%-1rem)] max-w-[calc(100%-1rem)] overflow-auto">
                    <Measures session={session} geometry={geometry} dense={dense} />
                </div>
            ) : null}
            {loading ? (
                <span className="absolute top-2 right-2 z-10 mt-8 flex items-center gap-1.5 rounded-md border border-line bg-surface/90 px-2 py-1 font-mono text-ui-10-5 text-muted">
                    <Loader2 className="icon-ui-12 animate-spin text-accent" />
                    {t.viewer.loading}
                </span>
            ) : null}
            {loadError ? (
                <div className="absolute inset-x-4 top-1/2 z-10 mx-auto flex max-w-[420px] -translate-y-1/2 items-start gap-2 rounded-lg border border-danger/50 bg-surface p-3 text-ui-12 text-fg">
                    <CircleAlert className="mt-px icon-ui-14 shrink-0 text-danger" />
                    <span className="selectable">{fmt(t.viewer.loadError, {msg: loadError})}</span>
                </div>
            ) : null}
        </div>
    )
}
