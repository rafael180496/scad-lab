import {useState} from 'react'
import {CloudOff, RefreshCw} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import type {Health} from '../../lib/chatHealth'

// Cómo se dibuja la salud de un proveedor del chat (ver lib/chatHealth):
//
//   HealthDot     el punto: verde responde, rojo no, gris sin dato o comprobando
//                 (late). Va sobre el ícono del proveedor en el selector.
//   HealthPill    punto + latencia («18 ms»), «Instalado» para un CLI o «Sin
//                 respuesta»; un clic vuelve a comprobar.
//   HealthBanner  el aviso sobre la caja del chat cuando el proveedor no
//                 contesta. No bloquea escribir: enviar falla enseguida solo.
//
// El color nunca es lo único que informa: el punto lleva `aria-label` y la
// píldora un `title` con el detalle (latencia y antigüedad, o el error entero).

const DOT: Record<Health['status'], string> = {
    ok: 'bg-ok',
    error: 'bg-danger',
    checking: 'bg-faint animate-pulse',
    unknown: 'bg-faint/60',
}

export function statusText(t: ReturnType<typeof useT>, status: Health['status']): string {
    const h = t.chat.health
    return status === 'ok' ? h.statusOk : status === 'error' ? h.statusError : status === 'checking' ? h.statusChecking : h.statusUnknown
}

// Texto completo de la salud: el `title` de la píldora y de los puntos.
export function healthSummary(t: ReturnType<typeof useT>, name: string, health: Health, now: number, cli: boolean): string {
    const h = t.chat.health
    switch (health.status) {
        case 'ok':
            return cli
                ? fmt(h.dotOkCli, {name})
                : fmt(h.dotOk, {name, latency: health.latencyMs, ago: Math.max(0, Math.round((now - health.checkedAt) / 1000))})
        case 'error':
            return fmt(h.dotError, {name, error: health.error})
        case 'checking':
            return fmt(h.dotChecking, {name})
        default:
            return fmt(h.dotUnknown, {name})
    }
}

export function HealthDot({health, name, overlay, className}: {health: Health | null; name: string; overlay?: boolean; className?: string}) {
    const t = useT()
    const status = health?.status ?? 'unknown'
    return (
        <span
            role="img"
            aria-label={`${name}: ${statusText(t, status)}`}
            data-health={status}
            className={cn(
                'inline-block size-2 shrink-0 rounded-full',
                DOT[status],
                // Pegado a la esquina del ícono del proveedor, con un anillo del color del fondo.
                overlay && 'absolute -right-0.5 -bottom-0.5 size-[7px] ring-2 ring-raised',
                className,
            )}
        />
    )
}

export function HealthPill({health, name, cli, onRetry, className}: {health: Health; name: string; cli: boolean; onRetry: () => void; className?: string}) {
    const t = useT()
    const h = t.chat.health
    // «Comprobado hace Xs» envejece: se recalcula al pasar el mouse, que es cuando
    // alguien lo lee, en vez de mantener un reloj por cada píldora.
    const [now, setNow] = useState(() => Date.now())
    const text =
        health.status === 'ok'
            ? cli
                ? h.pillCli
                : fmt(h.pillOk, {latency: health.latencyMs})
            : health.status === 'error'
              ? h.pillError
              : health.status === 'checking'
                ? h.pillChecking
                : h.statusUnknown
    return (
        <button
            type="button"
            data-health-pill={health.status}
            title={`${healthSummary(t, name, health, now, cli)}\n${h.recheckHint}`}
            onMouseEnter={() => setNow(Date.now())}
            onClick={onRetry}
            className={cn(
                'no-drag flex h-7 shrink-0 items-center gap-1.5 rounded-md border px-2 font-mono text-ui-11 transition-colors',
                health.status === 'error' ? 'border-danger/40 bg-danger/10 text-danger hover:bg-danger/15' : 'border-line bg-surface-2 text-muted hover:border-line-strong hover:text-fg',
                className,
            )}
        >
            <HealthDot health={health} name={name} />
            <span className="tabular-nums">{text}</span>
        </button>
    )
}

export function HealthBanner({health, name, onRetry}: {health: Health; name: string; onRetry: () => void}) {
    const t = useT()
    const h = t.chat.health
    const checking = health.status === 'checking'
    return (
        <div data-health-banner role="alert" className="flex shrink-0 items-start gap-2 border-t border-danger/40 bg-danger/10 px-3 py-1.5 text-ui-11-5 text-fg">
            <CloudOff className="mt-px icon-ui-14 shrink-0 text-danger" />
            <span className="min-w-0 flex-1">
                <span className="block font-medium text-danger">{fmt(h.bannerTitle, {name})}</span>
                <span className="selectable block break-words">{health.error}</span>
                <span className="block text-muted">{h.bannerHint}</span>
            </span>
            <button
                type="button"
                onClick={onRetry}
                disabled={checking}
                title={h.retryHint}
                className="no-drag flex shrink-0 items-center gap-1 rounded-md border border-line-strong px-1.5 py-0.5 text-muted hover:text-fg disabled:opacity-50"
            >
                <RefreshCw className={cn('icon-ui-12', checking && 'animate-spin')} />
                {checking ? h.retrying : h.retry}
            </button>
        </div>
    )
}
