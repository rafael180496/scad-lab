import type {ReactNode} from 'react'
import {cn} from '../../lib/cn'

// Pane es un panel del área de trabajo: cabecera de 32 px con rótulo en
// monoespaciada (y acciones a la derecha) sobre el contenido.
export function Pane({
    icon,
    title,
    subtitle,
    actions,
    className,
    style,
    children,
}: {
    icon?: ReactNode
    title: ReactNode
    subtitle?: ReactNode
    actions?: ReactNode
    className?: string
    style?: React.CSSProperties
    children: ReactNode
}) {
    return (
        <section
            style={style}
            className={cn('flex min-h-0 min-w-0 flex-col overflow-hidden rounded-[var(--radius-panel)] border border-line bg-surface', className)}
        >
            <div className="flex h-8 shrink-0 items-center gap-2 border-b border-line pr-1.5 pl-3">
                {icon ? <span className="text-faint">{icon}</span> : null}
                {title ? <span className="shrink-0 font-mono text-[10.5px] tracking-[0.12em] text-muted uppercase">{title}</span> : null}
                {subtitle ? <span className="selectable min-w-0 truncate font-mono text-[11.5px] text-fg">{subtitle}</span> : null}
                {actions ? <div className="ml-auto flex min-w-0 shrink-0 items-center gap-1">{actions}</div> : null}
            </div>
            <div className="min-h-0 flex-1">{children}</div>
        </section>
    )
}

export function Placeholder({text}: {text: string}) {
    return <p className="mx-auto max-w-[420px] px-6 text-center text-[12.5px] leading-relaxed text-muted">{text}</p>
}

// PhaseBadge marca lo que todavía no está: «Fase S5».
export function PhaseBadge({text, title}: {text: string; title: string}) {
    return (
        <span className="rounded border border-line-strong px-1.5 py-px font-mono text-[10px] text-faint" title={title}>
            {text}
        </span>
    )
}
