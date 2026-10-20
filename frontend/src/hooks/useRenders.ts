import {useCallback, useEffect, useRef, useState} from 'react'
import type {render} from '../../wailsjs/go/models'
import {CancelRender, GetRenderState, RenderCheckpoint} from '../../wailsjs/go/main/App'
import {toast} from '../components/ui/Toast'
import {errorText} from '../lib/errors'
import {onEvent} from '../lib/events'

export type RenderResults = Record<string, render.Result>
export type RenderRunning = Record<string, boolean>

/** Estado de render de un proyecto abierto. */
export type WsRender = {
    results: RenderResults
    running: RenderRunning
    /** Ya llegó GetRenderState: antes de eso «sin resultado» no significa nada. */
    loaded: boolean
    /** Llegó al menos un evento de render de este proyecto (su estado local manda sobre la foto del backend). */
    known: boolean
}

const EMPTY_RESULTS: RenderResults = {}
const EMPTY_RUNNING: RenderRunning = {}

// useRenders lleva el estado de render de los checkpoints de TODOS los proyectos
// abiertos (S8): cambiar de pestaña no pierde nada y las pestañas de fondo
// pueden mostrar que se está renderizando. La fuente es el backend: los eventos
// render:started/finished llegan también por los renders que dispara el
// watcher (un agente que creó un .scad), no solo por los que pide la UI, y
// llevan el workspaceId.
export function useRenders(openIds: number[], activeId: number | null) {
    const [byWs, setByWs] = useState<Record<number, WsRender>>({})
    // Proyectos para los que ya se pidió GetRenderState.
    const requested = useRef(new Set<number>())
    const idKey = openIds.join(',')

    const patch = useCallback((id: number, fn: (cur: WsRender) => Partial<WsRender>) => {
        setByWs((prev) => {
            const cur = prev[id] ?? {results: EMPTY_RESULTS, running: EMPTY_RUNNING, loaded: false, known: false}
            return {...prev, [id]: {...cur, ...fn(cur)}}
        })
    }, [])

    useEffect(() => {
        const open = new Set(openIds)
        for (const id of [...requested.current]) if (!open.has(id)) requested.current.delete(id)
        // Un proyecto cerrado suelta su estado: al reabrirlo se vuelve a leer.
        setByWs((prev) => {
            const gone = Object.keys(prev).filter((k) => !open.has(Number(k)))
            if (!gone.length) return prev
            const next = {...prev}
            for (const k of gone) delete next[Number(k)]
            return next
        })
        for (const id of openIds) {
            if (requested.current.has(id)) continue
            requested.current.add(id)
            GetRenderState(id)
                // Lo que ya llegó por eventos es más nuevo que la foto inicial.
                .then((state) => patch(id, (cur) => ({results: {...(state ?? {}), ...cur.results}, loaded: true})))
                .catch((e) => {
                    patch(id, () => ({loaded: true}))
                    toast.error(errorText(e))
                })
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [idKey, patch])

    useEffect(() => {
        const offStart = onEvent('render:started', (p) => {
            patch(p.workspaceId, (cur) => ({running: {...cur.running, [p.fileName]: true}, known: true}))
        })
        const offEnd = onEvent('render:finished', (p) => {
            const file = p.result.fileName
            patch(p.workspaceId, (cur) => {
                const running = {...cur.running}
                delete running[file]
                // Un cancelado no reemplaza el último resultado bueno: lo
                // superó otro render, no dice nada nuevo sobre ese checkpoint.
                return {
                    running,
                    known: true,
                    results: p.result.status === 'cancelled' ? cur.results : {...cur.results, [file]: p.result},
                }
            })
        })
        return () => {
            offStart()
            offEnd()
        }
    }, [patch])

    const renderFile = useCallback(
        async (file: string) => {
            if (activeId == null) return
            try {
                await RenderCheckpoint(activeId, file)
            } catch (e) {
                toast.error(errorText(e))
            }
        },
        [activeId],
    )

    // Olvida el resultado de un checkpoint cuyo contenido cambió en el disco (S16): el que había es de
    // la versión anterior y mostrarlo —punto verde, malla vieja— sería mentir. Al elegirlo se vuelve a
    // renderizar (o queda «sin render» si el auto-render está apagado).
    const forget = useCallback((id: number, file: string) => {
        setByWs((prev) => {
            const cur = prev[id]
            if (!cur || !(file in cur.results)) return prev
            const results = {...cur.results}
            delete results[file]
            return {...prev, [id]: {...cur, results}}
        })
    }, [])

    const cancel = useCallback(() => {
        if (activeId != null) CancelRender(activeId).catch((e) => toast.error(errorText(e)))
    }, [activeId])

    const cur = activeId != null ? byWs[activeId] : undefined
    return {
        results: cur?.results ?? EMPTY_RESULTS,
        running: cur?.running ?? EMPTY_RUNNING,
        loaded: !!cur?.loaded,
        render: renderFile,
        forget,
        cancel,
        byWs,
    }
}
