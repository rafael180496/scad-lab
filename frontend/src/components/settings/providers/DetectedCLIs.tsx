import {useCallback, useEffect, useState} from 'react'
import {RefreshCw} from 'lucide-react'
import {ListDetectedCLIs, RefreshChatProviders} from '../../../../wailsjs/go/main/App'
import type {main} from '../../../../wailsjs/go/models'
import {fmt, useT} from '../../../i18n'
import {cn} from '../../../lib/cn'
import {providerIcon} from '../../chat/providerIcons'
import {IconButton} from '../../ui/Button'
import {AgyPermissions} from './AgyPermissions'

// «CLIs detectados» (S12.b): los agentes de línea de comandos que ScadLab encontró
// en este equipo —Claude Code, Codex, Antigravity, OpenCode—, con su versión. Es de
// SOLO LECTURA a propósito: un CLI no se configura, aparece solo en el selector del
// chat cuando está instalado (PATH, rutas típicas y el PATH del shell de login en
// macOS). Mostrarlos acá responde «¿por qué no veo Codex en el chat?» sin ofrecer un
// campo de ruta que no haría falta: si no aparece, no está instalado.
export function DetectedCLIs() {
    const p = useT().providers.clis
    const [list, setList] = useState<main.DetectedCLI[] | null>(null)
    const [busy, setBusy] = useState(false)

    const load = useCallback((force: boolean) => {
        setBusy(true)
        // Volver a detectar salta la caché de 30 s de la detección: un CLI recién instalado aparece sin reiniciar.
        ;(force ? RefreshChatProviders().catch(() => []) : Promise.resolve([]))
            .then(() => ListDetectedCLIs())
            .then((l) => setList(l ?? []))
            .catch(() => setList([]))
            .finally(() => setBusy(false))
    }, [])

    useEffect(() => {
        load(false)
    }, [load])

    return (
        <section data-testid="detected-clis" className="flex flex-col gap-2 rounded-lg border border-line p-3">
            <div className="flex items-start gap-2">
                <div className="min-w-0 flex-1">
                    <h3 className="text-ui-12-5 font-medium text-fg" title={p.hint}>
                        {p.title}
                    </h3>
                    <p className="mt-0.5 text-ui-11-5 leading-relaxed text-muted">{p.note}</p>
                </div>
                <IconButton title={p.refreshHint} onClick={() => load(true)} disabled={busy} className="shrink-0">
                    <RefreshCw className={cn('icon-ui-14', busy && 'animate-spin')} />
                </IconButton>
            </div>
            {list === null ? <p className="text-ui-12 text-faint">{p.loading}</p> : null}
            {list && list.length > 0 ? (
                <ul className="flex flex-col divide-y divide-line overflow-clip rounded-md border border-line">
                    {list.map((c) => {
                        const Icon = providerIcon({id: `cli:${c.id}`, kind: 'cli', baseUrl: ''})
                        return (
                            <li key={c.id} data-cli={c.id} className={cn('flex flex-wrap items-center gap-2.5 px-3 py-1.5', !c.installed && 'opacity-60')} title={c.installed && c.path ? fmt(p.pathHint, {path: c.path}) : p.missing}>
                                <span className="flex icon-ui-28 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent">
                                    <Icon className="icon-ui-14" />
                                </span>
                                <div className="min-w-0 flex-1">
                                    <p className="flex flex-wrap items-center gap-x-2 text-ui-12-5 text-fg">
                                        <span className="truncate">{c.name}</span>
                                        {c.vendor ? <span className="shrink-0 rounded-full bg-surface-2 px-1.5 font-mono text-ui-10 text-muted">{c.vendor}</span> : null}
                                    </p>
                                    {c.installed ? (
                                        <p className="selectable truncate font-mono text-ui-10-5 text-faint">{c.version ? fmt(p.version, {v: c.version}) : p.noVersion}</p>
                                    ) : null}
                                </div>
                                <span className={cn('flex shrink-0 items-center gap-1.5 text-ui-11', c.installed ? 'text-ok' : 'text-faint')}>
                                    <span className={cn('inline-block size-2 rounded-full', c.installed ? 'bg-ok' : 'bg-faint/60')} />
                                    {c.installed ? p.installed : p.missing}
                                </span>
                                {c.id === 'antigravity' && c.installed ? <AgyPermissions /> : null}
                            </li>
                        )
                    })}
                </ul>
            ) : null}
        </section>
    )
}
