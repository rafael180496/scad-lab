import {useState, type KeyboardEvent} from 'react'
import {locale} from '../../i18n'
import {cn} from '../../lib/cn'

// Campo numérico del editor de STL: escribes y confirmas con Intro o al salir (cada confirmación es UNA
// operación en la pila; un campo que aplicara cada tecla llenaría la pila de pasos a medias). Las
// flechas ↑/↓ suman o restan `step` (Shift ×10, Alt ×0,1) y confirman al instante. Esc devuelve el valor
// que había. Acepta coma o punto decimal.

function show(v: number, digits: number): string {
    return v.toLocaleString(locale(), {maximumFractionDigits: digits, useGrouping: false})
}

function parse(text: string): number | null {
    const v = Number(text.trim().replace(',', '.'))
    return text.trim() !== '' && Number.isFinite(v) ? v : null
}

export function NumField({
    label,
    value,
    onCommit,
    title,
    step = 1,
    digits = 2,
    min,
    max,
    unit,
    disabled,
    className,
}: {
    /** Rótulo corto (X, Y, Z, Ø…): no se traduce. */
    label: string
    value: number
    onCommit: (v: number) => void
    title: string
    step?: number
    digits?: number
    min?: number
    max?: number
    unit?: string
    disabled?: boolean
    className?: string
}) {
    const [draft, setDraft] = useState<string | null>(null)
    const clamp = (v: number) => Math.min(max ?? Infinity, Math.max(min ?? -Infinity, v))

    const commit = (text: string) => {
        setDraft(null)
        const v = parse(text)
        if (v === null) return
        const next = clamp(v)
        if (Math.abs(next - value) > 1e-9) onCommit(next)
    }
    const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
        if (e.key === 'Enter') {
            e.preventDefault()
            commit(e.currentTarget.value)
            e.currentTarget.select()
        } else if (e.key === 'Escape' && draft !== null) {
            // Con un borrador, Esc lo descarta y NO sigue hasta el editor (que lo leería como «salir»).
            e.preventDefault()
            e.stopPropagation()
            setDraft(null)
        } else if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
            e.preventDefault()
            const base = parse(e.currentTarget.value) ?? value
            const k = (e.shiftKey ? 10 : e.altKey ? 0.1 : 1) * step * (e.key === 'ArrowUp' ? 1 : -1)
            const next = clamp(Math.round((base + k) * 1e6) / 1e6)
            setDraft(null)
            if (Math.abs(next - value) > 1e-9) onCommit(next)
        }
    }

    return (
        <label className={cn('flex min-w-0 items-center gap-1', disabled && 'opacity-50', className)} title={title}>
            <span className="w-3 shrink-0 text-center font-mono text-ui-10 text-faint">{label}</span>
            <input
                type="text"
                inputMode="decimal"
                title={title}
                aria-label={title}
                disabled={disabled}
                data-dirty={draft !== null ? '' : undefined}
                value={draft ?? show(value, digits)}
                onChange={(e) => setDraft(e.target.value)}
                onFocus={(e) => e.currentTarget.select()}
                onBlur={(e) => draft !== null && commit(e.currentTarget.value)}
                onKeyDown={onKeyDown}
                spellCheck={false}
                className="selectable h-6 min-w-0 flex-1 rounded border border-line bg-surface-2 px-1.5 text-right font-mono text-ui-11 text-fg tabular-nums outline-none transition-colors focus:border-accent"
            />
            {unit ? <span className="w-4 shrink-0 font-mono text-ui-10 text-faint">{unit}</span> : null}
        </label>
    )
}
