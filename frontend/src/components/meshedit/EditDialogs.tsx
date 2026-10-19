import {useEffect, useState} from 'react'
import {Check, Copy, GitBranchPlus, Loader2} from 'lucide-react'
import {PreviewMeshEdit} from '../../../wailsjs/go/main/App'
import {fmt, useT} from '../../i18n'
import {copyText} from '../../lib/clipboard'
import {errorText} from '../../lib/errors'
import {toScene, type Session} from '../../lib/meshEditSession'
import {slugify} from '../../lib/slug'
import {Button} from '../ui/Button'
import {Dialog} from '../ui/Dialog'
import {Field, Input} from '../ui/Field'

// «Aplicar»: pide el rótulo (opcional) del checkpoint nuevo. Aplicar nunca toca el STL ni el checkpoint de
// partida: crea uno hijo con el código de la edición.
export function ApplyDialog({open, parent, busy, onCancel, onApply, initial}: {open: boolean; parent: string; busy: boolean; onCancel: () => void; onApply: (label: string) => void; initial?: string}) {
    const t = useT()
    const M = t.meshEdit
    const [label, setLabel] = useState('')
    useEffect(() => {
        if (open) setLabel(initial ?? '')
    }, [open, initial])
    const submit = () => !busy && onApply(label.trim())
    return (
        <Dialog
            open={open}
            onClose={onCancel}
            title={M.apply.title}
            closeLabel={M.apply.cancel}
            className="w-[480px]"
            footer={
                <>
                    <Button title={M.apply.cancel} onClick={onCancel} disabled={busy}>
                        {M.apply.cancel}
                    </Button>
                    <Button variant="primary" title={M.apply.confirmHint} onClick={submit} disabled={busy} data-testid="apply-confirm">
                        {busy ? <Loader2 className="icon-ui-14 animate-spin" /> : <GitBranchPlus className="icon-ui-14" />}
                        {M.apply.confirm}
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
                <p className="text-ui-12-5 leading-relaxed text-muted">{fmt(M.apply.intro, {parent})}</p>
                <Field label={M.apply.label} hint={fmt(M.apply.labelHint, {name: `NNN-${slugify(label || M.apply.defaultLabel)}.scad`})}>
                    <Input data-autofocus value={label} placeholder={M.apply.defaultLabel} title={M.apply.label} maxLength={80} onChange={(e) => setLabel(e.target.value)} />
                </Field>
                <button type="submit" hidden title={M.apply.confirm} />
            </form>
        </Dialog>
    )
}

// «Ver código»: el OpenSCAD que se guardaría. Es una vista previa; no se escribe nada.
export function CodeDialog({open, session, onClose}: {open: boolean; session: Session; onClose: () => void}) {
    const t = useT()
    const M = t.meshEdit
    const [state, setState] = useState<{code: string; error: string; loading: boolean}>({code: '', error: '', loading: false})
    const [copied, setCopied] = useState(false)
    useEffect(() => {
        if (!open) return
        let alive = true
        setState({code: '', error: '', loading: true})
        setCopied(false)
        PreviewMeshEdit(session.workspaceId, toScene(session))
            .then((code) => alive && setState({code, error: '', loading: false}))
            .catch((e) => alive && setState({code: '', error: errorText(e), loading: false}))
        return () => {
            alive = false
        }
        // Se pide al abrir: el diálogo es modal, la edición no cambia mientras está abierto.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [open])
    return (
        <Dialog
            open={open}
            onClose={onClose}
            title={M.code.title}
            closeLabel={M.code.close}
            className="w-[760px]"
            footer={
                <>
                    <Button
                        title={M.code.copyHint}
                        disabled={!state.code}
                        onClick={async () => {
                            if (await copyText(state.code)) setCopied(true)
                        }}
                    >
                        {copied ? <Check className="icon-ui-14" /> : <Copy className="icon-ui-14" />}
                        {copied ? M.code.copied : M.code.copy}
                    </Button>
                    <Button variant="primary" title={M.code.close} onClick={onClose}>
                        {M.code.close}
                    </Button>
                </>
            }
        >
            <div className="flex flex-col gap-2 px-4 py-4">
                <p className="text-ui-12 leading-relaxed text-muted">{M.code.intro}</p>
                {state.loading ? (
                    <p className="flex items-center gap-2 text-ui-12 text-muted">
                        <Loader2 className="icon-ui-14 animate-spin text-accent" />
                        {M.code.loading}
                    </p>
                ) : null}
                {state.error ? <p className="selectable rounded-md border border-danger/50 px-3 py-2 text-ui-12 text-danger">{state.error}</p> : null}
                {state.code ? (
                    <pre data-testid="code" className="selectable max-h-[52vh] overflow-auto rounded border border-line bg-bg px-3 py-2 font-mono text-ui-11 leading-[1.5] whitespace-pre text-fg">
                        {state.code}
                    </pre>
                ) : null}
            </div>
        </Dialog>
    )
}
