import {EventsOn} from '../../../wailsjs/runtime/runtime'
import type {main} from '../../../wailsjs/go/models'
import {createStore} from '../../lib/store'

// Estado de las respuestas en curso, por chat. Vive a nivel de módulo y no en
// el componente por dos razones:
//
//  - Cada trozo llega por un evento de Wails (~25 por segundo). Guardarlo en el
//    estado de ChatPanel redibujaría la conversación entera con cada uno; acá
//    solo se suscribe a un chat la burbuja que muestra su texto.
//  - La pestaña Chat puede desmontarse a mitad de una respuesta. Los eventos
//    siguen llegando y, al volver, el panel retoma donde iba (GetChat trae lo
//    escrito hasta ahí; ver syncStream).
//
// Contrato de eventos con app_chat.go: chat:delta {chatId, text, seq},
// chat:done {chatId, messageId, cancelled}, chat:error {chatId, message, messageId}.

export type Stream = {
    running: boolean
    /** Texto de la respuesta recibido hasta ahora. */
    text: string
    /** Número del último trozo aplicado a `text`. */
    seq: number
    /**
     * `text` es confiable: el stream nació acá (arranca en 0) o ya se alineó con
     * el servidor. Un stream que se ve a mitad de camino (el panel se abrió con
     * la respuesta en curso) acumula en `pending` hasta alinearse.
     */
    synced: boolean
    pending: {seq: number; text: string}[]
    error: string | null
    /** Mensaje guardado al terminar (0 = no hubo texto que guardar). */
    messageId: number
    finished: boolean
}

type State = {byChat: Record<number, Stream>}
const store = createStore<State>({byChat: {}})

const fresh = (synced: boolean): Stream => ({
    running: true,
    text: '',
    seq: 0,
    synced,
    pending: [],
    error: null,
    messageId: 0,
    finished: false,
})

function patch(chatId: number, fn: (s: Stream) => Stream, create?: () => Stream) {
    const cur = store.get().byChat[chatId] ?? create?.()
    if (!cur) return
    store.set({byChat: {...store.get().byChat, [chatId]: fn(cur)}})
}

/** El usuario acaba de enviar un mensaje: la respuesta arranca desde cero. */
export function startStream(chatId: number) {
    store.set({byChat: {...store.get().byChat, [chatId]: fresh(true)}})
}

/** Olvida el estado del chat (ya se recargó la conversación guardada). */
export function clearStream(chatId: number) {
    const {[chatId]: _drop, ...rest} = store.get().byChat
    store.set({byChat: rest})
}

/** Descarta solo el aviso de error, sin tocar el resto. */
export function dismissError(chatId: number) {
    patch(chatId, (s) => ({...s, error: null}))
}

/**
 * Alinea el estado con lo que dice GetChat. Si la respuesta sigue en curso y
 * este stream se enteró tarde, toma el texto del servidor y le suma los trozos
 * que llegaron después de esa foto (seq mayor).
 */
export function syncStream(chatId: number, v: main.ChatView) {
    const cur = store.get().byChat[chatId]
    if (!v.running) return
    if (cur?.synced) return
    const pending = cur?.pending ?? []
    let text = v.partial
    let seq = v.partialSeq
    for (const p of pending) {
        if (p.seq > seq) {
            text += p.text
            seq = p.seq
        }
    }
    patch(chatId, (s) => ({...s, running: true, text, seq, synced: true, pending: []}), () => fresh(false))
}

let wired = false
/** Suscribe los tres eventos una sola vez por sesión de la app. */
export function ensureChatEvents() {
    if (wired) return
    wired = true
    EventsOn('chat:delta', (e: {chatId: number; text: string; seq: number}) => {
        patch(
            e.chatId,
            (s) => {
                if (!s.synced) return {...s, pending: [...s.pending, {seq: e.seq, text: e.text}]}
                if (e.seq <= s.seq) return s // ya incluido en lo que mostró GetChat
                return {...s, text: s.text + e.text, seq: e.seq}
            },
            () => fresh(false),
        )
    })
    EventsOn('chat:done', (e: {chatId: number; messageId: number}) => {
        patch(e.chatId, (s) => ({...s, running: false, finished: true, messageId: e.messageId}), () => fresh(false))
    })
    EventsOn('chat:error', (e: {chatId: number; message: string; messageId: number}) => {
        patch(e.chatId, (s) => ({...s, running: false, finished: true, error: e.message, messageId: e.messageId}), () => fresh(false))
    })
}

// Hooks: cada uno selecciona un valor primitivo, así que un componente solo se
// redibuja cuando cambia lo que de verdad lee.
export const useStreamText = (chatId: number | null) => store.use((s) => (chatId == null ? '' : (s.byChat[chatId]?.text ?? '')))
export const useStreamRunning = (chatId: number | null) => store.use((s) => (chatId == null ? false : (s.byChat[chatId]?.running ?? false)))
export const useStreamFinished = (chatId: number | null) => store.use((s) => (chatId == null ? false : (s.byChat[chatId]?.finished ?? false)))
export const useStreamError = (chatId: number | null) => store.use((s) => (chatId == null ? null : (s.byChat[chatId]?.error ?? null)))
export const useStreamMessageId = (chatId: number | null) => store.use((s) => (chatId == null ? 0 : (s.byChat[chatId]?.messageId ?? 0)))
