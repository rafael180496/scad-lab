import {useEffect, useRef, useState} from 'react'
import {Check, ChevronDown, MessageSquarePlus, MessagesSquare, Pencil, Trash2, X} from 'lucide-react'
import type {main, store} from '../../../wailsjs/go/models'
import {locale, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {MAX_CHAT_TITLE, cleanChatTitle} from '../../lib/chatRename'
import {IconButton} from '../ui/Button'
import {Input, Select} from '../ui/Field'
import {ModelCombobox} from './ModelCombobox'

const when = (unix: number) =>
    new Intl.DateTimeFormat(locale(), {day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit'}).format(new Date(unix * 1000))

// Una fila de la lista: abrir la conversación, o renombrarla en línea. Los
// botones de renombrar y borrar aparecen con el cursor encima o con el foco
// dentro (y siempre en la conversación abierta): una lista de diez conversaciones
// con veinte botones fijos sería ruido.
function ChatRow({
    chat,
    active,
    providerName,
    onSelect,
    onRename,
    onDelete,
}: {
    chat: store.Chat
    active: boolean
    providerName: string
    onSelect: () => void
    onRename: (title: string) => void
    onDelete: () => void
}) {
    const t = useT()
    const [editing, setEditing] = useState(false)
    const [draft, setDraft] = useState('')
    const name = chat.title || t.chat.untitled

    const start = () => {
        setDraft(chat.title)
        setEditing(true)
    }
    // Un nombre vacío no renombra: se conserva el anterior (el backend lo rechazaría).
    const commit = () => {
        const clean = cleanChatTitle(draft)
        setEditing(false)
        if (clean && clean !== chat.title) onRename(clean)
    }

    if (editing) {
        return (
            <div className="flex items-center gap-1 px-2 py-1.5">
                <Input
                    data-autofocus
                    autoFocus
                    value={draft}
                    maxLength={MAX_CHAT_TITLE * 2}
                    title={t.chat.renameHint}
                    placeholder={t.chat.renamePlaceholder}
                    onChange={(e) => setDraft(e.target.value)}
                    onFocus={(e) => e.currentTarget.select()}
                    onKeyDown={(e) => {
                        if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
                            e.preventDefault()
                            commit()
                        } else if (e.key === 'Escape') {
                            e.preventDefault()
                            e.stopPropagation()
                            setEditing(false)
                        }
                    }}
                    className="h-7 min-w-0 flex-1"
                />
                <IconButton title={t.chat.renameSave} className="size-6" onClick={commit}>
                    <Check className="icon-ui-14" />
                </IconButton>
                <IconButton title={t.chat.renameCancel} className="size-6" onClick={() => setEditing(false)}>
                    <X className="icon-ui-14" />
                </IconButton>
            </div>
        )
    }
    return (
        <div role="option" aria-selected={active} className={cn('group flex items-center gap-1 pr-1.5 transition-colors hover:bg-surface-2', active && 'bg-surface-2')}>
            <button type="button" title={name} onClick={onSelect} className="no-drag flex min-w-0 flex-1 flex-col gap-0.5 px-3 py-1.5 text-left">
                <span className="truncate text-ui-12-5 text-fg">{name}</span>
                <span className="truncate font-mono text-ui-10-5 text-faint">
                    {providerName} · {chat.model} · {when(chat.updatedAt)}
                </span>
            </button>
            <span className={cn('flex shrink-0 items-center opacity-0 group-focus-within:opacity-100 group-hover:opacity-100', active && 'opacity-100')}>
                <IconButton data-testid="chat-rename" title={t.chat.renameThis} className="size-6" onClick={start}>
                    <Pencil className="icon-ui-12" />
                </IconButton>
                <IconButton title={t.chat.deleteThis} className="size-6 hover:text-danger" onClick={onDelete}>
                    <Trash2 className="icon-ui-12" />
                </IconButton>
            </span>
        </div>
    )
}

// Lista de chats del workspace, en un menú que se abre desde la cabecera: el
// panel inferior es chico y una columna fija le quitaría ancho justo al texto.
function ChatSwitcher({
    chats,
    current,
    providers,
    onSelect,
    onRename,
    onDelete,
}: {
    chats: store.Chat[]
    current: store.Chat | null
    providers: store.ModelServer[]
    onSelect: (id: number) => void
    onRename: (id: number, title: string) => void
    onDelete: (id: number) => void
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
                data-testid="chat-switcher"
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
                    className="absolute top-8 left-0 z-20 max-h-72 w-[380px] max-w-[84vw] overflow-y-auto rounded-lg border border-line-strong bg-raised py-1 shadow-xl"
                >
                    {chats.length === 0 ? (
                        <p className="px-3 py-2 text-ui-12 text-faint">{t.chat.noChats}</p>
                    ) : (
                        chats.map((c) => (
                            <ChatRow
                                key={c.id}
                                chat={c}
                                active={c.id === current?.id}
                                providerName={providerName(c.providerId)}
                                onSelect={() => {
                                    setOpen(false)
                                    onSelect(c.id)
                                }}
                                onRename={(title) => onRename(c.id, title)}
                                onDelete={() => {
                                    setOpen(false)
                                    onDelete(c.id)
                                }}
                            />
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
    models: main.ModelInfo[]
    loading: boolean
    error: string
    onProvider: (id: string) => void
    onModel: (m: string) => void
    onReload: () => void
}

// Cabecera del chat: cambiar de conversación (con renombrar y borrar), abrir una
// nueva y elegir con qué modelo. El servidor y el modelo se eligen con un select
// y un combobox de modelos con búsqueda e insignias de visión y herramientas;
// en un chat abierto también se pueden cambiar a mitad de camino (el historial se
// conserva y el próximo mensaje ya usa el nuevo).
export function ChatHeader({
    chats,
    current,
    providers,
    picker,
    onSelect,
    onNew,
    onRename,
    onDelete,
    running,
}: {
    chats: store.Chat[]
    current: store.Chat | null
    providers: store.ModelServer[]
    picker: ModelPicker
    onSelect: (id: number) => void
    onNew: () => void
    onRename: (id: number, title: string) => void
    onDelete: (id: number) => void
    running: boolean
}) {
    const t = useT()
    return (
        // Envuelve a una segunda fila si no cabe: nunca corta un control.
        <div className="flex min-h-9 shrink-0 flex-wrap items-center gap-x-1.5 gap-y-1 border-b border-line py-1 pr-1.5 pl-1.5">
            <ChatSwitcher chats={chats} current={current} providers={providers} onSelect={onSelect} onRename={onRename} onDelete={onDelete} />
            <IconButton title={t.chat.newChatHint} onClick={onNew} disabled={!current}>
                <MessageSquarePlus className="icon-ui-14" />
            </IconButton>

            <div className="ml-auto flex min-w-0 flex-wrap items-center gap-1.5">
                <Select
                    value={picker.providerId}
                    title={running ? t.chat.modelBusyHint : current ? t.chat.providerSwitchHint : t.chat.providerHint}
                    disabled={running}
                    onChange={(e) => picker.onProvider(e.target.value)}
                    className="h-7 w-[12.5em] max-w-full text-ui-12"
                >
                    {providers.map((p) => (
                        <option key={p.id} value={p.id}>
                            {p.name}
                        </option>
                    ))}
                    {/* El servidor de un chat viejo puede estar borrado o apagado: sigue visible, y se puede cambiar. */}
                    {picker.providerId && !providers.some((p) => p.id === picker.providerId) ? (
                        <option value={picker.providerId}>{picker.providerId}</option>
                    ) : null}
                </Select>
                <ModelCombobox
                    models={picker.models}
                    value={picker.model}
                    onChange={picker.onModel}
                    loading={picker.loading}
                    error={picker.error}
                    onReload={picker.onReload}
                    disabled={running}
                    className="w-[22em] max-w-full"
                />
            </div>
        </div>
    )
}
