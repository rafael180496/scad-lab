import {useState} from 'react'
import {CircleAlert, KeyRound, ShieldAlert, ShieldCheck, ShieldQuestion} from 'lucide-react'
import {ForgetThisDevice, ResetVault, SetRememberDevice} from '../../../../wailsjs/go/main/App'
import type {main} from '../../../../wailsjs/go/models'
import {fmt, useT} from '../../../i18n'
import {errorText} from '../../../lib/errors'
import {cn} from '../../../lib/cn'
import {notifyVaultChanged} from '../../../lib/vault'
import {Button} from '../../ui/Button'
import {confirmDialog} from '../../ui/Dialog'
import {toast} from '../../ui/Toast'
import {MasterPasswordDialog} from '../../vault/MasterPasswordDialog'

// El llavero propio de ScadLab dentro de Configuración → Proveedores de IA
// (S12, 12.10): en qué estado están las claves de API y las acciones que lo
// cambian. La contraseña se CREA al guardar la primera clave, no desde acá; este
// panel solo muestra «todavía no hay» hasta entonces.
//
// Una contraseña olvidada no se recupera, y el panel lo dice: lo único que hay
// es Restablecer, que borra todas las claves guardadas.
export function VaultPanel({status, error}: {status: main.VaultStatus | null; error: string}) {
    const v = useT().vault
    const p = v.panel
    const [changing, setChanging] = useState(false)

    if (!status) {
        return error ? (
            <p className="flex items-start gap-1.5 rounded-md border border-danger/40 bg-danger/10 px-2 py-1.5 text-ui-11-5 text-danger">
                <CircleAlert className="icon-ui-14 mt-px shrink-0" />
                <span className="selectable min-w-0 break-words">{fmt(p.loadError, {msg: error})}</span>
            </p>
        ) : null
    }

    const run = async (fn: () => Promise<unknown>, done?: string) => {
        try {
            await fn()
            if (done) toast.ok(done)
            notifyVaultChanged()
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const reset = async () => {
        if (!(await confirmDialog(p.resetConfirm, p.resetAction, v.cancel, {danger: true}))) return
        try {
            const n = await ResetVault()
            toast.ok(fmt(p.resetDone, {n}))
            notifyVaultChanged()
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const state = !status.initialized ? p.none : status.unlocked && !status.remembered ? p.unlocked : status.remembered ? p.remembered : p.locked
    const Icon = !status.initialized ? ShieldQuestion : status.unlocked || status.remembered ? ShieldCheck : KeyRound
    const tone = !status.initialized ? 'text-faint' : status.unlocked || status.remembered ? 'text-ok' : 'text-warn'

    return (
        <div className="flex flex-col gap-2 rounded-lg border border-line px-3 py-2.5" data-testid="vault-panel">
            <div className="flex items-start gap-2.5">
                <Icon className={cn('icon-ui-20 mt-px shrink-0', tone)} />
                <div className="min-w-0 flex-1">
                    <p className="text-ui-12-5 font-medium text-fg">{p.title}</p>
                    <p className="text-ui-11-5 text-muted">{p.intro}</p>
                    <p className="mt-1 text-ui-12 text-fg" data-testid="vault-state">
                        {state}
                    </p>
                </div>
            </div>

            {status.initialized ? (
                <div className="flex flex-wrap items-center gap-2 pl-[30px]">
                    <Button data-testid="vault-change" title={p.changeBtnHint} onClick={() => setChanging(true)}>
                        {p.changeBtn}
                    </Button>
                    {status.remembered ? (
                        <Button data-testid="vault-forget" title={p.forgetBtnHint} onClick={() => void run(() => ForgetThisDevice(), p.forgotten)}>
                            {p.forgetBtn}
                        </Button>
                    ) : status.unlocked && status.keyringAvailable ? (
                        <Button data-testid="vault-remember" title={p.rememberBtnHint} onClick={() => void run(() => SetRememberDevice(true), p.remembered2)}>
                            {p.rememberBtn}
                        </Button>
                    ) : null}
                    <Button variant="danger" data-testid="vault-reset" title={p.resetBtnHint} onClick={() => void reset()} className="ml-auto">
                        {p.resetBtn}
                    </Button>
                </div>
            ) : null}

            {!status.keyringAvailable ? (
                <div className="flex items-start gap-2 rounded-md border border-warn/50 bg-warn/15 px-2.5 py-2 text-ui-11-5 text-muted" data-testid="keyring-off">
                    <ShieldAlert className="icon-ui-14 mt-px shrink-0 text-warn" />
                    <div className="min-w-0">
                        <p className="text-fg">{p.keychainDown}</p>
                        {status.keyringDetail ? <p className="selectable mt-0.5 break-words text-faint">{fmt(p.keychainDetail, {detail: status.keyringDetail})}</p> : null}
                    </div>
                </div>
            ) : null}

            <p className="pl-[30px] text-ui-11 text-faint">{v.noRecovery}</p>
            <p className="pl-[30px] text-ui-11 text-faint">{p.envHint}</p>

            <MasterPasswordDialog
                open={changing}
                mode="change"
                status={status}
                onClose={() => setChanging(false)}
            />
        </div>
    )
}
