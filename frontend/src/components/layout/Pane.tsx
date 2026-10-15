import type {ReactNode} from 'react'
import {cn} from '../../lib/cn'

// PanelToolbar es la franja de herramientas de 32 px que va bajo la pestaña de
// un panel acoplable: a la izquierda lo que se está mirando (p. ej. el nombre
// del archivo) y a la derecha las acciones. El título del panel ya lo lleva su
// pestaña; esto no lo repite.
export function PanelToolbar({
    label,
    actions,
    className,
}: {
    label?: ReactNode
    actions?: ReactNode
    className?: string
}) {
    return (
        <div className={cn('flex h-8 shrink-0 items-center gap-2 border-b border-line pr-1.5 pl-3', className)}>
            {label ? <span className="selectable min-w-0 truncate font-mono text-ui-11-5 text-fg">{label}</span> : null}
            {actions ? <div className="ml-auto flex min-w-0 shrink-0 items-center gap-1">{actions}</div> : null}
        </div>
    )
}

export function Placeholder({text}: {text: string}) {
    return <p className="mx-auto max-w-[420px] px-6 text-center text-ui-12-5 leading-relaxed text-muted">{text}</p>
}
