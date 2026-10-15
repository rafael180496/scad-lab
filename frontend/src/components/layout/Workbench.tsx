import {Code2, Cuboid, TerminalSquare} from 'lucide-react'
import type {render} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {RenderPanel} from '../render/RenderPanel'

// Disposición final del área de trabajo: visor a la izquierda, código a la
// derecha y agente abajo. Cada panel muestra qué va a tener y en qué fase
// llega; el código ya se lee (solo lectura, sin resaltado hasta S4) y el
// visor ya renderiza con OpenSCAD y muestra el estado (la malla 3D, en S2).
function Pane({
    icon,
    title,
    phase,
    className,
    children,
}: {
    icon: React.ReactNode
    title: string
    phase?: string
    className?: string
    children: React.ReactNode
}) {
    const t = useT()
    return (
        <section className={cn('flex min-h-0 min-w-0 flex-col overflow-hidden rounded-[var(--radius-panel)] border border-line bg-surface', className)}>
            <div className="flex h-8 shrink-0 items-center gap-2 border-b border-line px-3">
                <span className="text-faint">{icon}</span>
                <span className="font-mono text-[10.5px] tracking-[0.12em] text-muted uppercase">{title}</span>
                {phase ? (
                    <span
                        className="ml-auto rounded border border-line-strong px-1.5 py-px font-mono text-[10px] text-faint"
                        title={t.app.inDevelopment}
                    >
                        {fmt(t.app.phase, {phase})}
                    </span>
                ) : null}
            </div>
            <div className="min-h-0 flex-1">{children}</div>
        </section>
    )
}

function Placeholder({text}: {text: string}) {
    return <p className="mx-auto max-w-[420px] px-6 text-center text-[12.5px] leading-relaxed text-muted">{text}</p>
}

export function Workbench({
    file,
    source,
    result,
    running,
    onRender,
    onCancel,
    onOpenSettings,
}: {
    file: string | null
    source: string
    result: render.Result | undefined
    running: boolean
    onRender: () => void
    onCancel: () => void
    onOpenSettings: () => void
}) {
    const t = useT()
    return (
        <main className="grid min-h-0 flex-1 grid-cols-[minmax(0,1.45fr)_minmax(320px,1fr)] grid-rows-[minmax(0,1fr)_220px] gap-2 p-2">
            <Pane icon={<Cuboid className="size-3.5" />} title={t.panes.viewport} phase="S2">
                <RenderPanel
                    file={file}
                    result={result}
                    running={running}
                    onRender={onRender}
                    onCancel={onCancel}
                    onOpenSettings={onOpenSettings}
                />
            </Pane>
            <Pane
                icon={<Code2 className="size-3.5" />}
                title={file ? fmt(t.panes.source, {file}) : t.panes.editor}
                phase="S4"
                className="row-span-2"
            >
                {file ? (
                    <pre className="selectable h-full overflow-auto p-3 font-mono text-[12px] leading-[1.55] text-fg">{source}</pre>
                ) : (
                    <div className="flex h-full items-center">
                        <Placeholder text={t.panes.editorBody} />
                    </div>
                )}
            </Pane>
            <Pane icon={<TerminalSquare className="size-3.5" />} title={t.panes.terminal} phase="S5">
                <div className="flex h-full items-center bg-surface-2">
                    <Placeholder text={t.panes.terminalBody} />
                </div>
            </Pane>
        </main>
    )
}
