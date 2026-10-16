// Defensa en profundidad contra el desplazamiento del documento y de los
// contenedores de la estructura.
//
// index.css ya pone `overflow: clip` en html/body/#root, que no admite
// desplazamiento programático. Esto cubre el resto: un motor sin `clip`, o una
// extensión del sistema que mueva la página. Si el documento llega a
// desplazarse, vuelve a 0,0 en el mismo cuadro: la cabecera y la barra de
// estado son la app, y no hay barra de desplazamiento con la que recuperarlas.
//
// Los contenedores de la estructura se marcan con `data-lock`:
//   data-lock     (o "all")  no se desplaza nunca: vuelve a 0,0
//   data-lock="x"            solo se desplaza en vertical (la lista de la barra
//                            lateral): el horizontal vuelve a 0
// `overflow-x: clip` no alcanza para las listas: junto a `overflow-y: auto`, el
// navegador convierte el `clip` en `hidden`, y un `hidden` sigue pudiendo
// desplazarse por código (focus() de un nodo del árbol, scrollIntoView, el caret
// de un campo de renombrado más ancho que la lista). El resultado era el
// contenido de la barra lateral corrido unos píxeles a la izquierda («ORKSPACES»).

type LockMode = 'all' | 'x'

function modeOf(el: HTMLElement): LockMode | null {
    const v = el.dataset.lock
    if (v === undefined) return null
    return v === 'x' ? 'x' : 'all'
}

// unlock devuelve el elemento a su sitio; true si hubo que moverlo.
function unlock(el: HTMLElement): boolean {
    const mode = modeOf(el)
    if (!mode) return false
    let moved = false
    if (el.scrollLeft !== 0) {
        el.scrollLeft = 0
        moved = true
    }
    if (mode === 'all' && el.scrollTop !== 0) {
        el.scrollTop = 0
        moved = true
    }
    return moved
}

/** Devuelve a su sitio todos los contenedores marcados; cuántos estaban movidos. */
export function unlockAll(): number {
    let n = 0
    for (const el of document.querySelectorAll<HTMLElement>('[data-lock]')) if (unlock(el)) n++
    return n
}

export function installScrollGuard(): void {
    const reset = () => {
        const d = document.documentElement
        if (d.scrollTop !== 0 || d.scrollLeft !== 0) {
            d.scrollTop = 0
            d.scrollLeft = 0
        }
        const b = document.body
        if (b && (b.scrollTop !== 0 || b.scrollLeft !== 0)) {
            b.scrollTop = 0
            b.scrollLeft = 0
        }
        if (window.scrollX !== 0 || window.scrollY !== 0) window.scrollTo(0, 0)
    }
    window.addEventListener('scroll', reset, {passive: true})
    // Un elemento con overflow:hidden también se puede desplazar por código; el
    // evento `scroll` no sube (burbujea solo en el documento), pero en captura se ve.
    document.addEventListener(
        'scroll',
        (e) => {
            if (e.target instanceof HTMLElement) unlock(e.target)
        },
        {capture: true, passive: true},
    )
    // Enfocar algo dentro de un contenedor puede desplazarlo sin que el evento
    // llegue a tiempo (o, en un motor que no lo emite para `clip`, sin evento): al
    // enfocar y al cambiar de tamaño se repasan todos.
    const sweep = () => {
        reset()
        unlockAll()
    }
    document.addEventListener('focusin', () => requestAnimationFrame(sweep), true)
    window.addEventListener('resize', sweep, {passive: true})
    reset()
}
