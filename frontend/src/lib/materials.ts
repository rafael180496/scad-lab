import type {store} from '../../wailsjs/go/models'
import {ListMaterials} from '../../wailsjs/go/main/App'
import {createStore} from './store'

// Filamentos configurados (tabla `materials`). Los usan los selectores de
// material por parte del visor y del panel «Impresión». Configuración los
// edita (densidad y costo/kg) y avisa con reloadMaterials(): `rev` sube y la
// estimación se vuelve a pedir con los números nuevos.
export const materialsStore = createStore({list: [] as store.Material[], rev: 0, loaded: false})

let pending: Promise<void> | null = null

export function ensureMaterials(): Promise<void> {
    if (materialsStore.get().loaded) return Promise.resolve()
    pending ??= ListMaterials()
        .then((list) => materialsStore.set({list: list ?? [], loaded: true}))
        .catch(() => {})
        .finally(() => {
            pending = null
        })
    return pending
}

export async function reloadMaterials() {
    try {
        const list = await ListMaterials()
        materialsStore.set({list: list ?? [], loaded: true, rev: materialsStore.get().rev + 1})
    } catch {
        // Se queda con lo que había.
    }
}

