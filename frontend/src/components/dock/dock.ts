import type {AddPanelOptions, DockviewApi, DockviewGroupPanel, IDockviewPanel} from 'dockview-react'
import {store} from '../../../wailsjs/go/models'
import {SaveDockLayout, SaveDockLayouts} from '../../../wailsjs/go/main/App'
import {t as tNow} from '../../i18n'
import {appearance} from '../../lib/appearance'
import {errorText} from '../../lib/errors'
import {createStore} from '../../lib/store'
import {toast} from '../ui/Toast'
import {buildPreset, minimums, parseEnvelope, parseMinimized, toEnvelope, type MinimizedEntry, type PresetId} from './layouts'
import {PANEL_IDS, isPanelId, type PanelId} from './panels'

// Controlador de los paneles acoplables (S4b). Hay un solo dock en la ventana, así
// que vive como módulo: el menú Ver de la cabecera, los atajos, el menú de la
// pestaña y la pantalla vacía lo usan sin pasarse props. dockview es dueño de la
// geometría; esto lleva la cuenta de qué paneles están a la vista (`dockState`),
// guarda el diseño (con espera) y arma/aplica los diseños.

export type Flags = Record<PanelId, boolean>
export type SavedLayout = {name: string; json: string}

const none = (): Flags => ({viewport: false, source: false, 'agent-1': false, 'agent-2': false, chat: false, problems: false, print: false})

export const dockState = createStore({
    ready: false,
    /** Está en el dock (en algún grupo, flotante o no). */
    present: none(),
    /** Se ve ahora: es la pestaña activa de su grupo y el grupo está a la vista. */
    visible: none(),
    floating: none(),
    active: null as PanelId | null,
    /** Id del grupo con el foco (el que usan «Minimizar» del menú Ver y su atajo). */
    activeGroup: null as string | null,
    maximized: false,
    /** Ids de los grupos minimizados (achicados a su barra de pestañas o a una franja). */
    minimized: [] as string[],
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
        activeGroup: api.activeGroup?.id ?? null,
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
    SaveDockLayout(envelope()).catch((e) => toast.error(errorText(e)))
}

function schedulePersist() {
    if (loading || dockState.get().zen) return
    window.clearTimeout(timer)
    timer = window.setTimeout(persistNow, PERSIST_DELAY_MS)
}

// ── Carga de diseños ────────────────────────────────────────────────────────

// ── Minimizar ───────────────────────────────────────────────────────────────
//
// Minimizar achica un grupo a lo mínimo SIN cerrarlo ni desmontar nada: el
// contenido sigue en su sitio, solo que sin espacio. Según cómo esté dividido:
//   · apilado arriba/abajo (o flotante): queda solo su barra de pestañas (`bar`);
//   · lado a lado: queda una franja angosta con los íconos de sus pestañas (`strip`).
// Se logra con las restricciones del GRUPO (mínimo = máximo en la dimensión que se
// achica), y los vecinos se reparten el resto. Las restricciones por panel de
// dockview no sirven: `panel.api.setConstraints` no tiene a nadie escuchando; el
// grupo lee las suyas explícitas, que mandan sobre las del panel (ver
// `syncConstraints`). El mínimo especial vale solo mientras dura el estado
// minimizado: al restaurar el grupo vuelve a los mínimos de su panel activo.

type MinRec = {dim: 'width' | 'height'; prev: number}
const minimized = new Map<string, MinRec>()
const BIG = Number.MAX_SAFE_INTEGER
/** Alto de la barra de pestañas (`--dv-tabs-and-actions-container-height` de dock.css). */
const barSize = () => Math.round(35 * scale())
/** Ancho de la franja de un grupo minimizado al lado de otro. */
const stripSize = () => Math.round(36 * scale())

type GridNode = {type: 'leaf'; data: {id: string}} | {type: 'branch'; data: GridNode[]}
type GridJSON = {root: Extract<GridNode, {type: 'branch'}>; orientation: 'HORIZONTAL' | 'VERTICAL'}
type Shape = {dim: 'width' | 'height'; siblings: GridNode[]}

function gridJSON(): GridJSON | null {
    return api ? (api.toJSON().grid as unknown as GridJSON) : null
}

// gridShape dice qué dimensión se achica en un grupo y quiénes son sus vecinos.
// La orientación de cada rama alterna con la profundidad: hijos de una rama
// HORIZONTAL van lado a lado (se achica el ancho), los de una VERTICAL apilados.
function gridShape(g: DockviewGroupPanel): Shape | null {
    if (g.api.location.type === 'floating') return {dim: 'height', siblings: []}
    const grid = gridJSON()
    if (!grid) return null
    const walk = (node: Extract<GridNode, {type: 'branch'}>, o: GridJSON['orientation']): Shape | null => {
        for (const child of node.data) {
            if (child.type === 'leaf' && child.data.id === g.id) {
                return {dim: o === 'HORIZONTAL' ? 'width' : 'height', siblings: node.data.filter((c) => c !== child)}
            }
            if (child.type === 'branch') {
                const r = walk(child, o === 'HORIZONTAL' ? 'VERTICAL' : 'HORIZONTAL')
                if (r) return r
            }
        }
        return null
    }
    return walk(grid.root, grid.orientation)
}

export function isMinimized(g: DockviewGroupPanel | undefined | null): boolean {
    return !!g && minimized.has(g.id)
}

// canMinimize: un grupo solo en la grilla no se puede minimizar (no habría quien
// ocupe su espacio), ni uno cuyos vecinos ya estén todos minimizados del mismo
// lado: quedaría un hueco. Uno ya minimizado siempre se puede restaurar.
export function canMinimize(g: DockviewGroupPanel): boolean {
    if (minimized.has(g.id)) return true
    const s = gridShape(g)
    if (!s) return false
    if (g.api.location.type === 'floating') return true
    return s.siblings.some((n) => !(n.type === 'leaf' && minimized.get(n.data.id)?.dim === s.dim))
}

// desiredConstraints: lo que el grupo debe tener ahora. Sin minimizar son los
// mínimos de su panel activo (× escala de la interfaz) y sin tope; minimizado, el
// ancho o el alto fijos (mínimo = máximo) de la franja o la barra.
function desiredConstraints(g: DockviewGroupPanel) {
    const active = g.activePanel
    const base = active && isPanelId(active.id) ? minimums(active.id, scale()) : {minimumWidth: 100, minimumHeight: 100}
    const rec = minimized.get(g.id)
    const fix = (dim: 'width' | 'height', size: number, min: number) => (rec?.dim === dim ? {min: size, max: size} : {min, max: BIG})
    const w = fix('width', stripSize(), base.minimumWidth)
    const h = fix('height', barSize(), base.minimumHeight)
    return {minimumWidth: w.min, maximumWidth: w.max, minimumHeight: h.min, maximumHeight: h.max}
}

// syncConstraints aplica a cada grupo sus restricciones (solo si cambiaron: cada
// cambio provoca una pasada de layout). También reemplaza al antiguo
// reaplicado de los mínimos al cambiar la escala de la interfaz.
function syncConstraints() {
    if (!api) return
    for (const g of api.groups) {
        const d = desiredConstraints(g)
        if (g.minimumWidth === d.minimumWidth && g.maximumWidth === d.maximumWidth && g.minimumHeight === d.minimumHeight && g.maximumHeight === d.maximumHeight) continue
        g.api.setConstraints(d)
    }
}

function markMinimized(g: DockviewGroupPanel) {
    const rec = minimized.get(g.id)
    if (rec) g.element.dataset.min = rec.dim === 'width' ? 'strip' : 'bar'
    else delete g.element.dataset.min
}

function publishMinimized() {
    const ids = [...minimized.keys()]
    const cur = dockState.get().minimized
    if (ids.length !== cur.length || ids.some((id, i) => id !== cur[i])) dockState.set({minimized: ids})
}

// reconcileMinimized pone al día lo minimizado tras cualquier cambio de la grilla
// (también los arrastres): olvida los grupos que ya no existen o que se quedaron
// solos, sigue al grupo si cambió de orientación, y repasa restricciones y marcas.
let reconciling = false
function reconcileMinimized() {
    if (!api || loading || reconciling) return
    reconciling = true
    try {
        if (minimized.size) {
            const alive = new Map(api.groups.map((g) => [g.id, g]))
            for (const id of [...minimized.keys()]) {
                const g = alive.get(id)
                const s = g ? gridShape(g) : null
                if (!g || !s || (g.api.location.type !== 'floating' && s.siblings.length === 0)) {
                    minimized.delete(id)
                    continue
                }
                const rec = minimized.get(id)!
                if (rec.dim !== s.dim) {
                    // Cambió de lado a lado a apilado (o a flotante): el tamaño anterior
                    // era de la otra dimensión.
                    rec.dim = s.dim
                    rec.prev = Math.round((s.dim === 'width' ? 340 : 260) * scale())
                }
            }
        }
        syncConstraints()
        for (const g of api.groups) markMinimized(g)
        publishMinimized()
    } finally {
        reconciling = false
    }
}

function minimizedEntries(): MinimizedEntry[] {
    if (!api) return []
    const out: MinimizedEntry[] = []
    for (const [id, rec] of minimized) {
        const g = api.groups.find((x) => x.id === id)
        const p = g?.panels.find((x) => isPanelId(x.id))
        if (g && p) out.push({panel: p.id as PanelId, dim: rec.dim, prev: Math.round(rec.prev)})
    }
    return out
}

function envelope(): string {
    return api ? toEnvelope(api, minimizedEntries()) : ''
}

// setGroupSize deja el grupo en ese tamaño. Un grupo flotante tiene un borde de
// 1 px por lado que el tamaño de dockview no cuenta: se mide y se corrige una vez,
// para que la barra minimizada mida de verdad lo que su barra de pestañas.
function setGroupSize(g: DockviewGroupPanel, dim: 'width' | 'height', size: number) {
    g.api.setSize({[dim]: size})
    if (g.api.location.type !== 'floating') return
    const r = g.element.getBoundingClientRect()
    const got = dim === 'width' ? r.width : r.height
    if (got > 0 && Math.abs(got - size) > 0.5) g.api.setSize({[dim]: Math.round(size + (size - got))})
}

export function minimizeGroup(g: DockviewGroupPanel): boolean {
    if (!api || minimized.has(g.id) || !canMinimize(g)) return false
    const s = gridShape(g)
    if (!s) return false
    if (api.hasMaximizedGroup()) api.exitMaximizedGroup()
    const size = s.dim === 'width' ? stripSize() : barSize()
    minimized.set(g.id, {dim: s.dim, prev: Math.max(g.api[s.dim], size * 2)})
    markMinimized(g)
    syncConstraints()
    // Las restricciones nuevas se evalúan en la pasada de layout siguiente; el
    // setSize deja el tamaño final ya mismo.
    setGroupSize(g, s.dim, size)
    publishMinimized()
    return true
}

export function restoreGroup(g: DockviewGroupPanel): boolean {
    const rec = minimized.get(g.id)
    if (!api || !rec) return false
    minimized.delete(g.id)
    markMinimized(g)
    syncConstraints()
    setGroupSize(g, rec.dim, rec.prev)
    publishMinimized()
    return true
}

export function toggleMinimize(g: DockviewGroupPanel) {
    if (minimized.has(g.id)) restoreGroup(g)
    else if (!minimizeGroup(g)) toast.error(tNow().dock.minimizeAlone)
}

export function toggleMinimizeActive() {
    const g = api?.activeGroup ?? api?.activePanel?.group
    if (g) toggleMinimize(g)
}

// restoreGroupAt restaura el grupo minimizado que contiene ese elemento (doble
// clic en el vacío de su barra de pestañas).
export function restoreGroupAt(el: Element): boolean {
    const g = api?.groups.find((x) => x.element.contains(el))
    return !!g && restoreGroup(g)
}

function restoreIfMinimized(g: DockviewGroupPanel | undefined) {
    if (g) restoreGroup(g)
}

// loadJSON carga un diseño guardado. false si no se pudo (texto roto, otra
// versión, paneles desconocidos o dockview lo rechazó): quien llama cae a otro.
function loadJSON(json: string | null | undefined): boolean {
    const data = parseEnvelope(json)
    if (!api || !data) return false
    loading++
    try {
        minimized.clear()
        api.fromJSON(data)
        if (!api.panels.every((p) => isPanelId(p.id))) throw new Error('panel desconocido') // i18n-ignore: el catch de abajo solo lo manda a console.warn
        // Lo minimizado se guarda aparte (`min` del sobre) y se re-anota por un
        // panel de cada grupo; reconcile descarta lo que ya no tenga sentido.
        for (const e of parseMinimized(json)) {
            const g = api.getPanel(e.panel)?.group
            if (g && !minimized.has(g.id)) minimized.set(g.id, {dim: e.dim, prev: e.prev})
        }
        return true
    } catch (e) {
        console.warn('diseño de paneles inválido, se usa el predeterminado', e)
        minimized.clear()
        api.clear()
        return false
    } finally {
        loading--
        // Los mínimos guardados pueden ser de otra escala de interfaz.
        reconcileMinimized()
    }
}

function loadPreset(preset: PresetId) {
    if (!api) return
    loading++
    try {
        minimized.clear()
        api.clear()
        buildPreset(api, preset, scale())
    } finally {
        loading--
        reconcileMinimized()
    }
}

// Solo el banco de capturas (uishot): arma un estado concreto del dock dentro de
// `attach`, antes del primer dibujo. Hacerlo después, desde un efecto, dejaba una
// carrera: la captura podía salir con la capa de contenido de un panel todavía
// con la medida anterior.
export const dockShot: {
    mode?: 'custom' | 'hidden' | 'tall' | 'minimized' | 'print' | 'diff'
    /** Menú «…» del grupo de un panel a abrir; con `submenu`, entra en esa entrada (su `data-menu-id`). */
    menu?: {panel: PanelId; submenu?: string}
} = {}

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
        case 'minimized':
            // El código a una franja a la derecha y los agentes a su barra, abajo: el
            // visor se queda con todo lo demás.
            shotTools.minimize('source')
            shotTools.minimize('agent-1')
            break
        case 'print':
            // Panel Impresión: la tabla y los parámetros caben sin desplazarse y el visor
            // conserva alto para mostrar la tarjeta de la parte.
            shotTools.size('agent-1', {height: 0.46})
            break
        case 'diff':
            // Comparar con el padre: el código ancho para que el diff quede lado a lado.
            shotTools.size('source', {width: 0.6})
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
            reconcileMinimized()
            sync()
            enforceMinimums()
            schedulePersist()
        }),
        // Los mínimos de un grupo son los de su panel activo: al cambiar de pestaña
        // se repasan.
        a.onDidActivePanelChange(() => {
            reconcileMinimized()
            sync()
        }),
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
    minimized.clear()
    dockState.set({ready: false, present: none(), visible: none(), floating: none(), active: null, activeGroup: null, minimized: [], maximized: false})
}

// Los mínimos escalan con la interfaz: se reaplican al cambiar de tamaño.
export function rescaleConstraints() {
    reconcileMinimized()
}

// ── Mostrar, ocultar, enfocar ───────────────────────────────────────────────

const BOTTOM: PanelId[] = ['agent-1', 'agent-2', 'chat', 'problems', 'print']

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
        restoreIfMinimized(existing.group)
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
    // Si entró como pestaña de un grupo minimizado no se vería: se restaura.
    restoreIfMinimized(p.group)
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
    else {
        restoreIfMinimized(p.group)
        p.api.setActive()
    }
}

export function hideOthers(id: PanelId) {
    if (!api) return
    for (const p of [...api.panels]) if (p.id !== id) hidePanel(p.id as PanelId)
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
    const target = largestGridGroup(p.group)
    if (target) {
        restoreIfMinimized(target)
        p.api.moveTo({group: target, position: 'center'})
    } else movePanelTo(p, 'right')
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
    if (!p) return
    // Un grupo minimizado se restaura antes de maximizarlo: maximizar y minimizar
    // a la vez no tiene sentido y el tope de tamaño del minimizado lo impediría.
    restoreIfMinimized(p.group)
    api.maximizeGroup(p)
}

// ── Mover a… ────────────────────────────────────────────────────────────────
//
// Destinos explícitos que no dependen de arrastrar (el arrastre en la webview no
// se puede probar de forma automática, así que esta es la vía garantizada).
// Izquierda/Derecha/Arriba/Abajo son respecto del área de paneles COMPLETA:
//   · si ya hay un grupo ocupando ese borde entero (hijo directo de la raíz, en el
//     eje que corresponde) el panel entra a él como pestaña;
//   · si no, se crea uno nuevo que abarca todo ese borde (`addGroup` sin referencia).
// Centro = pestaña del grupo acoplado más grande (casi siempre el del visor).
// «Junto a…» = pestaña del grupo de otro panel. Mover nunca destruye el contenido:
// el panel solo cambia de grupo (el contenido vive fuera de dockview).

export type MoveTarget = 'left' | 'right' | 'top' | 'bottom' | 'center'
const DIRECTION = {left: 'left', right: 'right', top: 'above', bottom: 'below'} as const

function largestGridGroup(except?: DockviewGroupPanel): DockviewGroupPanel | undefined {
    if (!api) return undefined
    return api.groups
        .filter((g) => g.api.location.type === 'grid' && g !== except && !minimized.has(g.id))
        .sort((a, b) => b.api.width * b.api.height - a.api.width * a.api.height)[0]
}

// edgeGroup: el grupo que ocupa ese borde de toda el área, si lo hay.
function edgeGroup(target: Exclude<MoveTarget, 'center'>): DockviewGroupPanel | undefined {
    const grid = gridJSON()
    if (!api || !grid) return undefined
    const horizontal = grid.orientation === 'HORIZONTAL'
    if ((target === 'left' || target === 'right') !== horizontal) return undefined
    const kids = grid.root.data
    const node = target === 'left' || target === 'top' ? kids[0] : kids[kids.length - 1]
    if (node?.type !== 'leaf') return undefined
    return api.groups.find((g) => g.id === node.data.id)
}

// moveDisabled: destinos que no cambiarían nada (el panel ya está solo en ese
// borde, o no hay otro grupo al que sumarse).
export function moveDisabled(p: IDockviewPanel, target: MoveTarget): boolean {
    if (!api) return true
    const alone = p.group.panels.length < 2
    if (target === 'center') return !largestGridGroup(p.group)
    if (alone && api.groups.length < 2) return true
    return alone && edgeGroup(target) === p.group
}

export function movePanelTo(p: IDockviewPanel, target: MoveTarget) {
    if (!api || moveDisabled(p, target)) return
    if (target === 'center') {
        const dest = largestGridGroup(p.group)
        if (!dest) return
        restoreIfMinimized(dest)
        p.api.moveTo({group: dest, position: 'center'})
        return
    }
    const reuse = edgeGroup(target)
    if (reuse && reuse !== p.group) {
        restoreIfMinimized(reuse)
        p.api.moveTo({group: reuse, position: 'center'})
        return
    }
    const g = api.addGroup({direction: DIRECTION[target]})
    p.api.moveTo({group: g, position: 'center'})
    // Un grupo nuevo entra con la mitad del espacio: se deja en un tamaño de borde.
    if (g.api.location.type === 'grid') {
        if (target === 'left' || target === 'right') g.api.setSize({width: Math.round(api.width * 0.34)})
        else g.api.setSize({height: Math.round(api.height * 0.32)})
    }
}

// movePanelBeside suma el panel como pestaña del grupo de otro panel.
export function movePanelBeside(p: IDockviewPanel, mate: PanelId) {
    if (!api) return
    const dest = api.getPanel(mate)?.group
    if (!dest || dest === p.group) return
    restoreIfMinimized(dest)
    p.api.moveTo({group: dest, position: 'center'})
}

// presentPanels: los demás paneles que están en el dock (destinos de «Junto a…»).
export function otherPanels(p: IDockviewPanel): PanelId[] {
    return api ? PANEL_IDS.filter((id) => id !== p.id && !!api!.getPanel(id)) : []
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
    return api ? envelope() : null
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
    zenSaved = envelope()
    dockState.set({zen: true})
    if (api.hasMaximizedGroup()) api.exitMaximizedGroup()
    // Un panel minimizado no sirve de modo enfoque: se restaura (el diseño de antes
    // ya guardó que lo estaba).
    restoreIfMinimized(keep ? api.getPanel(keep)?.group : undefined)
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
    minimize: (id: PanelId) => {
        const g = api?.getPanel(id)?.group
        if (g) minimizeGroup(g)
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
