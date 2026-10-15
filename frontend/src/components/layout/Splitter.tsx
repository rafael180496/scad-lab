import {useRef, useState} from 'react'
import {cn} from '../../lib/cn'

// Splitter es un divisor arrastrable entre dos paneles. Cambia el tamaño de
// UNO de ellos (`size`, en px): `invert` cuando ese panel queda a la derecha o
// abajo del divisor (arrastrar hacia él lo agranda). Doble clic restaura el
// tamaño de fábrica; con el foco, las flechas lo mueven de a 16 px.
//
// Mientras se arrastra se llama a onResize (redibujo en vivo); al soltar,
// onCommit (es cuando se persiste: un guardado por arrastre, no por píxel).
export function Splitter({
    orientation,
    size,
    min,
    max,
    invert = false,
    title,
    onResize,
    onCommit,
    onReset,
    className,
}: {
    orientation: 'vertical' | 'horizontal' // vertical = línea vertical, mueve en X
    size: number
    min: number
    max: number
    invert?: boolean
    title: string
    onResize: (size: number) => void
    onCommit: (size: number) => void
    onReset: () => void
    className?: string
}) {
    const drag = useRef<{start: number; size: number; last: number} | null>(null)
    const [active, setActive] = useState(false)
    const vertical = orientation === 'vertical'
    const clamp = (v: number) => Math.round(Math.max(min, Math.min(max, v)))

    const move = (pos: number) => {
        const d = drag.current
        if (!d) return
        const delta = (pos - d.start) * (invert ? -1 : 1)
        d.last = clamp(d.size + delta)
        onResize(d.last)
    }

    return (
        <div
            role="separator"
            aria-orientation={vertical ? 'vertical' : 'horizontal'}
            aria-valuenow={size}
            aria-valuemin={min}
            aria-valuemax={max}
            aria-label={title}
            title={title}
            tabIndex={0}
            onPointerDown={(e) => {
                if (e.button !== 0) return
                e.preventDefault()
                e.currentTarget.setPointerCapture(e.pointerId)
                drag.current = {start: vertical ? e.clientX : e.clientY, size, last: size}
                setActive(true)
            }}
            onPointerMove={(e) => move(vertical ? e.clientX : e.clientY)}
            onPointerUp={(e) => {
                const d = drag.current
                drag.current = null
                setActive(false)
                e.currentTarget.releasePointerCapture(e.pointerId)
                if (d && d.last !== d.size) onCommit(d.last)
            }}
            onPointerCancel={() => {
                drag.current = null
                setActive(false)
            }}
            onDoubleClick={onReset}
            onKeyDown={(e) => {
                const back = vertical ? 'ArrowLeft' : 'ArrowUp'
                const fwd = vertical ? 'ArrowRight' : 'ArrowDown'
                if (e.key !== back && e.key !== fwd) return
                e.preventDefault()
                const step = (e.key === fwd ? 16 : -16) * (invert ? -1 : 1)
                onCommit(clamp(size + step))
            }}
            className={cn(
                'group relative z-10 flex shrink-0 touch-none items-center justify-center outline-none',
                vertical ? 'w-2 cursor-col-resize' : 'h-2 cursor-row-resize',
                className,
            )}
        >
            <span
                className={cn(
                    'rounded-full transition-colors duration-100 group-hover:bg-accent/60 group-focus-visible:bg-accent',
                    vertical ? 'h-10 w-[3px]' : 'h-[3px] w-10',
                    active ? 'bg-accent' : 'bg-transparent',
                )}
            />
            {/* Mientras se arrastra, una capa sobre toda la ventana mantiene el
                cursor y evita que el visor 3D o Monaco se queden con el puntero. */}
            {active ? <span className={cn('fixed inset-0', vertical ? 'cursor-col-resize' : 'cursor-row-resize')} /> : null}
        </div>
    )
}
