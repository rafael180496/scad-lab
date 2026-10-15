import {useSyncExternalStore} from 'react'
import es from './es'
import en from './en'

// Núcleo de i18n (ver claude/05-i18n.md). Mismo diseño que mini-tools:
//
// **Sin librería.** Los textos son objetos tipados: `t.settings.title` en vez
// de `t('settings.title')`. El compilador valida que la clave exista y types.ts
// obliga a que el diccionario inglés tenga la misma forma que el español.
//
// **Cambio en caliente sin remontar.** Remontar el árbol cerraría terminales y
// cancelaría renders en curso; cada componente se suscribe con useT() y se
// redibuja solo.

export type Lang = 'en' | 'es'
export type Dict = typeof es

// Inglés mientras no se haya elegido, igual que mini-tools.
export const DEFAULT_LANG: Lang = 'en'

export const LANGUAGES: {id: Lang; label: string}[] = [
    {id: 'en', label: 'English'},
    {id: 'es', label: 'Español'},
]

const dicts: Record<Lang, Dict> = {es, en}

let current: Lang = DEFAULT_LANG
const listeners = new Set<() => void>()

export function normalizeLang(raw: string | null | undefined): Lang {
    return raw === 'es' || raw === 'en' ? raw : DEFAULT_LANG
}

export function getLanguage(): Lang {
    return current
}

// setLanguage cambia el idioma en memoria. No persiste: eso lo hace quien
// llama (SetLanguage del backend), para que el núcleo no dependa de Wails.
export function setLanguage(lang: Lang) {
    if (lang === current) return
    current = lang
    document.documentElement.lang = lang
    for (const l of listeners) l()
}

function subscribe(cb: () => void) {
    listeners.add(cb)
    return () => listeners.delete(cb)
}

export function useT(): Dict {
    const lang = useSyncExternalStore(subscribe, getLanguage)
    return dicts[lang]
}

export function useLang(): Lang {
    return useSyncExternalStore(subscribe, getLanguage)
}

// t() es el diccionario activo fuera de un componente. Leelo al usarlo; no lo
// guardes en una constante de módulo (quedaría en el idioma del arranque).
export function t(): Dict {
    return dicts[current]
}

export function locale(lang: Lang = current): string {
    return lang === 'es' ? 'es-ES' : 'en-US'
}

// fmt reemplaza {nombre} en un texto del diccionario.
export function fmt(text: string, vars: Record<string, string | number>): string {
    return text.replace(/\{(\w+)\}/g, (m, k) => (k in vars ? String(vars[k]) : m))
}
