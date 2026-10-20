import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {Bot, Settings as SettingsIcon} from 'lucide-react'
import {CancelChat, ChatCheckpointParent, DeleteChat, EnsureAntigravityMCP, GetChat, GetLastModel, ListChats, ListWorkspaces, NewChat, NewChatWithSummary, RemoveChatAttachment, RenameChat, SaveChatCheckpoint, SendChatMessage, SetChatMode, SetChatModel, SetChatToolProfile, SetLastModel} from '../../../wailsjs/go/main/App'
import type {store} from '../../../wailsjs/go/models'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'
import {contextAdvice, contextUsage, estimateTokens, fixedOverhead, windowLimit} from '../../lib/chatContext'
import {asksToCreate} from '../../lib/chatIntent'
import {useProviderHealth} from '../../lib/chatHealth'
import {chatModeValue, isAgentMode} from '../../lib/chatMode'
import {isCLI} from '../../lib/chatProviders'
import {openProvidersSettings} from '../../lib/modelServers'
import {fromImageShot} from '../../lib/fromImageShot'
import {onChatDraft, onFromImage, onOpenChat, onProjectClosed, takePendingChat, takePendingDraft, takePendingFromImage, type ChatDraft, type FromImageRequest} from '../../lib/sessionBridge'
import type {VisionInfo} from '../../lib/chatAttachments'
import {SaveCheckpointDialog} from '../editor/SaveCheckpointDialog'
import {showPanel} from '../dock/dock'
import {Button} from '../ui/Button'
import {confirmDialog} from '../ui/Dialog'
import {toast} from '../ui/Toast'
import {DropOverlay} from './AttachmentChips'
import {ChatHeader} from './ChatHeader'
import {Composer, type AgentControl} from './Composer'
import {ContextNotice} from './ContextNotice'
import {EmptyChat} from './EmptyChat'
import {FromImageDialog} from './FromImageDialog'
import {HealthBanner} from './HealthIndicators'
import {withVault} from '../../lib/vault'
import {MessageList} from './Messages'
import {NoVisionNotice} from './NoVisionNotice'
import {SwitchAgentNotice} from './SwitchAgentNotice'
import type {ToolProfile} from './ToolsControl'
import {clearStream, ensureChatEvents, setChatContext, startStream, syncStream, useChatContextInfo, useStreamFinished, useStreamRunning, useStreamStartedAt} from './chatStreams'
import {useChatAttachments} from './useChatAttachments'
import {useChatTier} from './useChatTier'
import {headerDescription} from './scadBlock'
import {
    rememberModel,
    rememberProvider,
    rememberSteps,
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
    // El modo es de cada conversación (chats.mode). Mientras no hay chat abierto, `draftAgent` es el de la
    // conversación que se va a crear: siempre arranca en «Agente» y solo pasa a consulta si se elige a mano.
    const [draftAgent, setDraftAgent] = useState(true)
    useEffect(() => setDraftAgent(true), [chatId, workspaceId])
    // El guardado en curso del último cambio de modo: un envío inmediato espera a que termine.
    const modeSaving = useRef<Promise<unknown>>(Promise.resolve())
    // S17: el perfil de herramientas también es de cada conversación (chats.tools). Mientras no hay chat abierto es un
    // borrador ('' = Auto) que se guarda en la conversación al crearla.
    const [draftTools, setDraftTools] = useState<ToolProfile>('')
    useEffect(() => setDraftTools(''), [chatId, workspaceId])
    const ctxInfo = useChatContextInfo(chatId)
    const [summarizing, setSummarizing] = useState(false)
    const [adviceDismissed, setAdviceDismissed] = useState('')
    const [maxSteps, setMaxSteps] = useState(rememberedSteps)
    // «Crear desde una imagen» (S15): el diálogo, la señal que abre el selector de modelos filtrado a los que ven imágenes
    // (aviso sin visión) y el envío automático del pedido que armó el diálogo (ver más abajo).
    const [fromImageOpen, setFromImageOpen] = useState(() => !!fromImageShot.open)
    const [visionSignal, setVisionSignal] = useState(0)
    const [autoSendBy, setAutoSendBy] = useState(0)

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
            if (v.context) setChatContext({...v.context, profile: v.context.profile === 'essential' || v.context.profile === 'full' ? v.context.profile : ''})
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
    // «Agente» de la conversación abierta (o de la que se va a crear). Elegir «Solo consulta» vale solo para esta
    // conversación: no cambia otra ni lo que arranca la próxima. «Agente» efectivo: el de la conversación, salvo que
    // el modelo no llame herramientas.
    const agentOn = current ? isAgentMode(current.mode) : draftAgent
    const effectiveAgent = agentOn && !noTools
    const setAgent = (v: boolean) => {
        if (!current) {
            setDraftAgent(v)
            return
        }
        const id = current.id
        // Se refleja ya en la lista (el interruptor no espera al disco) y, si no se pudo guardar, se vuelve a leer.
        setChats((prev) => prev.map((c) => (c.id === id ? {...c, mode: v ? '' : 'ask'} : c)))
        modeSaving.current = SetChatMode(id, chatModeValue(v)).catch((e) => {
            toast.error(errorText(e))
            if (workspaceId != null) void reloadChats(workspaceId)
        })
    }

    // ── Contexto ────────────────────────────────────────────────────────────
    // S17: la ventana es la efectiva del último turno (lo que se le pide a Ollama) y suma lo fijo de cada vuelta —prompt de
    // sistema y herramientas declaradas— que el servidor ya reportó; antes del primer turno, la del modelo.
    const historyTokens = useMemo(() => messages.reduce((n, m) => n + estimateTokens(m.content), 0), [messages])
    const toolsValue: ToolProfile = (current ? current.tools : draftTools) === 'essential' ? 'essential' : (current ? current.tools : draftTools) === 'full' ? 'full' : ''
    // Lo fijo de cada vuelta (prompt y herramientas) solo vale mientras el perfil elegido sea el del último turno: si recién se
    // cambió de perfil, esos números son los del anterior y se ignoran hasta que el próximo turno informe los nuevos.
    const freshInfo = ctxInfo && (toolsValue === '' || toolsValue === ctxInfo.profile) ? ctxInfo : null
    const context = contextUsage(historyTokens, text, windowLimit(ctxInfo, activeInfo?.contextTokens ?? 0), fixedOverhead(freshInfo))
    const advice = !cli && current && !running ? contextAdvice(freshInfo, context, true, messages.length > 0) : null
    const adviceKey = advice ? `${current?.id}:${advice.kind}` : ''
    const setTools = (p: ToolProfile) => {
        if (!current) {
            setDraftTools(p)
            return
        }
        const id = current.id
        setChats((prev) => prev.map((c) => (c.id === id ? {...c, tools: p} : c)))
        SetChatToolProfile(id, p).catch((e) => {
            toast.error(errorText(e))
            if (workspaceId != null) void reloadChats(workspaceId)
        })
    }
    // «Nueva conversación con resumen»: el modelo resume esta conversación y se sigue en una nueva que empieza con ese resumen.
    const summarize = async () => {
        if (!current || workspaceId == null || summarizing) return
        setSummarizing(true)
        try {
            const nc = await NewChatWithSummary(current.id)
            await reloadChats(workspaceId)
            setChatId(nc.id)
            toast.ok(t.chat.contextNotice.opened)
        } catch (e) {
            toast.error(errorText(e))
        } finally {
            setSummarizing(false)
        }
    }

    // ── Enviar / detener ────────────────────────────────────────────────────
    // `asFiles` (S15, «Usar trazado de contorno»): las fotos viajan solo como archivos de refs/images y el mensaje pide
    // usar trace_image_outline; se envía aunque el modelo haya informado que no ve imágenes.
    const doSend = async (asFiles: boolean) => {
        const typed = text.trim()
        const body = asFiles ? [typed, t.chat.noVision.traceInstruction].filter(Boolean).join('\n\n') : typed
        const att = attach.takeReady()
        if ((!body && att.ids.length === 0) || workspaceId == null || running || attach.uploading || (attach.sendBlocked && !asFiles)) return
        let id = chatId
        try {
            await modeSaving.current
            if (id == null) {
                if (!providerId || (!isCLI(providerId) && !model.trim())) {
                    toast.error(t.chat.pickModelFirst)
                    return
                }
                let c = await NewChat(workspaceId, providerId, model.trim())
                // La conversación nace en «Agente»; si en el panel vacío se eligió «Solo consulta», se guarda ya para ESTA.
                if (!draftAgent) {
                    await SetChatMode(c.id, chatModeValue(false))
                    c = {...c, mode: 'ask'}
                }
                if (draftTools !== '' && !isCLI(providerId)) {
                    await SetChatToolProfile(c.id, draftTools)
                    c = {...c, tools: draftTools}
                }
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
            // Sin `agent` el backend usa el modo de la conversación; solo se fuerza cuando el modelo no llama herramientas.
            const base = effectiveAgent === agentOn ? {maxSteps} : {agent: effectiveAgent, maxSteps}
            const um = await withVault(() => SendChatMessage(sendId, body, currentFile ?? '', att.ids, asFiles ? {...base, imagesAsFiles: true} : base))
            // Go ya tomó los adjuntos: los chips se sueltan sin pedir borrarlos.
            attach.consume(att.keys)
            setMessages((prev) => (prev.some((m) => m.id === um.id) ? prev : [...prev, um]))
            void reloadChats(workspaceId) // el primer mensaje ya le puso título al chat
        } catch (e) {
            if (id != null) clearStream(id)
            setText((cur) => cur || typed) // que no se pierda lo que se escribió
            toast.error(errorText(e))
        }
    }
    const send = () => void doSend(false)

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
    // muestra siempre un checkpoint, y el editor manual no reescribe: guardar crea uno hijo), así que abrir
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
    // «Crear checkpoint y renderizar» (S12.c, «Solo consulta»): lo mismo que «Abrir en el editor» y, además, el visor lo
    // renderiza aunque el auto-render esté apagado (ver adoptChatCheckpoint en App). Con el workspace vacío el
    // padre es '' y el código queda como la raíz del árbol.
    const createAndRender = async (code: string) => {
        if (workspaceId == null) return
        try {
            const parent = await ChatCheckpointParent(workspaceId, currentFile ?? '')
            const desc = headerDescription(code)
            const name = await SaveChatCheckpoint(workspaceId, chatId ?? 0, parent, (desc || t.chat.openLabel).slice(0, 80), desc, code)
            toast.ok(fmt(t.chat.createdRendering, {file: name}))
            onCheckpointCreated(name)
        } catch (e) {
            toast.error(errorText(e))
        }
    }
    const askSaveRef = useRef(askSave)
    askSaveRef.current = askSave
    const openRef = useRef(openInEditor)
    openRef.current = openInEditor
    const createRef = useRef(createAndRender)
    createRef.current = createAndRender
    const openCheckpointRef = useRef(onCheckpointCreated)
    openCheckpointRef.current = onCheckpointCreated
    // «Crear checkpoint y renderizar» solo se ofrece en «Solo consulta»: en «Agente» lo hace ScadLab solo.
    const offerCreate = !effectiveAgent
    // Estable: los bloques ya pintados no se redibujan al cambiar el panel (solo al cambiar de modo).
    const actions = useMemo(
        () => ({
            onSave: (code: string) => void askSaveRef.current(code),
            onOpen: (code: string) => void openRef.current(code),
            onCreate: offerCreate ? (code: string) => void createRef.current(code) : undefined,
            onOpenCheckpoint: (file: string) => openCheckpointRef.current(file),
        }),
        [offerCreate],
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
        setDraftAgent(true) // una conversación nueva es siempre «Agente»
        setText('')
    }

    // «Modificar con IA» (S13): un modelo ya adjuntado en Go y un texto sugerido. Es un tema nuevo, así que abre un chat
    // nuevo (con el proveedor y el modelo que ya estaban elegidos) y deja el texto escrito para que la persona lo termine.
    // El panel entra con lazy(): un borrador pedido antes de montarse queda pendiente (sessionBridge) y se toma acá.
    const draftRef = useRef<(d: ChatDraft) => void>(() => {})
    draftRef.current = (d) => {
        if (d.workspaceId !== workspaceId) return
        setChatId(null)
        setDraftAgent(true)
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

    // «Crear desde una imagen» (S15): el diálogo entrega las fotos (ya adjuntas en Go) y el mensaje estructurado. Se abre una
    // conversación nueva en «Agente», las fotos quedan como chips, el texto en la caja y, en cuanto el proveedor y el
    // modelo están listos, se envía. Con un modelo que informó que no ve imágenes NO se envía solo: queda todo en la
    // caja y el aviso ofrece las dos salidas (cambiar de modelo, o el trazado de contorno).
    const fromImageRef = useRef<(r: FromImageRequest) => void>(() => {})
    fromImageRef.current = (r) => {
        if (r.workspaceId !== workspaceId) {
            // Era de otro proyecto: sus fotos se borran, no se cuelan en este.
            for (const a of r.attachments) void RemoveChatAttachment(a.id).catch(() => {})
            return
        }
        setChatId(null)
        setDraftAgent(true)
        setText(r.text)
        for (const a of r.attachments) attach.addReady(a)
        setAutoSendBy(Date.now() + 5000)
    }
    useEffect(() => {
        const timer = window.setTimeout(() => {
            const pending = takePendingFromImage()
            if (pending) fromImageRef.current(pending)
        }, 0)
        const off = onFromImage((r) => fromImageRef.current(r))
        return () => {
            window.clearTimeout(timer)
            off()
        }
    }, [])
    // Sin lista de dependencias a propósito: espera a que el estado esté completo (conversación nueva elegida, texto y
    // fotos puestos, modelo resuelto y su visión conocida) y envía UNA vez; pasados 5 s se rinde y deja todo en la caja.
    const sendRef = useRef(send)
    sendRef.current = send
    useEffect(() => {
        if (!autoSendBy) return
        if (Date.now() > autoSendBy) {
            setAutoSendBy(0)
            return
        }
        const modelReady = chatId == null && !!providerId && (cli || !!model.trim())
        const visionKnown = cli || !!activeInfo || !!modelsError || (!modelsLoading && models.length > 0)
        if (workspaceId == null || !modelReady || !visionKnown || attach.uploading || text.trim() === '') return
        setAutoSendBy(0)
        if (!attach.sendBlocked) sendRef.current()
    })

    // ── Antigravity: ScadLab en su configuración global ─────────────────────
    // Antigravity solo lee MCP de su configuración global: al elegirlo (o abrir un chat suyo) el backend registra a
    // ScadLab si falta y avisa por el evento mcp:antigravity (App lo muestra como toast). Sin esa entrada no tiene
    // ninguna herramienta de ScadLab y el turno no deja nada.
    useEffect(() => {
        if (pickerProviderId !== 'cli:antigravity') return
        void EnsureAntigravityMCP().catch(() => {})
    }, [pickerProviderId])

    // ── Aviso «Cambiar a Agente» (S12.c) ────────────────────────────────────
    // En «Solo consulta», si lo que se escribe (o, con la caja vacía, lo último que se envió) pide crear o cambiar algo.
    const lastUser = useMemo(() => [...messages].reverse().find((m) => m.role === 'user')?.content ?? '', [messages])
    const [noticeDismissed, setNoticeDismissed] = useState('')
    const askText = text.trim() !== '' ? text : lastUser
    const showSwitch = !effectiveAgent && !noTools && !running && workspaceId != null && asksToCreate(askText) && noticeDismissed !== askText

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
                model={{value: pickerModel, models, defaultHint, loading: modelsLoading, error: modelsError, onChange: onModel, onReload: reloadModels, openSignal: visionSignal, visionOnly: true}}
                mode={{agent: agentOn, onChange: setAgent, unavailable: noTools ? t.chat.agent.noTools : '', kind: cli ? 'cli' : 'server', disabled: false}}
                modeKind={cli ? 'cli' : 'server'}
                tools={!cli && effectiveAgent ? {value: toolsValue, resolved: ctxInfo?.profile ?? '', onChange: setTools, disabled: running} : undefined}
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
                empty={<EmptyChat provider={provider} modelLabel={modelLabel} agent={effectiveAgent} onPick={setText} onFromImage={() => setFromImageOpen(true)} />}
            />
            {showBanner ? <HealthBanner health={health} name={provider?.name ?? pickerProviderId} onRetry={recheckHealth} /> : null}
            {advice && adviceDismissed !== adviceKey ? (
                <ContextNotice advice={advice} busy={summarizing} onSummarize={() => void summarize()} onNarrow={() => {
                        setTools('essential')
                        setAdviceDismissed(adviceKey)
                    }} onDismiss={() => setAdviceDismissed(adviceKey)} />
            ) : null}
            {showSwitch ? <SwitchAgentNotice onSwitch={() => setAgent(true)} onDismiss={() => setNoticeDismissed(askText)} /> : null}
            {attach.blockedCount > 0 ? <NoVisionNotice disabled={running} onSwitch={() => setVisionSignal((n) => n + 1)} onTrace={() => void doSend(true)} /> : null}
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
            <FromImageDialog open={fromImageOpen} workspaceId={workspaceId} onClose={() => setFromImageOpen(false)} />
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
