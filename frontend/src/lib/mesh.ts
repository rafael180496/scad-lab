import {Box3, BufferGeometry, EdgesGeometry, Vector3} from 'three'
import {STLLoader} from 'three/examples/jsm/loaders/STLLoader.js'
import {toCreasedNormals} from 'three/examples/jsm/utils/BufferGeometryUtils.js'

// Mallas del visor 3D (ver claude/04-frontend.md, «Visor 3D»).
//
// La malla NO viaja por un binding: Go la sirve en /mesh/<clave>.stl (un
// binding con []byte cruzaría como base64 dentro de JSON). Acá se pide con
// fetch, se parsea una sola vez y queda en una caché LRU en memoria: volver a
// un checkpoint ya visto no pide nada.

// meshKey es el nombre base de render.Result.meshPath (64 hex + .stl). La
// ruta puede venir con separadores de Windows.
export function meshKey(meshPath: string): string {
    const parts = meshPath.split(/[\\/]/)
    return parts[parts.length - 1] ?? ''
}

export function meshUrl(meshPath: string): string {
    return '/mesh/' + meshKey(meshPath)
}

// Topes de la caché: lo que se alcance primero. Una malla de 1 M de
// triángulos con normales ocupa ~72 MB, así que 300 MB son unas cuatro de
// esas o decenas de piezas normales.
const MAX_BYTES = 300 * 1024 * 1024
const MAX_ENTRIES = 24
// Por encima de esto las normales «con pliegue» (suaves en curvas, vivas en
// aristas) tardan demasiado: se queda con las normales planas por cara.
const CREASE_MAX_TRIANGLES = 400_000
const CREASE_ANGLE = (30 * Math.PI) / 180

type Entry = {geometry: BufferGeometry; bytes: number}
const cache = new Map<string, Entry>()
const inflight = new Map<string, Promise<BufferGeometry>>()
const edges = new WeakMap<BufferGeometry, EdgesGeometry>()
let totalBytes = 0

function sizeOf(g: BufferGeometry): number {
    let n = g.index ? g.index.array.byteLength : 0
    for (const a of Object.values(g.attributes)) n += a.array.byteLength
    return n
}

function evict() {
    // Map conserva el orden de inserción: la primera es la menos usada.
    for (const [key, entry] of cache) {
        if (cache.size <= 1 || (totalBytes <= MAX_BYTES && cache.size <= MAX_ENTRIES)) break
        cache.delete(key)
        totalBytes -= entry.bytes
        edges.get(entry.geometry)?.dispose()
        entry.geometry.dispose()
    }
}

// prepare deja la pieza como la pondría el slicer: centrada en XY sobre la
// placa y apoyada en Z=0. La escena es Z arriba, igual que OpenSCAD.
function prepare(buf: ArrayBuffer): BufferGeometry {
    let g: BufferGeometry = new STLLoader().parse(buf)
    g.deleteAttribute('normal')
    g.computeBoundingBox()
    const box = g.boundingBox ?? new Box3()
    const c = box.getCenter(new Vector3())
    g.translate(-c.x, -c.y, -box.min.z)
    const triangles = g.attributes.position.count / 3
    if (triangles <= CREASE_MAX_TRIANGLES) {
        const creased = toCreasedNormals(g, CREASE_ANGLE)
        g.dispose()
        g = creased
    } else {
        g.computeVertexNormals()
    }
    g.computeBoundingBox()
    g.computeBoundingSphere()
    return g
}

// cachedMesh devuelve la geometría si ya está en la caché (y la marca como
// recién usada), sin pedir nada.
export function cachedMesh(key: string): BufferGeometry | undefined {
    const e = cache.get(key)
    if (!e) return undefined
    cache.delete(key)
    cache.set(key, e)
    return e.geometry
}

export function loadMesh(meshPath: string): Promise<BufferGeometry> {
    const key = meshKey(meshPath)
    const hit = cachedMesh(key)
    if (hit) return Promise.resolve(hit)
    const pending = inflight.get(key)
    if (pending) return pending
    const p = fetch(meshUrl(meshPath))
        .then((res) => {
            if (!res.ok) throw new Error(`HTTP ${res.status}`)
            return res.arrayBuffer()
        })
        .then((buf) => {
            const geometry = prepare(buf)
            const bytes = sizeOf(geometry)
            cache.set(key, {geometry, bytes})
            totalBytes += bytes
            evict()
            return geometry
        })
        .finally(() => inflight.delete(key))
    inflight.set(key, p)
    return p
}

// edgesOf calcula las aristas (ángulo > 30°) una sola vez por malla y solo si
// alguien las pide: en una pieza grande cuesta lo mismo que cargarla.
export function edgesOf(g: BufferGeometry): EdgesGeometry {
    let e = edges.get(g)
    if (!e) {
        e = new EdgesGeometry(g, 30)
        edges.set(g, e)
    }
    return e
}

export function triangleCount(g: BufferGeometry): number {
    return g.index ? g.index.count / 3 : g.attributes.position.count / 3
}
