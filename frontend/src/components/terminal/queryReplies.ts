// Respuestas que xterm.js genera SOLO ante las consultas que un programa le
// hace a la terminal (qué tipo de terminal eres, dónde está el cursor, de qué
// color es el fondo…). Salen por `onData`, igual que lo que teclea la persona, y
// TerminalView las reenvía al PTY: es lo que contesta la consulta de un agente
// que arranca.
//
// El problema es el reenganche. Al volver a una terminal, `ReadTerminalBuffer`
// reescribe en xterm lo que ya había pasado —consultas incluidas— y xterm las
// contesta OTRA VEZ, ahora que el agente ya terminó su arranque: lo que llega
// por el PTY se lee como texto tecleado, y aparece `^[[?1;2c` en su primera línea.
//
// Mientras se reproduce lo ya visto (la captura y los eventos que llegaron
// antes de engancharse) se descartan estas respuestas; lo que la persona teclee
// en esos milisegundos pasa intacto. Solo contesta la vista que recibe los
// datos nuevos.
//
// Es TS sin sintaxis de tipos especial a propósito: `node` lo ejecuta tal cual
// (scripts/terminal-replies.test.mjs) sin pasar por el compilador.

const ESC = '\\x1b'
const BEL = '\\x07'

// Cada patrón es una respuesta completa de xterm. Los de teclado (flechas,
// Ctrl+flechas, `ESC[200~` del pegado, F1–F12, Mayús+Tab) no están acá.
const REPLIES: RegExp[] = [
    // DA1 y DA2: «¿qué terminal eres?» → ESC [ ? 1 ; 2 c  /  ESC [ > 0 ; 276 ; 0 c
    new RegExp(`${ESC}\\[[?>][0-9;]*c`, 'g'),
    // CPR: «¿dónde está el cursor?» → ESC [ fila ; columna R (con ? la variante DEC)
    new RegExp(`${ESC}\\[\\??[0-9]+;[0-9]+R`, 'g'),
    // DSR de estado: ESC [ 0 n
    new RegExp(`${ESC}\\[[0-9]n`, 'g'),
    // OSC 10/11/12: colores de texto, fondo y cursor → ESC ] 11 ; rgb:… (BEL | ESC \)
    new RegExp(`${ESC}\\](?:10|11|12);[^${BEL}${ESC}]*(?:${BEL}|${ESC}\\\\)`, 'g'),
    // DECRPM: «¿está activo este modo?» → ESC [ ? 2026 ; 2 $ y
    new RegExp(`${ESC}\\[\\??[0-9;]*\\$y`, 'g'),
    // XTVERSION: nombre y versión de la terminal → ESC P > | xterm.js(6.0.0) ESC \
    new RegExp(`${ESC}P>\\|[^${ESC}]*${ESC}\\\\`, 'g'),
]

/** Quita de `data` las respuestas automáticas de xterm; el resto queda igual. */
export function stripQueryReplies(data: string): string {
    let out = data
    for (const re of REPLIES) out = out.replace(re, '')
    return out
}
