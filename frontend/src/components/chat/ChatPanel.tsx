import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {Bot, Settings as SettingsIcon} from 'lucide-react'
import {CancelChat, ChatCheckpointParent, DeleteChat, GetChat, GetLastModel, ListChats, ListWorkspaces, NewChat, RenameChat, SaveChatCheckpoint, SendChatMessage, SetChatModel, SetLastModel} from '../../../wailsjs/go/main/App'
import type {store} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {contextUsage, estimateTokens} from '../../lib/chatContext'
import {useProviderHealth} from '../../lib/chatHealth'
import {isCLI} from '../../lib/chatProviders'
import {openProvidersSettings} from '../../lib/modelServers'
import {onChatDraft, onOpenChat, onProjectClosed, takePendingChat, takePendingDraft, type ChatDraft} from '../../lib/sessionBridge'
import type {VisionInfo} from '../../lib/chatAttachments'
import {SaveCheckpointDialog} from '../editor/SaveCheckpointDialog'
import {showPanel} from '../dock/dock'
import {Button} from '../ui/Button'
import {confirmDialog} from '../ui/Dialog'
import {toast} from '../ui/Toast'
import {DropOverlay} from './AttachmentChips'
import {ChatHeader} from './ChatHeader'
import {Composer, type AgentControl} from './Composer'
import {EmptyChat} from './EmptyChat'
import {HealthBanner} from './HealthIndicators'
import {withVault} from '../../lib/vault'
import {MessageList} from './Messages'
import {clearStream, ensureChatEvents, startStream, syncStream, useStreamFinished, useStreamRunning, useStreamStartedAt} from './chatStreams'
import {useChatAttachments} from './useChatAttachments'
import {useChatTier} from './useChatTier'
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

// Panel «Chat» del panel inferior: conversación de diseño con un proveedor de IA
// —un CLI agéntico detectado (Claude Code, Codex, Antigravity, OpenCode) o un
// servidor de modelos (Ollama, compatible con OpenAI, Anthropic)—. Las props son el
// contrato con Workbench (ver .claude/specs/s5-agentes-ia.md); las dos últimas son
// opcionales: sin `currentFile` el modelo no ve el código abierto, y
// `onOpenSettings` solo es el respaldo de «Agregar proveedor de IA…», que abre
// Configuración → Proveedores de IA por lib/modelServers.
export type ChatPanelProps = {
    workspaceId: number | null
    /** Abre un checkpoint (recién creado o mencionado en un paso) en el árbol, el editor y el visor. */
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
            <div className="flex flex-wrap items-center justify-center gap-2">
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

export default function ChatPanel({workspaceId, onCheckpointCreated, currentFile = null, onOpenSettings}: ChatPanelProps) {
    const t = useT()
    const [tier, tierRef] = useChatTier()
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
    // Nombre del workspace para el chip de la barra: el chat trabaja en su carpeta.
    const [workspaceName, setWorkspaceName] = useState('')
    useEffect(() => {
        setWorkspaceName('')
        if (workspaceId == null) return
        let stale = false
        ListWorkspaces()
            .then((l) => !stale && setWorkspaceName((l ?? []).find((w) => w.id === workspaceId)?.name ?? ''))
            .catch(() => {})
        return () => {
            stale = true
        }
    }, [workspaceId])

    // Proveedor y modelo del chat NUEVO (en uno abierto son los del chat). Modelo ''
    // = «Por defecto»: con un CLI es el que el CLI tenga configurado; con un servidor,
    // su modelo predeterminado (el backend lo resuelve).
    const [providerId, setProviderId] = useState('')
    const [model, setModel] = useState('')
    // La última elección de este proyecto (o, si no tiene, la global): de ahí
    // arranca un chat nuevo. undefined = todavía no se leyó.
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
    // Cambiar de proveedor en un chat abierto: si el nuevo no tiene un modelo que se
    // pueda resolver solo (un servidor sin modelo predeterminado ni recordado), se
    // elige primero el proveedor y luego el modelo, y recién con el modelo se guarda
    // (SetChatModel). Hasta entonces el chat sigue como estaba.
    const [switching, setSwitching] = useState<string | null>(null)
    useEffect(() => setSwitching(null), [chatId])
    // El modo se recuerda por tipo de proveedor (ver useChatProviders).
    const [agentCli, setAgentCli] = useState(() => rememberedAgent(true))
    const [agentServer, setAgentServer] = useState(() => rememberedAgent(false))
    const [maxSteps, setMaxSteps] = useState(rememberedSteps)

    useEffect(() => ensureChatEvents(), [])

    // ── Proveedor y modelo elegidos ─────────────────────────────────────────
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

    // El proveedor y el modelo que mira el selector: los del chat abierto (o el
    // proveedor al que se está pasando) o, sin chat, los del chat nuevo.
    const pickerProviderId = current ? (switching ?? current.providerId) : providerId
    const pickerModel = current ? (switching ? '' : current.model) : model
    const provider = providers.find((p) => p.id === pickerProviderId)
    const cli = isCLI(pickerProviderId)
    const configured = provider?.defaultModel ?? ''
    const {models, defaultHint, loading: modelsLoading, error: modelsError, reload: reloadModels} = useProviderModels(pickerProviderId, configured)
    const {health, refresh: recheckHealth} = useProviderHealth(pickerProviderId, workspaceId != null)
    // El modelo con el que se va a escribir; con «Por defecto» de un servidor, el
    // predeterminado del servidor (de un CLI no se sabe cuál es).
    const effectiveModel = pickerModel || (cli ? '' : configured)
    // Lo que se sabe del modelo: si ve imágenes (los adjuntos), si llama
    // herramientas («Agente») y cuánto contexto tiene.
    const activeInfo = effectiveModel && !switching ? models.find((m) => m.id === effectiveModel) : undefined
    const vision: VisionInfo = activeInfo ? {vision: activeInfo.vision, visionKnown: activeInfo.visionKnown} : null
    const noTools = !!activeInfo && activeInfo.toolsKnown && !activeInfo.tools
    const attach = useChatAttachments({workspaceId, vision})

    const lastLoaded = last !== undefined
    useEffect(() => {
        if (!providerId || !lastLoaded) return
        modelPicked.current = false
        const l = lastRef.current
        const fromLast = l?.serverId === providerId ? l.model : undefined
        const remembered = rememberedModel(providerId)
        setModel(isCLI(providerId) ? (fromLast ?? remembered) : fromLast || configured || remembered)
    }, [providerId, configured, lastLoaded])

    // Si el modelo que se puso solo (recordado) ya no existe en la lista, se pasa al
    // primero (servidor) o a «Por defecto» (CLI); el que eligió o escribió el
    // usuario se respeta.
    useEffect(() => {
        if (models.length === 0 || modelPicked.current || current) return
        if (cli) {
            if (model && !models.some((m) => m.id === model)) setModel('')
        } else if (!model || !models.some((m) => m.id === model)) setModel(models[0].id)
    }, [models, model, cli, current])

    // Cada elección se recuerda por proyecto y como la global.
    const remember = useCallback(
        (serverId: string, m: string) => {
            if (!serverId || workspaceId == null) return
            if (!m.trim() && !isCLI(serverId)) return
            setLast({serverId, model: m.trim()})
            void SetLastModel(workspaceId, serverId, m.trim()).catch(() => {})
        },
        [workspaceId],
    )

    // «Por defecto» en un servidor sin modelo predeterminado es «el primero que
    // devuelva el servidor» (el texto que dice el catálogo): hay que dar uno.
    const resolveModel = (id: string, m: string, list = models, def = configured) => (m === '' && !isCLI(id) ? def || list[0]?.id || '' : m)

    const onProvider = (id: string) => {
        if (id === pickerProviderId) return
        rememberProvider(id)
        const next = providers.find((p) => p.id === id)
        if (current) {
            // Con un modelo que se pueda resolver solo se cambia ya; si no, se espera la elección del modelo.
            const known = isCLI(id) ? '' : next?.defaultModel || rememberedModel(id)
            if (known !== '' || isCLI(id)) void changeChatModel(current.id, id, known)
            else setSwitching(id === current.providerId ? null : id)
            return
        }
        setProviderId(id)
        // Un proveedor nuevo arranca con su modelo por defecto o el que se usó con él
        // la última vez, no con el del proveedor anterior.
        modelPicked.current = false
        setModel(isCLI(id) ? rememberedModel(id) : rememberedModel(id) || next?.defaultModel || '')
    }
    const onModel = (m: string) => {
        const resolved = resolveModel(pickerProviderId, m)
        if (current) {
            void changeChatModel(current.id, pickerProviderId, resolved)
            return
        }
        modelPicked.current = true
        setModel(resolved)
        if (providerId) {
            rememberModel(providerId, resolved)
            remember(providerId, resolved)
        }
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

    // ── Modo ────────────────────────────────────────────────────────────────
    // «Agente» efectivo: el interruptor de este tipo de proveedor, salvo que el
    // modelo no llame herramientas.
    const agentOn = cli ? agentCli : agentServer
    const effectiveAgent = agentOn && !noTools
    const setAgent = (v: boolean) => {
        if (cli) setAgentCli(v)
        else setAgentServer(v)
        rememberAgent(cli, v)
    }

    // ── Contexto ────────────────────────────────────────────────────────────
    const historyTokens = useMemo(() => messages.reduce((n, m) => n + estimateTokens(m.content), 0), [messages])
    const context = contextUsage(historyTokens, text, activeInfo?.contextTokens ?? 0)

    // ── Enviar / detener ────────────────────────────────────────────────────
    const send = async () => {
        const body = text.trim()
        const att = attach.takeReady()
        if ((!body && att.ids.length === 0) || workspaceId == null || running || attach.sendBlocked) return
        let id = chatId
        try {
            if (id == null) {
                if (!providerId || (!isCLI(providerId) && !model.trim())) {
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

    // Cambiar a mitad de camino el proveedor y el modelo de un chat abierto.
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

    // ── Guardar un bloque como checkpoint / abrirlo en el editor ────────────
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
    // «Abrir en el editor»: ScadLab no tiene un borrador sin archivo (el editor
    // muestra siempre un checkpoint, y los checkpoints son inmutables), así que abrir
    // el código ES guardarlo como checkpoint nuevo —sin diálogo, con el nombre sacado
    // de su encabezado— y abrirlo: se selecciona en el árbol, el editor y el visor lo
    // siguen, y el panel Código se muestra si estaba oculto.
    const openInEditor = async (code: string) => {
        if (workspaceId == null) return
        try {
            const parent = await ChatCheckpointParent(workspaceId, currentFile ?? '')
            const desc = headerDescription(code)
            const name = await SaveChatCheckpoint(workspaceId, chatId ?? 0, parent, (desc || t.chat.openLabel).slice(0, 80), desc, code)
            toast.ok(fmt(t.chat.openedInEditor, {file: name}))
            onCheckpointCreated(name)
            showPanel('source')
        } catch (e) {
            toast.error(errorText(e))
        }
    }
    const askSaveRef = useRef(askSave)
    askSaveRef.current = askSave
    const openRef = useRef(openInEditor)
    openRef.current = openInEditor
    const openCheckpointRef = useRef(onCheckpointCreated)
    openCheckpointRef.current = onCheckpointCreated
    // Estable: los bloques ya pintados no se redibujan al cambiar el panel.
    const actions = useMemo(
        () => ({
            onSave: (code: string) => void askSaveRef.current(code),
            onOpen: (code: string) => void openRef.current(code),
            onOpenCheckpoint: (file: string) => openCheckpointRef.current(file),
        }),
        [],
    )

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

    // «Modificar con IA» (S13): un modelo ya adjuntado en Go y un texto sugerido. Es un tema nuevo, así que abre un chat
    // nuevo (con el proveedor y el modelo que ya estaban elegidos) y deja el texto escrito para que la persona lo termine.
    // El panel entra con lazy(): un borrador pedido antes de montarse queda pendiente (sessionBridge) y se toma acá.
    const draftRef = useRef<(d: ChatDraft) => void>(() => {})
    draftRef.current = (d) => {
        if (d.workspaceId !== workspaceId) return
        setChatId(null)
        setText(d.text)
        attach.addReady(d.attachment)
        window.setTimeout(() => {
            const box = document.querySelector<HTMLTextAreaElement>('[data-font-area="chat"] textarea')
            box?.focus()
            box?.setSelectionRange(box.value.length, box.value.length)
        }, 80)
    }
    useEffect(() => {
        // Con un respiro: en desarrollo React monta, desmonta y vuelve a montar el efecto de seguido, y el desmontaje
        // suelta los adjuntos (useChatAttachments). Tomar el borrador en el segundo montaje evita perderlo.
        const timer = window.setTimeout(() => {
            const pending = takePendingDraft()
            if (pending) draftRef.current(pending)
        }, 0)
        const off = onChatDraft((d) => draftRef.current(d))
        return () => {
            window.clearTimeout(timer)
            off()
        }
    }, [])

    // ── Render ──────────────────────────────────────────────────────────────
    if (workspaceId == null) {
        return <p className="px-4 py-3 text-ui-12-5 text-faint">{t.chat.noWorkspace}</p>
    }
    const openProviders = () => (openProvidersSettings() ? undefined : onOpenSettings?.())
    if (loaded && providers.length === 0) {
        return (
            <div ref={tierRef} className="h-full" onPointerEnter={() => refresh()}>
                <NoProviders onOpenSettings={openProviders} onRefresh={() => refresh(true)} />
            </div>
        )
    }

    const desc = saving ? headerDescription(saving.code) : ''
    const noModel = chatId == null && (!providerId || (!isCLI(providerId) && !model.trim()))
    const agent: AgentControl = {
        on: agentOn,
        onChange: setAgent,
        unavailable: noTools ? t.chat.agent.noTools : '',
        steps: maxSteps,
        onSteps: (n) => {
            setMaxSteps(n)
            rememberSteps(n)
        },
        kind: cli ? 'cli' : 'server',
    }
    const modelLabel = pickerModel ? (models.find((m) => m.id === pickerModel)?.label || pickerModel) : cli ? t.chat.modelDefault : configured || t.chat.modelDefault
    // Un chat con un proveedor que ya no está (CLI desinstalado, servidor apagado)
    // se puede leer pero no seguir: el banner dice por qué.
    const showBanner = !!pickerProviderId && health.status === 'error' && health.error !== ''
    return (
        <div ref={tierRef} data-font-area="chat" className="relative flex h-full min-h-0 flex-col" onPointerEnter={() => refresh()} {...attach.dragProps}>
            <ChatHeader
                tier={tier}
                chats={chats}
                current={current}
                provider={{providers, id: pickerProviderId, onChange: onProvider, onAdd: openProviders, onOpen: () => refresh(true), health, onRecheck: recheckHealth}}
                model={{value: pickerModel, models, defaultHint, loading: modelsLoading, error: modelsError, onChange: onModel, onReload: reloadModels}}
                mode={{agent: agentOn, onChange: setAgent, unavailable: noTools ? t.chat.agent.noTools : '', kind: cli ? 'cli' : 'server', disabled: false}}
                modeKind={cli ? 'cli' : 'server'}
                workspaceName={workspaceName}
                onSelect={setChatId}
                onNew={newChat}
                onRename={rename}
                onDelete={remove}
                running={running}
            />
            <MessageList
                chatId={chatId}
                messages={messages}
                actions={actions}
                empty={<EmptyChat provider={provider} modelLabel={modelLabel} agent={effectiveAgent} onPick={setText} />}
            />
            {showBanner ? <HealthBanner health={health} name={provider?.name ?? pickerProviderId} onRetry={recheckHealth} /> : null}
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
                context={context}
                tier={tier}
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
