import {FolderOpen, FolderPlus, Plus, RefreshCw, Sparkles, Trash2} from 'lucide-react'
import type {main, render, store} from '../../../wailsjs/go/models'
import {IconButton} from '../ui/Button'
import {CheckpointTree} from './CheckpointTree'
import {useT} from '../../i18n'
import {cn} from '../../lib/cn'

function SectionTitle({children, actions}: {children: React.ReactNode; actions?: React.ReactNode}) {
    return (
        <div className="flex h-8 shrink-0 items-center justify-between px-3">
            <span className="font-mono text-[10.5px] tracking-[0.12em] text-faint uppercase">{children}</span>
            <div className="flex items-center">{actions}</div>
        </div>
    )
}

export function Sidebar({
    workspaces,
    view,
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
    renders,
    running,
    width,
}: {
    width: number
    workspaces: store.Workspace[]
    view: main.WorkspaceView | null
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
    renders: Record<string, render.Result>
    running: Record<string, boolean>
}) {
    const t = useT()
    const current = view?.workspace.id
    return (
        <aside style={{width}} className="flex shrink-0 flex-col border-r border-line bg-surface">
            <SectionTitle
                actions={
                    <>
                        <IconButton title={t.workspaces.importHint} onClick={onImport}>
                            <FolderPlus className="size-3.5" />
                        </IconButton>
                        <IconButton title={t.workspaces.newHint} onClick={onNew}>
                            <Plus className="size-4" />
                        </IconButton>
                    </>
                }
            >
                {t.workspaces.title}
            </SectionTitle>
            <div className="max-h-[38%] shrink-0 overflow-auto px-1.5 pb-2">
                {workspaces.length === 0 ? (
                    <p className="px-1.5 py-1 text-[12px] leading-relaxed text-faint">{t.workspaces.empty}</p>
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
                                title={w.path}
                                onClick={() => onOpen(w.id)}
                                className="flex min-w-0 flex-1 items-center gap-2 text-left"
                            >
                                <span
                                    className={cn('size-1.5 shrink-0 rounded-full', w.id === current ? 'bg-accent' : 'bg-line-strong')}
                                />
                                <span className="truncate text-[12.5px]">{w.name}</span>
                            </button>
                            <IconButton
                                title={t.workspaces.remove}
                                onClick={() => onRemove(w)}
                                className="size-6 opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
                            >
                                <Trash2 className="size-3.5" />
                            </IconButton>
                        </div>
                    ))
                )}
            </div>

            <div className="h-px shrink-0 bg-line" />

            <SectionTitle
                actions={
                    view ? (
                        <>
                            <IconButton title={t.workspaces.syncSkills} onClick={onSyncSkills}>
                                <Sparkles className="size-3.5" />
                            </IconButton>
                            <IconButton title={t.workspaces.reveal} onClick={onReveal}>
                                <FolderOpen className="size-3.5" />
                            </IconButton>
                            <IconButton title={t.workspaces.rescan} onClick={onRescan}>
                                <RefreshCw className="size-3.5" />
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
                        <p className="px-3 text-[12px] text-warn">{t.workspaces.missing}</p>
                    ) : (
                        <CheckpointTree
                            checkpoints={view.checkpoints}
                            renders={renders}
                            running={running}
                            selected={selected}
                            onSelect={onSelect}
                            onToggle={onToggle}
                        />
                    )
                ) : (
                    <p className="px-3 text-[12px] text-faint">{t.panes.noWorkspace}</p>
                )}
            </div>
        </aside>
    )
}
