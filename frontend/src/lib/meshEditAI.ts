import {AddChatAttachment, ListWorkspaces, PrepareMeshEdit} from '../../wailsjs/go/main/App'
import {toast} from '../components/ui/Toast'
import {fmt, type Dict} from '../i18n'
import {errorText} from './errors'
import {requestChatDraft} from './sessionBridge'

// «Modificar con IA» (S13): le deja al chat el modelo de un checkpoint —ya adjunto— y un texto para empezar.
//
// Go congela la malla del checkpoint en refs/ (PrepareMeshEdit; si es un import base, es el STL original) y el
// adjunto del chat (AddChatAttachment) es el mismo mecanismo de S12: analiza el modelo, lo deja en refs/ y manda
// su informe y su vista previa. El proveedor lo pone quien ya estaba elegido en el chat (Hermes o un CLI).
// No se manda nada: el texto queda escrito en el compositor para que la persona lo termine.
export async function modifyWithAI(workspaceId: number, file: string, t: Dict): Promise<void> {
    toast.ok(t.meshEdit.ai.preparing, 'mesh-ai')
    try {
        const base = await PrepareMeshEdit(workspaceId, file)
        const ws = (await ListWorkspaces()).find((w) => w.id === workspaceId)
        if (!ws) throw new Error(t.meshEdit.ai.noWorkspace)
        // Con «/»: Go limpia la ruta con filepath.Clean, que en Windows la deja con «\».
        const attachment = await AddChatAttachment(workspaceId, `${ws.path}/${base.ref}`)
        requestChatDraft({workspaceId, attachment, text: t.meshEdit.ai.draft})
    } catch (e) {
        toast.error(fmt(t.meshEdit.ai.error, {msg: errorText(e)}), 'mesh-ai')
    }
}
