import {useEffect, useState} from 'react'
import {CircleAlert, FileBox, Loader2, RotateCcw} from 'lucide-react'
import type {render} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {CURRENCIES, grams, meters, money} from '../../lib/printFormat'
import {ensureMaterials, materialsStore} from '../../lib/materials'
import {partColor} from '../../lib/printTypes'
import {printStore, resetOverrides, retryModel, selectPart, setOverrides, setPartMaterial, type PrintParams} from '../../lib/printSession'
import {Button} from '../ui/Button'
import {Select} from '../ui/Field'

// Rangos que acepta Go (backend/store/settings.go): un valor fuera de ellos no se
// aplica, el campo sigue mostrando lo que se escribe hasta que se sale de él.
const RANGES = {
    infillPercent: {min: 0, max: 100, step: 1},
    wallLoops: {min: 1, max: 10, step: 1},
    lineWidthMm: {min: 0.2, max: 1.5, step: 0.01},
    filamentDiameterMm: {min: 1, max: 3.5, step: 0.05},
} as const

type NumKey = keyof typeof RANGES

// NumField es un número con borrador propio: escribir «0,» o borrar el campo no
// tiene que recalcular (ni saltar a otro valor) a mitad de tecla.
function NumField({label, hint, name, value, onCommit}: {label: string; hint: string; name: NumKey; value: number; onCommit: (v: number) => void}) {
    const r = RANGES[name]
    const [draft, setDraft] = useState<string | null>(null)
    const shown = draft ?? String(value)
    return (
        <label className="flex min-w-0 flex-col gap-1" title={hint}>
            <span className="truncate font-mono text-ui-10 tracking-wider text-muted uppercase">{label}</span>
            <input
                type="number"
                inputMode="decimal"
                min={r.min}
                max={r.max}
                step={r.step}
                value={shown}
                title={hint}
                onChange={(e) => {
                    setDraft(e.target.value)
                    const v = e.target.valueAsNumber
                    if (Number.isFinite(v) && v >= r.min && v <= r.max) onCommit(v)
                }}
                onBlur={() => setDraft(null)}
                className="selectable h-7 w-full rounded-md border border-line bg-surface-2 px-2 text-ui-12 text-fg tabular-nums outline-none transition-colors focus:border-accent"
            />
        </label>
    )
}

function Stat({children, title, className}: {children: React.ReactNode; title?: string; className?: string}) {
    return (
        <td className={cn('px-2 py-1 text-right font-mono text-ui-11-5 whitespace-nowrap tabular-nums', className)} title={title}>
            {children}
        </td>
    )
}

// PrintPanel es el panel «Impresión» (S7): peso, filamento y costo estimados de
// cada parte del modelo y del total, con los parámetros del cálculo a la vista.
// El cálculo es de Go (backend/estimate); acá solo se piden los números y se
// muestran. Los parámetros se cambian SOLO para este cálculo: los de siempre se
// fijan en Configuración → Impresión.
export function PrintPanel({
    file,
    result,
    running,
    base,
    exportWhy,
    onExport3mf,
    onOpenSettings,
}: {
    file: string | null
    result: render.Result | undefined
    running: boolean
    base: PrintParams
    exportWhy: string
    onExport3mf: () => void
    onOpenSettings: () => void
}) {
    const t = useT()
    const model = printStore.use((s) => s.model)
    const modelStatus = printStore.use((s) => s.modelStatus)
    const modelError = printStore.use((s) => s.modelError)
    const estimate = printStore.use((s) => s.estimate)
    const estimateStatus = printStore.use((s) => s.estimateStatus)
    const estimateError = printStore.use((s) => s.estimateError)
    const selected = printStore.use((s) => s.selected)
    const overrides = printStore.use((s) => s.overrides)
    const chosen = printStore.use((s) => s.materials)
    const params = printStore.use((s) => s.params) ?? base
    const materials = materialsStore.use((s) => s.list)
    useEffect(() => {
        void ensureMaterials()
    }, [])

    const hasMesh = result?.status === 'ok' && !!result.meshPath
    const currency = estimate?.currency ?? params.currency
    const overridden = Object.keys(overrides).length > 0

    const exportBtn = (
        <Button
            variant="primary"
            title={exportWhy || t.print.exportHint}
            aria-disabled={!!exportWhy}
            className={exportWhy ? 'opacity-50' : undefined}
            onClick={() => {
                if (!exportWhy) onExport3mf()
            }}
        >
            <FileBox className="icon-ui-12" />
            {t.print.export3mf}
        </Button>
    )

    let body: React.ReactNode
    if (!file) body = <Empty text={t.print.noFile} />
    else if (!hasMesh || !model) {
        if (running || modelStatus === 'loading') {
            body = (
                <Empty
                    icon={<Loader2 className="icon-ui-16 animate-spin text-accent" />}
                    text={running ? t.print.rendering : t.print.loadingParts}
                />
            )
        } else if (modelStatus === 'error' && hasMesh) {
            body = (
                <div className="m-4 flex max-w-[560px] flex-col items-start gap-2 rounded-lg border border-danger/50 bg-surface p-3">
                    <p className="flex items-start gap-2 text-ui-12 text-fg">
                        <CircleAlert className="mt-px icon-ui-14 shrink-0 text-danger" />
                        <span className="selectable">{fmt(t.print.loadError, {msg: modelError})}</span>
                    </p>
                    <Button title={t.print.retryHint} onClick={retryModel}>
                        <RotateCcw className="icon-ui-12" />
                        {t.print.retry}
                    </Button>
                </div>
            )
        } else body = <Empty text={t.print.notRendered} />
    } else {
        const total = estimate?.total
        const table = (
            <div className="min-w-0 overflow-x-auto rounded-lg border border-line">
                <table className="w-full min-w-[460px] border-collapse text-ui-12">
                    <thead>
                        <tr className="border-b border-line font-mono text-ui-10 tracking-wider text-faint uppercase">
                            <th className="px-2 py-1.5 text-left font-normal">{t.print.colPart}</th>
                            <th className="px-2 py-1.5 text-left font-normal">{t.print.colMaterial}</th>
                            <th className="px-2 py-1.5 text-right font-normal" title={t.print.weightHint}>
                                {t.print.colWeight}
                            </th>
                            <th className="px-2 py-1.5 text-right font-normal" title={t.print.lengthHint}>
                                {t.print.colLength}
                            </th>
                            <th className="px-2 py-1.5 text-right font-normal" title={t.print.costHint}>
                                {t.print.colCost}
                            </th>
                        </tr>
                    </thead>
                    <tbody>
                        {model.parts.map((p) => {
                            const est = estimate?.parts.find((x) => x.index === p.index)
                            const isSel = selected === p.index
                            const mat = chosen[p.index] ?? est?.materialId ?? params.defaultMaterialId
                            return (
                                <tr
                                    key={p.index}
                                    aria-selected={isSel}
                                    title={t.print.rowHint}
                                    onClick={() => selectPart(isSel ? null : p.index)}
                                    className={cn(
                                        'cursor-pointer border-b border-line transition-colors duration-100',
                                        isSel ? 'bg-accent-soft' : 'hover:bg-surface-2',
                                    )}
                                >
                                    <td className="px-2 py-1">
                                        <span className="flex min-w-0 items-center gap-2">
                                            <span
                                                className="size-3.5 shrink-0 rounded-sm border border-line-strong"
                                                style={partColor(p) ? {backgroundColor: partColor(p)} : undefined}
                                                aria-hidden
                                            />
                                            <span className="truncate" title={p.name}>
                                                {p.name}
                                            </span>
                                        </span>
                                    </td>
                                    <td className="px-2 py-1" onClick={(e) => e.stopPropagation()}>
                                        <Select
                                            className="h-6 w-[104px] px-1.5 pr-5 text-ui-11-5"
                                            title={t.partCard.materialHint}
                                            aria-label={fmt(t.print.materialOf, {name: p.name})}
                                            value={mat}
                                            onChange={(e) => setPartMaterial(p.index, e.target.value)}
                                        >
                                            {materials.map((m) => (
                                                <option key={m.id} value={m.id}>
                                                    {m.name}
                                                </option>
                                            ))}
                                        </Select>
                                    </td>
                                    <Stat>{est ? fmt(t.print.g, {v: grams(est.massG)}) : '—'}</Stat>
                                    <Stat>{est ? fmt(t.print.m, {v: meters(est.lengthM)}) : '—'}</Stat>
                                    <Stat>{est ? money(est.cost, currency) : '—'}</Stat>
                                </tr>
                            )
                        })}
                        <tr className="font-medium">
                            <td className="px-2 py-1.5 font-mono text-ui-10-5 tracking-wider text-muted uppercase" colSpan={2}>
                                {t.print.total}
                            </td>
                            <Stat className="text-fg">{total ? fmt(t.print.g, {v: grams(total.massG)}) : '—'}</Stat>
                            <Stat className="text-fg">{total ? fmt(t.print.m, {v: meters(total.lengthM)}) : '—'}</Stat>
                            <Stat className="text-fg">{total ? money(total.cost, currency) : '—'}</Stat>
                        </tr>
                    </tbody>
                </table>
            </div>
        )
        const controls = (
            <section className="flex min-w-0 flex-col gap-2.5" aria-label={t.print.paramsTitle}>
                <div className="flex items-center justify-between gap-2">
                    <h3 className="font-mono text-ui-10-5 tracking-[0.12em] text-muted uppercase" title={t.print.paramsHint}>
                        {t.print.paramsTitle}
                    </h3>
                    {overridden ? (
                        <button
                            type="button"
                            title={t.print.resetHint}
                            onClick={resetOverrides}
                            className="inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-ui-11 text-accent transition-colors duration-100 hover:bg-surface-2"
                        >
                            <RotateCcw className="icon-ui-12" />
                            {t.print.reset}
                        </button>
                    ) : null}
                </div>
                <div className="grid grid-cols-3 gap-2.5 @3xl:grid-cols-6 @5xl:grid-cols-3">
                    <NumField label={t.print.infill} hint={t.print.infillHint} name="infillPercent" value={params.infillPercent} onCommit={(v) => setOverrides({infillPercent: v})} />
                    <NumField label={t.print.walls} hint={t.print.wallsHint} name="wallLoops" value={params.wallLoops} onCommit={(v) => setOverrides({wallLoops: Math.round(v)})} />
                    <NumField label={t.print.lineWidth} hint={t.print.lineWidthHint} name="lineWidthMm" value={params.lineWidthMm} onCommit={(v) => setOverrides({lineWidthMm: v})} />
                    <NumField
                        label={t.print.diameter}
                        hint={t.print.diameterHint}
                        name="filamentDiameterMm"
                        value={params.filamentDiameterMm}
                        onCommit={(v) => setOverrides({filamentDiameterMm: v})}
                    />
                    <label className="flex min-w-0 flex-col gap-1" title={t.print.defaultMaterialHint}>
                        <span className="truncate font-mono text-ui-10 tracking-wider text-muted uppercase">{t.print.defaultMaterial}</span>
                        <Select
                            className="h-7 text-ui-12"
                            title={t.print.defaultMaterialHint}
                            value={params.defaultMaterialId}
                            onChange={(e) => setOverrides({defaultMaterialId: e.target.value})}
                        >
                            {materials.map((m) => (
                                <option key={m.id} value={m.id}>
                                    {m.name}
                                </option>
                            ))}
                        </Select>
                    </label>
                    <label className="flex min-w-0 flex-col gap-1" title={t.print.currencyHint}>
                        <span className="truncate font-mono text-ui-10 tracking-wider text-muted uppercase">{t.print.currency}</span>
                        <Select className="h-7 text-ui-12" title={t.print.currencyHint} value={params.currency} onChange={(e) => setOverrides({currency: e.target.value})}>
                            {(CURRENCIES.includes(params.currency) ? CURRENCIES : [params.currency, ...CURRENCIES]).map((c) => (
                                <option key={c} value={c}>
                                    {c}
                                </option>
                            ))}
                        </Select>
                    </label>
                </div>
                <p className="text-ui-11 leading-snug text-faint">
                    {t.print.paramsFoot}{' '}
                    <button
                        type="button"
                        title={t.print.openSettingsHint}
                        onClick={onOpenSettings}
                        className="text-muted underline decoration-dotted underline-offset-2 transition-colors duration-100 hover:text-fg"
                    >
                        {t.print.openSettings}
                    </button>
                </p>
            </section>
        )
        body = (
            <div className="flex flex-col gap-3 p-3">
                {estimateStatus === 'error' ? (
                    <p className="flex items-start gap-2 rounded-lg border border-danger/50 bg-surface px-3 py-2 text-ui-12 text-fg">
                        <CircleAlert className="mt-px icon-ui-14 shrink-0 text-danger" />
                        <span className="selectable">{fmt(t.print.estimateError, {msg: estimateError})}</span>
                    </p>
                ) : null}
                <div className="grid grid-cols-1 gap-3 @5xl:grid-cols-[minmax(0,1.5fr)_minmax(0,1fr)] @5xl:items-start">
                    {table}
                    {controls}
                </div>
                <div className="rounded-lg border border-line bg-surface-2 px-3 py-2">
                    <p className="text-ui-11-5 leading-relaxed text-muted">
                        <strong className="font-medium text-fg">{t.print.estimateTitle}.</strong> {t.print.note}
                    </p>
                    {estimate && estimate.assumptions.length > 0 ? (
                        <ul className="mt-1 list-disc pl-4 text-ui-11 leading-snug text-faint">
                            {estimate.assumptions.map((a, i) => (
                                <li key={i} className="selectable">
                                    {a}
                                </li>
                            ))}
                        </ul>
                    ) : null}
                </div>
            </div>
        )
    }

    const status =
        hasMesh && model
            ? estimateStatus === 'loading'
                ? t.print.calculating
                : model.parts.length === 1
                  ? t.print.onePart
                  : fmt(t.print.nParts, {n: model.parts.length})
            : ''
    return (
        <div className="flex h-full min-h-0 flex-col">
            <div className="flex h-9 shrink-0 items-center gap-3 overflow-clip border-b border-line bg-surface px-3">
                <span className="min-w-0 truncate font-mono text-ui-11 text-muted" title={file ?? undefined}>
                    {file ?? t.print.noFileShort}
                </span>
                {status ? (
                    <span className="flex shrink-0 items-center gap-1.5 font-mono text-ui-10-5 whitespace-nowrap text-faint">
                        {estimateStatus === 'loading' ? <Loader2 className="icon-ui-12 animate-spin text-accent" /> : null}
                        {status}
                    </span>
                ) : null}
                <div className="ml-auto shrink-0">{exportBtn}</div>
            </div>
            <div className="@container min-h-0 flex-1 overflow-auto">{body}</div>
        </div>
    )
}

function Empty({text, icon}: {text: string; icon?: React.ReactNode}) {
    return (
        <div className="bed-grid flex h-full min-h-[80px] flex-col items-center justify-center gap-2 px-6 py-6">
            {icon}
            <p className="max-w-[420px] text-center text-ui-12 leading-relaxed text-muted">{text}</p>
        </div>
    )
}

