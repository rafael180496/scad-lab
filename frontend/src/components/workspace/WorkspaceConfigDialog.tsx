import {useEffect, useMemo, useRef, useState} from 'react'
import {CircleCheck, CircleDashed, ExternalLink, RotateCcw, Settings, TriangleAlert} from 'lucide-react'
import {main, store, type printers} from '../../../wailsjs/go/models'
import {DefaultWorkspaceSkills, GetWorkspaceConfig, ListMaterials, ListSkills, SaveWorkspaceConfig} from '../../../wailsjs/go/main/App'
import {BrowserOpenURL} from '../../../wailsjs/runtime/runtime'
import {Dialog} from '../ui/Dialog'
import {Button} from '../ui/Button'
import {Input, Select} from '../ui/Field'
import {toast} from '../ui/Toast'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {cn} from '../../lib/cn'
import {colorsCount, colorsLine, defaultColorsOf, installedSlicer, maxColorsOf, num, recommendedSlicer, swapBrandSkill, volumeLine} from '../../lib/printerInfo'
import {PrinterPicker} from '../printer/PrinterPicker'
import {SkillsChecklist} from './SkillsChecklist'

/**
 * «Configuración del workspace» (S14): la impresora y sus colores, las skills y los ajustes de
 * impresión PROPIOS de un workspace. Lo que no se fija se hereda de Configuración → Impresión; cada
 * ajuste de impresión muestra el valor heredado y se vuelve a él con «Usar el de Configuración».
 * Guarda con SaveWorkspaceConfig, que valida todo o nada, reescribe las reglas del workspace
 * (CLAUDE.md, AGENTS.md, printer.json) y sincroniza sus skills.
 */

export type WorkspaceConfigSection = 'printer' | 'skills' | 'print'
/** Solo el banco de capturas: la sección que abre. */
export type WorkspaceConfigShot = {section?: WorkspaceConfigSection}

// Los rangos que acepta Go (backend/store/settings.go, wsconfig.ValidatePrint): fuera de ellos Guardar se apaga.
const RANGES = {
    infill: {min: 0, max: 100, step: 1},
    walls: {min: 1, max: 10, step: 1},
    line: {min: 0.2, max: 1.5, step: 0.01},
    filament: {min: 1, max: 3.5, step: 0.05},
} as const
type NumKey = keyof typeof RANGES

// null = el ajuste no es del workspace: rige el heredado.
type Draft = {material: string | null} & Record<NumKey, string | null>
const emptyDraft: Draft = {material: null, infill: null, walls: null, line: null, filament: null}

const toNum = (s: string) => Number(s.trim().replace(',', '.'))
const inRange = (k: NumKey, s: string) => {
    const v = toNum(s)
    return s.trim() !== '' && Number.isFinite(v) && v >= RANGES[k].min && v <= RANGES[k].max
}
const draftOf = (p: store.WorkspacePrint | undefined): Draft => ({
    material: p?.material ? p.material : null,
    infill: p?.infillPercent != null ? String(p.infillPercent) : null,
    walls: p?.wallLoops != null ? String(p.wallLoops) : null,
    line: p?.lineWidthMm != null ? String(p.lineWidthMm) : null,
    filament: p?.filamentDiameterMm != null ? String(p.filamentDiameterMm) : null,
})

export function WorkspaceConfigDialog({
    open,
    onClose,
    workspaceId,
    workspaceName,
    printerList,
    brands,
    slicers,
    settings,
    onOpenSettings,
    shot,
}: {
    open: boolean
    onClose: () => void
    workspaceId: number | null
    workspaceName?: string
    printerList: printers.Printer[]
    brands: printers.Brand[]
    /** Todos los slicers del registro, detectados o no (DetectSlicers). */
    slicers: main.Slicer[]
    /** Configuración global: de ahí salen los valores heredados. */
    settings: store.Settings
    /** Abre Configuración → Impresión (donde se indica la ruta de un slicer). */
    onOpenSettings: () => void
    shot?: WorkspaceConfigShot
}) {
    const t = useT()
    const k = t.wsConfig
    const [section, setSection] = useState<WorkspaceConfigSection>('printer')
    const [loaded, setLoaded] = useState<main.WorkspaceConfigView | null>(null)
    const [skillList, setSkillList] = useState<main.SkillView[]>([])
    const [materials, setMaterials] = useState<store.Material[]>([])
    const [printerId, setPrinterId] = useState('')
    const [colors, setColors] = useState<number | null>(null)
    const [skills, setSkills] = useState<string[]>([])
    const [draft, setDraft] = useState<Draft>(emptyDraft)
    const [busy, setBusy] = useState(false)
    const brandOf = useRef('')
    const shotRef = useRef(shot)
    shotRef.current = shot

    // Al abrir se lee lo que rige hoy y se arma el borrador.
    useEffect(() => {
        if (!open || workspaceId == null) return
        let alive = true
        setSection(shotRef.current?.section ?? 'printer')
        setLoaded(null)
        Promise.all([GetWorkspaceConfig(workspaceId), ListSkills(), ListMaterials()])
            .then(([c, sk, mat]) => {
                if (!alive) return
                setLoaded(c)
                setSkillList(sk ?? [])
                setMaterials(mat ?? [])
                setPrinterId(c.workspace.printerId)
                setColors(c.print?.colors ? c.print.colors : null)
                setSkills(c.skills ?? [])
                setDraft(draftOf(c.print))
                brandOf.current = c.printer?.skill ?? ''
            })
            .catch((e) => {
                if (!alive) return
                toast.error(errorText(e))
                onClose()
            })
        return () => {
            alive = false
        }
        // onClose cambia de identidad en cada render del padre: no debe volver a leer el workspace.
    }, [open, workspaceId])

    const printer = printerList.find((p) => p.id === printerId)
    const maxColors = maxColorsOf(printer)
    const shownColors = Math.min(colors ?? defaultColorsOf(printer), maxColors)
    const brandSkills = useMemo(() => new Set(brands.map((b) => b.skill).filter(Boolean)), [brands])
    const existing = useMemo(() => new Set(skillList.map((s) => s.name)), [skillList])

    const choosePrinter = (id: string) => {
        const next = printerList.find((p) => p.id === id)
        setPrinterId(id)
        setColors(null)
        // La skill de la marca acompaña a la impresora; el resto de la lista no se toca.
        const from = brandOf.current // se lee ya: la función de setSkills corre después
        setSkills((cur) => swapBrandSkill(cur, from, next?.skill ?? '', existing))
        brandOf.current = next?.skill ?? ''
    }

    // «Restablecer por defecto»: las encendidas en Configuración → Skills que no son de marca + la de la impresora.
    const resetSkills = async () => {
        try {
            setSkills((await DefaultWorkspaceSkills(printerId)) ?? [])
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    // Lo heredado: de Configuración → Impresión, y el filamento de la máquina si la ficha lo trae.
    const inherited = {
        material: settings.defaultMaterial,
        infill: String(settings.infillPercent),
        walls: String(settings.wallLoops),
        line: String(settings.lineWidthMm),
        filament: String(printer && printer.filament_diameter_mm > 0 ? printer.filament_diameter_mm : settings.filamentDiameterMm),
    }
    const materialName = (id: string) => materials.find((m) => m.id === id)?.name ?? id
    const invalid = (Object.keys(RANGES) as NumKey[]).filter((key) => draft[key] != null && !inRange(key, draft[key]!))
    const canSave = !busy && !!loaded && !!printer && invalid.length === 0

    const save = async () => {
        if (!canSave || workspaceId == null) return
        const num = (key: NumKey) => (draft[key] == null ? undefined : toNum(draft[key]!))
        setBusy(true)
        try {
            const res = await SaveWorkspaceConfig(
                workspaceId,
                main.WorkspaceConfigInput.createFrom({
                    printerId,
                    skills,
                    print: store.WorkspacePrint.createFrom({
                        // 0 = los colores de fábrica de la impresora.
                        colors: colors ?? 0,
                        material: draft.material ?? '',
                        infillPercent: num('infill'),
                        wallLoops: draft.walls == null ? undefined : Math.round(toNum(draft.walls)),
                        lineWidthMm: num('line'),
                        filamentDiameterMm: num('filament'),
                    }),
                }),
            )
            toast.ok(k.saved)
            if (res.kept?.length) toast.error(fmt(k.kept, {names: res.kept.join(', ')}))
            if (res.syncError) toast.error(fmt(k.syncError, {msg: res.syncError}))
            onClose()
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setBusy(false)
        }
    }

    const sections: {id: WorkspaceConfigSection; label: string; hint: string}[] = [
        {id: 'printer', label: k.sections.printer, hint: k.sections.printerHint},
        {id: 'skills', label: k.sections.skills, hint: k.sections.skillsHint},
        {id: 'print', label: k.sections.print, hint: k.sections.printHint},
    ]

    return (
        <Dialog
            open={open}
            onClose={onClose}
            title={workspaceName ? fmt(k.titleFor, {name: workspaceName}) : k.title}
            closeLabel={k.cancel}
            className="w-[760px]"
            footer={
                <>
                    <Button title={k.cancel} onClick={onClose}>
                        {k.cancel}
                    </Button>
                    <Button variant="primary" title={invalid.length ? k.invalidHint : k.saveHint} disabled={!canSave} onClick={() => void save()}>
                        {k.save}
                    </Button>
                </>
            }
        >
            <div className="flex min-h-[420px] flex-col" data-testid="workspace-config">
                <div role="tablist" aria-label={k.title} className="flex shrink-0 gap-1 border-b border-line px-3 pt-2">
                    {sections.map((s) => (
                        <button
                            key={s.id}
                            type="button"
                            role="tab"
                            aria-selected={section === s.id}
                            title={s.hint}
                            data-testid={`wsconfig-tab-${s.id}`}
                            onClick={() => setSection(s.id)}
                            className={cn(
                                'no-drag -mb-px h-8 rounded-t-md border-b-2 px-3 text-ui-12-5 transition-colors duration-100',
                                section === s.id ? 'border-accent text-fg' : 'border-transparent text-muted hover:text-fg',
                            )}
                        >
                            {s.label}
                        </button>
                    ))}
                </div>

                {!loaded ? (
                    <p className="px-5 py-6 text-ui-12 text-faint">{t.app.loading}</p>
                ) : section === 'printer' ? (
                    <div className="flex flex-col gap-4 p-5">
                        <div className="flex flex-col gap-1">
                            <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{k.printer}</span>
                            <PrinterPicker value={printerId} onChange={choosePrinter} printerList={printerList} brands={brands} />
                        </div>
                        {maxColors > 1 ? (
                            <label className="flex flex-col gap-1">
                                <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{k.colors}</span>
                                <Select value={String(shownColors)} title={k.colorsTitle} onChange={(e) => setColors(Number(e.target.value))} className="max-w-[240px]">
                                    {Array.from({length: maxColors}, (_, i) => i + 1).map((n) => (
                                        <option key={n} value={n}>
                                            {colorsCount(t, n)}
                                        </option>
                                    ))}
                                </Select>
                                <span className="text-ui-11-5 text-faint">{fmt(k.colorsHint, {system: printer?.multicolor_system || t.printers.multicolor, max: maxColors})}</span>
                            </label>
                        ) : null}
                        {printer ? <PrinterSheet printer={printer} slicers={slicers} onOpenSettings={() => { onClose(); onOpenSettings() }} /> : null}
                    </div>
                ) : section === 'skills' ? (
                    <div className="flex flex-col gap-3 p-5">
                        <div className="flex items-start justify-between gap-3">
                            <p className="text-ui-12 leading-relaxed text-muted">{k.skillsIntro}</p>
                            <Button className="shrink-0" title={k.resetSkillsHint} onClick={() => void resetSkills()}>
                                <RotateCcw className="icon-ui-12" />
                                {k.resetSkills}
                            </Button>
                        </div>
                        <SkillsChecklist
                            skills={skillList}
                            value={skills}
                            onChange={setSkills}
                            printerSkill={printer?.skill ?? ''}
                            brandSkills={brandSkills}
                            className="max-h-[340px]"
                        />
                        <span className="font-mono text-ui-10-5 text-faint">{fmt(t.workspaces.skillsCount, {n: skills.length, total: skillList.length})}</span>
                    </div>
                ) : (
                    <div className="flex flex-col gap-1 p-5">
                        <p className="mb-2 text-ui-12 leading-relaxed text-muted">{k.printIntro}</p>
                        <InheritRow
                            label={t.settings.material}
                            overridden={draft.material != null}
                            inherited={materialName(inherited.material)}
                            onReset={() => setDraft((d) => ({...d, material: null}))}
                        >
                            <Select
                                value={draft.material ?? inherited.material}
                                title={t.settings.material}
                                onChange={(e) => setDraft((d) => ({...d, material: e.target.value}))}
                            >
                                {materials.map((m) => (
                                    <option key={m.id} value={m.id}>
                                        {m.name}
                                    </option>
                                ))}
                            </Select>
                        </InheritRow>
                        <NumRow k="infill" label={t.settings.infill} unit="%" hint={t.settings.estimateIntro} draft={draft} inherited={inherited.infill} setDraft={setDraft} />
                        <NumRow k="walls" label={t.settings.wallLoops} hint={t.settings.wallLoopsHint} draft={draft} inherited={inherited.walls} setDraft={setDraft} />
                        <NumRow k="line" label={t.settings.lineWidth} unit="mm" hint={t.settings.lineWidthHint} draft={draft} inherited={inherited.line} setDraft={setDraft} />
                        <NumRow
                            k="filament"
                            label={t.settings.filamentDiameter}
                            unit="mm"
                            hint={printer && printer.filament_diameter_mm > 0 ? fmt(k.filamentFromPrinter, {name: printer.name}) : t.settings.filamentDiameterHint}
                            draft={draft}
                            inherited={inherited.filament}
                            setDraft={setDraft}
                        />
                    </div>
                )}
            </div>
        </Dialog>
    )
}

// Una fila de «Impresión»: el control, y a su derecha el valor heredado o el botón para volver a él.
function InheritRow({
    label,
    overridden,
    inherited,
    onReset,
    children,
    hint,
    error,
}: {
    label: string
    overridden: boolean
    inherited: string
    onReset: () => void
    children: React.ReactNode
    hint?: string
    error?: string
}) {
    const t = useT()
    return (
        <div className="grid grid-cols-[minmax(0,1fr)_minmax(0,180px)_minmax(0,230px)] items-center gap-3 border-b border-line py-2.5 last:border-b-0" title={hint}>
            <span className="min-w-0">
                <span className="block truncate font-mono text-ui-10-5 tracking-wider text-muted uppercase">{label}</span>
                {error ? <span className="block text-ui-11 text-danger">{error}</span> : null}
            </span>
            <div className="min-w-0">{children}</div>
            <div className="flex min-w-0 items-center justify-end gap-2">
                {overridden ? (
                    <Button className="h-6 px-2 text-ui-11-5" title={fmt(t.wsConfig.useGlobalHint, {value: inherited})} onClick={onReset}>
                        <RotateCcw className="icon-ui-12" />
                        {t.wsConfig.useGlobal}
                    </Button>
                ) : (
                    <span className="truncate text-right font-mono text-ui-11 text-faint" title={t.wsConfig.inheritedHint}>
                        {fmt(t.wsConfig.inherited, {value: inherited})}
                    </span>
                )}
            </div>
        </div>
    )
}

function NumRow({
    k,
    label,
    unit,
    hint,
    draft,
    inherited,
    setDraft,
}: {
    k: NumKey
    label: string
    unit?: string
    hint: string
    draft: Draft
    inherited: string
    setDraft: React.Dispatch<React.SetStateAction<Draft>>
}) {
    const t = useT()
    const r = RANGES[k]
    const v = draft[k]
    const bad = v != null && !inRange(k, v)
    return (
        <InheritRow
            label={label}
            overridden={v != null}
            inherited={unit ? `${num(Number(inherited))} ${unit}` : num(Number(inherited))}
            onReset={() => setDraft((d) => ({...d, [k]: null}))}
            hint={hint}
            error={bad ? fmt(t.wsConfig.range, {min: num(r.min), max: num(r.max)}) : undefined}
        >
            <Input
                type="number"
                inputMode="decimal"
                min={r.min}
                max={r.max}
                step={r.step}
                value={v ?? inherited}
                title={hint}
                aria-invalid={bad}
                className={cn('tabular-nums', bad && 'border-danger')}
                onChange={(e) => setDraft((d) => ({...d, [k]: e.target.value}))}
            />
        </InheritRow>
    )
}

// La ficha de la impresora elegida: lo que dice el catálogo, de un vistazo.
function PrinterSheet({printer: p, slicers, onOpenSettings}: {printer: printers.Printer; slicers: main.Slicer[]; onOpenSettings: () => void}) {
    const t = useT()
    const s = t.printers.sheet
    const rec = recommendedSlicer(p, slicers)
    const inst = installedSlicer(p, slicers)
    const temp = (n: number) => (n > 0 ? `${n} °C` : '—')
    const rows: [string, React.ReactNode][] = [
        [s.volume, volumeLine(t, p)],
        [s.nozzle, p.nozzle_count > 1 ? fmt(s.nozzleMany, {n: p.nozzle_count, mm: num(p.nozzle_mm)}) : p.nozzle_mm > 0 ? fmt(s.nozzleMm, {mm: num(p.nozzle_mm)}) : '—'],
        [s.filament, p.filament_diameter_mm > 0 ? fmt(s.nozzleMm, {mm: num(p.filament_diameter_mm)}) : '—'],
        [s.multicolor, colorsLine(t, p)],
        [s.surface, p.bed_surface || '—'],
        [s.temps, fmt(s.tempsValue, {nozzle: temp(p.max_nozzle_temp_c), bed: temp(p.max_bed_temp_c)})],
        [s.chamber, p.heated_chamber ? s.chamberHeated : p.enclosed ? s.chamberClosed : s.chamberOpen],
        [
            s.slicer,
            rec ? (
                <span className="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <span>{rec.name}</span>
                    {inst?.id === rec.id ? (
                        <span className="flex items-center gap-1 text-ok" title={inst.path}>
                            <CircleCheck className="icon-ui-12" />
                            {s.installed}
                        </span>
                    ) : inst ? (
                        <span className="flex items-center gap-1 text-muted" title={inst.path}>
                            <CircleCheck className="icon-ui-12" />
                            {fmt(s.installedOther, {name: inst.name})}
                        </span>
                    ) : (
                        <>
                            <span className="flex items-center gap-1 text-faint">
                                <CircleDashed className="icon-ui-12" />
                                {s.notInstalled}
                            </span>
                            <Button variant="ghost" className="h-6 px-1.5 text-ui-11-5" title={fmt(s.pathHint, {name: rec.name})} onClick={onOpenSettings}>
                                <Settings className="icon-ui-12" />
                                {s.setPath}
                            </Button>
                        </>
                    )}
                </span>
            ) : (
                '—'
            ),
        ],
    ]
    return (
        <section className="rounded-lg border border-line bg-surface-2 p-3" data-testid="printer-sheet">
            <div className="mb-2 flex items-center justify-between gap-3">
                <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{s.title}</span>
                {p.sources?.length ? (
                    <Button variant="ghost" className="h-6 px-1.5 text-ui-11-5" title={fmt(s.sourceHint, {url: p.sources[0]})} onClick={() => BrowserOpenURL(p.sources[0])}>
                        <ExternalLink className="icon-ui-12" />
                        {s.source}
                    </Button>
                ) : null}
            </div>
            <dl className="grid grid-cols-[minmax(0,150px)_minmax(0,1fr)] gap-x-4 gap-y-1.5 text-ui-12">
                {rows.map(([label, value]) => (
                    <div key={label} className="contents">
                        <dt className="text-faint">{label}</dt>
                        <dd className="selectable min-w-0 text-fg">{value}</dd>
                    </div>
                ))}
            </dl>
            {p.verify?.length ? (
                <p className="mt-2.5 flex items-start gap-1.5 text-ui-11-5 leading-snug text-warn">
                    <TriangleAlert className="mt-px icon-ui-12 shrink-0" />
                    <span className="selectable">{fmt(t.printers.verifyHint, {items: p.verify.join('; ')})}</span>
                </p>
            ) : null}
        </section>
    )
}
