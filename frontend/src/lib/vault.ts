// Puente entre los bindings que descifran y el diálogo de la contraseña maestra
// (S12, 12.10). Mismo patrón que confirmDialog y lib/sessionBridge.ts: store de
// módulo sin contexto, así que se puede llamar desde cualquier lado.
//
// Los bindings que necesitan una clave de API responden con un error de código
// estable cuando no se puede (ver backend/vaultgate):
//
//   vault-uninitialized   todavía no hay contraseña maestra → «Crear»
//   vault-locked          hay, y la llave no se recuperó sola → «Desbloquear»
//
// withVault envuelve la llamada: si falla con uno de los dos, abre el diálogo
// que corresponde y, si el usuario completa, REINTENTA UNA SOLA VEZ. Nunca en
// bucle: si el reintento falla, el error sube tal cual.
import {errorCode} from './errors'
import {isMac} from './platform'

export const CODE_LOCKED = 'vault-locked'
export const CODE_UNINITIALIZED = 'vault-uninitialized'
export const CODE_WRONG_PASSWORD = 'vault-wrong-password'

export type VaultNeed = 'create' | 'unlock'

export function vaultNeed(e: unknown): VaultNeed | null {
    const c = errorCode(e)
    return c === CODE_UNINITIALIZED ? 'create' : c === CODE_LOCKED ? 'unlock' : null
}

interface Pending {
    mode: VaultNeed
    promise: Promise<boolean>
    resolve: (ok: boolean) => void
}

let pending: Pending | null = null
const hostListeners = new Set<() => void>()
const changeListeners = new Set<() => void>()

export function subscribeVaultRequest(cb: () => void) {
    hostListeners.add(cb)
    return () => {
        hostListeners.delete(cb)
    }
}

export function currentVaultRequest(): VaultNeed | null {
    return pending?.mode ?? null
}

/** Abre el diálogo y resuelve true si se creó o desbloqueó, false si se canceló.
 * Dos pedidos a la vez comparten el mismo diálogo. */
export function requestVault(mode: VaultNeed): Promise<boolean> {
    if (pending) return pending.promise
    let resolve!: (ok: boolean) => void
    const promise = new Promise<boolean>((r) => (resolve = r))
    pending = {mode, promise, resolve}
    hostListeners.forEach((l) => l())
    return promise
}

/** Lo llama el diálogo al terminar. */
export function settleVaultRequest(ok: boolean) {
    const p = pending
    pending = null
    hostListeners.forEach((l) => l())
    p?.resolve(ok)
}

export async function withVault<T>(run: () => Promise<T>): Promise<T> {
    try {
        return await run()
    } catch (e) {
        const need = vaultNeed(e)
        if (!need) throw e
        const ok = await requestVault(need)
        if (!ok) throw e
        return run()
    }
}

/** El estado del llavero cambió (se creó, desbloqueó, cambió, olvidó, restableció). */
export function notifyVaultChanged() {
    changeListeners.forEach((l) => l())
}

export function onVaultChanged(cb: () => void) {
    changeListeners.add(cb)
    return () => {
        changeListeners.delete(cb)
    }
}

/** Cómo se llama el llavero del sistema en este equipo, para los textos. */
export function keychainKind(): 'mac' | 'win' | 'other' {
    if (isMac()) return 'mac'
    return typeof navigator !== 'undefined' && /win/i.test(navigator.platform) ? 'win' : 'other'
}
