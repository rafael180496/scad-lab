// Detección de bloques OpenSCAD en la respuesta de un modelo. Espejo de
// backend/prompts.ScadBlocks (lo que cuenta como código OpenSCAD): ```openscad
// y ```scad siempre, y un cerco sin lenguaje solo si el contenido lo parece.

const SCAD_LANGS = new Set(['openscad', 'scad'])

// Cabezas que casi solo existen en OpenSCAD. Una sola no alcanza para un cerco
// sin lenguaje (un `translate(` puede ser JavaScript): hace falta una de estas
// más una sentencia que termine en `;` o un bloque.
const SCAD_HEADS =
    /\b(module|difference|union|intersection|translate|rotate|scale|mirror|cube|cylinder|sphere|polyhedron|linear_extrude|rotate_extrude|hull|minkowski|color|circle|square|polygon|offset)\s*\(/

export function looksLikeScad(code: string): boolean {
    return SCAD_HEADS.test(code) && /[;}]\s*$/m.test(code) && !/\b(function\s*\(|=>|const |let |var |import |def |print\()/.test(code)
}

export function isScadBlock(lang: string, code: string): boolean {
    const l = lang.trim().toLowerCase()
    if (SCAD_LANGS.has(l)) return true
    return l === '' && looksLikeScad(code)
}

/** `desc` del encabezado `// @checkpoint parent=… desc="…"`, o '' si no hay. */
export function headerDescription(code: string): string {
    const first = code.split('\n', 1)[0] ?? ''
    if (!/^\s*\/\/\s*@checkpoint\b/.test(first)) return ''
    const m = first.match(/desc="((?:[^"\\]|\\.)*)"/)
    return m ? m[1].replace(/\\(["\\])/g, '$1').trim() : ''
}
