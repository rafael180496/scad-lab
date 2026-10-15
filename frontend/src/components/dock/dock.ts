import type {AddPanelOptions, DockviewApi, IDockviewPanel} from 'dockview-react'
import {store} from '../../../wailsjs/go/models'
import {SaveDockLayout, SaveDockLayouts} from '../../../wailsjs/go/main/App'
import {t as tNow} from '../../i18n'
import {appearance} from '../../lib/appearance'
import {errorText} from '../../lib/errors'
import {createStore} from '../../lib/store'
import {toast} from '../ui/Toast'
import {buildPreset, minimums, parseEnvelope, toEnvelope, type PresetId} from './layouts'
import {PANEL_IDS, isPanelId, type PanelId} from './panels'

// Controlador de los paneles acoplables (S4b). Hay un solo dock en la ventana, así
// que vive como módulo: el menú Ver de la cabecera, los atajos, el menú de la
// pestaña y la pantalla vacía lo usan sin pasarse props. dockview es dueño de la
// geometría; esto lleva la cuenta de qué paneles están a la vista (`dockState`),
// guarda el diseño (con espera) y arma/aplica los diseños.

export type Flags = Record<PanelId, boolean>
export type SavedLayout = {name: string; json: string}

const none = (): Flags => ({viewport: false, source: false, 'agent-1': false, 'agent-2': false, chat: false, problems: false})

export const dockState = createStore({
    ready: false,
    /** Está en el dock (en algún grupo, flotante o no). */
    present: none(),
    /** Se ve ahora: es la pestaña activa de su grupo y el grupo está a la vista. */
    visible: none(),
    floating: none(),
    active: null as PanelId | null,
    maximized: false,
    /** Modo enfoque: solo el panel activo, sin barra lateral ni de estado. */
    zen: false,
    layouts: [] as SavedLayout[],
})

let api: DockviewApi | null = null
let disposers: Array<() => void> = []
let timer: number | undefined
// Mientras se carga un diseño (fromJSON dispara cambios) no se guarda ni se sincroniza.
let loading = 0
// Diseño de antes del modo enfoque, para volver a él.
let zenSaved: string | null = null
// Con quién estaba cada panel al ocultarse, y en qué lugar de sus pestañas: al
// volver a mostrarlo vuelve a ese grupo y a ese lugar.
const lastWith: Partial<Record<PanelId, {mate: PanelId; index: number}>> = {}

const PERSIST_DELAY_MS = 600
const scale = () => appearance.get().uiScale / 100

function sameFlags(a: Flags, b: Flags) {
    return PANEL_IDS.every((id) => a[id] === b[id])
}

function sync() {
    if (!api || loading) return
    const present = none()
    const visible = none()
    const floating = none()
    for (const p of api.panels) {
        if (!isPanelId(p.id)) continue
        present[p.id] = true
        visible[p.id] = p.api.isVisible
        floating[p.id] = p.api.location.type === 'floating'
    }
    const s = dockState.get()
    const active = api.activePanel && isPanelId(api.activePanel.id) ? api.activePanel.id : null
    dockState.set({
        present: sameFlags(s.present, present) ? s.present : present,
        visible: sameFlags(s.visible, visible) ? s.visible : visible,
        floating: sameFlags(s.floating, floating) ? s.floating : floating,
        active,
        maximized: api.hasMaximizedGroup(),
    })
}

// dockview no corrige solo un grupo que nace por debajo de su mínimo (al soltar
// una pestaña en un borde, el grupo nuevo entra con la mitad del espacio del que
// se parte, aunque sea menos de lo que su panel admite): una pasada forzada de
// layout lo reparte respetando los mínimos.
let enforcing = false
function enforceMinimums() {
    if (!api || loading || enforcing) return
    const a = api
    const small = a.groups.some(
        (g) => g.api.location.type === 'grid' && (g.api.width + 0.5 < g.minimumWidth || g.api.height + 0.5 < g.minimumHeight),
    )
    if (!small) return
    enforcing = true
    queueMicrotask(() => {
        try {
            a.layout(a.width, a.height, true)
        } finally {
            enforcing = false
        }
    })
}

// syncSoon: la visibilidad de un panel cambia dentro de eventos de dockview que
// todavía no actualizaron su grupo; se lee en el cuadro siguiente.
export function syncSoon() {
    queueMicrotask(sync)
}

function persistNow() {
    if (!api || loading || dockState.get().zen) return
    window.clearTimeout(timer)
    timer = undefined
    SaveDockLayout(toEnvelope(api)).catch((e) => toast.error(errorText(e)))
}

function schedulePersist() {
    if (loading || dockState.get().zen) return
    window.clearTimeout(timer)
    timer = window.setTimeout(persistNow, PERSIST_DELAY_MS)
}

// ── Carga de diseños ────────────────────────────────────────────────────────

function applyConstraints() {
    if (!api) return
    for (const p of api.panels) {
        if (isPanelId(p.id)) p.api.setConstraints(minimums(p.id, scale()))
    }
}

// loadJSON carga un diseño guardado. false si no se pudo (texto roto, otra
// versión, paneles desconocidos o dockview lo rechazó): quien llama cae a otro.
function loadJSON(json: string | null | undefined): boolean {
    const data = parseEnvelope(json)
    if (!api || !data) return false
    loading++
    try {
        api.fromJSON(data)
        if (!api.panels.every((p) => isPanelId(p.id))) throw new Error('panel desconocido')
        // Los mínimos guardados pueden ser de otra escala de interfaz.
        applyConstraints()
        return true
    } catch (e) {
        console.warn('diseño de paneles inválido, se usa el predeterminado', e)
        api.clear()
        return false
    } finally {
        loading--
    }
}

function loadPreset(preset: PresetId) {
    if (!api) return
    loading++
    try {
        api.clear()
        buildPreset(api, preset, scale())
    } finally {
        loading--
    }
}

// Solo el banco de capturas (uishot): arma un estado concreto del dock dentro de
// `attach`, antes del primer dibujo. Hacerlo después, desde un efecto, dejaba una
// carrera: la captura podía salir con la capa de contenido de un panel todavía
// con la medida anterior.
export const dockShot: {mode?: 'custom' | 'hidden' | 'tall'} = {}

function applyShotMode() {
    switch (dockShot.mode) {
        case 'hidden':
            shotTools.keepOnly('viewport')
            break
        case 'custom':
            // Una división distinta (el chat a la derecha de las terminales, en una
            // columna propia) y los problemas en una ventana flotante sobre el visor.
            shotTools.splitOut('chat', 'right')
            shotTools.float('problems')
            break
        case 'tall':
            // Terminal y chat: el grupo de abajo más alto, para que se vea la conversación.
            shotTools.size('agent-1', {height: 0.56})
            break
    }
}

// attach engancha el dock recién creado: restaura el diseño guardado y, si no se
// puede, arma el predeterminado.
export function attach(a: DockviewApi, saved: string, layouts: SavedLayout[]) {
    detach()
    api = a
    zenSaved = null
    if (!loadJSON(saved)) loadPreset('default')
    applyShotMode()
    disposers = [
        a.onDidLayoutChange(() => {
            sync()
            enforceMinimums()
            schedulePersist()
        }),
        a.onDidActivePanelChange(sync),
        a.onDidAddPanel(sync),
        a.onDidRemovePanel(sync),
        a.onDidMovePanel(sync),
        a.onDidMaximizedGroupChange(sync),
        a.onDidLayoutFromJSON(sync),
    ].map((d) => () => d.dispose())
    dockState.set({ready: true, layouts, zen: false})
    sync()
}

// Si se cierra la ventana dentro de la espera del guardado, se guarda al salir.
function flushOnLeave() {
    if (document.visibilityState === 'hidden' && timer !== undefined) persistNow()
}
window.addEventListener('pagehide', () => timer !== undefined && persistNow())
document.addEventListener('visibilitychange', flushOnLeave)

export function detach() {
    window.clearTimeout(timer)
    disposers.forEach((d) => d())
    disposers = []
    api = null
    dockState.set({ready: false, present: none(), visible: none(), floating: none(), active: null, maximized: false})
}

export function setSavedLayouts(layouts: SavedLayout[]) {
    dockState.set({layouts})
}

// Los mínimos escalan con la interfaz: se reaplican al cambiar de tamaño.
export function rescaleConstraints() {
    applyConstraints()
}

// ── Mostrar, ocultar, enfocar ───────────────────────────────────────────────

const BOTTOM: PanelId[] = ['agent-1', 'agent-2', 'chat', 'problems']

// placement decide dónde reaparece un panel oculto: junto a quien lo acompañaba;
// si no, según su papel (el código a la derecha del visor, lo demás debajo).
function placement(id: PanelId): AddPanelOptions['position'] | undefined {
    if (!api) return undefined
    const present = (p: PanelId) => !!api!.getPanel(p)
    const last = lastWith[id]
    if (last && present(last.mate)) return {referencePanel: last.mate, direction: 'within', index: last.index}
    const any = PANEL_IDS.find((p) => p !== id && present(p))
    if (!any) return undefined
    if (id === 'viewport') return {referencePanel: present('source') ? 'source' : any, direction: 'left'}
    if (id === 'source') return {referencePanel: present('viewport') ? 'viewport' : any, direction: 'right'}
    const group = BOTTOM.find((p) => p !== id && present(p))
    if (group) return {referencePanel: group, direction: 'within'}
    if (present('viewport')) return {referencePanel: 'viewport', direction: 'below'}
    return {referencePanel: any, direction: 'below'}
}

export function showPanel(id: PanelId) {
    if (!api) return
    const existing = api.getPanel(id)
    if (existing) {
        existing.api.setActive()
        return
    }
    // Un diseño con la pantalla maximizada o en modo enfoque se deja como está:
    // el panel nuevo entra al lado.
    const p = api.addPanel({
        id,
        component: 'slot',
        title: id,
        renderer: 'always',
        ...minimums(id, scale()),
        ...(placement(id) ? {position: placement(id)} : {}),
    } as AddPanelOptions)
    p.api.setActive()
}

export function hidePanel(id: PanelId) {
    if (!api) return
    const p = api.getPanel(id)
    if (!p) return
    const mate = p.group.panels.find((x) => x.id !== id && isPanelId(x.id))
    if (mate) lastWith[id] = {mate: mate.id as PanelId, index: p.group.panels.indexOf(p)}
    else delete lastWith[id]
    api.removePanel(p)
}

export function togglePanel(id: PanelId) {
    if (api?.getPanel(id)) hidePanel(id)
    else showPanel(id)
}

// shortcutPanel: ⌘/Ctrl+Shift+N. Oculto → lo muestra y enfoca; a la vista pero sin
// foco → lo enfoca; ya enfocado → lo oculta.
export function shortcutPanel(id: PanelId) {
    if (!api) return
    const p = api.getPanel(id)
    if (!p) return showPanel(id)
    if (api.activePanel?.id === id) hidePanel(id)
    else p.api.setActive()
}

export function hideOthers(id: PanelId) {
    if (!api) return
    for (const p of [...api.panels]) if (p.id !== id) hidePanel(p.id as PanelId)
}

export function hideAll() {
    if (!api) return
    for (const p of [...api.panels]) api.removePanel(p)
}

// ── Grupos: flotar, acoplar, dividir, maximizar ─────────────────────────────

export function floatPanel(p: IDockviewPanel) {
    if (!api) return
    const W = api.width
    const H = api.height
    const width = Math.min(Math.round(W * 0.5), Math.round(620 * scale()))
    const height = Math.min(Math.round(H * 0.55), Math.round(400 * scale()))
    api.addFloatingGroup(p, {width, height, x: Math.round((W - width) / 2), y: Math.round((H - height) / 3)})
}

// dockPanel devuelve un panel flotante a la grilla: junto al grupo acoplado más
// grande, o solo (a la derecha) si no queda ninguno.
export function dockPanel(p: IDockviewPanel) {
    if (!api) return
    const target = api.groups
        .filter((g) => g.api.location.type === 'grid' && g !== p.group)
        .sort((a, b) => b.api.width * b.api.height - a.api.width * a.api.height)[0]
    if (target) p.api.moveTo({group: target, position: 'center'})
    else p.api.moveTo({position: 'right'})
}

// splitPanel saca la pestaña a un grupo propio a la derecha o debajo del suyo.
export function splitPanel(p: IDockviewPanel, position: 'right' | 'bottom') {
    p.api.moveTo({group: p.group, position})
}

export function toggleMaximize(panel?: IDockviewPanel) {
    if (!api) return
    if (api.hasMaximizedGroup()) {
        api.exitMaximizedGroup()
        return
    }
    const p = panel ?? api.activePanel
    if (p) api.maximizeGroup(p)
}

// ── Diseños ─────────────────────────────────────────────────────────────────

export function applyPreset(preset: PresetId) {
    if (!api) return
    leaveZen(false)
    loadPreset(preset)
    sync()
    persistNow()
}

export function resetLayout() {
    applyPreset('default')
}

export function currentLayoutJSON(): string | null {
    return api ? toEnvelope(api) : null
}

export function applySaved(name: string) {
    const entry = dockState.get().layouts.find((l) => l.name === name)
    if (!api || !entry) return
    leaveZen(false)
    if (!loadJSON(entry.json)) {
        loadPreset('default')
        toast.error(tNow().dock.savedBroken)
    }
    sync()
    persistNow()
}

async function storeLayouts(list: SavedLayout[]) {
    const saved = await SaveDockLayouts(list.map((l) => store.DockLayoutEntry.createFrom(l)))
    dockState.set({layouts: saved.map((l) => ({name: l.name, json: l.json}))})
}

export async function saveCurrentAs(name: string): Promise<boolean> {
    const json = currentLayoutJSON()
    if (!json) return false
    // Si el diseño actual es el del modo enfoque, se guarda el de antes.
    const body = dockState.get().zen && zenSaved ? zenSaved : json
    const key = name.trim().toLowerCase()
    const rest = dockState.get().layouts.filter((l) => l.name.toLowerCase() !== key)
    try {
        await storeLayouts([...rest, {name: name.trim(), json: body}])
        return true
    } catch (e) {
        toast.error(errorText(e))
        return false
    }
}

export async function deleteSaved(name: string) {
    try {
        await storeLayouts(dockState.get().layouts.filter((l) => l.name !== name))
    } catch (e) {
        toast.error(errorText(e))
    }
}

// ── Modo enfoque ────────────────────────────────────────────────────────────

// enterZen deja solo el panel activo (los demás se ocultan, no se cierran: siguen
// vivos) y congela el guardado: lo que haya al salir es lo de antes.
export function enterZen() {
    if (!api || dockState.get().zen) return
    const keep = api.activePanel && isPanelId(api.activePanel.id) ? api.activePanel.id : (api.panels.find((p) => isPanelId(p.id))?.id as PanelId | undefined)
    window.clearTimeout(timer)
    zenSaved = toEnvelope(api)
    dockState.set({zen: true})
    if (api.hasMaximizedGroup()) api.exitMaximizedGroup()
    for (const p of [...api.panels]) if (p.id !== keep) api.removePanel(p)
    sync()
}

function leaveZen(restore: boolean) {
    if (!dockState.get().zen) return
    const saved = zenSaved
    zenSaved = null
    dockState.set({zen: false})
    if (restore && api) {
        if (!loadJSON(saved)) loadPreset('default')
        sync()
    }
}

export function exitZen() {
    leaveZen(true)
}

export function toggleZen() {
    if (dockState.get().zen) exitZen()
    else enterZen()
}

// ── Divisores ───────────────────────────────────────────────────────────────

// equalizeAtSash: doble clic en un divisor reparte en partes iguales el espacio
// de sus dos lados. dockview no lo ofrece, así que se hace como lo haría una
// mano: se «arrastra» el divisor la mitad de la diferencia con los mismos eventos
// de puntero que usa él, y así respeta los mínimos de cada panel.
export function equalizeAtSash(sash: HTMLElement) {
    const split = sash.closest<HTMLElement>('.dv-split-view-container')
    if (!split) return
    const sashes = [...split.querySelectorAll<HTMLElement>(':scope > .dv-sash-container > .dv-sash')]
    const views = [...split.querySelectorAll<HTMLElement>(':scope > .dv-view-container > .dv-view')]
    const i = sashes.indexOf(sash)
    const a = views[i]
    const b = views[i + 1]
    if (i < 0 || !a || !b) return
    const horizontal = split.classList.contains('dv-horizontal')
    const size = (el: HTMLElement) => (horizontal ? el.getBoundingClientRect().width : el.getBoundingClientRect().height)
    const delta = (size(b) - size(a)) / 2
    if (Math.abs(delta) < 1) return
    const r = sash.getBoundingClientRect()
    const x = r.left + r.width / 2
    const y = r.top + r.height / 2
    const ev = (type: string, dx: number, dy: number) =>
        new PointerEvent(type, {bubbles: true, cancelable: true, pointerId: 1, button: 0, clientX: x + dx, clientY: y + dy})
    sash.dispatchEvent(ev('pointerdown', 0, 0))
    sash.ownerDocument.dispatchEvent(ev('pointermove', horizontal ? delta : 0, horizontal ? 0 : delta))
    sash.ownerDocument.dispatchEvent(ev('pointerup', 0, 0))
}

// Para el banco de capturas (uishot): arma estados concretos del dock.
export const shotTools = {
    float: (id: PanelId) => {
        const p = api?.getPanel(id)
        if (p) floatPanel(p)
    },
    splitOut: (id: PanelId, position: 'right' | 'bottom') => {
        const p = api?.getPanel(id)
        if (p) splitPanel(p, position)
    },
    keepOnly: (id: PanelId) => {
        if (!api?.getPanel(id)) showPanel(id)
        hideOthers(id)
    },
    // Alto/ancho de un grupo como fracción del dock.
    size: (id: PanelId, f: {width?: number; height?: number}) => {
        const g = api?.getPanel(id)?.group
        if (!api || !g) return
        g.api.setSize({
            ...(f.width != null ? {width: Math.round(api.width * f.width)} : {}),
            ...(f.height != null ? {height: Math.round(api.height * f.height)} : {}),
        })
    },
    getApi: () => api,
}

// Lo que la pestaña de un panel muestra de su contenido: el punto de «agente
// vivo» y el contador de problemas. Lo escribe PanelHost (que es quien tiene el
// estado de las terminales y de los diagnósticos); las pestañas se suscriben solas.
export type AgentDot = 'none' | 'running' | 'ended'
export const panelStatus = createStore({
    agent1: 'none' as AgentDot,
    agent2: 'none' as AgentDot,
    errors: 0,
    warnings: 0,
    infos: 0,
})
