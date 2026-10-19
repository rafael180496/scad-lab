// Utilidades de los servidores de modelos (S12). Portado de mini-tools
// (lib/modelServers.ts); aquí no hay catálogo de agentes `llm:<id>`: el chat
// habla directo con el servidor por su id.
import {cloudPresetForUrl, type Preset, type ServerKind} from './modelServerPresets'

// Tipo de API de un servidor. El backend guarda el texto; cualquier valor que no
// sea «openai» ni «anthropic» se trata como Ollama.
export function serverKind(kind: string): ServerKind {
    return kind === 'openai' ? 'openai' : kind === 'anthropic' ? 'anthropic' : 'ollama'
}

// Dirección que nunca sale de esta máquina: lo que se le manda a un servidor así
// no viaja por ninguna red.
export function isLoopbackUrl(url: string): boolean {
    try {
        const host = new URL(url.trim()).hostname.replace(/^\[|\]$/g, '').toLowerCase()
        return host === 'localhost' || host === '127.0.0.1' || host === '::1' || host === '0.0.0.0' || host.endsWith('.localhost')
    } catch {
        return false
    }
}

// ¿El fallo de una prueba es de los que explica un Ollama que no escucha en la
// red? Conexión rechazada, tiempo agotado o red/host inalcanzable. Es una
// heurística sobre el texto (el backend devuelve el error ya traducido y sin un
// código aparte): reconoce las frases de `llm.ConnError` en los dos idiomas.
const UNREACHABLE_RE =
    /rechazad|refused|agot[oó] el tiempo|timed out|timeout|no route to host|network is unreachable|host is unreachable|inalcanzable/i

export function looksUnreachable(error: string): boolean {
    return UNREACHABLE_RE.test(error)
}

// Dónde corre un servidor, para agrupar la lista: «local» (esta máquina),
// «remote» (un servidor propio en otra máquina) o «cloud» (una API alojada que se
// usa con clave: OpenRouter, OpenAI, Anthropic…). Anthropic es siempre nube; el
// resto se reconoce por el host de su dirección, no por el tipo: un servidor
// «compatible con OpenAI» puede ser LM Studio en esta máquina o api.openai.com.
export type ServerGroup = 'local' | 'remote' | 'cloud'

interface ServerLike {
    kind: string
    baseUrl: string
}

export function cloudPresetOf(s: ServerLike): Preset | null {
    return cloudPresetForUrl(s.baseUrl)
}

export function serverGroup(s: ServerLike): ServerGroup {
    if (s.kind === 'anthropic' || cloudPresetForUrl(s.baseUrl)) return 'cloud'
    return isLoopbackUrl(s.baseUrl) ? 'local' : 'remote'
}

// Formatos de la lista de modelos.
export function contextLabel(tokens: number): string {
    if (tokens <= 0) return ''
    if (tokens >= 1_000_000) return `${+(tokens / 1_000_000).toFixed(1)}M`
    return `${Math.round(tokens / 1000)}k`
}

export function priceLabel(promptPerM: number, completionPerM: number): string {
    if (promptPerM <= 0 && completionPerM <= 0) return ''
    const f = (n: number) => (n >= 10 ? n.toFixed(0) : n >= 1 ? n.toFixed(1) : n.toFixed(2))
    return `$${f(promptPerM)} / $${f(completionPerM)}`
}

// Puente para abrir Configuración → Proveedores de IA desde el chat, que vive en
// otro árbol de React (panel lazy): ChatPanel avisa acá y App abre el diálogo.
// Mismo patrón que lib/sessionBridge.ts.
const settingsListeners = new Set<() => void>()
export function onOpenProvidersSettings(cb: () => void) {
    settingsListeners.add(cb)
    return () => {
        settingsListeners.delete(cb)
    }
}
export function openProvidersSettings(): boolean {
    settingsListeners.forEach((cb) => cb())
    return settingsListeners.size > 0
}
