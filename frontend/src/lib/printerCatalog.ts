import type {main, printers} from '../../wailsjs/go/models'

// Lo que la interfaz calcula del catálogo de impresoras (S14) sin tocar texto ni React: agrupar por marca,
// acotar los colores como `Printer.Colors` en Go, elegir el slicer y cambiar la skill de marca de una lista.
// Va aparte de printerInfo.ts (que arma textos con i18n) para poder probarlo con `pnpm test` en Node.

export type BrandGroup = {id: string; name: string; skill: string; items: printers.Printer[]}

/** Las impresoras agrupadas por marca, en el orden de `ListPrinterBrands`; dentro de cada una, el de `ListPrinters`. */
export function groupByBrand(list: printers.Printer[], brands: printers.Brand[]): BrandGroup[] {
    const out: BrandGroup[] = []
    const seen = new Set<string>()
    for (const b of brands) {
        seen.add(b.id)
        const items = list.filter((p) => p.brand_id === b.id)
        if (items.length) out.push({id: b.id, name: b.name, skill: b.skill, items})
    }
    // Una marca que el catálogo de marcas no nombra no debería existir; si pasa, sus impresoras
    // no desaparecen: van al final bajo el nombre que traen.
    for (const p of list) {
        if (seen.has(p.brand_id)) continue
        seen.add(p.brand_id)
        out.push({id: p.brand_id, name: p.brand_name || p.brand_id, skill: p.skill, items: list.filter((x) => x.brand_id === p.brand_id)})
    }
    return out
}

/** Tope de colores de la máquina (≥ 1). */
export const maxColorsOf = (p: Pick<printers.Printer, 'max_colors' | 'multicolor_slots'> | undefined) =>
    Math.max(1, p?.max_colors || p?.multicolor_slots || 1)

/** Colores con que nace un workspace de esa impresora (Printer.Colors(0) en Go). */
export function defaultColorsOf(p: printers.Printer | undefined): number {
    const max = maxColorsOf(p)
    const d = p?.default_colors && p.default_colors > 0 ? p.default_colors : max
    return Math.min(Math.max(d, 1), max)
}

/** El primer slicer instalado de la impresora (el recomendado va primero en el catálogo), o undefined. */
export function installedSlicer(printer: Pick<printers.Printer, 'slicers'> | undefined, detected: main.Slicer[]): main.Slicer | undefined {
    for (const id of printer?.slicers ?? []) {
        const s = detected.find((x) => x.id === id && x.found)
        if (s) return s
    }
    return undefined
}

/** El slicer recomendado de la impresora (el primero de su lista que la app conoce), instalado o no. */
export function recommendedSlicer(printer: Pick<printers.Printer, 'slicers'> | undefined, detected: main.Slicer[]): main.Slicer | undefined {
    for (const id of printer?.slicers ?? []) {
        const s = detected.find((x) => x.id === id)
        if (s) return s
    }
    return undefined
}

/**
 * Cambia la skill de marca de una lista sin tocar el resto: sale la de la impresora anterior y entra la de la nueva
 * (si existe: `existing` son los nombres de las skills que hay). Es lo que pasa al cambiar de impresora cuando alguien
 * ya tocó las casillas. Devuelve la lista ordenada.
 */
export function swapBrandSkill(list: string[], from: string, to: string, existing: ReadonlySet<string>): string[] {
    if (from === to) return list
    const next = list.filter((n) => n !== from)
    if (to && existing.has(to) && !next.includes(to)) next.push(to)
    return next.sort()
}
