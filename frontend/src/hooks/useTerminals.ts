import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import type {main} from '../../wailsjs/go/models'
import {CloseTerminal, ListAgentChoices, ListTerminals, SetAutoIterate, StartAgentTerminal} from '../../wailsjs/go/main/App'
import {onTerminalExit} from '../components/terminal/events'
import {showPanel} from '../components/dock/dock'
import {onProjectClosed, onTerminalOpened} from '../lib/sessionBridge'
import {toast} from '../components/ui/Toast'
import {errorText} from '../lib/errors'

// Estado de las terminales de agente (backend: app_terminal.go). El PROCESO
// vive en Go; acá solo se lleva la cuenta de cuál ocupa cada una de las dos
// pestañas «Agente» de cada workspace, para que no se muevan al cerrar una.

export type SlotIndex = 0 | 1
export type StartMode = 'new' | 'resume' | 'continue'

export type TermSlot = {
    id: string
    agentId: string
    workspaceId: number
    slot: SlotIndex
    /** Código de salida si el proceso ya terminó (la pantalla queda para leerla). */
    ended: number | null
}

// Tamaño de arranque, antes de que la vista mida: el primer fit lo corrige.
const START_COLS = 110
const START_ROWS = 28

function assign(list: main.TerminalInfo[]): TermSlot[] {
    const taken = new Map<number, Set<number>>()
    const out: TermSlot[] = []
    for (const t of [...list].sort((a, b) => a.startedAt - b.startedAt)) {
        const used = taken.get(t.workspaceId) ?? new Set<number>()
        const slot = used.has(0) ? 1 : 0
        used.add(slot)
        taken.set(t.workspaceId, used)
        out.push({id: t.id, agentId: t.agentId, workspaceId: t.workspaceId, slot: slot as SlotIndex, ended: null})
    }
    return out
}

export function useTerminals(workspaceId: number | null, autoIterateSetting: boolean, onAutoIterateChange?: (v: boolean) => void) {
    const [terms, setTerms] = useState<TermSlot[]>([])
    const [agents, setAgents] = useState<main.AgentChoice[]>([])
    const [busy, setBusy] = useState<SlotIndex | null>(null)
    // ListTerminals ya respondió: antes de eso «cero agentes» no significa nada.
    const [loaded, setLoaded] = useState(false)
    const [autoIterate, setAuto] = useState(autoIterateSetting)
    const current = useRef(terms)
    current.current = terms

    // Reengancha las terminales que siguen vivas en Go (la UI se recargó).
    useEffect(() => {
        ListTerminals()
            .then((list) => setTerms((prev) => (prev.length ? prev : assign(list))))
            .catch(() => {})
            .finally(() => setLoaded(true))
    }, [])

    // Una terminal que Go abrió por su cuenta («Retomar sesión» desde el árbol de
    // checkpoints): se la lleva a la pestaña «Agente» que esté libre y se muestra.
    useEffect(
        () =>
            onTerminalOpened((id) => {
                ListTerminals()
                    .then((list) => {
                        const info = list.find((t) => t.id === id)
                        if (!info || current.current.some((t) => t.id === id)) return
                        const used = new Set(current.current.filter((t) => t.workspaceId === info.workspaceId).map((t) => t.slot))
                        const slot: SlotIndex = used.has(0) ? 1 : 0
                        setTerms((l) => [...l, {id, agentId: info.agentId, workspaceId: info.workspaceId, slot, ended: null}])
                        showPanel(slot === 0 ? 'agent-1' : 'agent-2')
                    })
                    .catch(() => {})
            }),
        [],
    )

    useEffect(() => onTerminalExit((e) => setTerms((prev) => prev.map((t) => (t.id === e.id ? {...t, ended: e.code} : t)))), [])

    // Cerrar la pestaña de un proyecto cierra sus terminales en Go: acá se sueltan.
    useEffect(() => onProjectClosed((ws) => setTerms((prev) => prev.filter((t) => t.workspaceId !== ws))), [])

    // La configuración llega después del primer render.
    useEffect(() => setAuto(autoIterateSetting), [autoIterateSetting])

    const refreshAgents = useCallback(() => {
        ListAgentChoices()
            .then(setAgents)
            .catch(() => setAgents([]))
    }, [])
    useEffect(refreshAgents, [refreshAgents])

    const slotsOf = (ws: number | null): [TermSlot | undefined, TermSlot | undefined] => [
        terms.find((t) => t.workspaceId === ws && t.slot === 0),
        terms.find((t) => t.workspaceId === ws && t.slot === 1),
    ]

    // start abre un agente en la pestaña `slot`. Si ya había uno, lo cierra
    // antes (quien llama pide la confirmación).
    const start = useCallback(
        async (slot: SlotIndex, agentId: string, mode: StartMode, sessionId = '') => {
            if (workspaceId == null) return
            setBusy(slot)
            try {
                const prev = current.current.find((t) => t.workspaceId === workspaceId && t.slot === slot)
                if (prev) {
                    setTerms((l) => l.filter((t) => t.id !== prev.id))
                    await CloseTerminal(prev.id).catch(() => {})
                }
                const id = await StartAgentTerminal(workspaceId, agentId, mode, sessionId, START_COLS, START_ROWS)
                setTerms((l) => [...l, {id, agentId, workspaceId, slot, ended: null}])
            } catch (e) {
                toast.error(errorText(e))
            } finally {
                setBusy(null)
            }
        },
        [workspaceId],
    )

    const close = useCallback(async (id: string) => {
        setTerms((l) => l.filter((t) => t.id !== id))
        await CloseTerminal(id).catch((e) => toast.error(errorText(e)))
    }, [])

    const setAutoIterate = useCallback(
        async (v: boolean) => {
            setAuto(v)
            try {
                await SetAutoIterate(v)
                onAutoIterateChange?.(v)
            } catch (e) {
                setAuto(!v)
                toast.error(errorText(e))
            }
        },
        [onAutoIterateChange],
    )

    // Las terminales de TODOS los proyectos abiertos, por pestaña «Agente»: las de
    // los proyectos de fondo siguen montadas (ocultas) para conservar su pantalla.
    const all: [TermSlot[], TermSlot[]] = [terms.filter((t) => t.slot === 0), terms.filter((t) => t.slot === 1)]
    // Agentes vivos por proyecto (indicador de las pestañas de proyecto).
    const alive = useMemo(() => {
        const out: Record<number, number> = {}
        for (const t of terms) if (t.ended == null) out[t.workspaceId] = (out[t.workspaceId] ?? 0) + 1
        return out
    }, [terms])

    return {slots: slotsOf(workspaceId), all, alive, loaded, busy, agents, refreshAgents, start, close, autoIterate, setAutoIterate}
}
