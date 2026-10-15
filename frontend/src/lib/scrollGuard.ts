// Defensa en profundidad contra el desplazamiento del documento.
//
// index.css ya pone `overflow: clip` en html/body/#root, que no admite
// desplazamiento programático. Esto cubre el resto: un motor sin `clip`, o una
// extensión del sistema que mueva la página. Si el documento llega a
// desplazarse, vuelve a 0,0 en el mismo cuadro: la cabecera y la barra de
// estado son la app, y no hay barra de desplazamiento con la que recuperarlas.
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
    // Un elemento con overflow:hidden también se puede desplazar por código;
    // los contenedores de la estructura (marcados data-lock) vuelven a cero.
    document.addEventListener(
        'scroll',
        (e) => {
            const el = e.target
            if (el instanceof HTMLElement && el.dataset.lock !== undefined && (el.scrollTop !== 0 || el.scrollLeft !== 0)) {
                el.scrollTop = 0
                el.scrollLeft = 0
            }
        },
        {capture: true, passive: true},
    )
    reset()
}
