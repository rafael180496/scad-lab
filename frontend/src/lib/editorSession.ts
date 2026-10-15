import type {scad} from '../../wailsjs/go/models'
import {createStore} from './store'

// Estado del editor de código que leen otros paneles (barra de estado, panel
// Problemas, avisos de cambios sin guardar). Hay un solo editor en la ventana.
export type EditorSession = {
    line: number
    col: number
    editing: boolean
    dirty: boolean
    lint: scad.Diagnostic[]
    linting: boolean
}

export const editorSession = createStore<EditorSession>({
    line: 1,
    col: 1,
    editing: false,
    dirty: false,
    lint: [],
    linting: false,
})

// Comandos que expone el editor montado (entra con lazy(): hasta que carga,
// no hay a quién pedirle nada y las llamadas se ignoran).
export type EditorCommands = {
    revealLine: (line: number, col?: number) => void
    format: () => Promise<void>
    find: (replace: boolean) => void
    focus: () => void
    getValue: () => string
}

let commands: EditorCommands | null = null
// Una línea pedida antes de que el editor terminara de cargar se aplica al
// montarlo (clic en un diagnóstico con el editor todavía en camino).
let pendingLine: {line: number; col?: number} | null = null

export function registerEditor(c: EditorCommands | null) {
    commands = c
    if (c && pendingLine) {
        c.revealLine(pendingLine.line, pendingLine.col)
        pendingLine = null
    }
}

export function editorCommands(): EditorCommands | null {
    return commands
}

// Antes de ir a una línea hay que ver el editor: si su panel está oculto, quien
// lo registra (PanelHost) lo muestra.
let beforeGoTo: (() => void) | null = null
export function onBeforeGoToLine(fn: (() => void) | null) {
    beforeGoTo = fn
}

export function goToLine(line: number, col?: number) {
    if (line <= 0) return
    beforeGoTo?.()
    if (commands) commands.revealLine(line, col)
    else pendingLine = {line, col}
}
