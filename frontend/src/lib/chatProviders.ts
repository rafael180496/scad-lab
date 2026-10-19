// Proveedores del chat (S12.b): un CLI agéntico detectado (`cli:<id>`) o un
// servidor de modelos de Configuración → Proveedores de IA. Lo que los distingue
// importa en tres sitios: el selector (los CLIs primero), el modo («Solo
// consulta» / «Agente» lo gobiernan los dos igual, pero con explicaciones
// distintas) y el modelo («Por defecto» es válido en un CLI; en un servidor lo
// resuelve el backend con su modelo predeterminado).
import type {main} from '../../wailsjs/go/models'

export const CLI_PREFIX = 'cli:'

export const isCLI = (id: string) => id.startsWith(CLI_PREFIX)
export const cliIdOf = (id: string) => (isCLI(id) ? id.slice(CLI_PREFIX.length) : '')

/** Un proveedor del selector más lo que solo dice el servidor guardado (ListModelServers): su modelo predeterminado y su dirección (para el ícono). */
export type ChatProviderItem = main.ChatProvider & {defaultModel: string; baseUrl: string}

/** Los CLIs primero (como mini-tools) y, dentro de cada grupo, el orden que dio el backend. */
export function sortProviders<T extends {id: string}>(list: T[]): T[] {
    const cli = list.filter((p) => isCLI(p.id))
    return [...cli, ...list.filter((p) => !isCLI(p.id))]
}

/** Modos: «Solo consulta» es agent=false y «Agente» agent=true (SendOptions.agent). */
export type ChatMode = 'ask' | 'agent'
export const modeOf = (agent: boolean): ChatMode => (agent ? 'agent' : 'ask')
