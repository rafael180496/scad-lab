import {useCallback, useEffect, useRef, useState} from 'react'
import {store} from '../../wailsjs/go/models'
import {SaveLayout} from '../../wailsjs/go/main/App'
import {toast} from '../components/ui/Toast'
import {errorText} from '../lib/errors'
import {isMod} from '../lib/platform'

// Tamaños de fábrica y límites: los mismos que backend/store (Layout).
export const LAYOUT_DEFAULTS = {sidebarWidth: 264, codeWidth: 420, bottomHeight: 220}
export const LAYOUT_LIMITS = {
    sidebar: [200, 480],
    code: [280, 1400],
    bottom: [120, 800],
} as const

// useLayout lleva la disposición de los paneles: la que se ve (cambia en vivo
// mientras se arrastra un divisor) y la que se guarda (al soltar). Cmd/Ctrl+B
// pliega la barra lateral.
export function useLayout(initial: store.Layout | undefined, onSaved: (l: store.Layout) => void) {
    const [layout, setLayout] = useState<store.Layout>(() =>
        store.Layout.createFrom({...LAYOUT_DEFAULTS, sidebarCollapsed: false, dimsCollapsed: false, ...(initial ?? {})}),
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

    return {layout, preview, commit, toggleSidebar}
}
