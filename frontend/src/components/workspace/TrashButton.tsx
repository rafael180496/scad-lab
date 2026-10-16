import {useCallback, useEffect, useState} from 'react'
import {ArchiveRestore, Trash2} from 'lucide-react'
import type {checkpoint, store} from '../../../wailsjs/go/models'
import {ListTrash, RestoreCheckpoint} from '../../../wailsjs/go/main/App'
import {checkpointDate} from '../../lib/checkpointInfo'
import {errorText} from '../../lib/errors'
import {fmt, useT} from '../../i18n'
import {Button, IconButton} from '../ui/Button'
import {Dialog} from '../ui/Dialog'
import {toast} from '../ui/Toast'

// Entrada a la papelera de checkpoints, en la cabecera del árbol: un ícono con el
// contador de lo que hay y, al abrirlo, la lista con «Restaurar». Nada se borra
// de verdad (las operaciones viven en backend/checkpoint): esto solo devuelve.
//
// `checkpoints` es el índice del árbol; cambia cada vez que el backend avisa de
// un cambio, y ese es el momento de volver a contar la papelera.
export function TrashButton({workspaceId, checkpoints, shotOpen}: {workspaceId: number; checkpoints: store.Checkpoint[]; shotOpen?: boolean}) {
    const t = useT()
    const [items, setItems] = useState<checkpoint.TrashedItem[]>([])
    const [open, setOpen] = useState(!!shotOpen)
    const [busy, setBusy] = useState<string | null>(null)

    const load = useCallback(async () => {
        try {
            setItems((await ListTrash(workspaceId)) ?? [])
        } catch (e) {
            toast.error(`${t.checkpoints.trash.loadError}: ${errorText(e)}`)
        }
    }, [workspaceId, t])

    useEffect(() => {
        let stale = false
        ListTrash(workspaceId)
            .then((l) => !stale && setItems(l ?? []))
            .catch(() => !stale && setItems([]))
        return () => {
            stale = true
        }
    }, [workspaceId, checkpoints])

    const restore = async (it: checkpoint.TrashedItem) => {
        setBusy(it.name)
        try {
            const name = await RestoreCheckpoint(workspaceId, it.name)
            toast.ok(fmt(t.checkpoints.trash.restored, {file: name}))
            await load()
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setBusy(null)
        }
    }

    const n = items.length
    return (
        <>
            <IconButton
                title={n > 0 ? fmt(t.checkpoints.trash.hint, {n}) : t.checkpoints.trash.hintEmpty}
                onClick={() => {
                    setOpen(true)
                    void load()
                }}
                className="relative"
            >
                <Trash2 className="icon-ui-14" />
                {n > 0 ? (
                    <span className="absolute -top-px -right-px flex min-w-3.5 items-center justify-center rounded-full bg-accent px-1 font-mono text-ui-10 leading-[14px] text-accent-fg">
                        {n > 99 ? '99+' : n}
                    </span>
                ) : null}
            </IconButton>
            <Dialog open={open} onClose={() => setOpen(false)} title={t.checkpoints.trash.title} closeLabel={t.checkpoints.trash.close} className="w-[600px]">
                <p className="border-b border-line px-4 py-3 text-ui-12 leading-relaxed text-muted">{t.checkpoints.trash.intro}</p>
                {n === 0 ? (
                    <p className="px-4 py-8 text-center text-ui-12-5 text-faint">{t.checkpoints.trash.empty}</p>
                ) : (
                    <ul className="divide-y divide-line">
                        {items.map((it) => (
                            <li key={it.name} className="flex items-center gap-3 px-4 py-2.5">
                                <div className="min-w-0 flex-1">
                                    <p className="selectable truncate font-mono text-ui-12 text-fg" title={it.original}>
                                        {it.original}
                                    </p>
                                    {it.description ? (
                                        <p className="selectable truncate text-ui-12 text-muted" title={it.description}>
                                            {it.description}
                                        </p>
                                    ) : null}
                                    <p className="truncate font-mono text-ui-11 text-faint">
                                        {checkpointDate(it.trashedAt)}
                                        {it.parent ? ` · ${fmt(t.checkpoints.trash.parent, {parent: it.parent})}` : ''}
                                    </p>
                                </div>
                                <Button
                                    title={fmt(t.checkpoints.trash.restoreHint, {file: it.original})}
                                    disabled={busy !== null}
                                    onClick={() => void restore(it)}
                                >
                                    <ArchiveRestore className="icon-ui-14" />
                                    {t.checkpoints.trash.restore}
                                </Button>
                            </li>
                        ))}
                    </ul>
                )}
            </Dialog>
        </>
    )
}
