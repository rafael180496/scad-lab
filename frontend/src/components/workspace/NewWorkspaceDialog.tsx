import {useEffect, useState} from 'react'
import type {printers} from '../../../wailsjs/go/models'
import {CreateWorkspace, EffectiveWorkspacesRoot} from '../../../wailsjs/go/main/App'
import {Dialog} from '../ui/Dialog'
import {Button} from '../ui/Button'
import {Field, Input, Select} from '../ui/Field'
import {toast} from '../ui/Toast'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'

export function NewWorkspaceDialog({
    open,
    onClose,
    printerList,
    defaultPrinter,
    onCreated,
}: {
    open: boolean
    onClose: () => void
    printerList: printers.Printer[]
    defaultPrinter: string
    onCreated: (id: number) => void
}) {
    const t = useT()
    const [name, setName] = useState('')
    const [printer, setPrinter] = useState(defaultPrinter)
    const [root, setRoot] = useState('')
    const [busy, setBusy] = useState(false)

    useEffect(() => {
        if (!open) return
        setName('')
        setPrinter(defaultPrinter)
        EffectiveWorkspacesRoot().then(setRoot).catch(() => setRoot(''))
    }, [open, defaultPrinter])

    const submit = async () => {
        if (!name.trim() || busy) return
        setBusy(true)
        try {
            const w = await CreateWorkspace(name.trim(), printer)
            toast.ok(t.workspaces.created)
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
            className="w-[480px]"
            footer={
                <>
                    <Button title={t.workspaces.cancel} onClick={onClose}>
                        {t.workspaces.cancel}
                    </Button>
                    <Button variant="primary" title={t.workspaces.newHint} onClick={submit} disabled={!name.trim() || busy}>
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
                <Field label={t.workspaces.name} hint={root ? fmt(t.workspaces.rootHint, {root}) : undefined}>
                    <Input autoFocus value={name} placeholder={t.workspaces.namePlaceholder} onChange={(e) => setName(e.target.value)} />
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
            </form>
        </Dialog>
    )
}
