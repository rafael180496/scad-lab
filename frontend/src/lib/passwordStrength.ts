// Indicador de fuerza de la contraseña maestra (S12, 12.10). Es una guía para
// quien la escribe, no una barrera: el único piso que el backend exige es el
// largo mínimo (vaultgate.MinPasswordLen), y se repite acá para avisarlo antes
// de enviar. Sin librerías: cuenta largo, variedad de caracteres y descarta las
// contraseñas que ya son de manual.

export const MIN_PASSWORD_LEN = 8

export type StrengthLevel = 0 | 1 | 2 | 3 | 4

// 0 = vacía · 1 = muy débil · 2 = débil · 3 = buena · 4 = fuerte
export function passwordStrength(pw: string): StrengthLevel {
    const len = [...pw].length
    if (len === 0) return 0
    if (len < MIN_PASSWORD_LEN || isPredictable(pw)) return 1
    const classes = [/[a-záéíóúüñ]/, /[A-ZÁÉÍÓÚÜÑ]/, /\d/, /[^\p{L}\d]/u].filter((re) => re.test(pw)).length
    // Pasar el mínimo ya es «débil»; el largo y la variedad suben el nivel.
    let level = 2
    if (len >= 12) level++
    if (len >= 20 || (classes >= 3 && len >= 10)) level++
    return Math.min(level, 4) as StrengthLevel
}

const COMMON = ['password', 'contrasena', 'contraseña', 'qwerty', 'asdfgh', 'abc123', 'letmein', 'admin', 'scadlab', '12345678', '123456789', '1234567890', '11111111', '00000000']

function isPredictable(pw: string): boolean {
    const low = pw.toLowerCase()
    if (COMMON.some((c) => low.includes(c))) return true
    if (new Set(low).size <= 2) return true // «aaaaaaaa», «abababab»
    // Una secuencia corrida: 12345678, abcdefgh.
    let run = 1
    for (let i = 1; i < low.length; i++) {
        run = low.charCodeAt(i) - low.charCodeAt(i - 1) === 1 ? run + 1 : 1
        if (run >= 6) return true
    }
    return false
}
