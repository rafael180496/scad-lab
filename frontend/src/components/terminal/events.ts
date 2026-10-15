import {EventsOn} from '../../../wailsjs/runtime/runtime'

// Eventos de la terminal (Go → React), ver backend/pty. Viven acá y no en
// lib/events.ts para que este módulo no dependa de un archivo que comparten
// otras fases. Los payloads llegan como JSON plano.
export type TerminalDataEvent = {id: string; data: string; seq: number} // data: base64
export type TerminalExitEvent = {id: string; code: number}

export function onTerminalData(cb: (e: TerminalDataEvent) => void): () => void {
    return EventsOn('terminal:data', cb)
}

export function onTerminalExit(cb: (e: TerminalExitEvent) => void): () => void {
    return EventsOn('terminal:exit', cb)
}

// base64ToBytes decodifica los bytes crudos del PTY (pueden no ser UTF-8
// válido: por eso viajan en base64 y xterm los recibe como Uint8Array).
export function base64ToBytes(b64: string): Uint8Array {
    const bin = atob(b64)
    const out = new Uint8Array(bin.length)
    for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
    return out
}
