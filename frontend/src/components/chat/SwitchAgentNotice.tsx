import {Bot, X} from 'lucide-react'
import {useT} from '../../i18n'
import {Button, IconButton} from '../ui/Button'

// Aviso sobre la caja de mensaje (S12.c): en «Solo consulta» el modelo solo propone código, así que
// quien pide «hazme una caja» merece saberlo antes de enviar y poder pasar a «Agente» con un clic.
// Se descarta con la X; no vuelve para el mismo mensaje.
export function SwitchAgentNotice({onSwitch, onDismiss}: {onSwitch: () => void; onDismiss: () => void}) {
    const t = useT()
    const c = t.chat.switchAgent
    return (
        <div
            role="status"
            data-testid="switch-agent"
            className="mx-3 mb-1.5 flex items-start gap-2 rounded-lg border border-accent/40 bg-accent-soft px-2.5 py-2 text-ui-12 text-fg"
        >
            <Bot className="mt-0.5 icon-ui-14 shrink-0 text-accent" />
            <p className="min-w-0 flex-1 leading-snug">{c.text}</p>
            <Button variant="primary" title={c.hint} data-testid="switch-agent-button" className="shrink-0" onClick={onSwitch}>
                {c.button}
            </Button>
            <IconButton title={c.dismiss} className="-my-1 -mr-1 size-6" onClick={onDismiss}>
                <X className="icon-ui-14" />
            </IconButton>
        </div>
    )
}
