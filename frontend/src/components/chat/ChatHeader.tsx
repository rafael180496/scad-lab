import {useEffect, useRef, useState} from 'react'
import {ChevronDown, MessageSquarePlus, MessagesSquare, RefreshCw, Trash2} from 'lucide-react'
import type {store} from '../../../wailsjs/go/models'
import {fmt, locale, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {IconButton} from '../ui/Button'
import {Input, Select} from '../ui/Field'

const when = (unix: number) =>
    new Intl.DateTimeFormat(locale(), {day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit'}).format(new Date(unix * 1000))

// Lista de chats del workspace, en un menú que se abre desde la cabecera: el
// panel inferior es chico y una columna fija le quitaría ancho justo al texto.
function ChatSwitcher({
    chats,
    current,
    providers,
    onSelect,
}: {
    chats: store.Chat[]
    current: store.Chat | null
    providers: store.AIProvider[]
    onSelect: (id: number) => void
}) {
    const t = useT()
    const [open, setOpen] = useState(false)
    const root = useRef<HTMLDivElement>(null)

    useEffect(() => {
        if (!open) return
        const away = (e: PointerEvent) => !root.current?.contains(e.target as Node) && setOpen(false)
        const esc = (e: KeyboardEvent) => e.key === 'Escape' && setOpen(false)
        document.addEventListener('pointerdown', away)
        document.addEventListener('keydown', esc)
        return () => {
            document.removeEventListener('pointerdown', away)
            document.removeEventListener('keydown', esc)
        }
    }, [open])

    const name = (c: store.Chat) => c.title || t.chat.untitled
    const providerName = (id: string) => providers.find((p) => p.id === id)?.name ?? id
    return (
        <div ref={root} className="relative min-w-0">
            <button
                type="button"
                title={t.chat.chatsHint}
                aria-haspopup="listbox"
                aria-expanded={open}
                onClick={() => setOpen((o) => !o)}
                className="no-drag flex h-7 max-w-[260px] items-center gap-1.5 rounded-md px-2 text-ui-12-5 text-fg transition-colors hover:bg-surface-2"
            >
                <MessagesSquare className="icon-ui-14 shrink-0 text-faint" />
                <span className="min-w-0 truncate">{current ? name(current) : t.chat.newChat}</span>
                {chats.length > 0 ? <span className="shrink-0 rounded-full bg-surface-2 px-1.5 font-mono text-ui-10 text-muted">{chats.length}</span> : null}
                <ChevronDown className="icon-ui-12 shrink-0 text-faint" />
            </button>
            {open ? (
                <div
                    role="listbox"
                    className="absolute top-8 left-0 z-20 max-h-64 w-[340px] max-w-[80vw] overflow-y-auto rounded-lg border border-line-strong bg-raised py-1 shadow-xl"
                >
                    {chats.length === 0 ? (
                        <p className="px-3 py-2 text-ui-12 text-faint">{t.chat.noChats}</p>
                    ) : (
                        chats.map((c) => (
                            <button
                                key={c.id}
                                type="button"
                                role="option"
                                aria-selected={c.id === current?.id}
                                title={name(c)}
                                onClick={() => {
                                    setOpen(false)
                                    onSelect(c.id)
                                }}
                                className={cn(
                                    'flex w-full flex-col gap-0.5 px-3 py-1.5 text-left transition-colors hover:bg-surface-2',
                                    c.id === current?.id && 'bg-surface-2',
                                )}
                            >
                                <span className="truncate text-ui-12-5 text-fg">{name(c)}</span>
                                <span className="truncate font-mono text-ui-10-5 text-faint">
                                    {providerName(c.providerId)} · {c.model} · {when(c.updatedAt)}
                                </span>
                            </button>
                        ))
                    )}
                </div>
            ) : null}
        </div>
    )
}

export type ModelPicker = {
    providerId: string
    model: string
    models: string[]
    loading: boolean
    error: string
    onProvider: (id: string) => void
    onModel: (m: string) => void
    onReload: () => void
}

// Cabecera del chat: cambiar de conversación, abrir una nueva y elegir con qué
// modelo. El proveedor y el modelo se eligen UNA vez, al empezar el chat: un
// chat es una conversación con un modelo, y cambiarlo a mitad mezclaría dos
// estilos de respuesta en el mismo historial (por eso en un chat abierto se
// muestran fijos).
export function ChatHeader({
    chats,
    current,
    providers,
    picker,
    onSelect,
    onNew,
    onDelete,
    running,
}: {
    chats: store.Chat[]
    current: store.Chat | null
    providers: store.AIProvider[]
    picker: ModelPicker
    onSelect: (id: number) => void
    onNew: () => void
    onDelete: () => void
    running: boolean
}) {
    const t = useT()
    const providerName = (id: string) => providers.find((p) => p.id === id)?.name ?? id
    return (
        // Envuelve a una segunda fila si no cabe: nunca corta un control.
        <div className="flex min-h-9 shrink-0 flex-wrap items-center gap-x-1.5 gap-y-1 border-b border-line py-1 pr-1.5 pl-1.5">
            <ChatSwitcher chats={chats} current={current} providers={providers} onSelect={onSelect} />
            <IconButton title={t.chat.newChatHint} onClick={onNew} disabled={!current}>
                <MessageSquarePlus className="icon-ui-14" />
            </IconButton>

            <div className="ml-auto flex min-w-0 flex-wrap items-center gap-1.5">
                {current ? (
                    <>
                        <span
                            className="selectable min-w-0 truncate rounded border border-line px-1.5 py-0.5 font-mono text-ui-10-5 text-muted"
                            title={t.chat.fixedModelHint}
                        >
                            {providerName(current.providerId)} · {current.model}
                        </span>
                        <IconButton title={running ? t.chat.deleteBusyHint : t.chat.deleteHint} onClick={onDelete}>
                            <Trash2 className="icon-ui-14" />
                        </IconButton>
                    </>
                ) : (
                    <>
                        <Select
                            value={picker.providerId}
                            title={t.chat.providerHint}
                            onChange={(e) => picker.onProvider(e.target.value)}
                            className="h-7 w-[12.5em] max-w-full text-ui-12"
                        >
                            {providers.map((p) => (
                                <option key={p.id} value={p.id}>
                                    {p.name}
                                </option>
                            ))}
                        </Select>
                        {picker.models.length > 0 ? (
                            <Select
                                value={picker.model}
                                title={t.chat.modelHint}
                                onChange={(e) => picker.onModel(e.target.value)}
                                className="h-7 w-[16em] max-w-full text-ui-12"
                            >
                                {!picker.models.includes(picker.model) ? <option value={picker.model}>{picker.model || t.chat.pickModel}</option> : null}
                                {picker.models.map((m) => (
                                    <option key={m} value={m}>
                                        {m}
                                    </option>
                                ))}
                            </Select>
                        ) : (
                            <Input
                                value={picker.model}
                                title={picker.error ? fmt(t.chat.modelsError, {msg: picker.error}) : t.chat.modelTypeHint}
                                placeholder={t.chat.modelPlaceholder}
                                onChange={(e) => picker.onModel(e.target.value)}
                                className={cn('h-7 w-[16em] max-w-full text-ui-12', picker.error && 'border-warn')}
                            />
                        )}
                        <IconButton title={picker.error ? fmt(t.chat.modelsError, {msg: picker.error}) : t.chat.reloadModels} onClick={picker.onReload}>
                            <RefreshCw className={cn('icon-ui-14', picker.loading && 'animate-spin', picker.error && 'text-warn')} />
                        </IconButton>
                    </>
                )}
            </div>
        </div>
    )
}
