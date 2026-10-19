import {useEffect, useId, useMemo, useRef, useState} from 'react'
import {Check, ChevronDown, CirclePlus, TriangleAlert} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {isCLI, sortProviders, type ChatProviderItem} from '../../lib/chatProviders'
import {useProvidersHealth, type Health} from '../../lib/chatHealth'
import {HealthDot} from './HealthIndicators'
import {Popover, listKeys} from './Popover'
import {providerIcon} from './providerIcons'

// Selector de proveedor propio (S12.b), no un <select> nativo: los CLIs detectados
// primero y después los servidores, cada uno con su ícono, su fabricante, un punto
// de estado (verde responde, rojo no) y un check en el elegido; al final
// «Agregar proveedor de IA…», que lleva a Configuración. Se maneja con el teclado:
// flechas, Inicio/Fin, Enter y Esc.
//
// Los CLIs no se configuran: aparecen solos si están instalados. Abrir el
// selector vuelve a detectar (`onOpen`), así que uno recién instalado se ve sin
// reiniciar. Es el selector de proveedor de mini-tools (`Select` con
// ProviderIconWithHealth y la opción de cierre ADD_SERVER) con lucide.

type Row = {kind: 'provider'; p: ChatProviderItem} | {kind: 'missing'; id: string} | {kind: 'add'}

export function ProviderPicker({
    providers,
    value,
    onChange,
    onAdd,
    onOpen,
    disabled,
    disabledHint,
    title,
    iconOnly,
    health,
    className,
}: {
    providers: ChatProviderItem[]
    value: string
    onChange: (id: string) => void
    onAdd: () => void
    /** Se llama al abrir: vuelve a detectar los CLIs y relee los servidores. */
    onOpen: () => void
    disabled?: boolean
    disabledHint?: string
    title: string
    /** Solo el ícono con su punto (panel muy angosto). */
    iconOnly?: boolean
    /** Salud del proveedor elegido (la píldora la dibuja el encabezado). */
    health: Health
    className?: string
}) {
    const t = useT()
    const p = t.chat.provider
    const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
    const [open, setOpen] = useState(false)
    const [active, setActive] = useState(0)
    const listId = useId()
    const list = useRef<HTMLDivElement>(null)

    const sorted = useMemo(() => sortProviders(providers), [providers])
    const current = sorted.find((x) => x.id === value)
    // El proveedor de un chat viejo puede haberse desinstalado o apagado: sigue
    // visible (y se puede cambiar) en vez de dejar el botón en blanco.
    const missing = !!value && !current
    const rows: Row[] = useMemo(
        () => [...(missing ? [{kind: 'missing', id: value} as Row] : []), ...sorted.map((x) => ({kind: 'provider', p: x}) as Row), {kind: 'add'}],
        [sorted, missing, value],
    )

    // Un punto por fila mientras el selector está abierto; el elegido ya se sondea aparte.
    const ids = useMemo(() => sorted.map((x) => x.id), [sorted])
    const healths = useProvidersHealth(ids, open)

    useEffect(() => {
        if (!open) return
        const i = rows.findIndex((r) => (r.kind === 'provider' ? r.p.id === value : r.kind === 'missing'))
        setActive(Math.max(0, i))
    }, [open, rows, value])
    useEffect(() => {
        list.current?.querySelector<HTMLElement>(`[data-row="${active}"]`)?.scrollIntoView({block: 'nearest'})
    }, [active])

    const Icon = providerIcon(current)
    const name = current?.name ?? (missing ? value : p.pick)
    const choose = (r: Row | undefined) => {
        if (!r) return
        setOpen(false)
        if (r.kind === 'add') onAdd()
        else if (r.kind === 'provider') onChange(r.p.id)
    }

    return (
        <>
            <button
                ref={setAnchor}
                type="button"
                data-testid="provider-picker"
                title={disabled && disabledHint ? disabledHint : title}
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-controls={open ? listId : undefined}
                disabled={disabled}
                onClick={() => {
                    if (!open) onOpen()
                    setOpen((o) => !o)
                }}
                className={cn(
                    'no-drag flex h-7 min-w-0 items-center gap-1.5 rounded-md border border-line bg-surface-2 text-ui-12 text-fg transition-colors hover:border-line-strong disabled:opacity-40',
                    iconOnly ? 'px-1.5' : 'px-2',
                    className,
                )}
            >
                <span className="relative inline-flex shrink-0">
                    <Icon className={cn('icon-ui-14', current ? 'text-accent' : 'text-faint')} />
                    {value ? <HealthDot health={health} name={name} overlay className="ring-surface-2" /> : null}
                </span>
                {iconOnly ? null : <span className={cn('min-w-0 flex-1 truncate text-left', !current && 'text-faint')}>{name}</span>}
                <ChevronDown className="icon-ui-12 shrink-0 text-faint" />
            </button>

            {open && anchor ? (
                <Popover
                    anchor={anchor}
                    onClose={() => setOpen(false)}
                    width={340}
                    label={p.title}
                    onKeyDown={(e) => listKeys(e, {rows: rows.length, active, setActive, onEnter: () => choose(rows[active])})}
                >
                    <div ref={list} id={listId} role="listbox" aria-label={p.title} className="min-h-0 overflow-y-auto py-1">
                        {rows.map((r, i) => {
                            if (r.kind === 'add') return null // va fijo bajo la lista, siempre a la vista
                            if (r.kind === 'missing') {
                                return (
                                    <button
                                        key="missing"
                                        type="button"
                                        data-row={i}
                                        role="option"
                                        aria-selected
                                        title={fmt(p.missingHint, {id: r.id})}
                                        onClick={() => setOpen(false)}
                                        onPointerMove={() => setActive(i)}
                                        className={cn('flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors', i === active && 'bg-surface-2')}
                                    >
                                        <TriangleAlert className="icon-ui-14 shrink-0 text-warn" />
                                        <span className="min-w-0 flex-1">
                                            <span className="block truncate text-fg">{r.id}</span>
                                            <span className="block truncate text-ui-11 text-warn">{p.missing}</span>
                                        </span>
                                        <Check className="icon-ui-14 shrink-0 text-accent" />
                                    </button>
                                )
                            }
                            const x = r.p
                            const RowIcon = providerIcon(x)
                            const h = x.id === value ? health : (healths[x.id] ?? null)
                            // El rótulo de cada grupo va sobre su primera fila.
                            const si = sorted.indexOf(x)
                            const first = isCLI(x.id) && !sorted.slice(0, si).some((o) => isCLI(o.id))
                            const firstServer = !isCLI(x.id) && !sorted.slice(0, si).some((o) => !isCLI(o.id))
                            const detail = isCLI(x.id) ? fmt(p.cliDetail, {vendor: x.vendor}) : x.remote ? p.serverRemote : p.serverLocal
                            return (
                                <div key={x.id}>
                                    {first || firstServer ? (
                                        <p className="px-3 pt-1 pb-0.5 font-mono text-ui-10 tracking-[0.12em] text-faint uppercase">{first ? p.groupCli : p.groupServers}</p>
                                    ) : null}
                                    <button
                                        type="button"
                                        data-row={i}
                                        data-provider={x.id}
                                        role="option"
                                        aria-selected={x.id === value}
                                        title={fmt(p.rowHint, {name: x.name})}
                                        onClick={() => choose(r)}
                                        onPointerMove={() => setActive(i)}
                                        className={cn('flex w-full items-center gap-2.5 px-3 py-0.5 text-left transition-colors', i === active && 'bg-surface-2')}
                                    >
                                        <span className="relative inline-flex shrink-0">
                                            <RowIcon className="icon-ui-16 text-accent" />
                                            <HealthDot health={h} name={x.name} overlay />
                                        </span>
                                        <span className="min-w-0 flex-1">
                                            <span className="block truncate text-fg">{x.name}</span>
                                            <span className="block truncate font-mono text-ui-10-5 text-faint">{detail}</span>
                                        </span>
                                        {h?.status === 'ok' && !isCLI(x.id) ? <span className="shrink-0 font-mono text-ui-10-5 text-muted tabular-nums">{fmt(p.latency, {ms: h.latencyMs})}</span> : null}
                                        {h?.status === 'error' ? <span className="shrink-0 text-ui-10-5 text-danger">{p.down}</span> : null}
                                        <span className="flex icon-ui-14 shrink-0 items-center justify-center text-accent">{x.id === value ? <Check className="icon-ui-14" /> : null}</span>
                                    </button>
                                </div>
                            )
                        })}
                    </div>
                    {/* «Agregar proveedor…» y la leyenda quedan fijos: la lista hace scroll y el camino a agregar uno no se pierde. */}
                    <button
                        type="button"
                        data-row={rows.length - 1}
                        role="option"
                        aria-selected={false}
                        title={p.addHint}
                        onClick={() => choose(rows[rows.length - 1])}
                        onPointerMove={() => setActive(rows.length - 1)}
                        className={cn(
                            'flex w-full shrink-0 items-center gap-2 border-t border-line px-3 py-2 text-left text-accent transition-colors',
                            active === rows.length - 1 && 'bg-surface-2',
                        )}
                    >
                        <CirclePlus className="icon-ui-14 shrink-0" />
                        {p.add}
                    </button>
                    <p className="shrink-0 border-t border-line px-3 py-1.5 text-ui-10-5 leading-snug text-faint">{p.legend}</p>
                </Popover>
            ) : null}
        </>
    )
}
