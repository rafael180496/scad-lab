import {useState, type ReactNode} from 'react'
import {
    ArrowDownToLine,
    Box,
    Circle,
    Crosshair,
    Cylinder,
    FlipHorizontal2,
    Magnet,
    Move3d,
    MousePointerClick,
    Redo2,
    Rotate3d,
    Scale3d,
    Scissors,
    Type,
    Undo2,
} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import type {Shape} from '../../lib/meshEditMath'
import {canRedo, canUndo, docOf, redo, select, setSnap, setTool, undo, type PartMode, type Session, type Tool} from '../../lib/meshEditSession'
import {withKeys} from '../../lib/platform'
import type {EditActions} from './actions'

// La barra del modo «Editar»: herramienta del gizmo, ajuste a pasos, operaciones del modelo, corte, piezas
// nuevas y deshacer/rehacer. Los rótulos se omiten en un panel angosto (queda el título como nombre) y, si ni
// así cabe, la barra envuelve: nunca corta un control a la mitad.

export function Btn({
    on,
    title,
    onClick,
    disabled,
    label,
    children,
    tone,
    testid,
}: {
    on?: boolean
    title: string
    onClick: () => void
    disabled?: boolean
    label?: string
    children: ReactNode
    tone?: 'danger'
    testid?: string
}) {
    return (
        <button
            type="button"
            title={title}
            aria-label={title}
            aria-pressed={on === undefined ? undefined : on}
            disabled={disabled}
            data-testid={testid}
            onClick={onClick}
            className={cn(
                'inline-flex h-6 min-w-6 shrink-0 items-center justify-center gap-1 rounded px-1.5 font-mono text-ui-10-5 whitespace-nowrap transition-colors duration-100 disabled:pointer-events-none disabled:opacity-40',
                on ? 'bg-accent-soft text-accent' : tone === 'danger' ? 'text-danger hover:bg-surface-2' : 'text-muted hover:bg-surface-2 hover:text-fg',
            )}
        >
            {children}
            {label}
        </button>
    )
}

export function Sep() {
    return <span className="mx-0.5 h-4 w-px shrink-0 bg-line" />
}

const SHAPE_ICON: Record<Shape, typeof Box> = {cube: Box, cylinder: Cylinder, sphere: Circle, text: Type}
const SHAPES: Shape[] = ['cube', 'cylinder', 'sphere', 'text']

export function EditToolbar({session, actions, ready, compact}: {session: Session; actions: EditActions; ready: boolean; compact: boolean}) {
    const t = useT()
    const M = t.meshEdit
    const ws = session.workspaceId
    const doc = docOf(session)
    const [addMode, setAddMode] = useState<PartMode>('union')
    const lbl = (s: string) => (compact ? undefined : s)

    const tool = (id: Tool, key: string, icon: ReactNode, name: string, hint: string) => (
        <Btn key={id} on={session.tool === id} title={`${hint} (${key})`} label={lbl(name)} onClick={() => setTool(ws, id)} testid={`tool-${id}`}>
            {icon}
        </Btn>
    )

    return (
        <div data-meshedit-toolbar className="flex min-h-8 shrink-0 flex-wrap items-center gap-x-0.5 gap-y-0.5 border-b border-line bg-surface px-2 py-1">
            {tool('translate', 'W', <Move3d className="icon-ui-14" />, M.tools.translate, M.tools.translateHint)}
            {tool('rotate', 'E', <Rotate3d className="icon-ui-14" />, M.tools.rotate, M.tools.rotateHint)}
            {tool('scale', 'R', <Scale3d className="icon-ui-14" />, M.tools.scale, M.tools.scaleHint)}
            <Btn
                on={session.snap.on}
                title={session.snap.on ? M.snap.onHint : M.snap.offHint}
                onClick={() => setSnap(ws, {on: !session.snap.on})}
                testid="snap-toggle"
            >
                <Magnet className="icon-ui-14" />
            </Btn>
            <Sep />
            <div className="flex shrink-0 items-center" role="group" aria-label={M.mirror.group}>
                {([0, 1, 2] as const).map((a) => (
                    <Btn key={a} on={doc.model.mirror[a]} title={fmt(M.mirror.hint, {axis: ['X', 'Y', 'Z'][a]})} disabled={!ready} onClick={() => actions.mirror(a)} testid={`mirror-${a}`}>
                        {a === 0 ? <FlipHorizontal2 className="icon-ui-14" /> : null}
                        {['X', 'Y', 'Z'][a]}
                    </Btn>
                ))}
            </div>
            <Btn title={M.center.hint} disabled={!ready} label={lbl(M.center.label)} onClick={actions.center} testid="center">
                <Crosshair className="icon-ui-14" />
            </Btn>
            <Btn title={M.rest.hint} disabled={!ready} label={lbl(M.rest.label)} onClick={actions.rest} testid="rest">
                <ArrowDownToLine className="icon-ui-14" />
            </Btn>
            <Btn on={session.tool === 'face'} title={M.face.hint} disabled={!ready} label={lbl(M.face.label)} onClick={() => setTool(ws, session.tool === 'face' ? 'translate' : 'face')} testid="tool-face">
                <MousePointerClick className="icon-ui-14" />
            </Btn>
            <Sep />
            <Btn
                on={session.sel?.kind === 'cut'}
                title={doc.cut ? M.cut.selectHint : M.cut.addHint}
                disabled={!ready}
                label={lbl(M.cut.label)}
                onClick={() => (doc.cut ? select(ws, {kind: 'cut'}) : actions.addCut())}
                testid="cut"
            >
                <Scissors className="icon-ui-14" />
            </Btn>
            <Sep />
            <div className="flex shrink-0 items-center rounded bg-surface-2 p-px" role="group" aria-label={M.add.modeGroup}>
                {(['union', 'hole'] as const).map((m) => (
                    <button
                        key={m}
                        type="button"
                        title={m === 'union' ? M.add.unionHint : M.add.holeHint}
                        aria-pressed={addMode === m}
                        onClick={() => setAddMode(m)}
                        className={cn(
                            'inline-flex h-5 items-center rounded-[5px] px-1.5 font-mono text-ui-10-5 transition-colors duration-100',
                            addMode === m ? (m === 'hole' ? 'bg-danger/20 text-danger' : 'bg-accent-soft text-accent') : 'text-muted hover:text-fg',
                        )}
                    >
                        {M.mode[m]}
                    </button>
                ))}
            </div>
            {SHAPES.map((sh) => {
                const Icon = SHAPE_ICON[sh]
                return (
                    <Btn key={sh} title={fmt(M.add.hint, {shape: M.shape[sh].toLowerCase(), mode: M.modeAs[addMode]})} disabled={!ready} onClick={() => actions.addPart(sh, addMode)} testid={`add-${sh}`}>
                        <Icon className="icon-ui-14" />
                    </Btn>
                )
            })}
            <div className="ml-auto flex shrink-0 items-center gap-0.5 pl-2">
                <Btn title={withKeys(M.undo, 'mod+z')} disabled={!canUndo(session)} onClick={() => undo(ws)} testid="undo">
                    <Undo2 className="icon-ui-14" />
                </Btn>
                <Btn title={withKeys(M.redo, 'mod+shift+z')} disabled={!canRedo(session)} onClick={() => redo(ws)} testid="redo">
                    <Redo2 className="icon-ui-14" />
                </Btn>
            </div>
        </div>
    )
}
