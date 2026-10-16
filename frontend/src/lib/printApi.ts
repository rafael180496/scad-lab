import {EstimatePrint, GetModelParts} from '../../wailsjs/go/main/App'
import type {EstimateOptions, EstimateResult, ModelParts} from './printTypes'

// Bindings de partes y estimación (S7.A, app_parts.go). Capa fina: el resto del
// frontend no importa de wailsjs/ directamente para el módulo de impresión, y
// el banco de capturas los simula en window.go.main.App (src/uishot.tsx).

export function getModelParts(workspaceId: number, fileName: string): Promise<ModelParts> {
    return GetModelParts(workspaceId, fileName)
}

export function estimatePrint(workspaceId: number, fileName: string, opts: EstimateOptions): Promise<EstimateResult> {
    return EstimatePrint(workspaceId, fileName, opts)
}
