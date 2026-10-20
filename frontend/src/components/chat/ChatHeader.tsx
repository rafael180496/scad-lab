import {useEffect, useRef, useState} from 'react'
import {Bot, Ellipsis, Eye, Folder, MessageSquarePlus, MessagesSquare, Pencil, RefreshCw, Trash2} from 'lucide-react'
import type {main, store} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {MAX_CHAT_TITLE, cleanChatTitle} from '../../lib/chatRename'
import {isCLI, type ChatProviderItem} from '../../lib/chatProviders'
import type {Health} from '../../lib/chatHealth'
import {IconButton} from '../ui/Button'
import {Input} from '../ui/Field'
import {Menu, useMenu, type MenuEntry} from '../ui/Menu'
import {ConversationsMenu} from './ConversationsMenu'
import {HealthPill} from './HealthIndicators'
import {ModeControl, modeTexts, type ModeControlProps} from './ModeControl'
import {ModelPicker} from './ModelPicker'
import {ProviderPicker} from './ProviderPicker'
import type {ChatTier} from './useChatTier'

// Barra superior del chat (S12.b): UNA línea compacta con el título de la
// conversación (editable), el proveedor con su píldora de latencia, el modelo, el
// chip del workspace y el modo. En un panel angosto lo secundario se pliega en el
// menú «…» —el chip, la latencia, «Chat nuevo», el modo—: la barra nunca se desborda
// ni hace scroll horizontal; el que no cabe, se trunca o pasa al menú. Qué se pliega
// y a qué ancho lo decide useChatTier. Es el encabezado de AgentChat de mini-tools
// (título editable, selector de proveedor propio, píldora, selector de modelo)
// sobre el dock de ScadLab.

export type ProviderControl = {
    providers: ChatProviderItem[]
    id: string
    onChange: (id: string) => void
    onAdd: () => void
    onOpen: () => void
    health: Health
    onRecheck: () => void
}

export type ModelControl = {
    value: string
    models: main.ModelInfo[]
    defaultHint: string
    loading: boolean
    error: string
    onChange: (id: string) => void
    onReload: () => void
    /** S15: abre la lista de modelos sola cada vez que cambia (y no es 0), filtrada a los que ven imágenes con `visionOnly`. */
    openSignal?: number
    visionOnly?: boolean
}

// El título de la conversación abierta: un clic lo edita en el lugar (Enter
// guarda, Esc cancela, salir del campo guarda). Sin conversación abierta es el
// rótulo «Chat nuevo», que no se edita.
// El título conserva un ancho mínimo (los selectores se acortan antes que él): es lo que dice de qué va la
// conversación. En un panel muy angosto (tiny) el mínimo baja para que todo siga cabiendo.
const titleMinWide = 'min-w-[5.5rem]'
const titleMinTiny = 'min-w-[3.5rem]'

function ChatTitle({current, editing, onEdit, onRename, tiny}: {current: store.Chat | null; editing: boolean; onEdit: (v: boolean) => void; onRename: (title: string) => void; tiny: boolean}) {
    const t = useT()
    const titleMin = tiny ? titleMinTiny : titleMinWide
    const [draft, setDraft] = useState('')
    const done = useRef(false)
    useEffect(() => {
        if (editing) {
            done.current = false
            setDraft(current?.title ?? '')
        }
    }, [editing, current?.title])

    if (!current) {
        return <span className={cn('min-w-0 flex-1 truncate px-1.5 text-ui-12-5 text-muted', titleMin)}>{t.chat.newChat}</span>
    }
    const name = current.title || t.chat.untitled
    const commit = () => {
        if (done.current) return
        done.current = true
        const clean = cleanChatTitle(draft)
        onEdit(false)
        if (clean && clean !== current.title) onRename(clean)
    }
    if (editing) {
        return (
            <Input
                autoFocus
                data-testid="chat-title-input"
                value={draft}
                maxLength={MAX_CHAT_TITLE * 2}
                title={t.chat.renameHint}
                placeholder={t.chat.renamePlaceholder}
                onChange={(e) => setDraft(e.target.value)}
                onFocus={(e) => e.currentTarget.select()}
                onBlur={commit}
                onKeyDown={(e) => {
                    if (e.key === 'Enter' && !e.nativeEvent.isComposing) {
                        e.preventDefault()
                        commit()
                    } else if (e.key === 'Escape') {
                        e.preventDefault()
                        e.stopPropagation()
                        done.current = true
                        onEdit(false)
                    }
                }}
                className={cn('h-7 flex-1', titleMin)}
            />
        )
    }
    return (
        <button
            type="button"
            data-testid="chat-title"
            title={fmt(t.chat.titleHint, {title: name})}
            aria-label={`${t.chat.titleLabel}: ${name}`}
            onClick={() => onEdit(true)}
            className={cn('group/title no-drag flex h-7 flex-1 items-center gap-1.5 rounded-md px-1.5 text-left text-ui-12-5 font-medium text-fg transition-colors hover:bg-surface-2', titleMin)}
        >
            <span className="min-w-0 truncate">{name}</span>
            <Pencil className="icon-ui-12 shrink-0 text-faint opacity-0 transition-opacity group-hover/title:opacity-100 group-focus-visible/title:opacity-100" />
        </button>
    )
}

export function ChatHeader({
    tier,
    chats,
    current,
    provider,
    model,
    mode,
    modeKind,
    workspaceName,
    onSelect,
    onNew,
    onRename,
    onDelete,
    running,
}: {
    tier: ChatTier
    chats: store.Chat[]
    current: store.Chat | null
    provider: ProviderControl
    model: ModelControl
    mode: ModeControlProps
    modeKind: 'cli' | 'server'
    workspaceName: string
    onSelect: (id: number) => void
    onNew: () => void
    onRename: (id: number, title: string) => void
    onDelete: (id: number) => void
    running: boolean
}) {
    const t = useT()
    const menu = useMenu()
    const [editing, setEditing] = useState(false)
    const [listSignal, setListSignal] = useState(0)
    useEffect(() => setEditing(false), [current?.id])

    const cli = isCLI(provider.id)
    const current_ = provider.providers.find((p) => p.id === provider.id)
    const providerName = (id: string) => provider.providers.find((p) => p.id === id)?.name ?? id
    const busy = running ? t.chat.modelBusyHint : ''
    const effectiveAgent = mode.agent && !mode.unavailable
    const showChip = tier === 'wide' && !!workspaceName
    // La píldora dice la latencia de un servidor; de un CLI solo diría «instalado», que ya dice el punto verde del selector.
    const showPill = (tier === 'wide' || tier === 'mid') && !cli
    const modeInline = tier !== 'tiny'
    const newInline = tier === 'wide'
    const small = tier === 'tiny'

    const entries: MenuEntry[] = []
    if (workspaceName && !showChip) {
        entries.push({id: 'workspace', label: fmt(t.chat.menu.workspace, {name: workspaceName}), title: t.chat.workspaceHint, icon: <Folder className="icon-ui-14" />, disabled: true})
    }
    if (!showPill && provider.id && !cli) {
        entries.push({
            id: 'recheck',
            label: provider.health.status === 'ok' && !cli ? fmt(t.chat.menu.recheckMs, {ms: provider.health.latencyMs}) : t.chat.menu.recheck,
            title: t.chat.health.recheckHint,
            icon: <RefreshCw className="icon-ui-14" />,
            onSelect: provider.onRecheck,
        })
    }
    if (!modeInline) {
        for (const agent of [false, true]) {
            const m = modeTexts(t, modeKind, agent)
            entries.push({
                id: agent ? 'mode-agent' : 'mode-ask',
                label: m.label,
                title: agent && mode.unavailable ? mode.unavailable : `${m.label}: ${m.hint}`,
                icon: agent ? <Bot className="icon-ui-14" /> : <Eye className="icon-ui-14" />,
                checked: agent === effectiveAgent,
                disabled: agent && !!mode.unavailable,
                onSelect: () => mode.onChange(agent),
            })
        }
    }
    if (entries.length > 0) entries.push({kind: 'separator'})
    entries.push({id: 'conversations', label: t.chat.menu.conversations, title: t.chat.chatsHint, icon: <MessagesSquare className="icon-ui-14" />, onSelect: () => setListSignal((n) => n + 1)})
    if (!newInline) {
        entries.push({id: 'new', label: t.chat.newChat, title: t.chat.newChatHint, icon: <MessageSquarePlus className="icon-ui-14" />, disabled: !current, onSelect: onNew})
    }
    if (current) {
        entries.push({id: 'rename', label: t.chat.renameThis, title: t.chat.renameHint, icon: <Pencil className="icon-ui-14" />, onSelect: () => setEditing(true)})
        entries.push({id: 'delete', label: t.chat.deleteThis, title: t.chat.deleteThis, icon: <Trash2 className="icon-ui-14" />, danger: true, onSelect: () => onDelete(current.id)})
    }

    return (
        // Una sola línea: lo que no cabe se trunca (los selectores) o va al menú «…»; nada hace scroll.
        <div data-chat-header className="flex h-9 shrink-0 items-center gap-1.5 overflow-hidden border-b border-line px-1.5">
            <ConversationsMenu chats={chats} current={current} providerName={providerName} onSelect={onSelect} onRename={onRename} onDelete={onDelete} iconOnly={small} noChevron={tier === 'narrow' || small} openSignal={listSignal} />
            <ChatTitle current={current} editing={editing} onEdit={setEditing} onRename={(title) => current && onRename(current.id, title)} tiny={small} />

            <ProviderPicker
                providers={provider.providers}
                value={provider.id}
                onChange={provider.onChange}
                onAdd={provider.onAdd}
                onOpen={provider.onOpen}
                health={provider.health}
                disabled={running}
                disabledHint={busy}
                title={current ? t.chat.providerSwitchHint : t.chat.providerHint}
                iconOnly={small}
                className={small ? undefined : 'max-w-[10rem] min-w-[7rem]'}
            />
            {showPill && provider.id ? <HealthPill health={provider.health} name={current_?.name ?? provider.id} cli={false} onRetry={provider.onRecheck} /> : null}
            <ModelPicker
                models={model.models}
                value={model.value}
                onChange={model.onChange}
                defaultHint={model.defaultHint}
                loading={model.loading}
                error={model.error}
                onReload={model.onReload}
                disabled={running}
                disabledHint={busy}
                cli={cli}
                iconOnly={small}
                className={small ? undefined : 'max-w-[10.5rem] min-w-[4.5rem]'}
                openSignal={model.openSignal}
                visionOnly={model.visionOnly}
            />
            {showChip ? (
                <span title={t.chat.workspaceHint} className="flex h-7 max-w-[9rem] min-w-0 shrink items-center gap-1.5 rounded-md border border-line px-2 text-ui-11-5 text-muted">
                    <Folder className="icon-ui-12 shrink-0 text-faint" />
                    <span className="truncate">{workspaceName}</span>
                </span>
            ) : null}
            {modeInline ? <ModeControl {...mode} kind={modeKind} iconOnly={tier === 'narrow'} /> : null}
            {newInline ? (
                <IconButton data-testid="chat-new" title={t.chat.newChatHint} onClick={onNew} disabled={!current} className="shrink-0">
                    <MessageSquarePlus className="icon-ui-14" />
                </IconButton>
            ) : null}
            <IconButton
                data-testid="chat-more"
                title={t.chat.menu.more}
                aria-haspopup="menu"
                aria-expanded={menu.open}
                onClick={menu.toggle}
                className={cn('shrink-0', menu.open && 'bg-surface-2 text-fg')}
            >
                <Ellipsis className="icon-ui-14" />
            </IconButton>
            {menu.anchor ? <Menu anchor={menu.anchor} entries={entries} onClose={menu.close} align="end" label={t.chat.menu.more} /> : null}
        </div>
    )
}
