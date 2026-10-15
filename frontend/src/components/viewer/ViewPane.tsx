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

type SceneProps = {
    geometry: BufferGeometry | null
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

function Environment() {
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
function Bed({bed, theme}: {bed: BedVolume; theme: ThemeColors}) {
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
                <lineBasicMaterial color={theme.lineStrong} transparent opacity={0.35} toneMapped={false} />
            </lineSegments>
        </group>
    )
}

// edgeColor contrasta con la muestra: aristas claras sobre un filamento
// oscuro y oscuras sobre uno claro. Sale del color elegido, no es un color
// suelto.
function edgeColor(hex: string): string {
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

function Rig({geometry, bed, ortho, initialView, handleRef}: Pick<SceneProps, 'geometry' | 'bed' | 'ortho' | 'initialView' | 'handleRef'>) {
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
        if (geometry?.boundingBox) return geometry.boundingBox
        // Sin pieza: se encuadra la placa.
        return new Box3(new Vector3(-bed.x / 2, -bed.y / 2, 0), new Vector3(bed.x / 2, bed.y / 2, 0))
    }, [geometry, bed.x, bed.y])

    // Primer encuadre: cuando llega la primera pieza y el panel ya tiene
    // tamaño. Al cambiar de checkpoint la cámara se queda donde estaba:
    // comparar dos versiones desde el mismo ángulo es lo que se busca.
    useLayoutEffect(() => {
        if (fitted.current || size.width === 0 || size.height === 0) return
        fitView(cam, box, initialView, size.width, size.height, target)
        controls.current?.update()
        if (geometry) fitted.current = true
        invalidate()
    }, [geometry, box, size.width, size.height, cam, initialView, target, invalidate])

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

    return <OrbitControls ref={controls} camera={cam} enableDamping dampingFactor={0.18} />
}

function Scene(p: SceneProps) {
    const shadowFar = Math.max(10, (p.geometry?.boundingBox?.max.z ?? 10) * 1.2)
    return (
        <>
            <Environment />
            <ambientLight intensity={0.15} />
            <directionalLight position={[60, -90, 160]} intensity={0.8} />
            <Bed bed={p.bed} theme={p.theme} />
            {p.geometry ? (
                <>
                    <Model geometry={p.geometry} color={p.color} wireframe={p.wireframe} edges={p.edges} />
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

export type PaneKeyCommand = 'wireframe' | 'edges' | 'ortho'

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
