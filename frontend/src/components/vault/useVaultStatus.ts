import {useCallback, useEffect, useState} from 'react'
import {VaultStatus} from '../../../wailsjs/go/main/App'
import type {main} from '../../../wailsjs/go/models'
import {errorText} from '../../lib/errors'
import {onVaultChanged} from '../../lib/vault'

// Estado del llavero propio, que se vuelve a leer solo cuando algo lo cambia
// (crear, desbloquear, cambiar, olvidar, restablecer). `status` es null hasta que
// llega la primera respuesta. VaultStatus no lee la llave recordada: pedirlo no
// dispara ningún aviso del sistema.
export function useVaultStatus() {
    const [status, setStatus] = useState<main.VaultStatus | null>(null)
    const [error, setError] = useState('')
    const reload = useCallback(() => {
        VaultStatus()
            .then((s) => {
                setStatus(s)
                setError('')
            })
            .catch((e) => setError(errorText(e)))
    }, [])
    useEffect(() => {
        reload()
        return onVaultChanged(reload)
    }, [reload])
    return {status, error, reload}
}
