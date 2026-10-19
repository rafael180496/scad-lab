import {useCallback, useEffect, useState, useSyncExternalStore} from 'react'
import {ProviderHealth} from '../../wailsjs/go/main/App'
import {errorText} from './errors'

// Salud de los proveedores del chat: ¿responde ahora mismo? Portado de
// mini-tools (lib/modelServerHealth.ts): un almacén compartido por proveedor, de
// modo que el punto del selector, la píldora de latencia y el aviso sobre la caja
// del chat lean UN sondeo en vez de hacer cada uno el suyo.
//
// Cuándo se consulta: al elegir el proveedor y cada 60 s mientras el chat está a
// la vista; al volver a la ventana si la última lectura ya tiene más de 60 s. Con
// la ventana oculta o sin foco no se sondea: un servidor remoto caído no tiene por
// qué recibir una consulta por minuto de una app que nadie mira. El backend cachea
// 30 s cada respuesta buena (ProviderHealth), así que consultar de más no cuesta
// un viaje a la red. De un CLI, ProviderHealth dice solo «instalado» (latencia 0).

export const HEALTH_POLL_MS = 60_000

export type HealthStatus = 'unknown' | 'checking' | 'ok' | 'error'

export interface Health {
    status: HealthStatus
    latencyMs: number
    /** Epoch en ms de la última lectura terminada; 0 si no hubo. */
    checkedAt: number
    error: string
}

const UNKNOWN: Health = {status: 'unknown', latencyMs: 0, checkedAt: 0, error: ''}

interface Entry {
    health: Health
    subs: Set<() => void>
    pollers: number
    timer: number | null
    inflight: Promise<void> | null
}

const entries = new Map<string, Entry>()

function entryOf(id: string): Entry {
    let e = entries.get(id)
    if (!e) {
        e = {health: UNKNOWN, subs: new Set(), pollers: 0, timer: null, inflight: null}
        entries.set(id, e)
    }
    return e
}

function publish(id: string, health: Health) {
    const e = entryOf(id)
    e.health = health
    e.subs.forEach((fn) => fn())
}

// Consulta. `force` salta el «ya hay una en vuelo» y muestra «comprobando» aunque
// haya una lectura previa (un reintento pedido a mano tiene que verse).
function check(id: string, force: boolean): Promise<void> {
    const e = entryOf(id)
    if (e.inflight && !force) return e.inflight
    // Con una lectura previa se la deja a la vista mientras llega la nueva: un
    // punto que parpadea en gris cada minuto se lee como inestabilidad.
    if (e.health.status === 'unknown' || force) publish(id, {...e.health, status: 'checking'})
    let run: Promise<void> | undefined
    run = (async () => {
        try {
            const h = await ProviderHealth(id)
            publish(id, {status: h.ok ? 'ok' : 'error', latencyMs: h.latencyMs ?? 0, checkedAt: Date.now(), error: h.ok ? '' : h.error})
        } catch (err) {
            publish(id, {status: 'error', latencyMs: 0, checkedAt: Date.now(), error: errorText(err)})
        } finally {
            const cur = entryOf(id)
            if (cur.inflight === run) cur.inflight = null
        }
    })()
    e.inflight = run
    return run
}

const attention = () => document.visibilityState === 'visible' && document.hasFocus()

function catchUp() {
    if (!attention()) return
    const now = Date.now()
    entries.forEach((e, id) => {
        if (e.pollers > 0 && now - e.health.checkedAt >= HEALTH_POLL_MS) void check(id, false)
    })
}

let listening = false
function ensureListeners() {
    if (listening) return
    listening = true
    document.addEventListener('visibilitychange', catchUp)
    window.addEventListener('focus', catchUp)
}

function startPolling(id: string): () => void {
    const e = entryOf(id)
    e.pollers++
    ensureListeners()
    if (e.pollers === 1) e.timer = window.setInterval(() => attention() && void check(id, false), HEALTH_POLL_MS)
    // La lectura de arranque no espera al primer tic: al elegir el proveedor el
    // punto tiene que decir algo ya.
    void check(id, false)
    return () => {
        e.pollers--
        if (e.pollers <= 0 && e.timer !== null) {
            window.clearInterval(e.timer)
            e.timer = null
        }
    }
}

function subscribeTo(id: string, fn: () => void): () => void {
    const e = entryOf(id)
    e.subs.add(fn)
    return () => {
        e.subs.delete(fn)
    }
}

/**
 * Salud del proveedor `id`. Con `poll` (el chat está a la vista) consulta al
 * montar o al cambiar de proveedor y cada 60 s; sin él, solo lee lo que ya hay.
 * `refresh` vuelve a preguntar ya (el clic en la píldora o «Reintentar»).
 */
export function useProviderHealth(id: string, poll: boolean): {health: Health; refresh: () => void} {
    const health = useSyncExternalStore(
        (fn) => (id ? subscribeTo(id, fn) : () => {}),
        () => (id ? entryOf(id).health : UNKNOWN),
    )
    useEffect(() => {
        if (!id || !poll) return
        return startPolling(id)
    }, [id, poll])
    const refresh = useCallback(() => {
        if (id) void check(id, true)
    }, [id])
    return {health, refresh}
}

/**
 * Una lectura de cada proveedor de `ids` si no la tiene o ya envejeció, SIN
 * sondeo periódico: alimenta los puntos de las filas de un selector abierto (el
 * proveedor elegido se sondea aparte con useProviderHealth).
 */
export function useProvidersHealth(ids: string[], enabled: boolean): Record<string, Health> {
    const key = ids.join('|')
    const [, bump] = useState(0)
    useEffect(() => {
        if (!enabled || !key) return
        const list = key.split('|')
        const offs = list.map((id) => subscribeTo(id, () => bump((n) => n + 1)))
        const now = Date.now()
        for (const id of list) {
            const h = entryOf(id).health
            if (h.status === 'unknown' || now - h.checkedAt >= HEALTH_POLL_MS) void check(id, false)
        }
        return () => offs.forEach((off) => off())
    }, [enabled, key])
    const out: Record<string, Health> = {}
    for (const id of ids) out[id] = entryOf(id).health
    return out
}
