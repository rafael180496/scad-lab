import {Code2, Copy, FileClock, FileDiff, FolderSearch, GitBranch, GitCompare, History, ListCollapse, ListTree, Network, Pencil, Trash2, Undo2, WandSparkles} from 'lucide-react'
import type {store} from '../../../wailsjs/go/models'
import {MakeCheckpointRoot, ResumeCheckpointSession, RevealCheckpoint, SetActiveParent, TrashCheckpoint} from '../../../wailsjs/go/main/App'
import {agentName, resumeKind} from '../../lib/checkpointInfo'
import {countDescendants, type TreeNode} from '../../lib/checkpointTree'
import {copyText} from '../../lib/clipboard'
import {modifyWithAI} from '../../lib/meshEditAI'
import {errorText} from '../../lib/errors'
import {isMac, keys} from '../../lib/platform'
import {openChat, terminalOpened} from '../../lib/sessionBridge'
import {openDiff, openHistory} from '../../lib/historyUI'
import {fmt, getLanguage, useT} from '../../i18n'
import {confirmDialog} from '../ui/Dialog'
import type {MenuEntry} from '../ui/Menu'
import {toast} from '../ui/Toast'
import {undoAIChange} from './historyActions'

// Nombre del checkpoint tal como lo ve quien mira el árbol: su rótulo, o el
// archivo sin extensión.
export function checkpointDisplay(cp: store.Checkpoint): string {
    return cp.label || cp.fileName.replace(/\.scad$/i, '')
}

export type MenuContext = {
    node: TreeNode
    /** Su padre en el árbol (null si es raíz o su padre no está). */
    parent: TreeNode | null
    /** Es el padre activo (ramificar desde aquí no tendría efecto). */
    isActive: boolean
    onView: (file: string) => void
    onCompare: (file: string) => void
    onToggle: (cp: store.Checkpoint) => void
    onRename: () => void
}

// Acciones del menú de un checkpoint y su teclado (F2, Supr). Una sola
// implementación para el menú contextual, el botón «…» y los atajos del árbol.
export function useCheckpointActions(workspaceId: number) {
    const t = useT()
    const c = t.checkpoints

    // Mandar a la papelera: sin rama, los hijos DIRECTOS pasan a colgar del padre
    // (o quedan como raíz) y el diálogo lo dice; con rama, se va todo el árbol de abajo.
    const trash = async (node: TreeNode, parent: TreeNode | null, branch: boolean) => {
        const cp = node.cp
        const name = checkpointDisplay(cp)
        const kids = branch ? countDescendants(node) : node.children.length
        const text = branch
            ? fmt(c.trashBranchConfirm, {name, n: kids})
            : kids > 0
              ? parent
                  ? fmt(c.trashConfirmKids, {name, n: kids, parent: checkpointDisplay(parent.cp)})
                  : fmt(c.trashConfirmKidsRoot, {name, n: kids})
              : fmt(c.trashConfirm, {name})
        if (!(await confirmDialog(text, c.trashOk, c.trashCancel))) return
        try {
            const moved = await TrashCheckpoint(workspaceId, cp.fileName, branch)
            toast.ok(fmt(c.trashed, {n: moved?.length ?? 0}))
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const makeRoot = async (file: string) => {
        try {
            await MakeCheckpointRoot(workspaceId, file)
            toast.ok(fmt(c.madeRoot, {file}))
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const branchFrom = async (file: string) => {
        try {
            await SetActiveParent(workspaceId, file)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    // La terminal se abre en Go; el chat lo abre el panel.
    const resume = async (file: string) => {
        try {
            const r = await ResumeCheckpointSession(workspaceId, file)
            if (r.kind === 'terminal') terminalOpened(r.terminalId)
            else openChat(r.chatId)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const copyName = async (file: string) => {
        if (await copyText(file)) toast.ok(c.menu.copied)
    }

    const reveal = (file: string) => {
        RevealCheckpoint(workspaceId, file).catch((e) => toast.error(errorText(e)))
    }

    const menuEntries = ({node, parent, isActive, onView, onCompare, onToggle, onRename}: MenuContext): MenuEntry[] => {
        const cp = node.cp
        const file = cp.fileName
        const kids = countDescendants(node)
        const m = c.menu
        const h = t.history
        const canCompare = !!cp.parentFile && !node.orphan
        const kind = resumeKind(cp)
        const del = isMac() ? '⌫' : getLanguage() === 'es' ? 'Supr' : 'Del'

        const resumeEntry: MenuEntry =
            kind === 'agent'
                ? {id: 'resume', label: fmt(m.resumeAgent, {agent: agentName(cp.agentId)}), title: fmt(m.resumeAgentHint, {agent: agentName(cp.agentId)}), icon: <History className="icon-ui-14" />, onSelect: () => void resume(file)}
                : kind === 'chat'
                  ? {id: 'resume', label: m.resumeChat, title: m.resumeChatHint, icon: <History className="icon-ui-14" />, onSelect: () => void resume(file)}
                  : {id: 'resume', label: m.resumeNone, title: m.resumeNoneHint, icon: <History className="icon-ui-14" />, disabled: true}

        const entries: MenuEntry[] = [
            {id: 'view', label: m.view, title: fmt(m.viewHint, {file}), icon: <Code2 className="icon-ui-14" />, onSelect: () => onView(file)},
            {
                id: 'compare',
                label: m.compare,
                title: canCompare ? fmt(m.compareHint, {parent: cp.parentFile}) : cp.parentFile ? fmt(m.compareOrphan, {parent: cp.parentFile}) : m.compareRoot,
                icon: <GitCompare className="icon-ui-14" />,
                disabled: !canCompare,
                onSelect: () => onCompare(file),
            },
            {
                id: 'modify-ai',
                label: m.modifyAI,
                title: m.modifyAIHint,
                icon: <WandSparkles className="icon-ui-14" />,
                onSelect: () => void modifyWithAI(workspaceId, file, t),
            },
            // Historial de versiones (S16). «Deshacer» y «Ver diferencias» solo si el último cambio del archivo lo hizo la IA.
            ...(cp.editedByAI
                ? [
                      {
                          id: 'undo-ai',
                          label: h.undo,
                          title: fmt(h.undoHint, {file}),
                          icon: <Undo2 className="icon-ui-14" />,
                          onSelect: () => void undoAIChange(workspaceId, file, t),
                      } satisfies MenuEntry,
                      {
                          id: 'history-diff',
                          label: h.diff,
                          title: fmt(h.diffHint, {file}),
                          icon: <FileDiff className="icon-ui-14" />,
                          onSelect: () => openDiff(workspaceId, file),
                      } satisfies MenuEntry,
                  ]
                : []),
            {id: 'history', label: h.open, title: fmt(h.openHint, {file}), icon: <FileClock className="icon-ui-14" />, onSelect: () => openHistory(workspaceId, file)},
            {kind: 'separator'},
            {
                id: 'branch',
                label: m.branch,
                title: isActive ? m.branchActive : m.branchHint,
                icon: <GitBranch className="icon-ui-14" />,
                disabled: isActive,
                onSelect: () => void branchFrom(file),
            },
            ...(node.orphan
                ? [
                      {
                          id: 'make-root',
                          label: m.makeRoot,
                          title: fmt(m.makeRootHint, {parent: cp.parentFile}),
                          icon: <Network className="icon-ui-14" />,
                          onSelect: () => void makeRoot(file),
                      } satisfies MenuEntry,
                  ]
                : []),
            {id: 'rename', label: m.rename, title: m.renameHint, icon: <Pencil className="icon-ui-14" />, keys: keys('f2'), onSelect: onRename},
            resumeEntry,
            {kind: 'separator'},
        ]
        if (node.children.length > 0) {
            entries.push({
                id: 'fold',
                label: cp.collapsed ? m.unfold : m.fold,
                title: cp.collapsed ? m.unfoldHint : m.foldHint,
                icon: <ListTree className="icon-ui-14" />,
                onSelect: () => onToggle(cp),
            })
        }
        entries.push(
            {id: 'copy', label: m.copy, title: m.copyHint, icon: <Copy className="icon-ui-14" />, onSelect: () => void copyName(file)},
            {id: 'reveal', label: isMac() ? m.revealMac : m.revealWin, title: m.revealHint, icon: <FolderSearch className="icon-ui-14" />, onSelect: () => reveal(file)},
            {kind: 'separator'},
            {id: 'trash', label: m.trash, title: m.trashHint, icon: <Trash2 className="icon-ui-14" />, keys: del, danger: true, onSelect: () => void trash(node, parent, false)},
        )
        if (kids > 0) {
            entries.push({
                id: 'trash-branch',
                label: fmt(m.trashBranch, {n: kids}),
                title: fmt(m.trashBranchHint, {n: kids}),
                icon: <ListCollapse className="icon-ui-14" />,
                danger: true,
                onSelect: () => void trash(node, parent, true),
            })
        }
        return entries
    }

    return {trash, menuEntries}
}
