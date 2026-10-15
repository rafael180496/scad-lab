import {lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState} from 'react'
import type {main, printers, store} from '../wailsjs/go/models'
import {
    ChooseDirectory,
    DetectTools,
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
import {PanelHost} from './components/dock/PanelHost'
import {dockShot, dockState, resetLayout as resetDockLayout, showPanel} from './components/dock/dock'
import type {PanelId} from './components/dock/panels'
import type {ShotView} from './components/dock/ViewMenu'
import {StatusBar} from './components/layout/StatusBar'
import {Splitter} from './components/layout/Splitter'
import {confirmLeave, type EditorShot} from './components/editor/EditorPane'
import {Logo} from './components/layout/Logo'
import {Sidebar} from './components/workspace/Sidebar'
import {ExportDialog} from './components/export/ExportDialog'
import {NewWorkspaceDialog} from './components/workspace/NewWorkspaceDialog'
import {SettingsDialog, type SettingsTab} from './components/settings/SettingsDialog'
import {ConfirmHost, confirmDialog} from './components/ui/Dialog'
import {Toaster, toast} from './components/ui/Toast'
import {fmt, normalizeLang, setLanguage, useT} from './i18n'
import {errorText} from './lib/errors'
import {applyTheme} from './lib/theme'
import {onEvent} from './lib/events'
import {useRenders} from './hooks/useRenders'
import {LAYOUT_DEFAULTS, LAYOUT_LIMITS, sidebarMaxFor, useLayout, useWindowWidth} from './hooks/useLayout'
import {useAppearance} from './hooks/useAppearance'
import {useExport} from './hooks/useExport'
import {setPlatform} from './lib/platform'
import type {ViewerShot} from './components/viewer/Viewer'

// Los paneles acoplables (dockview, ~440 kB) van en su propio chunk: el núcleo de
// la app (cabecera, barra lateral, estado) no lo arrastra.
const DockWorkbench = lazy(() => import('./components/dock/DockWorkbench').then((m) => ({default: m.DockWorkbench})))

// ShotOptions las usa solo el banco de capturas (src/uishot.tsx) para abrir
// un diálogo sin simular clics. En la app real no se pasan.
export type ShotOptions = {
    settingsTab?: SettingsTab
    newWorkspace?: boolean
    select?: string
    viewer?: ViewerShot
    /** Panel que queda activo al abrir (agent-1, chat, problems…). */
    panel?: PanelId
    /** Diseño de paneles a armar: una división distinta con un panel flotante, o solo el visor. */
    dock?: 'custom' | 'hidden' | 'tall'
    /** Abre el menú Ver o el diálogo de diseños guardados. */
    viewMenu?: ShotView
    editor?: EditorShot
    /** Abre el diálogo de exportación 3MF en cuanto el checkpoint tiene render. */
    exportDialog?: boolean
}

// Volumen por defecto si la impresora del workspace no está en el catálogo
// (A1/P1S/X1C: 256 mm de lado).
const FALLBACK_BED = {x: 256, y: 256, z: 256}

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
    const [detectedOpenSCAD, setDetectedOpenSCAD] = useState<{version: string; missing: boolean} | null>(null)
    const {appearance, change: changeAppearance, reset: resetAppearance} = useAppearance(boot?.settings)
    const winW = useWindowWidth()
    const {layout, preview, commit, toggleSidebar, toggleStatusBar, resetLayout} = useLayout(boot?.settings.layout, (l) =>
        // El diálogo de Configuración guarda la configuración entera: tiene
        // que ver el layout nuevo para no pisarlo.
        setBoot((b) => (b ? ({...b, settings: {...b.settings, layout: l}} as main.Bootstrap) : b)),
    )

    const reloadWorkspaces = useCallback(async () => {
        try {
            setWorkspaces(await ListWorkspaces())
        } catch (e) {
            toast.error(errorText(e))
        }
    }, [])

    useEffect(() => {
        GetBootstrap().then((b) => {
            setPlatform(b.platform)
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

    // Banco de capturas: el estado del dock a fotografiar se arma dentro de attach
    // (dockShot); aquí solo se deja activo el panel pedido.
    dockShot.mode = shot?.dock
    const dockReady = dockState.use((s) => s.ready)
    useEffect(() => {
        if (dockReady && shot?.panel) showPanel(shot.panel)
    }, [dockReady, shot])

    const open = useCallback(
        async (id: number) => {
            if (!(await confirmLeave(t, selected))) return
            try {
                const v = await OpenWorkspace(id)
                // Arranca en el último checkpoint: es el estado más reciente del modelo.
                const last = shot?.select ?? v.checkpoints.at(-1)?.fileName ?? null
                const src = last ? await ReadCheckpoint(id, last) : ''
                // Nombre y código juntos: el editor no debe ver el nombre nuevo
                // con el código del anterior.
                setView(v)
                setSelected(last)
                setSource(src)
            } catch (e) {
                toast.error(errorText(e))
            }
        },
        [shot, t, selected],
    )

    // Al arrancar se reabre el último workspace usado (la lista viene ordenada
    // por last_opened): es donde el usuario estaba trabajando.
    const startup = useRef(open)
    startup.current = open
    useEffect(() => {
        ListWorkspaces()
            .then((list) => {
                setWorkspaces(list)
                if (list.length > 0) void startup.current(list[0].id)
            })
            .catch((e) => toast.error(errorText(e)))
    }, [])

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

    const exp = useExport({
        workspaceId: view?.workspace.id ?? null,
        file: selected,
        result: selected ? renderResults[selected] : undefined,
        running: !!(selected && renderRunning[selected]),
        bambuPath: boot?.settings.bambuStudioPath ?? '',
    })

    // Banco de capturas: abre el diálogo de exportación cuando hay render.
    const shotExport = !!shot?.exportDialog
    const exportReady = exp.why === ''
    const {openDialog: openExportDialog} = exp
    useEffect(() => {
        if (shotExport && exportReady) openExportDialog()
    }, [shotExport, exportReady, openExportDialog])

    const select = async (file: string) => {
        if (!view || file === selected) return
        if (!(await confirmLeave(t, selected))) return
        try {
            const src = await ReadCheckpoint(view.workspace.id, file)
            setSelected(file)
            setSource(src)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    // Versión de OpenSCAD para la barra de estado: la del último render; si
    // todavía no hubo ninguno, se detecta una vez (sin apurar el arranque).
    const renderedWith = Object.values(renderResults).find((r) => r.version)?.version ?? ''
    const openscadMissing = Object.values(renderResults).some((r) => r.status === 'missing')
    useEffect(() => {
        if (!boot || renderedWith || detectedOpenSCAD) return
        const timer = window.setTimeout(() => {
            DetectTools()
                .then((tools) => {
                    const o = tools.find((x) => x.id === 'openscad')
                    setDetectedOpenSCAD({version: o?.version ?? '', missing: !o?.found})
                })
                .catch(() => {})
        }, 1500)
        return () => window.clearTimeout(timer)
    }, [boot, renderedWith, detectedOpenSCAD])
    const openscad = openscadMissing
        ? {version: '', missing: true}
        : renderedWith
          ? {version: renderedWith, missing: false}
          : detectedOpenSCAD

    const toggle = async (cp: store.Checkpoint) => {
        if (!view) return
        const next = !cp.collapsed
        setView({...view, checkpoints: view.checkpoints.map((c) => (c.fileName === cp.fileName ? {...c, collapsed: next} : c))} as main.WorkspaceView)
        await SetCheckpointUI(view.workspace.id, cp.fileName, cp.label, next).catch((e) => toast.error(errorText(e)))
    }

    const printer = printerList.find((p) => p.id === view?.workspace.printerId)
    const bv = printer?.build_volume
    const bed = bv && bv.x > 0 && bv.y > 0 && bv.z > 0 ? bv : FALLBACK_BED

    // Los tamaños guardados se respetan mientras entren en la ventana: si se
    // achica, la barra lateral cede para que el visor y el código no desborden.
    const zen = dockState.use((s) => s.zen)
    const sidebarHidden = layout.sidebarCollapsed || zen
    const statusBarHidden = layout.statusBarHidden || zen
    const sidebarMax = sidebarMaxFor(winW)
    const sidebarWidth = Math.min(layout.sidebarWidth || LAYOUT_DEFAULTS.sidebarWidth, sidebarMax)
    // Lo que ven el editor y los paneles: la configuración guardada con la
    // apariencia en vivo (cambia al elegir, sin esperar a «Guardar»).
    const liveSettings = useMemo(() => (boot ? ({...boot.settings, ...appearance} as store.Settings) : null), [boot, appearance])

    if (!boot || !liveSettings) {
        return (
            <div className="flex h-full items-center justify-center gap-3 text-muted">
                <Logo className="icon-ui-24 animate-pulse text-fg" />
                {t.app.loading}
            </div>
        )
    }

    return (
        <div data-lock className="flex h-full flex-col overflow-clip">
            <Header
                platform={boot.platform}
                workspaceName={view?.workspace.name}
                sidebarCollapsed={sidebarHidden}
                onToggleSidebar={toggleSidebar}
                statusBarHidden={statusBarHidden}
                onToggleStatusBar={toggleStatusBar}
                viewShot={shot?.viewMenu}
                onSettings={() => openSettings()}
                exportWhy={exp.why}
                exportBusy={exp.busy}
                onExportStl={() => void exp.exportStl()}
                onExport3mf={exp.openDialog}
                onExportPng={() => void exp.exportPng()}
            />
            {boot.initError ? (
                <div className="selectable border-b border-danger/40 bg-surface px-4 py-2 text-ui-12 text-danger">
                    {t.app.initError}: {boot.initError}
                </div>
            ) : null}
            <div className="flex min-h-0 min-w-0 flex-1 overflow-clip">
                {sidebarHidden ? null : (
                <>
                <Sidebar
                    width={sidebarWidth}
                    workspacesHeight={layout.workspacesHeight || LAYOUT_DEFAULTS.workspacesHeight}
                    onWorkspacesResize={(v) => preview({workspacesHeight: v})}
                    onWorkspacesCommit={(v) => commit({workspacesHeight: v})}
                    onWorkspacesReset={() => commit({workspacesHeight: LAYOUT_DEFAULTS.workspacesHeight})}
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
                        if (view?.workspace.id === w.id && !(await confirmLeave(t, selected))) return
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
                <Splitter
                    orientation="vertical"
                    size={sidebarWidth}
                    min={LAYOUT_LIMITS.sidebar[0]}
                    max={sidebarMax}
                    title={t.layout.resizeSidebar}
                    className="-mx-1"
                    onResize={(v) => preview({sidebarWidth: v})}
                    onCommit={(v) => commit({sidebarWidth: v})}
                    onReset={() => commit({sidebarWidth: LAYOUT_DEFAULTS.sidebarWidth})}
                />
                </>
                )}
                <Suspense fallback={<main className="min-h-0 min-w-0 flex-1 p-2" />}>
                    <DockWorkbench initialLayout={boot.settings.dockLayout} savedLayouts={boot.settings.dockLayouts ?? []} />
                </Suspense>
                <PanelHost
                    workspaceId={view?.workspace.id ?? null}
                    file={selected}
                    source={source}
                    result={selected ? renderResults[selected] : undefined}
                    running={!!(selected && renderRunning[selected])}
                    bed={bed}
                    swatches={boot.settings.swatches}
                    settings={liveSettings}
                    dimsCollapsed={layout.dimsCollapsed}
                    onDimsCollapsed={(v) => commit({dimsCollapsed: v})}
                    onOpenAISettings={() => openSettings('ai')}
                    // El interruptor de la terminal guarda autoIterate por su
                    // lado: se refleja en boot.settings para que un guardado
                    // posterior de Configuración no lo pise con el valor viejo.
                    onAutoIterateChange={(v) =>
                        setBoot((b) => (b ? ({...b, settings: {...b.settings, autoIterate: v}} as main.Bootstrap) : b))
                    }
                    onSaved={(name) => {
                        // El watcher lo renderiza (auto-render) al verlo
                        // aparecer: el efecto de «elegido sin resultado» no
                        // debe lanzar otro render del mismo archivo.
                        if (view && boot.settings.autoRender) autoTried.current.add(`${view.workspace.id}:${name}`)
                        void select(name)
                    }}
                    onSwatchesSaved={(swatches) =>
                        // El diálogo de Configuración guarda la configuración
                        // entera: tiene que ver las muestras nuevas para no pisarlas.
                        setBoot((b) => (b ? ({...b, settings: {...b.settings, swatches}} as main.Bootstrap) : b))
                    }
                    viewerShot={shot?.viewer}
                    editorShot={shot?.editor}
                    onRender={() => selected && void renderFile(selected)}
                    onCancel={renders.cancel}
                    onOpenSettings={() => openSettings('openscad')}
                />
            </div>
            {statusBarHidden ? null : (
                <StatusBar
                    workspace={view?.workspace.name}
                    printer={printer?.name}
                    file={selected}
                    renderDiags={selected ? renderResults[selected]?.diagnostics : undefined}
                    openscad={openscad}
                    onProblems={() => showPanel('problems')}
                />
            )}

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
            <ExportDialog
                open={exp.dialog}
                onClose={exp.closeDialog}
                workspaceId={view?.workspace.id ?? null}
                fileName={selected}
                bambuFound={!!exp.bambu}
                onExported={exp.exported}
                onOpenSettings={() => openSettings('printing')}
            />
            <SettingsDialog
                open={settingsOpen}
                initialTab={settingsTab}
                onClose={() => setSettingsOpen(false)}
                boot={boot}
                printerList={printerList}
                appearance={appearance}
                onAppearance={changeAppearance}
                onResetAppearance={() => {
                    resetAppearance()
                    toast.ok(t.appearance.resetAppearanceDone)
                }}
                onResetLayout={() => {
                    resetLayout()
                    resetDockLayout()
                    toast.ok(t.appearance.resetLayoutDone)
                }}
                onSaved={(s) => setBoot({...boot, settings: s} as main.Bootstrap)}
            />
            <ConfirmHost title="ScadLab" />
            <Toaster />
        </div>
    )
}
