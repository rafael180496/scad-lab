import {useEffect, useRef, useSyncExternalStore, type ReactNode} from 'react'
import {X} from 'lucide-react'
import {Button, IconButton} from './Button'
import {cn} from '../../lib/cn'

// Modal sobre <dialog> nativo: foco atrapado, Escape y capa de fondo los da el
// navegador. Nunca confirm()/alert() nativos: no respetan el tema.
export function Dialog({
    open,
    onClose,
    title,
    closeLabel,
    children,
    footer,
    className,
    onCancel,
}: {
    open: boolean
    onClose: () => void
    title: string
    closeLabel: string
    children: ReactNode
    footer?: ReactNode
    className?: string
    /** Con esto, Escape no cierra el <dialog> por su cuenta: avisa acá (p. ej. para preguntar por cambios sin guardar). */
    onCancel?: () => void
}) {
    const ref = useRef<HTMLDialogElement>(null)
    useEffect(() => {
        const d = ref.current
        if (!d) return
        if (open && !d.open) {
            d.showModal()
            // showModal enfoca el primer control —la X de cerrar— y el anillo de
            // foco queda encendido como si se hubiera llegado por teclado. El
            // campo que deba arrancar con foco se marca con data-autofocus: el
            // autoFocus de React no sirve acá (no escribe el atributo en el DOM y
            // corre antes de que el <dialog> esté abierto, así que el foco se
            // pierde). Sin marca, se enfoca el diálogo.
            const target = d.querySelector<HTMLElement>('[data-autofocus]')
            if (target) target.focus()
            else d.focus()
        }
        if (!open && d.open) d.close()
    }, [open])
    return (
        <dialog
            ref={ref}
            tabIndex={-1}
            onClose={onClose}
            onCancel={
                onCancel
                    ? (e) => {
                          e.preventDefault()
                          onCancel()
                      }
                    : undefined
            }
            onClick={(e) => e.target === ref.current && onClose()}
            className={cn(
                'm-auto max-h-[86vh] outline-none w-[560px] max-w-[92vw] overflow-clip rounded-xl border border-line bg-surface p-0 text-fg shadow-2xl shadow-black/40 backdrop:bg-black/50',
                className,
            )}
        >
            {open ? (
                <div className="flex max-h-[86vh] flex-col">
                    <header className="flex h-11 shrink-0 items-center justify-between border-b border-line px-4">
                        <h2 className="text-ui-13-5 font-semibold">{title}</h2>
                        <IconButton title={closeLabel} onClick={onClose}>
                            <X className="icon-ui-16" />
                        </IconButton>
                    </header>
                    <div className="min-h-0 flex-1 overflow-auto">{children}</div>
                    {footer ? <footer className="flex shrink-0 flex-wrap justify-end gap-2 border-t border-line px-4 py-3">{footer}</footer> : null}
                </div>
            ) : null}
        </dialog>
    )
}

// ConfirmDialog reemplaza a confirm(): promesa que resuelve true/false.
let pending: ((ok: boolean) => void) | null = null
let state: {text: string; okLabel: string; cancelLabel: string; danger: boolean} | null = null
const listeners = new Set<() => void>()

// `danger` pinta el botón de confirmar como destructivo (borrar, descartar).
export function confirmDialog(text: string, okLabel: string, cancelLabel: string, opts?: {danger?: boolean}): Promise<boolean> {
    pending?.(false)
    state = {text, okLabel, cancelLabel, danger: !!opts?.danger}
    listeners.forEach((l) => l())
    return new Promise((resolve) => {
        pending = resolve
    })
}

export function ConfirmHost({title}: {title: string}) {
    const s = useSyncExternalStore(
        (cb) => {
            listeners.add(cb)
            return () => listeners.delete(cb)
        },
        () => state,
    )
    const close = (ok: boolean) => {
        pending?.(ok)
        pending = null
        state = null
        listeners.forEach((l) => l())
    }
    return (
        <Dialog
            open={!!s}
            onClose={() => close(false)}
            title={title}
            closeLabel={s?.cancelLabel ?? ''}
            className="w-[420px]"
            footer={
                <>
                    <Button title={s?.cancelLabel ?? ''} onClick={() => close(false)}>
                        {s?.cancelLabel}
                    </Button>
                    <Button variant={s?.danger ? 'destructive' : 'primary'} title={s?.okLabel ?? ''} onClick={() => close(true)}>
                        {s?.okLabel}
                    </Button>
                </>
            }
        >
            <p className="px-4 py-4 text-ui-13 leading-relaxed whitespace-pre-line text-muted">{s?.text}</p>
        </Dialog>
    )
}
