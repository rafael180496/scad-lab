import {useCallback, useEffect, useRef, useState} from 'react'
import {store} from '../../wailsjs/go/models'
import {SaveLayout} from '../../wailsjs/go/main/App'
import {toast} from '../components/ui/Toast'
import {errorText} from '../lib/errors'
import {isMod} from '../lib/platform'

// Tamaños de fábrica y límites de la barra lateral (los mismos que backend/store,
// Layout). Desde S4b el código y el panel inferior ya no tienen tamaño aquí:
// son paneles acoplables (components/dock) y dockview lleva sus medidas.
// `codeWidth` y `bottomHeight` siguen en el setting `layout` sin uso.
export const LAYOUT_DEFAULTS = {sidebarWidth: 264, workspacesHeight: 150}
export const LAYOUT_LIMITS = {
    sidebar: [200, 480],
    workspaces: [56, 640],
} as const

// Lo que se le deja al área de paneles al agrandar la barra lateral (ancho de
// dos columnas de panel con su mínimo) más los márgenes y el divisor.
const DOCK_MIN_W = 640
const SIDEBAR_GAP = 24

// sidebarMaxFor es el ancho máximo de la barra lateral para una ventana de `w`
// px. Así, al achicar la ventana la barra cede en vez de empujar a los paneles
// fuera de ella.
export function sidebarMaxFor(w: number): number {
    return Math.max(LAYOUT_LIMITS.sidebar[0], Math.min(LAYOUT_LIMITS.sidebar[1], w - SIDEBAR_GAP - DOCK_MIN_W))
}

// useWindowWidth sigue el ancho de la ventana (innerWidth).
export function useWindowWidth(): number {
    const [w, setW] = useState(() => window.innerWidth)
    useEffect(() => {
        const on = () => setW(window.innerWidth)
        window.addEventListener('resize', on)
        return () => window.removeEventListener('resize', on)
    }, [])
    return w
}

// useLayout lleva la disposición de la barra lateral (ancho, alto de la lista de
// workspaces, plegada) y de la barra de estado: la que se ve (cambia en vivo
// mientras se arrastra un divisor) y la que se guarda (al soltar). Cmd/Ctrl+B
// pliega la barra lateral.
export function useLayout(initial: store.Layout | undefined, onSaved: (l: store.Layout) => void) {
    const [layout, setLayout] = useState<store.Layout>(() =>
        store.Layout.createFrom({...LAYOUT_DEFAULTS, sidebarCollapsed: false, dimsCollapsed: false, statusBarHidden: false, ...(initial ?? {})}),
    )
    const saved = useRef(onSaved)
    saved.current = onSaved
    // La configuración llega después del primer render (GetBootstrap): se
    // adopta una sola vez. Después manda lo que hay en pantalla.
    const adopted = useRef(!!initial)
    useEffect(() => {
        if (!initial || adopted.current) return
        adopted.current = true
        setLayout(store.Layout.createFrom({...LAYOUT_DEFAULTS, ...initial}))
    }, [initial])

    const persist = useCallback((next: store.Layout) => {
        SaveLayout(next)
            .then((l) => saved.current(l))
            .catch((e) => toast.error(errorText(e)))
    }, [])

    // preview: solo en pantalla (arrastrando). commit: en pantalla y en disco.
    const preview = useCallback((patch: Partial<store.Layout>) => {
        setLayout((l) => store.Layout.createFrom({...l, ...patch}))
    }, [])
    const current = useRef(layout)
    current.current = layout
    const commit = useCallback(
        (patch: Partial<store.Layout>) => {
            const next = store.Layout.createFrom({...current.current, ...patch})
            current.current = next
            setLayout(next)
            persist(next)
        },
        [persist],
    )
    const toggleSidebar = useCallback(() => commit({sidebarCollapsed: !current.current.sidebarCollapsed}), [commit])
    const toggleStatusBar = useCallback(() => commit({statusBarHidden: !current.current.statusBarHidden}), [commit])
    // «Restablecer el diseño»: tamaños de fábrica de la barra lateral, con las
    // barras y las dimensiones a la vista (con la barra oculta el botón parecería
    // no haber hecho nada). Los paneles los restablece el dock (resetLayout de
    // components/dock).
    const resetLayout = useCallback(
        () => commit({...LAYOUT_DEFAULTS, sidebarCollapsed: false, dimsCollapsed: false, statusBarHidden: false}),
        [commit],
    )

    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (isMod(e) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'b') {
                e.preventDefault()
                e.stopPropagation()
                toggleSidebar()
            }
        }
        window.addEventListener('keydown', onKey, true)
        return () => window.removeEventListener('keydown', onKey, true)
    }, [toggleSidebar])

    return {layout, preview, commit, toggleSidebar, toggleStatusBar, resetLayout}
}
