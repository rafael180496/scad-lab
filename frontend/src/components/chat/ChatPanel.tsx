import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {Bot, Settings as SettingsIcon, Sparkles} from 'lucide-react'
import {CancelChat, ChatCheckpointParent, DeleteChat, GetChat, GetLastModel, ListChats, NewChat, RenameChat, SaveChatCheckpoint, SendChatMessage, SetChatModel, SetLastModel} from '../../../wailsjs/go/main/App'
import type {store} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {openProvidersSettings} from '../../lib/modelServers'
import {onOpenChat, onProjectClosed, takePendingChat} from '../../lib/sessionBridge'
import type {VisionInfo} from '../../lib/chatAttachments'
import {SaveCheckpointDialog} from '../editor/SaveCheckpointDialog'
import {Button} from '../ui/Button'
import {confirmDialog} from '../ui/Dialog'
import {toast} from '../ui/Toast'
import {DropOverlay} from './AttachmentChips'
import {ChatHeader} from './ChatHeader'
import {Composer, type AgentControl} from './Composer'
import {withVault} from '../../lib/vault'
import {MessageList} from './Messages'
import {clearStream, ensureChatEvents, startStream, syncStream, useStreamFinished, useStreamRunning, useStreamStartedAt} from './chatStreams'
import {useChatAttachments} from './useChatAttachments'
import {headerDescription} from './scadBlock'
import {
    rememberAgent,
    rememberModel,
    rememberProvider,
    rememberSteps,
    rememberedAgent,
    rememberedModel,
    rememberedProvider,
    rememberedSteps,
    useChatProviders,
    useProviderModels,
} from './useChatProviders'

// Panel «Chat» del panel inferior (S5, bloque 5.B): conversación de diseño con
// un modelo (Ollama, un servidor compatible con OpenAI o la API de Anthropic).
// Las props son el contrato con Workbench (ver .claude/specs/s5-agentes-ia.md);
// las dos últimas son opcionales: sin `currentFile` el modelo no ve el código
// abierto, y `onOpenSettings` solo es el respaldo del aviso de «sin servidores»,
// que abre Configuración → Proveedores de IA por lib/modelServers.
export type ChatPanelProps = {
    workspaceId: number | null
    /** Abre un checkpoint recién creado desde el chat (lo selecciona en el árbol). */
    onCheckpointCreated: (fileName: string) => void
    /** Checkpoint abierto en el editor: su código entra al prompt del modelo y es el padre por defecto al guardar. */
    currentFile?: string | null
    /** Respaldo para abrir Configuración si nadie escucha el puente de lib/modelServers. */
    onOpenSettings?: () => void
}

// Chat elegido en cada proyecto abierto (S8): al cambiar de pestaña de proyecto el
// panel vuelve al chat que tenía ese proyecto, o al «Nuevo chat» si lo había
// dejado así (null), en vez de saltar al más reciente.
const chosenChat = new Map<number, number | null>()

function NoProviders({onOpenSettings, onRefresh}: {onOpenSettings?: () => void; onRefresh: () => void}) {
    const t = useT()
    return (
        <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
            <span className="flex icon-ui-36 items-center justify-center rounded-full bg-surface-2 text-faint">
                <Bot className="icon-ui-20" />
            </span>
            <div className="max-w-[460px]">
                <p className="text-ui-13 font-medium text-fg">{t.chat.noProvidersTitle}</p>
                <p className="mt-1 text-ui-12-5 leading-relaxed text-muted">{t.chat.noProvidersBody}</p>
            </div>
            <div className="flex items-center gap-2">
                {onOpenSettings ? (
                    <Button variant="primary" title={t.chat.openSettingsHint} onClick={onOpenSettings}>
                        <SettingsIcon className="icon-ui-14" />
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
            <span className="flex icon-ui-36 items-center justify-center rounded-full bg-accent-soft text-accent">
                <Sparkles className="icon-ui-20" />
            </span>
            <div>
                <p className="text-ui-13 font-medium text-fg">{t.chat.emptyTitle}</p>
                <p className="mt-1 text-ui-12-5 leading-relaxed text-muted">{t.chat.emptyBody}</p>
            </div>
            <div className="flex flex-wrap justify-center gap-1.5">
                {t.chat.suggestions.map((s) => (
                    <button
                        key={s}
                        type="button"
                        title={t.chat.suggestionHint}
                        onClick={() => onPick(s)}
                        className="no-drag rounded-full border border-line px-2.5 py-1 text-ui-12 text-muted transition-colors hover:border-line-strong hover:bg-surface-2 hover:text-fg"
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
    const [chatId, setChatIdRaw] = useState<number | null>(null)
    // Cualquier elección del usuario se anota para su proyecto; los reinicios por
    // cambio de proyecto usan setChatIdRaw, que no anota nada.
    const wsRef = useRef(workspaceId)
    wsRef.current = workspaceId
    const setChatId = useCallback((id: number | null) => {
        setChatIdRaw(id)
        if (wsRef.current != null) chosenChat.set(wsRef.current, id)
    }, [])
    const [messages, setMessages] = useState<store.ChatMessage[]>([])
    const [text, setText] = useState('')
    const [saving, setSaving] = useState<{code: string; parent: string; busy: boolean} | null>(null)

    // Servidor y modelo del chat NUEVO (en uno abierto son fijos).
    const [providerId, setProviderId] = useState('')
    const [model, setModel] = useState('')
    // El último modelo elegido en este proyecto (o, si no tiene, el global): de
    // ahí arranca un chat nuevo. undefined = todavía no se leyó.
    const [last, setLast] = useState<{serverId: string; model: string} | null | undefined>(undefined)
    const lastRef = useRef(last)
    lastRef.current = last
    // El usuario eligió (o escribió) el modelo: una lista que lo desconoce no lo
    // reemplaza. Solo se reemplaza el que se puso solo (recordado o por defecto).
    const modelPicked = useRef(false)

    const running = useStreamRunning(chatId)
    const finished = useStreamFinished(chatId)
    const startedAt = useStreamStartedAt(chatId)
    const current = useMemo(() => chats.find((c) => c.id === chatId) ?? null, [chats, chatId])
    // Cambiar de servidor en un chat abierto: se elige primero el servidor y luego
    // el modelo, y recién con el modelo se guarda (SetChatModel). Hasta entonces el
    // chat sigue como estaba.
    const [switching, setSwitching] = useState<string | null>(null)
    useEffect(() => setSwitching(null), [chatId])
    // «Agente» y su tope de pasos: se recuerdan entre sesiones.
    const [agentOn, setAgentOn] = useState(rememberedAgent)
    const [maxSteps, setMaxSteps] = useState(rememberedSteps)

    useEffect(() => ensureChatEvents(), [])

    // ── Servidor y modelo elegidos ──────────────────────────────────────────
    useEffect(() => {
        setLast(undefined)
        if (workspaceId == null) return
        let stale = false
        GetLastModel(workspaceId)
            .then((lm) => !stale && setLast(lm?.serverId ? {serverId: lm.serverId, model: lm.model} : null))
            .catch(() => !stale && setLast(null))
        return () => {
            stale = true
        }
    }, [workspaceId])

    useEffect(() => {
        if (providers.length === 0 || last === undefined) return
        if (providers.some((p) => p.id === providerId)) return
        const wanted = [last?.serverId, rememberedProvider()]
        setProviderId(providers.find((p) => wanted.includes(p.id))?.id ?? providers[0].id)
    }, [providers, providerId, last])

    // El servidor y el modelo que mira el selector: los del chat abierto (o el
    // servidor al que se está pasando) o, sin chat, los del chat nuevo.
    const pickerProviderId = current ? (switching ?? current.providerId) : providerId
    const pickerModel = current ? (switching ? '' : current.model) : model
    const provider = providers.find((p) => p.id === pickerProviderId)
    const configured = provider?.defaultModel ?? ''
    const {models, loading: modelsLoading, error: modelsError, reload: reloadModels} = useProviderModels(pickerProviderId, configured)
    // Lo que el servidor sabe del modelo con el que se va a escribir: si ve
    // imágenes (los adjuntos) y si llama herramientas («Agente»).
    const activeInfo = current && !switching ? models.find((m) => m.id === current.model) : !current ? models.find((m) => m.id === model) : undefined
    const vision: VisionInfo = activeInfo ? {vision: activeInfo.vision, visionKnown: activeInfo.visionKnown} : null
    const noTools = !!activeInfo && activeInfo.toolsKnown && !activeInfo.tools
    const attach = useChatAttachments({workspaceId, vision})

    const lastLoaded = last !== undefined
    useEffect(() => {
        if (!providerId || !lastLoaded) return
        modelPicked.current = false
        const l = lastRef.current
        const fromLast = l?.serverId === providerId ? l.model : ''
        setModel(fromLast || configured || rememberedModel(providerId))
    }, [providerId, configured, lastLoaded])

    // Si el modelo que se puso solo (recordado) ya no existe en el servidor, se
    // pasa al primero; el que eligió o escribió el usuario se respeta.
    useEffect(() => {
        if (models.length === 0 || modelPicked.current) return
        if (!model || !models.some((m) => m.id === model)) setModel(models[0].id)
    }, [models, model])

    // Cada elección se recuerda por proyecto y como la global.
    const remember = useCallback(
        (serverId: string, m: string) => {
            if (!serverId || !m.trim() || workspaceId == null) return
            setLast({serverId, model: m.trim()})
            void SetLastModel(workspaceId, serverId, m.trim()).catch(() => {})
        },
        [workspaceId],
    )

    const picker = {
        providerId: pickerProviderId,
        model: pickerModel,
        models,
        loading: modelsLoading,
        error: modelsError,
        onProvider: (id: string) => {
            if (current) {
                setSwitching(id === current.providerId ? null : id)
                return
            }
            setProviderId(id)
            rememberProvider(id)
            // Un servidor nuevo arranca con su modelo por defecto o el que se usó
            // con él la última vez, no con el del servidor anterior.
            modelPicked.current = false
            setModel(rememberedModel(id) || providers.find((p) => p.id === id)?.defaultModel || '')
        },
        onModel: (m: string) => {
            if (current) {
                void changeChatModel(current.id, pickerProviderId, m)
                return
            }
            modelPicked.current = true
            setModel(m)
            if (providerId) {
                rememberModel(providerId, m)
                remember(providerId, m)
            }
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

    // Un chat pedido desde afuera («Abrir el chat» de un checkpoint): mientras
    // llega, la selección por defecto del workspace no debe pisarlo.
    const requested = useRef<number | null>(null)
    useEffect(() => {
        setChatIdRaw(null)
        setMessages([])
        setChats([])
        if (workspaceId == null) return
        let stale = false
        reloadChats(workspaceId).then((list) => {
            if (stale || list.length === 0 || requested.current != null) return
            // Vuelve al chat que tenía este proyecto; si lo dejó en «Nuevo chat»
            // (null) se respeta; sin elección previa, el más reciente.
            const kept = chosenChat.get(workspaceId)
            if (kept === null) return
            setChatIdRaw(list.find((c) => c.id === kept)?.id ?? list[0].id)
        })
        return () => {
            stale = true
        }
    }, [workspaceId, reloadChats])

    const openRequested = useCallback(
        async (id: number) => {
            if (workspaceId == null) return
            requested.current = id
            const list = await reloadChats(workspaceId)
            requested.current = null
            if (list.some((c) => c.id === id)) setChatId(id)
        },
        [workspaceId, reloadChats],
    )
    useEffect(() => onProjectClosed((ws) => void chosenChat.delete(ws)), [])
    useEffect(() => {
        const pending = takePendingChat()
        if (pending != null) void openRequested(pending)
        return onOpenChat((id) => void openRequested(id))
    }, [openRequested])

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
    // «Agente» efectivo: el interruptor, salvo que el modelo no llame herramientas.
    const effectiveAgent = agentOn && !noTools

    const send = async () => {
        const body = text.trim()
        const att = attach.takeReady()
        if ((!body && att.ids.length === 0) || workspaceId == null || running || attach.sendBlocked) return
        let id = chatId
        try {
            if (id == null) {
                if (!providerId || !model.trim()) {
                    toast.error(t.chat.pickModelFirst)
                    return
                }
                const c = await NewChat(workspaceId, providerId, model.trim())
                remember(providerId, model)
                id = c.id
                justCreated.current = id
                setChats((prev) => [c, ...prev])
                setChatId(id)
            }
            // Antes de la llamada: los primeros trozos pueden llegar antes de que
            // el binding responda y necesitan un stream que los reciba.
            startStream(id)
            setText('')
            // Con la contraseña maestra sin crear o la llave sin recuperar, el backend
            // responde con un error de código y withVault abre el diálogo y reintenta
            // una vez (lib/vault.ts). Cancelarlo deja el error y conserva lo escrito.
            const sendId = id
            const um = await withVault(() => SendChatMessage(sendId, body, currentFile ?? '', att.ids, {agent: effectiveAgent, maxSteps}))
            // Go ya tomó los adjuntos: los chips se sueltan sin pedir borrarlos.
            attach.consume(att.keys)
            setMessages((prev) => (prev.some((m) => m.id === um.id) ? prev : [...prev, um]))
            void reloadChats(workspaceId) // el primer mensaje ya le puso título al chat
        } catch (e) {
            if (id != null) clearStream(id)
            setText((cur) => cur || body) // que no se pierda lo que se escribió
            toast.error(errorText(e))
        }
    }

    const stop = () => chatId != null && void CancelChat(chatId)

    // Cambiar a mitad de camino el servidor y el modelo de un chat abierto.
    const changeChatModel = async (id: number, serverId: string, m: string) => {
        if (workspaceId == null) return
        try {
            await SetChatModel(id, serverId, m)
            setSwitching(null)
            rememberProvider(serverId)
            rememberModel(serverId, m)
            setLast({serverId, model: m})
            await reloadChats(workspaceId)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const rename = async (id: number, title: string) => {
        if (workspaceId == null) return
        try {
            await RenameChat(id, title)
            await reloadChats(workspaceId)
        } catch (e) {
            toast.error(errorText(e))
        }
    }

    const remove = async (id: number) => {
        const target = chats.find((c) => c.id === id)
        if (!target) return
        const ok = await confirmDialog(fmt(t.chat.deleteConfirm, {title: target.title || t.chat.untitled}), t.chat.delete, t.editor.cancel, {danger: true})
        if (!ok) return
        try {
            await DeleteChat(id)
            clearStream(id)
            const rest = chats.filter((c) => c.id !== id)
            setChats(rest)
            if (id === chatId) setChatId(rest[0]?.id ?? null)
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
            const name = await SaveChatCheckpoint(workspaceId, chatId ?? 0, saving.parent, label, description, saving.code)
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
        return <p className="px-4 py-3 text-ui-12-5 text-faint">{t.chat.noWorkspace}</p>
    }
    if (loaded && providers.length === 0) {
        return (
            <div className="h-full" onPointerEnter={() => refresh()}>
                <NoProviders onOpenSettings={() => (openProvidersSettings() ? undefined : onOpenSettings?.())} onRefresh={() => refresh(true)} />
            </div>
        )
    }

    const desc = saving ? headerDescription(saving.code) : ''
    const noModel = chatId == null && (!providerId || !model.trim())
    const agent: AgentControl = {
        on: agentOn,
        onChange: (v) => {
            setAgentOn(v)
            rememberAgent(v)
        },
        unavailable: noTools ? t.chat.agent.noTools : '',
        steps: maxSteps,
        onSteps: (n) => {
            setMaxSteps(n)
            rememberSteps(n)
        },
    }
    return (
        <div data-font-area="chat" className="relative flex h-full min-h-0 flex-col" onPointerEnter={() => refresh()} {...attach.dragProps}>
            <ChatHeader
                chats={chats}
                current={current}
                providers={providers}
                picker={picker}
                onSelect={setChatId}
                onNew={newChat}
                onRename={rename}
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
                startedAt={startedAt}
                canSend={!noModel}
                disabledReason={noModel ? t.chat.pickModelFirst : undefined}
                attach={attach}
                vision={vision}
                agent={agent}
            />
            {attach.dragging ? <DropOverlay /> : null}
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
