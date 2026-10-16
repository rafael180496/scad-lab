import {useSyncExternalStore} from 'react'

// Colores del tema para lo que no es CSS (la escena 3D). Se leen de los
// tokens de index.css —nunca colores sueltos— y se vuelven a leer cuando
// cambia la clase de <html> (claro/oscuro).
export type ThemeColors = {
    bg: string
    surface: string
    surface2: string
    raised: string
    line: string
    lineStrong: string
    fg: string
    faint: string
    accent: string
    danger: string
}

const tokens: Record<keyof ThemeColors, string> = {
    bg: '--c-bg',
    surface: '--c-surface',
    surface2: '--c-surface-2',
    raised: '--c-raised',
    line: '--c-line',
    lineStrong: '--c-line-strong',
    fg: '--c-fg',
    faint: '--c-faint',
    accent: '--c-accent',
    danger: '--c-danger',
}

let snapshot: ThemeColors | null = null

function read(): ThemeColors {
    const css = getComputedStyle(document.documentElement)
    const out = {} as ThemeColors
    for (const k of Object.keys(tokens) as (keyof ThemeColors)[]) out[k] = css.getPropertyValue(tokens[k]).trim() || '#808080'
    return out
}

function subscribe(cb: () => void) {
    // El tema pudo cambiar mientras nadie escuchaba.
    snapshot = read()
    const obs = new MutationObserver(() => {
        snapshot = read()
        cb()
    })
    obs.observe(document.documentElement, {attributes: true, attributeFilter: ['class']})
    return () => obs.disconnect()
}

function get(): ThemeColors {
    if (!snapshot) snapshot = read()
    return snapshot
}

export function useThemeColors(): ThemeColors {
    return useSyncExternalStore(subscribe, get)
}

// themeColors es la lectura fuera de React (la captura PNG pinta el fondo).
export function themeColors(): ThemeColors {
    return read()
}
