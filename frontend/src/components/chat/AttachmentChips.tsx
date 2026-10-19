import type {KeyboardEvent} from 'react'
import {Ban, Box, CircleAlert, FileImage, Loader2, TriangleAlert, UploadCloud, X} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {formatSize} from '../../lib/chatAttachments'
import type {AttachmentMeta} from '../../lib/chatTypes'
import type {AttachView} from './useChatAttachments'

// Fichas de lo que viaja con el próximo mensaje, sobre la caja de texto, y de lo
// que viajó con un mensaje ya enviado.
//
// Cada ficha dice en qué está: subiéndose, lista, con aviso del modelo (ámbar:
// puede que no la lea; rojo: no la va a leer y no deja enviar) o con error (no
// se pudo adjuntar). Los motivos de un error se escriben debajo, no solo en el
// tooltip: un tooltip no se ve con el teclado.

const FOCUS = 'focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-accent'

function Thumb({kind, url, px, busy}: {kind: string; url?: string; px: number; busy?: boolean}) {
    const Icon = kind === 'model' ? Box : FileImage
    return (
        <span
            className="relative flex shrink-0 items-center justify-center overflow-hidden rounded bg-surface-2 text-accent"
            style={{width: `calc(${px}px * var(--ui-font-scale))`, height: `calc(${px}px * var(--ui-font-scale))`}}
        >
            {url ? (
                // La miniatura es decoración: el nombre del archivo ya está al lado.
                <img src={url} alt="" draggable={false} className="h-full w-full object-cover" />
            ) : (
                <Icon className="icon-ui-16" />
            )}
            {busy ? (
                <span className="absolute inset-0 flex items-center justify-center bg-surface/70" aria-hidden>
                    <Loader2 className="icon-ui-14 animate-spin text-accent" />
                </span>
            ) : null}
        </span>
    )
}

export function ComposerAttachments({
    items,
    vision,
    onRemove,
}: {
    items: AttachView[]
    /** Lo que se sabe de la visión del modelo, para explicar los avisos. */
    vision: {vision: boolean; visionKnown: boolean} | null
    onRemove: (key: string) => void
}) {
    const t = useT()
    const a = t.chat.attach
    if (items.length === 0) return null

    const hintFor = (it: AttachView): string => {
        if (it.compat === 'maybe') return a.hint.maybe
        if (it.compat === 'blocked') return a.hint.blocked
        if (it.kind === 'model' && vision && vision.visionKnown && !vision.vision) return a.hint.modelNoVision
        return ''
    }
    const problems = items.filter((i) => i.status === 'error' && i.error)

    return (
        <div className="px-2 pt-2">
            <ul role="list" aria-label={a.listLabel} className="flex flex-wrap gap-1">
                {items.map((it) => {
                    const failed = it.status === 'error'
                    const ready = it.status === 'ready'
                    const blocked = ready && it.compat === 'blocked'
                    const maybe = ready && it.compat === 'maybe'
                    const kindLabel = a.kinds[it.kind]
                    const short = failed
                        ? a.short.failed
                        : it.status === 'uploading'
                          ? a.uploading
                          : blocked
                            ? a.short.noVision
                            : maybe
                              ? a.short.maybe
                              : it.refPath
                                ? it.refPath
                                : formatSize(it.size)
                    const hint = failed ? (it.error ?? '') : hintFor(it)
                    const title = [fmt(a.chipTitle, {name: it.name, kind: kindLabel, size: formatSize(it.size)}), hint].filter(Boolean).join('\n')
                    const tone =
                        failed || blocked
                            ? 'border-danger/50 bg-danger/10 text-danger'
                            : maybe
                              ? 'border-warn/50 bg-warn/10 text-warn'
                              : 'border-line bg-surface-2 text-muted'
                    const onKey = (e: KeyboardEvent) => {
                        if ((e.key === 'Delete' || e.key === 'Backspace') && e.target === e.currentTarget) {
                            e.preventDefault()
                            onRemove(it.key)
                        }
                    }
                    return (
                        <li
                            key={it.key}
                            tabIndex={0}
                            title={title}
                            aria-label={`${it.name}, ${kindLabel}, ${short}${hint ? `. ${hint.replace(/\n/g, ' ')}` : ''}`}
                            data-attach-chip={failed ? 'error' : blocked ? 'blocked' : maybe ? 'maybe' : it.status}
                            onKeyDown={onKey}
                            className={cn('flex max-w-full items-center gap-1.5 rounded-md border py-0.5 pr-1 pl-0.5 text-ui-10-5 sm:max-w-[16rem]', tone, FOCUS)}
                        >
                            <Thumb kind={it.kind} url={failed ? undefined : it.thumb} px={32} busy={it.status === 'uploading'} />
                            <span className="min-w-0 flex-1">
                                <span className="block truncate text-ui-11 font-medium text-fg">{it.name}</span>
                                <span className="flex items-center gap-0.5 truncate tabular-nums">
                                    {failed || blocked ? <Ban className="icon-ui-12 shrink-0" /> : null}
                                    {maybe ? <TriangleAlert className="icon-ui-12 shrink-0" /> : null}
                                    <span className="truncate">{short}</span>
                                </span>
                            </span>
                            <button
                                type="button"
                                onClick={() => onRemove(it.key)}
                                title={fmt(a.removeTitle, {name: it.name})}
                                aria-label={fmt(a.removeTitle, {name: it.name})}
                                className={cn('no-drag shrink-0 rounded p-0.5 hover:text-danger', FOCUS)}
                            >
                                <X className="icon-ui-12" />
                            </button>
                        </li>
                    )
                })}
            </ul>
            {problems.length > 0 ? (
                <ul role="alert" className="mt-1 space-y-0.5 text-ui-10-5 text-danger">
                    {problems.map((p) => (
                        <li key={p.key} className="flex items-start gap-1">
                            <CircleAlert className="mt-px icon-ui-12 shrink-0" />
                            <span className="min-w-0 break-words">{p.error}</span>
                        </li>
                    ))}
                </ul>
            ) : null}
        </div>
    )
}

// Adjuntos de un mensaje ya enviado. Con su miniatura (el historial la guarda) o,
// si no hay, el ícono: nunca el archivo.
export function MessageAttachments({attachments}: {attachments: AttachmentMeta[]}) {
    const t = useT()
    const a = t.chat.attach
    if (attachments.length === 0) return null
    return (
        <ul role="list" aria-label={a.sentListLabel} className="mb-1 flex max-w-[85%] flex-wrap justify-end gap-1">
            {attachments.map((att, i) => {
                const kind = att.kind === 'model' ? 'model' : 'image'
                const title = [fmt(a.chipTitle, {name: att.name, kind: a.kinds[kind], size: formatSize(att.size)}), att.refPath ?? '', a.sent[kind]].filter(Boolean).join('\n')
                return (
                    <li
                        key={`${att.name}-${i}`}
                        tabIndex={0}
                        title={title}
                        aria-label={`${att.name}, ${a.kinds[kind]}, ${formatSize(att.size)}`}
                        data-sent-attachment={kind}
                        className={cn('flex max-w-full items-center gap-1.5 rounded-lg border border-line bg-accent-soft py-0.5 pr-2 pl-0.5 text-ui-10-5 text-muted', FOCUS)}
                    >
                        <Thumb kind={kind} url={att.thumb} px={att.thumb ? 44 : 32} />
                        <span className="min-w-0">
                            <span className="block max-w-[12rem] truncate text-ui-11 font-medium text-fg">{att.name}</span>
                            <span className="block max-w-[12rem] truncate tabular-nums">{att.refPath ? att.refPath : `${a.kinds[kind]} · ${formatSize(att.size)}`}</span>
                        </span>
                    </li>
                )
            })}
        </ul>
    )
}

// Velo que tapa el panel mientras se arrastra un archivo por encima. No captura el
// puntero: los eventos de arrastre tienen que seguir llegando al panel de abajo,
// que es quien cuenta entradas y salidas.
export function DropOverlay() {
    const t = useT()
    const a = t.chat.attach
    return (
        <div
            aria-hidden
            data-attach-drop
            className="pointer-events-none absolute inset-1 z-30 flex flex-col items-center justify-center gap-1 rounded-xl border-2 border-dashed border-accent bg-surface/85 px-4 text-center backdrop-blur-[2px]"
        >
            <span className="flex icon-ui-36 items-center justify-center rounded-full bg-accent-soft text-accent">
                <UploadCloud className="icon-ui-20" />
            </span>
            <p className="text-ui-13 font-semibold text-fg">{a.dropTitle}</p>
            <p className="text-ui-11 text-muted">{a.dropHint}</p>
            <p className="max-w-xs text-ui-10-5 text-faint">{a.limits}</p>
        </div>
    )
}
