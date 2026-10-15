import {useEffect, useRef} from 'react'
import {Terminal} from '@xterm/xterm'
import {FitAddon} from '@xterm/addon-fit'
import {WebLinksAddon} from '@xterm/addon-web-links'
import '@xterm/xterm/css/xterm.css'
import './terminal.css'
import {ReadTerminalBuffer, ResizeTerminal, WriteTerminal} from '../../../wailsjs/go/main/App'
import {BrowserOpenURL} from '../../../wailsjs/runtime/runtime'
import {useThemeColors} from '../../hooks/useThemeColors'
import {isMac} from '../../lib/platform'
import {t as tNow} from '../../i18n'
import {base64ToBytes, onTerminalData, onTerminalExit, type TerminalDataEvent} from './events'
import {monoFont, terminalTheme} from './termTheme'

// Una terminal xterm.js enganchada a un PTY del backend.
//
// El PROCESO vive en Go y sobrevive a este componente: desmontarlo (cambiar de
// workspace, recargar la UI) no mata al agente. Por eso al montar se reengancha:
// primero se pide la salida reciente guardada en Go (ReadTerminalBuffer) y
// después se sigue en vivo. Los dos caminos se unen con `seq` — el desplazamiento
// en bytes de cada trozo — para no repetir ni perder nada si un evento llega
// mientras se pide la captura.
export function TerminalView({
    termId,
    visible,
    fontSize = 12,
    onExit,
}: {
    termId: string
    visible: boolean
    fontSize?: number
    onExit?: (code: number) => void
}) {
    const box = useRef<HTMLDivElement>(null)
    const term = useRef<Terminal | null>(null)
    const fit = useRef<FitAddon | null>(null)
    const shown = useRef(visible)
    shown.current = visible
    const exit = useRef(onExit)
    exit.current = onExit
    const colors = useThemeColors() // solo para repintar al cambiar claro/oscuro

    // Mide la caja y avisa al PTY. Oculta (display:none) mediría 0 columnas y
    // dejaría al agente dibujando sobre un tamaño absurdo.
    const refit = useRef(() => {})
    refit.current = () => {
        const el = box.current
        const f = fit.current
        const x = term.current
        if (!el || !f || !x || !shown.current || el.clientWidth < 20 || el.clientHeight < 20) return
        const before = `${x.cols}x${x.rows}`
        f.fit()
        if (`${x.cols}x${x.rows}` !== before) void ResizeTerminal(termId, x.cols, x.rows).catch(() => {})
    }

    useEffect(() => {
        const el = box.current
        if (!el) return
        const x = new Terminal({
            fontFamily: monoFont(),
            fontSize,
            lineHeight: 1.15,
            cursorBlink: true,
            // Una conversación larga con un agente: que se pueda volver atrás.
            scrollback: 8000,
            theme: terminalTheme(),
        })
        const f = new FitAddon()
        x.loadAddon(f)
        // Los enlaces se abren en el navegador del sistema: window.open dentro
        // de la webview de Wails abriría otra ventana de la propia app.
        x.loadAddon(new WebLinksAddon((_e, uri) => /^https?:\/\//i.test(uri) && BrowserOpenURL(uri)))
        x.open(el)
        // El margen interior va en el propio .xterm: FitAddon lo descuenta al medir;
        // en el contenedor lo ignoraría y la última columna se cortaría.
        if (x.element) x.element.style.padding = '4px 8px'
        term.current = x
        fit.current = f

        // Copiar y pegar. Ctrl+C sin selección TIENE que llegar al proceso (es
        // el ^C que interrumpe al agente); con selección copia. Ctrl+V no debe
        // mandar el ^V crudo: se deja pasar el evento `paste` nativo, que xterm
        // convierte en un pegado entre marcas (bracketed paste) — así un texto
        // de varias líneas no se ejecuta línea por línea en el CLI.
        x.attachCustomKeyEventHandler((e) => {
            if (e.type !== 'keydown') return true
            const mod = isMac() ? e.metaKey : e.ctrlKey
            if (!mod) return true
            const key = e.key.toLowerCase()
            if (key === 'c' && (x.hasSelection() || e.shiftKey)) {
                if (x.hasSelection()) void navigator.clipboard.writeText(x.getSelection()).catch(() => {})
                return false
            }
            if (key === 'v') return false
            return true
        })

        const input = x.onData((data) => {
            void WriteTerminal(termId, data).catch(() => {})
        })

        // ── Reenganche: captura + eventos en vivo, unidos por seq ──────────
        let live = false
        let next = 0 // desplazamiento esperado del próximo byte
        const pending: TerminalDataEvent[] = []
        const apply = (ev: TerminalDataEvent) => {
            const bytes = base64ToBytes(ev.data)
            const end = ev.seq + bytes.length
            if (end <= next) return // ya estaba en la captura
            x.write(ev.seq < next ? bytes.subarray(next - ev.seq) : bytes)
            next = end
        }
        const offData = onTerminalData((ev) => {
            if (ev.id !== termId) return
            if (live) apply(ev)
            else pending.push(ev)
        })
        const offExit = onTerminalExit((ev) => {
            if (ev.id !== termId) return
            x.write(`\r\n\x1b[90m${tNow().terminal.processEnded.replace('{code}', String(ev.code))}\x1b[0m\r\n`)
            exit.current?.(ev.code)
        })
        const goLive = () => {
            live = true
            for (const ev of pending.splice(0)) apply(ev)
            refit.current()
        }
        ReadTerminalBuffer(termId)
            .then((snap) => {
                x.write(base64ToBytes(snap.data))
                next = snap.seq
            })
            // El proceso ya no existe (terminó antes de reenganchar): no hay
            // nada que recuperar y los eventos en vivo no van a llegar.
            .catch(() => {})
            .finally(goLive)

        const ro = new ResizeObserver(() => refit.current())
        ro.observe(el)
        return () => {
            ro.disconnect()
            offData()
            offExit()
            input.dispose()
            x.dispose()
            term.current = null
            fit.current = null
        }
        // fontSize se aplica en su propio efecto: recrear la terminal borraría
        // lo que hay en pantalla.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [termId])

    useEffect(() => {
        if (term.current) term.current.options.theme = terminalTheme()
    }, [colors])

    useEffect(() => {
        if (term.current) term.current.options.fontSize = fontSize
        refit.current()
    }, [fontSize])

    // Al volver a la vista: medir (el ResizeObserver no dispara si el tamaño no
    // cambió) y devolver el foco, para escribir sin hacer clic primero.
    useEffect(() => {
        if (!visible) return
        refit.current()
        term.current?.focus()
    }, [visible])

    return <div ref={box} className="scadlab-term h-full w-full overflow-hidden bg-surface" />
}
