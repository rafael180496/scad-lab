import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import type {DragEvent} from 'react'
import {AddChatAttachmentData, ChooseChatAttachment, RemoveChatAttachment} from '../../../wailsjs/go/main/App'
import type {hermes} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {
    MAX_ATTACHMENTS,
    compatibility,
    formatSize,
    guessKind,
    limitFor,
    pastedName,
    readAsDataURL,
    type AttachmentKind,
    type Compat,
    type VisionInfo,
} from '../../lib/chatAttachments'
import {errorText} from '../../lib/errors'

// Un adjunto del compositor, desde que se elige hasta que se manda.
//
//   uploading  el archivo viaja a Go (el frontend lo lee y lo manda en base64; la
//              ruta real y la copia a refs/ las decide Go)
//   ready      Go ya lo tiene y lo registró: `id` es lo que viaja en
//              attachmentIds de SendChatMessage
//   error      no se pudo adjuntar; no se manda
export interface AttachItem {
    /** Clave local, estable desde el primer instante (el id de Go llega después). */
    key: string
    /** Id de Go; vacío mientras sube. */
    id: string
    name: string
    kind: AttachmentKind
    size: number
    status: 'uploading' | 'ready' | 'error'
    error?: string
    /** Miniatura: la imagen reducida o la vista previa de un modelo. */
    thumb?: string
    refPath?: string
}

export interface AttachView extends AttachItem {
    compat: Compat
}

let nextKey = 0

// Estado de los adjuntos de un chat: selección, subida, compatibilidad con el
// modelo elegido y arrastrar/soltar. Vive aparte de ChatPanel porque ese
// componente ya es grande y esto no depende de la conversación.
export function useChatAttachments({workspaceId, vision}: {workspaceId: number | null; vision: VisionInfo}) {
    const t = useT()
    const a = t.chat.attach
    const [items, setItems] = useState<AttachItem[]>([])
    const itemsRef = useRef<AttachItem[]>([])
    const apply = useCallback((fn: (prev: AttachItem[]) => AttachItem[]) => {
        itemsRef.current = fn(itemsRef.current)
        setItems(itemsRef.current)
    }, [])
    const enabled = workspaceId != null
    const wsRef = useRef(workspaceId)
    wsRef.current = workspaceId

    // Los adjuntos son de un proyecto (los modelos se copian a SU refs/): al
    // cambiar de proyecto se sueltan, y Go borra lo que se había copiado.
    useEffect(() => {
        return () => {
            for (const it of itemsRef.current) if (it.id) void RemoveChatAttachment(it.id).catch(() => {})
            itemsRef.current = []
            setItems([])
        }
    }, [workspaceId])

    const fromGo = useCallback(
        (att: hermes.Attachment): Partial<AttachItem> => ({
            id: att.id,
            name: att.name,
            kind: att.kind === 'model' ? 'model' : 'image',
            size: att.size,
            thumb: att.thumbDataUrl || undefined,
            refPath: att.refPath || undefined,
        }),
        [],
    )

    const upload = useCallback(
        (item: AttachItem, file: File) => {
            const ws = wsRef.current
            if (ws == null) return
            readAsDataURL(file)
                .then((data) => AddChatAttachmentData(ws, item.name, data))
                .then((att) => {
                    if (wsRef.current !== ws) {
                        void RemoveChatAttachment(att.id).catch(() => {})
                        return
                    }
                    if (!itemsRef.current.some((x) => x.key === item.key)) {
                        // Lo quitaron mientras subía: ya no hay chip, se borra en Go.
                        void RemoveChatAttachment(att.id).catch(() => {})
                        return
                    }
                    apply((prev) => prev.map((x) => (x.key === item.key ? {...x, ...fromGo(att), status: 'ready'} : x)))
                })
                .catch((err) => apply((prev) => prev.map((x) => (x.key === item.key ? {...x, status: 'error', error: errorText(err)} : x))))
        },
        [apply, fromGo],
    )

    const addFiles = useCallback(
        (files: File[], opts?: {pasted?: boolean}) => {
            if (!enabled || files.length === 0) return
            let live = itemsRef.current.filter((i) => i.status !== 'error').length
            const created: {item: AttachItem; file?: File}[] = []
            files.forEach((file, n) => {
                const name = opts?.pasted ? pastedName(file, n) : file.name || `file-${n + 1}`
                const base = {key: `att-${++nextKey}`, id: '', name, size: file.size}
                const fail = (kind: AttachmentKind, error: string) => created.push({item: {...base, kind, status: 'error', error}})
                const guess = guessKind(file)
                if (guess === 'image-unsupported') return fail('image', fmt(a.errors.unsupportedImage, {name}))
                if (guess === 'unsupported') return fail('image', fmt(a.errors.unsupported, {name}))
                if (file.size === 0) return fail(guess, fmt(a.errors.empty, {name}))
                const max = limitFor(guess)
                if (file.size > max) {
                    return fail(guess, fmt(a.errors.tooBig, {name, size: formatSize(file.size), kind: guess === 'image' ? a.kinds.image : a.kinds.model, max: formatSize(max)}))
                }
                if (live >= MAX_ATTACHMENTS) return fail(guess, fmt(a.errors.tooMany, {name, max: MAX_ATTACHMENTS}))
                live++
                // La miniatura de una imagen es la imagen misma mientras sube.
                created.push({item: {...base, kind: guess, status: 'uploading', thumb: undefined}, file})
            })
            if (created.length === 0) return
            apply((prev) => [...prev, ...created.map((c) => c.item)])
            for (const c of created) {
                if (c.item.status !== 'uploading' || !c.file) continue
                if (c.item.kind === 'image') {
                    readAsDataURL(c.file)
                        .then((url) => apply((prev) => prev.map((x) => (x.key === c.item.key && !x.thumb ? {...x, thumb: url} : x))))
                        .catch(() => {})
                }
                upload(c.item, c.file)
            }
        },
        [a, apply, enabled, upload],
    )

    // Archivos de un portapapeles o un arrastre. Devuelve si había alguno, para que
    // quien llama corte el pegado de texto: si no, además del chip quedaría el
    // nombre del archivo escrito en la caja.
    const addFromTransfer = useCallback(
        (data: DataTransfer | null, opts?: {pasted?: boolean}): boolean => {
            if (!data || !enabled) return false
            const files: File[] = []
            for (const item of Array.from(data.items ?? [])) {
                if (item.kind !== 'file') continue
                const f = item.getAsFile()
                if (f) files.push(f)
            }
            if (files.length === 0) files.push(...Array.from(data.files ?? []))
            if (files.length === 0) return false
            addFiles(files, opts)
            return true
        },
        [addFiles, enabled],
    )

    // El diálogo de archivos del sistema: Go lo abre, copia y analiza lo elegido.
    const [choosing, setChoosing] = useState(false)
    const [chooseError, setChooseError] = useState('')
    const choose = useCallback(async () => {
        const ws = wsRef.current
        if (ws == null) return
        setChoosing(true)
        setChooseError('')
        try {
            const list = (await ChooseChatAttachment(ws)) ?? []
            if (wsRef.current !== ws) {
                for (const att of list) void RemoveChatAttachment(att.id).catch(() => {})
                return
            }
            const free = MAX_ATTACHMENTS - itemsRef.current.filter((i) => i.status !== 'error').length
            const take = list.slice(0, Math.max(0, free))
            for (const att of list.slice(take.length)) void RemoveChatAttachment(att.id).catch(() => {})
            if (list.length > take.length) setChooseError(fmt(a.errors.tooMany, {name: list[take.length].name, max: MAX_ATTACHMENTS}))
            apply((prev) => [...prev, ...take.map((att): AttachItem => ({...(fromGo(att) as AttachItem), key: `att-${++nextKey}`, status: 'ready'}))])
        } catch (e) {
            setChooseError(errorText(e))
        } finally {
            setChoosing(false)
        }
    }, [a, apply, fromGo])

    const remove = useCallback(
        (key: string) => {
            const it = itemsRef.current.find((x) => x.key === key)
            if (!it) return
            // Mientras sube no hay id: cuando Go conteste, upload ve que el chip ya
            // no está y lo borra allá.
            if (it.id) void RemoveChatAttachment(it.id).catch(() => {})
            apply((prev) => prev.filter((x) => x.key !== key))
        },
        [apply],
    )

    const dismissError = useCallback(() => setChooseError(''), [])

    // Lo que viaja con el mensaje: los ids de Go. Tras enviar (Go los consumió) los
    // chips se sueltan SIN pedir borrarlos.
    const takeReady = useCallback(() => {
        const ready = itemsRef.current.filter((x) => x.status === 'ready' && x.id)
        return {ids: ready.map((x) => x.id), keys: ready.map((x) => x.key)}
    }, [])
    const consume = useCallback(
        (keys: string[]) => {
            apply((prev) => prev.filter((x) => !keys.includes(x.key)))
        },
        [apply],
    )

    // ── Qué impide enviar ───────────────────────────────────────────────────
    const view = useMemo<AttachView[]>(() => items.map((i) => ({...i, compat: compatibility(i.kind, vision)})), [items, vision])
    const uploading = view.some((i) => i.status === 'uploading') || choosing
    const blockedCount = view.filter((i) => i.status === 'ready' && i.compat === 'blocked').length
    const readyCount = view.filter((i) => i.status === 'ready').length
    const reason = uploading ? a.uploadingReason : blockedCount > 0 ? fmt(a.blockedReason, {n: blockedCount}) : ''

    // ── Arrastrar y soltar (sobre el panel entero) ──────────────────────────
    const [dragging, setDragging] = useState(false)
    const depth = useRef(0)
    const hasFiles = (e: DragEvent) => Array.from(e.dataTransfer?.types ?? []).includes('Files')
    const dragProps = {
        onDragEnter: (e: DragEvent) => {
            if (!enabled || !hasFiles(e)) return
            e.preventDefault()
            depth.current++
            setDragging(true)
        },
        onDragOver: (e: DragEvent) => {
            if (!enabled || !hasFiles(e)) return
            e.preventDefault()
            e.dataTransfer.dropEffect = 'copy'
        },
        onDragLeave: (e: DragEvent) => {
            if (!hasFiles(e)) return
            depth.current = Math.max(0, depth.current - 1)
            if (depth.current === 0) setDragging(false)
        },
        onDrop: (e: DragEvent) => {
            if (!enabled || !hasFiles(e)) return
            e.preventDefault()
            depth.current = 0
            setDragging(false)
            addFromTransfer(e.dataTransfer)
        },
    }
    // Si el arrastre termina fuera de la ventana, el velo no se queda pegado.
    useEffect(() => {
        if (!dragging) return
        const end = () => {
            depth.current = 0
            setDragging(false)
        }
        const esc = (e: KeyboardEvent) => e.key === 'Escape' && end()
        window.addEventListener('dragend', end)
        window.addEventListener('blur', end)
        window.addEventListener('keydown', esc)
        return () => {
            window.removeEventListener('dragend', end)
            window.removeEventListener('blur', end)
            window.removeEventListener('keydown', esc)
        }
    }, [dragging])

    return {
        items: view,
        addFiles,
        addFromTransfer,
        choose,
        choosing,
        chooseError,
        dismissError,
        remove,
        takeReady,
        consume,
        uploading,
        blockedCount,
        readyCount,
        reason,
        /** Enviar no puede salir mientras algo se sube o el modelo no lo lee. */
        sendBlocked: uploading || blockedCount > 0,
        dragging,
        dragProps,
        enabled,
    }
}
