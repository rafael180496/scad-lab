import {OpenInSlicer, RevealPath} from '../../wailsjs/go/main/App'
import {toast, type ToastAction} from '../components/ui/Toast'
import type {Dict} from '../i18n'
import {fmt} from '../i18n'
import {errorText} from './errors'
import {isMac} from './platform'

// Aviso tras guardar un archivo (STL, 3MF, ZIP, PNG): dice la ruta y ofrece
// mostrarlo en el Finder / Explorador y, si hay Bambu Studio y el archivo es de
// los que abre, abrirlo ahí. Abrir la app es siempre por un clic del usuario.
export function toastSaved(t: Dict, text: string, path: string, opts: {bambuId?: string} = {}) {
    const actions: ToastAction[] = [
        {
            label: isMac() ? t.export.revealMac : t.export.revealWin,
            title: t.export.revealHint,
            run: () => RevealPath(path).catch((e) => toast.error(errorText(e))),
        },
    ]
    if (opts.bambuId && /\.(3mf|stl)$/i.test(path)) {
        actions.push({
            label: t.export.openBambu,
            title: t.export.openBambuHint,
            run: () => void openInBambu(t, opts.bambuId!, path),
        })
    }
    toast.withActions(fmt(text, {path}), actions, t.export.dismiss)
}

export async function openInBambu(t: Dict, slicerId: string, path: string) {
    try {
        await OpenInSlicer(slicerId, path)
        toast.ok(t.export.opening)
    } catch (e) {
        toast.error(errorText(e))
    }
}
