import {useCallback, useEffect, useRef, useState} from 'react'
import type {store} from '../../wailsjs/go/models'
import {store as storeModels} from '../../wailsjs/go/models'
import {SaveAppearance} from '../../wailsjs/go/main/App'
import {toast} from '../components/ui/Toast'
import {t as tNow, fmt} from '../i18n'
import {
    APPEARANCE_DEFAULTS,
    FONT_MAX,
    FONT_MIN,
    applyAppearance,
    normalizeAppearance,
    normalizeFont,
    stepScale,
    type Appearance,
} from '../lib/appearance'
import {errorText} from '../lib/errors'
import {isMod} from '../lib/platform'

// useAppearance lleva el tamaño de la interfaz y la letra del editor, la
// terminal y el chat. Decisión: **se aplican en vivo y se guardan al instante**
// (App.SaveAppearance, con 300 ms de espera para no guardar en cada pulsación de
// un campo numérico), sin pasar por el botón «Guardar» de Configuración. Un
// tamaño de letra que no se ve hasta confirmar obliga a adivinar; verlo mientras
// se elige es el punto.
//
// Los atajos ⌘/Ctrl + `+` `-` `0` viven acá: con el foco dentro del editor, la
// terminal o el chat (`data-font-area`) cambian la letra de ESA área; en
// cualquier otro lado, el tamaño de la interfaz.
export function useAppearance(initial: store.Settings | undefined) {
    const [value, setValue] = useState<Appearance>(() => normalizeAppearance(initial))
    const current = useRef(value)
    current.current = value
    // La configuración llega después del primer render (GetBootstrap): se adopta
    // una sola vez. Después manda lo que hay en pantalla.
    const adopted = useRef(false)
    useEffect(() => {
        if (!initial || adopted.current) return
        adopted.current = true
        const a = normalizeAppearance(initial)
        current.current = a
        setValue(a)
        applyAppearance(a)
    }, [initial])

    const timer = useRef<number | undefined>(undefined)
    const persist = useCallback((a: Appearance, now = false) => {
        window.clearTimeout(timer.current)
        const run = () =>
            SaveAppearance(storeModels.Appearance.createFrom(a)).catch((e) => toast.error(errorText(e)))
        if (now) void run()
        else timer.current = window.setTimeout(run, 300)
    }, [])
    useEffect(() => () => window.clearTimeout(timer.current), [])

    const change = useCallback(
        (patch: Partial<Appearance>, now = false) => {
            const next = normalizeAppearance({...current.current, ...patch})
            current.current = next
            setValue(next)
            applyAppearance(next)
            persist(next, now)
        },
        [persist],
    )
    const reset = useCallback(() => change(APPEARANCE_DEFAULTS, true), [change])

    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (!isMod(e) || e.altKey) return
            let dir: 1 | -1 | 0 | null = null
            if (e.key === '+' || e.key === '=' || e.code === 'NumpadAdd') dir = 1
            else if (e.key === '-' || e.key === '_' || e.code === 'NumpadSubtract') dir = -1
            else if (e.key === '0' || e.code === 'Numpad0') dir = 0
            if (dir === null) return
            e.preventDefault()
            e.stopPropagation()
            const area = (document.activeElement as HTMLElement | null)?.closest<HTMLElement>('[data-font-area]')?.dataset.fontArea
            const a = current.current
            const tt = tNow().appearance.zoom
            const font = (key: 'editorFontSize' | 'terminalFontSize' | 'chatFontSize', msg: string) => {
                const next = dir === 0 ? APPEARANCE_DEFAULTS[key] : normalizeFont(Math.max(FONT_MIN, Math.min(FONT_MAX, a[key] + dir)))
                if (next === a[key]) return
                change({[key]: next}, true)
                toast.ok(fmt(msg, {px: next}), 'zoom')
            }
            if (area === 'editor') font('editorFontSize', tt.editor)
            else if (area === 'terminal') font('terminalFontSize', tt.terminal)
            else if (area === 'chat') font('chatFontSize', tt.chat)
            else {
                const next = dir === 0 ? APPEARANCE_DEFAULTS.uiScale : stepScale(a.uiScale, dir)
                if (next === a.uiScale) return
                change({uiScale: next}, true)
                toast.ok(fmt(tt.ui, {pct: next}), 'zoom')
            }
        }
        // En captura, para ganarle a Monaco y a xterm, que también miran el teclado.
        window.addEventListener('keydown', onKey, true)
        return () => window.removeEventListener('keydown', onKey, true)
    }, [change])

    return {appearance: value, change, reset}
}
