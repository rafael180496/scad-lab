import {useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode} from 'react'
import {createPortal} from 'react-dom'
import {Check} from 'lucide-react'
import {cn} from '../../lib/cn'

// Menú desplegable. Se dibuja en un portal con posición fija porque la
// cabecera y los grupos del dock recortan lo que desborda (overflow-clip) y un
// menú absoluto quedaría cortado. Se cierra con Escape, con un clic fuera o al
// elegir; flechas arriba/abajo y Enter lo recorren con el teclado.
export type MenuEntry =
    | {kind: 'heading'; label: string}
    | {kind: 'separator'}
    | {
          kind?: 'item'
          label: string
          /** Texto del atributo `title` (todo control interactivo lleva uno). */
          title: string
          onSelect: () => void
          /** Atajo a la derecha, ya con el formato de la plataforma (⌘⇧1). */
          keys?: string
          /** true/false dibuja la casilla; undefined, sin casilla. */
          checked?: boolean
          icon?: ReactNode
          disabled?: boolean
          danger?: boolean
          /** Deja el menú abierto al elegir (casillas que se alternan varias veces). */
          keepOpen?: boolean
      }

type Pos = {left: number; top: number; minWidth: number}

export function Menu({
    anchor,
    entries,
    onClose,
    align = 'start',
    label,
}: {
    anchor: HTMLElement
    entries: MenuEntry[]
    onClose: () => void
    align?: 'start' | 'end'
    label: string
}) {
    const box = useRef<HTMLDivElement>(null)
    const [pos, setPos] = useState<Pos | null>(null)

    // Posición bajo el ancla, acotada a la ventana (ni se sale por la derecha ni
    // por abajo; si no cabe debajo, sube).
    useLayoutEffect(() => {
        const a = anchor.getBoundingClientRect()
        const m = box.current?.getBoundingClientRect()
        const w = m?.width ?? 240
        const h = m?.height ?? 200
        let left = align === 'end' ? a.right - w : a.left
        left = Math.max(6, Math.min(left, window.innerWidth - w - 6))
        let top = a.bottom + 4
        if (top + h > window.innerHeight - 6) top = Math.max(6, a.top - h - 4)
        setPos({left, top, minWidth: Math.max(a.width, 200)})
    }, [anchor, align, entries.length])

    useEffect(() => {
        const down = (e: MouseEvent) => {
            const n = e.target as Node
            if (box.current?.contains(n) || anchor.contains(n)) return
            onClose()
        }
        const key = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                e.stopPropagation()
                onClose()
                anchor.focus()
                return
            }
            if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
            e.preventDefault()
            const items = [...(box.current?.querySelectorAll<HTMLElement>('[role^="menuitem"]:not([aria-disabled="true"])') ?? [])]
            if (!items.length) return
            const i = items.indexOf(document.activeElement as HTMLElement)
            items[(i + (e.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length].focus()
        }
        document.addEventListener('mousedown', down)
        document.addEventListener('keydown', key, true)
        window.addEventListener('blur', onClose)
        return () => {
            document.removeEventListener('mousedown', down)
            document.removeEventListener('keydown', key, true)
            window.removeEventListener('blur', onClose)
        }
    }, [anchor, onClose])

    return createPortal(
        <div
            ref={box}
            role="menu"
            aria-label={label}
            style={{left: pos?.left ?? 0, top: pos?.top ?? 0, minWidth: pos?.minWidth, visibility: pos ? 'visible' : 'hidden'}}
            className="no-drag fixed z-[2000] max-h-[calc(100vh-12px)] overflow-y-auto rounded-lg border border-line-strong bg-raised py-1 text-ui-12 text-fg shadow-xl shadow-black/30"
        >
            {entries.map((e, i) => {
                if (e.kind === 'separator') return <div key={i} role="separator" className="my-1 h-px bg-line" />
                if (e.kind === 'heading')
                    return (
                        <div key={i} className="px-3 pt-1.5 pb-1 font-mono text-ui-10 tracking-[0.12em] text-faint uppercase">
                            {e.label}
                        </div>
                    )
                const role = e.checked === undefined ? 'menuitem' : 'menuitemcheckbox'
                return (
                    <button
                        key={i}
                        type="button"
                        role={role}
                        aria-checked={e.checked}
                        aria-disabled={e.disabled || undefined}
                        title={e.title}
                        onClick={() => {
                            if (e.disabled) return
                            e.onSelect()
                            if (!e.keepOpen) onClose()
                        }}
                        className={cn(
                            'flex w-full items-center gap-2 px-3 py-1.5 text-left whitespace-nowrap focus-visible:outline-none transition-colors duration-75',
                            e.disabled ? 'cursor-default text-faint' : 'hover:bg-surface-2 focus-visible:bg-surface-2',
                            e.danger && !e.disabled && 'text-danger',
                        )}
                    >
                        <span className="flex icon-ui-14 shrink-0 items-center justify-center text-accent">
                            {e.checked ? <Check className="icon-ui-14" /> : e.icon}
                        </span>
                        <span className="min-w-0 flex-1 truncate">{e.label}</span>
                        {e.keys ? <span className="ml-4 shrink-0 font-mono text-ui-11 text-muted">{e.keys}</span> : null}
                    </button>
                )
            })}
        </div>,
        document.body,
    )
}

// useMenu administra el estado abierto/cerrado de un menú anclado a un botón.
export function useMenu() {
    const [anchor, setAnchor] = useState<HTMLElement | null>(null)
    const close = useCallback(() => setAnchor(null), [])
    const toggle = useCallback((e: React.MouseEvent<HTMLElement>) => {
        const el = e.currentTarget
        setAnchor((a) => (a === el ? null : el))
    }, [])
    return {anchor, close, toggle, openAt: setAnchor, open: anchor !== null}
}
