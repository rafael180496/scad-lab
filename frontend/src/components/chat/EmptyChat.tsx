import {useEffect, useRef, useState} from 'react'
import {Bot, Box, Camera, Cpu, Eye, Palette, Sparkles, Wrench, type LucideIcon} from 'lucide-react'
import {useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {isCLI, type ChatProviderItem} from '../../lib/chatProviders'
import {modeTexts} from './ModeControl'
import {providerIcon} from './providerIcons'

// Una por sugerencia de `t.chat.suggestions`, en el mismo orden: caja, foto,
// modificación y color/AMS.
const SUGGESTION_ICONS: LucideIcon[] = [Box, Camera, Wrench, Palette]

// Por debajo de este alto de la lista de mensajes (px) el estado vacío se aprieta:
// sin el ícono ni la explicación larga. El panel del chat suele ser bajo, y un título
// cortado arriba es peor que un texto de menos.
const TIGHT_HEIGHT = 380

// Alto de la lista de mensajes que contiene a este elemento.
function useScrollHeight(): [number, React.RefObject<HTMLDivElement | null>] {
    const ref = useRef<HTMLDivElement>(null)
    const [h, setH] = useState(Infinity)
    useEffect(() => {
        const box = ref.current?.closest<HTMLElement>('[data-chat-scroll]')
        if (!box) return
        setH(box.clientHeight)
        const ro = new ResizeObserver(() => setH(box.clientHeight))
        ro.observe(box)
        return () => ro.disconnect()
    }, [])
    return [h, ref]
}

// Estado vacío del chat (S12.b): qué se le puede pedir y con qué se va a hablar.
// Las sugerencias son pedidos de OpenSCAD listos para usar: un clic rellena la caja
// de mensaje (no envía: casi todos piden completar un dato, el modelo o la pieza).
// Arriba, el proveedor, el modelo y el modo activos —lo que más se olvida antes de
// escribir— con la explicación del modo.
export function EmptyChat({
    provider,
    modelLabel,
    agent,
    onPick,
}: {
    provider: ChatProviderItem | undefined
    modelLabel: string
    agent: boolean
    onPick: (text: string) => void
}) {
    const t = useT()
    const [height, ref] = useScrollHeight()
    const tight = height < TIGHT_HEIGHT
    const kind = provider && isCLI(provider.id) ? 'cli' : 'server'
    const mode = modeTexts(t, kind, agent)
    const Icon = providerIcon(provider)
    return (
        <div ref={ref} data-testid="chat-empty" className="@container m-auto flex w-full max-w-[600px] flex-col items-center gap-2.5 py-2 text-center">
            {tight ? null : (
                <span className="flex icon-ui-36 items-center justify-center rounded-full bg-accent-soft text-accent">
                    <Sparkles className="icon-ui-20" />
                </span>
            )}
            <div>
                <p className="text-ui-13 font-medium text-fg">{t.chat.emptyTitle}</p>
                {tight ? null : <p className="mx-auto mt-1 max-w-[480px] text-ui-12-5 leading-relaxed text-muted">{t.chat.emptyBody}</p>}
            </div>

            {provider ? (
                <div data-testid="chat-active" className="flex flex-wrap items-center justify-center gap-1.5 text-ui-11-5">
                    <span className="flex max-w-full items-center gap-1.5 rounded-full border border-line px-2.5 py-1 text-fg" title={t.chat.empty.providerHint}>
                        <Icon className="icon-ui-12 shrink-0 text-accent" />
                        <span className="truncate">{provider.name}</span>
                    </span>
                    <span className="flex max-w-full items-center gap-1.5 rounded-full border border-line px-2.5 py-1 text-fg" title={t.chat.empty.modelHint}>
                        <Cpu className="icon-ui-12 shrink-0 text-faint" />
                        <span className="truncate">{modelLabel}</span>
                    </span>
                    <span
                        className={cn('flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-fg', agent ? 'border-accent/50 bg-accent-soft' : 'border-line')}
                        title={`${mode.label}: ${mode.hint}`}
                    >
                        {agent ? <Bot className="icon-ui-12 shrink-0 text-accent" /> : <Eye className="icon-ui-12 shrink-0 text-muted" />}
                        {mode.label}
                    </span>
                </div>
            ) : null}
            {provider ? <p className="mx-auto max-w-[480px] text-ui-11-5 leading-snug text-muted">{tight ? mode.note : mode.hint}</p> : null}

            <div className="w-full">
                {tight ? null : <p className="mb-1.5 font-mono text-ui-10 tracking-[0.12em] text-faint uppercase">{t.chat.empty.try}</p>}
                <div className="grid w-full grid-cols-1 gap-1.5 @min-[30rem]:grid-cols-2">
                    {t.chat.suggestions.map((s, i) => {
                        const SIcon = SUGGESTION_ICONS[i % SUGGESTION_ICONS.length]
                        return (
                            <button
                                key={s}
                                type="button"
                                data-testid="chat-suggestion"
                                title={t.chat.suggestionHint}
                                onClick={() => onPick(s)}
                                className="no-drag group flex items-center gap-2 rounded-lg border border-line px-2.5 py-1.5 text-left text-ui-12 text-muted transition-colors hover:border-line-strong hover:bg-surface-2 hover:text-fg"
                            >
                                <SIcon className="icon-ui-14 shrink-0 text-faint transition-colors group-hover:text-accent" />
                                <span className="min-w-0 flex-1">{s}</span>
                            </button>
                        )
                    })}
                </div>
            </div>
        </div>
    )
}
