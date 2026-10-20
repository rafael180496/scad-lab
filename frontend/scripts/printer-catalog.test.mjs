#!/usr/bin/env node
// Prueba de los cálculos de impresoras de la interfaz (S14): agrupar por marca en el orden de Go, acotar los colores igual
// que `Printer.Colors` en Go, elegir el slicer instalado y recomendado, y cambiar la skill de marca de una lista.
//
//   cd frontend && pnpm test
import assert from 'node:assert/strict'
import {defaultColorsOf, groupByBrand, installedSlicer, maxColorsOf, recommendedSlicer, swapBrandSkill} from '../src/lib/printerCatalog.ts'

const brands = [
    {id: 'bambu', name: 'Bambu Lab', skill: 'bambu-lab-printers'},
    {id: 'prusa', name: 'Prusa Research', skill: 'prusa-printers'},
    {id: 'creality', name: 'Creality', skill: 'creality-printers'},
]
const p = (id, brand_id, extra = {}) => ({id, name: id, brand_id, brand_name: brand_id, skill: '', max_colors: 1, multicolor_slots: 1, default_colors: 1, slicers: [], ...extra})
const list = [p('prusa-mk4s', 'prusa'), p('bambu-a1', 'bambu'), p('prusa-mini', 'prusa'), p('ender', 'creality'), p('otra', 'marca-nueva', {brand_name: 'Marca Nueva'})]

// Agrupar: el orden de las marcas es el de ListPrinterBrands; dentro de cada una, el de ListPrinters; una marca que el
// catálogo de marcas no nombra no hace desaparecer a sus impresoras.
const groups = groupByBrand(list, brands)
assert.deepEqual(groups.map((g) => g.id), ['bambu', 'prusa', 'creality', 'marca-nueva'])
assert.deepEqual(groups[1].items.map((x) => x.id), ['prusa-mk4s', 'prusa-mini'])
assert.equal(groups[3].name, 'Marca Nueva')
assert.deepEqual(groupByBrand([], brands), [], 'sin impresoras no hay grupos vacíos')

// Colores: el máximo es max_colors (o multicolor_slots); por defecto, default_colors acotado a 1..máximo.
assert.equal(maxColorsOf(p('x', 'prusa', {max_colors: 16})), 16)
assert.equal(maxColorsOf(p('x', 'prusa', {max_colors: 0, multicolor_slots: 5})), 5)
assert.equal(maxColorsOf(p('x', 'prusa', {max_colors: 0, multicolor_slots: 0})), 1)
assert.equal(maxColorsOf(undefined), 1)
assert.equal(defaultColorsOf(p('x', 'bambu', {max_colors: 16, default_colors: 4})), 4)
assert.equal(defaultColorsOf(p('x', 'bambu', {max_colors: 4, default_colors: 9})), 4, 'no pasa del máximo de la máquina')
assert.equal(defaultColorsOf(p('x', 'bambu', {max_colors: 4, default_colors: 0})), 4, 'sin default rige el máximo, como Printer.Colors(0)')
assert.equal(defaultColorsOf(undefined), 1)

// Slicers: el recomendado es el primero que el registro conoce; «Abrir en» usa el primero INSTALADO de la lista.
const detected = [
    {id: 'prusaslicer', name: 'PrusaSlicer', found: false},
    {id: 'orcaslicer', name: 'OrcaSlicer', found: true},
    {id: 'ultimaker-cura', name: 'UltiMaker Cura', found: false},
]
const mk4s = p('prusa-mk4s', 'prusa', {slicers: ['prusaslicer', 'orcaslicer']})
assert.equal(recommendedSlicer(mk4s, detected)?.id, 'prusaslicer', 'recomendado aunque no esté instalado')
assert.equal(installedSlicer(mk4s, detected)?.id, 'orcaslicer', 'el primero instalado, no el recomendado')
assert.equal(installedSlicer(p('s7', 'ultimaker', {slicers: ['ultimaker-cura']}), detected), undefined, 'ninguno instalado')
assert.equal(recommendedSlicer(p('s7', 'ultimaker', {slicers: ['no-existe', 'ultimaker-cura']}), detected)?.id, 'ultimaker-cura', 'salta el id desconocido')
assert.equal(recommendedSlicer(p('x', 'x'), detected), undefined)
assert.equal(installedSlicer(undefined, detected), undefined)

// Cambiar de impresora con las casillas tocadas: sale la skill de la marca anterior y entra la nueva; lo demás no se toca.
const existing = new Set(['openscad-core', 'gear-helper', 'prusa-printers', 'bambu-lab-printers', 'creality-printers'])
assert.deepEqual(swapBrandSkill(['gear-helper', 'openscad-core', 'prusa-printers'], 'prusa-printers', 'creality-printers', existing), ['creality-printers', 'gear-helper', 'openscad-core'])
assert.deepEqual(swapBrandSkill(['gear-helper', 'creality-printers'], 'prusa-printers', 'creality-printers', existing), ['creality-printers', 'gear-helper'], 'no repite la que ya estaba')
assert.deepEqual(swapBrandSkill(['gear-helper', 'prusa-printers'], 'prusa-printers', 'skill-que-no-existe', existing), ['gear-helper'], 'una skill que no existe no entra')
assert.deepEqual(swapBrandSkill(['gear-helper', 'prusa-printers'], 'prusa-printers', '', existing), ['gear-helper'], 'una impresora sin skill de marca solo quita la anterior')
const same = ['openscad-core']
assert.equal(swapBrandSkill(same, 'prusa-printers', 'prusa-printers', existing), same, 'misma marca: no cambia nada')
assert.deepEqual(swapBrandSkill(['gear-helper'], '', 'prusa-printers', existing), ['gear-helper', 'prusa-printers'], 'sin marca anterior solo entra la nueva')

console.log('printer-catalog: OK')
