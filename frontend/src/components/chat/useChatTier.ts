import {useEffect, useState} from 'react'
import {appearance} from '../../lib/appearance'
import {tierFor, type ChatTier} from '../../lib/chatTier'

export type {ChatTier}

// Mide el panel del chat y devuelve su escalón (ver lib/chatTier.ts).
/** `[tier, ref]`: ref de callback, porque el panel puede montarse después del primer render. */
export function useChatTier(): [ChatTier, (el: HTMLElement | null) => void] {
    const [el, setEl] = useState<HTMLElement | null>(null)
    const [width, setWidth] = useState(Infinity)
    const scale = appearance.use((s) => s.uiScale) / 100
    useEffect(() => {
        if (!el) return
        setWidth(el.getBoundingClientRect().width)
        const ro = new ResizeObserver(([e]) => setWidth(e.borderBoxSize?.[0]?.inlineSize ?? e.contentRect.width))
        ro.observe(el)
        return () => ro.disconnect()
    }, [el])
    return [tierFor(width / scale), setEl]
}
