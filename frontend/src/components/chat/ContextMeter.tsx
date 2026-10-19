import {Gauge} from 'lucide-react'
import {fmt, locale, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {compactTokens, type ContextUsage} from '../../lib/chatContext'

// Contador de contexto de la caja de mensaje: tokens estimados del borrador más la
// conversación, contra la ventana del modelo si se conoce. Es una estimación (4
// caracteres ≈ 1 token) y el `title` lo dice: no incluye las instrucciones del
// sistema ni los adjuntos, y un CLI puede resumir su propio historial. Sirve para
// ver que la conversación se acerca al límite, no para contar con exactitud.
export function ContextMeter({usage, className}: {usage: ContextUsage; className?: string}) {
    const t = useT()
    const c = t.chat.context
    const loc = locale()
    const used = compactTokens(usage.used, loc)
    const known = usage.limit > 0
    const pct = Math.min(100, Math.round(usage.ratio * 100))
    const title = known
        ? fmt(c.hintKnown, {used: usage.used.toLocaleString(loc), limit: usage.limit.toLocaleString(loc), pct: Math.round(usage.ratio * 100)})
        : fmt(c.hintUnknown, {used: usage.used.toLocaleString(loc)})
    return (
        <span
            data-testid="context-meter"
            data-level={usage.level}
            title={`${title}\n${c.note}${usage.level === 'full' ? `\n${c.full}` : usage.level === 'warn' ? `\n${c.warn}` : ''}`}
            className={cn(
                'flex shrink-0 items-center gap-1.5 font-mono text-ui-10-5 tabular-nums',
                usage.level === 'full' ? 'text-danger' : usage.level === 'warn' ? 'text-warn' : 'text-faint',
                className,
            )}
        >
            <Gauge className="icon-ui-12 shrink-0" />
            <span>{known ? fmt(c.usedOf, {used, limit: compactTokens(usage.limit, loc)}) : fmt(c.usedOnly, {used})}</span>
            {known ? (
                <span aria-hidden className="h-1 w-8 overflow-hidden rounded-full bg-surface-2">
                    <span className={cn('block h-full rounded-full', usage.level === 'full' ? 'bg-danger' : usage.level === 'warn' ? 'bg-warn' : 'bg-muted')} style={{width: `${pct}%`}} />
                </span>
            ) : null}
        </span>
    )
}
