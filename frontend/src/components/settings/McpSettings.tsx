import {useCallback, useEffect, useState} from 'react'
import {AlertTriangle, CheckCircle2, CircleDashed, Copy, Plug, RefreshCw} from 'lucide-react'
import type {main} from '../../../wailsjs/go/models'
import {AddAntigravityMCP, AntigravityMCPStatus, GetMCPStatus, RemoveAntigravityMCP, SetMCPEnabled, SyncWorkspaceMCP} from '../../../wailsjs/go/main/App'
import {onEvent} from '../../lib/events'
import {Button} from '../ui/Button'
import {Toggle} from '../ui/Field'
import {toast} from '../ui/Toast'
import {confirmDialog} from '../ui/Dialog'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {copyText} from '../../lib/clipboard'
import {cn} from '../../lib/cn'

type Dict = ReturnType<typeof useT>['mcp']
type StateKey = keyof Dict['states']
type AgyKey = keyof Dict['agyStates']

// Estados de mcpconf.Inspect: skipped-invalid se muestra como «invalid».
const stateKey = (s: string): StateKey => (s === 'skipped-invalid' ? 'invalid' : (s as StateKey))

function StateBadge({status, detail}: {status: string; detail?: string}) {
    const t = useT().mcp
    const key = stateKey(status)
    const good = key === 'ok'
    const idle = key === 'off'
    const Icon = good ? CheckCircle2 : idle ? CircleDashed : AlertTriangle
    return (
        <span
            className={cn('flex items-center gap-1.5 text-ui-11-5 whitespace-nowrap', good ? 'text-ok' : idle ? 'text-faint' : 'text-warn')}
            title={detail ? `${t.stateHints[key]}\n${detail}` : t.stateHints[key]}
        >
            <Icon className="icon-ui-14" />
            {t.states[key]}
        </span>
    )
}

// Antigravity (agy) solo tiene configuración global (~/.gemini): no hay archivo
// de workspace que sincronizar. Se agrega y se quita con botones, siempre tras
// una confirmación que dice que se modifica esa configuración global.
function AntigravityRow() {
    const t = useT()
    const m = t.mcp
    const [agy, setAgy] = useState<main.AntigravityMCP | null>(null)
    const [busy, setBusy] = useState(false)

    const load = useCallback(() => {
        AntigravityMCPStatus()
            .then(setAgy)
            .catch((e) => toast.error(errorText(e)))
    }, [])
    useEffect(() => load(), [load])

    if (!agy) return null
    const state = agy.state as AgyKey
    const good = state === 'ok'
    const idle = state === 'missing' || state === 'not-installed'
    const Icon = good ? CheckCircle2 : idle ? CircleDashed : AlertTriangle
    const hint = [m.agyStateHints[state] ?? '', agy.detail, agy.current && state !== 'ok' ? agy.current : ''].filter(Boolean).join('\n')
    const canAdd = state !== 'not-installed' && state !== 'ok'
    const canRemove = state === 'ok' || state === 'other' || state === 'disabled'

    const run = async (kind: 'add' | 'remove') => {
        const ok = await confirmDialog(
            kind === 'add' ? fmt(m.agyAddConfirm, {command: agy.command}) : m.agyRemoveConfirm,
            kind === 'add' ? m.agyAdd : m.agyRemove,
            t.editor.cancel,
        )
        if (!ok) return
        setBusy(true)
        try {
            setAgy(kind === 'add' ? await AddAntigravityMCP() : await RemoveAntigravityMCP())
            toast.ok(kind === 'add' ? m.agyAdded : m.agyRemoved)
        } catch (e) {
            toast.error(errorText(e))
            load()
        } finally {
            setBusy(false)
        }
    }

    return (
        <div className="flex flex-col gap-1.5 rounded-lg border border-line p-3" data-testid="mcp-antigravity">
            <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
                <div className="flex items-center gap-3">
                    <span className="text-ui-12 font-medium">{m.agyTitle}</span>
                    <span className="text-ui-11-5 text-muted">{m.agyScope}</span>
                    <span className={cn('flex items-center gap-1.5 text-ui-11-5 whitespace-nowrap', good ? 'text-ok' : idle ? 'text-faint' : 'text-warn')} title={hint}>
                        <Icon className="icon-ui-14" />
                        {m.agyStates[state] ?? agy.state}
                    </span>
                </div>
                <div className="flex items-center gap-2">
                    <Button title={m.agyAddHint} disabled={busy || !canAdd} onClick={() => void run('add')}>
                        {m.agyAdd}
                    </Button>
                    <Button title={m.agyRemoveHint} disabled={busy || !canRemove} onClick={() => void run('remove')}>
                        {m.agyRemove}
                    </Button>
                </div>
            </div>
            <p className="text-ui-11-5 text-faint">{m.agyHint}</p>
            <p className="text-ui-11-5 text-faint">{m.agyCwdRisk}</p>
        </div>
    )
}

// Bloque «Servidor MCP de ScadLab» de Configuración → IA: interruptor, el
// comando que usan los agentes y, para el workspace abierto, el estado de cada
// archivo de configuración.
export function McpSettings({workspaceId, workspaceName}: {workspaceId: number | null; workspaceName?: string}) {
    const t = useT()
    const m = t.mcp
    const [st, setSt] = useState<main.MCPStatus | null>(null)
    const [busy, setBusy] = useState(false)

    const load = useCallback(() => {
        GetMCPStatus(workspaceId ?? 0)
            .then(setSt)
            .catch((e) => toast.error(errorText(e)))
    }, [workspaceId])
    useEffect(() => load(), [load])
    useEffect(() => onEvent('mcp:changed', () => load()), [load])

    if (!st) return null
    const enabled = st.enabled

    const toggle = async (v: boolean) => {
        setBusy(true)
        try {
            await SetMCPEnabled(v)
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setBusy(false)
            load()
        }
    }
    const resync = async () => {
        if (workspaceId == null) return
        setBusy(true)
        try {
            await SyncWorkspaceMCP(workspaceId)
            toast.ok(m.resynced)
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setBusy(false)
            load()
        }
    }
    const copy = async () => (await copyText(st.command)) ? toast.ok(m.copied) : toast.error(m.copyFailed)

    return (
        <div className="flex flex-col gap-2" data-testid="mcp">
            <div className="flex items-start justify-between gap-4">
                <div>
                    <h3 className="flex items-center gap-1.5 text-ui-13 font-semibold">
                        <Plug className="icon-ui-14 text-accent" />
                        {m.title}
                    </h3>
                    <p className="text-ui-11-5 text-faint">{m.hint}</p>
                </div>
                <Toggle checked={enabled} title={enabled ? m.toggleOnHint : m.toggleOffHint} onChange={(v) => void toggle(v)} />
            </div>

            {!enabled ? <p className="text-ui-11-5 text-muted">{m.offNote}</p> : null}

            {enabled ? (
                <div className="flex flex-col gap-3 rounded-lg border border-line p-3">
                    {st.binaryError ? <p className="text-ui-11-5 text-warn">{fmt(m.binaryError, {err: st.binaryError})}</p> : null}
                    {!st.openscad ? <p className="text-ui-11-5 text-warn">{m.openscadMissing}</p> : null}

                    {st.command ? (
                        <div className="flex flex-col gap-1">
                            <span className="text-ui-11-5 font-medium text-muted" title={m.commandHint}>
                                {m.commandLabel}
                            </span>
                            <div className="flex items-center gap-2">
                                <code className="selectable min-w-0 flex-1 rounded-md border border-line bg-surface-2 px-2 py-1 font-mono text-ui-11-5 break-all text-fg" title={st.command}>
                                    {st.command}
                                </code>
                                <Button title={m.copyHint} onClick={() => void copy()}>
                                    <Copy className="icon-ui-14" />
                                    {m.copy}
                                </Button>
                            </div>
                        </div>
                    ) : null}

                    {workspaceId == null ? (
                        <p className="text-ui-11-5 text-faint">{m.noWorkspace}</p>
                    ) : (
                        <div className="flex flex-col gap-2">
                            <div className="flex items-center justify-between gap-3">
                                <span className="text-ui-11-5 font-medium text-muted">{fmt(m.filesTitle, {name: workspaceName ?? ''})}</span>
                                <Button title={m.resyncHint} disabled={busy} onClick={() => void resync()}>
                                    <RefreshCw className={cn('icon-ui-14', busy && 'animate-spin')} />
                                    {m.resync}
                                </Button>
                            </div>
                            <div className="divide-y divide-line rounded-lg border border-line">
                                {st.files.map((f) => (
                                    <div key={f.name} className="grid grid-cols-[110px_minmax(0,1fr)_auto] items-center gap-3 px-3 py-1.5">
                                        <span className="text-ui-12 font-medium">{m.agents[f.agent as keyof Dict['agents']] ?? f.agent}</span>
                                        <span className="selectable truncate font-mono text-ui-11-5 text-muted" title={f.path}>
                                            {f.name}
                                        </span>
                                        <StateBadge status={f.status} detail={f.detail} />
                                    </div>
                                ))}
                                <div className="grid grid-cols-[110px_minmax(0,1fr)_auto] items-center gap-3 px-3 py-1.5">
                                    <span className="text-ui-12 font-medium">{m.agents.codex}</span>
                                    <span className="truncate text-ui-11-5 text-muted" title={st.codexFlags.length ? `${m.codexHint}\n${st.codexFlags.join(' ')}` : m.codexHint}>
                                        {m.codexVia}
                                    </span>
                                    <StateBadge status="ok" />
                                </div>
                            </div>
                        </div>
                    )}

                    <AntigravityRow />
                </div>
            ) : null}
        </div>
    )
}
