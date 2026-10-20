import {Gauge, Loader2, MessageSquarePlus, Wrench, X} from 'lucide-react'
import {compactTokens, type ContextAdvice} from '../../lib/chatContext'
import {fmt, locale, useT} from '../../i18n'
import {Button, IconButton} from '../ui/Button'

// Aviso sobre la caja de mensaje (S17) cuando el contexto se acaba. Dos casos, nunca los dos a la vez:
//
//  - la conversación ya ocupa el 70 % de la ventana o más → «Nueva conversación con resumen»: el modelo
//    escribe un resumen corto y se sigue en una conversación nueva que empieza con él (la actual se conserva);
//  - el prompt y las herramientas solos pasan del 60 % → se dice, con el atajo a las herramientas esenciales
//    (un modelo chico con el catálogo completo apenas tiene sitio para trabajar).
//
// Se descarta con la X para esa conversación.
export function ContextNotice({
    advice,
    busy,
    onSummarize,
    onNarrow,
    onDismiss,
}: {
    advice: ContextAdvice
    busy: boolean
    onSummarize: () => void
    onNarrow: () => void
    onDismiss: () => void
}) {
    const c = useT().chat.contextNotice
    const loc = locale()
    return (
        <div
            role="status"
            data-testid="context-notice"
            data-kind={advice.kind}
            className="mx-3 mb-1.5 flex items-start gap-2 rounded-lg border border-warn/50 bg-warn/10 px-2.5 py-2 text-ui-12 text-fg"
        >
            <Gauge className="mt-0.5 icon-ui-14 shrink-0 text-warn" />
            <p className="min-w-0 flex-1 leading-snug">
                {advice.kind === 'summary'
                    ? fmt(c.summaryText, {pct: advice.pct})
                    : fmt(advice.canNarrow ? c.overText : c.overTextEssential, {pct: advice.pct, limit: compactTokens(advice.limit, loc), used: compactTokens(advice.used, loc)})}
            </p>
            {advice.kind === 'summary' ? (
                <Button variant="primary" data-testid="context-summary" title={c.summaryHint} disabled={busy} className="shrink-0" onClick={onSummarize}>
                    {busy ? <Loader2 className="icon-ui-14 animate-spin" /> : <MessageSquarePlus className="icon-ui-14" />}
                    {busy ? c.summarizing : c.summaryButton}
                </Button>
            ) : advice.canNarrow ? (
                <Button variant="primary" data-testid="context-narrow" title={c.overHint} className="shrink-0" onClick={onNarrow}>
                    <Wrench className="icon-ui-14" />
                    {c.overButton}
                </Button>
            ) : null}
            <IconButton title={c.dismiss} className="-my-1 -mr-1 size-6" onClick={onDismiss}>
                <X className="icon-ui-14" />
            </IconButton>
        </div>
    )
}
