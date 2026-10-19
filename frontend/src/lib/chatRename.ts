// Nombre de una conversación puesto a mano. Espeja chat.CleanTitle (Go): recorta,
// colapsa los espacios y los saltos de línea y lo limita a MAX_CHAT_TITLE letras.
// El backend vuelve a validar; esto solo evita ir y volver con un nombre que
// se va a rechazar.

export const MAX_CHAT_TITLE = 120

/** El nombre limpio, o null si queda vacío (quien llama conserva el anterior). */
export function cleanChatTitle(raw: string): string | null {
    const t = raw.split(/\s+/).filter(Boolean).join(' ')
    if (!t) return null
    return Array.from(t).slice(0, MAX_CHAT_TITLE).join('').trim()
}
