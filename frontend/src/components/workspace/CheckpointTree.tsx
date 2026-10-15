import {useMemo} from 'react'
import {ChevronDown, ChevronRight, FileCode2, Loader2, TriangleAlert} from 'lucide-react'
import type {render, store} from '../../../wailsjs/go/models'
import {buildTree, type TreeNode} from '../../lib/checkpointTree'
import {renderTone, summary, type RenderTone} from '../../lib/render'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'

const dotColor: Partial<Record<RenderTone, string>> = {
    ok: 'bg-ok',
    warn: 'bg-warn',
    error: 'bg-danger',
    missing: 'bg-faint',
}

// RenderDot es el estado de render de un checkpoint: spinner mientras corre,
// punto verde/ámbar/rojo según el resultado y nada si nunca se renderizó. El
// title lleva el resumen (duración o el primer error con su línea).
function RenderDot({result, running}: {result: render.Result | undefined; running: boolean}) {
    const t = useT()
    const tone = renderTone(result, running)
    const text = summary(t, result, running)
    if (tone === 'running') {
        return (
            <span className="ml-auto flex shrink-0 items-center" title={text} aria-label={text}>
                <Loader2 className="icon-ui-12 animate-spin text-accent" />
            </span>
        )
    }
    const color = dotColor[tone]
    if (!color) return null
    return (
        <span className="ml-auto flex size-3 shrink-0 items-center justify-center" title={text} aria-label={text}>
            <span className={cn('size-1.5 rounded-full', color)} />
        </span>
    )
}

// Árbol de checkpoints con conectores de caja (├─ └─ │). El menú contextual
// (renombrar, ver código, retomar sesión) llega en S3.
export function CheckpointTree({
    checkpoints,
    selected,
    onSelect,
    onToggle,
    renders,
    running,
}: {
    checkpoints: store.Checkpoint[]
    renders: Record<string, render.Result>
    running: Record<string, boolean>
    selected: string | null
    onSelect: (file: string) => void
    onToggle: (cp: store.Checkpoint) => void
}) {
    const t = useT()
    const roots = useMemo(() => buildTree(checkpoints), [checkpoints])
    if (checkpoints.length === 0) {
        return <p className="px-3 py-2 text-ui-12 leading-relaxed text-faint">{t.checkpoints.empty}</p>
    }
    const rows: React.ReactNode[] = []
    const walk = (nodes: TreeNode[], prefix: string) => {
        nodes.forEach((n, i) => {
            const last = i === nodes.length - 1
            const branch = prefix === '' && nodes === roots ? '' : last ? '└─ ' : '├─ '
            const hasKids = n.children.length > 0
            const isSel = n.cp.fileName === selected
            rows.push(
                <div
                    key={n.cp.fileName}
                    role="treeitem"
                    aria-selected={isSel}
                    aria-expanded={hasKids ? !n.cp.collapsed : undefined}
                    tabIndex={0}
                    title={n.cp.description || n.cp.fileName}
                    onClick={() => onSelect(n.cp.fileName)}
                    onKeyDown={(e) => {
                        if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault()
                            onSelect(n.cp.fileName)
                        }
                    }}
                    className={cn(
                        'group flex h-7 cursor-default items-center gap-1 rounded-md pr-2 pl-1 text-ui-12 transition-colors duration-100',
                        isSel ? 'bg-accent-soft text-fg' : 'text-muted hover:bg-surface-2 hover:text-fg',
                    )}
                >
                    <span className="font-mono whitespace-pre text-faint">{prefix + branch}</span>
                    {hasKids ? (
                        <button
                            type="button"
                            title={n.cp.collapsed ? t.checkpoints.expand : t.checkpoints.collapse}
                            onClick={(e) => {
                                e.stopPropagation()
                                onToggle(n.cp)
                            }}
                            className="flex icon-ui-16 items-center justify-center rounded text-faint hover:text-fg"
                        >
                            {n.cp.collapsed ? <ChevronRight className="icon-ui-12" /> : <ChevronDown className="icon-ui-12" />}
                        </button>
                    ) : (
                        <FileCode2 className={cn('icon-ui-14 shrink-0', isSel ? 'text-accent' : 'text-faint')} />
                    )}
                    <span className="truncate font-mono">{n.cp.label || n.cp.fileName.replace(/\.scad$/i, '')}</span>
                    {n.orphan ? (
                        <TriangleAlert
                            className="icon-ui-12 shrink-0 text-warn"
                            aria-label={fmt(t.checkpoints.orphan, {parent: n.cp.parentFile})}
                        />
                    ) : null}
                    <RenderDot result={renders[n.cp.fileName]} running={!!running[n.cp.fileName]} />
                </div>,
            )
            if (hasKids && !n.cp.collapsed) {
                const childPrefix = prefix + (branch === '' ? '' : last ? '   ' : '│  ')
                walk(n.children, childPrefix)
            }
        })
    }
    walk(roots, '')
    return (
        <div role="tree" className="flex flex-col px-1.5">
            {rows}
        </div>
    )
}
