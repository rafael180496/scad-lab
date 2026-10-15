import {useEffect, useState} from 'react'
import {LayoutPanelLeft, Minus, Plus, RotateCcw} from 'lucide-react'
import {Button, IconButton} from '../ui/Button'
import {Field, Input, Select} from '../ui/Field'
import {fmt, useT} from '../../i18n'
import {FONT_MAX, FONT_MIN, UI_SCALES, type Appearance} from '../../lib/appearance'
import {modLabel} from '../../lib/platform'

// FontStepper: un tamaño de letra en px con − / + y un campo numérico. El campo
// guarda su propio texto mientras se escribe («1» camino a «16») y solo aplica
// los valores completos dentro de 9–28; al salir vuelve al valor vigente.
function FontStepper({label, value, onChange}: {label: string; value: number; onChange: (px: number) => void}) {
    const t = useT()
    const [draft, setDraft] = useState(String(value))
    useEffect(() => setDraft(String(value)), [value])
    const step = (d: number) => onChange(Math.max(FONT_MIN, Math.min(FONT_MAX, value + d)))
    return (
        <Field label={label} hint={fmt(t.appearance.range, {min: FONT_MIN, max: FONT_MAX})}>
            <div className="flex items-center gap-1">
                <IconButton
                    title={fmt(t.appearance.decrease, {area: label})}
                    disabled={value <= FONT_MIN}
                    onClick={() => step(-1)}
                    className="border border-line"
                >
                    <Minus className="icon-ui-14" />
                </IconButton>
                <Input
                    type="number"
                    inputMode="numeric"
                    min={FONT_MIN}
                    max={FONT_MAX}
                    step={1}
                    className="w-16 text-center"
                    title={fmt(t.appearance.fontInput, {area: label})}
                    value={draft}
                    onChange={(e) => {
                        setDraft(e.target.value)
                        const n = Number(e.target.value)
                        if (Number.isInteger(n) && n >= FONT_MIN && n <= FONT_MAX) onChange(n)
                    }}
                    onBlur={() => setDraft(String(value))}
                />
                <IconButton
                    title={fmt(t.appearance.increase, {area: label})}
                    disabled={value >= FONT_MAX}
                    onClick={() => step(1)}
                    className="border border-line"
                >
                    <Plus className="icon-ui-14" />
                </IconButton>
                <span className="font-mono text-ui-11-5 text-faint">px</span>
            </div>
        </Field>
    )
}

// Configuración → General → Apariencia y diseño. Todo se aplica y se guarda al
// instante (useAppearance / useLayout): no pasa por el botón «Guardar».
export function AppearanceSettings({
    appearance,
    onChange,
    onResetAppearance,
    onResetLayout,
}: {
    appearance: Appearance
    onChange: (patch: Partial<Appearance>) => void
    onResetAppearance: () => void
    onResetLayout: () => void
}) {
    const t = useT()
    const a = t.appearance
    return (
        <div className="mt-2 flex flex-col gap-4 border-t border-line pt-4" data-testid="appearance">
            <span className="font-mono text-ui-10-5 tracking-[0.12em] text-faint uppercase">{a.title}</span>
            <p className="text-ui-11-5 leading-relaxed text-faint">{a.intro}</p>

            <Field
                label={a.uiScale}
                hint={fmt(a.uiScaleHint, {inc: `${modLabel()}+`, dec: `${modLabel()}−`, reset: `${modLabel()}0`})}
            >
                <Select value={String(appearance.uiScale)} title={a.uiScaleTitle} onChange={(e) => onChange({uiScale: Number(e.target.value)})}>
                    {UI_SCALES.map((sc) => (
                        <option key={sc.value} value={sc.value}>
                            {a.scales[sc.key]} · {sc.value} %
                        </option>
                    ))}
                </Select>
            </Field>

            <div className="flex flex-col gap-1">
                <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{a.fontsTitle}</span>
                <p className="text-ui-11-5 text-faint">{fmt(a.fontsHint, {inc: `${modLabel()}+`, dec: `${modLabel()}−`, reset: `${modLabel()}0`})}</p>
            </div>
            <div className="grid grid-cols-3 gap-4 max-[900px]:grid-cols-1">
                <FontStepper label={a.editorFont} value={appearance.editorFontSize} onChange={(px) => onChange({editorFontSize: px})} />
                <FontStepper label={a.terminalFont} value={appearance.terminalFontSize} onChange={(px) => onChange({terminalFontSize: px})} />
                <FontStepper label={a.chatFont} value={appearance.chatFontSize} onChange={(px) => onChange({chatFontSize: px})} />
            </div>

            {/* Vista previa en vivo: cada muestra usa la letra de su área. */}
            <div className="flex flex-col gap-1.5">
                <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{a.preview}</span>
                <div className="grid grid-cols-3 gap-3 max-[900px]:grid-cols-1">
                    <pre
                        className="selectable overflow-hidden rounded-md border border-line bg-surface-2 px-3 py-2 font-mono leading-[1.55] break-words whitespace-pre-wrap text-fg"
                        style={{fontSize: appearance.editorFontSize}}
                    >
                        {a.previewEditor}
                    </pre>
                    <pre
                        className="selectable overflow-hidden rounded-md border border-line bg-surface px-3 py-2 font-mono leading-[1.15] whitespace-pre-wrap text-fg"
                        style={{fontSize: appearance.terminalFontSize}}
                    >
                        {a.previewTerminal}
                    </pre>
                    <p
                        className="selectable overflow-hidden rounded-lg border border-line bg-accent-soft px-3 py-2 leading-relaxed text-fg"
                        style={{fontSize: appearance.chatFontSize}}
                    >
                        {a.previewChat}
                    </p>
                </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
                <Button title={a.resetAppearanceHint} onClick={onResetAppearance}>
                    <RotateCcw className="icon-ui-14" />
                    {a.resetAppearance}
                </Button>
            </div>

            <div className="flex flex-col gap-2 border-t border-line pt-4">
                <span className="font-mono text-ui-10-5 tracking-[0.12em] text-faint uppercase">{a.layoutTitle}</span>
                <p className="text-ui-11-5 leading-relaxed text-faint">{a.layoutIntro}</p>
                <div className="flex flex-wrap items-center gap-2">
                    <Button title={a.resetLayoutHint} onClick={onResetLayout}>
                        <LayoutPanelLeft className="icon-ui-14" />
                        {a.resetLayout}
                    </Button>
                </div>
            </div>
        </div>
    )
}
