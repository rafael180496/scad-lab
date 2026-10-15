import {useCallback, useEffect, useRef, useState} from 'react'
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
import {Workbench, type BottomTab} from './components/layout/Workbench'
import {StatusBar} from './components/layout/StatusBar'
import {Splitter} from './components/layout/Splitter'
import {confirmLeave, type EditorShot} from './components/editor/EditorPane'
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
import {LAYOUT_DEFAULTS, LAYOUT_LIMITS, useLayout} from './hooks/useLayout'
import {setPlatform} from './lib/platform'
import type {ViewerShot} from './components/viewer/Viewer'

// ShotOptions las usa solo el banco de capturas (src/uishot.tsx) para abrir
// un diálogo sin simular clics. En la app real no se pasan.
export type ShotOptions = {
    settingsTab?: SettingsTab
    newWorkspace?: boolean
    select?: string
    viewer?: ViewerShot
    bottomTab?: BottomTab
    editor?: EditorShot
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
    const [bottomTab, setBottomTab] = useState<BottomTab>(shot?.bottomTab ?? 'agent')
    const [detectedOpenSCAD, setDetectedOpenSCAD] = useState<{version: string; missing: boolean} | null>(null)
    const {layout, preview, commit, toggleSidebar} = useLayout(boot?.settings.layout, (l) =>
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
            <Header
                platform={boot.platform}
                workspaceName={view?.workspace.name}
                sidebarCollapsed={layout.sidebarCollapsed}
                onToggleSidebar={toggleSidebar}
                onSettings={() => openSettings()}
            />
            {boot.initError ? (
                <div className="selectable border-b border-danger/40 bg-surface px-4 py-2 text-[12px] text-danger">
                    {t.app.initError}: {boot.initError}
                </div>
            ) : null}
            <div className="flex min-h-0 flex-1">
                {layout.sidebarCollapsed ? null : (
                <>
                <Sidebar
                    width={layout.sidebarWidth || LAYOUT_DEFAULTS.sidebarWidth}
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
                    size={layout.sidebarWidth || LAYOUT_DEFAULTS.sidebarWidth}
                    min={LAYOUT_LIMITS.sidebar[0]}
                    max={LAYOUT_LIMITS.sidebar[1]}
                    title={t.layout.resizeSidebar}
                    className="-mx-1"
                    onResize={(v) => preview({sidebarWidth: v})}
                    onCommit={(v) => commit({sidebarWidth: v})}
                    onReset={() => commit({sidebarWidth: LAYOUT_DEFAULTS.sidebarWidth})}
                />
                </>
                )}
                <Workbench
                    workspaceId={view?.workspace.id ?? null}
                    file={selected}
                    source={source}
                    result={selected ? renderResults[selected] : undefined}
                    running={!!(selected && renderRunning[selected])}
                    bed={bed}
                    swatches={boot.settings.swatches}
                    settings={boot.settings}
                    layout={layout}
                    bottomTab={bottomTab}
                    onBottomTab={setBottomTab}
                    onOpenAISettings={() => openSettings('ai')}
                    // El interruptor de la terminal guarda autoIterate por su
                    // lado: se refleja en boot.settings para que un guardado
                    // posterior de Configuración no lo pise con el valor viejo.
                    onAutoIterateChange={(v) =>
                        setBoot((b) => (b ? ({...b, settings: {...b.settings, autoIterate: v}} as main.Bootstrap) : b))
                    }
                    onLayoutPreview={preview}
                    onLayoutCommit={commit}
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
            <StatusBar
                workspace={view?.workspace.name}
                printer={printer?.name}
                file={selected}
                renderDiags={selected ? renderResults[selected]?.diagnostics : undefined}
                openscad={openscad}
                onProblems={() => setBottomTab('problems')}
            />

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
