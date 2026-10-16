import {ClipboardSetText} from '../../wailsjs/runtime/runtime'

// Copia al portapapeles. navigator.clipboard puede estar bloqueado en la
// webview (exige un contexto seguro y un gesto); el runtime de Wails lo hace
// por el sistema operativo y es el respaldo.
export async function copyText(text: string): Promise<boolean> {
    try {
        await navigator.clipboard.writeText(text)
        return true
    } catch {
        try {
            return await ClipboardSetText(text)
        } catch {
            return false
        }
    }
}
