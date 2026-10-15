// Un error de Go cruza el binding de Wails como string. El backend puede
// anteponer un código entre dos U+2063 (ver backend/i18n): se quita para mostrar.
const MARK = '⁣'

export function errorText(e: unknown): string {
    const s = e instanceof Error ? e.message : String(e)
    return s.replace(new RegExp(`${MARK}[^${MARK}]*${MARK}`), '')
}

export function errorCode(e: unknown): string {
    const s = e instanceof Error ? e.message : String(e)
    const m = s.match(new RegExp(`${MARK}([^${MARK}]*)${MARK}`))
    return m ? m[1] : ''
}
