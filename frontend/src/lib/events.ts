import {EventsOn} from '../../wailsjs/runtime/runtime'
import type {main, render} from '../../wailsjs/go/models'

// Eventos que Go emite hacia React (ver app_render.go y
// claude/06-go-react-contract.md). Los payloads llegan como JSON plano, no
// como instancias de las clases de models.ts: se leen campos, nada más.
export type RenderStartedEvent = {workspaceId: number; fileName: string}
export type RenderFinishedEvent = {workspaceId: number; result: render.Result}
// Progreso de «Descargar OpenSCAD» (app_openscad.go). message solo viene con phase = error.
export type OpenSCADDownloadPhase = 'download' | 'verify' | 'extract' | 'done' | 'error' | 'cancelled'
export type OpenSCADDownloadEvent = {phase: OpenSCADDownloadPhase; received: number; total: number; message: string}

type EventMap = {
    'render:started': RenderStartedEvent
    'render:finished': RenderFinishedEvent
    'checkpoints:changed': main.WorkspaceView
    'openscad:download': OpenSCADDownloadEvent
    'mcp:changed': {enabled: boolean}
    // ScadLab se registró (o falló al registrarse) en la configuración global de Antigravity (S12.c) y/o agregó sus
    // herramientas a `permissions.allow` de su settings.json (`permissions` reglas, en `settingsPath`) (T14).
    'mcp:antigravity': {added: boolean; permissions: number; settingsPath: string; error: string}
    // Antigravity bloqueó una herramienta de ScadLab por permisos (T14): el aviso trae «Reparar permisos de Antigravity».
    'mcp:antigravity-denied': {tool: string; settingsPath: string; managed: boolean}
    // Cambió la lista, el orden o el proyecto activo (S8). El payload no se usa: se vuelve a leer.
    'projects:changed': unknown
    // Cambió la configuración de un workspace (S14): el guardado de «Configuración del workspace» o la IA (set_workspace_printer).
    'workspace:config': main.WorkspaceConfigView
}

// onEvent se suscribe a un evento tipado y devuelve la función que corta la
// suscripción (pensado para el return de un useEffect).
export function onEvent<K extends keyof EventMap>(name: K, cb: (payload: EventMap[K]) => void): () => void {
    return EventsOn(name, (payload: EventMap[K]) => cb(payload))
}
