#!/usr/bin/env node
// Pruebas de la matemática del editor de STL (S13, 13.2): la convención de transformaciones del contrato con Go
// (`translate · rotate([x,y,z]) · scale · mirror` de OpenSCAD = position + Euler 'ZYX' + scale con el signo del espejo en
// three) y las operaciones que se calculan en el frontend (centrar, apoyar, espejar, apoyar sobre una cara, plano de corte).
//
//   cd frontend && pnpm test
import assert from 'node:assert/strict'
import {BoxGeometry, Euler, Matrix4, Quaternion, Vector3} from 'three'
import {
    boundsOf,
    boxCenter,
    boxSize,
    centerOnBed,
    clipPlane,
    defaultPrimitiveSize,
    faceDownRotation,
    identityTf,
    layFaceDown,
    mirrorKeepingCenter,
    normTf,
    normalOf,
    openscadPoint,
    planeQuaternion,
    quaternionToRotate,
    restOnBed,
    rotateAbout,
    rotationEuler,
    shapeBox,
    signedScale,
    tfFromObject,
    tfMatrix,
} from '../src/lib/meshEditMath.ts'

const near = (a, b, msg, eps = 1e-6) => assert.ok(Math.abs(a - b) <= eps, `${msg ?? ''}: ${a} != ${b}`)
const nearVec = (a, b, msg, eps = 1e-6) => a.forEach((v, i) => near(v, b[i], `${msg ?? ''}[${i}]`, eps))
const apply = (m, p) => {
    const v = new Vector3(...p).applyMatrix4(m)
    return [v.x, v.y, v.z]
}

// ── La convención: la matriz de three es la cuenta de OpenSCAD ──
// Mismo caso que TestRotationIsEulerZYX de backend/meshedit: una caja 10×20×30 con rotate([90,0,90]) mide 30×10×20.
{
    const box = new BoxGeometry(10, 20, 30)
    const t = {...identityTf(), rotate: [90, 0, 90]}
    const b = boundsOf(box, tfMatrix(t))
    nearVec(boxSize(b), [30, 10, 20], 'rotate([90,0,90])')
    // Con el orden por defecto de three ('XYZ') daría OTRA pose: el test sirve para algo.
    const wrong = new Matrix4().makeRotationFromEuler(new Euler((90 * Math.PI) / 180, 0, (90 * Math.PI) / 180, 'XYZ'))
    const bw = boundsOf(box, wrong)
    assert.notDeepEqual(boxSize(bw).map((v) => Math.round(v)), [30, 10, 20], "'XYZ' no es la convención")
}

// La fórmula de OpenSCAD escrita a mano (espejo → escala → rotación X, Y, Z → traslación) contra la matriz de three.
const poses = [
    {translate: [0, 0, 0], rotate: [90, 0, 90], scale: [1, 1, 1], mirror: [false, false, false]},
    {translate: [10, -20, 5], rotate: [30, 45, 60], scale: [1, 1, 1], mirror: [false, false, false]},
    {translate: [-3, 4, 12], rotate: [-120, 20, 75], scale: [2, 0.5, 1.5], mirror: [true, false, true]},
    {translate: [100, 0, -7], rotate: [0, 90, 0], scale: [1, 3, 1], mirror: [false, true, false]},
    {translate: [0, 0, 0], rotate: [10, 20, 30], scale: [0.25, 0.25, 0.25], mirror: [true, true, true]},
]
const points = [[1, 0, 0], [0, 1, 0], [0, 0, 1], [3, -4, 5], [-7, 2, 9.5]]
for (const t of poses) {
    const m = tfMatrix(t)
    for (const p of points) nearVec(apply(m, p), openscadPoint(t, p), `pose ${JSON.stringify(t)} punto ${p}`)
}
// Escala 0 = 1 (como Go).
nearVec(openscadPoint({...identityTf(), scale: [0, 0, 0]}, [1, 2, 3]), [1, 2, 3], 'escala 0 = 1')
assert.deepEqual(normTf({scale: [0, 2, 0]}).scale, [1, 2, 1])
assert.deepEqual(signedScale({...identityTf(), scale: [2, 3, 4], mirror: [true, false, true]}), [-2, 3, -4])
assert.deepEqual(normTf(undefined).translate, [0, 0, 0])

// Un valor conocido a mano: rotate([0,0,90]) lleva (1,0,0) a (0,1,0); rotate([90,0,0]) lleva (0,1,0) a (0,0,1).
nearVec(openscadPoint({...identityTf(), rotate: [0, 0, 90]}, [1, 0, 0]), [0, 1, 0], 'rotate Z 90')
nearVec(openscadPoint({...identityTf(), rotate: [90, 0, 0]}, [0, 1, 0]), [0, 0, 1], 'rotate X 90')
// El espejo va ANTES que la rotación: mirror([1,0,0]) y luego rotate([0,0,90]) lleva (1,0,0) a (0,-1,0).
nearVec(openscadPoint({...identityTf(), rotate: [0, 0, 90], mirror: [true, false, false]}, [1, 0, 0]), [0, -1, 0], 'espejo antes de rotar')

// ── Lo que deja un gizmo vuelve al contrato sin perder la pose ──
for (const t of poses) {
    const o = {position: new Vector3(...t.translate), quaternion: new Quaternion().setFromEuler(rotationEuler(t.rotate)), scale: new Vector3(...signedScale(t))}
    const back = tfFromObject(o, t.mirror)
    for (const p of points) nearVec(apply(tfMatrix(back), p), openscadPoint(t, p), `ida y vuelta de ${JSON.stringify(t.rotate)}`, 1e-3)
}
// quaternionToRotate invierte a rotationEuler (lejos del bloqueo de cardán).
for (const r of [[10, 20, 30], [-170, 45, 120], [90, -30, -90]]) nearVec(quaternionToRotate(new Quaternion().setFromEuler(rotationEuler(r))), r.map((v) => ((v + 540) % 360) - 180), `euler ${r}`, 1e-6)

// ── Operaciones del modelo ──
// Una caja 10×20×30 con una esquina en el origen (como un STL que no está centrado).
const stl = new BoxGeometry(10, 20, 30).translate(5, 10, 15)
{
    const centered = centerOnBed(identityTf(), stl)
    nearVec(boxCenter(boundsOf(stl, tfMatrix(centered))).slice(0, 2), [0, 0], 'centrar en X e Y')
    near(boundsOf(stl, tfMatrix(centered)).min.z, 0, 'centrar no cambia la altura')

    const raised = {...identityTf(), translate: [0, 0, 7]}
    near(boundsOf(stl, tfMatrix(restOnBed(raised, stl))).min.z, 0, 'apoyar en la placa (subido)')
    const sunk = {...identityTf(), translate: [0, 0, -12]}
    near(boundsOf(stl, tfMatrix(restOnBed(sunk, stl))).min.z, 0, 'apoyar en la placa (hundido)')
    // Apoyar un modelo rotado mira el punto más bajo REAL, no el de la caja sin rotar.
    const tilted = {...identityTf(), rotate: [45, 0, 0]}
    near(boundsOf(stl, tfMatrix(restOnBed(tilted, stl))).min.z, 0, 'apoyar un modelo inclinado')
}
{
    // Espejar conserva el centro de la caja (sin eso el STL, que no está centrado, salta de sitio).
    const t = {...identityTf(), translate: [-5, -10, 0]}
    for (const axis of [0, 1, 2]) {
        const m = mirrorKeepingCenter(t, axis, stl)
        assert.equal(m.mirror[axis], true)
        nearVec(boxCenter(boundsOf(stl, tfMatrix(m))), boxCenter(boundsOf(stl, tfMatrix(t))), `espejar el eje ${axis} conserva el centro`)
        nearVec(boxSize(boundsOf(stl, tfMatrix(m))), [10, 20, 30], 'el espejo no cambia las medidas')
        // Y volver a espejar lo devuelve a como estaba.
        const back = mirrorKeepingCenter(m, axis, stl)
        assert.equal(back.mirror[axis], false)
        nearVec(back.translate, t.translate, 'espejar dos veces', 1e-3)
    }
}
{
    // Apoyar sobre una cara: la normal +Y (la cara de 10×30) queda hacia abajo → el eje Y pasa a ser la altura.
    const t = {...identityTf(), translate: [0, 0, 15]}
    const before = boundsOf(stl, tfMatrix(t))
    const laid = layFaceDown(t, stl, new Vector3(0, 1, 0))
    const b = boundsOf(stl, tfMatrix(laid))
    near(b.min.z, 0, 'apoyada: toca la placa')
    nearVec(boxSize(b), [10, 30, 20], 'apoyada sobre la cara +Y: la altura pasa a ser 20')
    nearVec(boxCenter(b).slice(0, 2), boxCenter(before).slice(0, 2), 'apoyada: conserva el centro en X e Y', 1e-3)
    // Con la cara que ya miraba hacia abajo no hay giro.
    const down = layFaceDown(t, stl, new Vector3(0, 0, -1))
    nearVec(down.rotate, [0, 0, 0], 'cara ya hacia abajo: sin giro', 1e-6)
    // Una normal oblicua: tras el giro, esa normal mira exactamente a −Z.
    const n = new Vector3(1, 1, 1).normalize()
    const lean = layFaceDown({...identityTf(), rotate: [10, 20, 30]}, stl, n)
    const worldN = n.clone().applyQuaternion(faceDownRotation(n))
    nearVec([worldN.x, worldN.y, worldN.z], [0, 0, -1], 'faceDownRotation lleva la normal a −Z')
    near(boundsOf(stl, tfMatrix(lean)).min.z, 0, 'apoyada con normal oblicua', 1e-4) // la pose se guarda con 4 decimales
}
{
    // rotateAbout: girar alrededor de un pivote no mueve ese punto del modelo.
    const t = {...identityTf(), translate: [3, 4, 5], rotate: [0, 0, 30]}
    const pivot = [3, 4, 5]
    const q = new Quaternion().setFromAxisAngle(new Vector3(1, 0, 0), Math.PI / 2)
    const r = rotateAbout(t, q, pivot)
    nearVec(r.translate, pivot, 'el origen del modelo es el pivote: no se mueve')
    nearVec(apply(tfMatrix(r), [1, 0, 0]), apply(new Matrix4().makeRotationFromQuaternion(q), apply(tfMatrix(t), [1, 0, 0]).map((v, i) => v - pivot[i])).map((v, i) => v + pivot[i]), 'rotateAbout = girar el mundo', 1e-6)
}

// ── Plano de corte ──
{
    const keepUp = clipPlane([0, 0, 5], [0, 0, 1], 'above')
    assert.ok(keepUp.distanceToPoint(new Vector3(1, 1, 6)) > 0, 'arriba conserva el lado de la normal')
    assert.ok(keepUp.distanceToPoint(new Vector3(1, 1, 4)) < 0, 'y recorta el contrario')
    const keepDown = clipPlane([0, 0, 5], [0, 0, 1], 'below')
    assert.ok(keepDown.distanceToPoint(new Vector3(1, 1, 4)) > 0, 'abajo conserva el lado contrario')
    // Un plano oblicuo que pasa por el punto: el punto está en el plano.
    near(clipPlane([2, 3, 4], [1, 2, 3], 'above').distanceToPoint(new Vector3(2, 3, 4)), 0, 'el punto está en el plano')
    // La normal sobrevive al viaje por el cuaternión del gizmo.
    nearVec(normalOf(planeQuaternion([0, 1, 0])), [0, 1, 0], 'normal Y', 1e-5)
    nearVec(normalOf(planeQuaternion([1, 1, 0])), [Math.SQRT1_2, Math.SQRT1_2, 0], 'normal oblicua', 1e-5)
    nearVec(normalOf(planeQuaternion([0, 0, -1])), [0, 0, -1], 'normal −Z', 1e-5)
}

// ── Primitivas ──
{
    nearVec(boxSize(shapeBox('cube', [10, 20, 30], '')), [10, 20, 30], 'cubo')
    nearVec(boxSize(shapeBox('cylinder', [8, 8, 40], '')), [8, 8, 40], 'cilindro: Ø × alto')
    nearVec(boxSize(shapeBox('sphere', [12, 12, 12], '')), [12, 12, 12], 'esfera')
    const t = boxSize(shapeBox('text', [10, 10, 2], 'Hola'))
    assert.ok(t[0] > 10 && t[1] === 10 && t[2] === 2, 'texto: más ancho que alto, con su extrusión')
    // Un agujero pasante nuevo atraviesa todo el alto del modelo.
    const hole = defaultPrimitiveSize('cylinder', [78, 124, 27.6], 'hole')
    assert.ok(hole[2] > 27.6, 'el cilindro-agujero pasa de lado a lado')
    assert.ok(hole[0] >= 2 && hole[0] < 78, 'y es fino')
    const cube = defaultPrimitiveSize('cube', [78, 124, 27.6], 'union')
    assert.ok(cube.every((v) => v >= 4 && v <= 20), 'el cubo nuevo es chico')
}

console.log('mesh-edit.test.mjs: ok')
