// Aplica el tema elegido. "system" sigue al sistema operativo en vivo.
let mql: MediaQueryList | null = null
let onChange: (() => void) | null = null

export function applyTheme(theme: string) {
    const root = document.documentElement
    if (mql && onChange) mql.removeEventListener('change', onChange)
    mql = null
    onChange = null
    if (theme === 'system') {
        mql = window.matchMedia('(prefers-color-scheme: dark)')
        onChange = () => root.classList.toggle('dark', !!mql?.matches)
        mql.addEventListener('change', onChange)
        onChange()
        return
    }
    root.classList.toggle('dark', theme !== 'light')
}
