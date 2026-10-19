import type {Tf, Vec3} from '../../lib/meshEditMath'
import {createStore} from '../../lib/store'

// Lo que el gizmo va dejando MIENTRAS se arrastra, antes de que sea una operación de la pila: el panel de
// propiedades y las medidas del visor lo muestran en vivo. Al soltar, la operación se confirma
// (`commit` de lib/meshEditSession) y esto se limpia. Es un store aparte porque cambia ~60 veces por segundo
// y solo lo leen unos pocos componentes.
export type Live = {
    tf: Tf | null
    cutPoint: Vec3 | null
    cutNormal: Vec3 | null
    /** La caja en el mundo del objeto que se arrastra (o null si no se calcula). */
    box: {min: Vec3; max: Vec3} | null
}

const EMPTY: Live = {tf: null, cutPoint: null, cutNormal: null, box: null}

export const liveStore = createStore<Live>(EMPTY)
export const clearLive = () => liveStore.set(EMPTY)
