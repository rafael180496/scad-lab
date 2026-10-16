import {useRef} from 'react'
import {Box, Camera, CircleHelp, Columns2, Grid3x3, Maximize, Shapes} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {VIEWS, type ViewName} from './camera'

function Toggle({
    on,
    title,
    onClick,
    children,
}: {
    on: boolean
    title: string
    onClick: () => void
    children: React.ReactNode
}) {
    return (
        <button
            type="button"
            title={title}
            aria-label={title}
            aria-pressed={on}
            onClick={onClick}
            className={cn(
                'inline-flex h-6 min-w-6 shrink-0 items-center justify-center gap-1 rounded px-1.5 font-mono text-ui-10-5 transition-colors duration-100',
                on ? 'bg-accent-soft text-accent' : 'text-muted hover:bg-surface-2 hover:text-fg',
            )}
        >
            {children}
        </button>
    )
}

function Sep() {
    return <span className="mx-0.5 h-4 w-px shrink-0 bg-line" />
}

export function ViewerToolbar({
    wireframe,
    edges,
    ortho,
    split,
    colors,
    colorIndex,
    modelColors,
    onModelColors,
    onView,
    onWireframe,
    onEdges,
    onOrtho,
    onFit,
    onColor,
    onEditColor,
    onCapture,
    onSplit,
}: {
    wireframe: boolean
    edges: boolean
    ortho: boolean
    split: boolean
    colors: string[]
    colorIndex: number
    /** true = colores del modelo, false = muestra; null = el modelo no define colores (no se ofrece). */
    modelColors: boolean | null
    onModelColors: (v: boolean) => void
    onView: (v: ViewName) => void
    onWireframe: () => void
    onEdges: () => void
    onOrtho: () => void
    onFit: () => void
    onColor: (i: number) => void
    onEditColor: (i: number, color: string) => void
    onCapture: () => void
    onSplit: () => void
}) {
    const t = useT()
    const picker = useRef<HTMLInputElement>(null)
    const editing = useRef(0)

    // El selector nativo de color se abre sobre un <input type=color> oculto:
    // así cada muestra se ve como un punto y no como el control del sistema.
    const edit = (i: number) => {
        const el = picker.current
        if (!el) return
        editing.current = i
        el.value = colors[i]
        el.click()
    }

    return (
        <div className="flex min-h-8 shrink-0 flex-wrap items-center gap-x-0.5 gap-y-0.5 border-b border-line bg-surface px-2 py-1">
            <div className="flex flex-wrap items-center gap-0.5">
                {VIEWS.map((v, i) => (
                    <button
                        key={v}
                        type="button"
                        title={fmt(t.viewer.viewHint, {view: t.viewer.viewNames[v], key: i + 1})}
                        onClick={() => onView(v)}
                        className="inline-flex h-6 shrink-0 items-center rounded px-1 font-mono text-ui-10-5 tracking-wide text-muted uppercase transition-colors duration-100 hover:bg-surface-2 hover:text-fg"
                    >
                        {t.viewer.views[v]}
                    </button>
                ))}
            </div>
            <Sep />
            <Toggle on={wireframe} title={wireframe ? t.viewer.wireframeOff : t.viewer.wireframeOn} onClick={onWireframe}>
                <Grid3x3 className="icon-ui-14" />
            </Toggle>
            <Toggle on={edges} title={edges ? t.viewer.edgesOff : t.viewer.edgesOn} onClick={onEdges}>
                <Shapes className="icon-ui-14" />
            </Toggle>
            <Toggle on={ortho} title={ortho ? t.viewer.toPersp : t.viewer.toOrtho} onClick={onOrtho}>
                <Box className="icon-ui-14" />
                {ortho ? t.viewer.ortho : t.viewer.persp}
            </Toggle>
            <Toggle on={false} title={t.viewer.fit} onClick={onFit}>
                <Maximize className="icon-ui-14" />
            </Toggle>
            <Sep />
            {modelColors === null ? null : (
                <div className="flex shrink-0 items-center rounded bg-surface-2 p-px" role="group" aria-label={t.viewer.paintMode}>
                    <button
                        type="button"
                        title={t.viewer.modelColorsHint}
                        aria-pressed={modelColors}
                        onClick={() => onModelColors(true)}
                        className={cn(
                            'inline-flex h-5 items-center rounded-[5px] px-1.5 font-mono text-ui-10-5 transition-colors duration-100',
                            modelColors ? 'bg-accent-soft text-accent' : 'text-muted hover:text-fg',
                        )}
                    >
                        {t.viewer.modelColors}
                    </button>
                    <button
                        type="button"
                        title={t.viewer.swatchColorsHint}
                        aria-pressed={!modelColors}
                        onClick={() => onModelColors(false)}
                        className={cn(
                            'inline-flex h-5 items-center rounded-[5px] px-1.5 font-mono text-ui-10-5 transition-colors duration-100',
                            !modelColors ? 'bg-accent-soft text-accent' : 'text-muted hover:text-fg',
                        )}
                    >
                        {t.viewer.swatchColors}
                    </button>
                </div>
            )}
            <div className={cn('flex shrink-0 items-center gap-1 px-0.5', modelColors ? 'opacity-50' : '')}>
                {colors.map((c, i) => (
                    <button
                        key={i}
                        type="button"
                        title={fmt(t.viewer.swatchHint, {color: c})}
                        aria-label={fmt(t.viewer.swatchHint, {color: c})}
                        aria-pressed={i === colorIndex}
                        onClick={() => onColor(i)}
                        onDoubleClick={() => edit(i)}
                        onContextMenu={(e) => {
                            e.preventDefault()
                            edit(i)
                        }}
                        style={{backgroundColor: c}}
                        className={cn(
                            'size-4 shrink-0 rounded-full border transition-shadow duration-100',
                            i === colorIndex ? 'border-fg ring-2 ring-accent/60' : 'border-line-strong hover:ring-2 hover:ring-line-strong',
                        )}
                    />
                ))}
                <input
                    ref={picker}
                    type="color"
                    tabIndex={-1}
                    title={t.viewer.swatchEdit}
                    aria-hidden
                    className="pointer-events-none absolute size-0 opacity-0"
                    onChange={(e) => onEditColor(editing.current, e.target.value)}
                />
            </div>
            <div className="ml-auto flex shrink-0 items-center gap-0.5 pl-2">
                <Toggle on={false} title={t.viewer.capture} onClick={onCapture}>
                    <Camera className="icon-ui-14" />
                </Toggle>
                <Toggle on={split} title={split ? t.viewer.splitOff : t.viewer.splitOn} onClick={onSplit}>
                    <Columns2 className="icon-ui-14" />
                </Toggle>
                <span
                    className="inline-flex icon-ui-24 cursor-help items-center justify-center text-faint"
                    title={t.viewer.help}
                    aria-label={t.viewer.help}
                    role="img"
                >
                    <CircleHelp className="icon-ui-14" />
                </span>
            </div>
        </div>
    )
}
