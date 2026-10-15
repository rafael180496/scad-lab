import type {ButtonHTMLAttributes} from 'react'
import {cn} from '../../lib/cn'

type Variant = 'primary' | 'ghost' | 'outline' | 'danger'

const styles: Record<Variant, string> = {
    primary: 'bg-accent text-accent-fg hover:brightness-110 border border-transparent',
    ghost: 'text-muted hover:text-fg hover:bg-surface-2 border border-transparent',
    outline: 'border border-line-strong text-fg hover:bg-surface-2',
    danger: 'border border-transparent text-danger hover:bg-surface-2',
}

// Todo control interactivo lleva `title` que explique qué hace (regla de
// claude/04-frontend.md): por eso es obligatorio acá.
export function Button({
    variant = 'outline',
    className,
    title,
    ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {variant?: Variant; title: string}) {
    return (
        <button
            type="button"
            title={title}
            className={cn(
                'no-drag inline-flex h-7 shrink-0 items-center gap-1.5 rounded-md whitespace-nowrap px-2.5 text-[12px] font-medium transition-[background-color,filter,color] duration-150 disabled:pointer-events-none disabled:opacity-40',
                styles[variant],
                className,
            )}
            {...rest}
        />
    )
}

export function IconButton({className, title, ...rest}: ButtonHTMLAttributes<HTMLButtonElement> & {title: string}) {
    return (
        <button
            type="button"
            title={title}
            aria-label={title}
            className={cn(
                'no-drag inline-flex size-7 items-center justify-center rounded-md text-muted transition-colors duration-150 hover:bg-surface-2 hover:text-fg disabled:pointer-events-none disabled:opacity-40',
                className,
            )}
            {...rest}
        />
    )
}
