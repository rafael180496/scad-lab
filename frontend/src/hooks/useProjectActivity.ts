import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {onEvent} from '../lib/events'
import {projectActivity, type ProjectActivityItem} from '../lib/projectsApi'
import type {WsRender} from './useRenders'

/** Lo que muestran los indicadores de fondo de una pestaña de proyecto. */
export type TabActivity = {
    /** Hay un render en curso. */
    rendering: boolean
    /** El último render que terminó falló. */
    failed: boolean
    /** Agentes vivos del proyecto. */
    agents: number
}

// Cada cuánto se vuelve a pedir la foto del backend mientras hay más de un
// proyecto abierto: es solo una red de seguridad (los eventos ya la mantienen al
// día), para que una pestaña de fondo no quede con un indicador viejo.
const POLL_MS = 15_000

// useProjectActivity junta tres fuentes para las pestañas:
//   · los eventos render:* de cada proyecto (useRenders) — lo más fresco;
//   · las terminales que lleva useTerminals (cuenta de agentes vivos);
//   · la foto del backend (ProjectActivity), que cubre lo que todavía no
//     mandó eventos, p. ej. un render que ya estaba en curso al abrir la app.
// Si el backend no trae ProjectActivity, vale solo lo local.
export function useProjectActivity(
    ids: number[],
    renders: Record<number, WsRender>,
    agentsByWs: Record<number, number> | null,
    /** Checkpoint más nuevo de cada proyecto: su render es el que decide si la pestaña va en rojo. */
    latestByWs: Record<number, string>,
): Record<number, TabActivity> {
    const [snap, setSnap] = useState<Record<number, ProjectActivityItem>>({})
    const idKey = ids.join(',')
    const seq = useRef(0)

    const refresh = useCallback(() => {
        const mine = ++seq.current
        projectActivity()
            .then((list) => {
                if (mine !== seq.current || !list) return
                setSnap(Object.fromEntries(list.map((a) => [a.id, a])))
            })
            .catch(() => {})
    }, [])

    useEffect(() => {
        if (ids.length) refresh()
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [idKey, refresh])

    useEffect(() => {
        if (ids.length < 2) return
        const timer = window.setInterval(refresh, POLL_MS)
        const onFocus = () => refresh()
        window.addEventListener('focus', onFocus)
        return () => {
            window.clearInterval(timer)
            window.removeEventListener('focus', onFocus)
        }
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [ids.length, refresh])

    useEffect(() => onEvent('projects:changed', () => refresh()), [refresh])

    return useMemo(() => {
        const out: Record<number, TabActivity> = {}
        for (const id of ids) {
            const local = renders[id]
            const server = snap[id]
            const known = !!local?.known
            const rendering = known ? Object.keys(local.running).length > 0 : !!server?.rendering
            // Igual que el backend: falló el último render del checkpoint más nuevo. Con los
            // resultados ya leídos manda lo local (se actualiza con cada evento).
            const latest = latestByWs[id]
            const failed = local?.loaded ? !!latest && local.results[latest]?.status === 'error' : !!server?.renderFailed
            out[id] = {
                rendering,
                failed,
                agents: agentsByWs ? (agentsByWs[id] ?? 0) : (server?.agentsAlive ?? 0),
            }
        }
        return out
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [idKey, renders, snap, agentsByWs, latestByWs])
}
