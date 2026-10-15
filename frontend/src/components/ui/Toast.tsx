import {useSyncExternalStore} from 'react'
import {CircleAlert, CircleCheck, X} from 'lucide-react'
import {cn} from '../../lib/cn'
import {Button, IconButton} from './Button'

// Avisos efímeros. Store mínimo sin contexto: se llama toast.ok(...) desde
// cualquier lado, incluidos callbacks fuera de React.
// Un aviso puede traer acciones (p. ej. «Mostrar en el Finder» tras exportar):
// vive más tiempo, porque hay que alcanzar a leer y decidir, y se cierra a mano.
export type ToastAction = {label: string; title: string; run: () => void}
type Toast = {id: number; kind: 'ok' | 'error'; text: string; key?: string; actions?: ToastAction[]; closeLabel?: string}

let items: Toast[] = []
let seq = 0
const subs = new Set<() => void>()
const emit = () => subs.forEach((s) => s())

// `key` evita que un aviso repetido se apile (p. ej. el de cada paso del
// zoom con ⌘+): reemplaza al anterior con la misma clave.
function dismiss(id: number) {
    items = items.filter((x) => x.id !== id)
    emit()
}

function push(kind: Toast['kind'], text: string, key?: string, actions?: ToastAction[], closeLabel?: string) {
    const id = ++seq
    items = [...items.filter((x) => key === undefined || x.key !== key), {id, kind, text, key, actions, closeLabel}]
    emit()
    // Los errores se quedan más: hay que poder leerlos. Con acciones, aún más.
    setTimeout(() => dismiss(id), actions?.length ? 15000 : kind === 'error' ? 7000 : 3000)
}

export const toast = {
    ok: (text: string, key?: string) => push('ok', text, key),
    error: (text: string, key?: string) => push('error', text, key),
    // Aviso con botones; `closeLabel` es el `title` de la X que lo cierra.
    withActions: (text: string, actions: ToastAction[], closeLabel: string, key?: string) => push('ok', text, key, actions, closeLabel),
}

export function Toaster() {
    const list = useSyncExternalStore(
        (cb) => {
            subs.add(cb)
            return () => subs.delete(cb)
        },
        () => items,
    )
    return (
        <div className="pointer-events-none fixed right-4 bottom-4 z-50 flex w-[360px] flex-col gap-2" aria-live="polite">
            {list.map((x) => (
                <div
                    key={x.id}
                    className={cn(
                        'selectable pointer-events-auto flex items-start gap-2 rounded-lg border bg-raised px-3 py-2.5 text-ui-12-5 shadow-lg shadow-black/20',
                        x.kind === 'error' ? 'border-danger/50' : 'border-line',
                    )}
                >
                    {x.kind === 'error' ? (
                        <CircleAlert className="mt-px icon-ui-16 shrink-0 text-danger" />
                    ) : (
                        <CircleCheck className="mt-px icon-ui-16 shrink-0 text-ok" />
                    )}
                    <div className="flex min-w-0 flex-1 flex-col gap-2">
                        <span className="leading-snug break-words">{x.text}</span>
                        {x.actions?.length ? (
                            <div className="flex flex-wrap gap-1.5">
                                {x.actions.map((a) => (
                                    <Button
                                        key={a.label}
                                        title={a.title}
                                        onClick={() => {
                                            dismiss(x.id)
                                            a.run()
                                        }}
                                    >
                                        {a.label}
                                    </Button>
                                ))}
                            </div>
                        ) : null}
                    </div>
                    {x.actions?.length ? (
                        <IconButton title={x.closeLabel ?? ''} className="-mt-1 -mr-1.5 size-6" onClick={() => dismiss(x.id)}>
                            <X className="icon-ui-14" />
                        </IconButton>
                    ) : null}
                </div>
            ))}
        </div>
    )
}
