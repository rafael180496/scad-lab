import type {ComponentProps} from 'react'
import {Box, Download, Image, Loader2, PanelLeftClose, PanelLeftOpen, Settings} from 'lucide-react'
import {Button, IconButton} from '../ui/Button'
import {Logo} from './Logo'
import {ViewMenu, type ShotView} from '../dock/ViewMenu'
import {ProjectTabs} from './ProjectTabs'
import {useNarrow} from '../../hooks/useNarrow'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {withKeys} from '../../lib/platform'
import {TitleBarDoubleClick} from '../../../wailsjs/go/main/App'

// Ancho de cabecera (a interfaz 100 %) por debajo del cual los botones pasan a solo ícono.
const HEADER_COMPACT_W = 900

export function Header({
    platform,
    projects,
    sidebarCollapsed,
    onToggleSidebar,
    statusBarHidden,
    onToggleStatusBar,
    viewShot,
    onSettings,
    exportWhy,
    exportBusy,
    onExportStl,
    onExport3mf,
    onExportPng,
}: {
    platform: string
    /** Pestañas de los proyectos abiertos (S8), a la derecha del logo. */
    projects: ComponentProps<typeof ProjectTabs>
    sidebarCollapsed: boolean
    onToggleSidebar: () => void
    statusBarHidden: boolean
    onToggleStatusBar: () => void
    /** Solo el banco de capturas: abre el menú Ver o el diálogo de diseños. */
    viewShot?: ShotView
    onSettings: () => void
    /** Por qué no se puede exportar ahora ('' = se puede). */
    exportWhy: string
    exportBusy: boolean
    onExportStl: () => void
    onExport3mf: () => void
    onExportPng: () => void
}) {
    const t = useT()
    // Ventana angosta (o interfaz agrandada): los botones de exportación pierden
    // el rótulo y quedan como íconos con su `title`.
    const [compact, ref] = useNarrow(HEADER_COMPACT_W)
    // Sin un render correcto los botones quedan apagados pero con su `title`
    // diciendo por qué: `aria-disabled` y no `disabled`, que no deja pasar el
    // cursor y el `title` nunca se vería.
    const exportBtn = (hint: string, combo: string | null, onClick: () => void, busy = false) => {
        const base = combo ? withKeys(hint, combo) : hint
        const off = exportWhy !== ''
        return {
            title: off ? fmt(t.export.notAvailable, {hint: base, why: exportWhy}) : base,
            'aria-disabled': off || busy,
            onClick: () => !off && !busy && onClick(),
            className: cn((off || busy) && 'cursor-not-allowed opacity-40 hover:bg-transparent hover:text-muted'),
        }
    }
    return (
        <header
            ref={ref}
            data-lock
            // Doble clic en la cabecera = doble clic en la barra de título nativa
            // (agrandar/restaurar, o lo que diga la preferencia de macOS). Solo
            // sobre el fondo arrastrable: un botón, una pestaña o un menú tienen
            // su propio doble clic (renombrar, etc.).
            onDoubleClick={(e) => {
                if ((e.target as HTMLElement).closest('.no-drag, button, input, a, [role="tab"]')) return
                void TitleBarDoubleClick().catch(() => {})
            }}
            className={cn(
                'drag flex h-11 min-w-0 shrink-0 items-center gap-3 overflow-clip border-b border-line bg-surface pr-2',
                // Semáforos de macOS (TitleBarHiddenInset) a la izquierda.
                platform === 'darwin' ? 'pl-[82px]' : 'pl-3',
            )}
        >
            <IconButton
                title={withKeys(sidebarCollapsed ? t.layout.showSidebar : t.layout.hideSidebar, 'mod+b')}
                aria-pressed={!sidebarCollapsed}
                onClick={onToggleSidebar}
                className="-mx-1"
            >
                {sidebarCollapsed ? <PanelLeftOpen className="icon-ui-16" /> : <PanelLeftClose className="icon-ui-16" />}
            </IconButton>
            <div className="flex shrink-0 items-center gap-2">
                <Logo className="icon-ui-20 text-fg" />
                <span className="text-ui-13-5 font-semibold tracking-tight">ScadLab</span>
            </div>
            <ProjectTabs {...projects} />
            <div className="min-w-0 flex-1" />
            <ViewMenu
                compact={compact}
                sidebarHidden={sidebarCollapsed}
                onToggleSidebar={onToggleSidebar}
                statusBarHidden={statusBarHidden}
                onToggleStatusBar={onToggleStatusBar}
                shot={viewShot}
            />
            <div className="flex shrink-0 items-center gap-1">
                <Button variant="ghost" {...exportBtn(t.header.exportStlHint, 'mod+shift+e', onExportStl, exportBusy)}>
                    {exportBusy ? <Loader2 className="icon-ui-14 animate-spin" /> : <Box className="icon-ui-14" />}
                    {compact ? null : t.header.exportStl}
                </Button>
                <Button variant="ghost" {...exportBtn(t.header.export3mfHint, 'mod+e', onExport3mf)}>
                    <Download className="icon-ui-14" />
                    {compact ? null : t.header.export3mf}
                </Button>
                <Button variant="ghost" {...exportBtn(t.header.exportPngHint, null, onExportPng)}>
                    <Image className="icon-ui-14" />
                    {compact ? null : t.header.exportPng}
                </Button>
                <span className="mx-1 h-5 w-px bg-line" />
                <IconButton title={t.header.settingsHint} onClick={onSettings}>
                    <Settings className="icon-ui-16" />
                </IconButton>
            </div>
        </header>
    )
}
