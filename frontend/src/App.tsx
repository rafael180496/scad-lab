import {lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState} from 'react'
import type {main, printers, store} from '../wailsjs/go/models'
import {
    ChooseDirectory,
    DetectTools,
    GetBootstrap,
    ImportWorkspace,
    ListPrinters,
    ListWorkspaces,
    ReadCheckpoint,
    ReadCheckpointPair,
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
import {confirmLeave, type EditorShot, type EditorSnap} from './components/editor/EditorPane'
import {editorCommands, editorSession} from './lib/editorSession'
import type {ProjectTab, ProjectTabsShot} from './components/layout/ProjectTabs'
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
import {useTerminals} from './hooks/useTerminals'
import {useProjectActivity} from './hooks/useProjectActivity'
import {
    closeProject,
    listOpenProjects,
    MAX_PROJECTS,
    openProject,
    reorderProjects,
    setActiveProject,
    type OpenProjectInfo,
} from './lib/projectsApi'
import {forgetPartSelection, rememberPartSelection} from './lib/printSession'
import {projectClosed} from './lib/sessionBridge'
import {LAYOUT_DEFAULTS, LAYOUT_LIMITS, sidebarMaxFor, useLayout, useWindowWidth} from './hooks/useLayout'
import {useAppearance} from './hooks/useAppearance'
import {useExport} from './hooks/useExport'
import {isMac, isMod, setPlatform} from './lib/platform'
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
    dock?: 'custom' | 'hidden' | 'tall' | 'minimized' | 'print' | 'diff'
    /** Abre el menú «…» del grupo de un panel; con `submenu`, entra en esa entrada («move»). */
    dockMenu?: {panel: PanelId; submenu?: string}
    /** Abre el menú Ver o el diálogo de diseños guardados. */
    viewMenu?: ShotView
    editor?: EditorShot
    /** Abre el diálogo de exportación 3MF en cuanto el checkpoint tiene render. */
    exportDialog?: boolean
    /** Abre el menú contextual del árbol sobre este checkpoint. */
    treeMenu?: string
    /** Abre el diálogo de la papelera de checkpoints. */
    trash?: boolean
    /** Abre la comparación de este checkpoint con su padre. */
    compare?: string
    /** Abre el menú «+» de las pestañas de proyecto. */
    projectsMenu?: ProjectTabsShot
    /** Marca estas pestañas con «cambios sin guardar» (no hay otra forma de tener un borrador de fondo). */
    unsavedTabs?: number[]
}

// Lo que App guarda de cada proyecto abierto (S8). El resto del estado de un
// proyecto vive donde ya vivía, indexado por workspace: los resultados de render
// (useRenders), las terminales (useTerminals), el chat elegido (ChatPanel) y la
// parte seleccionada (printSession).
type Project = {
    view: main.WorkspaceView
    /** Checkpoint elegido. */
    selected: string | null
    /** Código guardado del checkpoint elegido. */
    source: string
    /** Lo que tenía el editor al irse el proyecto a segundo plano (borrador, edición, diff); null = nada que recuperar. */
    editor: EditorSnap | null
}

interface PatchFn {
    (p: Project): Partial<Project>
}
type ProjectPatch = Partial<Project> | PatchFn
interface BootTask {
    (): Promise<void>
}

// Marca de «ya se migró al modo de pestañas»: la primera vez con un backend que
// guarda proyectos abiertos y todavía sin ninguno guardado, se abre el último
// workspace usado (lo que la app hacía siempre); desde entonces una lista vacía
// es una decisión del usuario y se respeta.
const SEEN_KEY = 'scadlab.projects.seen'
const readSeen = () => {
    try {
        return localStorage.getItem(SEEN_KEY) === '1'
    } catch {
        return false
    }
}
const writeSeen = () => {
    try {
        localStorage.setItem(SEEN_KEY, '1')
    } catch {
        /* sin almacenamiento: se pregunta de nuevo la próxima vez, sin consecuencias */
    }
}

// Volumen por defecto si la impresora del workspace no está en el catálogo
// (A1/P1S/X1C: 256 mm de lado).
const FALLBACK_BED = {x: 256, y: 256, z: 256}

export default function App({shot}: {shot?: ShotOptions} = {}) {
    const t = useT()
    const [boot, setBoot] = useState<main.Bootstrap | null>(null)
    const [printerList, setPrinterList] = useState<printers.Printer[]>([])
    const [workspaces, setWorkspaces] = useState<store.Workspace[]>([])
    // Proyectos abiertos: el orden de las pestañas, cuál está al frente y el estado de cada uno.
    const [order, setOrder] = useState<number[]>([])
    const [activeId, setActiveId] = useState<number | null>(null)
    const [projects, setProjects] = useState<Record<number, Project>>({})
    const orderRef = useRef(order)
    orderRef.current = order
    const activeRef = useRef(activeId)
    activeRef.current = activeId
    const projectsRef = useRef(projects)
    projectsRef.current = projects
    // Operaciones propias en vuelo (abrir, cerrar, reordenar): mientras duran se ignora
    // projects:changed, que es el eco de lo mismo que la interfaz ya está haciendo.
    const opsInFlight = useRef(0)
    const cur = activeId != null ? projects[activeId] : undefined
    const view = cur?.view ?? null
    const selected = cur?.selected ?? null
    const source = cur?.source ?? ''
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
    dockShot.menu = shot?.dockMenu
    const dockReady = dockState.use((s) => s.ready)
    useEffect(() => {
        if (dockReady && shot?.panel) showPanel(shot.panel)
    }, [dockReady, shot])

    const patchProject = useCallback((id: number, patch: ProjectPatch) => {
        setProjects((prev) => {
            const p = prev[id]
            if (!p) return prev
            const d = typeof patch === 'function' ? patch(p) : patch
            return Object.keys(d).length ? {...prev, [id]: {...p, ...d}} : prev
        })
    }, [])

    // Terminales de agente de todos los proyectos abiertos. Viven acá y no en el
    // panel porque la cabecera también las necesita (indicador y confirmación al cerrar).
    const onAutoIterateChange = useCallback(
        (v: boolean) =>
            // El interruptor de la terminal guarda autoIterate por su lado: se refleja en
            // boot.settings para que un guardado posterior de Configuración no lo pise
            // con el valor viejo.
            setBoot((b) => (b ? ({...b, settings: {...b.settings, autoIterate: v}} as main.Bootstrap) : b)),
        [],
    )
    const terms = useTerminals(activeId, boot?.settings.autoIterate ?? false, onAutoIterateChange)
    const termsRef = useRef(terms)
    termsRef.current = terms
    const renders = useRenders(order, activeId)
    const latestByWs = useMemo(
        () => Object.fromEntries(order.map((id) => [id, projects[id]?.view.checkpoints.at(-1)?.fileName ?? ''])),
        [order, projects],
    )
    const activity = useProjectActivity(order, renders.byWs, terms.loaded ? terms.alive : null, latestByWs)
    const dirty = editorSession.use((s) => s.dirty)

    // ── Proyectos abiertos (S8) ────────────────────────────────────────────────
    // Antes de irse de un proyecto se guarda lo que el editor tiene y no vive en
    // otro lado: el código sin guardar, si estaba editando y la comparación con el
    // padre. El visor y el chat llevan lo suyo por workspace (ver Project).
    // El editor es uno solo y pertenece al proyecto que ya mostró (el último commit de
    // React), que puede no ser aún el activo si se cambia de pestaña dos veces seguidas.
    const editorOwner = useRef<number | null>(null)
    useEffect(() => {
        editorOwner.current = activeId
    }, [activeId])
    const snapshotActive = useCallback(() => {
        const id = editorOwner.current
        const p = id != null ? projectsRef.current[id] : undefined
        if (id == null || !p) return
        rememberPartSelection()
        const s = editorSession.get()
        // La comparación abierta es de este proyecto: no se queda en pantalla en el siguiente.
        if (s.diff) editorSession.set({diff: null})
        if (!p.selected || (!s.dirty && !s.editing && !s.diff)) {
            if (p.editor) patchProject(id, {editor: null})
            return
        }
        const text = s.dirty ? (editorCommands()?.getValue() ?? null) : null
        patchProject(id, {editor: {file: p.selected, text, editing: s.editing, dirty: s.dirty && text != null, diff: s.diff}})
    }, [patchProject])

    // Trae al frente un proyecto que ya está abierto. Es instantáneo: no pregunta
    // nada ni lee nada, todo lo suyo está en memoria.
    const activate = useCallback(
        (id: number) => {
            if (id === activeRef.current || !projectsRef.current[id]) return
            snapshotActive()
            activeRef.current = id
            setActiveId(id)
            setActiveProject(id).catch((e) => toast.error(errorText(e)))
        },
        [snapshotActive],
    )

    // Abre el proyecto en el backend (watcher, auto-render) y trae su árbol y el
    // código del último checkpoint, que es el estado más reciente del modelo.
    const loadProject = useCallback(
        async (info: {id: number; view?: main.WorkspaceView; missing?: boolean; workspace?: store.Workspace}): Promise<Project> => {
            if (info.missing && info.workspace) {
                return {view: {workspace: info.workspace, checkpoints: [], missing: true, activeParent: ''} as unknown as main.WorkspaceView, selected: null, source: '', editor: null}
            }
            const v = info.view ?? (await openProject(info.id))
            const last = (shot?.select && v.checkpoints.some((c) => c.fileName === shot.select) ? shot.select : null) ?? v.checkpoints.at(-1)?.fileName ?? null
            const src = last ? await ReadCheckpoint(info.id, last).catch(() => '') : ''
            return {view: v, selected: last, source: src, editor: null}
        },
        [shot],
    )

    // Abre un workspace como pestaña de proyecto (o va a su pestaña si ya lo está).
    const openTab = useCallback(
        async (id: number) => {
            if (orderRef.current.includes(id)) {
                activate(id)
                return
            }
            if (orderRef.current.length >= MAX_PROJECTS) {
                toast.error(fmt(t.projects.limitToast, {n: MAX_PROJECTS}))
                return
            }
            opsInFlight.current++
            try {
                const p = await loadProject({id})
                if (orderRef.current.includes(id)) return
                snapshotActive()
                projectsRef.current = {...projectsRef.current, [id]: p}
                orderRef.current = [...orderRef.current, id]
                activeRef.current = id
                setProjects(projectsRef.current)
                setOrder(orderRef.current)
                setActiveId(id)
                void reloadWorkspaces()
            } catch (e) {
                toast.error(errorText(e))
            } finally {
                opsInFlight.current--
            }
        },
        [activate, loadProject, snapshotActive, reloadWorkspaces, t],
    )

    // Cierra una o varias pestañas. Lo que se pierde (cambios sin guardar, agentes
    // vivos) se dice en UNA confirmación; si no se pierde nada, no se pregunta.
    const closeTabs = useCallback(
        async (ids: number[], keep?: number) => {
            const targets = ids.filter((id) => projectsRef.current[id])
            if (!targets.length) return
            const losses = targets
                .map((id) => {
                    const unsaved = id === activeRef.current ? editorSession.get().dirty : !!projectsRef.current[id].editor?.dirty
                    return {name: projectsRef.current[id].view.workspace.name, unsaved, agents: termsRef.current.alive[id] ?? 0}
                })
                .filter((l) => l.unsaved || l.agents > 0)
            if (losses.length) {
                const what = (l: (typeof losses)[number]) =>
                    [l.unsaved ? t.projects.lossUnsaved : '', l.agents ? fmt(l.agents === 1 ? t.projects.lossAgent : t.projects.lossAgents, {n: l.agents}) : '']
                        .filter(Boolean)
                        .join(t.projects.lossJoin)
                const text =
                    losses.length === 1
                        ? fmt(t.projects.closeConfirm, {name: losses[0].name, loss: what(losses[0])})
                        : fmt(t.projects.closeManyConfirm, {list: losses.map((l) => `• «${l.name}»: ${what(l)}`).join('\n')})
                if (!(await confirmDialog(text, t.projects.closeAnyway, t.projects.keepOpen))) return
            }
            opsInFlight.current++
            try {
                const wasActive = activeRef.current != null && targets.includes(activeRef.current)
                if (wasActive) editorSession.set({diff: null})
                const old = orderRef.current
                const rest = old.filter((id) => !targets.includes(id))
                for (const id of targets) {
                    await closeProject(id).catch((e) => toast.error(errorText(e)))
                    projectClosed(id)
                    forgetPartSelection(id)
                }
                // El que queda al frente: el pedido, o el vecino de la derecha del que se fue (si no hay, el de la izquierda).
                let next = activeRef.current
                if (wasActive) {
                    const at = old.indexOf(activeRef.current!)
                    next = keep ?? rest.find((id) => old.indexOf(id) > at) ?? rest.at(-1) ?? null
                }
                const left = {...projectsRef.current}
                for (const id of targets) delete left[id]
                projectsRef.current = left
                orderRef.current = rest
                activeRef.current = next
                setProjects(left)
                setOrder(rest)
                setActiveId(next)
                if (wasActive && next != null) setActiveProject(next).catch((e) => toast.error(errorText(e)))
            } finally {
                opsInFlight.current--
            }
        },
        [t],
    )

    const reorder = useCallback((ids: number[]) => {
        orderRef.current = ids
        setOrder(ids)
        opsInFlight.current++
        reorderProjects(ids)
            .catch((e) => toast.error(errorText(e)))
            .finally(() => opsInFlight.current--)
    }, [])

    // Al arrancar se reabren las pestañas guardadas y la que estaba al frente (el
    // backend no abre nada solo: cada una se abre con OpenProject). Sin ninguna
    // guardada, la primera vez tras actualizar, se abre el último workspace usado (la
    // lista viene ordenada por last_opened), que es lo que la app hacía antes.
    const bootProjects = useRef<BootTask>(async () => {})
    bootProjects.current = async () => {
        opsInFlight.current++
        try {
            const list = await ListWorkspaces()
            setWorkspaces(list)
            const saved = await listOpenProjects()
            let entries: OpenProjectInfo[] = saved.projects ?? []
            let active = saved.activeId ?? 0
            if (entries.length === 0 && list.length > 0 && !readSeen()) {
                entries = [{workspace: list[0], missing: false} as OpenProjectInfo]
                active = list[0].id
            }
            writeSeen()
            if (entries.length === 0) return
            if (!entries.some((e) => e.workspace.id === active)) active = entries[entries.length - 1].workspace.id
            // El activo se abre al final: el backend deja al frente el último que se abrió.
            const loadOrder = [...entries.filter((e) => e.workspace.id !== active), ...entries.filter((e) => e.workspace.id === active)]
            const loaded: Record<number, Project> = {}
            for (const e of loadOrder) {
                try {
                    loaded[e.workspace.id] = await loadProject({id: e.workspace.id, missing: e.missing, workspace: e.workspace})
                } catch (err) {
                    toast.error(errorText(err))
                }
            }
            const ids = entries.map((e) => e.workspace.id).filter((id) => loaded[id])
            if (ids.length === 0) return
            const front = ids.includes(active) ? active : ids[ids.length - 1]
            projectsRef.current = loaded
            orderRef.current = ids
            activeRef.current = front
            setProjects(loaded)
            setOrder(ids)
            setActiveId(front)
            if (front !== active) setActiveProject(front).catch(() => {})
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            opsInFlight.current--
        }
    }
    useEffect(() => {
        void bootProjects.current()
    }, [])

    // El backend avisa cuando cambia la lista o el orden de los proyectos abiertos. Lo
    // que la interfaz ya hizo por su cuenta llega de vuelta como eco y no cambia nada;
    // lo que sí cambia es un proyecto que el backend cerró (se quita la pestaña) y un
    // orden distinto (se adopta). Cuál está al frente lo decide la interfaz.
    useEffect(
        () =>
            onEvent('projects:changed', () => {
                if (opsInFlight.current > 0) return
                listOpenProjects()
                    .then((r) => {
                        if (!r || opsInFlight.current > 0) return
                        const server = r.projects.map((p) => p.workspace.id)
                        const local = orderRef.current
                        const gone = local.filter((id) => !server.includes(id))
                        const kept = local.filter((id) => server.includes(id))
                        if (gone.length) {
                            for (const id of gone) {
                                projectClosed(id)
                                forgetPartSelection(id)
                            }
                            const left = {...projectsRef.current}
                            for (const id of gone) delete left[id]
                            projectsRef.current = left
                            setProjects(left)
                            if (activeRef.current != null && gone.includes(activeRef.current)) {
                                const next = kept[0] ?? null
                                activeRef.current = next
                                setActiveId(next)
                            }
                        }
                        const sameSet = kept.length === server.length
                        const wanted = sameSet ? server : kept
                        if (wanted.join() !== local.join()) {
                            orderRef.current = wanted
                            setOrder(wanted)
                        }
                    })
                    .catch(() => {})
            }),
        [],
    )

    const openSettings = (tab: SettingsTab = 'general') => {
        setSettingsTab(tab)
        setSettingsOpen(true)
    }

    // Atajos de las pestañas de proyecto, en captura (antes que Monaco y xterm):
    //   ⌃Tab / ⌃⇧Tab     siguiente / anterior (Control, también en macOS)
    //   ⌘/Ctrl+1…4       ir a la pestaña N
    //   ⌘/Ctrl+W         cerrar la pestaña activa; en Windows con el foco en una terminal
    //                    de agente, Ctrl+W es de la shell (borrar palabra): ahí Ctrl+Shift+W
    // No chocan con el dock (⌘/Ctrl+Shift+N y ⌘⌥N), con el visor (1–7 sin modificador),
    // con el zoom (⌘/Ctrl + − 0) ni con ⌘B/⌘S/⌘E. Con un diálogo abierto no actúan.
    const shortcut = useRef<(e: KeyboardEvent) => void>(() => {})
    shortcut.current = (e) => {
        if (document.querySelector('dialog[open]')) return
        const ids = orderRef.current
        if (e.key === 'Tab' && e.ctrlKey && !e.metaKey && !e.altKey) {
            if (ids.length < 2) return
            e.preventDefault()
            e.stopPropagation()
            const i = ids.indexOf(activeRef.current ?? -1)
            activate(ids[(i + (e.shiftKey ? -1 : 1) + ids.length) % ids.length])
            return
        }
        if (!isMod(e) || e.altKey) return
        const digit = /^(?:Digit|Numpad)([1-4])$/.exec(e.code)?.[1]
        if (digit && !e.shiftKey) {
            const id = ids[Number(digit) - 1]
            e.preventDefault()
            e.stopPropagation()
            if (id != null) activate(id)
            return
        }
        if (e.code === 'KeyW') {
            const inTerminal = e.target instanceof Element && !!e.target.closest('.xterm')
            if (isMac() ? e.shiftKey : inTerminal && !e.shiftKey) return
            e.preventDefault()
            e.stopPropagation()
            if (activeRef.current != null) void closeTabs([activeRef.current])
        }
    }
    useEffect(() => {
        const onKey = (e: KeyboardEvent) => shortcut.current(e)
        window.addEventListener('keydown', onKey, true)
        return () => window.removeEventListener('keydown', onKey, true)
    }, [])

    // El watcher del backend avisa cuando un agente crea o borra un .scad: el árbol
    // se actualiza solo, sin pulsar «volver a leer». Llega de TODOS los proyectos
    // abiertos, también los de fondo. Si lo que se ve de un proyecto es el checkpoint
    // que acaba de salir (a la papelera, o borrado a mano), pasa a su padre —o al
    // más nuevo— en vez de seguir mostrando un archivo que ya no está.
    useEffect(
        () =>
            onEvent('checkpoints:changed', (v) => {
                const id = v.workspace.id
                const p = projectsRef.current[id]
                if (!p) return
                patchProject(id, {view: v})
                const sel = p.selected
                if (!sel) return
                const was = p.view.checkpoints.find((c) => c.fileName === sel)
                if (!was || v.checkpoints.some((c) => c.fileName === sel)) return
                // Con cambios sin guardar se deja el editor como está: es código del usuario.
                if (id === activeRef.current ? editorSession.get().dirty : p.editor?.dirty) return
                const next = v.checkpoints.find((c) => c.fileName === was.parentFile)?.fileName ?? v.checkpoints.at(-1)?.fileName ?? null
                if (!next) {
                    patchProject(id, {selected: null, source: '', editor: null})
                    return
                }
                ReadCheckpoint(id, next)
                    .then((src) => {
                        if (projectsRef.current[id]?.selected !== sel) return // el usuario ya eligió otro
                        patchProject(id, {selected: next, source: src, editor: null})
                    })
                    .catch(() => {})
            }),
        [patchProject],
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

    // openCheckpoint elige un checkpoint (preguntando antes si hay cambios sin
    // guardar) y dice si quedó elegido.
    const openCheckpoint = async (file: string): Promise<boolean> => {
        if (!view) return false
        if (file === selected) return true
        if (!(await confirmLeave(t, selected))) return false
        const id = view.workspace.id
        try {
            const src = await ReadCheckpoint(id, file)
            patchProject(id, {selected: file, source: src, editor: null})
            return true
        } catch (e) {
            toast.error(errorText(e))
            return false
        }
    }
    const select = async (file: string) => {
        await openCheckpoint(file)
    }

    // «Ver código» del menú del árbol: elige el checkpoint y muestra el panel Código.
    const viewCode = async (file: string) => {
        if (await openCheckpoint(file)) showPanel('source')
    }

    // «Comparar con el padre»: el panel Código pasa a mostrar el diff del
    // checkpoint contra su padre, de solo lectura.
    const compare = async (file: string) => {
        if (!view) return
        if (file === selected && !(await confirmLeave(t, selected))) return
        if (file !== selected && !(await openCheckpoint(file))) return
        try {
            const pair = await ReadCheckpointPair(view.workspace.id, file)
            editorSession.set({editing: false, dirty: false, diff: pair})
            showPanel('source')
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    // Banco de capturas: abre la comparación pedida cuando el workspace ya cargó.
    const shotCompare = shot?.compare
    const compareRef = useRef(compare)
    compareRef.current = compare
    const shotCompared = useRef(false)
    useEffect(() => {
        if (!shotCompare || !view || selected !== shot?.select || shotCompared.current) return
        shotCompared.current = true
        void compareRef.current(shotCompare)
    }, [shotCompare, view, selected, shot?.select])

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
        const id = view.workspace.id
        patchProject(id, (p) => ({view: {...p.view, checkpoints: p.view.checkpoints.map((c) => (c.fileName === cp.fileName ? {...c, collapsed: next} : c))} as main.WorkspaceView}))
        await SetCheckpointUI(view.workspace.id, cp.fileName, cp.label, next).catch((e) => toast.error(errorText(e)))
    }

    // Las pestañas: nombre, y los indicadores de lo que pasa en cada proyecto aunque no se mire.
    const tabs: ProjectTab[] = order.flatMap((id) => {
        const p = projects[id]
        if (!p) return []
        const a = activity[id]
        return {
            id,
            name: p.view.workspace.name,
            path: p.view.workspace.path,
            missing: p.view.missing,
            unsaved: id === activeId ? dirty : !!p.editor?.dirty || !!shot?.unsavedTabs?.includes(id),
            rendering: !!a?.rendering,
            failed: !!a?.failed,
            agents: a?.agents ?? 0,
        }
    })

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
                projects={{
                    tabs,
                    activeId,
                    workspaces,
                    shot: shot?.projectsMenu,
                    onActivate: activate,
                    onClose: (id) => void closeTabs([id]),
                    onCloseOthers: (id) => void closeTabs(order.filter((x) => x !== id), id),
                    onReorder: reorder,
                    onOpenWorkspace: (id) => void openTab(id),
                    onNew: () => setNewOpen(true),
                    onReveal: (id) => RevealWorkspace(id).catch((e) => toast.error(errorText(e))),
                }}
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
                    openIds={order}
                    selected={selected}
                    onOpen={(id) => void openTab(id)}
                    onNew={() => setNewOpen(true)}
                    onImport={async () => {
                        const dir = await ChooseDirectory(t.workspaces.import, '')
                        if (!dir) return
                        try {
                            const w = await ImportWorkspace(dir, boot.settings.defaultPrinter)
                            await reloadWorkspaces()
                            await openTab(w.id)
                        } catch (e) {
                            toast.error(errorText(e))
                        }
                    }}
                    onRescan={async () => {
                        if (!view) return
                        try {
                            patchProject(view.workspace.id, {view: await RescanWorkspace(view.workspace.id)})
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
                        // Si está abierto, su pestaña se cierra primero (con su propia confirmación si pierde algo).
                        if (order.includes(w.id)) {
                            await closeTabs([w.id])
                            if (orderRef.current.includes(w.id)) return
                        }
                        try {
                            await RemoveWorkspace(w.id)
                            await reloadWorkspaces()
                        } catch (e) {
                            toast.error(errorText(e))
                        }
                    }}
                    onSelect={select}
                    onToggle={toggle}
                    onView={(f) => void viewCode(f)}
                    onCompare={(f) => void compare(f)}
                    shot={shot ? {treeMenu: shot.treeMenu, trash: shot.trash} : undefined}
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
                    onOpenPrintSettings={() => openSettings('printing')}
                    exportWhy={exp.why}
                    onExport3mf={exp.openDialog}
                    terms={terms}
                    editorRestore={cur?.editor ?? null}
                    onEditorRestored={() => activeId != null && patchProject(activeId, (p) => (p.editor ? {editor: null} : {}))}
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
                    onPrint={() => showPanel('print')}
                />
            )}

            <NewWorkspaceDialog
                open={newOpen}
                onClose={() => setNewOpen(false)}
                printerList={printerList}
                defaultPrinter={boot.settings.defaultPrinter}
                onCreated={async (id) => {
                    await reloadWorkspaces()
                    await openTab(id)
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
                workspaceId={view?.workspace.id ?? null}
                workspaceName={view?.workspace.name}
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
