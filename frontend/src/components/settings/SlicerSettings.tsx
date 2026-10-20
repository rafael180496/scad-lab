import {CheckCircle2, CircleDashed} from 'lucide-react'
import type {main} from '../../../wailsjs/go/models'
import {ChooseFile} from '../../../wailsjs/go/main/App'
import {Button} from '../ui/Button'
import {toast} from '../ui/Toast'
import {fmt, useT} from '../../i18n'
import {errorText} from '../../lib/errors'

/**
 * Configuración → Impresión → Slicers (S14): los ocho slicers que ScadLab sabe abrir, con lo que se
 * detectó y la ruta que fijó el usuario (`settings.slicerPaths`). Sin ruta fijada rige la detección:
 * «Usar la detección» la borra. Los cambios se aplican al guardar la configuración, como el resto del
 * diálogo; `detected` es lo que DetectSlicers vio con lo ya guardado.
 */
export function SlicerSettings({
    detected,
    paths,
    onChange,
}: {
    detected: main.Slicer[]
    /** Las rutas del borrador (id → ruta); vacío o ausente = autodetectar. */
    paths: Record<string, string>
    onChange: (id: string, path: string) => void
}) {
    const t = useT()
    const k = t.settings.slicers
    const choose = async (s: main.Slicer) => {
        try {
            const f = await ChooseFile(fmt(k.chooseTitle, {name: s.name}))
            if (f) onChange(s.id, f)
        } catch (e) {
            toast.error(errorText(e))
        }
    }
    return (
        <section className="flex flex-col gap-2" data-testid="slicers">
            <h3 className="mt-2 text-ui-13 font-semibold">{k.title}</h3>
            <p className="text-ui-11-5 leading-relaxed text-faint">{k.intro}</p>
            <div className="divide-y divide-line rounded-lg border border-line">
                {detected.map((s) => {
                    const own = (paths[s.id] ?? '').trim()
                    return (
                        <div key={s.id} className="flex items-center gap-3 px-3 py-2">
                            <div className="min-w-0 flex-1">
                                <div className="font-mono text-ui-12">{s.name}</div>
                                {own ? (
                                    <div className="selectable truncate text-ui-11-5 text-info" title={own}>
                                        {k.configured}: {own}
                                    </div>
                                ) : s.found ? (
                                    <div className="selectable flex min-w-0 items-center gap-1.5 text-ui-11-5 text-ok" title={s.path}>
                                        <CheckCircle2 className="icon-ui-12 shrink-0" />
                                        <span className="truncate">
                                            {t.settings.found} · {s.path}
                                        </span>
                                    </div>
                                ) : (
                                    <div className="flex items-center gap-1.5 text-ui-11-5 text-faint">
                                        <CircleDashed className="icon-ui-12 shrink-0" />
                                        {t.settings.notFound}
                                    </div>
                                )}
                            </div>
                            <div className="flex shrink-0 items-center gap-1.5">
                                <Button className="h-6 px-2 text-ui-11-5" title={fmt(k.chooseHint, {name: s.name})} onClick={() => void choose(s)}>
                                    {t.settings.choose}
                                </Button>
                                {own ? (
                                    <Button variant="ghost" className="h-6 px-2 text-ui-11-5" title={fmt(k.autoHint, {name: s.name})} onClick={() => onChange(s.id, '')}>
                                        {k.auto}
                                    </Button>
                                ) : null}
                            </div>
                        </div>
                    )
                })}
            </div>
            <p className="text-ui-11 text-faint">{k.applyHint}</p>
        </section>
    )
}
