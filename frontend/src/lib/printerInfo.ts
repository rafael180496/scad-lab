import type {printers} from '../../wailsjs/go/models'
import {fmt, locale, type Dict} from '../i18n'
import {maxColorsOf} from './printerCatalog'

// Lo que la interfaz sabe decir de una impresora del catálogo (S14): volumen y colores en texto, con el
// idioma de la interfaz. Los cálculos puros (agrupar por marca, acotar colores, elegir slicer) están en
// printerCatalog.ts; se reexportan acá para que quien arma textos importe de un solo lugar.
export {defaultColorsOf, groupByBrand, installedSlicer, maxColorsOf, recommendedSlicer, swapBrandSkill} from './printerCatalog'
export type {BrandGroup} from './printerCatalog'

/** Un número con la coma o el punto del idioma de la interfaz (1,75 / 1.75). */
export const num = (n: number) => n.toLocaleString(locale(), {maximumFractionDigits: 3})

export const volumeLine = (t: Dict, p: printers.Printer) => fmt(t.printers.volume, {x: p.build_volume.x, y: p.build_volume.y, z: p.build_volume.z})

/** «hasta 16 colores · Bambu AMS», o «1 color» si la máquina no tiene multicolor. */
export function colorsLine(t: Dict, p: printers.Printer): string {
    const n = maxColorsOf(p)
    if (n <= 1) return t.printers.singleColor
    return p.multicolor_system ? fmt(t.printers.colorsUpTo, {n, system: p.multicolor_system}) : fmt(t.printers.colorsUpToNoSys, {n})
}

/** «3 colores» / «1 color». */
export const colorsCount = (t: Dict, n: number) => (n === 1 ? t.printers.colorOne : fmt(t.printers.colorMany, {n}))

