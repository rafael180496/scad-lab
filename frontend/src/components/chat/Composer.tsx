import {useLayoutEffect, useRef} from 'react'
import {ArrowUp, Square} from 'lucide-react'
import {useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {Button} from '../ui/Button'

// Caja de mensaje: crece con el texto hasta ~6 líneas y de ahí hace scroll.
// Enter envía, Mayús+Enter agrega una línea; mientras un IME compone (japonés,
// chino…) Enter confirma la composición y no envía.
export function Composer({
    value,
    onChange,
    onSend,
    onStop,
    running,
    canSend,
    disabledReason,
}: {
    value: string
    onChange: (v: string) => void
    onSend: () => void
    onStop: () => void
    running: boolean
    canSend: boolean
    disabledReason?: string
}) {
    const t = useT()
    const ta = useRef<HTMLTextAreaElement>(null)

    useLayoutEffect(() => {
        const el = ta.current
        if (!el) return
        el.style.height = 'auto'
        el.style.height = `${Math.min(el.scrollHeight, 132)}px`
    }, [value])

    const ready = canSend && value.trim() !== '' && !running
    return (
        <div className="shrink-0 border-t border-line bg-surface px-3 py-2">
            <div className={cn('flex items-end gap-2 rounded-lg border border-line bg-surface-2 py-1.5 pr-1.5 pl-3 transition-colors focus-within:border-accent')}>
                <textarea
                    ref={ta}
                    rows={1}
                    value={value}
                    title={disabledReason ?? t.chat.inputHint}
                    placeholder={t.chat.placeholder}
                    onChange={(e) => onChange(e.target.value)}
                    onKeyDown={(e) => {
                        if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                            e.preventDefault()
                            if (ready) onSend()
                        }
                    }}
                    className="selectable max-h-[132px] min-h-6 flex-1 resize-none bg-transparent py-0.5 text-chat leading-relaxed text-fg outline-none placeholder:text-faint"
                />
                {running ? (
                    <Button variant="outline" title={t.chat.stopHint} onClick={onStop} className="h-7">
                        <Square className="icon-ui-12 fill-current" />
                        {t.chat.stop}
                    </Button>
                ) : (
                    <Button variant="primary" title={disabledReason ?? t.chat.sendHint} onClick={onSend} disabled={!ready} className="h-7">
                        <ArrowUp className="icon-ui-14" />
                        {t.chat.send}
                    </Button>
                )}
            </div>
        </div>
    )
}
