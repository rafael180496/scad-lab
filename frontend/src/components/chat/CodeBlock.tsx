import {useRef, useState} from 'react'
import {Check, Copy, GitBranchPlus} from 'lucide-react'
import {ClipboardSetText} from '../../../wailsjs/runtime/runtime'
import {useT} from '../../i18n'
import {cn} from '../../lib/cn'
import {isScadBlock} from './scadBlock'

// Copia al portapapeles. navigator.clipboard puede estar bloqueado en la
// webview (exige un contexto seguro y un gesto); el runtime de Wails lo hace
// por el sistema operativo y es el respaldo.
async function copyText(text: string): Promise<boolean> {
    try {
        await navigator.clipboard.writeText(text)
        return true
    } catch {
        try {
            return await ClipboardSetText(text)
        } catch {
            return false
        }
    }
}

export type CodeActions = {
    /** Guardar este bloque como checkpoint; sin ella (o con `undefined`) no hay botón. */
    onSave?: (code: string) => void
}

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

    const copy = async () => {
        if (!(await copyText(code))) return
        setCopied(true)
        clearTimeout(timer.current)
        timer.current = setTimeout(() => setCopied(false), 1500)
    }

    return (
        <div className="my-2 overflow-hidden rounded-lg border border-line bg-bg">
            <div className="flex h-7 items-center gap-1 border-b border-line bg-surface-2 pr-1 pl-2.5">
                <span className={cn('font-mono text-[10px] tracking-[0.12em] uppercase', scad ? 'text-accent' : 'text-faint')}>
                    {scad ? 'OpenSCAD' : lang || t.chat.code}
                </span>
                <div className="ml-auto flex items-center gap-0.5">
                    <button
                        type="button"
                        title={t.chat.copyHint}
                        onClick={copy}
                        className="no-drag inline-flex h-5 items-center gap-1 rounded px-1.5 text-[11px] text-muted transition-colors duration-100 hover:bg-raised hover:text-fg"
                    >
                        {copied ? <Check className="size-3 text-ok" /> : <Copy className="size-3" />}
                        {copied ? t.chat.copied : t.chat.copy}
                    </button>
                    {canSave ? (
                        <button
                            type="button"
                            title={t.chat.saveHint}
                            onClick={() => actions?.onSave?.(code)}
                            className="no-drag inline-flex h-5 items-center gap-1 rounded bg-accent px-1.5 text-[11px] font-medium text-accent-fg transition-[filter] duration-100 hover:brightness-110"
                        >
                            <GitBranchPlus className="size-3" />
                            {t.chat.save}
                        </button>
                    ) : null}
                </div>
            </div>
            <pre className="selectable max-h-[22rem] overflow-auto px-3 py-2 font-mono text-[12px] leading-relaxed text-fg">
                <code>{code}</code>
            </pre>
        </div>
    )
}
