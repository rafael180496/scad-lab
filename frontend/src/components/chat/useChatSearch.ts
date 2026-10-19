import {useEffect, useMemo, useState} from 'react'
import {GetChat} from '../../../wailsjs/go/main/App'
import type {store} from '../../../wailsjs/go/models'
import {snippetAround} from '../../lib/chatSearch'

// Búsqueda en las conversaciones por título Y por contenido. No hay un binding de
// búsqueda en Go, así que el contenido se lee con GetChat y se guarda en memoria
// (por chat y fecha de modificación: un chat que cambió se vuelve a leer, uno que
// no, no). Solo se pide al escribir una búsqueda, de a pocos chats a la vez y
// hasta MAX_CHATS de los más recientes: abrir la lista sin buscar no cuesta nada.

export const MIN_QUERY = 2
const MAX_CHATS = 60
const CONCURRENCY = 3
const MAX_TEXT = 200_000

type Cached = {updatedAt: number; text: string}
const cache = new Map<number, Cached>()

async function load(c: store.Chat): Promise<void> {
    const hit = cache.get(c.id)
    if (hit && hit.updatedAt === c.updatedAt) return
    try {
        const v = await GetChat(c.id)
        const text = (v.messages ?? []).map((m) => m.content).join('\n').slice(0, MAX_TEXT)
        cache.set(c.id, {updatedAt: c.updatedAt, text})
    } catch {
        // un chat que no se pudo leer solo se busca por título
        cache.set(c.id, {updatedAt: c.updatedAt, text: ''})
    }
}

export type ChatHit = {chat: store.Chat; where: 'title' | 'content'; snippet: string}

/**
 * Chats que coinciden con `query` (título primero, después contenido). Sin
 * búsqueda devuelve todos, sin fragmento. `searching` es true mientras faltan
 * chats por leer.
 */
export function useChatSearch(chats: store.Chat[], query: string, enabled: boolean): {hits: ChatHit[]; searching: boolean; limited: boolean} {
    const q = query.trim()
    const active = enabled && q.length >= MIN_QUERY
    const [version, setVersion] = useState(0)
    const [searching, setSearching] = useState(false)
    const pool = useMemo(() => chats.slice(0, MAX_CHATS), [chats])

    useEffect(() => {
        if (!active) {
            setSearching(false)
            return
        }
        let stale = false
        const pending = pool.filter((c) => cache.get(c.id)?.updatedAt !== c.updatedAt)
        if (pending.length === 0) {
            setSearching(false)
            return
        }
        setSearching(true)
        // Un pequeño retraso: si se sigue escribiendo, no se lanza una lectura por tecla.
        const timer = window.setTimeout(async () => {
            let next = 0
            const worker = async () => {
                while (!stale && next < pending.length) await load(pending[next++])
            }
            await Promise.all(Array.from({length: CONCURRENCY}, worker))
            if (!stale) {
                setSearching(false)
                setVersion((v) => v + 1)
            }
        }, 250)
        return () => {
            stale = true
            window.clearTimeout(timer)
        }
    }, [active, pool])

    const hits = useMemo<ChatHit[]>(() => {
        void version
        if (!active) return chats.map((chat) => ({chat, where: 'title' as const, snippet: ''}))
        const low = q.toLowerCase()
        const byTitle: ChatHit[] = []
        const byContent: ChatHit[] = []
        for (const chat of chats) {
            if ((chat.title ?? '').toLowerCase().includes(low)) {
                byTitle.push({chat, where: 'title', snippet: ''})
                continue
            }
            const text = cache.get(chat.id)?.text ?? ''
            const snippet = snippetAround(text, q)
            if (snippet) byContent.push({chat, where: 'content', snippet})
        }
        return [...byTitle, ...byContent]
    }, [chats, q, active, version])

    return {hits, searching: active && searching, limited: chats.length > MAX_CHATS}
}
