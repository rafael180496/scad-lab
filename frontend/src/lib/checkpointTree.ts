import type {store} from '../../wailsjs/go/models'

export type TreeNode = {
    cp: store.Checkpoint
    children: TreeNode[]
    orphan: boolean // su parent no existe en el workspace
}

// buildTree arma el árbol de ramas a partir de parent_file. Un checkpoint cuyo
// padre no existe (borrado a mano, renombrado) se cuelga de la raíz marcado
// como huérfano: perderlo de la vista sería peor que mostrarlo fuera de lugar.
export function buildTree(list: store.Checkpoint[]): TreeNode[] {
    const byFile = new Map<string, TreeNode>()
    for (const cp of list) byFile.set(cp.fileName, {cp, children: [], orphan: false})
    const roots: TreeNode[] = []
    for (const node of byFile.values()) {
        const parent = node.cp.parentFile ? byFile.get(node.cp.parentFile) : undefined
        // Un archivo que se declara padre de sí mismo no debe colgar del árbol.
        if (parent && parent !== node) {
            parent.children.push(node)
        } else {
            node.orphan = !!node.cp.parentFile
            roots.push(node)
        }
    }
    const sort = (ns: TreeNode[]) => {
        ns.sort((a, b) => a.cp.fileName.localeCompare(b.cp.fileName))
        ns.forEach((n) => sort(n.children))
    }
    sort(roots)
    return roots
}

// descendants cuenta todos los checkpoints que cuelgan de node (hijos, nietos…),
// sin incluirlo: es lo que dice la confirmación de «mover la rama a la papelera».
export function countDescendants(node: TreeNode): number {
    return node.children.reduce((n, c) => n + 1 + countDescendants(c), 0)
}

// indexTree da, por archivo, su nodo y su padre en el árbol (ya sin huérfanos
// colgados de un padre inexistente: esos son raíces).
export function indexTree(roots: TreeNode[]): Map<string, {node: TreeNode; parent: TreeNode | null}> {
    const out = new Map<string, {node: TreeNode; parent: TreeNode | null}>()
    const walk = (nodes: TreeNode[], parent: TreeNode | null) => {
        for (const n of nodes) {
            out.set(n.cp.fileName, {node: n, parent})
            walk(n.children, n)
        }
    }
    walk(roots, null)
    return out
}

// latestFile es el checkpoint de número más alto (NNN-slug.scad): el padre del
// próximo cuando no hay un padre activo explícito, igual que backend/checkpoint.Latest.
export function latestFile(list: store.Checkpoint[]): string {
    let best = ''
    let bestN = -1
    for (const c of list) {
        const m = /^(\d+)-/.exec(c.fileName)
        const n = m ? Number(m[1]) : -1
        if (n > bestN || (n === bestN && c.fileName > best)) {
            best = c.fileName
            bestN = n
        }
    }
    return best
}

// effectiveActive es el padre activo que ve el árbol: el explícito si existe en
// el workspace y, si no, el más nuevo.
export function effectiveActive(list: store.Checkpoint[], explicit: string): string {
    return explicit && list.some((c) => c.fileName === explicit) ? explicit : latestFile(list)
}

// activePath es el conjunto de archivos desde la raíz hasta el activo (incluido):
// la rama que el árbol resalta.
export function activePath(index: Map<string, {node: TreeNode; parent: TreeNode | null}>, active: string): Set<string> {
    const out = new Set<string>()
    for (let cur = index.get(active); cur; cur = cur.parent ? index.get(cur.parent.cp.fileName) : undefined) {
        out.add(cur.node.cp.fileName)
    }
    return out
}
