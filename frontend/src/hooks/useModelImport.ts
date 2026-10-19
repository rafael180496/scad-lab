import {useCallback, useEffect, useRef, useState} from 'react'
import {ChooseModelFile} from '../../wailsjs/go/main/App'
import {OnFileDrop, OnFileDropOff} from '../../wailsjs/runtime/runtime'
import {toast} from '../components/ui/Toast'
import {t} from '../i18n'
import {errorText} from '../lib/errors'

// Importar un modelo 3D al proyecto abierto (S13): desde el botón de la barra lateral (diálogo de archivos del
// sistema) o arrastrando el archivo a la ventana. Los dos terminan en el mismo diálogo de importación, con el
// análisis del archivo; esto solo decide QUÉ archivo.
//
// El arrastre usa OnFileDrop de Wails: con DragAndDrop activo, main.go desactiva el arrastre HTML5 del webview
// (DisableWebViewDrop) y es el runtime quien entrega las RUTAS de lo soltado. `false` = cualquier lugar de la
// ventana vale, no solo un elemento marcado con --wails-drop-target.
const MODEL_RE = /\.(stl|3mf|obj)$/i

export function useModelImport(workspaceId: number | null) {
    const [path, setPath] = useState<string | null>(null)
    const wsRef = useRef(workspaceId)
    wsRef.current = workspaceId
    const openRef = useRef(path !== null)
    openRef.current = path !== null

    const offer = useCallback((p: string) => {
        if (wsRef.current == null) {
            toast.error(t().meshEdit.import.noProject)
            return
        }
        setPath(p)
    }, [])

    const choose = useCallback(async () => {
        if (wsRef.current == null) {
            toast.error(t().meshEdit.import.noProject)
            return
        }
        try {
            const p = await ChooseModelFile()
            if (p) setPath(p)
        } catch (e) {
            toast.error(errorText(e))
        }
    }, [])

    useEffect(() => {
        try {
            OnFileDrop((_x, _y, paths) => {
                if (openRef.current) return
                const models = (paths ?? []).filter((p) => MODEL_RE.test(p))
                if (models.length === 0) {
                    toast.error(t().meshEdit.import.dropUnsupported)
                    return
                }
                offer(models[0])
                if (models.length > 1) toast.ok(t().meshEdit.import.dropMany)
            }, false)
        } catch {
            // Sin el runtime de Wails (el banco de capturas, una página suelta) no hay arrastre de archivos.
        }
        return () => {
            try {
                OnFileDropOff()
            } catch {
                // idem
            }
        }
    }, [offer])

    return {
        choose,
        offer,
        dialog: {open: path !== null, path: path ?? '', workspaceId, onClose: () => setPath(null)},
    }
}
