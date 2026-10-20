import {useEffect, useId, useMemo, useRef, useState} from 'react'
import {Check, ChevronDown, Printer, Search, TriangleAlert} from 'lucide-react'
import type {printers} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {colorsLine, groupByBrand, volumeLine} from '../../lib/printerInfo'
import {Input} from '../ui/Field'
import {Popover, listKeys} from '../chat/Popover'

/**
 * Selector de impresora (S14): un botón con la impresora elegida que abre una lista con búsqueda,
 * agrupada por marca en el orden de Go (`ListPrinterBrands` + `ListPrinters`). Cada fila dice el
 * modelo, el volumen X×Y×Z, los colores máximos con su sistema y, discreto, si algún dato de la
 * ficha no se pudo confirmar. Íconos genéricos (lucide `Printer`): ningún logo de marca.
 *
 * Teclado: flechas, Inicio/Fin, Enter para elegir y Esc para cerrar (el foco vuelve al botón).
 * La lista se cuelga del <dialog> cuando el selector está dentro de uno (ver Popover).
 */

// Cuántas filas se pintan a la vez: más allá, se pide acotar escribiendo.
const MAX_ROWS = 120

type Row = {kind: 'brand'; id: string; name: string; count: number} | {kind: 'printer'; p: printers.Printer}

export function PrinterPicker({
    value,
    onChange,
    printerList,
    brands,
    disabled,
    className,
    testId = 'printer-picker',
    initialOpen,
    initialQuery,
}: {
    value: string
    onChange: (id: string) => void
    printerList: printers.Printer[]
    brands: printers.Brand[]
    disabled?: boolean
    className?: string
    testId?: string
    /** Solo el banco de capturas: la lista abierta, con esta búsqueda ya escrita. */
    initialOpen?: boolean
    initialQuery?: string
}) {
    const t = useT()
    const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
    const [open, setOpen] = useState(!!initialOpen)
    const [q, setQ] = useState(initialQuery ?? '')
    const [active, setActive] = useState(0)
    const list = useRef<HTMLDivElement>(null)
    const search = useRef<HTMLInputElement>(null)
    const listId = useId()

    const current = printerList.find((p) => p.id === value)
    const query = q.trim().toLowerCase()

    // Las filas: un rótulo por marca y sus impresoras (las que coinciden con la búsqueda).
    const {rows, options} = useMemo(() => {
        const out: Row[] = []
        const opts: printers.Printer[] = []
        for (const g of groupByBrand(printerList, brands)) {
            const items = query
                ? g.items.filter((p) => `${p.name} ${p.model} ${p.brand_name} ${p.multicolor_system} ${p.id}`.toLowerCase().includes(query))
                : g.items
            if (items.length === 0 || opts.length >= MAX_ROWS) continue
            out.push({kind: 'brand', id: g.id, name: g.name, count: items.length})
            for (const p of items) {
                if (opts.length >= MAX_ROWS) break
                out.push({kind: 'printer', p})
                opts.push(p)
            }
        }
        return {rows: out, options: opts}
    }, [printerList, brands, query])

    // La fila activa se cuenta sobre las impresoras (los rótulos no se recorren con el teclado).
    useEffect(() => {
        const i = options.findIndex((p) => p.id === value)
        setActive(open ? Math.max(0, i) : 0)
        // Solo al abrir o al cambiar la búsqueda: mover la fila activa con el ratón no debe volver a este efecto.
    }, [query, open, options.length, value])
    useEffect(() => {
        list.current?.querySelector<HTMLElement>(`[data-row="${active}"]`)?.scrollIntoView({block: 'nearest'})
    }, [active])

    // El foco va al buscador cuando la lista ya es visible: el Popover nace oculto hasta medirse y un autoFocus de React corre
    // antes (y sobre un elemento oculto no enfoca), así que dentro de un <dialog> el teclado seguía en el campo de antes.
    useEffect(() => {
        if (!open || !anchor) return
        // Un cuadro después: los efectos pasivos corren antes de que el Popover se muestre.
        const raf = requestAnimationFrame(() => search.current?.focus({preventScroll: true}))
        return () => cancelAnimationFrame(raf)
    }, [open, anchor])

    const close = () => {
        setOpen(false)
        setQ('')
    }
    const pick = (p: printers.Printer | undefined) => {
        if (!p) return
        onChange(p.id)
        close()
    }

    let n = -1
    return (
        <>
            <button
                ref={setAnchor}
                type="button"
                title={current ? fmt(t.printers.pickerHint, {name: current.name}) : t.printers.pickerEmptyHint}
                data-testid={testId}
                aria-haspopup="listbox"
                aria-expanded={open}
                aria-controls={open ? listId : undefined}
                disabled={disabled}
                onClick={() => setOpen((o) => !o)}
                className={cn(
                    'no-drag flex h-8 w-full min-w-0 items-center gap-2 rounded-md border border-line bg-surface-2 px-2.5 text-left text-ui-12-5 text-fg transition-colors hover:border-line-strong focus-visible:border-accent disabled:opacity-40',
                    open && 'border-accent',
                    className,
                )}
            >
                <Printer className="icon-ui-14 shrink-0 text-faint" />
                <span className="min-w-0 flex-1 truncate">{current ? current.name : value || t.printers.pickerEmpty}</span>
                {current ? <span className="shrink-0 font-mono text-ui-10-5 text-faint">{volumeLine(t, current)}</span> : null}
                <ChevronDown className="icon-ui-12 shrink-0 text-faint" />
            </button>

            {open && anchor ? (
                <Popover
                    anchor={anchor}
                    onClose={close}
                    width={Math.max(anchor.getBoundingClientRect().width, 460)}
                    label={t.printers.pickerTitle}
                    focusFirst={false}
                    onKeyDown={(e) => listKeys(e, {rows: options.length, active, setActive, onEnter: () => pick(options[active])})}
                >
                    <div className="flex shrink-0 items-center gap-1.5 border-b border-line px-2 py-1.5">
                        <Search className="icon-ui-14 shrink-0 text-faint" />
                        <Input
                            ref={search}
                            value={q}
                            placeholder={t.printers.search}
                            title={t.printers.searchHint}
                            role="combobox"
                            aria-expanded
                            aria-controls={listId}
                            aria-activedescendant={options.length ? `${listId}-${active}` : undefined}
                            onChange={(e) => setQ(e.target.value)}
                            className="h-7 border-0 bg-transparent px-0 focus:border-0"
                        />
                        <span className="shrink-0 font-mono text-ui-10 text-faint">{fmt(t.printers.count, {n: options.length})}</span>
                    </div>
                    <div ref={list} id={listId} role="listbox" aria-label={t.printers.pickerTitle} className="max-h-[440px] min-h-0 overflow-y-auto pb-1">
                        {rows.length === 0 ? <p className="px-3 py-2 text-ui-12 text-faint">{fmt(t.printers.noMatch, {q: q.trim()})}</p> : null}
                        {rows.map((r) => {
                            if (r.kind === 'brand') {
                                return (
                                    <div
                                        key={`b-${r.id}`}
                                        role="presentation"
                                        className="sticky top-0 z-10 flex items-center justify-between border-b border-line bg-raised px-3 py-1 font-mono text-ui-10 tracking-wider text-faint uppercase"
                                    >
                                        <span className="truncate">{r.name}</span>
                                        <span>{r.count}</span>
                                    </div>
                                )
                            }
                            n++
                            const i = n
                            const p = r.p
                            const sel = p.id === value
                            return (
                                <button
                                    key={p.id}
                                    id={`${listId}-${i}`}
                                    data-row={i}
                                    type="button"
                                    role="option"
                                    aria-selected={sel}
                                    title={printerRowTitle(t, p)}
                                    onClick={() => pick(p)}
                                    onPointerMove={() => setActive(i)}
                                    className={cn('flex w-full items-center gap-2 px-3 py-1.5 text-left transition-colors', i === active && 'bg-surface-2')}
                                >
                                    <Printer className={cn('icon-ui-14 shrink-0', sel ? 'text-accent' : 'text-faint')} />
                                    <span className="min-w-0 flex-1">
                                        <span className="flex items-center gap-1.5">
                                            <span className="truncate text-ui-12-5 text-fg">{p.name}</span>
                                            {p.verify?.length ? (
                                                <TriangleAlert className="icon-ui-12 shrink-0 text-warn" aria-label={t.printers.verifyBadge} />
                                            ) : null}
                                        </span>
                                        <span className="block truncate font-mono text-ui-10-5 text-faint">
                                            {volumeLine(t, p)} · {colorsLine(t, p)}
                                        </span>
                                    </span>
                                    {sel ? <Check className="icon-ui-14 shrink-0 text-accent" /> : null}
                                </button>
                            )
                        })}
                    </div>
                </Popover>
            ) : null}
        </>
    )
}

// El title de una fila: la ficha en una línea y, si algún dato no se pudo confirmar, qué falta.
function printerRowTitle(t: ReturnType<typeof useT>, p: printers.Printer): string {
    const base = `${p.name} — ${volumeLine(t, p)} · ${colorsLine(t, p)}`
    return p.verify?.length ? `${base}\n${fmt(t.printers.verifyHint, {items: p.verify.join('; ')})}` : base
}
