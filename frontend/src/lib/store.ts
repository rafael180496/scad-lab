import {useSyncExternalStore} from 'react'

// Store mínimo con suscripción, para estado que cambia seguido (el cursor del
// editor) y que solo leen unos pocos componentes: ponerlo en el estado de App
// redibujaría la ventana entera —visor 3D incluido— con cada tecla.
export function createStore<T>(initial: T) {
    let state = initial
    const subs = new Set<() => void>()
    const subscribe = (cb: () => void) => {
        subs.add(cb)
        return () => subs.delete(cb)
    }
    return {
        get: () => state,
        set(patch: Partial<T>) {
            let changed = false
            for (const k in patch) {
                if (!Object.is(state[k], patch[k])) changed = true
            }
            if (!changed) return
            state = {...state, ...patch}
            subs.forEach((s) => s())
        },
        use<S>(select: (s: T) => S): S {
            return useSyncExternalStore(subscribe, () => select(state))
        },
    }
}
