import {CircleCheck, TriangleAlert, X} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {grams, meters, money, num} from '../../lib/printFormat'
import {materialsStore} from '../../lib/materials'
import {printStore, selectPart, setPartMaterial} from '../../lib/printSession'
import {Select} from '../ui/Field'
import {Row} from './Dimensions'

// PartCard es la tarjeta de la parte seleccionada en el visor (S7): su color,
// medidas y la estimación de peso, filamento y costo con el material que se
// elija para ESA parte (cambiarlo recalcula en vivo). Los números de impresión
// son estimaciones (backend/estimate) y se rotulan con «≈».
export function PartCard({index, color}: {index: number; color: string}) {
    const t = useT()
    const model = printStore.use((s) => s.model)
    const estimate = printStore.use((s) => s.estimate)
    const status = printStore.use((s) => s.estimateStatus)
    const params = printStore.use((s) => s.params)
    const chosen = printStore.use((s) => s.materials[index])
    const materials = materialsStore.use((s) => s.list)
    const part = model?.parts.find((p) => p.index === index)
    if (!model || !part) return null
    const est = estimate?.parts.find((p) => p.index === index)
    const currency = estimate?.currency ?? params?.currency ?? 'USD'
    const materialId = chosen ?? est?.materialId ?? params?.defaultMaterialId ?? ''
    const info = part.info
    const busy = status === 'loading'
    const value = (v: string | null) => (v === null ? '—' : `≈ ${v}`)

    return (
        <div
            className="pointer-events-auto flex w-[248px] max-w-full flex-col gap-1 rounded-lg border border-accent/60 bg-surface/95 px-3 py-2 backdrop-blur-sm"
            role="group"
            aria-label={fmt(t.partCard.label, {name: part.name})}
            onPointerDown={(e) => e.stopPropagation()}
        >
            <div className="flex items-center gap-2">
                <span
                    className="size-3.5 shrink-0 rounded-sm border border-line-strong"
                    style={color ? {backgroundColor: color} : undefined}
                    title={color || t.partCard.noColor}
                    aria-hidden
                />
                <span className="min-w-0 flex-1 truncate font-mono text-ui-11 text-fg" title={part.name}>
                    {part.name}
                </span>
                <button
                    type="button"
                    title={t.partCard.close}
                    aria-label={t.partCard.close}
                    onClick={() => selectPart(null)}
                    className="-mr-1 inline-flex icon-ui-20 shrink-0 items-center justify-center rounded text-faint transition-colors duration-100 hover:bg-surface-2 hover:text-fg"
                >
                    <X className="icon-ui-12" />
                </button>
            </div>
            <Row label={t.viewer.size} hint={t.viewer.sizeHint}>
                {fmt(t.viewer.mm, {v: info.size.map((v) => num(v, 1)).join(' × ')})}
            </Row>
            <Row label={t.viewer.volume} hint={t.viewer.volumeHint}>
                {fmt(t.viewer.cm3, {v: num(info.volumeMm3 / 1000, 2)})}
            </Row>
            <Row label={t.partCard.weight} hint={t.partCard.estimateHint}>
                {value(est ? fmt(t.print.g, {v: grams(est.massG)}) : null)}
            </Row>
            <Row label={t.partCard.filament} hint={t.partCard.filamentHint}>
                {value(est ? fmt(t.print.m, {v: meters(est.lengthM)}) : null)}
            </Row>
            <Row label={t.partCard.cost} hint={t.partCard.estimateHint}>
                {value(est ? money(est.cost, currency) : null)}
            </Row>
            <div className="flex items-center justify-between gap-3" title={t.partCard.materialHint}>
                <span className="font-mono text-ui-10 tracking-[0.1em] text-faint uppercase">{t.partCard.material}</span>
                <Select
                    className="h-6 w-[120px] px-1.5 pr-5 text-ui-11"
                    title={t.partCard.materialHint}
                    aria-label={t.partCard.materialHint}
                    value={materialId}
                    onChange={(e) => setPartMaterial(index, e.target.value)}
                >
                    {materials.map((m) => (
                        <option key={m.id} value={m.id}>
                            {m.name}
                        </option>
                    ))}
                </Select>
            </div>
            <div className="flex items-center justify-between gap-3">
                <Row label={t.viewer.triangles} hint={t.viewer.trianglesHint}>
                    {num(info.triangles, 0)}
                </Row>
                {info.watertight || model.total.watertight ? (
                    <span className="flex shrink-0 items-center gap-1 text-ui-11 text-ok" title={info.watertight ? t.viewer.closedHint : t.viewer.colorBoundaryHint}>
                        <CircleCheck className="icon-ui-12" />
                        {info.watertight ? t.viewer.closed : t.viewer.colorBoundary}
                    </span>
                ) : (
                    <span className="flex shrink-0 items-center gap-1 text-ui-11 text-warn" title={t.viewer.openHint}>
                        <TriangleAlert className="icon-ui-12" />
                        {t.viewer.open}
                    </span>
                )}
            </div>
            <p className="text-ui-10-5 text-faint" title={t.print.note}>
                {busy ? t.print.calculating : t.partCard.estimate}
            </p>
        </div>
    )
}
