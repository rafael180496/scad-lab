import {OpenInSlicer, RevealPath} from '../../wailsjs/go/main/App'
import {toast, type ToastAction} from '../components/ui/Toast'
import type {Dict} from '../i18n'
import {fmt} from '../i18n'
import {errorText} from './errors'
import {isMac} from './platform'

/**
 * A qué slicer se puede abrir un archivo guardado (S14): `installed` es el primero instalado de los de la
 * impresora del workspace; `recommendedName` el recomendado, para decir cuál falta cuando ninguno está.
 * Sin ninguno de los dos no hay nada que ofrecer (la impresora no nombra slicers).
 */
export type SlicerTarget = {installed?: {id: string; name: string}; recommendedName?: string}

// Aviso tras guardar un archivo (STL, 3MF, ZIP, PNG): dice la ruta y ofrece
// mostrarlo en el Finder / Explorador y, si el archivo es de los que abre un slicer, abrirlo
// en el de la impresora. Abrir la app es siempre por un clic del usuario; sin ningún slicer
// instalado el botón se ve, apagado, y su `title` dice cuál falta y dónde indicar la ruta.
export function toastSaved(t: Dict, text: string, path: string, opts: {slicer?: SlicerTarget} = {}) {
    const actions: ToastAction[] = [
        {
            label: isMac() ? t.export.revealMac : t.export.revealWin,
            title: t.export.revealHint,
            run: () => RevealPath(path).catch((e) => toast.error(errorText(e))),
        },
    ]
    const sl = opts.slicer
    if (sl && /\.(3mf|stl)$/i.test(path)) {
        if (sl.installed) {
            const target = sl.installed
            actions.push({
                label: fmt(t.export.openSlicer, {name: target.name}),
                title: fmt(t.export.openSlicerHint, {name: target.name}),
                run: () => void openInSlicer(t, target, path),
            })
        } else if (sl.recommendedName) {
            actions.push({
                label: fmt(t.export.openSlicer, {name: sl.recommendedName}),
                title: fmt(t.export.noSlicerHint, {name: sl.recommendedName}),
                run: () => {},
                disabled: true,
            })
        }
    }
    toast.withActions(fmt(text, {path}), actions, t.export.dismiss)
}

export async function openInSlicer(t: Dict, slicer: {id: string; name: string}, path: string) {
    try {
        await OpenInSlicer(slicer.id, path)
        toast.ok(fmt(t.export.opening, {name: slicer.name}))
    } catch (e) {
        toast.error(errorText(e))
    }
}
