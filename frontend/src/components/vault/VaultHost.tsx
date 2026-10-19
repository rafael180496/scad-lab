import {useEffect, useState, useSyncExternalStore} from 'react'
import {VaultStatus} from '../../../wailsjs/go/main/App'
import type {main} from '../../../wailsjs/go/models'
import {currentVaultRequest, settleVaultRequest, subscribeVaultRequest} from '../../lib/vault'
import {MasterPasswordDialog} from './MasterPasswordDialog'

// Monta el diálogo de la contraseña maestra que pide withVault (lib/vault.ts).
// Va una sola vez en App, junto a ConfirmHost: los bindings que descifran lo
// abren desde cualquier pantalla (el chat, Proveedores de IA) sin que cada una
// tenga que montar el suyo.
export function VaultHost() {
    const mode = useSyncExternalStore(subscribeVaultRequest, currentVaultRequest)
    const [status, setStatus] = useState<main.VaultStatus | null>(null)
    const [last, setLast] = useState<'create' | 'unlock'>('unlock')
    useEffect(() => {
        if (!mode) return
        setLast(mode)
        VaultStatus()
            .then(setStatus)
            .catch(() => setStatus(null))
    }, [mode])
    return <MasterPasswordDialog open={mode !== null} mode={mode ?? last} status={status} onClose={settleVaultRequest} />
}
