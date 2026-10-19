import {Box3, type BufferGeometry, Vector3} from 'three'
import {fmt, locale, type Dict} from '../../i18n'
import {
    boundsOf,
    boxCenter,
    boxSize,
    centerOnBed,
    cleanNum,
    cleanVec,
    cornersBounds,
    defaultPrimitiveSize,
    layFaceDown,
    mirrorKeepingCenter,
    restOnBed,
    sameTf,
    shapeBox,
    tfMatrix,
    type Shape,
    type Tf,
    type Vec3,
} from '../../lib/meshEditMath'
import {
    commit,
    docOf,
    getSession,
    newPartId,
    partName,
    type Cut,
    type CutKeep,
    type CutSplit,
    type Doc,
    type Part,
    type PartMode,
    type Sel,
} from '../../lib/meshEditSession'

// Las operaciones del editor de STL: cada una lee el documento actual, calcula el siguiente y lo entrega a la
// pila (`commit`) con una etiqueta legible, que es la que va luego como comentario al código del checkpoint.
// Las que necesitan la malla (centrar, apoyar, espejar, apoyar sobre una cara, medidas por defecto) usan la
// geometría cruda cargada; hasta que llega, no hacen nada (la interfaz las deshabilita).

export type MoveKind = 'move' | 'rotate' | 'scale'

/** Un número para una etiqueta: hasta dos decimales, con la coma o el punto del idioma. */
const n2 = (v: number) => cleanNum(v, 2).toLocaleString(locale(), {maximumFractionDigits: 2, useGrouping: false})

export function editActions(ws: number, t: Dict, geometry: BufferGeometry | null) {
    const M = t.meshEdit
    const ctx = () => {
        const s = getSession(ws)
        return s ? {s, doc: docOf(s)} : null
    }
    const modelBox = (doc: Doc): Box3 => (geometry ? boundsOf(geometry, tfMatrix(doc.model)) : new Box3())
    const nameOf = (doc: Doc, sel: Exclude<Sel, null>) => (sel.kind === 'model' ? M.theModel : sel.kind === 'part' ? partName(t, doc.parts, sel.id) : M.theCut)
    const v3 = (v: Vec3) => ({x: n2(v[0]), y: n2(v[1]), z: n2(v[2])})
    const setPart = (doc: Doc, id: string, fn: (p: Part) => Part): Doc => ({...doc, parts: doc.parts.map((p) => (p.id === id ? fn(p) : p))})

    return {
        /** Una pose nueva para el modelo o una pieza (la deja el gizmo o un campo numérico). */
        applyTf(sel: Sel, tf: Tf, kind: MoveKind) {
            const c = ctx()
            if (!c || !sel || sel.kind === 'cut') return
            const { doc } = c
            const name = nameOf(doc, sel)
            const label =
                kind === 'move'
                    ? fmt(M.ops.move, { name, ...v3(tf.translate) })
                    : kind === 'rotate'
                      ? fmt(M.ops.rotate, { name, ...v3(tf.rotate) })
                      : fmt(M.ops.scale, { name, ...v3(tf.scale) })
            if (sel.kind === 'model') {
                if (sameTf(doc.model, tf)) return
                commit(ws, { ...doc, model: tf }, label, `${kind}:model`)
                return
            }
            const part = doc.parts.find((p) => p.id === sel.id)
            if (!part || sameTf(part.tf, tf)) return
            commit(ws, setPart(doc, sel.id, (p) => ({ ...p, tf })), label, `${kind}:${sel.id}`)
        },

        mirror(axis: 0 | 1 | 2) {
            const c = ctx()
            if (!c || !geometry) return
            commit(ws, { ...c.doc, model: mirrorKeepingCenter(c.doc.model, axis, geometry) }, fmt(M.ops.mirror, { axis: ['X', 'Y', 'Z'][axis] }), '')
        },
        center() {
            const c = ctx()
            if (!c || !geometry) return
            const tf = centerOnBed(c.doc.model, geometry)
            if (!sameTf(tf, c.doc.model)) commit(ws, { ...c.doc, model: tf }, M.ops.center, '')
        },
        rest() {
            const c = ctx()
            if (!c || !geometry) return
            const tf = restOnBed(c.doc.model, geometry)
            if (!sameTf(tf, c.doc.model)) commit(ws, { ...c.doc, model: tf }, M.ops.rest, '')
        },
        /** `normal` es la de la cara elegida, en el mundo. */
        layFace(normal: Vector3) {
            const c = ctx()
            if (!c || !geometry) return
            commit(ws, { ...c.doc, model: layFaceDown(c.doc.model, geometry, normal) }, M.ops.face, '')
        },

        setModelColor(color: string) {
            const c = ctx()
            if (!c || c.doc.color === color) return
            commit(ws, { ...c.doc, color }, fmt(M.ops.color, { name: M.theModel, color }), 'color:model')
        },
        setPartColor(id: string, color: string) {
            const c = ctx()
            const p = c?.doc.parts.find((x) => x.id === id)
            if (!c || !p || p.color === color) return
            commit(ws, setPart(c.doc, id, (x) => ({ ...x, color })), fmt(M.ops.color, { name: partName(t, c.doc.parts, id), color: color || M.sameAsModel }), `color:${id}`)
        },

        /** Agrega una pieza en el centro del modelo y la deja seleccionada. */
        addPart(shape: Shape, mode: PartMode) {
            const c = ctx()
            if (!c) return
            const box = modelBox(c.doc)
            const ms = boxSize(box)
            const mc = boxCenter(box)
            const size = defaultPrimitiveSize(shape, ms[0] > 0 ? ms : [40, 40, 20], mode)
            const half = shape === 'text' ? 0 : (shape === 'sphere' || shape === 'cylinder' ? size[mode === 'hole' ? 2 : 0] : size[2]) / 2
            // Un agujero va en el centro (atraviesa); una unión se apoya encima del modelo.
            const z = mode === 'hole' ? mc[2] : box.isEmpty() ? half : box.max.z + half
            const id = newPartId(ws)
            const part: Part = {
                id,
                shape,
                mode,
                size,
                text: M.defaultText,
                color: '',
                tf: { translate: cleanVec([mc[0], mc[1], z]), rotate: [0, 0, 0], scale: [1, 1, 1], mirror: [false, false, false] },
            }
            const doc = { ...c.doc, parts: [...c.doc.parts, part] }
            commit(ws, doc, fmt(M.ops.add, { name: partName(t, doc.parts, id), mode: M.modeAs[mode] }), '', { kind: 'part', id })
        },
        setPartSize(id: string, size: Vec3) {
            const c = ctx()
            const p = c?.doc.parts.find((x) => x.id === id)
            if (!c || !p || p.size.every((v, i) => v === size[i])) return
            const shown = p.shape === 'sphere' ? [size[0]] : p.shape === 'cylinder' ? [size[0], size[2]] : size
            commit(ws, setPart(c.doc, id, (x) => ({ ...x, size })), fmt(M.ops.size, { name: partName(t, c.doc.parts, id), size: shown.map(n2).join(' × ') }), `size:${id}`)
        },
        setPartText(id: string, text: string) {
            const c = ctx()
            const p = c?.doc.parts.find((x) => x.id === id)
            if (!c || !p || p.text === text) return
            commit(ws, setPart(c.doc, id, (x) => ({ ...x, text })), fmt(M.ops.text, { name: partName(t, c.doc.parts, id), text }), `text:${id}`)
        },
        setPartMode(id: string, mode: PartMode) {
            const c = ctx()
            const p = c?.doc.parts.find((x) => x.id === id)
            if (!c || !p || p.mode === mode) return
            commit(ws, setPart(c.doc, id, (x) => ({ ...x, mode })), fmt(M.ops.mode, { name: partName(t, c.doc.parts, id), mode: M.modeIs[mode] }), '')
        },
        removePart(id: string) {
            const c = ctx()
            if (!c || !c.doc.parts.some((p) => p.id === id)) return
            commit(ws, { ...c.doc, parts: c.doc.parts.filter((p) => p.id !== id) }, fmt(M.ops.remove, { name: partName(t, c.doc.parts, id) }), '', { kind: 'model' })
        },
        duplicatePart(id: string) {
            const c = ctx()
            const p = c?.doc.parts.find((x) => x.id === id)
            if (!c || !p) return
            const copyId = newPartId(ws)
            // La copia sale desplazada para que se vea que hay dos.
            const step = Math.max(2, Math.min(10, p.size[0] / 2))
            const copy: Part = { ...p, id: copyId, tf: { ...p.tf, translate: cleanVec([p.tf.translate[0] + step, p.tf.translate[1] + step, p.tf.translate[2]]) } }
            const doc = { ...c.doc, parts: [...c.doc.parts, copy] }
            commit(ws, doc, fmt(M.ops.duplicate, { name: partName(t, c.doc.parts, id) }), '', { kind: 'part', id: copyId })
        },

        /** Crea el plano de corte, horizontal, por el centro del modelo. */
        addCut() {
            const c = ctx()
            if (!c || c.doc.cut) return
            const box = modelBox(c.doc)
            const point = cleanVec(boxCenter(box))
            const cut: Cut = { point, normal: [0, 0, 1], keep: 'above', split: 'objects', colors: ['', ''] }
            commit(ws, { ...c.doc, cut }, fmt(M.ops.cutAdd, { ...v3(point), keep: M.keepLabel[cut.keep] }), '', { kind: 'cut' })
        },
        /** La posición y la inclinación del plano (las deja el gizmo o los campos). */
        setCutPlane(point: Vec3, normal: Vec3) {
            const c = ctx()
            const cut = c?.doc.cut
            if (!c || !cut) return
            const samePoint = cut.point.every((v, i) => v === point[i])
            const sameNormal = cut.normal.every((v, i) => v === normal[i])
            if (samePoint && sameNormal) return
            const label = sameNormal ? fmt(M.ops.cutMove, v3(point)) : fmt(M.ops.cutTilt, v3(normal))
            commit(ws, { ...c.doc, cut: { ...cut, point, normal } }, label, sameNormal ? 'cut:move' : 'cut:tilt')
        },
        setCutKeep(keep: CutKeep) {
            const c = ctx()
            const cut = c?.doc.cut
            if (!c || !cut || cut.keep === keep) return
            commit(ws, { ...c.doc, cut: { ...cut, keep } }, fmt(M.ops.cutKeep, { keep: M.keepLabel[keep] }), '')
        },
        setCutSplit(split: CutSplit) {
            const c = ctx()
            const cut = c?.doc.cut
            if (!c || !cut || cut.split === split) return
            commit(ws, { ...c.doc, cut: { ...cut, split } }, fmt(M.ops.cutSplit, { split: M.splitLabel[split] }), '')
        },
        setCutColor(i: 0 | 1, color: string) {
            const c = ctx()
            const cut = c?.doc.cut
            if (!c || !cut || cut.colors[i] === color) return
            const colors: [string, string] = [...cut.colors]
            colors[i] = color
            commit(ws, { ...c.doc, cut: { ...cut, colors } }, fmt(M.ops.cutColors, { a: colors[0] || M.sameAsModel, b: colors[1] || M.sameAsModel }), `cut:colors`)
        },
        removeCut() {
            const c = ctx()
            if (!c || !c.doc.cut) return
            commit(ws, { ...c.doc, cut: null }, M.ops.cutRemove, '', { kind: 'model' })
        },
    }
}

export type EditActions = ReturnType<typeof editActions>

/** La caja del mundo de una pieza (las 8 esquinas de su caja local: una cota si está girada). */
export function partWorldBox(p: Part): Box3 {
    return cornersBounds(shapeBox(p.shape, p.size, p.text), tfMatrix(p.tf))
}
