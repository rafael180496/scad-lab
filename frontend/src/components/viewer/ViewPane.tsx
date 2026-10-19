import {useEffect, useLayoutEffect, useMemo, useRef, type ComponentRef, type ReactNode, type RefObject} from 'react'
import {Canvas, useFrame, useThree} from '@react-three/fiber'
import {ContactShadows, OrbitControls} from '@react-three/drei'
import {
    Box3,
    BoxGeometry,
    BufferGeometry,
    Color,
    EdgesGeometry,
    Float32BufferAttribute,
    type Group,
    NeutralToneMapping,
    type Object3D,
    PMREMGenerator,
    Raycaster,
    Vector2,
    Vector3,
} from 'three'
import {RoomEnvironment} from 'three/examples/jsm/environments/RoomEnvironment.js'
import type {ThemeColors} from '../../hooks/useThemeColors'
import {edgesOf} from '../../lib/mesh'
import {cn} from '../../lib/cn'
import {fitView, makeCameras, orbit, pan, switchProjection, updateProjection, zoom, type ViewName} from './camera'

// Un panel del visor: un <Canvas> con su propia cámara y controles. La vista
// dividida monta dos que comparten la MISMA BufferGeometry (three la sube a
// cada contexto WebGL por separado; no se clona).

export type BedVolume = {x: number; y: number; z: number}

export type PaneHandle = {
    setView: (v: ViewName) => void
    fit: () => void
    orbit: (dAzimuth: number, dPolar: number) => void
    pan: (fx: number, fy: number) => void
    zoom: (factor: number) => void
    // Renderiza un cuadro y devuelve el canvas listo para toDataURL: se lee en
    // el mismo tick, así no hace falta preserveDrawingBuffer siempre.
    capture: () => HTMLCanvasElement
}

// El modelo por partes (S7): una geometría por color, con el color que le da el
// modelo, y cuál está seleccionada. La selección vive en el visor (la comparten
// las dos vistas de la vista dividida), no en cada Canvas.
export type PartsScene = {
    geoms: BufferGeometry[]
    /** Color de cada parte (#RRGGBB); '' = sin color propio, se usa la muestra. */
    colors: string[]
    /** true = cada parte con su color; false = todas con la muestra elegida. */
    modelColors: boolean
    selected: number | null
    onSelect: (index: number | null) => void
}

type SceneProps = {
    geometry: BufferGeometry | null
    parts?: PartsScene
    /** El modelo no cabe en la placa: contorno de la cama en rojo. */
    overflow?: boolean
    bed: BedVolume
    color: string
    wireframe: boolean
    edges: boolean
    ortho: boolean
    initialView: ViewName
    theme: ThemeColors
    handleRef: RefObject<PaneHandle | null>
}

// onLayer1 pasa un objeto y sus hijos a la capa 1 (ver makeCameras).
function onLayer1(o: Object3D | null) {
    o?.traverse((c) => c.layers.set(1))
}

export function Environment() {
    const gl = useThree((s) => s.gl)
    const scene = useThree((s) => s.scene)
    const invalidate = useThree((s) => s.invalidate)
    useEffect(() => {
        // Sin red: el mapa de entorno se genera acá a partir de una
        // habitación sintética de three (nada de HDR descargados).
        const pm = new PMREMGenerator(gl)
        const room = new RoomEnvironment()
        const rt = pm.fromScene(room, 0.04)
        scene.environment = rt.texture
        scene.environmentIntensity = 0.65
        invalidate()
        return () => {
            scene.environment = null
            rt.dispose()
            room.dispose()
            pm.dispose()
        }
    }, [gl, scene, invalidate])
    return null
}

function gridLines(x: number, y: number, step: number, every: number, major: boolean): BufferGeometry {
    const pos: number[] = []
    const hx = x / 2
    const hy = y / 2
    for (let i = 0; i * step <= hx; i++) {
        if ((i % every === 0) !== major) continue
        for (const s of i === 0 ? [0] : [i * step, -i * step]) pos.push(s, -hy, 0, s, hy, 0)
    }
    for (let i = 0; i * step <= hy; i++) {
        if ((i % every === 0) !== major) continue
        for (const s of i === 0 ? [0] : [i * step, -i * step]) pos.push(-hx, s, 0, hx, s, 0)
    }
    const g = new BufferGeometry()
    g.setAttribute('position', new Float32BufferAttribute(pos, 3))
    return g
}

// Bed: la placa de la impresora del workspace a escala, con cuadrícula de
// 10 mm (más marcada cada 50) y la caja del volumen de impresión en líneas
// tenues. Centrada en el origen, como la pieza.
export function Bed({bed, theme, overflow}: {bed: BedVolume; theme: ThemeColors; overflow: boolean}) {
    const minor = useMemo(() => gridLines(bed.x, bed.y, 10, 5, false), [bed.x, bed.y])
    const major = useMemo(() => gridLines(bed.x, bed.y, 10, 5, true), [bed.x, bed.y])
    const volume = useMemo(() => {
        const box = new BoxGeometry(bed.x, bed.y, bed.z)
        const e = new EdgesGeometry(box)
        box.dispose()
        e.translate(0, 0, bed.z / 2)
        return e
    }, [bed.x, bed.y, bed.z])
    useEffect(
        () => () => {
            minor.dispose()
            major.dispose()
            volume.dispose()
        },
        [minor, major, volume],
    )
    // Desde abajo (vista Abajo) la placa y su cuadrícula taparían la cara de
    // apoyo, que es justo lo que se quiere ver: se ocultan; queda la caja.
    const plate = useRef<Group>(null)
    useFrame(({camera}) => {
        if (plate.current) plate.current.visible = camera.position.z >= 0
    })
    return (
        <group ref={onLayer1}>
            <group ref={plate}>
                <mesh position={[0, 0, -0.08]}>
                    <planeGeometry args={[bed.x, bed.y]} />
                    <meshBasicMaterial color={theme.raised} toneMapped={false} />
                </mesh>
                <lineSegments geometry={minor} position={[0, 0, -0.04]}>
                    <lineBasicMaterial color={theme.line} toneMapped={false} />
                </lineSegments>
                <lineSegments geometry={major} position={[0, 0, -0.03]}>
                    <lineBasicMaterial color={theme.lineStrong} toneMapped={false} />
                </lineSegments>
            </group>
            <lineSegments geometry={volume}>
                <lineBasicMaterial color={overflow ? theme.danger : theme.lineStrong} transparent opacity={overflow ? 0.7 : 0.35} toneMapped={false} />
            </lineSegments>
            {overflow ? <OverflowFrame bed={bed} color={theme.danger} /> : null}
        </group>
    )
}

// OverflowFrame es el contorno de la cama en rojo cuando la pieza no cabe: cuatro
// tiras sobre el borde (una línea de WebGL mide 1 px y no se vería). Lleva su
// propia ref a la capa 1 porque aparece después del montaje del grupo.
const FRAME_MM = 3
function OverflowFrame({bed, color}: {bed: BedVolume; color: string}) {
    const hx = bed.x / 2
    const hy = bed.y / 2
    const strip = (x: number, y: number, w: number, h: number) => (
        <mesh position={[x, y, 0.03]}>
            <planeGeometry args={[w, h]} />
            <meshBasicMaterial color={color} toneMapped={false} />
        </mesh>
    )
    return (
        <group ref={onLayer1}>
            {strip(0, hy - FRAME_MM / 2, bed.x, FRAME_MM)}
            {strip(0, -hy + FRAME_MM / 2, bed.x, FRAME_MM)}
            {strip(-hx + FRAME_MM / 2, 0, FRAME_MM, bed.y - 2 * FRAME_MM)}
            {strip(hx - FRAME_MM / 2, 0, FRAME_MM, bed.y - 2 * FRAME_MM)}
        </group>
    )
}

// edgeColor contrasta con la muestra: aristas claras sobre un filamento
// oscuro y oscuras sobre uno claro. Sale del color elegido, no es un color
// suelto.
export function edgeColor(hex: string): string {
    const c = new Color(hex)
    const lum = 0.2126 * c.r + 0.7152 * c.g + 0.0722 * c.b
    return '#' + (lum < 0.3 ? c.lerp(new Color(1, 1, 1), 0.7) : c.lerp(new Color(0, 0, 0), 0.85)).getHexString()
}

function Model({geometry, color, wireframe, edges}: {geometry: BufferGeometry; color: string; wireframe: boolean; edges: boolean}) {
    const lines = edges && !wireframe ? edgesOf(geometry) : null
    return (
        <>
            <mesh geometry={geometry}>
                {wireframe ? (
                    <meshBasicMaterial color={color} wireframe />
                ) : (
                    <meshPhysicalMaterial
                        color={color}
                        roughness={0.5}
                        metalness={0}
                        clearcoat={0.15}
                        clearcoatRoughness={0.35}
                        polygonOffset
                        polygonOffsetFactor={2}
                        polygonOffsetUnits={4}
                    />
                )}
            </mesh>
            {lines ? (
                <lineSegments geometry={lines}>
                    <lineBasicMaterial color={edgeColor(color)} />
                </lineSegments>
            ) : null}
        </>
    )
}

// Parts dibuja el modelo por partes. La selección resalta la parte (brillo del
// color de acento y su contorno) y atenúa el resto.
//
// El clic NO se resuelve con los onClick de R3F: cada objeto con manejadores entra
// en la lista que R3F recorre con un raycast en CADA movimiento del ratón, y sin
// una estructura de aceleración eso son decenas de ms por movimiento en una malla
// grande. Acá se lanza UN rayo, al soltar el botón, y solo si el ratón casi no se
// movió desde que se pulsó (si no, era un arrastre de la órbita o del desplazamiento).
const CLICK_SLOP_PX = 4
const DIM_OPACITY = 0.32

function Parts({parts, swatch, wireframe, edges, accent}: {parts: PartsScene; swatch: string; wireframe: boolean; edges: boolean; accent: string}) {
    const gl = useThree((s) => s.gl)
    const get = useThree((s) => s.get)
    const group = useRef<Group>(null)
    // El manejador va por ref: el visor se vuelve a dibujar al activar el panel (en el
    // pointerdown de ese mismo clic) y re-suscribir los listeners ahí perdería el
    // «botón pulsado» y el clic se ignoraría.
    const onSelectRef = useRef(parts.onSelect)
    onSelectRef.current = parts.onSelect
    const hasSel = parts.selected !== null

    useEffect(() => {
        const el = gl.domElement
        const ray = new Raycaster()
        const ndc = new Vector2()
        let down: {x: number; y: number} | null = null
        const onDown = (e: PointerEvent) => {
            down = e.button === 0 ? {x: e.clientX, y: e.clientY} : null
        }
        const onUp = (e: PointerEvent) => {
            const d = down
            down = null
            if (!d || e.button !== 0 || Math.hypot(e.clientX - d.x, e.clientY - d.y) > CLICK_SLOP_PX) return
            const r = el.getBoundingClientRect()
            if (r.width === 0 || r.height === 0) return
            ndc.set(((e.clientX - r.left) / r.width) * 2 - 1, -((e.clientY - r.top) / r.height) * 2 + 1)
            ray.setFromCamera(ndc, get().camera)
            // Recursivo (cada parte es un grupo con su malla y sus aristas); solo cuentan
            // las mallas, que son las que llevan el índice de la parte.
            const hit = group.current ? ray.intersectObjects(group.current.children, true).find((h) => h.object.userData.part !== undefined) : undefined
            onSelectRef.current(hit ? (hit.object.userData.part as number) : null)
        }
        const cancel = () => {
            down = null
        }
        el.addEventListener('pointerdown', onDown)
        el.addEventListener('pointerup', onUp)
        el.addEventListener('pointercancel', cancel)
        return () => {
            el.removeEventListener('pointerdown', onDown)
            el.removeEventListener('pointerup', onUp)
            el.removeEventListener('pointercancel', cancel)
        }
    }, [gl, get])

    return (
        <group ref={group}>
            {parts.geoms.map((g, i) => {
                const own = parts.modelColors ? parts.colors[i] : ''
                const color = own || swatch
                const selected = parts.selected === i
                const dim = hasSel && !selected
                return (
                    <group key={i}>
                        <mesh geometry={g} userData={{part: i}}>
                            {wireframe ? (
                                <meshBasicMaterial color={color} wireframe transparent={dim} opacity={dim ? DIM_OPACITY : 1} />
                            ) : (
                                <meshPhysicalMaterial
                                    color={color}
                                    emissive={selected ? accent : '#000000'}
                                    emissiveIntensity={selected ? 0.2 : 0}
                                    roughness={0.5}
                                    metalness={0}
                                    clearcoat={0.15}
                                    clearcoatRoughness={0.35}
                                    transparent={dim}
                                    opacity={dim ? DIM_OPACITY : 1}
                                    depthWrite={!dim}
                                    polygonOffset
                                    polygonOffsetFactor={2}
                                    polygonOffsetUnits={4}
                                />
                            )}
                        </mesh>
                        {selected && !wireframe ? (
                            <lineSegments geometry={edgesOf(g)}>
                                <lineBasicMaterial color={accent} toneMapped={false} />
                            </lineSegments>
                        ) : edges && !wireframe && !dim ? (
                            <lineSegments geometry={edgesOf(g)}>
                                <lineBasicMaterial color={edgeColor(color)} />
                            </lineSegments>
                        ) : null}
                    </group>
                )
            })}
        </group>
    )
}

// `fitBox` es para quien no tiene una geometría que encuadrar (el editor de STL, que dibuja la pieza ya
// transformada): se encuadra esa caja, una sola vez, en vez de la de `geometry`.
export function Rig({geometry, bed, ortho, initialView, handleRef, fitBox}: Pick<SceneProps, 'geometry' | 'bed' | 'ortho' | 'initialView' | 'handleRef'> & {fitBox?: Box3}) {
    const set = useThree((s) => s.set)
    const size = useThree((s) => s.size)
    const gl = useThree((s) => s.gl)
    const scene = useThree((s) => s.scene)
    const invalidate = useThree((s) => s.invalidate)
    const cams = useMemo(makeCameras, [])
    const target = useMemo(() => new Vector3(), [])
    const cam = ortho ? cams.ortho : cams.persp
    const prev = useRef(cam)
    const controls = useRef<ComponentRef<typeof OrbitControls>>(null)
    const fitted = useRef(false)

    useLayoutEffect(() => {
        if (prev.current !== cam) {
            switchProjection(prev.current, cam, target, size.height)
            prev.current = cam
        }
        updateProjection(cam, size.width, size.height)
        set({camera: cam})
        invalidate()
    }, [cam, size.width, size.height, set, invalidate, target])

    // Los controles se recrean al cambiar de cámara: comparten el mismo
    // objetivo para que pasar a orto no recentre la vista.
    useLayoutEffect(() => {
        const c = controls.current
        if (!c) return
        c.target = target
        c.update()
    }, [cam, target])

    const box = useMemo(() => {
        if (fitBox) return fitBox
        if (geometry?.boundingBox) return geometry.boundingBox
        // Sin pieza: se encuadra la placa.
        return new Box3(new Vector3(-bed.x / 2, -bed.y / 2, 0), new Vector3(bed.x / 2, bed.y / 2, 0))
    }, [fitBox, geometry, bed.x, bed.y])

    // Primer encuadre: cuando llega la primera pieza y el panel ya tiene
    // tamaño. Al cambiar de checkpoint la cámara se queda donde estaba:
    // comparar dos versiones desde el mismo ángulo es lo que se busca.
    useLayoutEffect(() => {
        if (fitted.current || size.width === 0 || size.height === 0) return
        fitView(cam, box, initialView, size.width, size.height, target)
        controls.current?.update()
        if (geometry || fitBox) fitted.current = true
        invalidate()
    }, [geometry, fitBox, box, size.width, size.height, cam, initialView, target, invalidate])

    useLayoutEffect(() => {
        const done = () => {
            controls.current?.update()
            invalidate()
        }
        handleRef.current = {
            setView: (v) => {
                fitView(cam, box, v, size.width, size.height, target)
                done()
            },
            fit: () => {
                fitView(cam, box, null, size.width, size.height, target)
                done()
            },
            orbit: (a, p) => {
                orbit(cam, target, a, p)
                done()
            },
            pan: (fx, fy) => {
                pan(cam, target, fx, fy, size.height)
                done()
            },
            zoom: (f) => {
                zoom(cam, target, f)
                done()
            },
            capture: () => {
                gl.render(scene, cam)
                return gl.domElement
            },
        }
    })

    // makeDefault: los controles quedan en `state.controls`, que es lo que mira el gizmo del editor de STL
    // (TransformControls de drei) para apagar la órbita mientras se arrastra una flecha.
    return <OrbitControls ref={controls} camera={cam} makeDefault enableDamping dampingFactor={0.18} />
}

function Scene(p: SceneProps) {
    const shadowFar = Math.max(10, (p.geometry?.boundingBox?.max.z ?? 10) * 1.2)
    return (
        <>
            <Environment />
            <ambientLight intensity={0.15} />
            <directionalLight position={[60, -90, 160]} intensity={0.8} />
            <Bed bed={p.bed} theme={p.theme} overflow={!!p.overflow} />
            {p.geometry ? (
                <>
                    {p.parts ? (
                        <Parts parts={p.parts} swatch={p.color} wireframe={p.wireframe} edges={p.edges} accent={p.theme.accent} />
                    ) : (
                        <Model geometry={p.geometry} color={p.color} wireframe={p.wireframe} edges={p.edges} />
                    )}
                    {/* drei dibuja las sombras de contacto mirando a +Y; el grupo
                        las gira para que miren a +Z (la escena es Z arriba). */}
                    <group rotation={[Math.PI / 2, 0, 0]}>
                        <ContactShadows
                            key={p.geometry.uuid}
                            frames={1}
                            scale={[p.bed.x, p.bed.y]}
                            resolution={1024}
                            far={shadowFar}
                            blur={2.2}
                            opacity={0.55}
                        />
                    </group>
                </>
            ) : null}
            <Rig geometry={p.geometry} bed={p.bed} ortho={p.ortho} initialView={p.initialView} handleRef={p.handleRef} />
        </>
    )
}

const ORBIT_STEP = Math.PI / 12
const PAN_STEP = 0.06
const ZOOM_STEP = 0.85

export type PaneKeyCommand = 'wireframe' | 'edges' | 'ortho' | 'deselect'

export function ViewPane({
    scene,
    active,
    label,
    onActivate,
    onCommand,
    className,
    children,
}: {
    scene: SceneProps
    active: boolean
    label: string
    onActivate: () => void
    onCommand: (c: PaneKeyCommand) => void
    className?: string
    children?: ReactNode
}) {
    const {handleRef} = scene
    const onKeyDown = (e: React.KeyboardEvent) => {
        // Cmd/Ctrl/Alt quedan para los atajos de la app.
        if (e.metaKey || e.ctrlKey || e.altKey) return
        const h = handleRef.current
        if (!h) return
        const k = e.key
        let handled = true
        if (k === 'ArrowLeft') e.shiftKey ? h.pan(-PAN_STEP, 0) : h.orbit(-ORBIT_STEP, 0)
        else if (k === 'ArrowRight') e.shiftKey ? h.pan(PAN_STEP, 0) : h.orbit(ORBIT_STEP, 0)
        else if (k === 'ArrowUp') e.shiftKey ? h.pan(0, PAN_STEP) : h.orbit(0, -ORBIT_STEP)
        else if (k === 'ArrowDown') e.shiftKey ? h.pan(0, -PAN_STEP) : h.orbit(0, ORBIT_STEP)
        else if (k === '+' || k === '=') h.zoom(ZOOM_STEP)
        else if (k === '-' || k === '_') h.zoom(1 / ZOOM_STEP)
        else if (k >= '1' && k <= '7') h.setView((['front', 'back', 'left', 'right', 'top', 'bottom', 'iso'] as ViewName[])[Number(k) - 1])
        else if (k === 'f' || k === 'F') h.fit()
        else if (k === 'w' || k === 'W') onCommand('wireframe')
        else if (k === 'e' || k === 'E') onCommand('edges')
        else if (k === 'o' || k === 'O') onCommand('ortho')
        // Esc solo cuando hay algo que deseleccionar: si no, se lo deja al modo enfoque.
        else if (k === 'Escape' && scene.parts?.selected != null) onCommand('deselect')
        else handled = false
        if (handled) e.preventDefault()
    }
    return (
        <div
            tabIndex={0}
            role="application"
            aria-label={label}
            onKeyDown={onKeyDown}
            onPointerDown={onActivate}
            onFocus={onActivate}
            className={cn('relative min-h-0 min-w-0 overflow-hidden bg-surface-2 outline-none', className)}
        >
            <Canvas
                frameloop="demand"
                dpr={[1, 2]}
                gl={{antialias: true, alpha: true}}
                onCreated={({gl}) => {
                    gl.toneMapping = NeutralToneMapping
                }}
            >
                <Scene {...scene} />
            </Canvas>
            {/* Marco del panel activo (el que reciben la barra y el teclado). */}
            <div
                className={cn(
                    'pointer-events-none absolute inset-0 border transition-colors duration-100',
                    active ? 'border-accent/50' : 'border-transparent',
                )}
            />
            {children}
        </div>
    )
}
