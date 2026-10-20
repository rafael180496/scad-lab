import {useId, useState} from 'react'
import {Check, ChevronDown, Wrench} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {Popover, listKeys} from './Popover'

// Perfil de herramientas de la conversación (S17): qué herramientas de ScadLab le declara el chat al modelo.
// «Auto» declara las esenciales cuando la ventana del modelo es chica (< 32 768 tokens) y todas si no;
// «Esenciales» y «Todas» lo fijan a mano. Solo con servidores de modelos: un CLI trae su propio catálogo.
// Es un selector compacto, hermano de ModeControl: sale en la barra del chat y, en un panel angosto, pasa al
// menú «…» (ver ChatHeader).

export type ToolProfile = '' | 'essential' | 'full'

export type ToolsControlProps = {
    value: ToolProfile
    /** Lo que «Auto» resolvió en el último turno ('' = todavía no se sabe). */
    resolved: ToolProfile
    onChange: (profile: ToolProfile) => void
    disabled?: boolean
}

/** El rótulo de cada opción; «Auto» dice en qué se resolvió cuando se sabe. */
export function toolProfileLabel(t: ReturnType<typeof useT>, value: ToolProfile, resolved: ToolProfile): string {
    const c = t.chat.tools
    if (value === 'essential') return c.essential
    if (value === 'full') return c.full
    return resolved === 'essential' ? c.autoEssential : resolved === 'full' ? c.autoFull : c.auto
}

export function toolProfileHint(t: ReturnType<typeof useT>, value: ToolProfile): string {
    const c = t.chat.tools
    return value === 'essential' ? c.essentialHint : value === 'full' ? c.fullHint : c.autoHint
}

export const TOOL_PROFILES: ToolProfile[] = ['', 'essential', 'full']

export function ToolsControl({value, resolved, onChange, disabled, iconOnly, className}: ToolsControlProps & {iconOnly?: boolean; className?: string}) {
    const t = useT()
    const c = t.chat.tools
    const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
    const [open, setOpen] = useState(false)
    const [active, setActive] = useState(0)
    const listId = useId()
    const label = toolProfileLabel(t, value, resolved)
    const choose = (i: number) => {
        onChange(TOOL_PROFILES[i])
        setOpen(false)
    }
    return (
        <>
            <button
                ref={setAnchor}
                type="button"
                data-testid="tools-control"
                data-tools={value || 'auto'}
                title={fmt(c.hint, {current: label})}
                aria-label={`${c.label}: ${label}`}
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-controls={open ? listId : undefined}
                disabled={disabled}
                onClick={() => {
                    setActive(TOOL_PROFILES.indexOf(value))
                    setOpen((o) => !o)
                }}
                className={cn(
                    'no-drag flex h-7 shrink-0 items-center gap-1.5 rounded-md border border-line bg-surface-2 px-2 text-ui-12 text-fg transition-colors hover:border-line-strong disabled:opacity-40',
                    className,
                )}
            >
                <Wrench className="icon-ui-14 shrink-0 text-muted" />
                {iconOnly ? null : <span className="truncate">{label}</span>}
                <ChevronDown className="icon-ui-12 shrink-0 text-faint" />
            </button>
            {open && anchor ? (
                <Popover
                    anchor={anchor}
                    onClose={() => setOpen(false)}
                    width={340}
                    align="end"
                    label={c.title}
                    onKeyDown={(e) => listKeys(e, {rows: TOOL_PROFILES.length, active, setActive, onEnter: () => choose(active)})}
                >
                    <div id={listId} role="listbox" aria-label={c.title} className="min-h-0 overflow-y-auto py-1">
                        {TOOL_PROFILES.map((p, i) => (
                            <button
                                key={p || 'auto'}
                                type="button"
                                role="option"
                                aria-selected={p === value}
                                data-tools-option={p || 'auto'}
                                title={toolProfileHint(t, p)}
                                onClick={() => choose(i)}
                                onPointerMove={() => setActive(i)}
                                className={cn('flex w-full items-start gap-2.5 px-3 py-2 text-left transition-colors', i === active && 'bg-surface-2')}
                            >
                                <Wrench className="mt-0.5 icon-ui-14 shrink-0 text-muted" />
                                <span className="min-w-0 flex-1">
                                    <span className="block text-fg">{toolProfileLabel(t, p, resolved)}</span>
                                    <span className="block text-ui-11 leading-snug text-muted">{toolProfileHint(t, p)}</span>
                                </span>
                                <span className="mt-0.5 flex icon-ui-14 shrink-0 items-center justify-center text-accent">{p === value ? <Check className="icon-ui-14" /> : null}</span>
                            </button>
                        ))}
                    </div>
                </Popover>
            ) : null}
        </>
    )
}
