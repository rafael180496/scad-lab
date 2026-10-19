import {CircleAlert, CircleCheck} from 'lucide-react'
import type {main} from '../../../../wailsjs/go/models'
import {fmt, useT} from '../../../i18n'

// Resultado de una prueba de conexión, en línea. Lo comparten el formulario de
// alta/edición y la lista de servidores.
export type Probe = {state: 'testing'} | {state: 'done'; probe: main.ServerProbe} | {state: 'error'; error: string}

const MODELS_SHOWN = 6

export function ProbeResult({probe}: {probe: Exclude<Probe, {state: 'testing'}>}) {
    const t = useT()
    const p = t.providers
    if (probe.state === 'error' || (probe.state === 'done' && !probe.probe.ok)) {
        const msg = probe.state === 'error' ? probe.error : probe.probe.message
        return (
            <p className="flex items-start gap-1.5 rounded-md border border-danger/40 bg-danger/10 px-2 py-1.5 text-ui-11-5 text-danger">
                <CircleAlert className="icon-ui-14 mt-px shrink-0" />
                <span className="selectable min-w-0 break-words">{fmt(p.probeFail, {msg})}</span>
            </p>
        )
    }
    if (probe.state !== 'done') return null
    const ids = probe.probe.modelIds ?? []
    const version = probe.probe.message ? ` ${probe.probe.message}` : ''
    return (
        <div className="flex flex-col gap-0.5 rounded-md border border-ok/40 bg-ok/10 px-2 py-1.5 text-ui-11-5">
            <p className="flex items-center gap-1.5 text-ok">
                <CircleCheck className="icon-ui-14 shrink-0" />
                {fmt(p.probeOk, {version, models: probe.probe.models})}
                {probe.probe.latencyMs > 0 ? fmt(p.probeLatency, {ms: probe.probe.latencyMs}) : null}
            </p>
            {ids.length > 0 ? (
                <p className="selectable break-words font-mono text-ui-10-5 text-muted">
                    {fmt(p.probeModels, {list: ids.slice(0, MODELS_SHOWN).join(', ') + (probe.probe.models > MODELS_SHOWN ? ', …' : '')})}
                </p>
            ) : null}
        </div>
    )
}
