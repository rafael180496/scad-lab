import {useEffect, useRef, useState} from 'react'
import {Eye} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {keys, withKeys} from '../../lib/platform'
import {Button} from '../ui/Button'
import {confirmDialog, Dialog} from '../ui/Dialog'
import {Input} from '../ui/Field'
import {Menu, useMenu, type MenuEntry} from '../ui/Menu'
import {toast} from '../ui/Toast'
import {
    applyPreset,
    applySaved,
    deleteSaved,
    dockState,
    resetLayout,
    saveCurrentAs,
    toggleMaximize,
    togglePanel,
    toggleZen,
} from './dock'
import {PRESET_IDS, parseEnvelope} from './layouts'
import {PANEL_ICON, PANEL_IDS, PANEL_SHORTCUT, panelTitle} from './panels'

// Se muestra en la cabecera, entre el nombre del workspace y las exportaciones.
export type ShotView = 'menu' | 'layouts'

export function ViewMenu({
    compact,
    sidebarHidden,
    onToggleSidebar,
    statusBarHidden,
    onToggleStatusBar,
    shot,
}: {
    compact: boolean
    sidebarHidden: boolean
    onToggleSidebar: () => void
    statusBarHidden: boolean
    onToggleStatusBar: () => void
    /** Solo el banco de capturas: abre el menú o el diálogo de diseños sin clics. */
    shot?: ShotView
}) {
    const t = useT()
    const menu = useMenu()
    const ready = dockState.use((s) => s.ready)
    const present = dockState.use((s) => s.present)
    const layouts = dockState.use((s) => s.layouts)
    const zen = dockState.use((s) => s.zen)
    const maximized = dockState.use((s) => s.maximized)
    const [saveOpen, setSaveOpen] = useState(false)
    const [manageOpen, setManageOpen] = useState(shot === 'layouts')

    const {openAt} = menu
    useEffect(() => {
        const el = document.querySelector<HTMLElement>('[data-view-menu]')
        if (shot === 'menu' && ready && el) openAt(el)
    }, [shot, ready, openAt])

    const panelEntries: MenuEntry[] = PANEL_IDS.map((id) => {
        const Icon = PANEL_ICON[id]
        const title = panelTitle(t, id)
        return {
            label: title,
            title: withKeys(fmt(t.dock.panelToggleHint, {panel: title}), `mod+shift+${PANEL_SHORTCUT[id]}`),
            keys: keys(`mod+shift+${PANEL_SHORTCUT[id]}`),
            checked: present[id],
            icon: <Icon className="icon-ui-14" />,
            keepOpen: true,
            onSelect: () => togglePanel(id),
        }
    })

    const entries: MenuEntry[] = [
        {kind: 'heading', label: t.dock.panelsHeading},
        ...panelEntries,
        {kind: 'separator'},
        {kind: 'heading', label: t.dock.barsHeading},
        {
            label: t.layout.sidebar,
            title: withKeys(sidebarHidden ? t.layout.showSidebar : t.layout.hideSidebar, 'mod+b'),
            keys: keys('mod+b'),
            checked: !sidebarHidden,
            keepOpen: true,
            onSelect: onToggleSidebar,
        },
        {
            label: t.dock.statusBar,
            title: statusBarHidden ? t.dock.showStatusBar : t.dock.hideStatusBar,
            checked: !statusBarHidden,
            keepOpen: true,
            onSelect: onToggleStatusBar,
        },
        {kind: 'separator'},
        {kind: 'heading', label: t.dock.windowHeading},
        {
            label: maximized ? t.dock.restorePanel : t.dock.maximizePanel,
            title: maximized ? t.dock.restorePanelHint : t.dock.maximizePanelHint,
            disabled: !maximized && Object.values(present).every((v) => !v),
            onSelect: () => toggleMaximize(),
        },
        {
            label: zen ? t.dock.zenExit : t.dock.zen,
            title: withKeys(t.dock.zenHint, 'mod+shift+0'),
            keys: keys('mod+shift+0'),
            onSelect: toggleZen,
        },
        {kind: 'separator'},
        {kind: 'heading', label: t.dock.layoutsHeading},
        ...PRESET_IDS.map((p): MenuEntry => ({label: t.dock.presets[p], title: t.dock.presetHints[p], onSelect: () => applyPreset(p)})),
        ...layouts.map(
            (l): MenuEntry => ({label: l.name, title: fmt(t.dock.applyLayoutHint, {name: l.name}), onSelect: () => applySaved(l.name)}),
        ),
        {kind: 'separator'},
        {label: t.dock.reset, title: t.dock.resetHint, onSelect: resetLayout},
        {label: t.dock.saveAs, title: t.dock.saveAsHint, onSelect: () => setSaveOpen(true)},
        {label: t.dock.manage, title: t.dock.manageHint, onSelect: () => setManageOpen(true)},
    ]

    return (
        <>
            <Button
                data-view-menu=""
                variant="ghost"
                title={t.dock.viewHint}
                aria-haspopup="menu"
                aria-expanded={menu.open}
                onClick={menu.toggle}
                disabled={!ready}
            >
                <Eye className="icon-ui-14" />
                {compact ? null : t.dock.view}
            </Button>
            {menu.anchor ? <Menu anchor={menu.anchor} entries={entries} onClose={menu.close} label={t.dock.view} /> : null}
            <SaveLayoutDialog open={saveOpen} onClose={() => setSaveOpen(false)} />
            <LayoutsDialog open={manageOpen} onClose={() => setManageOpen(false)} />
        </>
    )
}

function SaveLayoutDialog({open, onClose}: {open: boolean; onClose: () => void}) {
    const t = useT()
    const [name, setName] = useState('')
    const [busy, setBusy] = useState(false)
    const form = useRef<HTMLFormElement>(null)
    // El foco al campo: el Dialog enfoca su propio <dialog> después de abrirlo (y
    // un autoFocus de React corre antes, con el diálogo todavía cerrado), así que
    // se hace aquí, cuando ya está abierto.
    useEffect(() => {
        if (!open) return
        setName('')
        form.current?.querySelector('input')?.focus()
    }, [open])
    const clean = name.trim()
    const save = async () => {
        if (!clean || busy) return
        const exists = dockState.get().layouts.some((l) => l.name.toLowerCase() === clean.toLowerCase())
        if (exists && !(await confirmDialog(fmt(t.dock.saveDialog.replaceConfirm, {name: clean}), t.dock.saveDialog.replace, t.dock.saveDialog.cancel))) return
        setBusy(true)
        const ok = await saveCurrentAs(clean)
        setBusy(false)
        if (ok) {
            toast.ok(fmt(t.dock.saveDialog.saved, {name: clean}))
            onClose()
        }
    }
    return (
        <Dialog
            open={open}
            onClose={onClose}
            title={t.dock.saveDialog.title}
            closeLabel={t.dock.saveDialog.cancel}
            className="w-[440px]"
            footer={
                <>
                    <Button title={t.dock.saveDialog.cancel} onClick={onClose}>
                        {t.dock.saveDialog.cancel}
                    </Button>
                    <Button variant="primary" title={t.dock.saveDialog.saveHint} disabled={!clean || busy} onClick={() => void save()}>
                        {t.dock.saveDialog.save}
                    </Button>
                </>
            }
        >
            <form
                ref={form}
                className="flex flex-col gap-2 px-4 py-4"
                onSubmit={(e) => {
                    e.preventDefault()
                    void save()
                }}
            >
                <label className="flex flex-col gap-1">
                    <span className="font-mono text-ui-10-5 tracking-wider text-muted uppercase">{t.dock.saveDialog.name}</span>
                    <Input
                        value={name}
                        maxLength={40}
                        title={t.dock.saveDialog.name}
                        placeholder={t.dock.saveDialog.placeholder}
                        onChange={(e) => setName(e.target.value)}
                    />
                </label>
                <p className="text-ui-11-5 leading-relaxed text-faint">{t.dock.saveDialog.hint}</p>
            </form>
        </Dialog>
    )
}

function LayoutsDialog({open, onClose}: {open: boolean; onClose: () => void}) {
    const t = useT()
    const layouts = dockState.use((s) => s.layouts)
    const apply = (run: () => void) => {
        run()
        onClose()
    }
    const remove = async (name: string) => {
        if (await confirmDialog(fmt(t.dock.layoutsDialog.deleteConfirm, {name}), t.dock.layoutsDialog.delete, t.dock.saveDialog.cancel)) await deleteSaved(name)
    }
    const row = 'flex items-center gap-3 border-b border-line px-4 py-2.5 last:border-b-0'
    return (
        <Dialog open={open} onClose={onClose} title={t.dock.layoutsDialog.title} closeLabel={t.dock.layoutsDialog.close} className="w-[520px]">
            <div className="flex flex-col py-2" data-testid="dock-layouts">
                <h3 className="px-4 pt-1 pb-1.5 font-mono text-ui-10 tracking-[0.12em] text-faint uppercase">{t.dock.layoutsDialog.mine}</h3>
                {layouts.length === 0 ? (
                    <p className="px-4 pb-3 text-ui-12-5 leading-relaxed text-muted">{t.dock.layoutsDialog.empty}</p>
                ) : (
                    layouts.map((l) => {
                        const n = Object.keys(parseEnvelope(l.json)?.panels ?? {}).length
                        return (
                            <div key={l.name} className={row}>
                                <div className="min-w-0 flex-1">
                                    <p className="truncate text-ui-13 font-medium text-fg">{l.name}</p>
                                    <p className="text-ui-11-5 text-faint">{n === 1 ? t.dock.layoutsDialog.panelsOne : fmt(t.dock.layoutsDialog.panels, {n})}</p>
                                </div>
                                <Button title={t.dock.layoutsDialog.applyHint} onClick={() => apply(() => applySaved(l.name))}>
                                    {t.dock.layoutsDialog.apply}
                                </Button>
                                <Button variant="danger" title={t.dock.layoutsDialog.deleteHint} onClick={() => void remove(l.name)}>
                                    {t.dock.layoutsDialog.delete}
                                </Button>
                            </div>
                        )
                    })
                )}
                <h3 className="mt-2 border-t border-line px-4 pt-3 pb-1.5 font-mono text-ui-10 tracking-[0.12em] text-faint uppercase">
                    {t.dock.layoutsDialog.builtin}
                </h3>
                {PRESET_IDS.map((p) => (
                    <div key={p} className={row}>
                        <div className="min-w-0 flex-1">
                            <p className="truncate text-ui-13 font-medium text-fg">{t.dock.presets[p]}</p>
                            <p className="text-ui-11-5 text-faint">{t.dock.presetHints[p]}</p>
                        </div>
                        <Button title={t.dock.layoutsDialog.applyHint} onClick={() => apply(() => applyPreset(p))}>
                            {t.dock.layoutsDialog.apply}
                        </Button>
                    </div>
                ))}
            </div>
        </Dialog>
    )
}
