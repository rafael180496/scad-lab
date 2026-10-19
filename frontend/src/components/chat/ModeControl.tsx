import {useId, useState} from 'react'
import {Bot, Check, ChevronDown, Eye} from 'lucide-react'
import {useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {Popover, listKeys} from './Popover'

// Modo del chat (S12.b): «Solo consulta» o «Agente». Con un CLI, «Solo consulta» es
// el modo `ask` de mini-tools (puede leer y usar las herramientas de lectura de
// ScadLab) y «Agente» su modo `edit` (además crea checkpoints y exporta); en
// ninguno corre comandos de shell. Con un servidor de modelos (Hermes) los dos modos
// son el interruptor «Agente» de S12. Cada opción trae su explicación: es lo que
// cambia lo que el modelo tiene permitido hacer, y tiene que leerse antes de enviar.

export type ModeKind = 'cli' | 'server'

export function modeTexts(t: ReturnType<typeof useT>, kind: ModeKind, agent: boolean) {
    const m = t.chat.mode
    const by = kind === 'cli' ? m.cli : m.server
    return agent ? by.agent : by.ask
}

export type ModeControlProps = {
    agent: boolean
    onChange: (agent: boolean) => void
    kind: ModeKind
    /** Por qué «Agente» no se puede usar (el modelo no llama herramientas); vacío = se puede. */
    unavailable: string
    disabled?: boolean
}

export function ModeControl({agent, onChange, kind, unavailable, disabled, iconOnly, className}: ModeControlProps & {iconOnly?: boolean; className?: string}) {
    const t = useT()
    const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
    const [open, setOpen] = useState(false)
    const [active, setActive] = useState(0)
    const listId = useId()
    // Sin herramientas disponibles el modo efectivo es «Solo consulta», diga lo que diga el interruptor.
    const effective = agent && !unavailable
    const cur = modeTexts(t, kind, effective)
    const Icon = effective ? Bot : Eye
    const options = [
        {agent: false, Icon: Eye, text: modeTexts(t, kind, false), off: ''},
        {agent: true, Icon: Bot, text: modeTexts(t, kind, true), off: unavailable},
    ]
    const choose = (i: number) => {
        const o = options[i]
        if (o.off) return
        onChange(o.agent)
        setOpen(false)
    }

    return (
        <>
            <button
                ref={setAnchor}
                type="button"
                data-testid="mode-control"
                data-mode={effective ? 'agent' : 'ask'}
                title={`${cur.label}: ${cur.hint}`}
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-controls={open ? listId : undefined}
                disabled={disabled}
                onClick={() => {
                    setActive(effective ? 1 : 0)
                    setOpen((o) => !o)
                }}
                className={cn(
                    'no-drag flex h-7 shrink-0 items-center gap-1.5 rounded-md border px-2 text-ui-12 transition-colors disabled:opacity-40',
                    effective ? 'border-accent/50 bg-accent-soft text-fg hover:border-accent' : 'border-line bg-surface-2 text-fg hover:border-line-strong',
                    className,
                )}
            >
                <Icon className={cn('icon-ui-14 shrink-0', effective ? 'text-accent' : 'text-muted')} />
                {iconOnly ? null : <span className="truncate">{cur.label}</span>}
                <ChevronDown className="icon-ui-12 shrink-0 text-faint" />
            </button>
            {open && anchor ? (
                <Popover
                    anchor={anchor}
                    onClose={() => setOpen(false)}
                    width={340}
                    align="end"
                    label={t.chat.mode.title}
                    onKeyDown={(e) => listKeys(e, {rows: options.length, active, setActive, onEnter: () => choose(active)})}
                >
                    <div id={listId} role="listbox" aria-label={t.chat.mode.title} className="min-h-0 overflow-y-auto py-1">
                        {options.map((o, i) => (
                            <button
                                key={String(o.agent)}
                                type="button"
                                role="option"
                                aria-selected={o.agent === effective}
                                aria-disabled={!!o.off}
                                data-mode-option={o.agent ? 'agent' : 'ask'}
                                title={o.off || `${o.text.label}: ${o.text.hint}`}
                                onClick={() => choose(i)}
                                onPointerMove={() => setActive(i)}
                                className={cn('flex w-full items-start gap-2.5 px-3 py-2 text-left transition-colors', i === active && 'bg-surface-2', o.off && 'cursor-default opacity-60')}
                            >
                                <o.Icon className={cn('mt-0.5 icon-ui-14 shrink-0', o.agent ? 'text-accent' : 'text-muted')} />
                                <span className="min-w-0 flex-1">
                                    <span className="block text-fg">{o.text.label}</span>
                                    <span className="block text-ui-11 leading-snug text-muted">{o.off || o.text.hint}</span>
                                </span>
                                <span className="mt-0.5 flex icon-ui-14 shrink-0 items-center justify-center text-accent">{o.agent === effective ? <Check className="icon-ui-14" /> : null}</span>
                            </button>
                        ))}
                    </div>
                </Popover>
            ) : null}
        </>
    )
}
