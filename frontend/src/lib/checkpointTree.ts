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
