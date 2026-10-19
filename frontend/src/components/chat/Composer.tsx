import {useEffect, useLayoutEffect, useRef, useState} from 'react'
import {ArrowUp, Bot, Eye, Paperclip, Square, X} from 'lucide-react'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {MAX_ATTACHMENTS, type VisionInfo} from '../../lib/chatAttachments'
import type {ContextUsage} from '../../lib/chatContext'
import {isMac, keys} from '../../lib/platform'
import {Button, IconButton} from '../ui/Button'
import {ComposerAttachments} from './AttachmentChips'
import {ContextMeter} from './ContextMeter'
import {modeTexts, type ModeKind} from './ModeControl'
import type {ChatTier} from './useChatTier'
import type {useChatAttachments} from './useChatAttachments'

export type AgentControl = {
    on: boolean
    onChange: (v: boolean) => void
    /** Por qué no se puede usar (el modelo no llama herramientas); vacío = se puede. */
    unavailable: string
    /** Tope de pasos de Hermes: solo con un servidor y «Agente». */
    steps: number
    onSteps: (n: number) => void
    kind: ModeKind
}

// 0:12, 1:05, 12:30… para el reloj del botón Detener.
function clock(sec: number): string {
    const m = Math.floor(sec / 60)
    return `${m}:${String(sec % 60).padStart(2, '0')}`
}

// Segundos desde `startedAt` mientras `running`. Un tick de 1 s: más fino no se lee.
function useElapsed(startedAt: number, running: boolean): number {
    const [now, setNow] = useState(() => Date.now())
    useEffect(() => {
        if (!running) return
        setNow(Date.now())
        const id = setInterval(() => setNow(Date.now()), 1000)
        return () => clearInterval(id)
    }, [running, startedAt])
    return running && startedAt > 0 ? Math.max(0, Math.floor((now - startedAt) / 1000)) : 0
}

// Caja de mensaje: crece con el texto hasta ~6 líneas y de ahí hace scroll.
//
// **Enter envía y Mayús+Enter agrega una línea** —como en el chat de mini-tools y
// como ya hacía el de S12—, y ⌘/Ctrl+Enter también envía, para quien lo tiene en la
// mano de otro chat. No se invirtió (Enter = línea, ⌘↩ = enviar) porque un mensaje
// de diseño suele ser una frase y el salto de línea es la excepción; los atajos
// quedan escritos bajo la caja. Mientras un IME compone (japonés, chino…) Enter
// confirma la composición y no envía.
//
// Debajo, lo que cambia lo que el modelo puede hacer: el modo con su explicación,
// el tope de pasos (solo Hermes) y, a la derecha, los atajos y el contador de
// contexto. Los archivos se adjuntan con el clip, pegándolos en la caja o
// arrastrándolos sobre el panel (el velo lo pinta ChatPanel).
export function Composer({
    value,
    onChange,
    onSend,
    onStop,
    running,
    startedAt,
    canSend,
    disabledReason,
    attach,
    vision,
    agent,
    context,
    tier,
}: {
    value: string
    onChange: (v: string) => void
    onSend: () => void
    onStop: () => void
    running: boolean
    startedAt: number
    canSend: boolean
    disabledReason?: string
    attach: ReturnType<typeof useChatAttachments>
    vision: VisionInfo
    agent: AgentControl
    context: ContextUsage
    tier: ChatTier
}) {
    const t = useT()
    const ta = useRef<HTMLTextAreaElement>(null)
    const elapsed = useElapsed(startedAt, running)

    useLayoutEffect(() => {
        const el = ta.current
        if (!el) return
        el.style.height = 'auto'
        el.style.height = `${Math.min(el.scrollHeight, 132)}px`
    }, [value])

    // Con adjuntos listos el texto puede ir vacío (se manda «revisa los adjuntos»).
    const hasContent = value.trim() !== '' || attach.readyCount > 0
    const ready = canSend && hasContent && !running && !attach.sendBlocked
    const why = attach.reason || disabledReason
    const effectiveAgent = agent.on && !agent.unavailable
    const mode = modeTexts(t, agent.kind, effectiveAgent)
    const ModeIcon = effectiveAgent ? Bot : Eye
    const note = agent.unavailable || mode.note
    const mac = isMac()
    const showHints = tier === 'wide' || tier === 'mid'
    const send = mac ? '↵' : 'Enter'
    const newline = mac ? '⇧↵' : 'Shift+Enter'
    return (
        <div className="shrink-0 border-t border-line bg-surface">
            <ComposerAttachments items={attach.items} vision={vision} onRemove={attach.remove} />
            {attach.chooseError ? (
                <p role="alert" className="mx-2 mt-2 flex items-start gap-1 text-ui-10-5 text-danger">
                    <span className="min-w-0 flex-1 break-words">{attach.chooseError}</span>
                    <IconButton title={t.chat.dismiss} onClick={attach.dismissError} className="-my-1 size-5 shrink-0">
                        <X className="icon-ui-12" />
                    </IconButton>
                </p>
            ) : null}
            <div className="px-3 pt-2 pb-1.5">
                <div className={cn('flex items-end gap-1.5 rounded-lg border border-line bg-surface-2 py-1.5 pr-1.5 pl-1.5 transition-colors focus-within:border-accent')}>
                    <IconButton
                        data-testid="chat-attach"
                        title={fmt(t.chat.attach.button, {max: MAX_ATTACHMENTS})}
                        onClick={() => void attach.choose()}
                        disabled={!attach.enabled || attach.choosing}
                        className="size-6 shrink-0"
                    >
                        <Paperclip className="icon-ui-14" />
                    </IconButton>
                    <textarea
                        ref={ta}
                        rows={1}
                        value={value}
                        title={why ?? fmt(t.chat.inputHint, {send: keys('mod+enter')})}
                        placeholder={t.chat.placeholder}
                        onChange={(e) => onChange(e.target.value)}
                        onPaste={(e) => {
                            // Una captura o un archivo pegado se adjunta; el texto, no.
                            if (attach.addFromTransfer(e.clipboardData, {pasted: true})) e.preventDefault()
                        }}
                        onKeyDown={(e) => {
                            if (e.key === 'Enter' && !e.shiftKey && !e.altKey && !e.nativeEvent.isComposing) {
                                e.preventDefault()
                                if (ready) onSend()
                            }
                        }}
                        className="selectable max-h-[132px] min-h-6 min-w-0 flex-1 resize-none bg-transparent py-0.5 text-chat leading-relaxed text-fg outline-none placeholder:text-faint"
                    />
                    {running ? (
                        <Button variant="outline" title={t.chat.stopHint} onClick={onStop} className="h-7">
                            <Square className="icon-ui-12 fill-current" />
                            {t.chat.stop}
                            <span className="font-mono text-ui-11 text-muted tabular-nums">{clock(elapsed)}</span>
                        </Button>
                    ) : (
                        <Button variant="primary" title={why ?? fmt(t.chat.sendHint, {keys: `${send} / ${keys('mod+enter')}`})} onClick={onSend} disabled={!ready} className="h-7">
                            <ArrowUp className="icon-ui-14" />
                            {tier === 'tiny' ? null : t.chat.send}
                        </Button>
                    )}
                </div>
                <div className="mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1">
                    <button
                        type="button"
                        data-testid="mode-note"
                        title={agent.unavailable || `${mode.label}: ${mode.hint}\n${t.chat.mode.toggleHint}`}
                        disabled={!!agent.unavailable}
                        onClick={() => agent.onChange(!agent.on)}
                        className="no-drag flex min-w-0 flex-1 basis-40 items-center gap-1.5 text-left"
                    >
                        <ModeIcon className={cn('icon-ui-12 shrink-0', effectiveAgent ? 'text-accent' : 'text-muted')} />
                        <span className={cn('shrink-0 text-ui-11-5 font-medium', effectiveAgent ? 'text-fg' : 'text-muted')}>{mode.label}</span>
                        <span className={cn('min-w-0 truncate text-ui-10-5', agent.unavailable ? 'text-warn' : 'text-faint')}>{note}</span>
                    </button>
                    {effectiveAgent && agent.kind === 'server' ? (
                        <label className="flex shrink-0 items-center gap-1.5 text-ui-11 text-muted" title={t.chat.agent.stepsHint}>
                            {t.chat.agent.steps}
                            <input
                                type="number"
                                min={1}
                                max={30}
                                value={agent.steps}
                                title={t.chat.agent.stepsHint}
                                onChange={(e) => {
                                    const n = Math.round(Number(e.target.value))
                                    if (Number.isFinite(n)) agent.onSteps(Math.min(30, Math.max(1, n)))
                                }}
                                className="selectable h-6 w-12 rounded-md border border-line bg-surface-2 px-1.5 text-center font-mono text-ui-11 text-fg outline-none focus:border-accent"
                            />
                        </label>
                    ) : null}
                    <div className="ml-auto flex shrink-0 items-center gap-3">
                        {showHints ? (
                            <span data-testid="shortcut-hint" title={fmt(t.chat.shortcutsHint, {send, newline, mod: keys('mod+enter')})} className="font-mono text-ui-10-5 whitespace-nowrap text-faint">
                                {fmt(t.chat.shortcuts, {send, newline})}
                            </span>
                        ) : null}
                        <ContextMeter usage={context} />
                    </div>
                </div>
            </div>
        </div>
    )
}
