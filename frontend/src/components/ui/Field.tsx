import type {InputHTMLAttributes, ReactNode, SelectHTMLAttributes} from 'react'
import {cn} from '../../lib/cn'

export function Field({label, hint, children}: {label: string; hint?: ReactNode; children: ReactNode}) {
    return (
        <label className="flex flex-col gap-1">
            <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{label}</span>
            {children}
            {hint ? <span className="text-ui-11-5 text-faint">{hint}</span> : null}
        </label>
    )
}

const control =
    'h-8 w-full rounded-md border border-line bg-surface-2 px-2.5 text-ui-12-5 text-fg placeholder:text-faint outline-none transition-colors focus:border-accent'

export function Input({className, ...rest}: InputHTMLAttributes<HTMLInputElement>) {
    return <input className={cn(control, 'selectable', className)} spellCheck={false} {...rest} />
}

export function Select({className, ...rest}: SelectHTMLAttributes<HTMLSelectElement>) {
    return <select className={cn(control, 'pr-7', className)} {...rest} />
}

export function Toggle({checked, onChange, title}: {checked: boolean; onChange: (v: boolean) => void; title: string}) {
    return (
        <button
            type="button"
            role="switch"
            aria-checked={checked}
            title={title}
            onClick={() => onChange(!checked)}
            className={cn(
                'relative h-[18px] w-8 shrink-0 rounded-full border transition-colors duration-150',
                checked ? 'border-accent bg-accent' : 'border-line-strong bg-surface-2',
            )}
        >
            <span
                className={cn(
                    'absolute top-1/2 left-[2px] size-3 -translate-y-1/2 rounded-full transition-transform duration-150',
                    checked ? 'translate-x-[14px] bg-white' : 'translate-x-0 bg-muted',
                )}
            />
        </button>
    )
}
