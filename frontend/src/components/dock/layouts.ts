import type {AddPanelOptions, DockviewApi, SerializedDockview} from 'dockview-react'
import {PANEL_IDS, PANEL_MIN, isPanelId, type PanelId} from './panels'

// Diseños de los paneles acoplables: los presets, y el sobre JSON con el que se
// guarda lo que arma dockview (setting `dockLayout` y `dockLayouts` en Go).

export const PRESET_IDS = ['default', 'modeling', 'codeAI', 'split'] as const
export type PresetId = (typeof PRESET_IDS)[number]

// Versión del sobre. Si cambia la forma de lo guardado, se sube y los diseños
// viejos caen al predeterminado en vez de romper la ventana.
export const LAYOUT_VERSION = 1
export const CONTENT_COMPONENT = 'slot'

export type Envelope = {v: number; dock: SerializedDockview}

export function toEnvelope(api: DockviewApi): string {
    return JSON.stringify({v: LAYOUT_VERSION, dock: api.toJSON()} satisfies Envelope)
}

// parseEnvelope devuelve lo que dockview puede cargar, o null si el texto está
// roto, es de otra versión o trae paneles que no conocemos (un diseño de una
// versión futura o editado a mano). Cargarlo igual dejaría paneles sin
// contenido; es mejor caer al predeterminado.
export function parseEnvelope(json: string | undefined | null): SerializedDockview | null {
    if (!json) return null
    let data: unknown
    try {
        data = JSON.parse(json)
    } catch {
        return null
    }
    if (typeof data !== 'object' || data === null) return null
    const env = data as Partial<Envelope>
    if (env.v !== LAYOUT_VERSION || typeof env.dock !== 'object' || env.dock === null) return null
    const dock = env.dock
    if (typeof dock.grid !== 'object' || dock.grid === null || typeof dock.panels !== 'object' || dock.panels === null) return null
    for (const [id, p] of Object.entries(dock.panels)) {
        if (!isPanelId(id) || p?.id !== id || p?.contentComponent !== CONTENT_COMPONENT) return null
    }
    return dock
}

// Panel de ancho/alto mínimo escalado con la interfaz.
export function minimums(id: PanelId, scale: number): {minimumWidth: number; minimumHeight: number} {
    const m = PANEL_MIN[id]
    return {minimumWidth: Math.round(m.w * scale), minimumHeight: Math.round(m.h * scale)}
}

type Add = (id: PanelId, position?: AddPanelOptions['position']) => void

// buildPreset arma un diseño sobre un dock vacío. Los tamaños son fracciones del
// área disponible (no píxeles fijos) para que el diseño se vea parecido en
// cualquier ventana.
export function buildPreset(api: DockviewApi, preset: PresetId, scale: number): void {
    const W = api.width
    const H = api.height
    const add: Add = (id, position) => {
        api.addPanel({
            id,
            component: CONTENT_COMPONENT,
            title: id,
            renderer: 'always',
            ...minimums(id, scale),
            ...(position ? {position} : {}),
        } as AddPanelOptions)
    }
    const size = (id: PanelId, s: {width?: number; height?: number}) => api.getPanel(id)?.group.api.setSize(s)
    const within = (id: PanelId, ref: PanelId) => add(id, {referencePanel: ref, direction: 'within'})
    const tabs = (ref: PanelId, ids: PanelId[]) => ids.forEach((id) => within(id, ref))

    switch (preset) {
        case 'default':
            // Como era antes: visor arriba a la izquierda, código a la derecha en
            // toda la altura y agentes / chat / problemas debajo del visor.
            add('viewport')
            add('source', {referencePanel: 'viewport', direction: 'right'})
            add('agent-1', {referencePanel: 'viewport', direction: 'below'})
            tabs('agent-1', ['agent-2', 'chat', 'problems'])
            size('source', {width: Math.round(W * 0.34)})
            size('agent-1', {height: Math.round(H * 0.3)})
            api.getPanel('agent-1')?.api.setActive()
            break
        case 'modeling':
            // Visor grande; código y agentes como pestañas a la derecha.
            add('viewport')
            add('source', {referencePanel: 'viewport', direction: 'right'})
            tabs('source', ['agent-1', 'agent-2', 'chat', 'problems'])
            size('source', {width: Math.round(W * 0.36)})
            api.getPanel('source')?.api.setActive()
            break
        case 'codeAI':
            // Código grande, el agente al lado y el visor chico arriba del agente.
            add('source')
            add('agent-1', {referencePanel: 'source', direction: 'right'})
            add('viewport', {referencePanel: 'agent-1', direction: 'above'})
            tabs('agent-1', ['agent-2', 'chat', 'problems'])
            size('agent-1', {width: Math.round(W * 0.46)})
            size('viewport', {height: Math.round(H * 0.36)})
            api.getPanel('agent-1')?.api.setActive()
            break
        case 'split':
            // Visor y código mitad y mitad; los agentes abajo, en todo el ancho.
            add('viewport')
            add('source', {referencePanel: 'viewport', direction: 'right'})
            add('agent-1', {direction: 'below'})
            tabs('agent-1', ['agent-2', 'chat', 'problems'])
            size('viewport', {width: Math.round(W * 0.5)})
            size('agent-1', {height: Math.round(H * 0.3)})
            api.getPanel('agent-1')?.api.setActive()
            break
    }
}

export function isFullLayout(api: DockviewApi): boolean {
    return PANEL_IDS.every((id) => !!api.getPanel(id))
}
