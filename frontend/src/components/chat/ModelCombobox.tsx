import {useEffect, useId, useMemo, useRef, useState} from 'react'
import {Check, ChevronDown, Eye, RefreshCw, Search, Wrench} from 'lucide-react'
import type {main} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {contextLabel, priceLabel} from '../../lib/modelServers'
import {IconButton} from '../ui/Button'
import {Input} from '../ui/Field'

// Selector de modelo con búsqueda (S12): un botón con el modelo elegido que abre
// una lista filtrable con las insignias de lo que el servidor sabe de cada uno
// —ve imágenes, usa herramientas, ventana y precio—. El selector de modelos de
// mini-tools no tiene búsqueda; acá la lista de OpenRouter pasa de 300.
//
// El modelo se puede escribir a mano aunque el servidor no lo liste («Usar
// «x»»): un proxy, un alias o un modelo recién bajado no figuran siempre, y un
// servidor sin /models tampoco da lista.

// Cuántas filas se pintan a la vez. Más allá, se pide acotar escribiendo: pintar
// 400 filas con insignias en cada pulsación no aporta nada a quien ya busca.
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

export function ModelCombobox({
    models,
    value,
    onChange,
    loading,
    error,
    onReload,
    disabled,
    className,
    align = 'right',
}: {
    models: main.ModelInfo[]
    value: string
    onChange: (id: string) => void
    loading: boolean
    error: string
    /** Sin esto no hay botón de refrescar (la lista no viene de una consulta que se pueda repetir). */
    onReload?: () => void
    disabled?: boolean
    className?: string
    /** Hacia qué lado se abre la lista respecto del botón. */
    align?: 'left' | 'right'
}) {
    const t = useT()
    const [open, setOpen] = useState(false)
    const [q, setQ] = useState('')
    const [active, setActive] = useState(0)
    const root = useRef<HTMLDivElement>(null)
    const list = useRef<HTMLDivElement>(null)
    const listId = useId()

    useEffect(() => {
        if (!open) return
        const away = (e: PointerEvent) => !root.current?.contains(e.target as Node) && setOpen(false)
        document.addEventListener('pointerdown', away)
        return () => document.removeEventListener('pointerdown', away)
    }, [open])

    const query = q.trim().toLowerCase()
    const filtered = useMemo(() => (query ? models.filter((m) => m.id.toLowerCase().includes(query)) : models), [models, query])
    const shown = filtered.slice(0, MAX_ROWS)
    const typed = q.trim()
    // «Usar lo escrito» solo si no es ya un modelo de la lista.
    const canUseTyped = typed !== '' && !models.some((m) => m.id === typed)
    const rows = canUseTyped ? shown.length + 1 : shown.length
    const current = models.find((m) => m.id === value)

    useEffect(() => setActive(0), [query])
    useEffect(() => {
        list.current?.querySelector<HTMLElement>(`[data-row="${active}"]`)?.scrollIntoView({block: 'nearest'})
    }, [active])

    const pick = (id: string) => {
        onChange(id)
        setOpen(false)
        setQ('')
    }
    const onKey = (e: React.KeyboardEvent) => {
        if (e.key === 'Escape') {
            e.stopPropagation()
            setOpen(false)
        } else if (e.key === 'ArrowDown') {
            e.preventDefault()
            setActive((a) => Math.min(a + 1, rows - 1))
        } else if (e.key === 'ArrowUp') {
            e.preventDefault()
            setActive((a) => Math.max(a - 1, 0))
        } else if (e.key === 'Enter') {
            e.preventDefault()
            if (active < shown.length) pick(shown[active].id)
            else if (canUseTyped) pick(typed)
        }
    }

    return (
        <div ref={root} className={cn('relative flex min-w-0 items-center gap-1', className)}>
            <button
                type="button"
                title={error ? fmt(t.chat.modelsError, {msg: error}) : t.chat.modelButtonHint}
                data-testid="model-picker"
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-controls={open ? listId : undefined}
                disabled={disabled}
                onClick={() => setOpen((o) => !o)}
                className={cn(
                    'no-drag flex h-7 min-w-0 flex-1 items-center gap-1.5 rounded-md border border-line bg-surface-2 px-2 text-ui-12 text-fg transition-colors hover:border-line-strong disabled:opacity-40',
                    error && 'border-warn',
                )}
            >
                <span className={cn('min-w-0 flex-1 truncate text-left font-mono text-ui-11-5', !value && 'text-faint')}>{value || t.chat.pickModel}</span>
                {current ? <CapBadges m={current} /> : null}
                <ChevronDown className="icon-ui-12 shrink-0 text-faint" />
            </button>
            {onReload ? (
                <IconButton title={error ? fmt(t.chat.modelsError, {msg: error}) : t.chat.reloadModels} onClick={onReload} disabled={disabled}>
                    <RefreshCw className={cn('icon-ui-14', loading && 'animate-spin', error && 'text-warn')} />
                </IconButton>
            ) : null}

            {open ? (
                <div
                    className={cn(
                        'absolute top-8 z-30 flex w-[420px] max-w-[88vw] flex-col overflow-clip rounded-lg border border-line-strong bg-raised shadow-xl',
                        align === 'right' ? 'right-0' : 'left-0',
                    )}
                    onKeyDown={onKey}
                >
                    <div className="flex items-center gap-1.5 border-b border-line px-2 py-1.5">
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
                    </div>
                    <div ref={list} id={listId} role="listbox" className="max-h-64 overflow-y-auto py-1">
                        {shown.length === 0 && !canUseTyped ? <p className="px-3 py-2 text-ui-12 text-faint">{fmt(t.chat.noModelMatch, {q: typed})}</p> : null}
                        {shown.map((m, i) => {
                            const meta = [contextLabel(m.contextTokens) && fmt(t.chat.contextTokens, {n: contextLabel(m.contextTokens)}), priceLabel(m.promptPricePerM, m.completionPricePerM)]
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
                                    title={m.id}
                                    onClick={() => pick(m.id)}
                                    onPointerMove={() => setActive(i)}
                                    className={cn('flex w-full items-center gap-2 px-3 py-1 text-left transition-colors', i === active && 'bg-surface-2')}
                                >
                                    <span className="flex icon-ui-12 shrink-0 items-center justify-center text-accent">{m.id === value ? <Check className="icon-ui-12" /> : null}</span>
                                    <span className="min-w-0 flex-1">
                                        <span className="block truncate font-mono text-ui-11-5 text-fg">{m.id}</span>
                                        {meta ? <span className="block truncate font-mono text-ui-10 text-faint">{meta}</span> : null}
                                    </span>
                                    <CapBadges m={m} />
                                </button>
                            )
                        })}
                        {filtered.length > shown.length ? <p className="px-3 py-1.5 text-ui-11 text-faint">{fmt(t.chat.moreModels, {n: filtered.length - shown.length})}</p> : null}
                        {canUseTyped ? (
                            <button
                                type="button"
                                role="option"
                                aria-selected={false}
                                id={`${listId}-${shown.length}`}
                                data-row={shown.length}
                                title={fmt(t.chat.useTypedHint, {id: typed})}
                                onClick={() => pick(typed)}
                                onPointerMove={() => setActive(shown.length)}
                                className={cn('flex w-full items-center gap-2 border-t border-line px-3 py-1.5 text-left text-ui-12 text-accent', active === shown.length && 'bg-surface-2')}
                            >
                                {fmt(t.chat.useTyped, {id: typed})}
                            </button>
                        ) : null}
                    </div>
                    <p className="border-t border-line px-3 py-1.5 text-ui-10-5 leading-snug text-faint">{t.chat.modelLegend}</p>
                </div>
            ) : null}
        </div>
    )
}
