import {useCallback, useEffect, useRef, useState} from 'react'
import {AlertTriangle, FolderOpen, Loader2, Plus, SquareTerminal, X} from 'lucide-react'
import type {store} from '../../../wailsjs/go/models'
import {IconButton} from '../ui/Button'
import {Menu, useMenu, type MenuEntry} from '../ui/Menu'
import {useNarrow} from '../../hooks/useNarrow'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {isMac, keys} from '../../lib/platform'
import {MAX_PROJECTS} from '../../lib/projectsApi'

// Una pestaña de proyecto: un workspace abierto (S8). `rendering`, `failed`,
// `agents` y `unsaved` son los indicadores de lo que pasa en el proyecto aunque
// no se esté mirando.
export type ProjectTab = {
    id: number
    name: string
    path: string
    missing: boolean
    unsaved: boolean
    rendering: boolean
    failed: boolean
    agents: number
}

// Solo el banco de capturas: abre el menú «+» sin simular un clic.
export type ProjectTabsShot = 'plus'

// Ancho mínimo que necesita cada pestaña (a interfaz 100 %) para mostrar el ícono
// de carpeta; por debajo de eso la barra pasa a modo compacto.
const TAB_ROOM = 150
const DRAG_THRESHOLD = 5

export function ProjectTabs({
    tabs,
    activeId,
    workspaces,
    shot,
    onActivate,
    onClose,
    onCloseOthers,
    onReorder,
    onOpenWorkspace,
    onNew,
    onReveal,
}: {
    tabs: ProjectTab[]
    activeId: number | null
    /** Todos los workspaces registrados, para el menú «+». */
    workspaces: store.Workspace[]
    shot?: ProjectTabsShot
    onActivate: (id: number) => void
    onClose: (id: number) => void
    onCloseOthers: (id: number) => void
    onReorder: (ids: number[]) => void
    onOpenWorkspace: (id: number) => void
    onNew: () => void
    onReveal: (id: number) => void
}) {
    const t = useT()
    // Con poco ancho por pestaña se omite el ícono de carpeta y los indicadores
    // se reducen al más importante: el nombre es lo que no puede perderse.
    const [compact, stripRef] = useNarrow(Math.max(1, tabs.length) * TAB_ROOM)
    const strip = useRef<HTMLDivElement | null>(null)
    const setStrip = useCallback(
        (el: HTMLDivElement | null) => {
            strip.current = el
            stripRef(el)
        },
        [stripRef],
    )

    // ── Arrastrar para reordenar (eventos de puntero, no HTML5: ver claude/12) ─────
    // Mientras se arrastra, el orden se previsualiza en vivo; al soltar se confirma.
    const [preview, setPreview] = useState<number[] | null>(null)
    const [dragId, setDragId] = useState<number | null>(null)
    const dragged = useRef(false)
    const order = preview ?? tabs.map((x) => x.id)
    const shown = order.map((id) => tabs.find((x) => x.id === id)).filter((x): x is ProjectTab => !!x)

    const startDrag = (e: React.PointerEvent, id: number) => {
        if (e.button !== 0 || tabs.length < 2) return
        const startX = e.clientX
        let moving = false
        let current = tabs.map((x) => x.id)
        const move = (ev: PointerEvent) => {
            if (!moving) {
                if (Math.abs(ev.clientX - startX) < DRAG_THRESHOLD) return
                moving = true
                setDragId(id)
            }
            // El destino es la cantidad de pestañas, sin la arrastrada, cuyo centro queda a la izquierda del puntero.
            const others = current.filter((x) => x !== id)
            let to = 0
            for (const o of others) {
                const r = strip.current?.querySelector<HTMLElement>(`[data-project-tab="${o}"]`)?.getBoundingClientRect()
                if (r && r.left + r.width / 2 < ev.clientX) to++
            }
            const next = [...others.slice(0, to), id, ...others.slice(to)]
            if (next.join() !== current.join()) {
                current = next
                setPreview(next)
            }
        }
        const up = () => {
            window.removeEventListener('pointermove', move)
            window.removeEventListener('pointerup', up)
            window.removeEventListener('pointercancel', up)
            if (moving) {
                dragged.current = true
                window.setTimeout(() => (dragged.current = false), 0)
                if (current.join() !== tabs.map((x) => x.id).join()) onReorder(current)
            }
            setDragId(null)
            setPreview(null)
        }
        window.addEventListener('pointermove', move)
        window.addEventListener('pointerup', up)
        window.addEventListener('pointercancel', up)
    }

    // ── Menús ───────────────────────────────────────────────────────────────────
    const plus = useMenu()
    const ctx = useMenu()
    const [ctxId, setCtxId] = useState<number | null>(null)
    const full = tabs.length >= MAX_PROJECTS

    const {openAt: openPlus} = plus
    useEffect(() => {
        const btn = document.querySelector<HTMLElement>('[data-project-plus]')
        if (shot === 'plus' && btn) openPlus(btn)
    }, [shot, openPlus])

    const plusEntries: MenuEntry[] = [
        {kind: 'heading', label: t.projects.openHeading},
        ...(workspaces.length
            ? workspaces.map(
                  (w): MenuEntry => ({
                      id: `ws-${w.id}`,
                      label: w.name,
                      title: tabs.some((x) => x.id === w.id) ? fmt(t.projects.goToHint, {name: w.name}) : fmt(t.projects.openHint, {name: w.name, path: w.path}),
                      checked: tabs.some((x) => x.id === w.id) || undefined,
                      onSelect: () => onOpenWorkspace(w.id),
                  }),
              )
            : [{label: t.projects.noWorkspaces, title: t.projects.noWorkspaces, disabled: true} as MenuEntry]),
        {kind: 'separator'},
        {id: 'new', label: t.projects.newWorkspace, title: t.workspaces.newHint, icon: <Plus className="icon-ui-14" />, onSelect: onNew},
    ]

    const ctxTab = tabs.find((x) => x.id === ctxId)
    const ctxIndex = tabs.findIndex((x) => x.id === ctxId)
    const move = (dir: -1 | 1) => {
        const ids = tabs.map((x) => x.id)
        const i = ids.indexOf(ctxId ?? -1)
        const j = i + dir
        if (i < 0 || j < 0 || j >= ids.length) return
        ;[ids[i], ids[j]] = [ids[j], ids[i]]
        onReorder(ids)
    }
    const ctxEntries: MenuEntry[] = ctxTab
        ? [
              {id: 'left', label: t.projects.moveLeft, title: t.projects.moveLeftHint, disabled: ctxIndex <= 0, onSelect: () => move(-1)},
              {id: 'right', label: t.projects.moveRight, title: t.projects.moveRightHint, disabled: ctxIndex >= tabs.length - 1, onSelect: () => move(1)},
              {kind: 'separator'},
              {id: 'close', label: t.projects.close, title: t.projects.closeHint, keys: keys('mod+w'), onSelect: () => onClose(ctxTab.id)},
              {id: 'others', label: t.projects.closeOthers, title: t.projects.closeOthersHint, disabled: tabs.length < 2, onSelect: () => onCloseOthers(ctxTab.id)},
              {kind: 'separator'},
              {
                  id: 'reveal',
                  label: isMac() ? t.projects.revealMac : t.projects.revealWin,
                  title: t.workspaces.reveal,
                  disabled: ctxTab.missing,
                  onSelect: () => onReveal(ctxTab.id),
              },
          ]
        : []

    return (
        <div className="no-drag flex min-w-0 items-center gap-1" role="tablist" aria-label={t.projects.tablistLabel}>
            <div ref={setStrip} className="flex min-w-0 items-center gap-1">
                {shown.map((p) => {
                    const active = p.id === activeId
                    // Un solo indicador cuando no hay sitio: el más urgente.
                    const urgent = p.failed ? 'failed' : p.rendering ? 'rendering' : p.agents > 0 ? 'agents' : p.unsaved ? 'unsaved' : ''
                    const show = (k: string) => !compact || urgent === k
                    const showCount = p.agents > 1 && !compact
                    const notes = [
                        p.missing ? t.projects.missing : '',
                        p.rendering ? t.projects.renderingHint : '',
                        p.failed ? t.projects.failedHint : '',
                        p.agents > 0 ? fmt(p.agents === 1 ? t.projects.agentOne : t.projects.agentMany, {n: p.agents}) : '',
                        p.unsaved ? t.projects.unsavedHint : '',
                    ].filter(Boolean)
                    return (
                        <div
                            key={p.id}
                            role="tab"
                            tabIndex={0}
                            aria-selected={active}
                            data-project-tab={p.id}
                            title={[fmt(t.projects.tabHint, {name: p.name, path: p.path}), ...notes].join('\n')}
                            onPointerDown={(e) => startDrag(e, p.id)}
                            onClick={() => !dragged.current && onActivate(p.id)}
                            onKeyDown={(e) => {
                                if (e.target !== e.currentTarget) return
                                if (e.key === 'Enter' || e.key === ' ') {
                                    e.preventDefault()
                                    onActivate(p.id)
                                }
                            }}
                            onContextMenu={(e) => {
                                setCtxId(p.id)
                                ctx.openContext(e)
                            }}
                            className={cn(
                                'group relative flex h-8 min-w-[calc(76px*var(--ui-font-scale))] max-w-[calc(240px*var(--ui-font-scale))] shrink basis-[calc(196px*var(--ui-font-scale))] cursor-default items-center gap-1.5 overflow-clip rounded-md border pr-1 pl-2.5 text-ui-12-5 transition-colors duration-100 select-none',
                                active
                                    ? 'border-line-strong bg-surface-2 text-fg'
                                    : 'border-transparent text-muted hover:bg-surface-2 hover:text-fg',
                                dragId === p.id && 'border-accent/60 opacity-80 shadow-md shadow-black/30',
                                p.missing && 'text-warn',
                            )}
                        >
                            {compact ? null : <FolderOpen className={cn('icon-ui-14 shrink-0', active ? 'text-accent' : 'text-faint')} />}
                            <span className="min-w-0 flex-1 truncate">{p.name}</span>
                            <span className="flex shrink-0 items-center gap-1">
                                {p.failed && show('failed') ? (
                                    <AlertTriangle className="icon-ui-12 text-danger" aria-label={t.projects.failedHint} />
                                ) : null}
                                {p.rendering && show('rendering') ? (
                                    <Loader2 className="icon-ui-12 animate-spin text-accent" aria-label={t.projects.renderingHint} />
                                ) : null}
                                {p.agents > 0 && show('agents') ? (
                                    <span className="flex items-center gap-0.5 text-ok" aria-label={fmt(p.agents === 1 ? t.projects.agentOne : t.projects.agentMany, {n: p.agents})}>
                                        <SquareTerminal className="icon-ui-12" />
                                        {showCount ? <span className="font-mono text-ui-10-5">{p.agents}</span> : null}
                                    </span>
                                ) : null}
                                {p.unsaved && show('unsaved') ? (
                                    <span className="size-1.5 rounded-full bg-accent" aria-label={t.projects.unsavedHint} />
                                ) : null}
                            </span>
                            <button
                                type="button"
                                title={fmt(t.projects.closeTabHint, {name: p.name})}
                                aria-label={fmt(t.projects.closeTabHint, {name: p.name})}
                                onPointerDown={(e) => e.stopPropagation()}
                                onClick={(e) => {
                                    e.stopPropagation()
                                    onClose(p.id)
                                }}
                                className={cn(
                                    'no-drag flex size-5 shrink-0 items-center justify-center rounded text-faint transition-colors duration-100 hover:bg-raised hover:text-fg',
                                    !active && 'opacity-60 group-hover:opacity-100',
                                )}
                            >
                                <X className="icon-ui-12" />
                            </button>
                            {active ? <span className="pointer-events-none absolute inset-x-2 bottom-0 h-0.5 rounded-full bg-accent" /> : null}
                        </div>
                    )
                })}
            </div>
            <IconButton
                title={full ? fmt(t.projects.limitHint, {n: MAX_PROJECTS}) : t.projects.plusHint}
                aria-disabled={full}
                aria-haspopup="menu"
                aria-expanded={plus.open}
                data-project-plus
                onClick={(e) => (full ? undefined : plus.toggle(e))}
                className={cn('shrink-0', full && 'cursor-not-allowed opacity-40 hover:bg-transparent hover:text-muted')}
            >
                <Plus className="icon-ui-16" />
            </IconButton>
            {plus.anchor ? <Menu anchor={plus.anchor} entries={plusEntries} onClose={plus.close} label={t.projects.plusHint} /> : null}
            {ctx.anchor && ctxTab ? (
                <Menu anchor={ctx.anchor} point={ctx.point} entries={ctxEntries} onClose={ctx.close} label={ctxTab.name} />
            ) : null}
        </div>
    )
}
