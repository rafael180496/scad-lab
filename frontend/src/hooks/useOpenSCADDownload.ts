import {useEffect} from 'react'
import {CancelOpenSCADDownload, DownloadOpenSCAD, GetOpenSCADDownload, RemoveDownloadedOpenSCAD} from '../../wailsjs/go/main/App'
import type {main} from '../../wailsjs/go/models'
import {confirmDialog} from '../components/ui/Dialog'
import {toast} from '../components/ui/Toast'
import {fmt, t} from '../i18n'
import {errorCode, errorText} from '../lib/errors'
import {onEvent, type OpenSCADDownloadEvent} from '../lib/events'
import {createStore} from '../lib/store'

// Estado de «Descargar OpenSCAD» (S11, claude/18-descarga-openscad.md). Es UNO
// solo para toda la ventana —el aviso del panel de render y Configuración →
// OpenSCAD muestran la misma descarga—, así que vive en un store de módulo y no
// en el estado de cada componente. El evento `openscad:download` lo alimenta;
// los bindings son las acciones.

// Código del error de DownloadOpenSCAD cuando el usuario cancela (ErrCodeDownloadCancelled en Go).
const CANCELLED = 'download-cancelled'

export type DownloadPhase = 'idle' | 'download' | 'verify' | 'extract'

export type DownloadState = {
    /** Lo que dice el backend (qué se descarga, si ya está, ruta). null hasta que llega. */
    info: main.OpenSCADDownload | null
    phase: DownloadPhase
    received: number
    total: number
    /** Descargando o instalando. */
    busy: boolean
    /** Quitando la descarga. */
    removing: boolean
    /** Sube al instalar o quitar: App vuelve a renderizar el checkpoint activo con el motor que corresponda. */
    epoch: number
}

export const openscadDownload = createStore<DownloadState>({
    info: null,
    phase: 'idle',
    received: 0,
    total: 0,
    busy: false,
    removing: false,
    epoch: 0,
})

const IDLE = {phase: 'idle', received: 0, total: 0, busy: false} as const

function loadInfo(): Promise<void> {
    return GetOpenSCADDownload()
        .then((info) => {
            const s = openscadDownload.get()
            // Una descarga que ya corre (la ventana se recargó, o la inició otro componente).
            openscadDownload.set({info, ...(info.running && !s.busy ? {busy: true, phase: 'download' as const, total: info.sizeBytes} : {})})
        })
        .catch((e) => toast.error(errorText(e)))
}

function onProgress(e: OpenSCADDownloadEvent) {
    const s = openscadDownload.get()
    switch (e.phase) {
        case 'download':
        case 'verify':
        case 'extract':
            openscadDownload.set({phase: e.phase, received: e.received, total: e.total || s.total, busy: true})
            break
        case 'done':
            openscadDownload.set({...IDLE})
            void loadInfo()
            break
        default: // error | cancelled: el aviso lo da la promesa de DownloadOpenSCAD
            openscadDownload.set({...IDLE})
    }
}

let attached = 0
let detach: (() => void) | null = null

// useOpenSCADDownloadSync engancha el evento mientras haya algún componente que
// lo necesite (cuenta de referencias). App lo usa sin leer el estado, para no
// redibujar la ventana con cada aviso de progreso.
export function useOpenSCADDownloadSync() {
    useEffect(() => {
        if (attached++ === 0) {
            detach = onEvent('openscad:download', onProgress)
            void loadInfo()
        }
        return () => {
            if (--attached === 0) {
                detach?.()
                detach = null
            }
        }
    }, [])
}

export async function startOpenSCADDownload() {
    const s = openscadDownload.get()
    if (s.busy || s.removing) return
    openscadDownload.set({busy: true, phase: 'download', received: 0, total: s.info?.sizeBytes ?? 0})
    try {
        const info = await DownloadOpenSCAD()
        openscadDownload.set({...IDLE, info, epoch: openscadDownload.get().epoch + 1})
        toast.ok(fmt(t().openscadDl.done, {v: info.version}))
    } catch (e) {
        openscadDownload.set({...IDLE})
        if (errorCode(e) === CANCELLED) toast.ok(t().openscadDl.cancelled)
        else toast.error(errorText(e))
        void loadInfo()
    }
}

export function cancelOpenSCADDownload() {
    CancelOpenSCADDownload().catch((e) => toast.error(errorText(e)))
}

export async function removeOpenSCADDownload() {
    const s = openscadDownload.get()
    if (s.busy || s.removing) return
    const d = t().openscadDl
    if (!(await confirmDialog(d.removeConfirm, d.remove, t().editor.cancel))) return
    openscadDownload.set({removing: true})
    try {
        const info = await RemoveDownloadedOpenSCAD()
        openscadDownload.set({info, epoch: openscadDownload.get().epoch + 1})
        toast.ok(d.removed)
    } catch (e) {
        toast.error(errorText(e))
        void loadInfo()
    } finally {
        openscadDownload.set({removing: false})
    }
}

// useOpenSCADDownload es el estado compartido, con las acciones.
export function useOpenSCADDownload() {
    useOpenSCADDownloadSync()
    const state = openscadDownload.use((s) => s)
    return {...state, start: startOpenSCADDownload, cancel: cancelOpenSCADDownload, remove: removeOpenSCADDownload}
}
