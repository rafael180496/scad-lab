import {useSyncExternalStore} from 'react'
import {CircleAlert, CircleCheck} from 'lucide-react'
import {cn} from '../../lib/cn'

// Avisos efímeros. Store mínimo sin contexto: se llama toast.ok(...) desde
// cualquier lado, incluidos callbacks fuera de React.
type Toast = {id: number; kind: 'ok' | 'error'; text: string}

let items: Toast[] = []
let seq = 0
const subs = new Set<() => void>()
const emit = () => subs.forEach((s) => s())

function push(kind: Toast['kind'], text: string) {
    const id = ++seq
    items = [...items, {id, kind, text}]
    emit()
    // Los errores se quedan más: hay que poder leerlos.
    setTimeout(() => {
        items = items.filter((x) => x.id !== id)
        emit()
    }, kind === 'error' ? 7000 : 3000)
}

export const toast = {
    ok: (text: string) => push('ok', text),
    error: (text: string) => push('error', text),
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
                        'selectable pointer-events-auto flex items-start gap-2 rounded-lg border bg-raised px-3 py-2.5 text-[12.5px] shadow-lg shadow-black/20',
                        x.kind === 'error' ? 'border-danger/50' : 'border-line',
                    )}
                >
                    {x.kind === 'error' ? (
                        <CircleAlert className="mt-px size-4 shrink-0 text-danger" />
                    ) : (
                        <CircleCheck className="mt-px size-4 shrink-0 text-ok" />
                    )}
                    <span className="leading-snug">{x.text}</span>
                </div>
            ))}
        </div>
    )
}
