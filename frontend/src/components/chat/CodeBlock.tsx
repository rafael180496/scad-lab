import {useRef, useState} from 'react'
import {Check, ChevronDown, Copy, GitBranchPlus} from 'lucide-react'
import {copyText} from '../../lib/clipboard'
import {fmt, useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {isScadBlock} from './scadBlock'

export type CodeActions = {
    /** Guardar este bloque como checkpoint; sin ella (o con `undefined`) no hay botón. */
    onSave?: (code: string) => void
}

// Un bloque más largo que esto arranca plegado: un archivo entero dentro de una
// respuesta empujaría fuera de la vista el texto que lo explica (y, con Hermes,
// los pasos que vienen después).
const COLLAPSE_LINES = 30
const COLLAPSED_LINES = 14

// Bloque de código de una respuesta. Los de OpenSCAD son los que importan:
// llevan «Guardar como checkpoint» (solo cuando el cerco ya cerró —durante el
// streaming el código está a medias y guardarlo crearía un checkpoint roto—) y
// todos llevan «Copiar».
export function CodeBlock({lang, code, closed, actions}: {lang: string; code: string; closed: boolean; actions?: CodeActions}) {
    const t = useT()
    const [copied, setCopied] = useState(false)
    const timer = useRef<ReturnType<typeof setTimeout>>(undefined)
    const scad = isScadBlock(lang, code)
    const canSave = scad && closed && !!actions?.onSave && code.trim() !== ''
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
        <div className="my-2 overflow-hidden rounded-lg border border-line bg-bg">
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
                        {copied ? t.chat.copied : t.chat.copy}
                    </button>
                    {canSave ? (
                        <button
                            type="button"
                            title={t.chat.saveHint}
                            onClick={() => actions?.onSave?.(code)}
                            className="no-drag inline-flex h-5 items-center gap-1 rounded bg-accent px-1.5 text-ui-11 font-medium text-accent-fg transition-[filter] duration-100 hover:brightness-110"
                        >
                            <GitBranchPlus className="icon-ui-12" />
                            {t.chat.save}
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
