import type {ITheme} from '@xterm/xterm'

// Tema de xterm.js a partir de los tokens de index.css (nunca colores sueltos):
// así la terminal sigue al modo claro/oscuro como el resto de la interfaz. Los
// tokens son hexadecimales; los dos matices que el tema no tiene (magenta y
// cian) se mezclan de los que sí tiene, para que un `ls --color` o el diff de
// un agente se vea con la misma paleta que la app.

function rgb(hex: string): [number, number, number] | null {
    const m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(hex.trim())
    if (!m) return null
    const h = m[1].length === 3 ? [...m[1]].map((c) => c + c).join('') : m[1]
    return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)]
}

// mix mezcla dos colores #rrggbb (t = 0 → a, t = 1 → b). Si alguno no es un
// hexadecimal (un token redefinido con otra sintaxis), devuelve `a` tal cual.
export function mix(a: string, b: string, t: number): string {
    const x = rgb(a)
    const y = rgb(b)
    if (!x || !y) return a
    const c = x.map((v, i) => Math.round(v + (y[i] - v) * t))
    return '#' + c.map((v) => v.toString(16).padStart(2, '0')).join('')
}

function token(css: CSSStyleDeclaration, name: string, fallback: string): string {
    return css.getPropertyValue(name).trim() || fallback
}

export function terminalTheme(): ITheme {
    const css = getComputedStyle(document.documentElement)
    const bg = token(css, '--c-surface', '#14171e')
    const fg = token(css, '--c-fg', '#e7e9ee')
    const muted = token(css, '--c-muted', '#9aa1ae')
    const faint = token(css, '--c-faint', '#6b7280')
    const accent = token(css, '--c-accent', '#ff7a1a')
    const red = token(css, '--c-danger', '#ff5c48')
    const green = token(css, '--c-ok', '#3ccf7a')
    const yellow = token(css, '--c-warn', '#f2b84b')
    const blue = token(css, '--c-info', '#5aa2ff')
    const magenta = mix(red, blue, 0.5)
    const cyan = mix(green, blue, 0.5)
    // Los «brillantes» se acercan al color del texto: más claros en oscuro,
    // más oscuros en claro, siempre legibles sobre el fondo.
    const bright = (c: string) => mix(c, fg, 0.28)
    return {
        background: bg,
        foreground: fg,
        cursor: accent,
        cursorAccent: bg,
        selectionBackground: mix(bg, accent, 0.38),
        black: token(css, '--c-line-strong', '#343c4b'),
        red,
        green,
        yellow,
        blue,
        magenta,
        cyan,
        white: muted,
        brightBlack: faint,
        brightRed: bright(red),
        brightGreen: bright(green),
        brightYellow: bright(yellow),
        brightBlue: bright(blue),
        brightMagenta: bright(magenta),
        brightCyan: bright(cyan),
        brightWhite: fg,
    }
}

// monoFont es la familia monoespaciada del tema (--font-mono) lista para
// xterm, que no entiende `var()`.
export function monoFont(): string {
    return getComputedStyle(document.documentElement).getPropertyValue('--font-mono').trim() || 'monospace'
}
