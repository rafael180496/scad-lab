import {ListCheckpointHistory, RestoreCheckpointVersion} from '../../../wailsjs/go/main/App'
import {absoluteTime, undoVersion, versionDate, type HistoryVersion} from '../../lib/history'
import {errorText} from '../../lib/errors'
import {fmt, locale, type Dict} from '../../i18n'
import {confirmDialog} from '../ui/Dialog'
import {toast} from '../ui/Toast'

// Acciones del historial de un checkpoint (S16) que piden confirmación y avisan: las comparten el
// menú del nodo del árbol, las cabeceras del editor y del visor, y el diálogo del historial. El
// aviso de que el archivo cambió (checkpoint:updated) lo emite Go; releer y volver a renderizar lo
// hace App.

/** Fecha de una versión para un texto de confirmación o de aviso. */
export function versionWhen(v: Pick<HistoryVersion, 'savedAt'>): string {
    const d = versionDate(v)
    return d ? absoluteTime(d, locale()) : ''
}

/** «Deshacer cambio de la IA»: restaura la versión más nueva si la guardó la IA. true = se deshizo. */
export async function undoAIChange(workspaceId: number, file: string, t: Dict): Promise<boolean> {
    let versions: HistoryVersion[]
    try {
        versions = (await ListCheckpointHistory(workspaceId, file)).versions ?? []
    } catch (e) {
        toast.error(errorText(e))
        return false
    }
    const v = undoVersion(versions)
    if (!v) {
        toast.error(t.history.undoNone)
        return false
    }
    if (!(await confirmDialog(fmt(t.history.undoConfirm, {file, date: versionWhen(v)}), t.history.undoOk, t.history.cancel))) return false
    try {
        await RestoreCheckpointVersion(workspaceId, file, v.id)
        toast.ok(fmt(t.history.undone, {file}))
        return true
    } catch (e) {
        toast.error(errorText(e))
        return false
    }
}

/** «Restaurar esta versión» desde el historial o desde la comparación. true = se restauró. */
export async function restoreVersion(workspaceId: number, file: string, v: HistoryVersion, t: Dict): Promise<boolean> {
    const date = versionWhen(v)
    if (!(await confirmDialog(fmt(t.history.restoreConfirm, {file, date}), t.history.restoreOk, t.history.cancel))) return false
    try {
        await RestoreCheckpointVersion(workspaceId, file, v.id)
        toast.ok(fmt(t.history.restored, {file, date}))
        return true
    } catch (e) {
        toast.error(errorText(e))
        return false
    }
}
