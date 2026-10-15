import {Box, Download, Image, PanelLeftClose, PanelLeftOpen, Settings} from 'lucide-react'
import {Button, IconButton} from '../ui/Button'
import {Logo} from './Logo'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {withKeys} from '../../lib/platform'

export function Header({
    platform,
    workspaceName,
    sidebarCollapsed,
    onToggleSidebar,
    onSettings,
}: {
    platform: string
    workspaceName?: string
    sidebarCollapsed: boolean
    onToggleSidebar: () => void
    onSettings: () => void
}) {
    const t = useT()
    // Las exportaciones llegan en S9: se muestran deshabilitadas para que la
    // cabecera ya tenga su forma final y no cambie de lugar más adelante.
    const soon = (hint: string) => `${hint} — ${fmt(t.app.phase, {phase: 'S9'})}`
    return (
        <header
            className={cn(
                'drag flex h-11 shrink-0 items-center gap-3 border-b border-line bg-surface pr-2',
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
                {sidebarCollapsed ? <PanelLeftOpen className="size-4" /> : <PanelLeftClose className="size-4" />}
            </IconButton>
            <div className="flex items-center gap-2">
                <Logo className="size-5 text-fg" />
                <span className="text-[13.5px] font-semibold tracking-tight">ScadLab</span>
            </div>
            {workspaceName ? (
                <>
                    <span className="text-faint">/</span>
                    <span className="truncate font-mono text-[12px] text-muted">{workspaceName}</span>
                </>
            ) : null}
            <div className="flex-1" />
            <div className="flex items-center gap-1">
                <Button variant="ghost" title={soon(t.header.exportStlHint)} disabled>
                    <Box className="size-3.5" />
                    {t.header.exportStl}
                </Button>
                <Button variant="ghost" title={soon(t.header.export3mfHint)} disabled>
                    <Download className="size-3.5" />
                    {t.header.export3mf}
                </Button>
                <Button variant="ghost" title={soon(t.header.exportPngHint)} disabled>
                    <Image className="size-3.5" />
                    {t.header.exportPng}
                </Button>
                <span className="mx-1 h-5 w-px bg-line" />
                <IconButton title={t.header.settingsHint} onClick={onSettings}>
                    <Settings className="size-4" />
                </IconButton>
            </div>
        </header>
    )
}
