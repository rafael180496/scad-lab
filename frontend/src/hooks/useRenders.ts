import {useCallback, useEffect, useRef, useState} from 'react'
import type {render} from '../../wailsjs/go/models'
import {CancelRender, GetRenderState, RenderCheckpoint} from '../../wailsjs/go/main/App'
import {toast} from '../components/ui/Toast'
import {errorText} from '../lib/errors'
import {onEvent} from '../lib/events'

export type RenderResults = Record<string, render.Result>
export type RenderRunning = Record<string, boolean>

// useRenders lleva el estado de render de los checkpoints del workspace
// abierto. La fuente es el backend: los eventos render:started/finished
// llegan también por los renders que dispara el watcher (un agente que creó
// un .scad), no solo por los que pide la UI.
export function useRenders(workspaceId: number | null) {
    const [results, setResults] = useState<RenderResults>({})
    const [running, setRunning] = useState<RenderRunning>({})
    // loaded = ya llegó GetRenderState: antes de eso «sin resultado» no
    // significa nada, y el auto-render volvería a renderizar lo que ya estaba.
    // Se guarda el id para el que se cargó y no un booleano: en el render en
    // que cambia el workspace, un `true` del anterior todavía estaría vigente.
    const [loadedId, setLoadedId] = useState<number | null>(null)
    const current = useRef(workspaceId)
    current.current = workspaceId

    useEffect(() => {
        setResults({})
        setRunning({})
        setLoadedId(null)
        if (workspaceId == null) return
        let alive = true
        GetRenderState(workspaceId)
            .then((state) => {
                if (!alive) return
                // Lo que ya llegó por eventos es más nuevo que la foto inicial.
                setResults((prev) => ({...(state ?? {}), ...prev}))
                setLoadedId(workspaceId)
            })
            .catch((e) => {
                if (alive) {
                    setLoadedId(workspaceId)
                    toast.error(errorText(e))
                }
            })
        return () => {
            alive = false
        }
    }, [workspaceId])

    useEffect(() => {
        const offStart = onEvent('render:started', (p) => {
            if (p.workspaceId !== current.current) return
            setRunning((prev) => ({...prev, [p.fileName]: true}))
        })
        const offEnd = onEvent('render:finished', (p) => {
            if (p.workspaceId !== current.current) return
            const file = p.result.fileName
            setRunning((prev) => {
                const next = {...prev}
                delete next[file]
                return next
            })
            // Un cancelado no reemplaza el último resultado bueno: lo
            // superó otro render, no dice nada nuevo sobre ese checkpoint.
            if (p.result.status !== 'cancelled') setResults((prev) => ({...prev, [file]: p.result}))
        })
        return () => {
            offStart()
            offEnd()
        }
    }, [])

    const renderFile = useCallback(
        async (file: string) => {
            if (workspaceId == null) return
            try {
                await RenderCheckpoint(workspaceId, file)
            } catch (e) {
                toast.error(errorText(e))
            }
        },
        [workspaceId],
    )

    const cancel = useCallback(() => {
        if (workspaceId != null) CancelRender(workspaceId).catch((e) => toast.error(errorText(e)))
    }, [workspaceId])

    return {results, running, loaded: workspaceId != null && loadedId === workspaceId, render: renderFile, cancel}
}
