import type {hermes} from '../../wailsjs/go/models'
import {showPanel} from '../components/dock/dock'

// Puente entre «Retomar sesión» (el árbol de checkpoints, en la barra lateral) y
// los paneles que muestran la sesión: la terminal de agente y el chat. Los dos
// viven en otro árbol de React y el chat entra con lazy(), así que no se pasan
// props: el árbol avisa acá y quien corresponda se suscribe.

type Listener<T> = (v: T) => void

function channel<T>() {
    const subs = new Set<Listener<T>>()
    return {
        on(cb: Listener<T>) {
            subs.add(cb)
            return () => {
                subs.delete(cb)
            }
        },
        emit(v: T) {
            subs.forEach((s) => s(v))
            return subs.size > 0
        },
    }
}

// Una terminal de agente que Go abrió por su cuenta (ResumeCheckpointSession):
// useTerminals la reengancha a una de las dos pestañas «Agente».
const terminals = channel<string>()
export const onTerminalOpened = terminals.on
export function terminalOpened(id: string) {
    terminals.emit(id)
}

// Un chat que hay que abrir. El panel puede no haberse montado todavía (lazy):
// el pedido queda pendiente hasta que se monta y lo toma.
const chats = channel<number>()
let pendingChat: number | null = null
export const onOpenChat = chats.on
export function takePendingChat(): number | null {
    const id = pendingChat
    pendingChat = null
    return id
}
export function openChat(id: number) {
    pendingChat = id
    showPanel('chat')
    if (chats.emit(id)) pendingChat = null
}

// Se cerró la pestaña de un proyecto (S8): quien guarda estado por workspace lo
// suelta (las terminales del proyecto, el chat elegido, la parte seleccionada).
const closedProjects = channel<number>()
export const onProjectClosed = closedProjects.on
export function projectClosed(workspaceId: number) {
    closedProjects.emit(workspaceId)
}

// Un borrador para el chat (S13, «Modificar con IA»): un modelo ya adjuntado en Go y un texto sugerido para el
// compositor. El panel Chat entra con lazy(), así que el pedido queda pendiente hasta que se monta y lo toma
// (igual que openChat).
export type ChatDraft = {workspaceId: number; text: string; attachment: hermes.Attachment}
const drafts = channel<ChatDraft>()
let pendingDraft: ChatDraft | null = null
export const onChatDraft = drafts.on
export function takePendingDraft(): ChatDraft | null {
    const d = pendingDraft
    pendingDraft = null
    return d
}
export function requestChatDraft(d: ChatDraft) {
    pendingDraft = d
    showPanel('chat')
    if (drafts.emit(d)) pendingDraft = null
}
