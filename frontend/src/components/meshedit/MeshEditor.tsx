import {useCallback, useEffect, useMemo, useRef, useState, type KeyboardEvent} from 'react'
import type {Box3, BufferGeometry} from 'three'
import {Code2, GitBranchPlus, Loader2, Trash2, WandSparkles} from 'lucide-react'
import {ApplyMeshEdit} from '../../../wailsjs/go/main/App'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {errorText} from '../../lib/errors'
import {boundsOf, tfMatrix, type Shape, type Vec3} from '../../lib/meshEditMath'
import {modifyWithAI} from '../../lib/meshEditAI'
import {
    docOf,
    endSession,
    getSession,
    isDirty,
    needsClosedMesh,
    redo,
    select,
    setTool,
    toScene,
    undo,
    useSession,
    type PartMode,
    type Tool,
} from '../../lib/meshEditSession'
import {cachedRawMesh, loadRawMesh} from '../../lib/mesh'
import {isMac, isMod} from '../../lib/platform'
import {useNarrow} from '../../hooks/useNarrow'
import {useThemeColors} from '../../hooks/useThemeColors'
import {Button} from '../ui/Button'
import {confirmDialog} from '../ui/Dialog'
import {toast} from '../ui/Toast'
import type {BedVolume, PaneHandle} from '../viewer/ViewPane'
import type {ViewName} from '../viewer/camera'
import {editActions, type EditActions} from './actions'
import {ApplyDialog, CodeDialog} from './EditDialogs'
import {EditToolbar} from './EditToolbar'
import {EditViewport} from './EditViewport'
import {SidePanel, type SideTab} from './SidePanel'

// El modo «Editar» del visor (S13, 13.2): mover, rotar, escalar, espejar, cortar con un plano y agregar piezas
// (uniones o agujeros) sobre un modelo, con vista previa inmediata. NO toca el STL: «Aplicar» le pide a Go el
// código OpenSCAD de la edición y lo guarda como un checkpoint nuevo, hijo del que se editaba. Ver
// claude/23-edicion-stl.md, «Interfaz».
//
// Entra con React.lazy (three y R3F son el chunk pesado). El estado vive en lib/meshEditSession.

// Ancho (a interfaz 100 %) por debajo del cual el panel lateral pasa a ser un cajón bajo el visor, y por
// debajo del cual la barra de herramientas omite los rótulos.
const STACK_BELOW = 640
const COMPACT_BELOW = 960

/** Una edición escrita de antemano, para el banco de capturas (src/uishot.tsx): se aplica con las mismas operaciones que los clics. */
export type ShotStep =
    | {op: 'move'; to: Vec3}
    | {op: 'rotate'; to: Vec3}
    | {op: 'center'}
    | {op: 'rest'}
    | {op: 'modelColor'; value: string}
    | {op: 'add'; shape: Shape; mode: PartMode; at?: Vec3; size?: Vec3; text?: string; color?: string}
    | {op: 'cut'; point?: Vec3; normal?: Vec3; keep?: 'above' | 'below' | 'both'; split?: 'objects' | 'colors'; colors?: [string, string]}

export type MeshEditShot = {
    steps?: ShotStep[]
    tool?: Tool
    /** model | cut | part:N (la N-ésima pieza). */
    select?: string
    view?: ViewName
    panel?: SideTab
}

function runShot(ws: number, shot: MeshEditShot, actions: EditActions) {
    for (const st of shot.steps ?? []) {
        const s = getSession(ws)
        if (!s) return
        const doc = docOf(s)
        switch (st.op) {
            case 'move':
                actions.applyTf({kind: 'model'}, {...doc.model, translate: st.to}, 'move')
                break
            case 'rotate':
                actions.applyTf({kind: 'model'}, {...doc.model, rotate: st.to}, 'rotate')
                break
            case 'center':
                actions.center()
                break
            case 'rest':
                actions.rest()
                break
            case 'modelColor':
                actions.setModelColor(st.value)
                break
            case 'add': {
                actions.addPart(st.shape, st.mode)
                const part = docOf(getSession(ws)!).parts.at(-1)
                if (!part) break
                if (st.size) actions.setPartSize(part.id, st.size)
                if (st.text) actions.setPartText(part.id, st.text)
                if (st.color) actions.setPartColor(part.id, st.color)
                if (st.at) actions.applyTf({kind: 'part', id: part.id}, {...docOf(getSession(ws)!).parts.at(-1)!.tf, translate: st.at}, 'move')
                break
            }
            case 'cut': {
                actions.addCut()
                const cut = docOf(getSession(ws)!).cut
                if (!cut) break
                if (st.point || st.normal) actions.setCutPlane(st.point ?? cut.point, st.normal ?? cut.normal)
                if (st.keep) actions.setCutKeep(st.keep)
                if (st.split) actions.setCutSplit(st.split)
                if (st.colors) {
                    actions.setCutColor(0, st.colors[0])
                    actions.setCutColor(1, st.colors[1])
                }
                break
            }
        }
    }
    const sel = shot.select
    if (sel === 'model') select(ws, {kind: 'model'})
    else if (sel === 'cut') select(ws, {kind: 'cut'})
    else if (sel?.startsWith('part:')) {
        const p = docOf(getSession(ws)!).parts[Number(sel.slice(5))]
        if (p) select(ws, {kind: 'part', id: p.id})
    }
    if (shot.tool) setTool(ws, shot.tool)
}

function useRawGeometry(meshPath: string): {geometry: BufferGeometry | null; loading: boolean; error: string} {
    const [state, setState] = useState(() => ({path: meshPath, geometry: cachedRawMesh(meshPath) ?? null, loading: false, error: ''}))
    useEffect(() => {
        let alive = true
        const hit = cachedRawMesh(meshPath)
        if (hit) {
            setState({path: meshPath, geometry: hit, loading: false, error: ''})
            return
        }
        setState({path: meshPath, geometry: null, loading: true, error: ''})
        loadRawMesh(meshPath)
            .then((g) => alive && setState({path: meshPath, geometry: g, loading: false, error: ''}))
            .catch((e) => alive && setState({path: meshPath, geometry: null, loading: false, error: errorText(e)}))
        return () => {
            alive = false
        }
    }, [meshPath])
    return state.path === meshPath ? state : {geometry: null, loading: true, error: ''}
}

export default function MeshEditor({
    workspaceId,
    bed,
    swatches,
    shot,
    onApplied,
}: {
    workspaceId: number
    bed: BedVolume
    swatches: string[]
    shot?: MeshEditShot
    /** Se aplicó: el checkpoint nuevo que dejó Go (el que hay que elegir). */
    onApplied: (file: string) => void
}) {
    const t = useT()
    const M = t.meshEdit
    const theme = useThemeColors()
    const s = useSession(workspaceId)
    const {geometry, loading, error} = useRawGeometry(s?.meshPath ?? '')
    const actions = useMemo(() => editActions(workspaceId, t, geometry), [workspaceId, t, geometry])
    const handleRef = useRef<PaneHandle | null>(null)
    const root = useRef<HTMLDivElement | null>(null)
    const [stack, stackRef] = useNarrow(STACK_BELOW)
    const [compact, compactRef] = useNarrow(COMPACT_BELOW)
    const setRoot = useCallback(
        (el: HTMLDivElement | null) => {
            root.current = el
            stackRef(el)
            compactRef(el)
        },
        [stackRef, compactRef],
    )
    const [tab, setTab] = useState<SideTab>(shot?.panel ?? 'props')
    const [applyOpen, setApplyOpen] = useState(false)
    const [codeOpen, setCodeOpen] = useState(false)
    const [busy, setBusy] = useState(false)

    // El foco entra al editor: así sus atajos (⌘Z, W/E/R, Supr…) funcionan sin tener que hacer clic antes.
    useEffect(() => {
        root.current?.focus({preventScroll: true})
    }, [])

    // Banco de capturas: una sola vez, cuando la malla ya está.
    const ran = useRef(false)
    useEffect(() => {
        if (!shot || ran.current || !geometry || !s || s.entries.length > 0) return
        ran.current = true
        runShot(workspaceId, shot, actions)
    }, [shot, geometry, s, workspaceId, actions])

    // Se encuadra el modelo con su pose inicial, una vez.
    const fitBox = useMemo(() => {
        if (!geometry || !s) return undefined
        const b = boundsOf(geometry, tfMatrix(s.base.model))
        return b.isEmpty() ? undefined : b
        // La pose base de una sesión no cambia.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [geometry, s?.meshPath])

    if (!s) return null
    const ws = workspaceId
    const doc = docOf(s)
    const dirty = isDirty(s)

    const discard = async () => {
        if (dirty && !(await confirmDialog(fmt(M.discardConfirmN, {n: s.index}), M.discardOk, M.keepEditing, {danger: true}))) return
        endSession(ws)
    }

    const startApply = async () => {
        if (doc.parts.some((p) => p.shape === 'text' && !p.text.trim())) {
            toast.error(M.emptyText)
            return
        }
        if (needsClosedMesh(doc) && !s.facts.watertight && !(await confirmDialog(M.openApplyConfirm, M.openApplyOk, M.keepEditing))) return
        setApplyOpen(true)
    }
    const doApply = async (label: string) => {
        setBusy(true)
        try {
            const file = await ApplyMeshEdit(ws, s.parent, label, toScene(s))
            endSession(ws)
            setApplyOpen(false)
            toast.ok(fmt(M.applied, {file}))
            onApplied(file)
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setBusy(false)
        }
    }

    const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
        const el = e.target as HTMLElement
        const typing = el.matches('input, textarea, select, [contenteditable="true"]')
        const key = e.key.toLowerCase()
        if (isMod(e) && key === 'z') {
            // En un campo con un borrador sin confirmar, ⌘Z es el deshacer del texto; en el resto, el de la pila.
            if (typing && el.hasAttribute('data-dirty')) return
            e.preventDefault()
            if (e.shiftKey) redo(ws)
            else undo(ws)
            return
        }
        if (isMod(e) && !isMac() && key === 'y') {
            e.preventDefault()
            redo(ws)
            return
        }
        if (typing || isMod(e) || e.altKey) return
        const view: Record<string, ViewName> = {'1': 'front', '2': 'back', '3': 'left', '4': 'right', '5': 'top', '6': 'bottom', '7': 'iso'}
        if (key === 'w') setTool(ws, 'translate')
        else if (key === 'e') setTool(ws, 'rotate')
        else if (key === 'r') setTool(ws, 'scale')
        else if (key === 'f') handleRef.current?.fit()
        else if (view[key]) handleRef.current?.setView(view[key])
        else if ((key === 'delete' || key === 'backspace') && s.sel?.kind === 'part') actions.removePart(s.sel.id)
        else if (key === 'escape') {
            if (s.tool === 'face') setTool(ws, 'translate')
            else void discard()
        } else return
        e.preventDefault()
        e.stopPropagation()
    }

    return (
        <div ref={setRoot} tabIndex={-1} onKeyDown={onKeyDown} data-meshedit-root className="flex h-full min-h-0 min-w-0 flex-col bg-surface outline-none">
            <EditToolbar session={s} actions={actions} ready={!!geometry} compact={compact} />
            <div className={cn('flex min-h-0 flex-1', stack && 'flex-col')}>
                <EditViewport
                    session={s}
                    actions={actions}
                    geometry={geometry}
                    loading={loading}
                    loadError={error}
                    bed={bed}
                    theme={theme}
                    fitBox={fitBox}
                    handleRef={handleRef}
                    initialView={shot?.view}
                    dense={stack}
                />
                <SidePanel
                    session={s}
                    actions={actions}
                    swatches={swatches}
                    tab={tab}
                    onTab={setTab}
                    tabbed={stack}
                    className={stack ? 'h-[40%] min-h-[132px] shrink-0 border-t border-line' : 'w-[272px] shrink-0 border-l border-line'}
                />
            </div>
            <div data-meshedit-footer className="flex min-h-9 shrink-0 flex-wrap items-center gap-x-2 gap-y-1 border-t border-line bg-surface px-2 py-1">
                <span className="min-w-0 truncate font-mono text-ui-10-5 text-muted" title={M.footer.changesHint}>
                    {dirty ? fmt(M.footer.changes, {n: s.index}) : M.footer.noChanges}
                </span>
                <div className="ml-auto flex flex-wrap items-center justify-end gap-1.5">
                    <Button title={M.footer.codeHint} disabled={!dirty} onClick={() => setCodeOpen(true)} data-testid="view-code">
                        <Code2 className="icon-ui-14" />
                        {M.footer.code}
                    </Button>
                    <Button title={M.footer.aiHint} onClick={() => void modifyWithAI(ws, s.file, t)} data-testid="edit-ai">
                        <WandSparkles className="icon-ui-14" />
                        {M.footer.ai}
                    </Button>
                    <Button title={M.footer.discardHint} onClick={() => void discard()} data-testid="discard">
                        <Trash2 className="icon-ui-14" />
                        {M.footer.discard}
                    </Button>
                    <Button variant="primary" title={dirty ? M.footer.applyHint : M.footer.applyNone} disabled={!dirty || !geometry} onClick={() => void startApply()} data-testid="apply">
                        {busy ? <Loader2 className="icon-ui-14 animate-spin" /> : <GitBranchPlus className="icon-ui-14" />}
                        {M.footer.apply}
                    </Button>
                </div>
            </div>
            <ApplyDialog open={applyOpen} parent={s.parent} busy={busy} onCancel={() => !busy && setApplyOpen(false)} onApply={(l) => void doApply(l)} />
            <CodeDialog open={codeOpen} session={s} onClose={() => setCodeOpen(false)} />
        </div>
    )
}
