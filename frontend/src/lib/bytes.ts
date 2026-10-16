import {locale} from '../i18n'

// formatBytes escribe un tamaño de archivo para una descarga: «62 MB», «1,2 GB».
// Sin decimales en MB (a esa escala no informan) y con uno en GB.
export function formatBytes(n: number): string {
    if (n >= 1 << 30) return `${(n / (1 << 30)).toLocaleString(locale(), {maximumFractionDigits: 1})} GB`
    return `${Math.round(n / (1 << 20)).toLocaleString(locale())} MB`
}
