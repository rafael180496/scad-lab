import {useLayoutEffect, useRef} from 'react'
import type {IDockviewHeaderActionsProps, IDockviewPanelHeaderProps, IDockviewPanelProps, IWatermarkPanelProps, IDockviewPanel} from 'dockview-react'
import {Ellipsis, Maximize2, Minimize2, X} from 'lucide-react'
import {fmt, useT, type Dict} from '../../i18n'
import {editorSession} from '../../lib/editorSession'
import {keys, withKeys} from '../../lib/platform'
import {cn} from '../../lib/cn'
import {Menu, useMenu, type MenuEntry} from '../ui/Menu'
import {
    applyPreset,
    dockState,
    dockPanel,
    floatPanel,
    hideOthers,
    hidePanel,
    panelStatus,
    showPanel,
    splitPanel,
    syncSoon,
    toggleMaximize,
} from './dock'
import {PRESET_IDS} from './layouts'
import {PANEL_ICON, PANEL_IDS, PANEL_SHORTCUT, isPanelId, panelHint, panelTitle, type PanelId} from './panels'
import {panelHostEl, parkHost} from './hosts'

// ── Contenido de un panel: un hueco donde se coloca el elemento persistente ──
// El contenido real de cada panel (PanelHost) vive fuera de dockview, en un
// elemento propio que sobrevive a ocultar, mover o flotar el panel; este
// componente solo lo cuelga del hueco que dockview le dio y lo aparca al irse.
export function Slot({api}: IDockviewPanelProps) {
    const ref = useRef<HTMLDivElement>(null)
    const id = api.id
    useLayoutEffect(() => {
        const el = ref.current
        if (!el || !isPanelId(id)) return
        const host = panelHostEl(id)
        el.appendChild(host)
        syncSoon()
        const d = api.onDidVisibilityChange(syncSoon)
        return () => {
            d.dispose()
            parkHost(host)
        }
    }, [api, id])
    return <div ref={ref} className="h-full min-h-0 w-full min-w-0 overflow-clip" />
}

// ── Pestaña ─────────────────────────────────────────────────────────────────

function useTabDot(id: PanelId): {tone: string; label: string} | null {
    const t = useT()
    const dirty = editorSession.use((s) => s.dirty)
    const a1 = panelStatus.use((s) => s.agent1)
    const a2 = panelStatus.use((s) => s.agent2)
    if (id === 'source' && dirty) return {tone: 'bg-accent', label: t.dock.unsaved}
    const agent = id === 'agent-1' ? a1 : id === 'agent-2' ? a2 : 'none'
    if (agent === 'running') return {tone: 'bg-ok', label: t.dock.agentAlive}
    if (agent === 'ended') return {tone: 'bg-faint', label: t.dock.agentEnded}
    return null
}

function ProblemsBadge() {
    const e = panelStatus.use((s) => s.errors)
    const w = panelStatus.use((s) => s.warnings)
    const i = panelStatus.use((s) => s.infos)
    const n = e + w + i
    if (n === 0) return null
    return (
        <span className={cn('scadlab-tab-badge', e > 0 ? 'bg-danger/15 text-danger' : w > 0 ? 'bg-warn/15 text-warn' : 'bg-info/15 text-info')}>{n}</span>
    )
}

// DockTab es la pestaña de editor: ícono + título en letra normal, la × (o un
// punto de estado que se vuelve × al pasar el ratón). Cerrar aquí OCULTA el
// panel; su contenido (visor, modelo del editor, terminal) sigue vivo.
export function DockTab({api}: IDockviewPanelHeaderProps) {
    const t = useT()
    const id = api.id as PanelId
    const dot = useTabDot(id)
    if (!isPanelId(id)) return null
    const Icon = PANEL_ICON[id]
    return (
        <div
            className="scadlab-tab"
            title={panelHint(t, id)}
            data-dot={dot ? '' : undefined}
            onDoubleClick={() => toggleMaximize(api.group.panels.find((p) => p.id === id))}
            onAuxClick={(e) => {
                if (e.button !== 1) return
                e.preventDefault()
                hidePanel(id)
            }}
        >
            <Icon className="scadlab-tab-icon icon-ui-14" />
            <span className="scadlab-tab-title">{panelTitle(t, id)}</span>
            {id === 'problems' ? <ProblemsBadge /> : null}
            <span className="scadlab-tab-end">
                {dot ? <span className={cn('scadlab-tab-dot', dot.tone)} role="img" aria-label={dot.label} title={dot.label} /> : null}
                <button
                    type="button"
                    className="scadlab-tab-close"
                    title={t.dock.closeTab}
                    aria-label={t.dock.closeTab}
                    onPointerDown={(e) => e.preventDefault()}
                    onClick={(e) => {
                        e.stopPropagation()
                        hidePanel(id)
                    }}
                    onDoubleClick={(e) => e.stopPropagation()}
                >
                    <X className="icon-ui-12" />
                </button>
            </span>
        </div>
    )
}

// ── Menú de la pestaña (clic derecho y botón «…» del grupo) ────────────────

type TabAction = {separator?: false; label: string; title: string; run: () => void; disabled?: boolean} | {separator: true}

export function tabActions(t: Dict, panel: IDockviewPanel): TabAction[] {
    const id = panel.id as PanelId
    const floating = panel.api.location.type === 'floating'
    const maximized = panel.api.isMaximized()
    const alone = panel.group.panels.length < 2
    return [
        {label: t.dock.tab.hide, title: t.dock.closeTab, run: () => hidePanel(id)},
        {label: t.dock.tab.hideOthers, title: t.dock.tab.hideOthers, run: () => hideOthers(id), disabled: alone},
        {separator: true},
        {
            label: maximized ? t.dock.tab.restore : t.dock.tab.maximize,
            title: maximized ? t.dock.restorePanelHint : t.dock.maximizePanelHint,
            run: () => toggleMaximize(panel),
        },
        floating
            ? {label: t.dock.tab.dock, title: t.dock.tab.dock, run: () => dockPanel(panel)}
            : {label: t.dock.tab.float, title: t.dock.tab.float, run: () => floatPanel(panel)},
        {separator: true},
        {label: t.dock.tab.moveRight, title: t.dock.tab.moveRight, run: () => splitPanel(panel, 'right'), disabled: alone},
        {label: t.dock.tab.moveDown, title: t.dock.tab.moveDown, run: () => splitPanel(panel, 'bottom'), disabled: alone},
    ]
}

// ── Acciones del grupo: maximizar y más opciones ───────────────────────────

export function GroupActions({group, activePanel}: IDockviewHeaderActionsProps) {
    const t = useT()
    const menu = useMenu()
    dockState.use((s) => s.maximized) // vuelve a dibujar el botón al maximizar o restaurar
    if (!activePanel) return null
    const maximized = group.api.isMaximized()
    const entries: MenuEntry[] = tabActions(t, activePanel).map((a) =>
        a.separator ? {kind: 'separator'} : {label: a.label, title: a.title, disabled: a.disabled, onSelect: a.run},
    )
    return (
        <div className="scadlab-group-actions">
            <button
                type="button"
                className="scadlab-group-action"
                aria-pressed={maximized}
                title={maximized ? t.dock.restorePanelHint : t.dock.maximizePanelHint}
                aria-label={maximized ? t.dock.restorePanel : t.dock.maximizePanel}
                onClick={() => toggleMaximize(activePanel)}
            >
                {maximized ? <Minimize2 className="icon-ui-14" /> : <Maximize2 className="icon-ui-14" />}
            </button>
            <button
                type="button"
                className="scadlab-group-action"
                aria-expanded={menu.open}
                title={t.dock.groupMenuHint}
                aria-label={t.dock.groupMenuHint}
                onClick={menu.toggle}
            >
                <Ellipsis className="icon-ui-14" />
            </button>
            {menu.anchor ? <Menu anchor={menu.anchor} entries={entries} onClose={menu.close} align="end" label={t.dock.groupMenu} /> : null}
        </div>
    )
}

// ── Sin paneles a la vista ──────────────────────────────────────────────────

export function EmptyDock(_: IWatermarkPanelProps) {
    const t = useT()
    const present = dockState.use((s) => s.present)
    return (
        <div className="flex h-full min-h-0 w-full flex-col items-center justify-center gap-5 overflow-auto bg-surface px-6 py-6 text-center">
            <div className="max-w-[460px]">
                <p className="text-ui-14 font-medium text-fg">{t.dock.empty.title}</p>
                <p className="mt-1 text-ui-12-5 leading-relaxed text-muted">{fmt(t.dock.empty.body, {keys: keys('mod+shift+1') + '…6'})}</p>
            </div>
            <div className="flex max-w-[560px] flex-col items-center gap-2">
                <span className="font-mono text-ui-10 tracking-[0.12em] text-faint uppercase">{t.dock.empty.panels}</span>
                <div className="flex flex-wrap justify-center gap-2">
                    {PANEL_IDS.map((id) => {
                        const Icon = PANEL_ICON[id]
                        return (
                            <button
                                key={id}
                                type="button"
                                disabled={present[id]}
                                title={withKeys(fmt(t.dock.panelToggleHint, {panel: panelTitle(t, id)}), `mod+shift+${PANEL_SHORTCUT[id]}`)}
                                onClick={() => showPanel(id)}
                                className="no-drag inline-flex h-8 items-center gap-1.5 rounded-md border border-line-strong px-3 text-ui-12 text-fg transition-colors hover:bg-surface-2 disabled:opacity-40"
                            >
                                <Icon className="icon-ui-14 text-accent" />
                                {panelTitle(t, id)}
                            </button>
                        )
                    })}
                </div>
            </div>
            <div className="flex max-w-[560px] flex-col items-center gap-2">
                <span className="font-mono text-ui-10 tracking-[0.12em] text-faint uppercase">{t.dock.empty.layouts}</span>
                <div className="flex flex-wrap justify-center gap-2">
                    {PRESET_IDS.map((p) => (
                        <button
                            key={p}
                            type="button"
                            title={t.dock.presetHints[p]}
                            onClick={() => applyPreset(p)}
                            className="no-drag inline-flex h-8 items-center rounded-md bg-surface-2 px-3 text-ui-12 text-fg transition-colors hover:bg-raised"
                        >
                            {t.dock.presets[p]}
                        </button>
                    ))}
                </div>
            </div>
        </div>
    )
}
