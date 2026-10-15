import {lazy, Suspense, useEffect, useState} from 'react'
import {createPortal} from 'react-dom'
import type {render, store} from '../../../wailsjs/go/models'
import {useT} from '../../i18n'
import {counts} from '../../lib/problems'
import {onBeforeGoToLine} from '../../lib/editorSession'
import {useProblems} from '../../hooks/useProblems'
import {useTerminals} from '../../hooks/useTerminals'
import {EditorPane, type EditorShot} from '../editor/EditorPane'
import {ProblemsPanel} from '../problems/ProblemsPanel'
import {RenderPanel} from '../render/RenderPanel'
import {AgentTerminalTab} from '../terminal/AgentTerminalTab'
import type {BedVolume} from '../viewer/ViewPane'
import type {ViewerShot} from '../viewer/Viewer'
import {dockState, panelStatus, showPanel} from './dock'
import {panelHostEl} from './hosts'
import {PANEL_IDS, type PanelId} from './panels'

// El chat (S5) entra con lazy(): trae un renderizador de markdown y la app abre
// sin cargarlo.
const ChatPanel = lazy(() => import('../chat/ChatPanel'))

export type PanelHostProps = {
    workspaceId: number | null
    file: string | null
    source: string
    result: render.Result | undefined
    running: boolean
    bed: BedVolume
    swatches: string[]
    settings: store.Settings
    dimsCollapsed: boolean
    onDimsCollapsed: (v: boolean) => void
    onSwatchesSaved: (colors: string[]) => void
    viewerShot?: ViewerShot
    editorShot?: EditorShot
    onRender: () => void
    onCancel: () => void
    onOpenSettings: () => void
    onOpenAISettings: () => void
    onAutoIterateChange: (v: boolean) => void
    onSaved: (fileName: string) => void
}

// PanelHost dibuja el contenido de cada panel acoplable. Vive FUERA de dockview
// (que solo reparte huecos): cada panel se renderiza una vez, con un portal, en
// su propio elemento persistente (hosts.ts), y dockview lo cuelga del grupo que
// toque. Por eso mover, dividir, flotar u ocultar un panel no desmonta nada: el
// visor conserva su contexto WebGL y la cámara, Monaco su modelo y su deshacer,
// y la terminal su xterm. Un panel se monta la primera vez que se muestra y
// desde ahí queda vivo (oculto, el elemento está aparcado fuera de pantalla).
export function PanelHost(p: PanelHostProps) {
    const t = useT()
    const present = dockState.use((s) => s.present)
    const visible = dockState.use((s) => s.visible)
    const [seen, setSeen] = useState<ReadonlySet<PanelId>>(() => new Set())
    useEffect(() => {
        const fresh = PANEL_IDS.filter((id) => present[id] && !seen.has(id))
        if (fresh.length) setSeen(new Set([...seen, ...fresh]))
    }, [present, seen])

    // Un clic en un diagnóstico lleva a una línea del editor: si su panel está
    // oculto, se muestra primero.
    useEffect(() => {
        onBeforeGoToLine(() => {
            if (!dockState.get().present.source) showPanel('source')
        })
        return () => onBeforeGoToLine(null)
    }, [])

    const problems = useProblems(p.file ? p.result?.diagnostics : undefined)
    const c = counts(p.file ? problems : [])
    useEffect(() => panelStatus.set({errors: c.error, warnings: c.warning, infos: c.info}), [c.error, c.warning, c.info])

    const terms = useTerminals(p.workspaceId, p.settings.autoIterate, p.onAutoIterateChange)
    const [a1, a2] = terms.slots
    useEffect(() => {
        const dot = (s: typeof a1) => (!s ? 'none' : s.ended == null ? 'running' : 'ended')
        panelStatus.set({agent1: dot(a1), agent2: dot(a2)})
    }, [a1, a2])
    const agentVisible = visible['agent-1'] || visible['agent-2']
    const {refreshAgents} = terms
    useEffect(() => {
        if (agentVisible) refreshAgents()
    }, [agentVisible, refreshAgents])

    const agent = (slot: 0 | 1, id: 'agent-1' | 'agent-2') => (
        <AgentTerminalTab
            visible={visible[id]}
            workspaceId={p.workspaceId}
            term={terms.slots[slot]}
            busy={terms.busy === slot}
            agents={terms.agents}
            defaultAgent={p.settings.defaultAgent}
            autoIterate={terms.autoIterate}
            onAutoIterate={(v) => void terms.setAutoIterate(v)}
            onStart={(agentId, mode, sessionId) => void terms.start(slot, agentId, mode, sessionId)}
            onClose={(termId) => void terms.close(termId)}
            onOpenSettings={p.onOpenAISettings}
        />
    )

    const body = (id: PanelId) => {
        switch (id) {
            case 'viewport':
                return (
                    <RenderPanel
                        file={p.file}
                        result={p.result}
                        running={p.running}
                        bed={p.bed}
                        swatches={p.swatches}
                        onSwatchesSaved={p.onSwatchesSaved}
                        viewerShot={p.viewerShot}
                        dimsCollapsed={p.dimsCollapsed}
                        onDimsCollapsed={p.onDimsCollapsed}
                        onRender={p.onRender}
                        onCancel={p.onCancel}
                        onOpenSettings={p.onOpenSettings}
                    />
                )
            case 'source':
                return (
                    <EditorPane
                        workspaceId={p.workspaceId}
                        file={p.file}
                        source={p.source}
                        renderDiags={p.result?.diagnostics}
                        settings={p.settings}
                        onSaved={p.onSaved}
                        shot={p.editorShot}
                    />
                )
            case 'agent-1':
                return agent(0, 'agent-1')
            case 'agent-2':
                return agent(1, 'agent-2')
            case 'chat':
                return (
                    <Suspense fallback={<p className="px-4 py-3 text-ui-12 text-faint">{t.app.loading}</p>}>
                        <ChatPanel
                            workspaceId={p.workspaceId}
                            currentFile={p.file}
                            onCheckpointCreated={p.onSaved}
                            onOpenSettings={p.onOpenAISettings}
                        />
                    </Suspense>
                )
            case 'problems':
                return <ProblemsPanel file={p.file} problems={p.file ? problems : []} />
        }
    }

    return (
        <>
            {PANEL_IDS.filter((id) => seen.has(id)).map((id) =>
                createPortal(
                    <div key={id} className="h-full min-h-0 min-w-0 bg-surface text-fg">
                        {body(id)}
                    </div>,
                    panelHostEl(id),
                    id,
                ),
            )}
        </>
    )
}
