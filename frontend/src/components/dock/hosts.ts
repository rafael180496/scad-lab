import type {PanelId} from './panels'

// Elementos persistentes donde vive el contenido de cada panel (PanelHost lo
// dibuja ahí con un portal). dockview solo reserva un hueco (Slot) y le cuelga
// este elemento; al ocultar o mover el panel el elemento se aparca fuera de
// pantalla, con todo lo que tiene dentro (contexto WebGL del visor, modelo y
// deshacer de Monaco, xterm) intacto.
const hosts = new Map<PanelId, HTMLDivElement>()
let parking: HTMLDivElement | null = null

function parkingLot(): HTMLDivElement {
    if (!parking) {
        parking = document.createElement('div')
        parking.hidden = true
        parking.style.display = 'none'
        parking.dataset.dockParking = ''
        document.body.appendChild(parking)
    }
    return parking
}

export function panelHostEl(id: PanelId): HTMLDivElement {
    let el = hosts.get(id)
    if (!el) {
        el = document.createElement('div')
        el.dataset.panelHost = id
        el.style.cssText = 'height:100%;width:100%;min-height:0;min-width:0;position:relative;overflow:clip'
        hosts.set(id, el)
        parkingLot().appendChild(el)
    }
    return el
}

export function parkHost(el: HTMLElement) {
    parkingLot().appendChild(el)
}
