import {useCallback, useEffect, useRef, useState} from 'react'
import type {main} from '../../wailsjs/go/models'
import {GetWorkspaceConfig} from '../../wailsjs/go/main/App'
import {onEvent} from '../lib/events'

// La configuración resuelta del workspace al frente (S14): su impresora efectiva, los colores,
// las skills y los ajustes de impresión. Se lee al cambiar de proyecto y se mantiene al día con
// el evento `workspace:config`, que lo emite el guardado del diálogo y también la IA cuando
// cambia la impresora desde el chat (`set_workspace_printer`).
//
// `onUpdated` recibe cada configuración nueva que llega por el evento: App la usa para que
// la lista de workspaces y el proyecto abierto vean la impresora nueva sin releer nada.
export function useWorkspaceConfig(workspaceId: number | null, onUpdated: (c: main.WorkspaceConfigView) => void) {
    const [byId, setById] = useState<Record<number, main.WorkspaceConfigView>>({})
    const cb = useRef(onUpdated)
    cb.current = onUpdated

    const reload = useCallback(async (id: number | null = workspaceId) => {
        if (id == null) return
        try {
            const c = await GetWorkspaceConfig(id)
            setById((p) => ({...p, [id]: c}))
        } catch {
            // Sin configuración legible la barra de estado cae en el nombre del catálogo: no vale un aviso.
        }
    }, [workspaceId])

    useEffect(() => {
        void reload()
    }, [reload])

    useEffect(
        () =>
            onEvent('workspace:config', (c) => {
                setById((p) => ({...p, [c.workspace.id]: c}))
                cb.current(c)
            }),
        [],
    )

    return {config: workspaceId != null ? byId[workspaceId] : undefined, reload}
}
