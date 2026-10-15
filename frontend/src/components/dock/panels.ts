import {Code2, Cuboid, ListTodo, MessageSquare, SquareTerminal, type LucideIcon} from 'lucide-react'
import {fmt, type Dict} from '../../i18n'

// Catálogo de paneles acoplables (S4b). Agregar uno = una entrada acá, su
// contenido en PanelHost.tsx, sus textos en i18n (dock.*) y, si lleva atajo,
// su número en PANEL_SHORTCUT. Ver claude/04-frontend.md, «Paneles acoplables».
export const PANEL_IDS = ['viewport', 'source', 'agent-1', 'agent-2', 'chat', 'problems'] as const
export type PanelId = (typeof PANEL_IDS)[number]

export function isPanelId(v: unknown): v is PanelId {
    return typeof v === 'string' && (PANEL_IDS as readonly string[]).includes(v)
}

export const PANEL_ICON: Record<PanelId, LucideIcon> = {
    viewport: Cuboid,
    source: Code2,
    'agent-1': SquareTerminal,
    'agent-2': SquareTerminal,
    chat: MessageSquare,
    problems: ListTodo,
}

// ⌘/Ctrl+Shift+N muestra y enfoca el panel (si ya está enfocado, lo oculta).
export const PANEL_SHORTCUT: Record<PanelId, number> = {
    viewport: 1,
    source: 2,
    'agent-1': 3,
    'agent-2': 4,
    chat: 5,
    problems: 6,
}

// Tamaño mínimo (px a interfaz 100 %; se multiplica por la escala) para que
// ningún panel quede inutilizable al arrastrar un divisor: el visor 200×150,
// el código y las terminales 240×120.
export const PANEL_MIN: Record<PanelId, {w: number; h: number}> = {
    viewport: {w: 200, h: 150},
    source: {w: 240, h: 120},
    'agent-1': {w: 240, h: 120},
    'agent-2': {w: 240, h: 120},
    chat: {w: 240, h: 120},
    problems: {w: 200, h: 90},
}

export function panelTitle(t: Dict, id: PanelId): string {
    switch (id) {
        case 'viewport':
            return t.panes.viewport
        case 'source':
            return t.panes.editor
        case 'agent-1':
            return fmt(t.terminal.agentTab, {n: 1})
        case 'agent-2':
            return fmt(t.terminal.agentTab, {n: 2})
        case 'chat':
            return t.terminal.chatTab
        case 'problems':
            return t.problems.title
    }
}

export function panelHint(t: Dict, id: PanelId): string {
    switch (id) {
        case 'viewport':
            return t.dock.hintViewport
        case 'source':
            return t.dock.hintSource
        case 'agent-1':
            return fmt(t.terminal.agentTabHint, {n: 1})
        case 'agent-2':
            return fmt(t.terminal.agentTabHint, {n: 2})
        case 'chat':
            return t.terminal.chatTabHint
        case 'problems':
            return t.layout.problemsTabHint
    }
}
