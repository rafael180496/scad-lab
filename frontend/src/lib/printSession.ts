import {useEffect, useRef} from 'react'
import {meshKey} from './mesh'
import {getMeshInfo} from './meshInfo'
import {estimatePrint, getModelParts} from './printApi'
import type {EstimateOptions, EstimateResult, ModelParts, PlateFit, Vec3} from './printTypes'
import {errorText} from './errors'
import {materialsStore} from './materials'
import {createStore} from './store'

// Sesión de impresión (S7): el modelo por partes del checkpoint a la vista, su
// estimación de peso y costo, la parte seleccionada y los parámetros del
// cálculo. Es un store de módulo y no estado de App porque lo comparten el
// visor (que pinta las partes y abre la tarjeta), el panel «Impresión» (la
// tabla), la barra de estado (el aviso de placa) y el menú: en pantallas
// distintas, a veces en paneles que no se ven a la vez.
//
// Quien lo alimenta es usePrintSession(), que se monta UNA vez en PanelHost.

export type Status = 'idle' | 'loading' | 'ready' | 'error'

/** Parámetros del cálculo que el usuario puede cambiar en el panel. */
export type PrintParams = {
    infillPercent: number
    wallLoops: number
    lineWidthMm: number
    filamentDiameterMm: number
    defaultMaterialId: string
    currency: string
}

export const printStore = createStore({
    /** Qué está cargado: `workspace:archivo:malla`. */
    ctx: '',
    /** Clave de la malla (`meshKey`) a la que pertenece `model`. */
    modelMesh: '',
    model: null as ModelParts | null,
    modelStatus: 'idle' as Status,
    modelError: '',
    selected: null as number | null,
    /** Lo que el panel cambió respecto de Configuración (solo para este cálculo). */
    overrides: {} as Partial<PrintParams>,
    /** Material por parte (índice → id). Las que no estén usan el predeterminado. */
    materials: {} as Record<number, string>,
    estimate: null as EstimateResult | null,
    estimateStatus: 'idle' as Status,
    estimateError: '',
    /** Parámetros efectivos (Configuración + lo que cambió el panel). */
    params: null as PrintParams | null,
    plate: null as PlateFit | null,
    /** Sube para volver a pedir el modelo tras un error (botón «Reintentar»). */
    retry: 0,
})

export const retryModel = () => printStore.set({ctx: '', retry: printStore.get().retry + 1})
export const selectPart = (i: number | null) => printStore.set({selected: i})

// La parte elegida al irse de un proyecto (S8): el store es uno solo y al cambiar
// de pestaña el contexto cambia y la selección se borra. Antes de irse se anota
// la selección con el contexto al que pertenece y, al volver a ese contexto, se
// recupera una sola vez. Es por contexto (proyecto + checkpoint + malla), así
// que no se cuela en otro checkpoint.
const partMemory = new Map<string, number>()

/** Anota la parte seleccionada del contexto actual (se llama al cambiar de pestaña). */
export function rememberPartSelection() {
    const {ctx, selected} = printStore.get()
    if (ctx && selected !== null) partMemory.set(ctx, selected)
}

/** Olvida las selecciones anotadas de un proyecto (se cerró su pestaña). */
export function forgetPartSelection(workspaceId: number) {
    for (const k of [...partMemory.keys()]) if (k.startsWith(`${workspaceId}:`)) partMemory.delete(k)
}

export function setPartMaterial(index: number, id: string) {
    printStore.set({materials: {...printStore.get().materials, [index]: id}})
}

export const setOverrides = (patch: Partial<PrintParams>) => printStore.set({overrides: {...printStore.get().overrides, ...patch}})
export const resetOverrides = () => printStore.set({overrides: {}})

/** Parámetros efectivos: lo de Configuración con lo del panel encima. */
export function effectiveParams(base: PrintParams, overrides: Partial<PrintParams>): PrintParams {
    return {...base, ...overrides}
}

// Cuánto sobra por eje; un eje que cabe da 0 (con holgura de 0,05 mm: la caja
// de una malla de 180 mm exactos no debe avisar por un error de redondeo).
const FIT_EPS = 0.05

export function plateFit(size: number[], plate: Vec3): PlateFit {
    const over = (v: number, max: number) => (v > max + FIT_EPS ? v - max : 0)
    const overflow = {x: over(size[0] ?? 0, plate.x), y: over(size[1] ?? 0, plate.y), z: over(size[2] ?? 0, plate.z)}
    return {fits: overflow.x === 0 && overflow.y === 0 && overflow.z === 0, overflow, plate}
}

// Los modelos ya calculados: volver a un checkpoint visto no repite el viaje a
// Go (que además re-renderiza a 3MF la primera vez) ni hace parpadear el visor.
const MAX_MODELS = 12
const models = new Map<string, ModelParts>()
function remember(ctx: string, m: ModelParts) {
    models.delete(ctx)
    models.set(ctx, m)
    for (const k of models.keys()) {
        if (models.size <= MAX_MODELS) break
        models.delete(k)
    }
}

function takeRemembered(ctx: string): number | null {
    const v = partMemory.get(ctx)
    partMemory.delete(ctx)
    return v ?? null
}

const ESTIMATE_DEBOUNCE_MS = 250

export function usePrintSession({
    workspaceId,
    file,
    meshPath,
    enabled,
    base,
    bed,
}: {
    workspaceId: number | null
    file: string | null
    /** Malla del render OK del checkpoint ('' si no hay). */
    meshPath: string
    /** Algún panel que usa las partes se ve: recién entonces se pide el modelo. */
    enabled: boolean
    base: PrintParams
    bed: Vec3
}) {
    const overrides = printStore.use((s) => s.overrides)
    const materials = printStore.use((s) => s.materials)
    const retry = printStore.use((s) => s.retry)
    const storedModel = printStore.use((s) => s.model)
    const modelMesh = printStore.use((s) => s.modelMesh)
    const matRev = materialsStore.use((s) => s.rev)
    const params = effectiveParams(base, overrides)
    // El modelo del store solo vale si es el de la malla a la vista: con el visor y el
    // panel ocultos no se actualiza y puede ser de otro checkpoint.
    const model = storedModel && !!meshPath && modelMesh === meshKey(meshPath) ? storedModel : null
    const ctx = workspaceId && file && meshPath ? `${workspaceId}:${file}:${meshKey(meshPath)}` : ''

    // 1. El modelo por partes del checkpoint a la vista.
    useEffect(() => {
        const cur = printStore.get()
        if (!ctx) {
            if (cur.ctx) printStore.set({ctx: '', modelMesh: '', model: null, modelStatus: 'idle', modelError: '', selected: null, estimate: null, estimateStatus: 'idle', materials: {}})
            return
        }
        if (!enabled || ctx === cur.ctx) return
        const hit = models.get(ctx)
        printStore.set({
            ctx,
            modelMesh: meshKey(meshPath),
            model: hit ?? null,
            modelStatus: hit ? 'ready' : 'loading',
            modelError: '',
            selected: takeRemembered(ctx),
            materials: {},
            estimate: null,
            estimateStatus: 'idle',
        })
        if (hit) return
        let alive = true
        getModelParts(workspaceId!, file!)
            .then((m) => {
                if (!alive) return
                remember(ctx, m)
                printStore.set({model: m, modelStatus: 'ready'})
            })
            .catch((e) => {
                if (!alive) return
                printStore.set({model: null, modelStatus: 'error', modelError: errorText(e)})
            })
        return () => {
            alive = false
            // Si se interrumpe antes de llegar (otro checkpoint), el siguiente efecto
            // arma su propio estado; este ctx queda vacío para poder reintentarse.
            if (printStore.get().ctx === ctx && printStore.get().modelStatus === 'loading') printStore.set({ctx: ''})
        }
    }, [ctx, enabled, workspaceId, file, meshPath, retry])

    // 2. La placa: del modelo si ya llegó; mientras tanto de la caja de la malla
    // (barata y exacta), así el aviso no espera al render a 3MF.
    useEffect(() => {
        if (model) {
            printStore.set({plate: {fits: model.fits, overflow: model.overflow, plate: model.plate}})
            return
        }
        if (!meshPath) {
            printStore.set({plate: null})
            return
        }
        let alive = true
        getMeshInfo(meshPath)
            .then((info) => alive && printStore.set({plate: plateFit(info.size, bed)}))
            .catch(() => {})
        return () => {
            alive = false
        }
    }, [model, meshPath, bed.x, bed.y, bed.z])

    // 3. La estimación, con un respiro: arrastrar un campo numérico dispara un
    // cambio por tecla. La primera de un modelo no espera.
    const seq = useRef(0)
    const matKey = JSON.stringify(materials)
    const paramKey = JSON.stringify(params)
    useEffect(() => {
        if (!model || !workspaceId || !file || !ctx || printStore.get().ctx !== ctx) return
        const opts: EstimateOptions = {
            infillPercent: params.infillPercent,
            wallLoops: params.wallLoops,
            lineWidthMm: params.lineWidthMm,
            filamentDiameterMm: params.filamentDiameterMm,
            defaultMaterialId: params.defaultMaterialId,
            partMaterials: materials,
            currency: params.currency,
        }
        const mine = ++seq.current
        const first = !printStore.get().estimate
        const timer = window.setTimeout(
            () => {
                printStore.set({estimateStatus: 'loading', estimateError: ''})
                estimatePrint(workspaceId, file, opts)
                    .then((r) => {
                        if (mine !== seq.current) return
                        printStore.set({estimate: r, estimateStatus: 'ready'})
                    })
                    .catch((e) => {
                        if (mine !== seq.current) return
                        printStore.set({estimateStatus: 'error', estimateError: errorText(e)})
                    })
            },
            first ? 0 : ESTIMATE_DEBOUNCE_MS,
        )
        return () => window.clearTimeout(timer)
    }, [model, workspaceId, file, ctx, matKey, paramKey, matRev])

    // Los parámetros efectivos, para quien los muestre (panel, tarjeta).
    useEffect(() => {
        printStore.set({params})
    }, [paramKey])
}
