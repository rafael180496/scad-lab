import {useEffect, useRef, useState} from 'react'
import {Cuboid, ListTodo, TerminalSquare} from 'lucide-react'
import type {render, store} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {counts} from '../../lib/problems'
import {useProblems} from '../../hooks/useProblems'
import {LAYOUT_DEFAULTS, LAYOUT_LIMITS} from '../../hooks/useLayout'
import {RenderPanel} from '../render/RenderPanel'
import {EditorPane, type EditorShot} from '../editor/EditorPane'
import {ProblemsPanel} from '../problems/ProblemsPanel'
import {Pane, PhaseBadge, Placeholder} from './Pane'
import {Splitter} from './Splitter'
import type {BedVolume} from '../viewer/ViewPane'
import type {ViewerShot} from '../viewer/Viewer'

export type BottomTab = 'agent' | 'problems'

// Lo mínimo que se le deja al visor al agrandar los otros paneles.
const VIEWER_MIN_W = 360
const VIEWER_MIN_H = 200

function Tab({active, title, onClick, children}: {active: boolean; title: string; onClick: () => void; children: React.ReactNode}) {
    return (
        <button
            type="button"
            role="tab"
            aria-selected={active}
            title={title}
            onClick={onClick}
            className={cn(
                'relative flex h-8 items-center gap-1.5 px-2.5 font-mono text-[10.5px] tracking-[0.12em] uppercase transition-colors duration-100',
                active ? 'text-fg' : 'text-faint hover:text-muted',
            )}
        >
            {children}
            {active ? <span className="absolute inset-x-2 bottom-0 h-0.5 rounded-full bg-accent" /> : null}
        </button>
    )
}

// Panel inferior: el agente de IA (S5) y los problemas del checkpoint abierto.
function BottomPanel({
    tab,
    onTab,
    file,
    renderDiags,
    style,
}: {
    tab: BottomTab
    onTab: (t: BottomTab) => void
    file: string | null
    renderDiags: render.Diagnostic[] | undefined
    style?: React.CSSProperties
}) {
    const t = useT()
    const problems = useProblems(file ? renderDiags : undefined)
    const c = counts(file ? problems : [])
    const n = c.error + c.warning + c.info
    return (
        <section style={style} className="flex min-h-0 shrink-0 flex-col overflow-hidden rounded-[var(--radius-panel)] border border-line bg-surface">
            <div role="tablist" className="flex h-8 shrink-0 items-center border-b border-line pr-2 pl-1">
                <Tab active={tab === 'agent'} title={t.layout.agentTabHint} onClick={() => onTab('agent')}>
                    <TerminalSquare className="size-3.5" />
                    {t.panes.terminal}
                </Tab>
                <Tab active={tab === 'problems'} title={t.layout.problemsTabHint} onClick={() => onTab('problems')}>
                    <ListTodo className="size-3.5" />
                    {t.problems.title}
                    {n > 0 ? (
                        <span
                            className={cn(
                                'rounded-full px-1.5 py-px text-[10px] tracking-normal',
                                c.error > 0 ? 'bg-danger/15 text-danger' : c.warning > 0 ? 'bg-warn/15 text-warn' : 'bg-info/15 text-info',
                            )}
                        >
                            {n}
                        </span>
                    ) : null}
                </Tab>
                {tab === 'agent' ? (
                    <span className="ml-auto">
                        <PhaseBadge text={fmt(t.app.phase, {phase: 'S5'})} title={t.app.inDevelopment} />
                    </span>
                ) : null}
            </div>
            <div className="min-h-0 flex-1">
                {tab === 'agent' ? (
                    <div className="flex h-full items-center bg-surface-2">
                        <Placeholder text={t.panes.terminalBody} />
                    </div>
                ) : (
                    <ProblemsPanel file={file} problems={file ? problems : []} />
                )}
            </div>
        </section>
    )
}

// Área de trabajo: visor arriba a la izquierda, panel inferior (agente /
// problemas) debajo y código a la derecha, con divisores arrastrables.
export function Workbench({
    workspaceId,
    file,
    source,
    result,
    running,
    bed,
    swatches,
    settings,
    layout,
    bottomTab,
    onBottomTab,
    onLayoutPreview,
    onLayoutCommit,
    onSwatchesSaved,
    viewerShot,
    editorShot,
    onRender,
    onCancel,
    onOpenSettings,
    onSaved,
}: {
    workspaceId: number | null
    file: string | null
    source: string
    result: render.Result | undefined
    running: boolean
    bed: BedVolume
    swatches: string[]
    settings: store.Settings
    layout: store.Layout
    bottomTab: BottomTab
    onBottomTab: (t: BottomTab) => void
    onLayoutPreview: (patch: Partial<store.Layout>) => void
    onLayoutCommit: (patch: Partial<store.Layout>) => void
    onSwatchesSaved: (colors: string[]) => void
    viewerShot?: ViewerShot
    editorShot?: EditorShot
    onRender: () => void
    onCancel: () => void
    onOpenSettings: () => void
    onSaved: (fileName: string) => void
}) {
    const t = useT()
    const main = useRef<HTMLElement>(null)
    const [box, setBox] = useState({w: 1200, h: 800})
    useEffect(() => {
        const el = main.current
        if (!el) return
        const ro = new ResizeObserver(([e]) => setBox({w: e.contentRect.width, h: e.contentRect.height}))
        ro.observe(el)
        return () => ro.disconnect()
    }, [])

    // Los tamaños guardados se respetan mientras entren en la ventana: si se
    // achica, el visor conserva su mínimo y los otros ceden.
    const codeMax = Math.max(LAYOUT_LIMITS.code[0], Math.min(LAYOUT_LIMITS.code[1], box.w - VIEWER_MIN_W - 8))
    const bottomMax = Math.max(LAYOUT_LIMITS.bottom[0], Math.min(LAYOUT_LIMITS.bottom[1], box.h - VIEWER_MIN_H - 8))
    const codeWidth = Math.min(layout.codeWidth || LAYOUT_DEFAULTS.codeWidth, codeMax)
    const bottomHeight = Math.min(layout.bottomHeight || LAYOUT_DEFAULTS.bottomHeight, bottomMax)
    const renderDiags = result?.diagnostics

    return (
        <main ref={main} className="flex min-h-0 min-w-0 flex-1 p-2">
            <div className="flex min-w-0 flex-1 flex-col">
                <Pane icon={<Cuboid className="size-3.5" />} title={t.panes.viewport} className="flex-1">
                    <RenderPanel
                        file={file}
                        result={result}
                        running={running}
                        bed={bed}
                        swatches={swatches}
                        onSwatchesSaved={onSwatchesSaved}
                        viewerShot={viewerShot}
                        dimsCollapsed={layout.dimsCollapsed}
                        onDimsCollapsed={(v) => onLayoutCommit({dimsCollapsed: v})}
                        onRender={onRender}
                        onCancel={onCancel}
                        onOpenSettings={onOpenSettings}
                    />
                </Pane>
                <Splitter
                    orientation="horizontal"
                    size={bottomHeight}
                    min={LAYOUT_LIMITS.bottom[0]}
                    max={bottomMax}
                    invert
                    title={t.layout.resizeBottom}
                    onResize={(v) => onLayoutPreview({bottomHeight: v})}
                    onCommit={(v) => onLayoutCommit({bottomHeight: v})}
                    onReset={() => onLayoutCommit({bottomHeight: LAYOUT_DEFAULTS.bottomHeight})}
                />
                <BottomPanel tab={bottomTab} onTab={onBottomTab} file={file} renderDiags={renderDiags} style={{height: bottomHeight}} />
            </div>
            <Splitter
                orientation="vertical"
                size={codeWidth}
                min={LAYOUT_LIMITS.code[0]}
                max={codeMax}
                invert
                title={t.layout.resizeCode}
                onResize={(v) => onLayoutPreview({codeWidth: v})}
                onCommit={(v) => onLayoutCommit({codeWidth: v})}
                onReset={() => onLayoutCommit({codeWidth: LAYOUT_DEFAULTS.codeWidth})}
            />
            <EditorPane
                workspaceId={workspaceId}
                file={file}
                source={source}
                renderDiags={renderDiags}
                settings={settings}
                className="shrink-0"
                style={{width: codeWidth}}
                onSaved={onSaved}
                shot={editorShot}
            />
        </main>
    )
}
