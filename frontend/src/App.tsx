import {useCallback, useEffect, useRef, useState} from 'react'
import type {main, printers, store} from '../wailsjs/go/models'
import {
    ChooseDirectory,
    GetBootstrap,
    ImportWorkspace,
    ListPrinters,
    ListWorkspaces,
    OpenWorkspace,
    ReadCheckpoint,
    RemoveWorkspace,
    RescanWorkspace,
    RevealWorkspace,
    SetCheckpointUI,
    SyncWorkspaceSkills,
} from '../wailsjs/go/main/App'
import {Header} from './components/layout/Header'
import {Workbench} from './components/layout/Workbench'
import {Logo} from './components/layout/Logo'
import {Sidebar} from './components/workspace/Sidebar'
import {NewWorkspaceDialog} from './components/workspace/NewWorkspaceDialog'
import {SettingsDialog, type SettingsTab} from './components/settings/SettingsDialog'
import {ConfirmHost, confirmDialog} from './components/ui/Dialog'
import {Toaster, toast} from './components/ui/Toast'
import {fmt, normalizeLang, setLanguage, useT} from './i18n'
import {errorText} from './lib/errors'
import {applyTheme} from './lib/theme'
import {onEvent} from './lib/events'
import {useRenders} from './hooks/useRenders'

// ShotOptions las usa solo el banco de capturas (src/uishot.tsx) para abrir
// un diálogo sin simular clics. En la app real no se pasan.
export type ShotOptions = {settingsTab?: SettingsTab; newWorkspace?: boolean; select?: string}

export default function App({shot}: {shot?: ShotOptions} = {}) {
    const t = useT()
    const [boot, setBoot] = useState<main.Bootstrap | null>(null)
    const [printerList, setPrinterList] = useState<printers.Printer[]>([])
    const [workspaces, setWorkspaces] = useState<store.Workspace[]>([])
    const [view, setView] = useState<main.WorkspaceView | null>(null)
    const [selected, setSelected] = useState<string | null>(null)
    const [source, setSource] = useState('')
    const [settingsOpen, setSettingsOpen] = useState(false)
    const [settingsTab, setSettingsTab] = useState<SettingsTab>('general')
    const [newOpen, setNewOpen] = useState(false)

    const reloadWorkspaces = useCallback(async () => {
        try {
            setWorkspaces(await ListWorkspaces())
        } catch (e) {
            toast.error(errorText(e))
        }
    }, [])

    useEffect(() => {
        GetBootstrap().then((b) => {
            setLanguage(normalizeLang(b.settings.language))
            applyTheme(b.settings.theme)
            setBoot(b)
        })
        ListPrinters().then(setPrinterList).catch((e) => toast.error(errorText(e)))
    }, [])

    useEffect(() => {
        if (!boot || !shot) return
        if (shot.settingsTab) {
            setSettingsTab(shot.settingsTab)
            setSettingsOpen(true)
        }
        if (shot.newWorkspace) setNewOpen(true)
    }, [boot, shot])

    const open = useCallback(async (id: number) => {
        try {
            const v = await OpenWorkspace(id)
            setView(v)
            // Arranca en el último checkpoint: es el estado más reciente del modelo.
            const last = shot?.select ?? v.checkpoints.at(-1)?.fileName ?? null
            setSelected(last)
            setSource(last ? await ReadCheckpoint(id, last) : '')
        } catch (e) {
            toast.error(errorText(e))
        }
    }, [shot])

    // Al arrancar se reabre el último workspace usado (la lista viene ordenada
    // por last_opened): es donde el usuario estaba trabajando.
    useEffect(() => {
        ListWorkspaces()
            .then((list) => {
                setWorkspaces(list)
                if (list.length > 0) void open(list[0].id)
            })
            .catch((e) => toast.error(errorText(e)))
    }, [open])

    const renders = useRenders(view?.workspace.id ?? null)

    const openSettings = (tab: SettingsTab = 'general') => {
        setSettingsTab(tab)
        setSettingsOpen(true)
    }

    // El watcher del backend avisa cuando un agente crea o borra un .scad:
    // el árbol se actualiza solo, sin pulsar «volver a leer».
    useEffect(
        () =>
            onEvent('checkpoints:changed', (v) => {
                setView((prev) => (prev && prev.workspace.id === v.workspace.id ? v : prev))
            }),
        [],
    )

    // Auto-render al elegir un checkpoint que todavía no tiene resultado. Se
    // intenta una sola vez por checkpoint: si el usuario lo cancela, no se
    // vuelve a lanzar solo.
    const autoTried = useRef(new Set<string>())
    const {loaded: rendersLoaded, results: renderResults, running: renderRunning, render: renderFile} = renders
    useEffect(() => {
        if (!boot?.settings.autoRender || !view || !selected || !rendersLoaded) return
        const key = `${view.workspace.id}:${selected}`
        if (renderResults[selected] || renderRunning[selected] || autoTried.current.has(key)) return
        autoTried.current.add(key)
        void renderFile(selected)
    }, [boot?.settings.autoRender, view, selected, rendersLoaded, renderResults, renderRunning, renderFile])

    const select = async (file: string) => {
        if (!view) return
        setSelected(file)
        try {
            setSource(await ReadCheckpoint(view.workspace.id, file))
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const toggle = async (cp: store.Checkpoint) => {
        if (!view) return
        const next = !cp.collapsed
        setView({...view, checkpoints: view.checkpoints.map((c) => (c.fileName === cp.fileName ? {...c, collapsed: next} : c))} as main.WorkspaceView)
        await SetCheckpointUI(view.workspace.id, cp.fileName, cp.label, next).catch((e) => toast.error(errorText(e)))
    }

    if (!boot) {
        return (
            <div className="flex h-full items-center justify-center gap-3 text-muted">
                <Logo className="size-6 animate-pulse text-fg" />
                {t.app.loading}
            </div>
        )
    }

    return (
        <div className="flex h-full flex-col">
            <Header platform={boot.platform} workspaceName={view?.workspace.name} onSettings={() => openSettings()} />
            {boot.initError ? (
                <div className="selectable border-b border-danger/40 bg-surface px-4 py-2 text-[12px] text-danger">
                    {t.app.initError}: {boot.initError}
                </div>
            ) : null}
            <div className="flex min-h-0 flex-1">
                <Sidebar
                    workspaces={workspaces}
                    view={view}
                    selected={selected}
                    onOpen={open}
                    onNew={() => setNewOpen(true)}
                    onImport={async () => {
                        const dir = await ChooseDirectory(t.workspaces.import, '')
                        if (!dir) return
                        try {
                            const w = await ImportWorkspace(dir, boot.settings.defaultPrinter)
                            await reloadWorkspaces()
                            await open(w.id)
                        } catch (e) {
                            toast.error(errorText(e))
                        }
                    }}
                    onRescan={async () => {
                        if (!view) return
                        try {
                            setView(await RescanWorkspace(view.workspace.id))
                        } catch (e) {
                            toast.error(errorText(e))
                        }
                    }}
                    onSyncSkills={async () => {
                        if (!view) return
                        try {
                            await SyncWorkspaceSkills(view.workspace.id)
                            toast.ok(t.workspaces.skillsSynced)
                        } catch (e) {
                            toast.error(errorText(e))
                        }
                    }}
                    onReveal={() => view && RevealWorkspace(view.workspace.id).catch((e) => toast.error(errorText(e)))}
                    onRemove={async (w) => {
                        const ok = await confirmDialog(fmt(t.workspaces.removeConfirm, {name: w.name}), t.workspaces.remove, t.workspaces.cancel)
                        if (!ok) return
                        try {
                            await RemoveWorkspace(w.id)
                            if (view?.workspace.id === w.id) {
                                setView(null)
                                setSelected(null)
                                setSource('')
                            }
                            await reloadWorkspaces()
                        } catch (e) {
                            toast.error(errorText(e))
                        }
                    }}
                    onSelect={select}
                    onToggle={toggle}
                    renders={renderResults}
                    running={renderRunning}
                />
                <Workbench
                    file={selected}
                    source={source}
                    result={selected ? renderResults[selected] : undefined}
                    running={!!(selected && renderRunning[selected])}
                    onRender={() => selected && void renderFile(selected)}
                    onCancel={renders.cancel}
                    onOpenSettings={() => openSettings('openscad')}
                />
            </div>

            <NewWorkspaceDialog
                open={newOpen}
                onClose={() => setNewOpen(false)}
                printerList={printerList}
                defaultPrinter={boot.settings.defaultPrinter}
                onCreated={async (id) => {
                    await reloadWorkspaces()
                    await open(id)
                }}
            />
            <SettingsDialog
                open={settingsOpen}
                initialTab={settingsTab}
                onClose={() => setSettingsOpen(false)}
                boot={boot}
                printerList={printerList}
                onSaved={(s) => setBoot({...boot, settings: s} as main.Bootstrap)}
            />
            <ConfirmHost title="ScadLab" />
            <Toaster />
        </div>
    )
}
