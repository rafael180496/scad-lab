import {createStore} from './store'

// Qué diálogo del historial está abierto (S16). Lo abren el menú del nodo del árbol y los
// botones de las cabeceras del editor y del visor; lo dibuja `HistoryHost`, montado una vez
// en App. Un store de módulo y no estado de App: ni el menú ni la cabecera tienen que saber
// dónde vive el diálogo.
export type HistoryDialogState =
    | {kind: null}
    /** Lista de versiones del archivo. */
    | {kind: 'history'; workspaceId: number; file: string}
    /** Comparación de una versión (la más nueva si no se dice cuál) con el archivo actual. */
    | {kind: 'diff'; workspaceId: number; file: string; versionId?: string}

export const historyUI = createStore<{dialog: HistoryDialogState}>({dialog: {kind: null}})

export const openHistory = (workspaceId: number, file: string) => historyUI.set({dialog: {kind: 'history', workspaceId, file}})
export const openDiff = (workspaceId: number, file: string, versionId?: string) =>
    historyUI.set({dialog: {kind: 'diff', workspaceId, file, versionId}})
export const closeHistory = () => historyUI.set({dialog: {kind: null}})
