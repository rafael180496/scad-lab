import {useEffect, useState} from 'react'
import {appearance} from '../lib/appearance'

// useNarrow dice si un elemento mide menos de `limit` px **a tamaño de
// interfaz 100 %**: el límite se multiplica por la escala, porque con la letra
// más grande los mismos rótulos ocupan más y tienen que pasar a solo ícono
// antes. Es lo que permite que una barra degrade (rótulos → íconos con `title`)
// en ventanas angostas o con la interfaz agrandada en vez de cortar controles.
//
// Se mide con ResizeObserver sobre el propio elemento (no sobre la ventana):
// una barra dentro de un panel redimensionable se compacta cuando SU panel se
// achica, no la ventana.
//
// Devuelve `[narrow, ref]` con un ref **de callback**: el elemento puede montarse
// después del primer render (un panel que solo existe con un archivo abierto) y
// un `useRef` no avisaría.
export function useNarrow(limit: number): [boolean, (el: HTMLElement | null) => void] {
    const [el, setEl] = useState<HTMLElement | null>(null)
    const [width, setWidth] = useState(Infinity)
    const scale = appearance.use((s) => s.uiScale) / 100
    useEffect(() => {
        if (!el) return
        setWidth(el.getBoundingClientRect().width)
        const ro = new ResizeObserver(([e]) => setWidth(e.borderBoxSize?.[0]?.inlineSize ?? e.contentRect.width))
        ro.observe(el)
        return () => ro.disconnect()
    }, [el])
    return [width < limit * scale, setEl]
}
