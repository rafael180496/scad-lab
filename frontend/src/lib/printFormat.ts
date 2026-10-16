import {locale} from '../i18n'

// Formato de las cifras de impresión. Siempre «≈» aparte, en el texto que lo
// muestra: todo esto es una estimación.

export function num(v: number, digits: number): string {
    return v.toLocaleString(locale(), {minimumFractionDigits: digits, maximumFractionDigits: digits})
}

/** Gramos: un decimal por debajo de 100 g, enteros desde ahí. */
export function grams(v: number): string {
    return num(v, v >= 100 ? 0 : 1)
}

/** Metros de filamento: dos decimales por debajo de 10 m. */
export function meters(v: number): string {
    return num(v, v >= 10 ? 1 : 2)
}

/** Costo con el símbolo de su moneda; tres decimales si es de centavos. */
export function money(v: number, currency: string): string {
    const digits = v > 0 && v < 0.1 ? 3 : 2
    try {
        return v.toLocaleString(locale(), {style: 'currency', currency, minimumFractionDigits: digits, maximumFractionDigits: digits})
    } catch {
        // Un código que Intl no conoce: se muestra tal cual.
        return `${num(v, digits)} ${currency}`
    }
}

/** Monedas que ofrece la lista (cualquier otra código ISO de 3 letras se conserva). */
export const CURRENCIES = ['USD', 'EUR', 'GBP', 'MXN', 'GTQ', 'COP', 'ARS', 'CLP', 'PEN', 'BRL', 'CAD', 'UYU']
