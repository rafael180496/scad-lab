import {useEffect, useId, useState} from 'react'
import {CircleAlert, Eye, EyeOff, KeyRound, ShieldAlert} from 'lucide-react'
import {ChangeMasterPassword, CreateMasterPassword, ResetVault, UnlockVault} from '../../../wailsjs/go/main/App'
import type {main} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {errorCode, errorText} from '../../lib/errors'
import {MIN_PASSWORD_LEN} from '../../lib/passwordStrength'
import {CODE_WRONG_PASSWORD, keychainKind, notifyVaultChanged} from '../../lib/vault'
import {Button, IconButton} from '../ui/Button'
import {Dialog, confirmDialog} from '../ui/Dialog'
import {Input} from '../ui/Field'
import {toast} from '../ui/Toast'
import {PasswordStrength} from './PasswordStrength'

export type VaultDialogMode = 'create' | 'unlock' | 'change'

// Diálogo de la contraseña maestra (S12, 12.10) en sus tres usos:
//
//   create  la primera vez que se guarda una clave de API: contraseña +
//           confirmación + indicador de fuerza + «Recordar en este equipo»
//           (marcado por defecto)
//   unlock  una vez por sesión, solo si la llave no se recuperó sola
//   change  desde Configuración → Proveedores de IA: re-cifra todas las claves
//
// La contraseña vive solo en el estado de este componente mientras está abierto
// y viaja a Go por CreateMasterPassword / UnlockVault / ChangeMasterPassword; no
// se guarda en ningún lado de la interfaz. onClose(true) = se completó.
export function MasterPasswordDialog({
    open,
    mode,
    status,
    onClose,
}: {
    open: boolean
    mode: VaultDialogMode
    /** Estado del llavero (si el del sistema responde, si ya se recordaba). */
    status: main.VaultStatus | null
    onClose: (ok: boolean) => void
}) {
    const t = useT()
    const v = t.vault
    const formId = useId()
    const [current, setCurrent] = useState('')
    const [pw, setPw] = useState('')
    const [confirm, setConfirm] = useState('')
    const [show, setShow] = useState(false)
    const [remember, setRemember] = useState(true)
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')

    const keyringOk = status?.keyringAvailable ?? true
    // Vuelve a su estado inicial cada vez que se abre: nada de lo escrito sobrevive.
    useEffect(() => {
        if (!open) return
        setCurrent('')
        setPw('')
        setConfirm('')
        setShow(false)
        setBusy(false)
        setError('')
        // Crear: marcado por defecto. Desbloquear: como estaba (si el llavero no
        // respondió esta sesión, no se ofrece).
        setRemember(mode === 'create' ? true : (status?.remembered ?? false))
    }, [open, mode, status?.remembered])

    const tooShort = mode !== 'unlock' && pw !== '' && [...pw].length < MIN_PASSWORD_LEN
    const mismatch = mode !== 'unlock' && confirm !== '' && confirm !== pw
    const ready =
        mode === 'unlock'
            ? pw !== ''
            : [...pw].length >= MIN_PASSWORD_LEN && confirm === pw && (mode === 'create' || current !== '')

    const finish = (r: main.VaultResult | null, done: string) => {
        toast.ok(done)
        if (r?.rememberFailed) toast.error(fmt(v.rememberFailed, {detail: r.detail}))
        if (r && r.lostKeys > 0) toast.error(fmt(v.lostKeys, {n: r.lostKeys}))
        notifyVaultChanged()
        onClose(true)
    }

    const submit = async () => {
        if (!ready || busy) return
        setBusy(true)
        setError('')
        try {
            if (mode === 'create') finish(await CreateMasterPassword(pw, remember && keyringOk), v.created)
            else if (mode === 'unlock') finish(await UnlockVault(pw, remember && keyringOk), v.unlocked)
            else finish(await ChangeMasterPassword(current, pw), v.changed)
        } catch (e) {
            setError(errorCode(e) === CODE_WRONG_PASSWORD ? v.wrongPassword : errorText(e))
            if (mode === 'unlock') setPw('')
            if (mode === 'change' && errorCode(e) === CODE_WRONG_PASSWORD) setCurrent('')
        } finally {
            setBusy(false)
        }
    }

    const forgot = async () => {
        const ok = await confirmDialog(v.panel.resetConfirm, v.panel.resetAction, v.cancel, {danger: true})
        if (!ok) return
        try {
            const n = await ResetVault()
            toast.ok(fmt(v.panel.resetDone, {n}))
            notifyVaultChanged()
            onClose(false)
        } catch (e) {
            setError(errorText(e))
        }
    }

    const title = mode === 'create' ? v.createTitle : mode === 'unlock' ? v.unlockTitle : v.changeTitle
    const intro = mode === 'create' ? v.createIntro : mode === 'unlock' ? v.unlockIntro : v.changeIntro
    const action = mode === 'create' ? v.create : mode === 'unlock' ? v.unlock : v.change
    const actionHint = mode === 'create' ? v.createHint : mode === 'unlock' ? v.unlockHint : v.changeHint

    return (
        <Dialog
            open={open}
            onClose={() => onClose(false)}
            title={title}
            closeLabel={v.close}
            className="w-[440px]"
            footer={
                <>
                    <Button variant="ghost" title={v.cancelHint} onClick={() => onClose(false)} disabled={busy}>
                        {v.cancel}
                    </Button>
                    <Button type="submit" form={formId} variant="primary" title={actionHint} disabled={!ready || busy} data-testid="vault-submit">
                        {busy ? v.working : action}
                    </Button>
                </>
            }
        >
            <form
                id={formId}
                data-testid={`vault-${mode}`}
                className="flex flex-col gap-3 px-4 py-4"
                onSubmit={(e) => {
                    e.preventDefault()
                    void submit()
                }}
            >
                <p className="flex items-start gap-2 text-ui-12 leading-relaxed text-muted">
                    <KeyRound className="icon-ui-16 mt-px shrink-0 text-accent" />
                    {intro}
                </p>

                {mode === 'change' ? (
                    <PasswordField label={v.current} value={current} onChange={setCurrent} show={show} onToggle={() => setShow((s) => !s)} autoFocus autoComplete="current-password" />
                ) : null}
                <PasswordField
                    label={mode === 'change' ? v.newPassword : v.password}
                    value={pw}
                    onChange={setPw}
                    show={show}
                    onToggle={() => setShow((s) => !s)}
                    autoFocus={mode !== 'change'}
                    autoComplete={mode === 'unlock' ? 'current-password' : 'new-password'}
                    invalid={tooShort}
                />
                {mode !== 'unlock' ? (
                    <>
                        <PasswordStrength password={pw} />
                        {tooShort ? <Problem text={fmt(v.tooShort, {n: MIN_PASSWORD_LEN})} /> : null}
                        <PasswordField label={v.confirm} value={confirm} onChange={setConfirm} show={show} onToggle={() => setShow((s) => !s)} autoComplete="new-password" invalid={mismatch} title={v.confirmTitle} />
                        {mismatch ? <Problem text={v.mismatch} /> : null}
                    </>
                ) : null}

                {mode !== 'change' ? (
                    <div className="flex flex-col gap-1">
                        <label className="flex items-center gap-2 text-ui-12-5 text-fg" title={v.rememberTitle}>
                            <input
                                type="checkbox"
                                className="icon-ui-14 accent-accent"
                                checked={remember && keyringOk}
                                disabled={!keyringOk}
                                onChange={(e) => setRemember(e.target.checked)}
                                data-testid="vault-remember"
                            />
                            {v.remember}
                        </label>
                        <p className="pl-6 text-ui-11 leading-relaxed text-faint">
                            {keyringOk ? fmt(v.rememberHint, {keychain: v.keychain[keychainKind()]}) : v.rememberOff}
                        </p>
                    </div>
                ) : null}

                <p className="flex items-start gap-1.5 rounded-md border border-warn/50 bg-warn/15 px-2.5 py-2 text-ui-11-5 leading-relaxed text-muted">
                    <ShieldAlert className="icon-ui-14 mt-px shrink-0 text-warn" />
                    {v.noRecovery}
                </p>

                {error ? <Problem text={error} /> : null}

                {mode === 'unlock' ? (
                    <button
                        type="button"
                        onClick={() => void forgot()}
                        title={v.forgotHint}
                        className="no-drag self-start rounded px-1 py-0.5 text-ui-11-5 text-muted underline-offset-2 hover:text-danger hover:underline"
                    >
                        {v.forgot}
                    </button>
                ) : null}
            </form>
        </Dialog>
    )
}

function PasswordField({
    label,
    value,
    onChange,
    show,
    onToggle,
    autoFocus,
    autoComplete,
    invalid,
    title,
}: {
    label: string
    value: string
    onChange: (v: string) => void
    show: boolean
    onToggle: () => void
    autoFocus?: boolean
    autoComplete: string
    invalid?: boolean
    title?: string
}) {
    const v = useT().vault
    return (
        <label className="flex flex-col gap-1">
            <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{label}</span>
            <span className="relative block">
                <Input
                    type={show ? 'text' : 'password'}
                    value={value}
                    onChange={(e) => onChange(e.target.value)}
                    data-autofocus={autoFocus ? '' : undefined}
                    autoComplete={autoComplete}
                    title={title ?? v.passwordTitle}
                    aria-label={label}
                    aria-invalid={invalid}
                    className={invalid ? 'border-danger pr-9' : 'pr-9'}
                />
                <IconButton title={show ? v.hide : v.show} onClick={onToggle} tabIndex={-1} className="absolute top-0.5 right-0.5 size-7">
                    {show ? <EyeOff className="icon-ui-14" /> : <Eye className="icon-ui-14" />}
                </IconButton>
            </span>
        </label>
    )
}

function Problem({text}: {text: string}) {
    return (
        <p className="selectable flex items-start gap-1.5 text-ui-11-5 text-danger">
            <CircleAlert className="icon-ui-14 mt-px shrink-0" />
            <span className="min-w-0 break-words">{text}</span>
        </p>
    )
}
