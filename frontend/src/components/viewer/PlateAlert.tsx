import {TriangleAlert} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {num} from '../../lib/printFormat'
import type {PlateFit} from '../../lib/printTypes'

// Texto corto de lo que sobra: «X +20,0 mm · Z +3,2 mm». Lo comparten el aviso del
// visor y la barra de estado.
export function overflowAxes(plate: PlateFit): string {
    return (['x', 'y', 'z'] as const)
        .filter((a) => plate.overflow[a] > 0)
        .map((a) => `${a.toUpperCase()} +${num(plate.overflow[a], 1)} mm`)
        .join(' · ')
}

export function plateSize(plate: PlateFit): string {
    return `${num(plate.plate.x, 0)} × ${num(plate.plate.y, 0)} × ${num(plate.plate.z, 0)}`
}

// PlateAlert avisa que el modelo no cabe en la placa de la impresora del
// workspace (S7): cuánto sobra por eje y qué se puede hacer. El visor, además,
// pinta el contorno de la cama en rojo.
export function PlateAlert({plate}: {plate: PlateFit}) {
    const t = useT()
    return (
        <div
            role="alert"
            className="pointer-events-auto flex max-w-[340px] items-start gap-2 rounded-lg border border-danger/60 bg-surface/95 px-2.5 py-2 backdrop-blur-sm"
            onPointerDown={(e) => e.stopPropagation()}
        >
            <TriangleAlert className="mt-px icon-ui-14 shrink-0 text-danger" />
            <div className="min-w-0 text-ui-11-5 leading-snug">
                <p className="font-medium text-danger">{t.plate.title}</p>
                <p className="text-fg">{fmt(t.plate.over, {axes: overflowAxes(plate), plate: plateSize(plate)})}</p>
                <p className="mt-0.5 text-muted">{t.plate.tips}</p>
            </div>
        </div>
    )
}
