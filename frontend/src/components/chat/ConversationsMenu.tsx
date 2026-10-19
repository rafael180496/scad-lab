import {useEffect, useId, useMemo, useState} from 'react'
import {Check, ChevronDown, Loader2, MessagesSquare, Pencil, Search, Trash2, X} from 'lucide-react'
import type {store} from '../../../wailsjs/go/models'
import {fmt, locale, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {MAX_CHAT_TITLE, cleanChatTitle} from '../../lib/chatRename'
import {IconButton} from '../ui/Button'
import {Input} from '../ui/Field'
import {Popover, listKeys} from './Popover'
import {MIN_QUERY, useChatSearch, type ChatHit} from './useChatSearch'

const when = (unix: number) =>
    new Intl.DateTimeFormat(locale(), {day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit'}).format(new Date(unix * 1000))

// Resalta la coincidencia dentro de un texto sin armar HTML: el texto de un chat
// es contenido no confiable y React lo escapa.
function Highlight({text, query}: {text: string; query: string}) {
    const q = query.trim()
    const i = q ? text.toLowerCase().indexOf(q.toLowerCase()) : -1
    if (i < 0) return <>{text}</>
    return (
        <>
            {text.slice(0, i)}
            <mark className="rounded-sm bg-accent-soft px-0.5 text-fg">{text.slice(i, i + q.length)}</mark>
            {text.slice(i + q.length)}
        </>
    )
}

// Una fila de la lista: abrir la conversación, o renombrarla en línea. Los
// botones de renombrar y borrar aparecen con el cursor encima o con el foco
// dentro (y siempre en la conversación abierta): una lista de diez conversaciones
// con veinte botones fijos sería ruido.
function ChatRow({
    hit,
    query,
    active,
    current,
    providerName,
    index,
    onHover,
    onSelect,
    onRename,
    onDelete,
}: {
    hit: ChatHit
    query: string
    active: boolean
    current: boolean
    providerName: string
    index: number
    onHover: () => void
    onSelect: () => void
    onRename: (title: string) => void
    onDelete: () => void
}) {
    const t = useT()
    const chat = hit.chat
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
                            e.stopPropagation()
                            commit()
                        } else if (e.key === 'Escape') {
                            e.preventDefault()
                            e.stopPropagation()
                            setEditing(false)
                        } else e.stopPropagation()
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
        <div
            role="option"
            aria-selected={current}
            data-row={index}
            onPointerMove={onHover}
            className={cn('group flex items-center gap-1 pr-1.5 transition-colors', (active || current) && 'bg-surface-2')}
        >
            <button type="button" title={name} onClick={onSelect} className="no-drag flex min-w-0 flex-1 flex-col gap-0.5 px-3 py-1.5 text-left">
                <span className="truncate text-ui-12-5 text-fg">
                    <Highlight text={name} query={hit.where === 'title' ? query : ''} />
                </span>
                {hit.snippet ? (
                    <span className="selectable line-clamp-2 text-ui-11 leading-snug text-muted">
                        <Highlight text={hit.snippet} query={query} />
                    </span>
                ) : null}
                <span className="truncate font-mono text-ui-10-5 text-faint">
                    {providerName} · {chat.model || t.chat.modelDefault} · {when(chat.updatedAt)}
                </span>
            </button>
            <span className={cn('flex shrink-0 items-center opacity-0 group-focus-within:opacity-100 group-hover:opacity-100', current && 'opacity-100')}>
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

// Lista de chats del workspace, en un desplegable que se abre desde el encabezado:
// el panel inferior es chico y una columna fija le quitaría ancho justo al texto.
// Trae un buscador por título y contenido; con la lista abierta, las flechas
// recorren los chats y Enter abre el elegido.
export function ConversationsMenu({
    chats,
    current,
    providerName,
    onSelect,
    onRename,
    onDelete,
    iconOnly,
    noChevron,
    openSignal,
}: {
    chats: store.Chat[]
    current: store.Chat | null
    providerName: (providerId: string) => string
    onSelect: (id: number) => void
    onRename: (id: number, title: string) => void
    onDelete: (id: number) => void
    /** Sin el contador de conversaciones (panel muy angosto). */
    iconOnly?: boolean
    /** Sin la flecha del desplegable. */
    noChevron?: boolean
    /** Cambia cuando el menú «…» pide abrir la lista. */
    openSignal?: number
}) {
    const t = useT()
    const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
    const [open, setOpen] = useState(false)
    const [q, setQ] = useState('')
    const [active, setActive] = useState(0)
    const listId = useId()
    const {hits, searching, limited} = useChatSearch(chats, q, open)

    useEffect(() => {
        if (openSignal) setOpen(true)
    }, [openSignal])
    useEffect(() => setActive(0), [q, open])
    useEffect(() => {
        if (open) document.querySelector<HTMLElement>(`[data-conversations] [data-row="${active}"]`)?.scrollIntoView({block: 'nearest'})
    }, [active, open])
    const close = () => {
        setOpen(false)
        setQ('')
    }
    const rows = useMemo(() => hits, [hits])

    return (
        <>
            <button
                ref={setAnchor}
                type="button"
                data-testid="chat-switcher"
                title={t.chat.chatsHint}
                aria-haspopup="listbox"
                aria-expanded={open}
                onClick={() => setOpen((o) => !o)}
                className="no-drag flex h-7 shrink-0 items-center gap-1.5 rounded-md px-1.5 text-ui-12 text-fg transition-colors hover:bg-surface-2"
            >
                <MessagesSquare className="icon-ui-14 shrink-0 text-faint" />
                {chats.length > 0 && !iconOnly ? <span className="shrink-0 rounded-full bg-surface-2 px-1.5 font-mono text-ui-10 text-muted">{chats.length}</span> : null}
                {noChevron ? null : <ChevronDown className="icon-ui-12 shrink-0 text-faint" />}
            </button>
            {open && anchor ? (
                <Popover
                    anchor={anchor}
                    onClose={close}
                    width={400}
                    label={t.chat.chatsHint}
                    focusFirst={false}
                    onKeyDown={(e) => listKeys(e, {rows: rows.length, active, setActive, onEnter: () => rows[active] && (close(), onSelect(rows[active].chat.id))})}
                >
                    <div data-conversations className="flex min-h-0 flex-col">
                        {chats.length > 0 ? (
                            <div className="flex shrink-0 items-center gap-1.5 border-b border-line px-2 py-1.5">
                                <Search className="icon-ui-14 shrink-0 text-faint" />
                                <Input
                                    autoFocus
                                    data-testid="chat-search"
                                    value={q}
                                    placeholder={t.chat.search.placeholder}
                                    title={t.chat.search.hint}
                                    aria-controls={listId}
                                    onChange={(e) => setQ(e.target.value)}
                                    className="h-7 border-0 bg-transparent px-0 focus:border-0"
                                />
                                {searching ? <Loader2 className="icon-ui-14 shrink-0 animate-spin text-faint" aria-label={t.chat.search.searching} /> : null}
                                {q ? (
                                    <IconButton title={t.chat.search.clear} className="size-6" onClick={() => setQ('')}>
                                        <X className="icon-ui-12" />
                                    </IconButton>
                                ) : null}
                            </div>
                        ) : null}
                        <div id={listId} role="listbox" aria-label={t.chat.chatsHint} className="min-h-0 overflow-y-auto py-1">
                            {chats.length === 0 ? <p className="px-3 py-2 text-ui-12 text-faint">{t.chat.noChats}</p> : null}
                            {chats.length > 0 && rows.length === 0 ? (
                                <p className="px-3 py-2 text-ui-12 text-faint">{searching ? t.chat.search.searching : fmt(t.chat.search.none, {q: q.trim()})}</p>
                            ) : null}
                            {rows.map((h, i) => (
                                <ChatRow
                                    key={h.chat.id}
                                    hit={h}
                                    query={q}
                                    index={i}
                                    active={i === active}
                                    current={h.chat.id === current?.id}
                                    providerName={providerName(h.chat.providerId)}
                                    onHover={() => setActive(i)}
                                    onSelect={() => {
                                        close()
                                        onSelect(h.chat.id)
                                    }}
                                    onRename={(title) => onRename(h.chat.id, title)}
                                    onDelete={() => {
                                        close()
                                        onDelete(h.chat.id)
                                    }}
                                />
                            ))}
                        </div>
                        {q.trim().length > 0 && q.trim().length < MIN_QUERY ? <p className="shrink-0 border-t border-line px-3 py-1.5 text-ui-10-5 text-faint">{fmt(t.chat.search.minChars, {n: MIN_QUERY})}</p> : null}
                        {limited && q.trim().length >= MIN_QUERY ? <p className="shrink-0 border-t border-line px-3 py-1.5 text-ui-10-5 text-faint">{t.chat.search.limited}</p> : null}
                    </div>
                </Popover>
            ) : null}
        </>
    )
}
