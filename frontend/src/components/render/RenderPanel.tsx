import {useState} from 'react'
import {
    ChevronDown,
    ChevronRight,
    CircleAlert,
    CircleCheck,
    CircleDashed,
    Clock,
    Cpu,
    Database,
    Loader2,
    Play,
    Settings,
    Square,
    TriangleAlert,
} from 'lucide-react'
import type {render} from '../../../wailsjs/go/models'
import {Button} from '../ui/Button'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {renderTone, seconds, warnings, type RenderTone} from '../../lib/render'

const toneText: Record<RenderTone, string> = {
    running: 'text-accent',
    idle: 'text-faint',
    ok: 'text-ok',
    warn: 'text-warn',
    error: 'text-danger',
    missing: 'text-warn',
}

function ToneIcon({tone}: {tone: RenderTone}) {
    const cls = cn('size-3.5 shrink-0', toneText[tone])
    switch (tone) {
        case 'running':
            return <Loader2 className={cn(cls, 'animate-spin')} />
        case 'ok':
            return <CircleCheck className={cls} />
        case 'warn':
        case 'missing':
            return <TriangleAlert className={cls} />
        case 'error':
            return <CircleAlert className={cls} />
        default:
            return <CircleDashed className={cls} />
    }
}

function Chip({icon, text, title}: {icon: React.ReactNode; text: string; title: string}) {
    return (
        <span className="flex items-center gap-1 font-mono text-[10.5px] text-muted" title={title}>
            <span className="text-faint">{icon}</span>
            {text}
        </span>
    )
}

// RenderPanel es el contenido del panel del visor en S1: botón de render,
// barra de estado y diagnósticos. El visor 3D en sí llega en S2 y va a ocupar
// el centro; la barra y los diagnósticos se quedan.
export function RenderPanel({
    file,
    result,
    running,
    onRender,
    onCancel,
    onOpenSettings,
}: {
    file: string | null
    result: render.Result | undefined
    running: boolean
    onRender: () => void
    onCancel: () => void
    onOpenSettings: () => void
}) {
    const t = useT()
    const [logOpen, setLogOpen] = useState(false)
    const tone = renderTone(result, running)

    if (!file) {
        return (
            <div className="bed-grid flex h-full items-center justify-center">
                <p className="mx-auto max-w-[420px] px-6 text-center text-[12.5px] leading-relaxed text-muted">{t.render.noFile}</p>
            </div>
        )
    }

    const label: Record<RenderTone, string> = {
        running: t.render.rendering,
        idle: result?.status === 'cancelled' ? t.render.cancelled : t.render.idle,
        ok: t.render.ok,
        warn: fmt(t.render.okWarnings, {n: result ? warnings(result).length : 0}),
        error: t.render.failed,
        missing: t.render.statusMissing,
    }
    const diags = result?.diagnostics ?? []
    const showMeta = !running && result && (result.status === 'ok' || result.status === 'error')

    return (
        <div className="flex h-full flex-col">
            <div className="flex h-9 shrink-0 items-center gap-3 border-b border-line bg-surface px-3">
                <span className={cn('flex min-w-0 items-center gap-1.5 text-[12px] font-medium', toneText[tone])}>
                    <ToneIcon tone={tone} />
                    <span className="truncate">{label[tone]}</span>
                </span>
                {showMeta ? (
                    <div className="flex min-w-0 items-center gap-3 overflow-hidden">
                        <Chip icon={<Clock className="size-3" />} text={fmt(t.render.seconds, {s: seconds(result.durationMs)})} title={t.render.durationHint} />
                        {result.cacheHit ? <Chip icon={<Database className="size-3" />} text={t.render.fromCache} title={t.render.fromCacheHint} /> : null}
                        {result.backend ? <Chip icon={<Cpu className="size-3" />} text={fmt(t.render.backend, {b: result.backend})} title={t.render.backendHint} /> : null}
                        {result.version ? (
                            <span className="truncate font-mono text-[10.5px] text-faint">{fmt(t.render.version, {v: result.version})}</span>
                        ) : null}
                    </div>
                ) : null}
                <div className="ml-auto shrink-0">
                    {running ? (
                        <Button variant="outline" title={t.render.cancelHint} onClick={onCancel}>
                            <Square className="size-3" />
                            {t.render.cancel}
                        </Button>
                    ) : (
                        <Button variant="primary" title={result ? t.render.rerenderHint : t.render.renderHint} onClick={onRender}>
                            <Play className="size-3" />
                            {t.render.render}
                        </Button>
                    )}
                </div>
            </div>

            <div className="bed-grid flex min-h-0 flex-1 flex-col overflow-auto">
                {tone === 'missing' ? (
                    <div className="m-4 flex flex-col items-start gap-3 rounded-lg border border-warn/50 bg-surface p-4">
                        <p className="text-[12.5px] leading-relaxed text-fg">{t.render.missing}</p>
                        <Button title={t.render.openSettingsHint} onClick={onOpenSettings}>
                            <Settings className="size-3.5" />
                            {t.render.openSettings}
                        </Button>
                    </div>
                ) : null}

                {diags.length > 0 && !running ? (
                    <div className="m-4 mb-0 flex flex-col overflow-hidden rounded-lg border border-line bg-surface">
                        <div className="border-b border-line px-3 py-1.5 font-mono text-[10.5px] tracking-[0.12em] text-muted uppercase">
                            {t.render.diagnostics}
                        </div>
                        <ul className="selectable divide-y divide-line">
                            {diags.map((d, i) => (
                                <li key={i} className="flex items-start gap-2 px-3 py-2 text-[12px]">
                                    {d.severity === 'error' ? (
                                        <CircleAlert className="mt-px size-3.5 shrink-0 text-danger" aria-label={t.render.severity.error} />
                                    ) : (
                                        <TriangleAlert className="mt-px size-3.5 shrink-0 text-warn" aria-label={t.render.severity.warning} />
                                    )}
                                    <span
                                        className={cn(
                                            'w-[74px] shrink-0 font-mono text-[11px]',
                                            d.severity === 'error' ? 'text-danger' : 'text-warn',
                                        )}
                                    >
                                        {d.line > 0 ? fmt(t.render.line, {n: d.line}) : t.render.noLine}
                                    </span>
                                    <span className="min-w-0 flex-1 leading-snug break-words text-fg">
                                        {d.file ? <span className="font-mono text-muted">{d.file}: </span> : null}
                                        {d.message}
                                    </span>
                                </li>
                            ))}
                        </ul>
                        {result?.status === 'error' ? (
                            <p className="border-t border-line px-3 py-2 text-[11.5px] leading-relaxed text-faint">{t.render.errorsFile}</p>
                        ) : null}
                    </div>
                ) : null}

                <div className="flex flex-1 flex-col items-center justify-center gap-2 px-6 py-6 text-center">
                    {result?.status === 'ok' && !running ? (
                        <>
                            <span className="flex items-center gap-1.5 text-[12.5px] font-medium text-ok">
                                <CircleCheck className="size-4" />
                                {t.render.meshReady}
                            </span>
                            {result.meshPath ? (
                                <span className="selectable max-w-full truncate font-mono text-[10.5px] text-faint" title={t.render.meshPathHint}>
                                    {result.meshPath}
                                </span>
                            ) : null}
                        </>
                    ) : null}
                    <p className="max-w-[420px] text-[12px] leading-relaxed text-muted">{t.render.viewerSoon}</p>
                </div>
            </div>

            {result && !running && result.status !== 'missing' ? (
                <div className="shrink-0 border-t border-line bg-surface">
                    <button
                        type="button"
                        title={logOpen ? t.render.hideLog : t.render.showLog}
                        aria-expanded={logOpen}
                        onClick={() => setLogOpen((v) => !v)}
                        className="flex h-7 w-full items-center gap-1.5 px-3 font-mono text-[10.5px] tracking-[0.12em] text-muted uppercase transition-colors duration-100 hover:text-fg"
                    >
                        {logOpen ? <ChevronDown className="size-3" /> : <ChevronRight className="size-3" />}
                        {t.render.log}
                    </button>
                    {logOpen ? (
                        <pre className="selectable max-h-[160px] overflow-auto border-t border-line bg-surface-2 px-3 py-2 font-mono text-[11px] leading-[1.5] whitespace-pre-wrap text-muted">
                            {result.log || t.render.emptyLog}
                        </pre>
                    ) : null}
                </div>
            ) : null}
        </div>
    )
}
