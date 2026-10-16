import {useCallback, useEffect, useState} from 'react'
import type {main} from '../../wailsjs/go/models'
import {GetMCPStatus} from '../../wailsjs/go/main/App'
import {onEvent} from '../lib/events'

// Estado del servidor MCP de ScadLab para un workspace (backend: app_mcp.go).
// Se vuelve a leer cuando Go avisa con mcp:changed (se apagó o encendió el
// interruptor, o se resincronizó un workspace).

export type McpHealth = {
    /** off: apagado · ok: todos los archivos al día · warn: algún archivo con problemas. */
    kind: 'off' | 'ok' | 'warn'
    /** Cantidad de archivos que no están al día (solo con kind = warn). */
    problems: number
}

/** Resume el estado en lo que muestra el indicador de la terminal. */
export function mcpHealth(s: main.MCPStatus): McpHealth {
    if (!s.enabled) return {kind: 'off', problems: 0}
    const problems = s.files.filter((f) => f.status !== 'ok').length + (s.binaryError ? 1 : 0)
    return {kind: problems ? 'warn' : 'ok', problems}
}

export function useMcpStatus(workspaceId: number | null): {status: main.MCPStatus | null; reload: () => void} {
    const [status, setStatus] = useState<main.MCPStatus | null>(null)
    const reload = useCallback(() => {
        if (workspaceId == null) {
            setStatus(null)
            return
        }
        GetMCPStatus(workspaceId)
            .then(setStatus)
            .catch(() => setStatus(null))
    }, [workspaceId])
    useEffect(() => reload(), [reload])
    useEffect(() => onEvent('mcp:changed', () => reload()), [reload])
    return {status, reload}
}
