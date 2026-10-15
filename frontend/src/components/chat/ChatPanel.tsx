import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {Bot, Settings as SettingsIcon, Sparkles} from 'lucide-react'
import {CancelChat, ChatCheckpointParent, DeleteChat, GetChat, ListChats, NewChat, SaveAsCheckpoint, SendChatMessage} from '../../../wailsjs/go/main/App'
import type {store} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {SaveCheckpointDialog} from '../editor/SaveCheckpointDialog'
import {Button} from '../ui/Button'
import {confirmDialog} from '../ui/Dialog'
import {toast} from '../ui/Toast'
import {ChatHeader} from './ChatHeader'
import {Composer} from './Composer'
import {MessageList} from './Messages'
import {clearStream, ensureChatEvents, startStream, syncStream, useStreamFinished, useStreamRunning} from './chatStreams'
import {headerDescription} from './scadBlock'
import {rememberModel, rememberProvider, rememberedModel, rememberedProvider, useChatProviders, useProviderModels} from './useChatProviders'

// Panel «Chat» del panel inferior (S5, bloque 5.B): conversación de diseño con
// un modelo (Ollama, un servidor compatible con OpenAI o la API de Anthropic).
// Las props son el contrato con Workbench (ver .claude/specs/s5-agentes-ia.md);
// las dos últimas son opcionales: sin `currentFile` el modelo no ve el código
// abierto, y sin `onOpenSettings` el aviso de «sin proveedores» solo nombra la
// pantalla.
export type ChatPanelProps = {
    workspaceId: number | null
    /** Abre un checkpoint recién creado desde el chat (lo selecciona en el árbol). */
    onCheckpointCreated: (fileName: string) => void
    /** Checkpoint abierto en el editor: su código entra al prompt del modelo y es el padre por defecto al guardar. */
    currentFile?: string | null
    /** Abre Configuración → IA. */
    onOpenSettings?: () => void
}

function NoProviders({onOpenSettings, onRefresh}: {onOpenSettings?: () => void; onRefresh: () => void}) {
    const t = useT()
    return (
        <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
            <span className="flex size-9 items-center justify-center rounded-full bg-surface-2 text-faint">
                <Bot className="size-5" />
            </span>
            <div className="max-w-[460px]">
                <p className="text-[13px] font-medium text-fg">{t.chat.noProvidersTitle}</p>
                <p className="mt-1 text-[12.5px] leading-relaxed text-muted">{t.chat.noProvidersBody}</p>
            </div>
            <div className="flex items-center gap-2">
                {onOpenSettings ? (
                    <Button variant="primary" title={t.chat.openSettingsHint} onClick={onOpenSettings}>
                        <SettingsIcon className="size-3.5" />
                        {t.chat.openSettings}
                    </Button>
                ) : null}
                <Button title={t.chat.recheckHint} onClick={onRefresh}>
                    {t.chat.recheck}
                </Button>
            </div>
        </div>
    )
}

function EmptyChat({onPick}: {onPick: (text: string) => void}) {
    const t = useT()
    return (
        <div className="m-auto flex max-w-[520px] flex-col items-center gap-3 py-4 text-center">
            <span className="flex size-9 items-center justify-center rounded-full bg-accent-soft text-accent">
                <Sparkles className="size-5" />
            </span>
            <div>
                <p className="text-[13px] font-medium text-fg">{t.chat.emptyTitle}</p>
                <p className="mt-1 text-[12.5px] leading-relaxed text-muted">{t.chat.emptyBody}</p>
            </div>
            <div className="flex flex-wrap justify-center gap-1.5">
                {t.chat.suggestions.map((s) => (
                    <button
                        key={s}
                        type="button"
                        title={t.chat.suggestionHint}
                        onClick={() => onPick(s)}
                        className="no-drag rounded-full border border-line px-2.5 py-1 text-[12px] text-muted transition-colors hover:border-line-strong hover:bg-surface-2 hover:text-fg"
                    >
                        {s}
                    </button>
                ))}
            </div>
        </div>
    )
}

export default function ChatPanel({workspaceId, onCheckpointCreated, currentFile = null, onOpenSettings}: ChatPanelProps) {
    const t = useT()
    const {providers, loaded, refresh} = useChatProviders()
    const [chats, setChats] = useState<store.Chat[]>([])
    const [chatId, setChatId] = useState<number | null>(null)
    const [messages, setMessages] = useState<store.ChatMessage[]>([])
    const [text, setText] = useState('')
    const [saving, setSaving] = useState<{code: string; parent: string; busy: boolean} | null>(null)

    // Proveedor y modelo del chat NUEVO (en uno abierto son fijos).
    const [providerId, setProviderId] = useState('')
    const [model, setModel] = useState('')

    const running = useStreamRunning(chatId)
    const finished = useStreamFinished(chatId)
    const current = useMemo(() => chats.find((c) => c.id === chatId) ?? null, [chats, chatId])

    useEffect(() => ensureChatEvents(), [])

    // ── Proveedor y modelo elegidos ─────────────────────────────────────────
    useEffect(() => {
        if (providers.length === 0) return
        if (providers.some((p) => p.id === providerId)) return
        const remembered = rememberedProvider()
        setProviderId(providers.find((p) => p.id === remembered)?.id ?? providers[0].id)
    }, [providers, providerId])

    const provider = providers.find((p) => p.id === providerId)
    const configured = typeof provider?.config?.model === 'string' ? provider.config.model : ''
    const {models, loading: modelsLoading, error: modelsError, reload: reloadModels} = useProviderModels(providerId, configured)

    useEffect(() => {
        if (!providerId) return
        setModel(rememberedModel(providerId) || configured)
    }, [providerId, configured])

    // Si el modelo recordado ya no existe en el servidor, se pasa al primero.
    useEffect(() => {
        if (models.length > 0 && model && !models.includes(model)) setModel(models[0])
        else if (models.length > 0 && !model) setModel(models[0])
    }, [models, model])

    const picker = {
        providerId,
        model,
        models,
        loading: modelsLoading,
        error: modelsError,
        onProvider: (id: string) => {
            setProviderId(id)
            rememberProvider(id)
        },
        onModel: (m: string) => {
            setModel(m)
            if (providerId) rememberModel(providerId, m)
        },
        onReload: reloadModels,
    }

    // ── Chats del workspace ─────────────────────────────────────────────────
    const loadSeq = useRef(0)
    const openChat = useCallback(async (id: number) => {
        const seq = ++loadSeq.current
        try {
            const v = await GetChat(id)
            if (seq !== loadSeq.current) return
            setMessages(v.messages ?? [])
            syncStream(id, v)
            // Una respuesta que terminó con el panel cerrado ya está en `messages`.
            if (!v.running) clearStream(id)
        } catch (e) {
            if (seq === loadSeq.current) toast.error(errorText(e))
        }
    }, [])

    const reloadChats = useCallback(async (wsId: number) => {
        try {
            const list = (await ListChats(wsId)) ?? []
            setChats(list)
            return list
        } catch (e) {
            toast.error(errorText(e))
            return []
        }
    }, [])

    useEffect(() => {
        setChatId(null)
        setMessages([])
        setChats([])
        if (workspaceId == null) return
        let stale = false
        reloadChats(workspaceId).then((list) => {
            if (stale || list.length === 0) return
            setChatId(list[0].id) // sigue donde se quedó: el más reciente
        })
        return () => {
            stale = true
        }
    }, [workspaceId, reloadChats])

    // Un chat que acaba de crear `send` no se carga: su GetChat podría leer antes
    // de que se guarde el primer mensaje y, al llegar tarde, borrarlo de la lista.
    const justCreated = useRef<number | null>(null)
    useEffect(() => {
        if (chatId != null && justCreated.current === chatId) {
            justCreated.current = null
            return
        }
        if (chatId != null) void openChat(chatId)
        else {
            loadSeq.current++
            setMessages([])
        }
    }, [chatId, openChat])

    // Terminó la respuesta del chat abierto (o falló): se recarga lo guardado.
    // El texto en vivo sigue a la vista hasta que el mensaje guardado llega
    // (ver showStream en Messages) y recién entonces se descarta, sin parpadeo.
    useEffect(() => {
        if (chatId == null || !finished || workspaceId == null) return
        let stale = false
        void (async () => {
            await reloadChats(workspaceId)
            if (stale) return
            const seq = ++loadSeq.current
            try {
                const v = await GetChat(chatId)
                if (stale || seq !== loadSeq.current) return
                setMessages(v.messages ?? [])
            } catch {
                // se reintenta al reabrir el chat; el texto en vivo sigue a la vista
            }
        })()
        return () => {
            stale = true
        }
    }, [finished, chatId, workspaceId, reloadChats])

    // ── Enviar / detener ────────────────────────────────────────────────────
    const send = async () => {
        const body = text.trim()
        if (!body || workspaceId == null || running) return
        let id = chatId
        try {
            if (id == null) {
                if (!providerId || !model.trim()) {
                    toast.error(t.chat.pickModelFirst)
                    return
                }
                const c = await NewChat(workspaceId, providerId, model.trim())
                id = c.id
                justCreated.current = id
                setChats((prev) => [c, ...prev])
                setChatId(id)
            }
            // Antes de la llamada: los primeros trozos pueden llegar antes de que
            // el binding responda y necesitan un stream que los reciba.
            startStream(id)
            setText('')
            const um = await SendChatMessage(id, body, currentFile ?? '')
            setMessages((prev) => (prev.some((m) => m.id === um.id) ? prev : [...prev, um]))
            void reloadChats(workspaceId) // el primer mensaje ya le puso título al chat
        } catch (e) {
            if (id != null) clearStream(id)
            setText((cur) => cur || body) // que no se pierda lo que se escribió
            toast.error(errorText(e))
        }
    }

    const stop = () => chatId != null && void CancelChat(chatId)

    const remove = async () => {
        if (chatId == null || !current) return
        const ok = await confirmDialog(fmt(t.chat.deleteConfirm, {title: current.title || t.chat.untitled}), t.chat.delete, t.editor.cancel)
        if (!ok) return
        try {
            await DeleteChat(chatId)
            clearStream(chatId)
            const rest = chats.filter((c) => c.id !== chatId)
            setChats(rest)
            setChatId(rest[0]?.id ?? null)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    // ── Guardar un bloque como checkpoint ───────────────────────────────────
    // El padre es el archivo abierto; si no hay, el último checkpoint; si el
    // workspace no tiene ninguno, el backend responde con un error claro.
    const askSave = async (code: string) => {
        if (workspaceId == null) return
        try {
            const parent = await ChatCheckpointParent(workspaceId, currentFile ?? '')
            setSaving({code, parent, busy: false})
        } catch (e) {
            toast.error(errorText(e))
        }
    }
    const askSaveRef = useRef(askSave)
    askSaveRef.current = askSave
    // Estable: los bloques ya pintados no se redibujan al cambiar el panel.
    const actions = useMemo(() => ({onSave: (code: string) => void askSaveRef.current(code)}), [])

    const doSave = async (label: string, description: string) => {
        if (!saving || workspaceId == null) return
        setSaving({...saving, busy: true})
        try {
            const name = await SaveAsCheckpoint(workspaceId, saving.parent, label, description, saving.code)
            toast.ok(fmt(t.editor.saved, {file: name}))
            setSaving(null)
            onCheckpointCreated(name)
        } catch (e) {
            toast.error(errorText(e))
            setSaving((s) => (s ? {...s, busy: false} : s))
        }
    }

    const newChat = () => {
        setChatId(null)
        setText('')
    }

    // ── Render ──────────────────────────────────────────────────────────────
    if (workspaceId == null) {
        return <p className="px-4 py-3 text-[12.5px] text-faint">{t.chat.noWorkspace}</p>
    }
    if (loaded && providers.length === 0) {
        return (
            <div className="h-full" onPointerEnter={() => refresh()}>
                <NoProviders onOpenSettings={onOpenSettings} onRefresh={() => refresh(true)} />
            </div>
        )
    }

    const desc = saving ? headerDescription(saving.code) : ''
    const noModel = chatId == null && (!providerId || !model.trim())
    return (
        <div className="flex h-full min-h-0 flex-col" onPointerEnter={() => refresh()}>
            <ChatHeader
                chats={chats}
                current={current}
                providers={providers}
                picker={picker}
                onSelect={setChatId}
                onNew={newChat}
                onDelete={remove}
                running={running}
            />
            <MessageList chatId={chatId} messages={messages} actions={actions} empty={<EmptyChat onPick={setText} />} />
            <Composer
                value={text}
                onChange={setText}
                onSend={send}
                onStop={stop}
                running={running}
                canSend={!noModel}
                disabledReason={noModel ? t.chat.pickModelFirst : undefined}
            />
            <SaveCheckpointDialog
                open={!!saving}
                parent={saving?.parent ?? ''}
                saving={!!saving?.busy}
                initial={desc ? {label: desc.slice(0, 80), description: desc} : undefined}
                onCancel={() => setSaving(null)}
                onSave={doSave}
            />
        </div>
    )
}
