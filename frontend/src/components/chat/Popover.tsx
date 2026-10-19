import {useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode} from 'react'
import {createPortal} from 'react-dom'
import {cn} from '../../lib/cn'

// Desplegable anclado a un botón para los selectores del chat (proveedor, modelo,
// modo, conversaciones). Va en un portal con posición fija por la misma razón que
// el Menu: los grupos del dock recortan lo que desborda (overflow-clip) y un
// desplegable absoluto quedaría cortado en un panel angosto o pegado al borde de
// la ventana.
//
// Se acota a la ventana: nunca se sale por la izquierda ni por la derecha (el
// ancho se reduce si hace falta) y, si abajo no cabe, sube o se queda con el lado
// que tenga más sitio y limita su alto (la lista de adentro hace scroll). Se
// recoloca al redimensionar la ventana o al moverse el panel.
//
// Cierra con Escape (en la fase de burbuja: un campo de adentro, como el renombrado en
// línea, lo atiende primero y lo detiene), con un clic fuera o al perder el foco la ventana. El foco
// entra en el desplegable al abrirse (para que las flechas lo recorran) y vuelve al
// ancla al cerrarse con teclado.

const MARGIN = 8
const GAP = 4

type Place = {left: number; top: number; width: number; maxHeight: number}

export function Popover({
    anchor,
    onClose,
    children,
    width = 320,
    align = 'start',
    label,
    onKeyDown,
    className,
    focusFirst = true,
}: {
    anchor: HTMLElement
    onClose: () => void
    children: ReactNode
    /** Ancho deseado en px; se reduce si la ventana es más angosta. */
    width?: number
    /** Con qué borde del ancla se alinea. */
    align?: 'start' | 'end'
    label: string
    onKeyDown?: (e: KeyboardEvent<HTMLDivElement>) => void
    className?: string
    /** Enfoca el contenedor al abrir (los que traen un buscador lo enfocan ellos). */
    focusFirst?: boolean
}) {
    const box = useRef<HTMLDivElement>(null)
    const [place, setPlace] = useState<Place | null>(null)

    useLayoutEffect(() => {
        const measure = () => {
            const a = anchor.getBoundingClientRect()
            const vw = window.innerWidth
            const vh = window.innerHeight
            const w = Math.min(width, vw - MARGIN * 2)
            let left = align === 'end' ? a.right - w : a.left
            left = Math.max(MARGIN, Math.min(left, vw - w - MARGIN))
            const below = vh - a.bottom - GAP - MARGIN
            const above = a.top - GAP - MARGIN
            // Debajo si cabe (lo que mide el contenido, hasta un tope razonable) o si arriba hay menos sitio:
            // un desplegable que salta hacia arriba tapa justo lo que se está mirando.
            const natural = box.current?.scrollHeight ?? 0
            const down = below >= Math.min(natural, 300) || below >= above
            const room = Math.max(120, down ? below : above)
            const h = Math.min(natural || room, room)
            const top = down ? a.bottom + GAP : a.top - GAP - h
            const next = {left, top: Math.max(MARGIN, top), width: w, maxHeight: room}
            setPlace((prev) => (prev && prev.left === next.left && prev.top === next.top && prev.width === next.width && prev.maxHeight === next.maxHeight ? prev : next))
        }
        measure()
        window.addEventListener('resize', measure)
        window.addEventListener('scroll', measure, true)
        const ro = new ResizeObserver(measure)
        ro.observe(document.documentElement)
        if (box.current) ro.observe(box.current)
        // El ancla también se mueve sin que la ventana cambie de tamaño (el dock reparte el alto entre sus
        // paneles, el panel se redimensiona con el desplegable abierto): se vigila su posición en cada cuadro
        // y solo se vuelve a medir si cambió.
        let raf = 0
        let last = anchor.getBoundingClientRect()
        const watch = () => {
            const r = anchor.getBoundingClientRect()
            if (r.left !== last.left || r.top !== last.top || r.width !== last.width || r.height !== last.height) {
                last = r
                measure()
            }
            raf = requestAnimationFrame(watch)
        }
        raf = requestAnimationFrame(watch)
        return () => {
            cancelAnimationFrame(raf)
            window.removeEventListener('resize', measure)
            window.removeEventListener('scroll', measure, true)
            ro.disconnect()
        }
    }, [anchor, width, align])

    useEffect(() => {
        const down = (e: PointerEvent) => {
            const n = e.target as Node
            if (box.current?.contains(n) || anchor.contains(n)) return
            onClose()
        }
        const key = (e: globalThis.KeyboardEvent) => {
            if (e.key !== 'Escape') return
            e.stopPropagation()
            e.preventDefault()
            onClose()
            anchor.focus()
        }
        document.addEventListener('pointerdown', down)
        document.addEventListener('keydown', key)
        window.addEventListener('blur', onClose)
        return () => {
            document.removeEventListener('pointerdown', down)
            document.removeEventListener('keydown', key)
            window.removeEventListener('blur', onClose)
        }
    }, [anchor, onClose])

    useEffect(() => {
        if (focusFirst) box.current?.focus({preventScroll: true})
    }, [focusFirst])

    return createPortal(
        <div
            ref={box}
            role="dialog"
            aria-label={label}
            tabIndex={-1}
            onKeyDown={onKeyDown}
            style={{
                left: place?.left ?? 0,
                top: place?.top ?? 0,
                width: place?.width ?? width,
                maxHeight: place?.maxHeight,
                visibility: place ? 'visible' : 'hidden',
            }}
            className={cn(
                'no-drag fixed z-[2000] flex flex-col overflow-clip rounded-lg border border-line-strong bg-raised text-ui-12 text-fg shadow-xl shadow-black/30 outline-none',
                className,
            )}
        >
            {children}
        </div>,
        document.body,
    )
}

/**
 * Anula el comportamiento por defecto de las flechas dentro de una lista y avisa
 * hacia dónde se movió la fila activa: una sola función para los cuatro
 * selectores, en vez de cuatro `onKeyDown` casi iguales.
 */
export function listKeys(
    e: KeyboardEvent,
    opts: {rows: number; active: number; setActive: (n: number) => void; onEnter: () => void},
): void {
    if (e.key === 'ArrowDown') {
        e.preventDefault()
        opts.setActive(Math.min(opts.active + 1, opts.rows - 1))
    } else if (e.key === 'ArrowUp') {
        e.preventDefault()
        opts.setActive(Math.max(opts.active - 1, 0))
    } else if (e.key === 'Home') {
        e.preventDefault()
        opts.setActive(0)
    } else if (e.key === 'End') {
        e.preventDefault()
        opts.setActive(Math.max(0, opts.rows - 1))
    } else if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
        e.preventDefault()
        opts.onEnter()
    }
}
