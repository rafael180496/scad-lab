import type {estimate, parts, printers} from '../../wailsjs/go/models'

// Tipos del modelo por partes y de la estimación (S7, bloque 7.A:
// backend/parts, backend/estimate, app_parts.go). Son los generados por
// `wails generate module`; se re-exportan acá con los nombres del contrato
// (.claude/specs/s7-medicion-impresion.md) para que el resto del frontend no
// dependa de los espacios de nombres de Go.

/** Una parte del modelo = un color (un `color()` de OpenSCAD). */
export type ModelPart = parts.Part
export type ModelParts = parts.Model
export type Vec3 = printers.Volume
export type EstimateOptions = estimate.Options
export type PartEstimate = estimate.PartResult
export type EstimateResult = estimate.Result

/** Si el modelo cabe en la placa; sale del modelo de partes o, mientras llega, de la malla. */
export type PlateFit = {fits: boolean; overflow: Vec3; plate: Vec3}

/** Color propio de una parte ('' si el modelo no se lo dio: se pinta con la muestra). */
export function partColor(p: ModelPart): string {
    return p.uncolored ? '' : p.color
}
