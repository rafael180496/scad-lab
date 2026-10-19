import {useCallback, useEffect, useRef, useState} from 'react'
import {ChatModelCatalog, ListChatProviders, ListModelServers, RefreshChatProviders} from '../../../wailsjs/go/main/App'
import {main} from '../../../wailsjs/go/models'
import {errorText} from '../../lib/errors'
import {sortProviders, type ChatProviderItem} from '../../lib/chatProviders'
import {withVault} from '../../lib/vault'

// localStorage solo para conveniencias del chat (último proveedor, modelo por
// proveedor, modo): puede lanzar o venir vacío (ventana privada, datos bloqueados)
// y la pantalla tiene que funcionar igual. La memoria «de verdad» por proyecto y
// global está en la base (GetLastModel / SetLastModel); esto es el respaldo.
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

// El modo se recuerda por TIPO de proveedor: con un CLI arranca en «Solo consulta»
// (como mini-tools: lo que se enciende a mano no se hereda de un servidor) y con un
// servidor en «Agente» (el default de S12). Cada uno guarda lo último que se eligió.
export const rememberedAgent = (cli: boolean) => (cli ? read('scadlab.chat.agent.cli') === 'on' : read('scadlab.chat.agent') !== 'off')
export const rememberAgent = (cli: boolean, on: boolean) => write(cli ? 'scadlab.chat.agent.cli' : 'scadlab.chat.agent', on ? 'on' : 'off')
export function rememberedSteps(): number {
    const n = Number(read('scadlab.chat.maxSteps'))
    return Number.isInteger(n) && n >= 1 && n <= 30 ? n : 12
}
export const rememberSteps = (n: number) => write('scadlab.chat.maxSteps', String(n))

/**
 * Los proveedores con los que el chat puede hablar: los CLIs detectados (primero) y
 * los servidores habilitados. `refresh(true)` vuelve a detectar los CLIs (al abrir el
 * selector, para que uno recién instalado aparezca sin reiniciar); sin `force` es una
 * lectura barata con tope de 2 s. A cada servidor se le suma lo que solo dice su
 * fila guardada: el modelo predeterminado y la dirección (para el ícono).
 */
export function useChatProviders() {
    const [providers, setProviders] = useState<ChatProviderItem[]>([])
    const [loaded, setLoaded] = useState(false)
    const last = useRef(0)

    const refresh = useCallback((force = false) => {
        const now = Date.now()
        if (!force && now - last.current < 2000) return
        last.current = now
        Promise.all([force ? RefreshChatProviders() : ListChatProviders(), ListModelServers().catch(() => [])])
            .then(([list, servers]) => {
                const byId = new Map((servers ?? []).map((s) => [s.id, s]))
                setProviders(sortProviders((list ?? []).map((p) => Object.assign(main.ChatProvider.createFrom(p), {defaultModel: byId.get(p.id)?.defaultModel ?? '', baseUrl: byId.get(p.id)?.baseUrl ?? ''}))))
            })
            .catch(() => setProviders([]))
            .finally(() => setLoaded(true))
    }, [])

    useEffect(() => {
        refresh(true)
        // Volver a la ventana tras editar la configuración o instalar un CLI en otro lado.
        const onFocus = () => refresh()
        window.addEventListener('focus', onFocus)
        return () => window.removeEventListener('focus', onFocus)
    }, [refresh])

    return {providers, loaded, refresh}
}

/**
 * Modelos de un proveedor con lo que se sabe de cada uno (visión, herramientas,
 * ventana, precio) y la explicación de «Por defecto». El backend guarda la lista 5
 * minutos (10 la de un CLI); `reload` pide una consulta nueva (el botón de
 * refrescar).
 */
export function useProviderModels(providerId: string, fallback: string) {
    const [catalog, setCatalog] = useState<{models: main.ModelInfo[]; defaultHint: string}>({models: [], defaultHint: ''})
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState('')
    const [nonce, setNonce] = useState(0)
    // El primer pedido de un proveedor usa la caché del backend; el que sale del
    // botón de refrescar la salta.
    const force = useRef(false)

    useEffect(() => {
        if (!providerId) return
        let stale = false
        setLoading(true)
        setError('')
        const refresh = force.current
        force.current = false
        withVault(() => ChatModelCatalog(providerId, refresh))
            .then((c) => !stale && setCatalog({models: c?.models ?? [], defaultHint: c?.defaultHint ?? ''}))
            .catch((e) => {
                if (stale) return
                setCatalog({models: [], defaultHint: ''})
                setError(errorText(e))
            })
            .finally(() => !stale && setLoading(false))
        return () => {
            stale = true
        }
    }, [providerId, nonce])

    // El modelo por defecto del servidor se ofrece siempre, aunque el servidor no
    // lo liste (un proxy, un modelo recién bajado).
    const models = fallback && !catalog.models.some((m) => m.id === fallback) ? [main.ModelInfo.createFrom({id: fallback}), ...catalog.models] : catalog.models
    return {
        models,
        defaultHint: catalog.defaultHint,
        loading,
        error,
        reload: () => {
            force.current = true
            setNonce((n) => n + 1)
        },
    }
}
