import {useEffect, useId, useMemo, useRef, useState} from 'react'
import {Check, ChevronDown, Cpu, Eye, RefreshCw, Search, Sparkles, Wrench} from 'lucide-react'
import type {main} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {contextLabel, priceLabel} from '../../lib/modelServers'
import {Input} from '../ui/Field'
import {Popover, listKeys} from './Popover'

// Selector de modelo (S12.b), evolución del combobox de S12: un botón con el
// modelo elegido que abre una lista filtrable. Primero la opción «Por defecto» con
// su explicación (qué modelo se usa si no se elige ninguno), después los modelos con
// su nombre —un CLI lista «Gemini 4 Argon (High)», un servidor su id— y, en los de
// un servidor, las insignias de visión y herramientas, la ventana y el precio.
//
// El modelo se puede escribir a mano aunque el proveedor no lo liste («Usar
// «x»»): un proxy, un alias o un modelo recién bajado no figuran siempre, y un
// servidor sin /models tampoco da lista.

// Cuántas filas se pintan a la vez: más allá, se pide acotar escribiendo.
const MAX_ROWS = 80

/** Las dos insignias de un modelo: solo se pintan cuando el servidor dijo «sí». */
export function CapBadges({m, className}: {m: Pick<main.ModelInfo, 'vision' | 'tools'>; className?: string}) {
    const t = useT()
    return (
        <span className={cn('flex shrink-0 items-center gap-1', className)}>
            {m.vision ? (
                <span title={t.chat.visionBadge} className="flex text-info">
                    <Eye className="icon-ui-12" aria-label={t.chat.visionBadge} />
                </span>
            ) : null}
            {m.tools ? (
                <span title={t.chat.toolsBadge} className="flex text-accent">
                    <Wrench className="icon-ui-12" aria-label={t.chat.toolsBadge} />
                </span>
            ) : null}
        </span>
    )
}

type Row = {kind: 'default'} | {kind: 'model'; m: main.ModelInfo} | {kind: 'typed'; id: string}

export function ModelPicker({
    models,
    value,
    onChange,
    defaultHint,
    loading,
    error,
    onReload,
    disabled,
    disabledHint,
    cli,
    iconOnly,
    align = 'end',
    className,
}: {
    models: main.ModelInfo[]
    /** '' = «Por defecto». */
    value: string
    onChange: (id: string) => void
    /** Qué modelo se usa con «Por defecto» (lo informa el catálogo). */
    defaultHint: string
    loading: boolean
    error: string
    /** Sin esto no hay botón de refrescar (la lista no viene de una consulta que se pueda repetir). */
    onReload?: () => void
    disabled?: boolean
    disabledHint?: string
    /** Los modelos de un CLI no llevan insignias: todos usan herramientas. */
    cli: boolean
    /** Solo el ícono (panel muy angosto). */
    iconOnly?: boolean
    /** Con qué borde del botón se alinea la lista. */
    align?: 'start' | 'end'
    className?: string
}) {
    const t = useT()
    const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
    const [open, setOpen] = useState(false)
    const [q, setQ] = useState('')
    const [active, setActive] = useState(0)
    const list = useRef<HTMLDivElement>(null)
    const listId = useId()

    const query = q.trim().toLowerCase()
    const typed = q.trim()
    const filtered = useMemo(
        () => (query ? models.filter((m) => `${m.id} ${m.label ?? ''} ${m.description ?? ''}`.toLowerCase().includes(query)) : models),
        [models, query],
    )
    const shown = filtered.slice(0, MAX_ROWS)
    // «Por defecto» siempre que no se esté buscando otra cosa.
    const showDefault = query === '' || t.chat.modelDefault.toLowerCase().includes(query)
    // «Usar lo escrito» solo si no es ya un modelo de la lista.
    const canUseTyped = typed !== '' && !models.some((m) => m.id === typed)
    const rows: Row[] = useMemo(
        () => [...(showDefault ? [{kind: 'default'} as Row] : []), ...shown.map((m) => ({kind: 'model', m}) as Row), ...(canUseTyped ? [{kind: 'typed', id: typed} as Row] : [])],
        [showDefault, shown, canUseTyped, typed],
    )

    useEffect(() => setActive(0), [query, open])
    useEffect(() => {
        list.current?.querySelector<HTMLElement>(`[data-row="${active}"]`)?.scrollIntoView({block: 'nearest'})
    }, [active])

    const current = value ? models.find((m) => m.id === value) : undefined
    const shownName = value === '' ? t.chat.modelDefault : (current?.label || value)
    const pick = (r: Row | undefined) => {
        if (!r) return
        onChange(r.kind === 'default' ? '' : r.kind === 'model' ? r.m.id : r.id)
        setOpen(false)
        setQ('')
    }
    const hint = error ? fmt(t.chat.modelsError, {msg: error}) : disabled && disabledHint ? disabledHint : value === '' ? `${t.chat.modelDefault}: ${defaultHint}` : t.chat.modelButtonHint

    return (
        <>
            <button
                ref={setAnchor}
                type="button"
                title={hint}
                data-testid="model-picker"
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-controls={open ? listId : undefined}
                disabled={disabled}
                onClick={() => setOpen((o) => !o)}
                className={cn(
                    'no-drag flex h-7 min-w-0 items-center gap-1.5 rounded-md border border-line bg-surface-2 text-ui-12 text-fg transition-colors hover:border-line-strong disabled:opacity-40',
                    iconOnly ? 'px-1.5' : 'px-2',
                    error && 'border-warn',
                    className,
                )}
            >
                <Cpu className={cn('icon-ui-14 shrink-0', error ? 'text-warn' : 'text-faint')} />
                {iconOnly ? null : (
                    <>
                        <span className={cn('min-w-0 flex-1 truncate text-left', value !== '' && !current?.label && 'font-mono text-ui-11-5', value === '' && 'text-muted')}>
                            {shownName}
                        </span>
                        {current && !cli ? <CapBadges m={current} /> : null}
                    </>
                )}
                <ChevronDown className="icon-ui-12 shrink-0 text-faint" />
            </button>

            {open && anchor ? (
                <Popover
                    anchor={anchor}
                    onClose={() => {
                        setOpen(false)
                        setQ('')
                    }}
                    width={420}
                    align={align}
                    label={t.chat.modelPickerTitle}
                    focusFirst={false}
                    onKeyDown={(e) => listKeys(e, {rows: rows.length, active, setActive, onEnter: () => pick(rows[active])})}
                >
                    <div className="flex shrink-0 items-center gap-1.5 border-b border-line px-2 py-1.5">
                        <Search className="icon-ui-14 shrink-0 text-faint" />
                        <Input
                            autoFocus
                            value={q}
                            placeholder={t.chat.searchModels}
                            title={t.chat.searchModelsHint}
                            role="combobox"
                            aria-expanded
                            aria-controls={listId}
                            aria-activedescendant={`${listId}-${active}`}
                            onChange={(e) => setQ(e.target.value)}
                            className="h-7 border-0 bg-transparent px-0 focus:border-0"
                        />
                        <span className="shrink-0 font-mono text-ui-10 text-faint">{loading ? t.chat.loadingModels : fmt(t.chat.modelsCount, {n: models.length})}</span>
                        {onReload ? (
                            <button
                                type="button"
                                title={error ? fmt(t.chat.modelsError, {msg: error}) : t.chat.reloadModels}
                                aria-label={t.chat.reloadModels}
                                onClick={onReload}
                                className="no-drag flex size-6 shrink-0 items-center justify-center rounded-md text-muted transition-colors hover:bg-surface-2 hover:text-fg"
                            >
                                <RefreshCw className={cn('icon-ui-14', loading && 'animate-spin', error && 'text-warn')} />
                            </button>
                        ) : null}
                    </div>
                    <div ref={list} id={listId} role="listbox" className="min-h-0 overflow-y-auto py-1">
                        {error ? <p className="selectable px-3 py-1.5 text-ui-11-5 text-warn">{fmt(t.chat.modelsError, {msg: error})}</p> : null}
                        {rows.length === 0 ? <p className="px-3 py-2 text-ui-12 text-faint">{fmt(t.chat.noModelMatch, {q: typed})}</p> : null}
                        {rows.map((r, i) => {
                            if (r.kind === 'default') {
                                return (
                                    <button
                                        key="default"
                                        id={`${listId}-${i}`}
                                        data-row={i}
                                        type="button"
                                        role="option"
                                        aria-selected={value === ''}
                                        title={`${t.chat.modelDefault}: ${defaultHint}`}
                                        onClick={() => pick(r)}
                                        onPointerMove={() => setActive(i)}
                                        className={cn('flex w-full items-start gap-2 border-b border-line px-3 py-1.5 text-left transition-colors', i === active && 'bg-surface-2')}
                                    >
                                        <span className="mt-0.5 flex icon-ui-12 shrink-0 items-center justify-center text-accent">{value === '' ? <Check className="icon-ui-12" /> : <Sparkles className="icon-ui-12 text-faint" />}</span>
                                        <span className="min-w-0 flex-1">
                                            <span className="block text-fg">{t.chat.modelDefault}</span>
                                            <span className="block text-ui-11 leading-snug text-muted">{defaultHint}</span>
                                        </span>
                                    </button>
                                )
                            }
                            if (r.kind === 'typed') {
                                return (
                                    <button
                                        key="typed"
                                        id={`${listId}-${i}`}
                                        data-row={i}
                                        type="button"
                                        role="option"
                                        aria-selected={false}
                                        title={fmt(t.chat.useTypedHint, {id: r.id})}
                                        onClick={() => pick(r)}
                                        onPointerMove={() => setActive(i)}
                                        className={cn('flex w-full items-center gap-2 border-t border-line px-3 py-1.5 text-left text-ui-12 text-accent', i === active && 'bg-surface-2')}
                                    >
                                        {fmt(t.chat.useTyped, {id: r.id})}
                                    </button>
                                )
                            }
                            const m = r.m
                            const meta = [
                                m.label && m.label !== m.id ? m.id : '',
                                m.description ?? '',
                                contextLabel(m.contextTokens) && fmt(t.chat.contextTokens, {n: contextLabel(m.contextTokens)}),
                                priceLabel(m.promptPricePerM, m.completionPricePerM),
                            ]
                                .filter(Boolean)
                                .join(' · ')
                            return (
                                <button
                                    key={m.id}
                                    id={`${listId}-${i}`}
                                    data-row={i}
                                    type="button"
                                    role="option"
                                    aria-selected={m.id === value}
                                    title={m.description ? `${m.label || m.id}\n${m.description}` : m.id}
                                    onClick={() => pick(r)}
                                    onPointerMove={() => setActive(i)}
                                    className={cn('flex w-full items-center gap-2 px-3 py-1 text-left transition-colors', i === active && 'bg-surface-2')}
                                >
                                    <span className="flex icon-ui-12 shrink-0 items-center justify-center text-accent">{m.id === value ? <Check className="icon-ui-12" /> : null}</span>
                                    <span className="min-w-0 flex-1">
                                        <span className={cn('block truncate text-fg', m.label ? 'text-ui-12' : 'font-mono text-ui-11-5')}>{m.label || m.id}</span>
                                        {meta ? <span className="block truncate font-mono text-ui-10 text-faint">{meta}</span> : null}
                                    </span>
                                    {cli ? null : <CapBadges m={m} />}
                                </button>
                            )
                        })}
                        {filtered.length > shown.length ? <p className="px-3 py-1.5 text-ui-11 text-faint">{fmt(t.chat.moreModels, {n: filtered.length - shown.length})}</p> : null}
                    </div>
                    <p className="shrink-0 border-t border-line px-3 py-1.5 text-ui-10-5 leading-snug text-faint">{cli ? t.chat.modelLegendCli : t.chat.modelLegend}</p>
                </Popover>
            ) : null}
        </>
    )
}
