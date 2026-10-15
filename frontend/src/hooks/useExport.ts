import {useCallback, useEffect, useRef, useState} from 'react'
import type {main, render} from '../../wailsjs/go/models'
import {DetectSlicers, ExportCheckpoint} from '../../wailsjs/go/main/App'
import {toast} from '../components/ui/Toast'
import {fmt, useT} from '../i18n'
import {errorText} from '../lib/errors'
import {openInBambu, toastSaved} from '../lib/saved'
import {isMod} from '../lib/platform'
import {captureViewer} from '../lib/viewerBridge'

const BAMBU = 'bambu-studio'

// useExport reúne lo que la cabecera necesita para exportar el checkpoint
// elegido (S9): si se puede y, si no, por qué; el STL directo, el PNG del visor
// y el diálogo de 3MF. Bambu Studio se detecta al arrancar y cada vez que
// cambia su ruta en Configuración.
export function useExport({
    workspaceId,
    file,
    result,
    running,
    bambuPath,
}: {
    workspaceId: number | null
    file: string | null
    result: render.Result | undefined
    running: boolean
    bambuPath: string
}) {
    const t = useT()
    const [slicers, setSlicers] = useState<main.Slicer[]>([])
    const [dialog, setDialog] = useState(false)
    const [busy, setBusy] = useState(false)
    const bambu = slicers.find((s) => s.id === BAMBU && s.found)

    useEffect(() => {
        let alive = true
        DetectSlicers()
            .then((l) => alive && setSlicers(l ?? []))
            .catch(() => {})
        return () => {
            alive = false
        }
    }, [bambuPath])

    // Por qué no se puede exportar ('' = se puede). Exportar necesita un render
    // correcto: el STL sale de su malla y el 3MF se renderiza con colores aparte.
    const why = !workspaceId || !file
        ? t.export.why.noFile
        : running
          ? t.export.why.running
          : !result
            ? t.export.why.notRendered
            : result.status === 'missing'
              ? t.export.why.missing
              : result.status !== 'ok'
                ? result.status === 'error'
                    ? t.export.why.failed
                    : t.export.why.notRendered
                : ''

    const openDialog = useCallback(() => setDialog(true), [])
    const closeDialog = useCallback(() => setDialog(false), [])

    const exportStl = useCallback(async () => {
        if (!workspaceId || !file || busy) return
        setBusy(true)
        try {
            const res = await ExportCheckpoint(workspaceId, file, 'stl')
            if (res.path) toastSaved(t, t.export.saved, res.path, {bambuId: bambu?.id})
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setBusy(false)
        }
    }, [workspaceId, file, busy, t, bambu?.id])

    const exportPng = useCallback(async () => {
        try {
            const path = await captureViewer()
            if (path) toastSaved(t, t.viewer.captureSaved, path)
        } catch (e) {
            toast.error(errorText(e))
        }
    }, [t])

    // Tras exportar desde el diálogo; `open` = «Exportar y abrir en Bambu Studio».
    const exported = useCallback(
        (path: string, open: boolean) => {
            if (open && bambu) {
                toast.ok(fmt(t.export.saved, {path}))
                void openInBambu(t, bambu.id, path)
            } else toastSaved(t, t.export.saved, path, {bambuId: bambu?.id})
        },
        [t, bambu],
    )

    // Atajos: ⌘/Ctrl+E = 3MF, ⌘/Ctrl+⇧+E = STL. En captura sobre window, porque
    // Monaco usa ⌘E (usar la selección para buscar) y se quedaría con la tecla.
    // Sin posibilidad de exportar el atajo lo dice, en vez de quedarse mudo; con
    // un diálogo abierto no actúa.
    const latest = useRef({why, exportStl, t})
    latest.current = {why, exportStl, t}
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (!isMod(e) || e.altKey || e.key.toLowerCase() !== 'e') return
            e.preventDefault()
            e.stopPropagation()
            if (e.repeat || document.querySelector('dialog[open]')) return
            const {why: reason, exportStl: stl, t: tt} = latest.current
            if (reason) toast.error(fmt(tt.export.blocked, {why: reason}), 'export-blocked')
            else if (e.shiftKey) void stl()
            else setDialog(true)
        }
        window.addEventListener('keydown', onKey, true)
        return () => window.removeEventListener('keydown', onKey, true)
    }, [])

    return {
        why,
        busy,
        bambu,
        dialog,
        openDialog,
        closeDialog,
        exportStl,
        exportPng,
        exported,
    }
}
