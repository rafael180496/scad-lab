import {useCallback, useEffect, useState} from 'react'
import {CircleAlert, KeyRound, Pencil, Plug, Plus, Trash2} from 'lucide-react'
import {DeleteModelServer, ListModelServers, SaveModelServer, TestModelServer} from '../../../../wailsjs/go/main/App'
import {store} from '../../../../wailsjs/go/models'
import {fmt, useT} from '../../../i18n'
import {errorText} from '../../../lib/errors'
import {cn} from '../../../lib/cn'
import {serverGroup, serverKind} from '../../../lib/modelServers'
import {withVault} from '../../../lib/vault'
import {Button, IconButton} from '../../ui/Button'
import {confirmDialog} from '../../ui/Dialog'
import {Toggle} from '../../ui/Field'
import {toast} from '../../ui/Toast'
import {useVaultStatus} from '../../vault/useVaultStatus'
import {ProbeResult, type Probe} from './ProbeResult'
import {ServerForm} from './ServerForm'
import {serverIcon} from './presetIcons'
import {VaultPanel} from './VaultPanel'

// Configuración → «Proveedores de IA» (S12): los servidores de modelos con los
// que conversa el chat. Es el ModelServersSettings de mini-tools, adaptado:
// cada fila se prueba, se edita, se enciende o apaga y se borra desde acá, y el
// alta/edición abre el formulario en línea (los cambios se guardan al instante;
// no pasan por «Guardar» de Configuración, que es de los ajustes).
//
// **La clave nunca vuelve del backend**: la fila solo dice si hay una guardada
// (cifrada con la contraseña maestra, ver VaultPanel) o de qué variable de
// entorno se lee. Lo que descifra pasa por withVault: si la contraseña no se
// creó o la llave no se recuperó sola, se abre el diálogo y se reintenta.

export function ProvidersSettings() {
    const t = useT()
    const p = t.providers
    const [servers, setServers] = useState<store.ModelServer[] | null>(null)
    const [error, setError] = useState('')
    const vault = useVaultStatus()
    // Qué se está editando: un servidor existente, o null dentro para uno nuevo.
    const [editor, setEditor] = useState<{server: store.ModelServer | null} | null>(null)
    const [probes, setProbes] = useState<Record<string, Probe>>({})

    const reload = useCallback(() => {
        ListModelServers()
            .then((l) => {
                setServers(l ?? [])
                setError('')
            })
            .catch((e) => {
                setServers([])
                setError(errorText(e))
            })
    }, [])

    useEffect(() => {
        reload()
    }, [reload])

    const test = async (s: store.ModelServer) => {
        setProbes((m) => ({...m, [s.id]: {state: 'testing'}}))
        try {
            const probe = await withVault(() => TestModelServer(s, ''))
            setProbes((m) => ({...m, [s.id]: {state: 'done', probe}}))
        } catch (e) {
            setProbes((m) => ({...m, [s.id]: {state: 'error', error: errorText(e)}}))
        }
    }

    const toggle = async (s: store.ModelServer, enabled: boolean) => {
        setServers((l) => (l ?? []).map((x) => (x.id === s.id ? store.ModelServer.createFrom({...x, enabled}) : x)))
        try {
            await SaveModelServer(store.ModelServer.createFrom({...s, enabled}), '', false)
        } catch (e) {
            toast.error(errorText(e))
            reload()
        }
    }

    const remove = async (s: store.ModelServer) => {
        const ok = await confirmDialog(fmt(p.removeConfirm, {name: s.name}), p.removeAction, t.editor.cancel)
        if (!ok) return
        try {
            await DeleteModelServer(s.id)
            toast.ok(fmt(p.removed, {name: s.name}))
            setProbes((m) => without(m, s.id))
            reload()
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const onSaved = (saved: store.ModelServer) => {
        toast.ok(fmt(p.saved, {name: saved.name}))
        setEditor(null)
        // La prueba anterior hablaba de la configuración vieja.
        setProbes((m) => without(m, saved.id))
        reload()
    }

    const list = servers ?? []
    const counts = {
        local: list.filter((s) => serverGroup(s) === 'local').length,
        remote: list.filter((s) => serverGroup(s) === 'remote').length,
        cloud: list.filter((s) => serverGroup(s) === 'cloud').length,
    }
    const off = list.filter((s) => !s.enabled).length

    return (
        <section data-testid="providers" className="flex flex-col gap-3">
            <p className="text-ui-12 leading-relaxed text-muted">{p.intro}</p>

            <VaultPanel status={vault.status} error={vault.error} />

            {error ? (
                <p className="flex items-start gap-1.5 rounded-md border border-danger/40 bg-danger/10 px-2 py-1.5 text-ui-11-5 text-danger">
                    <CircleAlert className="icon-ui-14 mt-px shrink-0" />
                    <span className="selectable min-w-0 break-words">{fmt(p.loadError, {msg: error})}</span>
                </p>
            ) : null}

            {servers === null ? <p className="text-ui-12 text-faint">{p.loading}</p> : null}
            {servers !== null && list.length === 0 && !editor ? (
                <p className="rounded-lg border border-dashed border-line px-3 py-3 text-ui-12 text-muted">{p.empty}</p>
            ) : null}

            {list.length > 0 ? (
                <>
                    <p className="flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-ui-10-5 text-faint">
                        <span>{fmt(p.summary, counts)}</span>
                        {off > 0 ? <span>{fmt(p.summaryOff, {n: off})}</span> : null}
                    </p>
                    <ul className="flex flex-col divide-y divide-line overflow-clip rounded-lg border border-line">
                        {list.map((s) => {
                            if (editor?.server?.id === s.id) return null
                            const group = serverGroup(s)
                            const Icon = serverIcon(s)
                            const probe = probes[s.id]
                            return (
                                <li key={s.id} data-server={s.id} className={cn('flex flex-col gap-1.5 px-3 py-2', !s.enabled && 'opacity-60')}>
                                    <div className="flex items-center gap-2.5">
                                        <span className="flex icon-ui-28 shrink-0 items-center justify-center rounded-full bg-accent-soft text-accent" title={p.kinds[serverKind(s.kind)]}>
                                            <Icon className="icon-ui-14" />
                                        </span>
                                        <div className="min-w-0 flex-1">
                                            <p className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-ui-12-5 font-medium text-fg">
                                                <span className="truncate">{s.name}</span>
                                                <span className="shrink-0 rounded-full bg-surface-2 px-1.5 font-mono text-ui-10 font-normal text-muted" title={p.groupHints[group]}>
                                                    {p.groups[group]}
                                                </span>
                                                {p.kinds[serverKind(s.kind)] !== s.name ? (
                                                    <span className="shrink-0 rounded-full bg-surface-2 px-1.5 font-mono text-ui-10 font-normal text-muted">{p.kinds[serverKind(s.kind)]}</span>
                                                ) : null}
                                                {!s.enabled ? <span className="shrink-0 font-mono text-ui-10 font-normal text-faint">{p.off}</span> : null}
                                            </p>
                                            <p className="selectable truncate font-mono text-ui-10-5 text-faint" title={s.baseUrl}>
                                                {s.baseUrl}
                                            </p>
                                            <p className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-ui-11 text-muted">
                                                <KeyChip s={s} />
                                                {s.defaultModel ? <span className="selectable font-mono text-ui-10-5">{fmt(p.defaultModelRow, {model: s.defaultModel})}</span> : null}
                                            </p>
                                        </div>
                                        <Toggle checked={s.enabled} title={p.enabledHint} onChange={(v) => void toggle(s, v)} />
                                        <Button data-testid="server-test" onClick={() => void test(s)} disabled={probe?.state === 'testing'} title={fmt(p.testHint, {name: s.name})}>
                                            <Plug className="icon-ui-14" />
                                            {probe?.state === 'testing' ? p.testing : p.test}
                                        </Button>
                                        <IconButton
                                            title={fmt(p.editHint, {name: s.name})}
                                            onClick={() => {
                                                setEditor({server: s})
                                                setError('')
                                            }}
                                        >
                                            <Pencil className="icon-ui-14" />
                                        </IconButton>
                                        <IconButton title={fmt(p.removeHint, {name: s.name})} onClick={() => void remove(s)}>
                                            <Trash2 className="icon-ui-14" />
                                        </IconButton>
                                    </div>
                                    {probe && probe.state !== 'testing' ? <ProbeResult probe={probe} /> : null}
                                </li>
                            )
                        })}
                    </ul>
                </>
            ) : null}

            {editor ? <ServerForm key={editor.server?.id ?? 'new'} server={editor.server} vault={vault.status} onSaved={onSaved} onCancel={() => setEditor(null)} /> : null}

            {!editor ? (
                <Button
                    data-testid="server-add"
                    className="self-start"
                    title={p.addHint}
                    onClick={() => {
                        setEditor({server: null})
                        setError('')
                    }}
                >
                    <Plus className="icon-ui-14" />
                    {p.add}
                </Button>
            ) : null}
        </section>
    )
}

function without(m: Record<string, Probe>, id: string): Record<string, Probe> {
    const next = {...m}
    delete next[id]
    return next
}

// De dónde sale la clave de un servidor: guardada (cifrada), una variable de
// entorno, o ninguna (lo normal en un servidor local).
function KeyChip({s}: {s: store.ModelServer}) {
    const p = useT().providers
    if (s.hasToken) {
        return (
            <span className="flex items-center gap-1 text-ok" title={p.keySavedHint}>
                <KeyRound className="icon-ui-12" />
                {p.keySaved}
            </span>
        )
    }
    if (s.apiKeyEnv) {
        return (
            <span className="flex items-center gap-1" title={fmt(p.keyEnvHint, {env: s.apiKeyEnv})}>
                <KeyRound className="icon-ui-12" />
                <span className="font-mono text-ui-10-5">{fmt(p.keyEnv, {env: s.apiKeyEnv})}</span>
            </span>
        )
    }
    return (
        <span className="flex items-center gap-1 text-faint" title={p.noKeyHint}>
            {p.noKey}
        </span>
    )
}
