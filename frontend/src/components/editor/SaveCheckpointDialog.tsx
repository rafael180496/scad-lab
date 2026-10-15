import {useEffect, useState} from 'react'
import {GitBranchPlus} from 'lucide-react'
import {Dialog} from '../ui/Dialog'
import {Button} from '../ui/Button'
import {Field, Input} from '../ui/Field'
import {fmt, useT} from '../../i18n'
import {slugify} from '../../lib/slug'

// Guardar desde el editor nunca sobrescribe: crea un checkpoint hijo del que
// se estaba viendo. Este diálogo pide el rótulo (da el nombre del archivo) y
// la descripción (va en el encabezado @checkpoint y en el árbol).
export function SaveCheckpointDialog({
    open,
    parent,
    saving,
    initial,
    onCancel,
    onSave,
}: {
    open: boolean
    parent: string
    saving: boolean
    initial?: {label: string; description: string} // banco de capturas
    onCancel: () => void
    onSave: (label: string, description: string) => void
}) {
    const t = useT()
    const [label, setLabel] = useState('')
    const [desc, setDesc] = useState('')
    useEffect(() => {
        if (open) {
            setLabel(initial?.label ?? '')
            setDesc(initial?.description ?? '')
        }
    }, [open, initial?.label, initial?.description])

    const ok = label.trim() !== '' && !saving
    const submit = () => ok && onSave(label.trim(), desc.trim())

    return (
        <Dialog
            open={open}
            onClose={onCancel}
            title={t.editor.saveTitle}
            closeLabel={t.editor.cancel}
            className="w-[480px]"
            footer={
                <>
                    <Button title={t.editor.cancel} onClick={onCancel}>
                        {t.editor.cancel}
                    </Button>
                    <Button variant="primary" title={t.editor.saveButtonHint} onClick={submit} disabled={!ok}>
                        <GitBranchPlus className="icon-ui-14" />
                        {t.editor.saveButton}
                    </Button>
                </>
            }
        >
            <form
                className="flex flex-col gap-4 px-4 py-4"
                onSubmit={(e) => {
                    e.preventDefault()
                    submit()
                }}
            >
                <p className="text-ui-12-5 leading-relaxed text-muted">
                    {fmt(t.editor.saveIntro, {parent})}
                </p>
                <Field label={t.editor.label} hint={fmt(t.editor.labelHint, {name: `NNN-${slugify(label)}.scad`})}>
                    <Input
                        data-autofocus
                        value={label}
                        placeholder={t.editor.labelPlaceholder}
                        title={t.editor.label}
                        maxLength={80}
                        onChange={(e) => setLabel(e.target.value)}
                    />
                </Field>
                <Field label={t.editor.description} hint={t.editor.descriptionHint}>
                    <Input
                        value={desc}
                        placeholder={t.editor.descriptionPlaceholder}
                        title={t.editor.description}
                        maxLength={200}
                        onChange={(e) => setDesc(e.target.value)}
                    />
                </Field>
                <button type="submit" hidden title={t.editor.saveButton} />
            </form>
        </Dialog>
    )
}
