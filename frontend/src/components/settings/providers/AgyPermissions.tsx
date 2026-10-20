import {useCallback, useEffect, useState} from 'react'
import {AlertTriangle, CheckCircle2, CircleDashed} from 'lucide-react'
import {AntigravityPermissionsStatus, RemoveAntigravityPermissions, RepairAntigravityPermissions} from '../../../../wailsjs/go/main/App'
import type {main} from '../../../../wailsjs/go/models'
import {fmt, useT} from '../../../i18n'
import {cn} from '../../../lib/cn'
import {errorText} from '../../../lib/errors'
import {onEvent} from '../../../lib/events'
import {Button} from '../../ui/Button'
import {confirmDialog} from '../../ui/Dialog'
import {toast} from '../../ui/Toast'

// «Permisos de ScadLab» de Antigravity (S12.c, T14): las reglas `mcp(scadlab/<herramienta>)` que ScadLab
// mantiene en `permissions.allow` del settings.json GLOBAL de Antigravity. Sin ellas, `agy --print` bloquea cada
// herramienta de ScadLab y termina el turno. ScadLab las agrega solo (al registrar el MCP y en cada turno) y aquí se
// ven, se quitan —solo las `mcp(scadlab/…)`, nunca otra regla— y se vuelven a agregar. Quitarlas es una decisión
// que se respeta: ScadLab no las repone hasta que se pida «Volver a agregar».
export function AgyPermissions() {
    const t = useT().providers.clis.agyPerms
    const [st, setSt] = useState<main.AntigravityPermissions | null>(null)
    const [busy, setBusy] = useState(false)

    const load = useCallback(() => {
        AntigravityPermissionsStatus()
            .then(setSt)
            .catch(() => setSt(null))
    }, [])
    useEffect(() => load(), [load])
    // El registro automático (al elegir Antigravity en el chat) agrega reglas por su cuenta.
    useEffect(() => onEvent('mcp:antigravity', () => load()), [load])

    if (!st || st.state === 'not-installed' || st.state === 'unavailable') return null

    const unreadable = st.state === 'unreadable'
    const good = st.state === 'ok'
    const Icon = good ? CheckCircle2 : unreadable ? AlertTriangle : CircleDashed
    const label = unreadable
        ? t.unreadable
        : st.approved === 0
          ? t.none
          : st.approved === st.total
            ? fmt(st.approved === 1 ? t.approvedOne : t.approved, {n: st.approved})
            : fmt(t.partial, {n: st.approved, total: st.total})
    const hint = [fmt(t.hint, {path: st.path}), unreadable ? st.detail : '', !st.managed ? t.offHint : ''].filter(Boolean).join('\n')

    const run = async (kind: 'remove' | 'add') => {
        const ok = await confirmDialog(
            fmt(kind === 'remove' ? t.removeConfirm : t.addConfirm, {path: st.path, total: st.total}),
            kind === 'remove' ? t.remove : t.add,
            t.cancel,
        )
        if (!ok) return
        setBusy(true)
        try {
            setSt(kind === 'remove' ? await RemoveAntigravityPermissions() : await RepairAntigravityPermissions())
            toast.ok(kind === 'remove' ? t.removed : t.added)
        } catch (e) {
            toast.error(errorText(e))
            load()
        } finally {
            setBusy(false)
        }
    }

    return (
        <div className="mt-1 flex w-full basis-full flex-col gap-1 border-t border-line pt-1.5" data-testid="agy-permissions">
            <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5">
                <span className={cn('flex items-center gap-1.5 text-ui-11-5', good ? 'text-ok' : unreadable ? 'text-warn' : 'text-muted')} title={hint}>
                    <Icon className="icon-ui-14" />
                    {label}
                </span>
                <div className="flex items-center gap-2">
                    <Button title={t.removeHint} disabled={busy || unreadable || (st.approved === 0 && st.stale === 0)} onClick={() => void run('remove')}>
                        {t.remove}
                    </Button>
                    <Button title={t.addHint} disabled={busy || (good && st.managed)} onClick={() => void run('add')}>
                        {t.add}
                    </Button>
                </div>
            </div>
            {!st.managed ? <p className="text-ui-11 text-faint">{t.off}</p> : null}
        </div>
    )
}
