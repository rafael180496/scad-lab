import {useCallback, useEffect, useRef, useState} from 'react'
import {ListProviderModels, ListProviders} from '../../../wailsjs/go/main/App'
import type {store} from '../../../wailsjs/go/models'
import {errorText} from '../../lib/errors'

// Los proveedores con los que el chat puede hablar: servidores de modelos
// habilitados. Los CLIs agénticos (claude, opencode…) van por la terminal.
export const CHAT_KINDS = new Set(['ollama', 'openai', 'anthropic'])

export const isChatProvider = (p: store.AIProvider) => p.enabled && CHAT_KINDS.has(p.kind)

// localStorage solo para conveniencias del visor (último proveedor y modelo):
// puede lanzar o venir vacío (ventana privada, datos bloqueados) y la pantalla
// tiene que funcionar igual.
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

/** Proveedores de chat habilitados. `refresh` relee (con tope: es una consulta a la base local). */
export function useChatProviders() {
    const [providers, setProviders] = useState<store.AIProvider[]>([])
    const [loaded, setLoaded] = useState(false)
    const last = useRef(0)

    const refresh = useCallback((force = false) => {
        const now = Date.now()
        if (!force && now - last.current < 2000) return
        last.current = now
        ListProviders()
            .then((all) => setProviders((all ?? []).filter(isChatProvider)))
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

/** Modelos de un proveedor (red para Ollama/OpenAI, lista fija para Anthropic). */
export function useProviderModels(providerId: string, fallback: string) {
    const [models, setModels] = useState<string[]>([])
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState('')
    const [nonce, setNonce] = useState(0)

    useEffect(() => {
        if (!providerId) return
        let stale = false
        setLoading(true)
        setError('')
        ListProviderModels(providerId)
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

    // El modelo configurado en Configuración → IA se ofrece siempre, aunque el
    // servidor no lo liste (un proxy, un modelo recién bajado).
    const options = fallback && !models.includes(fallback) ? [fallback, ...models] : models
    return {models: options, loading, error, reload: () => setNonce((n) => n + 1)}
}
