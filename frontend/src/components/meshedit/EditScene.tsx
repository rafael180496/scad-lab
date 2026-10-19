import {useEffect, useLayoutEffect, useMemo, useRef, useState, type ComponentRef, type ReactNode, type RefObject} from 'react'
import {useThree} from '@react-three/fiber'
import {TransformControls} from '@react-three/drei'
import {
    BackSide,
    Box3,
    BoxGeometry,
    BufferGeometry,
    CanvasTexture,
    Color,
    CylinderGeometry,
    DoubleSide,
    EdgesGeometry,
    Float32BufferAttribute,
    Matrix3,
    PlaneGeometry,
    Quaternion,
    Raycaster,
    SphereGeometry,
    Vector2,
    Vector3,
    type Group,
    type Intersection,
    type Mesh,
    type Object3D,
    type Plane,
} from 'three'
import type {ThemeColors} from '../../hooks/useThemeColors'
import {
    boundsOf,
    boxSize,
    clipPlane,
    cleanTf,
    cornersBounds,
    DEG,
    normalOf,
    planeQuaternion,
    shapeBox,
    signedScale,
    textWidth,
    tfFromObject,
    tfMatrix,
    type Tf,
    type Vec3,
} from '../../lib/meshEditMath'
import {docOf, select, useSession, type Cut, type Doc, type Part, type Sel, type Tool} from '../../lib/meshEditSession'
import {Bed, Environment, Rig, type BedVolume, type PaneHandle} from '../viewer/ViewPane'
import type {ViewName} from '../viewer/camera'
import {partWorldBox, type EditActions} from './actions'
import {clearLive, liveStore} from './live'

// La escena 3D del editor de STL. Vive dentro del <Canvas> (otra raíz de React: no comparte contexto con la
// interfaz, por eso todo lo que lee sale de los stores de módulo). Dibuja el modelo con su pose, las piezas
// (las uniones sólidas, los agujeros en rojo translúcido), el plano de corte y el gizmo de la selección.
//
// Qué es una vista previa y qué no: el modelo y las uniones se dibujan tal cual; los agujeros son un volumen
// rojo (restarlo de verdad es el trabajo de OpenSCAD al aplicar); el corte recorta lo que se ve con planos de
// recorte de la GPU, sin tocar la malla.

const rot = (t: Tf): [number, number, number, 'ZYX'] => [t.rotate[0] * DEG, t.rotate[1] * DEG, t.rotate[2] * DEG, 'ZYX']

// Una variante es una copia dibujada de un cuerpo: sin corte hay una; con «partir en dos» hay dos, cada una con
// su plano de recorte, su color y (como objetos) su desplazamiento en X para imprimirlas aparte.
type Variant = {plane: Plane | null; color: string | null; dx: number}

function variantsOf(cut: Cut | null, dx: number): Variant[] {
    if (!cut) return [{plane: null, color: null, dx: 0}]
    if (cut.keep === 'above') return [{plane: clipPlane(cut.point, cut.normal, 'above'), color: null, dx: 0}]
    if (cut.keep === 'below') return [{plane: clipPlane(cut.point, cut.normal, 'below'), color: null, dx: 0}]
    const up = clipPlane(cut.point, cut.normal, 'above')
    const down = clipPlane(cut.point, cut.normal, 'below')
    if (cut.split === 'colors') return [{plane: up, color: cut.colors[0] || null, dx: 0}, {plane: down, color: cut.colors[1] || null, dx: 0}]
    return [{plane: up, color: null, dx: 0}, {plane: down.clone().translate(new Vector3(dx, 0, 0)), color: null, dx}]
}

/** El color del lado oculto de un cuerpo cortado: el mismo, más oscuro (se ve por dentro, como una tapa). */
function darker(hex: string): string {
    return '#' + new Color(hex).multiplyScalar(0.55).getHexString()
}

// ── Un objeto seleccionable ────────────────────────────────────────────────────

function Selectable({
    id,
    selected,
    onObject,
    position,
    rotation,
    quaternion,
    scale,
    children,
}: {
    id: string
    selected: boolean
    onObject: (o: Object3D | null) => void
    position: Vec3
    rotation?: [number, number, number, 'ZYX']
    quaternion?: [number, number, number, number]
    scale?: Vec3
    children: ReactNode
}) {
    const ref = useRef<Group>(null)
    useLayoutEffect(() => {
        if (!selected) return
        onObject(ref.current)
        return () => onObject(null)
    }, [selected, onObject])
    // Solo las propiedades que hay: pasar `undefined` a R3F las trata como «volver al valor por defecto».
    const pose = quaternion ? {quaternion} : {rotation, scale}
    return (
        <group ref={ref} userData={{editId: id}} position={position} {...pose}>
            {children}
        </group>
    )
}

/** Un cuerpo y sus copias del corte: el principal es el que agarra el gizmo; las copias lo siguen al soltar. */
// `pivot` es un punto del sistema del propio objeto (la malla cruda) que hace de origen del gizmo: el centro de la caja
// del modelo. Un STL trae su origen donde lo dejó su autor (una esquina, o lejos de la pieza), y girar o escalar sobre
// ESE punto manda la pieza a pasear. La pose del contrato sigue siendo la de OpenSCAD (sobre el origen del archivo): el
// grupo que agarra el gizmo se coloca en `T + R·S·pivot` y la malla se corre `−pivot` dentro de él.
function pivotPosition(tf: Tf, pivot: Vec3 | undefined): Vec3 {
    if (!pivot) return tf.translate
    const p = new Vector3(...pivot).applyMatrix4(tfMatrix(tf))
    return [p.x, p.y, p.z]
}

/** La pose (sobre el origen del archivo) que deja un gizmo agarrado del pivote. */
function poseFromPivoted(o: {position: Vector3; quaternion: Quaternion; scale: Vector3}, mirror: Tf['mirror'], pivot: Vec3 | undefined): Tf {
    const tf = tfFromObject(o, mirror)
    if (!pivot) return tf
    const off = new Vector3(...pivot).applyMatrix4(tfMatrix({...tf, translate: [0, 0, 0]}))
    return cleanTf({...tf, translate: [tf.translate[0] - off.x, tf.translate[1] - off.y, tf.translate[2] - off.z]})
}

function Bodies({
    id,
    tf,
    pivot,
    selected,
    onObject,
    variants,
    children,
}: {
    id: string
    tf: Tf
    pivot?: Vec3
    selected: boolean
    onObject: (o: Object3D | null) => void
    variants: Variant[]
    children: (v: Variant) => ReactNode
}) {
    const pos = pivotPosition(tf, pivot)
    const inner = (v: Variant): ReactNode => (pivot ? <group position={[-pivot[0], -pivot[1], -pivot[2]]}>{children(v)}</group> : children(v))
    return (
        <>
            <Selectable id={id} selected={selected} onObject={onObject} position={pos} rotation={rot(tf)} scale={signedScale(tf)}>
                {inner(variants[0])}
            </Selectable>
            {variants.slice(1).map((v, i) => (
                <group key={i} position={[v.dx, 0, 0]}>
                    <group userData={{editId: id}} position={pos} rotation={rot(tf)} scale={signedScale(tf)}>
                        {inner(v)}
                    </group>
                </group>
            ))}
        </>
    )
}

// ── El modelo ──────────────────────────────────────────────────────────────────

function ModelMesh({geometry, color, plane, selected, accent}: {geometry: BufferGeometry; color: string; plane: Plane | null; selected: boolean; accent: string}) {
    const clip = plane ? [plane] : undefined
    const box = useMemo(() => {
        const b = geometry.boundingBox ?? new Box3()
        const size = new Vector3()
        b.getSize(size)
        const g = new EdgesGeometry(new BoxGeometry(size.x, size.y, size.z))
        const c = b.getCenter(new Vector3())
        g.translate(c.x, c.y, c.z)
        return g
    }, [geometry])
    useEffect(() => () => box.dispose(), [box])
    return (
        <>
            <mesh geometry={geometry}>
                <meshPhysicalMaterial
                    color={color}
                    roughness={0.5}
                    metalness={0}
                    clearcoat={0.15}
                    clearcoatRoughness={0.35}
                    clippingPlanes={clip}
                    polygonOffset
                    polygonOffsetFactor={2}
                    polygonOffsetUnits={4}
                />
            </mesh>
            {plane ? (
                // El interior que deja el corte: las caras de atrás, más oscuras, hacen de tapa.
                <mesh geometry={geometry}>
                    <meshBasicMaterial color={darker(color)} side={BackSide} clippingPlanes={clip} />
                </mesh>
            ) : null}
            {selected ? (
                <lineSegments geometry={box}>
                    <lineBasicMaterial color={accent} toneMapped={false} transparent opacity={0.8} />
                </lineSegments>
            ) : null}
        </>
    )
}

// ── Una pieza (unión o agujero) ────────────────────────────────────────────────

function partGeometry(p: Part): BufferGeometry {
    switch (p.shape) {
        case 'cube':
            return new BoxGeometry(p.size[0], p.size[1], p.size[2])
        case 'cylinder': {
            const g = new CylinderGeometry(p.size[0] / 2, p.size[0] / 2, p.size[2], 64)
            return g.rotateX(Math.PI / 2) // el de three va a lo largo de Y; el de OpenSCAD, de Z
        }
        case 'sphere':
            return new SphereGeometry(p.size[0] / 2, 48, 32).rotateX(Math.PI / 2)
        case 'text': {
            const b = shapeBox('text', p.size, p.text)
            const s = boxSize(b)
            return new BoxGeometry(s[0], s[1], s[2])
        }
    }
}

/** El texto de la primitiva dibujado sobre su cara de arriba: una aproximación (la forma exacta la da OpenSCAD al aplicar). */
function TextDecal({text, w, h, z, color}: {text: string; w: number; h: number; z: number; color: string}) {
    const tex = useMemo(() => {
        const cv = document.createElement('canvas')
        cv.width = 512
        cv.height = Math.min(512, Math.max(48, Math.round((512 * h) / Math.max(w, 1e-6))))
        const c = cv.getContext('2d')
        if (c) {
            c.clearRect(0, 0, cv.width, cv.height)
            c.fillStyle = color
            c.textAlign = 'center'
            c.textBaseline = 'middle'
            let px = cv.height * 0.8
            c.font = `bold ${px}px sans-serif`
            const wide = c.measureText(text).width
            if (wide > cv.width * 0.96) {
                px *= (cv.width * 0.96) / wide
                c.font = `bold ${px}px sans-serif`
            }
            c.fillText(text, cv.width / 2, cv.height / 2)
        }
        const t = new CanvasTexture(cv)
        t.anisotropy = 4
        return t
    }, [text, w, h, color])
    useEffect(() => () => tex.dispose(), [tex])
    return (
        <mesh position={[0, 0, z]}>
            <planeGeometry args={[w, h]} />
            <meshBasicMaterial map={tex} transparent toneMapped={false} depthWrite={false} />
        </mesh>
    )
}

function PartMesh({part, color, plane, selected, theme}: {part: Part; color: string; plane: Plane | null; selected: boolean; theme: ThemeColors}) {
    const geometry = useMemo(() => partGeometry(part), [part.shape, part.size[0], part.size[1], part.size[2], part.text]) // eslint-disable-line react-hooks/exhaustive-deps
    const edges = useMemo(() => new EdgesGeometry(geometry, 20), [geometry])
    useEffect(
        () => () => {
            geometry.dispose()
            edges.dispose()
        },
        [geometry, edges],
    )
    const hole = part.mode === 'hole'
    const clip = plane && !hole ? [plane] : undefined
    const tw = part.shape === 'text' ? textWidth(part.text, part.size[0]) : 0
    return (
        <>
            <mesh geometry={geometry}>
                {hole ? (
                    <meshStandardMaterial color={theme.danger} transparent opacity={0.42} depthWrite={false} roughness={0.6} />
                ) : (
                    <meshPhysicalMaterial
                        color={color}
                        roughness={0.5}
                        metalness={0}
                        clippingPlanes={clip}
                        polygonOffset
                        polygonOffsetFactor={2}
                        polygonOffsetUnits={4}
                    />
                )}
            </mesh>
            {part.shape === 'text' ? <TextDecal text={part.text} w={tw} h={part.size[0]} z={part.size[2] / 2 + 0.02} color={hole ? theme.danger : theme.fg} /> : null}
            <lineSegments geometry={edges}>
                <lineBasicMaterial color={selected ? theme.accent : hole ? theme.danger : theme.lineStrong} toneMapped={false} transparent opacity={selected || hole ? 0.9 : 0.5} />
            </lineSegments>
        </>
    )
}

// ── El plano de corte ──────────────────────────────────────────────────────────

function CutPlaneObject({cut, extent, selected, onObject, theme}: {cut: Cut; extent: number; selected: boolean; onObject: (o: Object3D | null) => void; theme: ThemeColors}) {
    const q = planeQuaternion(cut.normal)
    const edges = useMemo(() => new EdgesGeometry(new PlaneGeometry(extent, extent)), [extent])
    useEffect(() => () => edges.dispose(), [edges])
    return (
        <Selectable id="cut" selected={selected} onObject={onObject} position={cut.point} quaternion={[q.x, q.y, q.z, q.w]}>
            <mesh>
                <planeGeometry args={[extent, extent]} />
                <meshBasicMaterial color={theme.accent} transparent opacity={selected ? 0.16 : 0.1} side={DoubleSide} depthWrite={false} toneMapped={false} />
            </mesh>
            <lineSegments geometry={edges}>
                <lineBasicMaterial color={theme.accent} toneMapped={false} />
            </lineSegments>
            {/* La flecha apunta al lado que «arriba» conserva. */}
            <mesh position={[0, 0, extent * 0.08]} rotation={[Math.PI / 2, 0, 0]}>
                <cylinderGeometry args={[extent * 0.004, extent * 0.004, extent * 0.16, 8]} />
                <meshBasicMaterial color={theme.accent} toneMapped={false} />
            </mesh>
            <mesh position={[0, 0, extent * 0.18]} rotation={[Math.PI / 2, 0, 0]}>
                <coneGeometry args={[extent * 0.016, extent * 0.04, 16]} />
                <meshBasicMaterial color={theme.accent} toneMapped={false} />
            </mesh>
        </Selectable>
    )
}

// ── El gizmo de la selección ───────────────────────────────────────────────────

type GizmoMode = 'translate' | 'rotate' | 'scale'
type GizmoHandle = ComponentRef<typeof TransformControls>

function GizmoLayer({
    target,
    sel,
    mode,
    doc,
    geometry,
    pivot,
    snap,
    actions,
    gizmoRef,
}: {
    target: Object3D
    sel: Exclude<Sel, null>
    mode: GizmoMode
    doc: Doc
    geometry: BufferGeometry | null
    /** El pivote del modelo (centro de su caja); las piezas no lo necesitan. */
    pivot?: Vec3
    snap: {on: boolean; move: number; rotate: number; scale: number}
    actions: EditActions
    gizmoRef: RefObject<GizmoHandle | null>
}) {
    const raf = useRef(0)
    useEffect(() => () => cancelAnimationFrame(raf.current), [])

    const poseOf = () => poseFromPivoted(target, sel.kind === 'model' ? doc.model.mirror : [false, false, false], sel.kind === 'model' ? pivot : undefined)

    // Mientras se arrastra, el panel y las medidas siguen al gizmo (sin ser todavía una operación de la pila).
    const publish = () => {
        raf.current = 0
        if (sel.kind === 'cut') {
            liveStore.set({tf: null, cutPoint: [target.position.x, target.position.y, target.position.z], cutNormal: normalOf(target.quaternion), box: null})
            return
        }
        const tf = poseOf()
        const m = tfMatrix(tf)
        let box: Box3
        if (sel.kind === 'model') {
            const big = (geometry?.attributes.position.count ?? 0) > 900_000
            box = !geometry ? new Box3() : big && geometry.boundingBox ? cornersBounds(geometry.boundingBox, m) : boundsOf(geometry, m)
        } else {
            const p = doc.parts.find((x) => x.id === sel.id)
            box = p ? cornersBounds(shapeBox(p.shape, p.size, p.text), m) : new Box3()
        }
        liveStore.set({tf, cutPoint: null, cutNormal: null, box: box.isEmpty() ? null : {min: [box.min.x, box.min.y, box.min.z], max: [box.max.x, box.max.y, box.max.z]}})
    }
    const onChange = () => {
        if (!raf.current) raf.current = requestAnimationFrame(publish)
    }
    const onUp = () => {
        cancelAnimationFrame(raf.current)
        raf.current = 0
        if (sel.kind === 'cut') {
            actions.setCutPlane([cleanP(target.position.x), cleanP(target.position.y), cleanP(target.position.z)], normalOf(target.quaternion))
        } else {
            actions.applyTf(sel, poseOf(), mode === 'translate' ? 'move' : mode)
        }
        clearLive()
    }

    return (
        <TransformControls
            ref={gizmoRef}
            object={target}
            mode={mode}
            space={mode === 'scale' ? 'local' : 'world'}
            size={0.85}
            translationSnap={snap.on ? snap.move : null}
            rotationSnap={snap.on ? snap.rotate * DEG : null}
            scaleSnap={snap.on ? snap.scale : null}
            onObjectChange={onChange}
            onMouseUp={onUp}
        />
    )
}

const cleanP = (v: number) => Math.round(v * 1e4) / 1e4 + 0

// ── Clic: seleccionar y elegir una cara ────────────────────────────────────────

const CLICK_SLOP_PX = 4
const HOVER_MAX_TRIANGLES = 250_000

function ownerOf(o: Object3D): string | null {
    for (let n: Object3D | null = o; n; n = n.parent) if (typeof n.userData.editId === 'string') return n.userData.editId
    return null
}

type FaceHit = {tri: [Vector3, Vector3, Vector3]; normal: Vector3; point: Vector3}

function PickLayer({
    ws,
    tool,
    gizmoRef,
    triangles,
    onFace,
    onHover,
}: {
    ws: number
    tool: Tool
    gizmoRef: RefObject<GizmoHandle | null>
    triangles: number
    onFace: (normal: Vector3) => void
    onHover: (h: FaceHit | null) => void
}) {
    const gl = useThree((s) => s.gl)
    const scene = useThree((s) => s.scene)
    const get = useThree((s) => s.get)
    const toolRef = useRef(tool)
    toolRef.current = tool
    const onFaceRef = useRef(onFace)
    onFaceRef.current = onFace
    const onHoverRef = useRef(onHover)
    onHoverRef.current = onHover

    useEffect(() => {
        const el = gl.domElement
        const ray = new Raycaster()
        const ndc = new Vector2()
        let down: {x: number; y: number} | null = null
        let hoverAt = 0

        const cast = (e: PointerEvent): Intersection[] => {
            const r = el.getBoundingClientRect()
            if (r.width === 0 || r.height === 0) return []
            ndc.set(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1)
            ray.setFromCamera(ndc, get().camera)
            const roots = scene.children.filter((c) => typeof c.userData.editId === 'string' && c.visible)
            // Solo mallas (las aristas de resalte también son objetos de la escena y no se eligen).
            return ray.intersectObjects(roots, true).filter((h) => (h.object as Mesh).isMesh)
        }
        const faceOf = (h: Intersection): FaceHit | null => {
            if (!h.face) return null
            const mesh = h.object as Mesh
            mesh.updateWorldMatrix(true, false)
            const pos = mesh.geometry.getAttribute('position')
            const at = (i: number) => new Vector3().fromBufferAttribute(pos, i).applyMatrix4(mesh.matrixWorld)
            const normal = h.face.normal.clone().applyMatrix3(new Matrix3().getNormalMatrix(mesh.matrixWorld)).normalize()
            return {tri: [at(h.face.a), at(h.face.b), at(h.face.c)], normal, point: h.point.clone()}
        }

        const onDown = (e: PointerEvent) => {
            // Un clic sobre una flecha del gizmo es del gizmo, no una selección.
            // (`axis` es privado en los tipos de three-stdlib, pero es lo que dice qué flecha está bajo el puntero.)
            const hovering = (gizmoRef.current as unknown as {axis: string | null} | null)?.axis
            down = e.button === 0 && !hovering ? {x: e.clientX, y: e.clientY} : null
        }
        const onUp = (e: PointerEvent) => {
            const d = down
            down = null
            if (!d || e.button !== 0 || Math.hypot(e.clientX - d.x, e.clientY - d.y) > CLICK_SLOP_PX) return
            const hits = cast(e)
            if (toolRef.current === 'face') {
                const model = hits.find((h) => ownerOf(h.object) === 'model')
                const f = model ? faceOf(model) : null
                if (f) onFaceRef.current(f.normal)
                return
            }
            // El plano de corte se elige solo si no hay otra cosa detrás: es translúcido y enorme.
            const first = hits.find((h) => ownerOf(h.object) !== 'cut') ?? hits[0]
            const id = first ? ownerOf(first.object) : null
            if (id === null) select(ws, null)
            else if (id === 'model') select(ws, {kind: 'model'})
            else if (id === 'cut') select(ws, {kind: 'cut'})
            else select(ws, {kind: 'part', id})
        }
        const onMove = (e: PointerEvent) => {
            if (toolRef.current !== 'face' || triangles > HOVER_MAX_TRIANGLES) return
            const now = performance.now()
            if (now - hoverAt < 60) return
            hoverAt = now
            const model = cast(e).find((h) => ownerOf(h.object) === 'model')
            onHoverRef.current(model ? faceOf(model) : null)
        }
        const cancel = () => {
            down = null
        }
        const leave = () => onHoverRef.current(null)
        el.addEventListener('pointerdown', onDown)
        el.addEventListener('pointerup', onUp)
        el.addEventListener('pointermove', onMove)
        el.addEventListener('pointerleave', leave)
        el.addEventListener('pointercancel', cancel)
        return () => {
            el.removeEventListener('pointerdown', onDown)
            el.removeEventListener('pointerup', onUp)
            el.removeEventListener('pointermove', onMove)
            el.removeEventListener('pointerleave', leave)
            el.removeEventListener('pointercancel', cancel)
        }
    }, [gl, scene, get, ws, gizmoRef, triangles])
    return null
}

/** La cara que se va a elegir, resaltada, con su normal. */
function FaceHighlight({hit, accent}: {hit: FaceHit; accent: string}) {
    const geometry = useMemo(() => {
        const g = new BufferGeometry()
        const lift = hit.normal.clone().multiplyScalar(0.05)
        g.setAttribute('position', new Float32BufferAttribute(hit.tri.flatMap((p) => [p.x + lift.x, p.y + lift.y, p.z + lift.z]), 3))
        return g
    }, [hit])
    useEffect(() => () => geometry.dispose(), [geometry])
    const tip = hit.point.clone().addScaledVector(hit.normal, 10)
    const line = useMemo(() => {
        const g = new BufferGeometry()
        g.setAttribute('position', new Float32BufferAttribute([hit.point.x, hit.point.y, hit.point.z, tip.x, tip.y, tip.z], 3))
        return g
    }, [hit]) // eslint-disable-line react-hooks/exhaustive-deps
    useEffect(() => () => line.dispose(), [line])
    return (
        <>
            <mesh geometry={geometry} renderOrder={10}>
                <meshBasicMaterial color={accent} transparent opacity={0.85} side={DoubleSide} depthTest={false} toneMapped={false} />
            </mesh>
            <lineSegments geometry={line} renderOrder={10}>
                <lineBasicMaterial color={accent} depthTest={false} toneMapped={false} />
            </lineSegments>
        </>
    )
}

// ── La escena ──────────────────────────────────────────────────────────────────

export function EditScene({
    ws,
    geometry,
    bed,
    theme,
    actions,
    ortho,
    initialView,
    handleRef,
    fitBox,
}: {
    ws: number
    geometry: BufferGeometry | null
    bed: BedVolume
    theme: ThemeColors
    actions: EditActions
    ortho: boolean
    initialView: ViewName
    handleRef: RefObject<PaneHandle | null>
    fitBox?: Box3
}) {
    const s = useSession(ws)
    const [target, setTarget] = useState<Object3D | null>(null)
    const gizmoRef = useRef<GizmoHandle | null>(null)
    const [hover, setHover] = useState<FaceHit | null>(null)
    const tool: Tool = s?.tool ?? 'translate'
    useEffect(() => {
        if (tool !== 'face') setHover(null)
    }, [tool])

    const doc = s ? docOf(s) : null
    const modelMatrix = useMemo(() => (doc ? tfMatrix(doc.model) : null), [doc?.model]) // eslint-disable-line react-hooks/exhaustive-deps
    const worldBox = useMemo(() => {
        if (!geometry || !modelMatrix) return new Box3()
        return boundsOf(geometry, modelMatrix)
    }, [geometry, modelMatrix])

    // El ancho de lo que se corta (modelo y uniones): la mitad de abajo se separa ese ancho + 10 mm en X, como el código generado.
    const dx = useMemo(() => {
        const b = worldBox.clone()
        for (const p of doc?.parts ?? []) if (p.mode === 'union') b.union(partWorldBox(p))
        return b.isEmpty() ? 10 : boxSize(b)[0] + 10
    }, [worldBox, doc?.parts]) // eslint-disable-line react-hooks/exhaustive-deps

    if (!s || !doc) return null
    const pivot: Vec3 | undefined = geometry?.boundingBox ? ((c) => [c.x, c.y, c.z] as Vec3)(geometry.boundingBox.getCenter(new Vector3())) : undefined
    const sel = s.sel
    const variants = variantsOf(doc.cut, dx)
    const extent = Math.max(30, Math.max(...boxSize(worldBox), 1) * 1.25)
    const size = boxSize(worldBox)
    const overflow = size[0] > bed.x + 0.01 || size[1] > bed.y + 0.01 || size[2] > bed.z + 0.01

    // «cut» no se escala: el gizmo del plano pasa a mover.
    const gizmoMode: GizmoMode | null = tool === 'face' || !sel ? null : sel.kind === 'cut' && tool === 'scale' ? 'translate' : tool
    const triangles = geometry ? geometry.attributes.position.count / 3 : 0

    return (
        <>
            <Environment />
            <ambientLight intensity={0.15} />
            <directionalLight position={[60, -90, 160]} intensity={0.8} />
            <Bed bed={bed} theme={theme} overflow={overflow} />

            {geometry ? (
                <Bodies id="model" tf={doc.model} pivot={pivot} selected={sel?.kind === 'model'} onObject={setTarget} variants={variants}>
                    {(v) => <ModelMesh geometry={geometry} color={v.color || doc.color} plane={v.plane} selected={sel?.kind === 'model' && v === variants[0]} accent={theme.accent} />}
                </Bodies>
            ) : null}

            {doc.parts.map((p) => {
                const isSel = sel?.kind === 'part' && sel.id === p.id
                // Un agujero no se corta ni se copia: se resta de todo. Las uniones sí siguen al corte.
                const vs: Variant[] = p.mode === 'hole' ? [{plane: null, color: null, dx: 0}] : variants
                return (
                    <Bodies key={p.id} id={p.id} tf={p.tf} selected={isSel} onObject={setTarget} variants={vs}>
                        {(v) => <PartMesh part={p} color={p.color || v.color || doc.color} plane={v.plane} selected={isSel} theme={theme} />}
                    </Bodies>
                )
            })}

            {doc.cut ? <CutPlaneObject cut={doc.cut} extent={extent} selected={sel?.kind === 'cut'} onObject={setTarget} theme={theme} /> : null}

            {target && sel && gizmoMode ? (
                <GizmoLayer target={target} sel={sel} mode={gizmoMode} doc={doc} geometry={geometry} pivot={pivot} snap={s.snap} actions={actions} gizmoRef={gizmoRef} />
            ) : null}
            {hover && tool === 'face' ? <FaceHighlight hit={hover} accent={theme.accent} /> : null}

            <PickLayer ws={ws} tool={tool} gizmoRef={gizmoRef} triangles={triangles} onFace={(n) => actions.layFace(n)} onHover={setHover} />
            <Rig geometry={null} fitBox={fitBox} bed={bed} ortho={ortho} initialView={initialView} handleRef={handleRef} />
        </>
    )
}
