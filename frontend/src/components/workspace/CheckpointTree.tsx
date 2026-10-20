import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {ChevronDown, ChevronRight, Ellipsis, FileCode2, GitBranch, Loader2, TriangleAlert} from 'lucide-react'
import type {render, store} from '../../../wailsjs/go/models'
import {RenameCheckpoint} from '../../../wailsjs/go/main/App'
import {activePath, buildTree, effectiveActive, indexTree, type TreeNode} from '../../lib/checkpointTree'
import {checkpointTitle} from '../../lib/checkpointInfo'
import {renderTone, summary, type RenderTone} from '../../lib/render'
import {errorText} from '../../lib/errors'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {Menu} from '../ui/Menu'
import {toast} from '../ui/Toast'
import {useCheckpointActions} from './checkpointActions'
import {EditedByAIBadge} from './HistoryControls'

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
            <span className="flex shrink-0 items-center" title={text} aria-label={text}>
                <Loader2 className="icon-ui-12 animate-spin text-accent" />
            </span>
        )
    }
    const color = dotColor[tone]
    if (!color) return null
    return (
        <span className="flex size-3 shrink-0 items-center justify-center" title={text} aria-label={text}>
            <span className={cn('size-1.5 rounded-full', color)} />
        </span>
    )
}

// Campo de texto de «Renombrar» en línea: Intro guarda, Esc cancela y salir del
// campo también guarda (como renombrar un archivo en el Finder). Ninguna tecla
// sube al árbol: F2 o Supr dentro del campo son del campo.
function RenameInput({initial, label, hint, placeholder, onDone}: {initial: string; label: string; hint: string; placeholder: string; onDone: (value: string | null) => void}) {
    const ref = useRef<HTMLInputElement>(null)
    const done = useRef(false)
    useEffect(() => {
        ref.current?.focus()
        ref.current?.select()
    }, [])
    const finish = (value: string | null) => {
        if (done.current) return
        done.current = true
        onDone(value)
    }
    return (
        <input
            ref={ref}
            defaultValue={initial}
            maxLength={80}
            aria-label={label}
            title={hint}
            placeholder={placeholder}
            spellCheck={false}
            onClick={(e) => e.stopPropagation()}
            onDoubleClick={(e) => e.stopPropagation()}
            onContextMenu={(e) => e.stopPropagation()}
            onBlur={(e) => finish(e.currentTarget.value)}
            onKeyDown={(e) => {
                e.stopPropagation()
                if (e.key === 'Enter') {
                    e.preventDefault()
                    finish(e.currentTarget.value)
                } else if (e.key === 'Escape') {
                    e.preventDefault()
                    finish(null)
                }
            }}
            className="selectable h-5 min-w-0 flex-1 rounded border border-accent bg-surface px-1 font-mono text-ui-12 text-fg outline-none"
        />
    )
}

// Niveles de sangría (de 3 caracteres) que se dibujan. Los checkpoints suelen formar una
// cadena larga (001 → 002 → …) y cada nivel suma 3 caracteres de conector: a los diez
// niveles el nombre ya no cabía en la barra lateral, que no se desplaza en horizontal. Más
// allá del tope se omiten los niveles lejanos (con «…»); la rama activa y el nombre quedan
// a la vista, y el `title` de cada nodo dice de quién cuelga.
const MAX_INDENT_LEVELS = 2

function clipPrefix(prefix: string): string {
    const keep = MAX_INDENT_LEVELS * 3
    return prefix.length > keep ? '…' + prefix.slice(prefix.length - keep + 1) : prefix
}

type MenuState = {file: string; anchor: HTMLElement; point?: {x: number; y: number}}

// Árbol de checkpoints con conectores de caja (├─ └─ │). Cada nodo tiene menú
// contextual (clic derecho, tecla de menú o el botón «…»), renombrado en línea
// (F2) y papelera (Supr); el padre activo se marca con un ícono de rama y la
// rama que lleva a él se resalta.
export function CheckpointTree({
    workspaceId,
    checkpoints,
    activeParent,
    selected,
    onSelect,
    onToggle,
    onView,
    onCompare,
    renders,
    running,
    shotMenu,
    shotConfirm,
}: {
    workspaceId: number
    checkpoints: store.Checkpoint[]
    /** Padre activo explícito del workspace ("" = rige el más nuevo). */
    activeParent: string
    renders: Record<string, render.Result>
    running: Record<string, boolean>
    selected: string | null
    onSelect: (file: string) => void
    onToggle: (cp: store.Checkpoint) => void
    /** Abre el código del checkpoint en el panel Código. */
    onView: (file: string) => void
    /** Abre la comparación del checkpoint con su padre en el panel Código. */
    onCompare: (file: string) => void
    /** Solo el banco de capturas: archivo cuyo menú se abre al montar. */
    shotMenu?: string
    /** Solo el banco de capturas: archivo cuyo diálogo de «mover a la papelera» se abre al montar. */
    shotConfirm?: string
}) {
    const t = useT()
    const roots = useMemo(() => buildTree(checkpoints), [checkpoints])
    const index = useMemo(() => indexTree(roots), [roots])
    const active = effectiveActive(checkpoints, activeParent)
    const onPath = useMemo(() => activePath(index, active), [index, active])
    const actions = useCheckpointActions(workspaceId)
    const [menu, setMenu] = useState<MenuState | null>(null)
    const [renaming, setRenaming] = useState<string | null>(null)
    const box = useRef<HTMLDivElement>(null)
    const closeMenu = useCallback(() => setMenu(null), [])
    const rowOf = useCallback((file: string) => box.current?.querySelector(`[data-file="${file}"]`) as HTMLElement | null | undefined, [])

    // Banco de capturas: abre el menú del nodo pedido, donde estaría el puntero.
    useEffect(() => {
        if (!shotMenu) return
        const timer = window.setTimeout(() => {
            const el = rowOf(shotMenu)
            if (!el) return
            const r = el.getBoundingClientRect()
            setMenu({file: shotMenu, anchor: el, point: {x: r.right - 28, y: r.top + 10}})
        }, 300)
        return () => window.clearTimeout(timer)
    }, [shotMenu, roots, rowOf])

    // Banco de capturas: abre la confirmación de mandar ese nodo a la papelera.
    useEffect(() => {
        if (!shotConfirm) return
        const timer = window.setTimeout(() => {
            const hit = index.get(shotConfirm)
            if (hit) void actions.trash(hit.node, hit.parent, false)
        }, 300)
        return () => window.clearTimeout(timer)
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [shotConfirm])

    if (checkpoints.length === 0) {
        return <p className="px-3 py-2 text-ui-12 leading-relaxed text-faint">{t.checkpoints.empty}</p>
    }

    const display = (cp: store.Checkpoint) => cp.label || cp.fileName.replace(/\.scad$/i, '')

    const commitRename = async (cp: store.Checkpoint, value: string | null) => {
        setRenaming(null)
        // Devolver el foco al nodo: el campo se desmonta y el teclado no debe perderse.
        window.setTimeout(() => rowOf(cp.fileName)?.focus(), 0)
        if (value === null) return
        const next = value.trim()
        // El nombre por defecto no es un rótulo: dejarlo igual no guarda nada.
        const before = cp.label || cp.fileName.replace(/\.scad$/i, '')
        if (next === before) return
        const label = next === cp.fileName.replace(/\.scad$/i, '') ? '' : next
        try {
            await RenameCheckpoint(workspaceId, cp.fileName, label)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const openMenu = (cp: store.Checkpoint, anchor: HTMLElement, point?: {x: number; y: number}) => {
        setMenu((m) => (m && m.file === cp.fileName && m.anchor === anchor && !point ? null : {file: cp.fileName, anchor, point}))
    }

    const focusRow = (from: HTMLElement, dir: 1 | -1) => {
        const rows = [...(box.current?.querySelectorAll<HTMLElement>('[role="treeitem"]') ?? [])]
        const i = rows.indexOf(from)
        rows[i + dir]?.focus()
    }

    const rows: React.ReactNode[] = []
    const walk = (nodes: TreeNode[], prefix: string) => {
        nodes.forEach((n, i) => {
            const cp = n.cp
            const last = i === nodes.length - 1
            const branch = prefix === '' && nodes === roots ? '' : last ? '└─ ' : '├─ '
            const hasKids = n.children.length > 0
            const isSel = cp.fileName === selected
            const isActive = cp.fileName === active
            const hot = onPath.has(cp.fileName)
            const isOpen = menu?.file === cp.fileName
            rows.push(
                <div
                    key={cp.fileName}
                    role="treeitem"
                    data-file={cp.fileName}
                    aria-selected={isSel}
                    aria-expanded={hasKids ? !cp.collapsed : undefined}
                    tabIndex={0}
                    title={renaming === cp.fileName ? undefined : checkpointTitle(t, cp)}
                    onClick={() => onSelect(cp.fileName)}
                    onContextMenu={(e) => {
                        e.preventDefault()
                        openMenu(cp, e.currentTarget, {x: e.clientX, y: e.clientY})
                    }}
                    onKeyDown={(e) => {
                        // Con el menú abierto, las flechas y Esc son del menú; y un botón del propio nodo se atiende solo.
                        if (menu || e.target !== e.currentTarget) return
                        const el = e.currentTarget
                        if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault()
                            onSelect(cp.fileName)
                        } else if (e.key === 'F2') {
                            e.preventDefault()
                            setRenaming(cp.fileName)
                        } else if (e.key === 'Delete' || e.key === 'Backspace') {
                            e.preventDefault()
                            void actions.trash(n, index.get(cp.fileName)?.parent ?? null, false)
                        } else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
                            e.preventDefault()
                            focusRow(el, e.key === 'ArrowDown' ? 1 : -1)
                        } else if ((e.key === 'ArrowLeft' && hasKids && !cp.collapsed) || (e.key === 'ArrowRight' && hasKids && cp.collapsed)) {
                            e.preventDefault()
                            onToggle(cp)
                        } else if (e.key === 'ContextMenu' || (e.shiftKey && e.key === 'F10')) {
                            e.preventDefault()
                            const r = el.getBoundingClientRect()
                            openMenu(cp, el, {x: r.left + 96, y: r.bottom - 4})
                        }
                    }}
                    className={cn(
                        'group relative flex h-7 cursor-default items-center gap-1 rounded-md pr-1.5 pl-1 text-ui-12 transition-colors duration-100 outline-none focus-visible:ring-1 focus-visible:ring-accent',
                        // Barra de acento a la izquierda: el padre activo se ve de un vistazo.
                        isActive && 'before:absolute before:inset-y-1 before:left-0 before:w-0.5 before:rounded-full before:bg-accent',
                        isSel ? 'bg-accent-soft text-fg' : hot ? 'text-fg hover:bg-surface-2' : 'text-muted hover:bg-surface-2 hover:text-fg',
                        isOpen && !isSel && 'bg-surface-2 text-fg',
                    )}
                >
                    <span className="font-mono whitespace-pre text-faint">
                        {clipPrefix(prefix)}
                        {/* El tramo que baja hasta el padre activo va en acento. */}
                        <span className={hot && branch ? 'text-accent' : undefined}>{branch}</span>
                    </span>
                    {hasKids ? (
                        <button
                            type="button"
                            title={cp.collapsed ? t.checkpoints.expand : t.checkpoints.collapse}
                            tabIndex={-1}
                            onClick={(e) => {
                                e.stopPropagation()
                                onToggle(cp)
                            }}
                            className="flex icon-ui-16 items-center justify-center rounded text-faint hover:text-fg"
                        >
                            {cp.collapsed ? <ChevronRight className="icon-ui-12" /> : <ChevronDown className="icon-ui-12" />}
                        </button>
                    ) : (
                        <FileCode2 className={cn('icon-ui-14 shrink-0', isSel || hot ? 'text-accent' : 'text-faint')} />
                    )}
                    {renaming === cp.fileName ? (
                        <RenameInput
                            initial={display(cp)}
                            label={fmt(t.checkpoints.renameLabel, {file: cp.fileName})}
                            hint={t.checkpoints.renameHint}
                            placeholder={t.checkpoints.renamePlaceholder}
                            onDone={(v) => void commitRename(cp, v)}
                        />
                    ) : (
                        <span className={cn('truncate font-mono', isActive && 'font-semibold')}>{display(cp)}</span>
                    )}
                    {n.orphan ? (
                        <span className="flex shrink-0" title={fmt(t.checkpoints.orphan, {parent: cp.parentFile})}>
                            <TriangleAlert className="icon-ui-12 text-warn" aria-label={fmt(t.checkpoints.orphan, {parent: cp.parentFile})} />
                        </span>
                    ) : null}
                    {cp.editedByAI ? <EditedByAIBadge file={cp.fileName} className="p-0.5" /> : null}
                    {isActive ? (
                        <span
                            className="flex shrink-0 items-center rounded bg-accent-soft p-0.5 text-accent"
                            title={t.checkpoints.activeHint}
                            aria-label={t.checkpoints.active}
                        >
                            <GitBranch className="icon-ui-12" />
                        </span>
                    ) : null}
                    <span className="ml-auto flex shrink-0 items-center gap-0.5">
                        <button
                            type="button"
                            title={fmt(t.checkpoints.more, {name: display(cp)})}
                            aria-label={fmt(t.checkpoints.more, {name: display(cp)})}
                            aria-haspopup="menu"
                            aria-expanded={isOpen}
                            onClick={(e) => {
                                e.stopPropagation()
                                openMenu(cp, e.currentTarget)
                            }}
                            className={cn(
                                'no-drag flex icon-ui-16 items-center justify-center rounded text-muted transition-opacity duration-100 hover:bg-raised hover:text-fg focus-visible:opacity-100',
                                isOpen ? 'opacity-100' : 'opacity-0 group-focus-within:opacity-100 group-hover:opacity-100',
                            )}
                        >
                            <Ellipsis className="icon-ui-14" />
                        </button>
                        <RenderDot result={renders[cp.fileName]} running={!!running[cp.fileName]} />
                    </span>
                </div>,
            )
            if (hasKids && !cp.collapsed) {
                const childPrefix = prefix + (branch === '' ? '' : last ? '   ' : '│  ')
                walk(n.children, childPrefix)
            }
        })
    }
    walk(roots, '')

    const target = menu ? index.get(menu.file) : undefined
    return (
        <div ref={box} role="tree" className="flex flex-col px-1.5">
            {rows}
            {menu && target ? (
                <Menu
                    anchor={menu.anchor}
                    point={menu.point}
                    onClose={closeMenu}
                    label={fmt(t.checkpoints.menuLabel, {name: display(target.node.cp)})}
                    entries={actions.menuEntries({
                        node: target.node,
                        parent: target.parent,
                        isActive: menu.file === active,
                        onView,
                        onCompare,
                        onToggle,
                        onRename: () => setRenaming(menu.file),
                    })}
                />
            ) : null}
        </div>
    )
}
