import {useEffect, useRef, useState} from 'react'
import {FolderOpen, FolderPlus, Plus, RefreshCw, Sparkles, Trash2} from 'lucide-react'
import type {main, render, store} from '../../../wailsjs/go/models'
import {IconButton} from '../ui/Button'
import {CheckpointTree} from './CheckpointTree'
import {TrashButton} from './TrashButton'
import {Splitter} from '../layout/Splitter'
import {LAYOUT_LIMITS} from '../../hooks/useLayout'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'

// Dos rótulos de sección (32 px cada uno) y el filete que los separa.
const SIDEBAR_CHROME = 32 * 2 + 8
// Alto mínimo que conserva el árbol de checkpoints.
const CHECKPOINTS_MIN_H = 96

function SectionTitle({children, actions}: {children: React.ReactNode; actions?: React.ReactNode}) {
    return (
        <div className="flex h-8 shrink-0 items-center justify-between px-3">
            <span className="min-w-0 truncate font-mono text-ui-10-5 tracking-[0.12em] text-faint uppercase">{children}</span>
            <div className="flex shrink-0 items-center">{actions}</div>
        </div>
    )
}

export function Sidebar({
    workspaces,
    view,
    openIds,
    selected,
    onOpen,
    onNew,
    onImport,
    onRescan,
    onSyncSkills,
    onReveal,
    onRemove,
    onSelect,
    onToggle,
    onView,
    onCompare,
    shot,
    renders,
    running,
    width,
    workspacesHeight,
    onWorkspacesResize,
    onWorkspacesCommit,
    onWorkspacesReset,
}: {
    width: number
    /** Alto de la lista de workspaces; los checkpoints ocupan el resto. */
    workspacesHeight: number
    onWorkspacesResize: (h: number) => void
    onWorkspacesCommit: (h: number) => void
    onWorkspacesReset: () => void
    workspaces: store.Workspace[]
    view: main.WorkspaceView | null
    /** Workspaces abiertos como pestaña de proyecto. */
    openIds: number[]
    selected: string | null
    onOpen: (id: number) => void
    onNew: () => void
    onImport: () => void
    onRescan: () => void
    onSyncSkills: () => void
    onReveal: () => void
    onRemove: (w: store.Workspace) => void
    onSelect: (file: string) => void
    onToggle: (cp: store.Checkpoint) => void
    onView: (file: string) => void
    onCompare: (file: string) => void
    /** Solo el banco de capturas: menú del árbol y papelera abiertos. */
    shot?: {treeMenu?: string; trash?: boolean}
    renders: Record<string, render.Result>
    running: Record<string, boolean>
}) {
    const t = useT()
    const current = view?.workspace.id
    // El alto de la lista se acota al de la barra: los checkpoints conservan un
    // mínimo aunque la ventana sea baja (o la letra, grande).
    const box = useRef<HTMLElement>(null)
    const [boxH, setBoxH] = useState(600)
    useEffect(() => {
        const el = box.current
        if (!el) return
        const ro = new ResizeObserver(([e]) => setBoxH(e.contentRect.height))
        ro.observe(el)
        return () => ro.disconnect()
    }, [])
    const wsMax = Math.max(LAYOUT_LIMITS.workspaces[0], Math.min(LAYOUT_LIMITS.workspaces[1], boxH - SIDEBAR_CHROME - CHECKPOINTS_MIN_H))
    const wsHeight = Math.max(LAYOUT_LIMITS.workspaces[0], Math.min(workspacesHeight, wsMax))
    return (
        <aside ref={box} style={{width}} className="flex min-w-0 shrink-0 flex-col overflow-clip border-r border-line bg-surface">
            <SectionTitle
                actions={
                    <>
                        <IconButton title={t.workspaces.importHint} onClick={onImport}>
                            <FolderPlus className="icon-ui-14" />
                        </IconButton>
                        <IconButton title={t.workspaces.newHint} onClick={onNew}>
                            <Plus className="icon-ui-16" />
                        </IconButton>
                    </>
                }
            >
                {t.workspaces.title}
            </SectionTitle>
            <div style={{height: wsHeight}} className="shrink-0 overflow-auto px-1.5 pb-2">
                {workspaces.length === 0 ? (
                    <p className="px-1.5 py-1 text-ui-12 leading-relaxed text-faint">{t.workspaces.empty}</p>
                ) : (
                    workspaces.map((w) => (
                        <div
                            key={w.id}
                            className={cn(
                                'group flex h-8 items-center gap-2 rounded-md pr-1 pl-2 transition-colors duration-100',
                                w.id === current ? 'bg-surface-2 text-fg' : 'text-muted hover:bg-surface-2 hover:text-fg',
                            )}
                        >
                            <button
                                type="button"
                                title={openIds.includes(w.id) ? fmt(t.projects.goToHint, {name: w.name}) : fmt(t.projects.openHint, {name: w.name, path: w.path})}
                                onClick={() => onOpen(w.id)}
                                className="flex min-w-0 flex-1 items-center gap-2 text-left"
                            >
                                <span
                                    className={cn(
                                        'size-1.5 shrink-0 rounded-full',
                                        w.id === current ? 'bg-accent' : openIds.includes(w.id) ? 'bg-muted' : 'bg-line-strong',
                                    )}
                                />
                                <span className="truncate text-ui-12-5">{w.name}</span>
                            </button>
                            <IconButton
                                title={t.workspaces.remove}
                                onClick={() => onRemove(w)}
                                className="size-6 opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
                            >
                                <Trash2 className="icon-ui-14" />
                            </IconButton>
                        </div>
                    ))
                )}
            </div>

            <Splitter
                orientation="horizontal"
                size={wsHeight}
                min={LAYOUT_LIMITS.workspaces[0]}
                max={wsMax}
                title={t.layout.resizeWorkspaces}
                className="-my-[3px] h-[7px]"
                onResize={onWorkspacesResize}
                onCommit={onWorkspacesCommit}
                onReset={onWorkspacesReset}
            />
            <div className="h-px shrink-0 bg-line" />

            <SectionTitle
                actions={
                    view ? (
                        <>
                            {view.missing ? null : <TrashButton workspaceId={view.workspace.id} checkpoints={view.checkpoints} shotOpen={shot?.trash} />}
                            <IconButton title={t.workspaces.syncSkills} onClick={onSyncSkills}>
                                <Sparkles className="icon-ui-14" />
                            </IconButton>
                            <IconButton title={t.workspaces.reveal} onClick={onReveal}>
                                <FolderOpen className="icon-ui-14" />
                            </IconButton>
                            <IconButton title={t.workspaces.rescan} onClick={onRescan}>
                                <RefreshCw className="icon-ui-14" />
                            </IconButton>
                        </>
                    ) : null
                }
            >
                {t.checkpoints.title}
            </SectionTitle>
            <div className="min-h-0 flex-1 overflow-auto pb-3">
                {view ? (
                    view.missing ? (
                        <p className="px-3 text-ui-12 text-warn">{t.workspaces.missing}</p>
                    ) : (
                        <CheckpointTree
                            workspaceId={view.workspace.id}
                            checkpoints={view.checkpoints}
                            activeParent={view.activeParent}
                            renders={renders}
                            running={running}
                            selected={selected}
                            onSelect={onSelect}
                            onToggle={onToggle}
                            onView={onView}
                            onCompare={onCompare}
                            shotMenu={shot?.treeMenu}
                        />
                    )
                ) : (
                    <p className="px-3 text-ui-12 text-faint">{t.panes.noWorkspace}</p>
                )}
            </div>
        </aside>
    )
}
