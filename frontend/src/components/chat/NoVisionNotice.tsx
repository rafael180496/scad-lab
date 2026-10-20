import {Cpu, EyeOff, Spline} from 'lucide-react'
import {useT} from '../../i18n'
import {Button} from '../ui/Button'

// Aviso sobre la caja de mensaje (S15): hay fotos adjuntas y el modelo elegido informó que no ve imágenes. En vez de
// dejar solo un «Enviar» apagado, ofrece las dos salidas: pasar a un modelo con visión (abre el selector de modelos ya
// filtrado) o enviar igual pidiendo el trazado de contorno, que trabaja con el archivo de la foto y no necesita visión
// (sirve para siluetas y logos). Desaparece solo cuando el modelo ve o se quitan las fotos.
export function NoVisionNotice({onSwitch, onTrace, disabled}: {onSwitch: () => void; onTrace: () => void; disabled?: boolean}) {
    const c = useT().chat.noVision
    return (
        <div role="status" data-testid="no-vision" className="mx-3 mb-1.5 flex flex-col gap-2 rounded-lg border border-warn/50 bg-warn/10 px-2.5 py-2 text-ui-12 text-fg">
            <p className="flex items-start gap-2 leading-snug">
                <EyeOff className="mt-0.5 icon-ui-14 shrink-0 text-warn" />
                <span className="min-w-0 flex-1">{c.text}</span>
            </p>
            <div className="flex flex-wrap items-center gap-2 pl-6">
                <Button variant="primary" data-testid="no-vision-switch" title={c.switchHint} disabled={disabled} onClick={onSwitch}>
                    <Cpu className="icon-ui-14" />
                    {c.switch}
                </Button>
                <Button data-testid="no-vision-trace" title={c.traceHint} disabled={disabled} onClick={onTrace}>
                    <Spline className="icon-ui-14" />
                    {c.trace}
                </Button>
            </div>
        </div>
    )
}
