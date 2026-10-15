import type {BufferGeometry} from 'three'
import type {mesh} from '../../wailsjs/go/models'
import {GetMeshInfo} from '../../wailsjs/go/main/App'
import {meshKey, triangleCount} from './mesh'

// Medidas de una malla. Las calcula Go (backend/mesh, binding GetMeshInfo):
// las reutilizan el MCP, el peso/costo y el aviso de placa, así que no se
// duplican acá.
export type MeshInfo = mesh.Info

// `exact` = vino de Go. Mientras llega (o si falla), el visor muestra lo que
// sabe por la geometría (caja y triángulos) y deja volumen y estanqueidad
// sin dato.
export type MeshFacts = {info: MeshInfo; exact: boolean}

// La malla de una clave no cambia nunca: se pregunta una vez.
const infos = new Map<string, Promise<MeshInfo>>()

export function getMeshInfo(meshPath: string): Promise<MeshInfo> {
    const key = meshKey(meshPath)
    let p = infos.get(key)
    if (!p) {
        p = GetMeshInfo(meshPath)
        p.catch(() => infos.delete(key))
        infos.set(key, p)
    }
    return p
}

export function infoFromGeometry(g: BufferGeometry): MeshInfo {
    const b = g.boundingBox
    const min = b ? [b.min.x, b.min.y, b.min.z] : [0, 0, 0]
    const max = b ? [b.max.x, b.max.y, b.max.z] : [0, 0, 0]
    return {
        triangles: triangleCount(g),
        min,
        max,
        size: max.map((v, i) => v - min[i]),
        volumeMm3: 0,
        areaMm2: 0,
        watertight: false,
    }
}
