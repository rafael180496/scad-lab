import {EventsOn} from '../../wailsjs/runtime/runtime'
import type {main, render} from '../../wailsjs/go/models'

// Eventos que Go emite hacia React (ver app_render.go y
// claude/06-go-react-contract.md). Los payloads llegan como JSON plano, no
// como instancias de las clases de models.ts: se leen campos, nada más.
export type RenderStartedEvent = {workspaceId: number; fileName: string}
export type RenderFinishedEvent = {workspaceId: number; result: render.Result}

type EventMap = {
    'render:started': RenderStartedEvent
    'render:finished': RenderFinishedEvent
    'checkpoints:changed': main.WorkspaceView
    'mcp:changed': {enabled: boolean}
    // Cambió la lista, el orden o el proyecto activo (S8). El payload no se usa: se vuelve a leer.
    'projects:changed': unknown
}

// onEvent se suscribe a un evento tipado y devuelve la función que corta la
// suscripción (pensado para el return de un useEffect).
export function onEvent<K extends keyof EventMap>(name: K, cb: (payload: EventMap[K]) => void): () => void {
    return EventsOn(name, (payload: EventMap[K]) => cb(payload))
}
