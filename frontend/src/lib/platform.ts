// Plataforma de la app (boot.platform) para los rótulos de los atajos: ⌘ en
// macOS, Ctrl en Windows. Se fija una vez al arrancar (App) y se lee desde
// cualquier lado, incluidos los `title` armados fuera de React.
let mac = typeof navigator !== 'undefined' && /mac/i.test(navigator.platform)

export function setPlatform(platform: string) {
    mac = platform === 'darwin'
}

export function isMac(): boolean {
    return mac
}

const macSymbols: Record<string, string> = {mod: '⌘', shift: '⇧', alt: '⌥', ctrl: '⌃'}
const winNames: Record<string, string> = {mod: 'Ctrl', shift: 'Shift', alt: 'Alt', ctrl: 'Ctrl'}

// keys('mod+shift+s') → '⌘⇧S' en macOS, 'Ctrl+Shift+S' en Windows.
export function keys(combo: string): string {
    const parts = combo.split('+').map((p) => p.trim().toLowerCase())
    const key = parts.pop() ?? ''
    const label = key.length === 1 ? key.toUpperCase() : key[0].toUpperCase() + key.slice(1)
    if (mac) return parts.map((p) => macSymbols[p] ?? p).join('') + label
    return [...parts.map((p) => winNames[p] ?? p), label].join('+')
}

// withKeys agrega el atajo al final de un `title`: «Guardar (⌘S)».
export function withKeys(title: string, combo: string): string {
    return `${title} (${keys(combo)})`
}

// isMod dice si el evento lleva el modificador principal de la plataforma.
export function isMod(e: KeyboardEvent | React.KeyboardEvent): boolean {
    return mac ? e.metaKey : e.ctrlKey
}
