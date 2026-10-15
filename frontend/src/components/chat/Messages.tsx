import {memo, useEffect, useRef} from 'react'
import {CircleAlert, Loader2, X} from 'lucide-react'
import type {store} from '../../../wailsjs/go/models'
import {useT} from '../../i18n'
import {IconButton} from '../ui/Button'
import type {CodeActions} from './CodeBlock'
import {Markdown} from './Markdown'
import {dismissError, useStreamError, useStreamMessageId, useStreamRunning, useStreamText} from './chatStreams'

// Mensaje ya guardado. memo: terminada la respuesta, escribir en el cuadro de
// abajo o recibir trozos de otra no vuelve a pintar el historial.
const MessageBubble = memo(function MessageBubble({message, actions}: {message: store.ChatMessage; actions: CodeActions}) {
    if (message.role === 'user') {
        return (
            <div className="flex justify-end">
                <div className="selectable max-w-[85%] rounded-lg border border-line bg-accent-soft px-3 py-2 text-[13px] leading-relaxed break-words whitespace-pre-wrap text-fg">
                    {message.content}
                </div>
            </div>
        )
    }
    return <Markdown source={message.content} actions={actions} />
})

// La respuesta que está llegando. Es el único componente suscrito al texto en
// vivo: cada trozo redibuja esta burbuja —y dentro de ella, solo el último
// bloque— y nada más de la conversación.
function StreamingBubble({chatId, actions}: {chatId: number; actions: CodeActions}) {
    const t = useT()
    const text = useStreamText(chatId)
    const running = useStreamRunning(chatId)
    if (!text) {
        return running ? (
            <p className="flex items-center gap-2 text-[12.5px] text-muted" role="status">
                <Loader2 className="size-3.5 animate-spin" />
                {t.chat.thinking}
            </p>
        ) : null
    }
    // Mientras llega no se ofrece guardar: el código está a medias.
    return <Markdown source={text} actions={running ? undefined : actions} />
}

function ErrorBanner({chatId}: {chatId: number}) {
    const t = useT()
    const error = useStreamError(chatId)
    if (!error) return null
    return (
        <div role="alert" className="selectable flex items-start gap-2 rounded-lg border border-danger/40 bg-danger/10 px-3 py-2 text-[12.5px] text-fg">
            <CircleAlert className="mt-0.5 size-3.5 shrink-0 text-danger" />
            <span className="min-w-0 flex-1 break-words">{error}</span>
            <IconButton title={t.chat.dismiss} className="-my-1 -mr-1.5 size-6" onClick={() => dismissError(chatId)}>
                <X className="size-3.5" />
            </IconButton>
        </div>
    )
}

// Lista de la conversación con auto-scroll «pegado al fondo»: sigue la
// respuesta mientras el usuario no haya subido a leer algo; si subió, no lo
// arrastra de vuelta. Se mide el contenido con un ResizeObserver y no con el
// texto, así también acompaña el crecimiento de un bloque de código.
export function MessageList({
    chatId,
    messages,
    actions,
    empty,
}: {
    chatId: number | null
    messages: store.ChatMessage[]
    actions: CodeActions
    empty: React.ReactNode
}) {
    const box = useRef<HTMLDivElement>(null)
    const inner = useRef<HTMLDivElement>(null)
    const stick = useRef(true)
    const running = useStreamRunning(chatId)
    const text = useStreamText(chatId)
    const streamMessageId = useStreamMessageId(chatId)

    useEffect(() => {
        stick.current = true
        const el = box.current
        if (el) el.scrollTop = el.scrollHeight
    }, [chatId])

    useEffect(() => {
        const el = box.current
        const content = inner.current
        if (!el || !content) return
        const ro = new ResizeObserver(() => {
            if (stick.current) el.scrollTop = el.scrollHeight
        })
        ro.observe(content)
        return () => ro.disconnect()
    }, [])

    // Texto que terminó pero todavía no está en `messages` (se está recargando)
    // o que quedó sin guardar por un error: se sigue mostrando.
    const showStream = chatId != null && (running || (text !== '' && !messages.some((m) => m.id === streamMessageId)))

    return (
        <div
            ref={box}
            onScroll={(e) => {
                const el = e.currentTarget
                stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 48
            }}
            className="min-h-0 flex-1 overflow-y-auto"
        >
            <div ref={inner} className="flex min-h-full flex-col gap-3 px-4 py-3">
                {messages.length === 0 && !showStream ? (
                    empty
                ) : (
                    <>
                        {messages.map((m) => (
                            <MessageBubble key={m.id} message={m} actions={actions} />
                        ))}
                        {chatId != null && showStream ? <StreamingBubble chatId={chatId} actions={actions} /> : null}
                    </>
                )}
                {chatId != null ? <ErrorBanner chatId={chatId} /> : null}
            </div>
        </div>
    )
}
