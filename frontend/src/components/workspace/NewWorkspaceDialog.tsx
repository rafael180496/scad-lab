import {useEffect, useId, useMemo, useRef, useState} from 'react'
import {main, store, type printers} from '../../../wailsjs/go/models'
import {
    ChooseDirectory,
    CreateWorkspaceWith,
    DefaultWorkspaceSkills,
    EffectiveWorkspacesRoot,
    ListSkills,
    PreviewWorkspacePath,
    SetWorkspacesRoot,
} from '../../../wailsjs/go/main/App'
import {Dialog} from '../ui/Dialog'
import {Button} from '../ui/Button'
import {Field, Input, Select} from '../ui/Field'
import {toast} from '../ui/Toast'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {cn} from '../../lib/cn'
import {colorsCount, colorsLine, defaultColorsOf, maxColorsOf, swapBrandSkill, volumeLine} from '../../lib/printerInfo'
import {PrinterPicker} from '../printer/PrinterPicker'
import {SkillsChecklist} from './SkillsChecklist'

/** Solo el banco de capturas: el diálogo abierto con un nombre y una ubicación ya escritos, y la impresora y los colores elegidos. */
export type NewWorkspaceShot = {name?: string; parent?: string; printer?: string; colors?: number; printerOpen?: boolean; printerQuery?: string}

// Qué muestra la vista previa: la ruta exacta, o por qué todavía no hay una.
type Preview = {path: string; problem: 'name' | 'location' | null}

export function NewWorkspaceDialog({
    open,
    onClose,
    printerList,
    brands,
    defaultPrinter,
    onCreated,
    onDefaultRootChanged,
    shot,
}: {
    open: boolean
    onClose: () => void
    printerList: printers.Printer[]
    brands: printers.Brand[]
    defaultPrinter: string
    onCreated: (id: number) => void
    /** Se guardó «Usar esta carpeta como predeterminada»: Configuración tiene que ver la nueva. */
    onDefaultRootChanged: (root: string) => void
    shot?: NewWorkspaceShot
}) {
    const t = useT()
    const checkId = useId()
    const [name, setName] = useState('')
    const [printer, setPrinter] = useState(defaultPrinter)
    // Colores elegidos; null = los de fábrica de la impresora (cambiar de impresora los vuelve a null).
    const [colors, setColors] = useState<number | null>(null)
    // Skills: las de por defecto de la impresora mientras nadie toque una casilla; después, lo que eligió el usuario.
    const [skillList, setSkillList] = useState<main.SkillView[]>([])
    const [skills, setSkills] = useState<string[]>([])
    const touched = useRef(false)
    const brandOf = useRef('')
    // Carpeta padre que se muestra en el campo; defaultRoot, la predeterminada de hoy.
    const [parent, setParent] = useState('')
    const [defaultRoot, setDefaultRoot] = useState('')
    const [asDefault, setAsDefault] = useState(false)
    const [preview, setPreview] = useState<Preview>({path: '', problem: null})
    const [busy, setBusy] = useState(false)
    const shotRef = useRef(shot)
    shotRef.current = shot

    useEffect(() => {
        if (!open) return
        let alive = true
        setName(shotRef.current?.name ?? '')
        setPrinter(shotRef.current?.printer ?? defaultPrinter)
        setColors(shotRef.current?.colors ?? null)
        touched.current = false
        brandOf.current = ''
        setAsDefault(false)
        setParent(shotRef.current?.parent ?? '')
        EffectiveWorkspacesRoot()
            .then((r) => {
                if (!alive) return
                setDefaultRoot(r)
                setParent((p) => p || r)
            })
            .catch(() => {})
        ListSkills()
            .then((l) => alive && setSkillList(l ?? []))
            .catch((e) => alive && toast.error(errorText(e)))
        return () => {
            alive = false
        }
    }, [open, defaultPrinter])

    const chosen = printerList.find((p) => p.id === printer)
    const maxColors = maxColorsOf(chosen)
    const shownColors = Math.min(colors ?? defaultColorsOf(chosen), maxColors)
    const brandSkills = useMemo(() => new Set(brands.map((b) => b.skill).filter(Boolean)), [brands])
    const existing = useMemo(() => new Set(skillList.map((s) => s.name)), [skillList])

    // Las skills de la impresora elegida: mientras no se toque una casilla se recalculan enteras
    // (las encendidas en Configuración + la de la marca); una vez tocadas solo se cambia la de
    // marca, para no deshacer lo que el usuario eligió.
    useEffect(() => {
        if (!open || !printer || skillList.length === 0) return
        let alive = true
        const brand = chosen?.skill ?? ''
        DefaultWorkspaceSkills(printer)
            .then((def) => {
                if (!alive) return
                // `from` se lee ya: la función de setSkills corre después y brandOf ya valdría la marca nueva.
                const from = brandOf.current
                if (!touched.current) setSkills(def ?? [])
                else setSkills((cur) => swapBrandSkill(cur, from, brand, existing))
                brandOf.current = brand
            })
            .catch((e) => alive && toast.error(errorText(e)))
        return () => {
            alive = false
        }
    }, [open, printer, chosen?.skill, skillList.length, existing])

    const choosePrinter = (id: string) => {
        setPrinter(id)
        setColors(null) // los de fábrica de la impresora nueva (otra puede tener otro máximo)
    }

    // La ruta final la calcula Go (mismo Slug que al crear): lo que se ve es lo que se crea.
    // Un nombre vacío no tiene ruta; si Go devuelve vacío con un nombre escrito, se
    // distingue el nombre sin letras de la ubicación relativa probando con un nombre válido.
    useEffect(() => {
        if (!open) return
        let alive = true
        const where = parent.trim()
        if (!name.trim()) {
            setPreview({path: '', problem: null})
            return
        }
        PreviewWorkspacePath(name.trim(), where)
            .then(async (path) => {
                if (!alive) return
                if (path) return setPreview({path, problem: null})
                const probe = await PreviewWorkspacePath('x', where)
                if (alive) setPreview({path: '', problem: probe ? 'name' : 'location'})
            })
            .catch(() => alive && setPreview({path: '', problem: null}))
        return () => {
            alive = false
        }
    }, [open, name, parent])

    const sameAsDefault = parent.trim() === '' || parent.trim() === defaultRoot
    const canCreate = !!name.trim() && !!preview.path && !busy

    const choose = async () => {
        try {
            const dir = await ChooseDirectory(t.workspaces.chooseLocation, parent.trim() || defaultRoot)
            if (dir) setParent(dir)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const submit = async () => {
        if (!canCreate) return
        setBusy(true)
        try {
            const w = await CreateWorkspaceWith(
                main.NewWorkspaceInput.createFrom({
                    name: name.trim(),
                    parentDir: parent.trim(),
                    printerId: printer,
                    // Sin tocar las casillas se deja que Go resuelva las de por defecto (la lista que se ve es la misma); así un clic en Crear
                    // antes de que cargue la lista no crea un workspace sin skills.
                    skills: touched.current ? skills : undefined,
                    // 0 = los colores de fábrica de la impresora (se heredan, no se congelan).
                    print: store.WorkspacePrint.createFrom({colors: colors ?? 0}),
                }),
            )
            toast.ok(t.workspaces.created)
            if (asDefault && !sameAsDefault) {
                try {
                    await SetWorkspacesRoot(parent.trim())
                    onDefaultRootChanged(parent.trim())
                    toast.ok(t.workspaces.defaultSaved)
                } catch (e) {
                    toast.error(errorText(e))
                }
            }
            onCreated(w.id)
            onClose()
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setBusy(false)
        }
    }

    return (
        <Dialog
            open={open}
            onClose={onClose}
            title={t.workspaces.new}
            closeLabel={t.workspaces.cancel}
            className="w-[640px]"
            footer={
                <>
                    <Button title={t.workspaces.cancel} onClick={onClose}>
                        {t.workspaces.cancel}
                    </Button>
                    <Button variant="primary" title={t.workspaces.newHint} onClick={submit} disabled={!canCreate}>
                        {t.workspaces.create}
                    </Button>
                </>
            }
        >
            <form
                className="flex flex-col gap-4 p-4"
                onSubmit={(e) => {
                    e.preventDefault()
                    void submit()
                }}
            >
                <Field label={t.workspaces.name}>
                    <Input data-autofocus value={name} placeholder={t.workspaces.namePlaceholder} onChange={(e) => setName(e.target.value)} />
                </Field>
                <div className="flex flex-col gap-1.5">
                    <div className="grid grid-cols-[minmax(0,1fr)_auto] items-end gap-3">
                        {/* Sin <label>: el selector abre una lista y un clic dentro de ella no debe volver a pulsar el botón. */}
                        <div className="flex min-w-0 flex-col gap-1">
                            <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{t.workspaces.printer}</span>
                            <PrinterPicker
                                value={printer}
                                onChange={choosePrinter}
                                printerList={printerList}
                                brands={brands}
                                initialOpen={shot?.printerOpen}
                                initialQuery={shot?.printerQuery}
                            />
                        </div>
                        {maxColors > 1 ? (
                            <label className="flex w-[148px] flex-col gap-1" title={fmt(t.workspaces.colorsHint, {system: chosen?.multicolor_system || t.printers.multicolor, max: maxColors})}>
                                <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{t.workspaces.colors}</span>
                                <Select value={String(shownColors)} title={t.workspaces.colorsTitle} onChange={(e) => setColors(Number(e.target.value))}>
                                    {Array.from({length: maxColors}, (_, i) => i + 1).map((n) => (
                                        <option key={n} value={n}>
                                            {colorsCount(t, n)}
                                        </option>
                                    ))}
                                </Select>
                            </label>
                        ) : null}
                    </div>
                    {chosen ? (
                        <p className="font-mono text-ui-11 leading-snug text-faint" data-testid="printer-summary">
                            {volumeLine(t, chosen)} · {colorsLine(t, chosen)}
                            {chosen.verify?.length ? (
                                <span className="ml-1.5 text-warn" title={fmt(t.printers.verifyHint, {items: chosen.verify.join('; ')})}>
                                    · {t.printers.verifyBadge}
                                </span>
                            ) : null}
                        </p>
                    ) : null}
                </div>
                <div className="flex flex-col gap-1.5">
                    <div className="flex items-baseline justify-between gap-3">
                        <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{t.workspaces.skills}</span>
                        <span className="font-mono text-ui-10-5 text-faint">{fmt(t.workspaces.skillsCount, {n: skills.length, total: skillList.length})}</span>
                    </div>
                    <SkillsChecklist
                        skills={skillList}
                        value={skills}
                        onChange={(next) => {
                            touched.current = true
                            setSkills(next)
                        }}
                        printerSkill={chosen?.skill ?? ''}
                        brandSkills={brandSkills}
                        compact
                        className="max-h-[220px]"
                    />
                    <span className="text-ui-11-5 leading-snug text-faint">{t.workspaces.skillsHint}</span>
                </div>
                <Field label={t.workspaces.location} hint={t.workspaces.locationHint}>
                    <div className="flex gap-2">
                        <Input
                            value={parent}
                            title={t.workspaces.location}
                            placeholder={t.workspaces.locationPlaceholder}
                            onChange={(e) => setParent(e.target.value)}
                        />
                        <Button title={t.workspaces.chooseLocationHint} onClick={() => void choose()}>
                            {t.settings.choose}
                        </Button>
                    </div>
                </Field>
                <div className="flex flex-col gap-1 rounded-md border border-line bg-surface-2 px-2.5 py-2" aria-live="polite">
                    <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{t.workspaces.willCreate}</span>
                    {preview.path ? (
                        <span data-testid="workspace-preview" title={preview.path} className="selectable font-mono text-ui-12 break-words text-fg">
                            {/* Un salto posible tras cada separador: una ruta larga se parte entre carpetas, no a media palabra. */}
                            {preview.path.split(/([\\/])/).map((part, i) => (part === '/' || part === '\\' ? <span key={i}>{part}<wbr /></span> : part))}
                        </span>
                    ) : (
                        <span data-testid="workspace-preview" className={cn('text-ui-12', preview.problem ? 'text-danger' : 'text-faint')}>
                            {preview.problem === 'name'
                                ? t.workspaces.previewBadName
                                : preview.problem === 'location'
                                  ? t.workspaces.previewBadLocation
                                  : t.workspaces.previewEmpty}
                        </span>
                    )}
                </div>
                <label
                    htmlFor={checkId}
                    title={sameAsDefault ? t.workspaces.alreadyDefault : t.workspaces.makeDefaultHint}
                    className={cn('flex items-center gap-2 text-ui-12-5', sameAsDefault ? 'text-faint' : 'text-fg')}
                >
                    <input
                        id={checkId}
                        type="checkbox"
                        className="icon-ui-14 accent-accent"
                        checked={asDefault && !sameAsDefault}
                        disabled={sameAsDefault}
                        onChange={(e) => setAsDefault(e.target.checked)}
                    />
                    {t.workspaces.makeDefault}
                </label>
            </form>
        </Dialog>
    )
}
