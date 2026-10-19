import {Box3, BufferGeometry, Euler, Matrix4, Plane, Quaternion, Vector3} from 'three'

// Matemática del editor de STL (S13). Es PURA —solo three, sin React ni Wails— para
// poder probarla con Node (`cd frontend && pnpm test`, scripts/mesh-edit.test.mjs).
//
// # Convención de transformaciones (contrato con el núcleo Go, backend/meshedit)
//
// OpenSCAD aplica `translate(t) rotate([rx,ry,rz]) scale(s) mirror(m) objeto`: primero
// se espeja, luego se escala, luego se rota y al final se mueve. En three.js:
//
//     position = t
//     rotation = Euler(rx°, ry°, rz° en radianes, orden 'ZYX')
//     scale    = s, con el signo cambiado en cada eje espejado
//
// `rotate([x,y,z])` gira X, luego Y, luego Z sobre ejes FIJOS: R = Rz·Ry·Rx, que es el
// orden de Euler 'ZYX' de three (el 'XYZ' por defecto da OTRA pose en cuanto dos ejes
// son distintos de cero). Los ángulos viajan en grados; los radianes quedan aquí.
// Coordenadas del mundo = las de OpenSCAD (Z arriba, como el visor).

export type Vec3 = [number, number, number]
export type Bool3 = [boolean, boolean, boolean]

/** La pose de un objeto, tal como viaja a Go (`meshedit.Transform`). */
export type Tf = {translate: Vec3; rotate: Vec3; scale: Vec3; mirror: Bool3}

export const DEG = Math.PI / 180

export function identityTf(): Tf {
    return {translate: [0, 0, 0], rotate: [0, 0, 0], scale: [1, 1, 1], mirror: [false, false, false]}
}

export function cloneTf(t: Tf): Tf {
    return {translate: [...t.translate], rotate: [...t.rotate], scale: [...t.scale], mirror: [...t.mirror]}
}

/** Una pose que llega de Go (campos opcionales, escala 0 = 1) a una pose completa. */
export function normTf(t: Partial<{translate: number[]; rotate: number[]; scale: number[]; mirror: boolean[]}> | undefined | null): Tf {
    const v3 = (a: number[] | undefined, d: number): Vec3 => [a?.[0] ?? d, a?.[1] ?? d, a?.[2] ?? d]
    const s = v3(t?.scale, 1)
    return {
        translate: v3(t?.translate, 0),
        rotate: v3(t?.rotate, 0),
        scale: [s[0] || 1, s[1] || 1, s[2] || 1],
        mirror: [!!t?.mirror?.[0], !!t?.mirror?.[1], !!t?.mirror?.[2]],
    }
}

export function sameTf(a: Tf, b: Tf): boolean {
    return a.translate.every((v, i) => v === b.translate[i]) && a.rotate.every((v, i) => v === b.rotate[i]) && a.scale.every((v, i) => v === b.scale[i]) && a.mirror.every((v, i) => v === b.mirror[i])
}

export const rotationEuler = (r: Vec3): Euler => new Euler(r[0] * DEG, r[1] * DEG, r[2] * DEG, 'ZYX')
export const rotationQuaternion = (r: Vec3): Quaternion => new Quaternion().setFromEuler(rotationEuler(r))

/** La escala de three: la del contrato con el signo del espejo en cada eje. */
export const signedScale = (t: Tf): Vec3 => [t.scale[0] * (t.mirror[0] ? -1 : 1), t.scale[1] * (t.mirror[1] ? -1 : 1), t.scale[2] * (t.mirror[2] ? -1 : 1)]

/** La matriz de la pose: T · R(ZYX) · diag(escala con espejo). */
export function tfMatrix(t: Tf): Matrix4 {
    return new Matrix4().compose(new Vector3(...t.translate), rotationQuaternion(t.rotate), new Vector3(...signedScale(t)))
}

/**
 * La misma cuenta que hace OpenSCAD, escrita aparte y sin three: espejo, escala, rotación
 * X → Y → Z y traslación. Es contra lo que se prueba la matriz de arriba (y espeja
 * `Transform.Point` de backend/meshedit/scene.go).
 */
export function openscadPoint(t: Tf, p: Vec3): Vec3 {
    let [x, y, z] = p
    if (t.mirror[0]) x = -x
    if (t.mirror[1]) y = -y
    if (t.mirror[2]) z = -z
    x *= t.scale[0] || 1
    y *= t.scale[1] || 1
    z *= t.scale[2] || 1
    const [rx, ry, rz] = t.rotate.map((d) => d * DEG)
    ;[y, z] = [y * Math.cos(rx) - z * Math.sin(rx), y * Math.sin(rx) + z * Math.cos(rx)]
    ;[x, z] = [x * Math.cos(ry) + z * Math.sin(ry), -x * Math.sin(ry) + z * Math.cos(ry)]
    ;[x, y] = [x * Math.cos(rz) - y * Math.sin(rz), x * Math.sin(rz) + y * Math.cos(rz)]
    return [x + t.translate[0], y + t.translate[1], z + t.translate[2]]
}

/** Redondea (y quita el −0): lo que se guarda y se manda a Go no arrastra ruido de coma flotante. */
export function cleanNum(v: number, digits = 4): number {
    const f = 10 ** digits
    return Math.round(v * f) / f + 0
}

export const cleanVec = (v: Vec3, digits = 4): Vec3 => [cleanNum(v[0], digits), cleanNum(v[1], digits), cleanNum(v[2], digits)]

export function cleanTf(t: Tf): Tf {
    return {translate: cleanVec(t.translate), rotate: cleanVec(t.rotate), scale: cleanVec(t.scale, 5), mirror: [...t.mirror]}
}

/** Los ángulos `rotate([x,y,z])` (grados) de una rotación cualquiera. */
export function quaternionToRotate(q: Quaternion): Vec3 {
    const e = new Euler().setFromQuaternion(q, 'ZYX')
    return [e.x / DEG, e.y / DEG, e.z / DEG]
}

/**
 * La pose que dejó un gizmo en un objeto de three: posición, rotación y escala. El signo de
 * la escala no se lee (el espejo es una opción aparte): se queda con el valor absoluto.
 */
export function tfFromObject(o: {position: Vector3; quaternion: Quaternion; scale: Vector3}, mirror: Bool3): Tf {
    const min = 1e-3
    return cleanTf({
        translate: [o.position.x, o.position.y, o.position.z],
        rotate: quaternionToRotate(o.quaternion),
        scale: [Math.max(min, Math.abs(o.scale.x)), Math.max(min, Math.abs(o.scale.y)), Math.max(min, Math.abs(o.scale.z))],
        mirror: [...mirror],
    })
}

/**
 * Gira la pose con la rotación del MUNDO `q` alrededor de `pivot`: T' = pivot + q·(T − pivot) y
 * R' = q·R. Es lo que hace «apoyar sobre una cara» (el modelo gira sobre sí mismo, no
 * alrededor del origen de la placa).
 */
export function rotateAbout(t: Tf, q: Quaternion, pivot: Vec3): Tf {
    const p = new Vector3(...pivot)
    const pos = new Vector3(...t.translate).sub(p).applyQuaternion(q).add(p)
    const rot = q.clone().multiply(rotationQuaternion(t.rotate))
    return cleanTf({...t, translate: [pos.x, pos.y, pos.z], rotate: quaternionToRotate(rot)})
}

/** La rotación que lleva la normal `n` hacia −Z (la cara queda contra la placa). */
export function faceDownRotation(n: Vector3): Quaternion {
    return new Quaternion().setFromUnitVectors(n.clone().normalize(), new Vector3(0, 0, -1))
}

/** La caja exacta de una malla bajo una matriz: recorre los vértices (sin crear un objeto por cada uno). */
export function boundsOf(geometry: BufferGeometry, m: Matrix4): Box3 {
    const box = new Box3()
    const pos = geometry.getAttribute('position')
    if (!pos || pos.count === 0) return box
    const a = pos.array as ArrayLike<number>
    const e = m.elements
    let x0 = Infinity,
        y0 = Infinity,
        z0 = Infinity,
        x1 = -Infinity,
        y1 = -Infinity,
        z1 = -Infinity
    for (let i = 0; i < a.length; i += 3) {
        const x = a[i],
            y = a[i + 1],
            z = a[i + 2]
        const wx = e[0] * x + e[4] * y + e[8] * z + e[12]
        const wy = e[1] * x + e[5] * y + e[9] * z + e[13]
        const wz = e[2] * x + e[6] * y + e[10] * z + e[14]
        if (wx < x0) x0 = wx
        if (wx > x1) x1 = wx
        if (wy < y0) y0 = wy
        if (wy > y1) y1 = wy
        if (wz < z0) z0 = wz
        if (wz > z1) z1 = wz
    }
    box.min.set(x0, y0, z0)
    box.max.set(x1, y1, z1)
    return box
}

/** La caja de las 8 esquinas de `box` bajo `m`: exacta si no hay rotación, una cota si la hay (primitivas, mallas enormes). */
export function cornersBounds(box: Box3, m: Matrix4): Box3 {
    const out = new Box3()
    const v = new Vector3()
    for (let i = 0; i < 8; i++) {
        v.set(i & 1 ? box.max.x : box.min.x, i & 2 ? box.max.y : box.min.y, i & 4 ? box.max.z : box.min.z).applyMatrix4(m)
        out.expandByPoint(v)
    }
    return out
}

export const boxSize = (b: Box3): Vec3 => (b.isEmpty() ? [0, 0, 0] : [b.max.x - b.min.x, b.max.y - b.min.y, b.max.z - b.min.z])
export const boxCenter = (b: Box3): Vec3 => (b.isEmpty() ? [0, 0, 0] : [(b.min.x + b.max.x) / 2, (b.min.y + b.max.y) / 2, (b.min.z + b.max.z) / 2])

// ── Operaciones del modelo: cada una devuelve la pose nueva ────────────────────

/** Centra el modelo en la placa (X e Y); la altura no cambia. */
export function centerOnBed(t: Tf, g: BufferGeometry): Tf {
    const c = boxCenter(boundsOf(g, tfMatrix(t)))
    return cleanTf({...t, translate: [t.translate[0] - c[0], t.translate[1] - c[1], t.translate[2]]})
}

/** Apoya el modelo en la placa: su punto más bajo queda en Z = 0. */
export function restOnBed(t: Tf, g: BufferGeometry): Tf {
    const b = boundsOf(g, tfMatrix(t))
    if (b.isEmpty()) return t
    return cleanTf({...t, translate: [t.translate[0], t.translate[1], t.translate[2] - b.min.z]})
}

/**
 * Espeja el modelo en el eje `axis` de SU sistema (el del archivo, que es donde OpenSCAD aplica
 * `mirror`) y lo devuelve al lugar: el centro de su caja no se mueve. Sin eso, el espejo
 * respecto del origen del STL lo mandaba a otro sitio.
 */
export function mirrorKeepingCenter(t: Tf, axis: 0 | 1 | 2, g: BufferGeometry): Tf {
    const before = boxCenter(boundsOf(g, tfMatrix(t)))
    const mirror: Bool3 = [...t.mirror]
    mirror[axis] = !mirror[axis]
    const flipped: Tf = {...t, mirror}
    const after = boxCenter(boundsOf(g, tfMatrix(flipped)))
    return cleanTf({...flipped, translate: [t.translate[0] + before[0] - after[0], t.translate[1] + before[1] - after[1], t.translate[2] + before[2] - after[2]]})
}

/**
 * «Apoyar sobre una cara»: gira el modelo (alrededor del centro de su caja) hasta que la normal
 * `worldNormal` mire hacia −Z y lo apoya en la placa. El centro en X e Y se conserva.
 */
export function layFaceDown(t: Tf, g: BufferGeometry, worldNormal: Vector3): Tf {
    const c = boxCenter(boundsOf(g, tfMatrix(t)))
    const turned = rotateAbout(t, faceDownRotation(worldNormal), c)
    return restOnBed(turned, g)
}

// ── Plano de corte ────────────────────────────────────────────────────────────

const Z_AXIS = new Vector3(0, 0, 1)

/** La orientación de un plano cuya normal es `n` (el plano mira hacia +Z de su propio sistema). */
export function planeQuaternion(n: Vec3): Quaternion {
    return new Quaternion().setFromUnitVectors(Z_AXIS, new Vector3(...n).normalize())
}

export function normalOf(q: Quaternion): Vec3 {
    const n = Z_AXIS.clone().applyQuaternion(q).normalize()
    return cleanVec([n.x, n.y, n.z], 5)
}

/**
 * El plano de recorte de three para conservar un lado del corte. three descarta lo que queda del
 * lado NEGATIVO del plano: «arriba» (el lado hacia el que apunta la normal) conserva el positivo.
 */
export function clipPlane(point: Vec3, normal: Vec3, keep: 'above' | 'below'): Plane {
    const n = new Vector3(...normal).normalize()
    const p = new Vector3(...point)
    if (keep === 'below') n.negate()
    return new Plane(n, -n.dot(p))
}

// ── Primitivas ────────────────────────────────────────────────────────────────

export type Shape = 'cube' | 'cylinder' | 'sphere' | 'text'

/** Ancho aproximado del texto de OpenSCAD (la fuente por defecto, ~0,6 × el tamaño por carácter). */
export function textWidth(text: string, size: number): number {
    return Math.max(size * 0.6, [...text].length * size * 0.6)
}

/** La caja local de una primitiva centrada en su origen (el editor siempre manda `center: true`). */
export function shapeBox(shape: Shape, size: Vec3, text: string): Box3 {
    switch (shape) {
        case 'cube':
            return new Box3(new Vector3(-size[0] / 2, -size[1] / 2, -size[2] / 2), new Vector3(size[0] / 2, size[1] / 2, size[2] / 2))
        case 'cylinder':
            return new Box3(new Vector3(-size[0] / 2, -size[0] / 2, -size[2] / 2), new Vector3(size[0] / 2, size[0] / 2, size[2] / 2))
        case 'sphere':
            return new Box3(new Vector3(-size[0] / 2, -size[0] / 2, -size[0] / 2), new Vector3(size[0] / 2, size[0] / 2, size[0] / 2))
        case 'text': {
            const w = textWidth(text, size[0])
            return new Box3(new Vector3(-w / 2, -size[0] / 2, -size[2] / 2), new Vector3(w / 2, size[0] / 2, size[2] / 2))
        }
    }
}

/** El tamaño que se le da a una primitiva nueva según el modelo (un décimo de lo menor, entre 4 y 20 mm). */
export function defaultPrimitiveSize(shape: Shape, modelSize: Vec3, mode: 'union' | 'hole'): Vec3 {
    const base = Math.min(20, Math.max(4, Math.round(Math.min(...modelSize.filter((v) => v > 0)) / 10) || 10))
    const tall = Math.max(modelSize[2], 1) + 6
    switch (shape) {
        case 'cube':
            return [base, base, base]
        case 'cylinder':
            // Un agujero pasante: atraviesa todo el alto. Una unión es un cilindro cortito.
            return mode === 'hole' ? [Math.max(2, Math.round(base / 2)), Math.max(2, Math.round(base / 2)), tall] : [base, base, base]
        case 'sphere':
            return [base, base, base]
        case 'text':
            return [base, base, 1]
    }
}
