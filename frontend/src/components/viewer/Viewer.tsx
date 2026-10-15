import {useCallback, useEffect, useRef, useState, type ReactNode} from 'react'
import type {BufferGeometry} from 'three'
import {CircleAlert, Loader2, X} from 'lucide-react'
import {SaveScreenshot, SaveSwatches} from '../../../wailsjs/go/main/App'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {cachedMesh, loadMesh, meshKey} from '../../lib/mesh'
import {themeColors, useThemeColors} from '../../hooks/useThemeColors'
import {toast} from '../ui/Toast'
import {ViewPane, type BedVolume, type PaneHandle, type PaneKeyCommand} from './ViewPane'
import {ViewerToolbar} from './ViewerToolbar'
import {Dimensions} from './Dimensions'
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
}

// Viewer es el visor 3D (S2). Entra con React.lazy: three y R3F pesan más que
// todo el resto de la interfaz y no hacen falta para abrir la app.
export default function Viewer({meshPath, fileName, bed, swatches, onSwatchesSaved, overlay, initial, dimsCollapsed, onDimsCollapsed}: Props) {
    const t = useT()
    const theme = useThemeColors()
    const [geometry, setGeometry] = useState<BufferGeometry | null>(() => (meshPath ? (cachedMesh(meshKey(meshPath)) ?? null) : null))
    const [shownPath, setShownPath] = useState<string | null>(geometry ? meshPath : null)
    const [loading, setLoading] = useState(false)
    const [loadError, setLoadError] = useState('')
    const [wireframe, setWireframe] = useState(initial?.wireframe ?? false)
    const [edges, setEdges] = useState(initial?.edges ?? false)
    const [ortho, setOrtho] = useState<[boolean, boolean]>([initial?.ortho ?? false, initial?.ortho ?? false])
    const [split, setSplit] = useState(initial?.split ?? false)
    const [active, setActive] = useState<0 | 1>(0)
    const [colorIndex, setColorIndex] = useState(initial?.color ?? 0)
    const [colors, setColors] = useState(swatches)
    const pane0 = useRef<PaneHandle | null>(null)
    const pane1 = useRef<PaneHandle | null>(null)
    const panes = [pane0, pane1]

    useEffect(() => setColors(swatches), [swatches])

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

    const activePane = split ? active : 0
    const pane = () => panes[activePane].current

    const command = useCallback(
        (c: PaneKeyCommand, which: 0 | 1) => {
            if (c === 'wireframe') setWireframe((v) => !v)
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

    const capture = async () => {
        const h = pane()
        if (!h) return
        // El canvas es transparente (el fondo es el del panel): la captura se
        // compone sobre el color de superficie del tema.
        const src = h.capture()
        const out = document.createElement('canvas')
        out.width = src.width
        out.height = src.height
        const ctx = out.getContext('2d')
        if (!ctx) return
        ctx.fillStyle = themeColors().surface2
        ctx.fillRect(0, 0, out.width, out.height)
        ctx.drawImage(src, 0, 0)
        try {
            const path = await SaveScreenshot(out.toDataURL('image/png'), fileName.replace(/\.scad$/i, ''))
            if (path) toast.ok(fmt(t.viewer.captureSaved, {path}))
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const color = colors[colorIndex] ?? colors[0] ?? theme.accent
    const scene = (i: 0 | 1) => ({
        geometry,
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
                onView={(v) => pane()?.setView(v)}
                onWireframe={() => command('wireframe', activePane)}
                onEdges={() => command('edges', activePane)}
                onOrtho={() => command('ortho', activePane)}
                onFit={() => pane()?.fit()}
                onColor={setColorIndex}
                onEditColor={editColor}
                onCapture={() => void capture()}
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
                    {overlay ? <div className="absolute top-2 left-2 z-10 max-w-[70%]">{overlay}</div> : null}
                    {geometry && shownPath ? (
                        <div className="absolute bottom-2 left-2 z-10">
                            <Dimensions meshPath={shownPath} geometry={geometry} collapsed={dimsCollapsed} onCollapsed={onDimsCollapsed} />
                        </div>
                    ) : null}
                    {loading || stale ? (
                        <span className="absolute top-2 right-2 z-10 flex items-center gap-1.5 rounded-md border border-line bg-surface/90 px-2 py-1 font-mono text-[10.5px] text-muted">
                            <Loader2 className="size-3 animate-spin text-accent" />
                            {t.viewer.loading}
                        </span>
                    ) : null}
                    {loadError ? (
                        <div className="absolute inset-x-4 top-1/2 z-10 mx-auto flex max-w-[420px] -translate-y-1/2 items-start gap-2 rounded-lg border border-danger/50 bg-surface p-3 text-[12px] text-fg">
                            <CircleAlert className="mt-px size-3.5 shrink-0 text-danger" />
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
                            className="absolute top-2 right-2 z-10 inline-flex size-6 items-center justify-center rounded-md border border-line bg-surface/90 text-muted transition-colors duration-100 hover:text-fg"
                        >
                            <X className="size-3.5" />
                        </button>
                    </ViewPane>
                ) : null}
            </div>
        </div>
    )
}
