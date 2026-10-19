import {useCallback, useEffect, useRef, useState, type ReactNode} from 'react'
import type {BufferGeometry} from 'three'
import {CircleAlert, Loader2, X} from 'lucide-react'
import {SaveScreenshot, SaveSwatches, SaveViewer} from '../../../wailsjs/go/main/App'
import {store} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {toastSaved} from '../../lib/saved'
import {setViewerCapture} from '../../lib/viewerBridge'
import {errorText} from '../../lib/errors'
import {cachedMesh, cachedParts, loadMesh, loadParts, meshKey} from '../../lib/mesh'
import {ensureMaterials} from '../../lib/materials'
import {printStore, selectPart} from '../../lib/printSession'
import {partColor} from '../../lib/printTypes'
import {themeColors, useThemeColors} from '../../hooks/useThemeColors'
import {toast} from '../ui/Toast'
import {ViewPane, type BedVolume, type PaneHandle, type PaneKeyCommand, type PartsScene} from './ViewPane'
import {ViewerToolbar} from './ViewerToolbar'
import {Dimensions} from './Dimensions'
import {PartCard} from './PartCard'
import {PlateAlert} from './PlateAlert'
import type {ViewName} from './camera'

// Estado inicial del visor; en la app real no se pasa (lo usa el banco de
// capturas, src/uishot.tsx, para fotografiar cada modo sin simular clics).
export type ViewerShot = {
    view?: ViewName
    wireframe?: boolean
    edges?: boolean
    ortho?: boolean
    split?: boolean
    splitView?: ViewName
    color?: number
    /** Parte seleccionada al abrir (índice). */
    select?: number
    /** false = pintar con la muestra aunque el modelo traiga colores. */
    modelColors?: boolean
}

type Props = {
    // null = no hay malla que mostrar ahora (el visor queda montado y oculto
    // para conservar la cámara y la caché).
    meshPath: string | null
    fileName: string
    bed: BedVolume
    swatches: string[]
    onSwatchesSaved: (colors: string[]) => void
    overlay?: ReactNode
    initial?: ViewerShot
    dimsCollapsed: boolean
    onDimsCollapsed: (collapsed: boolean) => void
    /** Lo que el visor recordó de la sesión anterior (settings.viewer); se lee una vez, al montar. */
    saved?: store.ViewerState
    /** Se guardó el estado del visor: la configuración en memoria tiene que verlo. */
    onViewerSaved?: (v: store.ViewerState) => void
    /** Entrar al modo «Editar» (S13); sin esto, la barra no ofrece el botón. */
    onEdit?: () => void
    editBusy?: boolean
}

// Espera antes de guardar el estado del visor: alternar alambre y aristas varias veces
// seguidas no debe escribir en la base a cada clic.
const VIEWER_SAVE_DELAY_MS = 600

// Viewer es el visor 3D (S2). Entra con React.lazy: three y R3F pesan más que
// todo el resto de la interfaz y no hacen falta para abrir la app.
export default function Viewer({meshPath, fileName, bed, swatches, onSwatchesSaved, overlay, initial, dimsCollapsed, onDimsCollapsed, saved, onViewerSaved, onEdit, editBusy}: Props) {
    const t = useT()
    const theme = useThemeColors()
    const [geometry, setGeometry] = useState<BufferGeometry | null>(() => (meshPath ? (cachedMesh(meshKey(meshPath)) ?? null) : null))
    const [shownPath, setShownPath] = useState<string | null>(geometry ? meshPath : null)
    const [loading, setLoading] = useState(false)
    const [loadError, setLoadError] = useState('')
    // Cada modo arranca como lo dejó el usuario (saved); el banco de capturas (initial) manda sobre eso.
    const [wireframe, setWireframe] = useState(initial?.wireframe ?? saved?.wireframe ?? false)
    const [edges, setEdges] = useState(initial?.edges ?? saved?.edges ?? false)
    const [ortho, setOrtho] = useState<[boolean, boolean]>([initial?.ortho ?? saved?.ortho?.[0] ?? false, initial?.ortho ?? saved?.ortho?.[1] ?? false])
    const [split, setSplit] = useState(initial?.split ?? saved?.split ?? false)
    const [active, setActive] = useState<0 | 1>(0)
    const [colorIndex, setColorIndex] = useState(initial?.color ?? saved?.colorIndex ?? 0)
    const [colors, setColors] = useState(swatches)
    const [modelColors, setModelColors] = useState(initial?.modelColors ?? saved?.modelColors ?? true)
    // Modelo por partes (S7): lo pide usePrintSession (PanelHost) y llega por el
    // store; acá se bajan las mallas de cada parte.
    const model = printStore.use((s) => s.model)
    const modelMesh = printStore.use((s) => s.modelMesh)
    const selected = printStore.use((s) => s.selected)
    const plate = printStore.use((s) => s.plate)
    const [partGeoms, setPartGeoms] = useState<{key: string; geoms: BufferGeometry[]} | null>(null)
    const pane0 = useRef<PaneHandle | null>(null)
    const pane1 = useRef<PaneHandle | null>(null)
    const panes = [pane0, pane1]

    useEffect(() => setColors(swatches), [swatches])

    // Persistencia del estado del visor (settings.viewer): se guarda con espera al cambiar, y lo
    // pendiente se vuelca al salir. La base de comparación es lo que se montó, así abrir el visor
    // no escribe nada.
    const viewerNow = JSON.stringify({wireframe, edges, ortho, split, colorIndex, modelColors})
    const viewerSaved = useRef(viewerNow)
    const viewerPending = useRef<string | null>(null)
    const viewerTimer = useRef<number | undefined>(undefined)
    const onViewerSavedRef = useRef(onViewerSaved)
    onViewerSavedRef.current = onViewerSaved
    const flushViewer = useCallback(() => {
        window.clearTimeout(viewerTimer.current)
        const json = viewerPending.current
        viewerPending.current = null
        if (json === null || json === viewerSaved.current) return
        viewerSaved.current = json
        SaveViewer(store.ViewerState.createFrom(JSON.parse(json)))
            .then((v) => onViewerSavedRef.current?.(v))
            .catch((e) => toast.error(errorText(e)))
    }, [])
    useEffect(() => {
        if (viewerNow === viewerSaved.current) {
            viewerPending.current = null
            return
        }
        viewerPending.current = viewerNow
        window.clearTimeout(viewerTimer.current)
        viewerTimer.current = window.setTimeout(flushViewer, VIEWER_SAVE_DELAY_MS)
    }, [viewerNow, flushViewer])
    useEffect(() => {
        window.addEventListener('pagehide', flushViewer)
        return () => {
            window.removeEventListener('pagehide', flushViewer)
            flushViewer()
        }
    }, [flushViewer])

    // Carga: la malla anterior sigue a la vista mientras llega la nueva, así
    // cambiar de checkpoint no parpadea. Una ya vista sale de la caché.
    useEffect(() => {
        if (!meshPath) return
        let alive = true
        setLoadError('')
        const hit = cachedMesh(meshKey(meshPath))
        if (hit) {
            setGeometry(hit)
            setShownPath(meshPath)
            setLoading(false)
            return
        }
        setLoading(true)
        loadMesh(meshPath)
            .then((g) => {
                if (!alive) return
                setGeometry(g)
                setShownPath(meshPath)
            })
            .catch((e) => alive && setLoadError(errorText(e)))
            .finally(() => alive && setLoading(false))
        return () => {
            alive = false
        }
    }, [meshPath])

    // Las partes de cada color. La malla entera sigue a la vista hasta que llegan.
    useEffect(() => {
        if (!model || model.parts.length === 0) return
        void ensureMaterials()
        const hit = cachedParts(model.key)
        if (hit) {
            setPartGeoms({key: model.key, geoms: hit})
            return
        }
        let alive = true
        loadParts(
            model.key,
            model.parts.map((p) => p.url),
        )
            .then((geoms) => alive && setPartGeoms({key: model.key, geoms}))
            .catch(() => {})
        return () => {
            alive = false
        }
    }, [model])

    // Banco de capturas: arranca con una parte seleccionada.
    const shotSelect = initial?.select
    useEffect(() => {
        if (shotSelect !== undefined && model) selectPart(shotSelect)
    }, [shotSelect, model])

    // Solo se dibujan las partes si son las de la malla que está a la vista: tras
    // cambiar de checkpoint, el modelo viejo no se pinta sobre la pieza nueva.
    const partsReady =
        !!model && !!shownPath && model.parts.length > 0 && modelMesh === meshKey(shownPath) && partGeoms?.key === model.key && partGeoms.geoms.length === model.parts.length
    const hasColors = partsReady && model.parts.some((p) => partColor(p))
    const activePane = split ? active : 0
    const pane = () => panes[activePane].current

    const command = useCallback(
        (c: PaneKeyCommand, which: 0 | 1) => {
            if (c === 'deselect') selectPart(null)
            else if (c === 'wireframe') setWireframe((v) => !v)
            else if (c === 'edges') setEdges((v) => !v)
            else setOrtho((o) => (which === 0 ? [!o[0], o[1]] : [o[0], !o[1]]))
        },
        [],
    )

    // Guardado de muestras con un respiro: el selector de color dispara un
    // cambio por cada movimiento del cursor.
    const saveTimer = useRef<number | undefined>(undefined)
    const editColor = (i: number, c: string) => {
        const next = colors.map((x, j) => (j === i ? c.toLowerCase() : x))
        setColors(next)
        setColorIndex(i)
        window.clearTimeout(saveTimer.current)
        saveTimer.current = window.setTimeout(() => {
            SaveSwatches(next)
                .then(() => onSwatchesSaved(next))
                .catch((e) => toast.error(errorText(e)))
        }, 500)
    }
    useEffect(() => () => window.clearTimeout(saveTimer.current), [])

    // Guarda la vista activa como PNG; devuelve la ruta ("" si el usuario cancela).
    // La usan el botón de la barra del visor y el botón PNG de la cabecera
    // (lib/viewerBridge), que es quien muestra el aviso con «Mostrar en el Finder».
    const capture = async (): Promise<string> => {
        const h = pane()
        if (!h) return ''
        // El canvas es transparente (el fondo es el del panel): la captura se
        // compone sobre el color de superficie del tema.
        const src = h.capture()
        const out = document.createElement('canvas')
        out.width = src.width
        out.height = src.height
        const ctx = out.getContext('2d')
        if (!ctx) return ''
        ctx.fillStyle = themeColors().surface2
        ctx.fillRect(0, 0, out.width, out.height)
        ctx.drawImage(src, 0, 0)
        return SaveScreenshot(out.toDataURL('image/png'), fileName.replace(/\.scad$/i, ''))
    }
    const captureRef = useRef(capture)
    captureRef.current = capture
    useEffect(() => {
        setViewerCapture(() => captureRef.current())
        return () => setViewerCapture(null)
    }, [])
    const captureFromToolbar = async () => {
        try {
            const path = await capture()
            if (path) toastSaved(t, t.viewer.captureSaved, path)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const color = colors[colorIndex] ?? colors[0] ?? theme.accent
    const parts: PartsScene | undefined = partsReady
        ? {
              geoms: partGeoms.geoms,
              colors: model.parts.map(partColor),
              modelColors: modelColors && hasColors,
              selected: selected !== null && model.parts.some((p) => p.index === selected) ? model.parts.findIndex((p) => p.index === selected) : null,
              onSelect: (i) => selectPart(i === null ? null : (model.parts[i]?.index ?? null)),
          }
        : undefined
    const selectedPart = parts && parts.selected !== null ? model?.parts[parts.selected] : undefined
    const scene = (i: 0 | 1) => ({
        geometry,
        parts,
        overflow: plate ? !plate.fits : false,
        bed,
        color,
        wireframe,
        edges,
        ortho: ortho[i],
        initialView: i === 0 ? (initial?.view ?? 'iso') : (initial?.splitView ?? 'top'),
        theme,
        handleRef: panes[i],
    })
    const stale = !!meshPath && shownPath !== meshPath

    return (
        <div className={meshPath ? 'flex h-full min-h-0 flex-col' : 'hidden'}>
            <ViewerToolbar
                wireframe={wireframe}
                edges={edges}
                ortho={ortho[activePane]}
                split={split}
                colors={colors}
                colorIndex={colorIndex}
                modelColors={hasColors ? modelColors : null}
                onModelColors={setModelColors}
                onView={(v) => pane()?.setView(v)}
                onWireframe={() => command('wireframe', activePane)}
                onEdges={() => command('edges', activePane)}
                onOrtho={() => command('ortho', activePane)}
                onFit={() => pane()?.fit()}
                onColor={(i) => {
                    setColorIndex(i)
                    // Elegir una muestra es pedir ese color: pasa a pintar con ella.
                    setModelColors(false)
                }}
                onEditColor={editColor}
                onCapture={() => void captureFromToolbar()}
                onEdit={onEdit}
                editBusy={editBusy}
                onSplit={() => {
                    setSplit((s) => !s)
                    setActive(0)
                }}
            />
            <div className={split ? 'grid min-h-0 flex-1 grid-cols-2 gap-px bg-line' : 'flex min-h-0 flex-1'}>
                <ViewPane
                    scene={scene(0)}
                    active={split && active === 0}
                    label={t.viewer.pane}
                    onActivate={() => setActive(0)}
                    onCommand={(c) => command(c, 0)}
                    className="flex-1"
                >
                    {overlay || (plate && !plate.fits) ? (
                        <div className="absolute top-2 left-2 z-10 flex max-w-[70%] flex-col items-start gap-1.5">
                            {plate && !plate.fits ? <PlateAlert plate={plate} /> : null}
                            {overlay}
                        </div>
                    ) : null}
                    {selectedPart ? (
                        <div className="absolute right-2 bottom-2 z-10 max-h-[calc(100%-1rem)] max-w-[calc(50%-0.75rem)] overflow-auto">
                            <PartCard index={selectedPart.index} color={partColor(selectedPart)} />
                        </div>
                    ) : null}
                    {geometry && shownPath ? (
                        <div className="absolute bottom-2 left-2 z-10 max-h-[calc(100%-1rem)] max-w-[calc(100%-1rem)] overflow-auto">
                            <Dimensions meshPath={shownPath} geometry={geometry} collapsed={dimsCollapsed} onCollapsed={onDimsCollapsed} />
                        </div>
                    ) : null}
                    {loading || stale ? (
                        <span className="absolute top-2 right-2 z-10 flex items-center gap-1.5 rounded-md border border-line bg-surface/90 px-2 py-1 font-mono text-ui-10-5 text-muted">
                            <Loader2 className="icon-ui-12 animate-spin text-accent" />
                            {t.viewer.loading}
                        </span>
                    ) : null}
                    {loadError ? (
                        <div className="absolute inset-x-4 top-1/2 z-10 mx-auto flex max-w-[420px] -translate-y-1/2 items-start gap-2 rounded-lg border border-danger/50 bg-surface p-3 text-ui-12 text-fg">
                            <CircleAlert className="mt-px icon-ui-14 shrink-0 text-danger" />
                            <span className="selectable">{fmt(t.viewer.loadError, {msg: loadError})}</span>
                        </div>
                    ) : null}
                </ViewPane>
                {split ? (
                    <ViewPane
                        scene={scene(1)}
                        active={active === 1}
                        label={t.viewer.pane2}
                        onActivate={() => setActive(1)}
                        onCommand={(c) => command(c, 1)}
                    >
                        <button
                            type="button"
                            title={t.viewer.closePane}
                            aria-label={t.viewer.closePane}
                            onPointerDown={(e) => e.stopPropagation()}
                            onClick={() => {
                                setSplit(false)
                                setActive(0)
                            }}
                            className="absolute top-2 right-2 z-10 inline-flex icon-ui-24 items-center justify-center rounded-md border border-line bg-surface/90 text-muted transition-colors duration-100 hover:text-fg"
                        >
                            <X className="icon-ui-14" />
                        </button>
                    </ViewPane>
                ) : null}
            </div>
        </div>
    )
}
