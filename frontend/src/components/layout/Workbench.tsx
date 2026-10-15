import {lazy, Suspense, useEffect, useRef, useState} from 'react'
import {Cuboid, ListTodo, Maximize2, MessageSquare, Minimize2, SquareTerminal} from 'lucide-react'
import type {render, store} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {counts} from '../../lib/problems'
import {useProblems} from '../../hooks/useProblems'
import {useTerminals} from '../../hooks/useTerminals'
import {LAYOUT_DEFAULTS, LAYOUT_LIMITS} from '../../hooks/useLayout'
import {RenderPanel} from '../render/RenderPanel'
import {EditorPane, type EditorShot} from '../editor/EditorPane'
import {ProblemsPanel} from '../problems/ProblemsPanel'
import {AgentTerminalTab} from '../terminal/AgentTerminalTab'
import {IconButton} from '../ui/Button'
import {Pane} from './Pane'
import {Splitter} from './Splitter'
import type {BedVolume} from '../viewer/ViewPane'
import type {ViewerShot} from '../viewer/Viewer'

// 'agent' es la pestaña «Agente 1» (se conserva el nombre: lo usan App y el
// banco de capturas) y 'agent2' la segunda terminal de agente.
export type BottomTab = 'agent' | 'agent2' | 'chat' | 'problems'

// El chat (S5) entra con lazy(): trae un renderizador de markdown y la app abre
// sin cargarlo.
const ChatPanel = lazy(() => import('../chat/ChatPanel'))

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

// Panel inferior: las dos terminales de agente de IA (S5), el chat con modelos
// y los problemas del checkpoint abierto.
function BottomPanel({
    tab,
    onTab,
    workspaceId,
    file,
    renderDiags,
    settings,
    maximized,
    onMaximized,
    onAutoIterateChange,
    onCheckpointCreated,
    onOpenAISettings,
    style,
}: {
    tab: BottomTab
    onTab: (t: BottomTab) => void
    workspaceId: number | null
    file: string | null
    renderDiags: render.Diagnostic[] | undefined
    settings: store.Settings
    maximized: boolean
    onMaximized: (v: boolean) => void
    onAutoIterateChange?: (v: boolean) => void
    onCheckpointCreated: (fileName: string) => void
    onOpenAISettings: () => void
    style?: React.CSSProperties
}) {
    const t = useT()
    const problems = useProblems(file ? renderDiags : undefined)
    const c = counts(file ? problems : [])
    const n = c.error + c.warning + c.info
    const terms = useTerminals(workspaceId, settings.autoIterate, onAutoIterateChange)
    // Las pestañas que ya se abrieron siguen montadas (ocultas): la terminal
    // conserva su pantalla y el chat su conversación en curso.
    const [chatSeen, setChatSeen] = useState(tab === 'chat')
    useEffect(() => {
        if (tab === 'chat') setChatSeen(true)
        if (tab === 'agent' || tab === 'agent2') terms.refreshAgents()
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [tab])

    const agentTab = (slot: 0 | 1, id: BottomTab) => {
        const term = terms.slots[slot]
        return (
            <Tab active={tab === id} title={fmt(t.terminal.agentTabHint, {n: slot + 1})} onClick={() => onTab(id)}>
                <SquareTerminal className="size-3.5" />
                {fmt(t.terminal.agentTab, {n: slot + 1})}
                {term ? <span className={cn('size-1.5 rounded-full', term.ended == null ? 'bg-ok' : 'bg-faint')} /> : null}
            </Tab>
        )
    }
    const agentBody = (slot: 0 | 1, id: BottomTab) => (
        <div className={cn('h-full', tab !== id && 'hidden')}>
            <AgentTerminalTab
                visible={tab === id}
                workspaceId={workspaceId}
                term={terms.slots[slot]}
                busy={terms.busy === slot}
                agents={terms.agents}
                defaultAgent={settings.defaultAgent}
                autoIterate={terms.autoIterate}
                onAutoIterate={(v) => void terms.setAutoIterate(v)}
                onStart={(agentId, mode, sessionId) => void terms.start(slot, agentId, mode, sessionId)}
                onClose={(termId) => void terms.close(termId)}
                onOpenSettings={onOpenAISettings}
            />
        </div>
    )

    return (
        <section
            style={maximized ? undefined : style}
            className={cn('flex min-h-0 flex-col overflow-hidden rounded-[var(--radius-panel)] border border-line bg-surface', maximized ? 'flex-1' : 'shrink-0')}
        >
            <div role="tablist" className="flex h-8 shrink-0 items-center border-b border-line pr-1 pl-1">
                {agentTab(0, 'agent')}
                {agentTab(1, 'agent2')}
                <Tab active={tab === 'chat'} title={t.terminal.chatTabHint} onClick={() => onTab('chat')}>
                    <MessageSquare className="size-3.5" />
                    {t.terminal.chatTab}
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
                <IconButton
                    className="ml-auto size-6"
                    title={maximized ? t.terminal.restoreHint : t.terminal.maximizeHint}
                    aria-pressed={maximized}
                    onClick={() => onMaximized(!maximized)}
                >
                    {maximized ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}
                </IconButton>
            </div>
            <div className="min-h-0 flex-1">
                {agentBody(0, 'agent')}
                {agentBody(1, 'agent2')}
                {chatSeen ? (
                    <div className={cn('h-full', tab !== 'chat' && 'hidden')}>
                        <Suspense fallback={<p className="px-4 py-3 text-[12px] text-faint">{t.app.loading}</p>}>
                            <ChatPanel
                                workspaceId={workspaceId}
                                currentFile={file}
                                onCheckpointCreated={onCheckpointCreated}
                                onOpenSettings={onOpenAISettings}
                            />
                        </Suspense>
                    </div>
                ) : null}
                {tab === 'problems' ? <ProblemsPanel file={file} problems={file ? problems : []} /> : null}
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
    onOpenAISettings,
    onAutoIterateChange,
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
    /** Abre Configuración → IA (si falta, se usa onOpenSettings). */
    onOpenAISettings?: () => void
    /** Avisa que se encendió o apagó la auto-iteración, para que App actualice `settings`. */
    onAutoIterateChange?: (v: boolean) => void
    onSaved: (fileName: string) => void
}) {
    const t = useT()
    const main = useRef<HTMLElement>(null)
    const [box, setBox] = useState({w: 1200, h: 800})
    // Maximizado: el panel inferior ocupa el lugar del visor (que queda oculto,
    // no desmontado: conserva la cámara).
    const [maximized, setMaximized] = useState(false)
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
                <Pane icon={<Cuboid className="size-3.5" />} title={t.panes.viewport} className={cn('flex-1', maximized && 'hidden')}>
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
                {maximized ? null : (
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
                )}
                <BottomPanel
                    tab={bottomTab}
                    onTab={onBottomTab}
                    workspaceId={workspaceId}
                    file={file}
                    renderDiags={renderDiags}
                    settings={settings}
                    maximized={maximized}
                    onMaximized={setMaximized}
                    onAutoIterateChange={onAutoIterateChange}
                    onCheckpointCreated={onSaved}
                    onOpenAISettings={onOpenAISettings ?? onOpenSettings}
                    style={{height: bottomHeight}}
                />
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
