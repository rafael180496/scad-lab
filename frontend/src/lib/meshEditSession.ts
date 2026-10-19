import {PrepareMeshEdit} from '../../wailsjs/go/main/App'
import {meshedit} from '../../wailsjs/go/models'
import type {main} from '../../wailsjs/go/models'
import type {Dict} from '../i18n'
import {confirmDialog} from '../components/ui/Dialog'
import type {Shape, Tf, Vec3} from './meshEditMath'
import {onProjectClosed} from './sessionBridge'
import {createStore} from './store'

// Sesión de edición de un STL (S13, 13.2): el estado del modo «Editar» del visor.
//
// Vive en un store de módulo y no en el estado de un componente por dos razones: lo leen a la vez la
// barra, el panel de propiedades, la pila de operaciones y la escena 3D (que está en OTRA raíz de React,
// la del <Canvas>, y no comparte contexto con el resto), y tiene que sobrevivir al cambio de pestaña de
// proyecto sin perder lo que se hizo. No se guarda en disco: «Editar» no persiste al cerrar (se descarta,
// con confirmación si hay cambios).
//
// Este módulo NO importa three (la app abre sin cargarlo): la matemática que la necesita
// (lib/meshEditMath.ts) la llama el editor, que entra con lazy(), y deja aquí el resultado.
//
// El modelo de datos es un `Doc` inmutable por paso: la pila de operaciones es una lista de documentos
// completos (deshacer y rehacer son mover un índice), y a Go solo viaja el ESTADO FINAL como una Scene.

export type PartMode = 'union' | 'hole'
export type CutKeep = 'above' | 'below' | 'both'
export type CutSplit = 'objects' | 'colors'

export type Part = {
    id: string
    shape: Shape
    mode: PartMode
    /** Cubo X·Y·Z; cilindro [Ø, Ø, alto]; esfera [Ø]; texto [tamaño, —, alto de extrusión]. */
    size: Vec3
    text: string
    /** #rrggbb; '' = el del modelo. */
    color: string
    tf: Tf
}

export type Cut = {point: Vec3; normal: Vec3; keep: CutKeep; split: CutSplit; colors: [string, string]}

/** Todo lo que cambia una edición: el modelo, sus piezas y el corte. */
export type Doc = {model: Tf; color: string; parts: Part[]; cut: Cut | null}

export type Entry = {doc: Doc; label: string; key: string}

export type Sel = {kind: 'model'} | {kind: 'part'; id: string} | {kind: 'cut'} | null
export type Tool = 'translate' | 'rotate' | 'scale' | 'face'

export type Snap = {on: boolean; move: number; rotate: number; scale: number}
export const DEFAULT_SNAP: Snap = {on: true, move: 1, rotate: 15, scale: 0.1}

/** Lo que se sabe de la malla base sin cargarla. */
export type Facts = {watertight: boolean; triangles: number; size: Vec3; min: Vec3; max: Vec3; volumeMm3: number}

export type Session = {
    workspaceId: number
    /** El checkpoint que se edita. */
    file: string
    /** El STL de refs/ sobre el que se edita (Scene.ref). */
    ref: string
    /** De qué checkpoint cuelga la edición al aplicarla. */
    parent: string
    /** La malla cruda de `ref` (servida por /mesh/). */
    meshPath: string
    fromImport: boolean
    facts: Facts
    base: Doc
    entries: Entry[]
    /** Cuántas entradas están aplicadas (0 = el documento base). Lo que queda después es lo que se puede rehacer. */
    index: number
    sel: Sel
    tool: Tool
    snap: Snap
    nextId: number
}

export const meshEditStore = createStore<{sessions: Record<number, Session>}>({sessions: {}})

/** La sesión de un proyecto (undefined = no hay). */
export const useSession = (workspaceId: number | null): Session | undefined => meshEditStore.use((s) => (workspaceId == null ? undefined : s.sessions[workspaceId]))

export const getSession = (workspaceId: number): Session | undefined => meshEditStore.get().sessions[workspaceId]

export function docOf(s: Session): Doc {
    return s.index === 0 ? s.base : s.entries[s.index - 1].doc
}

export const isDirty = (s: Session | undefined): boolean => !!s && s.index > 0
export const hasSession = (workspaceId: number): boolean => !!getSession(workspaceId)
export const isSessionDirty = (workspaceId: number): boolean => isDirty(getSession(workspaceId))

function put(workspaceId: number, fn: (s: Session) => Session) {
    const cur = meshEditStore.get().sessions[workspaceId]
    if (!cur) return
    const next = fn(cur)
    if (next === cur) return
    meshEditStore.set({sessions: {...meshEditStore.get().sessions, [workspaceId]: next}})
}

// ── Empezar y terminar ──────────────────────────────────────────────────────

const v3 = (a: number[] | undefined, d: number): Vec3 => [a?.[0] ?? d, a?.[1] ?? d, a?.[2] ?? d]

/** La pose del contrato (escala 0 = 1) a una pose completa. Es normTf de meshEditMath, sin arrastrar three. */
function poseOf(t: meshedit.Transform | undefined): Tf {
    const s = v3(t?.scale, 1)
    return {
        translate: v3(t?.translate, 0),
        rotate: v3(t?.rotate, 0),
        scale: [s[0] || 1, s[1] || 1, s[2] || 1],
        mirror: [!!t?.mirror?.[0], !!t?.mirror?.[1], !!t?.mirror?.[2]],
    }
}

/** El color de un modelo sin color propio (meshedit.DefaultColor). */
export const DEFAULT_MODEL_COLOR = '#9ca3af'

export function startSession(workspaceId: number, file: string, b: main.EditBase): Session {
    const s: Session = {
        workspaceId,
        file,
        ref: b.ref,
        parent: b.parent,
        meshPath: b.meshPath,
        fromImport: b.fromImport,
        facts: {
            watertight: !!b.report?.watertight,
            triangles: b.report?.triangles ?? 0,
            size: v3(b.report?.size, 0),
            min: v3(b.report?.min, 0),
            max: v3(b.report?.max, 0),
            volumeMm3: b.report?.volumeMm3 ?? 0,
        },
        base: {model: poseOf(b.model), color: b.color || DEFAULT_MODEL_COLOR, parts: [], cut: null},
        entries: [],
        index: 0,
        sel: {kind: 'model'},
        tool: 'translate',
        snap: DEFAULT_SNAP,
        nextId: 1,
    }
    meshEditStore.set({sessions: {...meshEditStore.get().sessions, [workspaceId]: s}})
    return s
}

export function endSession(workspaceId: number) {
    const all = meshEditStore.get().sessions
    if (!all[workspaceId]) return
    const rest = {...all}
    delete rest[workspaceId]
    meshEditStore.set({sessions: rest})
}

// Cerrar la pestaña de un proyecto descarta su edición.
onProjectClosed(endSession)

const preparing = new Set<number>()
export const isPreparing = (workspaceId: number) => preparing.has(workspaceId)

/**
 * Prepara la edición de un checkpoint (Go congela su malla en refs/ si hace falta) y abre la sesión.
 * Puede tardar: si el checkpoint no tiene render, Go lo renderiza. Un error lo propaga quien llama.
 */
export async function prepareSession(workspaceId: number, file: string): Promise<Session | null> {
    if (preparing.has(workspaceId)) return null
    preparing.add(workspaceId)
    try {
        const b = await PrepareMeshEdit(workspaceId, file)
        return startSession(workspaceId, file, b)
    } finally {
        preparing.delete(workspaceId)
    }
}

/**
 * Antes de cambiar de checkpoint con una edición abierta: sin cambios se cierra sola; con cambios
 * se pregunta. true = se puede seguir (y la sesión ya se cerró).
 */
export async function confirmLeaveMeshEdit(t: Dict, workspaceId: number): Promise<boolean> {
    const s = getSession(workspaceId)
    if (!s) return true
    if (isDirty(s)) {
        const ok = await confirmDialog(t.meshEdit.discardConfirm, t.meshEdit.discardOk, t.meshEdit.keepEditing, {danger: true})
        if (!ok) return false
    }
    endSession(workspaceId)
    return true
}

// ── Pila de operaciones ─────────────────────────────────────────────────────

/** ¿Sigue existiendo lo seleccionado en este documento? Si no, se deselecciona. */
function validSel(sel: Sel, doc: Doc): Sel {
    if (!sel || sel.kind === 'model') return sel
    if (sel.kind === 'cut') return doc.cut ? sel : null
    return doc.parts.some((p) => p.id === sel.id) ? sel : null
}

/**
 * Aplica una operación: el documento resultante entra a la pila (lo que se había deshecho se
 * pierde). Dos operaciones seguidas con la misma `key` —mover el mismo objeto varias veces— se
 * funden en una: la pila cuenta qué se hizo, no cada empujón. Una `key` vacía nunca se funde.
 */
export function commit(workspaceId: number, doc: Doc, label: string, key: string, sel?: Sel) {
    put(workspaceId, (s) => {
        const last = s.entries[s.index - 1]
        const entry: Entry = {doc, label, key}
        const merge = key !== '' && s.index === s.entries.length && !!last && last.key === key
        const entries = merge ? [...s.entries.slice(0, s.index - 1), entry] : [...s.entries.slice(0, s.index), entry]
        return {...s, entries, index: entries.length, sel: sel === undefined ? validSel(s.sel, doc) : sel}
    })
}

export function jump(workspaceId: number, index: number) {
    put(workspaceId, (s) => {
        const i = Math.max(0, Math.min(index, s.entries.length))
        if (i === s.index) return s
        const doc = i === 0 ? s.base : s.entries[i - 1].doc
        return {...s, index: i, sel: validSel(s.sel, doc)}
    })
}
export const undo = (workspaceId: number) => jump(workspaceId, (getSession(workspaceId)?.index ?? 0) - 1)
export const redo = (workspaceId: number) => jump(workspaceId, (getSession(workspaceId)?.index ?? 0) + 1)
export const canUndo = (s: Session | undefined) => !!s && s.index > 0
export const canRedo = (s: Session | undefined) => !!s && s.index < s.entries.length

export const select = (workspaceId: number, sel: Sel) => put(workspaceId, (s) => (JSON.stringify(s.sel) === JSON.stringify(sel) ? s : {...s, sel}))
export const setTool = (workspaceId: number, tool: Tool) => put(workspaceId, (s) => (s.tool === tool ? s : {...s, tool}))
export const setSnap = (workspaceId: number, snap: Partial<Snap>) => put(workspaceId, (s) => ({...s, snap: {...s.snap, ...snap}}))

/** Un id nuevo para una pieza (p1, p2…): no se reutiliza aunque se borre una. */
export function newPartId(workspaceId: number): string {
    let id = 'p1'
    put(workspaceId, (s) => {
        id = `p${s.nextId}`
        return {...s, nextId: s.nextId + 1}
    })
    return id
}

/** Nombre de una pieza para mostrar: «Cilindro 2» (la segunda de ese tipo). */
export function partName(t: Dict, parts: Part[], id: string): string {
    const p = parts.find((x) => x.id === id)
    if (!p) return ''
    const n = parts.filter((x) => x.shape === p.shape).findIndex((x) => x.id === id) + 1
    return `${t.meshEdit.shape[p.shape]} ${n}`
}

// ── A Go ────────────────────────────────────────────────────────────────────

const pose = (t: Tf) => ({translate: [...t.translate], rotate: [...t.rotate], scale: [...t.scale], mirror: [...t.mirror]})

/** Las etiquetas de lo aplicado, en orden: van como comentarios al código del checkpoint. */
export const opsOf = (s: Session): string[] => s.entries.slice(0, s.index).map((e) => e.label)

/** El estado final como la escena que genera el código. Los números ya vienen limpios (meshEditMath.cleanTf). */
export function toScene(s: Session): meshedit.Scene {
    const d = docOf(s)
    return meshedit.Scene.createFrom({
        ref: s.ref,
        model: pose(d.model),
        color: d.color,
        parts: d.parts.map((p) => ({
            id: p.id,
            shape: p.shape,
            mode: p.mode,
            size: [...p.size],
            text: p.text,
            // El editor trabaja con las piezas centradas en su origen: el gizmo y las medidas son las de su centro.
            center: true,
            color: p.color,
            transform: pose(p.tf),
        })),
        cut: d.cut ? {point: [...d.cut.point], normal: [...d.cut.normal], keep: d.cut.keep, split: d.cut.keep === 'both' ? d.cut.split : '', colors: [...d.cut.colors]} : undefined,
        ops: opsOf(s),
    })
}

/** ¿La edición hace booleanos (cortar o perforar)? Una malla abierta los rechaza (Manifold y CGAL). */
export const needsClosedMesh = (d: Doc): boolean => !!d.cut || d.parts.some((p) => p.mode === 'hole')
