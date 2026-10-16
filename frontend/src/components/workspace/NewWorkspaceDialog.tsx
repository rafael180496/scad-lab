import {useEffect, useId, useRef, useState} from 'react'
import type {printers} from '../../../wailsjs/go/models'
import {ChooseDirectory, CreateWorkspaceIn, EffectiveWorkspacesRoot, PreviewWorkspacePath, SetWorkspacesRoot} from '../../../wailsjs/go/main/App'
import {Dialog} from '../ui/Dialog'
import {Button} from '../ui/Button'
import {Field, Input, Select} from '../ui/Field'
import {toast} from '../ui/Toast'
import {useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {cn} from '../../lib/cn'

/** Solo el banco de capturas: el diálogo abierto con un nombre y una ubicación ya escritos. */
export type NewWorkspaceShot = {name?: string; parent?: string}

// Qué muestra la vista previa: la ruta exacta, o por qué todavía no hay una.
type Preview = {path: string; problem: 'name' | 'location' | null}

export function NewWorkspaceDialog({
    open,
    onClose,
    printerList,
    defaultPrinter,
    onCreated,
    onDefaultRootChanged,
    shot,
}: {
    open: boolean
    onClose: () => void
    printerList: printers.Printer[]
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
        setPrinter(defaultPrinter)
        setAsDefault(false)
        setParent(shotRef.current?.parent ?? '')
        EffectiveWorkspacesRoot()
            .then((r) => {
                if (!alive) return
                setDefaultRoot(r)
                setParent((p) => p || r)
            })
            .catch(() => {})
        return () => {
            alive = false
        }
    }, [open, defaultPrinter])

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
            const w = await CreateWorkspaceIn(name.trim(), printer, parent.trim())
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
            className="w-[520px]"
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
                <Field label={t.workspaces.printer}>
                    <Select value={printer} onChange={(e) => setPrinter(e.target.value)}>
                        {printerList.map((p) => (
                            <option key={p.id} value={p.id}>
                                {p.name} — {p.build_volume.x}×{p.build_volume.y}×{p.build_volume.z} mm
                            </option>
                        ))}
                    </Select>
                </Field>
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
