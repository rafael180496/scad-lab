import {useCallback, useEffect, useMemo} from 'react'
import {DockviewReact, type DockviewReadyEvent, type DockviewTheme, type GetTabContextMenuItemsParams} from 'dockview-react'
import 'dockview-react/dist/styles/dockview.css'
import './dock.css'
import {t as tNow, useT} from '../../i18n'
import {appearance} from '../../lib/appearance'
import {isMac, isMod} from '../../lib/platform'
import {DockTab, EmptyDock, GroupActions, Slot, tabActions} from './DockParts'
import {attach, dockState, equalizeAtSash, exitZen, rescaleConstraints, shortcutPanel, toggleZen, type SavedLayout} from './dock'
import {PANEL_IDS, PANEL_SHORTCUT} from './panels'

const THEME: DockviewTheme = {name: 'scadlab', className: 'dockview-theme-scadlab'}
const COMPONENTS = {slot: Slot}

// El menú contextual de la pestaña lo dibuja dockview; sus entradas salen de la
// misma lista que el botón «…» del grupo.
function tabMenu({panel}: GetTabContextMenuItemsParams) {
    return tabActions(tNow(), panel).map((a) => (a.separator ? ('separator' as const) : {label: a.label, action: a.run, disabled: a.disabled}))
}

// Área de trabajo: el dock con los paneles. Reemplaza a la grilla fija con
// divisores de S4 (Workbench). Ver claude/04-frontend.md, «Paneles acoplables».
export function DockWorkbench({initialLayout, savedLayouts}: {initialLayout: string; savedLayouts: SavedLayout[]}) {
    const t = useT()
    const scale = appearance.use((s) => s.uiScale)
    const zen = dockState.use((s) => s.zen)

    // Los tamaños mínimos escalan con la interfaz. (El dock no se desengancha al
    // desmontar: StrictMode simula un desmontaje y dockview conserva su instancia
    // sin volver a llamar a onReady.)
    useEffect(() => rescaleConstraints(), [scale])

    const onReady = useCallback(({api}: DockviewReadyEvent) => attach(api, initialLayout, savedLayouts), [initialLayout, savedLayouts])

    // Atajos (en captura, antes que Monaco y xterm):
    //   ⌘/Ctrl+Shift+1…6   mostrar y enfocar un panel (si ya está enfocado, lo oculta)
    //   ⌘/Ctrl+Shift+0     modo enfoque
    //   ⌘⌥1…6 (macOS)      lo mismo que ⌘⇧N: ⌘⇧3/4/5 los reserva el sistema para capturas
    // Esc sale del modo enfoque, salvo que el foco esté en el editor o la terminal,
    // donde Esc ya significa otra cosa.
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => {
            if (!isMod(e)) return
            const digit = /^(?:Digit|Numpad)([0-6])$/.exec(e.code)?.[1]
            if (!digit) return
            const n = Number(digit)
            const viaShift = e.shiftKey && !e.altKey
            const viaAlt = isMac() && e.altKey && !e.shiftKey
            if (!viaShift && !viaAlt) return
            if (n === 0) {
                if (!viaShift) return
                e.preventDefault()
                e.stopPropagation()
                toggleZen()
                return
            }
            const id = PANEL_IDS.find((p) => PANEL_SHORTCUT[p] === n)
            if (!id) return
            e.preventDefault()
            e.stopPropagation()
            shortcutPanel(id)
        }
        const onEsc = (e: KeyboardEvent) => {
            if (e.key !== 'Escape' || e.defaultPrevented || !dockState.get().zen) return
            const el = e.target instanceof Element ? e.target : null
            if (el?.closest('.monaco-editor, .xterm, dialog, [role="menu"]')) return
            exitZen()
        }
        window.addEventListener('keydown', onKey, true)
        window.addEventListener('keydown', onEsc)
        return () => {
            window.removeEventListener('keydown', onKey, true)
            window.removeEventListener('keydown', onEsc)
        }
    }, [])

    const components = useMemo(() => COMPONENTS, [])
    return (
        <main
            data-lock
            className="relative flex min-h-0 min-w-0 flex-1 overflow-clip p-2"
            // Doble clic en un divisor: reparte el espacio entre sus dos lados.
            onDoubleClick={(e) => {
                const sash = (e.target as Element).closest<HTMLElement>('.dv-sash')
                if (sash) equalizeAtSash(sash)
            }}
        >
            <div className="relative min-h-0 min-w-0 flex-1 overflow-clip rounded-[var(--radius-panel)] border border-line bg-surface">
                <DockviewReact
                    components={components}
                    defaultTabComponent={DockTab}
                    rightHeaderActionsComponent={GroupActions}
                    watermarkComponent={EmptyDock}
                    theme={THEME}
                    defaultRenderer="always"
                    // Arrastre con eventos de puntero y no HTML5: en la webview de Wails el
                    // arrastre HTML5 no es fiable (hay un manejador de archivos soltados),
                    // y sin ventanas nativas no hace falta el de entre ventanas.
                    dndStrategy="pointer"
                    // Al achicar la ventana los grupos se reparten en proporción (no el último
                    // absorbe todo): es el valor por defecto, explícito porque se cuenta con él.
                    proportionalLayout
                    floatingGroupBounds="boundedWithinViewport"
                    floatingGroupDragHandle="tabbar"
                    getTabContextMenuItems={tabMenu}
                    onReady={onReady}
                />
                {zen ? (
                    <button
                        type="button"
                        title={t.dock.zenExit}
                        onClick={exitZen}
                        className="no-drag absolute top-1.5 right-3 z-[100] rounded-full border border-line-strong bg-raised/95 px-3 py-1 text-ui-11 text-muted shadow transition-colors hover:text-fg"
                    >
                        {t.dock.zenBanner}
                    </button>
                ) : null}
            </div>
        </main>
    )
}
