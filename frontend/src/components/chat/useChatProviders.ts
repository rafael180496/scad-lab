import {useCallback, useEffect, useRef, useState} from 'react'
import {ListModelServers, ListServerModels} from '../../../wailsjs/go/main/App'
import {main} from '../../../wailsjs/go/models'
import type {store} from '../../../wailsjs/go/models'
import {errorText} from '../../lib/errors'
import {withVault} from '../../lib/vault'

// Los servidores con los que el chat puede hablar (S12): los de «Configuración →
// Proveedores de IA» que están encendidos. Los CLIs agénticos (claude, opencode…)
// van por la terminal y no pasan por acá.
export const isChatServer = (s: store.ModelServer) => s.enabled

// localStorage solo para conveniencias del visor (último servidor y modelo):
// puede lanzar o venir vacío (ventana privada, datos bloqueados) y la pantalla
// tiene que funcionar igual. La memoria «de verdad» por proyecto y global está en
// la base (GetLastModel / SetLastModel); esto es el respaldo si todavía no hay.
function read(key: string): string {
    try {
        return localStorage.getItem(key) ?? ''
    } catch {
        return ''
    }
}
function write(key: string, value: string) {
    try {
        localStorage.setItem(key, value)
    } catch {
        // sin persistencia: se elige de nuevo la próxima vez
    }
}

export const rememberedProvider = () => read('scadlab.chat.provider')
export const rememberProvider = (id: string) => write('scadlab.chat.provider', id)
export const rememberedModel = (providerId: string) => read(`scadlab.chat.model.${providerId}`)
export const rememberModel = (providerId: string, model: string) => write(`scadlab.chat.model.${providerId}`, model)

// «Agente» (las herramientas) y el tope de pasos se recuerdan entre sesiones.
export const rememberedAgent = () => read('scadlab.chat.agent') !== 'off'
export const rememberAgent = (on: boolean) => write('scadlab.chat.agent', on ? 'on' : 'off')
export function rememberedSteps(): number {
    const n = Number(read('scadlab.chat.maxSteps'))
    return Number.isInteger(n) && n >= 1 && n <= 30 ? n : 12
}
export const rememberSteps = (n: number) => write('scadlab.chat.maxSteps', String(n))

/** Servidores de modelos habilitados. `refresh` relee (con tope: es una consulta a la base local). */
export function useChatProviders() {
    const [providers, setProviders] = useState<store.ModelServer[]>([])
    const [loaded, setLoaded] = useState(false)
    const last = useRef(0)

    const refresh = useCallback((force = false) => {
        const now = Date.now()
        if (!force && now - last.current < 2000) return
        last.current = now
        ListModelServers()
            .then((all) => setProviders((all ?? []).filter(isChatServer)))
            .catch(() => setProviders([]))
            .finally(() => setLoaded(true))
    }, [])

    useEffect(() => {
        refresh(true)
        // Volver a la ventana tras editar la configuración en otro lado.
        const onFocus = () => refresh()
        window.addEventListener('focus', onFocus)
        return () => window.removeEventListener('focus', onFocus)
    }, [refresh])

    return {providers, loaded, refresh}
}

/**
 * Modelos de un servidor con lo que el proveedor sabe de cada uno (visión,
 * herramientas, ventana, precio). El backend guarda la lista 5 minutos; `reload`
 * pide una consulta nueva (el botón de refrescar).
 */
export function useProviderModels(providerId: string, fallback: string) {
    const [models, setModels] = useState<main.ModelInfo[]>([])
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState('')
    const [nonce, setNonce] = useState(0)
    // El primer pedido de un servidor usa la caché del backend; el que sale del
    // botón de refrescar la salta.
    const force = useRef(false)

    useEffect(() => {
        if (!providerId) return
        let stale = false
        setLoading(true)
        setError('')
        const refresh = force.current
        force.current = false
        withVault(() => ListServerModels(providerId, refresh))
            .then((list) => !stale && setModels(list ?? []))
            .catch((e) => {
                if (stale) return
                setModels([])
                setError(errorText(e))
            })
            .finally(() => !stale && setLoading(false))
        return () => {
            stale = true
        }
    }, [providerId, nonce])

    // El modelo por defecto del servidor se ofrece siempre, aunque el servidor no
    // lo liste (un proxy, un modelo recién bajado).
    const options = fallback && !models.some((m) => m.id === fallback) ? [main.ModelInfo.createFrom({id: fallback}), ...models] : models
    return {
        models: options,
        loading,
        error,
        reload: () => {
            force.current = true
            setNonce((n) => n + 1)
        },
    }
}
