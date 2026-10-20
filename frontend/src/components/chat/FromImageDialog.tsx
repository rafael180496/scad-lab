import {useEffect, useState, type ClipboardEvent, type KeyboardEvent} from 'react'
import {ImagePlus, Loader2, TriangleAlert, X} from 'lucide-react'
import {hermes} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {MAX_ATTACHMENTS, formatSize} from '../../lib/chatAttachments'
import {fromImageMessage} from '../../lib/fromImage'
import {fromImageShot} from '../../lib/fromImageShot'
import {requestFromImage} from '../../lib/sessionBridge'
import {Button, IconButton} from '../ui/Button'
import {Dialog} from '../ui/Dialog'
import {Field, Input} from '../ui/Field'
import {useChatAttachments, type AttachView} from './useChatAttachments'

// «Crear desde una imagen» (S15): el diálogo que reúne lo que el asistente necesita para modelar a partir de fotos —
// las fotos (elegir, pegar o arrastrar: es la misma lógica de adjuntos del chat), una medida de referencia opcional y
// qué es el objeto— y lo manda al chat como UN mensaje estructurado en una conversación nueva en «Agente». Lo envía el
// panel Chat (lib/sessionBridge): este diálogo vive también en el estado vacío del workspace, donde el chat ni siquiera
// está montado.
//
// Las fotos se suben a Go apenas se eligen (son adjuntos pendientes del workspace). Cerrar sin confirmar las suelta y Go
// las borra; confirmar las entrega al chat sin borrarlas (consume).
export function FromImageDialog({open, workspaceId, onClose}: {open: boolean; workspaceId: number | null; onClose: () => void}) {
    // El contenido se monta solo con el diálogo abierto: cada apertura parte de cero y cerrar suelta las fotos.
    return open && workspaceId != null ? <FromImageBody workspaceId={workspaceId} onClose={onClose} /> : null
}

function Photo({it, onRemove}: {it: AttachView; onRemove: () => void}) {
    const t = useT()
    const failed = it.status === 'error'
    return (
        <li
            data-testid="from-image-photo"
            title={fmt(t.chat.attach.chipTitle, {name: it.name, kind: t.chat.attach.kinds.image, size: formatSize(it.size)})}
            className={cn('relative w-[88px] shrink-0 overflow-hidden rounded-lg border bg-surface-2', failed ? 'border-danger/50' : 'border-line')}
        >
            <div className="relative flex aspect-square items-center justify-center bg-bg">
                {it.thumb && !failed ? (
                    <img src={it.thumb} alt="" draggable={false} className="h-full w-full object-cover" />
                ) : failed ? (
                    <TriangleAlert className="icon-ui-20 text-danger" />
                ) : null}
                {it.status === 'uploading' ? (
                    <span className="absolute inset-0 flex items-center justify-center bg-surface/70" aria-hidden>
                        <Loader2 className="icon-ui-16 animate-spin text-accent" />
                    </span>
                ) : null}
                <IconButton
                    title={fmt(t.chat.attach.removeTitle, {name: it.name})}
                    onClick={onRemove}
                    className="absolute top-0.5 right-0.5 size-5 rounded bg-surface/80 text-fg hover:bg-surface hover:text-danger"
                >
                    <X className="icon-ui-12" />
                </IconButton>
            </div>
            <p className="truncate px-1.5 py-1 text-ui-10-5 text-muted">{it.name}</p>
        </li>
    )
}

function FromImageBody({workspaceId, onClose}: {workspaceId: number; onClose: () => void}) {
    const t = useT()
    const c = t.fromImage
    const attach = useChatAttachments({workspaceId, vision: null, imagesOnly: true})
    const [reference, setReference] = useState(fromImageShot.reference ?? '')
    const [what, setWhat] = useState(fromImageShot.what ?? '')

    // Banco de capturas: fotos inventadas ya subidas. En la app real `fromImageShot` está vacío y esto no hace nada.
    // addReady ignora un id repetido, así que el doble montaje de StrictMode (que suelta los chips entre uno y otro) no duplica.
    useEffect(() => {
        for (const p of fromImageShot.photos ?? []) {
            attach.addReady(hermes.Attachment.createFrom({id: p.id, kind: 'image', name: p.name, mime: 'image/jpeg', size: p.size, thumbDataUrl: p.thumb, refPath: '', summary: ''}))
        }
    }, [])

    const photos = attach.items
    const ready = photos.filter((i) => i.status === 'ready').length
    const why = attach.uploading ? c.uploading : ready === 0 ? c.needPhotos : ''
    const canCreate = why === ''

    const onPaste = (e: ClipboardEvent) => {
        // Una captura o una foto pegada se adjunta; el texto va al campo en el que se pega.
        if (attach.addFromTransfer(e.clipboardData, {pasted: true})) e.preventDefault()
    }

    const create = () => {
        if (!canCreate) return
        const taken = attach.takeReady()
        const attachments = photos
            .filter((i) => taken.keys.includes(i.key))
            .map((i) => hermes.Attachment.createFrom({id: i.id, kind: 'image', name: i.name, mime: '', size: i.size, thumbDataUrl: i.thumb ?? '', refPath: i.refPath ?? '', summary: ''}))
        // Las fotos pasan al chat: se sueltan los chips de aquí SIN pedir borrarlas (el desmontaje las borraría).
        attach.consume(taken.keys)
        requestFromImage({workspaceId, text: fromImageMessage(t, {what, reference, count: attachments.length}), attachments})
        onClose()
    }

    // Enter en uno de los dos campos crea el modelo (si ya se puede), como en cualquier formulario corto.
    const onEnter = (e: KeyboardEvent<HTMLInputElement>) => {
        if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
            e.preventDefault()
            create()
        }
    }

    return (
        <Dialog
            open
            onClose={onClose}
            title={c.title}
            closeLabel={c.close}
            className="w-[600px]"
            footer={
                <>
                    <Button title={c.cancelHint} onClick={onClose}>
                        {c.cancel}
                    </Button>
                    <Button variant="primary" data-testid="from-image-create" title={why || c.createHint} disabled={!canCreate} onClick={create}>
                        <ImagePlus className="icon-ui-14" />
                        {c.create}
                    </Button>
                </>
            }
        >
            <div data-testid="from-image" className="flex flex-col gap-4 px-4 py-4" onPaste={onPaste}>
                <p className="text-ui-12-5 leading-relaxed text-muted">{c.intro}</p>

                <section aria-label={c.photos} className="flex flex-col gap-1.5">
                    <div className="flex items-center justify-between">
                        <h3 className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{c.photos}</h3>
                        <span className="font-mono text-ui-10-5 text-faint tabular-nums">{fmt(c.count, {n: photos.filter((i) => i.status !== 'error').length, max: MAX_ATTACHMENTS})}</span>
                    </div>
                    <div
                        {...attach.dragProps}
                        className={cn(
                            'flex flex-wrap items-center gap-2 rounded-lg border border-dashed p-2 transition-colors',
                            attach.dragging ? 'border-accent bg-accent-soft' : 'border-line-strong bg-surface-2/40',
                        )}
                    >
                        {attach.dragging ? (
                            <p className="w-full py-6 text-center text-ui-12-5 font-medium text-accent">{c.dropMore}</p>
                        ) : (
                            <>
                                {photos.length > 0 ? (
                                    <ul role="list" aria-label={c.photos} className="contents">
                                        {photos.map((it) => (
                                            <Photo key={it.key} it={it} onRemove={() => attach.remove(it.key)} />
                                        ))}
                                    </ul>
                                ) : (
                                    <p className="min-w-[10rem] flex-1 px-1 text-ui-12 text-faint">{c.dropEmpty}</p>
                                )}
                                <Button data-testid="from-image-add" title={c.addHint} disabled={!attach.enabled || attach.choosing} onClick={() => void attach.choose()}>
                                    <ImagePlus className="icon-ui-14" />
                                    {c.add}
                                </Button>
                            </>
                        )}
                    </div>
                    {attach.chooseError ? (
                        <p role="alert" className="text-ui-11 text-danger">
                            {attach.chooseError}
                        </p>
                    ) : null}
                    {photos.some((i) => i.status === 'error') ? (
                        <ul role="alert" className="space-y-0.5 text-ui-11 text-danger">
                            {photos
                                .filter((i) => i.status === 'error')
                                .map((i) => (
                                    <li key={i.key}>{i.error}</li>
                                ))}
                        </ul>
                    ) : null}
                </section>

                <Field label={c.reference} hint={c.referenceHint}>
                    <Input data-testid="from-image-reference" value={reference} placeholder={c.referencePlaceholder} title={c.referenceHint} onChange={(e) => setReference(e.target.value)} onKeyDown={onEnter} />
                </Field>
                <Field label={c.what} hint={c.whatHint}>
                    <Input data-autofocus data-testid="from-image-what" value={what} placeholder={c.whatPlaceholder} title={c.whatHint} onChange={(e) => setWhat(e.target.value)} onKeyDown={onEnter} />
                </Field>
            </div>
        </Dialog>
    )
}
