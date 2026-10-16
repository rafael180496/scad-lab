import {cn} from '../../lib/cn'

// Barra de progreso. `value` va de 0 a 1; null = no se sabe cuánto falta (una
// franja que late, p. ej. mientras ditto copia la app y no informa avance).
export function ProgressBar({value, label, className}: {value: number | null; label: string; className?: string}) {
    const pct = value == null ? undefined : Math.round(Math.min(1, Math.max(0, value)) * 100)
    return (
        <div
            role="progressbar"
            aria-label={label}
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={pct}
            className={cn('h-1.5 w-full overflow-clip rounded-full bg-surface-2 ring-1 ring-line', className)}
        >
            <div
                className={cn('h-full rounded-full bg-accent transition-[width] duration-150', value == null && 'w-1/3 animate-pulse')}
                style={value == null ? undefined : {width: `${pct}%`}}
            />
        </div>
    )
}
