import {createStore} from './store'

// Apariencia de la app: tamaño de la interfaz y letra de cada área. Mismos
// valores y límites que backend/store (UIScalePresets, NormalizeFontSize).
//
// Qué escala cada cosa (ver claude/04-frontend.md, «Tamaños y escala»):
//   uiScale          texto e íconos de la interfaz, vía --ui-font-scale. NO los
//                    espaciados: es «más grande para leer», no un zoom de ventana.
//   editorFontSize   letra de Monaco (px).
//   terminalFontSize letra de xterm (px).
//   chatFontSize     letra de los mensajes del chat, vía --chat-font-size (px).
// Las tres letras de área son independientes de uiScale.

export type Appearance = {
    uiScale: number
    editorFontSize: number
    terminalFontSize: number
    chatFontSize: number
}

export const FONT_MIN = 9
export const FONT_MAX = 28
export const FONT_DEFAULT = 13

// Presets de la interfaz. `key` indexa t.appearance.scales.
export const UI_SCALES = [
    {value: 90, key: 'compact'},
    {value: 100, key: 'normal'},
    {value: 115, key: 'large'},
    {value: 130, key: 'larger'},
    {value: 150, key: 'max'},
] as const

export const APPEARANCE_DEFAULTS: Appearance = {
    uiScale: 100,
    editorFontSize: FONT_DEFAULT,
    terminalFontSize: FONT_DEFAULT,
    chatFontSize: FONT_DEFAULT,
}

export function normalizeScale(pct: number): number {
    return UI_SCALES.some((s) => s.value === pct) ? pct : APPEARANCE_DEFAULTS.uiScale
}

export function normalizeFont(px: number): number {
    return Number.isFinite(px) && px >= FONT_MIN && px <= FONT_MAX ? Math.round(px) : FONT_DEFAULT
}

export function normalizeAppearance(a: Partial<Appearance> | undefined): Appearance {
    return {
        uiScale: normalizeScale(a?.uiScale ?? 0),
        editorFontSize: normalizeFont(a?.editorFontSize ?? 0),
        terminalFontSize: normalizeFont(a?.terminalFontSize ?? 0),
        chatFontSize: normalizeFont(a?.chatFontSize ?? 0),
    }
}

// Lo que ven los componentes que no cuelgan del estado de App (Monaco, xterm,
// los cálculos de «¿cabe?»): se suscriben con appearance.use(...).
export const appearance = createStore<Appearance>(APPEARANCE_DEFAULTS)

// applyAppearance escribe las variables CSS y la store. Se llama al arrancar,
// en cada cambio (en vivo, antes de guardar) y al restablecer.
export function applyAppearance(a: Appearance): void {
    const root = document.documentElement
    // Sin unidad: `calc(12px * var(--ui-font-scale))` necesita un número pelado.
    root.style.setProperty('--ui-font-scale', String(a.uiScale / 100))
    root.style.setProperty('--chat-font-size', `${a.chatFontSize}px`)
    appearance.set(a)
}

// Escala de interfaz anterior/siguiente a la actual (los atajos recorren los presets).
export function stepScale(current: number, dir: 1 | -1): number {
    let i = UI_SCALES.findIndex((s) => s.value === current)
    if (i < 0) i = UI_SCALES.findIndex((s) => s.value === APPEARANCE_DEFAULTS.uiScale)
    return UI_SCALES[Math.max(0, Math.min(UI_SCALES.length - 1, i + dir))].value
}
