// Cuánto ancho tiene el panel del chat, en cuatro escalones. El panel vive en el
// dock y se redimensiona (de ~320 px a toda la ventana), así que la barra superior
// y la caja de mensaje no miran la ventana sino SU panel (ResizeObserver) y, como
// useNarrow, lo comparan a tamaño de interfaz 100 %: con la letra más grande los
// mismos rótulos ocupan más y tienen que plegarse antes.
//
//   wide     ≥ 900 px   todo a la vista: título, proveedor, latencia, modelo, chip del workspace, modo y «Chat nuevo»
//   mid      ≥ 720 px   el chip del workspace y «Chat nuevo» pasan al menú «…»
//   narrow   ≥ 480 px   sin la píldora de latencia (queda el punto) y el modo solo ícono
//   tiny     < 480 px   proveedor y modelo solo ícono, el modo pasa al menú
export type ChatTier = 'wide' | 'mid' | 'narrow' | 'tiny'

export const TIER_LIMITS = {wide: 900, mid: 720, narrow: 480} as const

export function tierFor(width: number): ChatTier {
    return width >= TIER_LIMITS.wide ? 'wide' : width >= TIER_LIMITS.mid ? 'mid' : width >= TIER_LIMITS.narrow ? 'narrow' : 'tiny'
}
