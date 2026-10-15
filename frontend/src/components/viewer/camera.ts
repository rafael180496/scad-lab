import {Box3, Matrix4, OrthographicCamera, PerspectiveCamera, Vector3, type Camera} from 'three'

// Matemática de cámara del visor. La escena es Z arriba (como OpenSCAD):
// las vistas se definen como la dirección DESDE la que se mira el centro.

export type ViewName = 'front' | 'back' | 'left' | 'right' | 'top' | 'bottom' | 'iso'
export const VIEWS: ViewName[] = ['front', 'back', 'left', 'right', 'top', 'bottom', 'iso']

// Arriba/Abajo llevan un épsilon en Y: con la cámara exactamente sobre el eje
// Z (el «arriba» de la escena) la orientación queda indefinida y la pieza
// puede aparecer girada. Así, en Arriba el +Y queda hacia arriba en pantalla.
const DIRS: Record<ViewName, [number, number, number]> = {
    front: [0, -1, 0],
    back: [0, 1, 0],
    left: [-1, 0, 0],
    right: [1, 0, 0],
    top: [0, -1e-4, 1],
    bottom: [0, -1e-4, -1],
    // Un poco girada respecto de la diagonal exacta: así las aristas
    // verticales de la caja de impresión no cruzan la pieza por el medio.
    iso: [1, -1.35, 1],
}

export const PERSP_FOV = 35
const FIT_MARGIN = 1.08

export function makeCameras(): {persp: PerspectiveCamera; ortho: OrthographicCamera} {
    const persp = new PerspectiveCamera(PERSP_FOV, 1, 0.5, 50_000)
    const ortho = new OrthographicCamera(-1, 1, 1, -1, -50_000, 50_000)
    for (const c of [persp, ortho]) {
        c.up.set(0, 0, 1)
        // La cama se dibuja en la capa 1: la ven las cámaras del visor pero
        // no la de las sombras de contacto (no proyecta sombra sobre sí).
        c.layers.enable(1)
        // Proyección a cargo del visor (resize y cambio orto/persp).
        ;(c as Camera & {manual?: boolean}).manual = true
    }
    persp.position.set(200, -200, 160)
    ortho.position.copy(persp.position)
    return {persp, ortho}
}

export function updateProjection(cam: PerspectiveCamera | OrthographicCamera, w: number, h: number) {
    if (w <= 0 || h <= 0) return
    if (cam instanceof PerspectiveCamera) {
        cam.aspect = w / h
    } else {
        cam.left = -w / 2
        cam.right = w / 2
        cam.top = h / 2
        cam.bottom = -h / 2
    }
    cam.updateProjectionMatrix()
}

// visibleHeight es cuánto mundo (mm) entra de alto en pantalla a la
// distancia del objetivo: es lo que se conserva al pasar de orto a persp.
function visibleHeight(cam: PerspectiveCamera | OrthographicCamera, target: Vector3, h: number): number {
    if (cam instanceof PerspectiveCamera) {
        return 2 * cam.position.distanceTo(target) * Math.tan((cam.fov * Math.PI) / 360)
    }
    return h / cam.zoom
}

// switchProjection copia la vista de una cámara a la otra sin que la pieza
// salte de tamaño.
export function switchProjection(
    from: PerspectiveCamera | OrthographicCamera,
    to: PerspectiveCamera | OrthographicCamera,
    target: Vector3,
    h: number,
) {
    const vh = visibleHeight(from, target, h)
    const dir = from.position.clone().sub(target).normalize()
    if (to instanceof PerspectiveCamera) {
        const dist = vh / (2 * Math.tan((to.fov * Math.PI) / 360))
        to.position.copy(target).addScaledVector(dir, dist)
    } else {
        // La orto no deforma por distancia: se la deja lejos para que el
        // plano cercano nunca corte la pieza.
        const dist = Math.max(from.position.distanceTo(target), 500)
        to.position.copy(target).addScaledVector(dir, dist)
        to.zoom = h / Math.max(vh, 1e-3)
    }
    to.lookAt(target)
    to.updateProjectionMatrix()
}

// fitView mira el centro de la caja desde `dir` a la distancia justa para
// que sus 8 esquinas entren en el panel, con margen. Se proyectan las
// esquinas y no la esfera envolvente: la esfera sobra mucho en piezas planas
// y deja la pieza chica, o corta un panel angosto (vista dividida).
export function fitView(
    cam: PerspectiveCamera | OrthographicCamera,
    box: Box3,
    view: ViewName | null,
    w: number,
    h: number,
    target: Vector3,
) {
    const dir = view ? new Vector3(...DIRS[view]).normalize() : cam.position.clone().sub(target).normalize()
    if (dir.lengthSq() === 0) dir.set(...DIRS.iso).normalize()
    const center = box.getCenter(new Vector3())
    target.copy(center)
    // Base de la cámara para esa dirección (misma que daría lookAt).
    const basis = new Matrix4().lookAt(dir, new Vector3(), cam.up)
    const right = new Vector3().setFromMatrixColumn(basis, 0)
    const up = new Vector3().setFromMatrixColumn(basis, 1)
    const aspect = w / Math.max(h, 1)
    let need = 0
    let maxX = 1e-3
    let maxY = 1e-3
    const tanV = cam instanceof PerspectiveCamera ? Math.tan((cam.fov * Math.PI) / 360) : 1
    const tanH = tanV * aspect
    const p = new Vector3()
    for (let i = 0; i < 8; i++) {
        p.set(i & 1 ? box.max.x : box.min.x, i & 2 ? box.max.y : box.min.y, i & 4 ? box.max.z : box.min.z).sub(center)
        const x = Math.abs(p.dot(right))
        const y = Math.abs(p.dot(up))
        // z > 0 = la esquina está más cerca de la cámara que el centro.
        const z = p.dot(dir)
        maxX = Math.max(maxX, x)
        maxY = Math.max(maxY, y)
        need = Math.max(need, x / tanH + z, y / tanV + z)
    }
    if (cam instanceof PerspectiveCamera) {
        cam.position.copy(center).addScaledVector(dir, Math.max(need * FIT_MARGIN, 1))
    } else {
        cam.position.copy(center).addScaledVector(dir, Math.max(box.getSize(new Vector3()).length() * 2, 500))
        cam.zoom = Math.min(w / (2 * maxX), h / (2 * maxY)) / FIT_MARGIN
    }
    cam.lookAt(target)
    cam.updateProjectionMatrix()
}

// orbit gira la cámara alrededor del objetivo: azimut sobre Z, polar desde +Z.
export function orbit(cam: Camera, target: Vector3, dAzimuth: number, dPolar: number) {
    const off = cam.position.clone().sub(target)
    const r = off.length()
    let theta = Math.atan2(off.y, off.x)
    let phi = Math.acos(Math.min(1, Math.max(-1, off.z / Math.max(r, 1e-9))))
    theta += dAzimuth
    phi = Math.min(Math.PI - 1e-3, Math.max(1e-3, phi + dPolar))
    off.set(r * Math.sin(phi) * Math.cos(theta), r * Math.sin(phi) * Math.sin(theta), r * Math.cos(phi))
    cam.position.copy(target).add(off)
    cam.lookAt(target)
}

// pan mueve cámara y objetivo en el plano de la pantalla; `fx`/`fy` son
// fracciones del alto visible.
export function pan(cam: PerspectiveCamera | OrthographicCamera, target: Vector3, fx: number, fy: number, h: number) {
    const vh = visibleHeight(cam, target, h)
    cam.updateMatrix()
    const right = new Vector3().setFromMatrixColumn(cam.matrix, 0)
    const up = new Vector3().setFromMatrixColumn(cam.matrix, 1)
    const delta = right.multiplyScalar(fx * vh).add(up.multiplyScalar(fy * vh))
    cam.position.add(delta)
    target.add(delta)
}

// zoom acerca (factor < 1) o aleja (factor > 1).
export function zoom(cam: PerspectiveCamera | OrthographicCamera, target: Vector3, factor: number) {
    if (cam instanceof PerspectiveCamera) {
        const off = cam.position.clone().sub(target).multiplyScalar(factor)
        if (off.length() < 1) return
        cam.position.copy(target).add(off)
    } else {
        cam.zoom = Math.min(500, Math.max(0.01, cam.zoom / factor))
        cam.updateProjectionMatrix()
    }
}
