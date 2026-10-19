import {useRef, useState, type ReactNode} from 'react'
import {Copy, FlipVertical2, Pipette, Trash2} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {cleanNum, cleanVec, type Tf, type Vec3} from '../../lib/meshEditMath'
import {docOf, jump, partName, setSnap, type Cut, type CutKeep, type CutSplit, type Part, type Session} from '../../lib/meshEditSession'
import type {EditActions} from './actions'
import {liveStore} from './live'
import {NumField} from './NumField'
import {Btn} from './EditToolbar'

// Panel lateral del modo «Editar»: Propiedades (lo que se puede escribir del objeto elegido) y Operaciones (la
// pila, con deshacer a cualquier paso). Es una columna a la derecha del visor o, si el panel es angosto, un cajón
// debajo de él.

export type SideTab = 'props' | 'ops'

function Section({title, children, hint}: {title: string; children: ReactNode; hint?: string}) {
    return (
        <div className="flex flex-col gap-1.5 border-b border-line px-3 py-2.5">
            <span className="font-mono text-ui-10 tracking-[0.12em] text-faint uppercase" title={hint}>
                {title}
            </span>
            {children}
        </div>
    )
}

const AXES = ['X', 'Y', 'Z'] as const

function Triple({
    values,
    onCommit,
    titles,
    step = 1,
    digits = 2,
    min,
    disabled,
}: {
    values: Vec3
    onCommit: (i: 0 | 1 | 2, v: number) => void
    titles: [string, string, string]
    step?: number
    digits?: number
    min?: number
    disabled?: boolean
}) {
    return (
        <div className="grid grid-cols-3 gap-1.5">
            {([0, 1, 2] as const).map((i) => (
                <NumField key={i} label={AXES[i]} value={values[i]} title={titles[i]} step={step} digits={digits} min={min} disabled={disabled} onCommit={(v) => onCommit(i, v)} />
            ))}
        </div>
    )
}

function ColorRow({value, swatches, onPick, none}: {value: string; swatches: string[]; onPick: (c: string) => void; none?: {label: string; hint: string}}) {
    const t = useT()
    const input = useRef<HTMLInputElement>(null)
    const lower = value.toLowerCase()
    return (
        <div className="flex flex-wrap items-center gap-1.5">
            {swatches.map((c) => (
                <button
                    key={c}
                    type="button"
                    title={fmt(t.meshEdit.props.swatchHint, {color: c})}
                    aria-label={fmt(t.meshEdit.props.swatchHint, {color: c})}
                    aria-pressed={lower === c.toLowerCase()}
                    onClick={() => onPick(c.toLowerCase())}
                    style={{backgroundColor: c}}
                    className={cn('size-4 shrink-0 rounded-full border transition-shadow duration-100', lower === c.toLowerCase() ? 'border-fg ring-2 ring-accent/60' : 'border-line-strong hover:ring-2 hover:ring-line-strong')}
                />
            ))}
            <button
                type="button"
                title={t.meshEdit.props.customColor}
                aria-label={t.meshEdit.props.customColor}
                onClick={() => input.current?.click()}
                className="inline-flex size-5 shrink-0 items-center justify-center rounded-full border border-line-strong text-muted transition-colors duration-100 hover:text-fg"
            >
                <Pipette className="icon-ui-12" />
            </button>
            <input ref={input} type="color" tabIndex={-1} aria-hidden title={t.meshEdit.props.customColor} value={value || '#9ca3af'} onChange={(e) => onPick(e.target.value.toLowerCase())} className="pointer-events-none absolute size-0 opacity-0" />
            {none ? (
                <button
                    type="button"
                    title={none.hint}
                    aria-pressed={value === ''}
                    onClick={() => onPick('')}
                    className={cn('h-5 rounded px-1.5 font-mono text-ui-10-5 transition-colors duration-100', value === '' ? 'bg-accent-soft text-accent' : 'text-muted hover:bg-surface-2 hover:text-fg')}
                >
                    {none.label}
                </button>
            ) : null}
        </div>
    )
}

function Seg<T extends string>({value, options, onChange, group}: {value: T; options: {id: T; label: string; hint: string}[]; onChange: (v: T) => void; group: string}) {
    return (
        <div className="flex w-full items-center rounded bg-surface-2 p-px" role="group" aria-label={group}>
            {options.map((o) => (
                <button
                    key={o.id}
                    type="button"
                    title={o.hint}
                    aria-pressed={value === o.id}
                    onClick={() => onChange(o.id)}
                    className={cn('inline-flex h-5 min-w-0 flex-1 items-center justify-center rounded-[5px] px-1 font-mono text-ui-10-5 transition-colors duration-100', value === o.id ? 'bg-accent-soft text-accent' : 'text-muted hover:text-fg')}
                >
                    <span className="truncate">{o.label}</span>
                </button>
            ))}
        </div>
    )
}

/** Cambia un componente de un vector de tres. */
const withAt = (v: Vec3, i: number, n: number): Vec3 => v.map((x, k) => (k === i ? n : x)) as Vec3

// ── Propiedades del modelo y de las piezas (misma pose) ────────────────────────

function PoseSections({
    tf,
    sizeLabelled,
    onTf,
    uniform,
    onUniform,
}: {
    tf: Tf
    sizeLabelled?: {values: Vec3; onSize: (i: 0 | 1 | 2, v: number) => void}
    onTf: (tf: Tf, kind: 'move' | 'rotate' | 'scale') => void
    uniform: boolean
    onUniform: (v: boolean) => void
}) {
    const t = useT()
    const P = t.meshEdit.props
    return (
        <>
            <Section title={P.position}>
                <Triple values={tf.translate} titles={[P.moveX, P.moveY, P.moveZ]} step={1} onCommit={(i, v) => onTf({...tf, translate: withAt(tf.translate, i, v)}, 'move')} />
            </Section>
            <Section title={P.rotation}>
                <Triple values={tf.rotate} titles={[P.rotX, P.rotY, P.rotZ]} step={15} onCommit={(i, v) => onTf({...tf, rotate: withAt(tf.rotate, i, v)}, 'rotate')} />
            </Section>
            <Section title={P.scale} hint={P.scaleHint}>
                <Triple
                    values={tf.scale}
                    titles={[P.scaleX, P.scaleY, P.scaleZ]}
                    step={0.1}
                    digits={3}
                    min={0.001}
                    onCommit={(i, v) => {
                        // Uniforme: los tres ejes siguen al que se editó (proporcional a lo que valía).
                        const k = uniform && tf.scale[i] > 0 ? v / tf.scale[i] : 1
                        const scale: Vec3 = uniform ? [cleanNum(tf.scale[0] * k, 5), cleanNum(tf.scale[1] * k, 5), cleanNum(tf.scale[2] * k, 5)] : withAt(tf.scale, i, v)
                        onTf({...tf, scale}, 'scale')
                    }}
                />
                <label className="flex cursor-pointer items-center gap-1.5 text-ui-11 text-muted" title={P.uniformHint}>
                    <input type="checkbox" checked={uniform} onChange={(e) => onUniform(e.target.checked)} title={P.uniformHint} className="accent-[var(--c-accent)]" />
                    {P.uniform}
                </label>
            </Section>
            {sizeLabelled ? (
                <Section title={P.size} hint={P.sizeHint}>
                    <Triple
                        values={sizeLabelled.values}
                        titles={[P.sizeX, P.sizeY, P.sizeZ]}
                        step={1}
                        min={0.01}
                        onCommit={sizeLabelled.onSize}
                    />
                </Section>
            ) : null}
        </>
    )
}

function ModelProps({session, actions, swatches, uniform, onUniform}: {session: Session; actions: EditActions; swatches: string[]; uniform: boolean; onUniform: (v: boolean) => void}) {
    const t = useT()
    const P = t.meshEdit.props
    const doc = docOf(session)
    const live = liveStore.use((s) => s.tf)
    const tf = live ?? doc.model
    const raw = session.facts.size
    return (
        <>
            <PoseSections
                tf={tf}
                uniform={uniform}
                onUniform={onUniform}
                onTf={(next, kind) => actions.applyTf({kind: 'model'}, next, kind)}
                sizeLabelled={{
                    values: [raw[0] * tf.scale[0], raw[1] * tf.scale[1], raw[2] * tf.scale[2]],
                    onSize: (i, v) => {
                        // Escribir una medida final: la escala del eje (o de los tres, si es uniforme) sale de dividir.
                        if (raw[i] <= 0) return
                        const s = v / raw[i]
                        const k = uniform ? s / tf.scale[i] : 1
                        const scale: Vec3 = uniform ? [cleanNum(tf.scale[0] * k, 5), cleanNum(tf.scale[1] * k, 5), cleanNum(tf.scale[2] * k, 5)] : withAt(tf.scale, i, cleanNum(s, 5))
                        actions.applyTf({kind: 'model'}, {...tf, scale}, 'scale')
                    },
                }}
            />
            <Section title={P.color}>
                <ColorRow value={doc.color} swatches={swatches} onPick={actions.setModelColor} />
            </Section>
        </>
    )
}

function PartProps({session, part, actions, swatches, uniform, onUniform}: {session: Session; part: Part; actions: EditActions; swatches: string[]; uniform: boolean; onUniform: (v: boolean) => void}) {
    const t = useT()
    const M = t.meshEdit
    const P = M.props
    const doc = docOf(session)
    const live = liveStore.use((s) => s.tf)
    const tf = live ?? part.tf
    const sizeFields: {i: 0 | 1 | 2; title: string; label: string}[] =
        part.shape === 'cube'
            ? [
                  {i: 0, title: P.sizeX, label: 'X'},
                  {i: 1, title: P.sizeY, label: 'Y'},
                  {i: 2, title: P.sizeZ, label: 'Z'},
              ]
            : part.shape === 'cylinder'
              ? [
                    {i: 0, title: P.diameter, label: 'Ø'},
                    {i: 2, title: P.height, label: 'H'},
                ]
              : part.shape === 'sphere'
                ? [{i: 0, title: P.diameter, label: 'Ø'}]
                : [
                      {i: 0, title: P.textSize, label: 'T'},
                      {i: 2, title: P.extrusion, label: 'H'},
                  ]
    const setSize = (i: number, v: number) => {
        // El cilindro y la esfera son redondos: el diámetro vive en los dos primeros lugares.
        const size: Vec3 = part.shape === 'cylinder' || part.shape === 'sphere' ? (i === 0 ? [v, v, part.size[2]] : [part.size[0], part.size[1], v]) : withAt(part.size, i, v)
        actions.setPartSize(part.id, size)
    }
    return (
        <>
            <Section title={M.mode.group}>
                <Seg
                    group={M.mode.group}
                    value={part.mode}
                    onChange={(m) => actions.setPartMode(part.id, m)}
                    options={[
                        {id: 'union', label: M.mode.union, hint: M.add.unionHint},
                        {id: 'hole', label: M.mode.hole, hint: M.add.holeHint},
                    ]}
                />
                <div className="flex items-center gap-0.5">
                    <Btn title={M.props.duplicate} label={M.props.duplicateLabel} onClick={() => actions.duplicatePart(part.id)}>
                        <Copy className="icon-ui-12" />
                    </Btn>
                    <Btn title={M.props.remove} label={M.props.removeLabel} tone="danger" onClick={() => actions.removePart(part.id)} testid="part-remove">
                        <Trash2 className="icon-ui-12" />
                    </Btn>
                </div>
            </Section>
            <Section title={P.shape}>
                {part.shape === 'text' ? (
                    <input
                        type="text"
                        value={part.text}
                        title={P.textHint}
                        aria-label={P.textHint}
                        maxLength={200}
                        onChange={(e) => actions.setPartText(part.id, e.target.value)}
                        onKeyDown={(e) => e.stopPropagation()}
                        className="selectable h-6 w-full min-w-0 rounded border border-line bg-surface-2 px-1.5 text-ui-12 text-fg outline-none transition-colors focus:border-accent"
                    />
                ) : null}
                <div className={cn('grid gap-1.5', sizeFields.length === 3 ? 'grid-cols-3' : sizeFields.length === 2 ? 'grid-cols-2' : 'grid-cols-1')}>
                    {sizeFields.map((f) => (
                        <NumField key={f.i} label={f.label} value={part.size[f.i]} title={f.title} step={1} min={0.01} onCommit={(v) => setSize(f.i, v)} />
                    ))}
                </div>
            </Section>
            <PoseSections tf={tf} uniform={uniform} onUniform={onUniform} onTf={(next, kind) => actions.applyTf({kind: 'part', id: part.id}, next, kind)} />
            {part.mode === 'union' ? (
                <Section title={P.color}>
                    <ColorRow value={part.color} swatches={swatches} onPick={(c) => actions.setPartColor(part.id, c)} none={{label: P.sameAsModel, hint: P.sameAsModelHint}} />
                </Section>
            ) : (
                <Section title={P.holeSection}>
                    <p className="text-ui-11 leading-relaxed text-muted">{P.holeNote}</p>
                </Section>
            )}
        </>
    )
}

function CutProps({session, cut, actions, swatches}: {session: Session; cut: Cut; actions: EditActions; swatches: string[]}) {
    const t = useT()
    const M = t.meshEdit
    const P = M.props
    const livePoint = liveStore.use((s) => s.cutPoint)
    const liveNormal = liveStore.use((s) => s.cutNormal)
    void session
    const point = livePoint ?? cut.point
    const normal = liveNormal ?? cut.normal
    const setNormal = (n: Vec3) => {
        const len = Math.hypot(...n)
        if (len < 1e-6) return
        actions.setCutPlane(cut.point, cleanVec([n[0] / len, n[1] / len, n[2] / len], 5))
    }
    const keeps: {id: CutKeep; label: string; hint: string}[] = [
        {id: 'above', label: M.keep.above, hint: M.keep.aboveHint},
        {id: 'below', label: M.keep.below, hint: M.keep.belowHint},
        {id: 'both', label: M.keep.both, hint: M.keep.bothHint},
    ]
    const splits: {id: CutSplit; label: string; hint: string}[] = [
        {id: 'objects', label: M.split.objects, hint: M.split.objectsHint},
        {id: 'colors', label: M.split.colors, hint: M.split.colorsHint},
    ]
    return (
        <>
            <Section title={P.cutPoint}>
                <Triple values={point} titles={[P.moveX, P.moveY, P.moveZ]} onCommit={(i, v) => actions.setCutPlane(withAt(cut.point, i, v), cut.normal)} />
            </Section>
            <Section title={P.cutNormal} hint={P.cutNormalHint}>
                <Triple values={normal} titles={[P.normalX, P.normalY, P.normalZ]} step={0.1} digits={3} onCommit={(i, v) => setNormal(withAt(cut.normal, i, v))} />
                <div className="flex flex-wrap items-center gap-0.5">
                    <Btn title={P.planeHorizontalHint} label={P.planeHorizontal} onClick={() => setNormal([0, 0, 1])}>
                        {null}
                    </Btn>
                    <Btn title={P.planeXHint} label={P.planeX} onClick={() => setNormal([1, 0, 0])}>
                        {null}
                    </Btn>
                    <Btn title={P.planeYHint} label={P.planeY} onClick={() => setNormal([0, 1, 0])}>
                        {null}
                    </Btn>
                    <Btn title={P.planeFlipHint} label={P.planeFlip} onClick={() => setNormal([-cut.normal[0], -cut.normal[1], -cut.normal[2]])}>
                        <FlipVertical2 className="icon-ui-12" />
                    </Btn>
                </div>
            </Section>
            <Section title={M.keep.group}>
                <Seg group={M.keep.group} value={cut.keep} options={keeps} onChange={actions.setCutKeep} />
                {cut.keep === 'both' ? (
                    <>
                        <Seg group={M.split.group} value={cut.split} options={splits} onChange={actions.setCutSplit} />
                        {cut.split === 'objects' ? <p className="text-ui-11 leading-relaxed text-muted">{M.split.objectsNote}</p> : null}
                    </>
                ) : (
                    <p className="text-ui-11 leading-relaxed text-muted">{cut.keep === 'above' ? M.keep.aboveNote : M.keep.belowNote}</p>
                )}
            </Section>
            {cut.keep === 'both' && cut.split === 'colors' ? (
                <Section title={P.cutColors}>
                    <span className="text-ui-11 text-muted">{M.keep.above}</span>
                    <ColorRow value={cut.colors[0]} swatches={swatches} onPick={(c) => actions.setCutColor(0, c)} none={{label: P.sameAsModel, hint: P.sameAsModelHint}} />
                    <span className="text-ui-11 text-muted">{M.keep.below}</span>
                    <ColorRow value={cut.colors[1]} swatches={swatches} onPick={(c) => actions.setCutColor(1, c)} none={{label: P.sameAsModel, hint: P.sameAsModelHint}} />
                </Section>
            ) : null}
            <Section title={P.cutSection}>
                <div>
                    <Btn title={P.cutRemove} label={P.cutRemoveLabel} tone="danger" onClick={actions.removeCut} testid="cut-remove">
                        <Trash2 className="icon-ui-12" />
                    </Btn>
                </div>
            </Section>
        </>
    )
}

function SnapSection({session}: {session: Session}) {
    const t = useT()
    const P = t.meshEdit.props
    const ws = session.workspaceId
    const sn = session.snap
    return (
        <Section title={P.snap} hint={P.snapHint}>
            <label className="flex cursor-pointer items-center gap-1.5 text-ui-11 text-muted" title={P.snapHint}>
                <input type="checkbox" checked={sn.on} onChange={(e) => setSnap(ws, {on: e.target.checked})} title={P.snapHint} className="accent-[var(--c-accent)]" />
                {P.snapOn}
            </label>
            <div className="grid grid-cols-3 gap-1.5">
                <NumField label="mm" value={sn.move} title={P.snapMove} step={0.5} digits={3} min={0.001} disabled={!sn.on} onCommit={(v) => setSnap(ws, {move: v})} />
                <NumField label="°" value={sn.rotate} title={P.snapRotate} step={5} digits={2} min={0.1} max={180} disabled={!sn.on} onCommit={(v) => setSnap(ws, {rotate: v})} />
                <NumField label="×" value={sn.scale} title={P.snapScale} step={0.05} digits={3} min={0.001} disabled={!sn.on} onCommit={(v) => setSnap(ws, {scale: v})} />
            </div>
        </Section>
    )
}

export function PropsPanel({session, actions, swatches}: {session: Session; actions: EditActions; swatches: string[]}) {
    const t = useT()
    const M = t.meshEdit
    const doc = docOf(session)
    const [uniform, setUniform] = useState(true)
    const sel = session.sel
    const part = sel?.kind === 'part' ? doc.parts.find((p) => p.id === sel.id) : undefined
    const name = !sel ? M.nothingSelected : sel.kind === 'model' ? M.theModelCap : sel.kind === 'cut' ? M.theCutCap : part ? `${partName(t, doc.parts, part.id)} · ${M.mode[part.mode].toLowerCase()}` : ''
    return (
        <div className="flex flex-col" data-testid="props">
            <div className="border-b border-line px-3 py-2">
                <span className="text-ui-12-5 font-medium text-fg">{name}</span>
            </div>
            {!sel ? <p className="px-3 py-3 text-ui-11-5 leading-relaxed text-muted">{M.props.pickHint}</p> : null}
            {sel?.kind === 'model' ? <ModelProps session={session} actions={actions} swatches={swatches} uniform={uniform} onUniform={setUniform} /> : null}
            {part ? <PartProps session={session} part={part} actions={actions} swatches={swatches} uniform={uniform} onUniform={setUniform} /> : null}
            {sel?.kind === 'cut' && doc.cut ? <CutProps session={session} cut={doc.cut} actions={actions} swatches={swatches} /> : null}
            <SnapSection session={session} />
        </div>
    )
}

// ── La pila de operaciones ────────────────────────────────────────────────────

export function OpsPanel({session}: {session: Session}) {
    const t = useT()
    const M = t.meshEdit
    const ws = session.workspaceId
    return (
        <div className="flex flex-col" data-testid="ops">
            <ol className="flex flex-col py-1">
                <li>
                    <button
                        type="button"
                        title={M.ops.initialHint}
                        aria-current={session.index === 0}
                        onClick={() => jump(ws, 0)}
                        className={cn('flex w-full items-baseline gap-2 px-3 py-1 text-left text-ui-12 transition-colors duration-100 hover:bg-surface-2', session.index === 0 ? 'text-fg' : 'text-muted')}
                    >
                        <span className="w-5 shrink-0 text-right font-mono text-ui-10-5 text-faint">0</span>
                        <span className="min-w-0 flex-1">{M.ops.initial}</span>
                    </button>
                </li>
                {session.entries.map((e, i) => {
                    const applied = i < session.index
                    const current = i === session.index - 1
                    return (
                        <li key={i}>
                            <button
                                type="button"
                                title={fmt(M.ops.goTo, {n: i + 1})}
                                aria-current={current}
                                onClick={() => jump(ws, i + 1)}
                                className={cn(
                                    'flex w-full items-baseline gap-2 px-3 py-1 text-left text-ui-12 transition-colors duration-100 hover:bg-surface-2',
                                    current ? 'bg-accent-soft text-fg' : applied ? 'text-fg' : 'text-faint line-through decoration-faint/50',
                                )}
                            >
                                <span className={cn('w-5 shrink-0 text-right font-mono text-ui-10-5', current ? 'text-accent' : 'text-faint')}>{i + 1}</span>
                                <span className="min-w-0 flex-1 leading-snug break-words">{e.label}</span>
                            </button>
                        </li>
                    )
                })}
            </ol>
            {session.entries.length === 0 ? <p className="px-3 pb-3 text-ui-11-5 leading-relaxed text-faint">{M.ops.empty}</p> : null}
            <p className="px-3 pb-3 text-ui-11 leading-relaxed text-faint">{M.ops.intro}</p>
            {session.index < session.entries.length ? <p className="px-3 pb-3 text-ui-11-5 leading-relaxed text-faint">{M.ops.redoNote}</p> : null}
        </div>
    )
}

function PaneTitle({children, title}: {children: ReactNode; title: string}) {
    return (
        <div className="flex h-7 shrink-0 items-center border-b border-line px-3" title={title}>
            <span className="font-mono text-ui-10-5 tracking-[0.1em] text-muted uppercase">{children}</span>
        </div>
    )
}

/**
 * Propiedades y Operaciones. En una columna ancha van las dos a la vez (propiedades arriba y, debajo, la pila: es lo
 * que se mira mientras se edita); en un cajón angosto, en pestañas, porque no caben juntas.
 */
export function SidePanel({session, actions, swatches, tab, onTab, tabbed, className}: {session: Session; actions: EditActions; swatches: string[]; tab: SideTab; onTab: (t: SideTab) => void; tabbed: boolean; className?: string}) {
    const t = useT()
    const M = t.meshEdit
    if (!tabbed) {
        return (
            <aside data-meshedit-side className={cn('flex min-h-0 min-w-0 flex-col bg-surface', className)}>
                <PaneTitle title={M.tabs.propsHint}>{M.tabs.props}</PaneTitle>
                <div data-lock="x" className="min-h-0 flex-1 overflow-x-clip overflow-y-auto">
                    <PropsPanel session={session} actions={actions} swatches={swatches} />
                </div>
                <div className="flex h-[30%] min-h-[96px] shrink-0 flex-col border-t border-line">
                    <PaneTitle title={M.tabs.opsHint}>{fmt(M.tabs.ops, {n: session.index})}</PaneTitle>
                    <div data-lock="x" className="min-h-0 flex-1 overflow-x-clip overflow-y-auto">
                        <OpsPanel session={session} />
                    </div>
                </div>
            </aside>
        )
    }
    return (
        <aside data-meshedit-side className={cn('flex min-h-0 min-w-0 flex-col bg-surface', className)}>
            <div className="flex h-8 shrink-0 items-stretch border-b border-line" role="tablist" aria-label={M.tabs.group}>
                {(['props', 'ops'] as const).map((id) => (
                    <button
                        key={id}
                        type="button"
                        role="tab"
                        aria-selected={tab === id}
                        title={id === 'props' ? M.tabs.propsHint : M.tabs.opsHint}
                        onClick={() => onTab(id)}
                        data-testid={`tab-${id}`}
                        className={cn(
                            'relative flex-1 px-2 font-mono text-ui-10-5 tracking-[0.1em] uppercase transition-colors duration-100',
                            tab === id ? 'text-fg after:absolute after:inset-x-2 after:bottom-0 after:h-0.5 after:bg-accent' : 'text-muted hover:text-fg',
                        )}
                    >
                        {id === 'props' ? M.tabs.props : fmt(M.tabs.ops, {n: session.index})}
                    </button>
                ))}
            </div>
            <div data-lock="x" className="min-h-0 flex-1 overflow-x-clip overflow-y-auto">
                {tab === 'props' ? <PropsPanel session={session} actions={actions} swatches={swatches} /> : <OpsPanel session={session} />}
            </div>
        </aside>
    )
}
