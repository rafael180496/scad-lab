import {useCallback, useEffect, useState} from 'react'
import {CircleAlert, Download, Loader2} from 'lucide-react'
import {ImportMiniToolsServers} from '../../../../wailsjs/go/main/App'
import type {main} from '../../../../wailsjs/go/models'
import {fmt, useT} from '../../../i18n'
import {errorText} from '../../../lib/errors'
import {cn} from '../../../lib/cn'
import {serverKind} from '../../../lib/modelServers'
import {Button} from '../../ui/Button'
import {Dialog} from '../../ui/Dialog'
import {toast} from '../../ui/Toast'

// «Importar servidores de mini-tools» (S12.b). Solo cuando el usuario lo pide y lo
// confirma, en tres pasos: una vista previa (dryRun, que no escribe nada) con la
// lista de lo que hay, la confirmación, y recién entonces la importación. Se copian
// nombre, tipo, dirección y modelo por defecto; las claves NO (están cifradas con la
// contraseña de mini-tools y ScadLab nunca las lee), así que los servidores llegan sin
// clave y hay que volver a escribirla: el aviso va antes del botón, no después.
// Si mini-tools no está en este equipo lo dice, y no es un error.
export function ImportMiniTools({onImported}: {onImported: () => void}) {
    const t = useT()
    const p = t.providers.import
    const [open, setOpen] = useState(false)
    const [preview, setPreview] = useState<main.MiniToolsImport | null>(null)
    const [error, setError] = useState('')
    const [importing, setImporting] = useState(false)

    const scan = useCallback(() => {
        setPreview(null)
        setError('')
        ImportMiniToolsServers(true)
            .then((r) => setPreview(r))
            .catch((e) => setError(errorText(e)))
    }, [])

    useEffect(() => {
        if (open) scan()
    }, [open, scan])

    const servers = preview?.servers ?? []
    const fresh = servers.filter((s) => !s.exists && !s.error)
    const existing = servers.filter((s) => s.exists)

    const run = async () => {
        setImporting(true)
        try {
            const r = await ImportMiniToolsServers(false)
            toast.ok(r.imported > 0 ? fmt(p.done, {n: r.imported}) : p.doneNone)
            setOpen(false)
            onImported()
        } catch (e) {
            setError(errorText(e))
        } finally {
            setImporting(false)
        }
    }
    const close = () => !importing && setOpen(false)

    return (
        <>
            <Button data-testid="import-minitools" title={p.buttonHint} onClick={() => setOpen(true)}>
                <Download className="icon-ui-14" />
                {p.button}
            </Button>
            <Dialog
                open={open}
                onClose={close}
                title={p.title}
                closeLabel={p.closeHint}
                className="w-[600px]"
                footer={
                    <>
                        <Button title={p.cancelHint} onClick={close} disabled={importing}>
                            {preview && fresh.length === 0 && !error ? p.close : p.cancel}
                        </Button>
                        {fresh.length > 0 ? (
                            <Button data-testid="import-confirm" variant="primary" title={p.confirmHint} onClick={() => void run()} disabled={importing}>
                                {importing ? <Loader2 className="icon-ui-14 animate-spin" /> : <Download className="icon-ui-14" />}
                                {importing ? p.importing : fmt(p.confirm, {n: fresh.length})}
                            </Button>
                        ) : null}
                    </>
                }
            >
                <div data-testid="import-dialog" className="flex flex-col gap-3 px-4 py-3">
                    {error ? (
                        <p className="flex items-start gap-1.5 rounded-md border border-danger/40 bg-danger/10 px-2 py-1.5 text-ui-11-5 text-danger">
                            <CircleAlert className="icon-ui-14 mt-px shrink-0" />
                            <span className="selectable min-w-0 break-words">{fmt(p.error, {msg: error})}</span>
                        </p>
                    ) : null}
                    {!preview && !error ? (
                        <p className="flex items-center gap-2 text-ui-12 text-muted">
                            <Loader2 className="icon-ui-14 animate-spin" />
                            {p.checking}
                        </p>
                    ) : null}
                    {preview && !preview.found ? (
                        <div className="flex flex-col gap-1">
                            <p className="text-ui-12-5 text-fg">{p.notFound}</p>
                            <p className="text-ui-11-5 text-muted">{p.notFoundHint}</p>
                        </div>
                    ) : null}
                    {preview?.found ? (
                        <>
                            <p className="text-ui-11-5 text-muted">
                                {p.intro} <span className="selectable font-mono text-ui-10-5 text-faint">{fmt(p.found, {path: preview.path})}</span>
                            </p>
                            {servers.length === 0 ? <p className="text-ui-12 text-muted">{p.empty}</p> : null}
                            {servers.length > 0 ? (
                                <>
                                    <p className="font-mono text-ui-10-5 text-faint">
                                        {fmt(p.listNew, {n: fresh.length})}
                                        {existing.length > 0 ? ` · ${fmt(p.listExists, {n: existing.length})}` : ''}
                                    </p>
                                    <ul className="flex max-h-72 flex-col divide-y divide-line overflow-auto rounded-md border border-line">
                                        {servers.map((s, i) => (
                                            <li key={`${s.name}-${i}`} data-import-row={s.exists ? 'exists' : s.error ? 'error' : 'new'} className={cn('flex items-center gap-2 px-3 py-1.5', s.exists && 'opacity-60')}>
                                                <div className="min-w-0 flex-1">
                                                    <p className="flex flex-wrap items-center gap-x-2 text-ui-12-5 text-fg">
                                                        <span className="truncate">{s.name}</span>
                                                        <span className="shrink-0 rounded-full bg-surface-2 px-1.5 font-mono text-ui-10 text-muted">{t.providers.kinds[serverKind(s.kind)]}</span>
                                                    </p>
                                                    <p className="selectable truncate font-mono text-ui-10-5 text-faint" title={s.baseUrl}>
                                                        {s.baseUrl}
                                                        {s.defaultModel ? ` · ${s.defaultModel}` : ''}
                                                    </p>
                                                    {s.error ? <p className="selectable text-ui-11 text-danger">{s.error}</p> : null}
                                                </div>
                                                <span className={cn('shrink-0 rounded-full border px-1.5 py-px font-mono text-ui-10', s.exists ? 'border-line text-faint' : s.error ? 'border-danger/40 text-danger' : 'border-ok/40 text-ok')}>
                                                    {s.exists ? p.rowExists : s.error ? p.rowError : p.rowNew}
                                                </span>
                                            </li>
                                        ))}
                                    </ul>
                                    {fresh.length === 0 ? <p className="text-ui-12 text-muted">{p.nothingNew}</p> : null}
                                    <p className="flex items-start gap-1.5 rounded-md border border-warn/40 bg-warn/10 px-2.5 py-2 text-ui-11-5 text-fg">
                                        <CircleAlert className="icon-ui-14 mt-px shrink-0 text-warn" />
                                        <span>{p.keysWarn}</span>
                                    </p>
                                </>
                            ) : null}
                        </>
                    ) : null}
                </div>
            </Dialog>
        </>
    )
}
