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

// Una entrada es una malla entera (una geometría) o un modelo por partes (una
// por color, clave `parts:<clave>`): las dos comparten tope y orden de uso, así
// que un modelo por partes no se queda a medias en la caché.
type Entry = {geometries: BufferGeometry[]; bytes: number}
const cache = new Map<string, Entry>()
const inflight = new Map<string, Promise<BufferGeometry>>()
const inflightParts = new Map<string, Promise<BufferGeometry[]>>()
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
        for (const g of entry.geometries) {
            edges.get(g)?.dispose()
            g.dispose()
        }
    }
}

// parse lee un STL binario, sin normales todavía.
function parse(buf: ArrayBuffer): BufferGeometry {
    const g = new STLLoader().parse(buf)
    g.deleteAttribute('normal')
    return g
}

// shade calcula las normales: con pliegue (suaves en curvas, vivas en aristas)
// hasta CREASE_MAX_TRIANGLES; planas por cara por encima.
function shade(g: BufferGeometry): BufferGeometry {
    const triangles = g.attributes.position.count / 3
    let out = g
    if (triangles <= CREASE_MAX_TRIANGLES) {
        out = toCreasedNormals(g, CREASE_ANGLE)
        g.dispose()
    } else {
        g.computeVertexNormals()
    }
    out.computeBoundingBox()
    out.computeBoundingSphere()
    return out
}

// prepare deja la pieza como la pondría el slicer: centrada en XY sobre la
// placa y apoyada en Z=0. La escena es Z arriba, igual que OpenSCAD.
function prepare(buf: ArrayBuffer): BufferGeometry {
    const g = parse(buf)
    g.computeBoundingBox()
    const box = g.boundingBox ?? new Box3()
    const c = box.getCenter(new Vector3())
    g.translate(-c.x, -c.y, -box.min.z)
    return shade(g)
}

// ── Malla CRUDA para el editor de STL (S13) ─────────────────────────────────
//
// El editor aplica la pose del modelo (`translate · rotate · scale · mirror`) ENCIMA de
// la malla tal como está en el archivo, igual que `import()` de OpenSCAD. Por eso esta
// carga NO la centra ni la apoya (lo que hace `prepare`): sus coordenadas son las del
// STL. Comparte la caché LRU con las demás, bajo la clave `raw:<clave>`.

export const rawCacheKey = (meshPath: string) => 'raw:' + meshKey(meshPath)

export function cachedRawMesh(meshPath: string): BufferGeometry | undefined {
    const e = cache.get(rawCacheKey(meshPath))
    if (!e) return undefined
    cache.delete(rawCacheKey(meshPath))
    cache.set(rawCacheKey(meshPath), e)
    return e.geometries[0]
}

export function loadRawMesh(meshPath: string): Promise<BufferGeometry> {
    const key = rawCacheKey(meshPath)
    const hit = cachedRawMesh(meshPath)
    if (hit) return Promise.resolve(hit)
    const pending = inflight.get(key)
    if (pending) return pending
    const p = fetch(meshUrl(meshPath))
        .then((res) => {
            if (!res.ok) throw new Error(`HTTP ${res.status}`)
            return res.arrayBuffer()
        })
        .then((buf) => {
            const geometry = shade(parse(buf))
            const bytes = sizeOf(geometry)
            cache.set(key, {geometries: [geometry], bytes})
            totalBytes += bytes
            evict()
            return geometry
        })
        .finally(() => inflight.delete(key))
    inflight.set(key, p)
    return p
}

// cachedMesh devuelve la geometría si ya está en la caché (y la marca como
// recién usada), sin pedir nada.
export function cachedMesh(key: string): BufferGeometry | undefined {
    const e = cache.get(key)
    if (!e) return undefined
    cache.delete(key)
    cache.set(key, e)
    return e.geometries[0]
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
            cache.set(key, {geometries: [geometry], bytes})
            totalBytes += bytes
            evict()
            return geometry
        })
        .finally(() => inflight.delete(key))
    inflight.set(key, p)
    return p
}

// ── Modelo por partes (S7) ──────────────────────────────────────────────────
//
// Cada parte (un color) llega por GET /parts/<clave>/NN.stl. Se apoyan todas con
// UNA sola traslación —el centro en XY y el mínimo en Z de la unión de las
// partes—, no cada una con la suya: así quedan una dentro de la otra tal como
// las modeló OpenSCAD, y en el mismo lugar que la malla entera.

export const partsCacheKey = (modelKey: string) => 'parts:' + modelKey

export function cachedParts(modelKey: string): BufferGeometry[] | undefined {
    const k = partsCacheKey(modelKey)
    const e = cache.get(k)
    if (!e) return undefined
    cache.delete(k)
    cache.set(k, e)
    return e.geometries
}

export function loadParts(modelKey: string, urls: string[]): Promise<BufferGeometry[]> {
    const hit = cachedParts(modelKey)
    if (hit) return Promise.resolve(hit)
    const k = partsCacheKey(modelKey)
    const pending = inflightParts.get(k)
    if (pending) return pending
    const p = Promise.all(
        urls.map((u) =>
            fetch(u).then((res) => {
                if (!res.ok) throw new Error(`HTTP ${res.status} (${u})`)
                return res.arrayBuffer()
            }),
        ),
    )
        .then((bufs) => {
            const raw = bufs.map(parse)
            const union = new Box3()
            for (const g of raw) {
                g.computeBoundingBox()
                if (g.boundingBox && !g.boundingBox.isEmpty()) union.union(g.boundingBox)
            }
            const c = union.isEmpty() ? new Vector3() : union.getCenter(new Vector3())
            const z = union.isEmpty() ? 0 : union.min.z
            const geometries = raw.map((g) => {
                g.translate(-c.x, -c.y, -z)
                return shade(g)
            })
            const bytes = geometries.reduce((n, g) => n + sizeOf(g), 0)
            cache.set(k, {geometries, bytes})
            totalBytes += bytes
            evict()
            return geometries
        })
        .finally(() => inflightParts.delete(k))
    inflightParts.set(k, p)
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
