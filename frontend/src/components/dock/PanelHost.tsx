import {lazy, Suspense, useEffect, useState} from 'react'
import {createPortal} from 'react-dom'
import type {render, store} from '../../../wailsjs/go/models'
import {useT} from '../../i18n'
import {counts} from '../../lib/problems'
import {onBeforeGoToLine} from '../../lib/editorSession'
import {useProblems} from '../../hooks/useProblems'
import type {useTerminals} from '../../hooks/useTerminals'
import {useMcpStatus} from '../../hooks/useMcpStatus'
import {usePrintSession, type PrintParams} from '../../lib/printSession'
import {EditorPane, type EditorShot, type EditorSnap} from '../editor/EditorPane'
import {PrintPanel} from '../print/PrintPanel'
import {ProblemsPanel} from '../problems/ProblemsPanel'
import {RenderPanel} from '../render/RenderPanel'
import {AgentTerminalTab} from '../terminal/AgentTerminalTab'
import type {BedVolume} from '../viewer/ViewPane'
import type {ViewerShot} from '../viewer/Viewer'
import type {MeshEditShot} from '../meshedit/MeshEditor'
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
    /** Las terminales de agente de todos los proyectos abiertos (las lleva App; ver useTerminals). */
    terms: ReturnType<typeof useTerminals>
    /** Estado del editor que dejó el proyecto al irse a segundo plano; se aplica una vez al volver. */
    editorRestore: EditorSnap | null
    onEditorRestored: () => void
    onSwatchesSaved: (colors: string[]) => void
    onViewerSaved: (v: store.ViewerState) => void
    viewerShot?: ViewerShot
    /** Banco de capturas: el modo «Editar» (S13) con unas operaciones ya hechas. */
    meshEditShot?: MeshEditShot
    editorShot?: EditorShot
    onRender: () => void
    onCancel: () => void
    onOpenSettings: () => void
    onOpenAISettings: () => void
    onOpenPrintSettings: () => void
    onSaved: (fileName: string) => void
    /** Un checkpoint nació en el chat (botón, herramienta o guardado automático): abrirlo y renderizarlo. */
    onChatCheckpoint: (fileName: string) => void
    /** El proyecto no tiene ningún checkpoint: el visor y Código ofrecen «Crear un modelo de inicio». */
    emptyWorkspace: boolean
    /** La última versión del checkpoint elegido la dejó la IA (S16). */
    editedByAI: boolean
    /** Por qué no se puede exportar ('' = se puede) y cómo abrir el diálogo de 3MF. */
    exportWhy: string
    onExport3mf: () => void
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

    // Modelo por partes y estimación de impresión (S7): se piden mientras se vea
    // el visor o el panel «Impresión». Los parámetros salen de Configuración.
    const meshPath = p.result?.status === 'ok' && p.result.meshPath ? p.result.meshPath : ''
    const base: PrintParams = {
        infillPercent: p.settings.infillPercent,
        wallLoops: p.settings.wallLoops,
        lineWidthMm: p.settings.lineWidthMm,
        filamentDiameterMm: p.settings.filamentDiameterMm,
        defaultMaterialId: p.settings.defaultMaterial,
        currency: p.settings.currency,
    }
    usePrintSession({
        workspaceId: p.workspaceId,
        file: p.file,
        meshPath,
        enabled: visible.viewport || visible.print,
        base,
        bed: p.bed,
    })

    const problems = useProblems(p.file ? p.result?.diagnostics : undefined)
    const c = counts(p.file ? problems : [])
    useEffect(() => panelStatus.set({errors: c.error, warnings: c.warning, infos: c.info}), [c.error, c.warning, c.info])

    const {status: mcp} = useMcpStatus(p.workspaceId)
    const terms = p.terms
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
            allTerms={terms.all[slot]}
            busy={terms.busy === slot}
            agents={terms.agents}
            defaultAgent={p.settings.defaultAgent}
            autoIterate={terms.autoIterate}
            onAutoIterate={(v) => void terms.setAutoIterate(v)}
            mcp={mcp}
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
                        workspaceId={p.workspaceId}
                        file={p.file}
                        result={p.result}
                        running={p.running}
                        bed={p.bed}
                        swatches={p.swatches}
                        onSwatchesSaved={p.onSwatchesSaved}
                        viewerShot={p.viewerShot}
                        dimsCollapsed={p.dimsCollapsed}
                        onDimsCollapsed={p.onDimsCollapsed}
                        viewerSaved={p.settings.viewer}
                        onViewerSaved={p.onViewerSaved}
                        onRender={p.onRender}
                        onCancel={p.onCancel}
                        onOpenSettings={p.onOpenSettings}
                        editShot={p.meshEditShot}
                        onEditApplied={p.onSaved}
                        emptyWorkspace={p.emptyWorkspace}
                        editedByAI={p.editedByAI}
                        onStarter={p.onSaved}
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
                        emptyWorkspace={p.emptyWorkspace}
                        editedByAI={p.editedByAI}
                        restore={p.editorRestore}
                        onRestored={p.onEditorRestored}
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
                            onCheckpointCreated={p.onChatCheckpoint}
                            onOpenSettings={p.onOpenAISettings}
                        />
                    </Suspense>
                )
            case 'problems':
                return <ProblemsPanel workspaceId={p.workspaceId} file={p.file} problems={p.file ? problems : []} />
            case 'print':
                return (
                    <PrintPanel
                        file={p.file}
                        result={p.result}
                        running={p.running}
                        base={base}
                        exportWhy={p.exportWhy}
                        onExport3mf={p.onExport3mf}
                        onOpenSettings={p.onOpenPrintSettings}
                    />
                )
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
