import {useCallback, useEffect, useRef, useState} from 'react'
import type {main, printers, render} from '../../wailsjs/go/models'
import {DetectSlicers, ExportCheckpoint} from '../../wailsjs/go/main/App'
import {toast} from '../components/ui/Toast'
import {fmt, useT} from '../i18n'
import {errorText} from '../lib/errors'
import {openInSlicer, toastSaved, type SlicerTarget} from '../lib/saved'
import {installedSlicer, recommendedSlicer} from '../lib/printerInfo'
import {isMod} from '../lib/platform'
import {captureViewer} from '../lib/viewerBridge'
import type {ExportFormat} from '../components/export/ExportDialog'

// useExport reúne lo que la cabecera necesita para exportar el checkpoint
// elegido (S9): si se puede y, si no, por qué; el STL directo, el PNG del visor
// y el diálogo de 3MF. Los slicers se detectan al arrancar y cada vez que cambia
// una ruta en Configuración (`slicerKey`); «Abrir en <slicer>» usa el primero
// instalado de los de la impresora del workspace (S14).
export function useExport({
    workspaceId,
    file,
    result,
    running,
    slicerKey,
    printer,
}: {
    workspaceId: number | null
    file: string | null
    result: render.Result | undefined
    running: boolean
    /** Cambia cuando cambia alguna ruta de slicer de Configuración: vuelve a detectar. */
    slicerKey: string
    /** La impresora del workspace: de ahí salen sus slicers. */
    printer: printers.Printer | undefined
}) {
    const t = useT()
    const [slicers, setSlicers] = useState<main.Slicer[]>([])
    const [dialog, setDialog] = useState(false)
    const [busy, setBusy] = useState(false)
    const installed = installedSlicer(printer, slicers)
    const recommended = recommendedSlicer(printer, slicers)
    // Lo que el aviso de guardado y el diálogo saben del slicer; sin ninguno que nombrar, nada que ofrecer.
    const slicer: SlicerTarget | undefined =
        installed || recommended ? {installed: installed ? {id: installed.id, name: installed.name} : undefined, recommendedName: recommended?.name} : undefined

    useEffect(() => {
        let alive = true
        DetectSlicers()
            .then((l) => alive && setSlicers(l ?? []))
            .catch(() => {})
        return () => {
            alive = false
        }
    }, [slicerKey])

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
            if (res.path) toastSaved(t, t.export.saved, res.path, {slicer})
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setBusy(false)
        }
    }, [workspaceId, file, busy, t, slicer])

    const exportPng = useCallback(async () => {
        try {
            const path = await captureViewer()
            if (path) toastSaved(t, t.viewer.captureSaved, path)
        } catch (e) {
            toast.error(errorText(e))
        }
    }, [t])

    // Tras exportar desde el diálogo; `open` = «Exportar y abrir en <slicer>».
    const exported = useCallback(
        (path: string, open: boolean) => {
            if (open && slicer?.installed) {
                toast.ok(fmt(t.export.saved, {path}))
                void openInSlicer(t, slicer.installed, path)
            } else toastSaved(t, t.export.saved, path, {slicer})
        },
        [t, slicer],
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
        slicers,
        slicer,
        /** El formato con que arranca el diálogo: PrusaSlicer lee su propio 3MF; Cura no asigna extrusores desde un 3MF de partes. */
        preferred: (recommended?.id === 'prusaslicer' ? '3mf-prusa' : recommended?.id === 'ultimaker-cura' ? 'stl-per-color' : '3mf') as ExportFormat,
        dialog,
        openDialog,
        closeDialog,
        exportStl,
        exportPng,
        exported,
    }
}
