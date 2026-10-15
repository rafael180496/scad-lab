import {useEffect, useRef, useState} from 'react'
import {Loader2} from 'lucide-react'
import type {agentsessions} from '../../../wailsjs/go/models'
import {ListAgentSessions} from '../../../wailsjs/go/main/App'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {sessionDate} from './format'

// Lista de conversaciones previas de un agente en este workspace. Se pide al
// abrirse (no junto con la terminal): leer las sesiones de un CLI recorre
// archivos del usuario y casi siempre se abre la terminal sin mirarlas.
export function SessionsMenu({
    workspaceId,
    agentId,
    agentName,
    onPick,
    onClose,
}: {
    workspaceId: number
    agentId: string
    agentName: string
    onPick: (sessionId: string) => void
    onClose: () => void
}) {
    const t = useT()
    const [list, setList] = useState<agentsessions.Session[] | null>(null)
    const [error, setError] = useState('')
    const box = useRef<HTMLDivElement>(null)

    useEffect(() => {
        let alive = true
        setList(null)
        setError('')
        ListAgentSessions(workspaceId, agentId)
            .then((l) => alive && setList(l))
            .catch((e) => alive && setError(errorText(e)))
        return () => {
            alive = false
        }
    }, [workspaceId, agentId])

    // Se cierra con Escape o con un clic fuera. El botón que lo abre queda
    // fuera del menú: su propio clic lo alterna.
    useEffect(() => {
        const down = (e: MouseEvent) => {
            if (box.current && !box.current.contains(e.target as Node) && !(e.target as Element).closest('[data-sessions-toggle]')) onClose()
        }
        const key = (e: KeyboardEvent) => e.key === 'Escape' && onClose()
        document.addEventListener('mousedown', down)
        document.addEventListener('keydown', key)
        return () => {
            document.removeEventListener('mousedown', down)
            document.removeEventListener('keydown', key)
        }
    }, [onClose])

    return (
        <div
            ref={box}
            role="menu"
            className="absolute top-1 left-2 z-20 flex max-h-[calc(100%-8px)] w-[460px] max-w-[calc(100%-16px)] flex-col overflow-hidden rounded-lg border border-line-strong bg-raised shadow-lg shadow-black/30"
        >
            <div className="shrink-0 border-b border-line px-3 py-1.5 font-mono text-ui-10-5 tracking-[0.12em] text-muted uppercase">
                {fmt(t.terminal.sessionsTitle, {agent: agentName})}
            </div>
            <div className="min-h-0 flex-1 overflow-auto">
                {error ? (
                    <p className="selectable px-3 py-2 text-ui-12 text-danger">
                        {t.terminal.sessionsError}: {error}
                    </p>
                ) : list === null ? (
                    <p className="flex items-center gap-2 px-3 py-2 text-ui-12 text-muted">
                        <Loader2 className="icon-ui-14 animate-spin" />
                        {t.terminal.sessionsLoading}
                    </p>
                ) : list.length === 0 ? (
                    <p className="px-3 py-2 text-ui-12 text-muted">{fmt(t.terminal.sessionsEmpty, {agent: agentName})}</p>
                ) : (
                    list.map((s) => {
                        const title = s.title || t.terminal.untitled
                        return (
                            <button
                                key={s.id}
                                type="button"
                                role="menuitem"
                                title={fmt(t.terminal.resumeHint, {title})}
                                onClick={() => onPick(s.id)}
                                className="flex w-full items-baseline gap-3 px-3 py-1.5 text-left transition-colors duration-100 hover:bg-surface-2"
                            >
                                <span className={s.title ? 'min-w-0 flex-1 truncate text-ui-12-5 text-fg' : 'min-w-0 flex-1 truncate text-ui-12-5 text-faint italic'}>{title}</span>
                                <span className="shrink-0 font-mono text-ui-10-5 text-faint">{sessionDate(s.updatedAt)}</span>
                            </button>
                        )
                    })
                )}
            </div>
        </div>
    )
}
