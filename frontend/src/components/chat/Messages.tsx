import {Fragment, memo, useEffect, useMemo, useRef} from 'react'
import {CircleAlert, Loader2, X} from 'lucide-react'
import type {store} from '../../../wailsjs/go/models'
import {fmt, locale, useT} from '../../i18n'
import {interleave, parseParts, toUsage, type ChatStep, type ChatUsage} from '../../lib/chatTypes'
import {hasUsage, usagePieces} from '../../lib/chatContext'
import {createdCheckpoint} from '../../lib/chatSteps'
import {IconButton} from '../ui/Button'
import {MessageAttachments} from './AttachmentChips'
import type {ChatActions, CodeActions} from './CodeBlock'
import {Markdown} from './Markdown'
import {ToolCallCard} from './ToolCallCard'
import {dismissError, useStreamError, useStreamMessageId, useStreamRunning, useStreamSteps, useStreamText, useStreamUsage} from './chatStreams'

// Tiempo y tokens de un turno, al pie de la respuesta (S17): «Contexto máx. 12k · ↑ 48k en 5 vueltas · ↓ 2k ·
// modelo 41 s · herramientas 6 s». ↑ es la SUMA de la entrada de todas las vueltas, no el contexto, y el
// `title` lo explica (lib/chatContext.ts). Solo se muestra lo que el proveedor informó. El costo solo existe si
// el proveedor publica precios (OpenRouter): sin ellos queda el aviso «no informado» en el título en vez de
// un «$0» que se leería como gratis.
function UsageLine({usage, model, stepLimit}: {usage: ChatUsage | null; model?: string; stepLimit?: boolean}) {
    const t = useT()
    const c = t.chat.usage
    if (!hasUsage(usage)) return null
    const loc = locale()
    const pieces = usagePieces(usage, c, loc)
    const cost = usage.costUsd > 0 ? (usage.costUsd < 0.01 ? `< $0.01` : `$${usage.costUsd.toFixed(2)}`) : ''
    const n = (v: number) => v.toLocaleString(loc)
    const lines = [
        usage.peakInput > 0 ? fmt(usage.inputEstimated ? c.hintContextEstimated : c.hintContext, {n: n(usage.peakInput)}) : '',
        usage.inputTokens > 0 ? fmt(usage.rounds > 1 ? c.hintInput : c.hintInputOne, {input: n(usage.inputTokens), rounds: usage.rounds}) : '',
        usage.outputTokens > 0 ? fmt(c.hintOutput, {output: n(usage.outputTokens)}) : '',
        usage.modelMs > 0 || usage.toolMs > 0 ? c.hintTime : '',
        fmt(c.hintCost, {cost: cost || c.costUnknown}),
    ].filter(Boolean)
    const items = [
        ...pieces.map((text) => ({text, className: undefined as string | undefined})),
        ...(cost ? [{text: cost, className: undefined}] : []),
        ...(model ? [{text: model, className: 'max-w-[16rem] truncate'}] : []),
        ...(stepLimit ? [{text: c.stepLimit, className: 'text-warn'}] : []),
    ]
    return (
        <p data-testid="usage-line" className="mt-1 flex flex-wrap items-center gap-x-1.5 font-mono text-ui-10-5 text-faint tabular-nums" title={lines.join('\n')}>
            {items.map((it, i) => (
                <Fragment key={i}>
                    {i > 0 ? <span aria-hidden>·</span> : null}
                    <span className={it.className}>{it.text}</span>
                </Fragment>
            ))}
        </p>
    )
}

// El turno del asistente: el texto con las tarjetas de paso intercaladas donde
// ocurrieron. Mientras llega no se ofrece «Guardar como checkpoint» (el código
// está a medias): `actions` viene undefined.
//
// Si el turno ya dejó un checkpoint (una herramienta, o el «Guardado automáticamente» de ScadLab), el bloque de código que
// repite el diseño no ofrece guardarlo otra vez: sería un duplicado a un clic.
const ONLY_OPEN: CodeActions = {}
function Turn({text, steps, actions, onOpenCheckpoint}: {text: string; steps: ChatStep[]; actions?: CodeActions; onOpenCheckpoint?: (file: string) => void}) {
    const items = useMemo(() => interleave(text, steps), [text, steps])
    const created = useMemo(() => steps.some((s) => createdCheckpoint(s) !== ''), [steps])
    if (created && actions) actions = ONLY_OPEN
    return (
        <>
            {items.map((it, i) =>
                it.type === 'text' ? (
                    <Markdown key={i} source={it.text} actions={actions} />
                ) : (
                    <div key={i}>
                        {it.steps.map((s) => (
                            <ToolCallCard key={s.id} step={s} onOpenCheckpoint={onOpenCheckpoint} />
                        ))}
                    </div>
                ),
            )}
        </>
    )
}

// Mensaje ya guardado. memo: terminada la respuesta, escribir en el cuadro de
// abajo o recibir trozos de otra no vuelve a pintar el historial.
const MessageBubble = memo(function MessageBubble({message, actions}: {message: store.ChatMessage; actions: ChatActions}) {
    const parts = useMemo(() => parseParts(message.parts), [message.parts])
    if (message.role === 'user') {
        return (
            <div className="flex flex-col items-end">
                {parts.attachments?.length ? <MessageAttachments attachments={parts.attachments} /> : null}
                {message.content.trim() ? (
                    <div className="selectable max-w-[85%] rounded-lg border border-line bg-accent-soft px-3 py-2 text-chat leading-relaxed break-words whitespace-pre-wrap text-fg">
                        {message.content}
                    </div>
                ) : null}
            </div>
        )
    }
    // Lo guardado en la fila (tokens y costo) y, desde S17, el tiempo y el contexto máximo de los `parts`.
    const usage = message.inputTokens || message.outputTokens ? toUsage({...parts.usage, inputTokens: message.inputTokens, outputTokens: message.outputTokens, costUsd: message.costUsd}) : null
    return (
        <div>
            <Turn text={message.content} steps={parts.steps ?? []} actions={actions} onOpenCheckpoint={actions.onOpenCheckpoint} />
            <UsageLine usage={usage} model={parts.model} stepLimit={parts.stepLimit} />
        </div>
    )
})

// La respuesta que está llegando. Es el único componente suscrito al texto en
// vivo: cada trozo redibuja esta burbuja —y dentro de ella, solo el último
// bloque— y nada más de la conversación.
function StreamingBubble({chatId, actions}: {chatId: number; actions: ChatActions}) {
    const t = useT()
    const text = useStreamText(chatId)
    const steps = useStreamSteps(chatId)
    const usage = useStreamUsage(chatId)
    const running = useStreamRunning(chatId)
    // Entre dos pasos el modelo está pensando la siguiente vuelta: sin un aviso
    // la pantalla parecería detenida justo cuando más tarda. Solo mientras no
    // llega texto nuevo después del último paso (si escribe, ya se ve que avanza).
    const last = steps.at(-1)
    const busy = running && !!last && last.status !== 'running' && text.length <= last.textAt
    if (!text && steps.length === 0) {
        return running ? (
            <p className="flex items-center gap-2 text-ui-12-5 text-muted" role="status">
                <Loader2 className="icon-ui-14 animate-spin" />
                {t.chat.thinking}
            </p>
        ) : null
    }
    return (
        <div>
            <Turn text={text} steps={steps} actions={running ? undefined : actions} onOpenCheckpoint={actions.onOpenCheckpoint} />
            {busy ? (
                <p className="mt-1 flex items-center gap-2 text-ui-12 text-muted" role="status">
                    <Loader2 className="icon-ui-14 animate-spin" />
                    {t.chat.thinking}
                </p>
            ) : null}
            <UsageLine usage={usage} />
        </div>
    )
}

function ErrorBanner({chatId}: {chatId: number}) {
    const t = useT()
    const error = useStreamError(chatId)
    if (!error) return null
    return (
        <div role="alert" className="selectable flex items-start gap-2 rounded-lg border border-danger/40 bg-danger/10 px-3 py-2 text-ui-12-5 text-fg">
            <CircleAlert className="mt-0.5 icon-ui-14 shrink-0 text-danger" />
            <span className="min-w-0 flex-1 break-words">{error}</span>
            <IconButton title={t.chat.dismiss} className="-my-1 -mr-1.5 size-6" onClick={() => dismissError(chatId)}>
                <X className="icon-ui-14" />
            </IconButton>
        </div>
    )
}

// Lista de la conversación con auto-scroll «pegado al fondo»: sigue la
// respuesta mientras el usuario no haya subido a leer algo; si subió, no lo
// arrastra de vuelta. Se mide el contenido con un ResizeObserver y no con el
// texto, así también acompaña el crecimiento de un bloque de código o de una
// tarjeta de paso que se despliega.
export function MessageList({
    chatId,
    messages,
    actions,
    empty,
}: {
    chatId: number | null
    messages: store.ChatMessage[]
    actions: ChatActions
    empty: React.ReactNode
}) {
    const box = useRef<HTMLDivElement>(null)
    const inner = useRef<HTMLDivElement>(null)
    const stick = useRef(true)
    const running = useStreamRunning(chatId)
    const text = useStreamText(chatId)
    const steps = useStreamSteps(chatId)
    const streamMessageId = useStreamMessageId(chatId)

    // Una conversación vacía muestra el estado vacío desde ARRIBA: pegarlo al fondo (como se hace con una
    // respuesta que crece) dejaría cortado el título justo en un panel bajo.
    const emptyRef = useRef(false)

    useEffect(() => {
        stick.current = true
        const el = box.current
        if (el) el.scrollTop = emptyRef.current ? 0 : el.scrollHeight
    }, [chatId])

    useEffect(() => {
        const el = box.current
        const content = inner.current
        if (!el || !content) return
        const ro = new ResizeObserver(() => {
            if (emptyRef.current) el.scrollTop = 0
            else if (stick.current) el.scrollTop = el.scrollHeight
        })
        ro.observe(content)
        return () => ro.disconnect()
    }, [])

    // Texto o pasos que terminaron pero todavía no están en `messages` (se está
    // recargando) o que quedaron sin guardar por un error: se siguen mostrando.
    const hasStream = text !== '' || steps.length > 0
    const showStream = chatId != null && (running || (hasStream && !messages.some((m) => m.id === streamMessageId)))
    emptyRef.current = messages.length === 0 && !showStream

    return (
        <div
            ref={box}
            data-chat-scroll
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
