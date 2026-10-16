import {lazy, Suspense, useEffect, useState} from 'react'
import {History, Play, Plug, Plus, Repeat, Settings, SquareTerminal, X} from 'lucide-react'
import type {main} from '../../../wailsjs/go/models'
import type {StartMode, TermSlot} from '../../hooks/useTerminals'
import {Button, IconButton} from '../ui/Button'
import {Select} from '../ui/Field'
import {confirmDialog} from '../ui/Dialog'
import {useNarrow} from '../../hooks/useNarrow'
import {mcpHealth} from '../../hooks/useMcpStatus'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {SessionsMenu} from './SessionsMenu'
import {terminalShot} from './shot'

// Ancho de la barra (a interfaz 100 %) por debajo del cual los botones pasan a
// solo ícono con su `title` y el interruptor de auto-iteración se abrevia.
const TOOLBAR_COMPACT_W = 860

// xterm.js (~300 kB) entra con lazy(): la app abre sin cargarlo y solo se baja
// cuando hay una terminal que mostrar.
const TerminalView = lazy(() => import('./TerminalView').then((m) => ({default: m.TerminalView})))

// Contenido de una pestaña «Agente N»: barra de control + terminal. La terminal
// sigue montada aunque la pestaña no se vea (mantiene el scrollback y el PTY
// al tamaño que tenía); `visible` solo decide si se mide y toma el foco.
export function AgentTerminalTab({
    visible,
    workspaceId,
    term,
    allTerms,
    busy,
    agents,
    defaultAgent,
    autoIterate,
    onAutoIterate,
    mcp,
    onStart,
    onClose,
    onOpenSettings,
}: {
    visible: boolean
    workspaceId: number | null
    /** La terminal de este proyecto en esta pestaña «Agente» (la que se ve). */
    term: TermSlot | undefined
    /** Las de todos los proyectos abiertos en esta pestaña: las de fondo siguen montadas, ocultas. */
    allTerms: TermSlot[]
    busy: boolean
    agents: main.AgentChoice[]
    defaultAgent: string
    autoIterate: boolean
    onAutoIterate: (v: boolean) => void
    /** Estado del servidor MCP en este workspace (null = sin workspace o aún sin leer). */
    mcp: main.MCPStatus | null
    onStart: (agentId: string, mode: StartMode, sessionId?: string) => void
    onClose: (id: string) => void
    onOpenSettings: () => void
}) {
    const t = useT()
    const [compact, bar] = useNarrow(TOOLBAR_COMPACT_W)
    const usable = agents.filter((a) => a.found && a.enabled)
    const [pick, setPick] = useState(defaultAgent)
    const [sessionsOpen, setSessionsOpen] = useState(!!terminalShot.openSessions)

    // La lista de agentes llega después, y puede no incluir al elegido.
    useEffect(() => {
        if (term) setPick(term.agentId)
        else if (usable.length && !usable.some((a) => a.id === pick)) setPick(usable.find((a) => a.id === defaultAgent)?.id ?? usable[0].id)
    }, [term?.id, usable.map((a) => a.id).join(','), defaultAgent]) // eslint-disable-line react-hooks/exhaustive-deps

    const agent = agents.find((a) => a.id === pick)
    const name = agent?.name ?? pick
    const noWorkspace = workspaceId == null
    const can = !noWorkspace && !busy && !!agent && agent.found && agent.enabled
    const running = !!term && term.ended == null

    // Reemplazar un agente vivo pide confirmación: abrir otro lo termina.
    const launch = async (mode: StartMode, sessionId?: string) => {
        setSessionsOpen(false)
        if (running && !(await confirmDialog(t.terminal.replaceConfirm, t.terminal.replace, t.terminal.cancel))) return
        onStart(pick, mode, sessionId)
    }

    return (
        <div className="flex h-full min-h-0 flex-col">
            {/* Si ni compacta cabe, la barra envuelve a una segunda fila: nunca corta un control. */}
            <div ref={bar} className="flex min-h-9 shrink-0 flex-wrap items-center gap-x-1.5 gap-y-1 border-b border-line px-2 py-1">
                {term ? (
                    <span
                        className={cn('size-2 shrink-0 rounded-full', running ? 'bg-ok' : 'bg-faint')}
                        title={running ? t.terminal.running : fmt(t.terminal.endedBanner, {code: term.ended ?? 0})}
                    />
                ) : null}
                <Select
                    value={pick}
                    title={t.terminal.agentSelectHint}
                    aria-label={t.terminal.agent}
                    disabled={noWorkspace || busy || agents.length === 0}
                    onChange={(e) => setPick(e.target.value)}
                    className={cn('h-7 shrink-0 text-ui-12', compact ? 'w-[11em]' : 'w-[13em]')}
                >
                    {agents.map((a) => (
                        <option key={a.id} value={a.id} disabled={!a.found || !a.enabled}>
                            {a.name}
                            {!a.found ? ` (${t.terminal.notFound})` : !a.enabled ? ` (${t.terminal.disabled})` : ''}
                        </option>
                    ))}
                </Select>
                <Button variant="primary" title={fmt(t.terminal.newSessionHint, {agent: name})} disabled={!can} onClick={() => void launch('new')}>
                    <Plus className="icon-ui-14" />
                    {compact ? null : t.terminal.newSession}
                </Button>
                <Button title={fmt(t.terminal.continueHint, {agent: name})} disabled={!can} onClick={() => void launch('continue')}>
                    <Play className="icon-ui-14" />
                    {compact ? null : t.terminal.continueLast}
                </Button>
                <span data-sessions-toggle>
                    <Button
                        title={agent && !agent.canList ? fmt(t.terminal.sessionsUnavailable, {agent: name}) : fmt(t.terminal.sessionsHint, {agent: name})}
                        disabled={!can || !agent?.canList}
                        aria-expanded={sessionsOpen}
                        onClick={() => setSessionsOpen((o) => !o)}
                    >
                        <History className="icon-ui-14" />
                        {compact ? null : t.terminal.sessions}
                    </Button>
                </span>
                <div className="ml-auto flex shrink-0 items-center gap-1.5">
                    <McpBadge status={mcp} onClick={onOpenSettings} />
                    <button
                        type="button"
                        role="switch"
                        aria-checked={autoIterate}
                        title={autoIterate ? t.terminal.autoIterateHintOn : t.terminal.autoIterateHintOff}
                        onClick={() => onAutoIterate(!autoIterate)}
                        className={cn(
                            'no-drag inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-ui-11-5 font-medium whitespace-nowrap transition-colors duration-150',
                            autoIterate ? 'border-accent/50 bg-accent-soft text-accent' : 'border-line text-faint hover:text-muted',
                        )}
                    >
                        <Repeat className="icon-ui-14" />
                        {compact ? null : `${t.terminal.autoIterate} · `}
                        {autoIterate ? t.terminal.autoIterateOn : t.terminal.autoIterateOff}
                    </button>
                    {term ? (
                        <IconButton title={t.terminal.closeHint} onClick={() => onClose(term.id)}>
                            <X className="icon-ui-16" />
                        </IconButton>
                    ) : null}
                </div>
            </div>

            <div className="relative min-h-0 flex-1 bg-surface">
                {/* Cada proyecto abierto conserva su xterm (pantalla y desplazamiento) mientras
                    está de fondo: cambiar de pestaña de proyecto solo cambia cuál se ve. */}
                {allTerms.map((tm) => (
                    <div key={tm.id} className={cn('absolute inset-0', tm.id !== term?.id && 'hidden')}>
                        <Suspense fallback={<p className="px-4 py-3 text-ui-12 text-faint">{t.app.loading}</p>}>
                            <TerminalView termId={tm.id} visible={visible && tm.id === term?.id} />
                        </Suspense>
                    </div>
                ))}
                {term ? (
                    <>
                        {term.ended != null ? (
                            <p className="selectable absolute inset-x-2 bottom-2 rounded-md border border-line bg-raised px-3 py-1.5 text-ui-12 text-muted shadow">
                                {fmt(t.terminal.endedBanner, {code: term.ended})}
                            </p>
                        ) : null}
                    </>
                ) : (
                    <EmptyState noWorkspace={noWorkspace} noAgents={agents.length > 0 && usable.length === 0} onOpenSettings={onOpenSettings} />
                )}
                {sessionsOpen && !noWorkspace ? (
                    <SessionsMenu
                        workspaceId={workspaceId}
                        agentId={pick}
                        agentName={name}
                        onClose={() => setSessionsOpen(false)}
                        onPick={(id) => void launch('resume', id)}
                    />
                ) : null}
            </div>
        </div>
    )
}

// «MCP ✓»: el servidor MCP de ScadLab está configurado en este workspace. Un
// clic lleva a Configuración → IA, donde se ve el detalle de cada archivo.
function McpBadge({status, onClick}: {status: main.MCPStatus | null; onClick: () => void}) {
    const t = useT().mcp
    if (!status) return null
    const h = mcpHealth(status)
    const label = h.kind === 'ok' ? t.badgeOn : h.kind === 'warn' ? t.badgeWarn : t.badgeOff
    const hint = h.kind === 'ok' ? t.badgeOnHint : h.kind === 'warn' ? fmt(t.badgeWarnHint, {n: h.problems}) : t.badgeOffHint
    return (
        <button
            type="button"
            title={hint}
            aria-label={hint}
            onClick={onClick}
            className={cn(
                'no-drag inline-flex h-7 items-center gap-1.5 rounded-md border px-2 text-ui-11-5 font-medium whitespace-nowrap transition-colors duration-150',
                h.kind === 'ok' && 'border-ok/40 text-ok hover:bg-surface-2',
                h.kind === 'warn' && 'border-warn/50 text-warn hover:bg-surface-2',
                h.kind === 'off' && 'border-line text-faint hover:text-muted',
            )}
        >
            <Plug className="icon-ui-14" />
            {label}
        </button>
    )
}

function EmptyState({noWorkspace, noAgents, onOpenSettings}: {noWorkspace: boolean; noAgents: boolean; onOpenSettings: () => void}) {
    const t = useT()
    return (
        <div className="flex h-full flex-col items-center justify-center gap-2 px-6 text-center">
            <SquareTerminal className="icon-ui-24 text-faint" />
            {noWorkspace ? (
                <p className="max-w-[420px] text-ui-12-5 leading-relaxed text-muted">{t.terminal.noWorkspace}</p>
            ) : noAgents ? (
                <>
                    <p className="text-ui-13 font-medium text-fg">{t.terminal.noAgents}</p>
                    <p className="max-w-[420px] text-ui-12 leading-relaxed text-muted">{t.terminal.noAgentsBody}</p>
                    <Button title={t.terminal.openSettingsHint} onClick={onOpenSettings}>
                        <Settings className="icon-ui-14" />
                        {t.terminal.openSettings}
                    </Button>
                </>
            ) : (
                <>
                    <p className="text-ui-13 font-medium text-fg">{t.terminal.emptyTitle}</p>
                    <p className="max-w-[460px] text-ui-12 leading-relaxed text-muted">{t.terminal.emptyBody}</p>
                </>
            )}
        </div>
    )
}
