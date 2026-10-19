// El modo del chat (S12.c, T12): «Agente» (puede crear y cambiar modelos) o «Solo
// consulta». Vive en la CONVERSACIÓN —columna `chats.mode`, binding `SetChatMode`—,
// no en una preferencia del navegador: elegir «Solo consulta» en una conversación no
// cambia el modo de ninguna otra ni el de las conversaciones que se abran después.
// Toda conversación nueva nace en «Agente» (el chiste de ScadLab es crear y modificar
// modelos) y solo lo que la persona eligió a mano para ESA conversación queda en
// consulta.

/** El modo que se manda a `SetChatMode` y que viaja en `Chat.mode`: '' es «Agente» (el predeterminado). */
export type ChatModeValue = 'agent' | 'ask'

/** ¿Está la conversación en «Agente»? Todo lo que no sea «ask» (vacío, filas anteriores a la migración) lo es. */
export const isAgentMode = (mode: string | null | undefined): boolean => mode !== 'ask'

/** El valor de `SetChatMode` para el interruptor «Agente». */
export const chatModeValue = (agent: boolean): ChatModeValue => (agent ? 'agent' : 'ask')

// Las claves de localStorage de las versiones anteriores, que guardaban el modo
// por tipo de proveedor (todas las conversaciones de un tipo compartían la elección):
//   · S12 / S12.b: `scadlab.chat.agent` (servidores) y `scadlab.chat.agent.cli`, 'on' | 'off';
//   · S12.c (T9):  `scadlab.chat.mode.cli` y `scadlab.chat.mode.server`, 'ask' | 'agent'.
// Ya no se leen: una elección vieja no puede dejar en consulta una conversación que
// nadie eligió en consulta.
export const LEGACY_MODE_KEYS = ['scadlab.chat.agent', 'scadlab.chat.agent.cli', 'scadlab.chat.mode.cli', 'scadlab.chat.mode.server'] as const

type Removable = Pick<Storage, 'removeItem'>

/**
 * Borra las preferencias de modo de las versiones anteriores. Se llama una vez al
 * arrancar (`main.tsx`) y es idempotente. `storage` es inyectable para probarla;
 * localStorage puede lanzar o no existir (ventana privada, datos bloqueados) y la
 * pantalla tiene que funcionar igual. Devuelve cuántas claves quitó.
 */
export function clearLegacyChatMode(storage?: Removable | null): number {
    let target: Removable | null | undefined = storage
    if (target === undefined) {
        try {
            target = localStorage
        } catch {
            target = null
        }
    }
    if (!target) return 0
    let n = 0
    for (const key of LEGACY_MODE_KEYS) {
        try {
            const had = (target as Partial<Storage>).getItem?.(key) != null
            target.removeItem(key)
            if (had) n++
        } catch {
            // sin acceso: no hay nada que borrar
        }
    }
    return n
}
