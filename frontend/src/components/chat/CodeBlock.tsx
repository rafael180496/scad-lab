import {useRef, useState} from 'react'
import {Check, ChevronDown, Copy, FileCode2, GitBranchPlus, Hammer} from 'lucide-react'
import {copyText} from '../../lib/clipboard'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {isScadBlock} from './scadBlock'

export type CodeActions = {
    /** Guardar este bloque como checkpoint (con el diálogo de nombre); sin ella (o con `undefined`) no hay botón. */
    onSave?: (code: string) => void
    /** Abrir este bloque en el editor: lo guarda como checkpoint nuevo, sin diálogo, y lo abre. */
    onOpen?: (code: string) => void
    /**
     * Crear un checkpoint con este código, sin diálogo, y renderizarlo en el visor (S12.c). Solo se pasa en
     * «Solo consulta»: ahí el modelo propone el código y este es el botón que lo convierte en una pieza. En
     * «Agente» lo hace ScadLab solo (el paso «Guardado automáticamente»).
     */
    onCreate?: (code: string) => void
}

/** Lo que el chat deja hacer sobre lo que dice un modelo: sus bloques de código y los checkpoints que nombra. */
export type ChatActions = CodeActions & {
    /** Selecciona un checkpoint en el árbol y lo abre en el editor y el visor. */
    onOpenCheckpoint?: (file: string) => void
}

// Un bloque más largo que esto arranca plegado: un archivo entero dentro de una
// respuesta empujaría fuera de la vista el texto que lo explica (y, con Hermes,
// los pasos que vienen después).
const COLLAPSE_LINES = 30
const COLLAPSED_LINES = 14

// Bloque de código de una respuesta. Los de OpenSCAD son los que importan:
// llevan «Guardar como checkpoint» y «Abrir en el editor» (solo cuando el cerco ya
// cerró —durante el streaming el código está a medias y guardarlo crearía un
// checkpoint roto—) y todos llevan «Copiar». Con el panel angosto los botones
// pierden el rótulo y quedan como ícono con su `title`.
export function CodeBlock({lang, code, closed, actions}: {lang: string; code: string; closed: boolean; actions?: CodeActions}) {
    const t = useT()
    const [copied, setCopied] = useState(false)
    const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
    const scad = isScadBlock(lang, code)
    const canSave = scad && closed && !!actions?.onSave && code.trim() !== ''
    const canCreate = scad && closed && !!actions?.onCreate && code.trim() !== ''
    // «Crear y renderizar» hace lo de «Abrir en el editor» y además renderiza: con él puesto, el otro sobra.
    const canOpen = scad && closed && !canCreate && !!actions?.onOpen && code.trim() !== ''
    const [expanded, setExpanded] = useState(false)
    const lines = code.split('\n')
    // Mientras llega (closed=false) no se pliega: el bloque está creciendo.
    const collapsible = closed && lines.length > COLLAPSE_LINES
    const shown = collapsible && !expanded ? lines.slice(0, COLLAPSED_LINES).join('\n') : code

    const copy = async () => {
        if (!(await copyText(code))) return
        setCopied(true)
        clearTimeout(timer.current)
        timer.current = setTimeout(() => setCopied(false), 1500)
    }

    return (
        <div className="@container my-2 overflow-hidden rounded-lg border border-line bg-bg">
            <div className="flex h-7 items-center gap-1 border-b border-line bg-surface-2 pr-1 pl-2.5">
                <span className={cn('font-mono text-ui-10 tracking-[0.12em] uppercase', scad ? 'text-accent' : 'text-faint')}>
                    {scad ? 'OpenSCAD' : lang || t.chat.code}
                </span>
                {lines.length > 3 ? <span className="font-mono text-ui-10 text-faint">{fmt(t.chat.codeLines, {n: lines.length})}</span> : null}
                <div className="ml-auto flex items-center gap-0.5">
                    <button
                        type="button"
                        title={t.chat.copyHint}
                        onClick={copy}
                        className="no-drag inline-flex h-5 items-center gap-1 rounded px-1.5 text-ui-11 text-muted transition-colors duration-100 hover:bg-raised hover:text-fg"
                    >
                        {copied ? <Check className="icon-ui-12 text-ok" /> : <Copy className="icon-ui-12" />}
                        <span className="hidden @min-[34rem]:inline">{copied ? t.chat.copied : t.chat.copy}</span>
                    </button>
                    {canCreate ? (
                        <button
                            type="button"
                            data-testid="code-create"
                            title={t.chat.createHint}
                            onClick={() => actions?.onCreate?.(code)}
                            className="no-drag inline-flex h-5 items-center gap-1 rounded bg-accent px-1.5 text-ui-11 font-medium text-accent-fg transition-[filter] duration-100 hover:brightness-110"
                        >
                            <Hammer className="icon-ui-12" />
                            <span className="hidden @min-[22rem]:inline @min-[34rem]:hidden">{t.chat.createShort}</span>
                            <span className="hidden @min-[34rem]:inline">{t.chat.create}</span>
                        </button>
                    ) : null}
                    {canOpen ? (
                        <button
                            type="button"
                            data-testid="code-open"
                            title={t.chat.openHint}
                            onClick={() => actions?.onOpen?.(code)}
                            className="no-drag inline-flex h-5 items-center gap-1 rounded px-1.5 text-ui-11 text-muted transition-colors duration-100 hover:bg-raised hover:text-fg"
                        >
                            <FileCode2 className="icon-ui-12" />
                            <span className="hidden @min-[34rem]:inline">{t.chat.open}</span>
                        </button>
                    ) : null}
                    {canSave ? (
                        <button
                            type="button"
                            data-testid="code-save"
                            title={t.chat.saveHint}
                            onClick={() => actions?.onSave?.(code)}
                            className={cn(
                                'no-drag inline-flex h-5 items-center gap-1 rounded px-1.5 text-ui-11 transition-colors duration-100',
                                canCreate ? 'text-muted hover:bg-raised hover:text-fg' : 'bg-accent font-medium text-accent-fg transition-[filter] hover:brightness-110',
                            )}
                        >
                            <GitBranchPlus className="icon-ui-12" />
                            <span className="hidden @min-[34rem]:inline">{t.chat.save}</span>
                        </button>
                    ) : null}
                </div>
            </div>
            <pre className="selectable max-h-[22rem] overflow-auto px-3 py-2 font-mono text-chat-sm leading-relaxed text-fg">
                <code>{shown}</code>
            </pre>
            {collapsible ? (
                <button
                    type="button"
                    title={expanded ? t.chat.codeCollapseHint : t.chat.codeExpandHint}
                    onClick={() => setExpanded((v) => !v)}
                    className="no-drag flex w-full items-center justify-center gap-1 border-t border-line bg-surface-2 py-1 text-ui-11 text-muted transition-colors hover:text-fg"
                >
                    <ChevronDown className={cn('icon-ui-12 transition-transform', expanded && 'rotate-180')} />
                    {expanded ? t.chat.codeCollapse : fmt(t.chat.codeExpand, {n: lines.length - COLLAPSED_LINES})}
                </button>
            ) : null}
        </div>
    )
}
